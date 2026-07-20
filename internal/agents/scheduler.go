package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	autobotagent "github.com/russellhaering/wasmdb/internal/autobot/agent"
	"github.com/russellhaering/wasmdb/internal/autobot/mcpx"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/functions"
	"github.com/russellhaering/wasmdb/internal/mcpservers"
)

// ErrAgentAlreadyRunning is returned by TriggerAgent when the named agent is
// already executing, so callers can distinguish an expected reentrancy refusal
// from a real failure and log it accordingly.
var ErrAgentAlreadyRunning = errors.New("agent is already running; refusing reentrant trigger")

// SchedulerConfig holds configuration for the agent scheduler.
type SchedulerConfig struct {
	Registry        *database.Registry
	AnthropicAPIKey string
	Model           string
	SubAgentModel   string
	FnEngine        *functions.Engine
	FnStore         *functions.Store
	MCPServerStore  *mcpservers.Store
}

// Scheduler runs background agents on their configured schedules.
type Scheduler struct {
	store  *Store
	config SchedulerConfig

	mu      sync.Mutex
	timers  map[string]*time.Timer // agent name -> timer
	running map[string]bool        // agent name -> currently executing
	cancel  context.CancelFunc
	ctx     context.Context
	wg      sync.WaitGroup
}

// NewScheduler creates a new agent scheduler.
func NewScheduler(store *Store, config SchedulerConfig) *Scheduler {
	return &Scheduler{
		store:   store,
		config:  config,
		timers:  make(map[string]*time.Timer),
		running: make(map[string]bool),
	}
}

// Start begins the scheduler, loading all enabled agents and scheduling them.
func (s *Scheduler) Start(ctx context.Context) {
	s.ctx, s.cancel = context.WithCancel(ctx)

	slog.Info("agent scheduler starting")

	// Initial load of agents.
	s.reload()

	// Periodically reload agent configs to pick up changes.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.reload()
			}
		}
	}()
}

// Stop gracefully shuts down the scheduler.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}

	s.mu.Lock()
	for name, timer := range s.timers {
		timer.Stop()
		delete(s.timers, name)
	}
	s.mu.Unlock()

	s.wg.Wait()
	slog.Info("agent scheduler stopped")
}

// RunAgent triggers a single agent run immediately (e.g., from a webhook).
// Returns the run result.
func (s *Scheduler) RunAgent(ctx context.Context, agentName string) (*AgentRun, error) {
	ag, err := s.store.Get(ctx, agentName)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	if ag == nil {
		return nil, fmt.Errorf("agent %q not found", agentName)
	}

	return s.executeAgent(ctx, ag)
}

// TriggerAgent runs a named agent immediately, but refuses to start it if the
// same agent is already running (whether from its timer or another trigger).
// This is the safe entry point for user/LLM-initiated triggers: RunAgent alone
// does not consult the running set, so a self-trigger (an agent triggering
// itself) would recurse without this guard. The running flag is cleared when
// the run completes.
func (s *Scheduler) TriggerAgent(ctx context.Context, name string) (*AgentRun, error) {
	s.mu.Lock()
	if s.running[name] {
		s.mu.Unlock()
		return nil, fmt.Errorf("agent %q: %w", name, ErrAgentAlreadyRunning)
	}
	s.running[name] = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.running, name)
		s.mu.Unlock()
	}()

	return s.RunAgent(ctx, name)
}

// reload loads enabled agents and reconciles scheduled timers.
func (s *Scheduler) reload() {
	agents, err := s.store.ListAllEnabled(s.ctx)
	if err != nil {
		slog.Error("scheduler: failed to list enabled agents", "err", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Build set of enabled agent names.
	enabled := make(map[string]*Agent, len(agents))
	for _, ag := range agents {
		if ag.TriggerType == "timer" {
			enabled[ag.Name] = ag
		}
	}

	// Cancel timers for agents that are no longer enabled.
	for name, timer := range s.timers {
		if _, ok := enabled[name]; !ok {
			timer.Stop()
			delete(s.timers, name)
			slog.Info("scheduler: unscheduled agent", "agent", name)
		}
	}

	// Schedule new or updated agents.
	for name, ag := range enabled {
		if _, ok := s.timers[name]; ok {
			// Already scheduled; skip. Timer will re-arm itself after each run.
			continue
		}
		s.scheduleAgent(ag)
	}
}

// scheduleAgent sets up a timer for an agent. Must be called with s.mu held.
func (s *Scheduler) scheduleAgent(ag *Agent) {
	duration, err := time.ParseDuration(ag.Schedule)
	if err != nil {
		slog.Error("scheduler: invalid schedule for agent", "agent", ag.Name, "schedule", ag.Schedule, "err", err)
		return
	}

	slog.Info("scheduler: scheduling agent", "agent", ag.Name, "interval", duration)

	timer := time.AfterFunc(duration, func() {
		s.runAndReschedule(ag.Name)
	})
	s.timers[ag.Name] = timer
}

// runAndReschedule executes an agent and re-schedules the next run.
func (s *Scheduler) runAndReschedule(agentName string) {
	// Check if context is still valid.
	if s.ctx.Err() != nil {
		return
	}

	// Check if already running.
	s.mu.Lock()
	if s.running[agentName] {
		s.mu.Unlock()
		slog.Warn("scheduler: agent still running, skipping", "agent", agentName)
		return
	}
	s.running[agentName] = true
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.running, agentName)
			s.mu.Unlock()
		}()

		// Re-fetch the agent to get latest config.
		ag, err := s.store.Get(s.ctx, agentName)
		if err != nil {
			slog.Error("scheduler: failed to fetch agent", "agent", agentName, "err", err)
			s.reschedule(agentName)
			return
		}
		if ag == nil || !ag.Enabled {
			slog.Info("scheduler: agent disabled or deleted, removing", "agent", agentName)
			s.mu.Lock()
			if t, ok := s.timers[agentName]; ok {
				t.Stop()
				delete(s.timers, agentName)
			}
			s.mu.Unlock()
			return
		}

		run, err := s.executeAgent(s.ctx, ag)
		if err != nil {
			slog.Error("scheduler: agent execution failed", "agent", agentName, "err", err)
		} else {
			slog.Info("scheduler: agent run completed",
				"agent", agentName,
				"status", run.Status,
				"duration_ms", run.DurationMS,
				"input_tokens", run.InputTokens,
				"output_tokens", run.OutputTokens,
			)
		}

		s.reschedule(agentName)
	}()
}

// reschedule re-fetches the agent's schedule and re-arms the timer.
func (s *Scheduler) reschedule(agentName string) {
	ag, err := s.store.Get(s.ctx, agentName)
	if err != nil || ag == nil || !ag.Enabled {
		s.mu.Lock()
		delete(s.timers, agentName)
		s.mu.Unlock()
		return
	}

	duration, err := time.ParseDuration(ag.Schedule)
	if err != nil {
		slog.Error("scheduler: invalid schedule on reschedule", "agent", agentName, "schedule", ag.Schedule)
		s.mu.Lock()
		delete(s.timers, agentName)
		s.mu.Unlock()
		return
	}

	s.mu.Lock()
	if s.ctx.Err() == nil {
		s.timers[agentName] = time.AfterFunc(duration, func() {
			s.runAndReschedule(agentName)
		})
	}
	s.mu.Unlock()
}

// executeAgent runs a single agent and records the result.
func (s *Scheduler) executeAgent(ctx context.Context, ag *Agent) (*AgentRun, error) {
	start := time.Now()

	slog.Info("scheduler: executing agent", "agent", ag.Name)

	// Create a temporary MCP server group with the same tools as the chat agent.
	result, err := s.runAgentWithTools(ctx, ag)

	completedAt := time.Now()
	durationMS := completedAt.Sub(start).Milliseconds()

	var (
		status       = "completed"
		output       string
		errorMsg     string
		inputTokens  int64
		outputTokens int64
	)

	if err != nil {
		status = "failed"
		errorMsg = err.Error()
	} else {
		output = result.Text
		inputTokens = result.TotalInputTokens
		outputTokens = result.TotalOutputTokens
	}

	run, recordErr := s.store.RecordRun(ctx, ag.ID, ag.Name, status, output, errorMsg, inputTokens, outputTokens, durationMS, start, completedAt)
	if recordErr != nil {
		slog.Error("scheduler: failed to record agent run", "agent", ag.Name, "err", recordErr)
		// Still return the execution error if any.
		if err != nil {
			return nil, err
		}
		return nil, recordErr
	}

	if err != nil {
		return run, err
	}

	return run, nil
}

// runAgentWithTools creates a fresh autobot agent with MCP tools and runs the prompt.
func (s *Scheduler) runAgentWithTools(ctx context.Context, ag *Agent) (*autobotagent.Result, error) {
	if s.config.AnthropicAPIKey == "" {
		return nil, fmt.Errorf("anthropic API key not configured")
	}

	model := s.config.Model
	if model == "" {
		model = "claude-sonnet-4-5-20250929"
	}

	// Import the agent package to create the table server.
	// We use a local import alias to avoid the circular reference
	// with the agent package.
	servers, cleanup, err := s.buildServerGroup(ctx)
	if err != nil {
		return nil, fmt.Errorf("building server group: %w", err)
	}
	defer cleanup()

	maxTurns := ag.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 20
	}

	agentBot := autobotagent.NewAgent(autobotagent.Config{
		Model:        model,
		APIKey:       s.config.AnthropicAPIKey,
		SystemPrompt: buildAgentSystemPrompt(ag),
		MaxTokens:    16384,
		MaxTurns:     maxTurns,
	}, servers)

	session, err := agentBot.NewSession(ctx, ag.Prompt)
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	result, err := session.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("running agent: %w", err)
	}

	return result, nil
}

// buildServerGroup creates a fresh MCP server group with the database tools.
func (s *Scheduler) buildServerGroup(ctx context.Context) (*mcpx.ServerGroup, func(), error) {
	// We need to avoid importing internal/agent directly to prevent circular deps.
	// Instead, create a minimal MCP server with just the core DB tools.
	// The agent package's NewTableServer is in internal/agent which we can't import here.
	// We'll use a factory function that gets set during initialization.
	if serverFactory == nil {
		return nil, nil, fmt.Errorf("agent server factory not configured")
	}

	return serverFactory(ctx, s.config)
}

// ServerFactory is a function that creates an MCP server group for background agents.
type ServerFactory func(ctx context.Context, cfg SchedulerConfig) (*mcpx.ServerGroup, func(), error)

var serverFactory ServerFactory

// SetServerFactory sets the factory function used to create MCP server groups for background agents.
func SetServerFactory(f ServerFactory) {
	serverFactory = f
}

func buildAgentSystemPrompt(ag *Agent) string {
	return fmt.Sprintf(`You are a background agent named %q running an automated task.
%s

You have access to tools for managing tables, documents, executing code, and other database operations.
Complete the task described in your prompt. Be thorough but concise in your output.
You are running autonomously without user interaction, so do not ask questions or wait for confirmation.
Proceed with your best judgment to complete the task.`, ag.Name, agentDescription(ag))
}

func agentDescription(ag *Agent) string {
	if ag.Description != "" {
		return "Description: " + ag.Description
	}
	return ""
}

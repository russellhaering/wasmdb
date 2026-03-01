package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/index"
)

const (
	agentsTable    = "_agents"
	agentRunsTable = "_agent_runs"
)

// Agent represents a background agent configuration.
type Agent struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Prompt      string    `json:"prompt"`
	Schedule    string    `json:"schedule"`
	TriggerType string    `json:"trigger_type"`
	Enabled     bool      `json:"enabled"`
	MaxTurns    int       `json:"max_turns,omitempty"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AgentRun represents a single execution of a background agent.
type AgentRun struct {
	ID           string    `json:"id"`
	AgentID      string    `json:"agent_id"`
	AgentName    string    `json:"agent_name"`
	Status       string    `json:"status"`
	Output       string    `json:"output,omitempty"`
	Error        string    `json:"error,omitempty"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	DurationMS   int64     `json:"duration_ms"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
}

// Store handles CRUD operations for background agents and their run history.
type Store struct {
	registry *database.Registry
}

// NewStore creates a new agent store.
func NewStore(registry *database.Registry) *Store {
	return &Store{registry: registry}
}

// Create creates a new background agent.
func (s *Store) Create(ctx context.Context, name, description, prompt, schedule, triggerType string, enabled bool, maxTurns int, userID string) (*Agent, error) {
	if err := validateTriggerType(triggerType); err != nil {
		return nil, err
	}
	if err := validateSchedule(schedule); err != nil {
		return nil, err
	}

	existing, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("agent %q already exists", name)
	}

	tbl, err := s.registry.GetTable(ctx, agentsTable)
	if err != nil {
		return nil, fmt.Errorf("get agents table: %w", err)
	}

	now := time.Now().UTC()
	doc := &document.Document{
		Attributes: map[string]any{
			"name":         name,
			"description":  description,
			"prompt":       prompt,
			"schedule":     schedule,
			"trigger_type": triggerType,
			"enabled":      enabled,
			"max_turns":    maxTurns,
			"created_by":   userID,
			"updated_at":   now.Format(time.RFC3339),
		},
	}

	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	return &Agent{
		ID:          doc.ID,
		Name:        name,
		Description: description,
		Prompt:      prompt,
		Schedule:    schedule,
		TriggerType: triggerType,
		Enabled:     enabled,
		MaxTurns:    maxTurns,
		CreatedBy:   userID,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   now,
	}, nil
}

// Get retrieves an agent by name.
func (s *Store) Get(ctx context.Context, name string) (*Agent, error) {
	tbl, err := s.registry.GetTable(ctx, agentsTable)
	if err != nil {
		return nil, fmt.Errorf("get agents table: %w", err)
	}

	docs, err := tbl.SearchAttributes(ctx, []index.Filter{
		{Field: "name", Op: index.OpEq, Value: name},
	}, 1, 0)
	if err != nil {
		return nil, fmt.Errorf("search agent: %w", err)
	}
	if len(docs) == 0 {
		return nil, nil
	}

	return docToAgent(docs[0]), nil
}

// GetByID retrieves an agent by ID.
func (s *Store) GetByID(ctx context.Context, id string) (*Agent, error) {
	tbl, err := s.registry.GetTable(ctx, agentsTable)
	if err != nil {
		return nil, fmt.Errorf("get agents table: %w", err)
	}

	doc, err := tbl.GetDocument(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get agent by id: %w", err)
	}
	if doc == nil {
		return nil, nil
	}

	return docToAgent(doc), nil
}

// List returns all agents.
func (s *Store) List(ctx context.Context) ([]*Agent, error) {
	tbl, err := s.registry.GetTable(ctx, agentsTable)
	if err != nil {
		return nil, fmt.Errorf("get agents table: %w", err)
	}

	docs, _, err := tbl.ListDocuments(ctx, 1000, "")
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}

	agents := make([]*Agent, 0, len(docs))
	for _, doc := range docs {
		agents = append(agents, docToAgent(doc))
	}
	return agents, nil
}

// ListAllEnabled returns all enabled agents.
func (s *Store) ListAllEnabled(ctx context.Context) ([]*Agent, error) {
	tbl, err := s.registry.GetTable(ctx, agentsTable)
	if err != nil {
		return nil, fmt.Errorf("get agents table: %w", err)
	}

	docs, err := tbl.SearchAttributes(ctx, []index.Filter{
		{Field: "enabled", Op: index.OpEq, Value: true},
	}, 1000, 0)
	if err != nil {
		return nil, fmt.Errorf("search enabled agents: %w", err)
	}

	agents := make([]*Agent, 0, len(docs))
	for _, doc := range docs {
		agents = append(agents, docToAgent(doc))
	}
	return agents, nil
}

// Update updates an agent configuration.
func (s *Store) Update(ctx context.Context, name, description, prompt, schedule, triggerType string, enabled bool, maxTurns int) (*Agent, error) {
	if err := validateTriggerType(triggerType); err != nil {
		return nil, err
	}
	if err := validateSchedule(schedule); err != nil {
		return nil, err
	}

	existing, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("agent %q not found", name)
	}

	tbl, err := s.registry.GetTable(ctx, agentsTable)
	if err != nil {
		return nil, fmt.Errorf("get agents table: %w", err)
	}

	now := time.Now().UTC()
	doc := &document.Document{
		ID: existing.ID,
		Attributes: map[string]any{
			"name":         name,
			"description":  description,
			"prompt":       prompt,
			"schedule":     schedule,
			"trigger_type": triggerType,
			"enabled":      enabled,
			"max_turns":    maxTurns,
			"created_by":   existing.CreatedBy,
			"updated_at":   now.Format(time.RFC3339),
		},
	}

	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("update agent: %w", err)
	}

	existing.Description = description
	existing.Prompt = prompt
	existing.Schedule = schedule
	existing.TriggerType = triggerType
	existing.Enabled = enabled
	existing.MaxTurns = maxTurns
	existing.UpdatedAt = now
	return existing, nil
}

// Delete removes an agent by name.
func (s *Store) Delete(ctx context.Context, name string) error {
	existing, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("agent %q not found", name)
	}

	tbl, err := s.registry.GetTable(ctx, agentsTable)
	if err != nil {
		return fmt.Errorf("get agents table: %w", err)
	}

	return tbl.DeleteDocument(ctx, existing.ID)
}

// RecordRun records a completed agent run.
func (s *Store) RecordRun(ctx context.Context, agentID, agentName, status, output, errorMsg string, inputTokens, outputTokens, durationMS int64, startedAt, completedAt time.Time) (*AgentRun, error) {
	tbl, err := s.registry.GetTable(ctx, agentRunsTable)
	if err != nil {
		return nil, fmt.Errorf("get agent_runs table: %w", err)
	}

	doc := &document.Document{
		Attributes: map[string]any{
			"agent_id":      agentID,
			"agent_name":    agentName,
			"status":        status,
			"output":        output,
			"error":         errorMsg,
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
			"duration_ms":   durationMS,
			"started_at":    startedAt.Format(time.RFC3339),
			"completed_at":  completedAt.Format(time.RFC3339),
		},
	}

	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("record agent run: %w", err)
	}

	return &AgentRun{
		ID:           doc.ID,
		AgentID:      agentID,
		AgentName:    agentName,
		Status:       status,
		Output:       output,
		Error:        errorMsg,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		DurationMS:   durationMS,
		StartedAt:    startedAt,
		CompletedAt:  completedAt,
	}, nil
}

// ListRuns returns recent runs for a given agent, ordered by most recent first.
func (s *Store) ListRuns(ctx context.Context, agentName string, limit int) ([]*AgentRun, error) {
	if limit <= 0 {
		limit = 20
	}

	tbl, err := s.registry.GetTable(ctx, agentRunsTable)
	if err != nil {
		return nil, fmt.Errorf("get agent_runs table: %w", err)
	}

	docs, err := tbl.SearchAttributes(ctx, []index.Filter{
		{Field: "agent_name", Op: index.OpEq, Value: agentName},
	}, limit, 0)
	if err != nil {
		return nil, fmt.Errorf("search agent runs: %w", err)
	}

	runs := make([]*AgentRun, 0, len(docs))
	for _, doc := range docs {
		runs = append(runs, docToAgentRun(doc))
	}
	return runs, nil
}

func validateTriggerType(triggerType string) error {
	if triggerType != "timer" {
		return fmt.Errorf("invalid trigger_type %q: only \"timer\" is supported", triggerType)
	}
	return nil
}

func validateSchedule(schedule string) error {
	d, err := time.ParseDuration(schedule)
	if err != nil {
		return fmt.Errorf("invalid schedule %q: must be a valid Go duration (e.g. \"5m\", \"1h\"): %w", schedule, err)
	}
	if d < time.Minute {
		return fmt.Errorf("invalid schedule %q: must be at least 1 minute", schedule)
	}
	return nil
}

func docToAgent(doc *document.Document) *Agent {
	a := &Agent{ID: doc.ID, CreatedAt: doc.CreatedAt}
	if v, ok := doc.Attributes["name"].(string); ok {
		a.Name = v
	}
	if v, ok := doc.Attributes["description"].(string); ok {
		a.Description = v
	}
	if v, ok := doc.Attributes["prompt"].(string); ok {
		a.Prompt = v
	}
	if v, ok := doc.Attributes["schedule"].(string); ok {
		a.Schedule = v
	}
	if v, ok := doc.Attributes["trigger_type"].(string); ok {
		a.TriggerType = v
	}
	if v, ok := doc.Attributes["enabled"].(bool); ok {
		a.Enabled = v
	}
	a.MaxTurns = anyToInt(doc.Attributes["max_turns"])
	if v, ok := doc.Attributes["created_by"].(string); ok {
		a.CreatedBy = v
	}
	if v, ok := doc.Attributes["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			a.UpdatedAt = t
		}
	}
	return a
}

func docToAgentRun(doc *document.Document) *AgentRun {
	r := &AgentRun{ID: doc.ID}
	if v, ok := doc.Attributes["agent_id"].(string); ok {
		r.AgentID = v
	}
	if v, ok := doc.Attributes["agent_name"].(string); ok {
		r.AgentName = v
	}
	if v, ok := doc.Attributes["status"].(string); ok {
		r.Status = v
	}
	if v, ok := doc.Attributes["output"].(string); ok {
		r.Output = v
	}
	if v, ok := doc.Attributes["error"].(string); ok {
		r.Error = v
	}
	r.InputTokens = anyToInt64(doc.Attributes["input_tokens"])
	r.OutputTokens = anyToInt64(doc.Attributes["output_tokens"])
	r.DurationMS = anyToInt64(doc.Attributes["duration_ms"])
	if v, ok := doc.Attributes["started_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			r.StartedAt = t
		}
	}
	if v, ok := doc.Attributes["completed_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			r.CompletedAt = t
		}
	}
	return r
}

func anyToInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		return 0
	}
}

func anyToInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	default:
		return 0
	}
}

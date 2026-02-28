// Package agent provides the core agent loop for the autobot framework.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/russellhaering/wasmdb/internal/autobot/mcpx"
)

// Config holds configuration for creating an Agent.
type Config struct {
	// Model is the Claude model to use (e.g., "claude-sonnet-4-5-20250929").
	Model string

	// APIKey is the Anthropic API key.
	APIKey string

	// SystemPrompt is the base system prompt for the agent.
	SystemPrompt string

	// MaxTokens is the maximum number of tokens to generate per response.
	MaxTokens int64

	// MaxTurns is the maximum number of agent turns (0 = unlimited).
	MaxTurns int

	// AllowedTools restricts tools to only these names (nil = all).
	AllowedTools map[string]bool

	// DisallowedTools excludes these tool names (nil = none excluded).
	DisallowedTools map[string]bool

	// Logger for the agent.
	Logger *slog.Logger
}

// Agent holds configuration and MCP servers for creating sessions.
type Agent struct {
	config  Config
	servers *mcpx.ServerGroup
	client  anthropic.Client
}

// NewAgent creates a new Agent with the given config and MCP server group.
func NewAgent(config Config, servers *mcpx.ServerGroup) *Agent {
	if config.MaxTokens == 0 {
		config.MaxTokens = 16384
	}
	if config.Model == "" {
		config.Model = string(anthropic.ModelClaudeSonnet4_5_20250929)
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	var opts []option.RequestOption
	if config.APIKey != "" {
		opts = append(opts, option.WithAPIKey(config.APIKey))
	}
	client := anthropic.NewClient(opts...)

	return &Agent{
		config:  config,
		servers: servers,
		client:  client,
	}
}

// Event represents a streaming event from the agent.
type Event struct {
	// Type is the event type.
	Type EventType

	// Text is set for TextDelta events.
	Text string

	// ToolName is set for ToolCallStart events.
	ToolName string

	// ToolID is set for ToolCallStart and ToolResult events.
	ToolID string

	// ToolInput is set for ToolCallStart events (raw JSON).
	ToolInput json.RawMessage

	// ToolResult is set for ToolResult events.
	ToolResult string

	// ToolIsError is set for ToolResult events.
	ToolIsError bool

	// Error is set for Error events.
	Error error
}

// EventType represents the type of streaming event.
type EventType int

const (
	EventTextDelta EventType = iota
	EventToolCallStart
	EventToolResult
	EventDone
	EventError
)

// Result holds the final result of a session run.
type Result struct {
	// Text is the final text output.
	Text string

	// StopReason is why the agent stopped.
	StopReason string

	// TotalInputTokens is the total input tokens used.
	TotalInputTokens int64

	// TotalOutputTokens is the total output tokens used.
	TotalOutputTokens int64
}

// Session represents a single agent conversation.
type Session struct {
	agent    *Agent
	messages []anthropic.MessageParam
	tools    []anthropic.ToolUnionParam

	totalInputTokens  int64
	totalOutputTokens int64
}

// NewSession creates a new session with the given user prompt.
func (a *Agent) NewSession(ctx context.Context, prompt string) (*Session, error) {
	tools, err := a.servers.ListTools(ctx, a.config.AllowedTools, a.config.DisallowedTools)
	if err != nil {
		return nil, fmt.Errorf("listing tools: %w", err)
	}

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
	}

	return &Session{
		agent:    a,
		messages: messages,
		tools:    tools,
	}, nil
}

// NewSessionWithHistory creates a new session with existing message history plus a new prompt.
func (a *Agent) NewSessionWithHistory(ctx context.Context, history []anthropic.MessageParam, prompt string) (*Session, error) {
	tools, err := a.servers.ListTools(ctx, a.config.AllowedTools, a.config.DisallowedTools)
	if err != nil {
		return nil, fmt.Errorf("listing tools: %w", err)
	}

	// Trim history if it's getting too long to leave room for the new message
	// and the response. Target keeping ~60% of context budget for history.
	trimmed := trimHistory(history, 150000)

	messages := make([]anthropic.MessageParam, len(trimmed))
	copy(messages, trimmed)
	messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)))

	return &Session{
		agent:    a,
		messages: messages,
		tools:    tools,
	}, nil
}

// estimateTokens gives a rough token count for a message list.
// Uses ~4 chars per token as a heuristic.
func estimateTokens(messages []anthropic.MessageParam) int {
	total := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.OfText != nil {
				total += len(block.OfText.Text) / 4
			} else if block.OfToolUse != nil {
				inputLen := 0
				if b, ok := block.OfToolUse.Input.(json.RawMessage); ok {
					inputLen = len(b)
				} else if b, err := json.Marshal(block.OfToolUse.Input); err == nil {
					inputLen = len(b)
				}
				total += len(block.OfToolUse.Name)/4 + inputLen/4
			} else if block.OfToolResult != nil {
				for _, c := range block.OfToolResult.Content {
					if c.OfText != nil {
						total += len(c.OfText.Text) / 4
					}
				}
			}
		}
	}
	return total
}

// trimHistory removes the oldest messages (from the front) to fit within
// a target token budget. It preserves message pairing (user/assistant) and
// ensures the first message is always a user message (API requirement).
func trimHistory(messages []anthropic.MessageParam, maxTokens int) []anthropic.MessageParam {
	if len(messages) == 0 {
		return messages
	}

	tokens := estimateTokens(messages)
	if tokens <= maxTokens {
		return messages
	}

	// Drop messages from the front until we're under budget.
	// Keep at least the last 2 messages for minimal context.
	result := messages
	for len(result) > 2 && estimateTokens(result) > maxTokens {
		result = result[1:]
	}

	// Ensure first message is a user message (API requirement).
	for len(result) > 1 && result[0].Role != anthropic.MessageParamRoleUser {
		result = result[1:]
	}

	if len(result) < len(messages) {
		slog.Info("trimmed chat history",
			"original_messages", len(messages),
			"trimmed_messages", len(result),
			"estimated_tokens", estimateTokens(result),
		)
	}

	return result
}

// Messages returns the current message history for the session.
func (s *Session) Messages() []anthropic.MessageParam {
	return s.messages
}

// Inject adds a message to the conversation mid-run.
func (s *Session) Inject(msg anthropic.MessageParam) {
	s.messages = append(s.messages, msg)
}

// Run runs the session to completion (non-streaming).
func (s *Session) Run(ctx context.Context) (*Result, error) {
	result := &Result{}
	turns := 0

	for {
		if s.agent.config.MaxTurns > 0 && turns >= s.agent.config.MaxTurns {
			result.Text += "\n\n[Reached the maximum number of tool-use steps for this response.]"
			result.StopReason = "max_turns"
			break
		}
		turns++

		msg, err := s.sendMessage(ctx)
		if err != nil {
			return nil, fmt.Errorf("sending message (turn %d): %w", turns, err)
		}

		s.totalInputTokens += msg.Usage.InputTokens
		s.totalOutputTokens += msg.Usage.OutputTokens

		// Extract text from the response
		for _, block := range msg.Content {
			switch block.Type {
			case "text":
				tb := block.AsText()
				result.Text += tb.Text
			}
		}

		// Check if we need to process tool calls
		if msg.StopReason == "tool_use" {
			if err := s.processToolCalls(ctx, msg); err != nil {
				return nil, fmt.Errorf("processing tool calls (turn %d): %w", turns, err)
			}
			continue
		}

		// Append the final assistant message so history is complete for
		// subsequent turns. Tool-use turns are already appended inside
		// processToolCalls; this covers the terminal turn.
		s.messages = append(s.messages, assistantMessageFromResponse(msg))

		if msg.StopReason == "max_tokens" {
			result.Text += "\n\n[Response truncated due to length.]"
		}

		result.StopReason = string(msg.StopReason)
		break
	}

	result.TotalInputTokens = s.totalInputTokens
	result.TotalOutputTokens = s.totalOutputTokens
	return result, nil
}

// Stream runs the session and streams events back via a channel.
func (s *Session) Stream(ctx context.Context) <-chan Event {
	events := make(chan Event, 64)

	go func() {
		defer close(events)
		turns := 0

		for {
			if s.agent.config.MaxTurns > 0 && turns >= s.agent.config.MaxTurns {
				// Notify the user that the agent hit the turn limit.
				events <- Event{
					Type: EventTextDelta,
					Text: "\n\n[Reached the maximum number of tool-use steps for this response. Please send another message to continue.]",
				}
				events <- Event{Type: EventDone}
				return
			}
			turns++

			msg, err := s.streamMessage(ctx, events)
			if err != nil {
				events <- Event{Type: EventError, Error: err}
				return
			}

			s.totalInputTokens += msg.Usage.InputTokens
			s.totalOutputTokens += msg.Usage.OutputTokens

			if msg.StopReason == "tool_use" {
				if err := s.processToolCallsStreaming(ctx, msg, events); err != nil {
					events <- Event{Type: EventError, Error: err}
					return
				}
				continue
			}

			// Append the final assistant message so history is complete for
			// subsequent turns. Tool-use turns are already appended inside
			// processToolCallsStreaming; this covers the terminal turn.
			s.messages = append(s.messages, assistantMessageFromResponse(msg))

			if msg.StopReason == "max_tokens" {
				events <- Event{
					Type: EventTextDelta,
					Text: "\n\n[Response truncated due to length. Send another message to continue.]",
				}
			}

			events <- Event{Type: EventDone}
			return
		}
	}()

	return events
}

// sendMessage sends the current messages to Claude and returns the response.
func (s *Session) sendMessage(ctx context.Context) (*anthropic.Message, error) {
	params := s.buildParams()
	return s.agent.client.Messages.New(ctx, params)
}

// streamMessage sends messages with real SSE streaming, emitting text deltas
// as they arrive from the API.
func (s *Session) streamMessage(ctx context.Context, events chan<- Event) (*anthropic.Message, error) {
	params := s.buildParams()
	stream := s.agent.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	var msg anthropic.Message
	for stream.Next() {
		evt := stream.Current()
		msg.Accumulate(evt)

		switch variant := evt.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			switch delta := variant.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				if delta.Text != "" {
					events <- Event{
						Type: EventTextDelta,
						Text: delta.Text,
					}
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}

	return &msg, nil
}

// buildParams constructs the MessageNewParams for the API call.
func (s *Session) buildParams() anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(s.agent.config.Model),
		MaxTokens: s.agent.config.MaxTokens,
		Messages:  s.messages,
	}

	if s.agent.config.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: s.agent.config.SystemPrompt},
		}
	}

	if len(s.tools) > 0 {
		params.Tools = s.tools
	}

	return params
}

// processToolCalls handles tool calls in a non-streaming context.
func (s *Session) processToolCalls(ctx context.Context, msg *anthropic.Message) error {
	// Add assistant message with all content blocks
	s.messages = append(s.messages, assistantMessageFromResponse(msg))

	// Process each tool call
	var toolResults []anthropic.ContentBlockParamUnion
	for _, block := range msg.Content {
		if block.Type != "tool_use" {
			continue
		}

		tu := block.AsToolUse()
		result, err := s.agent.servers.CallTool(ctx, tu.Name, tu.Input)
		if err != nil {
			// Tool errors are reported as tool results, not protocol errors
			toolResults = append(toolResults, anthropic.ContentBlockParamUnion{
				OfToolResult: &anthropic.ToolResultBlockParam{
					ToolUseID: tu.ID,
					IsError:   anthropic.Bool(true),
					Content: []anthropic.ToolResultBlockParamContentUnion{
						{OfText: &anthropic.TextBlockParam{Text: err.Error()}},
					},
				},
			})
			continue
		}

		blocks := mcpx.CallToolResultToBlocks(result)
		toolResults = append(toolResults, anthropic.ContentBlockParamUnion{
			OfToolResult: &anthropic.ToolResultBlockParam{
				ToolUseID: tu.ID,
				IsError:   anthropic.Bool(result.IsError),
				Content:   blocks,
			},
		})
	}

	// Add tool results as a user message
	s.messages = append(s.messages, anthropic.MessageParam{
		Role:    anthropic.MessageParamRoleUser,
		Content: toolResults,
	})

	return nil
}

// processToolCallsStreaming handles tool calls and emits events.
func (s *Session) processToolCallsStreaming(ctx context.Context, msg *anthropic.Message, events chan<- Event) error {
	// Add assistant message
	s.messages = append(s.messages, assistantMessageFromResponse(msg))

	var toolResults []anthropic.ContentBlockParamUnion
	for _, block := range msg.Content {
		if block.Type != "tool_use" {
			continue
		}

		tu := block.AsToolUse()

		events <- Event{
			Type:      EventToolCallStart,
			ToolName:  tu.Name,
			ToolID:    tu.ID,
			ToolInput: tu.Input,
		}

		result, err := s.agent.servers.CallTool(ctx, tu.Name, tu.Input)
		if err != nil {
			events <- Event{
				Type:        EventToolResult,
				ToolID:      tu.ID,
				ToolResult:  err.Error(),
				ToolIsError: true,
			}
			toolResults = append(toolResults, anthropic.ContentBlockParamUnion{
				OfToolResult: &anthropic.ToolResultBlockParam{
					ToolUseID: tu.ID,
					IsError:   anthropic.Bool(true),
					Content: []anthropic.ToolResultBlockParamContentUnion{
						{OfText: &anthropic.TextBlockParam{Text: err.Error()}},
					},
				},
			})
			continue
		}

		// Build result text for event
		resultText := ""
		for _, c := range result.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				resultText += tc.Text
			}
		}

		events <- Event{
			Type:        EventToolResult,
			ToolID:      tu.ID,
			ToolResult:  resultText,
			ToolIsError: result.IsError,
		}

		blocks := mcpx.CallToolResultToBlocks(result)
		toolResults = append(toolResults, anthropic.ContentBlockParamUnion{
			OfToolResult: &anthropic.ToolResultBlockParam{
				ToolUseID: tu.ID,
				IsError:   anthropic.Bool(result.IsError),
				Content:   blocks,
			},
		})
	}

	s.messages = append(s.messages, anthropic.MessageParam{
		Role:    anthropic.MessageParamRoleUser,
		Content: toolResults,
	})

	return nil
}

// assistantMessageFromResponse converts an API response Message to a MessageParam.
func assistantMessageFromResponse(msg *anthropic.Message) anthropic.MessageParam {
	var blocks []anthropic.ContentBlockParamUnion
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			tb := block.AsText()
			blocks = append(blocks, anthropic.ContentBlockParamUnion{
				OfText: &anthropic.TextBlockParam{Text: tb.Text},
			})
		case "tool_use":
			tu := block.AsToolUse()
			blocks = append(blocks, anthropic.ContentBlockParamUnion{
				OfToolUse: &anthropic.ToolUseBlockParam{
					ID:    tu.ID,
					Name:  tu.Name,
					Input: json.RawMessage(tu.Input),
				},
			})
		}
	}
	return anthropic.MessageParam{
		Role:    anthropic.MessageParamRoleAssistant,
		Content: blocks,
	}
}

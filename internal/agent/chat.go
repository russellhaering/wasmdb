package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/russellhaering/wasmdb/internal/autobot/agent"
	"github.com/russellhaering/wasmdb/internal/autobot/mcpx"
	"github.com/russellhaering/wasmdb/internal/database"
)

const systemPrompt = `You are a helpful assistant for WasmDB, a document-oriented database.
You have access to tools that let you manage tables and documents. You can:
- List, create, and inspect tables
- Create, read, update, and delete documents
- Search documents using full-text search or attribute filters

When users ask questions, use the available tools to help them. Be concise and helpful.
When showing document data, format it clearly. If a search returns no results, say so.
Always confirm destructive operations (deletes) before proceeding unless the user is explicit.

When the user's intent is clear enough to make a reasonable decision, proceed with your best
judgment rather than asking clarifying questions. Only ask for clarification when the ambiguity
would lead to significantly different outcomes.

## Rich Data Display (A2UI)

When displaying structured data (query results, table schemas, document details, lists), render it
as an A2UI surface inside a fenced code block. Use plain text for simple confirmations, errors, or
conversational responses.

Format:

` + "```" + `a2ui
{
  "components": [
    {"id": "root", "type": "Column", "children": ["child1"]},
    {"id": "child1", "type": "Text", "properties": {"text": "Hello"}}
  ]
}
` + "```" + `

### Supported Components

| Component | Properties | Use |
|-----------|-----------|-----|
| Column    | —         | Vertical layout container |
| Row       | —         | Horizontal layout container |
| DataTable | columns: [{key, label}], rows: [object], caption? | Tabular data |
| Card      | title?    | Bordered panel for a single record |
| Text      | text, label?, style? ("bold","dim","code") | Text with optional label |
| Divider   | —         | Horizontal separator |

All components have: id (string), type (string), optional children (array of IDs).

### Examples

Listing documents as a table:

` + "```" + `a2ui
{
  "components": [
    {"id": "root", "type": "Column", "children": ["t1"]},
    {"id": "t1", "type": "DataTable", "properties": {
      "columns": [{"key": "id", "label": "ID"}, {"key": "name", "label": "Name"}, {"key": "status", "label": "Status"}],
      "rows": [
        {"id": "doc-001", "name": "Getting Started", "status": "published"},
        {"id": "doc-002", "name": "API Reference", "status": "draft"}
      ],
      "caption": "Documents in 'docs'"
    }}
  ]
}
` + "```" + `

Showing a single document as a card:

` + "```" + `a2ui
{
  "components": [
    {"id": "root", "type": "Card", "properties": {"title": "Document: doc-001"}, "children": ["f1", "f2", "f3"]},
    {"id": "f1", "type": "Text", "properties": {"label": "ID", "text": "doc-001"}},
    {"id": "f2", "type": "Text", "properties": {"label": "Name", "text": "Getting Started"}},
    {"id": "f3", "type": "Text", "properties": {"label": "Content", "text": "Welcome to WasmDB...", "style": "dim"}}
  ]
}
` + "```" + `

Search results with summary:

` + "```" + `a2ui
{
  "components": [
    {"id": "root", "type": "Column", "children": ["summary", "d1", "t1"]},
    {"id": "summary", "type": "Text", "properties": {"text": "Found 3 results matching \"api\"", "style": "bold"}},
    {"id": "d1", "type": "Divider"},
    {"id": "t1", "type": "DataTable", "properties": {
      "columns": [{"key": "id", "label": "ID"}, {"key": "name", "label": "Name"}],
      "rows": [{"id": "doc-003", "name": "API Guide"}, {"id": "doc-004", "name": "API Errors"}]
    }}
  ]
}
` + "```" + `

### When NOT to use A2UI
- Simple confirmations ("Document created.", "Table deleted.")
- Error messages
- Conversational responses or explanations
- When there is no structured data to display`

// ChatConfig holds configuration for the chat agent.
type ChatConfig struct {
	AnthropicAPIKey string
	Model           string
	Registry        *database.Registry
}

// ChatManager manages chat sessions for the web interface.
type ChatManager struct {
	agent   *agent.Agent
	servers *mcpx.ServerGroup

	mu       sync.Mutex
	sessions map[string]*chatSession
}

type chatSession struct {
	mu      sync.Mutex
	history []anthropic.MessageParam
}

// NewChatManager creates a new chat manager with the given config.
func NewChatManager(ctx context.Context, cfg ChatConfig) (*ChatManager, error) {
	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-5-20250929"
	}

	servers := mcpx.NewServerGroup()
	servers.AddServer("table", NewTableServer(cfg.Registry))

	if err := servers.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connecting MCP servers: %w", err)
	}

	a := agent.NewAgent(agent.Config{
		Model:        model,
		APIKey:       cfg.AnthropicAPIKey,
		SystemPrompt: systemPrompt,
		MaxTokens:    16384,
		MaxTurns:     20,
	}, servers)

	return &ChatManager{
		agent:    a,
		servers:  servers,
		sessions: make(map[string]*chatSession),
	}, nil
}

// Close shuts down the chat manager.
func (cm *ChatManager) Close() {
	cm.servers.Close()
}

// getOrCreateSession returns an existing session or creates a new one.
func (cm *ChatManager) getOrCreateSession(sessionID string) *chatSession {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	s, ok := cm.sessions[sessionID]
	if !ok {
		s = &chatSession{}
		cm.sessions[sessionID] = s
	}
	return s
}

// StreamMessage sends a message in a chat session and streams events back.
func (cm *ChatManager) StreamMessage(ctx context.Context, sessionID, message string) <-chan agent.Event {
	cs := cm.getOrCreateSession(sessionID)

	cs.mu.Lock()

	events := make(chan agent.Event, 64)

	go func() {
		defer close(events)
		defer cs.mu.Unlock()

		session, err := cm.agent.NewSessionWithHistory(ctx, cs.history, message)
		if err != nil {
			events <- agent.Event{Type: agent.EventError, Error: err}
			return
		}

		for evt := range session.Stream(ctx) {
			events <- evt
			if evt.Type == agent.EventError {
				slog.Error("agent stream error", "session", sessionID, "err", evt.Error)
				return
			}
		}

		// Save the updated history (includes all tool calls and responses).
		cs.history = session.Messages()
	}()

	return events
}

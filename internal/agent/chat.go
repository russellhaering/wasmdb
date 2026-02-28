package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/oklog/ulid/v2"
	"github.com/russellhaering/wasmdb/internal/autobot/agent"
	"github.com/russellhaering/wasmdb/internal/autobot/mcpx"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/functions"
	"github.com/russellhaering/wasmdb/internal/index"
	"github.com/russellhaering/wasmdb/internal/memory"
	"github.com/russellhaering/wasmdb/internal/skills"
)

const systemPrompt = `You are a helpful assistant for WasmDB, a document-oriented database.
You have access to tools that let you manage tables and documents. You can:
- List, create, and inspect tables
- Create, read, update, and delete documents
- Search documents using full-text search or attribute filters

When users ask questions, use the available tools to help them. Be concise and helpful.
When showing document data, format it clearly. If a search returns no results, say so.
Always confirm destructive operations (deletes) before proceeding unless the user is explicit.
You can optionally use the delegate_subagent tool for isolated side tasks (research, draft plans, summarization).
Sub-agents are single-layer only (they cannot spawn further sub-agents).

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
- When there is no structured data to display

## JavaScript Execution

You can execute JavaScript code using the execute_code tool. The code runs in a
sandboxed environment with access to a ` + "`" + `db` + "`" + ` global object for database operations.

Available db methods:
- db.tables() — list all tables
- db.table(name).list(limit?) — list documents
- db.table(name).get(id) — get a document by ID
- db.table(name).put({id?, content?, attributes?}) — create or update a document
- db.table(name).delete(id) — delete a document
- db.table(name).search.text(query, limit?) — full-text search
- db.table(name).search.attr(filters, limit?) — attribute search (filters: [{field, op, value}])
- db.createTable(name) — create a new table
- db.deleteTable(name) — delete a table

Use execute_code for:
- Bulk operations (update many documents at once)
- Data transformations and enrichment
- Analytics and aggregations
- Finding duplicates or anomalies
- Any logic that's easier to express in code than as individual API calls

Define a handler(params) function for parameterized code, or write bare expressions for one-off work.
console.log() output is captured and returned in the result.

Use manage_function to save frequently-needed code as named stored functions that can be invoked later.

## Skills (Progressive Disclosure)

Use skills with progressive disclosure:
- First, inspect the compact skill catalog (names + short descriptions) via list_skills_catalog.
- Do NOT load full skill details unless needed.
- When you decide to use a skill, fetch details with get_skill_detail and then execute via manage_skill action=exec.

Skill routing rules:
- Prefer auto-invoking a relevant skill when confidence is high and risk is low.
- If a skill is manual-only, never auto-invoke it; ask the user to confirm and then invoke.
- If no skill clearly matches, proceed with normal tools.

Use manage_skill to create/update/get/list/delete/exec skills.

## Memories (Progressive Disclosure)

Use memory with progressive disclosure:
- First call list_memory_catalog with user_id to discover compact entries.
- Fetch full memory only when needed via get_memory_detail.
- Persist durable context via manage_memory action=create/update/pin.

Guidelines:
- Store stable user preferences and long-lived facts as memory.
- Keep memory summaries concise and specific.
- Retrieve memories selectively based on relevance, not all at once.`

const maxCachedSessions = 100

// ChatSessionInfo holds metadata about a chat session for listing.
type ChatSessionInfo struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ChatConfig holds configuration for the chat agent.
type ChatConfig struct {
	AnthropicAPIKey string
	Model           string
	SubAgentModel   string
	Registry        *database.Registry
	FnEngine        *functions.Engine
	FnStore         *functions.Store
}

// ChatManager manages chat sessions for the web interface.
type ChatManager struct {
	agent    *agent.Agent
	servers  *mcpx.ServerGroup
	registry *database.Registry

	mu       sync.Mutex
	sessions map[string]*chatSession
	// accessOrder tracks LRU order for cache eviction (most recent at end).
	accessOrder []string
}

type chatSession struct {
	mu      sync.Mutex
	userID  string
	history []anthropic.MessageParam
}

// NewChatManager creates a new chat manager with the given config.
func NewChatManager(ctx context.Context, cfg ChatConfig) (*ChatManager, error) {
	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-5-20250929"
	}

	subAgentModel := cfg.SubAgentModel
	if subAgentModel == "" {
		subAgentModel = model
	}

	servers := mcpx.NewServerGroup()
	skillStore := skills.NewStore(cfg.Registry, cfg.FnStore, cfg.FnEngine)
	memoryStore := memory.NewStore(cfg.Registry)
	servers.AddServer("table", NewTableServer(cfg.Registry, cfg.FnEngine, cfg.FnStore, skillStore, memoryStore, subAgentModel, cfg.AnthropicAPIKey))

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
		registry: cfg.Registry,
		sessions: make(map[string]*chatSession),
	}, nil
}

// Close shuts down the chat manager.
func (cm *ChatManager) Close() {
	cm.servers.Close()
}

// GenerateSessionID creates a new ULID-based session ID.
func GenerateSessionID() string {
	return ulid.Make().String()
}

// touchSession updates LRU tracking for a session and evicts if over capacity.
// Must be called with cm.mu held.
func (cm *ChatManager) touchSession(sessionID string) {
	// Remove from current position in access order.
	for i, id := range cm.accessOrder {
		if id == sessionID {
			cm.accessOrder = append(cm.accessOrder[:i], cm.accessOrder[i+1:]...)
			break
		}
	}
	// Add to end (most recently used).
	cm.accessOrder = append(cm.accessOrder, sessionID)

	// Evict oldest if over capacity.
	for len(cm.accessOrder) > maxCachedSessions {
		evictID := cm.accessOrder[0]
		cm.accessOrder = cm.accessOrder[1:]
		delete(cm.sessions, evictID)
	}
}

// getOrCreateSession returns an existing session or creates a new one.
// If the session exists in DB but not in memory, it is loaded from DB.
func (cm *ChatManager) getOrCreateSession(ctx context.Context, sessionID, userID string) (*chatSession, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if s, ok := cm.sessions[sessionID]; ok {
		cm.touchSession(sessionID)
		return s, nil
	}

	// Try loading from DB.
	s, err := cm.loadSession(ctx, sessionID)
	if err != nil {
		slog.Warn("failed to load chat session from DB", "session", sessionID, "err", err)
		// Fall through to create a new in-memory session.
	}

	if s == nil {
		s = &chatSession{userID: userID}
	}
	s.userID = userID

	cm.sessions[sessionID] = s
	cm.touchSession(sessionID)
	return s, nil
}

// loadSession loads a chat session's history from the _chat_sessions table.
func (cm *ChatManager) loadSession(ctx context.Context, sessionID string) (*chatSession, error) {
	table, err := cm.registry.GetTable(ctx, "_chat_sessions")
	if err != nil {
		return nil, fmt.Errorf("get chat_sessions table: %w", err)
	}

	doc, err := table.GetDocument(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get document: %w", err)
	}
	if doc == nil {
		return nil, nil
	}

	var history []anthropic.MessageParam
	if doc.Content != "" {
		if err := json.Unmarshal([]byte(doc.Content), &history); err != nil {
			return nil, fmt.Errorf("unmarshal history: %w", err)
		}
	}

	userID, _ := doc.Attributes["user_id"].(string)

	return &chatSession{
		userID:  userID,
		history: history,
	}, nil
}

// saveSession persists a chat session to the _chat_sessions table.
func (cm *ChatManager) saveSession(ctx context.Context, sessionID string, cs *chatSession) error {
	table, err := cm.registry.GetTable(ctx, "_chat_sessions")
	if err != nil {
		return fmt.Errorf("get chat_sessions table: %w", err)
	}

	historyJSON, err := json.Marshal(cs.history)
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}

	title := extractTitle(cs.history)
	now := time.Now().UTC()

	doc := &document.Document{
		ID:      sessionID,
		Content: string(historyJSON),
		Attributes: map[string]any{
			"user_id":    cs.userID,
			"title":      title,
			"updated_at": now.Format(time.RFC3339),
		},
	}

	if err := table.PutDocument(ctx, doc); err != nil {
		return fmt.Errorf("put document: %w", err)
	}

	return nil
}

// extractTitle derives a session title from the first user message (up to 100 chars).
func extractTitle(history []anthropic.MessageParam) string {
	for _, msg := range history {
		if msg.Role == anthropic.MessageParamRoleUser {
			for _, block := range msg.Content {
				if block.OfText != nil {
					text := block.OfText.Text
					if len(text) > 100 {
						return text[:100] + "..."
					}
					return text
				}
			}
		}
	}
	return "New Chat"
}

// StreamMessage sends a message in a chat session and streams events back.
// If sessionID is empty, a new session is created and the ID is returned via the channel
// as an EventSessionID event (piggy-backed on the first event).
func (cm *ChatManager) StreamMessage(ctx context.Context, sessionID, userID, message string) (string, <-chan agent.Event) {
	if sessionID == "" {
		sessionID = GenerateSessionID()
	}

	cs, err := cm.getOrCreateSession(ctx, sessionID, userID)
	if err != nil {
		events := make(chan agent.Event, 1)
		events <- agent.Event{Type: agent.EventError, Error: err}
		close(events)
		return sessionID, events
	}

	cs.mu.Lock()

	events := make(chan agent.Event, 64)

	go func() {
		defer close(events)
		defer cs.mu.Unlock()

		prompt := fmt.Sprintf("Authenticated user_id: %s\n\nUser request:\n%s", userID, message)
		prefix := ""
		if catalog := cm.buildSkillsCatalogPrompt(ctx); catalog != "" {
			prefix += catalog + "\n"
		}
		if mem := cm.buildMemoryCatalogPrompt(ctx, userID); mem != "" {
			prefix += mem + "\n"
		}
		if prefix != "" {
			prompt = fmt.Sprintf("Authenticated user_id: %s\n\n%s\nUser request:\n%s", userID, prefix, message)
		}

		session, err := cm.agent.NewSessionWithHistory(ctx, cs.history, prompt)
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

		// Save the updated history to in-memory cache synchronously.
		cs.history = session.Messages()

		// Persist to DB asynchronously.
		go func() {
			saveCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := cm.saveSession(saveCtx, sessionID, cs); err != nil {
				slog.Error("failed to persist chat session", "session", sessionID, "err", err)
			}
		}()
	}()

	return sessionID, events
}

// ListSessions returns chat session metadata for a given user, ordered by updated_at desc.
func (cm *ChatManager) ListSessions(ctx context.Context, userID string) ([]ChatSessionInfo, error) {
	table, err := cm.registry.GetTable(ctx, "_chat_sessions")
	if err != nil {
		return nil, fmt.Errorf("get chat_sessions table: %w", err)
	}

	docs, err := table.SearchAttributes(ctx, []index.Filter{
		{Field: "user_id", Op: index.OpEq, Value: userID},
	}, 1000, 0)
	if err != nil {
		return nil, fmt.Errorf("search sessions: %w", err)
	}

	sessions := make([]ChatSessionInfo, 0, len(docs))
	for _, doc := range docs {
		title, _ := doc.Attributes["title"].(string)
		if title == "" {
			title = "New Chat"
		}

		updatedAt := doc.UpdatedAt
		if ts, ok := doc.Attributes["updated_at"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
				updatedAt = parsed
			}
		}

		sessions = append(sessions, ChatSessionInfo{
			ID:        doc.ID,
			Title:     title,
			UpdatedAt: updatedAt,
		})
	}

	// Sort by updated_at descending.
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

// DeleteSession removes a chat session from both the cache and DB.
func (cm *ChatManager) DeleteSession(ctx context.Context, sessionID, userID string) error {
	table, err := cm.registry.GetTable(ctx, "_chat_sessions")
	if err != nil {
		return fmt.Errorf("get chat_sessions table: %w", err)
	}

	// Verify ownership before deletion.
	doc, err := table.GetDocument(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	if doc == nil {
		return fmt.Errorf("session not found")
	}
	if owner, _ := doc.Attributes["user_id"].(string); owner != userID {
		return fmt.Errorf("session not found")
	}

	if err := table.DeleteDocument(ctx, sessionID); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	// Remove from in-memory cache.
	cm.mu.Lock()
	delete(cm.sessions, sessionID)
	for i, id := range cm.accessOrder {
		if id == sessionID {
			cm.accessOrder = append(cm.accessOrder[:i], cm.accessOrder[i+1:]...)
			break
		}
	}
	cm.mu.Unlock()

	return nil
}

// buildSkillsCatalogPrompt injects a compact skill catalog (name + description + flags)
// so the model can discover capabilities without loading full skill definitions.
func (cm *ChatManager) buildSkillsCatalogPrompt(ctx context.Context) string {
	tbl, err := cm.registry.GetTable(ctx, "_skills")
	if err != nil {
		return ""
	}

	docs, _, err := tbl.ListDocuments(ctx, 500, "")
	if err != nil || len(docs) == 0 {
		return ""
	}

	const maxChars = 4000
	var b strings.Builder
	b.WriteString("Available skills catalog (compact; fetch detail only when needed):\n")

	for _, d := range docs {
		name, _ := d.Attributes["name"].(string)
		if name == "" {
			continue
		}
		desc, _ := d.Attributes["description"].(string)
		fn, _ := d.Attributes["function_name"].(string)
		manualOnly, _ := d.Attributes["disable_model_invocation"].(bool)
		if desc == "" {
			desc = "(no description)"
		}
		line := fmt.Sprintf("- %s: %s [function=%s, manual_only=%t]\n", name, desc, fn, manualOnly)
		if b.Len()+len(line) > maxChars {
			break
		}
		b.WriteString(line)
	}

	if b.Len() == 0 {
		return ""
	}
	return b.String()
}

// buildMemoryCatalogPrompt injects compact memory metadata (no full body) for
// Claude-style progressive disclosure.
func (cm *ChatManager) buildMemoryCatalogPrompt(ctx context.Context, userID string) string {
	if userID == "" {
		return ""
	}
	store := memory.NewStore(cm.registry)
	entries, err := store.ListCatalog(ctx, userID, 20)
	if err != nil || len(entries) == 0 {
		return ""
	}

	const maxChars = 3000
	var b strings.Builder
	b.WriteString("Memory catalog (compact; fetch detail only when needed):\n")
	for _, e := range entries {
		line := fmt.Sprintf("- %s [%s, pinned=%t, tags=%v]: %s\n", e.ID, e.Scope, e.Pinned, e.Tags, e.Summary)
		if b.Len()+len(line) > maxChars {
			break
		}
		b.WriteString(line)
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String()
}

package cli

import (
	"context"

	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/index"
)

// TableInfo holds basic table metadata.
type TableInfo struct {
	Name   string           `json:"name"`
	Schema *document.Schema `json:"schema,omitempty"`
}

// BulkResult holds the result of a bulk create operation.
type BulkResult struct {
	Count int `json:"count"`
}

// TextSearchResult holds full-text search results.
type TextSearchResult struct {
	Results []*document.Document `json:"results"`
	Total   int                  `json:"total"`
}

// UserInfo holds user metadata returned by the API.
type UserInfo struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

// FunctionInfo holds basic function metadata returned after create/update.
type FunctionInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// FunctionSummary holds function metadata for list display.
type FunctionSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	UpdatedAt   string `json:"updated_at"`
}

// FunctionDetail holds full function details.
type FunctionDetail struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Code        string `json:"code"`
	CreatedBy   string `json:"created_by,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// ExecResult holds the result of code execution.
type ExecResult struct {
	Result     any      `json:"result"`
	Logs       []string `json:"logs"`
	DurationMS int64    `json:"duration_ms"`
	Error      string   `json:"error,omitempty"`
}

// HealthStatus holds the result of a health or readiness check.
type HealthStatus struct {
	Status string `json:"status"`
}

// SkillInfo holds basic skill metadata returned after create/update.
type SkillInfo struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	FunctionName           string `json:"function_name"`
	DisableModelInvocation bool   `json:"disable_model_invocation,omitempty"`
	CreatedAt              string `json:"created_at,omitempty"`
	UpdatedAt              string `json:"updated_at,omitempty"`
}

// SkillSummary holds skill metadata for list display.
type SkillSummary struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Description            string `json:"description,omitempty"`
	FunctionName           string `json:"function_name"`
	DisableModelInvocation bool   `json:"disable_model_invocation,omitempty"`
	UpdatedAt              string `json:"updated_at"`
}

// SkillDetail holds full skill details.
type SkillDetail struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Description            string `json:"description,omitempty"`
	FunctionName           string `json:"function_name"`
	DisableModelInvocation bool   `json:"disable_model_invocation,omitempty"`
	CreatedBy              string `json:"created_by,omitempty"`
	CreatedAt              string `json:"created_at"`
	UpdatedAt              string `json:"updated_at"`
}

// Backend defines the operations available to CLI commands.
type Backend interface {
	CreateTable(ctx context.Context, name string, schema *document.Schema) (*TableInfo, error)
	ListTables(ctx context.Context) ([]TableInfo, error)
	GetTable(ctx context.Context, name string) (*TableInfo, error)
	DeleteTable(ctx context.Context, name string) error

	GetSchema(ctx context.Context, db string) (*document.Schema, error)
	UpdateSchema(ctx context.Context, db string, schema *document.Schema) (*document.Schema, error)

	CreateDocument(ctx context.Context, db string, doc *document.Document) (*document.Document, error)
	GetDocument(ctx context.Context, db string, id string) (*document.Document, error)
	UpdateDocument(ctx context.Context, db string, id string, doc *document.Document) (*document.Document, error)
	DeleteDocument(ctx context.Context, db string, id string) error
	BulkCreateDocuments(ctx context.Context, db string, docs []*document.Document) (*BulkResult, error)

	SearchText(ctx context.Context, db string, query string, limit, offset int) (*TextSearchResult, error)
	SearchVector(ctx context.Context, db string, query string, k int) ([]*document.Document, error)
	SearchAttributes(ctx context.Context, db string, filters []index.Filter, limit, offset int) ([]*document.Document, error)

	CreateUser(ctx context.Context, email, password string) (*UserInfo, error)
	ListUsers(ctx context.Context) ([]UserInfo, error)

	CreateFunction(ctx context.Context, name, description, code string) (*FunctionInfo, error)
	ListFunctions(ctx context.Context) ([]FunctionSummary, error)
	GetFunction(ctx context.Context, name string) (*FunctionDetail, error)
	UpdateFunction(ctx context.Context, name, code, description string) (*FunctionInfo, error)
	DeleteFunction(ctx context.Context, name string) error
	ExecFunction(ctx context.Context, name string, params map[string]any) (*ExecResult, error)
	ExecCode(ctx context.Context, code string, params map[string]any) (*ExecResult, error)

	CreateSkill(ctx context.Context, name, description, functionName string, disableModelInvocation bool) (*SkillInfo, error)
	ListSkills(ctx context.Context) ([]SkillSummary, error)
	GetSkill(ctx context.Context, name string) (*SkillDetail, error)
	UpdateSkill(ctx context.Context, name, description, functionName string, disableModelInvocation bool) (*SkillInfo, error)
	DeleteSkill(ctx context.Context, name string) error
	ExecSkill(ctx context.Context, name string, params map[string]any) (*ExecResult, error)

	CreateMemory(ctx context.Context, scope, title, summary string, tags []string, pinned bool) (*MemoryInfo, error)
	ListMemories(ctx context.Context) ([]MemoryCatalogEntry, error)
	GetMemory(ctx context.Context, id string) (*MemoryInfo, error)
	UpdateMemory(ctx context.Context, id, scope, title, summary string, tags []string, pinned bool) (*MemoryInfo, error)
	DeleteMemory(ctx context.Context, id string) error

	CreateMCPServer(ctx context.Context, name, description, transport, url, command string, args, env []string, headers map[string]string, oauth *OAuthConfig, enabled bool) (*MCPServerInfo, error)
	ListMCPServers(ctx context.Context) ([]MCPServerSummary, error)
	GetMCPServer(ctx context.Context, name string) (*MCPServerDetail, error)
	UpdateMCPServer(ctx context.Context, name, description, transport, url, command string, args, env []string, headers map[string]string, oauth *OAuthConfig, enabled bool) (*MCPServerInfo, error)
	DeleteMCPServer(ctx context.Context, name string) error

	CreateAgent(ctx context.Context, name, description, prompt, schedule, triggerType string, enabled bool, maxTurns int) (*AgentInfo, error)
	ListAgents(ctx context.Context) ([]AgentSummary, error)
	GetAgent(ctx context.Context, name string) (*AgentDetail, error)
	UpdateAgent(ctx context.Context, name, description, prompt, schedule, triggerType string, enabled bool, maxTurns int) (*AgentInfo, error)
	DeleteAgent(ctx context.Context, name string) error
	TriggerAgent(ctx context.Context, name string) (*AgentRunInfo, error)
	ListAgentRuns(ctx context.Context, name string, limit int) ([]AgentRunInfo, error)

	CreateUIConfig(ctx context.Context, name, title, description string, sourceTables []string, surfaceJSON, queryJS string, autoRefreshSec, sortOrder int, enabled bool) (*UIConfigInfo, error)
	ListUIConfigs(ctx context.Context) ([]UIConfigSummary, error)
	GetUIConfig(ctx context.Context, name string) (*UIConfigDetail, error)
	UpdateUIConfig(ctx context.Context, name, title, description string, sourceTables []string, surfaceJSON, queryJS string, autoRefreshSec, sortOrder int, enabled bool) (*UIConfigInfo, error)
	DeleteUIConfig(ctx context.Context, name string) error

	Health(ctx context.Context) (*HealthStatus, error)
	Ready(ctx context.Context) (*HealthStatus, error)

	ChatStream(ctx context.Context, sessionID, message string) (<-chan ChatEvent, error)
}

// ChatEvent represents a single SSE event from the chat stream.
// Fields are populated by the SSE parser, not JSON-unmarshaled directly.
// MemoryInfo holds memory details.
type MemoryInfo struct {
	ID         string   `json:"id"`
	UserID     string   `json:"user_id"`
	Scope      string   `json:"scope"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Tags       []string `json:"tags,omitempty"`
	Pinned     bool     `json:"pinned,omitempty"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
	LastUsedAt string   `json:"last_used_at,omitempty"`
}

// MemoryCatalogEntry is compact memory metadata.
type MemoryCatalogEntry struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	Scope     string   `json:"scope"`
	Tags      []string `json:"tags,omitempty"`
	Pinned    bool     `json:"pinned,omitempty"`
	UpdatedAt string   `json:"updated_at"`
}

// MCPServerInfo holds basic MCP server metadata returned after create/update.
type MCPServerInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// MCPServerSummary holds MCP server metadata for list display.
type MCPServerSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Transport   string `json:"transport"`
	URL         string `json:"url,omitempty"`
	Command     string `json:"command,omitempty"`
	Enabled     bool   `json:"enabled"`
	UpdatedAt   string `json:"updated_at"`
}

// MCPServerDetail holds full MCP server details.
type MCPServerDetail struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Transport   string            `json:"transport"`
	URL         string            `json:"url,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         []string          `json:"env,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	OAuth       *OAuthConfig      `json:"oauth,omitempty"`
	Enabled     bool              `json:"enabled"`
	CreatedBy   string            `json:"created_by,omitempty"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

// OAuthConfig holds OAuth 2.0 client_credentials configuration.
type OAuthConfig struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	TokenURL     string   `json:"token_url"`
	Scopes       []string `json:"scopes,omitempty"`
}

// AgentInfo holds basic agent metadata returned after create/update.
type AgentInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TriggerType string `json:"trigger_type"`
	Schedule    string `json:"schedule"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// AgentSummary holds agent metadata for list display.
type AgentSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Schedule    string `json:"schedule"`
	TriggerType string `json:"trigger_type"`
	Enabled     bool   `json:"enabled"`
	UpdatedAt   string `json:"updated_at"`
}

// AgentDetail holds full agent details.
type AgentDetail struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt"`
	Schedule    string `json:"schedule"`
	TriggerType string `json:"trigger_type"`
	Enabled     bool   `json:"enabled"`
	MaxTurns    int    `json:"max_turns,omitempty"`
	CreatedBy   string `json:"created_by,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// AgentRunInfo holds agent run details.
type AgentRunInfo struct {
	ID           string `json:"id"`
	AgentID      string `json:"agent_id"`
	AgentName    string `json:"agent_name"`
	Status       string `json:"status"`
	Output       string `json:"output,omitempty"`
	Error        string `json:"error,omitempty"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	DurationMS   int64  `json:"duration_ms"`
	StartedAt    string `json:"started_at"`
	CompletedAt  string `json:"completed_at,omitempty"`
}

// UIConfigInfo holds basic UI config metadata returned after create/update.
type UIConfigInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	Enabled   bool   `json:"enabled"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// UIConfigSummary holds UI config metadata for list display.
type UIConfigSummary struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	SourceTables       []string `json:"source_tables,omitempty"`
	AutoRefreshSeconds int      `json:"auto_refresh_seconds,omitempty"`
	SortOrder          int      `json:"sort_order"`
	Enabled            bool     `json:"enabled"`
	UpdatedAt          string   `json:"updated_at"`
}

// UIConfigDetail holds full UI config details.
type UIConfigDetail struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	SourceTables       []string `json:"source_tables,omitempty"`
	SurfaceJSON        string   `json:"surface_json"`
	QueryJS            string   `json:"query_js,omitempty"`
	AutoRefreshSeconds int      `json:"auto_refresh_seconds,omitempty"`
	SortOrder          int      `json:"sort_order"`
	Enabled            bool     `json:"enabled"`
	CreatedBy          string   `json:"created_by"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
}

type ChatEvent struct {
	Type string // "text", "tool_start", "tool_result", "error", "done"

	// text event
	Text string

	// tool_start event
	Tool   string
	ToolID string

	// tool_result event
	Result    string
	ToolError bool

	// error event
	Error string
}

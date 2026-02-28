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

	Health(ctx context.Context) (*HealthStatus, error)
	Ready(ctx context.Context) (*HealthStatus, error)

	ChatStream(ctx context.Context, sessionID, message string) (<-chan ChatEvent, error)
}

// ChatEvent represents a single SSE event from the chat stream.
// Fields are populated by the SSE parser, not JSON-unmarshaled directly.
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

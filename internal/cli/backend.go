package cli

import (
	"context"

	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/index"
)

// DatabaseInfo holds basic database metadata.
type DatabaseInfo struct {
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

// HealthStatus holds the result of a health or readiness check.
type HealthStatus struct {
	Status string `json:"status"`
}

// Backend defines the operations available to CLI commands.
type Backend interface {
	CreateDatabase(ctx context.Context, name string, schema *document.Schema) (*DatabaseInfo, error)
	ListDatabases(ctx context.Context) ([]DatabaseInfo, error)
	GetDatabase(ctx context.Context, name string) (*DatabaseInfo, error)
	DeleteDatabase(ctx context.Context, name string) error

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

	Health(ctx context.Context) (*HealthStatus, error)
	Ready(ctx context.Context) (*HealthStatus, error)
}

package uiconfig

import (
	"context"
	"fmt"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/index"
)

const uiConfigsTable = "_ui_configs"

// UIConfig represents a saved UI page configuration.
type UIConfig struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Title              string    `json:"title"`
	Description        string    `json:"description,omitempty"`
	SourceTables       []string  `json:"source_tables,omitempty"`
	SurfaceJSON        string    `json:"surface_json"`
	QueryJS            string    `json:"query_js,omitempty"`
	AutoRefreshSeconds int       `json:"auto_refresh_seconds,omitempty"`
	SortOrder          int       `json:"sort_order"`
	Enabled            bool      `json:"enabled"`
	CreatedBy          string    `json:"created_by"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Store handles CRUD operations for UI configurations.
type Store struct {
	registry *database.Registry
}

// NewStore creates a new UI config store.
func NewStore(registry *database.Registry) *Store {
	return &Store{registry: registry}
}

// Create creates a new UI configuration.
func (s *Store) Create(ctx context.Context, name, title, description string, sourceTables []string, surfaceJSON, queryJS string, autoRefreshSec, sortOrder int, enabled bool, userID string) (*UIConfig, error) {
	if name == "" {
		return nil, fmt.Errorf("name must not be empty")
	}
	if surfaceJSON == "" {
		return nil, fmt.Errorf("surface_json must not be empty")
	}

	existing, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("ui config %q already exists", name)
	}

	tbl, err := s.registry.GetTable(ctx, uiConfigsTable)
	if err != nil {
		return nil, fmt.Errorf("get ui_configs table: %w", err)
	}

	now := time.Now().UTC()

	// Convert []string to []any for storage.
	var sourceTablesAny []any
	if len(sourceTables) > 0 {
		sourceTablesAny = make([]any, len(sourceTables))
		for i, t := range sourceTables {
			sourceTablesAny[i] = t
		}
	}

	doc := &document.Document{
		Attributes: map[string]any{
			"name":                 name,
			"title":                title,
			"description":          description,
			"source_tables":        sourceTablesAny,
			"surface_json":         surfaceJSON,
			"query_js":             queryJS,
			"auto_refresh_seconds": autoRefreshSec,
			"sort_order":           sortOrder,
			"enabled":              enabled,
			"created_by":           userID,
			"updated_at":           now.Format(time.RFC3339),
		},
	}

	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("create ui config: %w", err)
	}

	return &UIConfig{
		ID:                 doc.ID,
		Name:               name,
		Title:              title,
		Description:        description,
		SourceTables:       sourceTables,
		SurfaceJSON:        surfaceJSON,
		QueryJS:            queryJS,
		AutoRefreshSeconds: autoRefreshSec,
		SortOrder:          sortOrder,
		Enabled:            enabled,
		CreatedBy:          userID,
		CreatedAt:          doc.CreatedAt,
		UpdatedAt:          now,
	}, nil
}

// Get retrieves a UI configuration by name.
func (s *Store) Get(ctx context.Context, name string) (*UIConfig, error) {
	tbl, err := s.registry.GetTable(ctx, uiConfigsTable)
	if err != nil {
		return nil, fmt.Errorf("get ui_configs table: %w", err)
	}

	docs, err := tbl.SearchAttributes(ctx, []index.Filter{
		{Field: "name", Op: index.OpEq, Value: name},
	}, 1, 0)
	if err != nil {
		return nil, fmt.Errorf("search ui config: %w", err)
	}
	if len(docs) == 0 {
		return nil, nil
	}

	return docToUIConfig(docs[0]), nil
}

// GetByID retrieves a UI configuration by ID.
func (s *Store) GetByID(ctx context.Context, id string) (*UIConfig, error) {
	tbl, err := s.registry.GetTable(ctx, uiConfigsTable)
	if err != nil {
		return nil, fmt.Errorf("get ui_configs table: %w", err)
	}

	doc, err := tbl.GetDocument(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get ui config by id: %w", err)
	}
	if doc == nil {
		return nil, nil
	}

	return docToUIConfig(doc), nil
}

// List returns all UI configurations.
func (s *Store) List(ctx context.Context) ([]*UIConfig, error) {
	tbl, err := s.registry.GetTable(ctx, uiConfigsTable)
	if err != nil {
		return nil, fmt.Errorf("get ui_configs table: %w", err)
	}

	docs, _, err := tbl.ListDocuments(ctx, 1000, "")
	if err != nil {
		return nil, fmt.Errorf("list ui configs: %w", err)
	}

	configs := make([]*UIConfig, 0, len(docs))
	for _, doc := range docs {
		configs = append(configs, docToUIConfig(doc))
	}
	return configs, nil
}

// ListEnabled returns all enabled UI configurations.
func (s *Store) ListEnabled(ctx context.Context) ([]*UIConfig, error) {
	tbl, err := s.registry.GetTable(ctx, uiConfigsTable)
	if err != nil {
		return nil, fmt.Errorf("get ui_configs table: %w", err)
	}

	docs, err := tbl.SearchAttributes(ctx, []index.Filter{
		{Field: "enabled", Op: index.OpEq, Value: true},
	}, 1000, 0)
	if err != nil {
		return nil, fmt.Errorf("search enabled ui configs: %w", err)
	}

	configs := make([]*UIConfig, 0, len(docs))
	for _, doc := range docs {
		configs = append(configs, docToUIConfig(doc))
	}
	return configs, nil
}

// Update updates a UI configuration.
func (s *Store) Update(ctx context.Context, name, title, description string, sourceTables []string, surfaceJSON, queryJS string, autoRefreshSec, sortOrder int, enabled bool) (*UIConfig, error) {
	if surfaceJSON == "" {
		return nil, fmt.Errorf("surface_json must not be empty")
	}

	existing, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("ui config %q not found", name)
	}

	tbl, err := s.registry.GetTable(ctx, uiConfigsTable)
	if err != nil {
		return nil, fmt.Errorf("get ui_configs table: %w", err)
	}

	now := time.Now().UTC()

	// Convert []string to []any for storage.
	var sourceTablesAny []any
	if len(sourceTables) > 0 {
		sourceTablesAny = make([]any, len(sourceTables))
		for i, t := range sourceTables {
			sourceTablesAny[i] = t
		}
	}

	doc := &document.Document{
		ID: existing.ID,
		Attributes: map[string]any{
			"name":                 name,
			"title":                title,
			"description":          description,
			"source_tables":        sourceTablesAny,
			"surface_json":         surfaceJSON,
			"query_js":             queryJS,
			"auto_refresh_seconds": autoRefreshSec,
			"sort_order":           sortOrder,
			"enabled":              enabled,
			"created_by":           existing.CreatedBy,
			"updated_at":           now.Format(time.RFC3339),
		},
	}

	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("update ui config: %w", err)
	}

	existing.Title = title
	existing.Description = description
	existing.SourceTables = sourceTables
	existing.SurfaceJSON = surfaceJSON
	existing.QueryJS = queryJS
	existing.AutoRefreshSeconds = autoRefreshSec
	existing.SortOrder = sortOrder
	existing.Enabled = enabled
	existing.UpdatedAt = now
	return existing, nil
}

// Delete removes a UI configuration by name.
func (s *Store) Delete(ctx context.Context, name string) error {
	existing, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("ui config %q not found", name)
	}

	tbl, err := s.registry.GetTable(ctx, uiConfigsTable)
	if err != nil {
		return fmt.Errorf("get ui_configs table: %w", err)
	}

	return tbl.DeleteDocument(ctx, existing.ID)
}

func docToUIConfig(doc *document.Document) *UIConfig {
	c := &UIConfig{ID: doc.ID, CreatedAt: doc.CreatedAt}
	if v, ok := doc.Attributes["name"].(string); ok {
		c.Name = v
	}
	if v, ok := doc.Attributes["title"].(string); ok {
		c.Title = v
	}
	if v, ok := doc.Attributes["description"].(string); ok {
		c.Description = v
	}
	if v, ok := doc.Attributes["source_tables"].([]any); ok {
		c.SourceTables = make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				c.SourceTables = append(c.SourceTables, s)
			}
		}
	}
	if v, ok := doc.Attributes["surface_json"].(string); ok {
		c.SurfaceJSON = v
	}
	if v, ok := doc.Attributes["query_js"].(string); ok {
		c.QueryJS = v
	}
	c.AutoRefreshSeconds = anyToInt(doc.Attributes["auto_refresh_seconds"])
	c.SortOrder = anyToInt(doc.Attributes["sort_order"])
	if v, ok := doc.Attributes["enabled"].(bool); ok {
		c.Enabled = v
	}
	if v, ok := doc.Attributes["created_by"].(string); ok {
		c.CreatedBy = v
	}
	if v, ok := doc.Attributes["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			c.UpdatedAt = t
		}
	}
	return c
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

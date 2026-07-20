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

// currentSpecVersion is the surface format version written by Create.
const currentSpecVersion = 2

// UIConfig represents a saved UI page configuration.
type UIConfig struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Title              string    `json:"title"`
	Description        string    `json:"description,omitempty"`
	SourceTables       []string  `json:"source_tables,omitempty"`
	SurfaceJSON        string    `json:"surface_json"`
	ActionsJSON        string    `json:"actions_json,omitempty"`
	QueryJS            string    `json:"query_js,omitempty"`
	AutoRefreshSeconds int       `json:"auto_refresh_seconds,omitempty"`
	SortOrder          int       `json:"sort_order"`
	Enabled            bool      `json:"enabled"`
	SpecVersion        int       `json:"spec_version"`
	Generator          string    `json:"generator,omitempty"` // "scaffold" | "agent" | "user"
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

// Create creates a new UI configuration. It always writes spec_version=2 and
// requires a generator ("scaffold" | "agent" | "user"); an empty generator
// defaults to "user".
func (s *Store) Create(ctx context.Context, name, title, description string, sourceTables []string, surfaceJSON, actionsJSON, queryJS string, autoRefreshSec, sortOrder int, enabled bool, generator, userID string) (*UIConfig, error) {
	if name == "" {
		return nil, fmt.Errorf("name must not be empty")
	}
	if surfaceJSON == "" {
		return nil, fmt.Errorf("surface_json must not be empty")
	}
	if generator == "" {
		generator = "user"
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

	doc := &document.Document{
		Attributes: map[string]any{
			"name":                 name,
			"title":                title,
			"description":          description,
			"source_tables":        stringsToAny(sourceTables),
			"surface_json":         surfaceJSON,
			"actions_json":         actionsJSON,
			"query_js":             queryJS,
			"auto_refresh_seconds": autoRefreshSec,
			"sort_order":           sortOrder,
			"enabled":              enabled,
			"spec_version":         currentSpecVersion,
			"generator":            generator,
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
		ActionsJSON:        actionsJSON,
		QueryJS:            queryJS,
		AutoRefreshSeconds: autoRefreshSec,
		SortOrder:          sortOrder,
		Enabled:            enabled,
		SpecVersion:        currentSpecVersion,
		Generator:          generator,
		CreatedBy:          userID,
		CreatedAt:          doc.CreatedAt,
		UpdatedAt:          now,
	}, nil
}

// UpdateParams holds the patchable fields for Update. A nil pointer preserves
// the existing value; a non-nil pointer overwrites it. To clear a string field,
// pass a pointer to "".
type UpdateParams struct {
	Title              *string
	Description        *string
	SurfaceJSON        *string
	ActionsJSON        *string
	QueryJS            *string
	SourceTables       *[]string
	AutoRefreshSeconds *int
	SortOrder          *int
	Enabled            *bool
	Generator          *string
}

// Update applies a partial patch to an existing UI configuration. Only fields
// with a non-nil pointer in params are changed; everything else (including ID,
// CreatedBy, CreatedAt, and SpecVersion) is preserved. updated_at is bumped.
func (s *Store) Update(ctx context.Context, name string, params UpdateParams) (*UIConfig, error) {
	existing, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("ui config %q not found", name)
	}

	// Apply the patch onto a copy of the existing config.
	updated := *existing
	if params.Title != nil {
		updated.Title = *params.Title
	}
	if params.Description != nil {
		updated.Description = *params.Description
	}
	if params.SurfaceJSON != nil {
		updated.SurfaceJSON = *params.SurfaceJSON
	}
	if params.ActionsJSON != nil {
		updated.ActionsJSON = *params.ActionsJSON
	}
	if params.QueryJS != nil {
		updated.QueryJS = *params.QueryJS
	}
	if params.SourceTables != nil {
		updated.SourceTables = *params.SourceTables
	}
	if params.AutoRefreshSeconds != nil {
		updated.AutoRefreshSeconds = *params.AutoRefreshSeconds
	}
	if params.SortOrder != nil {
		updated.SortOrder = *params.SortOrder
	}
	if params.Enabled != nil {
		updated.Enabled = *params.Enabled
	}
	if params.Generator != nil {
		updated.Generator = *params.Generator
	}

	if updated.SurfaceJSON == "" {
		return nil, fmt.Errorf("surface_json must not be empty")
	}

	tbl, err := s.registry.GetTable(ctx, uiConfigsTable)
	if err != nil {
		return nil, fmt.Errorf("get ui_configs table: %w", err)
	}

	now := time.Now().UTC()
	updated.UpdatedAt = now

	doc := &document.Document{
		ID: existing.ID,
		Attributes: map[string]any{
			"name":                 updated.Name,
			"title":                updated.Title,
			"description":          updated.Description,
			"source_tables":        stringsToAny(updated.SourceTables),
			"surface_json":         updated.SurfaceJSON,
			"actions_json":         updated.ActionsJSON,
			"query_js":             updated.QueryJS,
			"auto_refresh_seconds": updated.AutoRefreshSeconds,
			"sort_order":           updated.SortOrder,
			"enabled":              updated.Enabled,
			"spec_version":         existing.SpecVersion,
			"generator":            updated.Generator,
			"created_by":           existing.CreatedBy,
			"updated_at":           now.Format(time.RFC3339),
		},
	}

	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("update ui config: %w", err)
	}

	return &updated, nil
}

// SetEnabled toggles a UI configuration's enabled flag.
func (s *Store) SetEnabled(ctx context.Context, name string, enabled bool) error {
	_, err := s.Update(ctx, name, UpdateParams{Enabled: &enabled})
	return err
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
	if len(docs) > 0 {
		return docToUIConfig(docs[0]), nil
	}

	// The attribute index is populated asynchronously, so a read immediately
	// after a write can miss. Fall back to a paged full-table scan comparing the
	// "name" attribute so get-after-create is consistent.
	const scanPage = 200
	afterKey := ""
	for {
		batch, hasMore, err := tbl.ListDocuments(ctx, scanPage, afterKey)
		if err != nil {
			return nil, fmt.Errorf("scan ui configs: %w", err)
		}
		for _, doc := range batch {
			if n, ok := doc.Attributes["name"].(string); ok && n == name {
				return docToUIConfig(doc), nil
			}
		}
		if !hasMore || len(batch) == 0 {
			break
		}
		afterKey = batch[len(batch)-1].ID
	}

	return nil, nil
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
	if v, ok := doc.Attributes["actions_json"].(string); ok {
		c.ActionsJSON = v
	}
	if v, ok := doc.Attributes["query_js"].(string); ok {
		c.QueryJS = v
	}
	c.AutoRefreshSeconds = anyToInt(doc.Attributes["auto_refresh_seconds"])
	c.SortOrder = anyToInt(doc.Attributes["sort_order"])
	if v, ok := doc.Attributes["enabled"].(bool); ok {
		c.Enabled = v
	}
	c.SpecVersion = anyToInt(doc.Attributes["spec_version"])
	if v, ok := doc.Attributes["generator"].(string); ok {
		c.Generator = v
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

// stringsToAny converts a []string to the []any storage representation, or nil
// when empty.
func stringsToAny(ss []string) []any {
	if len(ss) == 0 {
		return nil
	}
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
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

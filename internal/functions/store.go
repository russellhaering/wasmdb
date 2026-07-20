package functions

import (
	"context"
	"fmt"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/moraine/document"
	"github.com/russellhaering/moraine/index"
)

const functionsTable = "_functions"

// Function represents a stored function.
type Function struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Code        string    `json:"code"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store handles CRUD operations for stored functions.
type Store struct {
	registry *database.Registry
}

// NewStore creates a new function store.
func NewStore(registry *database.Registry) *Store {
	return &Store{registry: registry}
}

// Create creates a new stored function.
func (s *Store) Create(ctx context.Context, name, description, code, userID string) (*Function, error) {
	// Check for duplicate name.
	existing, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("function %q already exists", name)
	}

	tbl, err := s.registry.GetTable(ctx, functionsTable)
	if err != nil {
		return nil, fmt.Errorf("get functions table: %w", err)
	}

	now := time.Now().UTC()
	doc := &document.Document{
		Content: code,
		Attributes: map[string]any{
			"name":        name,
			"description": description,
			"created_by":  userID,
			"updated_at":  now.Format(time.RFC3339),
		},
	}

	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("create function: %w", err)
	}

	return &Function{
		ID:          doc.ID,
		Name:        name,
		Description: description,
		Code:        code,
		CreatedBy:   userID,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   now,
	}, nil
}

// Get retrieves a stored function by name.
func (s *Store) Get(ctx context.Context, name string) (*Function, error) {
	tbl, err := s.registry.GetTable(ctx, functionsTable)
	if err != nil {
		return nil, fmt.Errorf("get functions table: %w", err)
	}

	docs, err := tbl.SearchAttributes(ctx, []index.Filter{
		{Field: "name", Op: index.OpEq, Value: name},
	}, 1, 0)
	if err != nil {
		return nil, fmt.Errorf("search function: %w", err)
	}
	if len(docs) == 0 {
		return nil, nil
	}

	return docToFunction(docs[0]), nil
}

// List returns all stored functions.
func (s *Store) List(ctx context.Context) ([]*Function, error) {
	tbl, err := s.registry.GetTable(ctx, functionsTable)
	if err != nil {
		return nil, fmt.Errorf("get functions table: %w", err)
	}

	docs, _, err := tbl.ListDocuments(ctx, 1000, "")
	if err != nil {
		return nil, fmt.Errorf("list functions: %w", err)
	}

	fns := make([]*Function, 0, len(docs))
	for _, doc := range docs {
		fns = append(fns, docToFunction(doc))
	}
	return fns, nil
}

// Update updates a stored function's code and description.
func (s *Store) Update(ctx context.Context, name, code, description string) (*Function, error) {
	existing, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("function %q not found", name)
	}

	tbl, err := s.registry.GetTable(ctx, functionsTable)
	if err != nil {
		return nil, fmt.Errorf("get functions table: %w", err)
	}

	now := time.Now().UTC()
	doc := &document.Document{
		ID:      existing.ID,
		Content: code,
		Attributes: map[string]any{
			"name":        name,
			"description": description,
			"created_by":  existing.CreatedBy,
			"updated_at":  now.Format(time.RFC3339),
		},
	}

	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("update function: %w", err)
	}

	existing.Code = code
	existing.Description = description
	existing.UpdatedAt = now
	return existing, nil
}

// Delete removes a stored function by name.
func (s *Store) Delete(ctx context.Context, name string) error {
	existing, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("function %q not found", name)
	}

	tbl, err := s.registry.GetTable(ctx, functionsTable)
	if err != nil {
		return fmt.Errorf("get functions table: %w", err)
	}

	return tbl.DeleteDocument(ctx, existing.ID)
}

// docToFunction converts a document to a Function.
func docToFunction(doc *document.Document) *Function {
	f := &Function{
		ID:        doc.ID,
		Code:      doc.Content,
		CreatedAt: doc.CreatedAt,
	}
	if v, ok := doc.Attributes["name"].(string); ok {
		f.Name = v
	}
	if v, ok := doc.Attributes["description"].(string); ok {
		f.Description = v
	}
	if v, ok := doc.Attributes["created_by"].(string); ok {
		f.CreatedBy = v
	}
	if v, ok := doc.Attributes["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.UpdatedAt = t
		}
	}
	return f
}

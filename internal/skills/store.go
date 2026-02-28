package skills

import (
	"context"
	"fmt"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/functions"
	"github.com/russellhaering/wasmdb/internal/index"
)

const skillsTable = "_skills"

// Skill represents a stored skill.
type Skill struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	FunctionName string    `json:"function_name"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Store handles CRUD and execution operations for skills.
type Store struct {
	registry *database.Registry
	fnStore  *functions.Store
	fnEngine *functions.Engine
}

// NewStore creates a new skill store.
func NewStore(registry *database.Registry, fnStore *functions.Store, fnEngine *functions.Engine) *Store {
	return &Store{registry: registry, fnStore: fnStore, fnEngine: fnEngine}
}

// Create creates a new skill.
func (s *Store) Create(ctx context.Context, name, description, functionName, userID string) (*Skill, error) {
	existing, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("skill %q already exists", name)
	}

	if err := s.ensureFunctionExists(ctx, functionName); err != nil {
		return nil, err
	}

	tbl, err := s.registry.GetTable(ctx, skillsTable)
	if err != nil {
		return nil, fmt.Errorf("get skills table: %w", err)
	}

	now := time.Now().UTC()
	doc := &document.Document{
		Attributes: map[string]any{
			"name":          name,
			"description":   description,
			"function_name": functionName,
			"created_by":    userID,
			"updated_at":    now.Format(time.RFC3339),
		},
	}

	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("create skill: %w", err)
	}

	return &Skill{
		ID:           doc.ID,
		Name:         name,
		Description:  description,
		FunctionName: functionName,
		CreatedBy:    userID,
		CreatedAt:    doc.CreatedAt,
		UpdatedAt:    now,
	}, nil
}

// Get retrieves a skill by name.
func (s *Store) Get(ctx context.Context, name string) (*Skill, error) {
	tbl, err := s.registry.GetTable(ctx, skillsTable)
	if err != nil {
		return nil, fmt.Errorf("get skills table: %w", err)
	}

	docs, err := tbl.SearchAttributes(ctx, []index.Filter{
		{Field: "name", Op: index.OpEq, Value: name},
	}, 1, 0)
	if err != nil {
		return nil, fmt.Errorf("search skill: %w", err)
	}
	if len(docs) == 0 {
		return nil, nil
	}

	return docToSkill(docs[0]), nil
}

// List returns all skills.
func (s *Store) List(ctx context.Context) ([]*Skill, error) {
	tbl, err := s.registry.GetTable(ctx, skillsTable)
	if err != nil {
		return nil, fmt.Errorf("get skills table: %w", err)
	}

	docs, _, err := tbl.ListDocuments(ctx, 1000, "")
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}

	skills := make([]*Skill, 0, len(docs))
	for _, doc := range docs {
		skills = append(skills, docToSkill(doc))
	}
	return skills, nil
}

// Update updates skill metadata.
func (s *Store) Update(ctx context.Context, name, description, functionName string) (*Skill, error) {
	existing, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("skill %q not found", name)
	}

	if err := s.ensureFunctionExists(ctx, functionName); err != nil {
		return nil, err
	}

	tbl, err := s.registry.GetTable(ctx, skillsTable)
	if err != nil {
		return nil, fmt.Errorf("get skills table: %w", err)
	}

	now := time.Now().UTC()
	doc := &document.Document{
		ID: existing.ID,
		Attributes: map[string]any{
			"name":          name,
			"description":   description,
			"function_name": functionName,
			"created_by":    existing.CreatedBy,
			"updated_at":    now.Format(time.RFC3339),
		},
	}

	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("update skill: %w", err)
	}

	existing.Description = description
	existing.FunctionName = functionName
	existing.UpdatedAt = now
	return existing, nil
}

// Delete removes a skill by name.
func (s *Store) Delete(ctx context.Context, name string) error {
	existing, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("skill %q not found", name)
	}

	tbl, err := s.registry.GetTable(ctx, skillsTable)
	if err != nil {
		return fmt.Errorf("get skills table: %w", err)
	}

	return tbl.DeleteDocument(ctx, existing.ID)
}

// Execute runs a skill by executing its linked stored function.
func (s *Store) Execute(ctx context.Context, name string, params map[string]any) (*functions.ExecResult, error) {
	if s.fnStore == nil || s.fnEngine == nil {
		return nil, fmt.Errorf("skill execution is not available")
	}

	skill, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, fmt.Errorf("skill %q not found", name)
	}

	fn, err := s.fnStore.Get(ctx, skill.FunctionName)
	if err != nil {
		return nil, fmt.Errorf("get function %q: %w", skill.FunctionName, err)
	}
	if fn == nil {
		return nil, fmt.Errorf("linked function %q not found", skill.FunctionName)
	}

	result := s.fnEngine.Execute(ctx, fn.Code, params)
	return result, nil
}

func (s *Store) ensureFunctionExists(ctx context.Context, functionName string) error {
	if functionName == "" {
		return fmt.Errorf("function_name is required")
	}
	if s.fnStore == nil {
		return nil
	}
	fn, err := s.fnStore.Get(ctx, functionName)
	if err != nil {
		return fmt.Errorf("get function %q: %w", functionName, err)
	}
	if fn == nil {
		return fmt.Errorf("function %q not found", functionName)
	}
	return nil
}

func docToSkill(doc *document.Document) *Skill {
	sk := &Skill{ID: doc.ID, CreatedAt: doc.CreatedAt}
	if v, ok := doc.Attributes["name"].(string); ok {
		sk.Name = v
	}
	if v, ok := doc.Attributes["description"].(string); ok {
		sk.Description = v
	}
	if v, ok := doc.Attributes["function_name"].(string); ok {
		sk.FunctionName = v
	}
	if v, ok := doc.Attributes["created_by"].(string); ok {
		sk.CreatedBy = v
	}
	if v, ok := doc.Attributes["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			sk.UpdatedAt = t
		}
	}
	return sk
}

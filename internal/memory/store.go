package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/index"
)

const memoriesTable = "_memories"

// Memory scope values.
const (
	ScopeUser    = "user"
	ScopeSession = "session"
)

// Memory represents a persisted memory.
type Memory struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Scope      string    `json:"scope"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary"`
	Tags       []string  `json:"tags,omitempty"`
	Pinned     bool      `json:"pinned,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
}

// CatalogEntry is the compact metadata view used for progressive disclosure.
type CatalogEntry struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	Scope     string   `json:"scope"`
	Tags      []string `json:"tags,omitempty"`
	Pinned    bool     `json:"pinned,omitempty"`
	UpdatedAt string   `json:"updated_at"`
}

// Store handles memory persistence/retrieval.
type Store struct {
	registry *database.Registry
}

func NewStore(registry *database.Registry) *Store {
	return &Store{registry: registry}
}

func (s *Store) Create(ctx context.Context, m *Memory) (*Memory, error) {
	if m == nil {
		return nil, fmt.Errorf("memory is required")
	}
	if m.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if m.Scope == "" {
		m.Scope = ScopeUser
	}
	if m.Summary == "" {
		return nil, fmt.Errorf("summary is required")
	}
	if m.Title == "" {
		m.Title = deriveTitle(m.Summary)
	}

	now := time.Now().UTC()
	doc := &document.Document{
		ID:      m.ID,
		Content: m.Summary,
		Attributes: map[string]any{
			"user_id":      m.UserID,
			"scope":        m.Scope,
			"title":        m.Title,
			"summary":      m.Summary,
			"tags":         stringSliceToAny(m.Tags),
			"pinned":       m.Pinned,
			"updated_at":   now.Format(time.RFC3339),
			"last_used_at": now.Format(time.RFC3339),
		},
	}

	tbl, err := s.registry.GetTable(ctx, memoriesTable)
	if err != nil {
		return nil, fmt.Errorf("get memories table: %w", err)
	}
	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("create memory: %w", err)
	}
	return docToMemory(doc), nil
}

func (s *Store) Get(ctx context.Context, id string) (*Memory, error) {
	tbl, err := s.registry.GetTable(ctx, memoriesTable)
	if err != nil {
		return nil, fmt.Errorf("get memories table: %w", err)
	}
	doc, err := tbl.GetDocument(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get memory: %w", err)
	}
	if doc == nil {
		return nil, nil
	}
	return docToMemory(doc), nil
}

func (s *Store) Update(ctx context.Context, id string, patch *Memory) (*Memory, error) {
	if patch == nil {
		return nil, fmt.Errorf("patch is required")
	}
	cur, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, fmt.Errorf("memory %q not found", id)
	}

	if patch.Title != "" {
		cur.Title = patch.Title
	}
	if patch.Summary != "" {
		cur.Summary = patch.Summary
	}
	if patch.Scope != "" {
		cur.Scope = patch.Scope
	}
	if patch.Tags != nil {
		cur.Tags = patch.Tags
	}
	cur.Pinned = patch.Pinned
	cur.UpdatedAt = time.Now().UTC()
	if patch.LastUsedAt.IsZero() {
		cur.LastUsedAt = time.Now().UTC()
	} else {
		cur.LastUsedAt = patch.LastUsedAt.UTC()
	}

	doc := &document.Document{
		ID:      cur.ID,
		Content: cur.Summary,
		Attributes: map[string]any{
			"user_id":      cur.UserID,
			"scope":        cur.Scope,
			"title":        cur.Title,
			"summary":      cur.Summary,
			"tags":         stringSliceToAny(cur.Tags),
			"pinned":       cur.Pinned,
			"updated_at":   cur.UpdatedAt.Format(time.RFC3339),
			"last_used_at": cur.LastUsedAt.Format(time.RFC3339),
		},
	}

	tbl, err := s.registry.GetTable(ctx, memoriesTable)
	if err != nil {
		return nil, fmt.Errorf("get memories table: %w", err)
	}
	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("update memory: %w", err)
	}
	return docToMemory(doc), nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	tbl, err := s.registry.GetTable(ctx, memoriesTable)
	if err != nil {
		return fmt.Errorf("get memories table: %w", err)
	}
	return tbl.DeleteDocument(ctx, id)
}

// ListCatalog returns compact memory entries for progressive disclosure.
func (s *Store) ListCatalog(ctx context.Context, userID string, limit int) ([]CatalogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	tbl, err := s.registry.GetTable(ctx, memoriesTable)
	if err != nil {
		return nil, fmt.Errorf("get memories table: %w", err)
	}

	docs, err := tbl.SearchAttributes(ctx, []index.Filter{{Field: "user_id", Op: index.OpEq, Value: userID}}, 1000, 0)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}

	mems := make([]*Memory, 0, len(docs))
	for _, d := range docs {
		mems = append(mems, docToMemory(d))
	}
	// Claude-like progressive disclosure ranking: pinned first, then last_used/update recency.
	sort.Slice(mems, func(i, j int) bool {
		if mems[i].Pinned != mems[j].Pinned {
			return mems[i].Pinned
		}
		il := mems[i].LastUsedAt
		jl := mems[j].LastUsedAt
		if !il.Equal(jl) {
			return il.After(jl)
		}
		return mems[i].UpdatedAt.After(mems[j].UpdatedAt)
	})

	if len(mems) > limit {
		mems = mems[:limit]
	}

	out := make([]CatalogEntry, 0, len(mems))
	for _, m := range mems {
		summary := m.Summary
		if len(summary) > 180 {
			summary = summary[:180] + "..."
		}
		out = append(out, CatalogEntry{
			ID:        m.ID,
			Title:     m.Title,
			Summary:   summary,
			Scope:     m.Scope,
			Tags:      m.Tags,
			Pinned:    m.Pinned,
			UpdatedAt: m.UpdatedAt.Format(time.RFC3339),
		})
	}
	return out, nil
}

func (s *Store) Touch(ctx context.Context, id string) error {
	m, err := s.Get(ctx, id)
	if err != nil || m == nil {
		return err
	}
	_, err = s.Update(ctx, id, &Memory{Pinned: m.Pinned, LastUsedAt: time.Now().UTC()})
	return err
}

func deriveTitle(summary string) string {
	s := strings.TrimSpace(summary)
	if s == "" {
		return "Memory"
	}
	if len(s) > 72 {
		return s[:72] + "..."
	}
	return s
}

func stringSliceToAny(in []string) []any {
	out := make([]any, 0, len(in))
	for _, v := range in {
		out = append(out, v)
	}
	return out
}

func anyToStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return append([]string(nil), x...)
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func docToMemory(doc *document.Document) *Memory {
	m := &Memory{ID: doc.ID, CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt}
	if s, ok := doc.Attributes["user_id"].(string); ok {
		m.UserID = s
	}
	if s, ok := doc.Attributes["scope"].(string); ok {
		m.Scope = s
	}
	if s, ok := doc.Attributes["title"].(string); ok {
		m.Title = s
	}
	if s, ok := doc.Attributes["summary"].(string); ok {
		m.Summary = s
	}
	if m.Summary == "" {
		m.Summary = doc.Content
	}
	m.Tags = anyToStringSlice(doc.Attributes["tags"])
	if b, ok := doc.Attributes["pinned"].(bool); ok {
		m.Pinned = b
	}
	if ts, ok := doc.Attributes["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			m.UpdatedAt = t
		}
	}
	if ts, ok := doc.Attributes["last_used_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			m.LastUsedAt = t
		}
	}
	if m.Title == "" {
		m.Title = deriveTitle(m.Summary)
	}
	if m.Scope == "" {
		m.Scope = ScopeUser
	}
	return m
}

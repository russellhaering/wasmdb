package mcpservers

import (
	"context"
	"fmt"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/index"
)

const mcpServersTable = "_mcp_servers"

// MCPServer represents a registered MCP server configuration.
type MCPServer struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Transport   string            `json:"transport"`             // "streamable-http" or "stdio"
	URL         string            `json:"url,omitempty"`         // for streamable-http
	Command     string            `json:"command,omitempty"`     // for stdio
	Args        []string          `json:"args,omitempty"`        // for stdio
	Env         []string          `json:"env,omitempty"`         // for stdio (KEY=VALUE pairs)
	Headers     map[string]string `json:"headers,omitempty"`     // for streamable-http
	Enabled     bool              `json:"enabled"`
	CreatedBy   string            `json:"created_by"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Store handles CRUD operations for MCP server registrations.
type Store struct {
	registry *database.Registry
}

// NewStore creates a new MCP server store.
func NewStore(registry *database.Registry) *Store {
	return &Store{registry: registry}
}

// Create creates a new MCP server registration.
func (s *Store) Create(ctx context.Context, name, description, transport, url, command string, args, env []string, headers map[string]string, enabled bool, userID string) (*MCPServer, error) {
	if err := validateTransport(transport); err != nil {
		return nil, err
	}

	existing, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("MCP server %q already exists", name)
	}

	tbl, err := s.registry.GetTable(ctx, mcpServersTable)
	if err != nil {
		return nil, fmt.Errorf("get mcp_servers table: %w", err)
	}

	now := time.Now().UTC()
	doc := &document.Document{
		Attributes: map[string]any{
			"name":        name,
			"description": description,
			"transport":   transport,
			"url":         url,
			"command":     command,
			"args":        stringSliceToAny(args),
			"env":         stringSliceToAny(env),
			"headers":     stringMapToAny(headers),
			"enabled":     enabled,
			"created_by":  userID,
			"updated_at":  now.Format(time.RFC3339),
		},
	}

	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("create mcp server: %w", err)
	}

	return &MCPServer{
		ID:          doc.ID,
		Name:        name,
		Description: description,
		Transport:   transport,
		URL:         url,
		Command:     command,
		Args:        args,
		Env:         env,
		Headers:     headers,
		Enabled:     enabled,
		CreatedBy:   userID,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   now,
	}, nil
}

// Get retrieves an MCP server by name.
func (s *Store) Get(ctx context.Context, name string) (*MCPServer, error) {
	tbl, err := s.registry.GetTable(ctx, mcpServersTable)
	if err != nil {
		return nil, fmt.Errorf("get mcp_servers table: %w", err)
	}

	docs, err := tbl.SearchAttributes(ctx, []index.Filter{
		{Field: "name", Op: index.OpEq, Value: name},
	}, 1, 0)
	if err != nil {
		return nil, fmt.Errorf("search mcp server: %w", err)
	}
	if len(docs) == 0 {
		return nil, nil
	}

	return docToMCPServer(docs[0]), nil
}

// List returns all MCP servers.
func (s *Store) List(ctx context.Context) ([]*MCPServer, error) {
	tbl, err := s.registry.GetTable(ctx, mcpServersTable)
	if err != nil {
		return nil, fmt.Errorf("get mcp_servers table: %w", err)
	}

	docs, _, err := tbl.ListDocuments(ctx, 1000, "")
	if err != nil {
		return nil, fmt.Errorf("list mcp servers: %w", err)
	}

	servers := make([]*MCPServer, 0, len(docs))
	for _, doc := range docs {
		servers = append(servers, docToMCPServer(doc))
	}
	return servers, nil
}

// Update updates an MCP server registration.
func (s *Store) Update(ctx context.Context, name, description, transport, url, command string, args, env []string, headers map[string]string, enabled bool) (*MCPServer, error) {
	if err := validateTransport(transport); err != nil {
		return nil, err
	}

	existing, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("MCP server %q not found", name)
	}

	tbl, err := s.registry.GetTable(ctx, mcpServersTable)
	if err != nil {
		return nil, fmt.Errorf("get mcp_servers table: %w", err)
	}

	now := time.Now().UTC()
	doc := &document.Document{
		ID: existing.ID,
		Attributes: map[string]any{
			"name":        name,
			"description": description,
			"transport":   transport,
			"url":         url,
			"command":     command,
			"args":        stringSliceToAny(args),
			"env":         stringSliceToAny(env),
			"headers":     stringMapToAny(headers),
			"enabled":     enabled,
			"created_by":  existing.CreatedBy,
			"updated_at":  now.Format(time.RFC3339),
		},
	}

	if err := tbl.PutDocument(ctx, doc); err != nil {
		return nil, fmt.Errorf("update mcp server: %w", err)
	}

	existing.Description = description
	existing.Transport = transport
	existing.URL = url
	existing.Command = command
	existing.Args = args
	existing.Env = env
	existing.Headers = headers
	existing.Enabled = enabled
	existing.UpdatedAt = now
	return existing, nil
}

// Delete removes an MCP server by name.
func (s *Store) Delete(ctx context.Context, name string) error {
	existing, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("MCP server %q not found", name)
	}

	tbl, err := s.registry.GetTable(ctx, mcpServersTable)
	if err != nil {
		return fmt.Errorf("get mcp_servers table: %w", err)
	}

	return tbl.DeleteDocument(ctx, existing.ID)
}

func validateTransport(transport string) error {
	if transport != "streamable-http" && transport != "stdio" {
		return fmt.Errorf("invalid transport %q: must be \"streamable-http\" or \"stdio\"", transport)
	}
	return nil
}

func docToMCPServer(doc *document.Document) *MCPServer {
	srv := &MCPServer{ID: doc.ID, CreatedAt: doc.CreatedAt}
	if v, ok := doc.Attributes["name"].(string); ok {
		srv.Name = v
	}
	if v, ok := doc.Attributes["description"].(string); ok {
		srv.Description = v
	}
	if v, ok := doc.Attributes["transport"].(string); ok {
		srv.Transport = v
	}
	if v, ok := doc.Attributes["url"].(string); ok {
		srv.URL = v
	}
	if v, ok := doc.Attributes["command"].(string); ok {
		srv.Command = v
	}
	srv.Args = anyToStringSlice(doc.Attributes["args"])
	srv.Env = anyToStringSlice(doc.Attributes["env"])
	srv.Headers = anyToStringMap(doc.Attributes["headers"])
	if v, ok := doc.Attributes["enabled"].(bool); ok {
		srv.Enabled = v
	}
	if v, ok := doc.Attributes["created_by"].(string); ok {
		srv.CreatedBy = v
	}
	if v, ok := doc.Attributes["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			srv.UpdatedAt = t
		}
	}
	return srv
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

func stringMapToAny(in map[string]string) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func anyToStringMap(v any) map[string]string {
	switch x := v.(type) {
	case map[string]string:
		out := make(map[string]string, len(x))
		for k, v := range x {
			out[k] = v
		}
		return out
	case map[string]any:
		out := make(map[string]string, len(x))
		for k, v := range x {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
		return out
	default:
		return nil
	}
}

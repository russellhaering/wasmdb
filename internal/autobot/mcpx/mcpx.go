// Package mcpx bridges in-process MCP servers with the Claude API tool format.
package mcpx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerEntry holds a named MCP server along with its in-process transport pair.
// For external servers (no Server field), the clientSession is connected via
// CommandTransport or StreamableClientTransport.
type ServerEntry struct {
	Name          string
	Server        *mcp.Server          // nil for external servers
	serverSession *mcp.ServerSession   // nil for external servers
	clientSession *mcp.ClientSession
	external      bool                 // true for externally-connected servers
}

// ServerGroup manages multiple in-process MCP servers.
type ServerGroup struct {
	mu      sync.Mutex
	servers []*ServerEntry
}

// NewServerGroup creates a new empty server group.
func NewServerGroup() *ServerGroup {
	return &ServerGroup{}
}

// AddServer registers an in-process MCP server with the group.
func (sg *ServerGroup) AddServer(name string, server *mcp.Server) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	sg.servers = append(sg.servers, &ServerEntry{
		Name:   name,
		Server: server,
	})
}

// AddExternalSession registers a pre-connected external MCP client session.
func (sg *ServerGroup) AddExternalSession(name string, session *mcp.ClientSession) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	sg.servers = append(sg.servers, &ServerEntry{
		Name:          name,
		clientSession: session,
		external:      true,
	})
}

// Connect establishes in-memory connections to all registered servers.
func (sg *ServerGroup) Connect(ctx context.Context) error {
	sg.mu.Lock()
	defer sg.mu.Unlock()

	for _, entry := range sg.servers {
		serverTransport, clientTransport := mcp.NewInMemoryTransports()

		ss, err := entry.Server.Connect(ctx, serverTransport, nil)
		if err != nil {
			return fmt.Errorf("connecting server %q: %w", entry.Name, err)
		}
		entry.serverSession = ss

		client := mcp.NewClient(&mcp.Implementation{
			Name:    "wasmdb-agent-client",
			Version: "v0.1.0",
		}, nil)

		cs, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			return fmt.Errorf("connecting client to %q: %w", entry.Name, err)
		}
		entry.clientSession = cs
	}
	return nil
}

// Close closes all server and client sessions.
func (sg *ServerGroup) Close() error {
	sg.mu.Lock()
	defer sg.mu.Unlock()

	for _, entry := range sg.servers {
		if entry.clientSession != nil {
			entry.clientSession.Close()
		}
		if entry.serverSession != nil {
			entry.serverSession.Close()
		}
	}
	return nil
}

// ServerNames returns the names of all registered servers.
func (sg *ServerGroup) ServerNames() []string {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	names := make([]string, len(sg.servers))
	for i, e := range sg.servers {
		names[i] = e.Name
	}
	return names
}

// ListToolsForServer returns tools from a specific named server.
func (sg *ServerGroup) ListToolsForServer(ctx context.Context, serverName string) ([]*mcp.Tool, error) {
	sg.mu.Lock()
	var entry *ServerEntry
	for _, e := range sg.servers {
		if e.Name == serverName {
			entry = e
			break
		}
	}
	sg.mu.Unlock()

	if entry == nil {
		return nil, fmt.Errorf("server %q not found", serverName)
	}
	if entry.clientSession == nil {
		return nil, fmt.Errorf("server %q not connected", serverName)
	}

	result, err := entry.clientSession.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("listing tools from %q: %w", serverName, err)
	}
	return result.Tools, nil
}

// ListAllToolsDetailed returns all tools with their source server names.
func (sg *ServerGroup) ListAllToolsDetailed(ctx context.Context) ([]ToolInfo, error) {
	sg.mu.Lock()
	entries := make([]*ServerEntry, len(sg.servers))
	copy(entries, sg.servers)
	sg.mu.Unlock()

	var tools []ToolInfo
	for _, entry := range entries {
		if entry.clientSession == nil {
			continue
		}
		result, err := entry.clientSession.ListTools(ctx, nil)
		if err != nil {
			continue // skip servers that error
		}
		for _, t := range result.Tools {
			tools = append(tools, ToolInfo{
				ServerName:  entry.Name,
				Name:        t.Name,
				Description: t.Description,
			})
		}
	}
	return tools, nil
}

// ToolInfo provides metadata about a tool and its source server.
type ToolInfo struct {
	ServerName  string `json:"server"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Servers returns a snapshot of all registered server entries.
func (sg *ServerGroup) Servers() []*ServerEntry {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	entries := make([]*ServerEntry, len(sg.servers))
	copy(entries, sg.servers)
	return entries
}

// ListTools returns Anthropic-format tool definitions from all connected MCP servers.
func (sg *ServerGroup) ListTools(ctx context.Context, allowedTools, disallowedTools map[string]bool) ([]anthropic.ToolUnionParam, error) {
	sg.mu.Lock()
	entries := make([]*ServerEntry, len(sg.servers))
	copy(entries, sg.servers)
	sg.mu.Unlock()

	var tools []anthropic.ToolUnionParam

	for _, entry := range entries {
		if entry.clientSession == nil {
			continue
		}

		result, err := entry.clientSession.ListTools(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("listing tools from %q: %w", entry.Name, err)
		}

		for _, mcpTool := range result.Tools {
			if allowedTools != nil && !allowedTools[mcpTool.Name] {
				continue
			}
			if disallowedTools != nil && disallowedTools[mcpTool.Name] {
				continue
			}

			tool, err := ConvertTool(mcpTool)
			if err != nil {
				return nil, fmt.Errorf("converting tool %q: %w", mcpTool.Name, err)
			}
			tools = append(tools, tool)
		}
	}

	return tools, nil
}

// ConvertTool converts an MCP Tool to an Anthropic ToolUnionParam.
func ConvertTool(t *mcp.Tool) (anthropic.ToolUnionParam, error) {
	schemaBytes, err := json.Marshal(t.InputSchema)
	if err != nil {
		return anthropic.ToolUnionParam{}, fmt.Errorf("marshaling input schema: %w", err)
	}

	var schemaMap map[string]any
	if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
		return anthropic.ToolUnionParam{}, fmt.Errorf("unmarshaling input schema: %w", err)
	}

	// Properties must never be nil or the Anthropic SDK omits input_schema entirely.
	properties, ok := schemaMap["properties"]
	if !ok || properties == nil {
		properties = map[string]any{}
	}
	required, _ := schemaMap["required"].([]any)

	var requiredStrs []string
	for _, r := range required {
		if s, ok := r.(string); ok {
			requiredStrs = append(requiredStrs, s)
		}
	}

	tool := anthropic.ToolParam{
		Name:        t.Name,
		Description: anthropic.String(t.Description),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: properties,
			Required:   requiredStrs,
		},
	}

	return anthropic.ToolUnionParam{OfTool: &tool}, nil
}

// CallTool routes a tool call to the appropriate MCP server and returns the result.
func (sg *ServerGroup) CallTool(ctx context.Context, name string, input json.RawMessage) (*mcp.CallToolResult, error) {
	sg.mu.Lock()
	entries := make([]*ServerEntry, len(sg.servers))
	copy(entries, sg.servers)
	sg.mu.Unlock()

	var args map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, fmt.Errorf("parsing tool arguments: %w", err)
		}
	}

	for _, entry := range entries {
		if entry.clientSession == nil {
			continue
		}

		result, err := entry.clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      name,
			Arguments: args,
		})
		if err != nil {
			continue
		}
		return result, nil
	}

	return nil, fmt.Errorf("tool %q not found in any server", name)
}

// CallToolResultToBlocks converts an MCP CallToolResult to Anthropic content blocks.
func CallToolResultToBlocks(result *mcp.CallToolResult) []anthropic.ToolResultBlockParamContentUnion {
	var blocks []anthropic.ToolResultBlockParamContentUnion

	for _, content := range result.Content {
		switch c := content.(type) {
		case *mcp.TextContent:
			blocks = append(blocks, anthropic.ToolResultBlockParamContentUnion{
				OfText: &anthropic.TextBlockParam{
					Text: c.Text,
				},
			})
		case *mcp.ImageContent:
			blocks = append(blocks, anthropic.ToolResultBlockParamContentUnion{
				OfImage: &anthropic.ImageBlockParam{
					Source: anthropic.ImageBlockParamSourceUnion{
						OfBase64: &anthropic.Base64ImageSourceParam{
							MediaType: anthropic.Base64ImageSourceMediaType(c.MIMEType),
							Data:      base64.StdEncoding.EncodeToString(c.Data),
						},
					},
				},
			})
		}
	}

	if len(blocks) == 0 {
		blocks = append(blocks, anthropic.ToolResultBlockParamContentUnion{
			OfText: &anthropic.TextBlockParam{
				Text: "(no output)",
			},
		})
	}

	return blocks
}

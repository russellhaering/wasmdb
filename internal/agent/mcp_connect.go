package agent

import (
	"context"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/russellhaering/wasmdb/internal/autobot/mcpx"
	"github.com/russellhaering/wasmdb/internal/mcpservers"
)

// connectExternalMCPServers reads registered MCP server configs and connects
// to each enabled one, adding their sessions to the server group.
func connectExternalMCPServers(ctx context.Context, store *mcpservers.Store, sg *mcpx.ServerGroup) {
	servers, err := store.List(ctx)
	if err != nil {
		slog.Warn("failed to list MCP servers for connection", "err", err)
		return
	}

	for _, srv := range servers {
		if !srv.Enabled {
			slog.Debug("skipping disabled MCP server", "name", srv.Name)
			continue
		}

		session, err := connectExternalMCPServer(ctx, srv)
		if err != nil {
			slog.Warn("failed to connect to MCP server",
				"name", srv.Name,
				"transport", srv.Transport,
				"err", err,
			)
			continue
		}

		sg.AddExternalSession(srv.Name, session)
		slog.Info("connected to external MCP server", "name", srv.Name, "transport", srv.Transport)
	}
}

// connectExternalMCPServer connects to a single external MCP server.
func connectExternalMCPServer(ctx context.Context, srv *mcpservers.MCPServer) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "wasmdb-mcp-client",
		Version: "v0.1.0",
	}, nil)

	var transport mcp.Transport

	switch srv.Transport {
	case "streamable-http":
		st := &mcp.StreamableClientTransport{
			Endpoint: srv.URL,
		}
		if len(srv.Headers) > 0 {
			// Wrap the default HTTP client to inject custom headers.
			st.HTTPClient = &http.Client{
				Transport: &headerTransport{
					headers: srv.Headers,
					base:    http.DefaultTransport,
				},
			}
		}
		transport = st

	case "stdio":
		cmd := exec.CommandContext(ctx, srv.Command, srv.Args...)
		if len(srv.Env) > 0 {
			cmd.Env = srv.Env
		}
		transport = &mcp.CommandTransport{Command: cmd}

	default:
		return nil, nil
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}

	return session, nil
}

// headerTransport injects custom HTTP headers into all requests.
type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}

// searchToolsInGroup searches for tools across all connected MCP servers
// by matching against tool names and descriptions.
func searchToolsInGroup(ctx context.Context, sg *mcpx.ServerGroup, query string) ([]mcpx.ToolInfo, error) {
	allTools, err := sg.ListAllToolsDetailed(ctx)
	if err != nil {
		return nil, err
	}

	if query == "" {
		return allTools, nil
	}

	q := strings.ToLower(query)
	tokens := strings.Fields(q)

	var matched []mcpx.ToolInfo
	for _, tool := range allTools {
		textLower := strings.ToLower(tool.Name + " " + tool.Description + " " + tool.ServerName)
		hit := false
		for _, tok := range tokens {
			if strings.Contains(textLower, tok) {
				hit = true
				break
			}
		}
		if hit {
			matched = append(matched, tool)
		}
	}
	return matched, nil
}

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

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

		// Build transport chain: base -> OAuth (if configured) -> headers
		var rt http.RoundTripper = http.DefaultTransport

		if srv.OAuth != nil && srv.OAuth.TokenURL != "" {
			rt = newOAuthTransport(srv.OAuth, rt)
		}

		if len(srv.Headers) > 0 {
			rt = &headerTransport{headers: srv.Headers, base: rt}
		}

		if rt != http.DefaultTransport {
			st.HTTPClient = &http.Client{Transport: rt}
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

// oauthTransport implements http.RoundTripper and automatically acquires/refreshes
// an OAuth 2.0 access token using the client_credentials grant.
type oauthTransport struct {
	config *mcpservers.OAuthConfig
	base   http.RoundTripper

	mu          sync.Mutex
	accessToken string
	expiry      time.Time
}

func newOAuthTransport(config *mcpservers.OAuthConfig, base http.RoundTripper) *oauthTransport {
	return &oauthTransport{config: config, base: base}
}

func (t *oauthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.token()
	if err != nil {
		return nil, fmt.Errorf("oauth token acquisition: %w", err)
	}
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+token)
	return t.base.RoundTrip(req)
}

func (t *oauthTransport) token() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Return cached token if still valid (with 30s buffer).
	if t.accessToken != "" && time.Now().Add(30*time.Second).Before(t.expiry) {
		return t.accessToken, nil
	}

	// Perform client_credentials grant.
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {t.config.ClientID},
		"client_secret": {t.config.ClientSecret},
	}
	if len(t.config.Scopes) > 0 {
		data.Set("scope", strings.Join(t.config.Scopes, " "))
	}

	resp, err := http.PostForm(t.config.TokenURL, data)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access_token in response")
	}

	t.accessToken = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		t.expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	} else {
		// Default to 1 hour if not specified.
		t.expiry = time.Now().Add(time.Hour)
	}

	slog.Debug("acquired OAuth token", "expires_in", tokenResp.ExpiresIn)
	return t.accessToken, nil
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

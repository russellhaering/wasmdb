package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

func init() {
	register(command{
		noun:        "mcp",
		verb:        "register",
		usage:       "wasmdb mcp register <name> --transport <type> [--url <url>] [--command <cmd>] [--args <a1,a2>] [--env <K=V,...>] [--header <K=V,...>] [--oauth-token-url <url> --oauth-client-id <id> --oauth-client-secret <secret> --oauth-scopes <s1,s2>] [--description <desc>] [--disabled] [--json]",
		description: "Register an MCP server",
		run:         mcpRegister,
	})
	register(command{
		noun:        "mcp",
		verb:        "list",
		usage:       "wasmdb mcp list [--json]",
		description: "List registered MCP servers",
		run:         mcpList,
	})
	register(command{
		noun:        "mcp",
		verb:        "get",
		usage:       "wasmdb mcp get <name> [--json]",
		description: "Get MCP server details",
		run:         mcpGet,
	})
	register(command{
		noun:        "mcp",
		verb:        "update",
		usage:       "wasmdb mcp update <name> --transport <type> [--url <url>] [--command <cmd>] [--args <a1,a2>] [--env <K=V,...>] [--header <K=V,...>] [--oauth-token-url <url> --oauth-client-id <id> --oauth-client-secret <secret> --oauth-scopes <s1,s2>] [--description <desc>] [--disabled] [--json]",
		description: "Update an MCP server registration",
		run:         mcpUpdate,
	})
	register(command{
		noun:        "mcp",
		verb:        "delete",
		usage:       "wasmdb mcp delete <name>",
		description: "Delete an MCP server registration",
		run:         mcpDelete,
	})
}

func parseMCPFlags(ctx *cmdContext) (description, transport, url, command string, args, env []string, headers map[string]string, oauth *OAuthConfig, enabled bool) {
	description = ctx.flag("description")
	transport = ctx.flag("transport")
	url = ctx.flag("url")
	command = ctx.flag("command")
	enabled = !ctx.hasFlag("disabled")

	if argsStr := ctx.flag("args"); argsStr != "" {
		args = strings.Split(argsStr, ",")
	}
	if envStr := ctx.flag("env"); envStr != "" {
		env = strings.Split(envStr, ",")
	}
	if headerStr := ctx.flag("header"); headerStr != "" {
		headers = make(map[string]string)
		for _, h := range strings.Split(headerStr, ",") {
			parts := strings.SplitN(h, "=", 2)
			if len(parts) == 2 {
				headers[parts[0]] = parts[1]
			}
		}
	}

	// Also try JSON for headers
	if headerJSON := ctx.flag("headers-json"); headerJSON != "" {
		headers = make(map[string]string)
		_ = json.Unmarshal([]byte(headerJSON), &headers)
	}

	// OAuth client_credentials
	if tokenURL := ctx.flag("oauth-token-url"); tokenURL != "" {
		oauth = &OAuthConfig{
			ClientID:     ctx.flag("oauth-client-id"),
			ClientSecret: ctx.flag("oauth-client-secret"),
			TokenURL:     tokenURL,
		}
		if scopeStr := ctx.flag("oauth-scopes"); scopeStr != "" {
			oauth.Scopes = strings.Split(scopeStr, ",")
		}
	}

	return
}

func mcpRegister(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("MCP server name required")
	}
	name := ctx.args[0]
	description, transport, url, command, args, env, headers, oauth, enabled := parseMCPFlags(ctx)

	if transport == "" {
		return fmt.Errorf("--transport is required (streamable-http or stdio)")
	}

	srv, err := ctx.backend.CreateMCPServer(ctx, name, description, transport, url, command, args, env, headers, oauth, enabled)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, srv)
	}
	fmt.Fprintf(ctx.stdout, "Registered MCP server %q (transport=%s, enabled=%t)\n", name, srv.Transport, srv.Enabled)
	return nil
}

func mcpList(ctx *cmdContext) error {
	servers, err := ctx.backend.ListMCPServers(ctx)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, servers)
	}

	if len(servers) == 0 {
		fmt.Fprintln(ctx.stdout, "no MCP servers registered")
		return nil
	}

	for _, srv := range servers {
		target := srv.URL
		if target == "" {
			target = srv.Command
		}
		status := "enabled"
		if !srv.Enabled {
			status = "disabled"
		}
		fmt.Fprintf(ctx.stdout, "%s\t%s\t%s\t%s\t%s\n", srv.Name, srv.Transport, target, status, srv.UpdatedAt)
	}
	return nil
}

func mcpGet(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("MCP server name required")
	}
	name := ctx.args[0]

	srv, err := ctx.backend.GetMCPServer(ctx, name)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, srv)
	}

	fmt.Fprintf(ctx.stdout, "Name: %s\nID: %s\nTransport: %s\n", srv.Name, srv.ID, srv.Transport)
	if srv.URL != "" {
		fmt.Fprintf(ctx.stdout, "URL: %s\n", srv.URL)
	}
	if srv.Command != "" {
		fmt.Fprintf(ctx.stdout, "Command: %s\n", srv.Command)
	}
	if len(srv.Args) > 0 {
		fmt.Fprintf(ctx.stdout, "Args: %s\n", strings.Join(srv.Args, ", "))
	}
	fmt.Fprintf(ctx.stdout, "Enabled: %t\n", srv.Enabled)
	if srv.Description != "" {
		fmt.Fprintf(ctx.stdout, "Description: %s\n", srv.Description)
	}
	fmt.Fprintf(ctx.stdout, "Created: %s\nUpdated: %s\n", srv.CreatedAt, srv.UpdatedAt)
	return nil
}

func mcpUpdate(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("MCP server name required")
	}
	name := ctx.args[0]
	description, transport, url, command, args, env, headers, oauth, enabled := parseMCPFlags(ctx)

	if transport == "" {
		return fmt.Errorf("--transport is required (streamable-http or stdio)")
	}

	srv, err := ctx.backend.UpdateMCPServer(ctx, name, description, transport, url, command, args, env, headers, oauth, enabled)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, srv)
	}
	fmt.Fprintf(ctx.stdout, "Updated MCP server %q (transport=%s, enabled=%t)\n", name, srv.Transport, srv.Enabled)
	return nil
}

func mcpDelete(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("MCP server name required")
	}
	name := ctx.args[0]

	if err := ctx.backend.DeleteMCPServer(ctx, name); err != nil {
		return err
	}

	fmt.Fprintf(ctx.stdout, "Deleted MCP server %q\n", name)
	return nil
}

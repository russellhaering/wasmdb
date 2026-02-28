# TODO

## Data UI Builder
Allow creation of a UI for data stored in the database. UI configuration is stored in a system table. Use Google's A2UI framework (research in depth at implementation time). Pre-create an agent that runs daily and updates the UI based on the schema and a sample of data in the system.

## Rego-Based Permissions
Add a permission system built on [OPA/Rego](https://www.openpolicyagent.org/). Policies would govern who can read/write which tables, documents, or fields. Evaluate policies per-request using the bearer token identity as input.

## Agent Data Display (Tables & Cards) 🟡
Agents can render structured data in conversations using A2UI components (DataTable, Card, Text, Row, Column, Divider). Claude emits ` ```a2ui ` fenced JSON blocks; the server-side `a2uiSplitter` strips fences and emits `event: artifact` SSE events; the web client renders them directly. TUI-themed web UI with monospace styling.

**Done:** A2UI Go types + validation, system prompt with examples, server-side fence detection (`a2ui_splitter.go`), `artifact` SSE event type, web JS renderer, CSS for all component types.
**Remaining:** CLI ANSI renderer for A2UI components (box-drawing tables/cards in terminal).

## Stored Functions ✅
JavaScript functions execute in a QuickJS-in-Wasm sandbox (via `github.com/fastschema/qjs` + wazero). Pure Go, no cgo, CGO_ENABLED=0 compatible. Functions have access to a `db` host API for table CRUD, search, and document operations. System tables are blocked from function access.

**Done:** Engine core (`internal/functions/`), `db` host API bindings, handler(params) + bare expression modes, console.log capture, 30s timeout, 64MB memory limit, 10 concurrent execution limit. REST API (CRUD: `POST/GET/PUT/DELETE /v1/functions`, exec: `POST /v1/functions/{name}/exec`, ephemeral: `POST /v1/exec`). Agent tools (`execute_code`, `manage_function`) with system prompt. CLI commands (`fn create/list/get/update/delete/exec`, `exec --file/--code`). `_functions` system table.

## Agents, Skills & Memories 🟡
Introduce first-class concepts of "agents", "skills", and "memories", all stored in the database (likely as system tables). Agents are configurable AI actors; skills define reusable capabilities an agent can invoke; memories are persistent context that agents accumulate over time and can recall in future interactions.

**Done (skills):** `_skills` system table, skills store (`internal/skills`) with CRUD + execute (via linked stored function), REST API (`POST/GET/PUT/DELETE /v1/skills`, `POST /v1/skills/{name}/exec`), CLI commands (`skill create/list/get/update/delete/exec`), agent `manage_skill` + progressive-disclosure tools (`list_skills_catalog`, `get_skill_detail`), compact catalog injection in chat, and manual-only skill control (`disable_model_invocation`).
**Remaining:** Implement skill + memory selection/ranking heuristic for catalog budgeting at scale (intent-match + recency + pinned + tag/name boosts, budget-aware packing, and selective detail fetch), plus agent first-class models + APIs.

## Agent MCP Server Configuration ✅
MCP servers can be registered via the `_mcp_servers` system table. Supports `streamable-http` (URL-based) and `stdio` (command-based) transports with custom headers and environment variables. Registered servers are automatically connected when chat sessions start, making their tools available to the agent.

**Done:** `_mcp_servers` system table, `internal/mcpservers` store (CRUD), REST API (`POST/GET/PUT/DELETE /v1/mcp-servers`), CLI commands (`mcp register/list/get/update/delete`), agent `manage_mcp_server` tool, `search_tools` tool for cross-server tool discovery, automatic connection to enabled servers at chat startup via `mcp.StreamableClientTransport` and `mcp.CommandTransport`. OAuth 2.0 `client_credentials` flow: stores client_id/secret/token_url/scopes per server, acquires and auto-refreshes Bearer tokens at connect time. Static headers also supported.

## Chat Agent Activity Indicator ✅
Implemented real SSE streaming (replaced fake batch-then-emit with `anthropic.NewStreaming()` + `Accumulate()`). Text deltas now stream token-by-token to the browser. Tool call indicators have animated dots (`...` CSS animation) while pending, switching to "done"/"error" on completion. Combined with token-level streaming, the UI no longer feels stuck during agent turns.

## Chat Session Persistence ✅
Chat sessions are now persisted to the `_chat_sessions` system table. Message history is JSON-serialized in the document Content field, keyed by ULID session IDs. Sessions survive restarts with an LRU in-memory cache (100 sessions) and DB fallback. The UI has a sidebar for session management (list, switch, delete, new).

## CLI User Management 🟡
`wasmdb user create --email E --password P` and `wasmdb user list` commands are implemented via the existing REST API (`POST /v1/users`, `GET /v1/users`).

**Remaining:** add `wasmdb user delete <id>` (and optional `--email` targeting) to cover the existing `DELETE /v1/users/{id}` API from the CLI.

## Headless Device-Code Login ✅
`wasmdb login --url URL --headless true` starts a device-code flow: server creates a pending code, CLI prints a login URL and polls every 2s, user completes login in browser, CLI receives token. Enables CLI auth from headless/remote environments where localhost callbacks don't work.

## CLI `api` Subcommand
Add a low-level `wasmdb api` command inspired by `gh api` for direct access to arbitrary endpoints.

Proposed scope:
- `wasmdb api <path>` defaults to `GET`
- `--method/-X` for HTTP verb override
- `--field/-F key=value` (repeatable) for JSON body construction
- `--raw-field key=value` for string-only fields
- `--input/-i <file>` for raw request body
- `--header/-H "Key: Value"` for custom headers
- `--paginate` helper for list endpoints
- `--jq` or `--template` style output filtering (optional follow-up)

Use existing auth/token resolution so this works for unsupported/new server endpoints without waiting for first-class CLI wrappers.

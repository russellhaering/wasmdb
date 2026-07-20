# CLAUDE.md

## Authentication

All API endpoints require session-based authentication except `/healthz`, `/readyz`, `/v1/auth/login`, and `/auth/cli-login`.

### Seed User

Bootstrap the first deployment by setting environment variables:

```bash
export WASMDB_SEED_USER_EMAIL=admin@example.com
export WASMDB_SEED_USER_PASSWORD=your-password
```

A user is created on startup only if the `_users` table is empty. Once any user exists, the seed is a no-op.

### Login

Authenticate via `POST /v1/auth/login` with `{"email": "...", "password": "..."}`. The response includes a session token and sets a `wasmdb_session` cookie.

Requests can authenticate with either:
- `wasmdb_session` cookie (set automatically by the login endpoint)
- `Authorization: Bearer <session-token>` header

Sessions expire after 7 days. Only the SHA-256 hash of the token is stored in the `_sessions` system table.

### CLI Login

```bash
wasmdb login --url http://localhost:8080
```

Opens a browser for interactive login. For headless environments:

```bash
wasmdb login --url http://localhost:8080 --email admin@example.com --password your-password
```

Credentials are stored at `~/.config/wasmdb/credentials.json`.

### Auth Endpoints

- `POST /v1/auth/login` — authenticate, returns token + sets cookie
- `POST /v1/auth/logout` — invalidate session, clear cookie
- `GET /v1/auth/me` — return current user info
- `GET /auth/cli-login` — HTML login page for CLI browser flow

The auth middleware is implemented in `internal/api/server.go`, session management in `internal/auth/`.

## Configuration File

The CLI reads `~/.config/wasmdb/config.json` for persistent settings:

```json
{
  "url": "https://wasmdb.fly.dev",
  "default_format": "json"
}
```

Resolution order for URL: `--url` flag > `WASMDB_URL` env > config file > `http://localhost:8080`.

Manage via CLI:

```bash
wasmdb config set url https://wasmdb.fly.dev
wasmdb config set default_format json
wasmdb config get url
wasmdb config list
wasmdb config path
```

## CLI Quick Reference

The CLI binary is at `cmd/wasmdb-cli`. Build with `go run ./cmd/wasmdb-cli ...` or `go build -o wasmdb ./cmd/wasmdb-cli`.

Target the deployed instance with `--url` or `WASMDB_URL`:

```bash
export WASMDB_URL=https://wasmdb.fly.dev
```

Login first (credentials saved to `~/.config/wasmdb/credentials.json`):

```bash
wasmdb login --url https://wasmdb.fly.dev --email EMAIL --password PASS
```

Then:

```bash
wasmdb db list                          # list tables
wasmdb db create mydb                   # create table
wasmdb doc create mydb --attr key=val   # create document
wasmdb doc get mydb DOC_ID              # get document
wasmdb search text mydb "query"         # full-text search
wasmdb user create --email E --password P  # create user
wasmdb user list                        # list users
wasmdb fn create myfn --file fn.js       # create stored function
wasmdb fn list                           # list stored functions
wasmdb fn get myfn                       # get function details + code
wasmdb fn update myfn --file fn.js       # update function code
wasmdb fn delete myfn                    # delete stored function
wasmdb fn exec myfn --params '{"x":1}'   # execute stored function
wasmdb exec --file script.js             # execute ephemeral JS
wasmdb exec --code 'db.tables()'         # inline ephemeral JS
wasmdb mcp register srv --transport streamable-http --url URL  # register MCP server
wasmdb mcp list                          # list registered MCP servers
wasmdb mcp get srv                       # get MCP server details
wasmdb mcp update srv --transport stdio --command cmd  # update MCP server
wasmdb mcp delete srv                    # delete MCP server
wasmdb ui create mypage --surface-file s.json --actions-file a.json --title "My Page"  # create UI page
wasmdb ui list                           # list UI pages
wasmdb ui get mypage                     # get UI page details (incl. surface/actions/query)
wasmdb ui update mypage --surface-file s.json --actions-file a.json  # update UI page (partial patch)
wasmdb ui render mypage --param q=foo    # server-render a page: resolved {data} as a table
wasmdb ui delete mypage                  # delete UI page
wasmdb agent create myagent --prompt "..." --schedule 1h  # create background agent
wasmdb agent list                        # list background agents
wasmdb agent get myagent                 # get agent details
wasmdb agent update myagent --prompt "..." --schedule 30m  # update agent
wasmdb agent delete myagent              # delete agent
wasmdb agent trigger myagent             # trigger agent run immediately
wasmdb agent runs myagent                # list recent agent runs
wasmdb api /v1/tables                    # GET request to API
wasmdb api /v1/tables -X POST -F name=test  # POST with JSON body
wasmdb api /v1/exec --input script.json  # POST with file body
wasmdb api /healthz -H 'X-Custom: val'   # custom headers
wasmdb config set url https://host       # set default URL
wasmdb config list                       # show all config
wasmdb chat                              # interactive chat
```

Add `--json` to any command for JSON output.

## UI Pages

WasmDB auto-generates an interactive, data-aware UI. For every non-system table a
deterministic pure-Go generator (`internal/uigen`) emits a scaffold page named
`tbl-<table>`: a live `DataTable`, a create `Form` derived from the schema (or
inferred from sampled documents for schemaless tables), edit/delete row actions,
and a search box when a field is full-text indexed. No LLM or API key is required,
so the UI is populated on day one. Scaffolding runs on a startup sweep, on schema
change (debounced), and on first write to a previously empty table.

**Surface format v2 (`internal/surface`)** — a flat, typed component list
(`Column`/`Row`/`Card`/`Text`/`Metric`/`DataTable`/`Form`/`Input`/`Button`/…) with
strict per-type validation. Data binds structurally via `{"$data":"path"}` refs
resolved against the `query_js` result (no string templating). Write behavior lives
in page-level **actions** (`insert`/`update`/`delete`/`query`); components may only
reference declared actions, and the executor validates params against the action
declaration and the table schema before touching the database (system tables are
rejected). `surface.SpecMarkdown()` is the single source of truth for the LLM spec.

**Endpoints** (all session-authenticated):

- `GET/POST /v1/ui/pages`, `GET/PATCH/DELETE /v1/ui/pages/{name}` (PATCH = partial patch)
- `POST /v1/ui/pages/{name}/render` — body `{params?}` → `{surface, data, actions, logs}`
- `POST /v1/ui/pages/{name}/actions/{action}` — body `{params}` → `{ok, result?, data?}`

The browser renderer is a single embedded `surface.js` (served from
`/ui/assets/*`), used both by the dashboard at `/ui` and by chat embeds. In chat,
the agent emits a ```surface-ref``` fence (`{"page":"<name>"}`) that mounts a live
embed of the stored page; the CLI prints a `[ui page … updated — view at /ui]`
notice instead. `wasmdb ui render` prints the resolved `{data}` as a table.

**Generator provenance** — each page records `generator`: `scaffold` (owned/overwritten
by the generator), `agent` (touched by `manage_ui`), or `user` (edited via HTTP/CLI).
The sweep only creates/overwrites `scaffold` pages and never clobbers `agent`/`user`
pages. On startup, pages not on `spec_version == 2` are disabled (not deleted) and
the scaffold regenerates v2 coverage for their tables.

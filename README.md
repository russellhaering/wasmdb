# WasmDB

**A database built for AI agents — that grows its own UI.**

WasmDB is a document database that runs as a single Go binary with object storage (S3) as its only dependency. Agents are first-class users: every capability — tables, documents, search, scripting, scheduled jobs, UI pages — is exposed as tools an LLM can call, and a built-in chat agent operates the database in plain English. When data appears, WasmDB generates a working web UI for it automatically: a deterministic scaffolder emits CRUD pages from the schema and actual data within seconds, and an AI agent keeps them polished. Nobody writes frontend code; you can ask for changes in chat.

The storage engine is [moraine](https://github.com/russellhaering/moraine), an LSM tree over object storage extracted from this project — stateless compute, S3-grade durability, scale-to-zero.

## Highlights

- **One binary, one dependency.** Go binary + an S3-compatible bucket (AWS, Tigris, MinIO, R2). No bucket configured → in-memory mode for local hacking.
- **Documents with optional schemas.** Markdown content plus typed attributes (`string`, `int`, `float`, `bool`, arrays, `datetime`, `reference`). Schemaless tables work too.
- **Three kinds of search.** BM25 full-text (Bleve), vector similarity (HNSW, embeddings via OpenAI), and typed attribute filtering.
- **A self-generating, self-maintaining UI.** Every table gets a live CRUD page — data table, create form, edit/delete, search — generated deterministically from schema and data, no LLM required. With an API key, agents refine the pages and you can tweak them via chat.
- **Agent-native.** A built-in chat agent with 23 tools, sandboxed JavaScript execution (QuickJS), stored functions, skills, persistent memories, scheduled background agents, and pluggable external MCP servers.
- **REST, GraphQL, CLI, and chat** interfaces over the same core.

## Quick start

```bash
go build -o wasmdb ./cmd/wasmdb
export WASMDB_SEED_USER_EMAIL=admin@example.com
export WASMDB_SEED_USER_PASSWORD=changeme
./wasmdb
```

With no `WASMDB_S3_BUCKET` set, the server runs on an in-memory store (nothing persists). Add S3 credentials for durability, and `ANTHROPIC_API_KEY` to enable the chat agent.

Then watch the UI appear:

```bash
# Log in
TOKEN=$(curl -s -X POST localhost:8080/v1/auth/login \
  -d '{"email":"admin@example.com","password":"changeme"}' | jq -r .token)

# Create a table with a schema
curl -s -X POST localhost:8080/v1/tables -H "Authorization: Bearer $TOKEN" -d '{
  "name": "issues",
  "schema": {"fields": [
    {"name": "title",  "type": "string", "required": true, "full_text": true},
    {"name": "status", "type": "string", "indexed": true},
    {"name": "open",   "type": "bool"}
  ]}}'
```

Open `http://localhost:8080/ui` — within ~5 seconds an **Issues** page exists, with a live table, a create form, edit/delete row actions, and a search box. Nobody built it. Or open `http://localhost:8080/chat` and just say *"track my vendor invoices"* — the agent creates the table, and the UI follows.

## The self-maintaining UI

WasmDB's UI is generated, not written, in two layers:

1. **Deterministic scaffold** (`internal/uigen`) — pure Go, no LLM, no API key. For every table it emits a page from the schema (or from sampled documents when schemaless): typed columns, a create form, edit/delete actions, search when full-text fields exist. Runs at startup, on schema changes, and on first write to an empty table, so the UI exists the moment data does.
2. **Agent polish** — with an `ANTHROPIC_API_KEY`, a built-in `ui-builder` background agent reviews the scaffolds against real data and improves them (summary metrics, select inputs from observed values, better layouts), and the chat agent edits pages on request: *"show total outstanding at the top and highlight overdue invoices."*

Pages are described in a typed component format (`internal/surface`): a validated component tree (tables, forms, inputs, buttons, metrics, layout) plus declarative **actions** (`insert`/`update`/`delete`/`query`) that are the only write path from the browser — every action is validated against its declaration and the table schema server-side. Data binds structurally via `{"$data": "path"}` references resolved against a sandboxed `query_js` result at render time, so pages are always live. The LLM-facing format spec is generated from the same component registry that validates pages, so the model's instructions can never drift from what the validator accepts. Provenance tracking (`scaffold` / `agent` / `user`) ensures the auto-generator never overwrites a page a human or agent customized.

A single embedded renderer (`surface.js`, no build step, no framework) drives both the dashboard at `/ui` and live page embeds inside chat.

## Chat and agents

`/chat` is a built-in agent (Claude, requires `ANTHROPIC_API_KEY`) with tools for everything the database can do: table and document CRUD, all three search types, sandboxed JavaScript (`execute_code` with a `db` API), stored functions, UI page management, skills, persistent memories, background agents, and tool discovery.

- **Background agents** run on timer schedules with the same toolset (`manage_agent`, or `wasmdb agent create`). The built-in `ui-builder` runs daily and after new pages are scaffolded.
- **External MCP servers** can be registered (`manage_mcp_server`, streamable-HTTP or stdio) to extend the agent with outside tools.
- **Sub-agents** handle isolated side tasks without polluting the main context.

## API

Everything requires session auth except health checks and login. Authenticate with `POST /v1/auth/login` (`{"email", "password"}`) and pass the token as a `wasmdb_session` cookie or `Authorization: Bearer` header; sessions last 7 days. The first user is seeded from `WASMDB_SEED_USER_EMAIL`/`_PASSWORD` when the users table is empty.

```
POST/GET       /v1/tables                            Create / list tables
GET/DELETE     /v1/tables/{t}                        Table info + schema / delete
PUT            /v1/tables/{t}/schema                 Update schema
POST/GET       /v1/tables/{t}/documents              Create / list documents
GET/PUT/DELETE /v1/tables/{t}/documents/{id}         Document by ID
POST           /v1/tables/{t}/documents/_bulk        Bulk create
POST           /v1/tables/{t}/search/text            BM25 full-text search
POST           /v1/tables/{t}/search/vector          Vector similarity search
POST           /v1/tables/{t}/search/attributes      Attribute filters (eq, neq, gt/gte, lt/lte, contains, prefix)
POST           /v1/graphql                           GraphQL over the same tables
POST           /v1/chat                              Streaming chat (SSE)
POST/GET       /v1/ui/pages                          Create / list UI pages
GET/PATCH/DELETE /v1/ui/pages/{name}                 Page by name (PATCH = partial update)
POST           /v1/ui/pages/{name}/render            Server-side render → {surface, data}
POST           /v1/ui/pages/{name}/actions/{action}  Execute a declared page action
POST/GET       /v1/users                             User management (GET/DELETE /{id})
GET            /healthz · /readyz                    Probes (unauthenticated)
```

Example — create a document and search it:

```bash
curl -s -X POST localhost:8080/v1/tables/issues/documents \
  -H "Authorization: Bearer $TOKEN" -d '{
    "content": "Login page returns 500 when password field is empty.",
    "attributes": {"title": "Login crash on empty password", "status": "open", "open": true}
  }'

curl -s -X POST localhost:8080/v1/tables/issues/search/text \
  -H "Authorization: Bearer $TOKEN" -d '{"query": "login crash", "limit": 10}'
```

## CLI

```bash
go build -o wasmdb-cli ./cmd/wasmdb-cli
wasmdb-cli login --url http://localhost:8080          # browser flow; add --email/--password for headless
wasmdb-cli db list                                    # tables
wasmdb-cli doc create issues --attr title="Fix login" --attr status=open
wasmdb-cli search text issues "login"
wasmdb-cli exec --code 'function handler() { return db.tables() }'
wasmdb-cli ui render tbl-issues                       # server-render a page, print the data
wasmdb-cli agent trigger ui-builder                   # run the UI polish agent now
wasmdb-cli chat                                       # interactive chat in the terminal
```

Covers tables, documents, search, stored functions, ephemeral JS, UI pages, agents, MCP servers, users, and raw API access (`wasmdb-cli api /v1/tables`). Add `--json` to any command. Config lives at `~/.config/wasmdb/config.json` (`wasmdb-cli config set url https://...`).

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                           HTTP API                          │
│    REST · GraphQL · Chat (SSE) · UI pages · Auth · Health   │
├──────────────┬───────────────────────────┬──────────────────┤
│  Chat agent  │       Table registry      │   UI pipeline    │
│  bg agents   │  schemas · system tables  │ scaffold, render │
│ skills, MCP  │                           │ actions, assets  │
├──────────────┴──────┬────────────────────┴──────────────────┤
│   Derived indexes   │ QuickJS sandbox (query_js, functions) │
│ Bleve · HNSW · attr │                                       │
├─────────────────────┴───────────────────────────────────────┤
│              moraine — LSM over object storage              │
│     MemTable · WAL · SSTables · compaction · disk cache     │
├─────────────────────────────────────────────────────────────┤
│             S3 / Tigris / MinIO / R2 / in-memory            │
└─────────────────────────────────────────────────────────────┘
```

**Storage** — [moraine](https://github.com/russellhaering/moraine), inspired by [SlateDB](https://slatedb.io/): single writer per table with epoch-based fencing via conditional puts, MemTable flushing to WAL/SSTables in object storage, tiered compaction, CAS-updated manifest, local LRU disk cache for reads.

**Consistency** — document CRUD is strongly consistent (synchronous WAL flush, reads consult the MemTable). Derived indexes — full-text, vector, attribute — are rebuilt asynchronously by tailing LSM sequence numbers and are eventually consistent.

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|---|---|---|
| `WASMDB_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `WASMDB_S3_BUCKET` | *(empty)* | Bucket name; empty → in-memory store |
| `WASMDB_S3_REGION` | `us-east-1` | Region |
| `WASMDB_S3_ENDPOINT` | *(empty)* | Custom S3 endpoint (Tigris, MinIO, R2, LocalStack) |
| `WASMDB_S3_PREFIX` | `wasmdb` | Key prefix within the bucket |
| `WASMDB_CACHE_DIR` | `/tmp/wasmdb-cache` | Local disk cache directory |
| `WASMDB_CACHE_MAX_SIZE` | `1073741824` (1 GB) | Max disk cache bytes |
| `WASMDB_MEMTABLE_MAX_SIZE` | `67108864` (64 MB) | MemTable size before flush |
| `WASMDB_L0_COMPACT_THRESHOLD` | `4` | L0 SSTables before compaction |
| `WASMDB_WAL_FLUSH_INTERVAL` | `1s` | Periodic WAL flush interval |
| `ANTHROPIC_API_KEY` | *(empty)* | Enables the chat agent and background agents |
| `OPENAI_API_KEY` | *(empty)* | Enables vector embeddings |
| `WASMDB_CHAT_MODEL` | *(empty)* | Chat model override (default: Claude Sonnet 4.5) |
| `WASMDB_SUBAGENT_MODEL` | *(empty)* | Model for delegated sub-agents |
| `WASMDB_SEED_USER_EMAIL` / `_PASSWORD` | *(empty)* | Bootstrap user (first run only) |

## Deployment

Configured for [Fly.io](https://fly.io) with [Tigris](https://www.tigrisdata.com/) object storage (`fly.toml`):

```bash
fly deploy
fly secrets set WASMDB_SEED_USER_EMAIL=admin@example.com \
                WASMDB_SEED_USER_PASSWORD=your-password \
                ANTHROPIC_API_KEY=sk-...
```

Or Docker:

```bash
docker build -f deploy/Dockerfile -t wasmdb .
docker run -p 8080:8080 \
  -e WASMDB_S3_BUCKET=my-bucket -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  -e WASMDB_SEED_USER_EMAIL=admin@example.com -e WASMDB_SEED_USER_PASSWORD=changeme \
  wasmdb
```

## Project structure

```
cmd/wasmdb                 Server entry point        cmd/wasmdb-cli   CLI
internal/
  api/                     HTTP server, routes, handlers
    webui/                 Embedded frontend (surface.js renderer, dashboard, chat)
    graphqlapi/            GraphQL schema and resolvers
  agent/                   Chat manager, tool server (23 tools), system prompt
  agents/                  Background agent scheduler, builtin ui-builder
  surface/                 UI format: typed components, actions, validation, generated LLM spec
  uiconfig/                UI page store, server-side render, action executor
  uigen/                   Deterministic schema→page scaffold generator + triggers
  functions/               QuickJS sandbox, stored functions, JS db API
  database/                Table registry, system tables (on moraine)
  auth/ · config/          Sessions and login · env configuration
  skills/ · memory/        Agent skills · persistent memories
  mcpservers/ · autobot/   External MCP registry · agent runtime
deploy/Dockerfile          Multi-stage build
```

Storage internals (LSM, SSTables, WAL, compaction, object stores, document serialization, index plumbing) live in [moraine](https://github.com/russellhaering/moraine).

## Status

A nights-and-weekends project, built almost entirely by AI agents — including the review and rebuild of its own UI system. It has a real test suite (~11k lines across wasmdb and moraine) and runs in production for its author, but it is young: expect sharp edges, and don't bet your company on it yet. Issues and ideas welcome.

## Testing

```bash
go test ./...
```

## License

Apache License 2.0. See [LICENSE](LICENSE).

# WasmDB

A document-oriented database with a custom LSM-tree storage engine built on object storage (S3). Provides REST, GraphQL, and chat APIs with strong read-after-write consistency for CRUD, and eventually consistent full-text search, vector search, and attribute filtering.

## Architecture

```
┌────────────────────────────────────────────────────────────┐
│                        HTTP API                            │
│  REST · GraphQL · Chat · Auth · Users · Health             │
├────────────────────────────────────────────────────────────┤
│                    Table Registry                          │
│             (multi-table orchestration)                    │
├──────────┬───────────────┬────────────────────────────────┤
│ Indexes  │   Embedding   │       LSM Storage Engine       │
│ Bleve    │   OpenAI      │  MemTable (skip-list)          │
│ HNSW     │   Pipeline    │  SSTable (blocks+bloom)        │
│ Attribute│               │  WAL · Manifest · Compaction   │
├──────────┴───────────────┼────────────────────────────────┤
│     Local Disk Cache     │     Object Storage (S3)        │
│    (LRU block + SST)    │                                 │
└──────────────────────────┴────────────────────────────────┘
```

**Storage engine** -- Inspired by [SlateDB](https://slatedb.io/). Single-writer per table with epoch-based fencing via conditional puts. MemTable flushes to WAL (sequential SSTables in S3), tiered compaction merges L0 into sorted runs. Manifest uses CAS updates for consistency.

**Consistency model** -- CRUD operations (get, put, delete) are strongly consistent: writes flush synchronously to WAL, reads check the active MemTable first. Search indexes (full-text, vector, attribute) are eventually consistent, rebuilt asynchronously from the LSM by tailing sequence numbers.

## Quick Start

### In-memory mode (no dependencies)

```bash
go build -o wasmdb ./cmd/wasmdb
export WASMDB_SEED_USER_EMAIL=admin@example.com
export WASMDB_SEED_USER_PASSWORD=changeme
./wasmdb
```

Without `WASMDB_S3_BUCKET` set, the server starts with an in-memory object store. Data does not persist across restarts.

### With S3

```bash
export WASMDB_S3_BUCKET=my-bucket
export WASMDB_S3_REGION=us-west-2
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
export WASMDB_SEED_USER_EMAIL=admin@example.com
export WASMDB_SEED_USER_PASSWORD=changeme
./wasmdb
```

### Docker

```bash
docker build -f deploy/Dockerfile -t wasmdb .
docker run -p 8080:8080 \
  -e WASMDB_SEED_USER_EMAIL=admin@example.com \
  -e WASMDB_SEED_USER_PASSWORD=changeme \
  wasmdb
```

## Authentication

All API endpoints require authentication except `/healthz`, `/readyz`, `/v1/auth/login`, and `/auth/cli-login`.

### Login

```bash
# Obtain a session token
curl -s -X POST http://localhost:8080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email": "admin@example.com", "password": "changeme"}'
```

The response sets a `wasmdb_session` cookie and returns the token in the body. Subsequent requests can authenticate with either:

- **Cookie:** `wasmdb_session` (set automatically by the login response)
- **Header:** `Authorization: Bearer <session-token>`

Sessions expire after 7 days.

### Seed User

The first user is bootstrapped via environment variables. A user is created on startup only if the `_users` table is empty; once any user exists the seed is a no-op.

```bash
export WASMDB_SEED_USER_EMAIL=admin@example.com
export WASMDB_SEED_USER_PASSWORD=your-password
```

### CLI Login

```bash
wasmdb login --url http://localhost:8080
```

Opens a browser for interactive login. For headless environments:

```bash
wasmdb login --url http://localhost:8080 --email admin@example.com --password changeme
```

Credentials are stored at `~/.config/wasmdb/credentials.json`.

### Auth Endpoints

```
POST  /v1/auth/login     # Authenticate, returns token + sets cookie
POST  /v1/auth/logout    # Invalidate session, clear cookie
GET   /v1/auth/me        # Return current user info
GET   /auth/cli-login    # HTML login page for CLI browser flow
```

## REST API

All resource endpoints are prefixed with `/v1` and require authentication.

### Tables

```
POST   /v1/tables              # Create a table
GET    /v1/tables              # List tables
GET    /v1/tables/{table}      # Get table info
DELETE /v1/tables/{table}      # Delete a table
```

### Schema

Each table has an optional schema defining typed attribute fields.

```
GET    /v1/tables/{table}/schema    # Get schema
PUT    /v1/tables/{table}/schema    # Update schema
```

Field types: `string`, `int`, `float`, `bool`, `[]string`, `[]int`, `[]float`, `datetime`, `reference`.

### Documents

```
POST   /v1/tables/{table}/documents          # Create document
GET    /v1/tables/{table}/documents          # List documents (paginated)
GET    /v1/tables/{table}/documents/{id}     # Get document
PUT    /v1/tables/{table}/documents/{id}     # Update document
DELETE /v1/tables/{table}/documents/{id}     # Delete document
POST   /v1/tables/{table}/documents/_bulk    # Bulk create
```

Documents have optional Markdown content and typed key/value attributes.

### Search

```
POST   /v1/tables/{table}/search/text        # Full-text search (BM25)
POST   /v1/tables/{table}/search/vector      # Vector similarity search
POST   /v1/tables/{table}/search/attributes  # Attribute filtering
```

### Users

```
POST   /v1/users          # Create user
GET    /v1/users           # List users
GET    /v1/users/{id}      # Get user
DELETE /v1/users/{id}      # Delete user
```

### Health

```
GET    /healthz    # Liveness probe (no auth required)
GET    /readyz     # Readiness probe (no auth required)
```

## GraphQL API

```
POST   /v1/graphql    # GraphQL endpoint
```

## Chat

WasmDB includes a built-in chat interface powered by Claude that can query and interact with your data.

```
GET    /chat         # Chat web UI (no auth required)
POST   /v1/chat      # Streaming chat endpoint
```

Requires `ANTHROPIC_API_KEY` to be set.

## Web UI

WasmDB auto-generates an interactive, data-aware web UI. A deterministic pure-Go
generator emits a working CRUD "scaffold" page for every table — a live data
table, a schema-derived create form, edit/delete/search actions — with no LLM or
API key required, so the UI is populated as soon as you create a table. Pages are
regenerated on startup, on schema change, and on first write. When an
`ANTHROPIC_API_KEY` is configured, the chat agent and a background `ui-builder`
agent can refine these pages, and chat can embed a live view of any stored page.

```
GET    /ui                              # Dashboard (page list + live render)
GET    /ui/assets/*                     # Embedded renderer (surface.js, CSS)
GET    /v1/ui/pages                      # List pages
POST   /v1/ui/pages/{name}/render        # Server-side render → {surface, data}
POST   /v1/ui/pages/{name}/actions/{a}   # Execute a declared insert/update/delete/query action
```

## Usage Examples

All examples below assume you have a session token. Pass it as a header:

```bash
TOKEN="your-session-token"
AUTH="-H 'Authorization: Bearer $TOKEN'"
```

Or use the cookie set by login (e.g. with `curl -b cookies.txt`).

Create a table:

```bash
curl -s -X POST http://localhost:8080/v1/tables \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name": "issues"}'
```

Set a schema:

```bash
curl -s -X PUT http://localhost:8080/v1/tables/issues/schema \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "fields": [
      {"name": "title", "type": "string", "required": true, "full_text": true},
      {"name": "status", "type": "string", "indexed": true},
      {"name": "labels", "type": "[]string", "indexed": true}
    ]
  }'
```

Create a document:

```bash
curl -s -X POST http://localhost:8080/v1/tables/issues/documents \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "content": "Login page returns 500 when password field is empty.",
    "attributes": {
      "title": "Login crash on empty password",
      "status": "open",
      "labels": ["bug", "auth"]
    }
  }'
```

Full-text search:

```bash
curl -s -X POST http://localhost:8080/v1/tables/issues/search/text \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"query": "login crash", "limit": 10}'
```

Attribute search:

```bash
curl -s -X POST http://localhost:8080/v1/tables/issues/search/attributes \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "filters": [
      {"field": "status", "op": "eq", "value": "open"},
      {"field": "labels", "op": "contains", "value": "bug"}
    ],
    "limit": 10
  }'
```

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|---|---|---|
| `WASMDB_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `WASMDB_S3_BUCKET` | *(empty)* | S3 bucket name; if empty, uses in-memory store |
| `WASMDB_S3_REGION` | `us-east-1` | AWS region |
| `WASMDB_S3_ENDPOINT` | *(empty)* | Custom S3 endpoint (MinIO, Tigris, LocalStack, etc.) |
| `WASMDB_S3_PREFIX` | `wasmdb` | Key prefix in the S3 bucket |
| `WASMDB_CACHE_DIR` | `/tmp/wasmdb-cache` | Local disk cache directory |
| `WASMDB_CACHE_MAX_SIZE` | `1073741824` (1 GB) | Max disk cache size in bytes |
| `WASMDB_MEMTABLE_MAX_SIZE` | `67108864` (64 MB) | MemTable size before flush |
| `WASMDB_L0_COMPACT_THRESHOLD` | `4` | L0 SSTables before compaction triggers |
| `WASMDB_WAL_FLUSH_INTERVAL` | `1s` | Periodic WAL flush interval |
| `OPENAI_API_KEY` | *(empty)* | Enables vector embeddings via OpenAI |
| `ANTHROPIC_API_KEY` | *(empty)* | Enables chat agent (`/chat`, `/v1/chat`) |
| `WASMDB_CHAT_MODEL` | *(empty)* | Optional main chat model override (defaults to Sonnet 4.5) |
| `WASMDB_SUBAGENT_MODEL` | *(empty)* | Optional default model for `delegate_subagent` tool |
| `WASMDB_SEED_USER_EMAIL` | *(empty)* | Bootstrap user email (first run only) |
| `WASMDB_SEED_USER_PASSWORD` | *(empty)* | Bootstrap user password (first run only) |

## Deployment

### Fly.io

WasmDB is configured for deployment on [Fly.io](https://fly.io) with [Tigris](https://www.tigrisdata.com/) object storage. Deployment config is in `fly.toml`.

```bash
fly deploy
```

Set secrets:

```bash
fly secrets set WASMDB_SEED_USER_EMAIL=admin@example.com
fly secrets set WASMDB_SEED_USER_PASSWORD=your-password
fly secrets set ANTHROPIC_API_KEY=sk-...
```

### Docker

```bash
docker build -f deploy/Dockerfile -t wasmdb .
docker run -p 8080:8080 \
  -e WASMDB_S3_BUCKET=my-bucket \
  -e AWS_ACCESS_KEY_ID=... \
  -e AWS_SECRET_ACCESS_KEY=... \
  -e WASMDB_SEED_USER_EMAIL=admin@example.com \
  -e WASMDB_SEED_USER_PASSWORD=changeme \
  wasmdb
```

## Testing

```bash
go test ./...
```

## Project Structure

```
cmd/wasmdb/main.go                 Entry point
internal/
  config/                          Environment-based configuration
  document/                        Document type, schema, binary serialization
  storage/
    objstore/                      ObjectStore interface, S3 + memory backends
    cache/                         LRU block cache, disk SSTable cache
    lsm/                           LSM engine: memtable, sstable, wal, manifest,
                                   writer, reader, compaction, db
  index/                           Bleve FTS, HNSW vector, attribute filtering,
                                   async builder
  embedding/                       Embedder interface, OpenAI, batching pipeline
  database/                        Table orchestration, multi-table registry
  surface/                         UI surface format v2: typed components, actions, validation
  uiconfig/                        UI page store, server-side render + action executor
  uigen/                           Deterministic schema-to-page scaffold generator
  api/                             HTTP server, routes, handlers
    webui/                         Embedded browser assets (surface.js renderer, dashboard, chat)
    graphqlapi/                    GraphQL schema and resolvers
  auth/                            Session management, login, seed user
deploy/
  Dockerfile                       Multi-stage build
```

## License

Apache License 2.0. See [LICENSE](LICENSE).

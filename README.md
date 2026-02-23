# WasmDB

A document-oriented database with an LSM-tree storage engine built on object storage (S3). Provides a REST API for document CRUD with strong read-after-write consistency, and eventually consistent full-text search, vector search, and attribute filtering.

## Architecture

```
┌──────────────────────────────────────────────────────┐
│                     REST API                         │
│  databases · schemas · documents · search · health   │
├──────────────────────────────────────────────────────┤
│                 Database Registry                     │
│           (multi-database orchestration)              │
├──────────┬───────────────┬───────────────────────────┤
│ Indexes  │   Embedding   │      LSM Storage Engine   │
│ Bleve    │   OpenAI      │  MemTable (skip-list)     │
│ HNSW     │   Pipeline    │  SSTable (blocks+bloom)   │
│ Attribute│               │  WAL · Manifest · Compact │
├──────────┴───────────────┼───────────────────────────┤
│     Local Disk Cache     │   Object Storage (S3)     │
│    (LRU block + SST)    │                            │
└──────────────────────────┴───────────────────────────┘
```

**Storage engine** — Inspired by [SlateDB](https://slatedb.io/). Single-writer per database with epoch-based fencing via conditional puts. MemTable flushes to WAL (sequential SSTables in S3), tiered compaction merges L0 into sorted runs. Manifest uses CAS updates for consistency.

**Consistency model** — CRUD operations (get, put, delete) are strongly consistent: writes flush synchronously to WAL, reads check the active MemTable first. Search indexes (full-text, vector, attribute) are eventually consistent, rebuilt asynchronously from the LSM by tailing sequence numbers.

## Quick Start

### In-memory mode (no dependencies)

```bash
go build -o wasmdb ./cmd/wasmdb
./wasmdb
```

Without `WASMDB_S3_BUCKET` set, the server starts with an in-memory object store. Data does not persist across restarts.

### With S3

```bash
export WASMDB_S3_BUCKET=my-bucket
export WASMDB_S3_REGION=us-west-2
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
./wasmdb
```

### Docker

```bash
docker build -f deploy/Dockerfile -t wasmdb .
docker run -p 8080:8080 wasmdb
```

## API

All endpoints are prefixed with `/v1`.

### Databases

```
POST   /v1/databases                  # Create a database
GET    /v1/databases                  # List databases
GET    /v1/databases/{db}             # Get database info
DELETE /v1/databases/{db}             # Delete a database
```

### Schema

Each database has an optional schema defining typed attribute fields.

```
GET    /v1/databases/{db}/schema      # Get schema
PUT    /v1/databases/{db}/schema      # Update schema
```

Field types: `string`, `int`, `float`, `bool`, `[]string`, `[]int`, `[]float`, `datetime`, `reference`.

### Documents

```
POST   /v1/databases/{db}/documents          # Create document
GET    /v1/databases/{db}/documents/{id}     # Get document
PUT    /v1/databases/{db}/documents/{id}     # Update document
DELETE /v1/databases/{db}/documents/{id}     # Delete document
```

Documents have optional Markdown content and typed key/value attributes.

### Search

```
POST   /v1/databases/{db}/search/text        # Full-text search (BM25)
POST   /v1/databases/{db}/search/vector      # Vector similarity search
POST   /v1/databases/{db}/search/attributes  # Attribute filtering
```

### Health

```
GET    /healthz    # Liveness probe
GET    /readyz     # Readiness probe
```

## Usage Examples

Create a database:

```bash
curl -s -X POST http://localhost:8080/v1/databases \
  -H 'Content-Type: application/json' \
  -d '{"name": "issues"}'
```

Set a schema:

```bash
curl -s -X PUT http://localhost:8080/v1/databases/issues/schema \
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
curl -s -X POST http://localhost:8080/v1/databases/issues/documents \
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
curl -s -X POST http://localhost:8080/v1/databases/issues/search/text \
  -H 'Content-Type: application/json' \
  -d '{"query": "login crash", "limit": 10}'
```

Attribute search:

```bash
curl -s -X POST http://localhost:8080/v1/databases/issues/search/attributes \
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
| `WASMDB_S3_BUCKET` | *(empty)* | S3 bucket name. If empty, uses in-memory store |
| `WASMDB_S3_REGION` | `us-east-1` | AWS region |
| `WASMDB_S3_ENDPOINT` | *(empty)* | Custom S3 endpoint (for MinIO, LocalStack, etc.) |
| `WASMDB_S3_PREFIX` | `wasmdb` | Key prefix in the S3 bucket |
| `WASMDB_CACHE_DIR` | `/tmp/wasmdb-cache` | Local disk cache directory |
| `WASMDB_CACHE_MAX_SIZE` | `1073741824` (1 GB) | Max disk cache size in bytes |
| `WASMDB_MEMTABLE_MAX_SIZE` | `67108864` (64 MB) | MemTable size before flush |
| `WASMDB_L0_COMPACT_THRESHOLD` | `4` | L0 SSTables before compaction triggers |
| `WASMDB_WAL_FLUSH_INTERVAL` | `1s` | Periodic WAL flush interval |
| `OPENAI_API_KEY` | *(empty)* | Enables vector embeddings via OpenAI |

## Kubernetes Deployment

Manifests are in `deploy/kubernetes/`.

```bash
kubectl create namespace wasmdb

# Create secrets for AWS credentials
kubectl -n wasmdb create secret generic wasmdb-secrets \
  --from-literal=AWS_ACCESS_KEY_ID=... \
  --from-literal=AWS_SECRET_ACCESS_KEY=...

# Apply manifests
kubectl -n wasmdb apply -f deploy/kubernetes/
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
  database/                        Database orchestration, multi-database registry
  api/                             HTTP server, routes, handlers
deploy/
  Dockerfile                       Multi-stage build
  kubernetes/                      Deployment, Service, ConfigMap
```

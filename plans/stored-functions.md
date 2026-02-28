# Stored Functions

## Overview

Add the ability to store and execute JavaScript functions in WasmDB. Functions run in a Wasm sandbox (QuickJS compiled to Wasm, executed via wazero) with access to a `db` host API for database operations. Functions can be **stored** (persisted to a system table, invoked by name) or **ephemeral** (executed on-demand without persisting). The chat agent can write and execute code in a single turn.

Inspired by Cloudflare Workers: simple function signatures, environment bindings (our `db` API), fast cold starts.

## Runtime

**Library:** `github.com/fastschema/qjs` (v0.x, 526 stars)
- QuickJS-ng compiled to Wasm, runs inside wazero
- Pure Go, no cgo — compatible with `CGO_ENABLED=0` build
- ES2023 support (async/await, optional chaining, nullish coalescing, BigInt)
- Go↔JS interop: expose Go functions to JS via `ctx.SetFunc()`
- ~1.5x faster than Goja on benchmarks
- Bytecode precompilation for stored functions (fast re-execution)
- Runtime pooling for concurrent requests

**Sandboxing:** Code runs inside QuickJS→Wasm→wazero. No filesystem, no network (except what we expose via host functions), no cgo. Memory and CPU limits configurable via wazero.

## Storage

Functions are stored in the `_functions` system table:

```json
{
  "id": "ulid",
  "attributes": {
    "name": "enrich-contacts",
    "description": "Enriches contact records with computed fields",
    "created_by": "user-id",
    "updated_at": "2024-01-01T00:00:00Z"
  },
  "content": "function handler(params) {\n  const contacts = db.table('contacts').list(100);\n  ...\n}"
}
```

The `name` field is unique and used for invocation. Source code lives in `content`.

## Function Signature

Every function must export or define a `handler` function:

```javascript
// Stored function: enrich-contacts
function handler(params) {
  const contacts = db.table("contacts").list(100);
  let updated = 0;

  for (const doc of contacts) {
    if (!doc.attributes.full_name && doc.attributes.first_name) {
      db.table("contacts").put({
        id: doc.id,
        attributes: {
          ...doc.attributes,
          full_name: doc.attributes.first_name + " " + (doc.attributes.last_name || "")
        }
      });
      updated++;
    }
  }

  return { updated };
}
```

- `params` — arbitrary JSON object passed by the caller
- Return value — any JSON-serializable value, sent back as the response
- `db` — host-provided global object for database access
- `console.log()` — captured and returned in response metadata

For **ephemeral** execution, the code can also be a bare expression or script:

```javascript
// Ephemeral: just run this code, return the last expression
const docs = db.table("contacts").list(10);
docs.map(d => ({ id: d.id, name: d.attributes.name }));
```

## Host API (`db` global)

### Tables

```javascript
db.tables()                              // → [{name, schema}]
db.createTable(name)                     // → {name}
db.createTable(name, {schema})           // → {name, schema}
db.deleteTable(name)                     // → true
```

### Documents

```javascript
const t = db.table("contacts");

t.get(id)                                // → {id, content, attributes, version, ...}
t.list(limit?, afterKey?)                // → [{id, content, attributes, ...}]
t.put({id?, content?, attributes?})      // → {id, version} (upsert)
t.delete(id)                             // → true
```

### Search

```javascript
t.search.text(query, limit?, offset?)    // → [{id, content, attributes, ...}]
t.search.attr(filters, limit?, offset?)  // → [{id, content, attributes, ...}]
// filters: [{field, op, value}] where op is "eq", "gt", "lt", "gte", "lte", "prefix"
```

### Schema

```javascript
t.schema()                               // → {fields: [...]}
t.setSchema({fields: [...]})             // → {fields: [...]}
```

All `db` methods are **synchronous** from the JS perspective (they call Go host functions that block).

## REST API

### Stored Functions CRUD

| Method | Path | Description |
|--------|------|-------------|
| `POST /v1/functions` | Create | `{name, description?, code}` → `{name, id, created_at}` |
| `GET /v1/functions` | List | → `[{name, description, id, updated_at}]` |
| `GET /v1/functions/{name}` | Get | → `{name, description, code, id, ...}` |
| `PUT /v1/functions/{name}` | Update | `{code, description?}` → `{name, id, updated_at}` |
| `DELETE /v1/functions/{name}` | Delete | → 204 |

### Execution

| Method | Path | Description |
|--------|------|-------------|
| `POST /v1/functions/{name}/exec` | Execute stored | `{params?}` → `{result, logs, duration_ms}` |
| `POST /v1/exec` | Execute ephemeral | `{code, params?}` → `{result, logs, duration_ms}` |

Execution response shape:

```json
{
  "result": {"updated": 5},
  "logs": ["processing 10 contacts...", "done"],
  "duration_ms": 42,
  "error": null
}
```

On error:

```json
{
  "result": null,
  "logs": ["processing..."],
  "duration_ms": 15,
  "error": "TypeError: Cannot read property 'name' of undefined at line 5"
}
```

## Agent Integration

Two new tools for the chat agent:

### `execute_code`

Execute JavaScript code on-demand. For data transformations, bulk updates, analytics, or any operation that benefits from programmatic logic.

```json
{
  "name": "execute_code",
  "input_schema": {
    "type": "object",
    "properties": {
      "code": {"type": "string", "description": "JavaScript source code to execute"},
      "params": {"type": "object", "description": "Parameters available as `params` in the code"}
    },
    "required": ["code"]
  }
}
```

The agent uses this when a user asks for something that requires logic: "update all contacts missing a full_name field", "calculate average deal size by stage", "find duplicate records".

### `manage_function`

Create, update, list, get, or delete stored functions.

```json
{
  "name": "manage_function",
  "input_schema": {
    "type": "object",
    "properties": {
      "action": {"type": "string", "enum": ["create", "update", "get", "list", "delete"]},
      "name": {"type": "string"},
      "code": {"type": "string"},
      "description": {"type": "string"}
    },
    "required": ["action"]
  }
}
```

### System Prompt Addition

Add to the agent's system prompt:

```
You can execute JavaScript code using the execute_code tool. The code runs in a
sandboxed environment with access to a `db` global object for database operations.

Available db methods:
- db.tables() — list all tables
- db.table(name).list(limit?) — list documents
- db.table(name).get(id) — get a document by ID
- db.table(name).put({id?, content?, attributes?}) — create or update a document
- db.table(name).delete(id) — delete a document
- db.table(name).search.text(query, limit?) — full-text search
- db.table(name).search.attr(filters, limit?) — attribute search

Use execute_code for:
- Bulk operations (update many documents)
- Data transformations and enrichment
- Analytics and aggregations
- Finding duplicates or anomalies
- Any logic that's easier to express in code than as individual API calls

Use manage_function to save frequently-needed code as named stored functions.
```

## Execution Engine

### `internal/functions/engine.go`

Core execution engine wrapping QJS:

```go
type Engine struct {
    registry *database.Registry
}

type ExecResult struct {
    Result     any      `json:"result"`
    Logs       []string `json:"logs"`
    DurationMS int64    `json:"duration_ms"`
    Error      string   `json:"error,omitempty"`
}

func (e *Engine) Execute(ctx context.Context, code string, params map[string]any) *ExecResult
```

Per-execution:
1. Create a new QJS runtime (or pool — benchmark first)
2. Inject `db` global with host function bindings
3. Inject `params` global
4. Capture `console.log` calls into `logs` slice
5. Evaluate the code with a timeout (default 30s, configurable)
6. Marshal return value to JSON
7. Tear down runtime

### `internal/functions/hostapi.go`

Implements the `db` host API by binding Go functions to the QJS context:

```go
func (e *Engine) bindDBAPI(ctx context.Context, jsCtx *qjs.Context) {
    // db.tables()
    // db.createTable(name, schema?)
    // db.deleteTable(name)
    // db.table(name) → returns proxy object with .get(), .list(), .put(), .delete(), .search.text(), .search.attr()
}
```

The tricky part is `db.table(name)` returning an object with methods. Options:
- Return a JS object with Go-bound methods (preferred — QJS supports this via property injection)
- Use a flat API like `db.get(table, id)` (simpler but less ergonomic)

### `internal/functions/store.go`

CRUD operations on the `_functions` system table:

```go
func (s *Store) Create(ctx context.Context, name, description, code, userID string) (*Function, error)
func (s *Store) Get(ctx context.Context, name string) (*Function, error)
func (s *Store) List(ctx context.Context) ([]*Function, error)
func (s *Store) Update(ctx context.Context, name, code, description string) (*Function, error)
func (s *Store) Delete(ctx context.Context, name string) error
```

## Security & Limits

- **Timeout:** 30s default per execution (configurable via env var `WASMDB_FUNCTION_TIMEOUT`)
- **Memory:** Wazero memory limits (configurable, default 64MB)
- **No network:** No `fetch()` or HTTP from inside functions (could add later as a host function)
- **No filesystem:** Wasm sandbox has no FS access
- **System tables:** `db.table()` should block access to `_` prefixed tables (or allow read-only on some)
- **Recursion:** QuickJS has a stack size limit; wazero has a call depth limit
- **Concurrent execution:** Each execution gets its own QJS runtime — no shared mutable state

## CLI Commands

```bash
wasmdb fn create <name> --file <path> [--description <desc>] [--json]
wasmdb fn get <name> [--json]
wasmdb fn list [--json]
wasmdb fn update <name> --file <path> [--description <desc>] [--json]
wasmdb fn delete <name>
wasmdb fn exec <name> [--params '{...}'] [--json]
wasmdb exec --file <path> [--params '{...}'] [--json]     # ephemeral
wasmdb exec --code 'db.tables()' [--json]                 # inline ephemeral
```

## Implementation Steps

### Phase 1: Engine Core
1. Add `github.com/fastschema/qjs` dependency
2. Create `internal/functions/engine.go` — basic JS execution with timeout
3. Create `internal/functions/hostapi.go` — bind `db` global with table operations
4. Create `internal/functions/engine_test.go` — test execution, db API, error handling, timeouts
5. Verify `CGO_ENABLED=0` build still works

### Phase 2: REST API
6. Create `internal/functions/store.go` — CRUD for `_functions` system table
7. Create `internal/api/handler_functions.go` — REST endpoints
8. Register routes in `routes.go`
9. Add `_functions` to system table initialization
10. Create `internal/api/handler_exec.go` — ephemeral execution endpoint

### Phase 3: Agent Integration
11. Add `execute_code` tool to `internal/agent/chat.go`
12. Add `manage_function` tool
13. Update system prompt with JS execution docs
14. Test agent code execution end-to-end

### Phase 4: CLI
15. Add `fn` and `exec` commands to CLI
16. Add `ExecuteCode`, `CreateFunction`, etc. to Backend interface
17. Implement in HTTP backend

## Files to Create/Modify

| File | Action | Description |
|------|--------|-------------|
| `internal/functions/engine.go` | Create | Core JS execution engine wrapping QJS |
| `internal/functions/hostapi.go` | Create | `db` host API bindings (table ops, search) |
| `internal/functions/store.go` | Create | `_functions` system table CRUD |
| `internal/functions/engine_test.go` | Create | Engine + host API tests |
| `internal/api/handler_functions.go` | Create | REST API for function CRUD |
| `internal/api/handler_exec.go` | Create | Ephemeral + stored function execution endpoints |
| `internal/api/routes.go` | Modify | Register new routes |
| `internal/api/server.go` | Modify | Add engine to Server struct, init `_functions` table |
| `internal/agent/chat.go` | Modify | Add execute_code + manage_function tools, update system prompt |
| `internal/cli/cmd_fn.go` | Create | `fn` CLI commands |
| `internal/cli/cmd_exec.go` | Create | `exec` CLI command |
| `internal/cli/backend.go` | Modify | Add function/exec methods to Backend interface |
| `internal/cli/httpbackend/client.go` | Modify | Implement function/exec HTTP methods |
| `go.mod` / `go.sum` | Modify | Add `github.com/fastschema/qjs` dependency |

## Open Questions

1. **Runtime pooling vs fresh runtime per execution?** QJS supports pooling for perf. Start with fresh-per-execution for simplicity, benchmark, add pooling if needed.
2. **Bytecode caching for stored functions?** QJS supports precompiling JS to bytecode. Worth doing for stored functions that get called repeatedly. Skip for v1.
3. **System table access from functions?** Block entirely? Read-only for some (e.g., `_users` for user info)? Start with blocking all `_` prefixed tables.
4. **Execution concurrency limit?** Should we limit how many functions can run simultaneously? Probably yes — add a semaphore. Default 10 concurrent.
5. **Should ephemeral execution require a `handler()` wrapper?** No — for ergonomics, ephemeral code should be bare expressions/statements. The engine wraps it if no `handler` is defined.

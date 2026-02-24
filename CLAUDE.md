# CLAUDE.md

## Authentication

All API endpoints require a bearer token except `/healthz` and `/readyz`.

Set `WASMDB_API_TOKENS` to a comma-separated list of valid tokens:

```bash
export WASMDB_API_TOKENS=my-secret-token
```

Requests must include the header:

```
Authorization: Bearer my-secret-token
```

Multiple tokens are supported for rotation: `WASMDB_API_TOKENS=token1,token2`.

If `WASMDB_API_TOKENS` is unset or empty, all API requests are rejected (fail closed).

The auth middleware uses constant-time comparison and is implemented in `internal/api/server.go`.

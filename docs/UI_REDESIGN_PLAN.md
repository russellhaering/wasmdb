# UI Generation Redesign — Implementation Plan

Status: proposed. Replaces the A2UI-based UI system with a single interactive surface pipeline.

## Motivation (from the 2026-07-20 review)

The current system cannot meet the product goal (auto-generated, data-aware, *interactive* DB UI, tweakable via chat):

- The component vocabulary is display-only — no button/input/form/action exists in the schema, validator, or any renderer. No write path from a rendered page to the database; `query_js` runs with `nil` params so pages can't even be parameterized.
- Two divergent pipelines (chat inline A2UI vs. stored dashboard pages) with three duplicated renderers, two fence parsers, and two drifted prompt specs that disagree on `Text.text` vs `Text.value`.
- Data binding is regex substitution of `{{key}}` into JSON strings (`internal/uiconfig/render.go`) — verified to produce invalid JSON whenever a data value contains `"`, `\`, or a newline, plus double-expansion and injection hazards.
- `a2ui.Validate` ignores properties entirely, so the `manage_ui` self-correction loop reports `render_status: ok` on blank/broken pages.
- Auto-generation is a 24h-timer LLM agent gated on an API key; day one is always an empty dashboard.

Backwards compatibility is explicitly **not** a constraint.

## What we keep

- The `_ui_configs` storage approach (system table + store), upgraded in place.
- Server-side render: `query_js` executed in the QuickJS sandbox per render (live data).
- The `manage_ui` tool with its create/update → render → self-correct loop.
- The `ui-builder` background agent concept, re-pointed at the new format.

## What we remove

- `internal/a2ui` (types + validator), `internal/api/a2ui_splitter.go`, `internal/cli/a2ui_render.go`, the `{{key}}` template engine in `internal/uiconfig/render.go`, both inline A2UI prompt specs, and both hand-rolled browser renderers.
- Inline full-surface artifacts in chat. Chat instead **embeds stored pages by reference** (see Phase 4) — one pipeline, no fence-format drift.

---

## Target architecture

```
                    ┌─ deterministic scaffold (internal/uigen) ──┐
 schema/data ──────►│                                            ├──► _ui_configs (surface v2)
                    └─ LLM (ui-builder agent / chat manage_ui) ──┘          │
                                                                            ▼
 browser ◄── shared renderer JS (embed.FS) ◄── POST /render {params} ── render pipeline
    │                                                                (query_js + $data binding)
    └── POST /actions/{name} {params} ──► action executor ──► registry writes / param’d query
```

### 1. Surface format v2 (`internal/surface`)

Flat component list with `root`, same as today, but with a typed component registry and **strict per-type property validation**.

Component set:

| Type | Purpose | Key properties |
|---|---|---|
| `Column`, `Row` | layout | `gap`, `align` |
| `Card` | panel | `title` |
| `Divider` | separator | — |
| `Text` | text | `value` (canonical — `text` is gone), `variant` (`body`/`heading`/`caption`/`code`) |
| `Metric` | stat tile | `label`, `value`, `unit` |
| `DataTable` | rows | `columns: [{key,label,type?}]`, `rows` ($data ref), `empty_text`, `row_actions: [{action, label, confirm?}]` |
| `Form` | input group | `fields: [{name,label,type,required?,options?,default?}]`, `submit: {action,label}` |
| `Input` | standalone control | `name`, `type`, `bind` (feeds render params, e.g. a search box) |
| `Button` | trigger | `label`, `action`, `params?`, `confirm?` |

Field/input types mirror `document.FieldType`: `string`, `int`, `float`, `bool`, `datetime`, `select`.

**Data binding — no string templating.** A property value may be `{"$data": "path.to.value"}`; the renderer resolves it against the `data` object returned by `query_js`. Render responses are `{surface, data}`; substitution happens structurally client-side (and in the validator for type checks), never by editing JSON text. This deletes the entire `TemplateReplace` bug class.

**Actions are declared on the page**, separate from components:

```json
{
  "actions": {
    "create_order":  {"type": "insert", "table": "orders"},
    "update_order":  {"type": "update", "table": "orders"},
    "delete_order":  {"type": "delete", "table": "orders", "confirm": true},
    "run_report":    {"type": "query", "params": ["since"]}
  }
}
```

- `insert`/`update`/`delete` execute against the named table through the normal `Registry`/`Table` write paths (schema validation applies; system tables rejected).
- `query` re-runs the page's `query_js` with the supplied params and returns fresh `{data}` (this is also how search/filter/pagination work).
- Components may only reference declared actions; the validator enforces it. The action executor validates incoming params against the declaration before touching the DB.

**Single source of truth for the LLM spec:** `surface.SpecMarkdown()` renders the component/action documentation from the registry itself. Both the chat system prompt and the ui-builder prompt embed its output. Prompt/validator drift becomes impossible.

Validation guarantees (all missing today): required properties per type, `$data` refs point into a declared shape, action refs resolve, form field types valid, cycle/depth checks (kept from v1).

### 2. Store + render (`internal/uiconfig`, reworked)

`_ui_configs` fields (breaking change, no migration — see Rollout):

- keep: `name`, `title`, `description`, `source_tables`, `surface_json`, `query_js`, `auto_refresh_seconds`, `sort_order`, `enabled`, `created_by`, `updated_at`
- add: `actions_json` (string), `spec_version` (int, `2`), `generator` (string: `scaffold` | `agent` | `user`)

`Store.Update` becomes a **patch**: fetch existing, apply only provided fields (pointer/optional fields in the params struct). Today it silently clears anything unspecified (`store.go:191-251`) — that's a footgun for the LLM tweak loop.

Render pipeline (`Render(ctx, cfg, params)`):
1. Run `query_js` via `fnEngine.Execute(ctx, code, params)` — params now flow through (the engine already supports this; render is the only `nil` caller today, `render.go:47`).
2. Parse `surface_json` + `actions_json`, run `surface.Validate` against the data shape.
3. Return `RenderResult{Surface, Data, Actions, Logs}`. No string substitution step exists.

New `Actions` executor (same package): `Execute(ctx, cfg, actionName, params)` → dispatch on declared action type; writes go through `registry.Get(table).PutDocument/DeleteDocument`; `query` re-invokes render with params.

### 3. HTTP API (`internal/api`)

Rename for clarity (breaking OK): `/v1/ui-configs*` → `/v1/ui/pages*`.

- `GET/POST /v1/ui/pages`, `GET/PATCH/DELETE /v1/ui/pages/{name}` (PATCH = partial update, matching the store)
- `POST /v1/ui/pages/{name}/render` — body `{params?}` → `{surface, data, actions, logs}`
- `POST /v1/ui/pages/{name}/actions/{action}` — body `{params}` → `{ok, result?, data?}` (returns refreshed data for `query` actions so the client can re-render without a second call)

All authenticated by default (default-on middleware in `server.go:196-209`); follow the `handler_ui_configs.go` handler pattern. `/ui` moves to **cookie-session auth like `/chat`** — drop the localStorage-bearer-token flow in `dashboard_ui.go:294-310`.

### 4. Frontend: one renderer, embedded assets

Introduce `internal/api/webui/` with `//go:embed` (repo currently has zero asset tooling — everything is Go string constants; embed.FS is the smallest step up that makes the JS diffable and testable):

```
internal/api/webui/
  webui.go            //go:embed *.html *.js *.css; served at /ui/assets/*
  surface.js          THE renderer: component switch, $data resolution, forms,
                      action POSTs, confirm dialogs, auto-refresh, error banner
  dashboard.html/.js  page list + tabs + render loop (thin shell over surface.js)
  chat.html/.js       existing chat UI, migrated out of the string constant
  shared.css
```

`surface.js` is the only implementation of component rendering. It escapes via `textContent` (as today), renders `Form`/`Input`/`Button` as real controls, and wires them to the actions endpoint. `Input.bind` values are collected into render params and trigger a re-render (debounced) — this is how a search box works.

**Chat integration:** the a2ui splitter is deleted. When the chat agent creates/updates a page via `manage_ui`, the tool result already flows through the stream; additionally the model may emit a one-line fence:

    ```surface-ref
    {"page": "orders"}
    ```

The chat client detects this trivially (it is a complete, tiny block — no streaming fence parser needed; parse on message completion like the CLI does today) and mounts a **live embed** of the stored page using the same `surface.js` + `/render` + `/actions` calls. The tweak loop becomes: user gives feedback → agent `manage_ui action=update` → embed re-renders. Ad-hoc throwaway visualizations in chat are served by ordinary markdown tables, which models produce reliably.

**CLI:** `wasmdb ui render <name>` prints the resolved `{data}` as a table (reuse existing table formatting); the ANSI A2UI renderer is deleted. `wasmdb chat` prints `[page updated: orders — open /ui]` for `surface-ref` blocks.

### 5. Deterministic scaffold (`internal/uigen`)

A pure-Go schema→page generator so "auto-generated UI" is true on day one, with no API key and no LLM:

- For each non-system table, emit a page named `tbl-<name>`:
  - `DataTable` with columns from `Schema.Fields` (or, for schemaless tables, the union of attribute keys from a sample of up to 50 docs via `List`), typed per `FieldType`.
  - `Form` for create (fields from schema: required flags, `select` for bool, datetime inputs), `row_actions: [edit, delete]`, a search `Input` when any field has `FullText` (wired to a `query` action using `t.search.text`).
  - `query_js` that lists recent docs and accepts `{q, after}` params.
- Provenance rules: scaffold only creates/overwrites pages with `generator == "scaffold"` (or missing). Pages the agent or user has touched (`generator` ∈ `agent`,`user`) are never clobbered; `manage_ui` sets `generator: "agent"` on update, HTTP/CLI writes set `user`.

Triggers (wired in `internal/api`/`cmd`, where both `database` and the scheduler are importable without cycles):
- **Startup sweep:** generate pages for tables that lack one.
- **Schema change:** extend the existing `Registry.OnSchemaChange` closure (`server.go:103-107`, fired from `registry.go:118` etc.) to also run a debounced (~5s) scaffold sweep.
- **First data:** add a lightweight `OnFirstWrite(table)` notification in `internal/database` fired from `PutDocument`/`PutDocumentsBulk` when a table transitions empty→non-empty (cheap: check once per table per process), so a schemaless table gets columns as soon as data exists.
- Optionally, the same debounced event also calls `Scheduler.RunAgent(ctx, "ui-builder")` (`scheduler.go:96-106` — same method the HTTP trigger uses) when an API key is configured, so the LLM polish pass follows the scaffold instead of waiting up to 24h.

### 6. LLM integration

- `manage_ui` v2 (`internal/agent/dbserver.go:1166-1293`): new fields (`actions_json`), **patch semantics** on update, sets `generator: "agent"`, keeps the auto-render validation loop (now backed by a validator that actually checks properties, so `render_status: ok` means something).
- Chat system prompt (`internal/agent/chat.go`): delete both A2UI spec sections (~200 lines); insert `surface.SpecMarkdown()` once; document `surface-ref`; document that scaffold pages exist and the agent's job is to *improve* them.
- `ui-builder` prompt (`internal/agents/builtin.go`): rewrite around "review scaffold + data, enhance" rather than "author from scratch"; embed the same `SpecMarkdown()`.
- Fix the broken instruction: add a `trigger` action to `manage_agent` (`dbserver.go:949-1044` has no such case; `chat.go:212` already tells the model to use it) by calling `Scheduler.RunAgent`.

---

## Phases

Each phase compiles, passes tests, and is a reviewable commit. Order matters: format → engine → API → frontend → generation → LLM → deletion.

### Phase 1 — `internal/surface` (new package)
Component registry, typed property validation, `$data` reference resolution/checking, action declarations, `SpecMarkdown()`, cycle/depth checks.
**Tests:** table-driven per component type (valid/invalid property sets), $data path resolution incl. missing paths, action ref validation, spec-markdown golden file.
~600–800 LoC + tests. No existing code touched.

### Phase 2 — store + render + actions (`internal/uiconfig`)
New fields in `systables.go` `_ui_configs` def; `Store` patch-update; `Render(ctx, cfg, params)` with params passthrough and no templating; `ExecuteAction`. Keep the old A2UI path compiling untouched until Phase 7 (new code lives alongside).
**Tests (currently zero in this package):** store CRUD + patch semantics; render with params; data values containing `"` `\` newlines and `{{…}}` (regression suite for the old bug class); action execution incl. schema-validation failures, undeclared action, system-table rejection.

### Phase 3 — HTTP API
New `/v1/ui/pages*` routes + handlers, actions endpoint, delete old `/v1/ui-configs*` routes. `/ui` and `/chat` stay on the unauth-shell + authenticated-API pattern, both cookie-based.
**Tests:** extend the `setupTestServer` pattern (`server_test.go:24`): page CRUD, render-with-params, action roundtrip (insert via action → visible in next render), auth required.

### Phase 4 — frontend
`internal/api/webui/` with embed.FS; write `surface.js`; port dashboard shell; migrate chat HTML out of the string constant unchanged except: remove the inline-A2UI artifact handler, add `surface-ref` embeds, switch artifact rendering to `surface.js`. Delete `dashboard_ui.go` string constant; slim `chat_ui.go` to a loader.
**Tests:** Go-side: assets served, HTML references resolve. Manual: `run` the server, verify scaffold page renders, form insert works, search input re-renders, chat embed updates after a `manage_ui` update.

### Phase 5 — deterministic scaffold (`internal/uigen`)
Generator (schema→page, schemaless sampling), provenance rules, startup sweep, `OnSchemaChange` debounce, `OnFirstWrite` hook in `internal/database`, optional `RunAgent("ui-builder")` chaining.
**Tests:** golden surfaces for representative schemas (typed, schemaless, fulltext, datetime); provenance (agent-owned page not clobbered); empty→non-empty trigger.

### Phase 6 — LLM integration
`manage_ui` v2, `manage_agent action=trigger`, chat prompt rewrite on `SpecMarkdown()`, ui-builder prompt rewrite, CLI `cmd_ui.go` updates (new fields, `ui render`), CLI chat `surface-ref` handling.
**Tests:** manage_ui patch/update against a live store; prompt contains generated spec (smoke); manage_agent trigger.

### Phase 7 — deletion + docs
Delete `internal/a2ui`, `a2ui_splitter.go` (+ test), `cli/a2ui_render.go` (+ test), old render/templating code, old prompt sections, old routes. Update `CLAUDE.md` CLI reference and README. Repo-wide grep for `a2ui`/`A2UI`/`{{` templating references.
**Tests:** full `go test ./...`; end-to-end: create table → scaffold page exists → render → action insert → re-render shows the row.

## Rollout / compatibility

Breaking by design. On startup, pages with `spec_version != 2` (or unparsable under the new validator) are **disabled** (`enabled=false`), not deleted; the scaffold + ui-builder regenerate coverage immediately. Old routes and CLI flags are removed, not aliased.

## Risks & mitigations

- **`surface.js` scope creep** — it's the largest new artifact. Mitigate: strict component set (no custom JS in surfaces), server does all validation, client only renders + POSTs.
- **Action endpoint as a write surface** — mitigate: only declared actions, params validated against declaration + table schema, system tables rejected, all writes through existing `Table.PutDocument` (existing validation applies). Note: page-level ownership is still not enforced (any authenticated user can edit any page) — unchanged from today, flagged as a deliberate single-team assumption.
- **Schemaless tables** — column inference from sampled docs can be wrong/partial; scaffold marks such pages and the ui-builder pass refines them.
- **QuickJS render cost per view** — unchanged from today (30s timeout, 10-concurrent semaphore, 64MB per runtime); auto-refresh intervals should default ≥ 30s in scaffolded pages.

## Decision points (defaults chosen, flag if you disagree)

1. **Inline ad-hoc surfaces in chat are removed** in favor of stored-page embeds + markdown tables. (Alternative: keep a display-only inline mode — costs a second validation mode and keeps a fence parser.)
2. **Route rename** to `/v1/ui/pages`. (Alternative: keep `/v1/ui-configs` paths with new payloads.)
3. **Old pages disabled, not deleted**, on first v2 startup.
4. **No per-page ownership/RBAC** — retained current model.

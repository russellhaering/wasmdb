# Agent Data Display with A2UI

## Context

The chat agent currently returns plain text for all data — query results, table schemas, document details. We want the agent to render structured data as rich, interactive displays (tables and cards) using Google's A2UI component format. The entire chat UI (web + CLI) should have a terminal/TUI aesthetic — monospace fonts, box-drawing characters, terminal colors.

**User choices:**
- **A2UI generation**: Agent-generated — Claude produces A2UI JSON directly in its text output
- **TUI scope**: Full chat UI redesign — the entire chat page looks terminal-like

## Approach

The agent emits A2UI component trees as fenced code blocks (` ```a2ui `) within its text responses. Both the web UI and CLI detect these blocks and render them as rich components. No changes to the SSE transport protocol — A2UI surfaces arrive as part of `text` events and are parsed client-side.

We implement a subset of the A2UI component model plus a custom `DataTable` component (A2UI's standard catalog doesn't include a table, but allows extensions). The web chat page is redesigned with a full TUI aesthetic.

## A2UI Surface Format

Agent emits fenced blocks like:

    ```a2ui
    {
      "components": [
        {"id": "root", "type": "Column", "children": ["t1"]},
        {"id": "t1", "type": "DataTable", "properties": {
          "columns": [{"key": "id", "label": "ID"}, {"key": "name", "label": "Name"}],
          "rows": [{"id": "doc-001", "name": "My Doc"}]
        }}
      ]
    }
    ```

Or for a single record card:

    ```a2ui
    {
      "components": [
        {"id": "root", "type": "Card", "properties": {
          "title": "Document: doc-001"
        }, "children": ["f1", "f2"]},
        {"id": "f1", "type": "Text", "properties": {"label": "Name", "text": "My Doc"}},
        {"id": "f2", "type": "Text", "properties": {"label": "Status", "text": "active"}}
      ]
    }
    ```

### Supported Components

| Component | Properties | Description |
|-----------|-----------|-------------|
| `Column` | — | Vertical layout container |
| `Row` | — | Horizontal layout container |
| `DataTable` | `columns: [{key, label}]`, `rows: [object]`, `caption?` | Tabular data display |
| `Card` | `title?` | Bordered panel for a single record |
| `Text` | `text`, `label?`, `style?` ("bold", "dim", "code") | Text display, optional label prefix |
| `Divider` | — | Horizontal separator |

All components have `id` (string), `type` (string), optional `children` (array of IDs).

## Implementation Steps

### 1. A2UI Go types (`internal/a2ui/a2ui.go`)

Define Go types for validation/documentation. The agent generates JSON directly, but we define types for:
- `Surface` — top-level: `Components []Component`
- `Component` — `ID`, `Type`, `Properties map[string]any`, `Children []string`
- `DataTableProps` — `Columns []ColumnDef`, `Rows []map[string]any`, `Caption string`
- `ColumnDef` — `Key`, `Label`

Include a `Validate(Surface) error` function that checks: root component exists, all child refs resolve, no cycles. Used by tests; not in the hot path.

### 2. System prompt update (`internal/agent/chat.go`)

Update `systemPrompt` to teach Claude:
- When to use A2UI (listing documents, showing query results, displaying schemas, showing single records)
- The fenced block format (` ```a2ui `)
- The supported component types with examples
- When NOT to use A2UI (simple confirmations, error messages, conversational text)

Add 2-3 concrete examples to the prompt:
1. A `DataTable` for listing documents
2. A `Card` for showing a single document
3. A `Column` with mixed `Text` and `DataTable` for a search result with summary

### 3. Web UI TUI redesign (`internal/api/chat_ui.go`)

**CSS overhaul:**
- Monospace font throughout: `"JetBrains Mono", "Fira Code", "SF Mono", "Cascadia Code", monospace`
- Dark terminal palette: background `#0a0a0a`, text `#b0b0b0`, bright text `#e0e0e0`
- Accent colors: green `#4ec94e` (success/prompts), cyan `#5ccfe6` (links/highlights), amber `#e6b450` (warnings), red `#f07070` (errors)
- User messages styled as command-line input with `> ` prefix, no bubble
- Assistant messages as plain terminal output, no bubble
- Tool calls styled as dim terminal output with `[tool: name]` prefix
- Header: minimal, styled like a terminal title bar with `─` border characters
- Input area: styled like a terminal prompt (`$ ` prefix, single-line feel)
- Login screen: terminal-style with box-drawing border

**A2UI renderer (JavaScript):**
- Parse assistant text output, splitting on ` ```a2ui ` / ` ``` ` boundaries
- For text segments: render as-is (preserve whitespace, monospace)
- For A2UI segments: parse JSON, walk component tree from root, render HTML
- `DataTable` renderer: HTML `<table>` with box-drawing borders via CSS (`border-collapse`, monospace cells)
- `Card` renderer: box-drawing bordered `<div>` with title in top border
- `Text` renderer: `<span>` with optional label prefix and style class
- `Row` / `Column` renderer: flex container
- `Divider` renderer: `<hr>` styled as `─` characters

**Key implementation detail:** The `handleEvent` function currently appends raw text to a span. Change it to:
1. Buffer text until a complete message turn
2. On `done` event (or next tool_start), split buffered text on A2UI fences
3. Render each segment (text or A2UI surface) as a child element of the assistant message div

Alternatively (simpler, works with streaming): detect ` ```a2ui ` and ` ``` ` markers incrementally in the text stream, switching between a text accumulator and an A2UI JSON accumulator.

### 4. CLI `chat` command (`internal/cli/cmd_chat.go`)

New CLI command: `wasmdb chat`

**Flags:**
- `--session` — session ID (default: auto-generated UUID)

**Implementation:**
- REPL loop: print `> ` prompt, read line from stdin, POST to `/v1/chat` with SSE
- Parse SSE events from response body (same as web client)
- Text events: print to stdout
- Tool events: print `[tool: name] ...` / `[tool: name] done` in dim color
- A2UI blocks: detect fenced blocks, parse JSON, render as ANSI

**ANSI A2UI renderer (`internal/cli/a2ui_render.go`):**
- `DataTable`: box-drawing table with `┌─┬─┐`, `│`, `├─┼─┤`, `└─┴─┘` characters, auto-column-width
- `Card`: box-drawing bordered panel with title: `┌─ Title ─┐` / `│ key: value │` / `└──────────┘`
- `Text`: plain text, bold via ANSI `\033[1m`, dim via `\033[2m`, code via backtick-like styling
- `Row` / `Column`: simple concatenation (columns side-by-side with `│` separator, rows stacked)
- `Divider`: full-width `─` line
- Use terminal width detection (`os.Stdout` stat or `COLUMNS` env var, default 80)

### 5. Register CLI command

**`internal/cli/commands.go`** (or wherever commands are registered):
- Add `chat` command: noun="chat", verb="", handler=`runChat`

**`internal/cli/backend.go`** or equivalent:
- The CLI backend needs a method to do SSE streaming from `/v1/chat`. Add `ChatStream(ctx, sessionID, message string) (<-chan ChatEvent, error)` or similar to the `Backend` interface
- `ChatEvent` mirrors the SSE event types (text, tool_start, tool_result, done, error)

### 6. Tests

**`internal/a2ui/a2ui_test.go`:**
- Validate well-formed surfaces (DataTable, Card, nested Column)
- Reject surfaces with missing root, broken child refs, unknown component types

**`internal/cli/a2ui_render_test.go`:**
- DataTable rendering: verify box-drawing output for 2x3 table
- Card rendering: verify bordered output with title and fields
- Empty table, single row, long cell values (truncation)

**`internal/api/server_test.go`:**
- Verify `/v1/chat` SSE stream still works (existing test coverage may suffice)

## Files to Create/Modify

| File | Action | Description |
|------|--------|-------------|
| `internal/a2ui/a2ui.go` | Create | A2UI Go types + validation |
| `internal/a2ui/a2ui_test.go` | Create | Validation tests |
| `internal/agent/chat.go` | Modify | Update system prompt with A2UI instructions |
| `internal/api/chat_ui.go` | Modify | Full TUI redesign + A2UI JS renderer |
| `internal/cli/cmd_chat.go` | Create | CLI chat REPL command |
| `internal/cli/a2ui_render.go` | Create | ANSI A2UI renderer |
| `internal/cli/a2ui_render_test.go` | Create | Renderer tests |
| `internal/cli/commands.go` | Modify | Register chat command |
| `internal/cli/backend.go` | Modify | Add ChatStream to Backend interface |
| `internal/cli/backend_http.go` | Modify | Implement ChatStream with SSE parsing |

## Verification

1. `go build ./...` — compiles cleanly
2. `go test ./internal/a2ui/...` — A2UI type tests pass
3. `go test ./internal/cli/...` — CLI renderer tests pass
4. `go test ./internal/...` — all existing tests pass
5. `go vet ./...` — no issues
6. Manual: open `/chat` in browser, verify TUI aesthetic, ask agent to "list documents in [table]" and verify A2UI table renders
7. Manual: `wasmdb chat`, send same query, verify ANSI table renders in terminal

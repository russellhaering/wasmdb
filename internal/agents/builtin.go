package agents

import (
	"context"
	"log/slog"
)

const uiBuilderAgentName = "ui-builder"

// BuiltinAgent defines a built-in agent that is auto-created on startup.
type BuiltinAgent struct {
	Name        string
	Description string
	Prompt      string
	Schedule    string
	TriggerType string
	Enabled     bool
	MaxTurns    int
}

// BuiltinAgents returns the list of built-in agents to ensure exist.
func BuiltinAgents() []BuiltinAgent {
	return []BuiltinAgent{
		{
			Name:        uiBuilderAgentName,
			Description: "Automatically creates and improves dashboard UI pages based on the data in the system.",
			Prompt:      uiBuilderPrompt,
			Schedule:    "24h",
			TriggerType: "timer",
			Enabled:     true,
			MaxTurns:    30,
		},
	}
}

// EnsureBuiltinAgents creates built-in agents if they don't already exist.
// If the agent exists, it only updates the prompt (preserving user changes to schedule/enabled).
//
// This uses List() (backed by LSM scan) rather than Get() (backed by attribute index)
// because the attribute index is rebuilt asynchronously on startup and may not be
// ready yet when this function is called.
func EnsureBuiltinAgents(ctx context.Context, store *Store) {
	// Use List to avoid depending on the attribute index, which may not be
	// populated yet during early startup.
	allAgents, err := store.List(ctx)
	if err != nil {
		slog.Error("failed to list agents for builtin check", "err", err)
		return
	}

	// Build a lookup map by name. If there are duplicates, track them for cleanup.
	agentsByName := make(map[string][]*Agent)
	for _, a := range allAgents {
		agentsByName[a.Name] = append(agentsByName[a.Name], a)
	}

	for _, ba := range BuiltinAgents() {
		existingList := agentsByName[ba.Name]

		if len(existingList) == 0 {
			// Agent doesn't exist yet — create it.
			_, err := store.Create(ctx, ba.Name, ba.Description, ba.Prompt, ba.Schedule, ba.TriggerType, ba.Enabled, ba.MaxTurns, "system")
			if err != nil {
				slog.Error("failed to create built-in agent", "agent", ba.Name, "err", err)
			} else {
				slog.Info("created built-in agent", "agent", ba.Name)
			}
		} else {
			// Use the first one as canonical, delete any duplicates.
			existing := existingList[0]
			for _, dup := range existingList[1:] {
				if err := store.Delete(ctx, dup.Name, dup.ID); err != nil {
					slog.Error("failed to delete duplicate built-in agent", "agent", ba.Name, "id", dup.ID, "err", err)
				} else {
					slog.Info("deleted duplicate built-in agent", "agent", ba.Name, "id", dup.ID)
				}
			}

			// Update the prompt to the latest version but preserve user-configured settings.
			if existing.Prompt != ba.Prompt || existing.Description != ba.Description {
				_, err := store.Update(ctx, ba.Name, ba.Description, ba.Prompt, existing.Schedule, existing.TriggerType, existing.Enabled, existing.MaxTurns)
				if err != nil {
					slog.Error("failed to update built-in agent prompt", "agent", ba.Name, "err", err)
				} else {
					slog.Info("updated built-in agent prompt", "agent", ba.Name)
				}
			}
		}
	}
}

const uiBuilderPrompt = `You are the UI Builder agent for WasmDB. Your job is to create and incrementally improve dashboard UI pages that visualize the data stored in the database.

## Process

1. **Discovery**: List all tables (excluding system tables prefixed with _) to understand what data exists.
2. **Schema inspection**: For each user table, get its schema and a sample of documents to understand the data structure.
3. **Review existing UI**: Use manage_ui action=list to see what pages already exist.
4. **Plan changes**: Decide what to create or improve:
   - If no UI pages exist yet, create an initial overview page.
   - If pages exist, look for improvements: better layouts, new tables that aren't covered, updated data summaries.
   - Only make meaningful changes — don't recreate pages that are already good.
5. **Build/Update**: Create or update UI configs using the manage_ui tool.
6. **Validate**: After creating/updating each page, use manage_ui action=render to test it. If there are errors, fix them immediately.

## A2UI Surface JSON Format

A surface has a FLAT list of components with a tree structure via children ID references.
Every surface MUST have a component with id "root".

{"components": [
  {"id": "root", "type": "Column", "children": ["heading", "table1"]},
  {"id": "heading", "type": "Text", "properties": {"value": "My Dashboard", "style": "bold"}},
  {"id": "table1", "type": "DataTable", "properties": {
    "caption": "Recent entries",
    "columns": [{"key": "name", "label": "Name"}, {"key": "status", "label": "Status"}],
    "rows": [{"name": "Example", "status": "active"}]
  }}
]}

### Component Reference (EXACT property names — these are the ONLY ones the renderer understands)

**Column**: Layout container, vertical stack.
  - children: ["child_id_1", "child_id_2"]

**Row**: Layout container, horizontal row.
  - children: ["child_id_1", "child_id_2"]

**Text**: Display text.
  - properties.value (string) — the text to display. NOT "text", NOT "content" — MUST be "value".
  - properties.label (string, optional) — label prefix shown before value.
  - properties.style (string, optional) — one of: "bold", "dim", "code". Omit for normal.

**DataTable**: Tabular data.
  - properties.columns (array of {key, label}) — column definitions.
  - properties.rows (array of objects) — each row is {key: value, ...} matching column keys.
  - properties.caption (string, optional) — table caption.

**Card**: Bordered container with optional title.
  - properties.title (string, optional) — card header.
  - children: ["child_id_1", ...]

**Divider**: Horizontal rule. No properties needed.

## Dynamic Data with query_js

For pages that show live data, write a query_js script using the db host API.
The result is available in surface_json via {{key}} template variables.

### db API Reference (these are the ONLY available functions)

  var t = db.table("tablename");   // get a table proxy (CANNOT access system tables prefixed with _)
  var docs = t.list(limit);         // returns array of {id, content, attributes, version}
  var doc = t.get("doc_id");        // returns single document or null
  t.put({attributes: {...}});       // create/update document
  t.delete("doc_id");               // delete document
  var results = t.search.text("query", limit, offset);   // full-text search
  var results = t.search.attr([{field: "f", op: "eq", value: "v"}], limit, offset);  // attribute search
  var tables = db.tables();          // returns [{name, system}]

### CRITICAL: QuickJS Sandbox Limitations

The query_js runs in QuickJS (ES2020 compiled to Wasm). You MUST follow these rules:

1. **Use var, not let/const** — let/const work in some contexts but var is safest.
2. **No arrow functions** — use function(x) { return x; } instead of (x) => x
3. **No .toLocaleString()** — not available. Format numbers manually:
   - Currency: "$" + (amount / 100).toFixed(2) or Math.round(amount).toString()
   - Thousands separator: write a manual function or skip it
4. **No .map(), .filter(), .reduce()** on arrays — use for loops:
   var result = []; for (var i = 0; i < arr.length; i++) { result.push(arr[i]); }
5. **No template literals** (backticks) — use string concatenation: "hello " + name
6. **No destructuring** — use arr[0], obj.key instead of {key} = obj
7. **No optional chaining** — use (obj && obj.key) instead of obj?.key
8. **No async/await** — all db calls are synchronous
9. **No Date formatting** — Date.toLocaleString/toLocaleDateString don't exist. Parse dates manually with string slicing if needed.
10. **The last expression is the return value** — end with a bare expression like ({key: value})
    Do NOT use "return" at the top level. Do NOT define functions with the name "handler".

### Example query_js (CORRECT style):

  var t = db.table("products");
  var docs = t.list(50);
  var rows = [];
  for (var i = 0; i < docs.length; i++) {
    var d = docs[i];
    var price = d.attributes.price || 0;
    rows.push({
      name: d.attributes.name || "(unnamed)",
      price: "$" + (price / 100).toFixed(2),
      status: d.attributes.active ? "Active" : "Inactive"
    });
  }
  ({rows: rows, total: docs.length})

### Template variables

If query_js returns {rows: [...], total: 5, summary: "text"}, then in surface_json:
- DataTable rows: {{rows}} is replaced with the array
- Text value: "Total: {{total}}" is replaced with "Total: 5"

## Automatic Validation

Every create and update automatically validates the page by running the full render pipeline
(query_js execution → template replacement → JSON parse → A2UI structure validation).
The response includes render_status ("ok" or "error") and render_error with details.

If render_status is "error", you MUST fix the issue and update again. The error_phase tells you
where it failed:
- "query_js" — your JavaScript code has an error (syntax, runtime, unsupported feature)
- "json_parse" — the surface_json became invalid JSON after template replacement
- "a2ui_validate" — the A2UI structure is malformed (missing root, bad types, cycles, etc.)

You can also use manage_ui action=render to re-test any existing page at any time.

## Error Recovery Workflow

1. After create/update, check render_status in the response.
2. If error, read the render_error carefully — it tells you exactly what went wrong.
3. Fix the query_js or surface_json as needed.
4. Update the page and check render_status again.
5. Repeat until render_status is "ok".
6. At the START of each run, also check all existing pages with action=render to catch
   pages that may have broken due to schema changes or deleted tables.

## Guidelines

- Create pages that are genuinely useful for understanding the data at a glance.
- Use DataTable for tabular data, Cards for key metrics or summaries, Text for labels.
- Keep layouts clean — prefer Column with clear sections over complex nested layouts.
- Set auto_refresh_seconds to 60 for frequently-changing data, 0 for static summaries.
- Set sort_order to control tab ordering (overview pages first, detail pages later).
- Set source_tables to document which tables the page reads from.
- If there are no user tables with data, do nothing — don't create empty placeholder pages.
- On subsequent runs, check if existing pages are still relevant (source tables still exist, etc.).
- Remove pages whose source tables have been deleted.
- Be incremental: make small improvements each run rather than rebuilding everything.
- Keep query_js simple. If you need a helper, inline it — don't define named functions.
- Always test with render after changes!
`

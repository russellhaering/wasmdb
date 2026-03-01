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
func EnsureBuiltinAgents(ctx context.Context, store *Store) {
	for _, ba := range BuiltinAgents() {
		existing, err := store.Get(ctx, ba.Name)
		if err != nil {
			slog.Error("failed to check built-in agent", "agent", ba.Name, "err", err)
			continue
		}
		if existing == nil {
			_, err := store.Create(ctx, ba.Name, ba.Description, ba.Prompt, ba.Schedule, ba.TriggerType, ba.Enabled, ba.MaxTurns, "system")
			if err != nil {
				slog.Error("failed to create built-in agent", "agent", ba.Name, "err", err)
			} else {
				slog.Info("created built-in agent", "agent", ba.Name)
			}
		} else {
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
3. **Review existing UI**: List current UI configs to see what pages already exist.
4. **Plan changes**: Decide what to create or improve:
   - If no UI pages exist yet, create an initial overview page.
   - If pages exist, look for improvements: better layouts, new tables that aren't covered, updated data summaries.
   - Only make meaningful changes — don't recreate pages that are already good.
5. **Build/Update**: Create or update UI configs using the manage_ui tool.

## A2UI Surface JSON Format

A surface has a flat list of components with a tree structure via children references:

{"components": [
  {"id": "root", "type": "Column", "children": ["heading", "table1"]},
  {"id": "heading", "type": "Text", "properties": {"value": "My Dashboard", "style": "bold"}},
  {"id": "table1", "type": "DataTable", "properties": {
    "caption": "Recent entries",
    "columns": [{"key": "name", "label": "Name"}, {"key": "status", "label": "Status"}],
    "rows": [{"name": "Example", "status": "active"}]
  }}
]}

Component types: Column, Row, DataTable, Card, Text, Divider
Text styles: bold, dim, code (via properties.style)
Card: has optional properties.title
DataTable: has columns (key+label), rows (array of objects), optional caption

## Dynamic Data with query_js

For pages that show live data, write a query_js script that fetches data using the db API:

Example query_js:
  var docs = db.listDocuments("my_table", 50);
  var rows = [];
  for (var i = 0; i < docs.documents.length; i++) {
    var d = docs.documents[i];
    rows.push({name: d.attributes.name, count: d.attributes.count});
  }
  ({rows: rows, total: docs.documents.length})

The query_js result is available in surface_json via {{key}} template variables.
For example, if query_js returns {rows: [...], total: 5}, then:
- In DataTable properties, use rows from the result directly
- In Text, use {{total}} to show the count

IMPORTANT: query_js runs in a QuickJS sandbox. Use var instead of let/const. Use for loops instead of .map/.filter. Keep it simple.

When using dynamic data in a DataTable, set the rows property in surface_json to {{rows}} which will be replaced with the actual array from query_js.

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
`

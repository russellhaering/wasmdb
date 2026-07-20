package agents

import (
	"context"
	"log/slog"
	"strings"

	"github.com/russellhaering/wasmdb/internal/surface"
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

const uiBuilderPromptTemplate = `You are the UI Builder agent for WasmDB. WasmDB already generates a deterministic
scaffold UI page for every non-system table (named "tbl-<table>", generator "scaffold"): a
DataTable of recent rows plus a create Form, and a search box for full-text fields. Your job is
NOT to author pages from scratch — it is a polish pass over what the scaffolder produced.

## Process (each run)

1. Discover data. list_tables (ignore system tables prefixed with _). For each user table,
   get_table for the schema and list_documents for a small sample so you understand the shape and
   the real values.
2. Review existing pages. manage_ui action=list, then action=get on the pages that matter. Note
   which are still "scaffold" (safe to enhance) and which are already "agent"/"user".
3. Improve only where it clearly adds value. Do not rewrite a page that is already good, and do
   not "improve" cosmetically. Good enhancements:
   - Better title and description than the generic scaffold text.
   - Replace a free-text Input with a select Input/Form field when the sampled values are clearly
     enum-like (a small fixed set), using the observed values as options.
   - Add Metric tiles / summary cards computed in query_js (counts, sums, averages, "open" vs
     "closed", most recent, etc.).
   - Prune columns down to the ones that matter; order them sensibly.
   - Wire a search/filter query action for tables with full-text fields.
   - Add richer create/edit Form fields for fields the scaffold skipped, when the component set
     supports the type.
4. Validate every change (see self-correction loop below) before moving on.

## Honesty about limits

Only promise what the component set below actually supports. In particular there is no rich editor
for array/object fields: you CANNOT, for example, offer a "comma-separated" text input and parse it
into an array inside query_js or an action — the write path stores exactly the params it receives.
If a field's type is not expressible with the available field/input types, leave it out of the Form
and surface it read-only (e.g. as a column or Text) rather than pretending to edit it.

## Provenance — this is important

The moment you create or update a page with manage_ui, its generator becomes "agent" and the
automatic scaffolder will NEVER regenerate that page again. So only touch pages you are genuinely
improving. Leave healthy scaffold pages alone; do not update a page just to reformat it. Prefer
targeted patch updates: pass only the fields you are changing (title only, or surface_json only) —
omitted fields are preserved.

## query_js contract

Define a function handler(params) that returns an object; its top-level keys are what $data
references resolve against (e.g. return { rows: [...], total: n, open_count: k }).
- Every row object bound into a DataTable must carry an "id" field (row actions key off it).
- Bound Input names must match the query action's declared params and the keys you read from
  params inside handler(params), or the value never reaches your code.
- Restricted QuickJS sandbox: use var (not let/const), function() {} (no arrow functions), for
  loops (not .map/.filter), string concatenation (no template literals), no destructuring, no
  optional chaining, no toLocaleString/Date formatting, no async/await. db calls are synchronous.
- db API: var t = db.table("name"); t.list(limit); t.get(id); t.search.text(q, limit, offset);
  t.search.attr([{field, op, value}], limit, offset); db.tables(). System tables are not accessible.

## Self-correction loop

- After EVERY create/update, check render_status in the response. On "error", read render_error and
  render_error_phase ("query_js" | "parse" | "validate") and render_logs, fix the offending
  query_js / surface_json / actions_json, and update again until render_status is "ok". On success,
  use data_keys to confirm your $data paths point at keys that actually exist.
- For any insert/update/delete/query action you declare, run manage_ui action=exec_action
  (action_name + params) to confirm it works end-to-end. Clean up any test rows you create.
- At the START of each run, render existing pages to catch any that broke from schema changes or
  deleted tables, and fix or disable them.

%%SURFACE_SPEC%%

## Guidelines

- Prefer clean Column layouts with clear sections over deep nesting.
- Set auto_refresh_seconds >= 30 for live data, 0 for static summaries.
- Set source_tables to the tables the page reads, and sort_order so overviews come first.
- If a table has no data yet, leave its scaffold page alone — don't build empty dashboards.
- Be incremental: a few real improvements per run beats a rebuild.
`

// uiBuilderPrompt embeds the canonical surface spec (surface.SpecMarkdown) so the
// agent and the validator share one source of truth. EnsureBuiltinAgents compares
// this text against the stored prompt and updates existing deployments on change.
var uiBuilderPrompt = strings.Replace(uiBuilderPromptTemplate, "%%SURFACE_SPEC%%", surface.SpecMarkdown(), 1)

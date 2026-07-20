package surface

import (
	"fmt"
	"strings"
)

// SpecMarkdown renders the LLM-facing specification of the surface format from
// the component registry. It is deterministic and embedded verbatim in system
// prompts, so both the validator and the spec are driven by the same registry
// and cannot drift.
func SpecMarkdown() string {
	var b strings.Builder

	b.WriteString("# Surface UI Format\n\n")
	b.WriteString("A page is a flat list of components plus a set of declared actions. ")
	b.WriteString("Each component has a unique `id`, a `type`, and `properties`. ")
	b.WriteString("Exactly one component must have the id `root`; it is the entry point. ")
	b.WriteString("Layout components (`Column`, `Row`, `Card`) list the ids of their `children`; ")
	b.WriteString("all other components are leaves and must not have children. ")
	b.WriteString("Unknown types and unknown properties are rejected.\n\n")

	b.WriteString("A page is JSON of the form:\n\n")
	b.WriteString("```json\n")
	b.WriteString("{\n")
	b.WriteString("  \"components\": [\n")
	b.WriteString("    {\"id\": \"root\", \"type\": \"Column\", \"children\": [\"t1\"]},\n")
	b.WriteString("    {\"id\": \"t1\", \"type\": \"Text\", \"properties\": {\"value\": \"Hello\"}}\n")
	b.WriteString("  ]\n")
	b.WriteString("}\n")
	b.WriteString("```\n\n")

	b.WriteString("## Components\n\n")
	for _, def := range registry {
		writeComponent(&b, def)
	}

	b.WriteString("## Data binding\n\n")
	b.WriteString("Any property marked \"$data: yes\" may hold a reference of the form ")
	b.WriteString("`{\"$data\": \"path.to.value\"}` instead of a literal value. The path is ")
	b.WriteString("resolved (dot-separated) against the data object returned by the page's query ")
	b.WriteString("at render time. There is no string templating. Example:\n\n")
	b.WriteString("```json\n")
	b.WriteString("{\"id\": \"tbl\", \"type\": \"DataTable\", \"properties\": {\n")
	b.WriteString("  \"columns\": [{\"key\": \"name\", \"label\": \"Name\"}],\n")
	b.WriteString("  \"rows\": {\"$data\": \"orders\"}\n")
	b.WriteString("}}\n")
	b.WriteString("```\n\n")
	b.WriteString("A `DataTable`'s `rows` $data path must resolve to an array. ")
	b.WriteString("$data is not allowed inside `columns`, `fields`, or action names.\n\n")

	b.WriteString("## Actions\n\n")
	b.WriteString("Actions are declared once per page, separately from components, and referenced ")
	b.WriteString("by `id`. Types:\n\n")
	b.WriteString("- `insert` / `update` / `delete`: write to `table` (required; system tables, ")
	b.WriteString("names starting with `_`, are rejected).\n")
	b.WriteString("- `query`: re-run the page's query with the given `params` and return fresh data ")
	b.WriteString("(this is how search, filter, and pagination work).\n\n")
	b.WriteString("```json\n")
	b.WriteString("{\n")
	b.WriteString("  \"create_order\": {\"type\": \"insert\", \"table\": \"orders\"},\n")
	b.WriteString("  \"delete_order\": {\"type\": \"delete\", \"table\": \"orders\", \"confirm\": true},\n")
	b.WriteString("  \"search\":       {\"type\": \"query\", \"params\": [\"q\"]}\n")
	b.WriteString("}\n")
	b.WriteString("```\n\n")
	b.WriteString("A `Button` and `Form` submit may reference `insert`/`update`/`query` actions; ")
	b.WriteString("`DataTable` row actions may reference `update`/`delete`/`query` (never `insert`).\n\n")

	b.WriteString("## Worked example\n\n")
	b.WriteString("A page listing orders with a search box, a create form, and a refresh button:\n\n")
	b.WriteString("```json\n")
	b.WriteString(workedExample)
	b.WriteString("\n```\n")

	return b.String()
}

func writeComponent(b *strings.Builder, def componentDef) {
	fmt.Fprintf(b, "### %s\n\n", def.Name)
	fmt.Fprintf(b, "%s", def.Summary)
	if def.AllowChildren {
		b.WriteString(" May have children.")
	}
	b.WriteString("\n\n")
	if len(def.Props) == 0 {
		return
	}
	b.WriteString("| Property | Type | Required | $data | Description |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, ps := range def.Props {
		fmt.Fprintf(b, "| `%s` | %s | %s | %s | %s |\n",
			ps.Name, ps.TypeDesc, yesNo(ps.Required), yesNo(ps.AllowData), ps.Desc)
	}
	b.WriteString("\n")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

const workedExample = `{
  "components": [
    {"id": "root", "type": "Column", "properties": {"gap": 16}, "children": ["hdr", "search", "tbl", "create", "refresh"]},
    {"id": "hdr", "type": "Text", "properties": {"value": "Orders", "variant": "heading"}},
    {"id": "search", "type": "Input", "properties": {"name": "q", "type": "string", "label": "Search", "bind": true}},
    {"id": "tbl", "type": "DataTable", "properties": {
      "columns": [
        {"key": "id", "label": "ID"},
        {"key": "total", "label": "Total", "type": "float"}
      ],
      "rows": {"$data": "orders"},
      "empty_text": "No orders yet.",
      "row_actions": [{"action": "delete_order", "label": "Delete", "confirm": true}]
    }},
    {"id": "create", "type": "Form", "properties": {
      "fields": [
        {"name": "customer", "label": "Customer", "type": "string", "required": true},
        {"name": "total", "label": "Total", "type": "float"}
      ],
      "submit": {"action": "create_order", "label": "Create order"}
    }},
    {"id": "refresh", "type": "Button", "properties": {"label": "Refresh", "action": "search", "params": {"q": {"$data": "query.q"}}}}
  ],
  "actions": {
    "create_order": {"type": "insert", "table": "orders"},
    "delete_order": {"type": "delete", "table": "orders", "confirm": true},
    "search": {"type": "query", "params": ["q"]}
  }
}`

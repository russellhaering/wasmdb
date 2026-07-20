package uigen

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/russellhaering/wasmdb/internal/surface"
)

// Stable component IDs used across every generated page.
const (
	idRoot    = "root"
	idMain    = "main"
	idSummary = "summary"
	idMetric  = "metric"
	idSearch  = "search"
	idTable   = "table"
	idAddCard = "add"
	idForm    = "form"
)

// Action names declared on generated pages.
const (
	actCreate = "create"
	actUpdate = "update_row"
	actDelete = "delete_row"
	actSearch = "search"
)

// buildSurface constructs the surface and action declarations for a page and
// returns their indented JSON encodings. Output is deterministic: components are
// built as an ordered slice and every object's keys are emitted in sorted order
// by encoding/json.
func buildSurface(title, singular, tableName string, cols []column, fields []formField, searchable, hasForm, emptyless bool) (surfaceJSON, actionsJSON string, err error) {
	var comps []surface.Component
	actions := surface.Actions{}

	// root Column.
	rootChildren := []string{idMain}
	if hasForm {
		rootChildren = append(rootChildren, idAddCard)
	}
	comps = append(comps, surface.Component{
		ID:         idRoot,
		Type:       "Column",
		Properties: map[string]any{"gap": 16},
		Children:   rootChildren,
	})

	// Main card: summary row + data table.
	comps = append(comps, surface.Component{
		ID:         idMain,
		Type:       "Card",
		Properties: map[string]any{"title": title},
		Children:   []string{idSummary, idTable},
	})

	// Summary row: metric (+ optional search input).
	summaryChildren := []string{idMetric}
	if searchable {
		summaryChildren = append(summaryChildren, idSearch)
	}
	comps = append(comps, surface.Component{
		ID:         idSummary,
		Type:       "Row",
		Properties: map[string]any{"gap": 12, "align": "end"},
		Children:   summaryChildren,
	})

	// Metric.
	comps = append(comps, surface.Component{
		ID:   idMetric,
		Type: "Metric",
		Properties: map[string]any{
			"label": "Total shown",
			"value": dataRef("count"),
		},
	})

	// Search input.
	if searchable {
		comps = append(comps, surface.Component{
			ID:   idSearch,
			Type: "Input",
			Properties: map[string]any{
				"name":        "q",
				"type":        "string",
				"label":       "Search",
				"placeholder": "Search " + strings.ToLower(title) + "…",
				"bind":        true,
			},
		})
	}

	// Data table.
	tableProps := map[string]any{
		"columns":    columnsToAny(cols),
		"rows":       dataRef("rows"),
		"empty_text": emptyText(emptyless),
	}
	if !emptyless {
		var rowActions []any
		if hasForm {
			rowActions = append(rowActions, map[string]any{"action": actUpdate, "label": "Edit"})
		}
		rowActions = append(rowActions, map[string]any{"action": actDelete, "label": "Delete", "confirm": true})
		tableProps["row_actions"] = rowActions
	}
	comps = append(comps, surface.Component{
		ID:         idTable,
		Type:       "DataTable",
		Properties: tableProps,
	})

	// Add card + form.
	if hasForm {
		addTitle := "Add " + singular
		comps = append(comps, surface.Component{
			ID:         idAddCard,
			Type:       "Card",
			Properties: map[string]any{"title": addTitle},
			Children:   []string{idForm},
		})
		comps = append(comps, surface.Component{
			ID:   idForm,
			Type: "Form",
			Properties: map[string]any{
				"fields": fieldsToAny(fields),
				"submit": map[string]any{"action": actCreate, "label": addTitle},
			},
		})
	}

	// Action declarations.
	if !emptyless {
		actions[actDelete] = surface.Action{Type: surface.ActionDelete, Table: tableName, Confirm: true}
		if hasForm {
			actions[actCreate] = surface.Action{Type: surface.ActionInsert, Table: tableName}
			actions[actUpdate] = surface.Action{Type: surface.ActionUpdate, Table: tableName}
		}
		if searchable {
			actions[actSearch] = surface.Action{Type: surface.ActionQuery, Params: []string{"q"}}
		}
	}

	surf := surface.Surface{Components: comps}
	sj, err := json.MarshalIndent(surf, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("uigen: marshal surface: %w", err)
	}
	aj, err := json.MarshalIndent(actions, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("uigen: marshal actions: %w", err)
	}
	return string(sj), string(aj), nil
}

func emptyText(emptyless bool) string {
	if emptyless {
		return "No documents yet. Add data to this table and the page will gain columns and a form."
	}
	return "No documents yet."
}

func dataRef(path string) map[string]any {
	return map[string]any{"$data": path}
}

func columnsToAny(cols []column) []any {
	out := make([]any, 0, len(cols))
	for _, c := range cols {
		m := map[string]any{"key": c.Key, "label": c.Label}
		if c.Type != "" {
			m["type"] = c.Type
		}
		out = append(out, m)
	}
	return out
}

func fieldsToAny(fields []formField) []any {
	out := make([]any, 0, len(fields))
	for _, f := range fields {
		m := map[string]any{"name": f.Name, "label": f.Label, "type": f.Type}
		if f.Required {
			m["required"] = true
		}
		out = append(out, m)
	}
	return out
}

// buildQueryJS emits a readable, LLM-editable query script defining
// handler(params). It lists recent documents (or full-text searches when a q
// param is present and the table is searchable) and flattens each into a row
// carrying id plus the chosen column keys.
func buildQueryJS(tableName string, cols []column, searchable bool) string {
	var b strings.Builder
	b.WriteString("function handler(params) {\n")
	fmt.Fprintf(&b, "  var t = db.table(%s);\n", strconv.Quote(tableName))
	if searchable {
		b.WriteString("  var docs;\n")
		b.WriteString("  if (params && params.q) {\n")
		b.WriteString("    docs = t.search.text(String(params.q), 50, 0);\n")
		b.WriteString("  } else {\n")
		b.WriteString("    docs = t.list(50);\n")
		b.WriteString("  }\n")
	} else {
		b.WriteString("  var docs = t.list(50);\n")
	}
	b.WriteString("  var rows = [];\n")
	b.WriteString("  for (var i = 0; i < docs.length; i++) {\n")
	b.WriteString("    var d = docs[i];\n")
	b.WriteString("    var a = d.attributes || {};\n")
	b.WriteString("    rows.push({\n")
	b.WriteString("      id: d.id")
	for _, c := range cols {
		if c.Key == "id" {
			continue
		}
		fmt.Fprintf(&b, ",\n      %s: a[%s]", strconv.Quote(c.Key), strconv.Quote(c.Key))
	}
	b.WriteString("\n    });\n")
	b.WriteString("  }\n")
	b.WriteString("  return { rows: rows, count: rows.length };\n")
	b.WriteString("}\n")
	return b.String()
}

// humanize converts a table or field identifier into a Title Cased label:
// underscores, dashes, and spaces separate words; each word's first rune is
// upper-cased and the remainder is left unchanged.
func humanize(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	for i, w := range words {
		runes := []rune(w)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

// singularize returns a naive singular form of a humanized label, operating on
// its last word. It handles the common English plural endings well enough for
// button/card labels.
func singularize(title string) string {
	if title == "" {
		return title
	}
	words := strings.Split(title, " ")
	last := words[len(words)-1]
	words[len(words)-1] = singularizeWord(last)
	return strings.Join(words, " ")
}

func singularizeWord(w string) string {
	lower := strings.ToLower(w)
	switch {
	case len(w) > 3 && strings.HasSuffix(lower, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 3 && (strings.HasSuffix(lower, "ses") ||
		strings.HasSuffix(lower, "xes") ||
		strings.HasSuffix(lower, "zes") ||
		strings.HasSuffix(lower, "ches") ||
		strings.HasSuffix(lower, "shes")):
		return w[:len(w)-2]
	case len(w) > 1 && strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss"):
		return w[:len(w)-1]
	default:
		return w
	}
}

// looksLikeRFC3339 reports whether s parses as an RFC3339 timestamp.
func looksLikeRFC3339(s string) bool {
	if len(s) < 20 {
		return false
	}
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

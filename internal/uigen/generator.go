// Package uigen is a deterministic, pure-Go schema→page generator. It produces
// working CRUD "scaffold" pages for user tables with zero LLM involvement, so
// the auto-generated UI is populated on day one.
//
// The generator is provenance-aware: it only creates or overwrites pages marked
// with generator=="scaffold" and never touches pages an agent or user has
// claimed. See Generator.Sweep.
package uigen

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/moraine/document"
	"github.com/russellhaering/wasmdb/internal/surface"
	"github.com/russellhaering/wasmdb/internal/uiconfig"
)

const (
	// pagePrefix is prepended to a table name to form its scaffold page name.
	pagePrefix = "tbl-"
	// GeneratorScaffold is the provenance value the generator writes and the only
	// value it will overwrite.
	GeneratorScaffold = "scaffold"
	// scaffoldAutoRefreshSeconds is the default auto-refresh interval for scaffold
	// pages. Kept at 30s to bound QuickJS render cost (one render per view).
	scaffoldAutoRefreshSeconds = 30
	// maxColumns caps the visible DataTable columns (the leading id column plus up
	// to maxColumns-1 fields).
	maxColumns = 8
	// sampleSize is how many documents a schemaless table is sampled for when
	// inferring columns.
	sampleSize = 50
)

// Generator builds and reconciles deterministic scaffold pages.
type Generator struct {
	registry *database.Registry
	store    *uiconfig.Store
	renderer *uiconfig.Renderer
}

// New creates a Generator bound to the registry (schema + data source), the UI
// config store (page persistence), and the renderer (self-check validation).
func New(registry *database.Registry, store *uiconfig.Store, renderer *uiconfig.Renderer) *Generator {
	return &Generator{registry: registry, store: store, renderer: renderer}
}

// PageSpec is the generated content for a single table's scaffold page. It holds
// no persistence concerns; Sweep is responsible for writing it to the store.
type PageSpec struct {
	Name               string
	Title              string
	Description        string
	SurfaceJSON        string
	ActionsJSON        string
	QueryJS            string
	SourceTables       []string
	AutoRefreshSeconds int
	SortOrder          int
}

// column is one DataTable column plus the bookkeeping the query generator needs.
type column struct {
	Key   string
	Label string
	Type  string // "" means untyped (frontend renders as text)
}

// formField is one Form input field.
type formField struct {
	Name     string
	Label    string
	Type     string
	Required bool
}

// GeneratePage builds the page content for one table without touching the store.
// It ends by validating its own output (a generator-bug guard) and returns an
// error if the generated surface/actions do not validate.
func (g *Generator) GeneratePage(ctx context.Context, tableName string) (*PageSpec, error) {
	if strings.HasPrefix(tableName, "_") {
		return nil, fmt.Errorf("uigen: refusing to scaffold system table %q", tableName)
	}

	tbl, err := g.registry.GetTable(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("uigen: get table %q: %w", tableName, err)
	}

	title := humanize(tableName)
	singular := singularize(title)

	var (
		columns    []column
		formFields []formField
		searchable bool
		descNotes  []string
		emptyless  bool // true for a schemaless table with zero sampled docs
	)

	if schema := tbl.Schema(); schema != nil && len(schema.Fields) > 0 {
		columns, formFields, searchable, descNotes = fromSchema(schema)
	} else {
		var docs []*document.Document
		docs, err = sampleDocuments(ctx, tbl)
		if err != nil {
			return nil, fmt.Errorf("uigen: sample %q: %w", tableName, err)
		}
		if len(docs) == 0 {
			emptyless = true
			columns = []column{{Key: "id", Label: "ID"}}
			searchable = false
			descNotes = append(descNotes, "This table has no documents yet; the page will gain columns and a form once data exists.")
		} else {
			columns, formFields = fromSampledDocs(docs)
			searchable = true // schemaless: search.text also matches document content
			descNotes = append(descNotes, "Columns were inferred from a sample of existing documents and may change as data evolves.")
		}
	}

	hasForm := len(formFields) > 0

	spec := &PageSpec{
		Name:               pagePrefix + tableName,
		Title:              title,
		Description:        buildDescription(tableName, descNotes),
		SourceTables:       []string{tableName},
		AutoRefreshSeconds: scaffoldAutoRefreshSeconds,
		SortOrder:          0,
	}

	surfaceJSON, actionsJSON, err := buildSurface(title, singular, tableName, columns, formFields, searchable, hasForm, emptyless)
	if err != nil {
		return nil, err
	}
	spec.SurfaceJSON = surfaceJSON
	spec.ActionsJSON = actionsJSON
	spec.QueryJS = buildQueryJS(tableName, columns, searchable)

	// Self-check: the generator's own output must validate. This guards against
	// generator bugs producing pages the render pipeline would reject.
	if err := selfCheck(spec); err != nil {
		return nil, fmt.Errorf("uigen: generated page for %q failed self-validation: %w", tableName, err)
	}

	return spec, nil
}

// selfCheck parses and validates the generated surface + actions (data nil, so
// $data paths are checked syntactically only).
func selfCheck(spec *PageSpec) error {
	surf, err := surface.ParseSurface([]byte(spec.SurfaceJSON))
	if err != nil {
		return err
	}
	actions, err := surface.ParseActions([]byte(spec.ActionsJSON))
	if err != nil {
		return err
	}
	return surface.Validate(surf, actions, nil)
}

// fromSchema derives columns, form fields, searchability, and description notes
// from a typed schema.
func fromSchema(schema *document.Schema) (cols []column, fields []formField, searchable bool, notes []string) {
	cols = append(cols, column{Key: "id", Label: "ID"})

	var skipped []string
	fieldColsAdded := 0
	for _, f := range schema.Fields {
		if f.FullText {
			searchable = true
		}
		if f.Name == "id" {
			// Avoid colliding with the implicit id column.
			continue
		}
		// Columns: include every field (capped), mapping unsupported types to text.
		if fieldColsAdded < maxColumns-1 {
			cols = append(cols, column{Key: f.Name, Label: humanize(f.Name), Type: columnType(f.Type)})
			fieldColsAdded++
		}
		// Form fields: skip array and reference types in v1.
		if ft, ok := formFieldType(f.Type); ok {
			fields = append(fields, formField{Name: f.Name, Label: humanize(f.Name), Type: ft, Required: f.Required})
		} else {
			skipped = append(skipped, f.Name)
		}
	}

	if len(skipped) > 0 {
		notes = append(notes, "The add form omits these fields (array or reference types not yet supported): "+strings.Join(skipped, ", ")+".")
	}
	return cols, fields, searchable, notes
}

// fromSampledDocs infers columns and form fields from sampled schemaless docs.
// Attribute keys are ordered by descending frequency, then name, and the whole
// set (id + attribute columns) is capped at maxColumns.
func fromSampledDocs(docs []*document.Document) (cols []column, fields []formField) {
	type stat struct {
		count int
		typ   string
	}
	stats := map[string]*stat{}
	var order []string
	for _, d := range docs {
		for k, v := range d.Attributes {
			if k == "id" {
				continue
			}
			s, ok := stats[k]
			if !ok {
				s = &stat{typ: inferType(v)}
				stats[k] = s
				order = append(order, k)
			} else if s.typ == "" {
				s.typ = inferType(v)
			}
			s.count++
		}
	}

	// Deterministic order: frequency desc, then name asc.
	sort.SliceStable(order, func(i, j int) bool {
		ci, cj := stats[order[i]].count, stats[order[j]].count
		if ci != cj {
			return ci > cj
		}
		return order[i] < order[j]
	})

	cols = append(cols, column{Key: "id", Label: "ID"})
	for _, k := range order {
		if len(cols) >= maxColumns {
			break
		}
		typ := stats[k].typ
		cols = append(cols, column{Key: k, Label: humanize(k), Type: typ})
		fields = append(fields, formField{Name: k, Label: humanize(k), Type: formFieldTypeOrString(typ), Required: false})
	}
	return cols, fields
}

// sampleDocuments lists up to sampleSize documents for column inference.
func sampleDocuments(ctx context.Context, tbl *database.Table) ([]*document.Document, error) {
	docs, _, err := tbl.ListDocuments(ctx, sampleSize, "")
	if err != nil {
		return nil, err
	}
	return docs, nil
}

// buildDescription assembles a stable, human-readable page description.
func buildDescription(tableName string, notes []string) string {
	desc := fmt.Sprintf("Auto-generated page for the %q table.", tableName)
	if len(notes) > 0 {
		desc += " " + strings.Join(notes, " ")
	}
	return desc
}

// columnType maps a schema FieldType to a DataTable column type. Array and
// reference types fall back to text (empty type).
func columnType(ft document.FieldType) string {
	switch ft {
	case document.FieldTypeString:
		return "string"
	case document.FieldTypeInt:
		return "int"
	case document.FieldTypeFloat:
		return "float"
	case document.FieldTypeBool:
		return "bool"
	case document.FieldTypeDatetime:
		return "datetime"
	default:
		// []string, []int, []float, reference → rendered as text.
		return "string"
	}
}

// formFieldType maps a schema FieldType to a Form field type, reporting false
// for types not supported in the v1 add form (arrays and references).
func formFieldType(ft document.FieldType) (string, bool) {
	switch ft {
	case document.FieldTypeString:
		return "string", true
	case document.FieldTypeInt:
		return "int", true
	case document.FieldTypeFloat:
		return "float", true
	case document.FieldTypeBool:
		return "bool", true
	case document.FieldTypeDatetime:
		return "datetime", true
	default:
		return "", false
	}
}

// formFieldTypeOrString returns a valid form field type, defaulting to string.
func formFieldTypeOrString(typ string) string {
	switch typ {
	case "int", "float", "bool", "datetime":
		return typ
	default:
		return "string"
	}
}

// inferType infers a column/field type from an observed attribute value.
func inferType(v any) string {
	switch x := v.(type) {
	case bool:
		return "bool"
	case float64:
		if x == float64(int64(x)) {
			return "int"
		}
		return "float"
	case float32:
		return "float"
	case int, int64, int32:
		return "int"
	case string:
		if looksLikeRFC3339(x) {
			return "datetime"
		}
		return "string"
	default:
		return "string"
	}
}

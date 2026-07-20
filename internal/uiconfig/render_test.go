package uiconfig

import (
	"strings"
	"testing"

	"github.com/russellhaering/moraine/document"
)

// dataTableSurface renders a DataTable whose rows bind to $data "rows".
const dataTableSurface = `{
  "components": [
    {"id": "root", "type": "Column", "children": ["tbl"]},
    {"id": "tbl", "type": "DataTable", "properties": {
      "columns": [{"key": "name", "label": "Name"}],
      "rows": {"$data": "rows"}
    }}
  ]
}`

// listQuery returns {rows:[{name, q}]} from the items table, echoing params.q so
// tests can confirm params flow through.
const listQuery = `function handler(params) {
  var docs = db.table("items").list();
  return { rows: docs.map(function(d){ return { name: d.attributes.name, q: (params && params.q) || "" }; }) };
}`

func TestRenderQueryDataBindingEndToEnd(t *testing.T) {
	ctx, reg, store, renderer := newTestEnv(t)
	tbl := createTable(t, ctx, reg, "items", nil)
	for _, n := range []string{"alpha", "beta"} {
		if err := tbl.PutDocument(ctx, &document.Document{Attributes: map[string]any{"name": n}}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	cfg, err := store.Create(ctx, "items-page", "Items", "", []string{"items"}, dataTableSurface, "", listQuery, 0, 0, true, "user", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	res := renderer.Render(ctx, cfg, nil)
	if res.Error != "" {
		t.Fatalf("render error (%s): %s", res.ErrorPhase, res.Error)
	}
	if res.Surface == nil {
		t.Fatal("expected non-nil surface")
	}
	rows, ok := res.Data["rows"].([]any)
	if !ok {
		t.Fatalf("Data[rows] not an array: %#v", res.Data["rows"])
	}
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(rows))
	}
}

func TestRenderParamsReachQueryJS(t *testing.T) {
	ctx, reg, store, renderer := newTestEnv(t)
	tbl := createTable(t, ctx, reg, "items", nil)
	if err := tbl.PutDocument(ctx, &document.Document{Attributes: map[string]any{"name": "x"}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	cfg, _ := store.Create(ctx, "p", "", "", nil, dataTableSurface, "", listQuery, 0, 0, true, "user", "")

	res := renderer.Render(ctx, cfg, map[string]any{"q": "hello"})
	if res.Error != "" {
		t.Fatalf("render error (%s): %s", res.ErrorPhase, res.Error)
	}
	rows := res.Data["rows"].([]any)
	first := rows[0].(map[string]any)
	if first["q"] != "hello" {
		t.Errorf("params.q did not reach query_js: got %#v", first["q"])
	}
}

func TestRenderSpecialCharsPassThroughByteIdentical(t *testing.T) {
	ctx, reg, store, renderer := newTestEnv(t)
	tbl := createTable(t, ctx, reg, "items", nil)

	// Values that broke the old {{}} string-templating engine.
	nasty := "quote:\" backslash:\\ newline:\n literal-template:{{rows}} end"
	if err := tbl.PutDocument(ctx, &document.Document{Attributes: map[string]any{"name": nasty}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	cfg, _ := store.Create(ctx, "p", "", "", nil, dataTableSurface, "", listQuery, 0, 0, true, "user", "")

	res := renderer.Render(ctx, cfg, nil)
	if res.Error != "" {
		t.Fatalf("render error (%s): %s", res.ErrorPhase, res.Error)
	}
	rows := res.Data["rows"].([]any)
	got := rows[0].(map[string]any)["name"]
	if got != nasty {
		t.Errorf("special chars not byte-identical:\n got: %q\nwant: %q", got, nasty)
	}
}

func TestRenderNilParams(t *testing.T) {
	ctx, reg, store, renderer := newTestEnv(t)
	createTable(t, ctx, reg, "items", nil)
	cfg, _ := store.Create(ctx, "p", "", "", nil, dataTableSurface, "", listQuery, 0, 0, true, "user", "")
	res := renderer.Render(ctx, cfg, nil)
	if res.Error != "" {
		t.Fatalf("render with nil params errored (%s): %s", res.ErrorPhase, res.Error)
	}
}

func TestRenderQueryReturnsNonMap(t *testing.T) {
	ctx, _, store, renderer := newTestEnv(t)
	cfg, _ := store.Create(ctx, "p", "", "", nil, dataTableSurface, "", "42", 0, 0, true, "user", "")
	res := renderer.Render(ctx, cfg, nil)
	if res.ErrorPhase != "query_js" {
		t.Fatalf("expected query_js phase, got %q (err=%s)", res.ErrorPhase, res.Error)
	}
	if !strings.Contains(res.Error, "object") {
		t.Errorf("expected message about returning an object, got %q", res.Error)
	}
}

func TestRenderInvalidSurfaceJSON(t *testing.T) {
	ctx, _, store, renderer := newTestEnv(t)
	cfg, _ := store.Create(ctx, "p", "", "", nil, "this is not json", "", "", 0, 0, true, "user", "")
	res := renderer.Render(ctx, cfg, nil)
	if res.ErrorPhase != "parse" {
		t.Fatalf("expected parse phase, got %q (err=%s)", res.ErrorPhase, res.Error)
	}
}

func TestRenderValidationFailureMultiError(t *testing.T) {
	ctx, _, store, renderer := newTestEnv(t)

	// Text missing required "value"; a Button referencing an undeclared action.
	surf := `{"components":[
      {"id":"root","type":"Column","children":["t","b"]},
      {"id":"t","type":"Text","properties":{}},
      {"id":"b","type":"Button","properties":{"label":"Go","action":"nope"}}
    ]}`
	cfg, _ := store.Create(ctx, "p", "", "", nil, surf, "", "", 0, 0, true, "user", "")
	res := renderer.Render(ctx, cfg, nil)
	if res.ErrorPhase != "validate" {
		t.Fatalf("expected validate phase, got %q (err=%s)", res.ErrorPhase, res.Error)
	}
	// Multi-error text: both problems should be reported.
	if !strings.Contains(res.Error, "value") {
		t.Errorf("missing required-property error: %q", res.Error)
	}
	if !strings.Contains(res.Error, "nope") {
		t.Errorf("missing undeclared-action error: %q", res.Error)
	}
}

func TestRenderNoQueryLiteralRows(t *testing.T) {
	ctx, _, store, renderer := newTestEnv(t)
	surf := `{"components":[
      {"id":"root","type":"DataTable","properties":{
        "columns":[{"key":"name","label":"Name"}],
        "rows":[{"name":"a"},{"name":"b"}]
      }}
    ]}`
	cfg, _ := store.Create(ctx, "p", "", "", nil, surf, "", "", 0, 0, true, "user", "")
	res := renderer.Render(ctx, cfg, nil)
	if res.Error != "" {
		t.Fatalf("render error (%s): %s", res.ErrorPhase, res.Error)
	}
	if res.Data != nil {
		t.Errorf("expected nil Data for a page without query_js, got %#v", res.Data)
	}
	if res.Surface == nil {
		t.Error("expected non-nil surface")
	}
}

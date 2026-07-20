package uigen

import (
	"testing"
	"time"
)

// TestGeneratePageGoldenTypedSchema covers a typed schema with string/int/
// float/bool/datetime fields plus a FullText field and skipped array/reference
// fields.
func TestGeneratePageGoldenTypedSchema(t *testing.T) {
	ctx, reg, _, _, gen := newTestGen(t)
	createTable(t, ctx, reg, "orders", ordersSchema())

	spec, err := gen.GeneratePage(ctx, "orders")
	if err != nil {
		t.Fatalf("GeneratePage: %v", err)
	}

	if spec.Name != "tbl-orders" || spec.Title != "Orders" {
		t.Errorf("name/title = %q/%q", spec.Name, spec.Title)
	}
	checkGolden(t, "typed.surface.json", spec.SurfaceJSON)
	checkGolden(t, "typed.actions.json", spec.ActionsJSON)
	checkGolden(t, "typed.query.js", spec.QueryJS)
}

// TestGeneratePageGoldenSchemaless covers a schemaless table whose columns are
// inferred from sampled documents (frequency-then-name ordering, type inference).
func TestGeneratePageGoldenSchemaless(t *testing.T) {
	ctx, reg, _, _, gen := newTestGen(t)
	tbl := createTable(t, ctx, reg, "posts", nil)

	putDoc(t, ctx, tbl, map[string]any{"title": "A", "views": 10, "published": true})
	putDoc(t, ctx, tbl, map[string]any{"title": "B", "views": 20, "when": "2026-01-01T00:00:00Z"})
	putDoc(t, ctx, tbl, map[string]any{"title": "C"})

	spec, err := gen.GeneratePage(ctx, "posts")
	if err != nil {
		t.Fatalf("GeneratePage: %v", err)
	}
	checkGolden(t, "schemaless.surface.json", spec.SurfaceJSON)
	checkGolden(t, "schemaless.actions.json", spec.ActionsJSON)
	checkGolden(t, "schemaless.query.js", spec.QueryJS)
}

// TestGeneratePageGoldenEmpty covers an empty schemaless table (id column only,
// no form, no actions).
func TestGeneratePageGoldenEmpty(t *testing.T) {
	ctx, reg, _, _, gen := newTestGen(t)
	createTable(t, ctx, reg, "notes", nil)

	spec, err := gen.GeneratePage(ctx, "notes")
	if err != nil {
		t.Fatalf("GeneratePage: %v", err)
	}
	checkGolden(t, "empty.surface.json", spec.SurfaceJSON)
	checkGolden(t, "empty.actions.json", spec.ActionsJSON)
	checkGolden(t, "empty.query.js", spec.QueryJS)
}

// TestGeneratePageRejectsSystemTable guards the system-table refusal.
func TestGeneratePageRejectsSystemTable(t *testing.T) {
	ctx, _, _, _, gen := newTestGen(t)
	if _, err := gen.GeneratePage(ctx, "_users"); err == nil {
		t.Fatal("expected error scaffolding a system table")
	}
}

// TestGeneratePageLiveRender stores a generated page and renders it against live
// data, exercising both the default list branch and the search (q) branch.
func TestGeneratePageLiveRender(t *testing.T) {
	ctx, reg, store, renderer, gen := newTestGen(t)
	tbl := createTable(t, ctx, reg, "orders", ordersSchema())

	putDoc(t, ctx, tbl, map[string]any{"customer": "Acme", "quantity": 2, "total": 9.5, "paid": true, "created": "2026-01-01T00:00:00Z"})
	putDoc(t, ctx, tbl, map[string]any{"customer": "Globex", "quantity": 1, "total": 4.0, "paid": false, "created": "2026-02-01T00:00:00Z"})

	spec, err := gen.GeneratePage(ctx, "orders")
	if err != nil {
		t.Fatalf("GeneratePage: %v", err)
	}
	cfg, err := store.Create(ctx, spec.Name, spec.Title, spec.Description, spec.SourceTables,
		spec.SurfaceJSON, spec.ActionsJSON, spec.QueryJS, spec.AutoRefreshSeconds, spec.SortOrder, true, GeneratorScaffold, "")
	if err != nil {
		t.Fatalf("store.Create: %v", err)
	}

	// Default render: no params → list branch.
	res := renderer.Render(ctx, cfg, nil)
	if res.Error != "" {
		t.Fatalf("render error (%s): %s", res.ErrorPhase, res.Error)
	}
	rows := rowsOf(t, res.Data)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	for _, r := range rows {
		if id, _ := r["id"].(string); id == "" {
			t.Fatalf("row missing id: %#v", r)
		}
	}

	// Search render: the FullText customer field makes search applicable. Poll
	// briefly because the full-text index is populated asynchronously.
	deadline := time.Now().Add(3 * time.Second)
	for {
		res := renderer.Render(ctx, cfg, map[string]any{"q": "Acme"})
		if res.Error != "" {
			t.Fatalf("search render error (%s): %s", res.ErrorPhase, res.Error)
		}
		rows := rowsOf(t, res.Data)
		if len(rows) == 1 {
			if c, _ := rows[0]["customer"].(string); c != "Acme" {
				t.Fatalf("search returned wrong row: %#v", rows[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("search branch never returned the expected single row (last count %d)", len(rows))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestGeneratePageLiveActions exercises the generated create and delete_row
// actions end to end.
func TestGeneratePageLiveActions(t *testing.T) {
	ctx, reg, store, renderer, gen := newTestGen(t)
	tbl := createTable(t, ctx, reg, "orders", ordersSchema())

	spec, err := gen.GeneratePage(ctx, "orders")
	if err != nil {
		t.Fatalf("GeneratePage: %v", err)
	}
	cfg, err := store.Create(ctx, spec.Name, spec.Title, spec.Description, spec.SourceTables,
		spec.SurfaceJSON, spec.ActionsJSON, spec.QueryJS, spec.AutoRefreshSeconds, spec.SortOrder, true, GeneratorScaffold, "")
	if err != nil {
		t.Fatalf("store.Create: %v", err)
	}

	// create action → new document.
	createRes := renderer.ExecuteAction(ctx, cfg, actCreate, map[string]any{
		"customer": "Initech", "quantity": 3, "total": 12.0, "paid": true, "created": "2026-03-01T00:00:00Z",
	})
	if !createRes.OK {
		t.Fatalf("create action failed: %s", createRes.Error)
	}
	resMap, _ := createRes.Result.(map[string]any)
	id, _ := resMap["id"].(string)
	if id == "" {
		t.Fatalf("create action returned no id: %#v", createRes.Result)
	}

	got, err := tbl.GetDocument(ctx, id)
	if err != nil || got == nil {
		t.Fatalf("document not found after create: err=%v doc=%v", err, got)
	}
	if got.Attributes["customer"] != "Initech" {
		t.Fatalf("unexpected customer: %#v", got.Attributes["customer"])
	}

	// delete_row action → gone.
	delRes := renderer.ExecuteAction(ctx, cfg, actDelete, map[string]any{"id": id})
	if !delRes.OK {
		t.Fatalf("delete action failed: %s", delRes.Error)
	}
	gone, err := tbl.GetDocument(ctx, id)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if gone != nil {
		t.Fatalf("document still present after delete: %#v", gone)
	}
}

func rowsOf(t *testing.T, data map[string]any) []map[string]any {
	t.Helper()
	if data == nil {
		t.Fatal("nil render data")
	}
	raw, ok := data["rows"].([]any)
	if !ok {
		t.Fatalf("data.rows is not an array: %#v", data["rows"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("row is not an object: %#v", r)
		}
		out = append(out, m)
	}
	return out
}

package uiconfig

import (
	"strings"
	"testing"

	"github.com/russellhaering/moraine/document"
)

func TestExecuteInsert(t *testing.T) {
	ctx, reg, store, renderer := newTestEnv(t)
	tbl := createTable(t, ctx, reg, "items", nil)

	cfg, _ := store.Create(ctx, "p", "", "", nil, minimalSurface,
		`{"add":{"type":"insert","table":"items"}}`, "", 0, 0, true, "user", "")

	res := renderer.ExecuteAction(ctx, cfg, "add", map[string]any{"name": "foo", "content": "body-text"})
	if !res.OK {
		t.Fatalf("insert failed: %s", res.Error)
	}
	m, ok := res.Result.(map[string]any)
	if !ok {
		t.Fatalf("Result not a map: %#v", res.Result)
	}
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatal("result missing id")
	}

	// The document must actually exist.
	doc, err := tbl.GetDocument(ctx, id)
	if err != nil || doc == nil {
		t.Fatalf("inserted doc not found: %v", err)
	}
	if doc.Attributes["name"] != "foo" {
		t.Errorf("attr name = %#v, want foo", doc.Attributes["name"])
	}
	if doc.Content != "body-text" {
		t.Errorf("content = %q, want body-text", doc.Content)
	}
	// "content" and "id" must not leak into attributes.
	if _, present := doc.Attributes["content"]; present {
		t.Error("content leaked into attributes")
	}
}

func TestExecuteInsertSchemaError(t *testing.T) {
	ctx, reg, store, renderer := newTestEnv(t)
	createTable(t, ctx, reg, "typed", &document.Schema{
		Fields: []document.FieldDefinition{{Name: "name", Type: document.FieldTypeString}},
	})
	cfg, _ := store.Create(ctx, "p", "", "", nil, minimalSurface,
		`{"add":{"type":"insert","table":"typed"}}`, "", 0, 0, true, "user", "")

	// name should be a string; pass a number to trigger schema validation.
	res := renderer.ExecuteAction(ctx, cfg, "add", map[string]any{"name": 123})
	if res.OK {
		t.Fatal("expected schema validation failure")
	}
	if !strings.Contains(res.Error, "name") {
		t.Errorf("expected schema error mentioning field, got %q", res.Error)
	}
}

func TestExecuteUpdateMergeAndNullDelete(t *testing.T) {
	ctx, reg, store, renderer := newTestEnv(t)
	tbl := createTable(t, ctx, reg, "items", nil)
	seed := &document.Document{Attributes: map[string]any{"a": "1", "b": "2"}}
	if err := tbl.PutDocument(ctx, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg, _ := store.Create(ctx, "p", "", "", nil, minimalSurface,
		`{"upd":{"type":"update","table":"items"}}`, "", 0, 0, true, "user", "")

	res := renderer.ExecuteAction(ctx, cfg, "upd", map[string]any{
		"id": seed.ID,
		"a":  "changed",
		"b":  nil, // null deletes the attribute
	})
	if !res.OK {
		t.Fatalf("update failed: %s", res.Error)
	}

	doc, _ := tbl.GetDocument(ctx, seed.ID)
	if doc.Attributes["a"] != "changed" {
		t.Errorf("a = %#v, want changed", doc.Attributes["a"])
	}
	if _, present := doc.Attributes["b"]; present {
		t.Errorf("b should have been deleted, got %#v", doc.Attributes["b"])
	}
}

func TestExecuteUpdateMissingDoc(t *testing.T) {
	ctx, reg, store, renderer := newTestEnv(t)
	createTable(t, ctx, reg, "items", nil)
	cfg, _ := store.Create(ctx, "p", "", "", nil, minimalSurface,
		`{"upd":{"type":"update","table":"items"}}`, "", 0, 0, true, "user", "")

	res := renderer.ExecuteAction(ctx, cfg, "upd", map[string]any{"id": "does-not-exist", "a": "x"})
	if res.OK {
		t.Fatal("expected update of missing doc to fail")
	}
}

func TestExecuteDelete(t *testing.T) {
	ctx, reg, store, renderer := newTestEnv(t)
	tbl := createTable(t, ctx, reg, "items", nil)
	seed := &document.Document{Attributes: map[string]any{"a": "1"}}
	if err := tbl.PutDocument(ctx, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg, _ := store.Create(ctx, "p", "", "", nil, minimalSurface,
		`{"del":{"type":"delete","table":"items"}}`, "", 0, 0, true, "user", "")

	res := renderer.ExecuteAction(ctx, cfg, "del", map[string]any{"id": seed.ID})
	if !res.OK {
		t.Fatalf("delete failed: %s", res.Error)
	}
	doc, _ := tbl.GetDocument(ctx, seed.ID)
	if doc != nil {
		t.Error("doc should have been deleted")
	}
}

func TestExecuteDeleteMissingIDParam(t *testing.T) {
	ctx, reg, store, renderer := newTestEnv(t)
	createTable(t, ctx, reg, "items", nil)
	cfg, _ := store.Create(ctx, "p", "", "", nil, minimalSurface,
		`{"del":{"type":"delete","table":"items"}}`, "", 0, 0, true, "user", "")

	res := renderer.ExecuteAction(ctx, cfg, "del", map[string]any{})
	if res.OK {
		t.Fatal("expected error when id param missing")
	}
}

func TestExecuteDeleteMissingDoc(t *testing.T) {
	ctx, reg, store, renderer := newTestEnv(t)
	createTable(t, ctx, reg, "items", nil)
	cfg, _ := store.Create(ctx, "p", "", "", nil, minimalSurface,
		`{"del":{"type":"delete","table":"items"}}`, "", 0, 0, true, "user", "")

	res := renderer.ExecuteAction(ctx, cfg, "del", map[string]any{"id": "does-not-exist"})
	if res.OK {
		t.Fatal("expected delete of nonexistent doc to fail, got ok:true")
	}
	if !strings.Contains(res.Error, "not found") {
		t.Errorf("expected 'not found' error, got %q", res.Error)
	}
}

func TestExecuteQueryFiltersDeclaredParams(t *testing.T) {
	ctx, _, store, renderer := newTestEnv(t)
	// Echo params so we can observe which ones survived filtering.
	q := `function handler(params){ return { echo: params }; }`
	cfg, _ := store.Create(ctx, "p", "", "", nil, minimalSurface,
		`{"run":{"type":"query","params":["q"]}}`, q, 0, 0, true, "user", "")

	res := renderer.ExecuteAction(ctx, cfg, "run", map[string]any{"q": "keep", "extra": "drop"})
	if !res.OK {
		t.Fatalf("query action failed: %s", res.Error)
	}
	echo, ok := res.Data["echo"].(map[string]any)
	if !ok {
		t.Fatalf("Data[echo] not a map: %#v", res.Data)
	}
	if echo["q"] != "keep" {
		t.Errorf("declared param q not passed: %#v", echo["q"])
	}
	if _, present := echo["extra"]; present {
		t.Errorf("undeclared param should have been dropped: %#v", echo["extra"])
	}
}

func TestExecuteUndeclaredAction(t *testing.T) {
	ctx, reg, store, renderer := newTestEnv(t)
	createTable(t, ctx, reg, "items", nil)
	cfg, _ := store.Create(ctx, "p", "", "", nil, minimalSurface,
		`{"add":{"type":"insert","table":"items"}}`, "", 0, 0, true, "user", "")

	res := renderer.ExecuteAction(ctx, cfg, "bogus", map[string]any{})
	if res.OK {
		t.Fatal("expected undeclared action to fail")
	}
	if !strings.Contains(res.Error, "not declared") {
		t.Errorf("expected 'not declared' error, got %q", res.Error)
	}
}

func TestExecuteActionSystemTableRejected(t *testing.T) {
	ctx, _, store, renderer := newTestEnv(t)
	cfg, _ := store.Create(ctx, "p", "", "", nil, minimalSurface,
		`{"sys":{"type":"insert","table":"_users"}}`, "", 0, 0, true, "user", "")

	res := renderer.ExecuteAction(ctx, cfg, "sys", map[string]any{"email": "x@y.z"})
	if res.OK {
		t.Fatal("expected system-table write to be rejected")
	}
	if !strings.Contains(res.Error, "system table") {
		t.Errorf("expected system-table error, got %q", res.Error)
	}
}

func TestExecuteActionMissingTable(t *testing.T) {
	ctx, _, store, renderer := newTestEnv(t)
	cfg, _ := store.Create(ctx, "p", "", "", nil, minimalSurface,
		`{"add":{"type":"insert","table":"ghost"}}`, "", 0, 0, true, "user", "")

	res := renderer.ExecuteAction(ctx, cfg, "add", map[string]any{"name": "x"})
	if res.OK {
		t.Fatal("expected missing-table error")
	}
	if !strings.Contains(res.Error, "not found") {
		t.Errorf("expected 'not found' error, got %q", res.Error)
	}
}

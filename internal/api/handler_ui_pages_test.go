package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/russellhaering/wasmdb/internal/document"
)

// doJSON runs an authenticated request with an optional JSON body and returns
// the recorder.
func doJSON(t *testing.T, srv *Server, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, method, path, raw))
	return w
}

// a minimal DataTable surface bound to $data "rows".
const uiTestSurface = `{
  "components": [
    {"id": "root", "type": "Column", "children": ["tbl"]},
    {"id": "tbl", "type": "DataTable", "properties": {
      "columns": [{"key": "text", "label": "Text"}],
      "rows": {"$data": "rows"}
    }}
  ]
}`

// query_js that lists a table and echoes params.q for param-flow assertions.
const uiTestQuery = `function handler(params) {
  var docs = db.table("notes").list();
  return { rows: docs.map(function(d){ return { text: d.attributes.text, q: (params && params.q) || "" }; }) };
}`

func TestUIPageCRUDRoundtrip(t *testing.T) {
	srv, token := setupTestServer(t)

	// Create with title + query_js. Use a self-contained query so the create-time
	// render check passes.
	create := map[string]any{
		"name":     "page1",
		"title":    "Page One",
		"query_js": `function handler(p){ return { rows: [] }; }`,
		"surface_json": uiTestSurface,
	}
	w := doJSON(t, srv, token, "POST", "/v1/ui/pages", create)
	if w.Code != 201 {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	if created["name"] != "page1" {
		t.Fatalf("expected name page1, got %v", created["name"])
	}
	if _, hasErr := created["render_error"]; hasErr {
		t.Fatalf("unexpected render_error on create: %v", created["render_error"])
	}

	// Get.
	w = doJSON(t, srv, token, "GET", "/v1/ui/pages/page1", nil)
	if w.Code != 200 {
		t.Fatalf("get: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	if got["title"] != "Page One" {
		t.Fatalf("title mismatch: %v", got["title"])
	}
	if got["generator"] != "user" {
		t.Fatalf("expected generator user, got %v", got["generator"])
	}

	// PATCH only the title; query_js must be preserved.
	w = doJSON(t, srv, token, "PATCH", "/v1/ui/pages/page1", map[string]any{"title": "Renamed"})
	if w.Code != 200 {
		t.Fatalf("patch: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = doJSON(t, srv, token, "GET", "/v1/ui/pages/page1", nil)
	json.Unmarshal(w.Body.Bytes(), &got)
	if got["title"] != "Renamed" {
		t.Fatalf("patch did not update title: %v", got["title"])
	}
	if got["query_js"] != `function handler(p){ return { rows: [] }; }` {
		t.Fatalf("patch did not preserve query_js: %v", got["query_js"])
	}

	// List includes the page with full fields.
	w = doJSON(t, srv, token, "GET", "/v1/ui/pages", nil)
	if w.Code != 200 {
		t.Fatalf("list: %d", w.Code)
	}
	var list []map[string]any
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 || list[0]["surface_json"] == nil {
		t.Fatalf("list did not include full fields: %v", list)
	}

	// Delete, then get → 404.
	w = doJSON(t, srv, token, "DELETE", "/v1/ui/pages/page1", nil)
	if w.Code != 204 {
		t.Fatalf("delete: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	w = doJSON(t, srv, token, "GET", "/v1/ui/pages/page1", nil)
	if w.Code != 404 {
		t.Fatalf("get after delete: expected 404, got %d", w.Code)
	}
	// Delete missing → 404.
	w = doJSON(t, srv, token, "DELETE", "/v1/ui/pages/page1", nil)
	if w.Code != 404 {
		t.Fatalf("delete missing: expected 404, got %d", w.Code)
	}
}

func TestUIPageRenderWithParams(t *testing.T) {
	srv, token := setupTestServer(t)
	ctx := context.Background()

	tbl, err := srv.registry.CreateTable(ctx, "notes", nil)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	for _, txt := range []string{"alpha", "beta"} {
		if err := tbl.PutDocument(ctx, &document.Document{Attributes: map[string]any{"text": txt}}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	create := map[string]any{
		"name":         "notes-page",
		"title":        "Notes",
		"surface_json": uiTestSurface,
		"query_js":     uiTestQuery,
	}
	if w := doJSON(t, srv, token, "POST", "/v1/ui/pages", create); w.Code != 201 {
		t.Fatalf("create: %d: %s", w.Code, w.Body.String())
	}

	// Render with params.q.
	w := doJSON(t, srv, token, "POST", "/v1/ui/pages/notes-page/render", map[string]any{
		"params": map[string]any{"q": "hello"},
	})
	if w.Code != 200 {
		t.Fatalf("render: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Surface any `json:"surface"`
		Data    struct {
			Rows []map[string]any `json:"rows"`
		} `json:"data"`
		Title string `json:"title"`
		Error string `json:"error"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Error != "" {
		t.Fatalf("unexpected render error: %s", res.Error)
	}
	if res.Surface == nil {
		t.Fatal("expected non-nil surface")
	}
	if res.Title != "Notes" {
		t.Fatalf("expected title Notes, got %q", res.Title)
	}
	if len(res.Data.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(res.Data.Rows), res.Data.Rows)
	}
	if res.Data.Rows[0]["q"] != "hello" {
		t.Fatalf("params.q did not reach query_js: %v", res.Data.Rows[0]["q"])
	}
}

func TestUIPageRenderValidationError(t *testing.T) {
	srv, token := setupTestServer(t)

	// Surface with a Text missing its required "value" property → validate phase.
	badSurface := `{"components":[
      {"id":"root","type":"Column","children":["t"]},
      {"id":"t","type":"Text","properties":{}}
    ]}`
	create := map[string]any{"name": "bad", "surface_json": badSurface}
	// Create still succeeds; the response carries render_error.
	w := doJSON(t, srv, token, "POST", "/v1/ui/pages", create)
	if w.Code != 201 {
		t.Fatalf("create: %d: %s", w.Code, w.Body.String())
	}
	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	if created["render_error_phase"] != "validate" {
		t.Fatalf("expected create render_error_phase validate, got %v", created["render_error_phase"])
	}

	// Render returns 200 with error_phase validate (page exists; content failure).
	w = doJSON(t, srv, token, "POST", "/v1/ui/pages/bad/render", nil)
	if w.Code != 200 {
		t.Fatalf("render: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res map[string]any
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["error_phase"] != "validate" {
		t.Fatalf("expected error_phase validate, got %v (body=%s)", res["error_phase"], w.Body.String())
	}
	if res["error"] == nil || res["error"] == "" {
		t.Fatal("expected non-empty error")
	}
}

func TestUIPageActions(t *testing.T) {
	srv, token := setupTestServer(t)
	ctx := context.Background()

	if _, err := srv.registry.CreateTable(ctx, "notes", nil); err != nil {
		t.Fatalf("create table: %v", err)
	}

	create := map[string]any{
		"name":         "notes-page",
		"surface_json": uiTestSurface,
		"query_js":     uiTestQuery,
		"actions_json": `{"add":{"type":"insert","table":"notes"}}`,
	}
	if w := doJSON(t, srv, token, "POST", "/v1/ui/pages", create); w.Code != 201 {
		t.Fatalf("create: %d: %s", w.Code, w.Body.String())
	}

	// Declared insert action → 200 ok:true.
	w := doJSON(t, srv, token, "POST", "/v1/ui/pages/notes-page/actions/add", map[string]any{
		"params": map[string]any{"text": "first note"},
	})
	if w.Code != 200 {
		t.Fatalf("action: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var actRes map[string]any
	json.Unmarshal(w.Body.Bytes(), &actRes)
	if actRes["ok"] != true {
		t.Fatalf("expected ok:true, got %v (body=%s)", actRes["ok"], w.Body.String())
	}

	// The inserted document is visible via a follow-up render.
	w = doJSON(t, srv, token, "POST", "/v1/ui/pages/notes-page/render", nil)
	if w.Code != 200 {
		t.Fatalf("render: %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Data struct {
			Rows []map[string]any `json:"rows"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Data.Rows) != 1 || res.Data.Rows[0]["text"] != "first note" {
		t.Fatalf("inserted note not visible after render: %+v", res.Data.Rows)
	}

	// Undeclared action → 400.
	w = doJSON(t, srv, token, "POST", "/v1/ui/pages/notes-page/actions/bogus", map[string]any{"params": map[string]any{}})
	if w.Code != 400 {
		t.Fatalf("undeclared action: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Missing page → 404.
	w = doJSON(t, srv, token, "POST", "/v1/ui/pages/nope/actions/add", map[string]any{"params": map[string]any{}})
	if w.Code != 404 {
		t.Fatalf("missing page action: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUIPageAuthRequired(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/v1/ui/pages", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("expected 401 without token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOldUIConfigRoutesGone(t *testing.T) {
	srv, token := setupTestServer(t)

	// Authenticated so a 404 proves the route is gone (not an auth rejection).
	w := doJSON(t, srv, token, "GET", "/v1/ui-configs", nil)
	if w.Code != 404 {
		t.Fatalf("expected 404 for removed /v1/ui-configs, got %d: %s", w.Code, w.Body.String())
	}
}

package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/uigen"
)

// TestE2EScaffoldToUserPage exercises the full v2 UI pipeline end to end:
//
//	create typed table (HTTP)
//	  → scaffold sweep (direct Generator.Sweep)
//	  → GET /v1/ui/pages lists tbl-<name> as a scaffold page
//	  → POST /render returns a surface with empty rows
//	  → POST the create action inserts a row (HTTP)
//	  → POST /render now shows the row
//	  → PATCH the title (HTTP, flips generator to "user")
//	  → sweep again: the user-owned page is skipped and its title survives.
//
// The sweeper wiring in main.go is not under test here; the test builds the
// Generator directly against the server's registry/store/renderer.
func TestE2EScaffoldToUserPage(t *testing.T) {
	srv, token := setupTestServer(t)
	ctx := context.Background()

	// 1. Create a typed table over HTTP.
	createTableBody := map[string]any{
		"name": "widgets",
		"schema": map[string]any{
			"fields": []map[string]any{
				{"name": "name", "type": "string", "required": true, "full_text": true},
				{"name": "qty", "type": "int"},
			},
		},
	}
	if w := doJSON(t, srv, token, "POST", "/v1/tables", createTableBody); w.Code != 201 {
		t.Fatalf("create table: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Run a scaffold sweep directly.
	gen := uigen.New(srv.registry, srv.uiConfigStore, srv.uiRenderer)
	res, err := gen.Sweep(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !uigenContains(res.Created, "tbl-widgets") {
		t.Fatalf("expected tbl-widgets created, got created=%v", res.Created)
	}

	// 3. GET /v1/ui/pages lists tbl-widgets as a scaffold page.
	pollUIPage(t, srv, token, "tbl-widgets")
	w := doJSON(t, srv, token, "GET", "/v1/ui/pages", nil)
	if w.Code != 200 {
		t.Fatalf("list pages: %d: %s", w.Code, w.Body.String())
	}
	var list []map[string]any
	json.Unmarshal(w.Body.Bytes(), &list)
	var found map[string]any
	for _, p := range list {
		if p["name"] == "tbl-widgets" {
			found = p
		}
	}
	if found == nil {
		t.Fatalf("tbl-widgets not in page list: %v", list)
	}
	if found["generator"] != "scaffold" {
		t.Fatalf("expected generator scaffold, got %v", found["generator"])
	}

	// 4. Render: surface present, zero rows.
	rows := renderRows(t, srv, token, "tbl-widgets", nil)
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows before insert, got %d: %v", len(rows), rows)
	}

	// 5. Insert a row via the scaffold "create" (insert) action.
	w = doJSON(t, srv, token, "POST", "/v1/ui/pages/tbl-widgets/actions/create", map[string]any{
		"params": map[string]any{"name": "Gizmo", "qty": 3},
	})
	if w.Code != 200 {
		t.Fatalf("create action: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var actRes map[string]any
	json.Unmarshal(w.Body.Bytes(), &actRes)
	if actRes["ok"] != true {
		t.Fatalf("expected action ok:true, got %v (body=%s)", actRes["ok"], w.Body.String())
	}

	// 6. Render again shows the inserted row (poll for index visibility).
	deadline := time.Now().Add(3 * time.Second)
	var got []map[string]any
	for {
		got = renderRows(t, srv, token, "tbl-widgets", nil)
		if len(got) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("row not visible after insert; got %d rows: %v", len(got), got)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got[0]["name"] != "Gizmo" {
		t.Fatalf("inserted row wrong: %v", got[0])
	}

	// 7. PATCH the title over HTTP — flips generator to "user".
	w = doJSON(t, srv, token, "PATCH", "/v1/ui/pages/tbl-widgets", map[string]any{"title": "My Widgets"})
	if w.Code != 200 {
		t.Fatalf("patch: %d: %s", w.Code, w.Body.String())
	}

	// 8. Sweep again: the now user-owned page must be skipped and untouched.
	res2, err := gen.Sweep(ctx)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if !uigenContains(res2.Skipped, "tbl-widgets") {
		t.Fatalf("expected tbl-widgets skipped after user edit, got skipped=%v updated=%v", res2.Skipped, res2.Updated)
	}

	w = doJSON(t, srv, token, "GET", "/v1/ui/pages/tbl-widgets", nil)
	if w.Code != 200 {
		t.Fatalf("get after sweep: %d: %s", w.Code, w.Body.String())
	}
	var final map[string]any
	json.Unmarshal(w.Body.Bytes(), &final)
	if final["title"] != "My Widgets" {
		t.Fatalf("user title did not survive sweep: %v", final["title"])
	}
	if final["generator"] != "user" {
		t.Fatalf("expected generator user, got %v", final["generator"])
	}

	// The inserted row still renders after the sweep (page content intact).
	if rows := renderRows(t, srv, token, "tbl-widgets", nil); len(rows) != 1 {
		t.Fatalf("expected 1 row after sweep, got %d", len(rows))
	}
}

// pollUIPage waits until GET /v1/ui/pages/{name} returns 200 (write indexing is
// asynchronous after Sweep's store writes).
func pollUIPage(t *testing.T, srv *Server, token, name string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		w := doJSON(t, srv, token, "GET", "/v1/ui/pages/"+name, nil)
		if w.Code == 200 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for page %q to be visible (last code %d)", name, w.Code)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// renderRows POSTs to the render endpoint and returns data.rows, failing on any
// render error.
func renderRows(t *testing.T, srv *Server, token, name string, params map[string]any) []map[string]any {
	t.Helper()
	var body any
	if params != nil {
		body = map[string]any{"params": params}
	}
	w := doJSON(t, srv, token, "POST", "/v1/ui/pages/"+name+"/render", body)
	if w.Code != 200 {
		t.Fatalf("render %q: %d: %s", name, w.Code, w.Body.String())
	}
	var res struct {
		Surface any `json:"surface"`
		Data    struct {
			Rows []map[string]any `json:"rows"`
		} `json:"data"`
		Error string `json:"error"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Error != "" {
		t.Fatalf("render %q returned error: %s", name, res.Error)
	}
	if res.Surface == nil {
		t.Fatalf("render %q returned nil surface", name)
	}
	return res.Data.Rows
}

func uigenContains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

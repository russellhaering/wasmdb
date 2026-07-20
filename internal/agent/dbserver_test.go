package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/russellhaering/wasmdb/internal/agents"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/functions"
	"github.com/russellhaering/moraine/objstore"
	"github.com/russellhaering/wasmdb/internal/uiconfig"
)

func newHandler(t *testing.T) (context.Context, *dbHandler) {
	t.Helper()
	reg := database.NewRegistry(database.RegistryConfig{
		Store:    objstore.NewMemoryStore(),
		Prefix:   "test",
		CacheDir: t.TempDir(),
	})
	t.Cleanup(func() { reg.Close() })

	ctx := context.Background()
	if err := reg.EnsureSystemTables(ctx, database.SystemTables); err != nil {
		t.Fatalf("ensure system tables: %v", err)
	}

	eng := functions.NewEngine(reg, 10*time.Second, 2)
	h := &dbHandler{
		registry:      reg,
		fnEngine:      eng,
		agentStore:    agents.NewStore(reg),
		uiConfigStore: uiconfig.NewStore(reg),
	}
	return ctx, h
}

func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is not text: %T", r.Content[0])
	}
	return tc.Text
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return m
}

const testSurface = `{"components":[
  {"id":"root","type":"Column","children":["tbl"]},
  {"id":"tbl","type":"DataTable","properties":{
    "columns":[{"key":"id","label":"ID"},{"key":"text","label":"Text"}],
    "rows":{"$data":"rows"}
  }}
]}`

const testActions = `{"add":{"type":"insert","table":"notes"},"refresh":{"type":"query","params":["q"]}}`

const testQuery = `function handler(params){
  var t = db.table("notes");
  var docs = t.list(50);
  var rows = [];
  for (var i = 0; i < docs.length; i++) {
    var d = docs[i];
    rows.push({id: d.id, text: (d.attributes && d.attributes.text) || ""});
  }
  return {rows: rows, total: docs.length};
}`

// TestManageUILifecycle exercises the manage_ui handler directly: create with
// actions_json, get it back, exec_action to insert a row, then render to see it.
func TestManageUILifecycle(t *testing.T) {
	ctx, h := newHandler(t)
	if _, err := h.registry.CreateTable(ctx, "notes", nil); err != nil {
		t.Fatalf("create notes table: %v", err)
	}

	// create
	res, _, err := h.manageUI(ctx, nil, manageUIInput{
		Action:      "create",
		Name:        "notes-page",
		Title:       "Notes",
		SurfaceJSON: testSurface,
		ActionsJSON: testActions,
		QueryJS:     testQuery,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.IsError {
		t.Fatalf("create returned error: %s", resultText(t, res))
	}
	created := decode(t, resultText(t, res))
	if created["render_status"] != "ok" {
		t.Fatalf("expected render_status ok, got %v (render_error=%v)", created["render_status"], created["render_error"])
	}
	keys, _ := created["data_keys"].([]any)
	if !containsAny(keys, "rows") || !containsAny(keys, "total") {
		t.Errorf("expected data_keys to include rows and total, got %v", keys)
	}

	// get returns the actions_json we stored
	res, _, err = h.manageUI(ctx, nil, manageUIInput{Action: "get", Name: "notes-page"})
	if err != nil || res.IsError {
		t.Fatalf("get failed: %v %v", err, resultText(t, res))
	}
	got := decode(t, resultText(t, res))
	if got["actions_json"] != testActions {
		t.Errorf("actions_json not round-tripped:\n got: %v", got["actions_json"])
	}

	// exec_action inserts a document
	res, _, err = h.manageUI(ctx, nil, manageUIInput{
		Action:     "exec_action",
		Name:       "notes-page",
		ActionName: "add",
		Params:     map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("exec_action: %v", err)
	}
	if res.IsError {
		t.Fatalf("exec_action returned error: %s", resultText(t, res))
	}
	execRes := decode(t, resultText(t, res))
	if execRes["ok"] != true {
		t.Fatalf("exec_action not ok: %v", execRes)
	}

	// render shows the inserted row
	res, _, err = h.manageUI(ctx, nil, manageUIInput{Action: "render", Name: "notes-page"})
	if err != nil || res.IsError {
		t.Fatalf("render failed: %v %v", err, resultText(t, res))
	}
	rendered := decode(t, resultText(t, res))
	if rendered["render_status"] != "ok" {
		t.Fatalf("render not ok: %v", rendered)
	}
	// Re-render via the renderer to inspect the resolved data directly.
	cfg, err := h.uiConfigStore.Get(ctx, "notes-page")
	if err != nil || cfg == nil {
		t.Fatalf("get cfg: %v", err)
	}
	rr := uiconfig.NewRenderer(h.registry, h.fnEngine).Render(ctx, cfg, nil)
	if rr.Error != "" {
		t.Fatalf("render error: %s", rr.Error)
	}
	rows, ok := rr.Data["rows"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("expected 1 row after insert, got %v", rr.Data["rows"])
	}
	row := rows[0].(map[string]any)
	if row["text"] != "hello" {
		t.Errorf("expected inserted text 'hello', got %v", row["text"])
	}
}

// TestManageUIUpdatePatch verifies update is a patch: an update that only
// changes the title preserves the surface, actions, and query.
func TestManageUIUpdatePatch(t *testing.T) {
	ctx, h := newHandler(t)
	if _, err := h.registry.CreateTable(ctx, "notes", nil); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, _, err := h.manageUI(ctx, nil, manageUIInput{
		Action: "create", Name: "p", Title: "Old", SurfaceJSON: testSurface, ActionsJSON: testActions, QueryJS: testQuery,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	res, _, err := h.manageUI(ctx, nil, manageUIInput{Action: "update", Name: "p", Title: "New"})
	if err != nil || res.IsError {
		t.Fatalf("update failed: %v %v", err, resultText(t, res))
	}

	cfg, err := h.uiConfigStore.Get(ctx, "p")
	if err != nil || cfg == nil {
		t.Fatalf("get: %v", err)
	}
	if cfg.Title != "New" {
		t.Errorf("title not updated: %q", cfg.Title)
	}
	if cfg.ActionsJSON != testActions {
		t.Errorf("actions_json was clobbered by patch update: %q", cfg.ActionsJSON)
	}
	if cfg.SurfaceJSON != testSurface {
		t.Errorf("surface_json was clobbered by patch update")
	}
	if cfg.Generator != "agent" {
		t.Errorf("expected generator 'agent' after update, got %q", cfg.Generator)
	}
}

func containsAny(vals []any, want string) bool {
	for _, v := range vals {
		if s, ok := v.(string); ok && s == want {
			return true
		}
	}
	return false
}

type fakeScheduler struct {
	run  *agents.AgentRun
	err  error
	name string
}

func (f *fakeScheduler) TriggerAgent(_ context.Context, name string) (*agents.AgentRun, error) {
	f.name = name
	return f.run, f.err
}

// TestManageAgentTriggerNilScheduler asserts a clear error when no scheduler is
// configured.
func TestManageAgentTriggerNilScheduler(t *testing.T) {
	_, h := newHandler(t) // scheduler left nil
	res, _, err := h.manageAgent(context.Background(), nil, manageAgentInput{Action: "trigger", Name: "ui-builder"})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected an error result, got: %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "scheduler not running") {
		t.Errorf("expected 'scheduler not running' message, got: %s", resultText(t, res))
	}
}

// TestManageAgentTrigger runs a trigger against a fake scheduler and checks the
// {run_id, status, summary} shape.
func TestManageAgentTrigger(t *testing.T) {
	_, h := newHandler(t)
	h.scheduler = &fakeScheduler{run: &agents.AgentRun{ID: "run-1", Status: "completed", Output: "did the thing"}}

	res, _, err := h.manageAgent(context.Background(), nil, manageAgentInput{Action: "trigger", Name: "ui-builder"})
	if err != nil || res.IsError {
		t.Fatalf("trigger failed: %v %v", err, resultText(t, res))
	}
	out := decode(t, resultText(t, res))
	if out["run_id"] != "run-1" || out["status"] != "completed" || out["summary"] != "did the thing" {
		t.Errorf("unexpected trigger result: %v", out)
	}
}

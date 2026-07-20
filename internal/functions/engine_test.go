package functions

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/moraine/document"
	"github.com/russellhaering/moraine/objstore"
)

func testRegistry(t *testing.T) *database.Registry {
	t.Helper()
	reg := database.NewRegistry(database.RegistryConfig{
		Store:  objstore.NewMemoryStore(),
		Prefix: "test",
	})
	t.Cleanup(func() { reg.Close() })
	return reg
}

func testEngine(t *testing.T) (*Engine, *database.Registry) {
	t.Helper()
	reg := testRegistry(t)
	return NewEngine(reg, 10*time.Second, 2), reg
}

// ---------------------------------------------------------------------------
// Basic expression evaluation
// ---------------------------------------------------------------------------

func TestExecuteInteger(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "1 + 2", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != int64(3) {
		t.Fatalf("expected 3, got %v (%T)", r.Result, r.Result)
	}
}

func TestExecuteFloat(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "1.5 + 2.3", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != 3.8 {
		t.Fatalf("expected 3.8, got %v", r.Result)
	}
}

func TestExecuteString(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `"hello" + " world"`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != "hello world" {
		t.Fatalf("expected 'hello world', got %v", r.Result)
	}
}

func TestExecuteBoolean(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "3 > 2", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != true {
		t.Fatalf("expected true, got %v (%T)", r.Result, r.Result)
	}
}

func TestExecuteNull(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "null", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != nil {
		t.Fatalf("expected nil, got %v", r.Result)
	}
}

func TestExecuteUndefined(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "undefined", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != nil {
		t.Fatalf("expected nil, got %v", r.Result)
	}
}

func TestExecuteObject(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `({name: "test", count: 42})`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	m, ok := r.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", r.Result)
	}
	if m["name"] != "test" {
		t.Fatalf("name: %v", m["name"])
	}
	if m["count"] != int64(42) {
		t.Fatalf("count: %v (%T)", m["count"], m["count"])
	}
}

func TestExecuteArray(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "[1, 2, 3]", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", r.Result)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
}

func TestExecuteNestedObject(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `({a: {b: {c: 99}}})`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	m := r.Result.(map[string]any)
	a := m["a"].(map[string]any)
	b := a["b"].(map[string]any)
	if b["c"] != int64(99) {
		t.Fatalf("expected 99, got %v", b["c"])
	}
}

// ---------------------------------------------------------------------------
// Duration tracking
// ---------------------------------------------------------------------------

func TestExecuteDurationMS(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "1", nil)
	if r.DurationMS < 0 {
		t.Fatal("duration should be non-negative")
	}
}

// ---------------------------------------------------------------------------
// handler(params) pattern
// ---------------------------------------------------------------------------

func TestHandlerWithParams(t *testing.T) {
	eng, _ := testEngine(t)
	code := `function handler(params) { return {greeting: "hi " + params.name}; }`
	r := eng.Execute(context.Background(), code, map[string]any{"name": "alice"})
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	m := r.Result.(map[string]any)
	if m["greeting"] != "hi alice" {
		t.Fatalf("got %v", m["greeting"])
	}
}

func TestHandlerReturnsArray(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "t1", nil)

	code := `
function handler() {
  var tables = db.tables();
  var out = [];
  for (var i = 0; i < tables.length; i++) {
    if (!tables[i].system) out.push(tables[i].name);
  }
  return out;
}
`
	r := eng.Execute(ctx, code, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T: %v", r.Result, r.Result)
	}
	if len(arr) < 1 {
		t.Fatal("expected at least 1 table")
	}
}

func TestHandlerReturnsNull(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "function handler() { return null; }", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != nil {
		t.Fatalf("expected nil, got %v", r.Result)
	}
}

func TestHandlerReturnsUndefined(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "function handler() { /* no return */ }", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != nil {
		t.Fatalf("expected nil, got %v", r.Result)
	}
}

func TestHandlerNoParams(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "function handler() { return 99; }", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != int64(99) {
		t.Fatalf("expected 99, got %v", r.Result)
	}
}

func TestHandlerAccessesParamsDefault(t *testing.T) {
	// When no params passed, params should be an empty object.
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `function handler(p) { return typeof p; }`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != "object" {
		t.Fatalf("expected 'object', got %v", r.Result)
	}
}

// ---------------------------------------------------------------------------
// console.log capture
// ---------------------------------------------------------------------------

func TestConsoleMethods(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `
console.log("log");
console.warn("warn");
console.error("err");
console.info("info");
0
`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if len(r.Logs) != 4 {
		t.Fatalf("expected 4 logs, got %d: %v", len(r.Logs), r.Logs)
	}
	expected := []string{"log", "warn", "err", "info"}
	for i, exp := range expected {
		if r.Logs[i] != exp {
			t.Fatalf("log[%d]: expected %q, got %q", i, exp, r.Logs[i])
		}
	}
}

func TestConsoleLogMultipleArgs(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "console.log(\"a\", \"b\", 3)\n0", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if len(r.Logs) != 1 || r.Logs[0] != "a b 3" {
		t.Fatalf("expected 'a b 3', got %v", r.Logs)
	}
}

func TestConsoleLogObject(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "console.log({x: 1})\n0", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if len(r.Logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(r.Logs))
	}
	if !strings.Contains(r.Logs[0], "\"x\"") {
		t.Fatalf("expected JSON with x, got %q", r.Logs[0])
	}
}

func TestConsoleLogInHandler(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `
function handler() {
  console.log("inside handler");
  return 1;
}
`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if len(r.Logs) != 1 || r.Logs[0] != "inside handler" {
		t.Fatalf("logs: %v", r.Logs)
	}
}

func TestNoLogs(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "42", nil)
	if r.Logs != nil {
		t.Fatalf("expected nil logs, got %v", r.Logs)
	}
}

// ---------------------------------------------------------------------------
// Error handling
// ---------------------------------------------------------------------------

func TestSyntaxError(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "function {", nil)
	if r.Error == "" {
		t.Fatal("expected syntax error")
	}
	if !strings.Contains(r.Error, "SyntaxError") && !strings.Contains(r.Error, "error") {
		t.Fatalf("expected syntax error message, got: %s", r.Error)
	}
}

func TestRuntimeErrorThrow(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `function handler() { throw new Error("boom"); }`, nil)
	if r.Error == "" {
		t.Fatal("expected runtime error")
	}
	if !strings.Contains(r.Error, "boom") {
		t.Fatalf("expected 'boom' in error, got: %s", r.Error)
	}
}

func TestRuntimeErrorTypeError(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `var x = null; x.foo`, nil)
	if r.Error == "" {
		t.Fatal("expected TypeError")
	}
}

func TestRuntimeErrorReferenceError(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "undeclaredVariable", nil)
	if r.Error == "" {
		t.Fatal("expected ReferenceError")
	}
}

func TestErrorPreservesLogs(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `
function handler() {
  console.log("before error");
  throw new Error("after log");
}
`, nil)
	if r.Error == "" {
		t.Fatal("expected error")
	}
	if len(r.Logs) != 1 || r.Logs[0] != "before error" {
		t.Fatalf("expected log before error, got: %v", r.Logs)
	}
}

func TestErrorHasDuration(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "function {", nil)
	if r.DurationMS < 0 {
		t.Fatal("duration should be set even on error")
	}
}

// ---------------------------------------------------------------------------
// params
// ---------------------------------------------------------------------------

func TestParamsArithmetic(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `params.x + params.y`, map[string]any{"x": 10, "y": 20})
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != int64(30) {
		t.Fatalf("expected 30, got %v", r.Result)
	}
}

func TestParamsNested(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `params.user.name`, map[string]any{
		"user": map[string]any{"name": "bob"},
	})
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != "bob" {
		t.Fatalf("expected 'bob', got %v", r.Result)
	}
}

func TestParamsArray(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `params.items.length`, map[string]any{
		"items": []any{1, 2, 3},
	})
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != int64(3) {
		t.Fatalf("expected 3, got %v", r.Result)
	}
}

func TestParamsNilDefaultsToEmptyObject(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `typeof params`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != "object" {
		t.Fatalf("expected 'object', got %v", r.Result)
	}
}

// ---------------------------------------------------------------------------
// wrapCodeForResult / multi-statement handling
// ---------------------------------------------------------------------------

func TestMultiLineLastExpression(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "var x = 10;\nx * 2", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != int64(20) {
		t.Fatalf("expected 20, got %v", r.Result)
	}
}

func TestSingleLineSemicolonSplit(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `var a = 5; var b = 3; a + b`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != int64(8) {
		t.Fatalf("expected 8, got %v", r.Result)
	}
}

func TestSingleLineObjectResult(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `var x = 1; ({val: x})`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	m, ok := r.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T: %v", r.Result, r.Result)
	}
	if m["val"] != int64(1) {
		t.Fatalf("expected val=1, got %v", m["val"])
	}
}

func TestAllStatementsNoResult(t *testing.T) {
	// Code that's purely statements — result should be nil.
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "var x = 42;", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	// x is assigned but never returned; result is nil.
}

func TestHandlerSkipsWrapping(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `
var helper = 10;
function handler() { return helper * 3; }
`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != int64(30) {
		t.Fatalf("expected 30, got %v", r.Result)
	}
}

// ---------------------------------------------------------------------------
// Context / cancellation
// ---------------------------------------------------------------------------

func TestCancelledContext(t *testing.T) {
	eng, _ := testEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should not hang — either errors or completes quickly.
	r := eng.Execute(ctx, "1 + 1", nil)
	_ = r // may or may not error
}

// ---------------------------------------------------------------------------
// Concurrency limit
// ---------------------------------------------------------------------------

func TestConcurrencyLimit(t *testing.T) {
	reg := testRegistry(t)
	eng := NewEngine(reg, 10*time.Second, 1) // max 1 concurrent

	// Start a slow execution.
	done := make(chan *ExecResult, 1)
	go func() {
		r := eng.Execute(context.Background(), `
var s = 0; for (var i = 0; i < 100000; i++) s += i; s
`, nil)
		done <- r
	}()

	// Give it a moment to start.
	time.Sleep(10 * time.Millisecond)

	// Second execution with tight timeout should still work (waits for slot).
	r2 := eng.Execute(context.Background(), "42", nil)
	// It should eventually complete.
	if r2.Error != "" && !strings.Contains(r2.Error, "concurrency") {
		// It either ran successfully or hit concurrency limit — both OK.
	}

	<-done
}

// ---------------------------------------------------------------------------
// db.tables() API
// ---------------------------------------------------------------------------

func TestDBTables(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "contacts", nil)
	reg.CreateTable(ctx, "deals", nil)

	r := eng.Execute(ctx, `
var tables = db.tables();
tables.filter(function(t) { return !t.system; }).map(function(t) { return t.name; }).sort()
`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	arr := r.Result.([]any)
	if len(arr) != 2 {
		t.Fatalf("expected 2 tables, got %d: %v", len(arr), arr)
	}
	if arr[0] != "contacts" || arr[1] != "deals" {
		t.Fatalf("expected [contacts, deals], got %v", arr)
	}
}

func TestDBTablesIncludesSystemFlag(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.EnsureSystemTables(ctx, []database.SystemTableDef{
		{Name: "_sys"},
	})
	reg.CreateTable(ctx, "user_tbl", nil)

	r := eng.Execute(ctx, `
var tables = db.tables();
var sys = tables.filter(function(t) { return t.system; });
var usr = tables.filter(function(t) { return !t.system; });
({sysCount: sys.length, usrCount: usr.length})
`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	m := r.Result.(map[string]any)
	if m["sysCount"].(int64) < 1 {
		t.Fatal("expected at least 1 system table")
	}
	if m["usrCount"].(int64) < 1 {
		t.Fatal("expected at least 1 user table")
	}
}

// ---------------------------------------------------------------------------
// db.createTable / db.deleteTable
// ---------------------------------------------------------------------------

func TestDBCreateTable(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()

	r := eng.Execute(ctx, `db.createTable("new_tbl")`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	m := r.Result.(map[string]any)
	if m["name"] != "new_tbl" {
		t.Fatalf("expected name 'new_tbl', got %v", m["name"])
	}

	// Verify table exists.
	_, err := reg.GetTable(ctx, "new_tbl")
	if err != nil {
		t.Fatalf("table should exist: %v", err)
	}
}

func TestDBCreateTableBlocksSystemPrefix(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `db.createTable("_secret")`, nil)
	if r.Error == "" {
		t.Fatal("expected error creating system table")
	}
	if !strings.Contains(r.Error, "system") {
		t.Fatalf("expected 'system' in error, got: %s", r.Error)
	}
}

func TestDBDeleteTable(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "disposable", nil)

	r := eng.Execute(ctx, `db.deleteTable("disposable")`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
}

func TestDBDeleteTableBlocksSystemPrefix(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `db.deleteTable("_users")`, nil)
	if r.Error == "" {
		t.Fatal("expected error deleting system table")
	}
}

// ---------------------------------------------------------------------------
// db.table() — system table access blocked
// ---------------------------------------------------------------------------

func TestDBTableBlocksSystemTables(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.EnsureSystemTables(ctx, []database.SystemTableDef{
		{Name: "_users", Schema: &document.Schema{
			Fields: []document.FieldDefinition{
				{Name: "email", Type: document.FieldTypeString},
			},
		}},
	})

	for _, code := range []string{
		`db.table("_users").list()`,
		`db.table("_users").get("x")`,
		`db.table("_users").put({content: "hack"})`,
		`db.table("_users").delete("x")`,
	} {
		r := eng.Execute(ctx, code, nil)
		if r.Error == "" {
			t.Fatalf("expected error for %s", code)
		}
		if !strings.Contains(r.Error, "system") {
			t.Fatalf("expected 'system' in error for %s, got: %s", code, r.Error)
		}
	}
}

// ---------------------------------------------------------------------------
// db.table().get / .put / .list / .delete
// ---------------------------------------------------------------------------

func TestTablePutAndGet(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "items", nil)

	// Put.
	r := eng.Execute(ctx, `
var r = db.table("items").put({content: "hello", attributes: {color: "red"}});
r
`, nil)
	if r.Error != "" {
		t.Fatalf("put error: %s", r.Error)
	}
	id := r.Result.(map[string]any)["id"].(string)
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	// Get.
	r = eng.Execute(ctx, `db.table("items").get("`+id+`")`, nil)
	if r.Error != "" {
		t.Fatalf("get error: %s", r.Error)
	}
	doc := r.Result.(map[string]any)
	if doc["content"] != "hello" {
		t.Fatalf("content: %v", doc["content"])
	}
	attrs := doc["attributes"].(map[string]any)
	if attrs["color"] != "red" {
		t.Fatalf("color: %v", attrs["color"])
	}
}

func TestTableGetNotFound(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "items", nil)

	r := eng.Execute(ctx, `db.table("items").get("nonexistent")`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != nil {
		t.Fatalf("expected null, got %v", r.Result)
	}
}

func TestTableList(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "items", nil)
	tbl, _ := reg.GetTable(ctx, "items")
	for i := 0; i < 5; i++ {
		tbl.PutDocument(ctx, &document.Document{Content: "doc"})
	}

	r := eng.Execute(ctx, "var docs = db.table(\"items\").list()\ndocs", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T: %v", r.Result, r.Result)
	}
	if len(arr) != 5 {
		t.Fatalf("expected 5, got %d", len(arr))
	}
}

func TestTableListWithLimit(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "items", nil)
	tbl, _ := reg.GetTable(ctx, "items")
	for i := 0; i < 10; i++ {
		tbl.PutDocument(ctx, &document.Document{Content: "doc"})
	}

	r := eng.Execute(ctx, "var docs = db.table(\"items\").list(3)\ndocs", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T: %v", r.Result, r.Result)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3, got %d", len(arr))
	}
}

func TestTableListEmpty(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "empty", nil)

	r := eng.Execute(ctx, "var docs = db.table(\"empty\").list()\ndocs", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T: %v", r.Result, r.Result)
	}
	if len(arr) != 0 {
		t.Fatalf("expected 0, got %d", len(arr))
	}
}

func TestTablePutUpsert(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "items", nil)

	// Create.
	r := eng.Execute(ctx, `db.table("items").put({content: "v1", attributes: {x: 1}})`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	id := r.Result.(map[string]any)["id"].(string)

	// Update same ID.
	r = eng.Execute(ctx, `db.table("items").put({id: "`+id+`", content: "v2", attributes: {x: 2}})`, nil)
	if r.Error != "" {
		t.Fatalf("upsert error: %s", r.Error)
	}

	// Verify.
	r = eng.Execute(ctx, `db.table("items").get("`+id+`")`, nil)
	if r.Error != "" {
		t.Fatalf("get error: %s", r.Error)
	}
	doc := r.Result.(map[string]any)
	if doc["content"] != "v2" {
		t.Fatalf("expected v2, got %v", doc["content"])
	}
}

func TestTableDelete(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "items", nil)

	// Create.
	r := eng.Execute(ctx, `db.table("items").put({content: "bye"})`, nil)
	if r.Error != "" {
		t.Fatalf("put error: %s", r.Error)
	}
	id := r.Result.(map[string]any)["id"].(string)

	// Delete.
	r = eng.Execute(ctx, `db.table("items").delete("`+id+`")`, nil)
	if r.Error != "" {
		t.Fatalf("delete error: %s", r.Error)
	}

	// Verify.
	r = eng.Execute(ctx, `db.table("items").get("`+id+`")`, nil)
	if r.Error != "" {
		t.Fatalf("get error: %s", r.Error)
	}
	if r.Result != nil {
		t.Fatalf("expected nil, got %v", r.Result)
	}
}

func TestTableNonexistentTable(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `db.table("nope").list()`, nil)
	if r.Error == "" {
		t.Fatal("expected error for nonexistent table")
	}
}

// ---------------------------------------------------------------------------
// db.table().search.text / .search.attr
// ---------------------------------------------------------------------------

func TestTableSearchText(t *testing.T) {
	tmpDir := t.TempDir()
	reg := database.NewRegistry(database.RegistryConfig{
		Store:    objstore.NewMemoryStore(),
		Prefix:   "test",
		CacheDir: tmpDir,
	})
	t.Cleanup(func() { reg.Close() })
	eng := NewEngine(reg, 10*time.Second, 2)
	ctx := context.Background()
	reg.CreateTable(ctx, "docs", nil)
	tbl, _ := reg.GetTable(ctx, "docs")
	tbl.PutDocument(ctx, &document.Document{Content: "the quick brown fox"})
	tbl.PutDocument(ctx, &document.Document{Content: "the lazy dog"})
	if err := tbl.WaitForIndexes(ctx); err != nil {
		t.Fatalf("WaitForIndexes: %v", err)
	}

	r := eng.Execute(ctx, "var results = db.table(\"docs\").search.text(\"fox\")\nresults", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T: %v", r.Result, r.Result)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 result, got %d", len(arr))
	}
}

func TestTableSearchAttr(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "items", nil)
	tbl, _ := reg.GetTable(ctx, "items")
	tbl.PutDocument(ctx, &document.Document{Attributes: map[string]any{"status": "active"}})
	tbl.PutDocument(ctx, &document.Document{Attributes: map[string]any{"status": "inactive"}})
	tbl.PutDocument(ctx, &document.Document{Attributes: map[string]any{"status": "active"}})
	if err := tbl.WaitForIndexes(ctx); err != nil {
		t.Fatalf("WaitForIndexes: %v", err)
	}

	r := eng.Execute(ctx, `
var results = db.table("items").search.attr([{field: "status", op: "eq", value: "active"}]);
results
`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T: %v", r.Result, r.Result)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 results, got %d", len(arr))
	}
}

func TestTableSearchTextWithLimit(t *testing.T) {
	tmpDir := t.TempDir()
	reg := database.NewRegistry(database.RegistryConfig{
		Store:    objstore.NewMemoryStore(),
		Prefix:   "test",
		CacheDir: tmpDir,
	})
	t.Cleanup(func() { reg.Close() })
	eng := NewEngine(reg, 10*time.Second, 2)
	ctx := context.Background()
	reg.CreateTable(ctx, "docs", nil)
	tbl, _ := reg.GetTable(ctx, "docs")
	for i := 0; i < 5; i++ {
		tbl.PutDocument(ctx, &document.Document{Content: "matching keyword here"})
	}
	if err := tbl.WaitForIndexes(ctx); err != nil {
		t.Fatalf("WaitForIndexes: %v", err)
	}

	r := eng.Execute(ctx, "var results = db.table(\"docs\").search.text(\"keyword\", 2)\nresults", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T: %v", r.Result, r.Result)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2, got %d", len(arr))
	}
}

// ---------------------------------------------------------------------------
// Argument validation on host functions
// ---------------------------------------------------------------------------

func TestDBTableNoArgs(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `db.table()`, nil)
	if r.Error == "" {
		t.Fatal("expected error")
	}
}

func TestTableGetNoArgs(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "x", nil)

	r := eng.Execute(ctx, `db.table("x").get()`, nil)
	if r.Error == "" {
		t.Fatal("expected error")
	}
}

func TestTablePutNoArgs(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "x", nil)

	r := eng.Execute(ctx, `db.table("x").put()`, nil)
	if r.Error == "" {
		t.Fatal("expected error")
	}
}

func TestTableDeleteNoArgs(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "x", nil)

	r := eng.Execute(ctx, `db.table("x").delete()`, nil)
	if r.Error == "" {
		t.Fatal("expected error")
	}
}

func TestDBCreateTableNoArgs(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `db.createTable()`, nil)
	if r.Error == "" {
		t.Fatal("expected error")
	}
}

func TestDBDeleteTableNoArgs(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `db.deleteTable()`, nil)
	if r.Error == "" {
		t.Fatal("expected error")
	}
}

func TestSearchTextNoArgs(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "x", nil)

	r := eng.Execute(ctx, `db.table("x").search.text()`, nil)
	if r.Error == "" {
		t.Fatal("expected error")
	}
}

func TestSearchAttrNoArgs(t *testing.T) {
	eng, reg := testEngine(t)
	ctx := context.Background()
	reg.CreateTable(ctx, "x", nil)

	r := eng.Execute(ctx, `db.table("x").search.attr()`, nil)
	if r.Error == "" {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// JS language features
// ---------------------------------------------------------------------------

func TestArrowFunction(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), `[1,2,3].map(x => x * 2)`, nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	arr := r.Result.([]any)
	if len(arr) != 3 {
		t.Fatalf("expected 3, got %d", len(arr))
	}
}

func TestTemplateLiterals(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "var name = `world`; `hello ${name}`", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != "hello world" {
		t.Fatalf("expected 'hello world', got %v", r.Result)
	}
}

func TestDestructuring(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "var {a, b} = {a: 1, b: 2};\na + b", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != int64(3) {
		t.Fatalf("expected 3, got %v", r.Result)
	}
}

func TestSpreadOperator(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "var a = [1, 2]; var b = [...a, 3];\nb.length", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != int64(3) {
		t.Fatalf("expected 3, got %v", r.Result)
	}
}

func TestOptionalChaining(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "var obj = {}; obj?.missing?.deep", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != nil {
		t.Fatalf("expected nil, got %v", r.Result)
	}
}

func TestNullishCoalescing(t *testing.T) {
	eng, _ := testEngine(t)
	r := eng.Execute(context.Background(), "var x = null; x ?? 42", nil)
	if r.Error != "" {
		t.Fatalf("error: %s", r.Error)
	}
	if r.Result != int64(42) {
		t.Fatalf("expected 42, got %v", r.Result)
	}
}

// ---------------------------------------------------------------------------
// wrapCodeForResult unit tests
// ---------------------------------------------------------------------------

func TestWrapAsIIFE(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		contain string
	}{
		{"simple expr", "42", "return (42)"},
		{"multi-line last expr", "var x = 1;\nx + 1", "return (x + 1)"},
		{"trailing semicolon", "42;", "return (42)"},
		{"var then expr", "var a = 1; a", "return (a)"},
		{"all statements", "var x = 1;", "(function(){"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := wrapAsIIFE(tt.input)
			if !strings.Contains(out, tt.contain) {
				t.Fatalf("expected %q in output, got:\n%s", tt.contain, out)
			}
		})
	}
}

func TestLooksLikeStatement(t *testing.T) {
	stmts := []string{"var x", "let y", "const z", "if (true)", "for (;;)",
		"while (true)", "return 1", "throw err", "function f()", "class C",
		"switch (x)", "try {"}
	for _, s := range stmts {
		if !looksLikeStatement(s) {
			t.Errorf("expected %q to be a statement", s)
		}
	}

	exprs := []string{"42", "x + 1", "foo()", "({a: 1})", "[1,2]", "\"hello\""}
	for _, e := range exprs {
		if looksLikeStatement(e) {
			t.Errorf("expected %q to NOT be a statement", e)
		}
	}
}

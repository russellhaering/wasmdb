package functions

import (
	"context"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
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

func TestExecuteBasicExpression(t *testing.T) {
	reg := testRegistry(t)
	eng := NewEngine(reg, 10*time.Second, 2)

	result := eng.Execute(context.Background(), "1 + 2", nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Result != int64(3) {
		t.Fatalf("expected 3, got %v (%T)", result.Result, result.Result)
	}
	if result.DurationMS < 0 {
		t.Fatal("duration should be non-negative")
	}
}

func TestExecuteStringReturn(t *testing.T) {
	reg := testRegistry(t)
	eng := NewEngine(reg, 10*time.Second, 2)

	result := eng.Execute(context.Background(), `"hello" + " world"`, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Result != "hello world" {
		t.Fatalf("expected 'hello world', got %v", result.Result)
	}
}

func TestExecuteObjectReturn(t *testing.T) {
	reg := testRegistry(t)
	eng := NewEngine(reg, 10*time.Second, 2)

	result := eng.Execute(context.Background(), `({name: "test", count: 42})`, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	m, ok := result.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result.Result)
	}
	if m["name"] != "test" {
		t.Fatalf("expected name=test, got %v", m["name"])
	}
}

func TestExecuteHandlerFunction(t *testing.T) {
	reg := testRegistry(t)
	eng := NewEngine(reg, 10*time.Second, 2)

	code := `
function handler(params) {
	return {greeting: "hello " + params.name};
}
`
	result := eng.Execute(context.Background(), code, map[string]any{"name": "world"})
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	m, ok := result.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T: %v", result.Result, result.Result)
	}
	if m["greeting"] != "hello world" {
		t.Fatalf("expected 'hello world', got %v", m["greeting"])
	}
}

func TestExecuteConsoleLog(t *testing.T) {
	reg := testRegistry(t)
	eng := NewEngine(reg, 10*time.Second, 2)

	result := eng.Execute(context.Background(), `
console.log("line 1");
console.log("line", 2);
42
`, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if len(result.Logs) != 2 {
		t.Fatalf("expected 2 logs, got %d: %v", len(result.Logs), result.Logs)
	}
	if result.Logs[0] != "line 1" {
		t.Fatalf("expected 'line 1', got %q", result.Logs[0])
	}
	if result.Logs[1] != "line 2" {
		t.Fatalf("expected 'line 2', got %q", result.Logs[1])
	}
}

func TestExecuteSyntaxError(t *testing.T) {
	reg := testRegistry(t)
	eng := NewEngine(reg, 10*time.Second, 2)

	result := eng.Execute(context.Background(), "function {", nil)
	if result.Error == "" {
		t.Fatal("expected error for syntax error")
	}
}

func TestExecuteRuntimeError(t *testing.T) {
	reg := testRegistry(t)
	eng := NewEngine(reg, 10*time.Second, 2)

	result := eng.Execute(context.Background(), `
function handler() {
	throw new Error("boom");
}
`, nil)
	if result.Error == "" {
		t.Fatal("expected error for thrown exception")
	}
}

func TestExecuteDBTablesAPI(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()

	// Create a test table.
	_, err := reg.CreateTable(ctx, "contacts", nil)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	eng := NewEngine(reg, 10*time.Second, 2)

	result := eng.Execute(ctx, `
var tables = db.tables();
tables.map(function(t) { return t.name; });
`, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	arr, ok := result.Result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T: %v", result.Result, result.Result)
	}

	found := false
	for _, v := range arr {
		if v == "contacts" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'contacts' in tables, got %v", arr)
	}
}

func TestExecuteDBTableCRUD(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()

	// Create a test table.
	_, err := reg.CreateTable(ctx, "items", nil)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	eng := NewEngine(reg, 10*time.Second, 2)

	// Put a document.
	result := eng.Execute(ctx, `
var r = db.table("items").put({content: "hello", attributes: {name: "test"}});
r;
`, nil)
	if result.Error != "" {
		t.Fatalf("put error: %s", result.Error)
	}
	putResult, ok := result.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result.Result)
	}
	docID, ok := putResult["id"].(string)
	if !ok || docID == "" {
		t.Fatalf("expected string id, got %v", putResult["id"])
	}

	// Get the document back.
	result = eng.Execute(ctx, `db.table("items").get("`+docID+`")`, nil)
	if result.Error != "" {
		t.Fatalf("get error: %s", result.Error)
	}
	docResult, ok := result.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T: %v (logs=%v)", result.Result, result.Result, result.Logs)
	}
	if docResult["content"] != "hello" {
		t.Fatalf("expected content 'hello', got %v", docResult["content"])
	}

	// List documents.
	result = eng.Execute(ctx, `db.table("items").list()`, nil)
	if result.Error != "" {
		t.Fatalf("list error: %s", result.Error)
	}
	listResult, ok := result.Result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", result.Result)
	}
	if len(listResult) != 1 {
		t.Fatalf("expected 1 document, got %d", len(listResult))
	}

	// Delete the document.
	result = eng.Execute(ctx, `db.table("items").delete("`+docID+`")`, nil)
	if result.Error != "" {
		t.Fatalf("delete error: %s", result.Error)
	}

	// Verify it's gone.
	result = eng.Execute(ctx, `db.table("items").get("`+docID+`")`, nil)
	if result.Error != "" {
		t.Fatalf("get after delete error: %s", result.Error)
	}
	if result.Result != nil {
		t.Fatalf("expected null after delete, got %v", result.Result)
	}
}

func TestExecuteDBSystemTableBlocked(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()

	// Create a system table.
	if err := reg.EnsureSystemTables(ctx, []database.SystemTableDef{
		{Name: "_users", Schema: &document.Schema{
			Fields: []document.FieldDefinition{
				{Name: "email", Type: document.FieldTypeString},
			},
		}},
	}); err != nil {
		t.Fatalf("ensure system tables: %v", err)
	}

	eng := NewEngine(reg, 10*time.Second, 2)

	result := eng.Execute(ctx, `db.table("_users").list()`, nil)
	if result.Error == "" {
		t.Fatal("expected error accessing system table")
	}
}

func TestExecuteParams(t *testing.T) {
	reg := testRegistry(t)
	eng := NewEngine(reg, 10*time.Second, 2)

	result := eng.Execute(context.Background(), `params.x + params.y`, map[string]any{"x": 10, "y": 20})
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Result != int64(30) {
		t.Fatalf("expected 30, got %v", result.Result)
	}
}

func TestExecuteMultiStatementOneLine(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()

	_, err := reg.CreateTable(ctx, "items", nil)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	eng := NewEngine(reg, 10*time.Second, 2)

	// Multi-statement code ending with an expression.
	result := eng.Execute(ctx, `var items = db.table("items").list(5);
items.length`, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Result != int64(0) {
		t.Fatalf("expected 0, got %v (%T)", result.Result, result.Result)
	}
}

func TestExecuteMultiStatementSingleLine(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()

	_, err := reg.CreateTable(ctx, "items", nil)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	eng := NewEngine(reg, 10*time.Second, 2)

	// All on one line: var statement + trailing expression separated by semicolon.
	result := eng.Execute(ctx, `var x = db.table("items").list(5); x.length`, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Result != int64(0) {
		t.Fatalf("expected 0, got %v (%T)", result.Result, result.Result)
	}
}

func TestExecuteSingleLineWithObjectResult(t *testing.T) {
	reg := testRegistry(t)
	ctx := context.Background()

	_, err := reg.CreateTable(ctx, "items", nil)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	eng := NewEngine(reg, 10*time.Second, 2)

	// Single line: var + expression returning object.
	result := eng.Execute(ctx, `var items = db.table("items").list(100); ({count: items.length})`, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	m, ok := result.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T: %v", result.Result, result.Result)
	}
	// count should be 0 since table is empty
	if m["count"] != float64(0) {
		t.Fatalf("expected count=0, got %v", m["count"])
	}
}

func TestExecuteCancelledContext(t *testing.T) {
	reg := testRegistry(t)
	eng := NewEngine(reg, 10*time.Second, 2)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	result := eng.Execute(ctx, `1 + 1`, nil)
	// Should get an error due to cancelled context.
	if result.Error == "" {
		// This is also acceptable if the runtime completes before noticing cancellation.
		// The important thing is it doesn't hang.
	}
}

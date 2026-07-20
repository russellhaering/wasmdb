package uigen

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/functions"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
	"github.com/russellhaering/wasmdb/internal/uiconfig"
)

var update = flag.Bool("update", false, "update golden files")

// newTestGen builds an in-memory registry (with system tables ensured so
// _ui_configs exists), a JS engine, a UI config store and renderer, and a
// Generator bound to them.
func newTestGen(t *testing.T) (context.Context, *database.Registry, *uiconfig.Store, *uiconfig.Renderer, *Generator) {
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
	store := uiconfig.NewStore(reg)
	renderer := uiconfig.NewRenderer(reg, eng)
	gen := New(reg, store, renderer)
	return ctx, reg, store, renderer, gen
}

func createTable(t *testing.T, ctx context.Context, reg *database.Registry, name string, schema *document.Schema) *database.Table {
	t.Helper()
	tbl, err := reg.CreateTable(ctx, name, schema)
	if err != nil {
		t.Fatalf("create table %q: %v", name, err)
	}
	return tbl
}

func putDoc(t *testing.T, ctx context.Context, tbl *database.Table, attrs map[string]any) *document.Document {
	t.Helper()
	doc := &document.Document{Attributes: attrs}
	if err := tbl.PutDocument(ctx, doc); err != nil {
		t.Fatalf("put document: %v", err)
	}
	return doc
}

// checkGolden compares got against the golden file at testdata/<name>, writing
// it instead when -update is set.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to generate): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s; run: go test ./internal/uigen -update\n--- got ---\n%s", path, got)
	}
}

func strptr(s string) *string { return &s }

// waitGet polls store.Get until the named config is visible (the attribute
// index is populated asynchronously after a write) or the deadline passes.
func waitGet(t *testing.T, ctx context.Context, store *uiconfig.Store, name string) *uiconfig.UIConfig {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := store.Get(ctx, name)
		if err != nil {
			t.Fatalf("get %q: %v", name, err)
		}
		if got != nil {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q to be indexed", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitGone polls store.Get until the named config is no longer visible or the
// deadline passes.
func waitGone(t *testing.T, ctx context.Context, store *uiconfig.Store, name string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := store.Get(ctx, name)
		if err != nil {
			t.Fatalf("get %q: %v", name, err)
		}
		if got == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q to be removed", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ordersSchema is a representative typed schema exercising every mapped field
// type plus a FullText field, an array field, and a reference field.
func ordersSchema() *document.Schema {
	return &document.Schema{
		Fields: []document.FieldDefinition{
			{Name: "customer", Type: document.FieldTypeString, FullText: true, Required: true},
			{Name: "quantity", Type: document.FieldTypeInt},
			{Name: "total", Type: document.FieldTypeFloat},
			{Name: "paid", Type: document.FieldTypeBool},
			{Name: "created", Type: document.FieldTypeDatetime},
			{Name: "tags", Type: document.FieldTypeStringSlice},
			{Name: "owner", Type: document.FieldTypeReference, ReferenceDB: "users"},
		},
	}
}

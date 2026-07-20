package uiconfig

import (
	"context"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/functions"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

// newTestEnv builds an in-memory registry (with system tables ensured so
// _ui_configs exists) plus a JS engine, and returns a Store and Renderer bound
// to them.
func newTestEnv(t *testing.T) (context.Context, *database.Registry, *Store, *Renderer) {
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
	store := NewStore(reg)
	renderer := NewRenderer(reg, eng)
	return ctx, reg, store, renderer
}

// createTable creates a non-system table, optionally with a schema.
func createTable(t *testing.T, ctx context.Context, reg *database.Registry, name string, schema *document.Schema) *database.Table {
	t.Helper()
	tbl, err := reg.CreateTable(ctx, name, schema)
	if err != nil {
		t.Fatalf("create table %q: %v", name, err)
	}
	return tbl
}

func strptr(s string) *string { return &s }

// waitGet polls store.Get until the named config is visible via the attribute
// index (which is populated asynchronously after a write) or the deadline
// passes. It returns the resolved config.
func waitGet(t *testing.T, ctx context.Context, store *Store, name string) *UIConfig {
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

// waitGone polls store.Get until the named config is no longer visible (the
// index delete is applied asynchronously) or the deadline passes.
func waitGone(t *testing.T, ctx context.Context, store *Store, name string) {
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

// waitEnabledCount polls ListEnabled until it reports want entries or times out.
func waitEnabledCount(t *testing.T, ctx context.Context, store *Store, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last int
	for {
		enabled, err := store.ListEnabled(ctx)
		if err != nil {
			t.Fatalf("ListEnabled: %v", err)
		}
		last = len(enabled)
		if last == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for ListEnabled count %d, last saw %d", want, last)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

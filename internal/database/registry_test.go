package database

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	store := objstore.NewMemoryStore()
	return NewRegistry(RegistryConfig{
		Store:    store,
		Prefix:   "test",
		CacheDir: t.TempDir(),
	})
}

// TestRegistryConcurrentGetTable verifies that multiple goroutines calling
// GetTable for the same name all get the same instance and only one DB is opened.
func TestRegistryConcurrentGetTable(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	// Create a table first.
	_, err := reg.CreateTable(ctx, "mydb", &document.Schema{
		Fields: []document.FieldDefinition{{Name: "x", Type: document.FieldTypeString}},
	})
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	// Close and clear the in-memory cache so GetTable has to load from store.
	reg.mu.Lock()
	if db, ok := reg.tables["mydb"]; ok {
		db.Close()
		delete(reg.tables, "mydb")
	}
	reg.mu.Unlock()

	const goroutines = 20
	var wg sync.WaitGroup
	results := make([]*Table, goroutines)
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			db, err := reg.GetTable(ctx, "mydb")
			results[n] = db
			errs[n] = err
		}(i)
	}

	wg.Wait()

	// All should succeed.
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: GetTable failed: %v", i, err)
		}
	}

	// All should get the same instance.
	first := results[0]
	for i := 1; i < goroutines; i++ {
		if results[i] != first {
			t.Fatalf("goroutine %d got a different DB instance", i)
		}
	}
}

// TestRegistryCreateTableDuplicateName verifies that creating a table
// with a duplicate name returns an error.
func TestRegistryCreateTableDuplicateName(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	_, err := reg.CreateTable(ctx, "dup", nil)
	if err != nil {
		t.Fatalf("CreateTable first: %v", err)
	}

	_, err = reg.CreateTable(ctx, "dup", nil)
	if err == nil {
		t.Fatal("expected error on duplicate CreateTable")
	}
}

// TestRegistryCreateAndDeleteConcurrent verifies that concurrent create and
// delete operations don't panic or deadlock.
func TestRegistryCreateAndDeleteConcurrent(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	// Create tables sequentially first.
	for i := range 10 {
		name := dbName(i)
		reg.CreateTable(ctx, name, nil)
	}

	var wg sync.WaitGroup

	// Concurrently delete even-numbered tables and get odd-numbered ones.
	for i := range 10 {
		wg.Add(1)
		if i%2 == 0 {
			go func(n int) {
				defer wg.Done()
				reg.DeleteTable(ctx, dbName(n))
			}(i)
		} else {
			go func(n int) {
				defer wg.Done()
				reg.GetTable(ctx, dbName(n))
			}(i)
		}
	}

	wg.Wait()

	// Odd-numbered tables should still be accessible.
	for i := 1; i < 10; i += 2 {
		_, err := reg.GetTable(ctx, dbName(i))
		if err != nil {
			t.Fatalf("GetTable(%s) after concurrent ops: %v", dbName(i), err)
		}
	}
}

// TestRegistryGetNonexistentTable verifies that getting a table that
// doesn't exist returns an error.
func TestRegistryGetNonexistentTable(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	_, err := reg.GetTable(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected error for nonexistent table")
	}
}

// TestRegistryListTables verifies that ListTables returns all created tables.
func TestRegistryListTables(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	reg.CreateTable(ctx, "db-a", nil)
	reg.CreateTable(ctx, "db-b", nil)
	reg.CreateTable(ctx, "db-c", nil)

	metas, err := reg.ListTables(ctx)
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("expected 3 tables, got %d", len(metas))
	}

	names := map[string]bool{}
	for _, m := range metas {
		names[m.Name] = true
	}
	for _, name := range []string{"db-a", "db-b", "db-c"} {
		if !names[name] {
			t.Fatalf("expected table %s in list", name)
		}
	}
}

// TestRegistryDeleteThenCreate verifies that a deleted table name can be reused.
func TestRegistryDeleteThenCreate(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	reg.CreateTable(ctx, "recycle", nil)
	if err := reg.DeleteTable(ctx, "recycle"); err != nil {
		t.Fatalf("DeleteTable: %v", err)
	}

	_, err := reg.CreateTable(ctx, "recycle", nil)
	if err != nil {
		t.Fatalf("CreateTable after delete: %v", err)
	}
}

// TestOnSchemaChangeReentrantNoDeadlock verifies the OnSchemaChange callback
// fires without the registry lock held, so a callback that re-enters the
// registry (here, GetTable on the just-created table) does not deadlock.
func TestOnSchemaChangeReentrantNoDeadlock(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	reg.OnSchemaChange = func(cbCtx context.Context) {
		// Re-enter the registry from within the callback. This would deadlock if
		// the callback fired while r.mu was still held by CreateTable/DeleteTable.
		reg.GetTable(cbCtx, "reentrant")
	}

	done := make(chan error, 1)
	go func() {
		_, err := reg.CreateTable(ctx, "reentrant", nil)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("CreateTable: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CreateTable deadlocked: OnSchemaChange re-entered the registry under the lock")
	}

	// DeleteTable must be safe too.
	go func() {
		done <- reg.DeleteTable(ctx, "reentrant")
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DeleteTable: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DeleteTable deadlocked in OnSchemaChange")
	}
}

// TestRegistryUpdateSchema verifies schema updates are persisted.
func TestRegistryUpdateSchema(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	reg.CreateTable(ctx, "schemadb", &document.Schema{
		Fields: []document.FieldDefinition{{Name: "a", Type: document.FieldTypeString}},
	})

	newSchema := &document.Schema{
		Fields: []document.FieldDefinition{
			{Name: "a", Type: document.FieldTypeString},
			{Name: "b", Type: document.FieldTypeInt},
		},
	}
	if err := reg.UpdateSchema(ctx, "schemadb", newSchema); err != nil {
		t.Fatalf("UpdateSchema: %v", err)
	}

	// Clear cache and reload to verify persistence.
	reg.mu.Lock()
	if db, ok := reg.tables["schemadb"]; ok {
		db.Close()
		delete(reg.tables, "schemadb")
	}
	reg.mu.Unlock()

	db, err := reg.GetTable(ctx, "schemadb")
	if err != nil {
		t.Fatalf("GetTable after schema update: %v", err)
	}
	if len(db.Schema.Fields) != 2 {
		t.Fatalf("expected 2 fields in schema, got %d", len(db.Schema.Fields))
	}
}

// TestEnsureSystemTables verifies that system tables are created with System=true
// and that calling EnsureSystemTables a second time is idempotent.
func TestEnsureSystemTables(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	defs := []SystemTableDef{
		{
			Name: "_sys_users",
			Schema: &document.Schema{
				Fields: []document.FieldDefinition{{Name: "email", Type: document.FieldTypeString}},
			},
		},
	}

	if err := reg.EnsureSystemTables(ctx, defs); err != nil {
		t.Fatalf("EnsureSystemTables: %v", err)
	}

	table, err := reg.GetTable(ctx, "_sys_users")
	if err != nil {
		t.Fatalf("GetTable: %v", err)
	}
	if !table.System {
		t.Fatal("expected table.System to be true")
	}

	// Second call should be idempotent (no error).
	if err := reg.EnsureSystemTables(ctx, defs); err != nil {
		t.Fatalf("EnsureSystemTables (idempotent): %v", err)
	}
}

// TestSystemTablePersistence verifies that the System flag survives a cache eviction
// and reload from the object store.
func TestSystemTablePersistence(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	defs := []SystemTableDef{
		{Name: "_sys_config"},
	}

	if err := reg.EnsureSystemTables(ctx, defs); err != nil {
		t.Fatalf("EnsureSystemTables: %v", err)
	}

	// Clear in-memory cache.
	reg.mu.Lock()
	if db, ok := reg.tables["_sys_config"]; ok {
		db.Close()
		delete(reg.tables, "_sys_config")
	}
	reg.mu.Unlock()

	// Reload from store.
	table, err := reg.GetTable(ctx, "_sys_config")
	if err != nil {
		t.Fatalf("GetTable after cache clear: %v", err)
	}
	if !table.System {
		t.Fatal("expected System=true after reload")
	}
}

// TestIsSystemTable verifies IsSystemTable returns false for regular tables
// and true for system tables.
func TestIsSystemTable(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	// Create a regular table.
	_, err := reg.CreateTable(ctx, "regular", nil)
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	isSystem, err := reg.IsSystemTable(ctx, "regular")
	if err != nil {
		t.Fatalf("IsSystemTable(regular): %v", err)
	}
	if isSystem {
		t.Fatal("expected regular table to not be system")
	}

	// Create a system table.
	if err := reg.EnsureSystemTables(ctx, []SystemTableDef{{Name: "_sys_test"}}); err != nil {
		t.Fatalf("EnsureSystemTables: %v", err)
	}

	isSystem, err = reg.IsSystemTable(ctx, "_sys_test")
	if err != nil {
		t.Fatalf("IsSystemTable(_sys_test): %v", err)
	}
	if !isSystem {
		t.Fatal("expected system table to be system")
	}
}

func dbName(i int) string {
	return "db-" + string(rune('a'+i))
}

package database

import (
	"context"
	"sync"
	"testing"

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

// TestRegistryConcurrentGetDatabase verifies that multiple goroutines calling
// GetDatabase for the same name all get the same instance and only one DB is opened.
func TestRegistryConcurrentGetDatabase(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	// Create a database first.
	_, err := reg.CreateDatabase(ctx, "mydb", &document.Schema{
		Fields: []document.FieldDefinition{{Name: "x", Type: document.FieldTypeString}},
	})
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}

	// Close and clear the in-memory cache so GetDatabase has to load from store.
	reg.mu.Lock()
	if db, ok := reg.databases["mydb"]; ok {
		db.Close()
		delete(reg.databases, "mydb")
	}
	reg.mu.Unlock()

	const goroutines = 20
	var wg sync.WaitGroup
	results := make([]*Database, goroutines)
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			db, err := reg.GetDatabase(ctx, "mydb")
			results[n] = db
			errs[n] = err
		}(i)
	}

	wg.Wait()

	// All should succeed.
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: GetDatabase failed: %v", i, err)
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

// TestRegistryCreateDatabaseDuplicateName verifies that creating a database
// with a duplicate name returns an error.
func TestRegistryCreateDatabaseDuplicateName(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	_, err := reg.CreateDatabase(ctx, "dup", nil)
	if err != nil {
		t.Fatalf("CreateDatabase first: %v", err)
	}

	_, err = reg.CreateDatabase(ctx, "dup", nil)
	if err == nil {
		t.Fatal("expected error on duplicate CreateDatabase")
	}
}

// TestRegistryCreateAndDeleteConcurrent verifies that concurrent create and
// delete operations don't panic or deadlock.
func TestRegistryCreateAndDeleteConcurrent(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	// Create databases sequentially first.
	for i := range 10 {
		name := dbName(i)
		reg.CreateDatabase(ctx, name, nil)
	}

	var wg sync.WaitGroup

	// Concurrently delete even-numbered databases and get odd-numbered ones.
	for i := range 10 {
		wg.Add(1)
		if i%2 == 0 {
			go func(n int) {
				defer wg.Done()
				reg.DeleteDatabase(ctx, dbName(n))
			}(i)
		} else {
			go func(n int) {
				defer wg.Done()
				reg.GetDatabase(ctx, dbName(n))
			}(i)
		}
	}

	wg.Wait()

	// Odd-numbered databases should still be accessible.
	for i := 1; i < 10; i += 2 {
		_, err := reg.GetDatabase(ctx, dbName(i))
		if err != nil {
			t.Fatalf("GetDatabase(%s) after concurrent ops: %v", dbName(i), err)
		}
	}
}

// TestRegistryGetNonexistentDatabase verifies that getting a database that
// doesn't exist returns an error.
func TestRegistryGetNonexistentDatabase(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	_, err := reg.GetDatabase(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected error for nonexistent database")
	}
}

// TestRegistryListDatabases verifies that ListDatabases returns all created databases.
func TestRegistryListDatabases(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	reg.CreateDatabase(ctx, "db-a", nil)
	reg.CreateDatabase(ctx, "db-b", nil)
	reg.CreateDatabase(ctx, "db-c", nil)

	metas, err := reg.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("expected 3 databases, got %d", len(metas))
	}

	names := map[string]bool{}
	for _, m := range metas {
		names[m.Name] = true
	}
	for _, name := range []string{"db-a", "db-b", "db-c"} {
		if !names[name] {
			t.Fatalf("expected database %s in list", name)
		}
	}
}

// TestRegistryDeleteThenCreate verifies that a deleted database name can be reused.
func TestRegistryDeleteThenCreate(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	reg.CreateDatabase(ctx, "recycle", nil)
	if err := reg.DeleteDatabase(ctx, "recycle"); err != nil {
		t.Fatalf("DeleteDatabase: %v", err)
	}

	_, err := reg.CreateDatabase(ctx, "recycle", nil)
	if err != nil {
		t.Fatalf("CreateDatabase after delete: %v", err)
	}
}

// TestRegistryUpdateSchema verifies schema updates are persisted.
func TestRegistryUpdateSchema(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()

	ctx := context.Background()

	reg.CreateDatabase(ctx, "schemadb", &document.Schema{
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
	if db, ok := reg.databases["schemadb"]; ok {
		db.Close()
		delete(reg.databases, "schemadb")
	}
	reg.mu.Unlock()

	db, err := reg.GetDatabase(ctx, "schemadb")
	if err != nil {
		t.Fatalf("GetDatabase after schema update: %v", err)
	}
	if len(db.Schema.Fields) != 2 {
		t.Fatalf("expected 2 fields in schema, got %d", len(db.Schema.Fields))
	}
}

func dbName(i int) string {
	return "db-" + string(rune('a'+i))
}

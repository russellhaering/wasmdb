package functions

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

func testRegistryWithFunctions(t *testing.T) *database.Registry {
	t.Helper()
	reg := database.NewRegistry(database.RegistryConfig{
		Store:  objstore.NewMemoryStore(),
		Prefix: "test",
	})
	err := reg.EnsureSystemTables(context.Background(), []database.SystemTableDef{
		{
			Name: "_functions",
			Schema: &document.Schema{
				Fields: []document.FieldDefinition{
					{Name: "name", Type: document.FieldTypeString, Required: true, Indexed: true},
					{Name: "description", Type: document.FieldTypeString},
					{Name: "created_by", Type: document.FieldTypeString, Required: true, Indexed: true},
					{Name: "updated_at", Type: document.FieldTypeDatetime, Required: true, Indexed: true},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ensure system tables: %v", err)
	}
	t.Cleanup(func() { reg.Close() })
	return reg
}

// waitForIndex gives the async index worker time to process.
func waitForIndex() { time.Sleep(100 * time.Millisecond) }

func TestStoreCreate(t *testing.T) {
	reg := testRegistryWithFunctions(t)
	store := NewStore(reg)
	ctx := context.Background()

	fn, err := store.Create(ctx, "my-func", "does stuff", "return 42;", "user-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if fn.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if fn.Name != "my-func" {
		t.Fatalf("expected name 'my-func', got %q", fn.Name)
	}
	if fn.Description != "does stuff" {
		t.Fatalf("expected description 'does stuff', got %q", fn.Description)
	}
	if fn.Code != "return 42;" {
		t.Fatalf("expected code 'return 42;', got %q", fn.Code)
	}
	if fn.CreatedBy != "user-1" {
		t.Fatalf("expected created_by 'user-1', got %q", fn.CreatedBy)
	}
	if fn.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero UpdatedAt")
	}
}

func TestStoreCreateDuplicate(t *testing.T) {
	reg := testRegistryWithFunctions(t)
	store := NewStore(reg)
	ctx := context.Background()

	_, err := store.Create(ctx, "dup", "", "code1", "u1")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	waitForIndex()

	_, err = store.Create(ctx, "dup", "", "code2", "u2")
	if err == nil {
		t.Fatal("expected error on duplicate name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got: %v", err)
	}
}

func TestStoreGet(t *testing.T) {
	reg := testRegistryWithFunctions(t)
	store := NewStore(reg)
	ctx := context.Background()

	created, err := store.Create(ctx, "getter", "desc", "code here", "user-x")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForIndex()

	got, err := store.Get(ctx, "getter")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil function")
	}
	if got.ID != created.ID {
		t.Fatalf("ID mismatch: %q vs %q", got.ID, created.ID)
	}
	if got.Name != "getter" {
		t.Fatalf("name mismatch: %q", got.Name)
	}
	if got.Code != "code here" {
		t.Fatalf("code mismatch: %q", got.Code)
	}
	if got.Description != "desc" {
		t.Fatalf("description mismatch: %q", got.Description)
	}
	if got.CreatedBy != "user-x" {
		t.Fatalf("created_by mismatch: %q", got.CreatedBy)
	}
}

func TestStoreGetNotFound(t *testing.T) {
	reg := testRegistryWithFunctions(t)
	store := NewStore(reg)

	got, err := store.Get(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestStoreList(t *testing.T) {
	reg := testRegistryWithFunctions(t)
	store := NewStore(reg)
	ctx := context.Background()

	names := []string{"alpha", "beta", "gamma"}
	for _, name := range names {
		_, err := store.Create(ctx, name, "", "code", "u")
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		waitForIndex()
	}

	fns, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(fns) != 3 {
		t.Fatalf("expected 3 functions, got %d", len(fns))
	}

	nameSet := map[string]bool{}
	for _, fn := range fns {
		nameSet[fn.Name] = true
	}
	for _, name := range names {
		if !nameSet[name] {
			t.Fatalf("expected function %q in list", name)
		}
	}
}

func TestStoreListEmpty(t *testing.T) {
	reg := testRegistryWithFunctions(t)
	store := NewStore(reg)

	fns, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(fns) != 0 {
		t.Fatalf("expected 0 functions, got %d", len(fns))
	}
}

func TestStoreUpdate(t *testing.T) {
	reg := testRegistryWithFunctions(t)
	store := NewStore(reg)
	ctx := context.Background()

	_, err := store.Create(ctx, "updatable", "v1", "old code", "u1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForIndex()

	updated, err := store.Update(ctx, "updatable", "new code", "v2")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Code != "new code" {
		t.Fatalf("expected code 'new code', got %q", updated.Code)
	}
	if updated.Description != "v2" {
		t.Fatalf("expected description 'v2', got %q", updated.Description)
	}

	// Verify persisted.
	waitForIndex()
	got, err := store.Get(ctx, "updatable")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Code != "new code" {
		t.Fatalf("persisted code mismatch: %q", got.Code)
	}
	if got.Description != "v2" {
		t.Fatalf("persisted description mismatch: %q", got.Description)
	}
}

func TestStoreUpdateNotFound(t *testing.T) {
	reg := testRegistryWithFunctions(t)
	store := NewStore(reg)

	_, err := store.Update(context.Background(), "nope", "code", "desc")
	if err == nil {
		t.Fatal("expected error for update of non-existent function")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestStoreUpdatePreservesCreatedBy(t *testing.T) {
	reg := testRegistryWithFunctions(t)
	store := NewStore(reg)
	ctx := context.Background()

	_, err := store.Create(ctx, "preserve", "", "code", "original-user")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForIndex()

	_, err = store.Update(ctx, "preserve", "new code", "new desc")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	waitForIndex()

	got, err := store.Get(ctx, "preserve")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CreatedBy != "original-user" {
		t.Fatalf("expected created_by 'original-user', got %q", got.CreatedBy)
	}
}

func TestStoreDelete(t *testing.T) {
	reg := testRegistryWithFunctions(t)
	store := NewStore(reg)
	ctx := context.Background()

	_, err := store.Create(ctx, "deleteme", "", "code", "u")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForIndex()

	if err := store.Delete(ctx, "deleteme"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	waitForIndex()

	got, err := store.Get(ctx, "deleteme")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
}

func TestStoreDeleteNotFound(t *testing.T) {
	reg := testRegistryWithFunctions(t)
	store := NewStore(reg)

	err := store.Delete(context.Background(), "ghost")
	if err == nil {
		t.Fatal("expected error for delete of non-existent function")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestStoreCreateAndGetRoundtrip(t *testing.T) {
	reg := testRegistryWithFunctions(t)
	store := NewStore(reg)
	ctx := context.Background()

	code := `function handler(p) {
  return db.table("x").list();
}`
	created, err := store.Create(ctx, "roundtrip", "full test", code, "user-42")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForIndex()

	got, err := store.Get(ctx, "roundtrip")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if got.ID != created.ID {
		t.Fatalf("ID: %q != %q", got.ID, created.ID)
	}
	if got.Name != "roundtrip" {
		t.Fatalf("Name: %q", got.Name)
	}
	if got.Code != code {
		t.Fatalf("Code mismatch")
	}
	if got.Description != "full test" {
		t.Fatalf("Description: %q", got.Description)
	}
	if got.CreatedBy != "user-42" {
		t.Fatalf("CreatedBy: %q", got.CreatedBy)
	}
}

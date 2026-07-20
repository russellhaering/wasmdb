package database

import (
	"context"
	"sync"
	"testing"

	"github.com/russellhaering/moraine/document"
)

// TestOnWriteFiresForUserTable verifies the OnWrite hook fires with the table
// name on a successful write to a non-system table.
func TestOnWriteFiresForUserTable(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()
	ctx := context.Background()

	var mu sync.Mutex
	var got []string
	reg.OnWrite = func(name string) {
		mu.Lock()
		got = append(got, name)
		mu.Unlock()
	}

	tbl, err := reg.CreateTable(ctx, "orders", nil)
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	if err := tbl.PutDocument(ctx, &document.Document{Attributes: map[string]any{"x": "1"}}); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "orders" {
		t.Fatalf("expected one OnWrite for \"orders\", got %v", got)
	}
}

// TestOnWriteBulkFiresOnce verifies PutDocumentsBulk fires the hook exactly once.
func TestOnWriteBulkFiresOnce(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()
	ctx := context.Background()

	var count int
	reg.OnWrite = func(name string) {
		if name == "bulk" {
			count++
		}
	}

	tbl, err := reg.CreateTable(ctx, "bulk", nil)
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	docs := []*document.Document{
		{Attributes: map[string]any{"a": "1"}},
		{Attributes: map[string]any{"a": "2"}},
		{Attributes: map[string]any{"a": "3"}},
	}
	if err := tbl.PutDocumentsBulk(ctx, docs); err != nil {
		t.Fatalf("PutDocumentsBulk: %v", err)
	}

	if count != 1 {
		t.Fatalf("expected exactly one OnWrite for bulk, got %d", count)
	}
}

// TestOnWriteSkipsSystemTables verifies writes to system tables do not fire the
// hook.
func TestOnWriteSkipsSystemTables(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()
	ctx := context.Background()

	fired := false
	reg.OnWrite = func(name string) { fired = true }

	if err := reg.EnsureSystemTables(ctx, []SystemTableDef{{Name: "_sys_writes"}}); err != nil {
		t.Fatalf("EnsureSystemTables: %v", err)
	}
	sys, err := reg.GetTable(ctx, "_sys_writes")
	if err != nil {
		t.Fatalf("GetTable: %v", err)
	}
	if err := sys.PutDocument(ctx, &document.Document{Attributes: map[string]any{"k": "v"}}); err != nil {
		t.Fatalf("PutDocument (system): %v", err)
	}

	if fired {
		t.Fatal("OnWrite fired for a system-table write")
	}
}

// TestOnWriteNilHookNoPanic verifies a nil hook is a no-op.
func TestOnWriteNilHookNoPanic(t *testing.T) {
	reg := newTestRegistry(t)
	defer reg.Close()
	ctx := context.Background()

	tbl, err := reg.CreateTable(ctx, "orders", nil)
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	// reg.OnWrite is nil; this must not panic.
	if err := tbl.PutDocument(ctx, &document.Document{Attributes: map[string]any{"x": "1"}}); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}
}

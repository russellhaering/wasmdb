package lsm

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

func TestDBWriteFlushRead(t *testing.T) {
	ctx := context.Background()
	store := objstore.NewMemoryStore()

	db, err := Open(ctx, DBConfig{
		Store:           store,
		Prefix:          "test",
		MemTableMaxSize: 1 << 20,
		CompactInterval: time.Hour, // disable auto-compact
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Write some data.
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%03d", i)
		val := fmt.Sprintf("val-%03d", i)
		if _, err := db.Put(ctx, key, []byte(val)); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	// Read before flush — should work from MemTable.
	e, ok, err := db.Get(ctx, "key-005")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected key-005 to be found before flush")
	}
	if string(e.Value) != "val-005" {
		t.Fatalf("expected val-005, got %s", string(e.Value))
	}

	// Flush.
	if err := db.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Read after flush — should work from L0 SSTable.
	e, ok, err = db.Get(ctx, "key-005")
	if err != nil {
		t.Fatalf("Get after flush: %v", err)
	}
	if !ok {
		t.Fatal("expected key-005 to be found after flush")
	}
	if string(e.Value) != "val-005" {
		t.Fatalf("expected val-005, got %s", string(e.Value))
	}

	// Read non-existent key.
	_, ok, err = db.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get nonexistent: %v", err)
	}
	if ok {
		t.Fatal("expected nonexistent to not be found")
	}
}

func TestDBDelete(t *testing.T) {
	ctx := context.Background()
	store := objstore.NewMemoryStore()

	db, err := Open(ctx, DBConfig{
		Store:           store,
		Prefix:          "test",
		CompactInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if _, err := db.Put(ctx, "key1", []byte("value1")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := db.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Delete the key.
	if _, err := db.Delete(ctx, "key1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Should still find it (as tombstone) in MemTable.
	e, ok, err := db.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if !ok {
		t.Fatal("expected tombstone entry")
	}
	if e.Value != nil {
		t.Fatalf("expected nil value (tombstone), got %q", e.Value)
	}
}

func TestDBScan(t *testing.T) {
	ctx := context.Background()
	store := objstore.NewMemoryStore()

	db, err := Open(ctx, DBConfig{
		Store:           store,
		Prefix:          "test",
		CompactInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Write data in two batches with a flush in between.
	for i := 0; i < 5; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte(fmt.Sprintf("val-%03d", i)))
	}
	db.Flush(ctx)

	for i := 5; i < 10; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte(fmt.Sprintf("val-%03d", i)))
	}

	entries, err := db.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(entries) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(entries))
	}

	// Verify sorted order.
	for i := 1; i < len(entries); i++ {
		if entries[i].Key <= entries[i-1].Key {
			t.Fatalf("entries not sorted: %s after %s", entries[i].Key, entries[i-1].Key)
		}
	}
}

func TestDBCompaction(t *testing.T) {
	ctx := context.Background()
	store := objstore.NewMemoryStore()

	db, err := Open(ctx, DBConfig{
		Store:           store,
		Prefix:          "test",
		L0CompactThresh: 2, // trigger compaction after 2 L0 SSTables
		CompactInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Write two batches to create 2 L0 SSTables.
	for i := 0; i < 50; i++ {
		db.Put(ctx, fmt.Sprintf("key-%05d", i), []byte(fmt.Sprintf("val-%05d", i)))
	}
	db.Flush(ctx)

	for i := 50; i < 100; i++ {
		db.Put(ctx, fmt.Sprintf("key-%05d", i), []byte(fmt.Sprintf("val-%05d", i)))
	}
	db.Flush(ctx)

	// Trigger compaction.
	if err := db.Compact(ctx); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// Verify all data is still readable after compaction.
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%05d", i)
		expected := fmt.Sprintf("val-%05d", i)
		e, ok, err := db.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get(%s): %v", key, err)
		}
		if !ok {
			t.Fatalf("Get(%s): not found after compaction", key)
		}
		if string(e.Value) != expected {
			t.Fatalf("Get(%s): expected %s, got %s", key, expected, string(e.Value))
		}
	}
}

func TestDBOverwrite(t *testing.T) {
	ctx := context.Background()
	store := objstore.NewMemoryStore()

	db, err := Open(ctx, DBConfig{
		Store:           store,
		Prefix:          "test",
		CompactInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Write initial value.
	db.Put(ctx, "key1", []byte("v1"))
	db.Flush(ctx)

	// Overwrite.
	db.Put(ctx, "key1", []byte("v2"))

	// Should read the new value.
	e, ok, err := db.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || string(e.Value) != "v2" {
		t.Fatalf("expected v2, got %v %v", ok, e)
	}
}

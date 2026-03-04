package lsm

import (
	"context"
	"fmt"
	"sync"
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

func TestDBScanRange(t *testing.T) {
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

	// Write 20 entries across two flushes (memtable + L0).
	for i := 0; i < 10; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte(fmt.Sprintf("val-%03d", i)))
	}
	db.Flush(ctx)
	for i := 10; i < 20; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte(fmt.Sprintf("val-%03d", i)))
	}

	// First page: limit=5, no cursor.
	result, err := db.ScanRange(ctx, "", 5)
	if err != nil {
		t.Fatalf("ScanRange page 1: %v", err)
	}
	if len(result.Entries) != 5 {
		t.Fatalf("page 1: expected 5 entries, got %d", len(result.Entries))
	}
	if !result.HasMore {
		t.Fatal("page 1: expected HasMore=true")
	}
	if result.Entries[0].Key != "key-000" {
		t.Fatalf("page 1: expected first key key-000, got %s", result.Entries[0].Key)
	}

	// Second page: use last key as cursor.
	cursor := result.Entries[len(result.Entries)-1].Key
	result, err = db.ScanRange(ctx, cursor, 5)
	if err != nil {
		t.Fatalf("ScanRange page 2: %v", err)
	}
	if len(result.Entries) != 5 {
		t.Fatalf("page 2: expected 5 entries, got %d", len(result.Entries))
	}
	if !result.HasMore {
		t.Fatal("page 2: expected HasMore=true")
	}
	// First entry should be cursor+1.
	if result.Entries[0].Key != "key-005" {
		t.Fatalf("page 2: expected first key key-005, got %s", result.Entries[0].Key)
	}

	// Continue paging until done.
	total := 10 // Already got 10 entries (2 pages of 5).
	for result.HasMore {
		cursor = result.Entries[len(result.Entries)-1].Key
		result, err = db.ScanRange(ctx, cursor, 5)
		if err != nil {
			t.Fatalf("ScanRange: %v", err)
		}
		total += len(result.Entries)
	}
	if total != 20 {
		t.Fatalf("expected 20 total entries, got %d", total)
	}
}

func TestDBScanRangeWithDeletes(t *testing.T) {
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

	// Write 10 entries, flush, then delete some.
	for i := 0; i < 10; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte(fmt.Sprintf("val-%03d", i)))
	}
	db.Flush(ctx)

	// Delete entries 3, 5, 7.
	db.Delete(ctx, "key-003")
	db.Delete(ctx, "key-005")
	db.Delete(ctx, "key-007")

	// Scan all — should get 7 entries (tombstones filtered out).
	result, err := db.ScanRange(ctx, "", 100)
	if err != nil {
		t.Fatalf("ScanRange: %v", err)
	}
	if len(result.Entries) != 7 {
		t.Fatalf("expected 7 entries after deletes, got %d", len(result.Entries))
	}
	if result.HasMore {
		t.Fatal("expected HasMore=false")
	}

	// Verify deleted keys are not in the result.
	for _, e := range result.Entries {
		switch e.Key {
		case "key-003", "key-005", "key-007":
			t.Fatalf("deleted key %s should not appear in results", e.Key)
		}
	}
}

func TestDBScanRangeWithOverwrites(t *testing.T) {
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

	// Write initial values.
	for i := 0; i < 5; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte("old"))
	}
	db.Flush(ctx)

	// Overwrite some values.
	db.Put(ctx, "key-001", []byte("new"))
	db.Put(ctx, "key-003", []byte("new"))

	result, err := db.ScanRange(ctx, "", 100)
	if err != nil {
		t.Fatalf("ScanRange: %v", err)
	}
	if len(result.Entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(result.Entries))
	}

	// Check that overwritten values have the new value.
	for _, e := range result.Entries {
		switch e.Key {
		case "key-001", "key-003":
			if string(e.Value) != "new" {
				t.Fatalf("key %s: expected 'new', got %q", e.Key, string(e.Value))
			}
		default:
			if string(e.Value) != "old" {
				t.Fatalf("key %s: expected 'old', got %q", e.Key, string(e.Value))
			}
		}
	}
}

func TestDBScanRangeAfterCompaction(t *testing.T) {
	ctx := context.Background()
	store := objstore.NewMemoryStore()

	db, err := Open(ctx, DBConfig{
		Store:           store,
		Prefix:          "test",
		L0CompactThresh: 2,
		CompactInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Create multiple L0 SSTables.
	for i := 0; i < 30; i++ {
		db.Put(ctx, fmt.Sprintf("key-%05d", i), []byte(fmt.Sprintf("val-%05d", i)))
	}
	db.Flush(ctx)
	for i := 30; i < 60; i++ {
		db.Put(ctx, fmt.Sprintf("key-%05d", i), []byte(fmt.Sprintf("val-%05d", i)))
	}
	db.Flush(ctx)

	// Compact.
	if err := db.Compact(ctx); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// Paginate through all entries after compaction.
	var all []Entry
	cursor := ""
	for {
		result, err := db.ScanRange(ctx, cursor, 10)
		if err != nil {
			t.Fatalf("ScanRange: %v", err)
		}
		all = append(all, result.Entries...)
		if !result.HasMore {
			break
		}
		cursor = result.Entries[len(result.Entries)-1].Key
	}

	if len(all) != 60 {
		t.Fatalf("expected 60 entries after compaction, got %d", len(all))
	}

	// Verify sorted order.
	for i := 1; i < len(all); i++ {
		if all[i].Key <= all[i-1].Key {
			t.Fatalf("entries not sorted: %s after %s", all[i].Key, all[i-1].Key)
		}
	}
}

func TestDBScanSince(t *testing.T) {
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

	// Write 5 entries, flush.
	var midSeq uint64
	for i := 0; i < 5; i++ {
		seq, _ := db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte(fmt.Sprintf("val-%03d", i)))
		if i == 2 {
			midSeq = seq
		}
	}
	db.Flush(ctx)

	// Write 5 more entries.
	for i := 5; i < 10; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte(fmt.Sprintf("val-%03d", i)))
	}

	// ScanSince(midSeq) should return entries with SeqNum > midSeq.
	entries, err := db.ScanSince(ctx, midSeq)
	if err != nil {
		t.Fatalf("ScanSince: %v", err)
	}

	for _, e := range entries {
		if e.SeqNum <= midSeq {
			t.Fatalf("ScanSince returned entry with SeqNum %d <= %d", e.SeqNum, midSeq)
		}
	}

	// Should have entries for keys 3-9 (7 entries).
	if len(entries) != 7 {
		t.Fatalf("expected 7 entries, got %d", len(entries))
	}
}

func TestDBScanSinceIncludesTombstones(t *testing.T) {
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

	seq1, _ := db.Put(ctx, "alive", []byte("yes"))
	db.Put(ctx, "doomed", []byte("yes"))
	db.Flush(ctx)

	db.Delete(ctx, "doomed")

	entries, err := db.ScanSince(ctx, seq1)
	if err != nil {
		t.Fatalf("ScanSince: %v", err)
	}

	// Should include the tombstone for "doomed" and the put for "doomed".
	var hasTombstone bool
	for _, e := range entries {
		if e.Key == "doomed" && e.Value == nil {
			hasTombstone = true
		}
	}
	if !hasTombstone {
		t.Fatal("ScanSince should include tombstones but didn't find one for 'doomed'")
	}
}

func TestDBScanSinceAfterCompaction(t *testing.T) {
	ctx := context.Background()
	store := objstore.NewMemoryStore()

	db, err := Open(ctx, DBConfig{
		Store:           store,
		Prefix:          "test",
		L0CompactThresh: 2,
		CompactInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Write batch 1.
	var lastBatch1Seq uint64
	for i := 0; i < 10; i++ {
		seq, _ := db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte(fmt.Sprintf("val-%03d", i)))
		lastBatch1Seq = seq
	}
	db.Flush(ctx)

	// Write batch 2.
	for i := 10; i < 20; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte(fmt.Sprintf("val-%03d", i)))
	}
	db.Flush(ctx)

	// Compact.
	db.Compact(ctx)

	// ScanSince should still return batch 2 entries.
	entries, err := db.ScanSince(ctx, lastBatch1Seq)
	if err != nil {
		t.Fatalf("ScanSince: %v", err)
	}
	if len(entries) != 10 {
		t.Fatalf("expected 10 entries from batch 2, got %d", len(entries))
	}
}

func TestDBScanSinceZero(t *testing.T) {
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

	for i := 0; i < 5; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte(fmt.Sprintf("val-%03d", i)))
	}
	db.Flush(ctx)

	entries, err := db.ScanSince(ctx, 0)
	if err != nil {
		t.Fatalf("ScanSince(0): %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
}

func TestDBScanSinceFuture(t *testing.T) {
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

	for i := 0; i < 5; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte("v"))
	}

	entries, err := db.ScanSince(ctx, 999999)
	if err != nil {
		t.Fatalf("ScanSince: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for future seq, got %d", len(entries))
	}
}

func TestDBAutoFlush(t *testing.T) {
	ctx := context.Background()
	store := objstore.NewMemoryStore()

	db, err := Open(ctx, DBConfig{
		Store:           store,
		Prefix:          "test",
		MemTableMaxSize: 512, // Very small to trigger auto-flush.
		CompactInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Write enough data to trigger auto-flush.
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%05d", i)
		val := fmt.Sprintf("value-with-some-padding-%05d", i)
		if _, err := db.Put(ctx, key, []byte(val)); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	// Verify all data is readable (some from L0, some from memtable).
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%05d", i)
		e, ok, err := db.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get(%s): %v", key, err)
		}
		if !ok {
			t.Fatalf("Get(%s): not found", key)
		}
		expected := fmt.Sprintf("value-with-some-padding-%05d", i)
		if string(e.Value) != expected {
			t.Fatalf("Get(%s): expected %s, got %s", key, expected, string(e.Value))
		}
	}
}

func TestDBCloseIdempotent(t *testing.T) {
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

	// Close multiple times — should not panic.
	for i := 0; i < 5; i++ {
		if err := db.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i+1, err)
		}
	}
}

func TestDBEmptyFlush(t *testing.T) {
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

	// Flush with nothing written should be a no-op.
	if err := db.Flush(ctx); err != nil {
		t.Fatalf("Flush on empty: %v", err)
	}

	// Flush again.
	if err := db.Flush(ctx); err != nil {
		t.Fatalf("Flush on empty again: %v", err)
	}
}

func TestDBConcurrentReads(t *testing.T) {
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

	// Prepopulate.
	for i := 0; i < 100; i++ {
		db.Put(ctx, fmt.Sprintf("key-%05d", i), []byte(fmt.Sprintf("val-%05d", i)))
	}
	db.Flush(ctx)

	// Concurrent readers.
	const numReaders = 8
	var wg sync.WaitGroup
	errCh := make(chan error, numReaders)

	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				key := fmt.Sprintf("key-%05d", i)
				e, ok, err := db.Get(ctx, key)
				if err != nil {
					errCh <- fmt.Errorf("Get(%s): %v", key, err)
					return
				}
				if !ok {
					errCh <- fmt.Errorf("Get(%s): not found", key)
					return
				}
				expected := fmt.Sprintf("val-%05d", i)
				if string(e.Value) != expected {
					errCh <- fmt.Errorf("Get(%s): expected %s, got %s", key, expected, string(e.Value))
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestDBScanEmpty(t *testing.T) {
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

	entries, err := db.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries on empty DB, got %d", len(entries))
	}
}

func TestDBScanRangeEmpty(t *testing.T) {
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

	result, err := db.ScanRange(ctx, "", 10)
	if err != nil {
		t.Fatalf("ScanRange: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(result.Entries))
	}
	if result.HasMore {
		t.Fatal("expected HasMore=false")
	}
}

func TestDBPutReturnsSequenceNumbers(t *testing.T) {
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

	var lastSeq uint64
	for i := 0; i < 20; i++ {
		seq, err := db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte("v"))
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		if seq <= lastSeq {
			t.Fatalf("sequence number not monotonically increasing: %d <= %d", seq, lastSeq)
		}
		lastSeq = seq
	}
}

func TestDBDeleteReturnsSequenceNumber(t *testing.T) {
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

	putSeq, _ := db.Put(ctx, "key", []byte("val"))
	delSeq, err := db.Delete(ctx, "key")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if delSeq <= putSeq {
		t.Fatalf("delete seq %d should be > put seq %d", delSeq, putSeq)
	}
}

func TestDBScanWithTombstonesFiltered(t *testing.T) {
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

	for i := 0; i < 10; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte("v"))
	}
	db.Flush(ctx)

	// Delete some.
	db.Delete(ctx, "key-002")
	db.Delete(ctx, "key-005")
	db.Delete(ctx, "key-008")

	entries, err := db.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// Scan should filter out tombstones.
	if len(entries) != 7 {
		t.Fatalf("expected 7 entries (3 deleted), got %d", len(entries))
	}
	for _, e := range entries {
		if e.Value == nil {
			t.Fatalf("Scan returned tombstone for key %s", e.Key)
		}
	}
}

func TestDBGetAfterMultipleFlushes(t *testing.T) {
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

	for batch := 0; batch < 5; batch++ {
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("batch%d-key-%03d", batch, i)
			val := fmt.Sprintf("batch%d-val-%03d", batch, i)
			db.Put(ctx, key, []byte(val))
		}
		db.Flush(ctx)
	}

	// Verify all 50 entries across 5 L0 SSTables.
	for batch := 0; batch < 5; batch++ {
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("batch%d-key-%03d", batch, i)
			expected := fmt.Sprintf("batch%d-val-%03d", batch, i)
			e, ok, err := db.Get(ctx, key)
			if err != nil {
				t.Fatalf("Get(%s): %v", key, err)
			}
			if !ok {
				t.Fatalf("Get(%s): not found", key)
			}
			if string(e.Value) != expected {
				t.Fatalf("Get(%s): expected %s, got %s", key, expected, string(e.Value))
			}
		}
	}
}

func TestDBScanRangeLimitOne(t *testing.T) {
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

	for i := 0; i < 5; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte("v"))
	}
	db.Flush(ctx)

	// Page through one at a time.
	var allKeys []string
	cursor := ""
	for {
		result, err := db.ScanRange(ctx, cursor, 1)
		if err != nil {
			t.Fatalf("ScanRange: %v", err)
		}
		if len(result.Entries) == 0 {
			break
		}
		if len(result.Entries) != 1 {
			t.Fatalf("expected 1 entry per page, got %d", len(result.Entries))
		}
		allKeys = append(allKeys, result.Entries[0].Key)
		cursor = result.Entries[0].Key
		if !result.HasMore {
			break
		}
	}

	if len(allKeys) != 5 {
		t.Fatalf("expected 5 keys total, got %d: %v", len(allKeys), allKeys)
	}
}

func TestDBCompactBelowThreshold(t *testing.T) {
	ctx := context.Background()
	store := objstore.NewMemoryStore()

	db, err := Open(ctx, DBConfig{
		Store:           store,
		Prefix:          "test",
		L0CompactThresh: 10, // high threshold
		CompactInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Write one batch — only 1 L0, below threshold of 10.
	for i := 0; i < 10; i++ {
		db.Put(ctx, fmt.Sprintf("key-%03d", i), []byte("v"))
	}
	db.Flush(ctx)

	// Compact should be a no-op (no error).
	if err := db.Compact(ctx); err != nil {
		t.Fatalf("Compact below threshold: %v", err)
	}

	// Data should still be intact.
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%03d", i)
		_, ok, err := db.Get(ctx, key)
		if err != nil || !ok {
			t.Fatalf("Get(%s) after no-op compact: err=%v ok=%v", key, err, ok)
		}
	}
}

func TestDBConcurrentReadsAndWrites(t *testing.T) {
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

	// Seed some data.
	for i := 0; i < 50; i++ {
		db.Put(ctx, fmt.Sprintf("key-%05d", i), []byte(fmt.Sprintf("val-%05d", i)))
	}
	db.Flush(ctx)

	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	// Writer goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 50; i < 150; i++ {
			key := fmt.Sprintf("key-%05d", i)
			val := fmt.Sprintf("val-%05d", i)
			if _, err := db.Put(ctx, key, []byte(val)); err != nil {
				errCh <- fmt.Errorf("Put(%s): %w", key, err)
				return
			}
		}
	}()

	// Reader goroutines.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				key := fmt.Sprintf("key-%05d", i)
				_, _, err := db.Get(ctx, key)
				if err != nil {
					errCh <- fmt.Errorf("Get(%s): %w", key, err)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestDBOverwriteAcrossFlushes(t *testing.T) {
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

	// Write version 1, flush.
	db.Put(ctx, "key", []byte("v1"))
	db.Flush(ctx)

	// Write version 2, flush.
	db.Put(ctx, "key", []byte("v2"))
	db.Flush(ctx)

	// Write version 3, don't flush (in memtable).
	db.Put(ctx, "key", []byte("v3"))

	e, ok, err := db.Get(ctx, "key")
	if err != nil || !ok {
		t.Fatalf("Get: err=%v ok=%v", err, ok)
	}
	if string(e.Value) != "v3" {
		t.Fatalf("expected v3, got %s", string(e.Value))
	}
}

func TestDBDeleteAndReinsert(t *testing.T) {
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

	db.Put(ctx, "key", []byte("original"))
	db.Flush(ctx)

	db.Delete(ctx, "key")
	db.Flush(ctx)

	db.Put(ctx, "key", []byte("resurrected"))

	e, ok, err := db.Get(ctx, "key")
	if err != nil || !ok {
		t.Fatalf("Get: err=%v ok=%v", err, ok)
	}
	if string(e.Value) != "resurrected" {
		t.Fatalf("expected resurrected, got %s", string(e.Value))
	}

	// Also check via Scan.
	entries, err := db.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry in scan, got %d", len(entries))
	}
	if string(entries[0].Value) != "resurrected" {
		t.Fatalf("scan: expected resurrected, got %s", string(entries[0].Value))
	}
}

func TestDBScanSortedOrder(t *testing.T) {
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

	// Write in reverse order.
	for i := 99; i >= 0; i-- {
		db.Put(ctx, fmt.Sprintf("key-%05d", i), []byte("v"))
	}
	db.Flush(ctx)

	entries, err := db.Scan(ctx)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 100 {
		t.Fatalf("expected 100, got %d", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Key <= entries[i-1].Key {
			t.Fatalf("not sorted: %s after %s", entries[i].Key, entries[i-1].Key)
		}
	}
}

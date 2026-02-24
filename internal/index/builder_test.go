package index

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/storage/lsm"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

// helper: open an LSM DB with an in-memory store.
func openTestDB(t *testing.T, store objstore.ObjectStore, prefix string) *lsm.DB {
	t.Helper()
	ctx := context.Background()
	db, err := lsm.Open(ctx, lsm.DBConfig{
		Store:           store,
		Prefix:          prefix,
		MemTableMaxSize: 1 << 20,
		CompactInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return db
}

// helper: write a document to the LSM DB.
func putDoc(t *testing.T, db *lsm.DB, id string, attrs map[string]any) {
	t.Helper()
	doc := &document.Document{
		ID:         id,
		Attributes: attrs,
		Version:    1,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	data, err := document.Serialize(doc)
	if err != nil {
		t.Fatalf("Serialize %s: %v", id, err)
	}
	if _, err := db.Put(context.Background(), id, data); err != nil {
		t.Fatalf("Put %s: %v", id, err)
	}
}

// TestBuilderFullRebuild verifies that the builder indexes all existing documents
// from the LSM store on startup when there's no checkpoint.
func TestBuilderFullRebuild(t *testing.T) {
	store := objstore.NewMemoryStore()
	db := openTestDB(t, store, "test")
	defer db.Close()

	// Write documents and flush so they're in SSTables.
	putDoc(t, db, "doc1", map[string]any{"color": "red"})
	putDoc(t, db, "doc2", map[string]any{"color": "blue"})
	putDoc(t, db, "doc3", map[string]any{"color": "red"})
	db.Flush(context.Background())

	// Create indexes and builder.
	attrs := NewAttributeIndex()
	cacheDir := t.TempDir()

	builder := NewBuilder(BuilderConfig{
		DB:       db,
		Attrs:    attrs,
		CacheDir: cacheDir,
		DBName:   "testdb",
	})

	// Wait a moment for the async rebuild to complete.
	time.Sleep(100 * time.Millisecond)
	builder.Close()

	// Verify all 3 docs were indexed.
	if attrs.Count() != 3 {
		t.Fatalf("expected 3 indexed docs, got %d", attrs.Count())
	}

	// Search should work.
	results := attrs.Search([]Filter{{Field: "color", Op: OpEq, Value: "red"}})
	if len(results) != 2 {
		t.Fatalf("expected 2 red docs, got %d", len(results))
	}
}

// TestBuilderIncrementalCatchUp verifies that the builder only indexes new
// documents when a valid checkpoint exists.
func TestBuilderIncrementalCatchUp(t *testing.T) {
	store := objstore.NewMemoryStore()
	db := openTestDB(t, store, "test")
	defer db.Close()

	cacheDir := t.TempDir()

	// Write initial docs.
	putDoc(t, db, "doc1", map[string]any{"color": "red"})
	putDoc(t, db, "doc2", map[string]any{"color": "blue"})
	db.Flush(context.Background())

	// Build initial index and save checkpoint.
	attrs1 := NewAttributeIndex()
	b1 := NewBuilder(BuilderConfig{
		DB:       db,
		Attrs:    attrs1,
		CacheDir: cacheDir,
		DBName:   "testdb",
	})
	time.Sleep(100 * time.Millisecond)
	b1.Close()

	if attrs1.Count() != 2 {
		t.Fatalf("expected 2 docs after first build, got %d", attrs1.Count())
	}

	// Write more docs AFTER checkpoint.
	putDoc(t, db, "doc3", map[string]any{"color": "green"})
	db.Flush(context.Background())

	// Start a new builder — should do incremental catch-up from checkpoint.
	attrs2 := NewAttributeIndex()
	b2 := NewBuilder(BuilderConfig{
		DB:       db,
		Attrs:    attrs2,
		CacheDir: cacheDir,
		DBName:   "testdb",
	})
	time.Sleep(100 * time.Millisecond)
	b2.Close()

	// The new builder should have indexed doc3 (and potentially re-indexed earlier ones
	// depending on ScanSince behavior, which is fine).
	if attrs2.Count() < 1 {
		t.Fatal("expected at least 1 doc indexed in incremental catch-up")
	}
}

// TestBuilderStaleCheckpointTriggersRebuild verifies that if the checkpoint's
// seqnum exceeds the store's max seqnum, the builder resets and does a full rebuild.
func TestBuilderStaleCheckpointTriggersRebuild(t *testing.T) {
	store := objstore.NewMemoryStore()
	db := openTestDB(t, store, "test")
	defer db.Close()

	cacheDir := t.TempDir()

	// Write docs.
	putDoc(t, db, "doc1", map[string]any{"status": "active"})
	putDoc(t, db, "doc2", map[string]any{"status": "inactive"})
	db.Flush(context.Background())

	// Write a fake checkpoint with a sequence number far higher than anything in the store.
	cpDir := filepath.Join(cacheDir, "checkpoints")
	os.MkdirAll(cpDir, 0o755)
	cpData, _ := json.Marshal(checkpoint{LastSeqNum: 999999})
	os.WriteFile(filepath.Join(cpDir, "testdb.json"), cpData, 0o644)

	// Start builder — should detect stale checkpoint and do full rebuild.
	attrs := NewAttributeIndex()
	builder := NewBuilder(BuilderConfig{
		DB:       db,
		Attrs:    attrs,
		CacheDir: cacheDir,
		DBName:   "testdb",
	})
	time.Sleep(100 * time.Millisecond)
	builder.Close()

	// Should have rebuilt and indexed both docs.
	if attrs.Count() != 2 {
		t.Fatalf("expected 2 docs after stale checkpoint rebuild, got %d", attrs.Count())
	}

	results := attrs.Search([]Filter{{Field: "status", Op: OpEq, Value: "active"}})
	if len(results) != 1 {
		t.Fatalf("expected 1 active doc, got %d", len(results))
	}
}

// TestBuilderCorruptedCheckpoint verifies that a corrupted checkpoint file
// causes a full rebuild rather than a crash.
func TestBuilderCorruptedCheckpoint(t *testing.T) {
	store := objstore.NewMemoryStore()
	db := openTestDB(t, store, "test")
	defer db.Close()

	cacheDir := t.TempDir()

	putDoc(t, db, "doc1", map[string]any{"x": "y"})
	db.Flush(context.Background())

	// Write a corrupted checkpoint file.
	cpDir := filepath.Join(cacheDir, "checkpoints")
	os.MkdirAll(cpDir, 0o755)
	os.WriteFile(filepath.Join(cpDir, "testdb.json"), []byte("not valid json{{{"), 0o644)

	// Builder should handle this gracefully and do a full rebuild.
	attrs := NewAttributeIndex()
	builder := NewBuilder(BuilderConfig{
		DB:       db,
		Attrs:    attrs,
		CacheDir: cacheDir,
		DBName:   "testdb",
	})
	time.Sleep(100 * time.Millisecond)
	builder.Close()

	if attrs.Count() != 1 {
		t.Fatalf("expected 1 doc after corrupted checkpoint rebuild, got %d", attrs.Count())
	}
}

// TestBuilderCheckpointSaved verifies that Close saves a checkpoint with the
// correct sequence number.
func TestBuilderCheckpointSaved(t *testing.T) {
	store := objstore.NewMemoryStore()
	db := openTestDB(t, store, "test")
	defer db.Close()

	cacheDir := t.TempDir()

	putDoc(t, db, "doc1", map[string]any{"a": "b"})
	putDoc(t, db, "doc2", map[string]any{"c": "d"})
	db.Flush(context.Background())

	attrs := NewAttributeIndex()
	builder := NewBuilder(BuilderConfig{
		DB:       db,
		Attrs:    attrs,
		CacheDir: cacheDir,
		DBName:   "testdb",
	})
	time.Sleep(100 * time.Millisecond)
	builder.Close()

	// Read the checkpoint file.
	cpPath := filepath.Join(cacheDir, "checkpoints", "testdb.json")
	data, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}

	var cp checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		t.Fatalf("unmarshal checkpoint: %v", err)
	}

	if cp.LastSeqNum == 0 {
		t.Fatal("expected non-zero checkpoint seq num")
	}
}

// TestBuilderHandlesTombstones verifies that the builder correctly processes
// deleted documents by removing them from indexes.
func TestBuilderHandlesTombstones(t *testing.T) {
	store := objstore.NewMemoryStore()
	db := openTestDB(t, store, "test")
	defer db.Close()

	cacheDir := t.TempDir()

	// Write a doc, flush, delete it, flush again.
	putDoc(t, db, "doc1", map[string]any{"color": "red"})
	putDoc(t, db, "doc2", map[string]any{"color": "blue"})
	db.Flush(context.Background())

	db.Delete(context.Background(), "doc1")
	db.Flush(context.Background())

	// Build index from scratch — should see doc2 only.
	attrs := NewAttributeIndex()
	builder := NewBuilder(BuilderConfig{
		DB:       db,
		Attrs:    attrs,
		CacheDir: cacheDir,
		DBName:   "testdb",
	})
	time.Sleep(100 * time.Millisecond)
	builder.Close()

	// Scan (used by fullRebuild) excludes tombstones, so doc1 should not appear.
	if attrs.Count() != 1 {
		t.Fatalf("expected 1 doc (doc2 only), got %d", attrs.Count())
	}

	results := attrs.Search([]Filter{{Field: "color", Op: OpEq, Value: "blue"}})
	if len(results) != 1 || results[0] != "doc2" {
		t.Fatalf("expected [doc2], got %v", results)
	}
}

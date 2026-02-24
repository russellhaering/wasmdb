package database

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/index"
	"github.com/russellhaering/wasmdb/internal/storage/lsm"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

// helper: create a Database backed by an in-memory store.
func newTestDatabase(t *testing.T, name string) (*Database, func()) {
	t.Helper()
	ctx := context.Background()
	store := objstore.NewMemoryStore()
	cacheDir := t.TempDir()

	lsmDB, err := lsm.Open(ctx, lsm.DBConfig{
		Store:           store,
		Prefix:          "test/" + name,
		MemTableMaxSize: 1 << 20,
		CompactInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("Open LSM: %v", err)
	}

	schema := &document.Schema{
		Fields: []document.FieldDefinition{
			{Name: "color", Type: document.FieldTypeString, Indexed: true},
			{Name: "count", Type: document.FieldTypeInt},
		},
	}

	db, err := NewDatabase(DatabaseConfig{
		Name:     name,
		Schema:   schema,
		DB:       lsmDB,
		CacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}

	cleanup := func() {
		db.Close()
	}
	return db, cleanup
}

// TestConcurrentPutDocumentSameID verifies that concurrent updates to the same
// document ID don't lose data or corrupt state.
func TestConcurrentPutDocumentSameID(t *testing.T) {
	db, cleanup := newTestDatabase(t, "concurrent")
	defer cleanup()

	ctx := context.Background()
	const goroutines = 10

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			doc := &document.Document{
				ID:         "shared-doc",
				Attributes: map[string]any{"color": fmt.Sprintf("color-%d", n)},
			}
			if err := db.PutDocument(ctx, doc); err != nil {
				errs <- fmt.Errorf("goroutine %d: %w", n, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent put error: %v", err)
	}

	// The document should exist and have a consistent state.
	doc, err := db.GetDocument(ctx, "shared-doc")
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if doc == nil {
		t.Fatal("document not found after concurrent puts")
	}

	// Version should be > 1 since it was overwritten multiple times.
	// (The exact version depends on ordering, but it should be at least 2.)
	if doc.Version < 2 {
		t.Fatalf("expected version >= 2, got %d", doc.Version)
	}

	// The document should have a color attribute.
	color, ok := doc.Attributes["color"]
	if !ok || color == "" {
		t.Fatal("expected color attribute to be set")
	}
}

// TestConcurrentPutDocumentDifferentIDs verifies concurrent writes to different
// documents all succeed.
func TestConcurrentPutDocumentDifferentIDs(t *testing.T) {
	db, cleanup := newTestDatabase(t, "concurrent-diff")
	defer cleanup()

	ctx := context.Background()
	const numDocs = 50

	var wg sync.WaitGroup
	errs := make(chan error, numDocs)

	for i := range numDocs {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			doc := &document.Document{
				Attributes: map[string]any{"color": fmt.Sprintf("color-%d", n)},
			}
			if err := db.PutDocument(ctx, doc); err != nil {
				errs <- fmt.Errorf("doc %d: %w", n, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent put error: %v", err)
	}

	// Wait for async indexing to drain — index channel is processed serially,
	// so we may need to wait a bit longer for all 50 docs.
	var results []*document.Document
	for attempt := range 20 {
		time.Sleep(100 * time.Millisecond)
		var err error
		results, err = db.SearchAttributes(ctx, []index.Filter{}, numDocs+10, 0)
		if err != nil {
			t.Fatalf("SearchAttributes: %v", err)
		}
		if len(results) == numDocs {
			break
		}
		_ = attempt
	}
	if len(results) != numDocs {
		t.Fatalf("expected %d docs, got %d", numDocs, len(results))
	}
}

// TestIndexChannelDrain verifies that all index operations are processed
// even when many documents are written quickly.
func TestIndexChannelDrain(t *testing.T) {
	db, cleanup := newTestDatabase(t, "drain")
	defer cleanup()

	ctx := context.Background()

	// Write many documents rapidly.
	for i := range 100 {
		doc := &document.Document{
			Attributes: map[string]any{"color": "red"},
		}
		if err := db.PutDocument(ctx, doc); err != nil {
			t.Fatalf("PutDocument %d: %v", i, err)
		}
	}

	// Wait for async index worker to drain the channel.
	var results []*document.Document
	for attempt := range 30 {
		time.Sleep(100 * time.Millisecond)
		var err error
		results, err = db.SearchAttributes(ctx, []index.Filter{
			{Field: "color", Op: index.OpEq, Value: "red"},
		}, 200, 0)
		if err != nil {
			t.Fatalf("SearchAttributes: %v", err)
		}
		if len(results) == 100 {
			break
		}
		_ = attempt
	}
	if len(results) != 100 {
		t.Fatalf("expected 100 indexed docs, got %d", len(results))
	}
}

// TestIndexChannelProcessesDeletes verifies that delete operations flow
// through the index channel and remove documents from the index.
func TestIndexChannelProcessesDeletes(t *testing.T) {
	db, cleanup := newTestDatabase(t, "deletes")
	defer cleanup()

	ctx := context.Background()

	// Create a doc with a known ID.
	doc := &document.Document{
		ID:         "to-delete",
		Attributes: map[string]any{"color": "red"},
	}
	if err := db.PutDocument(ctx, doc); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}

	// Wait for indexing.
	time.Sleep(50 * time.Millisecond)

	// Should be searchable.
	results, err := db.SearchAttributes(ctx, []index.Filter{
		{Field: "color", Op: index.OpEq, Value: "red"},
	}, 10, 0)
	if err != nil {
		t.Fatalf("SearchAttributes: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result before delete, got %d", len(results))
	}

	// Delete.
	if err := db.DeleteDocument(ctx, "to-delete"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}

	// Wait for index worker.
	time.Sleep(50 * time.Millisecond)

	// Should no longer be searchable.
	results, err = db.SearchAttributes(ctx, []index.Filter{
		{Field: "color", Op: index.OpEq, Value: "red"},
	}, 10, 0)
	if err != nil {
		t.Fatalf("SearchAttributes after delete: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results after delete, got %d", len(results))
	}
}

// TestSearchAttributesEmptyReturnsAll verifies that empty filters returns all docs.
func TestSearchAttributesEmptyReturnsAll(t *testing.T) {
	db, cleanup := newTestDatabase(t, "empty-filter")
	defer cleanup()

	ctx := context.Background()

	for i := range 5 {
		doc := &document.Document{
			Attributes: map[string]any{"color": fmt.Sprintf("c%d", i)},
		}
		if err := db.PutDocument(ctx, doc); err != nil {
			t.Fatalf("PutDocument %d: %v", i, err)
		}
	}

	var results []*document.Document
	for attempt := range 20 {
		time.Sleep(100 * time.Millisecond)
		var err error
		results, err = db.SearchAttributes(ctx, []index.Filter{}, 100, 0)
		if err != nil {
			t.Fatalf("SearchAttributes: %v", err)
		}
		if len(results) == 5 {
			break
		}
		_ = attempt
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 docs with empty filter, got %d", len(results))
	}
}

// TestSearchAttributesPagination verifies offset/limit work correctly.
func TestSearchAttributesPagination(t *testing.T) {
	db, cleanup := newTestDatabase(t, "pagination")
	defer cleanup()

	ctx := context.Background()

	for i := range 10 {
		doc := &document.Document{
			Attributes: map[string]any{"color": fmt.Sprintf("c%d", i)},
		}
		db.PutDocument(ctx, doc)
	}

	time.Sleep(100 * time.Millisecond)

	// Limit 3.
	results, err := db.SearchAttributes(ctx, []index.Filter{}, 3, 0)
	if err != nil {
		t.Fatalf("SearchAttributes limit 3: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}

	// Offset beyond results.
	results, err = db.SearchAttributes(ctx, []index.Filter{}, 10, 100)
	if err != nil {
		t.Fatalf("SearchAttributes offset 100: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 with offset beyond results, got %d", len(results))
	}
}

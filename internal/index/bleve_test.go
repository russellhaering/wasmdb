package index

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/russellhaering/wasmdb/internal/document"
)

// helper: create a BleveIndex in a temp dir with the given schema.
func newTestIndex(t *testing.T, schema *document.Schema) *BleveIndex {
	t.Helper()
	cacheDir := t.TempDir()
	idx, err := NewBleveIndex(cacheDir, "testdb", schema)
	if err != nil {
		t.Fatalf("NewBleveIndex: %v", err)
	}
	t.Cleanup(func() { idx.Close() })
	return idx
}

func TestNewIndex_NilSchema_IndexAndSearch(t *testing.T) {
	idx := newTestIndex(t, nil)

	docs := []*document.Document{
		{ID: "1", Content: "the quick brown fox jumps over the lazy dog"},
		{ID: "2", Content: "a fast cat chases a slow mouse"},
		{ID: "3", Content: "the fox and the hound are friends"},
	}
	for _, d := range docs {
		if err := idx.IndexDocument(d); err != nil {
			t.Fatalf("IndexDocument(%s): %v", d.ID, err)
		}
	}

	results, total, err := idx.Search("fox", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total < 2 {
		t.Fatalf("expected at least 2 hits for 'fox', got %d", total)
	}
	ids := make(map[string]bool)
	for _, r := range results {
		ids[r.DocID] = true
	}
	if !ids["1"] || !ids["3"] {
		t.Errorf("expected docs 1 and 3 in results, got %v", ids)
	}
}

func TestSearch_LimitAndOffset(t *testing.T) {
	idx := newTestIndex(t, nil)

	// Index 5 documents all containing the word "common".
	for i := 0; i < 5; i++ {
		d := &document.Document{
			ID:      fmt.Sprintf("doc%d", i),
			Content: "common shared word in every document",
		}
		if err := idx.IndexDocument(d); err != nil {
			t.Fatalf("IndexDocument: %v", err)
		}
	}

	// Limit to 2 results.
	results, total, err := idx.Search("common", 2, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results with limit=2, got %d", len(results))
	}

	// Offset past some results.
	results2, total2, err := idx.Search("common", 2, 3)
	if err != nil {
		t.Fatalf("Search with offset: %v", err)
	}
	if total2 != 5 {
		t.Errorf("expected total=5 with offset, got %d", total2)
	}
	if len(results2) != 2 {
		t.Errorf("expected 2 results with offset=3 limit=2, got %d", len(results2))
	}

	// Offset past all results.
	results3, _, err := idx.Search("common", 10, 5)
	if err != nil {
		t.Fatalf("Search with large offset: %v", err)
	}
	if len(results3) != 0 {
		t.Errorf("expected 0 results with offset=5, got %d", len(results3))
	}
}

func TestDeleteDocument(t *testing.T) {
	idx := newTestIndex(t, nil)

	doc := &document.Document{ID: "del1", Content: "unique elephant word"}
	if err := idx.IndexDocument(doc); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	// Verify it's found.
	results, total, err := idx.Search("elephant", 10, 0)
	if err != nil {
		t.Fatalf("Search before delete: %v", err)
	}
	if total != 1 || len(results) != 1 || results[0].DocID != "del1" {
		t.Fatalf("expected 1 hit for 'elephant' before delete, got %d", total)
	}

	// Delete it.
	if err := idx.DeleteDocument("del1"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}

	// Verify it's gone.
	results, total, err = idx.Search("elephant", 10, 0)
	if err != nil {
		t.Fatalf("Search after delete: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 hits after delete, got %d", total)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results after delete, got %v", results)
	}
}

func TestDocCount(t *testing.T) {
	idx := newTestIndex(t, nil)

	count, err := idx.DocCount()
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 docs initially, got %d", count)
	}

	for i := 0; i < 3; i++ {
		d := &document.Document{ID: fmt.Sprintf("d%d", i), Content: "hello"}
		if err := idx.IndexDocument(d); err != nil {
			t.Fatalf("IndexDocument: %v", err)
		}
	}

	count, err = idx.DocCount()
	if err != nil {
		t.Fatalf("DocCount: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 docs, got %d", count)
	}

	// Delete one.
	if err := idx.DeleteDocument("d1"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	count, err = idx.DocCount()
	if err != nil {
		t.Fatalf("DocCount after delete: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 docs after delete, got %d", count)
	}

	// Re-index same ID should not duplicate.
	d := &document.Document{ID: "d0", Content: "updated"}
	if err := idx.IndexDocument(d); err != nil {
		t.Fatalf("IndexDocument re-index: %v", err)
	}
	count, err = idx.DocCount()
	if err != nil {
		t.Fatalf("DocCount after re-index: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 docs after re-index, got %d", count)
	}
}

func TestCloseAndReopen(t *testing.T) {
	cacheDir := t.TempDir()

	// Create and populate.
	idx, err := NewBleveIndex(cacheDir, "persist", nil)
	if err != nil {
		t.Fatalf("NewBleveIndex (create): %v", err)
	}

	doc := &document.Document{ID: "p1", Content: "persistence verification test"}
	if err := idx.IndexDocument(doc); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	path := idx.Path()
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen via bleve.Open directly to confirm the path is correct.
	reopened, err := bleve.Open(path)
	if err != nil {
		t.Fatalf("bleve.Open after close: %v", err)
	}
	defer reopened.Close()

	query := bleve.NewQueryStringQuery("persistence")
	req := bleve.NewSearchRequestOptions(query, 10, 0, false)
	result, err := reopened.Search(req)
	if err != nil {
		t.Fatalf("Search reopened: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 hit after reopen, got %d", result.Total)
	}
	if len(result.Hits) != 1 || result.Hits[0].ID != "p1" {
		t.Errorf("expected hit for p1 after reopen, got %v", result.Hits)
	}
}

func TestCloseAndReopen_ViaNewBleveIndex(t *testing.T) {
	cacheDir := t.TempDir()

	idx, err := NewBleveIndex(cacheDir, "persist2", nil)
	if err != nil {
		t.Fatalf("NewBleveIndex (create): %v", err)
	}

	doc := &document.Document{ID: "r1", Content: "reopening through constructor"}
	if err := idx.IndexDocument(doc); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen through NewBleveIndex — should hit the bleve.Open path.
	idx2, err := NewBleveIndex(cacheDir, "persist2", nil)
	if err != nil {
		t.Fatalf("NewBleveIndex (reopen): %v", err)
	}
	defer idx2.Close()

	results, total, err := idx2.Search("reopening", 10, 0)
	if err != nil {
		t.Fatalf("Search after reopen: %v", err)
	}
	if total != 1 || len(results) != 1 || results[0].DocID != "r1" {
		t.Errorf("expected 1 hit for r1, got total=%d results=%v", total, results)
	}
}

func TestSchema_FullTextField(t *testing.T) {
	schema := &document.Schema{
		Fields: []document.FieldDefinition{
			{Name: "title", Type: document.FieldTypeString, FullText: true},
			{Name: "body", Type: document.FieldTypeString, FullText: true},
		},
	}
	idx := newTestIndex(t, schema)

	docs := []*document.Document{
		{
			ID:         "ft1",
			Content:    "generic content",
			Attributes: map[string]any{"title": "Quantum Computing Primer", "body": "an introduction to qubits"},
		},
		{
			ID:         "ft2",
			Content:    "generic content",
			Attributes: map[string]any{"title": "Classical Physics", "body": "newtonian mechanics overview"},
		},
	}
	for _, d := range docs {
		if err := idx.IndexDocument(d); err != nil {
			t.Fatalf("IndexDocument(%s): %v", d.ID, err)
		}
	}

	// Search by attribute field.
	results, total, err := idx.Search("attr_title:quantum", 10, 0)
	if err != nil {
		t.Fatalf("Search attr_title: %v", err)
	}
	if total != 1 || results[0].DocID != "ft1" {
		t.Errorf("expected ft1 for title search, got total=%d results=%v", total, results)
	}

	// Search by body attribute.
	results, total, err = idx.Search("attr_body:qubits", 10, 0)
	if err != nil {
		t.Fatalf("Search attr_body: %v", err)
	}
	if total != 1 || results[0].DocID != "ft1" {
		t.Errorf("expected ft1 for body search, got total=%d results=%v", total, results)
	}

	// Search by body for second doc.
	results, total, err = idx.Search("attr_body:newtonian", 10, 0)
	if err != nil {
		t.Fatalf("Search newtonian: %v", err)
	}
	if total != 1 || results[0].DocID != "ft2" {
		t.Errorf("expected ft2 for newtonian search, got total=%d results=%v", total, results)
	}
}

func TestSchema_NumericField(t *testing.T) {
	schema := &document.Schema{
		Fields: []document.FieldDefinition{
			{Name: "score", Type: document.FieldTypeInt},
			{Name: "rating", Type: document.FieldTypeFloat},
		},
	}
	idx := newTestIndex(t, schema)

	docs := []*document.Document{
		{ID: "n1", Content: "low score item", Attributes: map[string]any{"score": 10, "rating": 1.5}},
		{ID: "n2", Content: "high score item", Attributes: map[string]any{"score": 90, "rating": 9.2}},
		{ID: "n3", Content: "mid score item", Attributes: map[string]any{"score": 50, "rating": 5.0}},
	}
	for _, d := range docs {
		if err := idx.IndexDocument(d); err != nil {
			t.Fatalf("IndexDocument(%s): %v", d.ID, err)
		}
	}

	// Numeric range query on score: >=50.
	results, total, err := idx.Search("attr_score:>=50", 10, 0)
	if err != nil {
		t.Fatalf("Search numeric range: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 hits for score>=50, got %d", total)
	}
	ids := make(map[string]bool)
	for _, r := range results {
		ids[r.DocID] = true
	}
	if !ids["n2"] || !ids["n3"] {
		t.Errorf("expected n2 and n3, got %v", ids)
	}
}

func TestSearch_NoResults(t *testing.T) {
	idx := newTestIndex(t, nil)

	doc := &document.Document{ID: "x1", Content: "alpha beta gamma"}
	if err := idx.IndexDocument(doc); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	results, total, err := idx.Search("zyxwvutsrqp", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 total for nonsense query, got %d", total)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %v", results)
	}
}

func TestPath(t *testing.T) {
	cacheDir := t.TempDir()
	idx, err := NewBleveIndex(cacheDir, "mydb", nil)
	if err != nil {
		t.Fatalf("NewBleveIndex: %v", err)
	}
	defer idx.Close()

	want := filepath.Join(cacheDir, "bleve", "mydb")
	if got := idx.Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestRelevanceOrdering(t *testing.T) {
	idx := newTestIndex(t, nil)

	// doc with the word "banana" many times should score higher.
	docs := []*document.Document{
		{ID: "low", Content: "I like banana sometimes"},
		{ID: "high", Content: "banana banana banana banana banana banana banana banana banana banana"},
		{ID: "none", Content: "apple orange grape"},
	}
	for _, d := range docs {
		if err := idx.IndexDocument(d); err != nil {
			t.Fatalf("IndexDocument(%s): %v", d.ID, err)
		}
	}

	results, total, err := idx.Search("banana", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 hits for 'banana', got %d", total)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	// The high-frequency doc should be first (higher score).
	if results[0].DocID != "high" {
		t.Errorf("expected 'high' as top result, got %q (score=%.4f)", results[0].DocID, results[0].Score)
	}
	if results[1].DocID != "low" {
		t.Errorf("expected 'low' as second result, got %q (score=%.4f)", results[1].DocID, results[1].Score)
	}
	if results[0].Score <= results[1].Score {
		t.Errorf("expected first result score > second, got %.4f <= %.4f", results[0].Score, results[1].Score)
	}
}

func TestBleveDoc_EmptyContent(t *testing.T) {
	doc := &document.Document{
		ID:         "e1",
		Content:    "",
		Attributes: map[string]any{"tag": "hello"},
	}
	m := bleveDoc(doc)
	if _, ok := m["content"]; ok {
		t.Error("expected no 'content' key for empty content")
	}
	if v, ok := m["attr_tag"]; !ok || v != "hello" {
		t.Errorf("expected attr_tag=hello, got %v", m)
	}
}

func TestBleveDoc_WithContentAndAttributes(t *testing.T) {
	doc := &document.Document{
		ID:         "c1",
		Content:    "some content",
		Attributes: map[string]any{"color": "red", "count": 42},
	}
	m := bleveDoc(doc)
	if m["content"] != "some content" {
		t.Errorf("expected content='some content', got %v", m["content"])
	}
	if m["attr_color"] != "red" {
		t.Errorf("expected attr_color=red, got %v", m["attr_color"])
	}
	if m["attr_count"] != 42 {
		t.Errorf("expected attr_count=42, got %v", m["attr_count"])
	}
}

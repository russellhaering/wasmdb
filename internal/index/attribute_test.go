package index

import (
	"sort"
	"testing"
)

func TestAttributeIndexStringEq(t *testing.T) {
	idx := NewAttributeIndex()
	idx.IndexDocument("doc1", map[string]any{"color": "red"})
	idx.IndexDocument("doc2", map[string]any{"color": "blue"})
	idx.IndexDocument("doc3", map[string]any{"color": "red"})

	results := idx.Search([]Filter{{Field: "color", Op: OpEq, Value: "red"}})
	sort.Strings(results)
	if len(results) != 2 || results[0] != "doc1" || results[1] != "doc3" {
		t.Fatalf("expected [doc1, doc3], got %v", results)
	}
}

func TestAttributeIndexNumericRange(t *testing.T) {
	idx := NewAttributeIndex()
	idx.IndexDocument("doc1", map[string]any{"score": float64(10)})
	idx.IndexDocument("doc2", map[string]any{"score": float64(20)})
	idx.IndexDocument("doc3", map[string]any{"score": float64(30)})
	idx.IndexDocument("doc4", map[string]any{"score": float64(40)})

	// Greater than 15.
	results := idx.Search([]Filter{{Field: "score", Op: OpGt, Value: float64(15)}})
	sort.Strings(results)
	if len(results) != 3 {
		t.Fatalf("expected 3 results for >15, got %d: %v", len(results), results)
	}

	// Less than or equal to 20.
	results = idx.Search([]Filter{{Field: "score", Op: OpLte, Value: float64(20)}})
	sort.Strings(results)
	if len(results) != 2 {
		t.Fatalf("expected 2 results for <=20, got %d: %v", len(results), results)
	}
}

func TestAttributeIndexBool(t *testing.T) {
	idx := NewAttributeIndex()
	idx.IndexDocument("doc1", map[string]any{"active": true})
	idx.IndexDocument("doc2", map[string]any{"active": false})
	idx.IndexDocument("doc3", map[string]any{"active": true})

	results := idx.Search([]Filter{{Field: "active", Op: OpEq, Value: true}})
	sort.Strings(results)
	if len(results) != 2 {
		t.Fatalf("expected 2 active docs, got %d: %v", len(results), results)
	}
}

func TestAttributeIndexNeq(t *testing.T) {
	idx := NewAttributeIndex()
	idx.IndexDocument("doc1", map[string]any{"status": "active"})
	idx.IndexDocument("doc2", map[string]any{"status": "inactive"})
	idx.IndexDocument("doc3", map[string]any{"status": "active"})

	results := idx.Search([]Filter{{Field: "status", Op: OpNeq, Value: "active"}})
	if len(results) != 1 || results[0] != "doc2" {
		t.Fatalf("expected [doc2], got %v", results)
	}
}

func TestAttributeIndexIn(t *testing.T) {
	idx := NewAttributeIndex()
	idx.IndexDocument("doc1", map[string]any{"color": "red"})
	idx.IndexDocument("doc2", map[string]any{"color": "blue"})
	idx.IndexDocument("doc3", map[string]any{"color": "green"})

	results := idx.Search([]Filter{{Field: "color", Op: OpIn, Value: []any{"red", "blue"}}})
	sort.Strings(results)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(results), results)
	}
}

func TestAttributeIndexContains(t *testing.T) {
	idx := NewAttributeIndex()
	idx.IndexDocument("doc1", map[string]any{"tags": []any{"go", "rust"}})
	idx.IndexDocument("doc2", map[string]any{"tags": []any{"python", "go"}})
	idx.IndexDocument("doc3", map[string]any{"tags": []any{"java"}})

	results := idx.Search([]Filter{{Field: "tags", Op: OpContains, Value: "go"}})
	sort.Strings(results)
	if len(results) != 2 {
		t.Fatalf("expected 2 docs with 'go' tag, got %d: %v", len(results), results)
	}
}

func TestAttributeIndexDelete(t *testing.T) {
	idx := NewAttributeIndex()
	idx.IndexDocument("doc1", map[string]any{"color": "red"})
	idx.IndexDocument("doc2", map[string]any{"color": "red"})

	idx.DeleteDocument("doc1")

	results := idx.Search([]Filter{{Field: "color", Op: OpEq, Value: "red"}})
	if len(results) != 1 || results[0] != "doc2" {
		t.Fatalf("expected [doc2] after delete, got %v", results)
	}

	if idx.Count() != 1 {
		t.Fatalf("expected count 1, got %d", idx.Count())
	}
}

func TestAttributeIndexReindex(t *testing.T) {
	idx := NewAttributeIndex()
	idx.IndexDocument("doc1", map[string]any{"color": "red"})

	// Re-index with different value.
	idx.IndexDocument("doc1", map[string]any{"color": "blue"})

	// Should not find "red" anymore.
	results := idx.Search([]Filter{{Field: "color", Op: OpEq, Value: "red"}})
	if len(results) != 0 {
		t.Fatalf("expected no results for red after reindex, got %v", results)
	}

	// Should find "blue".
	results = idx.Search([]Filter{{Field: "color", Op: OpEq, Value: "blue"}})
	if len(results) != 1 || results[0] != "doc1" {
		t.Fatalf("expected [doc1] for blue, got %v", results)
	}
}

func TestAttributeIndexMultipleFilters(t *testing.T) {
	idx := NewAttributeIndex()
	idx.IndexDocument("doc1", map[string]any{"color": "red", "size": float64(10)})
	idx.IndexDocument("doc2", map[string]any{"color": "red", "size": float64(20)})
	idx.IndexDocument("doc3", map[string]any{"color": "blue", "size": float64(10)})

	// color=red AND size>15 -> only doc2
	results := idx.Search([]Filter{
		{Field: "color", Op: OpEq, Value: "red"},
		{Field: "size", Op: OpGt, Value: float64(15)},
	})
	if len(results) != 1 || results[0] != "doc2" {
		t.Fatalf("expected [doc2], got %v", results)
	}
}

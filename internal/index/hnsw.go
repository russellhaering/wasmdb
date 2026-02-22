package index

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/coder/hnsw"
)

// VectorIndex wraps an HNSW vector index for similarity search.
type VectorIndex struct {
	mu    sync.RWMutex
	graph *hnsw.Graph[string]
	dims  int
}

// NewVectorIndex creates a new in-memory HNSW vector index.
func NewVectorIndex(dimensions int) *VectorIndex {
	g := hnsw.NewGraph[string]()
	return &VectorIndex{
		graph: g,
		dims:  dimensions,
	}
}

// Add inserts or updates a vector for the given document ID.
func (v *VectorIndex) Add(docID string, embedding []float32) error {
	if len(embedding) != v.dims {
		return fmt.Errorf("vector: expected %d dimensions, got %d", v.dims, len(embedding))
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	vec := make([]float32, len(embedding))
	copy(vec, embedding)
	v.graph.Add(hnsw.MakeNode(docID, vec))
	return nil
}

// Delete removes a document from the vector index.
func (v *VectorIndex) Delete(docID string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.graph.Delete(docID)
}

// VectorSearchResult holds a vector search result.
type VectorSearchResult struct {
	DocID    string
	Distance float32
}

// Search finds the k nearest neighbors to the query vector.
func (v *VectorIndex) Search(query []float32, k int) ([]VectorSearchResult, error) {
	if len(query) != v.dims {
		return nil, fmt.Errorf("vector: expected %d dimensions, got %d", v.dims, len(query))
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	neighbors := v.graph.Search(query, k)
	results := make([]VectorSearchResult, len(neighbors))
	for i, n := range neighbors {
		results[i] = VectorSearchResult{
			DocID: n.Key,
		}
	}
	return results, nil
}

// Save serializes the vector index to disk using the hnsw Export format.
func (v *VectorIndex) Save(cacheDir, dbName string) error {
	v.mu.RLock()
	defer v.mu.RUnlock()

	dir := filepath.Join(cacheDir, "hnsw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	path := filepath.Join(dir, dbName+".hnsw")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return v.graph.Export(f)
}

// LoadVectorIndex loads a vector index from disk, or returns nil if not found.
func LoadVectorIndex(cacheDir, dbName string, dimensions int) *VectorIndex {
	path := filepath.Join(cacheDir, "hnsw", dbName+".hnsw")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	g := hnsw.NewGraph[string]()
	if err := g.Import(f); err != nil {
		return nil
	}

	return &VectorIndex{
		graph: g,
		dims:  dimensions,
	}
}

// Count returns the approximate number of vectors in the index.
func (v *VectorIndex) Count() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.graph.Len()
}

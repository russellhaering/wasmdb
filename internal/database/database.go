package database

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/embedding"
	"github.com/russellhaering/wasmdb/internal/index"
	"github.com/russellhaering/wasmdb/internal/storage/lsm"
)

// indexOp represents an async index operation.
type indexOp struct {
	doc    *document.Document // nil for deletes
	delete string            // non-empty for deletes
}

// Table ties together storage, indexes, and embeddings for a single table.
type Table struct {
	Name   string
	System bool
	Schema *document.Schema

	db       *lsm.DB
	cacheDir string

	// mu protects bleve, vector, attrs, and indexCh during index rebuilds.
	mu      sync.RWMutex
	bleve   *index.BleveIndex
	vector  *index.VectorIndex
	attrs   *index.AttributeIndex
	builder *index.Builder
	indexCh chan indexOp
	indexWg sync.WaitGroup

	embedder *embedding.Pipeline

	reembedCancel context.CancelFunc
	reembedWg     sync.WaitGroup
}

// TableConfig configures a table instance.
type TableConfig struct {
	Name     string
	System   bool
	Schema   *document.Schema
	DB       *lsm.DB
	CacheDir string
	Embedder *embedding.Pipeline
}

// NewTable creates a new Table, initializing indexes and the builder.
func NewTable(cfg TableConfig) (*Table, error) {
	// Create attribute index (always available).
	attrs := index.NewAttributeIndex()

	// Create Bleve full-text index.
	var bleveIdx *index.BleveIndex
	if cfg.CacheDir != "" {
		var err error
		bleveIdx, err = index.NewBleveIndex(cfg.CacheDir, cfg.Name, cfg.Schema)
		if err != nil {
			slog.Warn("failed to create bleve index", "db", cfg.Name, "err", err)
		}
	}

	// Create HNSW vector index.
	var vectorIdx *index.VectorIndex
	if cfg.Schema != nil && cfg.Schema.EmbeddingDimensions > 0 {
		// Try to load from disk first.
		vectorIdx = index.LoadVectorIndex(cfg.CacheDir, cfg.Name, cfg.Schema.EmbeddingDimensions)
		if vectorIdx == nil {
			vectorIdx = index.NewVectorIndex(cfg.Schema.EmbeddingDimensions)
		}
	}

	d := &Table{
		Name:     cfg.Name,
		System:   cfg.System,
		Schema:   cfg.Schema,
		db:       cfg.DB,
		cacheDir: cfg.CacheDir,
		bleve:    bleveIdx,
		vector:   vectorIdx,
		attrs:    attrs,
		embedder: cfg.Embedder,
		indexCh:  make(chan indexOp, 1024),
	}

	// Start async index worker.
	d.indexWg.Add(1)
	go d.indexWorker()

	// Start index builder (handles initial rebuild from existing data on startup).
	d.builder = index.NewBuilder(index.BuilderConfig{
		DB:       cfg.DB,
		Schema:   cfg.Schema,
		Bleve:    bleveIdx,
		Vector:   vectorIdx,
		Attrs:    attrs,
		CacheDir: cfg.CacheDir,
		DBName:   cfg.Name,
	})

	return d, nil
}

// PutDocument validates, embeds, serializes, and stores a document.
func (d *Table) PutDocument(ctx context.Context, doc *document.Document) error {
	// Validate schema.
	if d.Schema != nil && len(doc.Attributes) > 0 {
		if err := d.Schema.Validate(doc.Attributes); err != nil {
			return fmt.Errorf("validation: %w", err)
		}
	}

	now := time.Now().UTC()

	// Generate ID if not set. Generated IDs are unique (ULID),
	// so we can skip the existence check.
	isNew := doc.ID == ""
	if isNew {
		doc.ID = ulid.Make().String()
	}

	// Check if this is an update (existing doc). Skip for new docs
	// with generated IDs since they can't already exist.
	if !isNew {
		existing, ok, err := d.db.Get(ctx, doc.ID)
		if err != nil {
			return fmt.Errorf("check existing: %w", err)
		}

		if ok && existing.Value != nil {
			existingDoc, err := document.Deserialize(existing.Value)
			if err == nil {
				doc.CreatedAt = existingDoc.CreatedAt
				doc.Version = existingDoc.Version + 1
			}
		}
	}

	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = now
	}
	doc.UpdatedAt = now
	if doc.Version == 0 {
		doc.Version = 1
	}

	// Generate embedding if configured.
	if d.embedder != nil && d.Schema != nil && d.Schema.EmbeddingModel != "" {
		text := buildEmbeddingText(doc)
		if text != "" {
			emb, err := d.embedder.Embed(ctx, text)
			if err != nil {
				return fmt.Errorf("embedding: %w", err)
			}
			doc.Embedding = emb
			doc.EmbeddingModel = d.Schema.EmbeddingModel
		}
	}

	// Serialize.
	data, err := document.Serialize(doc)
	if err != nil {
		return fmt.Errorf("serialize: %w", err)
	}

	// Write to LSM.
	if _, err := d.db.Put(ctx, doc.ID, data); err != nil {
		return fmt.Errorf("put: %w", err)
	}

	// Flush for strong read-after-write consistency.
	if err := d.db.Flush(ctx); err != nil {
		return fmt.Errorf("flush: %w", err)
	}

	// Index inline — no need to wait for background builder.
	d.indexDocument(doc)

	return nil
}

// PutDocumentsBulk validates, serializes, and stores multiple documents with a single flush.
func (d *Table) PutDocumentsBulk(ctx context.Context, docs []*document.Document) error {
	now := time.Now().UTC()

	for _, doc := range docs {
		// Validate schema.
		if d.Schema != nil && len(doc.Attributes) > 0 {
			if err := d.Schema.Validate(doc.Attributes); err != nil {
				return fmt.Errorf("validation (doc %s): %w", doc.ID, err)
			}
		}

		// Generate ID if not set.
		if doc.ID == "" {
			doc.ID = ulid.Make().String()
		}

		if doc.CreatedAt.IsZero() {
			doc.CreatedAt = now
		}
		doc.UpdatedAt = now
		if doc.Version == 0 {
			doc.Version = 1
		}

		// Serialize.
		data, err := document.Serialize(doc)
		if err != nil {
			return fmt.Errorf("serialize (doc %s): %w", doc.ID, err)
		}

		// Write to LSM (MemTable only, no flush yet).
		if _, err := d.db.Put(ctx, doc.ID, data); err != nil {
			return fmt.Errorf("put (doc %s): %w", doc.ID, err)
		}
	}

	// Single flush for the entire batch.
	if err := d.db.Flush(ctx); err != nil {
		return fmt.Errorf("flush: %w", err)
	}

	// Index inline.
	for _, doc := range docs {
		d.indexDocument(doc)
	}

	return nil
}

// GetDocument retrieves a document by ID.
func (d *Table) GetDocument(ctx context.Context, id string) (*document.Document, error) {
	entry, ok, err := d.db.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	if !ok {
		return nil, nil
	}
	if entry.Value == nil {
		return nil, nil // tombstone
	}

	doc, err := document.Deserialize(entry.Value)
	if err != nil {
		return nil, fmt.Errorf("deserialize: %w", err)
	}
	doc.ID = id
	return doc, nil
}

// DeleteDocument deletes a document by ID.
func (d *Table) DeleteDocument(ctx context.Context, id string) error {
	if _, err := d.db.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if err := d.db.Flush(ctx); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	d.deindexDocument(id)
	return nil
}

// indexDocument enqueues a document for async indexing.
func (d *Table) indexDocument(doc *document.Document) {
	d.mu.RLock()
	ch := d.indexCh
	d.mu.RUnlock()
	ch <- indexOp{doc: doc}
}

// deindexDocument enqueues a document removal for async indexing.
func (d *Table) deindexDocument(id string) {
	d.mu.RLock()
	ch := d.indexCh
	d.mu.RUnlock()
	ch <- indexOp{delete: id}
}

// indexWorker drains the index channel and applies operations.
func (d *Table) indexWorker() {
	defer d.indexWg.Done()
	for op := range d.indexCh {
		if op.delete != "" {
			if d.bleve != nil {
				d.bleve.DeleteDocument(op.delete)
			}
			if d.vector != nil {
				d.vector.Delete(op.delete)
			}
			d.attrs.DeleteDocument(op.delete)
			continue
		}
		doc := op.doc
		if d.bleve != nil {
			if err := d.bleve.IndexDocument(doc); err != nil {
				slog.Warn("async bleve index failed", "doc", doc.ID, "err", err)
			}
		}
		if d.vector != nil && len(doc.Embedding) > 0 {
			if err := d.vector.Add(doc.ID, doc.Embedding); err != nil {
				slog.Warn("async vector index failed", "doc", doc.ID, "err", err)
			}
		}
		if len(doc.Attributes) > 0 {
			d.attrs.IndexDocument(doc.ID, doc.Attributes)
		}
	}
}

// SearchVector performs a vector similarity search.
func (d *Table) SearchVector(ctx context.Context, query []float32, k int) ([]*document.Document, error) {
	d.mu.RLock()
	v := d.vector
	d.mu.RUnlock()

	if v == nil {
		return nil, fmt.Errorf("vector search not configured (no embedding dimensions)")
	}

	results, err := v.Search(query, k)
	if err != nil {
		return nil, err
	}

	return d.fetchDocs(ctx, vectorResultIDs(results))
}

// SearchVectorByText embeds the query text and performs vector search.
func (d *Table) SearchVectorByText(ctx context.Context, queryText string, k int) ([]*document.Document, error) {
	if d.embedder == nil {
		return nil, fmt.Errorf("embedding not configured")
	}

	queryVec, err := d.embedder.Embed(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	return d.SearchVector(ctx, queryVec, k)
}

// SearchText performs a full-text search.
func (d *Table) SearchText(ctx context.Context, query string, limit, offset int) ([]*document.Document, int, error) {
	d.mu.RLock()
	b := d.bleve
	d.mu.RUnlock()

	if b == nil {
		return nil, 0, fmt.Errorf("full-text search not available")
	}

	results, total, err := b.Search(query, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.DocID
	}

	docs, err := d.fetchDocs(ctx, ids)
	return docs, total, err
}

// SearchAttributes performs an attribute filter search.
func (d *Table) SearchAttributes(ctx context.Context, filters []index.Filter, limit, offset int) ([]*document.Document, error) {
	d.mu.RLock()
	a := d.attrs
	d.mu.RUnlock()

	ids := a.Search(filters)

	// Apply pagination.
	if offset >= len(ids) {
		return []*document.Document{}, nil
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	ids = ids[offset:end]

	return d.fetchDocs(ctx, ids)
}

// ListDocuments returns up to limit documents with key > afterKey using cursor-based pagination.
// Returns the documents, whether more results exist, and any error.
func (d *Table) ListDocuments(ctx context.Context, limit int, afterKey string) ([]*document.Document, bool, error) {
	result, err := d.db.ScanRange(ctx, afterKey, limit)
	if err != nil {
		return nil, false, fmt.Errorf("scan range: %w", err)
	}

	var docs []*document.Document
	for _, entry := range result.Entries {
		doc, err := document.Deserialize(entry.Value)
		if err != nil {
			continue
		}
		doc.ID = entry.Key
		docs = append(docs, doc)
	}

	return docs, result.HasMore, nil
}

func (d *Table) fetchDocs(ctx context.Context, ids []string) ([]*document.Document, error) {
	docs := make([]*document.Document, 0, len(ids))
	for _, id := range ids {
		doc, err := d.GetDocument(ctx, id)
		if err != nil {
			return nil, err
		}
		if doc != nil {
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

func vectorResultIDs(results []index.VectorSearchResult) []string {
	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.DocID
	}
	return ids
}

func buildEmbeddingText(doc *document.Document) string {
	var parts []string
	if doc.Content != "" {
		parts = append(parts, doc.Content)
	}
	for _, v := range doc.Attributes {
		if s, ok := v.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

// stopIndexing stops the builder and drains the index worker without closing the LSM.
func (d *Table) stopIndexing() {
	if d.builder != nil {
		d.builder.Close()
		d.builder = nil
	}
	close(d.indexCh)
	d.indexWg.Wait()
}

// Close shuts down the table and its indexes.
func (d *Table) Close() error {
	// Cancel any active re-embed job.
	if d.reembedCancel != nil {
		d.reembedCancel()
	}
	d.reembedWg.Wait()

	d.stopIndexing()
	if d.bleve != nil {
		d.bleve.Close()
	}
	return d.db.Close()
}

// RebuildIndexes detects schema changes and rebuilds affected indexes.
func (d *Table) RebuildIndexes(ctx context.Context, oldSchema, newSchema *document.Schema) error {
	changes := document.DiffSchemas(oldSchema, newSchema)
	if !changes.Changed() {
		d.Schema = newSchema
		return nil
	}

	slog.Info("schema change detected, rebuilding indexes",
		"db", d.Name,
		"embedding_changed", changes.EmbeddingChanged,
		"indexed_changed", changes.IndexedFieldsChanged,
		"fulltext_changed", changes.FullTextFieldsChanged,
	)

	// Cancel any running re-embed job.
	if d.reembedCancel != nil {
		d.reembedCancel()
	}
	d.reembedWg.Wait()
	d.reembedCancel = nil

	d.mu.Lock()

	// Stop old indexing pipeline.
	d.stopIndexing()

	// Rebuild Bleve if full_text fields changed.
	if changes.FullTextFieldsChanged {
		if d.bleve != nil {
			oldPath := d.bleve.Path()
			d.bleve.Close()
			os.RemoveAll(oldPath)
			d.bleve = nil
		}
		if d.cacheDir != "" {
			var err error
			d.bleve, err = index.NewBleveIndex(d.cacheDir, d.Name, newSchema)
			if err != nil {
				slog.Warn("failed to create bleve index during rebuild", "db", d.Name, "err", err)
			}
		}
	}

	// Rebuild attribute index if indexed fields changed.
	if changes.IndexedFieldsChanged {
		d.attrs = index.NewAttributeIndex()
	}

	// Rebuild vector index if embedding config changed.
	if changes.EmbeddingChanged {
		// Delete old HNSW cache.
		if d.cacheDir != "" {
			os.Remove(filepath.Join(d.cacheDir, "hnsw", d.Name+".hnsw"))
		}
		d.vector = nil
		if newSchema != nil && newSchema.EmbeddingDimensions > 0 {
			d.vector = index.NewVectorIndex(newSchema.EmbeddingDimensions)
		}
	}

	d.Schema = newSchema

	// Create new index channel and worker.
	d.indexCh = make(chan indexOp, 1024)
	d.indexWg.Add(1)
	go d.indexWorker()

	d.mu.Unlock()

	// Delete checkpoint to force full rebuild.
	if d.cacheDir != "" {
		index.DeleteCheckpoint(d.cacheDir, d.Name)
	}

	// Start new builder.
	d.builder = index.NewBuilder(index.BuilderConfig{
		DB:       d.db,
		Schema:   newSchema,
		Bleve:    d.bleve,
		Vector:   d.vector,
		Attrs:    d.attrs,
		CacheDir: d.cacheDir,
		DBName:   d.Name,
	})

	// If embedding config changed and we have a model, start background re-embed.
	if changes.EmbeddingChanged && d.embedder != nil && newSchema != nil && newSchema.EmbeddingModel != "" {
		reembedCtx, cancel := context.WithCancel(context.Background())
		d.reembedCancel = cancel
		job := &reembedJob{
			db:    d,
			model: newSchema.EmbeddingModel,
		}
		d.reembedWg.Add(1)
		go func() {
			defer d.reembedWg.Done()
			job.run(reembedCtx)
		}()
	}

	return nil
}

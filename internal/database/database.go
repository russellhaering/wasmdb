package database

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/embedding"
	"github.com/russellhaering/wasmdb/internal/index"
	"github.com/russellhaering/wasmdb/internal/storage/lsm"
)

// Database ties together storage, indexes, and embeddings for a single database.
type Database struct {
	Name   string
	Schema *document.Schema

	db      *lsm.DB
	bleve   *index.BleveIndex
	vector  *index.VectorIndex
	attrs   *index.AttributeIndex
	builder *index.Builder

	embedder *embedding.Pipeline
}

// DatabaseConfig configures a database instance.
type DatabaseConfig struct {
	Name     string
	Schema   *document.Schema
	DB       *lsm.DB
	CacheDir string
	Embedder *embedding.Pipeline
}

// NewDatabase creates a new Database, initializing indexes and the builder.
func NewDatabase(cfg DatabaseConfig) (*Database, error) {
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

	d := &Database{
		Name:     cfg.Name,
		Schema:   cfg.Schema,
		db:       cfg.DB,
		bleve:    bleveIdx,
		vector:   vectorIdx,
		attrs:    attrs,
		embedder: cfg.Embedder,
	}

	// Start index builder.
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
func (d *Database) PutDocument(ctx context.Context, doc *document.Document) error {
	// Validate schema.
	if d.Schema != nil && len(doc.Attributes) > 0 {
		if err := d.Schema.Validate(doc.Attributes); err != nil {
			return fmt.Errorf("validation: %w", err)
		}
	}

	now := time.Now().UTC()

	// Generate ID if not set.
	if doc.ID == "" {
		doc.ID = ulid.Make().String()
	}

	// Check if this is an update (existing doc).
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

	return nil
}

// GetDocument retrieves a document by ID.
func (d *Database) GetDocument(ctx context.Context, id string) (*document.Document, error) {
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
func (d *Database) DeleteDocument(ctx context.Context, id string) error {
	if _, err := d.db.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if err := d.db.Flush(ctx); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	return nil
}

// SearchVector performs a vector similarity search.
func (d *Database) SearchVector(ctx context.Context, query []float32, k int) ([]*document.Document, error) {
	if d.vector == nil {
		return nil, fmt.Errorf("vector search not configured (no embedding dimensions)")
	}

	results, err := d.vector.Search(query, k)
	if err != nil {
		return nil, err
	}

	return d.fetchDocs(ctx, vectorResultIDs(results))
}

// SearchVectorByText embeds the query text and performs vector search.
func (d *Database) SearchVectorByText(ctx context.Context, queryText string, k int) ([]*document.Document, error) {
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
func (d *Database) SearchText(ctx context.Context, query string, limit, offset int) ([]*document.Document, int, error) {
	if d.bleve == nil {
		return nil, 0, fmt.Errorf("full-text search not available")
	}

	results, total, err := d.bleve.Search(query, limit, offset)
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
func (d *Database) SearchAttributes(ctx context.Context, filters []index.Filter, limit, offset int) ([]*document.Document, error) {
	ids := d.attrs.Search(filters)

	// Apply pagination.
	if offset >= len(ids) {
		return nil, nil
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	ids = ids[offset:end]

	return d.fetchDocs(ctx, ids)
}

func (d *Database) fetchDocs(ctx context.Context, ids []string) ([]*document.Document, error) {
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

// Close shuts down the database and its indexes.
func (d *Database) Close() error {
	if d.builder != nil {
		d.builder.Close()
	}
	if d.bleve != nil {
		d.bleve.Close()
	}
	return d.db.Close()
}

// UpdateSchema updates the database schema.
func (d *Database) UpdateSchema(schema *document.Schema) {
	d.Schema = schema
}

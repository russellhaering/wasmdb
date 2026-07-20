package database

import (
	"context"
	"sort"
	"strings"
	"sync"

	moraine "github.com/russellhaering/moraine"
	"github.com/russellhaering/moraine/document"
	"github.com/russellhaering/moraine/indexed"
	"github.com/russellhaering/wasmdb/internal/embedding"
)

// Table wraps moraine's indexed.Table, adding background re-embedding support
// and the registry OnWrite hook.
//
// The embedded *indexed.Table promotes the full document API
// (PutDocument/GetDocument/Search*/ListDocuments/Name()/System()/Schema()/
// WaitForIndexes/IndexStatus/SetSchema/RebuildIndexes/Close). The retained
// *moraine.DB is used only for batch re-embed writes, which bypass
// PutDocument's per-document embedding to preserve batching.
//
// PutDocument/PutDocumentsBulk are overridden (rather than relying on the
// promoted methods) so the registry's OnWrite hook fires with the same
// semantics upstream implemented for the pre-moraine engine: once per
// PutDocument and once per non-empty PutDocumentsBulk, only for non-system
// tables.
type Table struct {
	*indexed.Table

	db       *moraine.DB
	embedder *embedding.Pipeline

	// registry back-reference, used only to reach the OnWrite hook. May be nil
	// for tables constructed directly in tests.
	registry *Registry

	mu            sync.Mutex
	reembedCancel context.CancelFunc
	reembedWg     sync.WaitGroup
}

// Close cancels any active re-embed job, waits for it to finish, then closes
// the underlying indexed table (which closes the owned moraine.DB). It is
// idempotent via indexed.Table's closeOnce.
func (t *Table) Close() error {
	t.mu.Lock()
	if t.reembedCancel != nil {
		t.reembedCancel()
		t.reembedCancel = nil
	}
	t.mu.Unlock()
	t.reembedWg.Wait()
	return t.Table.Close()
}

// PutDocument stores a document via the embedded indexed table, then fires the
// registry OnWrite hook. The hook must run here because the embedded
// *indexed.Table's PutDocument would otherwise be promoted directly and bypass
// it.
func (t *Table) PutDocument(ctx context.Context, doc *document.Document) error {
	if err := t.Table.PutDocument(ctx, doc); err != nil {
		return err
	}
	t.fireOnWrite()
	return nil
}

// PutDocumentsBulk stores multiple documents via the embedded indexed table,
// then fires the registry OnWrite hook exactly once for a non-empty batch.
func (t *Table) PutDocumentsBulk(ctx context.Context, docs []*document.Document) error {
	if err := t.Table.PutDocumentsBulk(ctx, docs); err != nil {
		return err
	}
	if len(docs) > 0 {
		t.fireOnWrite()
	}
	return nil
}

// fireOnWrite invokes the registry's OnWrite hook for non-system tables. It
// mirrors the lock-free style of OnSchemaChange: the hook is read without
// additional locking and must be installed before writes begin. Handlers run
// inline, so they must be cheap.
func (t *Table) fireOnWrite() {
	if t.System() || strings.HasPrefix(t.Name(), "_") {
		return
	}
	if t.registry != nil && t.registry.OnWrite != nil {
		t.registry.OnWrite(t.Name())
	}
}

// startReembed cancels any prior re-embed job and starts a fresh one for the
// given embedding model.
func (t *Table) startReembed(model string) {
	// Cancel and drain any prior job first.
	t.mu.Lock()
	if t.reembedCancel != nil {
		t.reembedCancel()
	}
	t.mu.Unlock()
	t.reembedWg.Wait()

	ctx, cancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.reembedCancel = cancel
	t.mu.Unlock()

	job := &reembedJob{tbl: t, model: model}
	t.reembedWg.Add(1)
	go func() {
		defer t.reembedWg.Done()
		job.run(ctx)
	}()
}

// buildEmbeddingText mirrors moraine/indexed's embedding text construction so
// re-embedded vectors match what PutDocument would produce for the same doc.
func buildEmbeddingText(doc *document.Document) string {
	var parts []string
	if doc.Content != "" {
		parts = append(parts, doc.Content)
	}
	keys := make([]string, 0, len(doc.Attributes))
	for key := range doc.Attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v := doc.Attributes[key]
		switch vv := v.(type) {
		case string:
			parts = append(parts, vv)
		case []string:
			parts = append(parts, vv...)
		case []any:
			for _, elem := range vv {
				if s, ok := elem.(string); ok {
					parts = append(parts, s)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

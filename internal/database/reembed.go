package database

import (
	"context"
	"log/slog"
	"time"

	"github.com/russellhaering/wasmdb/internal/document"
)

// reembedJob re-embeds documents in the background when the embedding model changes.
type reembedJob struct {
	db    *Database
	model string
}

func (j *reembedJob) run(ctx context.Context) {
	slog.Info("reembed: starting", "db", j.db.Name, "model", j.model)

	entries, err := j.db.db.Scan(ctx)
	if err != nil {
		slog.Error("reembed: scan failed", "db", j.db.Name, "err", err)
		return
	}

	const batchSize = 50
	var (
		total     int
		skipped   int
		embedded  int
		batchDocs []*document.Document
		batchTexts []string
	)

	flush := func() {
		if len(batchDocs) == 0 {
			return
		}

		embeddings, err := j.db.embedder.EmbedBatch(ctx, batchTexts)
		if err != nil {
			slog.Error("reembed: embed batch failed", "db", j.db.Name, "err", err)
			return
		}

		for i, doc := range batchDocs {
			if i >= len(embeddings) {
				break
			}
			doc.Embedding = embeddings[i]
			doc.EmbeddingModel = j.model
			doc.UpdatedAt = time.Now().UTC()

			data, err := document.Serialize(doc)
			if err != nil {
				slog.Error("reembed: serialize failed", "doc", doc.ID, "err", err)
				continue
			}

			if _, err := j.db.db.Put(ctx, doc.ID, data); err != nil {
				slog.Error("reembed: put failed", "doc", doc.ID, "err", err)
				continue
			}

			j.db.indexDocument(doc)
			embedded++
		}

		// Flush the LSM batch.
		if err := j.db.db.Flush(ctx); err != nil {
			slog.Error("reembed: flush failed", "db", j.db.Name, "err", err)
		}

		batchDocs = batchDocs[:0]
		batchTexts = batchTexts[:0]
	}

	for _, e := range entries {
		if ctx.Err() != nil {
			slog.Info("reembed: cancelled", "db", j.db.Name, "embedded", embedded, "skipped", skipped)
			return
		}

		if e.Value == nil {
			continue
		}
		total++

		doc, err := document.Deserialize(e.Value)
		if err != nil {
			slog.Error("reembed: deserialize failed", "key", e.Key, "err", err)
			continue
		}
		doc.ID = e.Key

		// Skip if already embedded with the correct model.
		if doc.EmbeddingModel == j.model {
			skipped++
			continue
		}

		text := buildEmbeddingText(doc)
		if text == "" {
			skipped++
			continue
		}

		batchDocs = append(batchDocs, doc)
		batchTexts = append(batchTexts, text)

		if len(batchDocs) >= batchSize {
			flush()

			// Small sleep between batches to yield to API traffic.
			select {
			case <-ctx.Done():
				slog.Info("reembed: cancelled", "db", j.db.Name, "embedded", embedded, "skipped", skipped)
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}

	// Flush remaining.
	flush()

	slog.Info("reembed: complete", "db", j.db.Name, "total", total, "embedded", embedded, "skipped", skipped)
}

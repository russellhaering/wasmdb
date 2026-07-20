package database

import (
	"context"
	"log/slog"
	"time"

	"github.com/russellhaering/moraine/document"
)

// reembedJob re-embeds documents in the background when the embedding model
// changes. It writes directly to the retained moraine.DB (bypassing the
// indexed layer's per-document embedding) to preserve batching, then triggers a
// full index rebuild so the new vectors land in HNSW and the checkpoint advances.
type reembedJob struct {
	tbl   *Table
	model string
}

func (j *reembedJob) run(ctx context.Context) {
	t := j.tbl
	slog.Info("reembed: starting", "db", t.Name(), "model", j.model)

	const batchSize = 50
	var (
		total    int
		skipped  int
		embedded int
		afterKey string
	)

	for {
		if ctx.Err() != nil {
			slog.Info("reembed: cancelled", "db", t.Name(), "embedded", embedded, "skipped", skipped)
			return
		}

		docs, hasMore, err := t.ListDocuments(ctx, batchSize, afterKey)
		if err != nil {
			slog.Error("reembed: list failed", "db", t.Name(), "err", err)
			return
		}
		if len(docs) == 0 {
			break
		}

		var (
			batchDocs  []*document.Document
			batchTexts []string
		)
		for _, doc := range docs {
			afterKey = doc.ID
			total++

			// Skip if already embedded with the target model.
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
		}

		if len(batchDocs) > 0 {
			embeddings, err := t.embedder.EmbedBatch(ctx, batchTexts)
			if err != nil {
				slog.Error("reembed: embed batch failed", "db", t.Name(), "err", err)
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

				if _, err := t.db.Put(ctx, doc.ID, data); err != nil {
					slog.Error("reembed: put failed", "doc", doc.ID, "err", err)
					continue
				}
				embedded++
			}

			// Flush the LSM batch.
			if err := t.db.Flush(ctx); err != nil {
				slog.Error("reembed: flush failed", "db", t.Name(), "err", err)
			}

			// Small sleep between batches to yield to API traffic.
			select {
			case <-ctx.Done():
				slog.Info("reembed: cancelled", "db", t.Name(), "embedded", embedded, "skipped", skipped)
				return
			case <-time.After(100 * time.Millisecond):
			}
		}

		if !hasMore {
			break
		}
	}

	// Rebuild derived indexes so re-embedded vectors land in HNSW and the
	// checkpoint advances past the batch writes.
	if err := t.RebuildIndexes(ctx); err != nil {
		slog.Error("reembed: rebuild indexes failed", "db", t.Name(), "err", err)
		return
	}

	slog.Info("reembed: complete", "db", t.Name(), "total", total, "embedded", embedded, "skipped", skipped)
}

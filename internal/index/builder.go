package index

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/storage/lsm"
)

// Builder is a background goroutine that tails the LSM for new entries
// and indexes them in Bleve, HNSW, and attribute indexes.
type Builder struct {
	db       *lsm.DB
	schema   *document.Schema
	bleve    *BleveIndex
	vector   *VectorIndex
	attrs    *AttributeIndex
	cacheDir string
	dbName   string

	mu         sync.Mutex
	lastSeqNum uint64

	closeCh chan struct{}
	wg      sync.WaitGroup
}

// BuilderConfig configures the index builder.
type BuilderConfig struct {
	DB       *lsm.DB
	Schema   *document.Schema
	Bleve    *BleveIndex
	Vector   *VectorIndex
	Attrs    *AttributeIndex
	CacheDir string
	DBName   string
}

// NewBuilder creates and starts an index builder.
func NewBuilder(cfg BuilderConfig) *Builder {
	b := &Builder{
		db:       cfg.DB,
		schema:   cfg.Schema,
		bleve:    cfg.Bleve,
		vector:   cfg.Vector,
		attrs:    cfg.Attrs,
		cacheDir: cfg.CacheDir,
		dbName:   cfg.DBName,
		closeCh:  make(chan struct{}),
	}

	// Load checkpoint.
	b.lastSeqNum = b.loadCheckpoint()

	b.wg.Add(1)
	go b.run()
	return b
}

func (b *Builder) run() {
	defer b.wg.Done()

	// Validate checkpoint against actual data. If the checkpoint's seqnum
	// exceeds the max seqnum in the store (e.g., after a restart with a fresh
	// in-memory store), reset and do a full rebuild.
	if b.lastSeqNum > 0 {
		if maxSeq := b.maxSeqInStore(); maxSeq < b.lastSeqNum {
			slog.Info("index: stale checkpoint, triggering rebuild",
				"checkpoint_seq", b.lastSeqNum, "store_max_seq", maxSeq, "db", b.dbName)
			b.lastSeqNum = 0
		}
	}

	// Initial full rebuild if no checkpoint.
	if b.lastSeqNum == 0 {
		if err := b.fullRebuild(); err != nil {
			slog.Error("index: full rebuild failed", "err", err)
		}
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-b.closeCh:
			return
		case <-ticker.C:
			if err := b.poll(); err != nil {
				slog.Error("index: poll failed", "err", err)
			}
		}
	}
}

func (b *Builder) maxSeqInStore() uint64 {
	entries, err := b.db.Scan(context.Background())
	if err != nil {
		return 0
	}
	var maxSeq uint64
	for _, e := range entries {
		if e.SeqNum > maxSeq {
			maxSeq = e.SeqNum
		}
	}
	return maxSeq
}

func (b *Builder) fullRebuild() error {
	ctx := context.Background()
	entries, err := b.db.Scan(ctx)
	if err != nil {
		return fmt.Errorf("index: scan: %w", err)
	}

	slog.Info("index: rebuilding", "entries", len(entries), "db", b.dbName)

	for _, e := range entries {
		if err := b.indexEntry(e); err != nil {
			slog.Error("index: rebuild entry", "key", e.Key, "err", err)
		}
		if e.SeqNum > b.lastSeqNum {
			b.lastSeqNum = e.SeqNum
		}
	}

	b.saveCheckpoint()
	slog.Info("index: rebuild complete", "entries", len(entries), "db", b.dbName)
	return nil
}

func (b *Builder) poll() error {
	ctx := context.Background()
	entries, err := b.db.Scan(ctx)
	if err != nil {
		return err
	}

	var indexed int
	for _, e := range entries {
		if e.SeqNum <= b.lastSeqNum {
			continue
		}
		if err := b.indexEntry(e); err != nil {
			slog.Error("index: poll entry", "key", e.Key, "err", err)
			continue
		}
		if e.SeqNum > b.lastSeqNum {
			b.lastSeqNum = e.SeqNum
		}
		indexed++
	}

	if indexed > 0 {
		b.saveCheckpoint()
	}
	return nil
}

func (b *Builder) indexEntry(e lsm.Entry) error {
	if e.Value == nil {
		// Tombstone: delete from all indexes.
		if b.bleve != nil {
			b.bleve.DeleteDocument(e.Key)
		}
		if b.vector != nil {
			b.vector.Delete(e.Key)
		}
		b.attrs.DeleteDocument(e.Key)
		return nil
	}

	doc, err := document.Deserialize(e.Value)
	if err != nil {
		return fmt.Errorf("deserialize %s: %w", e.Key, err)
	}
	doc.ID = e.Key

	// Index in Bleve.
	if b.bleve != nil {
		if err := b.bleve.IndexDocument(doc); err != nil {
			return fmt.Errorf("bleve index %s: %w", e.Key, err)
		}
	}

	// Index in HNSW.
	if b.vector != nil && len(doc.Embedding) > 0 {
		if err := b.vector.Add(doc.ID, doc.Embedding); err != nil {
			return fmt.Errorf("vector index %s: %w", e.Key, err)
		}
	}

	// Index attributes.
	if len(doc.Attributes) > 0 {
		b.attrs.IndexDocument(doc.ID, doc.Attributes)
	}

	return nil
}

func (b *Builder) checkpointPath() string {
	return filepath.Join(b.cacheDir, "checkpoints", b.dbName+".json")
}

type checkpoint struct {
	LastSeqNum uint64 `json:"last_seq_num"`
}

func (b *Builder) saveCheckpoint() {
	path := b.checkpointPath()
	os.MkdirAll(filepath.Dir(path), 0o755)
	data, _ := json.Marshal(checkpoint{LastSeqNum: b.lastSeqNum})
	os.WriteFile(path, data, 0o644)
}

func (b *Builder) loadCheckpoint() uint64 {
	data, err := os.ReadFile(b.checkpointPath())
	if err != nil {
		return 0
	}
	var cp checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return 0
	}
	return cp.LastSeqNum
}

// Close stops the builder and saves checkpoints.
func (b *Builder) Close() {
	close(b.closeCh)
	b.wg.Wait()
	b.saveCheckpoint()
	if b.vector != nil {
		b.vector.Save(b.cacheDir, b.dbName)
	}
}

// LastSeqNum returns the sequence number of the last indexed entry.
func (b *Builder) LastSeqNum() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastSeqNum
}

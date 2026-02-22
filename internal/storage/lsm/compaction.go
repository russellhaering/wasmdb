package lsm

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

// Compactor merges L0 SSTables into sorted runs in the background.
type Compactor struct {
	store     objstore.ObjectStore
	prefix    string
	manifest  *ManifestStore
	threshold int // L0 count that triggers compaction
	epoch     uint64
}

// NewCompactor creates a new compactor.
func NewCompactor(store objstore.ObjectStore, prefix string, threshold int) *Compactor {
	if threshold <= 0 {
		threshold = 4
	}
	return &Compactor{
		store:     store,
		prefix:    prefix,
		manifest:  NewManifestStore(store, prefix),
		threshold: threshold,
	}
}

// MaybeCompact checks if compaction is needed and performs it.
func (c *Compactor) MaybeCompact(ctx context.Context) error {
	m, err := c.manifest.LoadLatest(ctx)
	if err != nil || m == nil {
		return err
	}

	if len(m.L0) < c.threshold {
		return nil
	}

	return c.compact(ctx, m)
}

func (c *Compactor) compact(ctx context.Context, m *Manifest) error {
	slog.Info("compaction starting", "l0_count", len(m.L0))

	// Merge all L0 SSTables into a single sorted run.
	var allEntries []Entry
	for _, sst := range m.L0 {
		data, err := c.store.Get(ctx, sst.Path)
		if err != nil {
			return fmt.Errorf("compactor: read %s: %w", sst.Path, err)
		}
		reader, err := NewSSTableReader(sst.ID, data)
		if err != nil {
			return fmt.Errorf("compactor: parse %s: %w", sst.ID, err)
		}
		iter := reader.Iterator()
		for iter.Next() {
			allEntries = append(allEntries, iter.Entry())
		}
	}

	// Also merge with the first sorted run if it exists, to avoid unbounded
	// growth of sorted runs.
	var existingRuns []SortedRun
	if len(m.SortedRuns) > 0 {
		firstRun := m.SortedRuns[0]
		for _, sst := range firstRun.SSTables {
			data, err := c.store.Get(ctx, sst.Path)
			if err != nil {
				return fmt.Errorf("compactor: read sorted run %s: %w", sst.Path, err)
			}
			reader, err := NewSSTableReader(sst.ID, data)
			if err != nil {
				return fmt.Errorf("compactor: parse sorted run %s: %w", sst.ID, err)
			}
			iter := reader.Iterator()
			for iter.Next() {
				allEntries = append(allEntries, iter.Entry())
			}
		}
		existingRuns = m.SortedRuns[1:] // Keep remaining runs.
	}

	// Deduplicate by key, keeping highest seqnum.
	merged := deduplicateEntries(allEntries)

	// Write merged SSTable.
	sstID := fmt.Sprintf("sorted-L1-%020d", m.Version+1)
	sstPath := fmt.Sprintf("%s/sorted/%s.sst", c.prefix, sstID)

	writer := NewSSTableWriter(sstID, DefaultBlockSize)
	for _, e := range merged {
		writer.Add(e)
	}

	data, meta, err := writer.Finish()
	if err != nil {
		return fmt.Errorf("compactor: write merged sst: %w", err)
	}

	if err := c.store.Put(ctx, sstPath, data, false); err != nil {
		return fmt.Errorf("compactor: put merged sst: %w", err)
	}

	// Build new manifest.
	newSortedRun := SortedRun{
		Level: 1,
		SSTables: []SSTInfo{{
			ID:     meta.ID,
			Path:   sstPath,
			MinKey: meta.MinKey,
			MaxKey: meta.MaxKey,
			MinSeq: meta.MinSeq,
			MaxSeq: meta.MaxSeq,
			Size:   meta.Size,
		}},
	}

	newManifest := &Manifest{
		Version:        m.Version + 1,
		WriterEpoch:    m.WriterEpoch,
		CompactorEpoch: m.CompactorEpoch + 1,
		WALIDNext:      m.WALIDNext,
		L0:             nil, // Clear L0.
		SortedRuns:     append([]SortedRun{newSortedRun}, existingRuns...),
	}

	if err := c.manifest.Save(ctx, newManifest); err != nil {
		return fmt.Errorf("compactor: update manifest: %w", err)
	}

	slog.Info("compaction complete",
		"merged_entries", meta.EntryCount,
		"manifest_version", newManifest.Version)

	return nil
}

// deduplicateEntries keeps the entry with the highest seqnum for each key.
func deduplicateEntries(entries []Entry) []Entry {
	// Sort by key, then by descending seqnum.
	sortEntries(entries)

	best := make(map[string]Entry)
	for _, e := range entries {
		if existing, ok := best[e.Key]; !ok || e.SeqNum > existing.SeqNum {
			best[e.Key] = e
		}
	}

	result := make([]Entry, 0, len(best))
	for _, e := range best {
		result = append(result, e)
	}
	sortEntries(result)
	return result
}

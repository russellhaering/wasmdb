package lsm

import (
	"context"
	"fmt"

	"github.com/russellhaering/wasmdb/internal/storage/cache"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

// Reader provides the read path for the LSM tree. It searches active MemTable,
// frozen MemTables, L0 SSTables, and sorted runs in order.
type Reader struct {
	store      objstore.ObjectStore
	blockCache *cache.LRUBlockCache
	diskCache  *cache.DiskCache
}

// NewReader creates a new Reader.
func NewReader(store objstore.ObjectStore, blockCache *cache.LRUBlockCache, diskCache *cache.DiskCache) *Reader {
	return &Reader{
		store:      store,
		blockCache: blockCache,
		diskCache:  diskCache,
	}
}

// Get searches for a key through all levels of the LSM tree.
// Search order: active MemTable -> frozen MemTables -> L0 SSTables -> sorted runs.
// Returns (entry, true) if found, (nil, false) if not found.
func (r *Reader) Get(ctx context.Context, key string, active *MemTable, frozen []*MemTable, manifest *Manifest) (*Entry, bool, error) {
	// 1. Check active MemTable.
	if e, ok := active.Get(key); ok {
		return e, true, nil
	}

	// 2. Check frozen MemTables (newest first).
	for _, mt := range frozen {
		if e, ok := mt.Get(key); ok {
			return e, true, nil
		}
	}

	// 3. Check L0 SSTables (newest first).
	for _, sst := range manifest.L0 {
		reader, err := r.loadSSTable(ctx, sst)
		if err != nil {
			return nil, false, fmt.Errorf("reader: load L0 %s: %w", sst.ID, err)
		}

		// Bloom filter check.
		if !reader.BloomMayContain(key) {
			continue
		}

		e, err := reader.Get(key)
		if err != nil {
			return nil, false, fmt.Errorf("reader: get from L0 %s: %w", sst.ID, err)
		}
		if e != nil {
			return e, true, nil
		}
	}

	// 4. Check sorted runs (newest first).
	for _, run := range manifest.SortedRuns {
		for _, sst := range run.SSTables {
			// Key range check.
			if key < sst.MinKey || key > sst.MaxKey {
				continue
			}

			reader, err := r.loadSSTable(ctx, sst)
			if err != nil {
				return nil, false, fmt.Errorf("reader: load L%d %s: %w", run.Level, sst.ID, err)
			}

			if !reader.BloomMayContain(key) {
				continue
			}

			e, err := reader.Get(key)
			if err != nil {
				return nil, false, fmt.Errorf("reader: get from L%d %s: %w", run.Level, sst.ID, err)
			}
			if e != nil {
				return e, true, nil
			}
		}
	}

	return nil, false, nil
}

// loadSSTable loads an SSTable, checking disk cache first, then object storage.
func (r *Reader) loadSSTable(ctx context.Context, sst SSTInfo) (*SSTableReader, error) {
	// Check disk cache.
	if r.diskCache != nil {
		data, err := r.diskCache.Get(sst.Path)
		if err == nil && data != nil {
			return NewSSTableReader(sst.ID, data)
		}
	}

	// Load from object storage.
	data, err := r.store.Get(ctx, sst.Path)
	if err != nil {
		return nil, err
	}

	// Cache on disk for future reads.
	if r.diskCache != nil {
		_ = r.diskCache.Put(sst.Path, data)
	}

	return NewSSTableReader(sst.ID, data)
}

// Scan returns all non-tombstone entries in key order across all levels.
// This is primarily used for index building and compaction.
func (r *Reader) Scan(ctx context.Context, active *MemTable, frozen []*MemTable, manifest *Manifest) ([]Entry, error) {
	// Collect all entries with their source priority (lower = newer).
	type prioritizedEntry struct {
		entry    Entry
		priority int
	}

	var all []prioritizedEntry
	priority := 0

	// Active MemTable.
	iter := active.Iterator()
	for iter.Next() {
		all = append(all, prioritizedEntry{entry: iter.Entry(), priority: priority})
	}
	priority++

	// Frozen MemTables.
	for _, mt := range frozen {
		iter := mt.Iterator()
		for iter.Next() {
			all = append(all, prioritizedEntry{entry: iter.Entry(), priority: priority})
		}
		priority++
	}

	// L0 SSTables.
	for _, sst := range manifest.L0 {
		reader, err := r.loadSSTable(ctx, sst)
		if err != nil {
			return nil, err
		}
		si := reader.Iterator()
		for si.Next() {
			all = append(all, prioritizedEntry{entry: si.Entry(), priority: priority})
		}
		priority++
	}

	// Sorted runs.
	for _, run := range manifest.SortedRuns {
		for _, sst := range run.SSTables {
			reader, err := r.loadSSTable(ctx, sst)
			if err != nil {
				return nil, err
			}
			si := reader.Iterator()
			for si.Next() {
				all = append(all, prioritizedEntry{entry: si.Entry(), priority: priority})
			}
		}
		priority++
	}

	// Deduplicate: for each key, keep the entry with the lowest priority (newest).
	seen := make(map[string]int) // key -> index in result
	var result []Entry

	// Sort by key, then priority.
	// We'll use a map to track the best entry per key.
	best := make(map[string]prioritizedEntry)
	for _, pe := range all {
		existing, ok := best[pe.entry.Key]
		if !ok || pe.priority < existing.priority {
			best[pe.entry.Key] = pe
		}
	}

	for _, pe := range best {
		// Skip tombstones.
		if pe.entry.Value == nil {
			continue
		}
		result = append(result, pe.entry)
		seen[pe.entry.Key] = len(result) - 1
	}

	_ = seen // silence unused warning

	// Sort result by key.
	sortEntries(result)
	return result, nil
}

func sortEntries(entries []Entry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].Key < entries[j-1].Key; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

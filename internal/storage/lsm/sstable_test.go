package lsm

import (
	"encoding/binary"
	"fmt"
	"testing"
)

func TestWriteReadRoundTrip(t *testing.T) {
	w := NewSSTableWriter("test-1", DefaultBlockSize)
	w.Add(Entry{Key: "apple", Value: []byte("red"), SeqNum: 1})
	w.Add(Entry{Key: "banana", Value: []byte("yellow"), SeqNum: 2})
	w.Add(Entry{Key: "cherry", Value: []byte("dark-red"), SeqNum: 3})

	data, meta, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if meta.EntryCount != 3 {
		t.Fatalf("expected 3 entries, got %d", meta.EntryCount)
	}
	if meta.MinKey != "apple" {
		t.Fatalf("expected MinKey=apple, got %s", meta.MinKey)
	}
	if meta.MaxKey != "cherry" {
		t.Fatalf("expected MaxKey=cherry, got %s", meta.MaxKey)
	}
	if meta.MinSeq != 1 || meta.MaxSeq != 3 {
		t.Fatalf("expected seq range [1,3], got [%d,%d]", meta.MinSeq, meta.MaxSeq)
	}
	if meta.Size != int64(len(data)) {
		t.Fatalf("expected Size=%d, got %d", len(data), meta.Size)
	}

	r, err := NewSSTableReader("test-1", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	rmeta := r.Meta()
	if rmeta.EntryCount != meta.EntryCount {
		t.Fatalf("reader entry count %d != writer %d", rmeta.EntryCount, meta.EntryCount)
	}
	if rmeta.MinKey != meta.MinKey || rmeta.MaxKey != meta.MaxKey {
		t.Fatalf("reader key range [%s,%s] != writer [%s,%s]",
			rmeta.MinKey, rmeta.MaxKey, meta.MinKey, meta.MaxKey)
	}
	if rmeta.MinSeq != meta.MinSeq || rmeta.MaxSeq != meta.MaxSeq {
		t.Fatalf("reader seq range [%d,%d] != writer [%d,%d]",
			rmeta.MinSeq, rmeta.MaxSeq, meta.MinSeq, meta.MaxSeq)
	}

	// Verify all values via Get.
	for _, tc := range []struct {
		key string
		val string
	}{
		{"apple", "red"},
		{"banana", "yellow"},
		{"cherry", "dark-red"},
	} {
		e, err := r.Get(tc.key)
		if err != nil {
			t.Fatalf("Get(%s): %v", tc.key, err)
		}
		if e == nil {
			t.Fatalf("Get(%s): not found", tc.key)
		}
		if string(e.Value) != tc.val {
			t.Fatalf("Get(%s): expected %s, got %s", tc.key, tc.val, string(e.Value))
		}
	}
}

func TestMultipleDataBlocks(t *testing.T) {
	// Use a tiny block size to force multiple blocks.
	w := NewSSTableWriter("test-multi", 64)

	numEntries := 100
	for i := 0; i < numEntries; i++ {
		key := fmt.Sprintf("key-%05d", i)
		val := fmt.Sprintf("value-%05d", i)
		w.Add(Entry{Key: key, Value: []byte(val), SeqNum: uint64(i + 1)})
	}

	data, meta, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if meta.EntryCount != numEntries {
		t.Fatalf("expected %d entries, got %d", numEntries, meta.EntryCount)
	}

	r, err := NewSSTableReader("test-multi", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	// Must have more than one index entry (meaning more than one data block).
	if len(r.index) <= 1 {
		t.Fatalf("expected multiple data blocks, got %d", len(r.index))
	}

	// Verify every entry is retrievable via Get.
	for i := 0; i < numEntries; i++ {
		key := fmt.Sprintf("key-%05d", i)
		expected := fmt.Sprintf("value-%05d", i)
		e, err := r.Get(key)
		if err != nil {
			t.Fatalf("Get(%s): %v", key, err)
		}
		if e == nil {
			t.Fatalf("Get(%s): not found", key)
		}
		if string(e.Value) != expected {
			t.Fatalf("Get(%s): expected %s, got %s", key, expected, string(e.Value))
		}
	}
}

func TestGetBinarySearch(t *testing.T) {
	// Insert entries out of order to verify sorting and binary search.
	w := NewSSTableWriter("test-bsearch", 64)
	w.Add(Entry{Key: "delta", Value: []byte("4"), SeqNum: 4})
	w.Add(Entry{Key: "alpha", Value: []byte("1"), SeqNum: 1})
	w.Add(Entry{Key: "charlie", Value: []byte("3"), SeqNum: 3})
	w.Add(Entry{Key: "bravo", Value: []byte("2"), SeqNum: 2})

	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	r, err := NewSSTableReader("test-bsearch", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	// Existing keys.
	for _, tc := range []struct {
		key string
		val string
	}{
		{"alpha", "1"},
		{"bravo", "2"},
		{"charlie", "3"},
		{"delta", "4"},
	} {
		e, err := r.Get(tc.key)
		if err != nil {
			t.Fatalf("Get(%s): %v", tc.key, err)
		}
		if e == nil {
			t.Fatalf("Get(%s): not found", tc.key)
		}
		if string(e.Value) != tc.val {
			t.Fatalf("Get(%s): expected %s, got %s", tc.key, tc.val, string(e.Value))
		}
	}

	// Non-existing keys.
	for _, key := range []string{"aaa", "az", "echo", "zzz"} {
		e, err := r.Get(key)
		if err != nil {
			t.Fatalf("Get(%s): %v", key, err)
		}
		if e != nil {
			t.Fatalf("Get(%s): expected nil, got %+v", key, e)
		}
	}
}

func TestBloomFilter(t *testing.T) {
	w := NewSSTableWriter("test-bloom", DefaultBlockSize)

	keys := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		keys[i] = fmt.Sprintf("bloom-key-%05d", i)
		w.Add(Entry{Key: keys[i], Value: []byte("v"), SeqNum: uint64(i + 1)})
	}

	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	r, err := NewSSTableReader("test-bloom", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	// All inserted keys must pass the bloom filter (no false negatives).
	for _, key := range keys {
		if !r.BloomMayContain(key) {
			t.Fatalf("BloomMayContain(%s) = false, expected true", key)
		}
	}

	// Check false positive rate with keys known to be absent.
	falsePositives := 0
	testCount := 10000
	for i := 0; i < testCount; i++ {
		key := fmt.Sprintf("absent-key-%06d", i)
		if r.BloomMayContain(key) {
			falsePositives++
		}
	}

	fpRate := float64(falsePositives) / float64(testCount)
	// Allow up to 5% false positive rate (should be around 1%).
	if fpRate > 0.05 {
		t.Fatalf("bloom false positive rate too high: %.4f (%d/%d)", fpRate, falsePositives, testCount)
	}
	t.Logf("bloom false positive rate: %.4f (%d/%d)", fpRate, falsePositives, testCount)
}

func TestSSTableIteratorOrdering(t *testing.T) {
	w := NewSSTableWriter("test-iter", 64)

	// Insert in reverse order to test sorting.
	for i := 49; i >= 0; i-- {
		key := fmt.Sprintf("iter-%05d", i)
		w.Add(Entry{Key: key, Value: []byte(key), SeqNum: uint64(i + 1)})
	}

	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	r, err := NewSSTableReader("test-iter", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	iter := r.Iterator()
	var prev string
	count := 0
	for iter.Next() {
		e := iter.Entry()
		if prev != "" && e.Key <= prev {
			t.Fatalf("iterator out of order: %s after %s", e.Key, prev)
		}
		prev = e.Key
		count++
	}

	if count != 50 {
		t.Fatalf("expected 50 entries from iterator, got %d", count)
	}
}

func TestEmptySSTable(t *testing.T) {
	w := NewSSTableWriter("test-empty", DefaultBlockSize)

	data, meta, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if meta.EntryCount != 0 {
		t.Fatalf("expected 0 entries, got %d", meta.EntryCount)
	}
	if meta.MinKey != "" || meta.MaxKey != "" {
		t.Fatalf("expected empty key range, got [%s, %s]", meta.MinKey, meta.MaxKey)
	}

	r, err := NewSSTableReader("test-empty", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	rmeta := r.Meta()
	if rmeta.EntryCount != 0 {
		t.Fatalf("reader expected 0 entries, got %d", rmeta.EntryCount)
	}

	// Get on empty table should return nil.
	e, err := r.Get("anything")
	if err != nil {
		t.Fatalf("Get on empty: %v", err)
	}
	if e != nil {
		t.Fatalf("Get on empty: expected nil, got %+v", e)
	}

	// Iterator should produce no entries.
	iter := r.Iterator()
	if iter.Next() {
		t.Fatal("iterator on empty SSTable should return false immediately")
	}

	// Bloom filter should not contain anything.
	if r.BloomMayContain("anything") {
		t.Fatal("BloomMayContain on empty SSTable should return false")
	}
}

func TestDuplicateKeysHighestSeqNum(t *testing.T) {
	w := NewSSTableWriter("test-dup", DefaultBlockSize)
	w.Add(Entry{Key: "key", Value: []byte("old"), SeqNum: 1})
	w.Add(Entry{Key: "key", Value: []byte("new"), SeqNum: 5})
	w.Add(Entry{Key: "key", Value: []byte("mid"), SeqNum: 3})

	data, meta, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if meta.EntryCount != 3 {
		t.Fatalf("expected 3 entries, got %d", meta.EntryCount)
	}

	r, err := NewSSTableReader("test-dup", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	e, err := r.Get("key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if e == nil {
		t.Fatal("Get: expected entry, got nil")
	}
	// Should return the entry with the highest sequence number.
	if string(e.Value) != "new" || e.SeqNum != 5 {
		t.Fatalf("Get: expected (new, seqnum=5), got (%s, seqnum=%d)", string(e.Value), e.SeqNum)
	}
}

func TestFooterMagicValidation(t *testing.T) {
	w := NewSSTableWriter("test-magic", DefaultBlockSize)
	w.Add(Entry{Key: "a", Value: []byte("b"), SeqNum: 1})

	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// Corrupt the magic bytes (located at len-8 through len-5).
	corrupted := make([]byte, len(data))
	copy(corrupted, data)
	corrupted[len(corrupted)-8] = 0xFF
	corrupted[len(corrupted)-7] = 0xFF

	_, err = NewSSTableReader("test-magic", corrupted)
	if err == nil {
		t.Fatal("expected error for corrupted magic, got nil")
	}
}

func TestSSTableIteratorFromEmpty(t *testing.T) {
	w := NewSSTableWriter("test-from-empty", DefaultBlockSize)
	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	r, err := NewSSTableReader("test-from-empty", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	it := r.IteratorFrom("anything")
	if it.Next() {
		t.Fatal("expected no entries from empty SSTable")
	}
}

func TestSSTableIteratorFromBeginning(t *testing.T) {
	w := NewSSTableWriter("test-from-begin", 64)
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key-%05d", i)
		w.Add(Entry{Key: key, Value: []byte(key), SeqNum: uint64(i + 1)})
	}
	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	r, err := NewSSTableReader("test-from-begin", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	// Empty afterKey should return all entries.
	it := r.IteratorFrom("")
	count := 0
	for it.Next() {
		count++
	}
	if it.Err() != nil {
		t.Fatalf("unexpected error: %v", it.Err())
	}
	if count != 20 {
		t.Fatalf("expected 20 entries, got %d", count)
	}
}

func TestSSTableIteratorFromMiddle(t *testing.T) {
	// Use tiny block size for multi-block SSTable.
	w := NewSSTableWriter("test-from-mid", 64)
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key-%05d", i)
		w.Add(Entry{Key: key, Value: []byte(key), SeqNum: uint64(i + 1)})
	}
	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	r, err := NewSSTableReader("test-from-mid", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	// After "key-00024" should give keys 25..49 (25 entries).
	it := r.IteratorFrom("key-00024")
	var got []string
	for it.Next() {
		got = append(got, it.Entry().Key)
	}
	if it.Err() != nil {
		t.Fatalf("unexpected error: %v", it.Err())
	}
	if len(got) != 25 {
		t.Fatalf("expected 25 entries, got %d", len(got))
	}
	if got[0] != "key-00025" {
		t.Fatalf("expected first key key-00025, got %s", got[0])
	}
	if got[len(got)-1] != "key-00049" {
		t.Fatalf("expected last key key-00049, got %s", got[len(got)-1])
	}
}

func TestSSTableIteratorFromPastEnd(t *testing.T) {
	w := NewSSTableWriter("test-from-past", 64)
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%05d", i)
		w.Add(Entry{Key: key, Value: []byte(key), SeqNum: uint64(i + 1)})
	}
	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	r, err := NewSSTableReader("test-from-past", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	it := r.IteratorFrom("zzz")
	if it.Next() {
		t.Fatal("expected no entries when afterKey is past all keys")
	}
}

func TestSSTableIteratorFromExactKey(t *testing.T) {
	w := NewSSTableWriter("test-from-exact", 64)
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%05d", i)
		w.Add(Entry{Key: key, Value: []byte(key), SeqNum: uint64(i + 1)})
	}
	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	r, err := NewSSTableReader("test-from-exact", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	// Exact key match: should NOT include the afterKey itself.
	it := r.IteratorFrom("key-00005")
	var got []string
	for it.Next() {
		got = append(got, it.Entry().Key)
	}
	if it.Err() != nil {
		t.Fatalf("unexpected error: %v", it.Err())
	}
	// Should get keys 6..9 (4 entries).
	if len(got) != 4 {
		t.Fatalf("expected 4 entries, got %d: %v", len(got), got)
	}
	if got[0] != "key-00006" {
		t.Fatalf("expected first key key-00006, got %s", got[0])
	}
}

func TestDataTooShortForFooter(t *testing.T) {
	_, err := NewSSTableReader("bad", make([]byte, 10))
	if err == nil {
		t.Fatal("expected error for data shorter than footer")
	}
}

func TestSSTableLargeValues(t *testing.T) {
	w := NewSSTableWriter("test-large-vals", DefaultBlockSize)

	numEntries := 10
	largeVal := make([]byte, 100*1024) // 100KB
	for i := range largeVal {
		largeVal[i] = byte(i % 251) // deterministic pattern
	}

	for i := 0; i < numEntries; i++ {
		key := fmt.Sprintf("large-%03d", i)
		// Make each value slightly different so we can verify correctness.
		val := make([]byte, len(largeVal))
		copy(val, largeVal)
		val[0] = byte(i)
		w.Add(Entry{Key: key, Value: val, SeqNum: uint64(i + 1)})
	}

	data, meta, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if meta.EntryCount != numEntries {
		t.Fatalf("expected %d entries, got %d", numEntries, meta.EntryCount)
	}

	r, err := NewSSTableReader("test-large-vals", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	// Verify every entry via Get.
	for i := 0; i < numEntries; i++ {
		key := fmt.Sprintf("large-%03d", i)
		e, err := r.Get(key)
		if err != nil {
			t.Fatalf("Get(%s): %v", key, err)
		}
		if e == nil {
			t.Fatalf("Get(%s): not found", key)
		}
		if len(e.Value) != 100*1024 {
			t.Fatalf("Get(%s): expected value length %d, got %d", key, 100*1024, len(e.Value))
		}
		if e.Value[0] != byte(i) {
			t.Fatalf("Get(%s): expected first byte %d, got %d", key, i, e.Value[0])
		}
		// Verify rest of pattern.
		for j := 1; j < len(e.Value); j++ {
			if e.Value[j] != byte(j%251) {
				t.Fatalf("Get(%s): value mismatch at byte %d: expected %d, got %d", key, j, byte(j%251), e.Value[j])
			}
		}
	}

	// Verify via iterator too.
	iter := r.Iterator()
	count := 0
	for iter.Next() {
		e := iter.Entry()
		if len(e.Value) != 100*1024 {
			t.Fatalf("iterator entry %s: expected value length %d, got %d", e.Key, 100*1024, len(e.Value))
		}
		count++
	}
	if iter.Err() != nil {
		t.Fatalf("iterator error: %v", iter.Err())
	}
	if count != numEntries {
		t.Fatalf("expected %d entries from iterator, got %d", numEntries, count)
	}
}

func TestSSTableTombstoneRoundTrip(t *testing.T) {
	w := NewSSTableWriter("test-tombstone", DefaultBlockSize)
	w.Add(Entry{Key: "alive", Value: []byte("hello"), SeqNum: 1})
	w.Add(Entry{Key: "dead", Value: nil, SeqNum: 2})  // tombstone
	w.Add(Entry{Key: "also-alive", Value: []byte("world"), SeqNum: 3})
	w.Add(Entry{Key: "also-dead", Value: nil, SeqNum: 4}) // tombstone

	data, meta, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if meta.EntryCount != 4 {
		t.Fatalf("expected 4 entries, got %d", meta.EntryCount)
	}

	r, err := NewSSTableReader("test-tombstone", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	// Verify tombstones return entries with zero-length values.
	for _, tc := range []struct {
		key       string
		expectNil bool
		expectVal string
	}{
		{"alive", false, "hello"},
		{"also-alive", false, "world"},
		{"dead", false, ""},
		{"also-dead", false, ""},
	} {
		e, err := r.Get(tc.key)
		if err != nil {
			t.Fatalf("Get(%s): %v", tc.key, err)
		}
		if e == nil {
			t.Fatalf("Get(%s): not found", tc.key)
		}
		if string(e.Value) != tc.expectVal {
			t.Fatalf("Get(%s): expected value %q, got %q", tc.key, tc.expectVal, string(e.Value))
		}
		// Tombstones have zero-length value.
		if tc.expectVal == "" && len(e.Value) != 0 {
			t.Fatalf("Get(%s): expected zero-length value for tombstone, got %d", tc.key, len(e.Value))
		}
	}

	// Verify via iterator.
	iter := r.Iterator()
	count := 0
	tombstones := 0
	for iter.Next() {
		e := iter.Entry()
		if len(e.Value) == 0 {
			tombstones++
		}
		count++
	}
	if count != 4 {
		t.Fatalf("expected 4 entries, got %d", count)
	}
	if tombstones != 2 {
		t.Fatalf("expected 2 tombstones, got %d", tombstones)
	}
}

func TestSSTableSingleEntry(t *testing.T) {
	w := NewSSTableWriter("test-single", DefaultBlockSize)
	w.Add(Entry{Key: "only", Value: []byte("one"), SeqNum: 42})

	data, meta, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if meta.EntryCount != 1 {
		t.Fatalf("expected 1 entry, got %d", meta.EntryCount)
	}
	if meta.MinKey != "only" || meta.MaxKey != "only" {
		t.Fatalf("expected key range [only, only], got [%s, %s]", meta.MinKey, meta.MaxKey)
	}
	if meta.MinSeq != 42 || meta.MaxSeq != 42 {
		t.Fatalf("expected seq range [42, 42], got [%d, %d]", meta.MinSeq, meta.MaxSeq)
	}

	r, err := NewSSTableReader("test-single", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	rmeta := r.Meta()
	if rmeta.EntryCount != 1 {
		t.Fatalf("reader expected 1 entry, got %d", rmeta.EntryCount)
	}
	if rmeta.MinKey != "only" || rmeta.MaxKey != "only" {
		t.Fatalf("reader key range [%s, %s] != [only, only]", rmeta.MinKey, rmeta.MaxKey)
	}

	e, err := r.Get("only")
	if err != nil {
		t.Fatalf("Get(only): %v", err)
	}
	if e == nil {
		t.Fatal("Get(only): not found")
	}
	if string(e.Value) != "one" || e.SeqNum != 42 {
		t.Fatalf("Get(only): expected (one, 42), got (%s, %d)", string(e.Value), e.SeqNum)
	}

	// Missing keys should return nil.
	for _, k := range []string{"a", "z", "", "onl", "onlyy"} {
		e, err := r.Get(k)
		if err != nil {
			t.Fatalf("Get(%s): %v", k, err)
		}
		if e != nil {
			t.Fatalf("Get(%s): expected nil, got %+v", k, e)
		}
	}

	// Iterator should yield exactly one entry.
	iter := r.Iterator()
	if !iter.Next() {
		t.Fatal("iterator should have one entry")
	}
	if iter.Entry().Key != "only" {
		t.Fatalf("iterator entry key: expected only, got %s", iter.Entry().Key)
	}
	if iter.Next() {
		t.Fatal("iterator should have no more entries")
	}

	// Bloom should contain the key.
	if !r.BloomMayContain("only") {
		t.Fatal("BloomMayContain(only) should be true")
	}
}

func TestSSTableBlockBoundaryGet(t *testing.T) {
	// Use a small block size to create many blocks with predictable boundaries.
	blockSize := 64
	w := NewSSTableWriter("test-boundary", blockSize)

	numEntries := 200
	for i := 0; i < numEntries; i++ {
		key := fmt.Sprintf("key-%05d", i)
		val := fmt.Sprintf("val-%05d", i)
		w.Add(Entry{Key: key, Value: []byte(val), SeqNum: uint64(i + 1)})
	}

	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	r, err := NewSSTableReader("test-boundary", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	if len(r.index) <= 1 {
		t.Fatalf("expected multiple blocks, got %d", len(r.index))
	}

	// Collect all first keys from blocks (these are at block boundaries).
	boundaryKeys := make(map[string]bool)
	for _, ie := range r.index {
		boundaryKeys[ie.FirstKey] = true
	}

	if len(boundaryKeys) < 2 {
		t.Fatalf("expected at least 2 boundary keys, got %d", len(boundaryKeys))
	}

	// Verify all boundary keys are retrievable.
	for key := range boundaryKeys {
		e, err := r.Get(key)
		if err != nil {
			t.Fatalf("Get(%s) at block boundary: %v", key, err)
		}
		if e == nil {
			t.Fatalf("Get(%s) at block boundary: not found", key)
		}
		if e.Key != key {
			t.Fatalf("Get(%s) at block boundary: got key %s", key, e.Key)
		}
	}

	// Also verify last key in each block is retrievable.
	for i, ie := range r.index {
		block := data[ie.Offset : ie.Offset+ie.Size]
		var lastEntry Entry
		off := 0
		for off < len(block) {
			e, n, err := decodeEntry(block, off)
			if err != nil {
				t.Fatalf("decodeEntry in block %d: %v", i, err)
			}
			lastEntry = e
			off += n
		}
		e, err := r.Get(lastEntry.Key)
		if err != nil {
			t.Fatalf("Get(%s) at end of block %d: %v", lastEntry.Key, i, err)
		}
		if e == nil {
			t.Fatalf("Get(%s) at end of block %d: not found", lastEntry.Key, i)
		}
	}
}

func TestSSTableIteratorCount(t *testing.T) {
	for _, numEntries := range []int{1, 10, 100, 500} {
		t.Run(fmt.Sprintf("%d-entries", numEntries), func(t *testing.T) {
			w := NewSSTableWriter("test-iter-count", 64) // small blocks
			for i := 0; i < numEntries; i++ {
				key := fmt.Sprintf("k-%06d", i)
				w.Add(Entry{Key: key, Value: []byte("v"), SeqNum: uint64(i + 1)})
			}

			data, meta, err := w.Finish()
			if err != nil {
				t.Fatalf("Finish: %v", err)
			}

			r, err := NewSSTableReader("test-iter-count", data)
			if err != nil {
				t.Fatalf("NewSSTableReader: %v", err)
			}

			iter := r.Iterator()
			count := 0
			for iter.Next() {
				count++
			}
			if iter.Err() != nil {
				t.Fatalf("iterator error: %v", iter.Err())
			}

			if count != meta.EntryCount {
				t.Fatalf("iterator yielded %d entries, meta.EntryCount=%d", count, meta.EntryCount)
			}
			if count != numEntries {
				t.Fatalf("iterator yielded %d entries, expected %d", count, numEntries)
			}
		})
	}
}

func TestSSTableGetKeyInWrongBlock(t *testing.T) {
	// Create an SSTable where keys span multiple blocks.
	// The binary search on index should find the right block even if a key
	// lexicographically falls between two block first-keys.
	w := NewSSTableWriter("test-wrong-block", 64)

	// Insert keys with gaps that will span blocks.
	keys := []string{
		"aaa", "aab", "aac", "aad", "aae",
		"bbb", "bbc", "bbd", "bbe", "bbf",
		"ccc", "ccd", "cce", "ccf", "ccg",
		"ddd", "dde", "ddf", "ddg", "ddh",
	}
	for i, k := range keys {
		w.Add(Entry{Key: k, Value: []byte(k), SeqNum: uint64(i + 1)})
	}

	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	r, err := NewSSTableReader("test-wrong-block", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	if len(r.index) <= 1 {
		t.Fatalf("expected multiple blocks, got %d", len(r.index))
	}

	// Verify all keys are found.
	for _, k := range keys {
		e, err := r.Get(k)
		if err != nil {
			t.Fatalf("Get(%s): %v", k, err)
		}
		if e == nil {
			t.Fatalf("Get(%s): not found", k)
		}
		if string(e.Value) != k {
			t.Fatalf("Get(%s): expected value %s, got %s", k, k, string(e.Value))
		}
	}

	// Keys that fall between blocks' key ranges should correctly return nil.
	for _, k := range []string{"aaf", "bba", "ccb", "ddc"} {
		e, err := r.Get(k)
		if err != nil {
			t.Fatalf("Get(%s): %v", k, err)
		}
		if e != nil {
			t.Fatalf("Get(%s): expected nil for missing key, got %+v", k, e)
		}
	}
}

func TestSSTableCorruptIndexOffset(t *testing.T) {
	w := NewSSTableWriter("test-corrupt-idx", DefaultBlockSize)
	w.Add(Entry{Key: "a", Value: []byte("b"), SeqNum: 1})

	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	corrupted := make([]byte, len(data))
	copy(corrupted, data)

	// Footer layout (last 48 bytes):
	// [0:8]   indexOffset
	// [8:16]  indexSize
	// [16:24] bloomOffset
	// [24:32] bloomSize
	// [32:40] entryCount
	// [40:44] magic
	// [44:48] padding
	//
	// Set indexOffset to a large value that, combined with indexSize, exceeds dataLen
	// without overflowing uint64. Use dataLen itself as the offset (with non-zero size,
	// indexOffset + indexSize > dataLen).
	footerStart := len(corrupted) - 48
	dataLen := uint64(len(corrupted))
	binary.LittleEndian.PutUint64(corrupted[footerStart:footerStart+8], dataLen) // indexOffset = dataLen
	// indexSize stays non-zero from original data, so indexOffset+indexSize > dataLen.

	_, err = NewSSTableReader("test-corrupt-idx", corrupted)
	if err == nil {
		t.Fatal("expected error for corrupt index offset, got nil")
	}
}

func TestSSTableCorruptBloomOffset(t *testing.T) {
	w := NewSSTableWriter("test-corrupt-bloom", DefaultBlockSize)
	w.Add(Entry{Key: "a", Value: []byte("b"), SeqNum: 1})

	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	corrupted := make([]byte, len(data))
	copy(corrupted, data)

	// Corrupt the bloom offset to point past end of data.
	// Use dataLen as bloomOffset so bloomOffset+bloomSize > dataLen.
	footerStart := len(corrupted) - 48
	dataLen := uint64(len(corrupted))
	binary.LittleEndian.PutUint64(corrupted[footerStart+16:footerStart+24], dataLen) // bloomOffset = dataLen

	_, err = NewSSTableReader("test-corrupt-bloom", corrupted)
	if err == nil {
		t.Fatal("expected error for corrupt bloom offset, got nil")
	}
}

func TestSSTableBloomCreateEmpty(t *testing.T) {
	result := bloomCreate([]string{})
	if result != nil {
		t.Fatalf("bloomCreate with empty keys: expected nil, got %v", result)
	}

	result = bloomCreate(nil)
	if result != nil {
		t.Fatalf("bloomCreate with nil keys: expected nil, got %v", result)
	}
}

func TestSSTableBloomContainsEmptyFilter(t *testing.T) {
	if bloomContains(nil, "any-key") {
		t.Fatal("bloomContains with nil filter should return false")
	}
	if bloomContains([]byte{}, "any-key") {
		t.Fatal("bloomContains with empty filter should return false")
	}
}

func TestSSTableWriterDefaultBlockSize(t *testing.T) {
	w := NewSSTableWriter("test-default-bs", 0)

	// Verify it uses DefaultBlockSize by adding enough data.
	// With DefaultBlockSize=4096, small entries should fit in one block.
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%03d", i)
		w.Add(Entry{Key: key, Value: []byte("v"), SeqNum: uint64(i + 1)})
	}

	data, meta, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if meta.EntryCount != 10 {
		t.Fatalf("expected 10 entries, got %d", meta.EntryCount)
	}

	r, err := NewSSTableReader("test-default-bs", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	// With 10 small entries and 4096 block size, should be exactly 1 block.
	if len(r.index) != 1 {
		t.Fatalf("expected 1 block with default block size, got %d", len(r.index))
	}

	// Also verify negative blockSize uses default.
	w2 := NewSSTableWriter("test-neg-bs", -1)
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%03d", i)
		w2.Add(Entry{Key: key, Value: []byte("v"), SeqNum: uint64(i + 1)})
	}
	data2, _, err := w2.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	r2, err := NewSSTableReader("test-neg-bs", data2)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}
	if len(r2.index) != 1 {
		t.Fatalf("expected 1 block with negative block size, got %d", len(r2.index))
	}
}

func TestSSTableMetaConsistency(t *testing.T) {
	w := NewSSTableWriter("test-meta-consistency", 128)

	numEntries := 500
	for i := 0; i < numEntries; i++ {
		key := fmt.Sprintf("meta-%06d", i)
		val := fmt.Sprintf("value-%06d", i)
		w.Add(Entry{Key: key, Value: []byte(val), SeqNum: uint64(i + 10)})
	}

	data, writerMeta, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	r, err := NewSSTableReader("test-meta-consistency", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	readerMeta := r.Meta()

	// EntryCount must match.
	if readerMeta.EntryCount != writerMeta.EntryCount {
		t.Fatalf("EntryCount: writer=%d, reader=%d", writerMeta.EntryCount, readerMeta.EntryCount)
	}
	if readerMeta.EntryCount != numEntries {
		t.Fatalf("EntryCount: expected %d, got %d", numEntries, readerMeta.EntryCount)
	}

	// Size must match.
	if readerMeta.Size != writerMeta.Size {
		t.Fatalf("Size: writer=%d, reader=%d", writerMeta.Size, readerMeta.Size)
	}
	if readerMeta.Size != int64(len(data)) {
		t.Fatalf("Size: expected %d, got %d", len(data), readerMeta.Size)
	}

	// MinKey/MaxKey must match.
	if readerMeta.MinKey != writerMeta.MinKey {
		t.Fatalf("MinKey: writer=%s, reader=%s", writerMeta.MinKey, readerMeta.MinKey)
	}
	if readerMeta.MaxKey != writerMeta.MaxKey {
		t.Fatalf("MaxKey: writer=%s, reader=%s", writerMeta.MaxKey, readerMeta.MaxKey)
	}

	// Verify expected min/max keys.
	expectedMinKey := "meta-000000"
	expectedMaxKey := fmt.Sprintf("meta-%06d", numEntries-1)
	if readerMeta.MinKey != expectedMinKey {
		t.Fatalf("MinKey: expected %s, got %s", expectedMinKey, readerMeta.MinKey)
	}
	if readerMeta.MaxKey != expectedMaxKey {
		t.Fatalf("MaxKey: expected %s, got %s", expectedMaxKey, readerMeta.MaxKey)
	}

	// MinSeq/MaxSeq must match.
	if readerMeta.MinSeq != writerMeta.MinSeq {
		t.Fatalf("MinSeq: writer=%d, reader=%d", writerMeta.MinSeq, readerMeta.MinSeq)
	}
	if readerMeta.MaxSeq != writerMeta.MaxSeq {
		t.Fatalf("MaxSeq: writer=%d, reader=%d", writerMeta.MaxSeq, readerMeta.MaxSeq)
	}
	if readerMeta.MinSeq != 10 {
		t.Fatalf("MinSeq: expected 10, got %d", readerMeta.MinSeq)
	}
	if readerMeta.MaxSeq != uint64(numEntries+9) {
		t.Fatalf("MaxSeq: expected %d, got %d", numEntries+9, readerMeta.MaxSeq)
	}

	// ID must match.
	if readerMeta.ID != writerMeta.ID {
		t.Fatalf("ID: writer=%s, reader=%s", writerMeta.ID, readerMeta.ID)
	}
}

func TestSSTableGetMissingKeyInPopulatedBlock(t *testing.T) {
	// Create blocks where the missing key falls within the key range of a block
	// but does not actually exist.
	w := NewSSTableWriter("test-missing-in-block", 256)

	// Insert keys with gaps.
	for i := 0; i < 100; i += 2 { // only even-numbered keys
		key := fmt.Sprintf("key-%05d", i)
		w.Add(Entry{Key: key, Value: []byte("v"), SeqNum: uint64(i + 1)})
	}

	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	r, err := NewSSTableReader("test-missing-in-block", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	// Look up odd-numbered keys which don't exist but fall within block ranges.
	for i := 1; i < 99; i += 2 {
		key := fmt.Sprintf("key-%05d", i)
		e, err := r.Get(key)
		if err != nil {
			t.Fatalf("Get(%s): %v", key, err)
		}
		if e != nil {
			t.Fatalf("Get(%s): expected nil for missing key within block, got %+v", key, e)
		}
	}

	// Verify even keys are still found.
	for i := 0; i < 100; i += 2 {
		key := fmt.Sprintf("key-%05d", i)
		e, err := r.Get(key)
		if err != nil {
			t.Fatalf("Get(%s): %v", key, err)
		}
		if e == nil {
			t.Fatalf("Get(%s): expected to find key, got nil", key)
		}
	}
}

func TestSSTableIteratorFromBeforeAllKeys(t *testing.T) {
	w := NewSSTableWriter("test-from-before", 64)
	numEntries := 30
	for i := 0; i < numEntries; i++ {
		key := fmt.Sprintf("m-%05d", i) // all keys start with "m"
		w.Add(Entry{Key: key, Value: []byte(key), SeqNum: uint64(i + 1)})
	}

	data, _, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	r, err := NewSSTableReader("test-from-before", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	// afterKey="aaa" is before all keys starting with "m".
	it := r.IteratorFrom("aaa")
	var got []string
	for it.Next() {
		got = append(got, it.Entry().Key)
	}
	if it.Err() != nil {
		t.Fatalf("unexpected error: %v", it.Err())
	}
	if len(got) != numEntries {
		t.Fatalf("expected %d entries, got %d", numEntries, len(got))
	}
	if got[0] != "m-00000" {
		t.Fatalf("expected first key m-00000, got %s", got[0])
	}
	if got[len(got)-1] != fmt.Sprintf("m-%05d", numEntries-1) {
		t.Fatalf("expected last key m-%05d, got %s", numEntries-1, got[len(got)-1])
	}
}

func TestSSTableDuplicateKeysIterator(t *testing.T) {
	w := NewSSTableWriter("test-dup-iter", DefaultBlockSize)

	// Add multiple entries with the same key but different seqnums.
	w.Add(Entry{Key: "alpha", Value: []byte("a1"), SeqNum: 1})
	w.Add(Entry{Key: "alpha", Value: []byte("a2"), SeqNum: 2})
	w.Add(Entry{Key: "alpha", Value: []byte("a3"), SeqNum: 3})
	w.Add(Entry{Key: "beta", Value: []byte("b1"), SeqNum: 4})
	w.Add(Entry{Key: "beta", Value: []byte("b2"), SeqNum: 5})
	w.Add(Entry{Key: "gamma", Value: []byte("g1"), SeqNum: 6})

	data, meta, err := w.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if meta.EntryCount != 6 {
		t.Fatalf("expected 6 entries, got %d", meta.EntryCount)
	}

	r, err := NewSSTableReader("test-dup-iter", data)
	if err != nil {
		t.Fatalf("NewSSTableReader: %v", err)
	}

	// Iterator must yield ALL entries including duplicates (no dedup).
	iter := r.Iterator()
	var entries []Entry
	for iter.Next() {
		entries = append(entries, iter.Entry())
	}
	if iter.Err() != nil {
		t.Fatalf("iterator error: %v", iter.Err())
	}

	if len(entries) != 6 {
		t.Fatalf("expected 6 entries from iterator, got %d", len(entries))
	}

	// Entries are sorted by key asc, seqnum desc.
	// Expected order: alpha(3), alpha(2), alpha(1), beta(5), beta(4), gamma(6)
	expected := []struct {
		key    string
		val    string
		seqNum uint64
	}{
		{"alpha", "a3", 3},
		{"alpha", "a2", 2},
		{"alpha", "a1", 1},
		{"beta", "b2", 5},
		{"beta", "b1", 4},
		{"gamma", "g1", 6},
	}

	for i, exp := range expected {
		if entries[i].Key != exp.key {
			t.Fatalf("entry %d: expected key %s, got %s", i, exp.key, entries[i].Key)
		}
		if string(entries[i].Value) != exp.val {
			t.Fatalf("entry %d: expected value %s, got %s", i, exp.val, string(entries[i].Value))
		}
		if entries[i].SeqNum != exp.seqNum {
			t.Fatalf("entry %d: expected seqnum %d, got %d", i, exp.seqNum, entries[i].SeqNum)
		}
	}
}

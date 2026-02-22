package lsm

import (
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

func TestDataTooShortForFooter(t *testing.T) {
	_, err := NewSSTableReader("bad", make([]byte, 10))
	if err == nil {
		t.Fatal("expected error for data shorter than footer")
	}
}

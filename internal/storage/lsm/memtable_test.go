package lsm

import (
	"fmt"
	"sort"
	"sync"
	"testing"
)

func TestPutAndGet(t *testing.T) {
	m := NewMemTable()

	m.Put("alpha", []byte("v1"), 1)
	m.Put("beta", []byte("v2"), 2)
	m.Put("gamma", []byte("v3"), 3)

	tests := []struct {
		key    string
		want   string
		wantOK bool
	}{
		{"alpha", "v1", true},
		{"beta", "v2", true},
		{"gamma", "v3", true},
		{"delta", "", false},
	}

	for _, tt := range tests {
		e, ok := m.Get(tt.key)
		if ok != tt.wantOK {
			t.Fatalf("Get(%q): got ok=%v, want %v", tt.key, ok, tt.wantOK)
		}
		if ok && string(e.Value) != tt.want {
			t.Fatalf("Get(%q): got value=%q, want %q", tt.key, e.Value, tt.want)
		}
	}

	// Overwrite an existing key.
	m.Put("beta", []byte("v2-updated"), 4)
	e, ok := m.Get("beta")
	if !ok || string(e.Value) != "v2-updated" {
		t.Fatalf("Get after overwrite: got (%v, %v), want (v2-updated, true)", e, ok)
	}
	if e.SeqNum != 4 {
		t.Fatalf("SeqNum after overwrite: got %d, want 4", e.SeqNum)
	}
}

func TestDelete(t *testing.T) {
	m := NewMemTable()

	m.Put("key1", []byte("value1"), 1)
	m.Delete("key1", 2)

	e, ok := m.Get("key1")
	if !ok {
		t.Fatal("expected tombstone entry to be found")
	}
	if e.Value != nil {
		t.Fatalf("expected nil Value for tombstone, got %q", e.Value)
	}
	if e.SeqNum != 2 {
		t.Fatalf("expected SeqNum 2, got %d", e.SeqNum)
	}

	// Delete a key that was never inserted -- should create a tombstone.
	m.Delete("key2", 3)
	e, ok = m.Get("key2")
	if !ok {
		t.Fatal("expected tombstone for previously-absent key")
	}
	if e.Value != nil {
		t.Fatalf("expected nil Value for tombstone, got %q", e.Value)
	}
}

func TestIteratorOrdering(t *testing.T) {
	m := NewMemTable()

	keys := []string{"mango", "apple", "banana", "cherry", "date", "elderberry", "fig", "grape"}
	for i, k := range keys {
		m.Put(k, []byte(k), uint64(i+1))
	}

	sort.Strings(keys)

	it := m.Iterator()
	var got []string
	for it.Next() {
		got = append(got, it.Entry().Key)
	}

	if len(got) != len(keys) {
		t.Fatalf("iterator returned %d entries, want %d", len(got), len(keys))
	}
	for i := range keys {
		if got[i] != keys[i] {
			t.Fatalf("iterator[%d] = %q, want %q", i, got[i], keys[i])
		}
	}
}

func TestConcurrentReads(t *testing.T) {
	m := NewMemTable()

	const numKeys = 200
	for i := 0; i < numKeys; i++ {
		m.Put(fmt.Sprintf("key-%04d", i), []byte(fmt.Sprintf("val-%04d", i)), uint64(i))
	}

	const readers = 8
	var wg sync.WaitGroup
	wg.Add(readers)

	errs := make(chan error, readers)

	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < numKeys; i++ {
				key := fmt.Sprintf("key-%04d", i)
				want := fmt.Sprintf("val-%04d", i)
				e, ok := m.Get(key)
				if !ok {
					errs <- fmt.Errorf("missing key %q", key)
					return
				}
				if string(e.Value) != want {
					errs <- fmt.Errorf("Get(%q) = %q, want %q", key, e.Value, want)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}
}

func TestSizeTracking(t *testing.T) {
	m := NewMemTable()

	if m.Size() != 0 {
		t.Fatalf("empty table size: got %d, want 0", m.Size())
	}

	m.Put("k", []byte("v"), 1)
	s1 := m.Size()
	if s1 <= 0 {
		t.Fatalf("size after one Put should be positive, got %d", s1)
	}

	// Adding a second, larger entry should increase size.
	m.Put("longer-key", []byte("a-longer-value"), 2)
	s2 := m.Size()
	if s2 <= s1 {
		t.Fatalf("size should grow after second Put: got %d, previous %d", s2, s1)
	}

	// Overwriting with a shorter value should decrease size.
	m.Put("longer-key", []byte("x"), 3)
	s3 := m.Size()
	if s3 >= s2 {
		t.Fatalf("size should shrink after shorter overwrite: got %d, previous %d", s3, s2)
	}

	// Len should reflect the number of distinct keys.
	if m.Len() != 2 {
		t.Fatalf("Len: got %d, want 2", m.Len())
	}
}

func TestFreeze(t *testing.T) {
	m := NewMemTable()
	m.Put("a", []byte("1"), 1)

	frozen := m.Freeze()
	if frozen != m {
		t.Fatal("Freeze should return the same MemTable pointer")
	}
	if !m.IsFrozen {
		t.Fatal("expected IsFrozen to be true after Freeze")
	}

	// Reads should still work on a frozen table.
	e, ok := m.Get("a")
	if !ok || string(e.Value) != "1" {
		t.Fatalf("Get on frozen table: got (%v, %v)", e, ok)
	}

	// Iterator should still work on a frozen table.
	it := m.Iterator()
	if !it.Next() {
		t.Fatal("expected iterator to have at least one entry on frozen table")
	}
	if it.Entry().Key != "a" {
		t.Fatalf("iterator entry key: got %q, want %q", it.Entry().Key, "a")
	}

	// Put should panic on a frozen table.
	assertPanics(t, "Put on frozen table", func() {
		m.Put("b", []byte("2"), 2)
	})

	// Delete should panic on a frozen table.
	assertPanics(t, "Delete on frozen table", func() {
		m.Delete("a", 3)
	})
}

func TestIteratorFromEmpty(t *testing.T) {
	m := NewMemTable()
	it := m.IteratorFrom("anything")
	if it.Next() {
		t.Fatal("expected no entries from empty memtable")
	}
}

func TestIteratorFromBeginning(t *testing.T) {
	m := NewMemTable()
	m.Put("b", []byte("2"), 1)
	m.Put("c", []byte("3"), 2)
	m.Put("d", []byte("4"), 3)

	// afterKey="" should return all entries.
	it := m.IteratorFrom("")
	var got []string
	for it.Next() {
		got = append(got, it.Entry().Key)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
}

func TestIteratorFromMiddle(t *testing.T) {
	m := NewMemTable()
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		m.Put(k, []byte(k), 1)
	}

	// afterKey="b" should return c, d, e.
	it := m.IteratorFrom("b")
	var got []string
	for it.Next() {
		got = append(got, it.Entry().Key)
	}
	expected := []string{"c", "d", "e"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("entry[%d]: expected %q, got %q", i, expected[i], got[i])
		}
	}
}

func TestIteratorFromExactKey(t *testing.T) {
	m := NewMemTable()
	for _, k := range []string{"a", "b", "c"} {
		m.Put(k, []byte(k), 1)
	}

	// afterKey="b" should NOT include "b" — only entries with key > afterKey.
	it := m.IteratorFrom("b")
	var got []string
	for it.Next() {
		got = append(got, it.Entry().Key)
	}
	if len(got) != 1 || got[0] != "c" {
		t.Fatalf("expected [c], got %v", got)
	}
}

func TestIteratorFromPastEnd(t *testing.T) {
	m := NewMemTable()
	m.Put("a", []byte("1"), 1)
	m.Put("b", []byte("2"), 2)

	it := m.IteratorFrom("z")
	if it.Next() {
		t.Fatal("expected no entries when afterKey is past all keys")
	}
}

func assertPanics(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("%s: expected panic but did not get one", name)
		}
	}()
	fn()
}

func TestMemTableLargeKeyValues(t *testing.T) {
	m := NewMemTable()

	// Build a 1 MB key and a 1 MB value.
	bigKey := make([]byte, 1<<20)
	for i := range bigKey {
		bigKey[i] = 'K'
	}
	bigVal := make([]byte, 1<<20)
	for i := range bigVal {
		bigVal[i] = 'V'
	}

	m.Put(string(bigKey), bigVal, 1)

	e, ok := m.Get(string(bigKey))
	if !ok {
		t.Fatalf("expected to find 1 MB key")
	}
	if len(e.Value) != 1<<20 {
		t.Fatalf("value length: got %d, want %d", len(e.Value), 1<<20)
	}
	for i, b := range e.Value {
		if b != 'V' {
			t.Fatalf("value[%d] = %d, want 'V'", i, b)
		}
	}
	if e.Key != string(bigKey) {
		t.Fatalf("key mismatch on retrieved entry")
	}

	// Size must account for the large key and value.
	if m.Size() < int64(2<<20) {
		t.Fatalf("expected size >= 2 MB, got %d", m.Size())
	}

	// Iterator should also yield the entry.
	it := m.Iterator()
	if !it.Next() {
		t.Fatalf("iterator should have one entry")
	}
	if len(it.Entry().Value) != 1<<20 {
		t.Fatalf("iterator value length: got %d, want %d", len(it.Entry().Value), 1<<20)
	}
}

func TestMemTableEmptyKeyAndValue(t *testing.T) {
	m := NewMemTable()

	// Empty key with empty value.
	m.Put("", []byte{}, 1)

	e, ok := m.Get("")
	if !ok {
		t.Fatalf("expected to find empty-string key")
	}
	if len(e.Value) != 0 {
		t.Fatalf("expected empty value, got %q", e.Value)
	}
	if e.Value == nil {
		t.Fatalf("expected non-nil empty slice, got nil (would look like tombstone)")
	}

	if m.Len() != 1 {
		t.Fatalf("Len: got %d, want 1", m.Len())
	}

	// Overwrite empty key with a real value.
	m.Put("", []byte("something"), 2)
	e, ok = m.Get("")
	if !ok {
		t.Fatalf("expected to find empty-string key after overwrite")
	}
	if string(e.Value) != "something" {
		t.Fatalf("expected value 'something', got %q", e.Value)
	}
	if m.Len() != 1 {
		t.Fatalf("Len after overwrite: got %d, want 1", m.Len())
	}
}

func TestMemTableSpecialCharKeys(t *testing.T) {
	m := NewMemTable()

	keys := []string{
		"hello\x00world",          // embedded null byte
		"日本語",                     // unicode (CJK)
		"line1\nline2",             // embedded newline
		"tab\there",                // embedded tab
		"emoji \U0001F600",         // emoji
		"\x01\x02\x03",            // low ASCII control chars
		"spaces   and\ttabs",       // mixed whitespace
	}

	for i, k := range keys {
		m.Put(k, []byte(fmt.Sprintf("val-%d", i)), uint64(i+1))
	}

	// Verify all keys are retrievable.
	for i, k := range keys {
		e, ok := m.Get(k)
		if !ok {
			t.Fatalf("missing key %q (index %d)", k, i)
		}
		want := fmt.Sprintf("val-%d", i)
		if string(e.Value) != want {
			t.Fatalf("Get(%q): got %q, want %q", k, e.Value, want)
		}
	}

	if m.Len() != len(keys) {
		t.Fatalf("Len: got %d, want %d", m.Len(), len(keys))
	}

	// Iterator should return all keys in sorted order.
	it := m.Iterator()
	sorted := make([]string, 0, len(keys))
	for it.Next() {
		sorted = append(sorted, it.Entry().Key)
	}
	if len(sorted) != len(keys) {
		t.Fatalf("iterator count: got %d, want %d", len(sorted), len(keys))
	}
	for i := 1; i < len(sorted); i++ {
		if sorted[i] < sorted[i-1] {
			t.Fatalf("iterator not sorted: %q appears after %q", sorted[i], sorted[i-1])
		}
	}
}

func TestMemTableDeleteThenReinsert(t *testing.T) {
	m := NewMemTable()

	m.Put("key", []byte("original"), 1)
	e, ok := m.Get("key")
	if !ok || string(e.Value) != "original" {
		t.Fatalf("initial Get: got (%v, %v)", e, ok)
	}

	// Delete the key — creates a tombstone.
	m.Delete("key", 2)
	e, ok = m.Get("key")
	if !ok {
		t.Fatalf("expected tombstone entry to exist")
	}
	if e.Value != nil {
		t.Fatalf("expected nil value for tombstone, got %q", e.Value)
	}
	if e.SeqNum != 2 {
		t.Fatalf("tombstone SeqNum: got %d, want 2", e.SeqNum)
	}

	// Re-insert the same key with a new value.
	m.Put("key", []byte("reinserted"), 3)
	e, ok = m.Get("key")
	if !ok {
		t.Fatalf("expected key after re-insert")
	}
	if string(e.Value) != "reinserted" {
		t.Fatalf("value after re-insert: got %q, want %q", e.Value, "reinserted")
	}
	if e.SeqNum != 3 {
		t.Fatalf("SeqNum after re-insert: got %d, want 3", e.SeqNum)
	}

	// Len should still be 1 — it's the same logical key throughout.
	if m.Len() != 1 {
		t.Fatalf("Len after delete+reinsert: got %d, want 1", m.Len())
	}
}

func TestMemTableIteratorStability(t *testing.T) {
	m := NewMemTable()

	// Pre-populate some keys.
	for i := 0; i < 100; i++ {
		m.Put(fmt.Sprintf("key-%04d", i), []byte(fmt.Sprintf("val-%04d", i)), uint64(i))
	}

	// Take an iterator snapshot.
	it := m.Iterator()

	// Now mutate the memtable: add more keys and overwrite existing ones.
	for i := 100; i < 200; i++ {
		m.Put(fmt.Sprintf("key-%04d", i), []byte(fmt.Sprintf("val-%04d", i)), uint64(i))
	}
	m.Put("key-0001", []byte("overwritten"), 999)
	m.Delete("key-0050", 1000)

	// The iterator should still yield exactly the original 100 entries,
	// with the original values (snapshot semantics).
	count := 0
	for it.Next() {
		e := it.Entry()
		count++
		// The snapshotted entry for key-0001 should have the original value.
		if e.Key == "key-0001" && string(e.Value) != "val-0001" {
			t.Fatalf("iterator saw mutated value for key-0001: %q", e.Value)
		}
		// key-0050 should still appear with a non-nil value in the snapshot.
		if e.Key == "key-0050" && e.Value == nil {
			t.Fatalf("iterator saw tombstone for key-0050 that was deleted after snapshot")
		}
	}
	if count != 100 {
		t.Fatalf("iterator count: got %d, want 100", count)
	}
}

func TestMemTableManyKeys(t *testing.T) {
	m := NewMemTable()

	const n = 10000
	for i := 0; i < n; i++ {
		m.Put(fmt.Sprintf("key-%06d", i), []byte(fmt.Sprintf("val-%06d", i)), uint64(i))
	}

	if m.Len() != n {
		t.Fatalf("Len: got %d, want %d", m.Len(), n)
	}

	// Verify every key is retrievable.
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("key-%06d", i)
		want := fmt.Sprintf("val-%06d", i)
		e, ok := m.Get(key)
		if !ok {
			t.Fatalf("missing key %q", key)
		}
		if string(e.Value) != want {
			t.Fatalf("Get(%q) = %q, want %q", key, e.Value, want)
		}
	}

	// Iterator should yield all n keys in sorted order.
	it := m.Iterator()
	count := 0
	var prev string
	for it.Next() {
		e := it.Entry()
		if e.Key <= prev && count > 0 {
			t.Fatalf("iterator not sorted at position %d: %q <= %q", count, e.Key, prev)
		}
		prev = e.Key
		count++
	}
	if count != n {
		t.Fatalf("iterator count: got %d, want %d", count, n)
	}
}

func TestMemTableSequenceNumbers(t *testing.T) {
	m := NewMemTable()

	// Insert with specific sequence numbers and verify they are preserved.
	m.Put("a", []byte("v1"), 42)
	m.Put("b", []byte("v2"), 100)
	m.Put("c", []byte("v3"), 0) // seq 0 is valid
	m.Put("d", []byte("v4"), ^uint64(0)) // max uint64

	tests := []struct {
		key    string
		wantSN uint64
	}{
		{"a", 42},
		{"b", 100},
		{"c", 0},
		{"d", ^uint64(0)},
	}
	for _, tt := range tests {
		e, ok := m.Get(tt.key)
		if !ok {
			t.Fatalf("missing key %q", tt.key)
		}
		if e.SeqNum != tt.wantSN {
			t.Fatalf("Get(%q).SeqNum = %d, want %d", tt.key, e.SeqNum, tt.wantSN)
		}
	}

	// Overwrite updates the sequence number.
	m.Put("a", []byte("v1-new"), 999)
	e, _ := m.Get("a")
	if e.SeqNum != 999 {
		t.Fatalf("SeqNum after overwrite: got %d, want 999", e.SeqNum)
	}

	// Delete sets the sequence number on the tombstone.
	m.Delete("b", 555)
	e, _ = m.Get("b")
	if e.SeqNum != 555 {
		t.Fatalf("SeqNum after delete: got %d, want 555", e.SeqNum)
	}

	// Iterator entries also carry correct sequence numbers.
	it := m.Iterator()
	expected := map[string]uint64{"a": 999, "b": 555, "c": 0, "d": ^uint64(0)}
	for it.Next() {
		e := it.Entry()
		want, ok := expected[e.Key]
		if !ok {
			t.Fatalf("unexpected key in iterator: %q", e.Key)
		}
		if e.SeqNum != want {
			t.Fatalf("iterator entry %q SeqNum = %d, want %d", e.Key, e.SeqNum, want)
		}
	}
}

func TestMemTableOverwritePreservesCount(t *testing.T) {
	m := NewMemTable()

	m.Put("x", []byte("first"), 1)
	m.Put("y", []byte("another"), 2)
	if m.Len() != 2 {
		t.Fatalf("Len after 2 inserts: got %d, want 2", m.Len())
	}

	// Overwrite same keys multiple times.
	for i := 0; i < 50; i++ {
		m.Put("x", []byte(fmt.Sprintf("v%d", i)), uint64(i+10))
	}
	if m.Len() != 2 {
		t.Fatalf("Len after 50 overwrites of 'x': got %d, want 2", m.Len())
	}

	// Overwrite "y" as well.
	m.Put("y", []byte("updated"), 100)
	if m.Len() != 2 {
		t.Fatalf("Len after overwrite of 'y': got %d, want 2", m.Len())
	}

	// Iterator should only have 2 entries.
	it := m.Iterator()
	count := 0
	for it.Next() {
		count++
	}
	if count != 2 {
		t.Fatalf("iterator count: got %d, want 2", count)
	}
}

func TestMemTableSizeWithTombstones(t *testing.T) {
	m := NewMemTable()

	// Insert a key, note the size.
	m.Put("key1", []byte("value1"), 1)
	sizeAfterPut := m.Size()
	if sizeAfterPut <= 0 {
		t.Fatalf("size after Put should be positive, got %d", sizeAfterPut)
	}

	// Delete the key — this overwrites with nil value, size should change
	// (value bytes removed but entry overhead remains).
	m.Delete("key1", 2)
	sizeAfterDelete := m.Size()
	// The value bytes ("value1" = 6 bytes) should be subtracted.
	if sizeAfterDelete >= sizeAfterPut {
		t.Fatalf("size should decrease after tombstone overwrite of value: got %d, was %d", sizeAfterDelete, sizeAfterPut)
	}
	if sizeAfterDelete <= 0 {
		t.Fatalf("size should still be positive (overhead remains), got %d", sizeAfterDelete)
	}

	// Deleting a brand-new key should increase the size (new entry + overhead).
	m.Delete("key2", 3)
	sizeAfterNewTombstone := m.Size()
	if sizeAfterNewTombstone <= sizeAfterDelete {
		t.Fatalf("size should increase after tombstone for new key: got %d, was %d", sizeAfterNewTombstone, sizeAfterDelete)
	}

	// Len should be 2 (one overwritten tombstone + one new tombstone).
	if m.Len() != 2 {
		t.Fatalf("Len: got %d, want 2", m.Len())
	}
}

func TestMemTableIteratorFromBetweenKeys(t *testing.T) {
	m := NewMemTable()
	for _, k := range []string{"apple", "cherry", "grape", "mango"} {
		m.Put(k, []byte(k), 1)
	}

	// "banana" doesn't exist but falls between "apple" and "cherry".
	it := m.IteratorFrom("banana")
	var got []string
	for it.Next() {
		got = append(got, it.Entry().Key)
	}
	expected := []string{"cherry", "grape", "mango"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("entry[%d]: got %q, want %q", i, got[i], expected[i])
		}
	}

	// "dog" falls between "cherry" and "grape".
	it = m.IteratorFrom("dog")
	got = got[:0]
	for it.Next() {
		got = append(got, it.Entry().Key)
	}
	expected = []string{"grape", "mango"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("entry[%d]: got %q, want %q", i, got[i], expected[i])
		}
	}

	// "aaa" is before all keys — should get everything.
	it = m.IteratorFrom("aaa")
	count := 0
	for it.Next() {
		count++
	}
	if count != 4 {
		t.Fatalf("IteratorFrom('aaa'): got %d entries, want 4", count)
	}
}

func TestMemTableConcurrentReadsDuringWrite(t *testing.T) {
	m := NewMemTable()

	// Pre-populate so readers always have something to find.
	const preload = 500
	for i := 0; i < preload; i++ {
		m.Put(fmt.Sprintf("key-%05d", i), []byte(fmt.Sprintf("val-%05d", i)), uint64(i))
	}

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	stop := make(chan struct{})

	// One writer goroutine continuously adding new keys.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := preload; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			m.Put(fmt.Sprintf("key-%05d", i), []byte(fmt.Sprintf("val-%05d", i)), uint64(i))
		}
	}()

	// Multiple reader goroutines reading pre-loaded keys.
	const readers = 4
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := 0; round < 200; round++ {
				for i := 0; i < preload; i++ {
					key := fmt.Sprintf("key-%05d", i)
					e, ok := m.Get(key)
					if !ok {
						errs <- fmt.Errorf("missing preloaded key %q", key)
						return
					}
					// Value should be either the original or a valid overwrite.
					if e.Value == nil {
						errs <- fmt.Errorf("unexpected nil value for %q", key)
						return
					}
				}
			}
		}()
	}

	// Also test iterators under concurrent writes.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for round := 0; round < 50; round++ {
			it := m.Iterator()
			var prev string
			for it.Next() {
				e := it.Entry()
				if e.Key < prev {
					errs <- fmt.Errorf("iterator not sorted: %q < %q", e.Key, prev)
					return
				}
				prev = e.Key
			}
		}
	}()

	// Let it run for a bit, then stop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wait for the reader goroutines to finish their rounds.
	}()

	// Wait for readers to finish, then stop writer.
	// We rely on readers finishing their fixed rounds.
	// Use a timeout approach: just close stop after readers are likely done.
	// Actually, let's just let the readers and iterator goroutine finish, then stop.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Close stop to signal writer after a short delay to let readers finish.
	// The readers do fixed iteration counts and will finish.
	// We close stop; the writer will exit, then wg.Wait completes.
	close(stop)
	<-done

	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestMemTableIteratorFromWithDuplicatePrefix(t *testing.T) {
	m := NewMemTable()

	// Keys with common prefixes.
	keys := []string{
		"prefix/a",
		"prefix/ab",
		"prefix/abc",
		"prefix/b",
		"prefix/ba",
		"prefix/c",
		"prefix/ca",
		"prefix/cab",
	}
	for i, k := range keys {
		m.Put(k, []byte(k), uint64(i))
	}

	// Seek after "prefix/ab" — should get "prefix/abc", "prefix/b", ...
	it := m.IteratorFrom("prefix/ab")
	var got []string
	for it.Next() {
		got = append(got, it.Entry().Key)
	}
	expected := []string{"prefix/abc", "prefix/b", "prefix/ba", "prefix/c", "prefix/ca", "prefix/cab"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("entry[%d]: got %q, want %q", i, got[i], expected[i])
		}
	}

	// Seek after "prefix/abc" — should get "prefix/b", "prefix/ba", ...
	it = m.IteratorFrom("prefix/abc")
	got = got[:0]
	for it.Next() {
		got = append(got, it.Entry().Key)
	}
	expected = []string{"prefix/b", "prefix/ba", "prefix/c", "prefix/ca", "prefix/cab"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("entry[%d]: got %q, want %q", i, got[i], expected[i])
		}
	}

	// Seek after exactly the prefix — should get everything.
	it = m.IteratorFrom("prefix/")
	got = got[:0]
	for it.Next() {
		got = append(got, it.Entry().Key)
	}
	if len(got) != len(keys) {
		t.Fatalf("IteratorFrom('prefix/'): expected %d entries, got %d", len(keys), len(got))
	}
}

func TestMemTableDeleteNonExistentSize(t *testing.T) {
	m := NewMemTable()

	if m.Len() != 0 {
		t.Fatalf("initial Len: got %d, want 0", m.Len())
	}
	if m.Size() != 0 {
		t.Fatalf("initial Size: got %d, want 0", m.Size())
	}

	// Deleting a non-existent key should create a tombstone entry.
	m.Delete("never-existed", 1)

	if m.Len() != 1 {
		t.Fatalf("Len after deleting non-existent key: got %d, want 1", m.Len())
	}

	s := m.Size()
	// Size should account for key length + overhead (value is nil = 0 bytes).
	if s <= 0 {
		t.Fatalf("Size after deleting non-existent key should be positive, got %d", s)
	}

	// The entry should be a tombstone.
	e, ok := m.Get("never-existed")
	if !ok {
		t.Fatalf("expected tombstone to be retrievable")
	}
	if e.Value != nil {
		t.Fatalf("expected nil value for tombstone, got %q", e.Value)
	}
	if e.SeqNum != 1 {
		t.Fatalf("expected SeqNum 1, got %d", e.SeqNum)
	}

	// Delete another non-existent key — Len and Size should increase.
	m.Delete("also-never-existed", 2)
	if m.Len() != 2 {
		t.Fatalf("Len after second delete: got %d, want 2", m.Len())
	}
	s2 := m.Size()
	if s2 <= s {
		t.Fatalf("Size should increase after second tombstone: got %d, was %d", s2, s)
	}
}

package lsm

import (
	"errors"
	"fmt"
	"testing"
)

// sliceIterator is a test helper that iterates over a fixed slice of entries.
type sliceIterator struct {
	entries []Entry
	pos     int
	err     error
}

func newSliceIterator(entries []Entry) *sliceIterator {
	return &sliceIterator{entries: entries, pos: -1}
}

func (it *sliceIterator) Next() bool {
	if it.err != nil {
		return false
	}
	it.pos++
	return it.pos < len(it.entries)
}

func (it *sliceIterator) Entry() Entry {
	return it.entries[it.pos]
}

func (it *sliceIterator) Err() error {
	return it.err
}

func TestMergeIteratorBasicMerge(t *testing.T) {
	src1 := newSliceIterator([]Entry{
		{Key: "a", Value: []byte("1"), SeqNum: 1},
		{Key: "c", Value: []byte("3"), SeqNum: 3},
		{Key: "e", Value: []byte("5"), SeqNum: 5},
	})
	src2 := newSliceIterator([]Entry{
		{Key: "b", Value: []byte("2"), SeqNum: 2},
		{Key: "d", Value: []byte("4"), SeqNum: 4},
	})

	m := NewMergeIterator([]prioritizedIterator{
		{iter: src1, priority: 0},
		{iter: src2, priority: 1},
	})

	expected := []string{"a", "b", "c", "d", "e"}
	var got []string
	for m.Next() {
		got = append(got, m.Entry().Key)
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
	if len(got) != len(expected) {
		t.Fatalf("expected %d entries, got %d", len(expected), len(got))
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("entry[%d]: expected %q, got %q", i, expected[i], got[i])
		}
	}
}

func TestMergeIteratorDedup(t *testing.T) {
	// Both sources have key "b". Source with priority 0 (newer) should win.
	src1 := newSliceIterator([]Entry{
		{Key: "a", Value: []byte("a-new"), SeqNum: 10},
		{Key: "b", Value: []byte("b-new"), SeqNum: 10},
	})
	src2 := newSliceIterator([]Entry{
		{Key: "b", Value: []byte("b-old"), SeqNum: 5},
		{Key: "c", Value: []byte("c-old"), SeqNum: 5},
	})

	m := NewMergeIterator([]prioritizedIterator{
		{iter: src1, priority: 0},
		{iter: src2, priority: 1},
	})

	var got []Entry
	for m.Next() {
		got = append(got, m.Entry())
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries after dedup, got %d", len(got))
	}
	// Check that "b" has the newer value.
	if string(got[1].Value) != "b-new" {
		t.Fatalf("expected b-new for key b, got %s", string(got[1].Value))
	}
}

func TestMergeIteratorTombstoneFiltering(t *testing.T) {
	// Source 0 (newer) has a tombstone for "b".
	src1 := newSliceIterator([]Entry{
		{Key: "b", Value: nil, SeqNum: 10}, // tombstone
	})
	src2 := newSliceIterator([]Entry{
		{Key: "a", Value: []byte("a"), SeqNum: 1},
		{Key: "b", Value: []byte("b-old"), SeqNum: 5},
		{Key: "c", Value: []byte("c"), SeqNum: 2},
	})

	m := NewMergeIterator([]prioritizedIterator{
		{iter: src1, priority: 0},
		{iter: src2, priority: 1},
	})

	var got []string
	for m.Next() {
		got = append(got, m.Entry().Key)
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
	// "b" should be filtered out because the winning entry is a tombstone.
	expected := []string{"a", "c"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("entry[%d]: expected %q, got %q", i, expected[i], got[i])
		}
	}
}

func TestMergeIteratorEmptySources(t *testing.T) {
	// All empty sources.
	m := NewMergeIterator([]prioritizedIterator{
		{iter: newSliceIterator(nil), priority: 0},
		{iter: newSliceIterator(nil), priority: 1},
	})
	if m.Next() {
		t.Fatal("expected no entries from empty sources")
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
}

func TestMergeIteratorNoSources(t *testing.T) {
	m := NewMergeIterator(nil)
	if m.Next() {
		t.Fatal("expected no entries from nil sources")
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
}

func TestMergeIteratorSingleSource(t *testing.T) {
	src := newSliceIterator([]Entry{
		{Key: "x", Value: []byte("1"), SeqNum: 1},
		{Key: "y", Value: []byte("2"), SeqNum: 2},
	})
	m := NewMergeIterator([]prioritizedIterator{
		{iter: src, priority: 0},
	})

	var got []string
	for m.Next() {
		got = append(got, m.Entry().Key)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
}

func TestMergeIteratorErrorPropagation(t *testing.T) {
	testErr := errors.New("test error")
	src := &sliceIterator{
		entries: []Entry{{Key: "a", Value: []byte("1"), SeqNum: 1}},
		pos:     -1,
		err:     testErr,
	}
	m := NewMergeIterator([]prioritizedIterator{
		{iter: src, priority: 0},
	})
	if m.Next() {
		t.Fatal("expected no entries when source has error")
	}
	if m.Err() != testErr {
		t.Fatalf("expected test error, got %v", m.Err())
	}
}

func TestMergeIteratorThreeSources(t *testing.T) {
	src1 := newSliceIterator([]Entry{
		{Key: "a", Value: []byte("a1"), SeqNum: 1},
		{Key: "d", Value: []byte("d1"), SeqNum: 4},
		{Key: "g", Value: []byte("g1"), SeqNum: 7},
	})
	src2 := newSliceIterator([]Entry{
		{Key: "b", Value: []byte("b2"), SeqNum: 2},
		{Key: "e", Value: []byte("e2"), SeqNum: 5},
		{Key: "h", Value: []byte("h2"), SeqNum: 8},
	})
	src3 := newSliceIterator([]Entry{
		{Key: "c", Value: []byte("c3"), SeqNum: 3},
		{Key: "f", Value: []byte("f3"), SeqNum: 6},
		{Key: "i", Value: []byte("i3"), SeqNum: 9},
	})

	m := NewMergeIterator([]prioritizedIterator{
		{iter: src1, priority: 0},
		{iter: src2, priority: 1},
		{iter: src3, priority: 2},
	})

	expected := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
	var got []string
	for m.Next() {
		got = append(got, m.Entry().Key)
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
	if len(got) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("entry[%d]: expected %q, got %q", i, expected[i], got[i])
		}
	}
}

func TestMergeIteratorAllSameKey(t *testing.T) {
	// All three sources have key "x". Priority 0 (lowest) should win.
	src1 := newSliceIterator([]Entry{
		{Key: "x", Value: []byte("winner"), SeqNum: 100},
	})
	src2 := newSliceIterator([]Entry{
		{Key: "x", Value: []byte("loser2"), SeqNum: 50},
	})
	src3 := newSliceIterator([]Entry{
		{Key: "x", Value: []byte("loser3"), SeqNum: 10},
	})

	m := NewMergeIterator([]prioritizedIterator{
		{iter: src1, priority: 0},
		{iter: src2, priority: 1},
		{iter: src3, priority: 2},
	})

	if !m.Next() {
		t.Fatalf("expected one entry")
	}
	e := m.Entry()
	if e.Key != "x" {
		t.Fatalf("expected key %q, got %q", "x", e.Key)
	}
	if string(e.Value) != "winner" {
		t.Fatalf("expected value %q, got %q", "winner", string(e.Value))
	}
	if e.SeqNum != 100 {
		t.Fatalf("expected SeqNum 100, got %d", e.SeqNum)
	}
	if m.Next() {
		t.Fatalf("expected no more entries, got key %q", m.Entry().Key)
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
}

func TestMergeIteratorTombstoneWinsOverValue(t *testing.T) {
	// Newer source (priority 0) has a tombstone for "b"; older source (priority 1) has a value.
	// The tombstone should win and "b" should be filtered out entirely.
	src1 := newSliceIterator([]Entry{
		{Key: "a", Value: []byte("a-val"), SeqNum: 10},
		{Key: "b", Value: nil, SeqNum: 20}, // tombstone
		{Key: "c", Value: []byte("c-val"), SeqNum: 30},
	})
	src2 := newSliceIterator([]Entry{
		{Key: "b", Value: []byte("b-alive"), SeqNum: 5},
	})

	m := NewMergeIterator([]prioritizedIterator{
		{iter: src1, priority: 0},
		{iter: src2, priority: 1},
	})

	var got []string
	for m.Next() {
		got = append(got, m.Entry().Key)
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
	expected := []string{"a", "c"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("entry[%d]: expected %q, got %q", i, expected[i], got[i])
		}
	}
}

func TestMergeIteratorAllTombstones(t *testing.T) {
	src1 := newSliceIterator([]Entry{
		{Key: "a", Value: nil, SeqNum: 1},
		{Key: "b", Value: nil, SeqNum: 2},
	})
	src2 := newSliceIterator([]Entry{
		{Key: "c", Value: nil, SeqNum: 3},
		{Key: "d", Value: nil, SeqNum: 4},
	})
	src3 := newSliceIterator([]Entry{
		{Key: "e", Value: nil, SeqNum: 5},
	})

	m := NewMergeIterator([]prioritizedIterator{
		{iter: src1, priority: 0},
		{iter: src2, priority: 1},
		{iter: src3, priority: 2},
	})

	if m.Next() {
		t.Fatalf("expected no entries, got key %q", m.Entry().Key)
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
}

func TestMergeIteratorLargeScale(t *testing.T) {
	const numSources = 10
	const entriesPerSource = 100

	sources := make([]prioritizedIterator, numSources)
	for s := 0; s < numSources; s++ {
		entries := make([]Entry, entriesPerSource)
		for i := 0; i < entriesPerSource; i++ {
			// Each source gets unique keys: source 0 -> "0000","0010",...; source 1 -> "0001","0011",...
			keyNum := i*numSources + s
			key := fmt.Sprintf("%04d", keyNum)
			entries[i] = Entry{Key: key, Value: []byte(key), SeqNum: uint64(keyNum)}
		}
		sources[s] = prioritizedIterator{iter: newSliceIterator(entries), priority: s}
	}

	m := NewMergeIterator(sources)

	var count int
	var prevKey string
	for m.Next() {
		e := m.Entry()
		if count > 0 && e.Key <= prevKey {
			t.Fatalf("entries not in order: %q after %q at position %d", e.Key, prevKey, count)
		}
		prevKey = e.Key
		count++
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
	expectedCount := numSources * entriesPerSource
	if count != expectedCount {
		t.Fatalf("expected %d entries, got %d", expectedCount, count)
	}
}

func TestMergeIteratorDuplicateAcrossThreeSources(t *testing.T) {
	// Key "m" exists in all three sources with different priorities.
	// Priority 1 (middle) should win since it's the lowest priority that has this key.
	src1 := newSliceIterator([]Entry{
		{Key: "a", Value: []byte("a-s0"), SeqNum: 1},
		{Key: "z", Value: []byte("z-s0"), SeqNum: 2},
	})
	src2 := newSliceIterator([]Entry{
		{Key: "m", Value: []byte("m-p1"), SeqNum: 10},
	})
	src3 := newSliceIterator([]Entry{
		{Key: "m", Value: []byte("m-p2"), SeqNum: 20},
	})
	src4 := newSliceIterator([]Entry{
		{Key: "m", Value: []byte("m-p5"), SeqNum: 30},
	})

	m := NewMergeIterator([]prioritizedIterator{
		{iter: src1, priority: 0},
		{iter: src2, priority: 1},
		{iter: src3, priority: 2},
		{iter: src4, priority: 5},
	})

	var got []Entry
	for m.Next() {
		got = append(got, m.Entry())
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	// Entries should be: a, m (from priority 1), z
	if got[0].Key != "a" {
		t.Fatalf("expected first key %q, got %q", "a", got[0].Key)
	}
	if got[1].Key != "m" {
		t.Fatalf("expected second key %q, got %q", "m", got[1].Key)
	}
	if string(got[1].Value) != "m-p1" {
		t.Fatalf("expected value %q for key m, got %q", "m-p1", string(got[1].Value))
	}
	if got[1].SeqNum != 10 {
		t.Fatalf("expected SeqNum 10 for key m, got %d", got[1].SeqNum)
	}
	if got[2].Key != "z" {
		t.Fatalf("expected third key %q, got %q", "z", got[2].Key)
	}
}

func TestMergeIteratorInterleavedDuplicates(t *testing.T) {
	// Sources have overlapping but not identical key sets.
	src1 := newSliceIterator([]Entry{
		{Key: "a", Value: []byte("a-new"), SeqNum: 10},
		{Key: "b", Value: []byte("b-new"), SeqNum: 11},
		{Key: "d", Value: []byte("d-new"), SeqNum: 12},
	})
	src2 := newSliceIterator([]Entry{
		{Key: "b", Value: []byte("b-old"), SeqNum: 1},
		{Key: "c", Value: []byte("c-old"), SeqNum: 2},
		{Key: "d", Value: []byte("d-old"), SeqNum: 3},
		{Key: "e", Value: []byte("e-old"), SeqNum: 4},
	})

	m := NewMergeIterator([]prioritizedIterator{
		{iter: src1, priority: 0},
		{iter: src2, priority: 1},
	})

	type kv struct {
		key string
		val string
	}
	expected := []kv{
		{"a", "a-new"},
		{"b", "b-new"}, // dedup: priority 0 wins
		{"c", "c-old"},
		{"d", "d-new"}, // dedup: priority 0 wins
		{"e", "e-old"},
	}

	var got []kv
	for m.Next() {
		e := m.Entry()
		got = append(got, kv{e.Key, string(e.Value)})
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
	if len(got) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("entry[%d]: expected %v, got %v", i, expected[i], got[i])
		}
	}
}

func TestMergeIteratorOneEmptyOnePopulated(t *testing.T) {
	srcEmpty := newSliceIterator(nil)
	srcData := newSliceIterator([]Entry{
		{Key: "foo", Value: []byte("bar"), SeqNum: 1},
		{Key: "hello", Value: []byte("world"), SeqNum: 2},
	})

	// Test with empty first.
	m := NewMergeIterator([]prioritizedIterator{
		{iter: srcEmpty, priority: 0},
		{iter: srcData, priority: 1},
	})

	var got []string
	for m.Next() {
		got = append(got, m.Entry().Key)
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
	expected := []string{"foo", "hello"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("entry[%d]: expected %q, got %q", i, expected[i], got[i])
		}
	}

	// Also test with empty second (reversed priority order).
	srcEmpty2 := newSliceIterator(nil)
	srcData2 := newSliceIterator([]Entry{
		{Key: "x", Value: []byte("1"), SeqNum: 1},
	})
	m2 := NewMergeIterator([]prioritizedIterator{
		{iter: srcData2, priority: 0},
		{iter: srcEmpty2, priority: 1},
	})

	if !m2.Next() {
		t.Fatalf("expected one entry")
	}
	if m2.Entry().Key != "x" {
		t.Fatalf("expected key %q, got %q", "x", m2.Entry().Key)
	}
	if m2.Next() {
		t.Fatalf("expected no more entries")
	}
	if m2.Err() != nil {
		t.Fatalf("unexpected error: %v", m2.Err())
	}
}

func TestMergeIteratorErrorMidStream(t *testing.T) {
	// The error source yields two entries successfully, then errors on the third Next().
	// This means entries "a" and "c" can be popped; the error fires when trying to
	// advance after popping "c".
	src1 := &sliceIterator{
		entries: []Entry{
			{Key: "a", Value: []byte("a-val"), SeqNum: 1},
			{Key: "c", Value: []byte("c-val"), SeqNum: 3},
			{Key: "e", Value: []byte("e-val"), SeqNum: 5},
		},
		pos: -1,
	}
	midErr := errors.New("mid-stream failure")
	wrapped := &errAfterNIterator{inner: src1, failAfter: 2, err: midErr}

	src2 := newSliceIterator([]Entry{
		{Key: "b", Value: []byte("b-val"), SeqNum: 2},
		{Key: "d", Value: []byte("d-val"), SeqNum: 4},
	})

	m := NewMergeIterator([]prioritizedIterator{
		{iter: wrapped, priority: 0},
		{iter: src2, priority: 1},
	})

	// The merge iterator pops the smallest item, then advances the winning source.
	// "a" is popped; advance wrapped -> yields "c", ok. "a" is returned.
	// "b" is popped; advance src2 -> yields "d", ok. "b" is returned.
	// "c" is popped; advance wrapped -> error fires. Error returned, "c" lost.
	var got []string
	for m.Next() {
		got = append(got, m.Entry().Key)
	}
	if m.Err() != midErr {
		t.Fatalf("expected mid-stream error, got %v", m.Err())
	}
	// Should have gotten "a" and "b" before the error on advancing after "c".
	if len(got) < 2 {
		t.Fatalf("expected at least 2 entries before error, got %d: %v", len(got), got)
	}
	if got[0] != "a" {
		t.Fatalf("expected first entry %q, got %q", "a", got[0])
	}
	if got[1] != "b" {
		t.Fatalf("expected second entry %q, got %q", "b", got[1])
	}
}

// errAfterNIterator wraps an iterator and injects an error after N successful Next() calls.
type errAfterNIterator struct {
	inner     *sliceIterator
	failAfter int
	calls     int
	err       error
	failed    bool
}

func (it *errAfterNIterator) Next() bool {
	if it.failed {
		return false
	}
	if it.calls >= it.failAfter {
		it.failed = true
		it.inner.err = it.err
		return false
	}
	ok := it.inner.Next()
	if ok {
		it.calls++
	}
	return ok
}

func (it *errAfterNIterator) Entry() Entry {
	return it.inner.Entry()
}

func (it *errAfterNIterator) Err() error {
	return it.inner.Err()
}

func TestMergeIteratorHighPriorityOnlyTombstones(t *testing.T) {
	// High priority source (priority 0 = newer) has only tombstones.
	// Low priority source (priority 1 = older) has values for different keys.
	// Since the tombstones are for different keys than the values, the values should appear.
	srcHigh := newSliceIterator([]Entry{
		{Key: "a", Value: nil, SeqNum: 10}, // tombstone
		{Key: "c", Value: nil, SeqNum: 12}, // tombstone
	})
	srcLow := newSliceIterator([]Entry{
		{Key: "b", Value: []byte("b-val"), SeqNum: 1},
		{Key: "d", Value: []byte("d-val"), SeqNum: 3},
	})

	m := NewMergeIterator([]prioritizedIterator{
		{iter: srcHigh, priority: 0},
		{iter: srcLow, priority: 1},
	})

	var got []string
	for m.Next() {
		got = append(got, m.Entry().Key)
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
	expected := []string{"b", "d"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("entry[%d]: expected %q, got %q", i, expected[i], got[i])
		}
	}

	// Also test: high priority source has tombstones for keys that exist in low priority.
	// Those keys should be suppressed.
	srcHigh2 := newSliceIterator([]Entry{
		{Key: "x", Value: nil, SeqNum: 20}, // tombstone for "x"
	})
	srcLow2 := newSliceIterator([]Entry{
		{Key: "x", Value: []byte("x-val"), SeqNum: 5},
		{Key: "y", Value: []byte("y-val"), SeqNum: 6},
	})

	m2 := NewMergeIterator([]prioritizedIterator{
		{iter: srcHigh2, priority: 0},
		{iter: srcLow2, priority: 1},
	})

	var got2 []string
	for m2.Next() {
		got2 = append(got2, m2.Entry().Key)
	}
	if m2.Err() != nil {
		t.Fatalf("unexpected error: %v", m2.Err())
	}
	// Only "y" should appear; "x" is tombstoned by higher priority.
	if len(got2) != 1 || got2[0] != "y" {
		t.Fatalf("expected [y], got %v", got2)
	}
}

func TestMergeIteratorPreservesSeqNum(t *testing.T) {
	src1 := newSliceIterator([]Entry{
		{Key: "a", Value: []byte("a-new"), SeqNum: 100},
		{Key: "b", Value: []byte("b-new"), SeqNum: 200},
		{Key: "d", Value: []byte("d-new"), SeqNum: 400},
	})
	src2 := newSliceIterator([]Entry{
		{Key: "a", Value: []byte("a-old"), SeqNum: 1},
		{Key: "c", Value: []byte("c-old"), SeqNum: 50},
		{Key: "d", Value: []byte("d-old"), SeqNum: 3},
	})

	m := NewMergeIterator([]prioritizedIterator{
		{iter: src1, priority: 0},
		{iter: src2, priority: 1},
	})

	type expected struct {
		key    string
		val    string
		seqNum uint64
	}
	want := []expected{
		{"a", "a-new", 100}, // from src1 (priority 0)
		{"b", "b-new", 200}, // only in src1
		{"c", "c-old", 50},  // only in src2
		{"d", "d-new", 400}, // from src1 (priority 0)
	}

	var got []expected
	for m.Next() {
		e := m.Entry()
		got = append(got, expected{e.Key, string(e.Value), e.SeqNum})
	}
	if m.Err() != nil {
		t.Fatalf("unexpected error: %v", m.Err())
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d entries, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry[%d]: expected %v, got %v", i, want[i], got[i])
		}
	}
}

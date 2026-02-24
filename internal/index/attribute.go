package index

import (
	"fmt"
	"sync"

	"github.com/google/btree"
)

// FilterOp represents a filter operation.
type FilterOp string

const (
	OpEq       FilterOp = "eq"
	OpNeq      FilterOp = "neq"
	OpGt       FilterOp = "gt"
	OpGte      FilterOp = "gte"
	OpLt       FilterOp = "lt"
	OpLte      FilterOp = "lte"
	OpIn       FilterOp = "in"
	OpContains FilterOp = "contains"
)

// Filter represents a single attribute filter.
type Filter struct {
	Field string   `json:"field"`
	Op    FilterOp `json:"op"`
	Value any      `json:"value"`
}

// AttributeIndex provides in-memory inverted indexes for document attributes.
type AttributeIndex struct {
	mu sync.RWMutex

	// String/reference fields: value -> set of doc IDs.
	stringIndexes map[string]map[string]map[string]struct{} // field -> value -> docIDs

	// Numeric fields: B-tree for range queries.
	numericIndexes map[string]*btree.BTreeG[numericEntry] // field -> btree

	// Bool fields: two sets per field.
	boolIndexes map[string][2]map[string]struct{} // field -> [false, true] -> docIDs

	// Reverse index: docID -> field -> values (for deletion).
	docFields map[string]map[string][]any
}

type numericEntry struct {
	value float64
	docID string
}

func numericLess(a, b numericEntry) bool {
	if a.value != b.value {
		return a.value < b.value
	}
	return a.docID < b.docID
}

// NewAttributeIndex creates a new empty attribute index.
func NewAttributeIndex() *AttributeIndex {
	return &AttributeIndex{
		stringIndexes:  make(map[string]map[string]map[string]struct{}),
		numericIndexes: make(map[string]*btree.BTreeG[numericEntry]),
		boolIndexes:    make(map[string][2]map[string]struct{}),
		docFields:      make(map[string]map[string][]any),
	}
}

// IndexDocument adds a document's attributes to the index.
func (idx *AttributeIndex) IndexDocument(docID string, attrs map[string]any) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Remove old entries if the document was previously indexed.
	idx.removeDocLocked(docID)

	fields := make(map[string][]any)

	for field, value := range attrs {
		switch v := value.(type) {
		case string:
			idx.indexString(field, v, docID)
			fields[field] = []any{v}
		case float64:
			idx.indexNumeric(field, v, docID)
			fields[field] = []any{v}
		case bool:
			idx.indexBool(field, v, docID)
			fields[field] = []any{v}
		case []any:
			for _, elem := range v {
				switch ev := elem.(type) {
				case string:
					idx.indexString(field, ev, docID)
					fields[field] = append(fields[field], ev)
				case float64:
					idx.indexNumeric(field, ev, docID)
					fields[field] = append(fields[field], ev)
				}
			}
		}
	}

	idx.docFields[docID] = fields
}

// DeleteDocument removes a document from the index.
func (idx *AttributeIndex) DeleteDocument(docID string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.removeDocLocked(docID)
}

func (idx *AttributeIndex) removeDocLocked(docID string) {
	fields, ok := idx.docFields[docID]
	if !ok {
		return
	}

	for field, values := range fields {
		for _, value := range values {
			switch v := value.(type) {
			case string:
				if fm, ok := idx.stringIndexes[field]; ok {
					if docs, ok := fm[v]; ok {
						delete(docs, docID)
						if len(docs) == 0 {
							delete(fm, v)
						}
					}
				}
			case float64:
				if tree, ok := idx.numericIndexes[field]; ok {
					tree.Delete(numericEntry{value: v, docID: docID})
				}
			case bool:
				bi := 0
				if v {
					bi = 1
				}
				if bf, ok := idx.boolIndexes[field]; ok {
					delete(bf[bi], docID)
				}
			}
		}
	}

	delete(idx.docFields, docID)
}

func (idx *AttributeIndex) indexString(field, value, docID string) {
	if _, ok := idx.stringIndexes[field]; !ok {
		idx.stringIndexes[field] = make(map[string]map[string]struct{})
	}
	if _, ok := idx.stringIndexes[field][value]; !ok {
		idx.stringIndexes[field][value] = make(map[string]struct{})
	}
	idx.stringIndexes[field][value][docID] = struct{}{}
}

func (idx *AttributeIndex) indexNumeric(field string, value float64, docID string) {
	if _, ok := idx.numericIndexes[field]; !ok {
		idx.numericIndexes[field] = btree.NewG[numericEntry](16, numericLess)
	}
	idx.numericIndexes[field].ReplaceOrInsert(numericEntry{value: value, docID: docID})
}

func (idx *AttributeIndex) indexBool(field string, value bool, docID string) {
	if _, ok := idx.boolIndexes[field]; !ok {
		idx.boolIndexes[field] = [2]map[string]struct{}{
			make(map[string]struct{}),
			make(map[string]struct{}),
		}
	}
	bi := 0
	if value {
		bi = 1
	}
	idx.boolIndexes[field][bi][docID] = struct{}{}
}

// Search applies filters and returns matching document IDs.
func (idx *AttributeIndex) Search(filters []Filter) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var result map[string]struct{}

	if len(filters) == 0 {
		// No filters: return all indexed document IDs.
		result = make(map[string]struct{}, len(idx.docFields))
		for id := range idx.docFields {
			result[id] = struct{}{}
		}
	} else {
		for _, f := range filters {
			matches := idx.applyFilter(f)
			if result == nil {
				result = matches
			} else {
				// Intersect.
				for id := range result {
					if _, ok := matches[id]; !ok {
						delete(result, id)
					}
				}
			}
		}
	}

	ids := make([]string, 0, len(result))
	for id := range result {
		ids = append(ids, id)
	}
	return ids
}

func (idx *AttributeIndex) applyFilter(f Filter) map[string]struct{} {
	result := make(map[string]struct{})

	switch f.Op {
	case OpEq:
		idx.filterEq(f.Field, f.Value, result)
	case OpNeq:
		idx.filterNeq(f.Field, f.Value, result)
	case OpGt:
		idx.filterNumericRange(f.Field, f.Value, result, false, false)
	case OpGte:
		idx.filterNumericRange(f.Field, f.Value, result, true, false)
	case OpLt:
		idx.filterNumericRange(f.Field, f.Value, result, false, true)
	case OpLte:
		idx.filterNumericRange(f.Field, f.Value, result, true, true)
	case OpIn:
		if values, ok := f.Value.([]any); ok {
			for _, v := range values {
				idx.filterEq(f.Field, v, result)
			}
		}
	case OpContains:
		// For array fields: check if any element matches.
		idx.filterEq(f.Field, f.Value, result)
	}

	return result
}

func (idx *AttributeIndex) filterEq(field string, value any, result map[string]struct{}) {
	switch v := value.(type) {
	case string:
		if fm, ok := idx.stringIndexes[field]; ok {
			if docs, ok := fm[v]; ok {
				for id := range docs {
					result[id] = struct{}{}
				}
			}
		}
	case float64:
		if tree, ok := idx.numericIndexes[field]; ok {
			tree.AscendGreaterOrEqual(numericEntry{value: v}, func(e numericEntry) bool {
				if e.value != v {
					return false
				}
				result[e.docID] = struct{}{}
				return true
			})
		}
	case bool:
		bi := 0
		if v {
			bi = 1
		}
		if bf, ok := idx.boolIndexes[field]; ok {
			for id := range bf[bi] {
				result[id] = struct{}{}
			}
		}
	}
}

func (idx *AttributeIndex) filterNeq(field string, value any, result map[string]struct{}) {
	switch v := value.(type) {
	case string:
		if fm, ok := idx.stringIndexes[field]; ok {
			for val, docs := range fm {
				if val != v {
					for id := range docs {
						result[id] = struct{}{}
					}
				}
			}
		}
	case float64:
		if tree, ok := idx.numericIndexes[field]; ok {
			tree.Ascend(func(e numericEntry) bool {
				if e.value != v {
					result[e.docID] = struct{}{}
				}
				return true
			})
		}
	case bool:
		bi := 0
		if !v {
			bi = 1
		}
		if bf, ok := idx.boolIndexes[field]; ok {
			for id := range bf[bi] {
				result[id] = struct{}{}
			}
		}
	}
}

func (idx *AttributeIndex) filterNumericRange(field string, value any, result map[string]struct{}, inclusive, lessThan bool) {
	v, ok := toFloat64(value)
	if !ok {
		return
	}

	tree, ok := idx.numericIndexes[field]
	if !ok {
		return
	}

	if lessThan {
		tree.AscendLessThan(numericEntry{value: v + 1}, func(e numericEntry) bool {
			if inclusive && e.value <= v {
				result[e.docID] = struct{}{}
			} else if !inclusive && e.value < v {
				result[e.docID] = struct{}{}
			}
			return true
		})
	} else {
		tree.AscendGreaterOrEqual(numericEntry{value: v}, func(e numericEntry) bool {
			if inclusive {
				result[e.docID] = struct{}{}
			} else if e.value > v {
				result[e.docID] = struct{}{}
			}
			return true
		})
	}
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// Count returns the number of indexed documents.
func (idx *AttributeIndex) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.docFields)
}

// String returns a debug summary.
func (idx *AttributeIndex) String() string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return fmt.Sprintf("AttributeIndex{docs=%d, string_fields=%d, numeric_fields=%d, bool_fields=%d}",
		len(idx.docFields), len(idx.stringIndexes), len(idx.numericIndexes), len(idx.boolIndexes))
}

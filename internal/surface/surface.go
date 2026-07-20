// Package surface implements the surface v2 UI format: a flat, typed component
// tree with declared data-bound actions. It has no dependencies outside the
// standard library so it can be embedded by the store, render pipeline, HTTP
// API, and LLM prompt layers without import cycles.
//
// A Surface is a flat list of Components addressed by ID. A component with ID
// "root" is the entry point; layout components (Column, Row, Card) reference
// their children by ID. Property values may be $data references of the form
// {"$data": "path.to.value"} which the renderer resolves against the data
// object returned by the page's query at render time.
package surface

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Surface is the top-level component tree.
type Surface struct {
	Components []Component `json:"components"`
}

// Component is a single node in the surface. Properties are validated per-type
// against the component registry. Children are IDs of other components and are
// only permitted on layout types (Column, Row, Card).
type Component struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
	Children   []string       `json:"children,omitempty"`
}

// ParseSurface unmarshals a surface document. It does not validate; call
// Validate for structural and semantic checks.
func ParseSurface(data []byte) (*Surface, error) {
	var s Surface
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("parse surface: %w", err)
	}
	return &s, nil
}

// IsDataRef reports whether v is a $data reference — a map with exactly one key
// "$data" whose value is a string — and returns the referenced path.
func IsDataRef(v any) (path string, ok bool) {
	m, isMap := v.(map[string]any)
	if !isMap || len(m) != 1 {
		return "", false
	}
	raw, has := m["$data"]
	if !has {
		return "", false
	}
	s, isStr := raw.(string)
	if !isStr {
		return "", false
	}
	return s, true
}

// ResolvePath looks up a dot-separated path in a nested map, e.g. "a.b.c".
// It returns (value, true) on success. It returns (nil, false) when the path
// is empty, a segment is missing, or an intermediate value is not a map.
func ResolvePath(data map[string]any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	segments := strings.Split(path, ".")
	var cur any = data
	for _, seg := range segments {
		if seg == "" {
			return nil, false
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, exists := m[seg]
		if !exists {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

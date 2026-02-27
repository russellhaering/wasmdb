package a2ui

import (
	"encoding/json"
	"fmt"
)

// Surface is the top-level A2UI component tree.
type Surface struct {
	Components []Component `json:"components"`
}

// Component represents a single A2UI component node.
type Component struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
	Children   []string       `json:"children,omitempty"`
}

// ColumnDef defines a column in a DataTable.
type ColumnDef struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// DataTableProps holds the typed properties for a DataTable component.
type DataTableProps struct {
	Columns []ColumnDef      `json:"columns"`
	Rows    []map[string]any `json:"rows"`
	Caption string           `json:"caption,omitempty"`
}

// ParseDataTableProps extracts typed DataTable properties from a component.
func ParseDataTableProps(c Component) (DataTableProps, error) {
	data, err := json.Marshal(c.Properties)
	if err != nil {
		return DataTableProps{}, fmt.Errorf("marshal properties: %w", err)
	}
	var props DataTableProps
	if err := json.Unmarshal(data, &props); err != nil {
		return DataTableProps{}, fmt.Errorf("unmarshal DataTable properties: %w", err)
	}
	return props, nil
}

// validTypes is the set of recognized component types.
var validTypes = map[string]bool{
	"Column":    true,
	"Row":       true,
	"DataTable": true,
	"Card":      true,
	"Text":      true,
	"Divider":   true,
}

// Validate checks that a Surface is well-formed:
// - At least one component exists
// - A component with id "root" exists
// - All child references resolve to existing component IDs
// - All component types are recognized
// - No cycles in the component tree
func Validate(s Surface) error {
	if len(s.Components) == 0 {
		return fmt.Errorf("surface has no components")
	}

	index := make(map[string]*Component, len(s.Components))
	for i := range s.Components {
		c := &s.Components[i]
		if c.ID == "" {
			return fmt.Errorf("component at index %d has empty id", i)
		}
		if _, dup := index[c.ID]; dup {
			return fmt.Errorf("duplicate component id: %q", c.ID)
		}
		index[c.ID] = c
	}

	if _, ok := index["root"]; !ok {
		return fmt.Errorf("no root component found")
	}

	for _, c := range s.Components {
		if !validTypes[c.Type] {
			return fmt.Errorf("unknown component type %q on component %q", c.Type, c.ID)
		}
		for _, childID := range c.Children {
			if _, ok := index[childID]; !ok {
				return fmt.Errorf("component %q references unknown child %q", c.ID, childID)
			}
		}
	}

	// Cycle detection via DFS.
	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make(map[string]int, len(s.Components))

	var visit func(id string) error
	visit = func(id string) error {
		if state[id] == visited {
			return nil
		}
		if state[id] == visiting {
			return fmt.Errorf("cycle detected at component %q", id)
		}
		state[id] = visiting
		c := index[id]
		for _, childID := range c.Children {
			if err := visit(childID); err != nil {
				return err
			}
		}
		state[id] = visited
		return nil
	}

	for _, c := range s.Components {
		if err := visit(c.ID); err != nil {
			return err
		}
	}

	return nil
}

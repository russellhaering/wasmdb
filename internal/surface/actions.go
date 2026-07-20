package surface

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ActionType is the kind of operation an action performs.
type ActionType string

const (
	ActionInsert ActionType = "insert"
	ActionUpdate ActionType = "update"
	ActionDelete ActionType = "delete"
	ActionQuery  ActionType = "query"
)

// Action is a declared operation a page component may trigger. Insert, update,
// and delete write to Table; query re-runs the page's query with Params.
type Action struct {
	Type    ActionType `json:"type"`
	Table   string     `json:"table,omitempty"`  // required for insert/update/delete
	Params  []string   `json:"params,omitempty"` // declared param names for query actions
	Confirm bool       `json:"confirm,omitempty"`
}

// Actions maps action names to their declarations.
type Actions map[string]Action

// ParseActions unmarshals the page's action declarations. Empty or nil input
// yields an empty Actions with no error. It is purely structural: semantic
// checks (types, tables, references) are performed by Validate so that all
// problems can be reported to the caller at once.
func ParseActions(data []byte) (Actions, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return Actions{}, nil
	}
	var a Actions
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("parse actions: %w", err)
	}
	if a == nil {
		return Actions{}, nil
	}
	return a, nil
}

package a2ui

import (
	"strings"
	"testing"
)

func TestValidate_DataTable(t *testing.T) {
	s := Surface{
		Components: []Component{
			{ID: "root", Type: "Column", Children: []string{"t1"}},
			{ID: "t1", Type: "DataTable", Properties: map[string]any{
				"columns": []any{
					map[string]any{"key": "id", "label": "ID"},
					map[string]any{"key": "name", "label": "Name"},
				},
				"rows": []any{
					map[string]any{"id": "doc-001", "name": "My Doc"},
				},
			}},
		},
	}
	if err := Validate(s); err != nil {
		t.Fatalf("expected valid surface, got: %v", err)
	}
}

func TestValidate_Card(t *testing.T) {
	s := Surface{
		Components: []Component{
			{ID: "root", Type: "Card", Properties: map[string]any{
				"title": "Document: doc-001",
			}, Children: []string{"f1", "f2"}},
			{ID: "f1", Type: "Text", Properties: map[string]any{"label": "Name", "text": "My Doc"}},
			{ID: "f2", Type: "Text", Properties: map[string]any{"label": "Status", "text": "active"}},
		},
	}
	if err := Validate(s); err != nil {
		t.Fatalf("expected valid surface, got: %v", err)
	}
}

func TestValidate_NestedColumn(t *testing.T) {
	s := Surface{
		Components: []Component{
			{ID: "root", Type: "Column", Children: []string{"summary", "d1", "t1"}},
			{ID: "summary", Type: "Text", Properties: map[string]any{"text": "Found 3 results"}},
			{ID: "d1", Type: "Divider"},
			{ID: "t1", Type: "DataTable", Properties: map[string]any{
				"columns": []any{map[string]any{"key": "id", "label": "ID"}},
				"rows":    []any{map[string]any{"id": "1"}},
			}},
		},
	}
	if err := Validate(s); err != nil {
		t.Fatalf("expected valid surface, got: %v", err)
	}
}

func TestValidate_RowLayout(t *testing.T) {
	s := Surface{
		Components: []Component{
			{ID: "root", Type: "Row", Children: []string{"c1", "c2"}},
			{ID: "c1", Type: "Card", Properties: map[string]any{"title": "Left"}},
			{ID: "c2", Type: "Card", Properties: map[string]any{"title": "Right"}},
		},
	}
	if err := Validate(s); err != nil {
		t.Fatalf("expected valid surface, got: %v", err)
	}
}

func TestValidate_EmptySurface(t *testing.T) {
	err := Validate(Surface{})
	if err == nil {
		t.Fatal("expected error for empty surface")
	}
	if !strings.Contains(err.Error(), "no components") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_MissingRoot(t *testing.T) {
	s := Surface{
		Components: []Component{
			{ID: "notroot", Type: "Text", Properties: map[string]any{"text": "hi"}},
		},
	}
	err := Validate(s)
	if err == nil {
		t.Fatal("expected error for missing root")
	}
	if !strings.Contains(err.Error(), "no root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_BrokenChildRef(t *testing.T) {
	s := Surface{
		Components: []Component{
			{ID: "root", Type: "Column", Children: []string{"missing"}},
		},
	}
	err := Validate(s)
	if err == nil {
		t.Fatal("expected error for broken child ref")
	}
	if !strings.Contains(err.Error(), "unknown child") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_UnknownType(t *testing.T) {
	s := Surface{
		Components: []Component{
			{ID: "root", Type: "FancyWidget"},
		},
	}
	err := Validate(s)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if !strings.Contains(err.Error(), "unknown component type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_DuplicateID(t *testing.T) {
	s := Surface{
		Components: []Component{
			{ID: "root", Type: "Column", Children: []string{"root"}},
			{ID: "root", Type: "Text", Properties: map[string]any{"text": "dup"}},
		},
	}
	err := Validate(s)
	if err == nil {
		t.Fatal("expected error for duplicate id")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_Cycle(t *testing.T) {
	s := Surface{
		Components: []Component{
			{ID: "root", Type: "Column", Children: []string{"a"}},
			{ID: "a", Type: "Column", Children: []string{"b"}},
			{ID: "b", Type: "Column", Children: []string{"a"}},
		},
	}
	err := Validate(s)
	if err == nil {
		t.Fatal("expected error for cycle")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_EmptyID(t *testing.T) {
	s := Surface{
		Components: []Component{
			{ID: "", Type: "Text"},
		},
	}
	err := Validate(s)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
	if !strings.Contains(err.Error(), "empty id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseDataTableProps(t *testing.T) {
	c := Component{
		ID:   "t1",
		Type: "DataTable",
		Properties: map[string]any{
			"columns": []any{
				map[string]any{"key": "id", "label": "ID"},
				map[string]any{"key": "name", "label": "Name"},
			},
			"rows": []any{
				map[string]any{"id": "doc-001", "name": "My Doc"},
				map[string]any{"id": "doc-002", "name": "Other"},
			},
			"caption": "Documents",
		},
	}
	props, err := ParseDataTableProps(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(props.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(props.Columns))
	}
	if props.Columns[0].Key != "id" || props.Columns[0].Label != "ID" {
		t.Fatalf("unexpected column 0: %+v", props.Columns[0])
	}
	if len(props.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(props.Rows))
	}
	if props.Caption != "Documents" {
		t.Fatalf("expected caption 'Documents', got %q", props.Caption)
	}
}

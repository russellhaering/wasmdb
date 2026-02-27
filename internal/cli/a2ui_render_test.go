package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderDataTable(t *testing.T) {
	jsonStr := `{
		"components": [
			{"id": "root", "type": "Column", "children": ["t1"]},
			{"id": "t1", "type": "DataTable", "properties": {
				"columns": [{"key": "id", "label": "ID"}, {"key": "name", "label": "Name"}, {"key": "status", "label": "Status"}],
				"rows": [
					{"id": "doc-001", "name": "Getting Started", "status": "published"},
					{"id": "doc-002", "name": "API Reference", "status": "draft"}
				]
			}}
		]
	}`

	var buf bytes.Buffer
	if err := RenderA2UI(&buf, jsonStr); err != nil {
		t.Fatalf("render error: %v", err)
	}

	out := buf.String()

	// Check box-drawing characters.
	if !strings.Contains(out, "┌") || !strings.Contains(out, "┐") {
		t.Error("missing top border")
	}
	if !strings.Contains(out, "├") || !strings.Contains(out, "┤") {
		t.Error("missing header separator")
	}
	if !strings.Contains(out, "└") || !strings.Contains(out, "┘") {
		t.Error("missing bottom border")
	}
	if !strings.Contains(out, "│") {
		t.Error("missing column separators")
	}

	// Check data is present.
	if !strings.Contains(out, "doc-001") {
		t.Error("missing row data doc-001")
	}
	if !strings.Contains(out, "Getting Started") {
		t.Error("missing row data Getting Started")
	}
	if !strings.Contains(out, "draft") {
		t.Error("missing row data draft")
	}
}

func TestRenderDataTableWithCaption(t *testing.T) {
	jsonStr := `{
		"components": [
			{"id": "root", "type": "DataTable", "properties": {
				"columns": [{"key": "id", "label": "ID"}],
				"rows": [{"id": "1"}],
				"caption": "Test Table"
			}}
		]
	}`

	var buf bytes.Buffer
	if err := RenderA2UI(&buf, jsonStr); err != nil {
		t.Fatalf("render error: %v", err)
	}

	if !strings.Contains(buf.String(), "Test Table") {
		t.Error("missing caption")
	}
}

func TestRenderCard(t *testing.T) {
	jsonStr := `{
		"components": [
			{"id": "root", "type": "Card", "properties": {"title": "My Document"}, "children": ["f1", "f2"]},
			{"id": "f1", "type": "Text", "properties": {"label": "Name", "text": "Test Doc"}},
			{"id": "f2", "type": "Text", "properties": {"label": "Status", "text": "active"}}
		]
	}`

	var buf bytes.Buffer
	if err := RenderA2UI(&buf, jsonStr); err != nil {
		t.Fatalf("render error: %v", err)
	}

	out := buf.String()

	if !strings.Contains(out, "My Document") {
		t.Error("missing card title")
	}
	if !strings.Contains(out, "Name:") {
		t.Error("missing label Name")
	}
	if !strings.Contains(out, "Test Doc") {
		t.Error("missing value Test Doc")
	}
	if !strings.Contains(out, "┌") || !strings.Contains(out, "└") {
		t.Error("missing card border")
	}
}

func TestRenderEmptyTable(t *testing.T) {
	jsonStr := `{
		"components": [
			{"id": "root", "type": "DataTable", "properties": {
				"columns": [{"key": "id", "label": "ID"}, {"key": "name", "label": "Name"}],
				"rows": []
			}}
		]
	}`

	var buf bytes.Buffer
	if err := RenderA2UI(&buf, jsonStr); err != nil {
		t.Fatalf("render error: %v", err)
	}

	out := buf.String()
	// Should have header but no data rows.
	if !strings.Contains(out, "ID") {
		t.Error("missing header")
	}
	// Count data rows (lines between ┤ and └).
	lines := strings.Split(out, "\n")
	dataRows := 0
	pastHeader := false
	for _, l := range lines {
		if strings.Contains(l, "┤") {
			pastHeader = true
			continue
		}
		if pastHeader && strings.HasPrefix(l, "│") {
			dataRows++
		}
	}
	if dataRows != 0 {
		t.Errorf("expected 0 data rows, got %d", dataRows)
	}
}

func TestRenderSingleRow(t *testing.T) {
	jsonStr := `{
		"components": [
			{"id": "root", "type": "DataTable", "properties": {
				"columns": [{"key": "x", "label": "X"}],
				"rows": [{"x": "hello"}]
			}}
		]
	}`

	var buf bytes.Buffer
	if err := RenderA2UI(&buf, jsonStr); err != nil {
		t.Fatalf("render error: %v", err)
	}

	if !strings.Contains(buf.String(), "hello") {
		t.Error("missing single row data")
	}
}

func TestRenderTextStyles(t *testing.T) {
	jsonStr := `{
		"components": [
			{"id": "root", "type": "Column", "children": ["t1", "t2", "t3", "t4"]},
			{"id": "t1", "type": "Text", "properties": {"text": "Normal text"}},
			{"id": "t2", "type": "Text", "properties": {"text": "Bold text", "style": "bold"}},
			{"id": "t3", "type": "Text", "properties": {"text": "Dim text", "style": "dim"}},
			{"id": "t4", "type": "Text", "properties": {"label": "Key", "text": "Value"}}
		]
	}`

	var buf bytes.Buffer
	if err := RenderA2UI(&buf, jsonStr); err != nil {
		t.Fatalf("render error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Normal text") {
		t.Error("missing normal text")
	}
	if !strings.Contains(out, "Bold text") {
		t.Error("missing bold text")
	}
	if !strings.Contains(out, "Key:") {
		t.Error("missing label")
	}
}

func TestRenderDivider(t *testing.T) {
	jsonStr := `{
		"components": [
			{"id": "root", "type": "Column", "children": ["d1"]},
			{"id": "d1", "type": "Divider"}
		]
	}`

	var buf bytes.Buffer
	if err := RenderA2UI(&buf, jsonStr); err != nil {
		t.Fatalf("render error: %v", err)
	}

	if !strings.Contains(buf.String(), "─") {
		t.Error("missing divider line")
	}
}

func TestExtractA2UIBlocks(t *testing.T) {
	text := "Here are your results:\n```a2ui\n{\"components\":[]}\n```\nDone."

	segments := ExtractA2UIBlocks(text)

	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segments))
	}
	if segments[0].IsA2UI || !strings.Contains(segments[0].Text, "results") {
		t.Errorf("segment 0 wrong: %+v", segments[0])
	}
	if !segments[1].IsA2UI {
		t.Errorf("segment 1 should be A2UI: %+v", segments[1])
	}
	if segments[2].IsA2UI || !strings.Contains(segments[2].Text, "Done") {
		t.Errorf("segment 2 wrong: %+v", segments[2])
	}
}

func TestExtractA2UIBlocks_NoBlocks(t *testing.T) {
	text := "Just plain text."
	segments := ExtractA2UIBlocks(text)
	if len(segments) != 1 || segments[0].IsA2UI {
		t.Fatalf("expected 1 plain segment, got %+v", segments)
	}
}

func TestExtractA2UIBlocks_MultipleBlocks(t *testing.T) {
	text := "A\n```a2ui\n{\"a\":1}\n```\nB\n```a2ui\n{\"b\":2}\n```\nC"
	segments := ExtractA2UIBlocks(text)
	if len(segments) != 5 {
		t.Fatalf("expected 5 segments, got %d: %+v", len(segments), segments)
	}
	if !segments[1].IsA2UI || !segments[3].IsA2UI {
		t.Error("expected segments 1 and 3 to be A2UI")
	}
}

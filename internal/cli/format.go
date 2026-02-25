package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/russellhaering/wasmdb/internal/document"
)

// formatJSON writes v as indented JSON followed by a newline.
func formatJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// formatTableList writes a compact list of table names.
func formatTableList(w io.Writer, tables []TableInfo) {
	for _, tbl := range tables {
		fmt.Fprintln(w, tbl.Name)
	}
}

// formatTableInfo writes compact table info.
func formatTableInfo(w io.Writer, tbl *TableInfo) {
	fmt.Fprintf(w, "name: %s\n", tbl.Name)
	if tbl.Schema != nil && len(tbl.Schema.Fields) > 0 {
		fmt.Fprintln(w, "schema:")
		for _, f := range tbl.Schema.Fields {
			flags := fieldFlags(f)
			if flags != "" {
				fmt.Fprintf(w, "  %s: %s (%s)\n", f.Name, f.Type, flags)
			} else {
				fmt.Fprintf(w, "  %s: %s\n", f.Name, f.Type)
			}
		}
	}
	if tbl.Schema != nil && tbl.Schema.EmbeddingModel != "" {
		fmt.Fprintf(w, "embedding_model: %s\n", tbl.Schema.EmbeddingModel)
	}
}

func fieldFlags(f document.FieldDefinition) string {
	var flags []string
	if f.Required {
		flags = append(flags, "required")
	}
	if f.Indexed {
		flags = append(flags, "indexed")
	}
	if f.FullText {
		flags = append(flags, "full_text")
	}
	return strings.Join(flags, ", ")
}

// formatDocument writes a compact document representation.
func formatDocument(w io.Writer, doc *document.Document) {
	fmt.Fprintf(w, "id: %s\n", doc.ID)
	if doc.Content != "" {
		fmt.Fprintf(w, "content: %s\n", doc.Content)
	}
	if len(doc.Attributes) > 0 {
		fmt.Fprintln(w, "attributes:")
		keys := sortedKeys(doc.Attributes)
		for _, k := range keys {
			fmt.Fprintf(w, "  %s: %v\n", k, formatValue(doc.Attributes[k]))
		}
	}
	fmt.Fprintf(w, "version: %d\n", doc.Version)
	if !doc.CreatedAt.IsZero() {
		fmt.Fprintf(w, "created: %s\n", doc.CreatedAt.Format(time.RFC3339))
	}
	if !doc.UpdatedAt.IsZero() {
		fmt.Fprintf(w, "updated: %s\n", doc.UpdatedAt.Format(time.RFC3339))
	}
}

// formatDocumentShort writes a one-line document summary (id + content prefix).
func formatDocumentShort(w io.Writer, doc *document.Document) {
	content := doc.Content
	if len(content) > 80 {
		content = content[:77] + "..."
	}
	if content != "" {
		fmt.Fprintf(w, "%s\t%s\n", doc.ID, content)
	} else {
		fmt.Fprintln(w, doc.ID)
	}
}

// formatSchema writes a compact schema representation.
func formatSchema(w io.Writer, schema *document.Schema) {
	if schema == nil {
		fmt.Fprintln(w, "(no schema)")
		return
	}
	if schema.EmbeddingModel != "" {
		fmt.Fprintf(w, "embedding_model: %s\n", schema.EmbeddingModel)
		if schema.EmbeddingDimensions > 0 {
			fmt.Fprintf(w, "embedding_dimensions: %d\n", schema.EmbeddingDimensions)
		}
	}
	if len(schema.Fields) == 0 {
		fmt.Fprintln(w, "fields: (none)")
		return
	}
	fmt.Fprintln(w, "fields:")
	for _, f := range schema.Fields {
		flags := fieldFlags(f)
		if flags != "" {
			fmt.Fprintf(w, "  %s: %s (%s)\n", f.Name, f.Type, flags)
		} else {
			fmt.Fprintf(w, "  %s: %s\n", f.Name, f.Type)
		}
	}
}

// formatBulkResult writes a compact bulk result.
func formatBulkResult(w io.Writer, r *BulkResult) {
	fmt.Fprintf(w, "created: %d\n", r.Count)
}

// formatTextSearchResult writes compact text search results.
func formatTextSearchResult(w io.Writer, r *TextSearchResult) {
	fmt.Fprintf(w, "total: %d\n", r.Total)
	for _, doc := range r.Results {
		formatDocumentShort(w, doc)
	}
}

// formatDocumentList writes a compact list of documents.
func formatDocumentList(w io.Writer, docs []*document.Document) {
	for _, doc := range docs {
		formatDocumentShort(w, doc)
	}
}

func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []any:
		parts := make([]string, len(val))
		for i, elem := range val {
			parts[i] = fmt.Sprintf("%v", elem)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

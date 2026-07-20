package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/russellhaering/moraine/document"
)

func init() {
	register(command{
		noun:        "doc",
		verb:        "create",
		usage:       "wasmdb doc create <db> [--id <id>] [--content <text>] [--attr key=value]... [--file <path>] [--json]",
		description: "Create a document",
		run:         docCreate,
	})
	register(command{
		noun:        "doc",
		verb:        "get",
		usage:       "wasmdb doc get <db> <id> [--json]",
		description: "Get a document",
		run:         docGet,
	})
	register(command{
		noun:        "doc",
		verb:        "update",
		usage:       "wasmdb doc update <db> <id> [--content <text>] [--attr key=value]... [--file <path>] [--json]",
		description: "Update a document",
		run:         docUpdate,
	})
	register(command{
		noun:        "doc",
		verb:        "delete",
		usage:       "wasmdb doc delete <db> <id>",
		description: "Delete a document",
		run:         docDelete,
	})
	register(command{
		noun:        "doc",
		verb:        "bulk",
		usage:       "wasmdb doc bulk <db> --file <path> [--json]",
		description: "Bulk create documents from JSON",
		run:         docBulk,
	})
}

func docCreate(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb doc create <db>")
	}
	db := ctx.args[0]

	doc := &document.Document{
		ID:      ctx.flag("id"),
		Content: ctx.flag("content"),
	}

	// Parse --attr key=value flags.
	attrs, err := parseAttrs(ctx.flagAll("attr"))
	if err != nil {
		return err
	}
	if len(attrs) > 0 {
		doc.Attributes = attrs
	}

	// Load from file if --file provided.
	if file := ctx.flag("file"); file != "" {
		filDoc, err := readDocumentFile(file, ctx.stdin)
		if err != nil {
			return err
		}
		// File fields are overridden by explicit flags.
		if doc.ID == "" {
			doc.ID = filDoc.ID
		}
		if doc.Content == "" {
			doc.Content = filDoc.Content
		}
		if doc.Attributes == nil {
			doc.Attributes = filDoc.Attributes
		}
	}

	result, err := ctx.backend.CreateDocument(ctx, db, doc)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, result)
	}
	fmt.Fprintf(ctx.stdout, "id: %s\nversion: %d\n", result.ID, result.Version)
	return nil
}

func docGet(ctx *cmdContext) error {
	if len(ctx.args) < 2 {
		return fmt.Errorf("usage: wasmdb doc get <db> <id>")
	}
	db, id := ctx.args[0], ctx.args[1]

	doc, err := ctx.backend.GetDocument(ctx, db, id)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, doc)
	}
	formatDocument(ctx.stdout, doc)
	return nil
}

func docUpdate(ctx *cmdContext) error {
	if len(ctx.args) < 2 {
		return fmt.Errorf("usage: wasmdb doc update <db> <id>")
	}
	db, id := ctx.args[0], ctx.args[1]

	doc := &document.Document{
		Content: ctx.flag("content"),
	}

	attrs, err := parseAttrs(ctx.flagAll("attr"))
	if err != nil {
		return err
	}
	if len(attrs) > 0 {
		doc.Attributes = attrs
	}

	if file := ctx.flag("file"); file != "" {
		filDoc, err := readDocumentFile(file, ctx.stdin)
		if err != nil {
			return err
		}
		if doc.Content == "" {
			doc.Content = filDoc.Content
		}
		if doc.Attributes == nil {
			doc.Attributes = filDoc.Attributes
		}
	}

	result, err := ctx.backend.UpdateDocument(ctx, db, id, doc)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, result)
	}
	fmt.Fprintf(ctx.stdout, "id: %s\nversion: %d\n", result.ID, result.Version)
	return nil
}

func docDelete(ctx *cmdContext) error {
	if len(ctx.args) < 2 {
		return fmt.Errorf("usage: wasmdb doc delete <db> <id>")
	}
	db, id := ctx.args[0], ctx.args[1]

	if err := ctx.backend.DeleteDocument(ctx, db, id); err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, map[string]string{"status": "deleted"})
	}
	fmt.Fprintln(ctx.stdout, "deleted")
	return nil
}

func docBulk(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb doc bulk <db> --file <path>")
	}
	db := ctx.args[0]

	file := ctx.flag("file")
	if file == "" {
		return fmt.Errorf("--file is required")
	}

	var data []byte
	var err error
	if file == "-" {
		data, err = io.ReadAll(ctx.stdin)
	} else {
		data, err = os.ReadFile(file)
	}
	if err != nil {
		return fmt.Errorf("read bulk file: %w", err)
	}

	var docs []*document.Document
	if err := json.Unmarshal(data, &docs); err != nil {
		return fmt.Errorf("parse bulk file: %w", err)
	}

	result, err := ctx.backend.BulkCreateDocuments(ctx, db, docs)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, result)
	}
	formatBulkResult(ctx.stdout, result)
	return nil
}

// parseAttrs parses key=value pairs into a map. Values are parsed as JSON
// if they look like JSON, otherwise treated as strings.
func parseAttrs(pairs []string) (map[string]any, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	attrs := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("invalid attribute: %q (expected key=value)", pair)
		}
		attrs[key] = parseJSONValue(value)
	}
	return attrs, nil
}

// parseJSONValue tries to parse s as JSON. If it fails, returns s as a string.
func parseJSONValue(s string) any {
	if len(s) == 0 {
		return s
	}
	// Try JSON for values that look like JSON.
	switch s[0] {
	case '[', '{', '"':
		var v any
		if err := json.Unmarshal([]byte(s), &v); err == nil {
			return v
		}
	}
	switch s {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	// Try number.
	if s[0] >= '0' && s[0] <= '9' || s[0] == '-' {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err == nil {
			return v
		}
	}
	return s
}

func readDocumentFile(path string, stdin io.Reader) (*document.Document, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read document file: %w", err)
	}
	var doc document.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse document file: %w", err)
	}
	return &doc, nil
}

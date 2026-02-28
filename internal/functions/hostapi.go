package functions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fastschema/qjs"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/index"
)

// bindDBAPI injects the `db` global object into the JS context.
func bindDBAPI(jsCtx *qjs.Context, registry *database.Registry, ctx context.Context) {
	dbObj := jsCtx.NewObject()

	// db.tables() → [{name, system}]
	dbObj.SetPropertyStr("tables", jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		metas, err := registry.ListTables(ctx)
		if err != nil {
			return nil, fmt.Errorf("db.tables(): %w", err)
		}
		var result []map[string]any
		for _, m := range metas {
			result = append(result, map[string]any{
				"name":   m.Name,
				"system": m.System,
			})
		}
		return toJsJSON(jsCtx, result)
	}))

	// db.createTable(name) → {name}
	dbObj.SetPropertyStr("createTable", jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) < 1 {
			return nil, fmt.Errorf("db.createTable: name required")
		}
		name := args[0].String()
		if strings.HasPrefix(name, "_") {
			return nil, fmt.Errorf("db.createTable: cannot create system tables")
		}
		tbl, err := registry.CreateTable(ctx, name, nil)
		if err != nil {
			return nil, fmt.Errorf("db.createTable: %w", err)
		}
		return toJsJSON(jsCtx, map[string]any{"name": tbl.Name})
	}))

	// db.deleteTable(name) → true
	dbObj.SetPropertyStr("deleteTable", jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) < 1 {
			return nil, fmt.Errorf("db.deleteTable: name required")
		}
		name := args[0].String()
		if strings.HasPrefix(name, "_") {
			return nil, fmt.Errorf("db.deleteTable: cannot delete system tables")
		}
		if err := registry.DeleteTable(ctx, name); err != nil {
			return nil, fmt.Errorf("db.deleteTable: %w", err)
		}
		return jsCtx.NewBool(true), nil
	}))

	// db.table(name) → table proxy object
	dbObj.SetPropertyStr("table", jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) < 1 {
			return nil, fmt.Errorf("db.table: name required")
		}
		name := args[0].String()
		if strings.HasPrefix(name, "_") {
			return nil, fmt.Errorf("db.table: cannot access system tables")
		}
		return makeTableProxy(jsCtx, registry, ctx, name)
	}))

	jsCtx.Global().SetPropertyStr("db", dbObj)
}

// makeTableProxy creates a JS object with methods for table operations.
func makeTableProxy(jsCtx *qjs.Context, registry *database.Registry, ctx context.Context, tableName string) (*qjs.Value, error) {
	tblObj := jsCtx.NewObject()

	// t.get(id) → document
	tblObj.SetPropertyStr("get", jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) < 1 {
			return nil, fmt.Errorf("table.get: id required")
		}
		id := args[0].String()

		tbl, err := registry.GetTable(ctx, tableName)
		if err != nil {
			return nil, fmt.Errorf("table.get: %w", err)
		}

		doc, err := tbl.GetDocument(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("table.get: %w", err)
		}
		if doc == nil {
			return jsCtx.NewNull(), nil
		}
		return docToJs(jsCtx, doc)
	}))

	// t.list(limit?, afterKey?) → [document]
	tblObj.SetPropertyStr("list", jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		limit := 100
		afterKey := ""
		if len(args) > 0 && !args[0].IsUndefined() {
			limit = int(args[0].Int64())
		}
		if len(args) > 1 && !args[1].IsUndefined() {
			afterKey = args[1].String()
		}

		tbl, err := registry.GetTable(ctx, tableName)
		if err != nil {
			return nil, fmt.Errorf("table.list: %w", err)
		}

		docs, _, err := tbl.ListDocuments(ctx, limit, afterKey)
		if err != nil {
			return nil, fmt.Errorf("table.list: %w", err)
		}
		return docsToJs(jsCtx, docs)
	}))

	// t.put({id?, content?, attributes?}) → {id, version}
	tblObj.SetPropertyStr("put", jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) < 1 {
			return nil, fmt.Errorf("table.put: document object required")
		}

		doc, err := jsToDoc(args[0])
		if err != nil {
			return nil, fmt.Errorf("table.put: %w", err)
		}

		tbl, err := registry.GetTable(ctx, tableName)
		if err != nil {
			return nil, fmt.Errorf("table.put: %w", err)
		}

		if err := tbl.PutDocument(ctx, doc); err != nil {
			return nil, fmt.Errorf("table.put: %w", err)
		}

		return toJsJSON(jsCtx, map[string]any{
			"id":      doc.ID,
			"version": doc.Version,
		})
	}))

	// t.delete(id) → true
	tblObj.SetPropertyStr("delete", jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) < 1 {
			return nil, fmt.Errorf("table.delete: id required")
		}
		id := args[0].String()

		tbl, err := registry.GetTable(ctx, tableName)
		if err != nil {
			return nil, fmt.Errorf("table.delete: %w", err)
		}

		if err := tbl.DeleteDocument(ctx, id); err != nil {
			return nil, fmt.Errorf("table.delete: %w", err)
		}
		return jsCtx.NewBool(true), nil
	}))

	// t.search.text(query, limit?, offset?) and t.search.attr(filters, limit?, offset?)
	searchObj := jsCtx.NewObject()

	searchObj.SetPropertyStr("text", jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) < 1 {
			return nil, fmt.Errorf("table.search.text: query required")
		}
		query := args[0].String()
		limit := 10
		offset := 0
		if len(args) > 1 && !args[1].IsUndefined() {
			limit = int(args[1].Int64())
		}
		if len(args) > 2 && !args[2].IsUndefined() {
			offset = int(args[2].Int64())
		}

		tbl, err := registry.GetTable(ctx, tableName)
		if err != nil {
			return nil, fmt.Errorf("table.search.text: %w", err)
		}

		docs, _, err := tbl.SearchText(ctx, query, limit, offset)
		if err != nil {
			return nil, fmt.Errorf("table.search.text: %w", err)
		}
		return docsToJs(jsCtx, docs)
	}))

	searchObj.SetPropertyStr("attr", jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) < 1 {
			return nil, fmt.Errorf("table.search.attr: filters required")
		}

		// Parse filters from JS array.
		filtersJSON, err := args[0].JSONStringify()
		if err != nil {
			return nil, fmt.Errorf("table.search.attr: %w", err)
		}
		var rawFilters []struct {
			Field string `json:"field"`
			Op    string `json:"op"`
			Value any    `json:"value"`
		}
		if err := json.Unmarshal([]byte(filtersJSON), &rawFilters); err != nil {
			return nil, fmt.Errorf("table.search.attr: invalid filters: %w", err)
		}

		filters := make([]index.Filter, len(rawFilters))
		for i, f := range rawFilters {
			filters[i] = index.Filter{
				Field: f.Field,
				Op:    index.FilterOp(f.Op),
				Value: f.Value,
			}
		}

		limit := 10
		offset := 0
		if len(args) > 1 && !args[1].IsUndefined() {
			limit = int(args[1].Int64())
		}
		if len(args) > 2 && !args[2].IsUndefined() {
			offset = int(args[2].Int64())
		}

		tbl, err := registry.GetTable(ctx, tableName)
		if err != nil {
			return nil, fmt.Errorf("table.search.attr: %w", err)
		}

		docs, err := tbl.SearchAttributes(ctx, filters, limit, offset)
		if err != nil {
			return nil, fmt.Errorf("table.search.attr: %w", err)
		}
		return docsToJs(jsCtx, docs)
	}))

	tblObj.SetPropertyStr("search", searchObj)

	return tblObj, nil
}

// toJsJSON marshals a Go value to JSON and parses it as a JS value.
func toJsJSON(jsCtx *qjs.Context, v any) (*qjs.Value, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return jsCtx.ParseJSON(string(data)), nil
}

// docToJs converts a document.Document to a JS value.
func docToJs(jsCtx *qjs.Context, doc *document.Document) (*qjs.Value, error) {
	m := map[string]any{
		"id":         doc.ID,
		"content":    doc.Content,
		"attributes": doc.Attributes,
		"version":    doc.Version,
		"created_at": doc.CreatedAt,
		"updated_at": doc.UpdatedAt,
	}
	return toJsJSON(jsCtx, m)
}

// docsToJs converts a slice of documents to a JS array.
func docsToJs(jsCtx *qjs.Context, docs []*document.Document) (*qjs.Value, error) {
	var result []map[string]any
	for _, doc := range docs {
		result = append(result, map[string]any{
			"id":         doc.ID,
			"content":    doc.Content,
			"attributes": doc.Attributes,
			"version":    doc.Version,
		})
	}
	if result == nil {
		result = []map[string]any{}
	}
	return toJsJSON(jsCtx, result)
}

// jsToDoc converts a JS object to a document.Document.
func jsToDoc(v *qjs.Value) (*document.Document, error) {
	jsonStr, err := v.JSONStringify()
	if err != nil {
		return nil, fmt.Errorf("failed to stringify document: %w", err)
	}

	var raw struct {
		ID         string         `json:"id"`
		Content    string         `json:"content"`
		Attributes map[string]any `json:"attributes"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("invalid document JSON: %w", err)
	}

	return &document.Document{
		ID:         raw.ID,
		Content:    raw.Content,
		Attributes: raw.Attributes,
	}, nil
}

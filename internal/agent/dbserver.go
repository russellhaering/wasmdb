// Package agent provides the WasmDB agent integration, including MCP tools
// for table operations and chat session management.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/functions"
	"github.com/russellhaering/wasmdb/internal/index"
)

// NewTableServer creates an MCP server exposing wasmdb table operations as tools.
func NewTableServer(registry *database.Registry, fnEngine *functions.Engine, fnStore *functions.Store) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "wasmdb-table",
		Version: "v0.1.0",
	}, nil)

	h := &dbHandler{registry: registry, fnEngine: fnEngine, fnStore: fnStore}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_tables",
		Description: "List all tables in the WasmDB instance.",
	}, h.listTables)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_table",
		Description: "Get information about a specific table including its schema.",
	}, h.getTable)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_table",
		Description: "Create a new table with an optional schema.",
	}, h.createTable)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_document",
		Description: "Retrieve a document by its ID from a table.",
	}, h.getDocument)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_document",
		Description: "Create a new document in a table. You can provide an ID or let the system generate one.",
	}, h.createDocument)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_document",
		Description: "Update an existing document by ID in a table.",
	}, h.updateDocument)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_document",
		Description: "Delete a document by its ID from a table.",
	}, h.deleteDocument)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_documents",
		Description: "List documents in a table. Returns documents with pagination. Use this to browse or enumerate documents when you don't have a specific search query.",
	}, h.listDocuments)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_text",
		Description: "Search documents using full-text search. Returns matching documents with relevance ranking.",
	}, h.searchText)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_attributes",
		Description: "Search documents by filtering on attribute values. Supports eq, neq, gt, gte, lt, lte, contains, prefix operators.",
	}, h.searchAttributes)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "execute_code",
		Description: "Execute JavaScript code in a sandboxed environment with access to the db API for database operations. Use for bulk operations, data transformations, analytics, aggregations, finding duplicates, or any logic easier to express in code.",
	}, h.executeCode)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "manage_function",
		Description: "Create, update, get, list, or delete stored JavaScript functions. Stored functions persist and can be invoked by name.",
	}, h.manageFunction)

	return srv
}

type dbHandler struct {
	registry *database.Registry
	fnEngine *functions.Engine
	fnStore  *functions.Store
}

// --- Tool input types ---

type listTablesInput struct{}

type getTableInput struct {
	Name string `json:"name" jsonschema:"Table name"`
}

type createTableInput struct {
	Name   string           `json:"name" jsonschema:"Table name"`
	Schema *document.Schema `json:"schema,omitempty" jsonschema:"Optional schema definition"`
}

type getDocumentInput struct {
	Table string `json:"table" jsonschema:"Table name"`
	ID    string `json:"id" jsonschema:"Document ID"`
}

type createDocumentInput struct {
	Table      string         `json:"table" jsonschema:"Table name"`
	ID         string         `json:"id,omitempty" jsonschema:"Optional document ID (auto-generated if not provided)"`
	Content    string         `json:"content,omitempty" jsonschema:"Document text content"`
	Attributes map[string]any `json:"attributes,omitempty" jsonschema:"Document attributes as key-value pairs"`
}

type updateDocumentInput struct {
	Table      string         `json:"table" jsonschema:"Table name"`
	ID         string         `json:"id" jsonschema:"Document ID to update"`
	Content    string         `json:"content,omitempty" jsonschema:"New document text content"`
	Attributes map[string]any `json:"attributes,omitempty" jsonschema:"New document attributes"`
}

type deleteDocumentInput struct {
	Table string `json:"table" jsonschema:"Table name"`
	ID    string `json:"id" jsonschema:"Document ID to delete"`
}

type listDocumentsInput struct {
	Table string `json:"table" jsonschema:"Table name"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 20)"`
	After string `json:"after,omitempty" jsonschema:"Cursor: document ID to start after for pagination"`
}

type searchTextInput struct {
	Table string `json:"table" jsonschema:"Table name"`
	Query string `json:"query" jsonschema:"Full-text search query"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 10)"`
}

type searchAttributesInput struct {
	Table   string         `json:"table" jsonschema:"Table name"`
	Filters []filterInput  `json:"filters" jsonschema:"List of attribute filters"`
	Limit   int            `json:"limit,omitempty" jsonschema:"Maximum number of results (default 10)"`
}

type filterInput struct {
	Field string `json:"field" jsonschema:"Attribute field name"`
	Op    string `json:"op" jsonschema:"Filter operator: eq, neq, gt, gte, lt, lte, contains, prefix"`
	Value any    `json:"value" jsonschema:"Value to compare against"`
}

// --- Tool handlers ---

func (h *dbHandler) listTables(ctx context.Context, _ *mcp.CallToolRequest, _ listTablesInput) (*mcp.CallToolResult, any, error) {
	metas, err := h.registry.ListTables(ctx)
	if err != nil {
		return textError("Failed to list tables: " + err.Error()), nil, nil
	}

	if len(metas) == 0 {
		return textResult("No tables found."), nil, nil
	}

	var names []string
	for _, m := range metas {
		names = append(names, m.Name)
	}
	return textResult("Tables: " + strings.Join(names, ", ")), nil, nil
}

func (h *dbHandler) getTable(ctx context.Context, _ *mcp.CallToolRequest, input getTableInput) (*mcp.CallToolResult, any, error) {
	db, err := h.registry.GetTable(ctx, input.Name)
	if err != nil {
		return textError("Table not found: " + input.Name), nil, nil
	}

	result := map[string]any{"name": db.Name}
	if db.Schema != nil {
		result["schema"] = db.Schema
	}
	return jsonResult(result), nil, nil
}

func (h *dbHandler) createTable(ctx context.Context, _ *mcp.CallToolRequest, input createTableInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return textError("Table name is required."), nil, nil
	}

	db, err := h.registry.CreateTable(ctx, input.Name, input.Schema)
	if err != nil {
		return textError("Failed to create table: " + err.Error()), nil, nil
	}

	return textResult(fmt.Sprintf("Table %q created successfully.", db.Name)), nil, nil
}

func (h *dbHandler) getDocument(ctx context.Context, _ *mcp.CallToolRequest, input getDocumentInput) (*mcp.CallToolResult, any, error) {
	db, err := h.registry.GetTable(ctx, input.Table)
	if err != nil {
		return textError("Table not found: " + input.Table), nil, nil
	}

	doc, err := db.GetDocument(ctx, input.ID)
	if err != nil {
		return textError("Error getting document: " + err.Error()), nil, nil
	}
	if doc == nil {
		return textError("Document not found: " + input.ID), nil, nil
	}

	return jsonResult(doc), nil, nil
}

func (h *dbHandler) createDocument(ctx context.Context, _ *mcp.CallToolRequest, input createDocumentInput) (*mcp.CallToolResult, any, error) {
	db, err := h.registry.GetTable(ctx, input.Table)
	if err != nil {
		return textError("Table not found: " + input.Table), nil, nil
	}

	doc := &document.Document{
		ID:         input.ID,
		Content:    input.Content,
		Attributes: input.Attributes,
	}

	if err := db.PutDocument(ctx, doc); err != nil {
		return textError("Failed to create document: " + err.Error()), nil, nil
	}

	return jsonResult(map[string]any{
		"id":      doc.ID,
		"message": "Document created successfully.",
	}), nil, nil
}

func (h *dbHandler) updateDocument(ctx context.Context, _ *mcp.CallToolRequest, input updateDocumentInput) (*mcp.CallToolResult, any, error) {
	db, err := h.registry.GetTable(ctx, input.Table)
	if err != nil {
		return textError("Table not found: " + input.Table), nil, nil
	}

	doc := &document.Document{
		ID:         input.ID,
		Content:    input.Content,
		Attributes: input.Attributes,
	}

	if err := db.PutDocument(ctx, doc); err != nil {
		return textError("Failed to update document: " + err.Error()), nil, nil
	}

	return textResult(fmt.Sprintf("Document %q updated successfully.", input.ID)), nil, nil
}

func (h *dbHandler) deleteDocument(ctx context.Context, _ *mcp.CallToolRequest, input deleteDocumentInput) (*mcp.CallToolResult, any, error) {
	db, err := h.registry.GetTable(ctx, input.Table)
	if err != nil {
		return textError("Table not found: " + input.Table), nil, nil
	}

	if err := db.DeleteDocument(ctx, input.ID); err != nil {
		return textError("Failed to delete document: " + err.Error()), nil, nil
	}

	return textResult(fmt.Sprintf("Document %q deleted successfully.", input.ID)), nil, nil
}

func (h *dbHandler) listDocuments(ctx context.Context, _ *mcp.CallToolRequest, input listDocumentsInput) (*mcp.CallToolResult, any, error) {
	db, err := h.registry.GetTable(ctx, input.Table)
	if err != nil {
		return textError("Table not found: " + input.Table), nil, nil
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}

	docs, hasMore, err := db.ListDocuments(ctx, limit, input.After)
	if err != nil {
		return textError("Failed to list documents: " + err.Error()), nil, nil
	}

	result := map[string]any{
		"documents": docs,
		"has_more":  hasMore,
		"limit":     limit,
	}
	if len(docs) > 0 && hasMore {
		result["next_cursor"] = docs[len(docs)-1].ID
	}

	return jsonResult(result), nil, nil
}

func (h *dbHandler) searchText(ctx context.Context, _ *mcp.CallToolRequest, input searchTextInput) (*mcp.CallToolResult, any, error) {
	db, err := h.registry.GetTable(ctx, input.Table)
	if err != nil {
		return textError("Table not found: " + input.Table), nil, nil
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}

	results, total, err := db.SearchText(ctx, input.Query, limit, 0)
	if err != nil {
		return textError("Search failed: " + err.Error()), nil, nil
	}

	return jsonResult(map[string]any{
		"results": results,
		"total":   total,
	}), nil, nil
}

func (h *dbHandler) searchAttributes(ctx context.Context, _ *mcp.CallToolRequest, input searchAttributesInput) (*mcp.CallToolResult, any, error) {
	db, err := h.registry.GetTable(ctx, input.Table)
	if err != nil {
		return textError("Table not found: " + input.Table), nil, nil
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}

	filters := make([]index.Filter, len(input.Filters))
	for i, f := range input.Filters {
		filters[i] = index.Filter{
			Field: f.Field,
			Op:    index.FilterOp(f.Op),
			Value: f.Value,
		}
	}

	results, err := db.SearchAttributes(ctx, filters, limit, 0)
	if err != nil {
		return textError("Search failed: " + err.Error()), nil, nil
	}

	return jsonResult(results), nil, nil
}

type executeCodeInput struct {
	Code   string         `json:"code" jsonschema:"JavaScript source code to execute"`
	Params map[string]any `json:"params,omitempty" jsonschema:"Parameters available as 'params' in the code"`
}

type manageFunctionInput struct {
	Action      string `json:"action" jsonschema:"Action: create, update, get, list, or delete"`
	Name        string `json:"name,omitempty" jsonschema:"Function name (required for create, update, get, delete)"`
	Code        string `json:"code,omitempty" jsonschema:"JavaScript source code (required for create, update)"`
	Description string `json:"description,omitempty" jsonschema:"Function description"`
}

func (h *dbHandler) executeCode(ctx context.Context, _ *mcp.CallToolRequest, input executeCodeInput) (*mcp.CallToolResult, any, error) {
	if h.fnEngine == nil {
		return textError("JavaScript execution is not available."), nil, nil
	}

	result := h.fnEngine.Execute(ctx, input.Code, input.Params)
	return jsonResult(result), nil, nil
}

func (h *dbHandler) manageFunction(ctx context.Context, _ *mcp.CallToolRequest, input manageFunctionInput) (*mcp.CallToolResult, any, error) {
	if h.fnStore == nil {
		return textError("Function storage is not available."), nil, nil
	}

	switch input.Action {
	case "create":
		if input.Name == "" || input.Code == "" {
			return textError("name and code are required for create"), nil, nil
		}
		fn, err := h.fnStore.Create(ctx, input.Name, input.Description, input.Code, "")
		if err != nil {
			return textError("Failed to create function: " + err.Error()), nil, nil
		}
		return jsonResult(map[string]any{
			"id":   fn.ID,
			"name": fn.Name,
			"message": "Function created successfully.",
		}), nil, nil

	case "update":
		if input.Name == "" || input.Code == "" {
			return textError("name and code are required for update"), nil, nil
		}
		fn, err := h.fnStore.Update(ctx, input.Name, input.Code, input.Description)
		if err != nil {
			return textError("Failed to update function: " + err.Error()), nil, nil
		}
		return jsonResult(map[string]any{
			"id":   fn.ID,
			"name": fn.Name,
			"message": "Function updated successfully.",
		}), nil, nil

	case "get":
		if input.Name == "" {
			return textError("name is required for get"), nil, nil
		}
		fn, err := h.fnStore.Get(ctx, input.Name)
		if err != nil {
			return textError("Failed to get function: " + err.Error()), nil, nil
		}
		if fn == nil {
			return textError("Function not found: " + input.Name), nil, nil
		}
		return jsonResult(fn), nil, nil

	case "list":
		fns, err := h.fnStore.List(ctx)
		if err != nil {
			return textError("Failed to list functions: " + err.Error()), nil, nil
		}
		if len(fns) == 0 {
			return textResult("No stored functions found."), nil, nil
		}
		return jsonResult(fns), nil, nil

	case "delete":
		if input.Name == "" {
			return textError("name is required for delete"), nil, nil
		}
		if err := h.fnStore.Delete(ctx, input.Name); err != nil {
			return textError("Failed to delete function: " + err.Error()), nil, nil
		}
		return textResult(fmt.Sprintf("Function %q deleted successfully.", input.Name)), nil, nil

	default:
		return textError("Unknown action: " + input.Action + ". Use create, update, get, list, or delete."), nil, nil
	}
}

// --- Helpers ---

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func textError(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}
}

func jsonResult(v any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return textError("Failed to marshal result: " + err.Error())
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}
}

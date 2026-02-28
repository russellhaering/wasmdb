// Package agent provides the WasmDB agent integration, including MCP tools
// for table operations and chat session management.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	autobotagent "github.com/russellhaering/wasmdb/internal/autobot/agent"
	"github.com/russellhaering/wasmdb/internal/autobot/mcpx"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/functions"
	"github.com/russellhaering/wasmdb/internal/index"
	"github.com/russellhaering/wasmdb/internal/mcpservers"
	"github.com/russellhaering/wasmdb/internal/memory"
	"github.com/russellhaering/wasmdb/internal/skills"
)

// TableServerResult holds the MCP server and a function to set the server group
// (which is only available after Connect).
type TableServerResult struct {
	Server  *mcp.Server
	handler *dbHandler
}

// SetServerGroup wires the server group into the handler so search_tools works.
func (r *TableServerResult) SetServerGroup(sg *mcpx.ServerGroup) {
	r.handler.serverGroup = sg
}

// NewTableServer creates an MCP server exposing wasmdb table operations as tools.
func NewTableServer(registry *database.Registry, fnEngine *functions.Engine, fnStore *functions.Store, skillStore *skills.Store, memoryStore *memory.Store, subAgentModel, anthropicAPIKey string, mcpServerStore ...*mcpservers.Store) *TableServerResult {
	var mcpStore *mcpservers.Store
	if len(mcpServerStore) > 0 {
		mcpStore = mcpServerStore[0]
	}
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "wasmdb-table",
		Version: "v0.1.0",
	}, nil)

	h := &dbHandler{registry: registry, fnEngine: fnEngine, fnStore: fnStore, skillStore: skillStore, memoryStore: memoryStore, mcpServerStore: mcpStore, subAgentModel: subAgentModel, anthropicAPIKey: anthropicAPIKey}

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

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_skills_catalog",
		Description: "List a compact skills catalog for discovery (name, description, function_name, manual_only). Use this first for progressive disclosure.",
	}, h.listSkillsCatalog)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_skill_detail",
		Description: "Get full detail for a single skill by name. Use only after selecting a candidate from list_skills_catalog.",
	}, h.getSkillDetail)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "manage_skill",
		Description: "Create, update, get, list, delete, or execute skills. Skills map a stable capability name to a stored function.",
	}, h.manageSkill)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_memory_catalog",
		Description: "List compact memory entries for the current user (progressive disclosure).",
	}, h.listMemoryCatalog)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_memory_detail",
		Description: "Get full detail for a memory by ID and mark it used.",
	}, h.getMemoryDetail)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "manage_memory",
		Description: "Create, update, delete, or pin memories. Use this to persist important user preferences and context across sessions.",
	}, h.manageMemory)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "manage_mcp_server",
		Description: "Register, update, get, list, or delete external MCP server integrations. These servers provide additional tools to the agent.",
	}, h.manageMCPServer)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_tools",
		Description: "Search for tools across all connected MCP servers (both built-in and external). Use to discover available tools by keyword.",
	}, h.searchTools)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "delegate_subagent",
		Description: "Delegate a focused sub-task to a one-level-deep sub-agent with optional model override. " +
			"Use this for research/summarization/planning when isolating context helps. " +
			"Input: {task, model?, max_turns?}. Model may be an alias like sonnet/opus/haiku.",
	}, h.delegateSubagent)

	return &TableServerResult{Server: srv, handler: h}
}

type dbHandler struct {
	registry        *database.Registry
	fnEngine        *functions.Engine
	fnStore         *functions.Store
	skillStore      *skills.Store
	memoryStore     *memory.Store
	mcpServerStore  *mcpservers.Store
	subAgentModel   string
	anthropicAPIKey string
	// serverGroup is set by the chat manager so tools can query it.
	serverGroup     *mcpx.ServerGroup
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
	Table   string        `json:"table" jsonschema:"Table name"`
	Filters []filterInput `json:"filters" jsonschema:"List of attribute filters"`
	Limit   int           `json:"limit,omitempty" jsonschema:"Maximum number of results (default 10)"`
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

type manageSkillInput struct {
	Action                 string         `json:"action" jsonschema:"Action: create, update, get, list, delete, or exec"`
	Name                   string         `json:"name,omitempty" jsonschema:"Skill name (required for create, update, get, delete, exec)"`
	Description            string         `json:"description,omitempty" jsonschema:"Skill description"`
	FunctionName           string         `json:"function_name,omitempty" jsonschema:"Linked stored function name (required for create, update)"`
	DisableModelInvocation bool           `json:"disable_model_invocation,omitempty" jsonschema:"If true, model should not auto-invoke this skill"`
	Params                 map[string]any `json:"params,omitempty" jsonschema:"Params passed when action=exec"`
}

type listSkillsCatalogInput struct{}

type getSkillDetailInput struct {
	Name string `json:"name" jsonschema:"Skill name"`
}

type listMemoryCatalogInput struct {
	UserID string `json:"user_id" jsonschema:"Authenticated user ID"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum memories to return (default 25)"`
}

type getMemoryDetailInput struct {
	UserID string `json:"user_id" jsonschema:"Authenticated user ID"`
	ID     string `json:"id" jsonschema:"Memory ID"`
}

type manageMemoryInput struct {
	UserID  string   `json:"user_id" jsonschema:"Authenticated user ID"`
	Action  string   `json:"action" jsonschema:"Action: create, update, delete, or pin"`
	ID      string   `json:"id,omitempty" jsonschema:"Memory ID (required for update/delete/pin)"`
	Title   string   `json:"title,omitempty" jsonschema:"Memory title"`
	Summary string   `json:"summary,omitempty" jsonschema:"Memory summary text"`
	Scope   string   `json:"scope,omitempty" jsonschema:"Memory scope: user or session"`
	Tags    []string `json:"tags,omitempty" jsonschema:"Optional tags"`
	Pinned  bool     `json:"pinned,omitempty" jsonschema:"Pinned flag for create/update/pin"`
}

type manageMCPServerInput struct {
	Action      string            `json:"action" jsonschema:"Action: register, update, get, list, or delete"`
	Name        string            `json:"name,omitempty" jsonschema:"Server name (required for register, update, get, delete)"`
	Description string            `json:"description,omitempty" jsonschema:"Server description"`
	Transport   string            `json:"transport,omitempty" jsonschema:"Transport type: streamable-http or stdio (required for register, update)"`
	URL         string            `json:"url,omitempty" jsonschema:"Server URL (for streamable-http transport)"`
	Command     string            `json:"command,omitempty" jsonschema:"Command to run (for stdio transport)"`
	Args        []string          `json:"args,omitempty" jsonschema:"Command arguments (for stdio transport)"`
	Env         []string          `json:"env,omitempty" jsonschema:"Environment variables KEY=VALUE (for stdio transport)"`
	Headers     map[string]string `json:"headers,omitempty" jsonschema:"HTTP headers (for streamable-http transport)"`
	Enabled     *bool             `json:"enabled,omitempty" jsonschema:"Whether the server is enabled (default true)"`
}

type searchToolsInput struct {
	Query string `json:"query,omitempty" jsonschema:"Search query to filter tools by name or description. Empty returns all."`
}

type delegateSubagentInput struct {
	Task     string `json:"task" jsonschema:"Task for the sub-agent to execute"`
	Model    string `json:"model,omitempty" jsonschema:"Optional model override for sub-agent (defaults to WASMDB_SUBAGENT_MODEL or parent model)"`
	MaxTurns int    `json:"max_turns,omitempty" jsonschema:"Optional max tool-use turns for sub-agent (default 8, max 20)"`
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
			"id":      fn.ID,
			"name":    fn.Name,
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
			"id":      fn.ID,
			"name":    fn.Name,
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

func (h *dbHandler) manageSkill(ctx context.Context, _ *mcp.CallToolRequest, input manageSkillInput) (*mcp.CallToolResult, any, error) {
	if h.skillStore == nil {
		return textError("Skill storage is not available."), nil, nil
	}

	switch input.Action {
	case "create":
		if input.Name == "" || input.FunctionName == "" {
			return textError("name and function_name are required for create"), nil, nil
		}
		sk, err := h.skillStore.Create(ctx, input.Name, input.Description, input.FunctionName, "", input.DisableModelInvocation)
		if err != nil {
			return textError("Failed to create skill: " + err.Error()), nil, nil
		}
		return jsonResult(map[string]any{
			"id":      sk.ID,
			"name":    sk.Name,
			"message": "Skill created successfully.",
		}), nil, nil

	case "update":
		if input.Name == "" || input.FunctionName == "" {
			return textError("name and function_name are required for update"), nil, nil
		}
		sk, err := h.skillStore.Update(ctx, input.Name, input.Description, input.FunctionName, input.DisableModelInvocation)
		if err != nil {
			return textError("Failed to update skill: " + err.Error()), nil, nil
		}
		return jsonResult(map[string]any{
			"id":      sk.ID,
			"name":    sk.Name,
			"message": "Skill updated successfully.",
		}), nil, nil

	case "get":
		if input.Name == "" {
			return textError("name is required for get"), nil, nil
		}
		sk, err := h.skillStore.Get(ctx, input.Name)
		if err != nil {
			return textError("Failed to get skill: " + err.Error()), nil, nil
		}
		if sk == nil {
			return textError("Skill not found: " + input.Name), nil, nil
		}
		return jsonResult(sk), nil, nil

	case "list":
		skills, err := h.skillStore.List(ctx)
		if err != nil {
			return textError("Failed to list skills: " + err.Error()), nil, nil
		}
		if len(skills) == 0 {
			return textResult("No stored skills found."), nil, nil
		}
		return jsonResult(skills), nil, nil

	case "delete":
		if input.Name == "" {
			return textError("name is required for delete"), nil, nil
		}
		if err := h.skillStore.Delete(ctx, input.Name); err != nil {
			return textError("Failed to delete skill: " + err.Error()), nil, nil
		}
		return textResult(fmt.Sprintf("Skill %q deleted successfully.", input.Name)), nil, nil

	case "exec":
		if input.Name == "" {
			return textError("name is required for exec"), nil, nil
		}
		sk, err := h.skillStore.Get(ctx, input.Name)
		if err != nil {
			return textError("Failed to load skill: " + err.Error()), nil, nil
		}
		if sk == nil {
			return textError("Skill not found: " + input.Name), nil, nil
		}
		res, err := h.skillStore.Execute(ctx, input.Name, input.Params)
		if err != nil {
			return textError("Failed to execute skill: " + err.Error()), nil, nil
		}
		return jsonResult(map[string]any{
			"skill": map[string]any{
				"name":                     sk.Name,
				"disable_model_invocation": sk.DisableModelInvocation,
			},
			"result": res,
		}), nil, nil

	default:
		return textError("Unknown action: " + input.Action + ". Use create, update, get, list, delete, or exec."), nil, nil
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

func (h *dbHandler) listMemoryCatalog(ctx context.Context, _ *mcp.CallToolRequest, input listMemoryCatalogInput) (*mcp.CallToolResult, any, error) {
	if h.memoryStore == nil {
		return textError("Memory storage is not available."), nil, nil
	}
	if input.UserID == "" {
		return textError("user_id is required"), nil, nil
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 25
	}
	entries, err := h.memoryStore.ListCatalog(ctx, input.UserID, limit)
	if err != nil {
		return textError("Failed to list memory catalog: " + err.Error()), nil, nil
	}
	if len(entries) == 0 {
		return textResult("No memories found."), nil, nil
	}
	return jsonResult(entries), nil, nil
}

func (h *dbHandler) getMemoryDetail(ctx context.Context, _ *mcp.CallToolRequest, input getMemoryDetailInput) (*mcp.CallToolResult, any, error) {
	if h.memoryStore == nil {
		return textError("Memory storage is not available."), nil, nil
	}
	if input.UserID == "" || input.ID == "" {
		return textError("user_id and id are required"), nil, nil
	}
	m, err := h.memoryStore.Get(ctx, input.ID)
	if err != nil {
		return textError("Failed to get memory: " + err.Error()), nil, nil
	}
	if m == nil || m.UserID != input.UserID {
		return textError("Memory not found: " + input.ID), nil, nil
	}
	_ = h.memoryStore.Touch(ctx, m.ID)
	return jsonResult(m), nil, nil
}

func (h *dbHandler) manageMemory(ctx context.Context, _ *mcp.CallToolRequest, input manageMemoryInput) (*mcp.CallToolResult, any, error) {
	if h.memoryStore == nil {
		return textError("Memory storage is not available."), nil, nil
	}
	if input.UserID == "" {
		return textError("user_id is required"), nil, nil
	}

	switch input.Action {
	case "create":
		m, err := h.memoryStore.Create(ctx, &memory.Memory{
			UserID:  input.UserID,
			Scope:   input.Scope,
			Title:   input.Title,
			Summary: input.Summary,
			Tags:    input.Tags,
			Pinned:  input.Pinned,
		})
		if err != nil {
			return textError("Failed to create memory: " + err.Error()), nil, nil
		}
		return jsonResult(m), nil, nil
	case "update":
		if input.ID == "" {
			return textError("id is required for update"), nil, nil
		}
		cur, err := h.memoryStore.Get(ctx, input.ID)
		if err != nil {
			return textError("Failed to get memory: " + err.Error()), nil, nil
		}
		if cur == nil || cur.UserID != input.UserID {
			return textError("Memory not found: " + input.ID), nil, nil
		}
		m, err := h.memoryStore.Update(ctx, input.ID, &memory.Memory{
			Scope:   input.Scope,
			Title:   input.Title,
			Summary: input.Summary,
			Tags:    input.Tags,
			Pinned:  input.Pinned,
		})
		if err != nil {
			return textError("Failed to update memory: " + err.Error()), nil, nil
		}
		return jsonResult(m), nil, nil
	case "delete":
		if input.ID == "" {
			return textError("id is required for delete"), nil, nil
		}
		cur, err := h.memoryStore.Get(ctx, input.ID)
		if err != nil {
			return textError("Failed to get memory: " + err.Error()), nil, nil
		}
		if cur == nil || cur.UserID != input.UserID {
			return textError("Memory not found: " + input.ID), nil, nil
		}
		if err := h.memoryStore.Delete(ctx, input.ID); err != nil {
			return textError("Failed to delete memory: " + err.Error()), nil, nil
		}
		return textResult(fmt.Sprintf("Memory %q deleted successfully.", input.ID)), nil, nil
	case "pin":
		if input.ID == "" {
			return textError("id is required for pin"), nil, nil
		}
		cur, err := h.memoryStore.Get(ctx, input.ID)
		if err != nil {
			return textError("Failed to get memory: " + err.Error()), nil, nil
		}
		if cur == nil || cur.UserID != input.UserID {
			return textError("Memory not found: " + input.ID), nil, nil
		}
		m, err := h.memoryStore.Update(ctx, input.ID, &memory.Memory{Pinned: input.Pinned})
		if err != nil {
			return textError("Failed to pin memory: " + err.Error()), nil, nil
		}
		return jsonResult(m), nil, nil
	default:
		return textError("Unknown action: " + input.Action + ". Use create, update, delete, or pin."), nil, nil
	}
}

func (h *dbHandler) manageMCPServer(ctx context.Context, _ *mcp.CallToolRequest, input manageMCPServerInput) (*mcp.CallToolResult, any, error) {
	if h.mcpServerStore == nil {
		return textError("MCP server management is not available."), nil, nil
	}

	switch input.Action {
	case "register":
		if input.Name == "" || input.Transport == "" {
			return textError("name and transport are required for register"), nil, nil
		}
		enabled := true
		if input.Enabled != nil {
			enabled = *input.Enabled
		}
		srv, err := h.mcpServerStore.Create(ctx, input.Name, input.Description, input.Transport, input.URL, input.Command, input.Args, input.Env, input.Headers, enabled, "")
		if err != nil {
			return textError("Failed to register MCP server: " + err.Error()), nil, nil
		}
		return jsonResult(map[string]any{
			"id":      srv.ID,
			"name":    srv.Name,
			"message": "MCP server registered successfully. It will be connected on the next chat session.",
		}), nil, nil

	case "update":
		if input.Name == "" || input.Transport == "" {
			return textError("name and transport are required for update"), nil, nil
		}
		enabled := true
		if input.Enabled != nil {
			enabled = *input.Enabled
		}
		srv, err := h.mcpServerStore.Update(ctx, input.Name, input.Description, input.Transport, input.URL, input.Command, input.Args, input.Env, input.Headers, enabled)
		if err != nil {
			return textError("Failed to update MCP server: " + err.Error()), nil, nil
		}
		return jsonResult(map[string]any{
			"id":      srv.ID,
			"name":    srv.Name,
			"message": "MCP server updated. Changes take effect on the next chat session.",
		}), nil, nil

	case "get":
		if input.Name == "" {
			return textError("name is required for get"), nil, nil
		}
		srv, err := h.mcpServerStore.Get(ctx, input.Name)
		if err != nil {
			return textError("Failed to get MCP server: " + err.Error()), nil, nil
		}
		if srv == nil {
			return textError("MCP server not found: " + input.Name), nil, nil
		}
		return jsonResult(srv), nil, nil

	case "list":
		servers, err := h.mcpServerStore.List(ctx)
		if err != nil {
			return textError("Failed to list MCP servers: " + err.Error()), nil, nil
		}
		if len(servers) == 0 {
			return textResult("No MCP servers registered."), nil, nil
		}
		return jsonResult(servers), nil, nil

	case "delete":
		if input.Name == "" {
			return textError("name is required for delete"), nil, nil
		}
		if err := h.mcpServerStore.Delete(ctx, input.Name); err != nil {
			return textError("Failed to delete MCP server: " + err.Error()), nil, nil
		}
		return textResult(fmt.Sprintf("MCP server %q deleted successfully.", input.Name)), nil, nil

	default:
		return textError("Unknown action: " + input.Action + ". Use register, update, get, list, or delete."), nil, nil
	}
}

func (h *dbHandler) searchTools(ctx context.Context, _ *mcp.CallToolRequest, input searchToolsInput) (*mcp.CallToolResult, any, error) {
	if h.serverGroup == nil {
		return textError("Tool search is not available."), nil, nil
	}

	results, err := searchToolsInGroup(ctx, h.serverGroup, input.Query)
	if err != nil {
		return textError("Search failed: " + err.Error()), nil, nil
	}

	if len(results) == 0 {
		return textResult("No tools found matching query."), nil, nil
	}

	return jsonResult(results), nil, nil
}

func (h *dbHandler) delegateSubagent(ctx context.Context, req *mcp.CallToolRequest, input delegateSubagentInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(input.Task) == "" {
		return textError("task is required"), nil, nil
	}

	// One-level deep only: nesting is blocked by excluding delegate_subagent from sub-agent tools.
	_ = req

	defaultModel := strings.TrimSpace(h.subAgentModel)
	if defaultModel == "" {
		defaultModel = string(anthropic.ModelClaudeSonnet4_5_20250929)
	}

	requestedModel := normalizeModelAlias(input.Model)
	model := requestedModel
	if model == "" {
		model = defaultModel
	}

	maxTurns := input.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 8
	}
	if maxTurns > 20 {
		maxTurns = 20
	}

	tableResult := NewTableServer(h.registry, h.fnEngine, h.fnStore, h.skillStore, h.memoryStore, h.subAgentModel, h.anthropicAPIKey)
	sg := mcpxSingleServerGroup(tableResult.Server)
	defer sg.Close()
	if err := sg.Connect(ctx); err != nil {
		return textError("failed to start sub-agent tools: " + err.Error()), nil, nil
	}

	system := "You are a focused sub-agent. Complete the assigned task and return a concise result. " +
		"You may use tools as needed. Do not call delegate_subagent (nesting is not allowed)."

	runWithModel := func(m string) (*autobotagent.Result, error) {
		a := autobotagent.NewAgent(autobotagent.Config{
			APIKey:          h.anthropicAPIKey,
			Model:           m,
			SystemPrompt:    system,
			MaxTokens:       8192,
			MaxTurns:        maxTurns,
			DisallowedTools: map[string]bool{"delegate_subagent": true},
		}, sg)

		s, err := a.NewSession(ctx, input.Task)
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
		return s.Run(ctx)
	}

	res, err := runWithModel(model)
	fallbackUsed := ""
	fallbackReason := ""
	if err != nil && requestedModel != "" && model != defaultModel {
		res, err = runWithModel(defaultModel)
		if err == nil {
			fallbackUsed = defaultModel
			fallbackReason = "requested model failed; used default sub-agent model"
			model = defaultModel
		}
	}
	if err != nil {
		return textError("sub-agent execution failed: " + err.Error()), nil, nil
	}

	resultText := strings.TrimSpace(res.Text)
	payload := map[string]any{
		"ok":                  true,
		"model":               model,
		"summary":             resultText,
		"result":              resultText,
		"stop_reason":         res.StopReason,
		"total_input_tokens":  res.TotalInputTokens,
		"total_output_tokens": res.TotalOutputTokens,
	}
	if fallbackUsed != "" {
		payload["fallback_model"] = fallbackUsed
		payload["fallback_reason"] = fallbackReason
	}
	if requestedModel != "" {
		payload["requested_model"] = requestedModel
	}
	return jsonResult(payload), nil, nil
}

func mcpxSingleServerGroup(server *mcp.Server) *mcpx.ServerGroup {
	sg := mcpx.NewServerGroup()
	sg.AddServer("table", server)
	return sg
}

func normalizeModelAlias(model string) string {
	m := strings.TrimSpace(strings.ToLower(model))
	switch m {
	case "":
		return ""
	case "sonnet", "claude-sonnet", "sonnet-4.5", "claude-sonnet-4-5", "claude-sonnet-4.5":
		return string(anthropic.ModelClaudeSonnet4_5_20250929)
	case "sonnet-4.6", "claude-sonnet-4-6", "claude-sonnet-4.6":
		return string(anthropic.ModelClaudeSonnet4_6)
	case "opus", "claude-opus", "opus-4.5", "claude-opus-4-5", "claude-opus-4.5":
		return string(anthropic.ModelClaudeOpus4_5)
	case "opus-4.6", "claude-opus-4-6", "claude-opus-4.6":
		return string(anthropic.ModelClaudeOpus4_6)
	case "haiku", "claude-haiku", "haiku-4.5", "claude-haiku-4-5", "claude-haiku-4.5":
		return string(anthropic.ModelClaudeHaiku4_5)
	default:
		return strings.TrimSpace(model)
	}
}

func (h *dbHandler) listSkillsCatalog(ctx context.Context, _ *mcp.CallToolRequest, _ listSkillsCatalogInput) (*mcp.CallToolResult, any, error) {
	if h.skillStore == nil {
		return textError("Skill storage is not available."), nil, nil
	}

	skills, err := h.skillStore.List(ctx)
	if err != nil {
		return textError("Failed to list skills: " + err.Error()), nil, nil
	}
	if len(skills) == 0 {
		return textResult("No stored skills found."), nil, nil
	}

	type catalogSkill struct {
		Name                   string `json:"name"`
		Description            string `json:"description"`
		FunctionName           string `json:"function_name"`
		DisableModelInvocation bool   `json:"disable_model_invocation,omitempty"`
	}
	catalog := make([]catalogSkill, 0, len(skills))
	for _, sk := range skills {
		catalog = append(catalog, catalogSkill{
			Name:                   sk.Name,
			Description:            sk.Description,
			FunctionName:           sk.FunctionName,
			DisableModelInvocation: sk.DisableModelInvocation,
		})
	}
	return jsonResult(catalog), nil, nil
}

func (h *dbHandler) getSkillDetail(ctx context.Context, _ *mcp.CallToolRequest, input getSkillDetailInput) (*mcp.CallToolResult, any, error) {
	if h.skillStore == nil {
		return textError("Skill storage is not available."), nil, nil
	}
	if input.Name == "" {
		return textError("name is required"), nil, nil
	}

	sk, err := h.skillStore.Get(ctx, input.Name)
	if err != nil {
		return textError("Failed to get skill: " + err.Error()), nil, nil
	}
	if sk == nil {
		return textError("Skill not found: " + input.Name), nil, nil
	}
	return jsonResult(sk), nil, nil
}

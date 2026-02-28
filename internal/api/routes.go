package api

import "net/http"

// registerRoutes sets up all API routes using Go 1.22+ enhanced routing.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Table management.
	mux.HandleFunc("POST /v1/tables", s.handleCreateTable)
	mux.HandleFunc("GET /v1/tables", s.handleListTables)
	mux.HandleFunc("GET /v1/tables/{table}", s.handleGetTable)
	mux.HandleFunc("DELETE /v1/tables/{table}", s.handleDeleteTable)

	// Schema.
	mux.HandleFunc("GET /v1/tables/{table}/schema", s.handleGetSchema)
	mux.HandleFunc("PUT /v1/tables/{table}/schema", s.handleUpdateSchema)

	// Documents.
	mux.HandleFunc("GET /v1/tables/{table}/documents", s.handleListDocuments)
	mux.HandleFunc("POST /v1/tables/{table}/documents", s.handleCreateDocument)
	mux.HandleFunc("POST /v1/tables/{table}/documents/_bulk", s.handleBulkCreateDocuments)
	mux.HandleFunc("GET /v1/tables/{table}/documents/{id}", s.handleGetDocument)
	mux.HandleFunc("PUT /v1/tables/{table}/documents/{id}", s.handleUpdateDocument)
	mux.HandleFunc("DELETE /v1/tables/{table}/documents/{id}", s.handleDeleteDocument)

	// Search.
	mux.HandleFunc("POST /v1/tables/{table}/search/vector", s.handleVectorSearch)
	mux.HandleFunc("POST /v1/tables/{table}/search/text", s.handleTextSearch)
	mux.HandleFunc("POST /v1/tables/{table}/search/attributes", s.handleAttributeSearch)

	// Users.
	mux.HandleFunc("POST /v1/users", s.handleCreateUser)
	mux.HandleFunc("GET /v1/users", s.handleListUsers)
	mux.HandleFunc("GET /v1/users/{id}", s.handleGetUser)
	mux.HandleFunc("DELETE /v1/users/{id}", s.handleDeleteUser)

	// Auth.
	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /v1/auth/me", s.handleAuthMe)
	mux.HandleFunc("GET /auth/cli-login", s.handleCLILoginPage)
	mux.HandleFunc("POST /v1/auth/device-login", s.handleDeviceLoginStart)
	mux.HandleFunc("GET /v1/auth/device-login/poll", s.handleDeviceLoginPoll)
	mux.HandleFunc("POST /v1/auth/device-login/complete", s.handleDeviceLoginComplete)

	// Functions.
	mux.HandleFunc("POST /v1/functions", s.handleCreateFunction)
	mux.HandleFunc("GET /v1/functions", s.handleListFunctions)
	mux.HandleFunc("GET /v1/functions/{name}", s.handleGetFunction)
	mux.HandleFunc("PUT /v1/functions/{name}", s.handleUpdateFunction)
	mux.HandleFunc("DELETE /v1/functions/{name}", s.handleDeleteFunction)
	mux.HandleFunc("POST /v1/functions/{name}/exec", s.handleExecStored)
	mux.HandleFunc("POST /v1/exec", s.handleExecEphemeral)

	// Skills.
	mux.HandleFunc("POST /v1/skills", s.handleCreateSkill)
	mux.HandleFunc("GET /v1/skills", s.handleListSkills)
	mux.HandleFunc("GET /v1/skills/{name}", s.handleGetSkill)
	mux.HandleFunc("PUT /v1/skills/{name}", s.handleUpdateSkill)
	mux.HandleFunc("DELETE /v1/skills/{name}", s.handleDeleteSkill)
	mux.HandleFunc("POST /v1/skills/{name}/exec", s.handleExecSkill)

	// GraphQL.
	mux.Handle("POST /v1/graphql", s.graphql)

	// Chat.
	mux.HandleFunc("GET /chat", s.handleChatUI)
	mux.HandleFunc("POST /v1/chat", s.handleChatStream)
	mux.HandleFunc("GET /v1/chat/sessions", s.handleListChatSessions)
	mux.HandleFunc("DELETE /v1/chat/sessions/{id}", s.handleDeleteChatSession)

	// Memories.
	mux.HandleFunc("POST /v1/memories", s.handleCreateMemory)
	mux.HandleFunc("GET /v1/memories", s.handleListMemories)
	mux.HandleFunc("GET /v1/memories/{id}", s.handleGetMemory)
	mux.HandleFunc("PUT /v1/memories/{id}", s.handleUpdateMemory)
	mux.HandleFunc("DELETE /v1/memories/{id}", s.handleDeleteMemory)
	mux.HandleFunc("GET /v1/memories/catalog", s.handleMemoryCatalog)

	// MCP Servers.
	mux.HandleFunc("POST /v1/mcp-servers", s.handleCreateMCPServer)
	mux.HandleFunc("GET /v1/mcp-servers", s.handleListMCPServers)
	mux.HandleFunc("GET /v1/mcp-servers/{name}", s.handleGetMCPServer)
	mux.HandleFunc("PUT /v1/mcp-servers/{name}", s.handleUpdateMCPServer)
	mux.HandleFunc("DELETE /v1/mcp-servers/{name}", s.handleDeleteMCPServer)

	// Health.
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
}

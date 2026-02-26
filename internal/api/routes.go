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

	// GraphQL.
	mux.Handle("POST /v1/graphql", s.graphql)

	// Chat.
	mux.HandleFunc("GET /chat", s.handleChatUI)
	mux.HandleFunc("POST /v1/chat", s.handleChatStream)

	// Health.
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
}

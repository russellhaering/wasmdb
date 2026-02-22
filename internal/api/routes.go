package api

import "net/http"

// registerRoutes sets up all API routes using Go 1.22+ enhanced routing.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Database management.
	mux.HandleFunc("POST /v1/databases", s.handleCreateDatabase)
	mux.HandleFunc("GET /v1/databases", s.handleListDatabases)
	mux.HandleFunc("GET /v1/databases/{db}", s.handleGetDatabase)
	mux.HandleFunc("DELETE /v1/databases/{db}", s.handleDeleteDatabase)

	// Schema.
	mux.HandleFunc("GET /v1/databases/{db}/schema", s.handleGetSchema)
	mux.HandleFunc("PUT /v1/databases/{db}/schema", s.handleUpdateSchema)

	// Documents.
	mux.HandleFunc("POST /v1/databases/{db}/documents", s.handleCreateDocument)
	mux.HandleFunc("GET /v1/databases/{db}/documents/{id}", s.handleGetDocument)
	mux.HandleFunc("PUT /v1/databases/{db}/documents/{id}", s.handleUpdateDocument)
	mux.HandleFunc("DELETE /v1/databases/{db}/documents/{id}", s.handleDeleteDocument)

	// Search.
	mux.HandleFunc("POST /v1/databases/{db}/search/vector", s.handleVectorSearch)
	mux.HandleFunc("POST /v1/databases/{db}/search/text", s.handleTextSearch)
	mux.HandleFunc("POST /v1/databases/{db}/search/attributes", s.handleAttributeSearch)

	// Health.
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
}

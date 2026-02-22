package api

import (
	"net/http"
	"strings"

	"github.com/russellhaering/wasmdb/internal/document"
)

type createDatabaseRequest struct {
	Name   string           `json:"name"`
	Schema *document.Schema `json:"schema,omitempty"`
}

type databaseResponse struct {
	Name   string           `json:"name"`
	Schema *document.Schema `json:"schema,omitempty"`
}

func (s *Server) handleCreateDatabase(w http.ResponseWriter, r *http.Request) {
	var req createDatabaseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	if req.Name == "" {
		writeErrorMsg(w, 400, "bad_request", "name is required")
		return
	}

	db, err := s.registry.CreateDatabase(r.Context(), req.Name, req.Schema)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeErrorMsg(w, 409, "conflict", err.Error())
			return
		}
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	writeJSON(w, 201, databaseResponse{
		Name:   db.Name,
		Schema: db.Schema,
	})
}

func (s *Server) handleListDatabases(w http.ResponseWriter, r *http.Request) {
	metas, err := s.registry.ListDatabases(r.Context())
	if err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	type item struct {
		Name string `json:"name"`
	}
	result := make([]item, len(metas))
	for i, m := range metas {
		result[i] = item{Name: m.Name}
	}
	writeJSON(w, 200, result)
}

func (s *Server) handleGetDatabase(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")

	db, err := s.registry.GetDatabase(r.Context(), dbName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "database not found: "+dbName)
		return
	}

	writeJSON(w, 200, databaseResponse{
		Name:   db.Name,
		Schema: db.Schema,
	})
}

func (s *Server) handleDeleteDatabase(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")

	if err := s.registry.DeleteDatabase(r.Context(), dbName); err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	w.WriteHeader(204)
}

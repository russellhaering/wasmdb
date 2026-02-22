package api

import (
	"net/http"

	"github.com/russellhaering/wasmdb/internal/document"
)

func (s *Server) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")

	db, err := s.registry.GetDatabase(r.Context(), dbName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "database not found: "+dbName)
		return
	}

	if db.Schema == nil {
		writeJSON(w, 200, &document.Schema{})
		return
	}

	writeJSON(w, 200, db.Schema)
}

func (s *Server) handleUpdateSchema(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")

	var schema document.Schema
	if err := decodeJSON(r, &schema); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	if err := s.registry.UpdateSchema(r.Context(), dbName, &schema); err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	writeJSON(w, 200, &schema)
}

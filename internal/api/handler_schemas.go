package api

import (
	"net/http"

	"github.com/russellhaering/moraine/document"
)

func (s *Server) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")

	table, err := s.registry.GetTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}

	if table.Schema() == nil {
		writeJSON(w, 200, &document.Schema{})
		return
	}

	writeJSON(w, 200, table.Schema())
}

func (s *Server) handleUpdateSchema(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")

	isSystem, err := s.registry.IsSystemTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}
	if isSystem {
		writeErrorMsg(w, 403, "forbidden", "cannot modify schema of system table")
		return
	}

	var schema document.Schema
	if err := decodeJSON(r, &schema); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	if err := s.registry.UpdateSchema(r.Context(), tableName, &schema); err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	writeJSON(w, 200, &schema)
}

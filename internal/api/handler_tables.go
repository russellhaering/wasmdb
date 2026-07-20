package api

import (
	"net/http"
	"strings"

	"github.com/russellhaering/moraine/document"
)

type createTableRequest struct {
	Name   string           `json:"name"`
	Schema *document.Schema `json:"schema,omitempty"`
}

type tableResponse struct {
	Name   string           `json:"name"`
	Schema *document.Schema `json:"schema,omitempty"`
	System bool             `json:"system"`
}

func (s *Server) handleCreateTable(w http.ResponseWriter, r *http.Request) {
	var req createTableRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	if req.Name == "" {
		writeErrorMsg(w, 400, "bad_request", "name is required")
		return
	}

	table, err := s.registry.CreateTable(r.Context(), req.Name, req.Schema)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeErrorMsg(w, 409, "conflict", err.Error())
			return
		}
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	writeJSON(w, 201, tableResponse{
		Name:   table.Name(),
		Schema: table.Schema(),
		System: table.System(),
	})
}

func (s *Server) handleListTables(w http.ResponseWriter, r *http.Request) {
	metas, err := s.registry.ListTables(r.Context())
	if err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	type item struct {
		Name   string `json:"name"`
		System bool   `json:"system"`
	}
	result := make([]item, len(metas))
	for i, m := range metas {
		result[i] = item{Name: m.Name, System: m.System}
	}
	writeJSON(w, 200, result)
}

func (s *Server) handleGetTable(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")

	table, err := s.registry.GetTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}

	writeJSON(w, 200, tableResponse{
		Name:   table.Name(),
		Schema: table.Schema(),
		System: table.System(),
	})
}

func (s *Server) handleDeleteTable(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")

	isSystem, err := s.registry.IsSystemTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}
	if isSystem {
		writeErrorMsg(w, 403, "forbidden", "cannot delete system table")
		return
	}

	if err := s.registry.DeleteTable(r.Context(), tableName); err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	w.WriteHeader(204)
}

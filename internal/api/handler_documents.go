package api

import (
	"net/http"
	"strconv"

	"github.com/russellhaering/moraine/document"
)

type listDocumentsResponse struct {
	Documents []*document.Document `json:"documents"`
	HasMore   bool                 `json:"has_more"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")

	if isSystem, _ := s.registry.IsSystemTable(r.Context(), tableName); isSystem {
		writeErrorMsg(w, 403, "forbidden", "direct document access to system tables is not allowed")
		return
	}

	table, err := s.registry.GetTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	after := r.URL.Query().Get("after")

	docs, hasMore, err := table.ListDocuments(r.Context(), limit, after)
	if err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	resp := listDocumentsResponse{
		Documents: docs,
		HasMore:   hasMore,
	}
	if resp.Documents == nil {
		resp.Documents = []*document.Document{}
	}
	if len(docs) > 0 && hasMore {
		resp.NextCursor = docs[len(docs)-1].ID
	}

	writeJSON(w, 200, resp)
}

type createDocumentRequest struct {
	ID         string         `json:"id,omitempty"`
	Content    string         `json:"content,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

func (s *Server) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")

	if isSystem, _ := s.registry.IsSystemTable(r.Context(), tableName); isSystem {
		writeErrorMsg(w, 403, "forbidden", "direct document access to system tables is not allowed")
		return
	}

	table, err := s.registry.GetTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}

	var req createDocumentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	doc := &document.Document{
		ID:         req.ID,
		Content:    req.Content,
		Attributes: req.Attributes,
	}

	if err := table.PutDocument(r.Context(), doc); err != nil {
		writeErrorMsg(w, 400, "bad_request", err.Error())
		return
	}

	writeJSON(w, 201, doc)
}

func (s *Server) handleBulkCreateDocuments(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")

	if isSystem, _ := s.registry.IsSystemTable(r.Context(), tableName); isSystem {
		writeErrorMsg(w, 403, "forbidden", "direct document access to system tables is not allowed")
		return
	}

	table, err := s.registry.GetTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}

	var docs []*document.Document
	if err := decodeJSON(r, &docs); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	if len(docs) == 0 {
		writeJSON(w, 200, map[string]any{"count": 0})
		return
	}

	if err := table.PutDocumentsBulk(r.Context(), docs); err != nil {
		writeErrorMsg(w, 400, "bad_request", err.Error())
		return
	}

	writeJSON(w, 201, map[string]any{"count": len(docs)})
}

func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")
	docID := r.PathValue("id")

	if isSystem, _ := s.registry.IsSystemTable(r.Context(), tableName); isSystem {
		writeErrorMsg(w, 403, "forbidden", "direct document access to system tables is not allowed")
		return
	}

	table, err := s.registry.GetTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}

	doc, err := table.GetDocument(r.Context(), docID)
	if err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}
	if doc == nil {
		writeErrorMsg(w, 404, "not_found", "document not found: "+docID)
		return
	}

	writeJSON(w, 200, doc)
}

func (s *Server) handleUpdateDocument(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")
	docID := r.PathValue("id")

	if isSystem, _ := s.registry.IsSystemTable(r.Context(), tableName); isSystem {
		writeErrorMsg(w, 403, "forbidden", "direct document access to system tables is not allowed")
		return
	}

	table, err := s.registry.GetTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}

	var req createDocumentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	doc := &document.Document{
		ID:         docID,
		Content:    req.Content,
		Attributes: req.Attributes,
	}

	if err := table.PutDocument(r.Context(), doc); err != nil {
		writeErrorMsg(w, 400, "bad_request", err.Error())
		return
	}

	writeJSON(w, 200, doc)
}

func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")
	docID := r.PathValue("id")

	if isSystem, _ := s.registry.IsSystemTable(r.Context(), tableName); isSystem {
		writeErrorMsg(w, 403, "forbidden", "direct document access to system tables is not allowed")
		return
	}

	table, err := s.registry.GetTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}

	if err := table.DeleteDocument(r.Context(), docID); err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	w.WriteHeader(204)
}

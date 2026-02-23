package api

import (
	"net/http"

	"github.com/russellhaering/wasmdb/internal/document"
)

type createDocumentRequest struct {
	ID         string         `json:"id,omitempty"`
	Content    string         `json:"content,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

func (s *Server) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")

	db, err := s.registry.GetDatabase(r.Context(), dbName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "database not found: "+dbName)
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

	if err := db.PutDocument(r.Context(), doc); err != nil {
		writeErrorMsg(w, 400, "bad_request", err.Error())
		return
	}

	writeJSON(w, 201, doc)
}

func (s *Server) handleBulkCreateDocuments(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")

	db, err := s.registry.GetDatabase(r.Context(), dbName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "database not found: "+dbName)
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

	if err := db.PutDocumentsBulk(r.Context(), docs); err != nil {
		writeErrorMsg(w, 400, "bad_request", err.Error())
		return
	}

	writeJSON(w, 201, map[string]any{"count": len(docs)})
}

func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	docID := r.PathValue("id")

	db, err := s.registry.GetDatabase(r.Context(), dbName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "database not found: "+dbName)
		return
	}

	doc, err := db.GetDocument(r.Context(), docID)
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
	dbName := r.PathValue("db")
	docID := r.PathValue("id")

	db, err := s.registry.GetDatabase(r.Context(), dbName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "database not found: "+dbName)
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

	if err := db.PutDocument(r.Context(), doc); err != nil {
		writeErrorMsg(w, 400, "bad_request", err.Error())
		return
	}

	writeJSON(w, 200, doc)
}

func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	docID := r.PathValue("id")

	db, err := s.registry.GetDatabase(r.Context(), dbName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "database not found: "+dbName)
		return
	}

	if err := db.DeleteDocument(r.Context(), docID); err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	w.WriteHeader(204)
}

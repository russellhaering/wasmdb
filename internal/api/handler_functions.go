package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// handleCreateFunction handles POST /v1/functions.
func (s *Server) handleCreateFunction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Code        string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}
	if req.Name == "" || req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and code are required"})
		return
	}

	session := SessionFromContext(r.Context())
	userID := ""
	if session != nil {
		userID = session.UserID
	}

	fn, err := s.fnStore.Create(r.Context(), req.Name, req.Description, req.Code, userID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         fn.ID,
		"name":       fn.Name,
		"created_at": fn.CreatedAt.Format(time.RFC3339),
	})
}

// handleListFunctions handles GET /v1/functions.
func (s *Server) handleListFunctions(w http.ResponseWriter, r *http.Request) {
	fns, err := s.fnStore.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type fnSummary struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		UpdatedAt   string `json:"updated_at"`
	}
	result := make([]fnSummary, 0, len(fns))
	for _, fn := range fns {
		result = append(result, fnSummary{
			ID:          fn.ID,
			Name:        fn.Name,
			Description: fn.Description,
			UpdatedAt:   fn.UpdatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// handleGetFunction handles GET /v1/functions/{name}.
func (s *Server) handleGetFunction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	fn, err := s.fnStore.Get(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if fn == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "function not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          fn.ID,
		"name":        fn.Name,
		"description": fn.Description,
		"code":        fn.Code,
		"created_by":  fn.CreatedBy,
		"created_at":  fn.CreatedAt.Format(time.RFC3339),
		"updated_at":  fn.UpdatedAt.Format(time.RFC3339),
	})
}

// handleUpdateFunction handles PUT /v1/functions/{name}.
func (s *Server) handleUpdateFunction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req struct {
		Code        string `json:"code"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}
	if req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code is required"})
		return
	}

	fn, err := s.fnStore.Update(r.Context(), name, req.Code, req.Description)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         fn.ID,
		"name":       fn.Name,
		"updated_at": fn.UpdatedAt.Format(time.RFC3339),
	})
}

// handleDeleteFunction handles DELETE /v1/functions/{name}.
func (s *Server) handleDeleteFunction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	if err := s.fnStore.Delete(r.Context(), name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

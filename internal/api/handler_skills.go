package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// handleCreateSkill handles POST /v1/skills.
func (s *Server) handleCreateSkill(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "skills store not available"})
		return
	}

	var req struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		FunctionName string `json:"function_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}
	if req.Name == "" || req.FunctionName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and function_name are required"})
		return
	}

	session := SessionFromContext(r.Context())
	userID := ""
	if session != nil {
		userID = session.UserID
	}

	sk, err := s.skillStore.Create(r.Context(), req.Name, req.Description, req.FunctionName, userID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":            sk.ID,
		"name":          sk.Name,
		"function_name": sk.FunctionName,
		"created_at":    sk.CreatedAt.Format(time.RFC3339),
	})
}

// handleListSkills handles GET /v1/skills.
func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "skills store not available"})
		return
	}

	skills, err := s.skillStore.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type skillSummary struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Description  string `json:"description,omitempty"`
		FunctionName string `json:"function_name"`
		UpdatedAt    string `json:"updated_at"`
	}
	result := make([]skillSummary, 0, len(skills))
	for _, sk := range skills {
		result = append(result, skillSummary{
			ID:           sk.ID,
			Name:         sk.Name,
			Description:  sk.Description,
			FunctionName: sk.FunctionName,
			UpdatedAt:    sk.UpdatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// handleGetSkill handles GET /v1/skills/{name}.
func (s *Server) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "skills store not available"})
		return
	}

	name := r.PathValue("name")
	sk, err := s.skillStore.Get(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if sk == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "skill not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":            sk.ID,
		"name":          sk.Name,
		"description":   sk.Description,
		"function_name": sk.FunctionName,
		"created_by":    sk.CreatedBy,
		"created_at":    sk.CreatedAt.Format(time.RFC3339),
		"updated_at":    sk.UpdatedAt.Format(time.RFC3339),
	})
}

// handleUpdateSkill handles PUT /v1/skills/{name}.
func (s *Server) handleUpdateSkill(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "skills store not available"})
		return
	}

	name := r.PathValue("name")

	var req struct {
		Description  string `json:"description"`
		FunctionName string `json:"function_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}
	if req.FunctionName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "function_name is required"})
		return
	}

	sk, err := s.skillStore.Update(r.Context(), name, req.Description, req.FunctionName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":            sk.ID,
		"name":          sk.Name,
		"function_name": sk.FunctionName,
		"updated_at":    sk.UpdatedAt.Format(time.RFC3339),
	})
}

// handleDeleteSkill handles DELETE /v1/skills/{name}.
func (s *Server) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "skills store not available"})
		return
	}

	name := r.PathValue("name")

	if err := s.skillStore.Delete(r.Context(), name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleExecSkill handles POST /v1/skills/{name}/exec.
func (s *Server) handleExecSkill(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "skills store not available"})
		return
	}

	name := r.PathValue("name")

	var req struct {
		Params map[string]any `json:"params"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, ErrBadRequest)
			return
		}
	}

	result, err := s.skillStore.Execute(r.Context(), name, req.Params)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

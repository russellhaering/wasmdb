package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// handleCreateAgent handles POST /v1/agents.
func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	if s.agentStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent store not available"})
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
		Schedule    string `json:"schedule"`
		TriggerType string `json:"trigger_type"`
		Enabled     *bool  `json:"enabled"`
		MaxTurns    int    `json:"max_turns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}
	if req.Name == "" || req.Prompt == "" || req.Schedule == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, prompt, and schedule are required"})
		return
	}
	if req.TriggerType == "" {
		req.TriggerType = "timer"
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	session := SessionFromContext(r.Context())
	userID := ""
	if session != nil {
		userID = session.UserID
	}

	ag, err := s.agentStore.Create(r.Context(), req.Name, req.Description, req.Prompt, req.Schedule, req.TriggerType, enabled, req.MaxTurns, userID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           ag.ID,
		"name":         ag.Name,
		"trigger_type": ag.TriggerType,
		"schedule":     ag.Schedule,
		"enabled":      ag.Enabled,
		"created_at":   ag.CreatedAt.Format(time.RFC3339),
	})
}

// handleListAgents handles GET /v1/agents.
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if s.agentStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent store not available"})
		return
	}

	agents, err := s.agentStore.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type agentSummary struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Schedule    string `json:"schedule"`
		TriggerType string `json:"trigger_type"`
		Enabled     bool   `json:"enabled"`
		UpdatedAt   string `json:"updated_at"`
	}
	result := make([]agentSummary, 0, len(agents))
	for _, ag := range agents {
		result = append(result, agentSummary{
			ID:          ag.ID,
			Name:        ag.Name,
			Description: ag.Description,
			Schedule:    ag.Schedule,
			TriggerType: ag.TriggerType,
			Enabled:     ag.Enabled,
			UpdatedAt:   ag.UpdatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// handleGetAgent handles GET /v1/agents/{name}.
func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	if s.agentStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent store not available"})
		return
	}

	name := r.PathValue("name")
	ag, err := s.agentStore.Get(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if ag == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}

	writeJSON(w, http.StatusOK, ag)
}

// handleUpdateAgent handles PUT /v1/agents/{name}.
func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	if s.agentStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent store not available"})
		return
	}

	name := r.PathValue("name")

	var req struct {
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
		Schedule    string `json:"schedule"`
		TriggerType string `json:"trigger_type"`
		Enabled     *bool  `json:"enabled"`
		MaxTurns    int    `json:"max_turns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}
	if req.Prompt == "" || req.Schedule == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt and schedule are required"})
		return
	}
	if req.TriggerType == "" {
		req.TriggerType = "timer"
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	ag, err := s.agentStore.Update(r.Context(), name, req.Description, req.Prompt, req.Schedule, req.TriggerType, enabled, req.MaxTurns)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":           ag.ID,
		"name":         ag.Name,
		"trigger_type": ag.TriggerType,
		"schedule":     ag.Schedule,
		"enabled":      ag.Enabled,
		"updated_at":   ag.UpdatedAt.Format(time.RFC3339),
	})
}

// handleDeleteAgent handles DELETE /v1/agents/{name}.
func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	if s.agentStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent store not available"})
		return
	}

	name := r.PathValue("name")

	if err := s.agentStore.Delete(r.Context(), name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleTriggerAgent handles POST /v1/agents/{name}/trigger.
func (s *Server) handleTriggerAgent(w http.ResponseWriter, r *http.Request) {
	if s.agentScheduler == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent scheduler not available"})
		return
	}

	name := r.PathValue("name")

	run, err := s.agentScheduler.RunAgent(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, run)
}

// handleListAgentRuns handles GET /v1/agents/{name}/runs.
func (s *Server) handleListAgentRuns(w http.ResponseWriter, r *http.Request) {
	if s.agentStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent store not available"})
		return
	}

	name := r.PathValue("name")
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	runs, err := s.agentStore.ListRuns(r.Context(), name, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, runs)
}

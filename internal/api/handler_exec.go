package api

import (
	"encoding/json"
	"net/http"
)

// handleExecStored handles POST /v1/functions/{name}/exec.
func (s *Server) handleExecStored(w http.ResponseWriter, r *http.Request) {
	if s.fnEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "functions engine not available"})
		return
	}

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

	var req struct {
		Params map[string]any `json:"params"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, ErrBadRequest)
			return
		}
	}

	result := s.fnEngine.Execute(r.Context(), fn.Code, req.Params)
	writeJSON(w, http.StatusOK, result)
}

// handleExecEphemeral handles POST /v1/exec.
func (s *Server) handleExecEphemeral(w http.ResponseWriter, r *http.Request) {
	if s.fnEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "functions engine not available"})
		return
	}

	var req struct {
		Code   string         `json:"code"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}
	if req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code is required"})
		return
	}

	result := s.fnEngine.Execute(r.Context(), req.Code, req.Params)
	writeJSON(w, http.StatusOK, result)
}

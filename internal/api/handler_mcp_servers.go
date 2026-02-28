package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/russellhaering/wasmdb/internal/mcpservers"
)

// handleCreateMCPServer handles POST /v1/mcp-servers.
func (s *Server) handleCreateMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcpServerStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "mcp server store not available"})
		return
	}

	var req struct {
		Name        string              `json:"name"`
		Description string              `json:"description"`
		Transport   string              `json:"transport"`
		URL         string              `json:"url"`
		Command     string              `json:"command"`
		Args        []string            `json:"args"`
		Env         []string            `json:"env"`
		Headers     map[string]string   `json:"headers"`
		OAuth       *mcpservers.OAuthConfig `json:"oauth"`
		Enabled     *bool               `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}
	if req.Name == "" || req.Transport == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and transport are required"})
		return
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

	srv, err := s.mcpServerStore.Create(r.Context(), req.Name, req.Description, req.Transport, req.URL, req.Command, req.Args, req.Env, req.Headers, req.OAuth, enabled, userID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        srv.ID,
		"name":      srv.Name,
		"transport": srv.Transport,
		"enabled":   srv.Enabled,
		"created_at": srv.CreatedAt.Format(time.RFC3339),
	})
}

// handleListMCPServers handles GET /v1/mcp-servers.
func (s *Server) handleListMCPServers(w http.ResponseWriter, r *http.Request) {
	if s.mcpServerStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "mcp server store not available"})
		return
	}

	servers, err := s.mcpServerStore.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type serverSummary struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Transport   string `json:"transport"`
		URL         string `json:"url,omitempty"`
		Command     string `json:"command,omitempty"`
		Enabled     bool   `json:"enabled"`
		UpdatedAt   string `json:"updated_at"`
	}
	result := make([]serverSummary, 0, len(servers))
	for _, srv := range servers {
		result = append(result, serverSummary{
			ID:          srv.ID,
			Name:        srv.Name,
			Description: srv.Description,
			Transport:   srv.Transport,
			URL:         srv.URL,
			Command:     srv.Command,
			Enabled:     srv.Enabled,
			UpdatedAt:   srv.UpdatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// handleGetMCPServer handles GET /v1/mcp-servers/{name}.
func (s *Server) handleGetMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcpServerStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "mcp server store not available"})
		return
	}

	name := r.PathValue("name")
	srv, err := s.mcpServerStore.Get(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if srv == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "MCP server not found"})
		return
	}

	writeJSON(w, http.StatusOK, srv)
}

// handleUpdateMCPServer handles PUT /v1/mcp-servers/{name}.
func (s *Server) handleUpdateMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcpServerStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "mcp server store not available"})
		return
	}

	name := r.PathValue("name")

	var req struct {
		Description string              `json:"description"`
		Transport   string              `json:"transport"`
		URL         string              `json:"url"`
		Command     string              `json:"command"`
		Args        []string            `json:"args"`
		Env         []string            `json:"env"`
		Headers     map[string]string   `json:"headers"`
		OAuth       *mcpservers.OAuthConfig `json:"oauth"`
		Enabled     *bool               `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}
	if req.Transport == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "transport is required"})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	srv, err := s.mcpServerStore.Update(r.Context(), name, req.Description, req.Transport, req.URL, req.Command, req.Args, req.Env, req.Headers, req.OAuth, enabled)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":        srv.ID,
		"name":      srv.Name,
		"transport": srv.Transport,
		"enabled":   srv.Enabled,
		"updated_at": srv.UpdatedAt.Format(time.RFC3339),
	})
}

// handleDeleteMCPServer handles DELETE /v1/mcp-servers/{name}.
func (s *Server) handleDeleteMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcpServerStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "mcp server store not available"})
		return
	}

	name := r.PathValue("name")

	if err := s.mcpServerStore.Delete(r.Context(), name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

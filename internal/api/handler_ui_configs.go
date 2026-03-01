package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/russellhaering/wasmdb/internal/uiconfig"
)

// handleCreateUIConfig handles POST /v1/ui-configs.
func (s *Server) handleCreateUIConfig(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui config store not available"})
		return
	}

	var req struct {
		Name               string   `json:"name"`
		Title              string   `json:"title"`
		Description        string   `json:"description"`
		SourceTables       []string `json:"source_tables"`
		SurfaceJSON        string   `json:"surface_json"`
		QueryJS            string   `json:"query_js"`
		AutoRefreshSeconds int      `json:"auto_refresh_seconds"`
		SortOrder          int      `json:"sort_order"`
		Enabled            *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}
	if req.Name == "" || req.SurfaceJSON == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and surface_json are required"})
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

	cfg, err := s.uiConfigStore.Create(r.Context(), req.Name, req.Title, req.Description, req.SourceTables, req.SurfaceJSON, req.QueryJS, req.AutoRefreshSeconds, req.SortOrder, enabled, userID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         cfg.ID,
		"name":       cfg.Name,
		"title":      cfg.Title,
		"enabled":    cfg.Enabled,
		"created_at": cfg.CreatedAt.Format(time.RFC3339),
	})
}

// handleListUIConfigs handles GET /v1/ui-configs.
func (s *Server) handleListUIConfigs(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui config store not available"})
		return
	}

	configs, err := s.uiConfigStore.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type configSummary struct {
		ID                 string   `json:"id"`
		Name               string   `json:"name"`
		Title              string   `json:"title"`
		Description        string   `json:"description,omitempty"`
		SourceTables       []string `json:"source_tables,omitempty"`
		AutoRefreshSeconds int      `json:"auto_refresh_seconds,omitempty"`
		SortOrder          int      `json:"sort_order"`
		Enabled            bool     `json:"enabled"`
		UpdatedAt          string   `json:"updated_at"`
	}
	result := make([]configSummary, 0, len(configs))
	for _, cfg := range configs {
		result = append(result, configSummary{
			ID:                 cfg.ID,
			Name:               cfg.Name,
			Title:              cfg.Title,
			Description:        cfg.Description,
			SourceTables:       cfg.SourceTables,
			AutoRefreshSeconds: cfg.AutoRefreshSeconds,
			SortOrder:          cfg.SortOrder,
			Enabled:            cfg.Enabled,
			UpdatedAt:          cfg.UpdatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// handleGetUIConfig handles GET /v1/ui-configs/{name}.
func (s *Server) handleGetUIConfig(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui config store not available"})
		return
	}

	name := r.PathValue("name")
	cfg, err := s.uiConfigStore.Get(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if cfg == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ui config not found"})
		return
	}

	writeJSON(w, http.StatusOK, cfg)
}

// handleUpdateUIConfig handles PUT /v1/ui-configs/{name}.
func (s *Server) handleUpdateUIConfig(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui config store not available"})
		return
	}

	name := r.PathValue("name")

	var req struct {
		Title              string   `json:"title"`
		Description        string   `json:"description"`
		SourceTables       []string `json:"source_tables"`
		SurfaceJSON        string   `json:"surface_json"`
		QueryJS            string   `json:"query_js"`
		AutoRefreshSeconds int      `json:"auto_refresh_seconds"`
		SortOrder          int      `json:"sort_order"`
		Enabled            *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}
	if req.SurfaceJSON == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "surface_json is required"})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	cfg, err := s.uiConfigStore.Update(r.Context(), name, req.Title, req.Description, req.SourceTables, req.SurfaceJSON, req.QueryJS, req.AutoRefreshSeconds, req.SortOrder, enabled)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         cfg.ID,
		"name":       cfg.Name,
		"title":      cfg.Title,
		"enabled":    cfg.Enabled,
		"updated_at": cfg.UpdatedAt.Format(time.RFC3339),
	})
}

// handleDeleteUIConfig handles DELETE /v1/ui-configs/{name}.
func (s *Server) handleDeleteUIConfig(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui config store not available"})
		return
	}

	name := r.PathValue("name")

	if err := s.uiConfigStore.Delete(r.Context(), name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRenderUIConfig handles POST /v1/ui-configs/{name}/render.
// Runs the full server-side pipeline: query_js → template replace → JSON parse → A2UI validate.
func (s *Server) handleRenderUIConfig(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui config store not available"})
		return
	}

	name := r.PathValue("name")
	cfg, err := s.uiConfigStore.Get(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if cfg == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ui config not found"})
		return
	}

	result := uiconfig.Render(r.Context(), cfg, s.fnEngine)
	writeJSON(w, http.StatusOK, result)
}

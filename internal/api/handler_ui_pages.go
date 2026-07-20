package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/russellhaering/wasmdb/internal/uiconfig"
)

// handleCreateUIPage handles POST /v1/ui/pages. It creates a page with
// generator "user" and created_by taken from the session, then runs the render
// pipeline once (params nil) so the response surfaces any render_error — the
// page is still created even when the render fails (content-level failure).
func (s *Server) handleCreateUIPage(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui page store not available"})
		return
	}

	var req struct {
		Name               string   `json:"name"`
		Title              string   `json:"title"`
		Description        string   `json:"description"`
		SourceTables       []string `json:"source_tables"`
		SurfaceJSON        string   `json:"surface_json"`
		ActionsJSON        string   `json:"actions_json"`
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

	cfg, err := s.uiConfigStore.Create(r.Context(), req.Name, req.Title, req.Description, req.SourceTables, req.SurfaceJSON, req.ActionsJSON, req.QueryJS, req.AutoRefreshSeconds, req.SortOrder, enabled, "user", userID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, s.uiPageResponse(r, cfg))
}

// handleListUIPages handles GET /v1/ui/pages. It returns all pages with every
// UIConfig field populated.
func (s *Server) handleListUIPages(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui page store not available"})
		return
	}

	configs, err := s.uiConfigStore.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	result := make([]*uiconfig.UIConfig, 0, len(configs))
	result = append(result, configs...)
	writeJSON(w, http.StatusOK, result)
}

// handleGetUIPage handles GET /v1/ui/pages/{name}.
func (s *Server) handleGetUIPage(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui page store not available"})
		return
	}

	name := r.PathValue("name")
	cfg, err := s.uiConfigStore.Get(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if cfg == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ui page not found"})
		return
	}

	writeJSON(w, http.StatusOK, cfg)
}

// handleUpdateUIPage handles PATCH /v1/ui/pages/{name}. Only fields present in
// the JSON body are applied (partial update); the rest are preserved. It sets
// generator "user" and render-checks the result like create does.
func (s *Server) handleUpdateUIPage(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui page store not available"})
		return
	}

	name := r.PathValue("name")

	var req struct {
		Title              *string   `json:"title"`
		Description        *string   `json:"description"`
		SourceTables       *[]string `json:"source_tables"`
		SurfaceJSON        *string   `json:"surface_json"`
		ActionsJSON        *string   `json:"actions_json"`
		QueryJS            *string   `json:"query_js"`
		AutoRefreshSeconds *int      `json:"auto_refresh_seconds"`
		SortOrder          *int      `json:"sort_order"`
		Enabled            *bool     `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}

	generator := "user"
	cfg, err := s.uiConfigStore.Update(r.Context(), name, uiconfig.UpdateParams{
		Title:              req.Title,
		Description:        req.Description,
		SourceTables:       req.SourceTables,
		SurfaceJSON:        req.SurfaceJSON,
		ActionsJSON:        req.ActionsJSON,
		QueryJS:            req.QueryJS,
		AutoRefreshSeconds: req.AutoRefreshSeconds,
		SortOrder:          req.SortOrder,
		Enabled:            req.Enabled,
		Generator:          &generator,
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, s.uiPageResponse(r, cfg))
}

// handleDeleteUIPage handles DELETE /v1/ui/pages/{name}.
func (s *Server) handleDeleteUIPage(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui page store not available"})
		return
	}

	name := r.PathValue("name")
	if err := s.uiConfigStore.Delete(r.Context(), name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRenderUIPage handles POST /v1/ui/pages/{name}/render.
//
// It runs the full server-side pipeline: query_js (with the supplied params) →
// parse surface + actions → validate against the query data shape. A render
// failure is content-level, not a request error: the page exists, so we return
// HTTP 200 with {error, error_phase, logs} and let the client draw an error
// banner. HTTP 404 is reserved for a missing page.
//
// On success the response is {surface, data, actions, logs, auto_refresh_seconds,
// title, description}.
func (s *Server) handleRenderUIPage(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil || s.uiRenderer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui renderer not available"})
		return
	}

	name := r.PathValue("name")
	cfg, err := s.uiConfigStore.Get(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if cfg == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ui page not found"})
		return
	}

	var req struct {
		Params map[string]any `json:"params"`
	}
	// Body is optional; ignore decode errors from an empty body.
	_ = json.NewDecoder(r.Body).Decode(&req)

	result := s.uiRenderer.Render(r.Context(), cfg, req.Params)
	if result.Error != "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"error":       result.Error,
			"error_phase": result.ErrorPhase,
			"logs":        result.Logs,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"surface":              result.Surface,
		"data":                 result.Data,
		"actions":              result.Actions,
		"logs":                 result.Logs,
		"auto_refresh_seconds": cfg.AutoRefreshSeconds,
		"title":                cfg.Title,
		"description":          cfg.Description,
	})
}

// handleUIPageAction handles POST /v1/ui/pages/{name}/actions/{action}.
//
// Body is {params: {…}}. Error mapping:
//   - unknown page                → 404
//   - action not declared on page → 400 (a malformed/undeclared request)
//   - execution/validation errors → 200 with {ok:false, error} (data-level
//     failures the client shows inline, e.g. schema validation on a write)
//
// On success the ActionResult JSON is returned (ok:true plus result/data).
func (s *Server) handleUIPageAction(w http.ResponseWriter, r *http.Request) {
	if s.uiConfigStore == nil || s.uiRenderer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ui renderer not available"})
		return
	}

	name := r.PathValue("name")
	actionName := r.PathValue("action")

	cfg, err := s.uiConfigStore.Get(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if cfg == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ui page not found"})
		return
	}

	// Reject actions that are not declared on the page up front so an undeclared
	// action is a 400 (bad request) rather than a 200 with ok:false.
	if !s.uiRenderer.HasAction(cfg, actionName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action not declared: " + actionName})
		return
	}

	var req struct {
		Params map[string]any `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	result := s.uiRenderer.ExecuteAction(r.Context(), cfg, actionName, req.Params)
	writeJSON(w, http.StatusOK, result)
}

// uiPageResponse builds the create/update response body: the render-checked
// metadata plus render_error / render_error_phase when the one-shot render
// (params nil) fails. The page is created/updated regardless.
func (s *Server) uiPageResponse(r *http.Request, cfg *uiconfig.UIConfig) map[string]any {
	resp := map[string]any{
		"id":         cfg.ID,
		"name":       cfg.Name,
		"title":      cfg.Title,
		"enabled":    cfg.Enabled,
		"created_at": cfg.CreatedAt.Format(time.RFC3339),
		"updated_at": cfg.UpdatedAt.Format(time.RFC3339),
	}
	if s.uiRenderer != nil {
		render := s.uiRenderer.Render(r.Context(), cfg, nil)
		if render.Error != "" {
			resp["render_error"] = render.Error
			resp["render_error_phase"] = render.ErrorPhase
		}
	}
	return resp
}

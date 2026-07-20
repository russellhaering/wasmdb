package uiconfig

import (
	"context"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/functions"
	"github.com/russellhaering/wasmdb/internal/surface"
)

// RenderResult holds the result of a full server-side render pipeline. On
// success Surface (plus optional Data and Actions) is populated; on failure
// Error and ErrorPhase describe what went wrong.
type RenderResult struct {
	Surface *surface.Surface `json:"surface,omitempty"`
	Data    map[string]any   `json:"data,omitempty"`
	Actions surface.Actions  `json:"actions,omitempty"`
	Logs    []string         `json:"logs,omitempty"`
	// on failure:
	Error      string `json:"error,omitempty"`
	ErrorPhase string `json:"error_phase,omitempty"` // "query_js" | "parse" | "validate"
}

// Renderer runs the server-side render and action pipelines for UI pages. It
// holds the database registry (for action writes) and the JS engine (for
// query_js execution).
type Renderer struct {
	registry *database.Registry
	fnEngine *functions.Engine
}

// NewRenderer creates a Renderer bound to a registry and JS engine.
func NewRenderer(registry *database.Registry, fnEngine *functions.Engine) *Renderer {
	return &Renderer{registry: registry, fnEngine: fnEngine}
}

// Render executes the full server-side render pipeline:
//  1. If cfg.QueryJS is non-empty, run it via the JS engine with params to
//     produce the data object bound by $data references.
//  2. Parse the surface and action declarations.
//  3. Validate the surface against the declared actions and the query data.
//
// There is no string templating: data flows to the client structurally in Data
// and is bound at render time against $data references.
func (r *Renderer) Render(ctx context.Context, cfg *UIConfig, params map[string]any) *RenderResult {
	result := &RenderResult{}

	// Step 1: run query_js to produce the data object.
	var data map[string]any
	if cfg.QueryJS != "" {
		if r.fnEngine == nil {
			result.Error = "query_js requires the JavaScript engine but it is not available"
			result.ErrorPhase = "query_js"
			return result
		}
		if params == nil {
			params = map[string]any{}
		}
		exec := r.fnEngine.Execute(ctx, cfg.QueryJS, params)
		result.Logs = exec.Logs
		if exec.Error != "" {
			result.Error = "query_js execution failed: " + exec.Error
			result.ErrorPhase = "query_js"
			return result
		}
		m, ok := exec.Result.(map[string]any)
		if !ok {
			result.Error = "query_js must return an object (a JSON map) so its values can be bound via $data references; " +
				"got a non-object result. Return something like { rows: [...], total: n }."
			result.ErrorPhase = "query_js"
			return result
		}
		data = m
	}

	// Step 2: parse surface + actions.
	surf, err := surface.ParseSurface([]byte(cfg.SurfaceJSON))
	if err != nil {
		result.Error = err.Error()
		result.ErrorPhase = "parse"
		return result
	}
	actions, err := surface.ParseActions([]byte(cfg.ActionsJSON))
	if err != nil {
		result.Error = err.Error()
		result.ErrorPhase = "parse"
		return result
	}

	// Step 3: validate against the query data shape.
	if err := surface.Validate(surf, actions, data); err != nil {
		result.Error = err.Error()
		result.ErrorPhase = "validate"
		return result
	}

	result.Surface = surf
	result.Data = data
	result.Actions = actions
	return result
}

package uiconfig

import (
	"context"
	"fmt"
	"strings"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/surface"
)

// ActionResult holds the outcome of executing a declared page action.
type ActionResult struct {
	OK     bool           `json:"ok"`
	Result any            `json:"result,omitempty"` // e.g. {id, version} for writes
	Data   map[string]any `json:"data,omitempty"`   // refreshed query data for type=query
	Logs   []string       `json:"logs,omitempty"`
	Error  string         `json:"error,omitempty"`
}

func actionError(format string, args ...any) *ActionResult {
	return &ActionResult{OK: false, Error: fmt.Sprintf(format, args...)}
}

// HasAction reports whether the page declares an action with the given name. It
// lets callers (e.g. the HTTP API) distinguish an undeclared action (a bad
// request) from a data-level execution failure before invoking ExecuteAction.
func (r *Renderer) HasAction(cfg *UIConfig, actionName string) bool {
	actions, err := surface.ParseActions([]byte(cfg.ActionsJSON))
	if err != nil {
		return false
	}
	_, ok := actions[actionName]
	return ok
}

// ExecuteAction runs a single declared action against the page's data model.
// Insert, update, and delete write to the action's table through the normal
// registry write paths (schema validation applies; system tables are rejected).
// Query re-runs the page's query_js with the action's declared params.
func (r *Renderer) ExecuteAction(ctx context.Context, cfg *UIConfig, actionName string, params map[string]any) *ActionResult {
	actions, err := surface.ParseActions([]byte(cfg.ActionsJSON))
	if err != nil {
		return actionError("parse actions: %s", err.Error())
	}
	action, ok := actions[actionName]
	if !ok {
		return actionError("action not declared: %q", actionName)
	}

	if params == nil {
		params = map[string]any{}
	}

	switch action.Type {
	case surface.ActionInsert:
		return r.executeInsert(ctx, action, params)
	case surface.ActionUpdate:
		return r.executeUpdate(ctx, action, params)
	case surface.ActionDelete:
		return r.executeDelete(ctx, action, params)
	case surface.ActionQuery:
		return r.executeQuery(ctx, cfg, action, params)
	default:
		return actionError("action %q has unknown type %q", actionName, action.Type)
	}
}

func (r *Renderer) executeInsert(ctx context.Context, action surface.Action, params map[string]any) *ActionResult {
	tbl, errRes := r.writableTable(ctx, action.Table)
	if errRes != nil {
		return errRes
	}

	content, attrs, errRes := splitParams(params)
	if errRes != nil {
		return errRes
	}

	doc := &document.Document{Content: content, Attributes: attrs}
	if err := tbl.PutDocument(ctx, doc); err != nil {
		return actionError("insert failed: %s", err.Error())
	}
	return &ActionResult{OK: true, Result: map[string]any{"id": doc.ID, "version": doc.Version}}
}

func (r *Renderer) executeUpdate(ctx context.Context, action surface.Action, params map[string]any) *ActionResult {
	tbl, errRes := r.writableTable(ctx, action.Table)
	if errRes != nil {
		return errRes
	}

	id, ok := params["id"].(string)
	if !ok || id == "" {
		return actionError("update requires a non-empty string %q param", "id")
	}

	existing, err := tbl.GetDocument(ctx, id)
	if err != nil {
		return actionError("update failed: %s", err.Error())
	}
	if existing == nil {
		return actionError("update failed: document %q not found", id)
	}

	content, attrs, errRes := splitParams(params)
	if errRes != nil {
		return errRes
	}

	if existing.Attributes == nil {
		existing.Attributes = map[string]any{}
	}
	// Merge attributes: a nil value deletes the key, otherwise it is set.
	for k, v := range attrs {
		if v == nil {
			delete(existing.Attributes, k)
		} else {
			existing.Attributes[k] = v
		}
	}
	// A "content" param replaces the document content.
	if _, present := params["content"]; present {
		existing.Content = content
	}

	if err := tbl.PutDocument(ctx, existing); err != nil {
		return actionError("update failed: %s", err.Error())
	}
	return &ActionResult{OK: true, Result: map[string]any{"id": existing.ID, "version": existing.Version}}
}

func (r *Renderer) executeDelete(ctx context.Context, action surface.Action, params map[string]any) *ActionResult {
	tbl, errRes := r.writableTable(ctx, action.Table)
	if errRes != nil {
		return errRes
	}

	id, ok := params["id"].(string)
	if !ok || id == "" {
		return actionError("delete requires a non-empty string %q param", "id")
	}

	if err := tbl.DeleteDocument(ctx, id); err != nil {
		return actionError("delete failed: %s", err.Error())
	}
	return &ActionResult{OK: true, Result: map[string]any{"id": id}}
}

func (r *Renderer) executeQuery(ctx context.Context, cfg *UIConfig, action surface.Action, params map[string]any) *ActionResult {
	if cfg.QueryJS == "" {
		return actionError("query action requires the page to define query_js")
	}
	if r.fnEngine == nil {
		return actionError("query action requires the JavaScript engine but it is not available")
	}

	// Filter incoming params to the action's declared list; drop the rest.
	filtered := map[string]any{}
	for _, name := range action.Params {
		if v, ok := params[name]; ok {
			filtered[name] = v
		}
	}

	exec := r.fnEngine.Execute(ctx, cfg.QueryJS, filtered)
	if exec.Error != "" {
		return &ActionResult{OK: false, Error: "query_js execution failed: " + exec.Error, Logs: exec.Logs}
	}
	m, ok := exec.Result.(map[string]any)
	if !ok {
		return &ActionResult{
			OK:    false,
			Error: "query_js must return an object (a JSON map); got a non-object result",
			Logs:  exec.Logs,
		}
	}
	return &ActionResult{OK: true, Data: m, Logs: exec.Logs}
}

// writableTable resolves a table for a write action, rejecting system tables
// and missing tables with clear errors.
func (r *Renderer) writableTable(ctx context.Context, name string) (*database.Table, *ActionResult) {
	if strings.TrimSpace(name) == "" {
		return nil, actionError("action is missing a target table")
	}
	if strings.HasPrefix(name, "_") {
		return nil, actionError("table %q is a system table and cannot be written", name)
	}
	tbl, err := r.registry.GetTable(ctx, name)
	if err != nil {
		return nil, actionError("table %q not found", name)
	}
	if tbl.System {
		return nil, actionError("table %q is a system table and cannot be written", name)
	}
	return tbl, nil
}

// splitParams separates action params into an optional content string and the
// remaining document attributes. The reserved "id" and "content" keys are
// dropped from attributes; every other key must be a valid identifier.
func splitParams(params map[string]any) (content string, attrs map[string]any, errRes *ActionResult) {
	attrs = map[string]any{}
	if c, present := params["content"]; present {
		s, ok := c.(string)
		if !ok {
			return "", nil, actionError("param %q must be a string", "content")
		}
		content = s
	}
	for k, v := range params {
		if k == "id" || k == "content" {
			continue
		}
		if !isIdent(k) {
			return "", nil, actionError("param key %q is not a valid identifier", k)
		}
		attrs[k] = v
	}
	return content, attrs, nil
}

// isIdent reports whether s is a valid identifier: a letter or underscore
// followed by letters, digits, or underscores.
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

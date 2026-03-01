package uiconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/russellhaering/wasmdb/internal/a2ui"
	"github.com/russellhaering/wasmdb/internal/functions"
)

// RenderResult holds the result of a full server-side render pipeline.
type RenderResult struct {
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Surface     *a2ui.Surface `json:"surface,omitempty"`
	Data        any           `json:"data,omitempty"`
	Logs        []string      `json:"logs,omitempty"`
	Status      string        `json:"status"` // "ok" or "error"
	Error       string        `json:"error,omitempty"`
	ErrorPhase  string        `json:"error_phase,omitempty"` // "query_js", "template", "json_parse", "a2ui_validate"
}

// Render runs the complete server-side render pipeline:
//  1. Execute query_js (if any) to get dynamic data
//  2. Apply template replacement to surface_json
//  3. Parse the resulting JSON into an A2UI Surface
//  4. Validate the surface structure
func Render(ctx context.Context, cfg *UIConfig, fnEngine *functions.Engine) *RenderResult {
	result := &RenderResult{
		Title:       cfg.Title,
		Description: cfg.Description,
		Status:      "ok",
	}

	// Step 1: Execute query_js if present.
	var data map[string]any
	if cfg.QueryJS != "" {
		if fnEngine == nil {
			result.Status = "error"
			result.Error = "query_js requires JavaScript engine but it is not available"
			result.ErrorPhase = "query_js"
			return result
		}
		execResult := fnEngine.Execute(ctx, cfg.QueryJS, nil)
		if execResult.Error != "" {
			result.Status = "error"
			result.Error = "query_js execution failed: " + execResult.Error
			result.ErrorPhase = "query_js"
			result.Logs = execResult.Logs
			return result
		}
		result.Logs = execResult.Logs

		// Convert result to map for template replacement.
		if m, ok := execResult.Result.(map[string]any); ok {
			data = m
			result.Data = data
		} else if execResult.Result != nil {
			data = map[string]any{"result": execResult.Result}
			result.Data = execResult.Result
		}
	}

	// Step 2: Apply template replacement.
	surfaceStr := cfg.SurfaceJSON
	if data != nil {
		surfaceStr = TemplateReplace(surfaceStr, data)
	}

	// Step 3: Parse JSON.
	var surface a2ui.Surface
	if err := json.Unmarshal([]byte(surfaceStr), &surface); err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("surface JSON parse error after template replacement: %s", err.Error())
		result.ErrorPhase = "json_parse"
		result.Error += "\n\nHint: this usually means a template variable like {{rows}} produced invalid JSON. " +
			"Make sure array/object templates are the entire property value: \"rows\": \"{{rows}}\" (the quotes get stripped automatically for objects/arrays)."
		return result
	}

	// Step 4: Validate A2UI structure.
	if err := a2ui.Validate(surface); err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("A2UI validation error: %s", err.Error())
		result.ErrorPhase = "a2ui_validate"
		return result
	}

	result.Surface = &surface
	return result
}

var (
	quotedTemplateRe = regexp.MustCompile(`"\{\{([^}]+)\}\}"`)
	inlineTemplateRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)
)

// TemplateReplace applies {{key}} and {{key.subkey}} template replacement.
// Two-pass approach:
//  1. "{{key}}" (quoted) — if value is object/array, strips quotes and injects raw JSON
//  2. {{key}} inline — replaces with string representation
func TemplateReplace(s string, data map[string]any) string {
	// First pass: quoted templates.
	s = quotedTemplateRe.ReplaceAllStringFunc(s, func(match string) string {
		path := match[3 : len(match)-3] // strip "{{ and }}"
		val := resolveTemplatePath(strings.TrimSpace(path), data)
		if val == nil {
			return match
		}
		b, err := json.Marshal(val)
		if err != nil {
			return match
		}
		return string(b)
	})

	// Second pass: inline templates.
	s = inlineTemplateRe.ReplaceAllStringFunc(s, func(match string) string {
		path := match[2 : len(match)-2] // strip {{ and }}
		val := resolveTemplatePath(strings.TrimSpace(path), data)
		if val == nil {
			return match
		}
		return fmt.Sprintf("%v", val)
	})

	return s
}

func resolveTemplatePath(path string, data map[string]any) any {
	keys := strings.Split(path, ".")
	var current any = data
	for _, k := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[k]
		if current == nil {
			return nil
		}
	}
	return current
}

// Package functions provides a sandboxed JavaScript execution engine
// backed by QuickJS compiled to WebAssembly (via wazero).
package functions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fastschema/qjs"
	"github.com/russellhaering/wasmdb/internal/database"
)

// ExecResult holds the result of a JavaScript execution.
type ExecResult struct {
	Result any      `json:"result"`
	Logs   []string `json:"logs"`
	DurationMS int64 `json:"duration_ms"`
	Error  string   `json:"error,omitempty"`
}

// Engine wraps QuickJS-in-Wasm for executing JavaScript functions.
type Engine struct {
	registry *database.Registry
	timeout  time.Duration
	sem      chan struct{} // concurrency limiter
}

// NewEngine creates a new JS execution engine.
func NewEngine(registry *database.Registry, timeout time.Duration, maxConcurrent int) *Engine {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}
	return &Engine{
		registry: registry,
		timeout:  timeout,
		sem:      make(chan struct{}, maxConcurrent),
	}
}

// Execute runs JavaScript code with optional parameters.
// The code can be either:
//   - A script with a handler(params) function that is called
//   - A bare expression/script whose last value is returned
func (e *Engine) Execute(ctx context.Context, code string, params map[string]any) *ExecResult {
	start := time.Now()
	result := &ExecResult{}

	// Acquire concurrency slot.
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	case <-ctx.Done():
		result.Error = "execution cancelled: concurrency limit reached"
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	// Create timeout context.
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	// Capture logs.
	var logs []string
	var logMu sync.Mutex

	// Capture stdout for console.log.
	var stdout bytes.Buffer

	// Create a fresh QJS runtime.
	rt, err := qjs.New(qjs.Option{
		Context:            execCtx,
		CloseOnContextDone: true,
		MaxExecutionTime:   int(e.timeout.Seconds()),
		MemoryLimit:        64 * 1024 * 1024, // 64MB
		MaxStackSize:       512 * 1024,        // 512KB
		Stdout:             &stdout,
		Stderr:             &stdout,
	})
	if err != nil {
		result.Error = fmt.Sprintf("failed to create JS runtime: %v", err)
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	defer rt.Close()

	jsCtx := rt.Context()

	// Bind console.log.
	consoleObj := jsCtx.NewObject()
	logFn := jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		var parts []string
		for _, arg := range this.Args() {
			if arg.IsObject() || arg.IsArray() {
				if s, err := arg.JSONStringify(); err == nil {
					parts = append(parts, s)
					continue
				}
			}
			parts = append(parts, arg.String())
		}
		logMu.Lock()
		logs = append(logs, strings.Join(parts, " "))
		logMu.Unlock()
		return jsCtx.NewUndefined(), nil
	})
	consoleObj.SetPropertyStr("log", logFn)
	consoleObj.SetPropertyStr("warn", logFn)
	consoleObj.SetPropertyStr("error", logFn)
	consoleObj.SetPropertyStr("info", logFn)
	jsCtx.Global().SetPropertyStr("console", consoleObj)

	// Bind the db host API.
	bindDBAPI(jsCtx, e.registry, execCtx)

	// Inject params as a global.
	if params != nil {
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			result.Error = fmt.Sprintf("failed to marshal params: %v", err)
			result.DurationMS = time.Since(start).Milliseconds()
			return result
		}
		paramsVal := jsCtx.ParseJSON(string(paramsJSON))
		jsCtx.Global().SetPropertyStr("params", paramsVal)
	} else {
		jsCtx.Global().SetPropertyStr("params", jsCtx.NewObject())
	}

	// Execute the code. We use a wrapper that captures the result via
	// a global variable, since QJS eval doesn't always propagate return
	// values from host-bound function calls.
	//
	// Strategy:
	//  1. Run the raw code to define functions and execute statements.
	//  2. If a handler() function was defined, call it with params.
	//  3. Otherwise, re-evaluate the last expression via __result wrapper.
	// Check if code defines a handler function.
	hasHandler := strings.Contains(strings.TrimSpace(code), "function handler")

	var val *qjs.Value

	if hasHandler {
		// Run the code to define the handler, then call it.
		_, err = execSafe(rt, code)
		if err != nil {
			result.Error = err.Error()
			result.Logs = logs
			result.DurationMS = time.Since(start).Milliseconds()
			return result
		}

		handlerVal := jsCtx.Global().GetPropertyStr("handler")
		if handlerVal != nil && !handlerVal.IsUndefined() && handlerVal.IsFunction() {
			paramsVal := jsCtx.Global().GetPropertyStr("params")
			callResult, callErr := jsCtx.Invoke(handlerVal, jsCtx.Global(), paramsVal)
			handlerVal.Free()
			if callErr != nil {
				result.Error = callErr.Error()
				result.Logs = logs
				result.DurationMS = time.Since(start).Milliseconds()
				return result
			}
			val = callResult
		} else {
			if handlerVal != nil {
				handlerVal.Free()
			}
		}
	} else {
		// No handler — wrap as IIFE to capture the return value directly.
		// This avoids the stale-reference problem with __result globals.
		iifeCode := wrapAsIIFE(code)
		val, err = execSafe(rt, iifeCode)
		if err != nil {
			result.Error = err.Error()
			result.Logs = logs
			result.DurationMS = time.Since(start).Milliseconds()
			return result
		}
	}

	// Convert return value by JSON-serializing inside JS, then parsing in Go.
	// This avoids QJS value-type quirks where JSONStringify on Go-side
	// produces corrupted output for objects created via ParseJSON.
	if val != nil && !val.IsUndefined() && !val.IsNull() {
		result.Result = extractResultViaJS(jsCtx, val)
		val.Free()
	}

	result.Logs = logs
	result.DurationMS = time.Since(start).Milliseconds()
	return result
}

// extractResultViaJS serializes a JS value by calling JSON.stringify inside
// the JS runtime, then parses the JSON string in Go. This is more reliable
// than using QJS's Go-side JSONStringify which can produce corrupted output
// for objects created via ParseJSON.
func extractResultViaJS(jsCtx *qjs.Context, val *qjs.Value) any {
	// Store the value as a global so we can reference it in eval.
	jsCtx.Global().SetPropertyStr("__tmp", val)

	// Use JSON.stringify inside JS.
	jsonVal, err := jsCtx.Eval("__json__.js", qjs.Code("JSON.stringify(__tmp)"))
	if err != nil || jsonVal == nil || jsonVal.IsUndefined() || jsonVal.IsNull() {
		// Fallback for primitives that JSON.stringify handles oddly (undefined, etc)
		return jsValueToGo(val)
	}
	defer jsonVal.Free()

	jsonStr := jsonVal.String()
	if jsonStr == "" || jsonStr == "undefined" {
		return jsValueToGo(val)
	}

	var out any
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		return jsValueToGo(val)
	}

	// Promote whole-number floats to int64 for cleaner output.
	return promoteInts(out)
}

// promoteInts recursively converts float64 whole numbers to int64.
func promoteInts(v any) any {
	switch val := v.(type) {
	case float64:
		if val == float64(int64(val)) && val >= -1e15 && val <= 1e15 {
			return int64(val)
		}
		return val
	case []any:
		for i, item := range val {
			val[i] = promoteInts(item)
		}
		return val
	case map[string]any:
		for k, item := range val {
			val[k] = promoteInts(item)
		}
		return val
	default:
		return v
	}
}

// wrapAsIIFE wraps user code in an immediately-invoked function expression
// so the eval return captures the result directly from the QJS runtime.
// This avoids issues with stale Value references from global property access.
//
// It splits the code into preamble statements and a trailing expression,
// wrapping as: (function(){ <preamble>; return (<lastExpr>); })()
func wrapAsIIFE(code string) string {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return code
	}

	// Split into lines and find last non-empty line.
	lines := strings.Split(trimmed, "\n")
	lastIdx := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			lastIdx = i
			break
		}
	}
	if lastIdx < 0 {
		return code
	}

	preamble := strings.Join(lines[:lastIdx], "\n")
	lastLine := strings.TrimSpace(lines[lastIdx])
	lastLine = strings.TrimRight(lastLine, ";")

	// If the last line is a statement, try to split on semicolons.
	if looksLikeStatement(lastLine) {
		parts := strings.Split(lastLine, ";")
		for i := len(parts) - 1; i >= 0; i-- {
			p := strings.TrimSpace(parts[i])
			if p == "" {
				continue
			}
			if !looksLikeStatement(p) {
				stmts := strings.Join(parts[:i], ";")
				if preamble != "" {
					stmts = preamble + "\n" + stmts
				}
				return "(function(){" + stmts + "; return (" + p + ");})()" 
			}
			break
		}
		// All statements — no return value.
		return "(function(){" + code + "})()" 
	}

	// Last line is an expression.
	if preamble != "" {
		return "(function(){" + preamble + "\nreturn (" + lastLine + ");})()" 
	}
	return "(function(){ return (" + lastLine + ");})()" 
}

// looksLikeStatement returns true if the line starts with a JS statement keyword.
func looksLikeStatement(line string) bool {
	for _, prefix := range []string{"var ", "let ", "const ", "if ", "if(",
		"for ", "for(", "while ", "while(", "return ", "return;",
		"throw ", "function ", "class ", "switch ", "switch(",
		"try ", "try{"} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// execSafe runs the JS eval with panic recovery.
func execSafe(rt *qjs.Runtime, code string) (val *qjs.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("JS execution error: %v", r)
		}
	}()

	val, err = rt.Eval("script.js", qjs.Code(code))
	if err != nil {
		return nil, err
	}

	// Check for exceptions.
	if rt.Context().HasException() {
		excErr := rt.Context().Exception()
		return nil, excErr
	}

	return val, nil
}

// jsValueToGo converts a QJS Value to a Go value suitable for JSON marshaling.
// QJS type-tag checks (IsArray, IsObject, etc.) can be unreliable for values
// obtained via GetPropertyStr, so for compound types we use JSONStringify.
func jsValueToGo(v *qjs.Value) any {
	if v == nil || v.IsNull() || v.IsUndefined() {
		return nil
	}

	// Check reliable primitives first.
	if v.IsString() {
		return v.String()
	}
	if v.IsNumber() {
		f := v.Float64()
		if f == float64(int64(f)) && f >= -1e15 && f <= 1e15 {
			return int64(f)
		}
		return f
	}
	if v.IsError() {
		exc := v.Exception()
		if exc != nil {
			return exc.Error()
		}
		return v.String()
	}

	// For anything else (objects, arrays, booleans via QJS internal tags),
	// use JSONStringify as the most reliable conversion path.
	if jsonStr, err := v.JSONStringify(); err == nil && jsonStr != "" {
		var out any
		if err := json.Unmarshal([]byte(jsonStr), &out); err == nil {
			return out
		}
	}

	// Final fallback.
	if v.IsBool() {
		return v.Bool()
	}
	return v.String()
}

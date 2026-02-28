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
	wrapperCode := wrapCodeForResult(code)
	_, err = execSafe(rt, wrapperCode)
	if err != nil {
		result.Error = err.Error()
		result.Logs = logs
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	var val *qjs.Value
	handlerVal := jsCtx.Global().GetPropertyStr("handler")
	if handlerVal != nil && !handlerVal.IsUndefined() && handlerVal.IsFunction() {
		// Call handler(params).
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
		// Get __result from the wrapper.
		val = jsCtx.Global().GetPropertyStr("__result")
	}

	// Convert return value.
	if val != nil && !val.IsUndefined() && !val.IsNull() {
		result.Result = jsValueToGo(val)
		val.Free()
	}

	result.Logs = logs
	result.DurationMS = time.Since(start).Milliseconds()
	return result
}

// wrapCodeForResult wraps user code so the last expression's value
// is captured in the __result global. This is necessary because QJS
// eval doesn't reliably return values from host-bound function calls.
//
// For code containing a handler() function definition, we skip wrapping
// and call handler() separately.
func wrapCodeForResult(code string) string {
	trimmed := strings.TrimSpace(code)

	// If code defines a handler function, don't wrap — we'll call it explicitly.
	if strings.Contains(trimmed, "function handler") {
		return code
	}

	// Split into statements. We use newlines as the primary delimiter.
	lines := strings.Split(trimmed, "\n")
	// Find the last non-empty line.
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

	// Take everything before the last line as preamble.
	preamble := strings.Join(lines[:lastIdx], "\n")
	lastLine := strings.TrimSpace(lines[lastIdx])

	// Strip trailing semicolon from last line.
	lastLine = strings.TrimRight(lastLine, ";")

	// Check if the last line looks like a statement (var/let/const/if/for/while/etc).
	// If so, try splitting on semicolons to find a trailing expression.
	if looksLikeStatement(lastLine) {
		// Try to split on ";" and check if the last non-empty segment is an expression.
		parts := strings.Split(lastLine, ";")
		trailingExpr := ""
		var stmtParts []string
		for i := len(parts) - 1; i >= 0; i-- {
			p := strings.TrimSpace(parts[i])
			if p == "" {
				continue
			}
			if !looksLikeStatement(p) {
				trailingExpr = p
				stmtParts = parts[:i]
			}
			break
		}

		if trailingExpr != "" {
			stmts := strings.Join(stmtParts, ";")
			if preamble != "" {
				return "var __result;\n" + preamble + "\n" + stmts + "; __result = (" + trailingExpr + ");"
			}
			return "var __result; " + stmts + "; __result = (" + trailingExpr + ");"
		}

		// Truly all statements — just run them.
		return "var __result; " + code
	}

	if preamble != "" {
		return "var __result;\n" + preamble + "\n__result = (" + lastLine + ");"
	}
	return "var __result = (" + lastLine + ");"
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
func jsValueToGo(v *qjs.Value) any {
	if v == nil || v.IsNull() || v.IsUndefined() {
		return nil
	}
	if v.IsError() {
		exc := v.Exception()
		if exc != nil {
			return exc.Error()
		}
		return v.String()
	}
	// Check objects/arrays before primitives — QJS arrays and objects
	// can also satisfy IsBool()/IsNumber() for internal tag reasons.
	if v.IsArray() || v.IsObject() {
		jsonStr, err := v.JSONStringify()
		if err != nil {
			return v.String()
		}
		var out any
		if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
			return jsonStr
		}
		return out
	}
	if v.IsString() {
		return v.String()
	}
	if v.IsNumber() {
		f := v.Float64()
		// Return int if it's a whole number.
		if f == float64(int64(f)) && f >= -1e15 && f <= 1e15 {
			return int64(f)
		}
		return f
	}
	if v.IsBool() {
		return v.Bool()
	}
	return v.String()
}

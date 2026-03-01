package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func init() {
	register(command{
		noun:        "api",
		verb:        "",
		usage:       "wasmdb api <path> [--method METHOD] [--field key=value ...] [--raw-field key=value ...] [--input FILE] [--header 'Key: Value' ...]",
		description: "Make an authenticated API request",
		run:         runAPI,
	})
}

func runAPI(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb api <path> [flags]")
	}

	apiPath := ctx.args[0]
	// Ensure path starts with /.
	if !strings.HasPrefix(apiPath, "/") {
		apiPath = "/" + apiPath
	}

	method := strings.ToUpper(ctx.flag("method"))
	if method == "" {
		method = ctx.flag("X")
	}

	// Build request body from --field and --raw-field flags.
	fields := ctx.flagAll("field")
	fields = append(fields, ctx.flagAll("F")...)
	rawFields := ctx.flagAll("raw-field")
	inputFile := ctx.flag("input")
	if inputFile == "" {
		inputFile = ctx.flag("i")
	}

	// Custom headers.
	headers := ctx.flagAll("header")
	headers = append(headers, ctx.flagAll("H")...)

	var bodyReader io.Reader
	var contentType string

	hasBody := len(fields) > 0 || len(rawFields) > 0 || inputFile != ""

	// Default method: GET if no body, POST if body.
	if method == "" {
		if hasBody {
			method = "POST"
		} else {
			method = "GET"
		}
	}

	if inputFile != "" {
		if len(fields) > 0 || len(rawFields) > 0 {
			return fmt.Errorf("cannot combine --input with --field or --raw-field")
		}

		var data []byte
		var err error
		if inputFile == "-" {
			data, err = io.ReadAll(ctx.stdin)
		} else {
			data, err = os.ReadFile(inputFile)
		}
		if err != nil {
			return fmt.Errorf("read input: %w", err)
		}
		bodyReader = bytes.NewReader(data)
		contentType = "application/json"
	} else if len(fields) > 0 || len(rawFields) > 0 {
		body := make(map[string]any)

		// --raw-field: always string values.
		for _, rf := range rawFields {
			k, v, ok := strings.Cut(rf, "=")
			if !ok {
				return fmt.Errorf("invalid --raw-field: %q (expected key=value)", rf)
			}
			body[k] = v
		}

		// --field: attempt JSON parsing for the value, fall back to string.
		for _, f := range fields {
			k, v, ok := strings.Cut(f, "=")
			if !ok {
				return fmt.Errorf("invalid --field: %q (expected key=value)", f)
			}
			body[k] = parseFieldValue(v)
		}

		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
		contentType = "application/json"
	}

	serverURL := ctx.serverURL
	url := serverURL + apiPath

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if ctx.token != "" {
		req.Header.Set("Authorization", "Bearer "+ctx.token)
	}

	// Apply custom headers.
	for _, h := range headers {
		k, v, ok := strings.Cut(h, ":")
		if !ok {
			return fmt.Errorf("invalid --header: %q (expected 'Key: Value')", h)
		}
		req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Try to pretty-print JSON.
	var jsonObj any
	if json.Unmarshal(respBody, &jsonObj) == nil {
		pretty, err := json.MarshalIndent(jsonObj, "", "  ")
		if err == nil {
			respBody = pretty
		}
	}

	if resp.StatusCode >= 400 {
		fmt.Fprintf(ctx.stderr, "%s %s\n", resp.Proto, resp.Status)
		ctx.stdout.Write(respBody)
		fmt.Fprintln(ctx.stdout)
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	ctx.stdout.Write(respBody)
	fmt.Fprintln(ctx.stdout)
	return nil
}

// parseFieldValue tries to parse v as JSON (number, bool, array, object),
// falling back to a plain string.
func parseFieldValue(v string) any {
	// Try JSON decode.
	var parsed any
	if err := json.Unmarshal([]byte(v), &parsed); err == nil {
		// Only use the parsed value if it's not a plain string
		// (we want "hello" to stay a string, not require quoting).
		switch parsed.(type) {
		case string:
			// json.Unmarshal succeeded on a quoted string like `"foo"`;
			// return the unquoted version.
			return parsed
		default:
			return parsed
		}
	}
	// Not valid JSON — treat as plain string.
	return v
}

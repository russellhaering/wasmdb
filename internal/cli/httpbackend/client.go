package httpbackend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/russellhaering/wasmdb/internal/cli"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/index"
)

// Client implements cli.Backend via HTTP calls to the wasmdb REST API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New creates a new HTTP backend client.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{},
	}
}

// apiError is the error shape returned by the wasmdb API.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string {
	return e.Message
}

// do performs an HTTP request and decodes the JSON response into result.
// If result is nil, the response body is discarded.
func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var apiErr apiError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return &apiErr
	}

	if result != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

func (c *Client) CreateUser(ctx context.Context, email, password string) (*cli.UserInfo, error) {
	body := struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}{Email: email, Password: password}

	var resp cli.UserInfo
	if err := c.do(ctx, http.MethodPost, "/v1/users", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListUsers(ctx context.Context) ([]cli.UserInfo, error) {
	var resp []cli.UserInfo
	if err := c.do(ctx, http.MethodGet, "/v1/users", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) CreateTable(ctx context.Context, name string, schema *document.Schema) (*cli.TableInfo, error) {
	body := struct {
		Name   string           `json:"name"`
		Schema *document.Schema `json:"schema,omitempty"`
	}{Name: name, Schema: schema}

	var resp cli.TableInfo
	if err := c.do(ctx, http.MethodPost, "/v1/tables", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListTables(ctx context.Context) ([]cli.TableInfo, error) {
	// The API returns [{"name": "..."}] items.
	var resp []cli.TableInfo
	if err := c.do(ctx, http.MethodGet, "/v1/tables", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetTable(ctx context.Context, name string) (*cli.TableInfo, error) {
	var resp cli.TableInfo
	if err := c.do(ctx, http.MethodGet, "/v1/tables/"+name, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteTable(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/tables/"+name, nil, nil)
}

func (c *Client) GetSchema(ctx context.Context, db string) (*document.Schema, error) {
	var resp document.Schema
	if err := c.do(ctx, http.MethodGet, "/v1/tables/"+db+"/schema", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateSchema(ctx context.Context, db string, schema *document.Schema) (*document.Schema, error) {
	var resp document.Schema
	if err := c.do(ctx, http.MethodPut, "/v1/tables/"+db+"/schema", schema, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) CreateDocument(ctx context.Context, db string, doc *document.Document) (*document.Document, error) {
	body := struct {
		ID         string         `json:"id,omitempty"`
		Content    string         `json:"content,omitempty"`
		Attributes map[string]any `json:"attributes,omitempty"`
	}{
		ID:         doc.ID,
		Content:    doc.Content,
		Attributes: doc.Attributes,
	}

	var resp document.Document
	if err := c.do(ctx, http.MethodPost, "/v1/tables/"+db+"/documents", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetDocument(ctx context.Context, db string, id string) (*document.Document, error) {
	var resp document.Document
	if err := c.do(ctx, http.MethodGet, "/v1/tables/"+db+"/documents/"+id, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateDocument(ctx context.Context, db string, id string, doc *document.Document) (*document.Document, error) {
	body := struct {
		Content    string         `json:"content,omitempty"`
		Attributes map[string]any `json:"attributes,omitempty"`
	}{
		Content:    doc.Content,
		Attributes: doc.Attributes,
	}

	var resp document.Document
	if err := c.do(ctx, http.MethodPut, "/v1/tables/"+db+"/documents/"+id, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteDocument(ctx context.Context, db string, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/tables/"+db+"/documents/"+id, nil, nil)
}

func (c *Client) BulkCreateDocuments(ctx context.Context, db string, docs []*document.Document) (*cli.BulkResult, error) {
	var resp cli.BulkResult
	if err := c.do(ctx, http.MethodPost, "/v1/tables/"+db+"/documents/_bulk", docs, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) SearchText(ctx context.Context, db string, query string, limit, offset int) (*cli.TextSearchResult, error) {
	body := struct {
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}{Query: query, Limit: limit, Offset: offset}

	var resp cli.TextSearchResult
	if err := c.do(ctx, http.MethodPost, "/v1/tables/"+db+"/search/text", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) SearchVector(ctx context.Context, db string, query string, k int) ([]*document.Document, error) {
	body := struct {
		Query string `json:"query"`
		K     int    `json:"k"`
	}{Query: query, K: k}

	var resp []*document.Document
	if err := c.do(ctx, http.MethodPost, "/v1/tables/"+db+"/search/vector", body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) SearchAttributes(ctx context.Context, db string, filters []index.Filter, limit, offset int) ([]*document.Document, error) {
	body := struct {
		Filters []index.Filter `json:"filters"`
		Limit   int            `json:"limit"`
		Offset  int            `json:"offset"`
	}{Filters: filters, Limit: limit, Offset: offset}

	var resp []*document.Document
	if err := c.do(ctx, http.MethodPost, "/v1/tables/"+db+"/search/attributes", body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) Health(ctx context.Context) (*cli.HealthStatus, error) {
	var resp cli.HealthStatus
	if err := c.do(ctx, http.MethodGet, "/healthz", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Ready(ctx context.Context) (*cli.HealthStatus, error) {
	var resp cli.HealthStatus
	if err := c.do(ctx, http.MethodGet, "/readyz", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) CreateFunction(ctx context.Context, name, description, code string) (*cli.FunctionInfo, error) {
	body := struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Code        string `json:"code"`
	}{Name: name, Description: description, Code: code}

	var resp cli.FunctionInfo
	if err := c.do(ctx, http.MethodPost, "/v1/functions", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListFunctions(ctx context.Context) ([]cli.FunctionSummary, error) {
	var resp []cli.FunctionSummary
	if err := c.do(ctx, http.MethodGet, "/v1/functions", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetFunction(ctx context.Context, name string) (*cli.FunctionDetail, error) {
	var resp cli.FunctionDetail
	if err := c.do(ctx, http.MethodGet, "/v1/functions/"+name, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateFunction(ctx context.Context, name, code, description string) (*cli.FunctionInfo, error) {
	body := struct {
		Code        string `json:"code"`
		Description string `json:"description"`
	}{Code: code, Description: description}

	var resp cli.FunctionInfo
	if err := c.do(ctx, http.MethodPut, "/v1/functions/"+name, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteFunction(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/functions/"+name, nil, nil)
}

func (c *Client) ExecFunction(ctx context.Context, name string, params map[string]any) (*cli.ExecResult, error) {
	body := struct {
		Params map[string]any `json:"params,omitempty"`
	}{Params: params}

	var resp cli.ExecResult
	if err := c.do(ctx, http.MethodPost, "/v1/functions/"+name+"/exec", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ExecCode(ctx context.Context, code string, params map[string]any) (*cli.ExecResult, error) {
	body := struct {
		Code   string         `json:"code"`
		Params map[string]any `json:"params,omitempty"`
	}{Code: code, Params: params}

	var resp cli.ExecResult
	if err := c.do(ctx, http.MethodPost, "/v1/exec", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ChatStream(ctx context.Context, sessionID, message string) (<-chan cli.ChatEvent, error) {
	body := struct {
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
	}{SessionID: sessionID, Message: message}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		var apiErr apiError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil, &apiErr
	}

	events := make(chan cli.ChatEvent, 64)
	go func() {
		defer close(events)
		defer resp.Body.Close()
		parseSSE(resp.Body, events)
	}()

	return events, nil
}

// parseSSE reads SSE events from r and sends ChatEvents on ch.
func parseSSE(r io.Reader, ch chan<- cli.ChatEvent) {
	buf := make([]byte, 4096)
	var remainder string
	var eventType string

	for {
		n, err := r.Read(buf)
		if n > 0 {
			remainder += string(buf[:n])
			for {
				idx := strings.Index(remainder, "\n")
				if idx == -1 {
					break
				}
				line := remainder[:idx]
				remainder = remainder[idx+1:]

				if strings.HasPrefix(line, "event: ") {
					eventType = strings.TrimSpace(line[7:])
				} else if strings.HasPrefix(line, "data: ") && eventType != "" {
					dataStr := line[6:]
					evt := cli.ChatEvent{Type: eventType}

					switch eventType {
					case "text":
						var d struct {
							Text string `json:"text"`
						}
						if json.Unmarshal([]byte(dataStr), &d) == nil {
							evt.Text = d.Text
						}
					case "tool_start":
						var d struct {
							Tool string `json:"tool"`
							ID   string `json:"id"`
						}
						if json.Unmarshal([]byte(dataStr), &d) == nil {
							evt.Tool = d.Tool
							evt.ToolID = d.ID
						}
					case "tool_result":
						var d struct {
							ID     string `json:"id"`
							Result string `json:"result"`
							Error  bool   `json:"error"`
						}
						if json.Unmarshal([]byte(dataStr), &d) == nil {
							evt.ToolID = d.ID
							evt.Result = d.Result
							evt.ToolError = d.Error
						}
					case "error":
						var d struct {
							Error string `json:"error"`
						}
						if json.Unmarshal([]byte(dataStr), &d) == nil {
							evt.Error = d.Error
						}
					}

					ch <- evt
					eventType = ""

					if evt.Type == "done" || evt.Type == "error" {
						return
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}


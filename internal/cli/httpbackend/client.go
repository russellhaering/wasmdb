package httpbackend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

func (c *Client) CreateDatabase(ctx context.Context, name string, schema *document.Schema) (*cli.DatabaseInfo, error) {
	body := struct {
		Name   string           `json:"name"`
		Schema *document.Schema `json:"schema,omitempty"`
	}{Name: name, Schema: schema}

	var resp cli.DatabaseInfo
	if err := c.do(ctx, http.MethodPost, "/v1/databases", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListDatabases(ctx context.Context) ([]cli.DatabaseInfo, error) {
	// The API returns [{"name": "..."}] items.
	var resp []cli.DatabaseInfo
	if err := c.do(ctx, http.MethodGet, "/v1/databases", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetDatabase(ctx context.Context, name string) (*cli.DatabaseInfo, error) {
	var resp cli.DatabaseInfo
	if err := c.do(ctx, http.MethodGet, "/v1/databases/"+name, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteDatabase(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/databases/"+name, nil, nil)
}

func (c *Client) GetSchema(ctx context.Context, db string) (*document.Schema, error) {
	var resp document.Schema
	if err := c.do(ctx, http.MethodGet, "/v1/databases/"+db+"/schema", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateSchema(ctx context.Context, db string, schema *document.Schema) (*document.Schema, error) {
	var resp document.Schema
	if err := c.do(ctx, http.MethodPut, "/v1/databases/"+db+"/schema", schema, &resp); err != nil {
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
	if err := c.do(ctx, http.MethodPost, "/v1/databases/"+db+"/documents", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetDocument(ctx context.Context, db string, id string) (*document.Document, error) {
	var resp document.Document
	if err := c.do(ctx, http.MethodGet, "/v1/databases/"+db+"/documents/"+id, nil, &resp); err != nil {
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
	if err := c.do(ctx, http.MethodPut, "/v1/databases/"+db+"/documents/"+id, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteDocument(ctx context.Context, db string, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/databases/"+db+"/documents/"+id, nil, nil)
}

func (c *Client) BulkCreateDocuments(ctx context.Context, db string, docs []*document.Document) (*cli.BulkResult, error) {
	var resp cli.BulkResult
	if err := c.do(ctx, http.MethodPost, "/v1/databases/"+db+"/documents/_bulk", docs, &resp); err != nil {
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
	if err := c.do(ctx, http.MethodPost, "/v1/databases/"+db+"/search/text", body, &resp); err != nil {
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
	if err := c.do(ctx, http.MethodPost, "/v1/databases/"+db+"/search/vector", body, &resp); err != nil {
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
	if err := c.do(ctx, http.MethodPost, "/v1/databases/"+db+"/search/attributes", body, &resp); err != nil {
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

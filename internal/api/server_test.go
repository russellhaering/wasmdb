package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

const testToken = "test-token-secret"

func setupTestServer(t *testing.T) *Server {
	t.Helper()
	store := objstore.NewMemoryStore()
	registry := database.NewRegistry(database.RegistryConfig{
		Store:  store,
		Prefix: "test",
	})
	t.Cleanup(func() { registry.Close() })

	if err := registry.EnsureSystemTables(context.Background(), database.SystemTables); err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(context.Background(), ServerConfig{
		ListenAddr: ":0",
		Registry:   registry,
		APITokens:  []string{testToken},
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func authedRequest(t *testing.T, method, path string, body []byte) *http.Request {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	return req
}

func TestCreateAndGetTable(t *testing.T) {
	srv := setupTestServer(t)

	// Create table.
	body, _ := json.Marshal(map[string]any{
		"name": "testdb",
		"schema": map[string]any{
			"fields": []map[string]any{
				{"name": "title", "type": "string", "required": true},
			},
		},
	})

	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "POST", "/v1/tables", body))

	if w.Code != 201 {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Get table.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "GET", "/v1/tables/testdb", nil))

	if w.Code != 200 {
		t.Fatalf("get: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp tableResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Name != "testdb" {
		t.Fatalf("expected name testdb, got %s", resp.Name)
	}
}

func TestDocumentCRUD(t *testing.T) {
	srv := setupTestServer(t)

	// Create table.
	body, _ := json.Marshal(map[string]any{"name": "docs"})
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "POST", "/v1/tables", body))
	if w.Code != 201 {
		t.Fatalf("create table: %d: %s", w.Code, w.Body.String())
	}

	// Wait for the table to be ready.
	time.Sleep(100 * time.Millisecond)

	// Create document.
	body, _ = json.Marshal(map[string]any{
		"id":      "doc-1",
		"content": "# Hello World",
	})
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "POST", "/v1/tables/docs/documents", body))
	if w.Code != 201 {
		t.Fatalf("create doc: %d: %s", w.Code, w.Body.String())
	}

	// Get document.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "GET", "/v1/tables/docs/documents/doc-1", nil))
	if w.Code != 200 {
		t.Fatalf("get doc: %d: %s", w.Code, w.Body.String())
	}

	var doc document.Document
	json.Unmarshal(w.Body.Bytes(), &doc)
	if doc.ID != "doc-1" {
		t.Fatalf("expected doc-1, got %s", doc.ID)
	}
	if doc.Content != "# Hello World" {
		t.Fatalf("expected '# Hello World', got %q", doc.Content)
	}

	// Update document.
	body, _ = json.Marshal(map[string]any{
		"content": "# Updated",
	})
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "PUT", "/v1/tables/docs/documents/doc-1", body))
	if w.Code != 200 {
		t.Fatalf("update doc: %d: %s", w.Code, w.Body.String())
	}

	// Verify update.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "GET", "/v1/tables/docs/documents/doc-1", nil))
	json.Unmarshal(w.Body.Bytes(), &doc)
	if doc.Content != "# Updated" {
		t.Fatalf("expected '# Updated', got %q", doc.Content)
	}

	// Delete document.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "DELETE", "/v1/tables/docs/documents/doc-1", nil))
	if w.Code != 204 {
		t.Fatalf("delete doc: %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "GET", "/v1/tables/docs/documents/doc-1", nil))
	if w.Code != 404 {
		t.Fatalf("expected 404 after delete, got %d", w.Code)
	}
}

func TestListTables(t *testing.T) {
	srv := setupTestServer(t)

	// Create two tables.
	for _, name := range []string{"db1", "db2"} {
		body, _ := json.Marshal(map[string]any{"name": name})
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "POST", "/v1/tables", body))
		if w.Code != 201 {
			t.Fatalf("create %s: %d", name, w.Code)
		}
	}

	// List tables.
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "GET", "/v1/tables", nil))
	if w.Code != 200 {
		t.Fatalf("list: %d: %s", w.Code, w.Body.String())
	}

	var list []map[string]any
	json.Unmarshal(w.Body.Bytes(), &list)

	// Filter out system tables to check only user-created tables.
	var userTables []map[string]any
	for _, tbl := range list {
		if sys, _ := tbl["system"].(bool); !sys {
			userTables = append(userTables, tbl)
		}
	}
	if len(userTables) != 2 {
		t.Fatalf("expected 2 user tables, got %d", len(userTables))
	}
}

func TestHealthEndpoints(t *testing.T) {
	srv := setupTestServer(t)

	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s: expected 200, got %d", path, w.Code)
		}
	}
}

func TestDuplicateTable(t *testing.T) {
	srv := setupTestServer(t)

	body, _ := json.Marshal(map[string]any{"name": "dup"})

	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "POST", "/v1/tables", body))
	if w.Code != 201 {
		t.Fatalf("first create: %d", w.Code)
	}

	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "POST", "/v1/tables", body))
	if w.Code != 409 {
		t.Fatalf("expected 409 for duplicate, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSchemaEndpoints(t *testing.T) {
	srv := setupTestServer(t)

	// Create table with schema.
	body, _ := json.Marshal(map[string]any{
		"name": "schemadb",
		"schema": map[string]any{
			"fields": []map[string]any{
				{"name": "title", "type": "string"},
			},
		},
	})
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "POST", "/v1/tables", body))
	if w.Code != 201 {
		t.Fatalf("create: %d: %s", w.Code, w.Body.String())
	}

	// Get schema.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "GET", "/v1/tables/schemadb/schema", nil))
	if w.Code != 200 {
		t.Fatalf("get schema: %d: %s", w.Code, w.Body.String())
	}

	// Update schema.
	body, _ = json.Marshal(map[string]any{
		"fields": []map[string]any{
			{"name": "title", "type": "string", "required": true},
			{"name": "count", "type": "int"},
		},
	})
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "PUT", "/v1/tables/schemadb/schema", body))
	if w.Code != 200 {
		t.Fatalf("update schema: %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthRejectsNoToken(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest("GET", "/v1/tables", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("expected 401 without token, got %d: %s", w.Code, w.Body.String())
	}

	var errResp APIError
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if errResp.Code != "unauthorized" {
		t.Fatalf("expected code 'unauthorized', got %q", errResp.Code)
	}
}

func TestAuthRejectsWrongToken(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest("GET", "/v1/tables", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("expected 401 with wrong token, got %d", w.Code)
	}
}

func TestAuthAllowsHealthChecksWithoutToken(t *testing.T) {
	srv := setupTestServer(t)

	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s: expected 200 without token, got %d", path, w.Code)
		}
	}
}

func TestDeleteSystemTableForbidden(t *testing.T) {
	srv := setupTestServer(t)

	// Create a system table directly via the registry.
	err := srv.registry.EnsureSystemTables(context.Background(), []database.SystemTableDef{
		{Name: "_sys_protected"},
	})
	if err != nil {
		t.Fatalf("EnsureSystemTables: %v", err)
	}

	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "DELETE", "/v1/tables/_sys_protected", nil))

	if w.Code != 403 {
		t.Fatalf("expected 403 for system table delete, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSystemTableSchemaForbidden(t *testing.T) {
	srv := setupTestServer(t)

	// Create a system table directly via the registry.
	err := srv.registry.EnsureSystemTables(context.Background(), []database.SystemTableDef{
		{Name: "_sys_locked"},
	})
	if err != nil {
		t.Fatalf("EnsureSystemTables: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"fields": []map[string]any{
			{"name": "foo", "type": "string"},
		},
	})
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "PUT", "/v1/tables/_sys_locked/schema", body))

	if w.Code != 403 {
		t.Fatalf("expected 403 for system table schema update, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAndGetUser(t *testing.T) {
	srv := setupTestServer(t)

	// Create user.
	body, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "s3cret",
	})
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "POST", "/v1/users", body))
	if w.Code != 201 {
		t.Fatalf("create user: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	if created["email"] != "alice@example.com" {
		t.Fatalf("expected email alice@example.com, got %v", created["email"])
	}
	if _, ok := created["password_hash"]; ok {
		t.Fatal("password_hash should not be in response")
	}
	if _, ok := created["password"]; ok {
		t.Fatal("password should not be in response")
	}

	userID := created["id"].(string)

	// Get user.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "GET", "/v1/users/"+userID, nil))
	if w.Code != 200 {
		t.Fatalf("get user: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var fetched map[string]any
	json.Unmarshal(w.Body.Bytes(), &fetched)
	if fetched["email"] != "alice@example.com" {
		t.Fatalf("expected email alice@example.com, got %v", fetched["email"])
	}
	if _, ok := fetched["password_hash"]; ok {
		t.Fatal("password_hash should not be in GET response")
	}
}

func TestCreateUserDuplicateEmail(t *testing.T) {
	srv := setupTestServer(t)

	body, _ := json.Marshal(map[string]string{
		"email":    "bob@example.com",
		"password": "pass1",
	})

	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "POST", "/v1/users", body))
	if w.Code != 201 {
		t.Fatalf("first create: expected 201, got %d", w.Code)
	}

	// Second user with same email should succeed (no uniqueness constraint).
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "POST", "/v1/users", body))
	if w.Code != 201 {
		t.Fatalf("second create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteUser(t *testing.T) {
	srv := setupTestServer(t)

	// Create user.
	body, _ := json.Marshal(map[string]string{
		"email":    "del@example.com",
		"password": "pass",
	})
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "POST", "/v1/users", body))
	if w.Code != 201 {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	userID := created["id"].(string)

	// Delete user.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "DELETE", "/v1/users/"+userID, nil))
	if w.Code != 204 {
		t.Fatalf("delete: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Get should return 404.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "GET", "/v1/users/"+userID, nil))
	if w.Code != 404 {
		t.Fatalf("get after delete: expected 404, got %d", w.Code)
	}
}

func TestSystemTableDocumentCRUDBlocked(t *testing.T) {
	srv := setupTestServer(t)

	body, _ := json.Marshal(map[string]any{
		"content": "should not work",
	})

	// POST document to _users should be 403.
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "POST", "/v1/tables/_users/documents", body))
	if w.Code != 403 {
		t.Fatalf("POST documents: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// GET document from _users should be 403.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "GET", "/v1/tables/_users/documents/some-id", nil))
	if w.Code != 403 {
		t.Fatalf("GET document: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// PUT document in _users should be 403.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "PUT", "/v1/tables/_users/documents/some-id", body))
	if w.Code != 403 {
		t.Fatalf("PUT document: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// DELETE document from _users should be 403.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, "DELETE", "/v1/tables/_users/documents/some-id", nil))
	if w.Code != 403 {
		t.Fatalf("DELETE document: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

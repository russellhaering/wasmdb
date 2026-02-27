package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/auth"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

const (
	testUserEmail    = "test@example.com"
	testUserPassword = "test-password"
)

// setupTestServer creates a test server with a seed user and returns the server + session token.
func setupTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	store := objstore.NewMemoryStore()
	registry := database.NewRegistry(database.RegistryConfig{
		Store:  store,
		Prefix: "test",
	})
	t.Cleanup(func() { registry.Close() })

	ctx := context.Background()

	if err := registry.EnsureSystemTables(ctx, database.SystemTables); err != nil {
		t.Fatal(err)
	}

	// Seed a test user.
	if err := auth.SeedUser(ctx, registry, testUserEmail, testUserPassword); err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(ctx, ServerConfig{
		ListenAddr: ":0",
		Registry:   registry,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Log in to get a session token.
	loginBody, _ := json.Marshal(map[string]string{
		"email":    testUserEmail,
		"password": testUserPassword,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("login failed: %d: %s", w.Code, w.Body.String())
	}

	var loginResp struct {
		Token string `json:"token"`
	}
	json.Unmarshal(w.Body.Bytes(), &loginResp)

	return srv, loginResp.Token
}

func authedRequest(t *testing.T, token, method, path string, body []byte) *http.Request {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestCreateAndGetTable(t *testing.T) {
	srv, token := setupTestServer(t)

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
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "POST", "/v1/tables", body))

	if w.Code != 201 {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Get table.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "GET", "/v1/tables/testdb", nil))

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
	srv, token := setupTestServer(t)

	// Create table.
	body, _ := json.Marshal(map[string]any{"name": "docs"})
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "POST", "/v1/tables", body))
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
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "POST", "/v1/tables/docs/documents", body))
	if w.Code != 201 {
		t.Fatalf("create doc: %d: %s", w.Code, w.Body.String())
	}

	// Get document.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "GET", "/v1/tables/docs/documents/doc-1", nil))
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
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "PUT", "/v1/tables/docs/documents/doc-1", body))
	if w.Code != 200 {
		t.Fatalf("update doc: %d: %s", w.Code, w.Body.String())
	}

	// Verify update.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "GET", "/v1/tables/docs/documents/doc-1", nil))
	json.Unmarshal(w.Body.Bytes(), &doc)
	if doc.Content != "# Updated" {
		t.Fatalf("expected '# Updated', got %q", doc.Content)
	}

	// Delete document.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "DELETE", "/v1/tables/docs/documents/doc-1", nil))
	if w.Code != 204 {
		t.Fatalf("delete doc: %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "GET", "/v1/tables/docs/documents/doc-1", nil))
	if w.Code != 404 {
		t.Fatalf("expected 404 after delete, got %d", w.Code)
	}
}

func TestListTables(t *testing.T) {
	srv, token := setupTestServer(t)

	// Create two tables.
	for _, name := range []string{"db1", "db2"} {
		body, _ := json.Marshal(map[string]any{"name": name})
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "POST", "/v1/tables", body))
		if w.Code != 201 {
			t.Fatalf("create %s: %d", name, w.Code)
		}
	}

	// List tables.
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "GET", "/v1/tables", nil))
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
	srv, _ := setupTestServer(t)

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
	srv, token := setupTestServer(t)

	body, _ := json.Marshal(map[string]any{"name": "dup"})

	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "POST", "/v1/tables", body))
	if w.Code != 201 {
		t.Fatalf("first create: %d", w.Code)
	}

	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "POST", "/v1/tables", body))
	if w.Code != 409 {
		t.Fatalf("expected 409 for duplicate, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSchemaEndpoints(t *testing.T) {
	srv, token := setupTestServer(t)

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
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "POST", "/v1/tables", body))
	if w.Code != 201 {
		t.Fatalf("create: %d: %s", w.Code, w.Body.String())
	}

	// Get schema.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "GET", "/v1/tables/schemadb/schema", nil))
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
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "PUT", "/v1/tables/schemadb/schema", body))
	if w.Code != 200 {
		t.Fatalf("update schema: %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthRejectsNoToken(t *testing.T) {
	srv, _ := setupTestServer(t)

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
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/v1/tables", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("expected 401 with wrong token, got %d", w.Code)
	}
}

func TestAuthAllowsHealthChecksWithoutToken(t *testing.T) {
	srv, _ := setupTestServer(t)

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
	srv, token := setupTestServer(t)

	// Create a system table directly via the registry.
	err := srv.registry.EnsureSystemTables(context.Background(), []database.SystemTableDef{
		{Name: "_sys_protected"},
	})
	if err != nil {
		t.Fatalf("EnsureSystemTables: %v", err)
	}

	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "DELETE", "/v1/tables/_sys_protected", nil))

	if w.Code != 403 {
		t.Fatalf("expected 403 for system table delete, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSystemTableSchemaForbidden(t *testing.T) {
	srv, token := setupTestServer(t)

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
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "PUT", "/v1/tables/_sys_locked/schema", body))

	if w.Code != 403 {
		t.Fatalf("expected 403 for system table schema update, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAndGetUser(t *testing.T) {
	srv, token := setupTestServer(t)

	// Create user.
	body, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "s3cret",
	})
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "POST", "/v1/users", body))
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
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "GET", "/v1/users/"+userID, nil))
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
	srv, token := setupTestServer(t)

	body, _ := json.Marshal(map[string]string{
		"email":    "bob@example.com",
		"password": "pass1",
	})

	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "POST", "/v1/users", body))
	if w.Code != 201 {
		t.Fatalf("first create: expected 201, got %d", w.Code)
	}

	// Second user with same email should now be rejected.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "POST", "/v1/users", body))
	if w.Code != 409 {
		t.Fatalf("second create: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteUser(t *testing.T) {
	srv, token := setupTestServer(t)

	// Create user.
	body, _ := json.Marshal(map[string]string{
		"email":    "del@example.com",
		"password": "pass",
	})
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "POST", "/v1/users", body))
	if w.Code != 201 {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	userID := created["id"].(string)

	// Delete user.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "DELETE", "/v1/users/"+userID, nil))
	if w.Code != 204 {
		t.Fatalf("delete: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Get should return 404.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "GET", "/v1/users/"+userID, nil))
	if w.Code != 404 {
		t.Fatalf("get after delete: expected 404, got %d", w.Code)
	}
}

func TestSystemTableDocumentCRUDBlocked(t *testing.T) {
	srv, token := setupTestServer(t)

	body, _ := json.Marshal(map[string]any{
		"content": "should not work",
	})

	// POST document to _users should be 403.
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "POST", "/v1/tables/_users/documents", body))
	if w.Code != 403 {
		t.Fatalf("POST documents: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// GET document from _users should be 403.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "GET", "/v1/tables/_users/documents/some-id", nil))
	if w.Code != 403 {
		t.Fatalf("GET document: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// PUT document in _users should be 403.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "PUT", "/v1/tables/_users/documents/some-id", body))
	if w.Code != 403 {
		t.Fatalf("PUT document: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// DELETE document from _users should be 403.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "DELETE", "/v1/tables/_users/documents/some-id", nil))
	if w.Code != 403 {
		t.Fatalf("DELETE document: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoginSuccess(t *testing.T) {
	srv, _ := setupTestServer(t)

	body, _ := json.Marshal(map[string]string{
		"email":    testUserEmail,
		"password": testUserPassword,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["token"] == nil || resp["token"] == "" {
		t.Fatal("expected token in response")
	}

	// Check cookie was set.
	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == "wasmdb_session" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected wasmdb_session cookie")
	}
}

func TestLoginBadPassword(t *testing.T) {
	srv, _ := setupTestServer(t)

	body, _ := json.Marshal(map[string]string{
		"email":    testUserEmail,
		"password": "wrong-password",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogout(t *testing.T) {
	srv, token := setupTestServer(t)

	// Logout with bearer token.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != 204 {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Token should no longer work.
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "GET", "/v1/tables", nil))
	if w.Code != 401 {
		t.Fatalf("expected 401 after logout, got %d", w.Code)
	}
}

func TestCookieAuth(t *testing.T) {
	srv, token := setupTestServer(t)

	// Make a request using cookie instead of Authorization header.
	req := httptest.NewRequest("GET", "/v1/tables", nil)
	req.AddCookie(&http.Cookie{Name: "wasmdb_session", Value: token})
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200 with cookie auth, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBearerSessionAuth(t *testing.T) {
	srv, token := setupTestServer(t)

	// Bearer token auth should also work with session tokens.
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "GET", "/v1/tables", nil))
	if w.Code != 200 {
		t.Fatalf("expected 200 with bearer session auth, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSeedUser(t *testing.T) {
	store := objstore.NewMemoryStore()
	registry := database.NewRegistry(database.RegistryConfig{
		Store:  store,
		Prefix: "test",
	})
	t.Cleanup(func() { registry.Close() })

	ctx := context.Background()
	if err := registry.EnsureSystemTables(ctx, database.SystemTables); err != nil {
		t.Fatal(err)
	}

	// First seed should create user.
	if err := auth.SeedUser(ctx, registry, "admin@test.com", "pass"); err != nil {
		t.Fatal(err)
	}

	// Second seed should be a no-op (users exist).
	if err := auth.SeedUser(ctx, registry, "other@test.com", "pass"); err != nil {
		t.Fatal(err)
	}

	// Verify only one user exists.
	table, _ := registry.GetTable(ctx, "_users")
	docs, _, _ := table.ListDocuments(ctx, 100, "")
	if len(docs) != 1 {
		t.Fatalf("expected 1 user, got %d", len(docs))
	}
	email, _ := docs[0].Attributes["email"].(string)
	if email != "admin@test.com" {
		t.Fatalf("expected admin@test.com, got %s", email)
	}
}

func TestAuthMe(t *testing.T) {
	srv, token := setupTestServer(t)

	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, authedRequest(t, token, "GET", "/v1/auth/me", nil))
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["email"] != testUserEmail {
		t.Fatalf("expected email %s, got %v", testUserEmail, resp["email"])
	}
}

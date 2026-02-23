package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/storage/objstore"
)

func setupTestServer(t *testing.T) *Server {
	t.Helper()
	store := objstore.NewMemoryStore()
	registry := database.NewRegistry(database.RegistryConfig{
		Store:  store,
		Prefix: "test",
	})
	t.Cleanup(func() { registry.Close() })

	srv, err := NewServer(context.Background(), ServerConfig{
		ListenAddr: ":0",
		Registry:   registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestCreateAndGetDatabase(t *testing.T) {
	srv := setupTestServer(t)

	// Create database.
	body, _ := json.Marshal(map[string]any{
		"name": "testdb",
		"schema": map[string]any{
			"fields": []map[string]any{
				{"name": "title", "type": "string", "required": true},
			},
		},
	})

	req := httptest.NewRequest("POST", "/v1/databases", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Get database.
	req = httptest.NewRequest("GET", "/v1/databases/testdb", nil)
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("get: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp databaseResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Name != "testdb" {
		t.Fatalf("expected name testdb, got %s", resp.Name)
	}
}

func TestDocumentCRUD(t *testing.T) {
	srv := setupTestServer(t)

	// Create database.
	body, _ := json.Marshal(map[string]any{"name": "docs"})
	req := httptest.NewRequest("POST", "/v1/databases", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create db: %d: %s", w.Code, w.Body.String())
	}

	// Wait for the database to be ready.
	time.Sleep(100 * time.Millisecond)

	// Create document.
	body, _ = json.Marshal(map[string]any{
		"id":      "doc-1",
		"content": "# Hello World",
	})
	req = httptest.NewRequest("POST", "/v1/databases/docs/documents", bytes.NewReader(body))
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create doc: %d: %s", w.Code, w.Body.String())
	}

	// Get document.
	req = httptest.NewRequest("GET", "/v1/databases/docs/documents/doc-1", nil)
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
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
	req = httptest.NewRequest("PUT", "/v1/databases/docs/documents/doc-1", bytes.NewReader(body))
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("update doc: %d: %s", w.Code, w.Body.String())
	}

	// Verify update.
	req = httptest.NewRequest("GET", "/v1/databases/docs/documents/doc-1", nil)
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &doc)
	if doc.Content != "# Updated" {
		t.Fatalf("expected '# Updated', got %q", doc.Content)
	}

	// Delete document.
	req = httptest.NewRequest("DELETE", "/v1/databases/docs/documents/doc-1", nil)
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("delete doc: %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted.
	req = httptest.NewRequest("GET", "/v1/databases/docs/documents/doc-1", nil)
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("expected 404 after delete, got %d", w.Code)
	}
}

func TestListDatabases(t *testing.T) {
	srv := setupTestServer(t)

	// Create two databases.
	for _, name := range []string{"db1", "db2"} {
		body, _ := json.Marshal(map[string]any{"name": name})
		req := httptest.NewRequest("POST", "/v1/databases", bytes.NewReader(body))
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		if w.Code != 201 {
			t.Fatalf("create %s: %d", name, w.Code)
		}
	}

	// List databases.
	req := httptest.NewRequest("GET", "/v1/databases", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list: %d: %s", w.Code, w.Body.String())
	}

	var list []map[string]string
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 2 {
		t.Fatalf("expected 2 databases, got %d", len(list))
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

func TestDuplicateDatabase(t *testing.T) {
	srv := setupTestServer(t)

	body, _ := json.Marshal(map[string]any{"name": "dup"})

	req := httptest.NewRequest("POST", "/v1/databases", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("first create: %d", w.Code)
	}

	req = httptest.NewRequest("POST", "/v1/databases", bytes.NewReader(body))
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatalf("expected 409 for duplicate, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSchemaEndpoints(t *testing.T) {
	srv := setupTestServer(t)

	// Create database with schema.
	body, _ := json.Marshal(map[string]any{
		"name": "schemadb",
		"schema": map[string]any{
			"fields": []map[string]any{
				{"name": "title", "type": "string"},
			},
		},
	})
	req := httptest.NewRequest("POST", "/v1/databases", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create: %d: %s", w.Code, w.Body.String())
	}

	// Get schema.
	req = httptest.NewRequest("GET", "/v1/databases/schemadb/schema", nil)
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
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
	req = httptest.NewRequest("PUT", "/v1/databases/schemadb/schema", bytes.NewReader(body))
	w = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("update schema: %d: %s", w.Code, w.Body.String())
	}
}

// Unused import guard.
var _ = context.Background

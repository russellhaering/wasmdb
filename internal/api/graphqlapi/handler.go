package graphqlapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/graphql-go/graphql"
	"github.com/russellhaering/wasmdb/internal/database"
)

// Handler serves the GraphQL endpoint and manages schema lifecycle.
type Handler struct {
	registry *database.Registry

	mu     sync.RWMutex
	schema graphql.Schema
}

type graphqlRequest struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables,omitempty"`
	OperationName string         `json:"operationName,omitempty"`
}

// NewHandler creates a new GraphQL handler and builds the initial schema.
func NewHandler(ctx context.Context, registry *database.Registry) (*Handler, error) {
	h := &Handler{
		registry: registry,
	}
	if err := h.RebuildSchema(ctx); err != nil {
		return nil, err
	}
	return h, nil
}

// RebuildSchema rebuilds the GraphQL schema from the current registry state.
// This should be called after databases are created, deleted, or their schemas change.
func (h *Handler) RebuildSchema(ctx context.Context) error {
	schema, err := buildSchema(ctx, h.registry)
	if err != nil {
		return err
	}

	h.mu.Lock()
	h.schema = schema
	h.mu.Unlock()

	slog.Info("graphql schema rebuilt")
	return nil
}

// ServeHTTP handles GraphQL queries via POST.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req graphqlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	h.mu.RLock()
	schema := h.schema
	h.mu.RUnlock()

	result := graphql.Do(graphql.Params{
		Schema:         schema,
		RequestString:  req.Query,
		VariableValues: req.Variables,
		OperationName:  req.OperationName,
		Context:        r.Context(),
	})

	w.Header().Set("Content-Type", "application/json")
	if len(result.Errors) > 0 {
		w.WriteHeader(http.StatusOK) // GraphQL returns 200 even with errors
	}
	json.NewEncoder(w).Encode(result)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]string{
			{"message": message},
		},
	})
}

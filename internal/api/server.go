package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/russellhaering/wasmdb/internal/api/graphqlapi"
	"github.com/russellhaering/wasmdb/internal/database"
)

// Server is the HTTP server for the WasmDB API.
type Server struct {
	httpServer *http.Server
	registry   *database.Registry
	graphql    *graphqlapi.Handler
}

// ServerConfig configures the API server.
type ServerConfig struct {
	ListenAddr string
	Registry   *database.Registry
}

// NewServer creates a new API server.
func NewServer(ctx context.Context, cfg ServerConfig) (*Server, error) {
	s := &Server{
		registry: cfg.Registry,
	}

	gqlHandler, err := graphqlapi.NewHandler(ctx, cfg.Registry)
	if err != nil {
		return nil, fmt.Errorf("init graphql: %w", err)
	}
	s.graphql = gqlHandler

	// Rebuild the GraphQL schema when databases change.
	cfg.Registry.OnSchemaChange = func(ctx context.Context) {
		if err := gqlHandler.RebuildSchema(ctx); err != nil {
			slog.Error("failed to rebuild graphql schema", "err", err)
		}
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	handler := s.middleware(mux)

	s.httpServer = &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s, nil
}

// Start begins listening for requests.
func (s *Server) Start() error {
	slog.Info("server starting", "addr", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// middleware chains request ID, logging, and panic recovery middleware.
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Request ID.
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = fmt.Sprintf("%d", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-ID", reqID)

		// Panic recovery.
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"request_id", reqID,
					"method", r.Method,
					"path", r.URL.Path,
					"panic", rec,
				)
				writeError(w, ErrInternalServer)
			}
		}()

		// Wrap response writer to capture status.
		wrapped := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(wrapped, r)

		slog.Info("request",
			"request_id", reqID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.status,
			"duration", time.Since(start).String(),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/russellhaering/wasmdb/internal/agent"
	"github.com/russellhaering/wasmdb/internal/api/graphqlapi"
	"github.com/russellhaering/wasmdb/internal/database"
)

// Server is the HTTP server for the WasmDB API.
type Server struct {
	httpServer  *http.Server
	registry    *database.Registry
	graphql     *graphqlapi.Handler
	chatManager *agent.ChatManager
	apiTokens   map[string]struct{}
}

// ServerConfig configures the API server.
type ServerConfig struct {
	ListenAddr      string
	Registry        *database.Registry
	APITokens       []string
	AnthropicAPIKey string
}

// NewServer creates a new API server.
func NewServer(ctx context.Context, cfg ServerConfig) (*Server, error) {
	tokens := make(map[string]struct{}, len(cfg.APITokens))
	for _, t := range cfg.APITokens {
		tokens[t] = struct{}{}
	}

	s := &Server{
		registry:  cfg.Registry,
		apiTokens: tokens,
	}

	gqlHandler, err := graphqlapi.NewHandler(ctx, cfg.Registry)
	if err != nil {
		return nil, fmt.Errorf("init graphql: %w", err)
	}
	s.graphql = gqlHandler

	// Initialize chat agent if Anthropic API key is provided.
	if cfg.AnthropicAPIKey != "" {
		cm, err := agent.NewChatManager(ctx, agent.ChatConfig{
			AnthropicAPIKey: cfg.AnthropicAPIKey,
			Registry:        cfg.Registry,
		})
		if err != nil {
			return nil, fmt.Errorf("init chat agent: %w", err)
		}
		s.chatManager = cm
		slog.Info("chat agent enabled")
	}

	// Rebuild the GraphQL schema when tables change.
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
		WriteTimeout: 5 * time.Minute,
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

// checkAuth validates the bearer token in the Authorization header against the
// configured set of API tokens. Returns true if the token is valid.
func (s *Server) checkAuth(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	token := header[len("Bearer "):]
	for allowed := range s.apiTokens {
		if subtle.ConstantTimeCompare([]byte(token), []byte(allowed)) == 1 {
			return true
		}
	}
	return false
}

// middleware chains request ID, auth, logging, and panic recovery middleware.
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

		// Auth — skip for health check and chat UI page.
		switch r.URL.Path {
		case "/healthz", "/readyz", "/chat":
			// No auth required.
		default:
			if !s.checkAuth(r) {
				writeError(w, ErrUnauthorized)
				return
			}
		}

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

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

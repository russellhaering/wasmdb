package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/russellhaering/wasmdb/internal/agent"
	"github.com/russellhaering/wasmdb/internal/api/graphqlapi"
	"github.com/russellhaering/wasmdb/internal/auth"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/functions"
	"github.com/russellhaering/wasmdb/internal/skills"
)

type sessionContextKeyType struct{}

var sessionContextKey = sessionContextKeyType{}

// SessionFromContext returns the session attached to the request context, if any.
func SessionFromContext(ctx context.Context) *auth.Session {
	s, _ := ctx.Value(sessionContextKey).(*auth.Session)
	return s
}

// Server is the HTTP server for the WasmDB API.
type Server struct {
	httpServer  *http.Server
	registry    *database.Registry
	graphql     *graphqlapi.Handler
	chatManager *agent.ChatManager
	sessions    *auth.SessionManager
	fnEngine    *functions.Engine
	fnStore     *functions.Store
	skillStore  *skills.Store
}

// ServerConfig configures the API server.
type ServerConfig struct {
	ListenAddr      string
	Registry        *database.Registry
	AnthropicAPIKey string
	ChatModel       string
	SubAgentModel   string
}

// NewServer creates a new API server.
func NewServer(ctx context.Context, cfg ServerConfig) (*Server, error) {
	fnEngine := functions.NewEngine(cfg.Registry, 0, 0)
	fnStore := functions.NewStore(cfg.Registry)

	s := &Server{
		registry:   cfg.Registry,
		sessions:   auth.NewSessionManager(cfg.Registry),
		fnEngine:   fnEngine,
		fnStore:    fnStore,
		skillStore: skills.NewStore(cfg.Registry, fnStore, fnEngine),
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
			Model:           cfg.ChatModel,
			SubAgentModel:   cfg.SubAgentModel,
			Registry:        cfg.Registry,
			FnEngine:        s.fnEngine,
			FnStore:         s.fnStore,
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

// authenticateRequest extracts and validates a session token from the request.
// It checks the wasmdb_session cookie first, then falls back to Authorization header.
func (s *Server) authenticateRequest(r *http.Request) (*auth.Session, error) {
	var rawToken string

	// Try cookie first.
	if cookie, err := r.Cookie("wasmdb_session"); err == nil && cookie.Value != "" {
		rawToken = cookie.Value
	}

	// Fall back to Authorization header.
	if rawToken == "" {
		header := r.Header.Get("Authorization")
		if strings.HasPrefix(header, "Bearer ") {
			rawToken = header[len("Bearer "):]
		}
	}

	if rawToken == "" {
		return nil, fmt.Errorf("no session token")
	}

	return s.sessions.ValidateSession(r.Context(), rawToken)
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

		// Auth — skip for health checks, login, and CLI login page.
		switch r.URL.Path {
		case "/healthz", "/readyz", "/v1/auth/login", "/auth/cli-login", "/chat",
			"/v1/auth/device-login", "/v1/auth/device-login/poll", "/v1/auth/device-login/complete":
			// No auth required.
		default:
			session, err := s.authenticateRequest(r)
			if err != nil {
				writeError(w, ErrUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), sessionContextKey, session)
			r = r.WithContext(ctx)
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

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
	"github.com/russellhaering/wasmdb/internal/index"
)

const sessionLifetime = 7 * 24 * time.Hour

// Session represents an authenticated session.
type Session struct {
	ID        string
	UserID    string
	UserEmail string
	ExpiresAt time.Time
}

// SessionManager handles session creation and validation.
type SessionManager struct {
	registry *database.Registry
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(registry *database.Registry) *SessionManager {
	return &SessionManager{registry: registry}
}

// CreateSession generates a new session token and stores its hash.
// The token hash is used as the document ID for direct lookup.
// Returns the raw token (to give to the client) and the session record.
func (m *SessionManager) CreateSession(ctx context.Context, userID, userEmail string) (string, *Session, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	rawToken := base64.RawURLEncoding.EncodeToString(raw)
	tokenHash := hashToken(rawToken)

	expiresAt := time.Now().UTC().Add(sessionLifetime)

	table, err := m.registry.GetTable(ctx, "_sessions")
	if err != nil {
		return "", nil, fmt.Errorf("get sessions table: %w", err)
	}

	doc := &document.Document{
		ID: tokenHash, // Use token hash as document ID for direct lookup.
		Attributes: map[string]any{
			"token_hash": tokenHash,
			"user_id":    userID,
			"user_email": userEmail,
			"expires_at": expiresAt.Format(time.RFC3339),
		},
	}

	if err := table.PutDocument(ctx, doc); err != nil {
		return "", nil, fmt.Errorf("store session: %w", err)
	}

	return rawToken, &Session{
		ID:        doc.ID,
		UserID:    userID,
		UserEmail: userEmail,
		ExpiresAt: expiresAt,
	}, nil
}

// ValidateSession checks a raw token against stored sessions.
// Uses direct document lookup by token hash (the document ID).
func (m *SessionManager) ValidateSession(ctx context.Context, rawToken string) (*Session, error) {
	tokenHash := hashToken(rawToken)

	table, err := m.registry.GetTable(ctx, "_sessions")
	if err != nil {
		return nil, fmt.Errorf("get sessions table: %w", err)
	}

	doc, err := table.GetDocument(ctx, tokenHash)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	if doc == nil {
		return nil, fmt.Errorf("invalid session")
	}

	expiresStr, _ := doc.Attributes["expires_at"].(string)
	expiresAt, err := time.Parse(time.RFC3339, expiresStr)
	if err != nil {
		return nil, fmt.Errorf("parse expiry: %w", err)
	}

	if time.Now().UTC().After(expiresAt) {
		_ = table.DeleteDocument(ctx, doc.ID)
		return nil, fmt.Errorf("session expired")
	}

	userID, _ := doc.Attributes["user_id"].(string)
	userEmail, _ := doc.Attributes["user_email"].(string)

	return &Session{
		ID:        doc.ID,
		UserID:    userID,
		UserEmail: userEmail,
		ExpiresAt: expiresAt,
	}, nil
}

// DeleteSession removes a session by raw token.
func (m *SessionManager) DeleteSession(ctx context.Context, rawToken string) error {
	tokenHash := hashToken(rawToken)

	table, err := m.registry.GetTable(ctx, "_sessions")
	if err != nil {
		return fmt.Errorf("get sessions table: %w", err)
	}

	return table.DeleteDocument(ctx, tokenHash)
}

// DeleteUserSessions removes all sessions for a user.
// Uses the attribute index (eventually consistent) for the user_id lookup.
func (m *SessionManager) DeleteUserSessions(ctx context.Context, userID string) error {
	table, err := m.registry.GetTable(ctx, "_sessions")
	if err != nil {
		return fmt.Errorf("get sessions table: %w", err)
	}

	docs, err := table.SearchAttributes(ctx, []index.Filter{
		{Field: "user_id", Op: index.OpEq, Value: userID},
	}, 1000, 0)
	if err != nil {
		return fmt.Errorf("search user sessions: %w", err)
	}

	for _, doc := range docs {
		if err := table.DeleteDocument(ctx, doc.ID); err != nil {
			return fmt.Errorf("delete session %s: %w", doc.ID, err)
		}
	}

	return nil
}

func hashToken(rawToken string) string {
	h := sha256.Sum256([]byte(rawToken))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

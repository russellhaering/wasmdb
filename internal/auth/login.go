package auth

import (
	"context"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
)

// Login authenticates a user by email and password, returning a session token.
func Login(ctx context.Context, registry *database.Registry, sessions *SessionManager, email, password string) (string, *Session, error) {
	table, err := registry.GetTable(ctx, "_users")
	if err != nil {
		return "", nil, fmt.Errorf("get users table: %w", err)
	}

	doc, err := findUserByEmail(ctx, table, email)
	if err != nil {
		return "", nil, err
	}
	if doc == nil {
		return "", nil, fmt.Errorf("invalid email or password")
	}

	hash, _ := doc.Attributes["password_hash"].(string)

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return "", nil, fmt.Errorf("invalid email or password")
	}

	userEmail, _ := doc.Attributes["email"].(string)
	return sessions.CreateSession(ctx, doc.ID, userEmail)
}

// findUserByEmail scans the _users table and returns the first user with the given email.
// This avoids relying on the async attribute index.
func findUserByEmail(ctx context.Context, table *database.Table, email string) (*document.Document, error) {
	afterKey := ""
	for {
		docs, hasMore, err := table.ListDocuments(ctx, 100, afterKey)
		if err != nil {
			return nil, fmt.Errorf("list users: %w", err)
		}

		for _, doc := range docs {
			if e, _ := doc.Attributes["email"].(string); e == email {
				return doc, nil
			}
		}

		if !hasMore || len(docs) == 0 {
			break
		}
		afterKey = docs[len(docs)-1].ID
	}

	return nil, nil
}

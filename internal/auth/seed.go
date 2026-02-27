package auth

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/crypto/bcrypt"

	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/wasmdb/internal/document"
)

// SeedUser creates an initial user if the _users table is empty.
// This is intended for bootstrapping the first deployment.
func SeedUser(ctx context.Context, registry *database.Registry, email, password string) error {
	table, err := registry.GetTable(ctx, "_users")
	if err != nil {
		return fmt.Errorf("get users table: %w", err)
	}

	docs, _, err := table.ListDocuments(ctx, 1, "")
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}

	if len(docs) > 0 {
		slog.Info("seed user skipped, users already exist")
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	doc := &document.Document{
		Attributes: map[string]any{
			"email":         email,
			"password_hash": string(hash),
		},
	}

	if err := table.PutDocument(ctx, doc); err != nil {
		return fmt.Errorf("create seed user: %w", err)
	}

	slog.Info("seed user created", "email", email)
	return nil
}

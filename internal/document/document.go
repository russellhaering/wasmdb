package document

import "time"

// Document represents a single document in the database.
type Document struct {
	ID         string         `json:"id"`
	Content    string         `json:"content,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Embedding  []float32      `json:"embedding,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	Version    uint64         `json:"version"`
}

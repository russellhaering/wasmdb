package document

import (
	"testing"
	"time"
)

func TestSerializeRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Nanosecond)
	doc := &Document{
		ID:      "doc-123",
		Content: "# Hello World\n\nThis is markdown.",
		Attributes: map[string]any{
			"title":  "Hello",
			"count":  float64(42),
			"active": true,
		},
		Embedding: []float32{0.1, 0.2, 0.3, -0.5},
		CreatedAt: now,
		UpdatedAt: now.Add(time.Hour),
		Version:   7,
	}

	data, err := Serialize(doc)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	got, err := Deserialize(data)
	if err != nil {
		t.Fatalf("Deserialize: %v", err)
	}

	// ID is not stored in the value.
	got.ID = doc.ID

	if got.ID != doc.ID {
		t.Errorf("ID: got %q, want %q", got.ID, doc.ID)
	}
	if got.Content != doc.Content {
		t.Errorf("Content: got %q, want %q", got.Content, doc.Content)
	}
	if got.Version != doc.Version {
		t.Errorf("Version: got %d, want %d", got.Version, doc.Version)
	}
	if !got.CreatedAt.Equal(doc.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, doc.CreatedAt)
	}
	if !got.UpdatedAt.Equal(doc.UpdatedAt) {
		t.Errorf("UpdatedAt: got %v, want %v", got.UpdatedAt, doc.UpdatedAt)
	}
	if len(got.Embedding) != len(doc.Embedding) {
		t.Fatalf("Embedding len: got %d, want %d", len(got.Embedding), len(doc.Embedding))
	}
	for i := range got.Embedding {
		if got.Embedding[i] != doc.Embedding[i] {
			t.Errorf("Embedding[%d]: got %f, want %f", i, got.Embedding[i], doc.Embedding[i])
		}
	}
}

func TestSerializeNoOptionalFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Nanosecond)
	doc := &Document{
		CreatedAt: now,
		UpdatedAt: now,
		Version:   1,
	}

	data, err := Serialize(doc)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	got, err := Deserialize(data)
	if err != nil {
		t.Fatalf("Deserialize: %v", err)
	}

	if got.Content != "" {
		t.Errorf("Content: got %q, want empty", got.Content)
	}
	if got.Attributes != nil {
		t.Errorf("Attributes: got %v, want nil", got.Attributes)
	}
	if got.Embedding != nil {
		t.Errorf("Embedding: got %v, want nil", got.Embedding)
	}
}

func TestTombstone(t *testing.T) {
	data := SerializeTombstone()
	if !IsTombstone(data) {
		t.Error("expected tombstone")
	}

	_, err := Deserialize(data)
	if err == nil {
		t.Error("expected error deserializing tombstone")
	}
}

func TestSerializeEmpty(t *testing.T) {
	if IsTombstone(nil) {
		t.Error("nil should not be tombstone")
	}
	if IsTombstone([]byte{}) {
		t.Error("empty should not be tombstone")
	}
}

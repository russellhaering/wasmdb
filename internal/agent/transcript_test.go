package agent

import (
	"context"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/russellhaering/wasmdb/internal/database"
	"github.com/russellhaering/moraine/objstore"
)

// TestGetSessionTranscript saves a crafted history (user text, assistant text +
// tool_use, an intervening tool_result-only user message, then a final
// assistant text) and asserts the flattened transcript is ordered correctly:
// tool_result blocks are dropped and each tool_use becomes its own tool item.
func TestGetSessionTranscript(t *testing.T) {
	reg := database.NewRegistry(database.RegistryConfig{
		Store:    objstore.NewMemoryStore(),
		Prefix:   "test",
		CacheDir: t.TempDir(),
	})
	t.Cleanup(func() { reg.Close() })

	ctx := context.Background()
	if err := reg.EnsureSystemTables(ctx, database.SystemTables); err != nil {
		t.Fatalf("ensure system tables: %v", err)
	}

	cm := &ChatManager{registry: reg, sessions: make(map[string]*chatSession)}

	history := []anthropic.MessageParam{
		{Role: anthropic.MessageParamRoleUser, Content: []anthropic.ContentBlockParamUnion{
			anthropic.NewTextBlock("hello there"),
		}},
		{Role: anthropic.MessageParamRoleAssistant, Content: []anthropic.ContentBlockParamUnion{
			anthropic.NewTextBlock("let me look that up"),
			anthropic.NewToolUseBlock("tool-1", map[string]any{"q": "x"}, "search_text"),
		}},
		// Pure tool_result user message: must produce no transcript item.
		{Role: anthropic.MessageParamRoleUser, Content: []anthropic.ContentBlockParamUnion{
			anthropic.NewToolResultBlock("tool-1", "some result", false),
		}},
		{Role: anthropic.MessageParamRoleAssistant, Content: []anthropic.ContentBlockParamUnion{
			anthropic.NewTextBlock("found it"),
		}},
	}

	cs := &chatSession{userID: "user-123", history: history}
	if err := cm.saveSession(ctx, "sess-1", cs); err != nil {
		t.Fatalf("saveSession: %v", err)
	}

	ownerID, items, err := cm.GetSessionTranscript(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSessionTranscript: %v", err)
	}
	if ownerID != "user-123" {
		t.Fatalf("owner: expected user-123, got %q", ownerID)
	}

	want := []TranscriptItem{
		{Role: "user", Text: "hello there"},
		{Role: "assistant", Text: "let me look that up"},
		{Role: "tool", Tool: "search_text"},
		{Role: "assistant", Text: "found it"},
	}
	if len(items) != len(want) {
		t.Fatalf("expected %d items, got %d: %+v", len(want), len(items), items)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Fatalf("item %d: expected %+v, got %+v", i, want[i], items[i])
		}
	}

	// Unknown session: nil items, nil error (caller 404s).
	owner2, items2, err := cm.GetSessionTranscript(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("GetSessionTranscript(unknown): unexpected error %v", err)
	}
	if owner2 != "" || items2 != nil {
		t.Fatalf("unknown session: expected empty owner and nil items, got %q %+v", owner2, items2)
	}
}

func TestStripUserPreamble(t *testing.T) {
	cases := map[string]string{
		"Authenticated user_id: 01ABC\n\nUser request:\nhello":                        "hello",
		"Authenticated user_id: 01ABC\n\nMemories:\n- x\nUser request:\nmulti\nline": "multi\nline",
		"plain message with no preamble":                                             "plain message with no preamble",
		"User request:\nnot actually prefixed":                                       "User request:\nnot actually prefixed",
	}
	for in, want := range cases {
		if got := stripUserPreamble(in); got != want {
			t.Errorf("stripUserPreamble(%q) = %q, want %q", in, got, want)
		}
	}
}

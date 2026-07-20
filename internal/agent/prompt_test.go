package agent

import (
	"strings"
	"testing"

	"github.com/russellhaering/wasmdb/internal/surface"
)

// TestSystemPromptEmbedsSurfaceSpec verifies the chat system prompt is driven by
// the canonical surface spec and no longer describes the removed A2UI format.
func TestSystemPromptEmbedsSurfaceSpec(t *testing.T) {
	// A distinctive line that only exists in surface.SpecMarkdown().
	const distinctive = "There is no string templating"
	if !strings.Contains(surface.SpecMarkdown(), distinctive) {
		t.Fatalf("test anchor %q not present in SpecMarkdown; update the test", distinctive)
	}
	if !strings.Contains(systemPrompt, distinctive) {
		t.Errorf("system prompt does not embed the surface spec (missing %q)", distinctive)
	}
	if !strings.Contains(systemPrompt, "# Surface UI Format") {
		t.Errorf("system prompt does not contain the surface spec heading")
	}

	if !strings.Contains(systemPrompt, "surface-ref") {
		t.Errorf("system prompt does not document the surface-ref embed fence")
	}

	if strings.Contains(strings.ToLower(systemPrompt), "a2ui") {
		t.Errorf("system prompt still mentions the removed A2UI format")
	}
}

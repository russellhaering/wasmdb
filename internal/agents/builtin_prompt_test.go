package agents

import (
	"strings"
	"testing"

	"github.com/russellhaering/wasmdb/internal/surface"
)

// TestUIBuilderPromptEmbedsSurfaceSpec verifies the ui-builder prompt is driven
// by the canonical surface spec and no longer describes the removed A2UI format.
func TestUIBuilderPromptEmbedsSurfaceSpec(t *testing.T) {
	const distinctive = "There is no string templating"
	if !strings.Contains(surface.SpecMarkdown(), distinctive) {
		t.Fatalf("test anchor %q not present in SpecMarkdown; update the test", distinctive)
	}
	if !strings.Contains(uiBuilderPrompt, distinctive) {
		t.Errorf("ui-builder prompt does not embed the surface spec (missing %q)", distinctive)
	}
	if !strings.Contains(uiBuilderPrompt, "# Surface UI Format") {
		t.Errorf("ui-builder prompt does not contain the surface spec heading")
	}

	// It must describe the scaffold/provenance reality and the exec_action loop.
	if !strings.Contains(uiBuilderPrompt, "scaffold") {
		t.Errorf("ui-builder prompt does not mention scaffold pages")
	}
	if !strings.Contains(uiBuilderPrompt, "exec_action") {
		t.Errorf("ui-builder prompt does not mention the exec_action verification loop")
	}

	if strings.Contains(strings.ToLower(uiBuilderPrompt), "a2ui") {
		t.Errorf("ui-builder prompt still mentions the removed A2UI format")
	}
	// The old template-variable engine is gone; the prompt must not describe it.
	if strings.Contains(uiBuilderPrompt, "{{") {
		t.Errorf("ui-builder prompt still describes the removed {{key}} template engine")
	}
}

package api

import (
	"testing"
)

func TestA2UISplitter_PlainText(t *testing.T) {
	var s a2uiSplitter
	chunks := s.Write("hello world")
	if len(chunks) != 1 || chunks[0].Text != "hello world" {
		t.Fatalf("expected single text chunk 'hello world', got %+v", chunks)
	}
}

func TestA2UISplitter_SingleArtifact(t *testing.T) {
	var s a2uiSplitter
	var all []a2uiChunk

	all = append(all, s.Write("before ")...)
	all = append(all, s.Write("```a2ui\n{\"components\":[]}\n```\nafter")...)
	all = append(all, s.Flush()...)

	// Should have: text("before "), artifact, text("after")
	wantText := []string{"before ", "", "after"}
	wantArtifact := []string{"", `{"components":[]}`, ""}

	if len(all) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %+v", len(all), all)
	}
	for i, c := range all {
		if c.Text != wantText[i] {
			t.Errorf("chunk[%d].Text = %q, want %q", i, c.Text, wantText[i])
		}
		if c.Artifact != wantArtifact[i] {
			t.Errorf("chunk[%d].Artifact = %q, want %q", i, c.Artifact, wantArtifact[i])
		}
	}
}

func TestA2UISplitter_StreamingCharByChar(t *testing.T) {
	input := "hi ```a2ui\n{\"x\":1}\n``` bye"
	var s a2uiSplitter
	var all []a2uiChunk

	for _, ch := range input {
		all = append(all, s.Write(string(ch))...)
	}
	all = append(all, s.Flush()...)

	var texts, artifacts []string
	for _, c := range all {
		if c.Text != "" {
			texts = append(texts, c.Text)
		}
		if c.Artifact != "" {
			artifacts = append(artifacts, c.Artifact)
		}
	}

	if len(artifacts) != 1 || artifacts[0] != `{"x":1}` {
		t.Errorf("artifacts = %v, want ['{\"x\":1}']", artifacts)
	}
	// Should have some text before and after
	joined := ""
	for _, t := range texts {
		joined += t
	}
	if joined != "hi  bye" {
		t.Errorf("joined text = %q, want 'hi  bye'", joined)
	}
}

func TestA2UISplitter_MultipleArtifacts(t *testing.T) {
	var s a2uiSplitter
	var all []a2uiChunk

	all = append(all, s.Write("```a2ui\n{\"a\":1}\n```\ntext\n```a2ui\n{\"b\":2}\n```")...)
	all = append(all, s.Flush()...)

	var artifacts []string
	for _, c := range all {
		if c.Artifact != "" {
			artifacts = append(artifacts, c.Artifact)
		}
	}

	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d: %+v", len(artifacts), all)
	}
	if artifacts[0] != `{"a":1}` || artifacts[1] != `{"b":2}` {
		t.Errorf("artifacts = %v", artifacts)
	}
}

func TestA2UISplitter_PartialFenceBuffered(t *testing.T) {
	// Ensure a partial fence ("``") at the end of a write doesn't get flushed as text.
	var s a2uiSplitter
	chunks := s.Write("hello ``")
	// Should flush "hello " but hold "``" back.
	if len(chunks) != 1 || chunks[0].Text != "hello " {
		t.Fatalf("expected 'hello ', got %+v", chunks)
	}
	// Now complete the fence.
	chunks = s.Write("`a2ui\n{\"z\":0}\n```")
	var hasArtifact bool
	for _, c := range chunks {
		if c.Artifact == `{"z":0}` {
			hasArtifact = true
		}
	}
	if !hasArtifact {
		t.Fatalf("expected artifact, got %+v", chunks)
	}
}

func TestA2UISplitter_UnclosedFenceFlush(t *testing.T) {
	var s a2uiSplitter
	s.Write("```a2ui\n{\"x\":1}")
	// No closing fence, flush should emit as text.
	chunks := s.Flush()
	if len(chunks) != 1 || chunks[0].Text == "" {
		t.Fatalf("expected text chunk from unclosed fence flush, got %+v", chunks)
	}
}

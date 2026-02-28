package api

import (
	"strings"
)

const (
	a2uiOpenFence  = "```a2ui"
	a2uiCloseFence = "\n```"
)

// a2uiChunk is a piece of output from the splitter.
type a2uiChunk struct {
	Text     string // non-empty for plain text
	Artifact string // non-empty for a2ui JSON (without fences)
}

// a2uiSplitter buffers streaming text deltas and splits them into
// plain-text chunks and a2ui artifact chunks. It detects ```a2ui ... ```
// fences and emits artifacts when complete.
type a2uiSplitter struct {
	buf     strings.Builder
	inFence bool
}

// Write accepts a text delta and returns any chunks that can be emitted.
// Plain text is flushed eagerly (up to any potential fence start).
// Artifact JSON is emitted only when the closing fence is found.
func (s *a2uiSplitter) Write(text string) []a2uiChunk {
	s.buf.WriteString(text)
	return s.drain()
}

// Flush returns any remaining buffered content as a text chunk.
// Call this when the stream ends (done/error).
func (s *a2uiSplitter) Flush() []a2uiChunk {
	if s.buf.Len() == 0 {
		return nil
	}
	remaining := s.buf.String()
	s.buf.Reset()
	s.inFence = false
	if strings.TrimSpace(remaining) == "" {
		return nil
	}
	return []a2uiChunk{{Text: remaining}}
}

func (s *a2uiSplitter) drain() []a2uiChunk {
	var chunks []a2uiChunk

	for {
		content := s.buf.String()

		if !s.inFence {
			// Look for the opening fence.
			idx := strings.Index(content, a2uiOpenFence)
			if idx == -1 {
				// No fence found. Emit text up to the point where a partial
				// fence match could start (keep a suffix that could be a prefix
				// of the open fence).
				safe := safeFlushLen(content, a2uiOpenFence)
				if safe > 0 {
					chunks = append(chunks, a2uiChunk{Text: content[:safe]})
					s.buf.Reset()
					s.buf.WriteString(content[safe:])
				}
				return chunks
			}

			// Found opening fence. Emit text before it.
			if idx > 0 {
				before := content[:idx]
				if strings.TrimSpace(before) != "" {
					chunks = append(chunks, a2uiChunk{Text: before})
				}
			}
			// Buffer now starts from after the open fence.
			s.buf.Reset()
			s.buf.WriteString(content[idx+len(a2uiOpenFence):])
			s.inFence = true
			continue // try to find close fence immediately
		}

		// Inside a fence — look for closing fence.
		idx := strings.Index(content, a2uiCloseFence)
		if idx == -1 {
			// Not yet closed, keep buffering.
			return chunks
		}

		// Found closing fence. Extract the JSON.
		jsonStr := strings.TrimSpace(content[:idx])
		if jsonStr != "" {
			chunks = append(chunks, a2uiChunk{Artifact: jsonStr})
		}

		// Advance past the closing fence.
		after := content[idx+len(a2uiCloseFence):]
		// Skip optional leading newline after closing fence.
		if len(after) > 0 && after[0] == '\n' {
			after = after[1:]
		}
		s.buf.Reset()
		s.buf.WriteString(after)
		s.inFence = false
		continue // there might be more fences
	}
}

// safeFlushLen returns the number of bytes from the start of content that
// can be safely flushed without splitting a potential partial match of marker.
// It keeps up to len(marker)-1 trailing bytes that could be a prefix of marker.
func safeFlushLen(content, marker string) int {
	// Check suffixes from longest to shortest; return on the first (longest)
	// suffix of content that is also a prefix of marker.
	maxCheck := len(marker) - 1
	if maxCheck > len(content) {
		maxCheck = len(content)
	}
	for i := maxCheck; i >= 1; i-- {
		suffix := content[len(content)-i:]
		if strings.HasPrefix(marker, suffix) {
			return len(content) - i
		}
	}
	return len(content)
}

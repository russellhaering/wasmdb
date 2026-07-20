package cli

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func init() {
	register(command{
		noun:        "chat",
		verb:        "",
		usage:       "wasmdb chat [--session <id>]",
		description: "Interactive chat with the WasmDB agent",
		run:         runChat,
	})
}

func runChat(ctx *cmdContext) error {
	sessionID := ctx.flag("session")
	if sessionID == "" {
		sessionID = newSessionID()
	}

	scanner := bufio.NewScanner(ctx.stdin)
	fmt.Fprint(ctx.stdout, "> ")

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Fprint(ctx.stdout, "> ")
			continue
		}
		if line == "exit" || line == "quit" {
			return nil
		}

		events, err := ctx.backend.ChatStream(ctx, sessionID, line)
		if err != nil {
			fmt.Fprintf(ctx.stderr, "error: %s\n", err)
			fmt.Fprint(ctx.stdout, "> ")
			continue
		}

		var textBuf strings.Builder
		toolNames := make(map[string]string) // id -> name

		for evt := range events {
			switch evt.Type {
			case "text":
				textBuf.WriteString(evt.Text)
				// Stream text immediately for responsiveness.
				fmt.Fprint(ctx.stdout, evt.Text)

			case "tool_start":
				// Flush any text before tool output.
				toolNames[evt.ToolID] = evt.Tool
				fmt.Fprintf(ctx.stdout, "\033[2m[%s ...]\033[0m\n", evt.Tool)

			case "tool_result":
				name := toolNames[evt.ToolID]
				if name == "" {
					name = "tool"
				}
				if evt.ToolError {
					fmt.Fprintf(ctx.stdout, "\033[2m[\033[31m%s error\033[0;2m]\033[0m\n", name)
				} else {
					fmt.Fprintf(ctx.stdout, "\033[2m[%s done]\033[0m\n", name)
				}
				// Reset text buffer for post-tool text.
				textBuf.Reset()

			case "error":
				fmt.Fprintf(ctx.stderr, "\033[31merror: %s\033[0m\n", evt.Error)

			case "done":
				fullText := textBuf.String()
				// A completed page reference is a tiny, complete fenced block;
				// print a notice pointing at /ui instead of dumping the fence.
				// (The interactive embed lives in the web chat.)
				if pages := extractSurfaceRefPages(fullText); len(pages) > 0 {
					base := strings.TrimRight(ctx.serverURL, "/")
					for _, page := range pages {
						if base != "" {
							fmt.Fprintf(ctx.stdout, "\n[ui page %q updated — view at %s/ui]\n", page, base)
						} else {
							fmt.Fprintf(ctx.stdout, "\n[ui page %q updated — view at /ui]\n", page)
						}
					}
				}
				// Check accumulated text for A2UI blocks and re-render (legacy path;
				// removed in Phase 7).
				if strings.Contains(fullText, "```a2ui") {
					// Move cursor up to overwrite streamed text.
					// Count lines we streamed.
					lineCount := strings.Count(fullText, "\n") + 1
					for i := 0; i < lineCount; i++ {
						fmt.Fprint(ctx.stdout, "\033[A\033[2K")
					}
					// Render with A2UI.
					segments := ExtractA2UIBlocks(fullText)
					for _, seg := range segments {
						if seg.IsA2UI {
							if err := RenderA2UI(ctx.stdout, seg.Text); err != nil {
								fmt.Fprint(ctx.stdout, seg.Text)
							}
						} else {
							fmt.Fprint(ctx.stdout, seg.Text)
						}
					}
					if !strings.HasSuffix(fullText, "\n") {
						fmt.Fprintln(ctx.stdout)
					}
				}
			}
		}

		// Ensure newline after response.
		text := textBuf.String()
		if text != "" && !strings.HasSuffix(text, "\n") && !strings.Contains(text, "```a2ui") {
			fmt.Fprintln(ctx.stdout)
		}

		fmt.Fprintln(ctx.stdout)
		fmt.Fprint(ctx.stdout, "> ")
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading input: %w", err)
	}
	return nil
}

// extractSurfaceRefPages scans completed assistant text for ```surface-ref
// fenced blocks and returns the "page" value from each. Parsing happens on
// message completion (the block is small and complete), so no streaming fence
// parser is needed.
func extractSurfaceRefPages(text string) []string {
	const fence = "```surface-ref"
	var pages []string
	rest := text
	for {
		i := strings.Index(rest, fence)
		if i < 0 {
			break
		}
		rest = rest[i+len(fence):]
		end := strings.Index(rest, "```")
		if end < 0 {
			break
		}
		body := rest[:end]
		rest = rest[end+3:]

		var ref struct {
			Page string `json:"page"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &ref); err == nil && ref.Page != "" {
			pages = append(pages, ref.Page)
		}
	}
	return pages
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// isTerminal checks if the given file is a terminal.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

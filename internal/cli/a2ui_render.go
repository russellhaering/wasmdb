package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/russellhaering/wasmdb/internal/a2ui"
)

// termWidth returns the terminal width, defaulting to 80.
func termWidth() int {
	if cols := os.Getenv("COLUMNS"); cols != "" {
		if n, err := strconv.Atoi(cols); err == nil && n > 0 {
			return n
		}
	}
	return 80
}

// RenderA2UI parses a JSON A2UI surface and writes ANSI-formatted output.
func RenderA2UI(w io.Writer, jsonStr string) error {
	var s a2ui.Surface
	if err := json.Unmarshal([]byte(jsonStr), &s); err != nil {
		return err
	}
	index := make(map[string]*a2ui.Component, len(s.Components))
	for i := range s.Components {
		index[s.Components[i].ID] = &s.Components[i]
	}
	root, ok := index["root"]
	if !ok {
		return fmt.Errorf("no root component")
	}
	renderComp(w, root, index, termWidth())
	return nil
}

func renderComp(w io.Writer, c *a2ui.Component, index map[string]*a2ui.Component, width int) {
	switch c.Type {
	case "Column":
		renderColumnCLI(w, c, index, width)
	case "Row":
		renderRowCLI(w, c, index, width)
	case "DataTable":
		renderDataTableCLI(w, c, width)
	case "Card":
		renderCardCLI(w, c, index, width)
	case "Text":
		renderTextCLI(w, c)
	case "Divider":
		renderDividerCLI(w, width)
	}
}

func renderColumnCLI(w io.Writer, c *a2ui.Component, index map[string]*a2ui.Component, width int) {
	for _, childID := range c.Children {
		if child, ok := index[childID]; ok {
			renderComp(w, child, index, width)
		}
	}
}

func renderRowCLI(w io.Writer, c *a2ui.Component, index map[string]*a2ui.Component, width int) {
	// Simple implementation: render children separated by " │ ".
	// For a more sophisticated approach we'd need to capture output and lay out side-by-side.
	for i, childID := range c.Children {
		if i > 0 {
			fmt.Fprint(w, " │ ")
		}
		if child, ok := index[childID]; ok {
			renderComp(w, child, index, width)
		}
	}
}

func renderDataTableCLI(w io.Writer, c *a2ui.Component, maxWidth int) {
	props, err := a2ui.ParseDataTableProps(*c)
	if err != nil {
		return
	}

	if len(props.Columns) == 0 {
		return
	}

	// Compute column widths.
	widths := make([]int, len(props.Columns))
	for i, col := range props.Columns {
		label := col.Label
		if label == "" {
			label = col.Key
		}
		widths[i] = runeLen(label)
	}
	for _, row := range props.Rows {
		for i, col := range props.Columns {
			val := fmtVal(row[col.Key])
			if l := runeLen(val); l > widths[i] {
				widths[i] = l
			}
		}
	}

	// Clamp total width.
	totalWidth := 1 // leading │
	for _, w := range widths {
		totalWidth += w + 3 // " val │" = content + padding + border
	}
	if totalWidth > maxWidth && maxWidth > 0 {
		// Shrink the widest column.
		excess := totalWidth - maxWidth
		maxIdx := 0
		for i, w := range widths {
			if w > widths[maxIdx] {
				maxIdx = i
				_ = w
			}
		}
		if widths[maxIdx] > excess+3 {
			widths[maxIdx] -= excess
		}
	}

	if props.Caption != "" {
		fmt.Fprintf(w, "  %s\n", props.Caption)
	}

	// Top border: ┌──┬──┐
	fmt.Fprint(w, "┌")
	for i, cw := range widths {
		fmt.Fprint(w, strings.Repeat("─", cw+2))
		if i < len(widths)-1 {
			fmt.Fprint(w, "┬")
		}
	}
	fmt.Fprintln(w, "┐")

	// Header row.
	fmt.Fprint(w, "│")
	for i, col := range props.Columns {
		label := col.Label
		if label == "" {
			label = col.Key
		}
		fmt.Fprintf(w, " \033[1m%-*s\033[0m │", widths[i], truncate(label, widths[i]))
	}
	fmt.Fprintln(w)

	// Header separator: ├──┼──┤
	fmt.Fprint(w, "├")
	for i, cw := range widths {
		fmt.Fprint(w, strings.Repeat("─", cw+2))
		if i < len(widths)-1 {
			fmt.Fprint(w, "┼")
		}
	}
	fmt.Fprintln(w, "┤")

	// Data rows.
	for _, row := range props.Rows {
		fmt.Fprint(w, "│")
		for i, col := range props.Columns {
			val := fmtVal(row[col.Key])
			fmt.Fprintf(w, " %-*s │", widths[i], truncate(val, widths[i]))
		}
		fmt.Fprintln(w)
	}

	// Bottom border: └──┴──┘
	fmt.Fprint(w, "└")
	for i, cw := range widths {
		fmt.Fprint(w, strings.Repeat("─", cw+2))
		if i < len(widths)-1 {
			fmt.Fprint(w, "┴")
		}
	}
	fmt.Fprintln(w, "┘")
}

func renderCardCLI(w io.Writer, c *a2ui.Component, index map[string]*a2ui.Component, maxWidth int) {
	p := c.Properties
	title, _ := p["title"].(string)

	// Collect child text lines to compute width.
	var lines []string
	for _, childID := range c.Children {
		child, ok := index[childID]
		if !ok || child.Type != "Text" {
			continue
		}
		cp := child.Properties
		label, _ := cp["label"].(string)
		text, _ := cp["text"].(string)
		if label != "" {
			lines = append(lines, label+": "+text)
		} else {
			lines = append(lines, text)
		}
	}

	// Determine box width.
	boxWidth := runeLen(title) + 4
	for _, l := range lines {
		if lw := runeLen(l) + 4; lw > boxWidth {
			boxWidth = lw
		}
	}
	if boxWidth > maxWidth && maxWidth > 0 {
		boxWidth = maxWidth
	}
	if boxWidth < 10 {
		boxWidth = 10
	}
	innerWidth := boxWidth - 2

	// Top border with title.
	if title != "" {
		tLen := runeLen(title)
		padRight := innerWidth - tLen - 3
		if padRight < 1 {
			padRight = 1
		}
		fmt.Fprintf(w, "┌─ \033[1;36m%s\033[0m %s┐\n", truncate(title, innerWidth-3), strings.Repeat("─", padRight))
	} else {
		fmt.Fprintf(w, "┌%s┐\n", strings.Repeat("─", innerWidth))
	}

	// Content lines.
	for _, childID := range c.Children {
		child, ok := index[childID]
		if !ok {
			continue
		}
		if child.Type == "Divider" {
			fmt.Fprintf(w, "├%s┤\n", strings.Repeat("─", innerWidth))
			continue
		}
		if child.Type != "Text" {
			continue
		}
		cp := child.Properties
		label, _ := cp["label"].(string)
		text, _ := cp["text"].(string)
		var line string
		if label != "" {
			line = fmt.Sprintf("\033[2m%s:\033[0m %s", label, text)
			// For padding, use the visible length.
			visLen := runeLen(label) + 2 + runeLen(text)
			pad := innerWidth - visLen
			if pad < 0 {
				pad = 0
			}
			fmt.Fprintf(w, "│ %s%s│\n", line, strings.Repeat(" ", pad))
		} else {
			line = text
			pad := innerWidth - runeLen(line)
			if pad < 0 {
				pad = 0
			}
			fmt.Fprintf(w, "│ %-*s│\n", innerWidth-1, truncate(line, innerWidth-1))
		}
	}

	// Bottom border.
	fmt.Fprintf(w, "└%s┘\n", strings.Repeat("─", innerWidth))
}

func renderTextCLI(w io.Writer, c *a2ui.Component) {
	p := c.Properties
	label, _ := p["label"].(string)
	text, _ := p["text"].(string)
	style, _ := p["style"].(string)

	if label != "" {
		fmt.Fprintf(w, "\033[2m%s:\033[0m ", label)
	}

	switch style {
	case "bold":
		fmt.Fprintf(w, "\033[1m%s\033[0m\n", text)
	case "dim":
		fmt.Fprintf(w, "\033[2m%s\033[0m\n", text)
	case "code":
		fmt.Fprintf(w, "\033[33m%s\033[0m\n", text)
	default:
		fmt.Fprintln(w, text)
	}
}

func renderDividerCLI(w io.Writer, width int) {
	if width <= 0 {
		width = 40
	}
	fmt.Fprintln(w, strings.Repeat("─", width))
}

// fmtVal converts an arbitrary value to a display string.
func fmtVal(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}

func runeLen(s string) int {
	return len([]rune(s))
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// ExtractA2UIBlocks splits text on ```a2ui fences, returning text and A2UI JSON segments.
// Each returned segment has IsA2UI=true for A2UI blocks, false for plain text.
type TextSegment struct {
	Text   string
	IsA2UI bool
}

func ExtractA2UIBlocks(text string) []TextSegment {
	const fence = "```a2ui"
	const closeFence = "```"

	var segments []TextSegment
	pos := 0

	for pos < len(text) {
		start := strings.Index(text[pos:], fence)
		if start == -1 {
			segments = append(segments, TextSegment{Text: text[pos:]})
			break
		}
		start += pos

		// Text before fence.
		if start > pos {
			segments = append(segments, TextSegment{Text: text[pos:start]})
		}

		jsonStart := start + len(fence)
		if jsonStart < len(text) && text[jsonStart] == '\n' {
			jsonStart++
		}

		end := strings.Index(text[jsonStart:], closeFence)
		if end == -1 {
			// Unclosed fence.
			segments = append(segments, TextSegment{Text: text[start:]})
			break
		}
		end += jsonStart

		jsonStr := strings.TrimSpace(text[jsonStart:end])
		segments = append(segments, TextSegment{Text: jsonStr, IsA2UI: true})

		pos = end + len(closeFence)
		if pos < len(text) && text[pos] == '\n' {
			pos++
		}
	}

	return segments
}

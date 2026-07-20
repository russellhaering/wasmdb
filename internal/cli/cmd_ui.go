package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

func init() {
	register(command{
		noun:        "ui",
		verb:        "create",
		usage:       "wasmdb ui create <name> --surface-file <file> [--actions-file <file>] [--title <t>] [--description <d>] [--source-tables <t1,t2>] [--query-file <file>] [--auto-refresh <sec>] [--sort-order <n>] [--disabled]",
		description: "Create a dashboard UI page",
		run:         uiCreate,
	})
	register(command{
		noun:        "ui",
		verb:        "list",
		usage:       "wasmdb ui list [--json]",
		description: "List dashboard UI pages",
		run:         uiList,
	})
	register(command{
		noun:        "ui",
		verb:        "get",
		usage:       "wasmdb ui get <name> [--json]",
		description: "Get UI page details",
		run:         uiGet,
	})
	register(command{
		noun:        "ui",
		verb:        "update",
		usage:       "wasmdb ui update <name> --surface-file <file> [--actions-file <file>] [--title <t>] [--description <d>] [--source-tables <t1,t2>] [--query-file <file>] [--auto-refresh <sec>] [--sort-order <n>] [--disabled]",
		description: "Update a dashboard UI page",
		run:         uiUpdate,
	})
	register(command{
		noun:        "ui",
		verb:        "render",
		usage:       "wasmdb ui render <name> [--param k=v ...] [--json]",
		description: "Render a UI page and print its resolved data",
		run:         uiRender,
	})
	register(command{
		noun:        "ui",
		verb:        "delete",
		usage:       "wasmdb ui delete <name>",
		description: "Delete a dashboard UI page",
		run:         uiDelete,
	})
}

func uiCreate(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("page name required")
	}
	name := ctx.args[0]

	surface, err := resolveContentFlag(ctx, "surface-json", "surface-file")
	if err != nil {
		return fmt.Errorf("surface: %w", err)
	}
	if surface == "" {
		return fmt.Errorf("--surface-file or --surface-json is required")
	}

	queryJS, err := resolveContentFlag(ctx, "query-js", "query-file")
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}

	actionsJSON, err := resolveContentFlag(ctx, "actions", "actions-file")
	if err != nil {
		return fmt.Errorf("actions: %w", err)
	}

	title := ctx.flag("title")
	description := ctx.flag("description")
	sourceTables := parseCommaSeparated(ctx.flag("source-tables"))
	autoRefresh := parseIntFlag(ctx.flag("auto-refresh"), 0)
	sortOrder := parseIntFlag(ctx.flag("sort-order"), 0)
	enabled := !ctx.hasFlag("disabled")

	result, err := ctx.backend.CreateUIConfig(ctx, name, title, description, sourceTables, surface, actionsJSON, queryJS, autoRefresh, sortOrder, enabled)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, result)
	}
	fmt.Fprintf(ctx.stdout, "Created UI page %q (id=%s)\n", result.Name, result.ID)
	return nil
}

func uiList(ctx *cmdContext) error {
	configs, err := ctx.backend.ListUIConfigs(ctx)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, configs)
	}

	if len(configs) == 0 {
		fmt.Fprintln(ctx.stdout, "no UI pages configured")
		return nil
	}

	for _, c := range configs {
		status := "enabled"
		if !c.Enabled {
			status = "disabled"
		}
		titleStr := c.Title
		if titleStr == "" {
			titleStr = c.Name
		}
		fmt.Fprintf(ctx.stdout, "%s\t%s\t%s\torder:%d\ttables:%s\n",
			c.Name, truncate(titleStr, 30), status, c.SortOrder,
			strings.Join(c.SourceTables, ","))
	}
	return nil
}

func uiGet(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("page name required")
	}

	config, err := ctx.backend.GetUIConfig(ctx, ctx.args[0])
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, config)
	}

	fmt.Fprintf(ctx.stdout, "Name:          %s\n", config.Name)
	fmt.Fprintf(ctx.stdout, "Title:         %s\n", config.Title)
	if config.Description != "" {
		fmt.Fprintf(ctx.stdout, "Description:   %s\n", config.Description)
	}
	fmt.Fprintf(ctx.stdout, "Enabled:       %v\n", config.Enabled)
	fmt.Fprintf(ctx.stdout, "Sort Order:    %d\n", config.SortOrder)
	if config.AutoRefreshSeconds > 0 {
		fmt.Fprintf(ctx.stdout, "Auto Refresh:  %ds\n", config.AutoRefreshSeconds)
	}
	if len(config.SourceTables) > 0 {
		fmt.Fprintf(ctx.stdout, "Source Tables: %s\n", strings.Join(config.SourceTables, ", "))
	}
	fmt.Fprintf(ctx.stdout, "Created:       %s\n", config.CreatedAt)
	fmt.Fprintf(ctx.stdout, "Updated:       %s\n", config.UpdatedAt)
	if config.Generator != "" {
		fmt.Fprintf(ctx.stdout, "Generator:     %s\n", config.Generator)
	}
	if config.QueryJS != "" {
		fmt.Fprintf(ctx.stdout, "\nQuery JS:\n%s\n", config.QueryJS)
	}
	fmt.Fprintf(ctx.stdout, "\nSurface JSON:\n%s\n", config.SurfaceJSON)
	if config.ActionsJSON != "" {
		fmt.Fprintf(ctx.stdout, "\nActions JSON:\n%s\n", config.ActionsJSON)
	}
	return nil
}

func uiUpdate(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("page name required")
	}
	name := ctx.args[0]

	surface, err := resolveContentFlag(ctx, "surface-json", "surface-file")
	if err != nil {
		return fmt.Errorf("surface: %w", err)
	}
	if surface == "" {
		return fmt.Errorf("--surface-file or --surface-json is required")
	}

	queryJS, err := resolveContentFlag(ctx, "query-js", "query-file")
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}

	actionsJSON, err := resolveContentFlag(ctx, "actions", "actions-file")
	if err != nil {
		return fmt.Errorf("actions: %w", err)
	}

	title := ctx.flag("title")
	description := ctx.flag("description")
	sourceTables := parseCommaSeparated(ctx.flag("source-tables"))
	autoRefresh := parseIntFlag(ctx.flag("auto-refresh"), 0)
	sortOrder := parseIntFlag(ctx.flag("sort-order"), 0)
	enabled := !ctx.hasFlag("disabled")

	result, err := ctx.backend.UpdateUIConfig(ctx, name, title, description, sourceTables, surface, actionsJSON, queryJS, autoRefresh, sortOrder, enabled)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, result)
	}
	fmt.Fprintf(ctx.stdout, "Updated UI page %q\n", result.Name)
	return nil
}

// uiRender renders a page server-side and prints the resolved data as aligned
// tables (array-of-object keys) and key: value lines (scalar keys), or the
// render error.
func uiRender(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("page name required")
	}
	name := ctx.args[0]

	params := map[string]string{}
	for _, kv := range ctx.flagAll("param") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("invalid --param %q (want key=value)", kv)
		}
		params[strings.TrimSpace(k)] = v
	}

	result, err := ctx.backend.RenderUIConfig(ctx, name, params)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, result)
	}

	if result.Error != "" {
		phase := result.ErrorPhase
		if phase == "" {
			phase = "render"
		}
		fmt.Fprintf(ctx.stderr, "render error (%s): %s\n", phase, result.Error)
		for _, line := range result.Logs {
			fmt.Fprintf(ctx.stderr, "  log: %s\n", line)
		}
		return fmt.Errorf("page %q failed to render", name)
	}

	title := result.Title
	if title == "" {
		title = name
	}
	fmt.Fprintf(ctx.stdout, "%s\n", title)
	if result.Description != "" {
		fmt.Fprintf(ctx.stdout, "%s\n", result.Description)
	}

	printRenderData(ctx.stdout, result.Data)
	for _, line := range result.Logs {
		fmt.Fprintf(ctx.stdout, "log: %s\n", line)
	}
	return nil
}

// printRenderData prints each top-level data key: arrays of objects become
// aligned tables, everything else prints as "key: value".
func printRenderData(w io.Writer, data map[string]any) {
	if len(data) == 0 {
		return
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := data[k]
		if rows, ok := asRowSlice(v); ok {
			fmt.Fprintf(w, "\n%s:\n", k)
			printTable(w, rows)
			continue
		}
		fmt.Fprintf(w, "%s: %v\n", k, v)
	}
}

// asRowSlice returns v as a slice of object rows if it is a non-empty JSON array
// whose elements are objects.
func asRowSlice(v any) ([]map[string]any, bool) {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return nil, false
	}
	rows := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		rows = append(rows, m)
	}
	return rows, true
}

// printTable prints rows as an aligned text table. Columns are the union of keys
// across rows, with "id" first when present, then the rest sorted.
func printTable(w io.Writer, rows []map[string]any) {
	colSet := map[string]bool{}
	for _, r := range rows {
		for k := range r {
			colSet[k] = true
		}
	}
	cols := make([]string, 0, len(colSet))
	for k := range colSet {
		if k != "id" {
			cols = append(cols, k)
		}
	}
	sort.Strings(cols)
	if colSet["id"] {
		cols = append([]string{"id"}, cols...)
	}

	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = len(c)
	}
	cells := make([][]string, 0, len(rows))
	for _, r := range rows {
		row := make([]string, len(cols))
		for i, c := range cols {
			s := ""
			if val, ok := r[c]; ok && val != nil {
				s = fmt.Sprintf("%v", val)
			}
			row[i] = s
			if len(s) > widths[i] {
				widths[i] = len(s)
			}
		}
		cells = append(cells, row)
	}

	writeRow := func(vals []string) {
		var b strings.Builder
		for i, v := range vals {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(v)
			for p := len(v); p < widths[i]; p++ {
				b.WriteByte(' ')
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
	writeRow(cols)
	for _, row := range cells {
		writeRow(row)
	}
}

func uiDelete(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("page name required")
	}

	if err := ctx.backend.DeleteUIConfig(ctx, ctx.args[0]); err != nil {
		return err
	}

	fmt.Fprintf(ctx.stdout, "Deleted UI page %q\n", ctx.args[0])
	return nil
}

// resolveContentFlag reads inline or file content from a pair of flags.
func resolveContentFlag(ctx *cmdContext, inlineFlag, fileFlag string) (string, error) {
	if inline := ctx.flag(inlineFlag); inline != "" {
		return inline, nil
	}
	if filePath := ctx.flag(fileFlag); filePath != "" {
		var r io.Reader
		if filePath == "-" {
			r = ctx.stdin
		} else {
			f, err := os.Open(filePath)
			if err != nil {
				return "", err
			}
			defer f.Close()
			r = f
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return "", nil
}

func parseCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func parseIntFlag(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

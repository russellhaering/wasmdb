package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func init() {
	register(command{
		noun:        "ui",
		verb:        "create",
		usage:       "wasmdb ui create <name> --surface-file <file> [--title <t>] [--description <d>] [--source-tables <t1,t2>] [--query-file <file>] [--auto-refresh <sec>] [--sort-order <n>] [--disabled]",
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
		usage:       "wasmdb ui update <name> --surface-file <file> [--title <t>] [--description <d>] [--source-tables <t1,t2>] [--query-file <file>] [--auto-refresh <sec>] [--sort-order <n>] [--disabled]",
		description: "Update a dashboard UI page",
		run:         uiUpdate,
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

	title := ctx.flag("title")
	description := ctx.flag("description")
	sourceTables := parseCommaSeparated(ctx.flag("source-tables"))
	autoRefresh := parseIntFlag(ctx.flag("auto-refresh"), 0)
	sortOrder := parseIntFlag(ctx.flag("sort-order"), 0)
	enabled := !ctx.hasFlag("disabled")

	result, err := ctx.backend.CreateUIConfig(ctx, name, title, description, sourceTables, surface, queryJS, autoRefresh, sortOrder, enabled)
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
	if config.QueryJS != "" {
		fmt.Fprintf(ctx.stdout, "\nQuery JS:\n%s\n", config.QueryJS)
	}
	fmt.Fprintf(ctx.stdout, "\nSurface JSON:\n%s\n", config.SurfaceJSON)
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

	title := ctx.flag("title")
	description := ctx.flag("description")
	sourceTables := parseCommaSeparated(ctx.flag("source-tables"))
	autoRefresh := parseIntFlag(ctx.flag("auto-refresh"), 0)
	sortOrder := parseIntFlag(ctx.flag("sort-order"), 0)
	enabled := !ctx.hasFlag("disabled")

	result, err := ctx.backend.UpdateUIConfig(ctx, name, title, description, sourceTables, surface, queryJS, autoRefresh, sortOrder, enabled)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, result)
	}
	fmt.Fprintf(ctx.stdout, "Updated UI page %q\n", result.Name)
	return nil
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

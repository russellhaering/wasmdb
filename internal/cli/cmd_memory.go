package cli

import (
	"fmt"
	"strings"
)

func init() {
	register(command{noun: "memory", verb: "create", usage: "wasmdb memory create --summary <text> [--title <title>] [--scope user|session] [--tag t] [--pinned] [--json]", description: "Create a memory", run: memoryCreate})
	register(command{noun: "memory", verb: "list", usage: "wasmdb memory list [--json]", description: "List memory catalog", run: memoryList})
	register(command{noun: "memory", verb: "get", usage: "wasmdb memory get <id> [--json]", description: "Get memory detail", run: memoryGet})
	register(command{noun: "memory", verb: "update", usage: "wasmdb memory update <id> [--summary <text>] [--title <title>] [--scope user|session] [--tag t] [--pinned] [--json]", description: "Update memory", run: memoryUpdate})
	register(command{noun: "memory", verb: "delete", usage: "wasmdb memory delete <id>", description: "Delete memory", run: memoryDelete})
}

func memoryCreate(ctx *cmdContext) error {
	summary := ctx.flag("summary")
	if summary == "" {
		return fmt.Errorf("--summary is required")
	}
	title := ctx.flag("title")
	scope := ctx.flag("scope")
	if scope == "" {
		scope = "user"
	}
	tags := ctx.flagAll("tag")
	pinned := ctx.hasFlag("pinned")

	m, err := ctx.backend.CreateMemory(ctx, scope, title, summary, tags, pinned)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, m)
	}
	fmt.Fprintf(ctx.stdout, "Created memory %s\n", m.ID)
	return nil
}

func memoryList(ctx *cmdContext) error {
	items, err := ctx.backend.ListMemories(ctx)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, items)
	}
	if len(items) == 0 {
		fmt.Fprintln(ctx.stdout, "no memories")
		return nil
	}
	for _, m := range items {
		fmt.Fprintf(ctx.stdout, "%s\t%s\tpinned=%t\ttags=%s\t%s\n", m.ID, m.Title, m.Pinned, strings.Join(m.Tags, ","), m.Summary)
	}
	return nil
}

func memoryGet(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("memory id required")
	}
	m, err := ctx.backend.GetMemory(ctx, ctx.args[0])
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, m)
	}
	fmt.Fprintf(ctx.stdout, "ID: %s\nTitle: %s\nScope: %s\nPinned: %t\nTags: %s\nSummary: %s\nUpdated: %s\n", m.ID, m.Title, m.Scope, m.Pinned, strings.Join(m.Tags, ","), m.Summary, m.UpdatedAt)
	return nil
}

func memoryUpdate(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("memory id required")
	}
	id := ctx.args[0]
	summary := ctx.flag("summary")
	title := ctx.flag("title")
	scope := ctx.flag("scope")
	tags := ctx.flagAll("tag")
	pinned := ctx.hasFlag("pinned")

	m, err := ctx.backend.UpdateMemory(ctx, id, scope, title, summary, tags, pinned)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, m)
	}
	fmt.Fprintf(ctx.stdout, "Updated memory %s\n", id)
	return nil
}

func memoryDelete(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("memory id required")
	}
	if err := ctx.backend.DeleteMemory(ctx, ctx.args[0]); err != nil {
		return err
	}
	fmt.Fprintf(ctx.stdout, "Deleted memory %s\n", ctx.args[0])
	return nil
}

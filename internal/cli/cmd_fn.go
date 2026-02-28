package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

func init() {
	register(command{
		noun:        "fn",
		verb:        "create",
		usage:       "wasmdb fn create <name> --file <path> [--description <desc>] [--json]",
		description: "Create a stored function",
		run:         fnCreate,
	})
	register(command{
		noun:        "fn",
		verb:        "list",
		usage:       "wasmdb fn list [--json]",
		description: "List stored functions",
		run:         fnList,
	})
	register(command{
		noun:        "fn",
		verb:        "get",
		usage:       "wasmdb fn get <name> [--json]",
		description: "Get a stored function",
		run:         fnGet,
	})
	register(command{
		noun:        "fn",
		verb:        "update",
		usage:       "wasmdb fn update <name> --file <path> [--description <desc>] [--json]",
		description: "Update a stored function",
		run:         fnUpdate,
	})
	register(command{
		noun:        "fn",
		verb:        "delete",
		usage:       "wasmdb fn delete <name>",
		description: "Delete a stored function",
		run:         fnDelete,
	})
	register(command{
		noun:        "fn",
		verb:        "exec",
		usage:       "wasmdb fn exec <name> [--params '{...}'] [--json]",
		description: "Execute a stored function",
		run:         fnExec,
	})
}

func fnCreate(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("function name required")
	}
	name := ctx.args[0]
	filePath := ctx.flag("file")
	if filePath == "" {
		return fmt.Errorf("--file is required")
	}

	code, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	description := ctx.flag("description")

	fn, err := ctx.backend.CreateFunction(ctx, name, description, string(code))
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, fn)
	}
	fmt.Fprintf(ctx.stdout, "Created function %q (id: %s)\n", name, fn.ID)
	return nil
}

func fnList(ctx *cmdContext) error {
	fns, err := ctx.backend.ListFunctions(ctx)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, fns)
	}

	if len(fns) == 0 {
		fmt.Fprintln(ctx.stdout, "no stored functions")
		return nil
	}

	for _, fn := range fns {
		if fn.Description != "" {
			fmt.Fprintf(ctx.stdout, "%s\t%s\t%s\n", fn.Name, fn.Description, fn.UpdatedAt)
		} else {
			fmt.Fprintf(ctx.stdout, "%s\t%s\n", fn.Name, fn.UpdatedAt)
		}
	}
	return nil
}

func fnGet(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("function name required")
	}
	name := ctx.args[0]

	fn, err := ctx.backend.GetFunction(ctx, name)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, fn)
	}

	fmt.Fprintf(ctx.stdout, "Name: %s\nID: %s\nDescription: %s\nCreated: %s\nUpdated: %s\n\n--- Code ---\n%s\n",
		fn.Name, fn.ID, fn.Description, fn.CreatedAt, fn.UpdatedAt, fn.Code)
	return nil
}

func fnUpdate(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("function name required")
	}
	name := ctx.args[0]
	filePath := ctx.flag("file")
	if filePath == "" {
		return fmt.Errorf("--file is required")
	}

	code, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	description := ctx.flag("description")

	fn, err := ctx.backend.UpdateFunction(ctx, name, string(code), description)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, fn)
	}
	fmt.Fprintf(ctx.stdout, "Updated function %q\n", name)
	return nil
}

func fnDelete(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("function name required")
	}
	name := ctx.args[0]

	if err := ctx.backend.DeleteFunction(ctx, name); err != nil {
		return err
	}

	fmt.Fprintf(ctx.stdout, "Deleted function %q\n", name)
	return nil
}

func fnExec(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("function name required")
	}
	name := ctx.args[0]

	var params map[string]any
	if paramsStr := ctx.flag("params"); paramsStr != "" {
		if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
			return fmt.Errorf("invalid --params JSON: %w", err)
		}
	}

	result, err := ctx.backend.ExecFunction(ctx, name, params)
	if err != nil {
		return err
	}

	return printExecResult(ctx, result)
}

func printExecResult(ctx *cmdContext, result *ExecResult) error {
	if ctx.json {
		return formatJSON(ctx.stdout, result)
	}

	if result.Error != "" {
		fmt.Fprintf(ctx.stderr, "Error: %s\n", result.Error)
	}
	for _, log := range result.Logs {
		fmt.Fprintf(ctx.stderr, "[log] %s\n", log)
	}
	if result.Result != nil {
		data, _ := json.MarshalIndent(result.Result, "", "  ")
		fmt.Fprintln(ctx.stdout, string(data))
	}
	fmt.Fprintf(ctx.stderr, "(%dms)\n", result.DurationMS)
	return nil
}

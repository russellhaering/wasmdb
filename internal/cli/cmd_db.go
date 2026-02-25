package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/russellhaering/wasmdb/internal/document"
)

func init() {
	register(command{
		noun:        "db",
		verb:        "list",
		usage:       "wasmdb db list [--json]",
		description: "List all tables",
		run:         tableList,
	})
	register(command{
		noun:        "db",
		verb:        "create",
		usage:       "wasmdb db create <name> [--schema-file <path>] [--json]",
		description: "Create a table",
		run:         tableCreate,
	})
	register(command{
		noun:        "db",
		verb:        "get",
		usage:       "wasmdb db get <name> [--json]",
		description: "Get table details",
		run:         tableGet,
	})
	register(command{
		noun:        "db",
		verb:        "delete",
		usage:       "wasmdb db delete <name>",
		description: "Delete a table",
		run:         tableDelete,
	})
}

func tableList(ctx *cmdContext) error {
	tables, err := ctx.backend.ListTables(ctx)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, tables)
	}
	formatTableList(ctx.stdout, tables)
	return nil
}

func tableCreate(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb db create <name>")
	}
	name := ctx.args[0]

	var schema *document.Schema
	if schemaFile := ctx.flag("schema-file"); schemaFile != "" {
		data, err := os.ReadFile(schemaFile)
		if err != nil {
			return fmt.Errorf("read schema file: %w", err)
		}
		schema = &document.Schema{}
		if err := json.Unmarshal(data, schema); err != nil {
			return fmt.Errorf("parse schema file: %w", err)
		}
	}

	tbl, err := ctx.backend.CreateTable(ctx, name, schema)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, tbl)
	}
	formatTableInfo(ctx.stdout, tbl)
	return nil
}

func tableGet(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb db get <name>")
	}
	name := ctx.args[0]

	tbl, err := ctx.backend.GetTable(ctx, name)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, tbl)
	}
	formatTableInfo(ctx.stdout, tbl)
	return nil
}

func tableDelete(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb db delete <name>")
	}
	name := ctx.args[0]

	if err := ctx.backend.DeleteTable(ctx, name); err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, map[string]string{"status": "deleted"})
	}
	fmt.Fprintln(ctx.stdout, "deleted")
	return nil
}

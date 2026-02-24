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
		description: "List all databases",
		run:         dbList,
	})
	register(command{
		noun:        "db",
		verb:        "create",
		usage:       "wasmdb db create <name> [--schema-file <path>] [--json]",
		description: "Create a database",
		run:         dbCreate,
	})
	register(command{
		noun:        "db",
		verb:        "get",
		usage:       "wasmdb db get <name> [--json]",
		description: "Get database details",
		run:         dbGet,
	})
	register(command{
		noun:        "db",
		verb:        "delete",
		usage:       "wasmdb db delete <name>",
		description: "Delete a database",
		run:         dbDelete,
	})
}

func dbList(ctx *cmdContext) error {
	dbs, err := ctx.backend.ListDatabases(ctx)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, dbs)
	}
	formatDatabaseList(ctx.stdout, dbs)
	return nil
}

func dbCreate(ctx *cmdContext) error {
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

	db, err := ctx.backend.CreateDatabase(ctx, name, schema)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, db)
	}
	formatDatabaseInfo(ctx.stdout, db)
	return nil
}

func dbGet(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb db get <name>")
	}
	name := ctx.args[0]

	db, err := ctx.backend.GetDatabase(ctx, name)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, db)
	}
	formatDatabaseInfo(ctx.stdout, db)
	return nil
}

func dbDelete(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb db delete <name>")
	}
	name := ctx.args[0]

	if err := ctx.backend.DeleteDatabase(ctx, name); err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, map[string]string{"status": "deleted"})
	}
	fmt.Fprintln(ctx.stdout, "deleted")
	return nil
}

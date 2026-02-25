package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/russellhaering/wasmdb/internal/document"
)

func init() {
	register(command{
		noun:        "schema",
		verb:        "get",
		usage:       "wasmdb schema get <db> [--json]",
		description: "Get table schema",
		run:         schemaGet,
	})
	register(command{
		noun:        "schema",
		verb:        "set",
		usage:       "wasmdb schema set <db> --file <path> [--json]",
		description: "Set table schema",
		run:         schemaSet,
	})
}

func schemaGet(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb schema get <db>")
	}
	db := ctx.args[0]

	schema, err := ctx.backend.GetSchema(ctx, db)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, schema)
	}
	formatSchema(ctx.stdout, schema)
	return nil
}

func schemaSet(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb schema set <db> --file <path>")
	}
	db := ctx.args[0]

	file := ctx.flag("file")
	if file == "" {
		return fmt.Errorf("--file is required")
	}

	var data []byte
	var err error
	if file == "-" {
		data, err = io.ReadAll(ctx.stdin)
	} else {
		data, err = os.ReadFile(file)
	}
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}

	var schema document.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}

	result, err := ctx.backend.UpdateSchema(ctx, db, &schema)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, result)
	}
	formatSchema(ctx.stdout, result)
	return nil
}

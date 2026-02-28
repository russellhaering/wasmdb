package cli

import (
	"encoding/json"
	"fmt"
)

func init() {
	register(command{
		noun:        "skill",
		verb:        "create",
		usage:       "wasmdb skill create <name> --function <function-name> [--description <desc>] [--json]",
		description: "Create a skill linked to a stored function",
		run:         skillCreate,
	})
	register(command{
		noun:        "skill",
		verb:        "list",
		usage:       "wasmdb skill list [--json]",
		description: "List skills",
		run:         skillList,
	})
	register(command{
		noun:        "skill",
		verb:        "get",
		usage:       "wasmdb skill get <name> [--json]",
		description: "Get a skill",
		run:         skillGet,
	})
	register(command{
		noun:        "skill",
		verb:        "update",
		usage:       "wasmdb skill update <name> --function <function-name> [--description <desc>] [--json]",
		description: "Update a skill",
		run:         skillUpdate,
	})
	register(command{
		noun:        "skill",
		verb:        "delete",
		usage:       "wasmdb skill delete <name>",
		description: "Delete a skill",
		run:         skillDelete,
	})
	register(command{
		noun:        "skill",
		verb:        "exec",
		usage:       "wasmdb skill exec <name> [--params '{...}'] [--json]",
		description: "Execute a skill",
		run:         skillExec,
	})
}

func skillCreate(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("skill name required")
	}
	name := ctx.args[0]
	functionName := ctx.flag("function")
	if functionName == "" {
		return fmt.Errorf("--function is required")
	}
	description := ctx.flag("description")

	sk, err := ctx.backend.CreateSkill(ctx, name, description, functionName)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, sk)
	}
	fmt.Fprintf(ctx.stdout, "Created skill %q -> function %q\n", name, functionName)
	return nil
}

func skillList(ctx *cmdContext) error {
	skills, err := ctx.backend.ListSkills(ctx)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, skills)
	}

	if len(skills) == 0 {
		fmt.Fprintln(ctx.stdout, "no skills")
		return nil
	}

	for _, sk := range skills {
		if sk.Description != "" {
			fmt.Fprintf(ctx.stdout, "%s\t%s\t%s\t%s\n", sk.Name, sk.FunctionName, sk.Description, sk.UpdatedAt)
		} else {
			fmt.Fprintf(ctx.stdout, "%s\t%s\t%s\n", sk.Name, sk.FunctionName, sk.UpdatedAt)
		}
	}
	return nil
}

func skillGet(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("skill name required")
	}
	name := ctx.args[0]

	sk, err := ctx.backend.GetSkill(ctx, name)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, sk)
	}

	fmt.Fprintf(ctx.stdout, "Name: %s\nID: %s\nFunction: %s\nDescription: %s\nCreated: %s\nUpdated: %s\n",
		sk.Name, sk.ID, sk.FunctionName, sk.Description, sk.CreatedAt, sk.UpdatedAt)
	return nil
}

func skillUpdate(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("skill name required")
	}
	name := ctx.args[0]
	functionName := ctx.flag("function")
	if functionName == "" {
		return fmt.Errorf("--function is required")
	}
	description := ctx.flag("description")

	sk, err := ctx.backend.UpdateSkill(ctx, name, description, functionName)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, sk)
	}
	fmt.Fprintf(ctx.stdout, "Updated skill %q\n", name)
	return nil
}

func skillDelete(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("skill name required")
	}
	name := ctx.args[0]

	if err := ctx.backend.DeleteSkill(ctx, name); err != nil {
		return err
	}

	fmt.Fprintf(ctx.stdout, "Deleted skill %q\n", name)
	return nil
}

func skillExec(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("skill name required")
	}
	name := ctx.args[0]

	var params map[string]any
	if paramsStr := ctx.flag("params"); paramsStr != "" {
		if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
			return fmt.Errorf("invalid --params JSON: %w", err)
		}
	}

	result, err := ctx.backend.ExecSkill(ctx, name, params)
	if err != nil {
		return err
	}

	return printExecResult(ctx, result)
}

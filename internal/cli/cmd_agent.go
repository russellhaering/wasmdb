package cli

import (
	"fmt"
	"strconv"
)

func init() {
	register(command{
		noun:        "agent",
		verb:        "create",
		usage:       "wasmdb agent create <name> --prompt <prompt> --schedule <duration> [--trigger-type timer] [--description <desc>] [--max-turns <n>] [--disabled] [--json]",
		description: "Create a background agent",
		run:         agentCreate,
	})
	register(command{
		noun:        "agent",
		verb:        "list",
		usage:       "wasmdb agent list [--json]",
		description: "List background agents",
		run:         agentList,
	})
	register(command{
		noun:        "agent",
		verb:        "get",
		usage:       "wasmdb agent get <name> [--json]",
		description: "Get agent details",
		run:         agentGet,
	})
	register(command{
		noun:        "agent",
		verb:        "update",
		usage:       "wasmdb agent update <name> --prompt <prompt> --schedule <duration> [--trigger-type timer] [--description <desc>] [--max-turns <n>] [--disabled] [--json]",
		description: "Update a background agent",
		run:         agentUpdate,
	})
	register(command{
		noun:        "agent",
		verb:        "delete",
		usage:       "wasmdb agent delete <name>",
		description: "Delete a background agent",
		run:         agentDelete,
	})
	register(command{
		noun:        "agent",
		verb:        "trigger",
		usage:       "wasmdb agent trigger <name> [--json]",
		description: "Trigger a background agent run immediately",
		run:         agentTrigger,
	})
	register(command{
		noun:        "agent",
		verb:        "runs",
		usage:       "wasmdb agent runs <name> [--limit <n>] [--json]",
		description: "List recent runs for an agent",
		run:         agentRuns,
	})
}

func agentCreate(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("agent name required")
	}
	name := ctx.args[0]
	prompt := ctx.flag("prompt")
	schedule := ctx.flag("schedule")
	if prompt == "" || schedule == "" {
		return fmt.Errorf("--prompt and --schedule are required")
	}

	triggerType := ctx.flag("trigger-type")
	if triggerType == "" {
		triggerType = "timer"
	}

	description := ctx.flag("description")
	enabled := !ctx.hasFlag("disabled")

	var maxTurns int
	if mt := ctx.flag("max-turns"); mt != "" {
		n, err := strconv.Atoi(mt)
		if err != nil {
			return fmt.Errorf("invalid --max-turns: %w", err)
		}
		maxTurns = n
	}

	ag, err := ctx.backend.CreateAgent(ctx, name, description, prompt, schedule, triggerType, enabled, maxTurns)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, ag)
	}
	fmt.Fprintf(ctx.stdout, "Created agent %q (schedule=%s, trigger=%s, enabled=%t)\n", name, ag.Schedule, ag.TriggerType, ag.Enabled)
	return nil
}

func agentList(ctx *cmdContext) error {
	agents, err := ctx.backend.ListAgents(ctx)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, agents)
	}

	if len(agents) == 0 {
		fmt.Fprintln(ctx.stdout, "no agents configured")
		return nil
	}

	for _, ag := range agents {
		status := "enabled"
		if !ag.Enabled {
			status = "disabled"
		}
		fmt.Fprintf(ctx.stdout, "%s\t%s\t%s\t%s\t%s\n", ag.Name, ag.TriggerType, ag.Schedule, status, ag.UpdatedAt)
	}
	return nil
}

func agentGet(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("agent name required")
	}
	name := ctx.args[0]

	ag, err := ctx.backend.GetAgent(ctx, name)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, ag)
	}

	fmt.Fprintf(ctx.stdout, "Name: %s\nID: %s\nTrigger: %s\nSchedule: %s\nEnabled: %t\n", ag.Name, ag.ID, ag.TriggerType, ag.Schedule, ag.Enabled)
	if ag.Description != "" {
		fmt.Fprintf(ctx.stdout, "Description: %s\n", ag.Description)
	}
	if ag.MaxTurns > 0 {
		fmt.Fprintf(ctx.stdout, "Max Turns: %d\n", ag.MaxTurns)
	}
	fmt.Fprintf(ctx.stdout, "Prompt:\n%s\n", ag.Prompt)
	fmt.Fprintf(ctx.stdout, "Created: %s\nUpdated: %s\n", ag.CreatedAt, ag.UpdatedAt)
	return nil
}

func agentUpdate(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("agent name required")
	}
	name := ctx.args[0]
	prompt := ctx.flag("prompt")
	schedule := ctx.flag("schedule")
	if prompt == "" || schedule == "" {
		return fmt.Errorf("--prompt and --schedule are required")
	}

	triggerType := ctx.flag("trigger-type")
	if triggerType == "" {
		triggerType = "timer"
	}

	description := ctx.flag("description")
	enabled := !ctx.hasFlag("disabled")

	var maxTurns int
	if mt := ctx.flag("max-turns"); mt != "" {
		n, err := strconv.Atoi(mt)
		if err != nil {
			return fmt.Errorf("invalid --max-turns: %w", err)
		}
		maxTurns = n
	}

	ag, err := ctx.backend.UpdateAgent(ctx, name, description, prompt, schedule, triggerType, enabled, maxTurns)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, ag)
	}
	fmt.Fprintf(ctx.stdout, "Updated agent %q (schedule=%s, trigger=%s, enabled=%t)\n", name, ag.Schedule, ag.TriggerType, ag.Enabled)
	return nil
}

func agentDelete(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("agent name required")
	}
	name := ctx.args[0]

	if err := ctx.backend.DeleteAgent(ctx, name); err != nil {
		return err
	}

	fmt.Fprintf(ctx.stdout, "Deleted agent %q\n", name)
	return nil
}

func agentTrigger(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("agent name required")
	}
	name := ctx.args[0]

	run, err := ctx.backend.TriggerAgent(ctx, name)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, run)
	}

	fmt.Fprintf(ctx.stdout, "Agent %q run completed\n", name)
	fmt.Fprintf(ctx.stdout, "Status: %s\nDuration: %dms\nTokens: %d in / %d out\n", run.Status, run.DurationMS, run.InputTokens, run.OutputTokens)
	if run.Error != "" {
		fmt.Fprintf(ctx.stdout, "Error: %s\n", run.Error)
	}
	if run.Output != "" {
		fmt.Fprintf(ctx.stdout, "Output:\n%s\n", run.Output)
	}
	return nil
}

func agentRuns(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("agent name required")
	}
	name := ctx.args[0]

	limit := 20
	if l := ctx.flag("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil {
			return fmt.Errorf("invalid --limit: %w", err)
		}
		limit = n
	}

	runs, err := ctx.backend.ListAgentRuns(ctx, name, limit)
	if err != nil {
		return err
	}

	if ctx.json {
		return formatJSON(ctx.stdout, runs)
	}

	if len(runs) == 0 {
		fmt.Fprintln(ctx.stdout, "no runs found")
		return nil
	}

	for _, run := range runs {
		errStr := ""
		if run.Error != "" {
			errStr = " err=" + truncate(run.Error, 60)
		}
		fmt.Fprintf(ctx.stdout, "%s\t%s\t%dms\t%s%s\n", run.ID, run.Status, run.DurationMS, run.StartedAt, errStr)
	}
	return nil
}


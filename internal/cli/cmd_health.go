package cli

import "fmt"

func init() {
	register(command{
		noun:        "health",
		verb:        "",
		usage:       "wasmdb health",
		description: "Check server health",
		run:         healthCheck,
	})
	register(command{
		noun:        "ready",
		verb:        "",
		usage:       "wasmdb ready",
		description: "Check server readiness",
		run:         readyCheck,
	})
}

func healthCheck(ctx *cmdContext) error {
	status, err := ctx.backend.Health(ctx)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, status)
	}
	fmt.Fprintln(ctx.stdout, status.Status)
	return nil
}

func readyCheck(ctx *cmdContext) error {
	status, err := ctx.backend.Ready(ctx)
	if err != nil {
		return err
	}
	if ctx.json {
		return formatJSON(ctx.stdout, status)
	}
	fmt.Fprintln(ctx.stdout, status.Status)
	return nil
}

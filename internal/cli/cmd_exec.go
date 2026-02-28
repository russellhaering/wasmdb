package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

func init() {
	register(command{
		noun:        "exec",
		verb:        "",
		usage:       "wasmdb exec --file <path> | --code '<code>' [--params '{...}'] [--json]",
		description: "Execute JavaScript code (ephemeral)",
		run:         execCode,
	})
}

func execCode(ctx *cmdContext) error {
	var code string

	if filePath := ctx.flag("file"); filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
		code = string(data)
	} else if c := ctx.flag("code"); c != "" {
		code = c
	} else {
		return fmt.Errorf("--file or --code is required")
	}

	var params map[string]any
	if paramsStr := ctx.flag("params"); paramsStr != "" {
		if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
			return fmt.Errorf("invalid --params JSON: %w", err)
		}
	}

	result, err := ctx.backend.ExecCode(ctx, code, params)
	if err != nil {
		return err
	}

	return printExecResult(ctx, result)
}

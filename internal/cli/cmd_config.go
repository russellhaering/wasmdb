package cli

import (
	"fmt"
	"sort"
	"strings"
)

func init() {
	register(command{
		noun:        "config",
		verb:        "set",
		usage:       "wasmdb config set <key> <value>",
		description: "Set a config value (url, default_format)",
		run:         runConfigSet,
	})
	register(command{
		noun:        "config",
		verb:        "get",
		usage:       "wasmdb config get <key>",
		description: "Get a config value",
		run:         runConfigGet,
	})
	register(command{
		noun:        "config",
		verb:        "list",
		usage:       "wasmdb config list",
		description: "List all config values",
		run:         runConfigList,
	})
	register(command{
		noun:        "config",
		verb:        "path",
		usage:       "wasmdb config path",
		description: "Print the config file path",
		run:         runConfigPath,
	})
}

var validConfigKeys = map[string]string{
	"url":            "Default server URL",
	"default_format": "Default output format (text or json)",
}

func runConfigSet(ctx *cmdContext) error {
	if len(ctx.args) < 2 {
		return fmt.Errorf("usage: wasmdb config set <key> <value>")
	}

	key := ctx.args[0]
	value := ctx.args[1]

	if _, ok := validConfigKeys[key]; !ok {
		return fmt.Errorf("unknown config key %q; valid keys: %s", key, validKeyList())
	}

	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	switch key {
	case "url":
		cfg.URL = value
	case "default_format":
		if value != "text" && value != "json" {
			return fmt.Errorf("default_format must be 'text' or 'json'")
		}
		cfg.DefaultFormat = value
	}

	if err := SaveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintf(ctx.stdout, "%s=%s\n", key, value)
	return nil
}

func runConfigGet(ctx *cmdContext) error {
	if len(ctx.args) < 1 {
		return fmt.Errorf("usage: wasmdb config get <key>")
	}

	key := ctx.args[0]
	if _, ok := validConfigKeys[key]; !ok {
		return fmt.Errorf("unknown config key %q; valid keys: %s", key, validKeyList())
	}

	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	var value string
	switch key {
	case "url":
		value = cfg.URL
	case "default_format":
		value = cfg.DefaultFormat
	}

	if value != "" {
		fmt.Fprintln(ctx.stdout, value)
	}
	return nil
}

func runConfigList(ctx *cmdContext) error {
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	entries := map[string]string{
		"url":            cfg.URL,
		"default_format": cfg.DefaultFormat,
	}

	if ctx.json {
		return formatJSON(ctx.stdout, entries)
	}

	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := entries[k]
		if v == "" {
			v = "(unset)"
		}
		fmt.Fprintf(ctx.stdout, "%-20s %s\n", k, v)
	}
	return nil
}

func runConfigPath(ctx *cmdContext) error {
	fmt.Fprintln(ctx.stdout, ConfigPath())
	return nil
}

func validKeyList() string {
	keys := make([]string, 0, len(validConfigKeys))
	for k := range validConfigKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

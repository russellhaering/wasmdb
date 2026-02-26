package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// RunConfig holds configuration for a CLI invocation.
type RunConfig struct {
	Backend   Backend
	ServerURL string
	Stdout    io.Writer
	Stderr    io.Writer
	Stdin     io.Reader
}

// cmdContext is the context passed to each command handler.
type cmdContext struct {
	context.Context
	backend Backend
	stdout  io.Writer
	stderr  io.Writer
	stdin   io.Reader
	args    []string // positional args after noun+verb
	flags   map[string][]string
	json    bool
}

// flag returns the first value for a flag, or empty string.
func (c *cmdContext) flag(name string) string {
	if vals, ok := c.flags[name]; ok && len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// flagAll returns all values for a repeated flag.
func (c *cmdContext) flagAll(name string) []string {
	return c.flags[name]
}

// hasFlag returns true if the flag was provided.
func (c *cmdContext) hasFlag(name string) bool {
	_, ok := c.flags[name]
	return ok
}

// Run executes the CLI with the given argv (excluding the program name).
func Run(ctx context.Context, argv []string, cfg RunConfig) error {
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}

	if len(argv) == 0 {
		printHelp(cfg.Stdout, "")
		return nil
	}

	noun := argv[0]

	// Top-level help.
	if noun == "help" {
		topic := ""
		if len(argv) > 1 {
			topic = argv[1]
		}
		printHelp(cfg.Stdout, topic)
		return nil
	}

	// Parse verb and remaining args.
	var verb string
	var rest []string
	if len(argv) > 1 {
		verb = argv[1]
		rest = argv[2:]
	}

	// Check for --help on noun.
	if verb == "--help" || verb == "-h" || verb == "help" {
		printHelp(cfg.Stdout, noun)
		return nil
	}

	// Find command.
	cmd := findCommand(noun, verb)
	if cmd == nil {
		// Maybe it's a top-level command with no verb (e.g. "health", "ready").
		cmd = findCommand(noun, "")
		if cmd != nil {
			// verb was actually a positional arg.
			if verb != "" {
				rest = append([]string{verb}, rest...)
			}
		} else {
			return fmt.Errorf("unknown command: %s %s", noun, verb)
		}
	}

	// Parse flags and positional args from rest.
	args, flags, err := parseFlags(rest)
	if err != nil {
		return err
	}

	_, jsonMode := flags["json"]

	// Inject server URL as a flag so commands can access it.
	if cfg.ServerURL != "" {
		if _, ok := flags["url"]; !ok {
			flags["url"] = []string{cfg.ServerURL}
		}
	}

	cctx := &cmdContext{
		Context: ctx,
		backend: cfg.Backend,
		stdout:  cfg.Stdout,
		stderr:  cfg.Stderr,
		stdin:   cfg.Stdin,
		args:    args,
		flags:   flags,
		json:    jsonMode,
	}

	return cmd.run(cctx)
}

// parseFlags splits args into positional args and flags.
// Flags are --key value or --key=value. Boolean flags like --json have no value.
func parseFlags(args []string) (positional []string, flags map[string][]string, err error) {
	flags = make(map[string][]string)
	boolFlags := map[string]bool{"json": true}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			positional = append(positional, arg)
			continue
		}

		key := strings.TrimPrefix(arg, "--")

		// Handle --key=value.
		if idx := strings.IndexByte(key, '='); idx >= 0 {
			flags[key[:idx]] = append(flags[key[:idx]], key[idx+1:])
			continue
		}

		// Boolean flag.
		if boolFlags[key] {
			flags[key] = append(flags[key], "true")
			continue
		}

		// Flags that take a value.
		if i+1 >= len(args) {
			return nil, nil, fmt.Errorf("flag --%s requires a value", key)
		}
		i++
		flags[key] = append(flags[key], args[i])
	}

	return positional, flags, nil
}

func printHelp(w io.Writer, topic string) {
	if topic == "" {
		fmt.Fprintln(w, "Usage: wasmdb <command> [args] [flags]")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Commands:")
		// Group by noun.
		seen := make(map[string]bool)
		for _, c := range commands {
			if seen[c.noun] {
				continue
			}
			seen[c.noun] = true
			subs := commandsForNoun(c.noun)
			if len(subs) == 1 && subs[0].verb == "" {
				fmt.Fprintf(w, "  %-20s %s\n", subs[0].noun, subs[0].description)
			} else {
				verbs := make([]string, len(subs))
				for i, s := range subs {
					verbs[i] = s.verb
				}
				fmt.Fprintf(w, "  %-20s %s\n", c.noun+" <"+strings.Join(verbs, "|")+">", "")
			}
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Flags:")
		fmt.Fprintln(w, "  --json               Output as JSON")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Run 'wasmdb help <command>' for details.")
		return
	}

	subs := commandsForNoun(topic)
	if len(subs) == 0 {
		fmt.Fprintf(w, "Unknown command: %s\n", topic)
		return
	}

	for _, c := range subs {
		fmt.Fprintf(w, "  %s\n", c.usage)
		fmt.Fprintf(w, "    %s\n", c.description)
		fmt.Fprintln(w)
	}
}

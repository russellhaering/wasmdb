package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/russellhaering/wasmdb/internal/cli"
	"github.com/russellhaering/wasmdb/internal/cli/httpbackend"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	// Load config file (best-effort; missing file is fine).
	cfg, _ := cli.LoadConfig()
	if cfg == nil {
		cfg = &cli.Config{}
	}

	// Extract global flags (--url, --token) before passing to CLI engine.
	var url, token string
	var remaining []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--url":
			if i+1 >= len(args) {
				return fmt.Errorf("--url requires a value")
			}
			i++
			url = args[i]
		case "--token":
			if i+1 >= len(args) {
				return fmt.Errorf("--token requires a value")
			}
			i++
			token = args[i]
		default:
			remaining = append(remaining, args[i])
		}
	}

	// URL resolution: --url flag > WASMDB_URL env > config file > default.
	if url == "" {
		url = os.Getenv("WASMDB_URL")
	}
	if url == "" {
		url = cfg.URL
	}
	if url == "" {
		url = "http://localhost:8080"
	}

	// Token resolution: --token flag > WASMDB_API_TOKEN env > stored credentials.
	if token == "" {
		token = os.Getenv("WASMDB_API_TOKEN")
	}
	if token == "" {
		if creds, err := cli.LoadCredentials(url); err == nil {
			token = creds.Token
		}
	}

	backend := httpbackend.New(url, token)

	// Check if config specifies default JSON output.
	jsonDefault := cfg.DefaultFormat == "json"

	return cli.Run(ctx, remaining, cli.RunConfig{
		Backend:     backend,
		ServerURL:   url,
		Token:       token,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Stdin:       os.Stdin,
		JSONDefault: jsonDefault,
	})
}

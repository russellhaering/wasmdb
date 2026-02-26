package cli

import (
	"bytes"
	"fmt"
	"net/http"
)

func init() {
	register(command{
		noun:        "logout",
		verb:        "",
		usage:       "wasmdb logout",
		description: "Log out of the WasmDB server",
		run:         runLogout,
	})
}

func runLogout(ctx *cmdContext) error {
	serverURL := ctx.flag("url")
	if serverURL == "" {
		return fmt.Errorf("server URL is required (use --url or WASMDB_URL)")
	}

	creds, err := LoadCredentials(serverURL)
	if err != nil {
		// No credentials stored — nothing to do.
		fmt.Fprintln(ctx.stdout, "Not logged in.")
		return nil
	}

	// Call server logout endpoint to invalidate the session.
	req, err := http.NewRequestWithContext(ctx, "POST", serverURL+"/v1/auth/logout", bytes.NewReader(nil))
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+creds.Token)
		http.DefaultClient.Do(req)
	}

	if err := DeleteCredentials(serverURL); err != nil {
		return fmt.Errorf("delete credentials: %w", err)
	}

	fmt.Fprintln(ctx.stdout, "Logged out.")
	return nil
}

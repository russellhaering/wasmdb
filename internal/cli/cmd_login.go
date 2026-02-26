package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

func init() {
	register(command{
		noun:        "login",
		verb:        "",
		usage:       "wasmdb login [--email EMAIL --password PASS]",
		description: "Authenticate with the WasmDB server",
		run:         runLogin,
	})
}

func runLogin(ctx *cmdContext) error {
	serverURL := ctx.flag("url")
	if serverURL == "" {
		return fmt.Errorf("server URL is required (use --url or WASMDB_URL)")
	}

	email := ctx.flag("email")
	password := ctx.flag("password")

	// If email/password provided, do direct login (headless mode).
	if email != "" && password != "" {
		return loginDirect(ctx, serverURL, email, password)
	}

	// Browser-based login flow.
	return loginBrowser(ctx, serverURL)
}

func loginDirect(ctx *cmdContext, serverURL, email, password string) error {
	token, userEmail, err := doLoginRequest(ctx, serverURL, email, password)
	if err != nil {
		return err
	}

	if err := SaveCredentials(serverURL, token); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	fmt.Fprintf(ctx.stdout, "Logged in as %s\n", userEmail)
	return nil
}

func loginBrowser(ctx *cmdContext, serverURL string) error {
	// Generate state parameter.
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return fmt.Errorf("generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	// Find a free port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Channel to receive the token.
	type callbackResult struct {
		token string
		err   error
	}
	resultCh := make(chan callbackResult, 1)

	// Start local HTTP server for callback.
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		receivedState := r.URL.Query().Get("state")
		receivedToken := r.URL.Query().Get("token")

		if receivedState != state {
			w.WriteHeader(400)
			fmt.Fprint(w, "Invalid state parameter.")
			resultCh <- callbackResult{err: fmt.Errorf("state mismatch")}
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><body style="font-family:sans-serif;text-align:center;padding:40px;"><h2>Login successful!</h2><p>You can close this tab.</p></body></html>`)
		resultCh <- callbackResult{token: receivedToken}
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: mux,
	}

	go srv.ListenAndServe()
	defer srv.Shutdown(context.Background())

	// Open browser.
	loginURL := fmt.Sprintf("%s/auth/cli-login?port=%d&state=%s", serverURL, port, state)
	fmt.Fprintf(ctx.stderr, "Opening browser to login...\n")
	fmt.Fprintf(ctx.stderr, "If the browser doesn't open, visit: %s\n", loginURL)
	openBrowser(loginURL)

	// Wait for callback.
	select {
	case result := <-resultCh:
		if result.err != nil {
			return result.err
		}

		if err := SaveCredentials(serverURL, result.token); err != nil {
			return fmt.Errorf("save credentials: %w", err)
		}

		fmt.Fprintf(ctx.stdout, "Login successful! Credentials saved.\n")
		return nil

	case <-time.After(5 * time.Minute):
		return fmt.Errorf("login timed out")

	case <-ctx.Done():
		return ctx.Err()
	}
}

type loginAPIResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	User      struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

func doLoginRequest(ctx *cmdContext, serverURL, email, password string) (token, userEmail string, err error) {
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", serverURL+"/v1/auth/login", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var apiErr struct {
			Message string `json:"message"`
		}
		json.NewDecoder(resp.Body).Decode(&apiErr)
		msg := apiErr.Message
		if msg == "" {
			msg = resp.Status
		}
		return "", "", fmt.Errorf("login failed: %s", msg)
	}

	var loginResp loginAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return "", "", fmt.Errorf("decode login response: %w", err)
	}

	return loginResp.Token, loginResp.User.Email, nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	cmd.Start()
}

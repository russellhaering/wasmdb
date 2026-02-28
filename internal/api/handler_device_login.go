package api

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// deviceLogin tracks a pending device login flow.
type deviceLogin struct {
	code      string
	createdAt time.Time
	token     string // set when the user completes login
	email     string // set when the user completes login
}

const deviceLoginTTL = 5 * time.Minute

var (
	deviceLogins   = make(map[string]*deviceLogin)
	deviceLoginsMu sync.Mutex
)

func generateCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pruneExpiredDeviceLogins() {
	now := time.Now()
	for code, dl := range deviceLogins {
		if now.Sub(dl.createdAt) > deviceLoginTTL {
			delete(deviceLogins, code)
		}
	}
}

// POST /v1/auth/device-login — start a device login flow.
// No auth required. Returns {"device_code": "...", "login_url": "...", "expires_in": 300}.
func (s *Server) handleDeviceLoginStart(w http.ResponseWriter, r *http.Request) {
	code, err := generateCode()
	if err != nil {
		writeErrorMsg(w, 500, "internal_error", "failed to generate code")
		return
	}

	deviceLoginsMu.Lock()
	pruneExpiredDeviceLogins()
	deviceLogins[code] = &deviceLogin{code: code, createdAt: time.Now()}
	deviceLoginsMu.Unlock()

	// Build the login URL from the request host.
	scheme := "https"
	if r.TLS == nil {
		if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
			scheme = fwd
		} else {
			scheme = "http"
		}
	}
	loginURL := fmt.Sprintf("%s://%s/auth/cli-login?device_code=%s", scheme, r.Host, code)

	writeJSON(w, 200, map[string]any{
		"device_code": code,
		"login_url":   loginURL,
		"expires_in":  int(deviceLoginTTL.Seconds()),
	})
}

// GET /v1/auth/device-login/poll?code=CODE — poll for completion.
// No auth required. Returns 202 while pending, 200 with token on success, 410 if expired.
func (s *Server) handleDeviceLoginPoll(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeErrorMsg(w, 400, "bad_request", "code is required")
		return
	}

	deviceLoginsMu.Lock()
	dl, ok := deviceLogins[code]
	if ok && time.Since(dl.createdAt) > deviceLoginTTL {
		delete(deviceLogins, code)
		ok = false
	}
	deviceLoginsMu.Unlock()

	if !ok {
		writeErrorMsg(w, 410, "gone", "device code expired or not found")
		return
	}

	if dl.token == "" {
		writeJSON(w, 202, map[string]string{"status": "pending"})
		return
	}

	// Login complete — clean up and return token.
	deviceLoginsMu.Lock()
	delete(deviceLogins, code)
	deviceLoginsMu.Unlock()

	writeJSON(w, 200, map[string]string{
		"token": dl.token,
		"email": dl.email,
	})
}

// POST /v1/auth/device-login/complete — called by the browser login page.
// No auth required (the login page authenticates via /v1/auth/login first).
func (s *Server) handleDeviceLoginComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceCode string `json:"device_code"`
		Token      string `json:"token"`
		Email      string `json:"email"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON")
		return
	}

	deviceLoginsMu.Lock()
	dl, ok := deviceLogins[req.DeviceCode]
	if ok && time.Since(dl.createdAt) > deviceLoginTTL {
		delete(deviceLogins, req.DeviceCode)
		ok = false
	}
	if ok {
		dl.token = req.Token
		dl.email = req.Email
	}
	deviceLoginsMu.Unlock()

	if !ok {
		writeErrorMsg(w, 410, "gone", "device code expired or not found")
		return
	}

	writeJSON(w, 200, map[string]string{"status": "ok"})
}

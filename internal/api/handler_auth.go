package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/russellhaering/wasmdb/internal/auth"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	User      struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	if req.Email == "" || req.Password == "" {
		writeErrorMsg(w, 400, "bad_request", "email and password are required")
		return
	}

	rawToken, session, err := auth.Login(r.Context(), s.registry, s.sessions, req.Email, req.Password)
	if err != nil {
		writeErrorMsg(w, 401, "unauthorized", "invalid email or password")
		return
	}

	// Set session cookie.
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     "wasmdb_session",
		Value:    rawToken,
		Path:     "/",
		MaxAge:   int(7 * 24 * time.Hour / time.Second),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	resp := loginResponse{
		Token:     rawToken,
		ExpiresAt: session.ExpiresAt.Format(time.RFC3339),
	}
	resp.User.ID = session.UserID
	resp.User.Email = session.UserEmail

	writeJSON(w, 200, resp)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	var rawToken string

	if cookie, err := r.Cookie("wasmdb_session"); err == nil && cookie.Value != "" {
		rawToken = cookie.Value
	}
	if rawToken == "" {
		header := r.Header.Get("Authorization")
		if strings.HasPrefix(header, "Bearer ") {
			rawToken = header[len("Bearer "):]
		}
	}

	if rawToken != "" {
		_ = s.sessions.DeleteSession(r.Context(), rawToken)
	}

	// Clear cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "wasmdb_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	w.WriteHeader(204)
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	session := SessionFromContext(r.Context())
	if session == nil {
		writeError(w, ErrUnauthorized)
		return
	}

	writeJSON(w, 200, map[string]any{
		"id":    session.UserID,
		"email": session.UserEmail,
	})
}

func (s *Server) handleCLILoginPage(w http.ResponseWriter, r *http.Request) {
	port := r.URL.Query().Get("port")
	state := r.URL.Query().Get("state")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(cliLoginHTML(port, state)))
}

func cliLoginHTML(port, state string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>WasmDB Login</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background: #0f1117;
    color: #e4e4e7;
    height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .card {
    background: #18181b;
    border: 1px solid #27272a;
    border-radius: 12px;
    padding: 32px;
    width: 360px;
  }
  h1 { font-size: 20px; margin-bottom: 8px; color: #fafafa; }
  p { font-size: 14px; color: #a1a1aa; margin-bottom: 20px; }
  label { display: block; font-size: 13px; color: #a1a1aa; margin-bottom: 4px; }
  input {
    width: 100%;
    padding: 10px 12px;
    border-radius: 8px;
    border: 1px solid #3f3f46;
    background: #27272a;
    color: #fafafa;
    font-size: 14px;
    outline: none;
    margin-bottom: 14px;
  }
  input:focus { border-color: #3b82f6; }
  button {
    width: 100%;
    padding: 10px;
    border-radius: 8px;
    border: none;
    background: #3b82f6;
    color: white;
    font-weight: 600;
    font-size: 14px;
    cursor: pointer;
  }
  button:hover { background: #2563eb; }
  button:disabled { background: #3f3f46; color: #71717a; cursor: not-allowed; }
  .error { color: #fca5a5; font-size: 13px; margin-top: 10px; display: none; }
  .success { color: #86efac; font-size: 14px; text-align: center; }
</style>
</head>
<body>
<div class="card">
  <h1>WasmDB Login</h1>
  <p>Sign in to authorize the CLI.</p>
  <div id="form">
    <label for="email">Email</label>
    <input id="email" type="email" placeholder="you@example.com" autofocus>
    <label for="password">Password</label>
    <input id="password" type="password" placeholder="Password">
    <button id="login-btn" onclick="doLogin()">Sign In</button>
    <div id="error" class="error"></div>
  </div>
  <div id="success" style="display:none;" class="success">
    <p>Login successful! You can close this tab.</p>
  </div>
</div>
<script>
const PORT = ` + jsonStr(port) + `;
const STATE = ` + jsonStr(state) + `;

document.getElementById('password').addEventListener('keydown', (e) => {
  if (e.key === 'Enter') doLogin();
});

async function doLogin() {
  const email = document.getElementById('email').value.trim();
  const password = document.getElementById('password').value;
  const errEl = document.getElementById('error');
  const btn = document.getElementById('login-btn');

  if (!email || !password) {
    errEl.textContent = 'Email and password are required.';
    errEl.style.display = 'block';
    return;
  }

  btn.disabled = true;
  errEl.style.display = 'none';

  try {
    const resp = await fetch('/v1/auth/login', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({email, password})
    });
    const data = await resp.json();
    if (!resp.ok) {
      errEl.textContent = data.message || 'Login failed.';
      errEl.style.display = 'block';
      btn.disabled = false;
      return;
    }

    document.getElementById('form').style.display = 'none';
    document.getElementById('success').style.display = 'block';

    if (PORT && STATE) {
      window.location.href = 'http://localhost:' + PORT + '/callback?token=' + encodeURIComponent(data.token) + '&state=' + encodeURIComponent(STATE);
    }
  } catch (e) {
    errEl.textContent = 'Connection error: ' + e.message;
    errEl.style.display = 'block';
    btn.disabled = false;
  }
}
</script>
</body>
</html>`
}

func jsonStr(s string) string {
	// Simple JSON string escaping for embedding in JS.
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return `"` + s + `"`
}

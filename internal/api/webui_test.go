package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUIShellPages checks that the dashboard and chat shells are served
// unauthenticated with the correct content type and non-empty bodies.
func TestUIShellPages(t *testing.T) {
	srv, _ := setupTestServer(t)

	cases := []struct {
		path      string
		wantCT    string
		wantSubst string
	}{
		{"/ui", "text/html", "/ui/assets/surface.js"},
		{"/chat", "text/html", "/ui/assets/chat.js"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", tc.path, nil)
		srv.httpServer.Handler.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("%s: status = %d, want 200", tc.path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, tc.wantCT) {
			t.Errorf("%s: content-type = %q, want prefix %q", tc.path, ct, tc.wantCT)
		}
		body := w.Body.String()
		if len(body) == 0 {
			t.Errorf("%s: empty body", tc.path)
		}
		if !strings.Contains(body, tc.wantSubst) {
			t.Errorf("%s: body does not reference %q", tc.path, tc.wantSubst)
		}
	}
}

// TestUIAssets checks that each embedded asset is served with the right content
// type and a non-empty body.
func TestUIAssets(t *testing.T) {
	srv, _ := setupTestServer(t)

	cases := []struct {
		file   string
		wantCT string
	}{
		{"surface.js", "text/javascript"},
		{"dashboard.js", "text/javascript"},
		{"chat.js", "text/javascript"},
		{"auth.js", "text/javascript"},
		{"dashboard.html", "text/html"},
		{"chat.html", "text/html"},
		{"shared.css", "text/css"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/ui/assets/"+tc.file, nil)
		srv.httpServer.Handler.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Fatalf("/ui/assets/%s: status = %d, want 200", tc.file, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, tc.wantCT) {
			t.Errorf("/ui/assets/%s: content-type = %q, want prefix %q", tc.file, ct, tc.wantCT)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("/ui/assets/%s: cache-control = %q, want no-cache", tc.file, cc)
		}
		if w.Body.Len() == 0 {
			t.Errorf("/ui/assets/%s: empty body", tc.file)
		}
	}
}

// TestUIAssetsAuthAllowlisted confirms assets are reachable without a session
// (the prefix is on the auth allowlist).
func TestUIAssetsAuthAllowlisted(t *testing.T) {
	srv, _ := setupTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/assets/surface.js", nil) // no auth header/cookie
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("unauthenticated asset fetch: status = %d, want 200", w.Code)
	}
}

// TestUIAssetUnknown404 checks that an unknown asset yields 404.
func TestUIAssetUnknown404(t *testing.T) {
	srv, _ := setupTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/assets/does-not-exist.js", nil)
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("unknown asset: status = %d, want 404", w.Code)
	}
}

// TestSurfaceJSContract is a smoke assertion that surface.js exposes the public
// contract the pages depend on.
func TestSurfaceJSContract(t *testing.T) {
	srv, _ := setupTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/assets/surface.js", nil)
	srv.httpServer.Handler.ServeHTTP(w, req)
	body := w.Body.String()
	for _, want := range []string{"SurfaceUI", "mount", "/render", "/actions/"} {
		if !strings.Contains(body, want) {
			t.Errorf("surface.js does not contain %q", want)
		}
	}
}

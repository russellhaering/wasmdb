package api

import (
	"net/http"

	"github.com/russellhaering/wasmdb/internal/api/webui"
)

// handleDashboardUI serves the dashboard shell HTML. The page is an unauth
// shell; its API calls authenticate via the wasmdb_session cookie.
func (s *Server) handleDashboardUI(w http.ResponseWriter, r *http.Request) {
	serveAsset(w, "dashboard.html")
}

// handleChatUI serves the chat shell HTML.
func (s *Server) handleChatUI(w http.ResponseWriter, r *http.Request) {
	serveAsset(w, "chat.html")
}

// handleUIAsset serves an embedded frontend asset (surface.js, shared.css, …)
// from /ui/assets/{file...}. Unknown files 404. Cache-Control is no-cache so
// updates take effect without cache-busting.
func (s *Server) handleUIAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if name == "" {
		http.NotFound(w, r)
		return
	}
	serveAsset(w, name)
}

func serveAsset(w http.ResponseWriter, name string) {
	data, err := webui.File(name)
	if err != nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", webui.ContentType(name))
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

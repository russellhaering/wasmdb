// Package webui serves the shared browser frontend for wasmdb: a single
// component renderer (surface.js) plus the dashboard and chat pages. All assets
// are embedded via embed.FS so they ship in the binary yet remain diffable and
// testable as ordinary files.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed assets
var assetsFS embed.FS

// assets is the embedded filesystem rooted at the assets/ directory.
var assets = func() fs.FS {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic("webui: embed assets: " + err.Error())
	}
	return sub
}()

// File returns the bytes of an embedded asset by its name relative to the
// assets directory (e.g. "surface.js", "dashboard.html").
func File(name string) ([]byte, error) {
	return fs.ReadFile(assets, path.Clean(name))
}

// ContentType returns the HTTP content type for an asset file name based on its
// extension. It covers every asset type the package ships.
func ContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// Handler serves embedded assets. It expects the request path to already have
// the routing prefix stripped, so r.URL.Path names the asset file. Unknown
// files yield 404. Every response carries Cache-Control: no-cache — we favor
// simplicity over cache-busting.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" || strings.Contains(name, "..") {
			http.NotFound(w, r)
			return
		}
		data, err := File(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ContentType(name))
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(data)
	})
}

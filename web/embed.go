package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed static
var staticFS embed.FS

// StaticHandler serves the embedded frontend assets.
func StaticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}

// ServeStatic serves a static file, falling back to index.html for SPA routes.
func ServeStatic(w http.ResponseWriter, r *http.Request) {
	// Clean the path and prevent directory traversal.
	p := path.Clean("/" + r.URL.Path)
	if p == "/" {
		p = "/index.html"
	}

	// If the requested file exists, serve it; otherwise serve index.html.
	if _, err := fs.Stat(staticFS, "static"+p); err == nil {
		StaticHandler().ServeHTTP(w, r)
		return
	}

	// SPA fallback: serve index.html for any non-API route.
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/index.html"
		StaticHandler().ServeHTTP(w, r2)
		return
	}

	http.NotFound(w, r)
}
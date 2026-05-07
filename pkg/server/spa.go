package server

import (
	"io/fs"
	"net/http"
	"strings"
)

// SPAHandler serves a single-page application from an embedded filesystem.
// It serves static files when they exist, and falls back to index.html
// for client-side routing.
func SPAHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Check if the file exists
		f, err := fsys.Open(path)
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// For non-file paths (no extension), serve index.html for SPA routing
		if !strings.Contains(path, ".") {
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}

		// File not found
		http.NotFound(w, r)
	})
}

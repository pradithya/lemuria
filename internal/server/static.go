package server

import (
	"io/fs"
	"net/http"
	"strings"

	lemuria "github.com/org/lemuria"
)

// setupStaticFiles configures the static file server for the frontend.
func (s *Server) setupStaticFiles() {
	// Get the embedded static files
	staticContent, err := fs.Sub(lemuria.StaticFS, "static")
	if err != nil {
		s.logger.Error("failed to access embedded static files", "error", err)
		return
	}

	// Create file server
	fileServer := http.FileServer(http.FS(staticContent))

	// Serve static files with SPA fallback
	s.router.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		// Skip API and auth routes
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/") ||
			strings.HasPrefix(path, "/auth/") ||
			strings.HasPrefix(path, "/webhook") ||
			strings.HasPrefix(path, "/health") ||
			strings.HasPrefix(path, "/ready") {
			http.NotFound(w, r)
			return
		}

		// Try to serve the file directly
		// For paths like /locks, /admin, serve index.html (SPA routing)
		filePath := strings.TrimPrefix(path, "/")
		if filePath == "" {
			filePath = "index.html"
		}

		// Check if file exists
		if _, err := fs.Stat(staticContent, filePath); err != nil {
			// File doesn't exist, serve index.html for SPA routing
			r.URL.Path = "/"
		}

		fileServer.ServeHTTP(w, r)
	})

	s.logger.Info("static file server configured")
}

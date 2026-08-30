package main

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/fusuycorp/pikpik/web"
)

// FallbackHTML is returned if the embed.FS is unpopulated during testing or development.
const FallbackHTML = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><title>Pikpik Control Plane</title></head>
<body style="background:#09090b;color:#f4f4f5;font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;">
  <div style="text-align:center;">
    <h1 style="font-family:monospace;color:#22d3ee;">pikpik control plane</h1>
    <p style="color:#a1a1aa;font-size:14px;">Frontend assets are compiling...</p>
  </div>
</body>
</html>`

// NewStaticHandler creates an SPA fallback HTTP handler that serves pre-built embedded static assets
// with proper Cache-Control headers and falls back to index.html for client-side routing.
func NewStaticHandler() http.Handler {
	distFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, FallbackHTML)
		})
	}

	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		cleanPath := path.Clean(r.URL.Path)
		trimmedPath := strings.TrimPrefix(cleanPath, "/")

		// Direct request to root or index.html
		if trimmedPath == "" || trimmedPath == "index.html" {
			serveIndexHTML(w, distFS)
			return
		}

		// Check if the requested file exists in embedded dist
		file, err := distFS.Open(trimmedPath)
		if err == nil {
			stat, statErr := file.Stat()
			_ = file.Close()
			if statErr == nil && !stat.IsDir() {
				// Caching policy: Immutable long-term caching for hashed /assets/, short for others
				if strings.HasPrefix(cleanPath, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "public, max-age=3600")
				}

				ext := filepath.Ext(trimmedPath)
				if mimeType := mime.TypeByExtension(ext); mimeType != "" {
					w.Header().Set("Content-Type", mimeType)
				}

				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// SPA Fallback: Never serve HTML fallback for API or WebSocket routes
		if strings.HasPrefix(cleanPath, "/api/") || strings.HasPrefix(cleanPath, "/ws") || strings.HasPrefix(cleanPath, "/healthz") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not_found","message":"API or WebSocket endpoint not found"}`)
			return
		}

		// Serve index.html for all client-side routed paths
		serveIndexHTML(w, distFS)
	})
}

func serveIndexHTML(w http.ResponseWriter, distFS fs.FS) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	indexFile, err := distFS.Open("index.html")
	if err != nil {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, FallbackHTML)
		return
	}
	defer indexFile.Close()

	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, indexFile)
}

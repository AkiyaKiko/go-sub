package main

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed web/dist
var embeddedWebFS embed.FS

func NewEmbeddedWebHandler() (http.Handler, error) {
	distFS, err := fs.Sub(embeddedWebFS, "web/dist")
	if err != nil {
		return nil, err
	}

	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		cleanPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		relativePath := strings.TrimPrefix(cleanPath, "/")

		if relativePath == "" {
			serveEmbeddedIndex(w, r, distFS)
			return
		}

		if stat, statErr := fs.Stat(distFS, relativePath); statErr == nil && !stat.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: unknown route returns index.html
		serveEmbeddedIndex(w, r, distFS)
	}), nil
}

func serveEmbeddedIndex(w http.ResponseWriter, r *http.Request, webFS fs.FS) {
	content, err := fs.ReadFile(webFS, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(content)
}

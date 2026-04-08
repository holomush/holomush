// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// FileServer returns an HTTP handler that serves static files from an embedded
// FS or an override directory. If webDir is non-empty the filesystem at that
// path is used instead of the embedded build. Requests for unknown paths that
// lack a file extension fall back to index.html to support client-side routing.
//
// The embedded FS root is selected at compile time via build tags: the default
// build (no tags) embeds internal/web/placeholder/ as a tracked stub so
// `go build ./cmd/holomush` compiles on a fresh checkout; builds with
// `-tags realweb` embed internal/web/dist/ after `task web:embed` has
// populated it with the real SvelteKit bundle. See static_placeholder.go and
// static_realweb.go.
func FileServer(webDir string) http.Handler {
	var root fs.FS
	if webDir != "" {
		root = os.DirFS(webDir)
	} else {
		root = embeddedRoot()
	}

	fileServer := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		f, err := root.Open(path)
		if err == nil {
			if closeErr := f.Close(); closeErr != nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback — only for paths without file extensions
		if filepath.Ext(path) == "" {
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	})
}

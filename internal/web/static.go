// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:dist
var embeddedFS embed.FS

// FileServer returns an HTTP handler that serves static files from an embedded
// FS or an override directory. If webDir is non-empty the filesystem at that
// path is used instead of the embedded build. Requests for unknown paths that
// lack a file extension fall back to index.html to support client-side routing.
func FileServer(webDir string) http.Handler {
	var root fs.FS
	if webDir != "" {
		root = os.DirFS(webDir)
	} else {
		sub, err := fs.Sub(embeddedFS, "dist")
		if err != nil {
			panic("web: failed to sub embedded FS: " + err.Error())
		}
		root = sub
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

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build realweb

// Real-web build: embed the real SvelteKit bundle from internal/web/dist/.
// This build tag is set by `task build` / `task docker:build` after
// `task web:embed` has copied web/build/ into internal/web/dist/. Building
// with this tag when dist/ is empty will fail at compile time with
// "pattern all:dist: no matching files found" — run `task web:embed` first.
//
// internal/web/dist/ is fully gitignored; it is an ephemeral build artifact,
// not source.

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

func embeddedRoot() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("web: failed to sub dist FS: " + err.Error())
	}
	return sub
}

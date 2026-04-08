// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !realweb

// Default build: embed the tracked placeholder directory. This lets
// `go build ./cmd/holomush` succeed on a fresh checkout without requiring
// `task web:embed` to have populated internal/web/dist/ first.
//
// The resulting binary serves a static "web bundle not built" page at / and
// returns 404 for every other path. To produce a binary with the real
// SvelteKit UI, use `task build` / `task docker:build` which run
// `web:embed` first and then pass `-tags realweb` to `go build`.

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:placeholder
var placeholderFS embed.FS

func embeddedRoot() fs.FS {
	sub, err := fs.Sub(placeholderFS, "placeholder")
	if err != nil {
		panic("web: failed to sub placeholder FS: " + err.Error())
	}
	return sub
}

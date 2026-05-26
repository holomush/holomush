// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// assemblePluginsDir and related helpers implement the WithInTreePlugins
// capability; see harness.go for the Server lifecycle they plug into.
package integrationtest

import (
	"io"
	"os"
	"path/filepath"

	"github.com/samber/oops"
)

// assemblePluginsDir builds a unified plugins directory under dst by copying
// the source plugins tree (Lua/setting manifests + Lua source) then overlaying
// the compiled binary artifacts, mirroring the production image overlay
// (Dockerfile COPY plugins/ then COPY build/plugins/). All copies are real
// files: Manager.Discover skips symlinked dirs and goplugin.Host rejects
// symlinked binaries.
func assemblePluginsDir(dst, srcPlugins, buildPlugins string) error {
	if err := copyTree(srcPlugins, dst); err != nil {
		return oops.Code("PLUGINS_DIR_COPY_SOURCE").Wrap(err)
	}
	// build/plugins may be absent when binaries are not built; the binary gate
	// (Task 2) handles that case. Overlay only if present.
	if _, err := os.Stat(buildPlugins); err == nil {
		if err := copyTree(buildPlugins, dst); err != nil {
			return oops.Code("PLUGINS_DIR_OVERLAY_BUILD").Wrap(err)
		}
	}
	return nil
}

// copyTree recursively copies src into dst, creating dst dirs as needed.
// Existing dst files are overwritten (overlay semantics).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error { //nolint:wrapcheck // test helper: walk errors are filesystem errors from t.TempDir paths
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err //nolint:wrapcheck // test helper: Rel only errors on unrooted paths, not possible here
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755) //nolint:gosec // test-only: 0o755 matches plugin dir permissions in the real tree
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err //nolint:wrapcheck // test helper: OS errors pass through directly
	}
	defer in.Close() //nolint:errcheck // read-only; close error inconsequential
	dstDir := filepath.Dir(dst)
	if mkErr := os.MkdirAll(dstDir, 0o755); mkErr != nil { //nolint:gosec // test-only: 0o755 matches plugin dir permissions
		return mkErr //nolint:wrapcheck // test helper: OS errors pass through directly
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err //nolint:wrapcheck // test helper: OS errors pass through directly
	}
	defer out.Close() //nolint:errcheck // final close below is the authoritative error check
	if _, cpErr := io.Copy(out, in); cpErr != nil {
		return cpErr //nolint:wrapcheck // test helper: IO errors pass through directly
	}
	return out.Close() //nolint:wrapcheck // test helper: final flush/close error is surfaced directly
}

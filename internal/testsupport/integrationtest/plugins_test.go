// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssemblePluginsDirOverlaysSourceAndBuild(t *testing.T) {
	root := t.TempDir()
	// Fake source tree: two plugin dirs with manifests.
	srcPlugins := filepath.Join(root, "plugins")
	require.NoError(t, os.MkdirAll(filepath.Join(srcPlugins, "core-help"), 0o755))                                                  //nolint:gosec // test-only: mirroring real plugin dir permissions
	require.NoError(t, os.MkdirAll(filepath.Join(srcPlugins, "core-scenes"), 0o755))                                                //nolint:gosec // test-only: mirroring real plugin dir permissions
	require.NoError(t, os.WriteFile(filepath.Join(srcPlugins, "core-help", "plugin.yaml"), []byte("name: core-help\n"), 0o644))     //nolint:gosec // test-only: yaml manifests are not sensitive
	require.NoError(t, os.WriteFile(filepath.Join(srcPlugins, "core-scenes", "plugin.yaml"), []byte("name: core-scenes\n"), 0o644)) //nolint:gosec // test-only: yaml manifests are not sensitive
	// Fake build tree: core-scenes binary overlay.
	buildPlugins := filepath.Join(root, "build", "plugins")
	require.NoError(t, os.MkdirAll(filepath.Join(buildPlugins, "core-scenes", "linux-amd64"), 0o755))                                //nolint:gosec // test-only: mirroring real plugin dir permissions
	require.NoError(t, os.WriteFile(filepath.Join(buildPlugins, "core-scenes", "linux-amd64", "core-scenes"), []byte("ELF"), 0o755)) //nolint:gosec // test-only: executable bit required for plugin binary simulation

	dst := t.TempDir()
	err := assemblePluginsDir(dst, srcPlugins, buildPlugins)
	require.NoError(t, err)

	// Both source manifests present.
	require.FileExists(t, filepath.Join(dst, "core-help", "plugin.yaml"))
	require.FileExists(t, filepath.Join(dst, "core-scenes", "plugin.yaml"))
	// Binary overlay present in the same plugin dir.
	require.FileExists(t, filepath.Join(dst, "core-scenes", "linux-amd64", "core-scenes"))
	// No symlinks (Discover skips them).
	info, err := os.Lstat(filepath.Join(dst, "core-scenes"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Zero(t, info.Mode()&os.ModeSymlink, "plugin dir must be a real dir, not a symlink")
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// This file is intentionally NOT build-tagged `//go:build integration`, unlike
// the rest of this package. It is a source-scanning meta-test that only reads
// plugins.go as a file (no integration symbols, no DB/bus/Docker), so it runs in
// the fast `task test` lane to guard INV-PLUGIN-18 on every build.

package integrationtest

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWithInTreePluginsReusesSubsystem enforces INV-PLUGIN-18: the capability MUST
// reuse setup.PluginSubsystem and MUST NOT construct plugins.NewManager directly
// in this package. Guards against a future refactor that silently forks the
// production wiring.
func TestWithInTreePluginsReusesSubsystem(t *testing.T) {
	src, err := os.ReadFile("plugins.go")
	require.NoError(t, err)
	body := string(src)
	require.Contains(t, body, "pluginsetup.NewPluginSubsystem(",
		"INV-PLUGIN-18: capability must construct the PluginSubsystem")
	require.NotContains(t, body, "plugins.NewManager(",
		"INV-PLUGIN-18: capability must not construct plugins.NewManager directly — reuse PluginSubsystem")
}

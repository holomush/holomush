// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStartWithoutPluginsLeavesHarnessPluginFree enforces INV-PLUGIN-21: omitting
// WithInTreePlugins yields a plugin-free harness; the plugin accessors panic.
func TestStartWithoutPluginsLeavesHarnessPluginFree(t *testing.T) {
	srv := Start(t)
	defer srv.Stop()
	require.Nil(t, srv.pluginSub, "default Start must not start the plugin subsystem")
	require.Panics(t, func() { _ = srv.PluginManager() },
		"PluginManager must panic when plugins were not requested")
}

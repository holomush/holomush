// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootHasPluginSubcommand(t *testing.T) {
	root := NewRootCmd()
	plugin, _, err := root.Find([]string{"plugin"})
	require.NoError(t, err)
	require.NotNil(t, plugin)
	assert.Equal(t, "plugin", plugin.Name())
}

func TestPluginCommandPrintsHelp(t *testing.T) {
	out, code := runCmd(t, []string{"plugin", "--help"})
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "plugin")
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginEventsListPrintsAllManifestEmits(t *testing.T) {
	// runCmd is defined in test_helper_test.go.
	// Tests run from cmd/holomush/, so ../../plugins is the repo's plugins dir.
	out, code := runCmd(t, []string{
		"plugin", "events", "list",
		"--plugin-dir", "../../plugins",
	})
	require.Equal(t, 0, code, "output:\n%s", out)
	require.Contains(t, out, "core-communication:whisper")
	require.Contains(t, out, "always")
	require.Contains(t, out, "core-objects:object_create")
	require.Contains(t, out, "never")
}

func TestPluginEventsListFiltersBySensitivity(t *testing.T) {
	out, code := runCmd(t, []string{
		"plugin", "events", "list",
		"--plugin-dir", "../../plugins",
		"--sensitivity", "always",
	})
	require.Equal(t, 0, code)
	require.Contains(t, out, "core-communication:whisper")
	assert.False(t, strings.Contains(out, "object_create"),
		"never-sensitivity events must not appear in --sensitivity=always output:\n%s", out)
}

func TestPluginEventsShowPrintsDetail(t *testing.T) {
	out, code := runCmd(t, []string{
		"plugin", "events", "show",
		"--plugin-dir", "../../plugins",
		"core-communication:whisper",
	})
	require.Equal(t, 0, code)
	require.Contains(t, out, "private message")
	require.Contains(t, out, "core-communication")
	require.Contains(t, out, "always")
}

func TestPluginEventsShowFailsForUnknownEvent(t *testing.T) {
	out, code := runCmd(t, []string{
		"plugin", "events", "show",
		"--plugin-dir", "../../plugins",
		"core-communication:no-such-event",
	})
	require.NotEqual(t, 0, code)
	assert.NotEmpty(t, out)
}

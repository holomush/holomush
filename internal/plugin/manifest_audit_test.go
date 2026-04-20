// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestParseManifestAcceptsBinaryPluginWithAuditBlock(t *testing.T) {
	t.Parallel()
	data := []byte(`
name: core-scenes
version: 1.0.0
type: binary
binary-plugin:
  executable: core-scenes
audit:
  - subjects: ["events.*.scene.>"]
    schema: plugin_core_scenes
    table: scene_log
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	require.Len(t, m.Audit, 1)
	assert.Equal(t, []string{"events.*.scene.>"}, m.Audit[0].Subjects)
	assert.Equal(t, "plugin_core_scenes", m.Audit[0].Schema)
	assert.Equal(t, "scene_log", m.Audit[0].Table)
}

func TestParseManifestRejectsLuaPluginWithAuditBlock(t *testing.T) {
	t.Parallel()
	data := []byte(`
name: some-lua
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
audit:
  - subjects: ["events.*.foo.>"]
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary plugins")
}

func TestParseManifestRejectsEmptyAuditSubjects(t *testing.T) {
	t.Parallel()
	data := []byte(`
name: bad-audit
version: 1.0.0
type: binary
binary-plugin:
  executable: bad-audit
audit:
  - subjects: []
    schema: bad
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one subject pattern")
}

func TestParseManifestRejectsEmptySubjectString(t *testing.T) {
	t.Parallel()
	data := []byte(`
name: bad-audit
version: 1.0.0
type: binary
binary-plugin:
  executable: bad-audit
audit:
  - subjects: ["   "]
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

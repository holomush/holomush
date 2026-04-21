// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// TestManagerAuditSubjectsAggregatesAcrossLoadedPlugins verifies the
// Manager surfaces every (plugin, subject) pair declared via Audit
// blocks in plugin manifests. The host audit subsystem consumes this
// list to build the OwnerMap at startup.
func TestManagerAuditSubjectsAggregatesAcrossLoadedPlugins(t *testing.T) {
	t.Parallel()

	m := plugins.NewManager("")

	scenes := &plugins.Manifest{
		Name:    "core-scenes",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "core-scenes",
		},
		Audit: []plugins.AuditBlock{
			{Subjects: []string{"events.*.scene.>"}, Schema: "plugin_core_scenes", Table: "scene_log"},
		},
	}
	require.NoError(t, scenes.Validate())

	nochan := &plugins.Manifest{
		Name:    "core-nochan",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "core-nochan",
		},
	}
	require.NoError(t, nochan.Validate())

	m.TestLoadPlugin("core-scenes", scenes)
	m.TestLoadPlugin("core-nochan", nochan)

	decls := m.AuditSubjects()
	require.Len(t, decls, 1)
	assert.Equal(t, "core-scenes", decls[0].PluginName)
	assert.Equal(t, "events.*.scene.>", decls[0].Subject)
}

// TestManagerAuditSubjectsEmptyWhenNoPluginsDeclare verifies the manager
// returns a nil/empty slice when no loaded plugin declares an audit
// block. The caller (audit subsystem) treats this as "host owns
// everything" — the Phase A default.
func TestManagerAuditSubjectsEmptyWhenNoPluginsDeclare(t *testing.T) {
	t.Parallel()

	m := plugins.NewManager("")

	plain := &plugins.Manifest{
		Name:    "plain",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "plain",
		},
	}
	require.NoError(t, plain.Validate())
	m.TestLoadPlugin("plain", plain)

	assert.Empty(t, m.AuditSubjects())
}

// TestManagerAuditSubjectsFlattensMultipleSubjectsPerBlock verifies that
// a single audit block listing multiple subjects produces one
// declaration per subject (not one per block).
func TestManagerAuditSubjectsFlattensMultipleSubjectsPerBlock(t *testing.T) {
	t.Parallel()

	m := plugins.NewManager("")

	multi := &plugins.Manifest{
		Name:    "multi",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "multi",
		},
		Audit: []plugins.AuditBlock{
			{Subjects: []string{
				"events.*.scene.>",
				"events.*.scene-meta.>",
			}},
		},
	}
	require.NoError(t, multi.Validate())
	m.TestLoadPlugin("multi", multi)

	decls := m.AuditSubjects()
	require.Len(t, decls, 2)
	sort.Slice(decls, func(i, j int) bool { return decls[i].Subject < decls[j].Subject })
	assert.Equal(t, "events.*.scene-meta.>", decls[0].Subject)
	assert.Equal(t, "events.*.scene.>", decls[1].Subject)
}

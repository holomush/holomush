// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestCoreScenesManifestDeclaresQualifiedVerbsForEveryEmitType pins
// holomush-r0kup: every emitted scene wire type MUST have a qualified
// verbs[].type entry, or the production RenderingPublisher hard-fails
// EMIT_UNKNOWN_VERB. Before the fix core-scenes shipped no verbs: block at all.
func TestCoreScenesManifestDeclaresQualifiedVerbsForEveryEmitType(t *testing.T) {
	data, err := os.ReadFile("plugin.yaml")
	require.NoError(t, err)

	var m struct {
		Verbs []struct {
			Type string `yaml:"type"`
		} `yaml:"verbs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &m))

	got := make(map[string]bool, len(m.Verbs))
	for _, v := range m.Verbs {
		got[v.Type] = true
	}

	want := []string{
		"core-scenes:scene_pose", "core-scenes:scene_say", "core-scenes:scene_emit",
		"core-scenes:scene_ooc", "core-scenes:scene_join_ic", "core-scenes:scene_leave_ic",
		"core-scenes:scene_pose_order_changed_ic", "core-scenes:scene_idle_nudge",
		"core-scenes:scene_publish_started", "core-scenes:scene_publish_vote_cast",
		"core-scenes:scene_publish_cooloff_started", "core-scenes:scene_publish_resolved",
		"core-scenes:scene_publish_withdrawn", "core-scenes:scene_publish_vote_attempts_extended",
	}
	for _, w := range want {
		require.Truef(t, got[w], "missing qualified verb entry %q", w)
	}
}

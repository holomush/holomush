// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// TestPlugin_CryptoEmitsMatchesRegistry pins INV-P4-2: the 8 scene
// event types in crypto.emits MUST equal the set registered via
// EmitTypeRegistrar.
func TestPlugin_CryptoEmitsMatchesRegistry(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("plugin.yaml")
	require.NoError(t, err)

	var m struct {
		Crypto struct {
			Emits []struct {
				EventType   string `yaml:"event_type"`
				Sensitivity string `yaml:"sensitivity"`
			} `yaml:"emits"`
		} `yaml:"crypto"`
	}
	require.NoError(t, yaml.Unmarshal(data, &m))

	manifestSet := make([]string, 0, len(m.Crypto.Emits))
	for _, e := range m.Crypto.Emits {
		manifestSet = append(manifestSet, e.EventType)
	}
	sort.Strings(manifestSet)

	reg := pluginsdk.NewEmitRegistry()
	reg.RegisterEmitTypes(phase4EmitTypes())
	registrySet := reg.RegisteredEmitTypes()
	sort.Strings(registrySet)

	assert.Equal(t, manifestSet, registrySet,
		"INV-P4-2: manifest crypto.emits MUST equal EmitTypeRegistrar set")
}

// TestPlugin_SensitivityMatrix pins INV-P4-3: per-type sensitivity matches
// spec §2 table.
func TestPlugin_SensitivityMatrix(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("plugin.yaml")
	require.NoError(t, err)

	var m struct {
		Crypto struct {
			Emits []struct {
				EventType   string `yaml:"event_type"`
				Sensitivity string `yaml:"sensitivity"`
			} `yaml:"emits"`
		} `yaml:"crypto"`
	}
	require.NoError(t, yaml.Unmarshal(data, &m))

	want := map[string]string{
		"scene_pose":                  "always",
		"scene_say":                   "always",
		"scene_emit":                  "always",
		"scene_ooc":                   "always",
		"scene_join_ic":               "never",
		"scene_leave_ic":              "never",
		"scene_pose_order_changed_ic": "never",
		"scene_idle_nudge":            "never",
	}
	got := make(map[string]string)
	for _, e := range m.Crypto.Emits {
		got[e.EventType] = e.Sensitivity
	}
	assert.Equal(t, want, got,
		"INV-P4-3: sensitivity matrix MUST match spec §2 table")
}

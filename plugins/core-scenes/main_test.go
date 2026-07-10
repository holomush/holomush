// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// TestInitAppliesManifestConfig verifies that applyConfig decodes plugin_config
// into service.cfg and schedInterval correctly (INV-PLUGIN-7).
func TestInitAppliesManifestConfig(t *testing.T) {
	t.Parallel()
	p := &scenePlugin{service: &SceneServiceImpl{}}
	cfg := &pluginv1.ServiceConfig{PluginConfig: map[string]string{
		"vote_window": "168h", "cooloff_window": "30m", "scheduler_interval": "30s",
		"idle_timeout_default": "30m", "idle_nudge_enabled": "true",
	}}
	require.NoError(t, p.applyConfig(cfg))
	require.Equal(t, 168*time.Hour, p.service.cfg.DefaultVoteWindow)
	require.Equal(t, 30*time.Minute, p.service.cfg.DefaultCoolOffWindow)
	require.Equal(t, 30*time.Second, p.schedInterval)
	require.Equal(t, 30*time.Minute, p.idleTimeoutDefault)
	require.True(t, p.idleNudgeEnabled)
	require.NotZero(t, p.service.cfg.DefaultVoteWindow)
}

func TestApplyConfigRejectsNonPositiveIdleTimeoutDefault(t *testing.T) {
	t.Parallel()
	// idle_timeout_default is type-valid as a duration but 0s would flag every
	// active scene as idle immediately; applyConfig MUST reject it fail-loud.
	p := &scenePlugin{service: &SceneServiceImpl{}}
	cfg := &pluginv1.ServiceConfig{PluginConfig: map[string]string{
		"vote_window": "168h", "cooloff_window": "30m", "scheduler_interval": "30s",
		"idle_timeout_default": "0s",
	}}
	err := p.applyConfig(cfg)
	errutil.AssertErrorCode(t, err, "SCENE_INIT_FAILED")
}

func TestApplyConfigRejectsNonPositiveSchedulerInterval(t *testing.T) {
	t.Parallel()
	// scheduler_interval is type-valid as a duration but 0s would panic
	// time.NewTicker at scheduler start; applyConfig MUST reject it fail-loud.
	p := &scenePlugin{service: &SceneServiceImpl{}}
	cfg := &pluginv1.ServiceConfig{PluginConfig: map[string]string{
		"vote_window": "168h", "cooloff_window": "30m", "scheduler_interval": "0s",
	}}
	err := p.applyConfig(cfg)
	errutil.AssertErrorCode(t, err, "SCENE_INIT_FAILED")
}

// TestPlugin_CryptoEmitsMatchesRegistry pins INV-SCENE-2 / INV-PLUGIN-32: the scene
// event types in crypto.emits (8 Phase 4 + 6 Phase 6 publication notices)
// MUST equal the set registered via EmitTypeRegistrar.
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
	reg.RegisterEmitTypes(phase6EmitTypes())
	registrySet := reg.RegisteredEmitTypes()
	sort.Strings(registrySet)

	assert.Equal(t, manifestSet, registrySet,
		"INV-SCENE-2: manifest crypto.emits MUST equal EmitTypeRegistrar set")
}

// TestCoreScenesManifestDeclaresReadback pins INV-CRYPTO-27: the three IC content
// event types (scene_pose, scene_say, scene_emit) MUST declare readback:true so
// the host can decrypt historical snapshots on behalf of the plugin. scene_ooc
// is excluded because OOC content is never archived into the published scene log.
func TestCoreScenesManifestDeclaresReadback(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("plugin.yaml")
	require.NoError(t, err)

	var m struct {
		Crypto struct {
			Emits []struct {
				EventType   string `yaml:"event_type"`
				Sensitivity string `yaml:"sensitivity"`
				Readback    bool   `yaml:"readback"`
			} `yaml:"emits"`
		} `yaml:"crypto"`
	}
	require.NoError(t, yaml.Unmarshal(data, &m))

	byType := make(map[string]struct {
		Sensitivity string
		Readback    bool
	})
	for _, e := range m.Crypto.Emits {
		byType[e.EventType] = struct {
			Sensitivity string
			Readback    bool
		}{e.Sensitivity, e.Readback}
	}

	for _, et := range []string{"scene_pose", "scene_say", "scene_emit"} {
		e, ok := byType[et]
		require.True(t, ok, "event type %q not found in crypto.emits", et)
		assert.True(t, e.Readback, "%s MUST declare readback:true for snapshot decrypt", et)
		assert.Equal(t, "always", e.Sensitivity, "%s MUST declare sensitivity:always", et)
	}

	// scene_ooc MUST NOT declare readback (never archived in published log).
	if e, ok := byType["scene_ooc"]; ok {
		assert.False(t, e.Readback, "scene_ooc MUST NOT declare readback:true (OOC is never archived)")
	}
}

// TestPlugin_SensitivityMatrix pins INV-SCENE-3: per-type sensitivity matches
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
		"scene_pose":                           "always",
		"scene_say":                            "always",
		"scene_emit":                           "always",
		"scene_ooc":                            "always",
		"scene_join_ic":                        "never",
		"scene_leave_ic":                       "never",
		"scene_pose_order_changed_ic":          "never",
		"scene_idle_nudge":                     "never",
		"scene_publish_started":                "never",
		"scene_publish_vote_cast":              "never",
		"scene_publish_cooloff_started":        "never",
		"scene_publish_resolved":               "never",
		"scene_publish_withdrawn":              "never",
		"scene_publish_vote_attempts_extended": "never",
	}
	got := make(map[string]string)
	for _, e := range m.Crypto.Emits {
		got[e.EventType] = e.Sensitivity
	}
	assert.Equal(t, want, got,
		"INV-SCENE-3: sensitivity matrix MUST match spec §2 table")
}

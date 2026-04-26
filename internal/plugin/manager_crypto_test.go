// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
)

func parseTestManifest(t *testing.T, src string) *plugins.Manifest {
	t.Helper()
	var m plugins.Manifest
	require.NoError(t, yaml.Unmarshal([]byte(src), &m))
	require.NoError(t, plugins.ValidateCrypto(&m), "manifest fixture must pass static validation")
	return &m
}

func TestResolveCryptoRefsRejectsUnknownEventTypeInOtherPlugin(t *testing.T) {
	consumer := parseTestManifest(t, `
name: plugin-b
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
dependencies:
  plugin-a: ">= 1.0.0"
crypto:
  consumes:
    - subjects: ["events.>"]
      requests_decryption: ["plugin-a:whisper"]
`)
	registry := map[string][]plugins.CryptoEmit{
		"plugin-a": {{EventType: "actually_known", Sensitivity: plugins.SensitivityAlways}},
	}
	err := plugins.ResolveCryptoRefs(consumer, registry)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_UNKNOWN_EVENT_REF")
}

func TestResolveCryptoRefsAcceptsKnownEventTypeInOtherPlugin(t *testing.T) {
	consumer := parseTestManifest(t, `
name: plugin-b
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
dependencies:
  plugin-a: ">= 1.0.0"
crypto:
  consumes:
    - subjects: ["events.>"]
      requests_decryption: ["plugin-a:whisper"]
`)
	registry := map[string][]plugins.CryptoEmit{
		"plugin-a": {{EventType: "whisper", Sensitivity: plugins.SensitivityAlways}},
	}
	require.NoError(t, plugins.ResolveCryptoRefs(consumer, registry))
}

func TestResolveCryptoRefsRejectsRefToPluginNotInRegistry(t *testing.T) {
	consumer := parseTestManifest(t, `
name: plugin-b
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
dependencies:
  plugin-a: ">= 1.0.0"
crypto:
  consumes:
    - subjects: ["events.>"]
      requests_decryption: ["plugin-a:whisper"]
`)
	err := plugins.ResolveCryptoRefs(consumer, map[string][]plugins.CryptoEmit{})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_REF_PLUGIN_NOT_LOADED")
}

func TestResolveCryptoRefsRejectsRefToNeverSensitiveEventType(t *testing.T) {
	consumer := parseTestManifest(t, `
name: plugin-b
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
dependencies:
  plugin-a: ">= 1.0.0"
crypto:
  consumes:
    - subjects: ["events.>"]
      requests_decryption: ["plugin-a:heartbeat"]
`)
	registry := map[string][]plugins.CryptoEmit{
		"plugin-a": {{EventType: "heartbeat", Sensitivity: plugins.SensitivityNever}},
	}
	err := plugins.ResolveCryptoRefs(consumer, registry)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_CRYPTO_REF_NEVER_SENSITIVE")
}

func TestResolveCryptoRefsAcceptsSelfReferenceWithoutRegistryEntry(t *testing.T) {
	consumer := parseTestManifest(t, `
name: plugin-a
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
crypto:
  emits:
    - event_type: whisper
      sensitivity: always
  consumes:
    - subjects: ["events.>"]
      requests_decryption: ["plugin-a:whisper"]
`)
	require.NoError(t, plugins.ResolveCryptoRefs(consumer, map[string][]plugins.CryptoEmit{}))
}

func TestResolveCryptoRefsAcceptsNilCrypto(t *testing.T) {
	m := &plugins.Manifest{Name: "x"}
	assert.NoError(t, plugins.ResolveCryptoRefs(m, map[string][]plugins.CryptoEmit{}))
}

func TestDiscoverSkipsPluginWithInvalidCryptoSection(t *testing.T) {
	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "bad-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, "plugin.yaml"),
		[]byte(`
name: bad-plugin
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
crypto:
  emits:
    - event_type: foo
      sensitivity: kinda
`),
		0o600,
	))
	mgr := plugins.NewManager(tmp)
	discovered, err := mgr.Discover(t.Context())
	require.NoError(t, err)
	assert.Empty(t, discovered, "plugin with invalid crypto section MUST be filtered out")
}

func TestDiscoverSkipsPluginWithUnresolvableCryptoRefs(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "plugin-b"), 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmp, "plugin-b", "plugin.yaml"),
		[]byte(`
name: plugin-b
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
dependencies:
  plugin-a: ">= 1.0.0"
crypto:
  consumes:
    - subjects: ["events.>"]
      requests_decryption: ["plugin-a:whisper"]
`),
		0o600,
	))
	mgr := plugins.NewManager(tmp)
	discovered, err := mgr.Discover(t.Context())
	require.NoError(t, err)
	assert.Empty(t, discovered)
}

func TestDiscoverAcceptsValidCryptoSection(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "plugin-a"), 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmp, "plugin-a", "plugin.yaml"),
		[]byte(`
name: plugin-a
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
crypto:
  emits:
    - event_type: whisper
      sensitivity: always
`),
		0o600,
	))
	mgr := plugins.NewManager(tmp)
	discovered, err := mgr.Discover(t.Context())
	require.NoError(t, err)
	require.Len(t, discovered, 1)
	assert.Equal(t, "plugin-a", discovered[0].Manifest.Name)
}

func TestDiscoverAcceptsRealPluginsDirectory(t *testing.T) {
	// Load every real plugin from the repo's plugins/ directory.
	// Confirms the crypto.emits declarations all pass validation.
	pluginsDir := filepath.Join("..", "..", "plugins")
	mgr := plugins.NewManager(pluginsDir)
	discovered, err := mgr.Discover(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, discovered, "expected at least one real plugin to be discovered")

	expectedNames := []string{
		"core-communication",
		"core-objects",
	}
	gotNames := make(map[string]bool)
	for _, dp := range discovered {
		gotNames[dp.Manifest.Name] = true
	}
	for _, name := range expectedNames {
		assert.True(t, gotNames[name], "plugin %q was not discovered (validator rejected it?)", name)
	}
}

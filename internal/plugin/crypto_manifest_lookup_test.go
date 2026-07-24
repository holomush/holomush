// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestManagerPluginRequestsDecryptionMatchesQualifiedRef(t *testing.T) {
	// mod-filter declares: consumes [{ requests_decryption: ["core-comm:whisper"] }]
	mgr := newTestManagerWithManifest(t, &plugins.Manifest{
		Name:    "mod-filter",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "mod-filter",
		},
		Dependencies: map[string]string{"core-comm": "1.0.0"},
		Crypto: &plugins.CryptoSection{
			Consumes: []plugins.CryptoConsume{
				{
					Subjects:           []string{"events.>"},
					RequestsDecryption: []string{"core-comm:whisper"},
				},
			},
		},
	})

	assert.True(t, mgr.PluginRequestsDecryption("mod-filter", "core-comm:whisper"))
	assert.False(t, mgr.PluginRequestsDecryption("mod-filter", "core-comm:undeclared"))
	assert.False(t, mgr.PluginRequestsDecryption("nonexistent-plugin", "core-comm:whisper"))
}

func TestManagerPluginRequestsDecryptionFalseForNoCryptoSection(t *testing.T) {
	mgr := newTestManagerWithManifest(t, &plugins.Manifest{
		Name:    "no-crypto-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "no-crypto-plugin",
		},
	})
	assert.False(t, mgr.PluginRequestsDecryption("no-crypto-plugin", "anything:event"))
}

// newTestManagerWithManifest returns a *plugins.Manager with the given
// manifest registered in its loaded map, suitable for unit-testing
// manifest-lookup methods. Uses TestLoadPlugin, the project's standard
// helper for injecting manifests into a Manager without a real plugins dir.
func newTestManagerWithManifest(t *testing.T, m *plugins.Manifest) *plugins.Manager {
	t.Helper()
	mgr, err := plugins.NewManager("", plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, err)
	mgr.TestLoadPlugin(m.Name, m)
	return mgr
}

// TestManagerPluginRequestsDecryptionReturnsFalseOnNilReceiver pins the
// fail-closed contract formerly held by authguard's deleted manifestAdapter.
// A typed-nil *Manager stored in an authguard.ManifestLookup is NOT
// interface-nil, so authguard.New's AUTHGUARD_DEPENDENCY_NIL check cannot
// catch it; without a receiver guard this gate panics on the decrypt path.
func TestManagerPluginRequestsDecryptionReturnsFalseOnNilReceiver(t *testing.T) {
	var mgr *plugins.Manager

	require.NotPanics(t, func() {
		assert.False(t, mgr.PluginRequestsDecryption("any-plugin", "any-event"),
			"nil manager must deny (fail-closed), not panic")
	})
}

// TestManagerPluginCanReadBackReturnsFalseOnNilReceiver is the read-back
// half of the same fail-closed contract.
func TestManagerPluginCanReadBackReturnsFalseOnNilReceiver(t *testing.T) {
	var mgr *plugins.Manager

	require.NotPanics(t, func() {
		assert.False(t, mgr.PluginCanReadBack("any-plugin", "any-event"),
			"nil manager must deny (fail-closed), not panic")
	})
}

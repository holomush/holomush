// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus/authguard"
	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestPluginManifestLookupAdapterReturnsFalseAndDoesNotPanicOnNilManager(t *testing.T) {
	// NewPluginManifestLookup accepts nil for robustness; the nil-guard in
	// PluginRequestsDecryption ensures fail-closed (returns false) rather than panic.
	lookup := authguard.NewPluginManifestLookup(nil)
	require.NotPanics(t, func() {
		result := lookup.PluginRequestsDecryption("any-plugin", "any-event")
		assert.False(t, result, "nil manager must return false (fail-closed)")
	})
}

func TestPluginManifestLookupAdapterReturnsTrueForDeclaredEventType(t *testing.T) {
	mgr, err := plugins.NewManager("", plugins.WithVerbRegistry(core.NewVerbRegistry()))
	require.NoError(t, err)
	mgr.TestLoadPlugin("mod-filter", &plugins.Manifest{
		Name:    "mod-filter",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{Executable: "mod-filter"},
		Crypto: &plugins.CryptoSection{
			Consumes: []plugins.CryptoConsume{
				{Subjects: []string{"events.>"}, RequestsDecryption: []string{"core-comm:whisper"}},
			},
		},
	})

	lookup := authguard.NewPluginManifestLookup(mgr)
	assert.True(t, lookup.PluginRequestsDecryption("mod-filter", "core-comm:whisper"))
	assert.False(t, lookup.PluginRequestsDecryption("mod-filter", "core-comm:other"))
}

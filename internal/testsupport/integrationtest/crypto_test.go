// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/eventbus/codec"
)

// INV-5IA-2: WithPluginCrypto without WithInTreePlugins must panic.
func TestWithPluginCryptoWithoutPluginsPanics(t *testing.T) {
	assert.PanicsWithValue(t,
		"integrationtest: WithPluginCrypto() requires WithInTreePlugins()",
		func() { Start(t, WithPluginCrypto()) })
}

// INV-5IA-1: WithInTreePlugins ALONE must NOT wire plugin crypto. A crypto-only
// helper called without WithPluginCrypto panics via requirePluginCrypto, proving
// the substrate is unwired (the census suite already runs WithInTreePlugins-only).
func TestWithInTreePluginsAloneDoesNotWireCrypto(t *testing.T) {
	ts := Start(t, WithInTreePlugins())
	defer ts.Stop()
	assert.Panics(t, func() {
		ts.EmitPluginEvent(context.Background(), "core-scenes", "scene_pose", `{"text":"x"}`, true)
	})
}

// INV-5IA-3: the codec selector is shared (pointer-identity) across links 2-4.
// The substrate's source selector (pc.selector, also threaded into the crypto
// publisher at link 2) MUST be the SAME instance the PluginConsumerManager
// (link 3) holds — asserting source-vs-sink, not the source against itself.
func TestPluginCryptoSharesSelectorInstance(t *testing.T) {
	ts := Start(t, WithInTreePlugins(), WithPluginCrypto())
	defer ts.Stop()
	assert.Same(t, ts.cryptoSelectorForTest(), ts.pluginConsumers.KeySelectorForTest())
}

// cryptoSelectorForTest is a test-only accessor exposing the substrate's source
// codec.KeySelector (pc.selector) — the instance wired into the crypto publisher
// (WithCodecSelector, link 2). INV-5IA-3 asserts it is the same pointer the
// PluginConsumerManager (link 3) holds via KeySelectorForTest().
func (s *Server) cryptoSelectorForTest() codec.KeySelector {
	return s.pluginCrypto.selector
}

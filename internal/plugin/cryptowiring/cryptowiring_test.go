// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cryptowiring_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/plugin/cryptowiring"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestKeySelectorReturnsIdentityCodecForEncrypt(t *testing.T) {
	t.Parallel()
	sel := cryptowiring.KeySelector()
	name, label, err := sel.SelectForEncrypt(context.Background(), "events.g1.scene.x.ic")
	assert.NoError(t, err)
	assert.Equal(t, codec.NameIdentity, name)
	assert.Equal(t, codec.KeyLabel(""), label)
}

func TestKeySelectorReturnsNoKeyForDecrypt(t *testing.T) {
	t.Parallel()
	sel := cryptowiring.KeySelector()
	key, err := sel.SelectForDecrypt(context.Background(), codec.NameIdentity, codec.KeyID(0))
	assert.NoError(t, err)
	assert.Equal(t, codec.NoKey, key)
}

type fakeLoadedPlugin struct {
	name        string
	alwaysTypes []string // event types declared sensitivity:always
}

type fakeManifestSource struct{ plugins []fakeLoadedPlugin }

func (f fakeManifestSource) ListPlugins() []string {
	out := make([]string, len(f.plugins))
	for i, p := range f.plugins {
		out[i] = p.name
	}
	return out
}

func (f fakeManifestSource) AlwaysSensitiveEmitTypes(pluginName string) []string {
	for _, p := range f.plugins {
		if p.name == pluginName {
			return p.alwaysTypes
		}
	}
	return nil
}

func TestAlwaysSensitiveSetQualifiesUnqualifiedTypes(t *testing.T) {
	t.Parallel()
	src := fakeManifestSource{plugins: []fakeLoadedPlugin{
		{name: "core-scenes", alwaysTypes: []string{"scene_pose", "core-scenes:scene_say"}},
	}}
	got := cryptowiring.AlwaysSensitiveSet(src)
	assert.Equal(t, map[string]struct{}{
		"core-scenes:scene_pose": {},
		"core-scenes:scene_say":  {},
	}, got)
}

func TestAlwaysSensitiveSetEmptyForNilSource(t *testing.T) {
	t.Parallel()
	assert.Empty(t, cryptowiring.AlwaysSensitiveSet(nil))
}

func TestCryptoKeysLookupNilPoolReturnsError(t *testing.T) {
	t.Parallel()
	lookup := cryptowiring.CryptoKeysLookup(nil)
	exists, err := lookup.Exists(context.Background(), 42)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CRYPTO_KEYS_LOOKUP_POOL_NIL")
	assert.False(t, exists, "nil pool MUST NOT report existence")
}

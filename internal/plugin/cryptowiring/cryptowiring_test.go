// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cryptowiring_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/plugin/cryptowiring"
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

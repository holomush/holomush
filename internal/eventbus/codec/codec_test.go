// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package codec_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/codec"
)

func TestIdentityCodecRoundTripPreservesBytes(t *testing.T) {
	c := codec.IdentityCodec{}
	plaintext := []byte("hello, scene 01JABC")

	encoded, err := c.Encode(context.Background(), plaintext, codec.NoKey)
	require.NoError(t, err)

	decoded, err := c.Decode(context.Background(), encoded, codec.NoKey)
	require.NoError(t, err)
	require.Equal(t, plaintext, decoded)
}

func TestIdentityCodecHandlesEmptyPlaintext(t *testing.T) {
	c := codec.IdentityCodec{}
	encoded, err := c.Encode(context.Background(), nil, codec.NoKey)
	require.NoError(t, err)
	decoded, err := c.Decode(context.Background(), encoded, codec.NoKey)
	require.NoError(t, err)
	require.Empty(t, decoded)
}

func TestIdentityCodecName(t *testing.T) {
	c := codec.IdentityCodec{}
	require.Equal(t, codec.NameIdentity, c.Name())
}

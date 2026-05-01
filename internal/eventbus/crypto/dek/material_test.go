// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func TestNewMaterial_ConstructsOpaqueWrapper(t *testing.T) {
	bytes := make([]byte, 32)
	for i := range bytes {
		bytes[i] = byte(i)
	}
	m := dek.NewMaterial(bytes)
	assert.NotNil(t, m)
}

func TestMaterial_AsCodecKey_ReturnsCodecKeyWithSameBytes(t *testing.T) {
	bytes := []byte("0123456789abcdef0123456789abcdef") // 32 bytes
	m := dek.NewMaterial(bytes)

	key := m.AsCodecKey(codec.KeyID(42))
	assert.Equal(t, codec.KeyID(42), key.ID)
	require.Len(t, key.Bytes, 32)
	assert.Equal(t, bytes, key.Bytes)
}

func TestMaterial_NewMaterial_CopiesInputBytes(t *testing.T) {
	// Defensive copy — caller must not be able to mutate Material's
	// internal bytes by retaining a reference to the input slice.
	src := []byte("0123456789abcdef0123456789abcdef")
	m := dek.NewMaterial(src)
	src[0] = 0xFF // mutate caller's slice

	key := m.AsCodecKey(codec.KeyID(1))
	assert.Equal(t, byte('0'), key.Bytes[0],
		"Material must defensively copy input; caller's mutation leaked")
}

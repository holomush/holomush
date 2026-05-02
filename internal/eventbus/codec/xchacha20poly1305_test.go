// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package codec_test

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/chacha20poly1305"

	"github.com/holomush/holomush/internal/eventbus/codec"
)

func newXChachaKey(t *testing.T) codec.Key {
	t.Helper()
	km := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(km)
	require.NoError(t, err)
	return codec.Key{ID: 1, Version: 1, Bytes: km}
}

func TestXChaCha20Poly1305RoundTripsPlaintext(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	key := newXChachaKey(t)
	plaintext := []byte("hello, secret world")
	aad := []byte("test-aad")

	ct, err := c.Encode(context.Background(), plaintext, key, aad)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ct, "ciphertext must differ from plaintext")

	got, err := c.Decode(context.Background(), ct, key, aad)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)
}

func TestXChaCha20Poly1305DetectsCiphertextTamper(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	key := newXChachaKey(t)
	aad := []byte("test-aad")

	ct, err := c.Encode(context.Background(), []byte("hello"), key, aad)
	require.NoError(t, err)
	ct[len(ct)-1] ^= 0x01

	_, err = c.Decode(context.Background(), ct, key, aad)
	require.Error(t, err, "tampered ciphertext must not decrypt")
}

func TestXChaCha20Poly1305DetectsAADTamper(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	key := newXChachaKey(t)

	ct, err := c.Encode(context.Background(), []byte("hello"), key, []byte("aad-A"))
	require.NoError(t, err)

	_, err = c.Decode(context.Background(), ct, key, []byte("aad-B"))
	require.Error(t, err, "AAD mismatch must fail decryption")
}

func TestXChaCha20Poly1305AcceptsNilAAD(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	key := newXChachaKey(t)

	ct, err := c.Encode(context.Background(), []byte("hello"), key, nil)
	require.NoError(t, err)

	got, err := c.Decode(context.Background(), ct, key, nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), got)
}

func TestXChaCha20Poly1305NameIsXChaCha20Poly1305v1(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	assert.Equal(t, codec.NameXChaCha20v1, c.Name())
}

func TestXChaCha20Poly1305RejectsWrongLengthKey(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	badKey := codec.Key{ID: 1, Version: 1, Bytes: []byte("too-short")}
	_, err := c.Encode(context.Background(), []byte("hello"), badKey, nil)
	require.Error(t, err)
}

func TestXChaCha20Poly1305DecodeRejectsShortCiphertext(t *testing.T) {
	c := codec.NewXChaCha20Poly1305v1()
	key := newXChachaKey(t)
	_, err := c.Decode(context.Background(), []byte("short"), key, nil)
	require.Error(t, err, "ciphertext shorter than nonce+tag must error")
}

func TestXChaCha20Poly1305RegisteredInRegistry(t *testing.T) {
	c, err := codec.Resolve(codec.NameXChaCha20v1)
	require.NoError(t, err)
	assert.Equal(t, codec.NameXChaCha20v1, c.Name())
}

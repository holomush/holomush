// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package codec — xchacha20poly1305-v1 implementation.
//
// Wire layout: nonce || ciphertext || tag
//   nonce      : 24 bytes (XChaCha20-Poly1305 NonceSizeX)
//   ciphertext : len(plaintext) bytes
//   tag        : 16 bytes (Poly1305 tag, appended by Seal)
//
// AAD is supplied via the codec interface's `aad []byte` parameter
// (Phase 3a substrate edit). Phase 3a's emit path calls
// internal/eventbus/crypto/aad.Build to construct AAD.
package codec

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// XChaCha20Poly1305v1 implements Codec for sensitive payloads.
type XChaCha20Poly1305v1 struct{}

// NewXChaCha20Poly1305v1 returns a stateless codec instance.
func NewXChaCha20Poly1305v1() *XChaCha20Poly1305v1 { return &XChaCha20Poly1305v1{} }

// Name returns NameXChaCha20v1.
func (*XChaCha20Poly1305v1) Name() Name { return NameXChaCha20v1 }

// Encode produces nonce || ciphertext || tag using key.Bytes as the
// AEAD key and aad as additional authenticated data. Errors on
// wrong-length keys or RNG failure.
func (*XChaCha20Poly1305v1) Encode(_ context.Context, plaintext []byte, key Key, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key.Bytes)
	if err != nil {
		return nil, fmt.Errorf("xchacha20poly1305-v1: new aead: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("xchacha20poly1305-v1: rng: %w", err)
	}
	out := make([]byte, 0, len(nonce)+len(plaintext)+aead.Overhead())
	out = append(out, nonce...)
	out = aead.Seal(out, nonce, plaintext, aad)
	return out, nil
}

// Decode validates and decrypts. AAD MUST equal the value supplied at
// Encode; any mismatch surfaces as a generic error (no oracle).
func (*XChaCha20Poly1305v1) Decode(_ context.Context, ciphertext []byte, key Key, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key.Bytes)
	if err != nil {
		return nil, fmt.Errorf("xchacha20poly1305-v1: new aead: %w", err)
	}
	if len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("xchacha20poly1305-v1: ciphertext too short")
	}
	nonce, sealed := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	pt, err := aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, fmt.Errorf("xchacha20poly1305-v1: open: %w", err)
	}
	return pt, nil
}

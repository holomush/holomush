// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package kek defines the Key Encryption Key provider stack: an
// abstract Provider interface and concrete implementations for local
// AEAD (with pluggable KEK source) and a no-op None provider.
//
// Per master spec §5.1 "Layer 1": providers see only opaque secret
// bytes — Data Encryption Keys (DEKs, Phase 2), TOTP secrets (Phase 5),
// and any future secret tier added in subsequent phases. They never see
// event payloads or secret semantic context (which scene, which player,
// which version). Tier-specific routing (DEK→event, secret→player)
// lives in the caller (e.g. dek.Manager).
package kek

import "context"

// Provider wraps and unwraps opaque secret bytes (DEKs in Phase 2;
// TOTP secrets in Phase 5; future tiers in subsequent phases) using a
// master Key Encryption Key (KEK) it manages internally.
// Implementations MUST keep KEK material out of process memory
// whenever possible; LocalAEADProvider necessarily holds it in process
// for the life of the server, while VaultTransitProvider (Phase 6)
// keeps it remote.
type Provider interface {
	// Name returns the provider identifier persisted in
	// crypto_keys.wrap_provider. Examples: "local-aead/file",
	// "local-aead/env", "vault-transit", "none".
	Name() string

	// Wrap encrypts plaintext (an opaque secret — DEK, TOTP secret,
	// or a future tier) under the current KEK version. Returns the
	// wrapped bytes and a provider-specific kekKeyID identifying which
	// KEK version was used.
	Wrap(ctx context.Context, plaintext []byte) (wrapped []byte, kekKeyID string, err error)

	// Unwrap decrypts wrapped using the KEK identified by kekKeyID.
	Unwrap(ctx context.Context, wrapped []byte, kekKeyID string) (plaintext []byte, err error)

	// RotateKEK creates a new KEK version. Phase 4+ uses this; Phase 2
	// ships the method but production callers are out of scope.
	RotateKEK(ctx context.Context) (newKEKKeyID string, err error)

	// HealthCheck verifies the provider is reachable and the KEK is
	// available. Used by the readiness probe.
	HealthCheck(ctx context.Context) error
}

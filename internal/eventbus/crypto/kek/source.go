// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek

import "context"

// KEKSource fetches and refreshes master KEK material from a backing
// store. The KEK never leaves the LocalAEADProvider's process memory
// once Load returns. Implementations are tagged by Name; the tag is
// persisted in crypto_keys.wrap_provider as "local-aead/<source-name>".
//
// The "KEK" prefix is intentional and load-bearing — master spec §5.1
// uses the term "KEKSource" as the canonical name for this abstraction
// (distinct from a generic "Source"), so the stutter warning is
// suppressed.
//
//nolint:revive // KEK prefix matches master spec §5.1 terminology.
type KEKSource interface {
	Name() string
	Load(ctx context.Context) ([]byte, error)
	// Persist stores new KEK material after rotation. Some sources
	// (env, systemd-credential) are read-only and return a typed
	// CRYPTO_*_READ_ONLY error; rotation requires a different path
	// for those.
	Persist(ctx context.Context, kek []byte) error
}

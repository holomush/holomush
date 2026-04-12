// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package settings provides typed namespaced configuration reads with
// scope chaining. The same key may be set at game (server-wide), player,
// or character scope; resolution order is most-specific to least-specific,
// with first-match-wins and a substrate fallback.
//
// Scope storage backing:
//   - Game:      holomush_system_info table (key/value)
//   - Player:    players.preferences JSONB column
//   - Character: characters.preferences JSONB column (future; null impl in Phase 4)
package settings

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
)

// Settings is the read-only typed namespaced accessor shared by every
// scope. Each scope-specific store produces a Settings instance for a
// specific principal (a player ID, a character ID, or the singleton game
// instance) and that instance is used identically by callers.
//
// All read methods return (zero, false) for unset keys and never return
// errors. Infrastructure failures are logged and surfaced as "unset."
// Callers using the resolution Chain rely on this contract to avoid
// double error-handling at every scope level.
//
// Keys MUST be dot-namespaced and MUST begin with a registered top-level
// namespace (see RegisteredNamespaces). Unknown namespaces on reads return
// (zero, false) with a debug-level warning; writes return an error.
type Settings interface {
	StringN(ctx context.Context, key string) (string, bool)
	IntN(ctx context.Context, key string) (int, bool)
	BoolN(ctx context.Context, key string) (bool, bool)
	DurationN(ctx context.Context, key string) (time.Duration, bool)
}

// GameSettings is the server-wide scope backed by holomush_system_info.
// Exactly one GameSettings instance per server. Writes are supported for
// operator tooling.
type GameSettings interface {
	Settings
	SetString(ctx context.Context, key, value string) error
}

// PlayerSettingsStore is the factory for per-player Settings instances.
// Call For(playerID) to obtain the read view for a specific player,
// backed by their row in players.preferences.
type PlayerSettingsStore interface {
	For(ctx context.Context, playerID ulid.ULID) Settings
	SetString(ctx context.Context, playerID ulid.ULID, key, value string) error
}

// CharacterSettingsStore is the factory for per-character Settings. Phase 4
// ships this interface with a null implementation (always-unset) because
// no character.preferences schema exists yet.
type CharacterSettingsStore interface {
	For(ctx context.Context, characterID ulid.ULID) Settings
	SetString(ctx context.Context, characterID ulid.ULID, key, value string) error
}

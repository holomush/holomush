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
	// StringSliceN returns the string-slice value for key and whether it
	// was present. Player/character scopes decode a native JSON array from
	// the preferences JSONB; the game scope decodes a JSON-array-encoded
	// string. A scalar or non-array value reports (nil, false).
	StringSliceN(ctx context.Context, key string) ([]string, bool)
}

// GameSettings is the server-wide scope backed by holomush_system_info.
// Exactly one GameSettings instance per server. It is owner-partitioned
// (embeds Scoped): bare Settings reads target the namespace-validated host
// keyspace, Owner(name) narrows to a plugin's isolated "plugin/<name>/"-
// prefixed partition, and Host() returns the namespace-validated host
// Writable. SetString persists a single host key directly (legacy operator-
// tooling surface, equivalent to Host().SetString).
type GameSettings interface {
	Scoped
	SetString(ctx context.Context, key, value string) error
}

// Scoped is an owner-partitioned settings handle. Bare Settings reads target
// the HOST partition (namespace-validated, identical to the legacy behavior).
// Owner(name) narrows to a plugin's isolated partition (NOT namespace-
// validated); Host() returns the host partition for writes.
type Scoped interface {
	Settings                    // bare reads -> host partition (namespace-validated)
	Owner(name string) Writable // plugin partition: isolated, NOT namespace-validated
	Host() Writable             // host partition (namespace-validated)
}

// Writable is a read+write settings view. Reads follow the same typed,
// namespaced contract as Settings; writes persist a single key.
type Writable interface {
	Settings
	SetString(ctx context.Context, key, value string) error
	SetStringSlice(ctx context.Context, key string, values []string) error
}

// PlayerSettingsStore is the factory for per-player Scoped settings handles.
// Call For(playerID) to obtain the owner-partitioned view for a specific
// player, backed by their row in players.preferences. Bare reads on the
// returned Scoped target the host partition.
type PlayerSettingsStore interface {
	For(ctx context.Context, playerID ulid.ULID) Scoped
	SetString(ctx context.Context, playerID ulid.ULID, key, value string) error
}

// CharacterSettingsStore is the factory for per-character Scoped handles.
// Phase 4 ships this interface with a null implementation (always-unset
// host partition) because no character.preferences schema exists yet.
type CharacterSettingsStore interface {
	For(ctx context.Context, characterID ulid.ULID) Scoped
	SetString(ctx context.Context, characterID ulid.ULID, key, value string) error
}

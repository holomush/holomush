// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
)

// MovementHook is invoked by Service.MoveCharacter AFTER the character move and
// its move envelope have committed together in ONE transaction (05-06). It
// propagates the new location to a dependent store (session.Store) — a SEPARATE
// connection pool that cannot enroll in the world transaction — so it necessarily
// runs POST-commit.
//
// Per holomush-iwzt §5.1 / ADR holomush-kmac.
//
// A hook error is OPERATIONAL DEGRADATION, NOT a command failure (05-06 round-5
// finding 3): the move already happened and the envelope is durable, so
// MoveCharacter logs the failure, increments a metric, and RETURNS SUCCESS — it
// does NOT surface a command error after the state+envelope commit. The documented
// consequence is that the session's DERIVED location MAY LAG (stay stale) until the
// next authoritative event re-syncs it — a reconnect, or another explicit
// session-location write. Phase 5 ships zero product projections, so there is NO
// automatic re-derivation of the stale session row (round-6 Codex MEDIUM); a
// bounded durable retry / read-path repair is the follow-up if the lag proves
// material.
type MovementHook interface {
	OnCharacterMoved(ctx context.Context, characterID ulid.ULID, newLocationID ulid.ULID, arrivedAt time.Time) error
}

// NoopMovementHook is the default when no hook is wired (e.g., test contexts).
type NoopMovementHook struct{}

// OnCharacterMoved is a no-op implementation of MovementHook.OnCharacterMoved.
func (NoopMovementHook) OnCharacterMoved(_ context.Context, _, _ ulid.ULID, _ time.Time) error {
	return nil
}

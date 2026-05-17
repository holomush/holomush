// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
)

// MovementHook is invoked by Service.MoveCharacter after the character row
// is updated but before the move event is emitted. Implementations propagate
// the new location to dependent stores (e.g., session.Store.UpdateLocationOnMove)
// so consumers reading those stores observe the new location atomically with
// the move event.
//
// Per holomush-iwzt §5.1 / ADR holomush-kmac.
//
// Returning an error from the hook fails the move — caller MUST handle.
type MovementHook interface {
	OnCharacterMoved(ctx context.Context, characterID ulid.ULID, newLocationID ulid.ULID, arrivedAt time.Time) error
}

// NoopMovementHook is the default when no hook is wired (e.g., test contexts).
type NoopMovementHook struct{}

// OnCharacterMoved is a no-op implementation of MovementHook.OnCharacterMoved.
func (NoopMovementHook) OnCharacterMoved(_ context.Context, _, _ ulid.ULID, _ time.Time) error {
	return nil
}

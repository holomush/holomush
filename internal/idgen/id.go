// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package idgen provides crypto/rand-backed ULID generation.
//
// All production code MUST use idgen.New() instead of ulid.Make(), which uses
// math/rand internally. Test code may continue using ulid.Make() since
// cryptographic strength is not required for test identifiers.
package idgen

import (
	cryptorand "crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// New generates a ULID with fresh crypto/rand entropy on every call.
//
// Use this for: entity primary keys (players, sessions, locations,
// characters, exits, objects, policies, audit rows) where the ID is pure
// identity and there is no requirement for IDs minted in temporal order to
// also sort in temporal order.
//
// Do NOT use this for event IDs (eventbus.Event.ID). Two calls in the same
// millisecond produce IDs in random lexicographic order, which silently
// breaks PostgresEventStore.Replay (ORDER BY id, WHERE id > afterID) and
// PostgresSessionStore.UpdateCursors monotonicity. Use core.NewULID()
// instead. eventbus.Event{} struct literals must use eventbus.NewEvent()
// (which stamps a monotonic ULID via core.NewULID()) — never construct an
// Event literal with a manually-supplied ID.
//
// Panics if the system's cryptographic random source is unavailable,
// which indicates an unrecoverable OS-level failure.
func New() ulid.ULID {
	id, err := ulid.New(ulid.Timestamp(time.Now()), cryptorand.Reader)
	if err != nil {
		panic("id: crypto/rand unavailable: " + err.Error())
	}
	return id
}

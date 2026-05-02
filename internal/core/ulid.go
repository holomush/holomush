// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

var (
	entropy     = ulid.Monotonic(rand.Reader, 0)
	entropyLock sync.Mutex
)

// NewULID generates a monotonic-within-millisecond ULID using crypto/rand.
//
// Use this for: event IDs (core.Event.ID), session IDs, and any identifier
// whose lexicographic order MUST match arrival order. The PostgresEventStore
// relies on this property — Replay uses `WHERE id > afterID ORDER BY id` and
// PostgresSessionStore.UpdateCursors uses a per-key monotonicity CAS on the
// cursor JSONB value. A non-monotonic event ID can produce a lex-inverted
// pair within the same millisecond, which silently breaks both replay
// (the second event is skipped) and cursor advances (the second cursor is
// rejected by the CAS).
//
// Do NOT use idgen.New() for these. core.Event{} struct literals must use
// core.NewEvent() (which stamps a monotonic ULID via core.NewULID()) —
// never construct an Event literal with a manually-supplied ID.
func NewULID() ulid.ULID {
	entropyLock.Lock()
	defer entropyLock.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy)
}

// ParseULID parses a ULID string.
func ParseULID(s string) (ulid.ULID, error) {
	id, err := ulid.Parse(s)
	if err != nil {
		return ulid.ULID{}, oops.With("ulid", s).Wrap(err)
	}
	return id, nil
}

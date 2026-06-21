// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"crypto/rand"
	"io"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

var (
	// entropy, entropyLock, and lastMs are one coupled unit: generateULID keeps
	// lastMs in lockstep with entropy's internal ms, and the clamp's monotonicity
	// guarantee depends on that coupling. Mutate them only under entropyLock, and
	// if one is ever reset, reset all three together.
	entropy     = ulid.Monotonic(rand.Reader, 0)
	entropyLock sync.Mutex
	lastMs      uint64
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
	return generateULID(entropy, &lastMs, ulid.Timestamp(time.Now()))
}

// generateULID is the testable core of NewULID. It clamps ms to be
// non-decreasing (never below *last) before stamping it into a ULID with the
// shared monotonic reader r, then records it via last.
//
// The clamp is the correctness fix for holomush-nri6e: ulid.Monotonic only
// increments its 80-bit random component when fed the SAME ms it saw last
// (MonotonicRead's `m.ms == ms` branch); a smaller ms makes it reseed a fresh,
// unrelated random and adopt the lower ms, which can produce a lex-inverted
// (non-monotonic) pair. Under concurrency the entropyLock serialises callers
// but the wall clock it reads is not monotonic across them — one goroutine can
// advance the entropy to ms M+1 between another's two ms-M reads. Clamping to
// *last keeps every call on the increment path (or, when the clock genuinely
// advances, on a strictly greater timestamp), so successive ULIDs are strictly
// increasing. We deliberately do NOT spin-wait for the wall clock to advance:
// clamping keeps callers on the increment path, whereas waiting would block
// them — needlessly, since ulid.Monotonic's per-ms random increment is the very
// mechanism that makes clock-waiting unnecessary.
func generateULID(r io.Reader, last *uint64, ms uint64) ulid.ULID {
	ms = max(ms, *last)
	*last = ms
	return ulid.MustNew(ms, r)
}

// ParseULID parses a ULID string.
func ParseULID(s string) (ulid.ULID, error) {
	id, err := ulid.Parse(s)
	if err != nil {
		return ulid.ULID{}, oops.With("ulid", s).Wrap(err)
	}
	return id, nil
}

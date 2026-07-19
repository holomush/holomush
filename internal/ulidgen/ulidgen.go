// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package ulidgen is the single home for HoloMUSH's monotonic ULID generator.
// It is a dependency-free leaf (its only imports are stdlib + oklog/ulid +
// samber/oops) so gateway packages (internal/telnet, internal/web) can
// generate IDs without importing internal/core (INV-EVENTBUS-1).
package ulidgen

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

// New generates a monotonic-within-millisecond ULID using crypto/rand.
//
// Use this for: event IDs, session IDs, and other identifiers needing a
// stable, nonzero, unique dedup key. Two properties, kept separate:
//
//  1. Event IDs need a stable, nonzero, unique Nats-Msg-Id dedup identity —
//     the publisher rejects only the zero ULID
//     (internal/eventbus/publisher.go:165-170). Dedup does not require lex
//     order; EventBus ordering is exclusively JetStream's per-stream sequence,
//     never ULID lexicographic order.
//  2. Monotonic-within-millisecond generation is retained as a generator
//     property for session/cursor compatibility with any downstream consumer
//     that relies on it — verify such a consumer still exists before treating
//     this as load-bearing for a new use.
func New() ulid.ULID {
	entropyLock.Lock()
	defer entropyLock.Unlock()
	return generateULID(entropy, &lastMs, ulid.Timestamp(time.Now()))
}

// generateULID is the testable core of New. It clamps ms to be
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

// Parse parses a ULID string.
func Parse(s string) (ulid.ULID, error) {
	id, err := ulid.Parse(s)
	if err != nil {
		return ulid.ULID{}, oops.With("ulid", s).Wrap(err)
	}
	return id, nil
}

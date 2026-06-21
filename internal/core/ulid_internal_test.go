// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"crypto/rand"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

// generateULID MUST emit strictly increasing ULIDs even when the wall-clock ms
// it is fed repeats or regresses (the holomush-nri6e clamp; the increment-vs-
// reseed mechanism is documented on generateULID). This pins it deterministically
// — the concurrency stress test TestEventIDMonotonicityUnderLoad only hits the
// regression probabilistically.
func TestGenerateULIDClampsRegressingClock(t *testing.T) {
	r := ulid.Monotonic(rand.Reader, 0)
	var last uint64

	// A sequence that repeats and regresses — exactly what concurrent callers
	// produce when one goroutine advances the clock between another's calls.
	seq := []uint64{1000, 1000, 999, 1000, 998, 1001, 1001, 1000, 1002}

	prev := generateULID(r, &last, seq[0])
	for _, ms := range seq[1:] {
		prevLast := last
		cur := generateULID(r, &last, ms)
		require.Truef(t, cur.Compare(prev) > 0,
			"non-monotonic: fed ms=%d (prev last=%d); prev=%s cur=%s", ms, prevLast, prev, cur)
		prev = cur
	}
}

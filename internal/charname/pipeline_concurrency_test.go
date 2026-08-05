// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname"
)

// Normalize is called concurrently in production and MUST be safe there.
//
// Every character create runs it (auth.CharacterService.Create -> Gate.Admit ->
// Gate.Check -> Normalize), and two players creating characters at the same
// moment is the ordinary case, not an exotic one. The guest path runs it in a
// retry loop, and migration 000055's backfill runs it over the whole corpus.
//
// The hazard is specific and it is NOT visible by reading Normalize's body:
// golang.org/x/text's transformers are STATEFUL. cases.Caser's own doc says "A
// Caser may be stateful and should therefore not be shared between goroutines",
// and transform.Chain returns a *chain carrying mutable `link`, `err` and
// `errStart` fields plus per-link buffers. Holding either in a package-level var
// and calling String() from two goroutines interleaves one call's Reset with
// another's Transform: the observable results are a truncated or garbage
// display form (which the syntactic rule then rejects, on a name that is
// unambiguously letters-only) and, when the buffer indices cross, a panic.

// concurrencyFixtures deliberately mixes inputs whose pipeline work differs, so
// the transformers do unequal amounts of buffering and the interleaving window
// is wide. The ß case matters twice over: it is the one that proves full case
// folding, and it is the one whose fold EXPANDS the string, which is what moves
// a shared buffer's write index.
var concurrencyFixtures = []string{
	"Brenna",
	"Cocoa",
	"Straße",
	"Ariel Windmender",
	"Ｂｒｅｎｎａ",
}

// TestNormalizeIsSafeForConcurrentUse is the regression for the defect that
// made the 02-12 concurrent-claim spec intermittent.
//
// It asserts EQUALITY against the sequential result rather than merely that
// nothing panicked: a corrupted transformer usually returns a wrong string
// rather than crashing, and "did not crash" would pass against the bug.
func TestNormalizeIsSafeForConcurrentUse(t *testing.T) {
	// The sequential truth, computed before any concurrency.
	want := make([]charname.Normalized, len(concurrencyFixtures))
	for i, in := range concurrencyFixtures {
		got, err := charname.Normalize(in)
		require.NoError(t, err)
		want[i] = got
	}

	const rounds = 200
	var wg sync.WaitGroup
	results := make([][]charname.Normalized, len(concurrencyFixtures))
	errs := make([][]error, len(concurrencyFixtures))
	for i := range concurrencyFixtures {
		results[i] = make([]charname.Normalized, rounds)
		errs[i] = make([]error, rounds)
	}

	start := make(chan struct{})
	for i, in := range concurrencyFixtures {
		for round := range rounds {
			wg.Add(1)
			go func(i, round int, in string) {
				defer wg.Done()
				<-start
				results[i][round], errs[i][round] = charname.Normalize(in)
			}(i, round, in)
		}
	}
	close(start)
	wg.Wait()

	for i, in := range concurrencyFixtures {
		for round := range rounds {
			require.NoErrorf(t, errs[i][round],
				"Normalize(%q) failed under concurrency at round %d", in, round)
			assert.Equalf(t, want[i], results[i][round],
				"Normalize(%q) returned a DIFFERENT result under concurrency at round %d — "+
					"the pipeline is sharing stateful x/text transformers", in, round)
		}
	}
}

// TestSkeletonIsSafeForConcurrentUse covers the other half of the admission
// pipeline on the same terms.
//
// norm.Form is a value type whose String() allocates its own buffer, so this is
// expected to hold — which is exactly why it is worth pinning: it is the control
// that says the Normalize failure above is about SHARED STATE specifically, not
// about x/text being unusable concurrently.
func TestSkeletonIsSafeForConcurrentUse(t *testing.T) {
	want := make([]string, len(concurrencyFixtures))
	for i, in := range concurrencyFixtures {
		want[i] = charname.Skeleton(in)
	}

	const rounds = 200
	var wg sync.WaitGroup
	results := make([][]string, len(concurrencyFixtures))
	for i := range concurrencyFixtures {
		results[i] = make([]string, rounds)
	}

	start := make(chan struct{})
	for i, in := range concurrencyFixtures {
		for round := range rounds {
			wg.Add(1)
			go func(i, round int, in string) {
				defer wg.Done()
				<-start
				results[i][round] = charname.Skeleton(in)
			}(i, round, in)
		}
	}
	close(start)
	wg.Wait()

	for i, in := range concurrencyFixtures {
		for round := range rounds {
			assert.Equalf(t, want[i], results[i][round],
				"Skeleton(%q) returned a DIFFERENT result under concurrency at round %d", in, round)
		}
	}
}

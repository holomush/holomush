// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package blocklist_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/charname/blocklist"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
)

const testKey = "core.character.name.blocklist"

// fakeRaw is a blocklist.RawGetter double over a single settings row. It
// models the three states the real store can be in — absent, present, and
// erroring — as three DISTINGUISHABLE outcomes, which is precisely what
// settings.StringSliceN collapses and what this package exists to keep apart.
type fakeRaw struct {
	mu      sync.Mutex
	value   string
	present bool
	err     error
	calls   int
}

func (f *fakeRaw) GetSystemInfo(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	if !f.present {
		return "", oops.With("key", key).Wrap(store.ErrSystemInfoNotFound)
	}
	return f.value, nil
}

func (f *fakeRaw) set(value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.value = value
	f.present = true
}

func present(value string) *fakeRaw { return &fakeRaw{value: value, present: true} }

func absent() *fakeRaw { return &fakeRaw{} }

func newCache(raw blocklist.RawGetter) *blocklist.Cache {
	return blocklist.NewCache(func() blocklist.RawGetter { return raw }, testKey)
}

// ---------------------------------------------------------------------------
// Load — the strict decode that replaces settings.StringSliceN
// ---------------------------------------------------------------------------

func TestLoadTreatsAnAbsentRowAsAValidEmptyListRatherThanAnError(t *testing.T) {
	got, err := blocklist.Load(t.Context(), absent(), testKey)

	require.NoError(t, err, "an unconfigured block list is a valid empty list")
	assert.Empty(t, got)
}

func TestLoadReadsAJSONArrayOfStrings(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
		want  []string
	}{
		{"the seeded empty array", `[]`, nil},
		{"a one-element list", `["^ok$"]`, []string{"^ok$"}},
		{"a multi-element list", `["^ok$","admin"]`, []string{"^ok$", "admin"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := blocklist.Load(t.Context(), present(tt.value), testKey)

			require.NoError(t, err)
			if len(tt.want) == 0 {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadRejectsAMalformedValueRatherThanReadingItAsAnEmptyList(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
	}{
		{"invalid JSON", `{`},
		{"a JSON string scalar", `"hello"`},
		{"a JSON number scalar", `7`},
		{"an array with a non-string element", `["ok", 7]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Paired on the same fixture with a value that SUCCEEDS, so
			// "errors" cannot pass because Load errors on everything.
			ok, okErr := blocklist.Load(t.Context(), present(`["^ok$"]`), testKey)
			require.NoError(t, okErr, "the paired valid case must succeed")
			require.Equal(t, []string{"^ok$"}, ok)

			_, err := blocklist.Load(t.Context(), present(tt.value), testKey)

			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "BLOCKLIST_VALUE_MALFORMED")
			errutil.AssertErrorContext(t, err, "key", testKey)
		})
	}
}

func TestLoadVerdictForAMalformedValueDiffersInKindFromItsVerdictForAnAbsentRow(t *testing.T) {
	// THE assertion the whole fail-open finding turns on. settings.StringSliceN
	// returns (nil, false) for BOTH cases, so a loader built on it cannot tell
	// "the operator configured nothing" from "the operator's direct-SQL edit
	// produced garbage" — and reads the second as "block nothing".
	absentList, absentErr := blocklist.Load(t.Context(), absent(), testKey)
	malformedList, malformedErr := blocklist.Load(t.Context(), present(`{`), testKey)

	require.NoError(t, absentErr)
	assert.Empty(t, absentList)

	require.Error(t, malformedErr)
	errutil.AssertErrorCode(t, malformedErr, "BLOCKLIST_VALUE_MALFORMED")
	assert.Nil(t, malformedList)

	assert.NotEqual(t, absentErr == nil, malformedErr == nil,
		"an absent row and an unparseable row MUST NOT be the same verdict")
}

func TestLoadSurfacesADatabaseFailureRatherThanFlatteningItIntoAbsent(t *testing.T) {
	boom := errors.New("connection refused")

	_, err := blocklist.Load(t.Context(), &fakeRaw{err: boom}, testKey)

	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	// Paired control on the same shape: a readable row succeeds.
	_, okErr := blocklist.Load(t.Context(), present(`[]`), testKey)
	require.NoError(t, okErr)
}

// ---------------------------------------------------------------------------
// Cache — compile-then-swap, and a live matcher
// ---------------------------------------------------------------------------

func TestCacheReloadInstallsANewSnapshot(t *testing.T) {
	raw := present(`["^admin$"]`)
	c := newCache(raw)

	require.NoError(t, c.Reload(t.Context()))

	blocked, idx := c.Snapshot().Match("admin")
	assert.True(t, blocked)
	assert.Equal(t, 0, idx)
}

func TestCacheStartsEmptySoAnUnreloadedCacheRejectsNothing(t *testing.T) {
	c := newCache(present(`["^admin$"]`))

	blocked, _ := c.Match("admin")

	assert.False(t, blocked, "an un-Prepared cache matches nothing; it never blocks everything")
}

func TestCacheReloadLeavesThePriorNonEmptyListInForceWhenAPatternDoesNotCompile(t *testing.T) {
	raw := present(`["^admin$"]`)
	c := newCache(raw)
	require.NoError(t, c.Reload(t.Context()))
	requireBlocks(t, c)

	raw.set(`["^admin$", "(unclosed"]`)
	err := c.Reload(t.Context())

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "BLOCKLIST_PATTERN_INVALID")
	// The distinguishing assertion: the prior list is still ENFORCING. A cache
	// that swapped before compiling would have reset to empty, which reads as
	// "no list configured" and silently admits every name.
	requireBlocks(t, c)
}

func TestCacheReloadLeavesThePriorNonEmptyListInForceWhenTheValueIsMalformed(t *testing.T) {
	raw := present(`["^admin$"]`)
	c := newCache(raw)
	require.NoError(t, c.Reload(t.Context()))
	requireBlocks(t, c)

	raw.set(`{`)
	err := c.Reload(t.Context())

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "BLOCKLIST_VALUE_MALFORMED")
	requireBlocks(t, c)
}

func TestCacheReloadInstallsAnEmptyListWhenTheOperatorGenuinelyEmptiesIt(t *testing.T) {
	// Paired control for the two tests above: "prior list retained" must not
	// pass because the cache never swaps at all.
	raw := present(`["^admin$"]`)
	c := newCache(raw)
	require.NoError(t, c.Reload(t.Context()))
	requireBlocks(t, c)

	raw.set(`[]`)
	require.NoError(t, c.Reload(t.Context()))

	blocked, _ := c.Match("admin")
	assert.False(t, blocked, "an explicit empty list is a real edit and does take effect")
}

func TestCacheSnapshotConcurrentWithReloadNeverReturnsAPartiallyPopulatedSnapshot(t *testing.T) {
	raw := present(`["^a$","^b$","^c$"]`)
	c := newCache(raw)
	require.NoError(t, c.Reload(t.Context()))

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Alternate between a 3-pattern and a 1-pattern list. A reader must
			// see one of those two shapes and never a torn intermediate.
			raw.set(`["^a$","^b$","^c$"]`)
			_ = c.Reload(t.Context())
			raw.set(`["^a$"]`)
			_ = c.Reload(t.Context())
		}
	}()

	for range 500 {
		snap := c.Snapshot()
		n := snap.Len()
		assert.Contains(t, []int{1, 3}, n, "observed a torn snapshot of %d patterns", n)
		// A whole evaluation reads ONE immutable snapshot: "a" is blocked by
		// both shapes, so the verdict cannot flip mid-evaluation.
		blocked, _ := snap.Match("a")
		assert.True(t, blocked)
	}

	close(stop)
	wg.Wait()
}

func TestCacheMatchDelegatesToTheCurrentSnapshotOnEveryCall(t *testing.T) {
	raw := present(`[]`)
	c := newCache(raw)
	require.NoError(t, c.Reload(t.Context()))

	blocked, _ := c.Match("admin")
	require.False(t, blocked)

	raw.set(`["^admin$"]`)
	require.NoError(t, c.Reload(t.Context()))

	blocked, idx := c.Match("admin")
	assert.True(t, blocked, "Match reads the CURRENT snapshot, not one captured at construction")
	assert.Equal(t, 0, idx)
}

func TestAGateBuiltOverACacheSeesAReloadThatHappensAfterItsConstruction(t *testing.T) {
	// The single assertion that distinguishes handing the gate a *Cache from
	// handing it a *Snapshot. A gate holding a snapshot captured at
	// construction would keep admitting "Admin" forever while the poller
	// cheerfully refreshed a cache nothing reads — silent, with no failing
	// test and no log line.
	raw := present(`[]`)
	c := newCache(raw)
	require.NoError(t, c.Reload(t.Context()))

	gate := &charname.Gate{Skeletons: emptySkeletons{}, BlockList: c}

	_, _, err := gate.Check(t.Context(), "Admin")
	require.NoError(t, err, "accepted under the empty list")

	raw.set(`["^admin$"]`)
	require.NoError(t, c.Reload(t.Context()))

	_, _, err = gate.Check(t.Context(), "Admin")
	require.Error(t, err, "the SAME gate instance must now reject it")
	errutil.AssertErrorCode(t, err, "NAME_BLOCKED")

	// Paired positive control on the same gate and the same post-reload list.
	_, _, err = gate.Check(t.Context(), "Alaric")
	require.NoError(t, err)
}

// emptySkeletons is a charname.SkeletonLookup over an empty, fully-backfilled
// corpus: nothing collides and nothing is unverifiable.
type emptySkeletons struct{}

func (emptySkeletons) SkeletonExists(_ context.Context, _ string, _ *ulid.ULID) (bool, bool, error) {
	return false, false, nil
}

func requireBlocks(t *testing.T, c *blocklist.Cache) {
	t.Helper()
	blocked, _ := c.Match("admin")
	require.True(t, blocked, "%q must still be blocked by the last valid list", "admin")
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/pkg/errutil"
)

// newSnapshotForTest builds a pre-populated snapshot without needing a JS
// client. Test-only; must live in a _test.go file.
func newSnapshotForTest(firstSeq, lastSeq uint64) *StreamStateSnapshot {
	s := &StreamStateSnapshot{firstSeq: firstSeq, lastSeq: lastSeq}
	s.once.Do(func() {}) // mark as populated
	return s
}

// TestStreamStateSnapshotGetOnNilSnapshotReturnsZeros verifies the nil
// receiver guard — callers that have no snapshot get (0, 0, nil).
func TestStreamStateSnapshotGetOnNilSnapshotReturnsZeros(t *testing.T) {
	t.Parallel()
	var s *StreamStateSnapshot
	first, last, err := s.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(0), first)
	assert.Equal(t, uint64(0), last)
}

// TestStreamStateSnapshotGetOnNilJSReturnsZeros verifies that when the
// snapshot has no JS client (once.Do body skips) Get returns (0, 0, nil).
// This is the path taken by unit tests that pre-populate via newSnapshotForTest.
func TestStreamStateSnapshotGetOnNilJSReturnsZeros(t *testing.T) {
	t.Parallel()
	// js is nil — the once.Do body short-circuits via the `if s.js == nil` guard.
	s := &StreamStateSnapshot{}
	first, last, err := s.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(0), first)
	assert.Equal(t, uint64(0), last)
}

// TestStreamStateSnapshotGetReturnsStreamInfoState verifies that when a real
// JetStream client is provided, Get returns the correct FirstSeq and LastSeq.
func TestStreamStateSnapshotGetReturnsStreamInfoState(t *testing.T) {
	embedded := eventbustest.New(t)

	s := newStreamStateSnapshot(embedded.JS)
	first, last, err := s.Get(context.Background())
	require.NoError(t, err)
	// A freshly created stream with no messages has FirstSeq == 0 or 1.
	// Both are valid; we just assert no error and that last >= first.
	assert.GreaterOrEqual(t, last, first)
}

// TestStreamStateSnapshotGetPropagatesStreamLookupFailure verifies that a
// failed JS Stream() call produces EVENTBUS_HISTORY_STREAM_LOOKUP_FAILED.
func TestStreamStateSnapshotGetPropagatesStreamLookupFailure(t *testing.T) {
	embedded := eventbustest.New(t)
	js := embedded.JS

	// Stop the server before calling Get so the Stream lookup fails.
	require.NoError(t, embedded.Bus.Stop(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	s := newStreamStateSnapshot(js)
	_, _, err := s.Get(ctx)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_STREAM_LOOKUP_FAILED")
}

// TestStreamStateSnapshotGetIsMemoized verifies that subsequent calls to Get
// return the cached values without re-querying the JS server.
func TestStreamStateSnapshotGetIsMemoized(t *testing.T) {
	embedded := eventbustest.New(t)

	s := newStreamStateSnapshot(embedded.JS)

	// First call — fetches from the server.
	first1, last1, err1 := s.Get(context.Background())
	require.NoError(t, err1)

	// Stop the server before the second call. If Get were not memoized,
	// the second call would fail. If it is memoized, it returns the cached
	// result without touching JS.
	require.NoError(t, embedded.Bus.Stop(context.Background()))

	first2, last2, err2 := s.Get(context.Background())
	require.NoError(t, err2, "second Get must use cached value even after server stops")
	assert.Equal(t, first1, first2)
	assert.Equal(t, last1, last2)
}

// TestNewSnapshotForTestReturnsPopulatedSnapshot verifies that newSnapshotForTest
// produces a snapshot whose Get returns the seeded values without a JS call.
func TestNewSnapshotForTestReturnsPopulatedSnapshot(t *testing.T) {
	t.Parallel()
	s := newSnapshotForTest(10, 99)
	first, last, err := s.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(10), first)
	assert.Equal(t, uint64(99), last)
}

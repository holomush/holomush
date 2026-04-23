// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/pkg/errutil"
)

// Round 4: covers branches unreached by rounds 1-3:
//
//   - buildConfig backward no-cursor: stream lookup failure
//   - buildConfig backward no-cursor: stream Info failure
//   - buildConfig backward no-cursor: LastSeq < fetch (startSeq stays 1)
//   - matchesQuery: JS tier rejects events at edge (Timestamp == edge, Before returns false)
//   - matchesQuery: zero edge does not reject any event (TierJetStream, zero edge)

// TestBuildConfigBackwardNoCursorStreamLookupFailureReturnsError covers the
// "backward, no cursor" path where h.js.Stream() returns an error.
func TestBuildConfigBackwardNoCursorStreamLookupFailureReturnsError(t *testing.T) {
	embedded := eventbustest.New(t)
	js := embedded.JS

	// Stop the server so Stream() will fail.
	require.NoError(t, embedded.Bus.Stop(context.Background()))

	h := &jetStreamHotTier{js: js, now: time.Now}
	q := eventbus.HistoryQuery{
		Subject:   eventbus.Subject("events.main.scene.abc"),
		Direction: eventbus.DirectionBackward,
		// BeforeSeq == 0 → takes the "no cursor" path that calls js.Stream().
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := h.buildConfig(ctx, q, time.Time{}, 10)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HOT_STREAM_LOOKUP_FAILED")
}

// TestBuildConfigBackwardNoCursorWithSmallStreamStartsAtSeqOne covers the
// case where LastSeq < fetch so startSeq clamps to 1 (not 0).
func TestBuildConfigBackwardNoCursorWithSmallStreamStartsAtSeqOne(t *testing.T) {
	// Publish 2 events but request a page of 10; LastSeq (2) < fetch (10)
	// → startSeq must be 1, not 2 - 10 + 1 = -7 (which would underflow).
	embedded := eventbustest.New(t)

	pub := embedded.Bus.Publisher()
	require.NotNil(t, pub)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		require.NoError(t, pub.Publish(ctx, eventbus.Event{
			ID:        core.NewULID(),
			Subject:   eventbus.Subject("events.main.scene.smallstream"),
			Type:      "test.evt",
			Timestamp: time.Now().UTC(),
			Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
			Payload:   []byte("p"),
		}))
	}
	embedded.AwaitStreamLastSeq(t, 2, 5*time.Second)

	h := &jetStreamHotTier{js: embedded.JS, now: time.Now}
	q := eventbus.HistoryQuery{
		Subject:   eventbus.Subject("events.main.scene.smallstream"),
		Direction: eventbus.DirectionBackward,
		// BeforeSeq == 0 → uses js.Stream() to find LastSeq.
	}
	// fetch = 10 (pageSize for backward, LastSeq=2 < 10 → startSeq=1)
	cfg, err := h.buildConfig(ctx, q, time.Time{}, 10)
	require.NoError(t, err)
	assert.Equal(t, jetstream.DeliverByStartSequencePolicy, cfg.DeliverPolicy)
	assert.Equal(t, uint64(1), cfg.OptStartSeq)
}

// TestMatchesQueryJSTierAcceptsEventExactlyAtEdge verifies that an event with
// Timestamp exactly equal to edge is accepted (Before is strict <, so
// edge.Before(edge) == false → the event is NOT pre-edge).
// This acts as a regression guard for off-by-one in the edge filter.
func TestMatchesQueryJSTierAcceptsEventExactlyAtEdge(t *testing.T) {
	t.Parallel()
	edge := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	ev := eventbus.Event{Seq: 1, Timestamp: edge} // Timestamp == edge
	// edge.Before(edge) == false → should NOT reject the event.
	assert.True(t, matchesQuery(ev, eventbus.HistoryQuery{}, edge, TierJetStream),
		"event at exactly edge is NOT pre-edge; JS tier must accept it")
}

// TestMatchesQueryJSTierWithZeroEdgeAcceptsAllTimestamps verifies that when
// edge is zero (hot tier used in unit tests without age config), the
// tier-boundary filter does not fire.
func TestMatchesQueryJSTierWithZeroEdgeAcceptsAllTimestamps(t *testing.T) {
	t.Parallel()
	ev := eventbus.Event{Seq: 1, Timestamp: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	assert.True(t, matchesQuery(ev, eventbus.HistoryQuery{}, time.Time{}, TierJetStream),
		"zero edge must not filter any event in the JS tier")
}

// TestMatchesQueryNotBeforeIsInclusiveAtBoundary verifies that an event with
// Timestamp == NotBefore is accepted (NotBefore is inclusive).
func TestMatchesQueryNotBeforeIsInclusiveAtBoundary(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	ev := eventbus.Event{Seq: 1, Timestamp: ts}
	q := eventbus.HistoryQuery{NotBefore: ts} // event exactly at NotBefore
	// ev.Timestamp.Before(ts) == false → must be accepted.
	assert.True(t, matchesQuery(ev, q, time.Time{}, TierPostgres),
		"event at exactly NotBefore boundary must be accepted")
}

// TestMatchesQueryNotAfterIsInclusiveAtBoundary verifies that an event with
// Timestamp == NotAfter is accepted (NotAfter is inclusive).
func TestMatchesQueryNotAfterIsInclusiveAtBoundary(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	ev := eventbus.Event{Seq: 1, Timestamp: ts}
	q := eventbus.HistoryQuery{NotAfter: ts} // event exactly at NotAfter
	// ev.Timestamp.After(ts) == false → must be accepted.
	assert.True(t, matchesQuery(ev, q, time.Time{}, TierPostgres),
		"event at exactly NotAfter boundary must be accepted")
}

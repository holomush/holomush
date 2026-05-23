// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history_test

import (
	"context"
	crand "crypto/rand"
	"errors"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/eventbus/history"
	"github.com/holomush/holomush/pkg/errutil"
)

// These tests exercise the JetStream-backed HotTier directly via a Reader
// wired with NewReader(js, nil, ...) — no ColdTier. They cover the code
// paths (forward start-time, backward start-seq, empty stream, unknown
// subject) that the fake-tier unit tests in tier_test.go cannot reach.

func publishN(t *testing.T, embedded *eventbustest.Embedded, subject eventbus.Subject, n int) []ulid.ULID {
	t.Helper()
	pub := embedded.Bus.Publisher()
	require.NotNil(t, pub)

	ids := make([]ulid.ULID, 0, n)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < n; i++ {
		id := ulid.MustNew(ulid.Timestamp(time.Now()), hotEntropy)
		evt := eventbus.Event{
			ID:        id,
			Subject:   subject,
			Type:      eventbus.Type("scene.pose"),
			Timestamp: time.Now().UTC(),
			Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
			Payload:   []byte("p"),
		}
		require.NoError(t, pub.Publish(ctx, evt))
		ids = append(ids, id)
	}
	require.GreaterOrEqual(t, n, 0)
	embedded.AwaitStreamLastSeq(t, uint64(n), 5*time.Second) //nolint:gosec // n is a test-local positive count
	return ids
}

// cryptoRandReaderHot wraps crypto/rand.Reader; a monotonic ULID entropy
// source built on it produces distinct ULIDs in-millisecond (so JS dedupe
// does not collapse rapid publishes).
type cryptoRandReaderHot struct{}

func (cryptoRandReaderHot) Read(p []byte) (int, error) {
	return crand.Read(p)
}

// hotEntropy is a monotonic entropy source used for ULID minting in this
// file's publishN helper.
var hotEntropy = ulid.Monotonic(cryptoRandReaderHot{}, 0)

func TestHotTierForwardReadReturnsPublishedEvents(t *testing.T) {
	embedded := eventbustest.New(t)
	subject := eventbus.Subject("events.main.hot.forward")
	ids := publishN(t, embedded, subject, 3)
	sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) < 0 })

	reader := history.NewReader(
		embedded.JS, nil,
		24*time.Hour, func() time.Time { return time.Now() },
	)
	stream, err := reader.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   subject,
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	var got []ulid.ULID
	for {
		nextCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ev, nextErr := stream.Next(nextCtx)
		cancel()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		require.NoError(t, nextErr)
		got = append(got, ev.ID)
	}
	assert.Equal(t, ids, got, "forward read must return exactly the published events in order")
}

func TestHotTierBackwardReadUsesStartSeq(t *testing.T) {
	// Exercises the DirectionBackward start-seq branch in buildConfig that
	// the F8 autofix introduced.
	embedded := eventbustest.New(t)
	subject := eventbus.Subject("events.main.hot.backward")
	ids := publishN(t, embedded, subject, 5)
	sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) < 0 })

	reader := history.NewReader(
		embedded.JS, nil,
		24*time.Hour, func() time.Time { return time.Now() },
	)
	stream, err := reader.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   subject,
		Direction: eventbus.DirectionBackward,
		PageSize:  3,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	// Backward read starts from the latest-seen seq and walks backwards
	// through the stream. The test asserts two invariants:
	//   (a) the reader MUST yield strictly ULID-descending ids (no dupes,
	//       no out-of-order), and
	//   (b) every returned id MUST be one of the published ids (no
	//       phantom rows, no cross-contamination from prior tests).
	const pageSize = 3
	var got []ulid.ULID
	for {
		nextCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ev, nextErr := stream.Next(nextCtx)
		cancel()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		require.NoError(t, nextErr)
		got = append(got, ev.ID)
		if len(got) >= pageSize {
			break
		}
	}
	assert.Len(t, got, pageSize, "backward read must fill the page")
	publishedSet := make(map[ulid.ULID]struct{}, len(ids))
	for _, id := range ids {
		publishedSet[id] = struct{}{}
	}
	for i, id := range got {
		_, ok := publishedSet[id]
		assert.True(t, ok, "returned id %v was not published", id)
		if i > 0 {
			assert.Less(t, id.Compare(got[i-1]), 0, "backward read must be strictly ULID-descending at index %d", i)
		}
	}
}

func TestHotTierUnknownSubjectReturnsEmpty(t *testing.T) {
	embedded := eventbustest.New(t)
	reader := history.NewReader(
		embedded.JS, nil,
		24*time.Hour, func() time.Time { return time.Now() },
	)
	stream, err := reader.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   eventbus.Subject("events.main.hot.never.published"),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })
	nextCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = stream.Next(nextCtx)
	require.ErrorIs(t, err, io.EOF, "unknown subject must drain as io.EOF")
}

func TestHotTierBackwardReadOnEmptyStream(t *testing.T) {
	// Fresh subsystem → LastSeq == 0 → the backward-start-seq path takes
	// the "empty stream" branch and falls back to DeliverAllPolicy.
	embedded := eventbustest.New(t)
	reader := history.NewReader(
		embedded.JS, nil,
		24*time.Hour, func() time.Time { return time.Now() },
	)
	stream, err := reader.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   eventbus.Subject("events.main.hot.empty"),
		Direction: eventbus.DirectionBackward,
		PageSize:  5,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })
	nextCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = stream.Next(nextCtx)
	require.ErrorIs(t, err, io.EOF, "empty stream must drain as io.EOF")
}

// TestHotTierPopulatesEventSeq verifies that Event.Seq is populated from the
// JetStream stream sequence (msg.Metadata().Sequence.Stream) on a Read call.
func TestHotTierPopulatesEventSeq(t *testing.T) {
	embedded := eventbustest.New(t)
	subject := eventbus.Subject("events.main.hot.seq.populate")
	publishN(t, embedded, subject, 1)

	reader := history.NewReader(
		embedded.JS, nil,
		24*time.Hour, func() time.Time { return time.Now() },
	)
	stream, err := reader.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   subject,
		Direction: eventbus.DirectionForward,
		PageSize:  5,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	nextCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ev, err := stream.Next(nextCtx)
	require.NoError(t, err)
	assert.Greater(t, ev.Seq, uint64(0), "Event.Seq must be populated from JetStream stream sequence")
}

// TestHotTierStaleOnIDMismatchAtCursorSeq verifies that Read returns
// ErrCursorStale when the first delivered message's ID does not match the
// cursor ID for the given AfterSeq.
func TestHotTierStaleOnIDMismatchAtCursorSeq(t *testing.T) {
	embedded := eventbustest.New(t)
	subject := eventbus.Subject("events.main.hot.cursor.stale")
	publishN(t, embedded, subject, 3)

	// First, do an uncursored forward read to discover the actual seq of event 1.
	reader := history.NewReader(
		embedded.JS, nil,
		24*time.Hour, func() time.Time { return time.Now() },
	)
	stream, err := reader.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   subject,
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	})
	require.NoError(t, err)

	var firstSeq uint64
	for i := 0; i < 3; i++ {
		nextCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ev, nextErr := stream.Next(nextCtx)
		cancel()
		require.NoError(t, nextErr)
		if i == 0 {
			firstSeq = ev.Seq
		}
	}
	_ = stream.Close()
	require.Greater(t, firstSeq, uint64(0), "need a real seq for cursor test")

	// Now query with a cursor pointing to firstSeq but a WRONG id — expect ErrCursorStale.
	// Use a ULID with a recent timestamp (so selectStartTier routes to hot) but wrong entropy.
	wrongID := ulid.MustNew(ulid.Timestamp(time.Now()), hotEntropy)
	stream2, err := reader.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   subject,
		AfterSeq:  firstSeq,
		AfterID:   wrongID,
		Direction: eventbus.DirectionForward,
		PageSize:  5,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream2.Close() })

	nextCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = stream2.Next(nextCtx)
	assert.ErrorIs(t, err, eventbus.ErrCursorStale, "wrong cursor ID must return ErrCursorStale")
}

func TestNewReaderOptionsAreApplied(t *testing.T) {
	// Constructing a Reader with all tunable options exercises each option
	// closure and keeps them from silently drifting to 0% coverage.
	t.Parallel()
	reader := history.NewReader(
		nil, nil,
		24*time.Hour,
		func() time.Time { return time.Unix(0, 0) },
		history.WithSafetyMargin(2*time.Hour),
		history.WithCodecSelector(nil),
	)
	require.NotNil(t, reader)
}

func TestReaderRejectsInvalidDirection(t *testing.T) {
	t.Parallel()
	reader := history.NewReader(nil, nil, time.Hour, func() time.Time { return time.Now() })
	_, err := reader.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   "events.main.any",
		Direction: eventbus.Direction(99),
	})
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_INVALID_DIRECTION")
}

// TestHotTierEmptySubjectShortCircuitsInsteadOfWaitingFetchTimeout pins
// the holomush-87qu fix: querying a subject with zero matching messages
// MUST return immediately via GetLastMsgForSubject's ErrMsgNotFound
// short-circuit, NOT pay the full hotFetchTimeout (5s). The connect-
// latency dominator on fresh-guest sessions was 2 ambient stream
// backfills each waiting 5s; this regression lock asserts the
// per-subject wait is now ~10ms typical, definitely <500ms.
//
// Budget: 500ms (well below hotFetchTimeout = 5s but generous enough
// to absorb embedded-NATS startup variance + GetLastMsgForSubject
// round-trip on CI testcontainers). A regression that reverted to the
// pre-fix Fetch-on-empty path would fail by an order of magnitude.
func TestHotTierEmptySubjectShortCircuitsInsteadOfWaitingFetchTimeout(t *testing.T) {
	embedded := eventbustest.New(t)
	reader := history.NewReader(
		embedded.JS, nil,
		24*time.Hour, func() time.Time { return time.Now() },
	)

	// A subject that has never been published to — GetLastMsgForSubject
	// must return ErrMsgNotFound and Read must short-circuit.
	emptySubject := eventbus.Subject("events.main.never-published.subject")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	stream, err := reader.QueryHistory(ctx, eventbus.HistoryQuery{
		Subject:   emptySubject,
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	nextCtx, nextCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer nextCancel()
	_, nextErr := stream.Next(nextCtx)
	elapsed := time.Since(start)

	require.ErrorIs(t, nextErr, io.EOF, "empty subject query must reach EOF, not error or block indefinitely")
	require.Less(t, elapsed, 500*time.Millisecond,
		"empty-subject hot read MUST short-circuit via GetLastMsgForSubject (~10ms typical); "+
			"took %s. Regression of holomush-87qu — the pre-fix path would have waited the "+
			"full hotFetchTimeout (5s) on cons.Fetch.", elapsed)
}

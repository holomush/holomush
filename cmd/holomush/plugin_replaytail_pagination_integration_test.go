// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// D-07 regression: plugin history pagination via busHistoryReaderAdapter.
//
// The bug, in the codebase's own words (internal/eventbus/bus.go:98-104):
// cursors are (seq, id) pairs, where BeforeID is a "tripwire for BeforeSeq;
// zero = skip validation" — it validates a nonzero seq, it is NOT itself a
// filter. busHistoryReaderAdapter.ReplayTail (sub_grpc.go) sets only
// q.BeforeID and never q.BeforeSeq, so every call's BeforeSeq is zero.
//
// Both tiers treat BeforeSeq==0 as "no cursor: read the tail":
//   - hot tier: matchesQuery (internal/eventbus/history/hot_jetstream.go:392-397)
//     filters on AfterSeq/BeforeSeq/NotBefore/NotAfter and has NO BeforeID
//     branch at all — a lone BeforeID excludes nothing.
//   - hot tier: buildConfig (hot_jetstream.go:338) only takes the cursor path
//     "if q.BeforeSeq > 0"; otherwise it falls through to the tail-oriented
//     read and returns the newest page every time.
//   - cold tier: cold_postgres.go computes hasCursor := cursorSeq > 0, so a
//     lone BeforeID produces no SQL cursor bound either.
//
// Consequence: on a QUIET stream, with NO concurrency, plugin multipage
// pagination via ReplayTail repeats the newest page forever — page 2 is
// page 1 verbatim, and the walk never advances.
//
// Post-fix (this plan's Task 2): the cursor is seq-keyed. encodeHostEventCursor
// carries the real eventbus.Event.Seq into cursor.HostCursor.Seq, and
// busHistoryReaderAdapter.ReplayTail threads a new beforeSeq parameter into
// eventbus.HistoryQuery.BeforeSeq, alongside the existing beforeID tripwire.
//
// This file drives busHistoryReaderAdapter directly (package main — the type
// is unexported there) against a REAL embedded-JetStream bus (eventbustest),
// following the packaging precedent already in this package:
// sub_grpc_adapters_test.go (package main, constructs the adapter directly)
// and cmd_audit_dlq_replay_integration_test.go (an integration-tagged
// cmd/holomush test standing up a real bus).
package main

import (
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/eventbus/history"
)

const (
	replayTailPaginationGameID    = "main"
	replayTailPaginationMaxWalk   = 10
	replayTailPaginationStreamMax = 30 * 24 * time.Hour
)

// newReplayTailPaginationReader builds a hot-tier-only history.Reader (no
// Postgres pool — this plan's specs stay entirely within JetStream retention)
// against the embedded bus, with a fixed clock so retention-edge computation
// is deterministic.
func newReplayTailPaginationReader(bus *eventbustest.Embedded, now time.Time) *history.Reader {
	return history.NewReader(bus.JS, nil, replayTailPaginationStreamMax, func() time.Time { return now })
}

// drainReplayTailPaginationStream pulls every event from stream until io.EOF,
// failing the test on any other error. Mirrors
// test/integration/eventbus_e2e/cross_tier_query_test.go's drainStream.
func drainReplayTailPaginationStream(t *testing.T, ctx context.Context, stream eventbus.HistoryStream) []eventbus.Event { //nolint:revive // ctx-after-t mirrors this package's existing helper style
	t.Helper()
	var out []eventbus.Event
	for {
		e, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		out = append(out, e)
	}
}

// TestReplayTailPaginationAdvancesAcrossPagesOnQuietStream is Spec A — THE RED
// GATE. It publishes 100 events sequentially on one subject (a quiet stream:
// no concurrency, no ULID drift needed) and then pages backward through
// busHistoryReaderAdapter.ReplayTail with pageSize=20, round-tripping the
// cursor exactly as internal/plugin/hostcap/servers.go does: the page-advance
// anchor is the OLDEST (index 0) event of each ascending page — never the
// last.
//
// Before Task 2's fix: page 2 repeats page 1 verbatim (BeforeSeq is never
// set, so every call reads the tail) and the walk never advances — this is
// asserted as a "no repeat" failure, with the walk bounded at
// replayTailPaginationMaxWalk iterations so a non-advancing cursor fails the
// test rather than hanging the suite.
func TestReplayTailPaginationAdvancesAcrossPagesOnQuietStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	bus := eventbustest.New(t)
	now := time.Now().UTC()
	reader := newReplayTailPaginationReader(bus, now)
	pub := bus.Bus.Publisher()

	const streamRef = "plugin.replaytail.quiet"
	subject := eventbus.Subject("events." + replayTailPaginationGameID + "." + streamRef)

	const total = 100
	published := make(map[ulid.ULID]bool, total)
	for i := 0; i < total; i++ {
		ev := eventbus.NewEvent(subject, eventbus.Type("test.replaytail.quiet"),
			eventbus.Actor{Kind: eventbus.ActorKindSystem}, []byte(fmt.Sprintf(`{"n":%d}`, i)))
		require.NoError(t, pub.Publish(ctx, ev))
		published[ev.ID] = true
	}
	require.Len(t, published, total, "sequential ULIDs must be unique")

	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return replayTailPaginationGameID }}

	const pageSize = 20
	seen := make(map[ulid.ULID]bool, total)
	var beforeSeq uint64
	var beforeID ulid.ULID
	for iteration := 1; ; iteration++ {
		require.LessOrEqualf(t, iteration, replayTailPaginationMaxWalk,
			"walk did not terminate within %d iterations — cursor is not advancing (page repeat)", replayTailPaginationMaxWalk)

		events, err := adapter.ReplayTail(ctx, streamRef, pageSize, time.Time{}, beforeSeq, beforeID)
		require.NoError(t, err)
		if len(events) == 0 {
			break
		}
		for _, e := range events {
			require.Falsef(t, seen[e.ID], "event %s repeated across pages — cursor is not advancing (D-07)", e.ID)
			seen[e.ID] = true
		}

		// Page-advance anchor: the OLDEST event of the ascending page,
		// index 0 (hostcap/servers.go:904 precedent). BeforeSeq is an
		// exclusive upper bound; anchoring on the newest event would
		// re-request the page just returned.
		anchor := events[0]
		beforeSeq = anchor.Seq
		beforeID = anchor.ID

		if len(events) < pageSize {
			break
		}
	}

	require.Lenf(t, seen, total, "union of all pages must equal the %d published IDs (no skips)", total)
	for id := range published {
		require.Truef(t, seen[id], "published event %s missing from every page (skip)", id)
	}
}

// TestReplayTailPaginationAdvancesWhenULIDOrderDisagreesWithSeqOrder is Spec
// B — post-green defence-in-depth (NOT the RED gate; it is expected to fail
// at RED too). It proves the cursor advances by JetStream stream sequence,
// not by ULID lex order.
//
// Built deterministically (not via concurrent publishers, which would only
// probabilistically disagree and could silently false-green): >=40 ULIDs are
// precomputed with ulid.New(ulid.Timestamp(time.Now()), crand.Reader) — NOT
// core.NewULID(), whose monotonic clamp would fight the reordering — sorted,
// then published sequentially in DESCENDING lex order, so stream sequence
// 1..N provably inverts ULID lex order at every adjacent pair. Each
// precomputed ID is applied by overriding ev.ID after eventbus.NewEvent,
// exactly as NewEvent's own doc grants (internal/eventbus/types.go:212-214)
// and as precedented in internal/eventbus/publisher_test.go:129 and
// test/integration/eventbus_e2e/suite_test.go's mintEvent.
func TestReplayTailPaginationAdvancesWhenULIDOrderDisagreesWithSeqOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	bus := eventbustest.New(t)
	now := time.Now().UTC()
	reader := newReplayTailPaginationReader(bus, now)
	pub := bus.Bus.Publisher()

	const streamRef = "plugin.replaytail.drift"
	subject := eventbus.Subject("events." + replayTailPaginationGameID + "." + streamRef)

	const total = 40
	ids := make([]ulid.ULID, total)
	for i := range ids {
		id, err := ulid.New(ulid.Timestamp(time.Now()), crand.Reader)
		require.NoError(t, err)
		ids[i] = id
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) < 0 })

	// Publish in DESCENDING lex order: highest ULID first, lowest last.
	// Stream sequence is assigned in publish order, so seq order is the
	// exact reverse of ULID lex order.
	published := make(map[ulid.ULID]bool, total)
	for i := total - 1; i >= 0; i-- {
		ev := eventbus.NewEvent(subject, eventbus.Type("test.replaytail.drift"),
			eventbus.Actor{Kind: eventbus.ActorKindSystem}, []byte("d"))
		ev.ID = ids[i]
		require.NoError(t, pub.Publish(ctx, ev))
		published[ev.ID] = true
	}
	require.Len(t, published, total)

	// Premise self-check: scan the whole stream forward (ascending seq),
	// independent of the adapter/cursor path, and assert the resulting ID
	// order is strictly DEscending — i.e. seq order and ULID lex order
	// genuinely disagree at every adjacent pair. This is what prevents the
	// spec from vacuously passing if the construction is ever "simplified".
	scanStream, err := reader.QueryHistory(ctx, eventbus.HistoryQuery{
		Subject:   subject,
		Direction: eventbus.DirectionForward,
		PageSize:  total,
	})
	require.NoError(t, err)
	scanned := drainReplayTailPaginationStream(t, ctx, scanStream)
	require.NoError(t, scanStream.Close())
	require.Len(t, scanned, total)
	for i := 1; i < len(scanned); i++ {
		require.Truef(t, scanned[i-1].Seq < scanned[i].Seq,
			"forward scan must yield strictly increasing seq at position %d", i)
		require.Truef(t, scanned[i-1].ID.Compare(scanned[i].ID) > 0,
			"premise self-check FAILED: ULID lex order must strictly disagree with seq order at position %d — construction is not actually inverted", i)
	}

	adapter := &busHistoryReaderAdapter{reader: reader, gameID: func() string { return replayTailPaginationGameID }}

	const pageSize = 10
	seen := make(map[ulid.ULID]bool, total)
	var beforeSeq uint64
	var beforeID ulid.ULID
	for iteration := 1; ; iteration++ {
		require.LessOrEqualf(t, iteration, replayTailPaginationMaxWalk,
			"walk did not terminate within %d iterations — cursor is not advancing (page repeat)", replayTailPaginationMaxWalk)

		events, err := adapter.ReplayTail(ctx, streamRef, pageSize, time.Time{}, beforeSeq, beforeID)
		require.NoError(t, err)
		if len(events) == 0 {
			break
		}
		for _, e := range events {
			require.Falsef(t, seen[e.ID], "event %s repeated across pages — cursor is not advancing by seq (D-07)", e.ID)
			seen[e.ID] = true
		}

		anchor := events[0]
		beforeSeq = anchor.Seq
		beforeID = anchor.ID

		if len(events) < pageSize {
			break
		}
	}

	require.Lenf(t, seen, total, "union of all pages must equal the %d published IDs (no skips)", total)
	for id := range published {
		require.Truef(t, seen[id], "published event %s missing from every page (skip)", id)
	}
}

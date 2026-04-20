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
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/history"
)

// ---------------------------------------------------------------------------
// Test harness: an in-memory fake that splits a known event list across
// hot (recent) and cold (archived) tiers based on an injected retention
// edge. Each tier counts reads so tests can assert "this tier served X
// pages / wasn't touched at all".
// ---------------------------------------------------------------------------

// fakeTier serves events matching a query from an in-memory slice. Tests
// seed one fakeTier with "hot" events and another with "cold" events, then
// wire them into the Reader via WithHotTier / WithColdTier.
type fakeTier struct {
	events []eventbus.Event
	tier   history.Tier
	// err, when non-nil, is returned by Read — used to prove a tier was
	// not consulted (by observing the absence of the forced error).
	err   error
	calls int
}

func (f *fakeTier) Read(_ context.Context, q eventbus.HistoryQuery, edge time.Time, pageSize int) ([]eventbus.Event, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	var matches []eventbus.Event
	for _, e := range f.events {
		if e.Subject != q.Subject {
			continue
		}
		if !q.After.IsZero() && e.ID.Compare(q.After) <= 0 {
			continue
		}
		if !q.Before.IsZero() && e.ID.Compare(q.Before) >= 0 {
			continue
		}
		if !q.NotBefore.IsZero() && e.Timestamp.Before(q.NotBefore) {
			continue
		}
		if !q.NotAfter.IsZero() && e.Timestamp.After(q.NotAfter) {
			continue
		}
		// Simulate tier-boundary enforcement:
		//   hot tier serves Timestamp >= edge;
		//   cold tier, when edge is non-zero, serves Timestamp < edge;
		//   when edge is zero (whole-archive), cold serves all.
		switch f.tier {
		case history.TierJetStream:
			if !edge.IsZero() && e.Timestamp.Before(edge) {
				continue
			}
		case history.TierPostgres:
			if !edge.IsZero() && !e.Timestamp.Before(edge) {
				continue
			}
		}
		matches = append(matches, e)
	}
	dir := q.Direction
	if dir == 0 {
		dir = eventbus.DirectionForward
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if dir == eventbus.DirectionBackward {
			return matches[i].ID.Compare(matches[j].ID) > 0
		}
		return matches[i].ID.Compare(matches[j].ID) < 0
	})
	if len(matches) > pageSize {
		matches = matches[:pageSize]
	}
	return matches, nil
}

// mintEvent builds a test event whose ULID encodes the given timestamp.
func mintEvent(t *testing.T, ts time.Time, subject eventbus.Subject) eventbus.Event {
	t.Helper()
	id, err := ulid.New(ulid.Timestamp(ts), crand.Reader)
	require.NoError(t, err)
	return eventbus.Event{
		ID:        id,
		Subject:   subject,
		Type:      eventbus.Type("scene.pose"),
		Timestamp: ts.UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte("{}"),
	}
}

func drain(t *testing.T, stream eventbus.HistoryStream) []eventbus.Event {
	t.Helper()
	var out []eventbus.Event
	for {
		ev, err := stream.Next(context.Background())
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		out = append(out, ev)
	}
}

// fixedClock returns a clock function that always returns the given time.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// commonSubject is reused across tests.
const commonSubject = eventbus.Subject("events.main.location.01ABCDEFGHJKMNPQRSTVWXYZ00")

// testBuild assembles a Reader with the standard 30d maxAge + 1h safety.
func testBuild(hot, cold *fakeTier, now time.Time, opts ...history.Option) *history.Reader {
	streamMaxAge := 30 * 24 * time.Hour
	all := []history.Option{
		history.WithClock(fixedClock(now)),
	}
	if hot != nil {
		all = append(all, history.WithHotTier(hot))
	}
	if cold != nil {
		all = append(all, history.WithColdTier(cold))
	}
	all = append(all, opts...)
	return history.NewReader(nil, nil, streamMaxAge, fixedClock(now), all...)
}

// edgeAt computes the edge given now, maxAge, safetyMargin (defaults to
// history.DefaultSafetyMargin).
func edgeAt(now time.Time) time.Time {
	return now.Add(-30 * 24 * time.Hour).Add(history.DefaultSafetyMargin)
}

// ---------------------------------------------------------------------------
// Scenario 1: Cursor strictly within JS retention (Forward)
//
// All events recent; Reader starts in JS, returns expected events, zero PG
// queries.
// ---------------------------------------------------------------------------

func TestTierCrossoverCursorWithinJSRetention(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	hotEvents := []eventbus.Event{
		mintEvent(t, now.Add(-2*time.Hour), commonSubject),
		mintEvent(t, now.Add(-1*time.Hour), commonSubject),
		mintEvent(t, now.Add(-30*time.Minute), commonSubject),
	}
	hot := &fakeTier{events: hotEvents, tier: history.TierJetStream}
	cold := &fakeTier{events: nil, tier: history.TierPostgres}

	r := testBuild(hot, cold, now)
	stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		NotBefore: now.Add(-3 * time.Hour),
		Direction: eventbus.DirectionForward,
		PageSize:  50,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	got := drain(t, stream)
	require.Len(t, got, 3)
	assert.Equal(t, hotEvents[0].ID, got[0].ID)
	assert.Equal(t, hotEvents[2].ID, got[2].ID)
	// Crossover may still probe cold once to confirm no overflow — but
	// with a full page returned from hot, cold is not touched.
	assert.GreaterOrEqual(t, hot.calls, 1)
	assert.Equal(t, 0, cold.calls, "cold tier must NOT be queried when JS fills the page")
}

// ---------------------------------------------------------------------------
// Scenario 2: Cursor strictly older than retention (Forward)
//
// All events aged; Reader starts in PG, crosses into JS to pick up any tail.
// ---------------------------------------------------------------------------

func TestTierCrossoverCursorOlderThanRetention(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	edge := edgeAt(now)
	coldEvents := []eventbus.Event{
		mintEvent(t, edge.Add(-10*24*time.Hour), commonSubject),
		mintEvent(t, edge.Add(-5*24*time.Hour), commonSubject),
		mintEvent(t, edge.Add(-time.Hour), commonSubject),
	}
	hot := &fakeTier{events: nil, tier: history.TierJetStream}
	cold := &fakeTier{events: coldEvents, tier: history.TierPostgres}

	r := testBuild(hot, cold, now)
	stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		NotBefore: edge.Add(-20 * 24 * time.Hour),
		Direction: eventbus.DirectionForward,
		PageSize:  50,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	got := drain(t, stream)
	require.Len(t, got, 3)
	// Direction is forward: oldest → newest.
	assert.Equal(t, coldEvents[0].ID, got[0].ID)
	assert.Equal(t, coldEvents[2].ID, got[2].ID)
	assert.Equal(t, 1, cold.calls, "cold tier serves the start of this query")
}

// ---------------------------------------------------------------------------
// Scenario 3: Cursor exactly at the boundary edge
//
// No double-delivery, no skip. Either tier may serve; result is identical.
// ---------------------------------------------------------------------------

func TestTierCrossoverCursorAtBoundaryEdge(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	edge := edgeAt(now)
	// Event exactly at the edge: our rule "edge ties go to JS" routes it
	// to the hot tier. Cold tier deliberately empty so a misroute to PG
	// would produce zero events.
	atEdge := mintEvent(t, edge, commonSubject)
	hot := &fakeTier{events: []eventbus.Event{atEdge}, tier: history.TierJetStream}
	cold := &fakeTier{events: nil, tier: history.TierPostgres}

	r := testBuild(hot, cold, now)
	stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		NotBefore: edge,
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	got := drain(t, stream)
	require.Len(t, got, 1)
	assert.Equal(t, atEdge.ID, got[0].ID)
}

// ---------------------------------------------------------------------------
// Scenario 4: Page boundary crosses tier edge
//
// Half page in PG, half in JS, page continues cleanly across the tier
// transition.
// ---------------------------------------------------------------------------

func TestTierCrossoverPageBoundaryCrosses(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	edge := edgeAt(now)

	coldEvents := []eventbus.Event{
		mintEvent(t, edge.Add(-3*time.Hour), commonSubject),
		mintEvent(t, edge.Add(-2*time.Hour), commonSubject),
	}
	hotEvents := []eventbus.Event{
		mintEvent(t, edge.Add(time.Hour), commonSubject),
		mintEvent(t, edge.Add(2*time.Hour), commonSubject),
		mintEvent(t, edge.Add(3*time.Hour), commonSubject),
	}
	hot := &fakeTier{events: hotEvents, tier: history.TierJetStream}
	cold := &fakeTier{events: coldEvents, tier: history.TierPostgres}

	r := testBuild(hot, cold, now)
	stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		NotBefore: edge.Add(-5 * time.Hour),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	got := drain(t, stream)
	require.Len(t, got, 5, "all 5 events across both tiers")
	// Strictly ascending by ULID.
	for i := 1; i < len(got); i++ {
		assert.True(t, got[i-1].ID.Compare(got[i].ID) < 0,
			"events out of order at position %d", i)
	}
	// Both tiers should have been queried exactly once.
	assert.Equal(t, 1, cold.calls)
	assert.Equal(t, 1, hot.calls)
}

// ---------------------------------------------------------------------------
// Scenario 5: Clock skew absorbed by safetyMargin
//
// Inject a 5s skew between clock and event timestamps. The 1h safety margin
// absorbs the skew; no duplicates, no gaps.
// ---------------------------------------------------------------------------

func TestTierCrossoverClockSkewAbsorbed(t *testing.T) {
	serverNow := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	// Events stamped at PG's clock, which runs 5s behind the server's.
	pgSkew := -5 * time.Second

	edge := edgeAt(serverNow)
	// An event whose timestamp is in the overlap window — strictly less
	// than edge by pgSkew, meaning PG has it but JS still retains it too.
	// safetyMargin (1h) easily absorbs 5s; Reader must not return it twice.
	overlapTime := edge.Add(pgSkew)
	shared := mintEvent(t, overlapTime, commonSubject)

	hot := &fakeTier{events: []eventbus.Event{shared}, tier: history.TierJetStream}
	// Inject the same event on the cold side too; ULID dedup in the
	// crossover iterator is the thing under test.
	cold := &fakeTier{events: []eventbus.Event{shared}, tier: history.TierPostgres}

	r := testBuild(hot, cold, serverNow)
	stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		NotBefore: edge.Add(-24 * time.Hour),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	got := drain(t, stream)
	require.Len(t, got, 1, "ULID dedup must suppress the duplicate")
	assert.Equal(t, shared.ID, got[0].ID)
}

// ---------------------------------------------------------------------------
// Scenario 6: Forward direction end-to-end
// ---------------------------------------------------------------------------

func TestTierCrossoverForwardDirection(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	edge := edgeAt(now)

	coldEvents := []eventbus.Event{
		mintEvent(t, edge.Add(-10*24*time.Hour), commonSubject),
		mintEvent(t, edge.Add(-5*24*time.Hour), commonSubject),
	}
	hotEvents := []eventbus.Event{
		mintEvent(t, edge.Add(time.Hour), commonSubject),
		mintEvent(t, edge.Add(10*time.Hour), commonSubject),
	}
	hot := &fakeTier{events: hotEvents, tier: history.TierJetStream}
	cold := &fakeTier{events: coldEvents, tier: history.TierPostgres}

	r := testBuild(hot, cold, now)
	stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		NotBefore: edge.Add(-100 * 24 * time.Hour),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	got := drain(t, stream)
	require.Len(t, got, 4)
	// Oldest → newest.
	assert.Equal(t, coldEvents[0].ID, got[0].ID)
	assert.Equal(t, hotEvents[1].ID, got[3].ID)
}

// ---------------------------------------------------------------------------
// Scenario 7: Backward direction end-to-end
// ---------------------------------------------------------------------------

func TestTierCrossoverBackwardDirection(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	edge := edgeAt(now)

	coldEvents := []eventbus.Event{
		mintEvent(t, edge.Add(-10*24*time.Hour), commonSubject),
		mintEvent(t, edge.Add(-5*24*time.Hour), commonSubject),
	}
	hotEvents := []eventbus.Event{
		mintEvent(t, edge.Add(time.Hour), commonSubject),
		mintEvent(t, edge.Add(10*time.Hour), commonSubject),
	}
	hot := &fakeTier{events: hotEvents, tier: history.TierJetStream}
	cold := &fakeTier{events: coldEvents, tier: history.TierPostgres}

	r := testBuild(hot, cold, now)
	stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		NotAfter:  now,
		Direction: eventbus.DirectionBackward,
		PageSize:  10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	got := drain(t, stream)
	require.Len(t, got, 4)
	// Newest → oldest.
	assert.Equal(t, hotEvents[1].ID, got[0].ID)
	assert.Equal(t, coldEvents[0].ID, got[3].ID)
	// JS-first for backward.
	assert.Equal(t, 1, hot.calls)
	assert.Equal(t, 1, cold.calls)
}

// ---------------------------------------------------------------------------
// Scenario 8: Empty PG, full JS
// ---------------------------------------------------------------------------

func TestTierCrossoverEmptyPGFullJS(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	edge := edgeAt(now)
	hotEvents := []eventbus.Event{
		mintEvent(t, edge.Add(time.Hour), commonSubject),
		mintEvent(t, edge.Add(2*time.Hour), commonSubject),
	}
	hot := &fakeTier{events: hotEvents, tier: history.TierJetStream}
	cold := &fakeTier{events: nil, tier: history.TierPostgres}

	r := testBuild(hot, cold, now)
	stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		NotBefore: edge.Add(30 * time.Minute),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	got := drain(t, stream)
	require.Len(t, got, 2)
}

// ---------------------------------------------------------------------------
// Scenario 9: Full PG, empty JS (subject aged out)
// ---------------------------------------------------------------------------

func TestTierCrossoverEmptyJSFullPG(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	edge := edgeAt(now)
	coldEvents := []eventbus.Event{
		mintEvent(t, edge.Add(-10*24*time.Hour), commonSubject),
		mintEvent(t, edge.Add(-5*24*time.Hour), commonSubject),
	}
	hot := &fakeTier{events: nil, tier: history.TierJetStream}
	cold := &fakeTier{events: coldEvents, tier: history.TierPostgres}

	r := testBuild(hot, cold, now)
	stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		NotBefore: edge.Add(-30 * 24 * time.Hour),
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	got := drain(t, stream)
	require.Len(t, got, 2)
}

// ---------------------------------------------------------------------------
// Scenario 10: Both empty
// ---------------------------------------------------------------------------

func TestTierCrossoverBothEmpty(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	hot := &fakeTier{events: nil, tier: history.TierJetStream}
	cold := &fakeTier{events: nil, tier: history.TierPostgres}

	r := testBuild(hot, cold, now)
	stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	_, err = stream.Next(context.Background())
	assert.ErrorIs(t, err, io.EOF)
}

// ---------------------------------------------------------------------------
// Scenario 11: Non-existent subject
// ---------------------------------------------------------------------------

func TestTierCrossoverNonExistentSubject(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	edge := edgeAt(now)
	// Seed both tiers with events on a DIFFERENT subject; the Reader
	// must filter them out and return empty.
	otherSubject := eventbus.Subject("events.main.scene.01DEADBEEF123456789ABCDEFG")
	hot := &fakeTier{
		events: []eventbus.Event{mintEvent(t, edge.Add(time.Hour), otherSubject)},
		tier:   history.TierJetStream,
	}
	cold := &fakeTier{
		events: []eventbus.Event{mintEvent(t, edge.Add(-time.Hour), otherSubject)},
		tier:   history.TierPostgres,
	}
	r := testBuild(hot, cold, now)
	stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		Direction: eventbus.DirectionForward,
		PageSize:  10,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	_, err = stream.Next(context.Background())
	assert.ErrorIs(t, err, io.EOF, "non-existent subject must return EOF, not an error")
}

// ---------------------------------------------------------------------------
// Scenario 12: Plugin-owned subject routes to plugin
//
// F4 ships without a PluginHistoryRouter wired. A plugin-owned subject
// therefore MUST surface the EVENTBUS_PLUGIN_HISTORY_NOT_WIRED error rather
// than silently serving from host storage. Wiring the real router is F5
// scope (holomush-1tvn.12).
// ---------------------------------------------------------------------------

func TestTierCrossoverPluginOwnedSubjectRoutesToPlugin(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	owners, err := audit.NewOwnerMap([]audit.SubjectOwner{
		{PluginName: "core-scenes", Pattern: "events.*.scene.>"},
	})
	require.NoError(t, err)

	pluginSubject := eventbus.Subject("events.main.scene.01ABC.ic")

	t.Run("without router wired, surfaces explicit error", func(t *testing.T) {
		hot := &fakeTier{tier: history.TierJetStream, err: errors.New("hot must not be consulted")}
		cold := &fakeTier{tier: history.TierPostgres, err: errors.New("cold must not be consulted")}
		r := testBuild(hot, cold, now, history.WithOwners(owners))
		_, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
			Subject:   pluginSubject,
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		require.Error(t, err)
		// Code is EVENTBUS_PLUGIN_HISTORY_NOT_WIRED — but we match on
		// substring to avoid pulling oops.AsOops here.
		assert.Contains(t, err.Error(), "plugin-owned subject requires PluginHistoryRouter")
		assert.Equal(t, 0, hot.calls)
		assert.Equal(t, 0, cold.calls)
	})

	t.Run("with router wired, delegates to the plugin", func(t *testing.T) {
		router := &stubRouter{
			response: []eventbus.Event{
				mintEvent(t, now.Add(-1*time.Hour), pluginSubject),
			},
		}
		hot := &fakeTier{tier: history.TierJetStream, err: errors.New("hot must not be consulted")}
		cold := &fakeTier{tier: history.TierPostgres, err: errors.New("cold must not be consulted")}
		r := testBuild(hot, cold, now,
			history.WithOwners(owners),
			history.WithPluginRouter(router),
		)
		stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
			Subject:   pluginSubject,
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })
		got := drain(t, stream)
		require.Len(t, got, 1)
		assert.Equal(t, "core-scenes", router.lastPlugin)
		assert.Equal(t, 0, hot.calls, "plugin-owned subjects MUST NOT touch host tiers")
		assert.Equal(t, 0, cold.calls)
	})
}

// stubRouter satisfies PluginHistoryRouter for the Scenario 12 positive
// path. Records the plugin name and returns a pre-baked event slice.
type stubRouter struct {
	response   []eventbus.Event
	lastPlugin string
}

func (s *stubRouter) QueryHistory(_ context.Context, pluginName string, _ eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
	s.lastPlugin = pluginName
	return &sliceStream{events: s.response}, nil
}

// sliceStream is a trivial HistoryStream that yields a pre-computed slice.
type sliceStream struct {
	events []eventbus.Event
	i      int
}

func (s *sliceStream) Next(_ context.Context) (eventbus.Event, error) {
	if s.i >= len(s.events) {
		return eventbus.Event{}, io.EOF
	}
	e := s.events[s.i]
	s.i++
	return e, nil
}

func (s *sliceStream) Close() error { return nil }

// ---------------------------------------------------------------------------
// Boundary unit tests for selectStartTier via the exported Reader surface.
// These prove the edge-detection logic independent of the backing tiers.
// ---------------------------------------------------------------------------

func TestClampPageSizeHandlesBoundaries(t *testing.T) {
	t.Run("zero defaults to 50", func(t *testing.T) {
		assert.Equal(t, 50, history.ClampPageSize(0))
	})
	t.Run("negative defaults to 50", func(t *testing.T) {
		assert.Equal(t, 50, history.ClampPageSize(-5))
	})
	t.Run("above cap clamps to 200", func(t *testing.T) {
		assert.Equal(t, 200, history.ClampPageSize(5000))
	})
	t.Run("within range passes through", func(t *testing.T) {
		assert.Equal(t, 42, history.ClampPageSize(42))
	})
}

func TestReaderRejectsEmptySubject(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	r := testBuild(&fakeTier{tier: history.TierJetStream}, &fakeTier{tier: history.TierPostgres}, now)
	_, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Subject required")
}

func TestReaderRejectsInvalidTimeRange(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	r := testBuild(&fakeTier{tier: history.TierJetStream}, &fakeTier{tier: history.TierPostgres}, now)
	_, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		NotBefore: now,
		NotAfter:  now.Add(-1 * time.Hour),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, eventbus.ErrInvalidTimeRange)
}

// pathologicalTier ignores q.After and always returns the same full page.
// Simulates the "cursor not advanced" regression: if advanceCursor is
// removed (or returned events' IDs aren't wired into the query), a naive
// loadNextPage loop would read this tier forever while dedup hid the
// output but the internal buffer grew unbounded. The runaway-loop guard
// in loadNextPage MUST trip, returning EVENTBUS_HISTORY_BUFFER_OVERFLOW
// in bounded memory within a few page reads.
type pathologicalTier struct {
	events []eventbus.Event
	tier   history.Tier
	calls  int
}

func (p *pathologicalTier) Read(_ context.Context, q eventbus.HistoryQuery, _ time.Time, pageSize int) ([]eventbus.Event, error) {
	p.calls++
	// Deliberately return the same events regardless of q.After. Copy so
	// the returned slice can't be mutated by the Reader.
	out := make([]eventbus.Event, 0, pageSize)
	for _, e := range p.events {
		if e.Subject != q.Subject {
			continue
		}
		out = append(out, e)
		if len(out) >= pageSize {
			break
		}
	}
	return out, nil
}

func TestReaderGuardsAgainstUnboundedBufferGrowth(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	// Populate a full page worth so every tier read returns the page-size
	// cap, which is the worst-case trigger for the leak.
	events := make([]eventbus.Event, 0, 50)
	for i := 0; i < 50; i++ {
		events = append(events, mintEvent(t, now.Add(-time.Duration(i)*time.Minute), commonSubject))
	}
	hot := &pathologicalTier{events: events, tier: history.TierJetStream}
	cold := &fakeTier{tier: history.TierPostgres}
	r := history.NewReader(nil, nil, 30*24*time.Hour, fixedClock(now),
		history.WithHotTier(hot),
		history.WithColdTier(cold),
	)

	stream, err := r.QueryHistory(context.Background(), eventbus.HistoryQuery{
		Subject:   commonSubject,
		NotBefore: now.Add(-3 * time.Hour),
		Direction: eventbus.DirectionForward,
		PageSize:  50,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	// Drain — the guard must trip before memory becomes pathological. If
	// the guard is missing, this test will OOM long before the assertion
	// runs (which is itself the signal that the guard's absence is
	// unacceptable).
	var drainErr error
	for {
		_, iterErr := stream.Next(context.Background())
		if iterErr != nil {
			drainErr = iterErr
			break
		}
	}
	require.Error(t, drainErr, "guard must trip rather than loop forever")
	assert.NotErrorIs(t, drainErr, io.EOF, "guard should not silently terminate as EOF")
	assert.Contains(t, drainErr.Error(), "buffer exceeded")
	// Confirm the guard fired in bounded time: far fewer than infinity.
	assert.LessOrEqual(t, hot.calls, 20, "guard must trip well before 20 tier reads")
}

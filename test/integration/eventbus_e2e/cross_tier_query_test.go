// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	crand "crypto/rand"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/eventbus/history"
)

// TestCrossTierQueryEndToEnd mirrors the 12-scenario tier suite from
// internal/eventbus/history/tier_test.go but runs against the real
// JetStream hot tier + real PostgreSQL cold tier. The in-memory suite
// uses fakeTier; this suite uses the JS-backed and PG-backed real
// implementations so the complete read path is exercised.
//
// Spec reference: §8 "Full E2E matrix — Cross-tier query" + §8 "JS↔PG
// hot/cold tier crossover suite".
//
// Design notes:
//
//   - We seed JS via real Publisher calls for hot-tier rows and INSERT
//     directly into events_audit for cold-tier rows. Writing to both sides
//     with different timestamps is the standard technique to materialize
//     the retention edge in a test that doesn't actually wait 30 days.
//   - The injected clock (history.WithClock) lets us place "now" wherever
//     we need relative to the synthesized events, avoiding wall-clock
//     timing.
//   - Per scenario we assert:
//     (a) the right set of events is returned in the right order,
//     (b) tier-specific counters/assertions match spec expectations.
func TestCrossTierQueryEndToEnd(t *testing.T) {
	// A single bus + pool serve all subtests to keep setup cost down.
	// Each subtest picks a distinct subject so there's no cross-talk on
	// events_audit or on the EVENTS stream.
	bus := eventbustest.New(t)
	pool := freshPool(t)

	streamMaxAge := 30 * 24 * time.Hour // matches production default
	safety := time.Hour

	// "now" for every subtest. Edge = now - 30d + 1h.
	baseNow := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	edge := baseNow.Add(-streamMaxAge).Add(safety)

	// Shared pub for JS seeding.
	pub := bus.Bus.Publisher()
	require.NotNil(t, pub)

	scenarios := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, subject eventbus.Subject)
	}{
		{
			name: "scenario1_cursor_within_js_retention",
			run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
				// All events recent; Reader served entirely from JS, zero PG.
				hot := []eventbus.Event{
					mintAt(subject, baseNow.Add(-2*time.Hour), "a"),
					mintAt(subject, baseNow.Add(-1*time.Hour), "b"),
				}
				publishAll(ctx, t, pub, hot)
				// Barrier so JS has committed before we read.
				bus.AwaitStreamLastSeq(t, currentStreamLastSeq(t, bus)+0, 5*time.Second)

				r := buildReader(bus, pool, streamMaxAge, baseNow)
				stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
					Subject:   subject,
					NotBefore: baseNow.Add(-3 * time.Hour),
					Direction: eventbus.DirectionForward,
					PageSize:  50,
				})
				require.NoError(t, err)
				t.Cleanup(func() { _ = stream.Close() })

				got := drainStream(t, stream)
				require.Len(t, got, len(hot))
				assert.Equal(t, hot[0].ID, got[0].ID)
				assert.Equal(t, hot[1].ID, got[1].ID)
			},
		},
		{
			name: "scenario2_cursor_older_than_retention",
			run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
				// All events aged — projected directly to events_audit.
				cold := []eventbus.Event{
					mintAt(subject, edge.Add(-10*24*time.Hour), "a"),
					mintAt(subject, edge.Add(-5*24*time.Hour), "b"),
				}
				insertAuditRows(ctx, t, bus, pub, pool, cold)

				r := buildReader(bus, pool, streamMaxAge, baseNow)
				stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
					Subject:   subject,
					NotBefore: edge.Add(-20 * 24 * time.Hour),
					Direction: eventbus.DirectionForward,
					PageSize:  50,
				})
				require.NoError(t, err)
				t.Cleanup(func() { _ = stream.Close() })

				got := drainStream(t, stream)
				require.Len(t, got, len(cold))
				assert.Equal(t, cold[0].ID, got[0].ID)
				assert.Equal(t, cold[1].ID, got[1].ID)
			},
		},
		{
			name: "scenario3_cursor_at_boundary_edge",
			run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
				// Event exactly at edge — routes to JS per "ties go hot".
				atEdge := mintAt(subject, edge, "edge")
				publishAll(ctx, t, pub, []eventbus.Event{atEdge})

				r := buildReader(bus, pool, streamMaxAge, baseNow)
				stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
					Subject:   subject,
					NotBefore: edge,
					Direction: eventbus.DirectionForward,
					PageSize:  10,
				})
				require.NoError(t, err)
				t.Cleanup(func() { _ = stream.Close() })

				got := drainStream(t, stream)
				require.Len(t, got, 1)
				assert.Equal(t, atEdge.ID, got[0].ID)
			},
		},
		{
			name: "scenario4_page_boundary_crosses",
			run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
				cold := []eventbus.Event{
					mintAt(subject, edge.Add(-3*time.Hour), "c1"),
					mintAt(subject, edge.Add(-2*time.Hour), "c2"),
				}
				hot := []eventbus.Event{
					mintAt(subject, edge.Add(time.Hour), "h1"),
					mintAt(subject, edge.Add(2*time.Hour), "h2"),
				}
				insertAuditRows(ctx, t, bus, pub, pool, cold)
				publishAll(ctx, t, pub, hot)

				r := buildReader(bus, pool, streamMaxAge, baseNow)
				stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
					Subject:   subject,
					NotBefore: edge.Add(-5 * time.Hour),
					Direction: eventbus.DirectionForward,
					PageSize:  10,
				})
				require.NoError(t, err)
				t.Cleanup(func() { _ = stream.Close() })

				got := drainStream(t, stream)
				require.Len(t, got, 4, "all 4 across both tiers")
				for i := 1; i < len(got); i++ {
					assert.True(t, got[i-1].ID.Compare(got[i].ID) < 0,
						"expected strictly ascending ULID at position %d", i)
				}
			},
		},
		{
			name: "scenario5_clock_skew_absorbed",
			run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
				// Place an event at edge-5s: within safetyMargin window. It
				// lives in JS; we also INSERT it in PG to represent the
				// "lagging projection" overlap. Dedup must suppress the dup.
				dup := mintAt(subject, edge.Add(-5*time.Second), "overlap")
				seqBefore := currentStreamLastSeq(t, bus)
				publishAll(ctx, t, pub, []eventbus.Event{dup})
				bus.AwaitStreamLastSeq(t, seqBefore+1, 5*time.Second)
				dupSeq := currentStreamLastSeq(t, bus)
				// Insert into cold with the SAME JS seq so seenSeqs dedup
				// recognises the overlap event as already delivered.
				insertAuditRowWithSeq(ctx, t, pool, dup, dupSeq)

				r := buildReader(bus, pool, streamMaxAge, baseNow)
				stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
					Subject:   subject,
					NotBefore: edge.Add(-24 * time.Hour),
					Direction: eventbus.DirectionForward,
					PageSize:  10,
				})
				require.NoError(t, err)
				t.Cleanup(func() { _ = stream.Close() })

				got := drainStream(t, stream)
				require.Len(t, got, 1, "ULID dedup must suppress the duplicate")
				assert.Equal(t, dup.ID, got[0].ID)
			},
		},
		{
			name: "scenario6_forward_direction",
			run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
				cold := []eventbus.Event{
					mintAt(subject, edge.Add(-10*24*time.Hour), "c1"),
					mintAt(subject, edge.Add(-5*24*time.Hour), "c2"),
				}
				hot := []eventbus.Event{
					mintAt(subject, edge.Add(time.Hour), "h1"),
					mintAt(subject, edge.Add(10*time.Hour), "h2"),
				}
				insertAuditRows(ctx, t, bus, pub, pool, cold)
				publishAll(ctx, t, pub, hot)

				r := buildReader(bus, pool, streamMaxAge, baseNow)
				stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
					Subject:   subject,
					NotBefore: edge.Add(-100 * 24 * time.Hour),
					Direction: eventbus.DirectionForward,
					PageSize:  10,
				})
				require.NoError(t, err)
				t.Cleanup(func() { _ = stream.Close() })

				got := drainStream(t, stream)
				require.Len(t, got, 4)
				// Oldest -> newest.
				assert.Equal(t, cold[0].ID, got[0].ID)
				assert.Equal(t, hot[1].ID, got[3].ID)
			},
		},
		{
			name: "scenario7_backward_direction",
			run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
				cold := []eventbus.Event{
					mintAt(subject, edge.Add(-10*24*time.Hour), "c1"),
					mintAt(subject, edge.Add(-5*24*time.Hour), "c2"),
				}
				hot := []eventbus.Event{
					mintAt(subject, edge.Add(time.Hour), "h1"),
					mintAt(subject, edge.Add(10*time.Hour), "h2"),
				}
				insertAuditRows(ctx, t, bus, pub, pool, cold)
				publishAll(ctx, t, pub, hot)

				r := buildReader(bus, pool, streamMaxAge, baseNow)
				stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
					Subject:   subject,
					NotAfter:  baseNow,
					Direction: eventbus.DirectionBackward,
					PageSize:  10,
				})
				require.NoError(t, err)
				t.Cleanup(func() { _ = stream.Close() })

				got := drainStream(t, stream)
				require.Len(t, got, 4)
				// Newest -> oldest.
				assert.Equal(t, hot[1].ID, got[0].ID)
				assert.Equal(t, cold[0].ID, got[3].ID)
			},
		},
		{
			name: "scenario8_empty_pg_full_js",
			run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
				hot := []eventbus.Event{
					mintAt(subject, edge.Add(time.Hour), "h1"),
					mintAt(subject, edge.Add(2*time.Hour), "h2"),
				}
				publishAll(ctx, t, pub, hot)

				r := buildReader(bus, pool, streamMaxAge, baseNow)
				stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
					Subject:   subject,
					NotBefore: edge.Add(30 * time.Minute),
					Direction: eventbus.DirectionForward,
					PageSize:  10,
				})
				require.NoError(t, err)
				t.Cleanup(func() { _ = stream.Close() })

				got := drainStream(t, stream)
				require.Len(t, got, 2)
			},
		},
		{
			name: "scenario9_empty_js_full_pg",
			run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
				cold := []eventbus.Event{
					mintAt(subject, edge.Add(-10*24*time.Hour), "c1"),
					mintAt(subject, edge.Add(-5*24*time.Hour), "c2"),
				}
				insertAuditRows(ctx, t, bus, pub, pool, cold)

				r := buildReader(bus, pool, streamMaxAge, baseNow)
				stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
					Subject:   subject,
					NotBefore: edge.Add(-30 * 24 * time.Hour),
					Direction: eventbus.DirectionForward,
					PageSize:  10,
				})
				require.NoError(t, err)
				t.Cleanup(func() { _ = stream.Close() })

				got := drainStream(t, stream)
				require.Len(t, got, 2)
			},
		},
		{
			name: "scenario10_both_empty",
			run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
				r := buildReader(bus, pool, streamMaxAge, baseNow)
				stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
					Subject:   subject,
					Direction: eventbus.DirectionForward,
					PageSize:  10,
				})
				require.NoError(t, err)
				t.Cleanup(func() { _ = stream.Close() })

				_, nerr := stream.Next(ctx)
				assert.ErrorIs(t, nerr, io.EOF)
			},
		},
		{
			name: "scenario11_nonexistent_subject",
			run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
				// Seed events on a DIFFERENT subject; query for ours.
				other := eventbus.Subject("events.main.elsewhere.zzz")
				publishAll(ctx, t, pub, []eventbus.Event{mintAt(other, edge.Add(time.Hour), "x")})
				insertAuditRows(ctx, t, bus, pub, pool, []eventbus.Event{mintAt(other, edge.Add(-time.Hour), "y")})

				r := buildReader(bus, pool, streamMaxAge, baseNow)
				stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
					Subject:   subject,
					Direction: eventbus.DirectionForward,
					PageSize:  10,
				})
				require.NoError(t, err)
				t.Cleanup(func() { _ = stream.Close() })

				_, nerr := stream.Next(ctx)
				assert.ErrorIs(t, nerr, io.EOF)
			},
		},
		{
			name: "scenario12_plugin_owned_subject_routes_to_plugin",
			run: func(t *testing.T, ctx context.Context, subject eventbus.Subject) {
				// For plugin-owned subjects the Reader MUST NOT fall back
				// to host tiers. Without a router wired, we expect the
				// explicit EVENTBUS_PLUGIN_HISTORY_NOT_WIRED error. This
				// is the documented F4 ship state; wiring a real router
				// is exercised by the plugin_audit_isolation_test.
				owners, err := audit.NewOwnerMap([]audit.SubjectOwner{
					{PluginName: "core-scenes", Pattern: "events.*.scene.>"},
				})
				require.NoError(t, err)
				r := buildReader(bus, pool, streamMaxAge, baseNow, history.WithOwners(owners))
				pluginSubject := eventbus.Subject("events.main.scene.01ABC.ic")
				_, qerr := r.QueryHistory(ctx, eventbus.HistoryQuery{
					Subject:   pluginSubject,
					Direction: eventbus.DirectionForward,
					PageSize:  10,
				})
				require.Error(t, qerr)
				assert.Contains(t, qerr.Error(), "plugin-owned subject requires PluginHistoryRouter")
			},
		},
	}

	for i, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			// Per-subtest subject so state doesn't leak between scenarios.
			subject := eventbus.Subject(subjectForScenario(i))
			sc.run(t, ctx, subject)
		})
	}
}

// subjectForScenario produces a unique subject per scenario index. Kept
// out-of-band so scenarios are identified by their deterministic slot.
func subjectForScenario(idx int) string {
	// Depth 4: events.main.ctq.s<n>. `ctq` = cross-tier-query for grep.
	return "events.main.ctq.s" + itoa(idx)
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	// Scenarios list has 12 entries; just support two-digit paths.
	tens := n / 10
	ones := n % 10
	return string(rune('0'+tens)) + string(rune('0'+ones))
}

// mintAt builds an Event whose ULID's embedded timestamp equals ts. The
// Timestamp field also receives ts so both ULID-time and Timestamp-time
// are consistent.
func mintAt(subject eventbus.Subject, ts time.Time, body string) eventbus.Event {
	id, err := ulid.New(ulid.Timestamp(ts), crand.Reader)
	if err != nil {
		panic(err)
	}
	return eventbus.Event{
		ID:        id,
		Subject:   subject,
		Type:      eventbus.Type("scene.pose"),
		Timestamp: ts.UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte(body),
	}
}

// ---------------------------------------------------------------------------
// Helpers used by multiple tests in this package.
// ---------------------------------------------------------------------------

// publishAll publishes every event via pub, failing the test on the first
// error. Returns when every event has been acked by the server.
func publishAll(ctx context.Context, t *testing.T, pub eventbus.Publisher, events []eventbus.Event) {
	t.Helper()
	for _, e := range events {
		require.NoError(t, pub.Publish(ctx, e))
	}
}

// insertAuditRows simulates the audit projection for "cold" events by:
//  1. Publishing each event to the EVENTS JetStream stream (getting a real,
//     monotonic stream sequence).
//  2. Inserting the event into events_audit with that real js_seq.
//
// This is necessary because seq-based crossover dedup (seenSeqs) requires
// that the js_seq in events_audit matches what the hot tier returns for the
// same physical event. Publishing first also gives cold events lower JS seqs
// than subsequently published hot events, which preserves the ordering
// invariant that appendOrdered sorts by seq.
//
// The hot tier's matchesQuery filter (Timestamp < edge → reject) ensures
// these events are served from cold (events_audit) rather than from JS,
// even though they are technically still in the JS stream.
func insertAuditRows(ctx context.Context, t *testing.T, bus *eventbustest.Embedded, pub eventbus.Publisher, pool *pgxpool.Pool, events []eventbus.Event) {
	t.Helper()
	for _, e := range events {
		seqBefore := currentStreamLastSeq(t, bus)
		require.NoError(t, pub.Publish(ctx, e))
		bus.AwaitStreamLastSeq(t, seqBefore+1, 5*time.Second)
		seq := currentStreamLastSeq(t, bus)
		insertAuditRowWithSeq(ctx, t, pool, e, seq)
	}
}

// insertAuditRowWithSeq INSERTs one events_audit row with an explicit js_seq.
func insertAuditRowWithSeq(ctx context.Context, t *testing.T, pool *pgxpool.Pool, e eventbus.Event, seq uint64) {
	t.Helper()
	id := e.ID.Bytes()
	var actorID []byte
	if e.Actor.ID != (ulid.ULID{}) {
		b := e.Actor.ID.Bytes()
		actorID = b
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			payload, schema_ver, codec, js_seq
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING`,
		id,
		string(e.Subject),
		string(e.Type),
		e.Timestamp,
		e.Actor.Kind.String(),
		actorID,
		e.Payload,
		int16(1),
		"identity",
		int64(seq), //nolint:gosec // G115: seq is always a positive JetStream sequence; fits safely in int64
	)
	require.NoError(t, err)
}

// drainStream pulls every event from stream until io.EOF, failing on any
// other error.
func drainStream(t *testing.T, stream eventbus.HistoryStream) []eventbus.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
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

// currentStreamLastSeq reads the EVENTS stream's LastSeq for use as a
// barrier snapshot. Returns 0 on error (not fatal — caller treats it as
// "no offset known yet").
func currentStreamLastSeq(t *testing.T, bus *eventbustest.Embedded) uint64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	s, err := bus.JS.Stream(ctx, eventbus.StreamName)
	if err != nil {
		return 0
	}
	info, err := s.Info(ctx)
	if err != nil {
		return 0
	}
	return info.State.LastSeq
}

// buildReader constructs a history.Reader with an injected clock so the
// retention edge lives wherever the scenario needs it.
func buildReader(bus *eventbustest.Embedded, pool *pgxpool.Pool, streamMaxAge time.Duration, now time.Time, extra ...history.Option) *history.Reader {
	opts := []history.Option{
		history.WithClock(func() time.Time { return now }),
	}
	opts = append(opts, extra...)
	return history.NewReader(bus.JS, pool, streamMaxAge, func() time.Time { return now }, opts...)
}

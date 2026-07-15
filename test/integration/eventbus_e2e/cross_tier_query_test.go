// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/eventbus/history"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/plugin/plugintest"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// Cross-tier query end-to-end specs — mirrors the 12-scenario tier suite from
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
var _ = Describe("Cross-tier query end-to-end", func() {
	var (
		bus          *eventbustest.Embedded
		pool         *pgxpool.Pool
		pub          eventbus.Publisher
		streamMaxAge time.Duration
		baseNow      time.Time
		edge         time.Time
	)

	BeforeEach(func() {
		// A single bus + pool serve all specs to keep setup cost down.
		// Each spec picks a distinct subject so there's no cross-talk on
		// events_audit or on the EVENTS stream.
		bus = freshBus()
		pool = freshPool()

		streamMaxAge = 30 * 24 * time.Hour // matches production default
		safety := time.Hour

		// "now" for every spec. Edge = now - 30d + 1h.
		baseNow = time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
		edge = baseNow.Add(-streamMaxAge).Add(safety)

		// Shared pub for JS seeding.
		pub = bus.Bus.Publisher()
		Expect(pub).NotTo(BeNil())
	})

	It("scenario1: cursor within JS retention serves entirely from hot tier", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		subject := eventbus.Subject(subjectForScenario(1))

		// All events recent; Reader served entirely from JS, zero PG.
		hot := []eventbus.Event{
			mintAt(subject, baseNow.Add(-2*time.Hour), "a"),
			mintAt(subject, baseNow.Add(-1*time.Hour), "b"),
		}
		publishAll(ctx, suiteT, pub, hot)
		// Barrier so JS has committed before we read.
		bus.AwaitStreamLastSeq(suiteT, currentStreamLastSeq(suiteT, bus)+0, 5*time.Second)

		r := buildReader(bus, pool, streamMaxAge, baseNow)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			NotBefore: baseNow.Add(-3 * time.Hour),
			Direction: eventbus.DirectionForward,
			PageSize:  50,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		got := drainStream(suiteT, stream)
		Expect(got).To(HaveLen(len(hot)))
		Expect(got[0].ID).To(Equal(hot[0].ID))
		Expect(got[1].ID).To(Equal(hot[1].ID))
	})

	It("scenario2: cursor older than retention serves from cold tier", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		subject := eventbus.Subject(subjectForScenario(2))

		// All events aged — projected directly to events_audit.
		cold := []eventbus.Event{
			mintAt(subject, edge.Add(-10*24*time.Hour), "a"),
			mintAt(subject, edge.Add(-5*24*time.Hour), "b"),
		}
		insertAuditRows(ctx, suiteT, bus, pub, pool, cold)

		r := buildReader(bus, pool, streamMaxAge, baseNow)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			NotBefore: edge.Add(-20 * 24 * time.Hour),
			Direction: eventbus.DirectionForward,
			PageSize:  50,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		got := drainStream(suiteT, stream)
		Expect(got).To(HaveLen(len(cold)))
		Expect(got[0].ID).To(Equal(cold[0].ID))
		Expect(got[1].ID).To(Equal(cold[1].ID))
	})

	It("scenario3: cursor at boundary edge routes to JS per ties-go-hot", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		subject := eventbus.Subject(subjectForScenario(3))

		// Event exactly at edge — routes to JS per "ties go hot".
		atEdge := mintAt(subject, edge, "edge")
		publishAll(ctx, suiteT, pub, []eventbus.Event{atEdge})

		r := buildReader(bus, pool, streamMaxAge, baseNow)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			NotBefore: edge,
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		got := drainStream(suiteT, stream)
		Expect(got).To(HaveLen(1))
		Expect(got[0].ID).To(Equal(atEdge.ID))
	})

	It("scenario4: page boundary crosses tiers returning all events in order", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		subject := eventbus.Subject(subjectForScenario(4))

		cold := []eventbus.Event{
			mintAt(subject, edge.Add(-3*time.Hour), "c1"),
			mintAt(subject, edge.Add(-2*time.Hour), "c2"),
		}
		hot := []eventbus.Event{
			mintAt(subject, edge.Add(time.Hour), "h1"),
			mintAt(subject, edge.Add(2*time.Hour), "h2"),
		}
		insertAuditRows(ctx, suiteT, bus, pub, pool, cold)
		publishAll(ctx, suiteT, pub, hot)

		r := buildReader(bus, pool, streamMaxAge, baseNow)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			NotBefore: edge.Add(-5 * time.Hour),
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		got := drainStream(suiteT, stream)
		Expect(got).To(HaveLen(4), "all 4 across both tiers")
		for i := 1; i < len(got); i++ {
			Expect(got[i-1].ID.Compare(got[i].ID)).To(BeNumerically("<", 0),
				"expected strictly ascending ULID at position %d", i)
		}
	})

	It("scenario5: clock skew within safety margin is absorbed by ULID dedup", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		subject := eventbus.Subject(subjectForScenario(5))

		// Place an event at edge-5s: within safetyMargin window. It
		// lives in JS; we also INSERT it in PG to represent the
		// "lagging projection" overlap. Dedup must suppress the dup.
		dup := mintAt(subject, edge.Add(-5*time.Second), "overlap")
		seqBefore := currentStreamLastSeq(suiteT, bus)
		publishAll(ctx, suiteT, pub, []eventbus.Event{dup})
		bus.AwaitStreamLastSeq(suiteT, seqBefore+1, 5*time.Second)
		dupSeq := currentStreamLastSeq(suiteT, bus)
		// Insert into cold with the SAME JS seq so seenSeqs dedup
		// recognises the overlap event as already delivered.
		insertAuditRowWithSeq(ctx, suiteT, pool, dup, dupSeq)

		r := buildReader(bus, pool, streamMaxAge, baseNow)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			NotBefore: edge.Add(-24 * time.Hour),
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		got := drainStream(suiteT, stream)
		Expect(got).To(HaveLen(1), "ULID dedup must suppress the duplicate")
		Expect(got[0].ID).To(Equal(dup.ID))
	})

	It("scenario6: forward direction returns events oldest to newest", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		subject := eventbus.Subject(subjectForScenario(6))

		cold := []eventbus.Event{
			mintAt(subject, edge.Add(-10*24*time.Hour), "c1"),
			mintAt(subject, edge.Add(-5*24*time.Hour), "c2"),
		}
		hot := []eventbus.Event{
			mintAt(subject, edge.Add(time.Hour), "h1"),
			mintAt(subject, edge.Add(10*time.Hour), "h2"),
		}
		insertAuditRows(ctx, suiteT, bus, pub, pool, cold)
		publishAll(ctx, suiteT, pub, hot)

		r := buildReader(bus, pool, streamMaxAge, baseNow)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			NotBefore: edge.Add(-100 * 24 * time.Hour),
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		got := drainStream(suiteT, stream)
		Expect(got).To(HaveLen(4))
		// Oldest -> newest.
		Expect(got[0].ID).To(Equal(cold[0].ID))
		Expect(got[3].ID).To(Equal(hot[1].ID))
	})

	It("scenario7: backward direction returns events newest to oldest", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		subject := eventbus.Subject(subjectForScenario(7))

		cold := []eventbus.Event{
			mintAt(subject, edge.Add(-10*24*time.Hour), "c1"),
			mintAt(subject, edge.Add(-5*24*time.Hour), "c2"),
		}
		hot := []eventbus.Event{
			mintAt(subject, edge.Add(time.Hour), "h1"),
			mintAt(subject, edge.Add(10*time.Hour), "h2"),
		}
		insertAuditRows(ctx, suiteT, bus, pub, pool, cold)
		publishAll(ctx, suiteT, pub, hot)

		r := buildReader(bus, pool, streamMaxAge, baseNow)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			NotAfter:  baseNow,
			Direction: eventbus.DirectionBackward,
			PageSize:  10,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		got := drainStream(suiteT, stream)
		Expect(got).To(HaveLen(4))
		// Newest -> oldest.
		Expect(got[0].ID).To(Equal(hot[1].ID))
		Expect(got[3].ID).To(Equal(cold[0].ID))
	})

	It("scenario8: empty PG with hot-tier events returns hot events", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		subject := eventbus.Subject(subjectForScenario(8))

		hot := []eventbus.Event{
			mintAt(subject, edge.Add(time.Hour), "h1"),
			mintAt(subject, edge.Add(2*time.Hour), "h2"),
		}
		publishAll(ctx, suiteT, pub, hot)

		r := buildReader(bus, pool, streamMaxAge, baseNow)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			NotBefore: edge.Add(30 * time.Minute),
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		got := drainStream(suiteT, stream)
		Expect(got).To(HaveLen(2))
	})

	It("scenario9: empty JS with cold-tier events returns cold events", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		subject := eventbus.Subject(subjectForScenario(9))

		cold := []eventbus.Event{
			mintAt(subject, edge.Add(-10*24*time.Hour), "c1"),
			mintAt(subject, edge.Add(-5*24*time.Hour), "c2"),
		}
		insertAuditRows(ctx, suiteT, bus, pub, pool, cold)

		r := buildReader(bus, pool, streamMaxAge, baseNow)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			NotBefore: edge.Add(-30 * 24 * time.Hour),
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		got := drainStream(suiteT, stream)
		Expect(got).To(HaveLen(2))
	})

	It("scenario10: both tiers empty yields EOF immediately", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		subject := eventbus.Subject(subjectForScenario(10))

		r := buildReader(bus, pool, streamMaxAge, baseNow)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		_, nerr := stream.Next(ctx)
		Expect(errors.Is(nerr, io.EOF)).To(BeTrue())
	})

	It("scenario11: nonexistent subject returns no events", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		subject := eventbus.Subject(subjectForScenario(11))

		// Seed events on a DIFFERENT subject; query for ours.
		other := eventbus.Subject("events.main.elsewhere.zzz")
		publishAll(ctx, suiteT, pub, []eventbus.Event{mintAt(other, edge.Add(time.Hour), "x")})
		insertAuditRows(ctx, suiteT, bus, pub, pool, []eventbus.Event{mintAt(other, edge.Add(-time.Hour), "y")})

		r := buildReader(bus, pool, streamMaxAge, baseNow)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		_, nerr := stream.Next(ctx)
		Expect(errors.Is(nerr, io.EOF)).To(BeTrue())
	})

	It("scenario_w9ml: plugin actor ULID round-trips through hot tier byte-equal", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		subject := eventbus.Subject(subjectForScenario(12))

		// Post-w9ml: plugin actors carry a ULID via Actor.ID
		// (resolved at stamp time via IdentityRegistry.IDByName).
		// This scenario asserts a published plugin-actor event
		// round-trips through JS hot tier with the ULID intact —
		// i.e., publisher serializes ULID bytes into the proto
		// envelope, subscriber/reader reconstructs them, and the
		// resulting eventbus.Event.Actor preserves both Kind and ID.
		pluginULID := plugintest.PluginULIDFromName("core-scenes")
		pluginEv := mintAt(subject, baseNow.Add(-30*time.Minute), "plugin")
		pluginEv.Actor = eventbus.Actor{
			Kind: eventbus.ActorKindPlugin,
			ID:   pluginULID,
		}
		publishAll(ctx, suiteT, pub, []eventbus.Event{pluginEv})
		bus.AwaitStreamLastSeq(suiteT, currentStreamLastSeq(suiteT, bus)+0, 5*time.Second)

		r := buildReader(bus, pool, streamMaxAge, baseNow)
		stream, err := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   subject,
			NotBefore: baseNow.Add(-1 * time.Hour),
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = stream.Close() })

		got := drainStream(suiteT, stream)
		Expect(got).To(HaveLen(1))
		Expect(got[0].ID).To(Equal(pluginEv.ID))
		Expect(got[0].Actor.Kind).To(Equal(eventbus.ActorKindPlugin),
			"plugin Actor.Kind MUST round-trip through hot tier")
		Expect(got[0].Actor.ID).To(Equal(pluginULID),
			"plugin Actor.ID (ULID) MUST round-trip byte-equal through hot tier")
	})

	It("scenario12: plugin-owned subject returns EVENTBUS_PLUGIN_HISTORY_NOT_WIRED error", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		// For plugin-owned subjects the Reader MUST NOT fall back
		// to host tiers. Without a router wired, we expect the
		// explicit EVENTBUS_PLUGIN_HISTORY_NOT_WIRED error. This
		// is the documented F4 ship state; wiring a real router
		// is exercised by the plugin_audit_isolation_test.
		owners, err := audit.NewOwnerMap([]audit.SubjectOwner{
			{PluginName: "core-scenes", Pattern: "events.*.scene.>"},
		})
		Expect(err).NotTo(HaveOccurred())
		r := buildReader(bus, pool, streamMaxAge, baseNow, history.WithOwners(owners))
		pluginSubject := eventbus.Subject("events.main.scene.01ABC.ic")
		_, qerr := r.QueryHistory(ctx, eventbus.HistoryQuery{
			Subject:   pluginSubject,
			Direction: eventbus.DirectionForward,
			PageSize:  10,
		})
		Expect(qerr).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, qerr, "EVENTBUS_PLUGIN_HISTORY_NOT_WIRED")
	})
})

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
		if err := pub.Publish(ctx, e); err != nil {
			t.Fatalf("publishAll: %v", err)
		}
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
		if err := pub.Publish(ctx, e); err != nil {
			t.Fatalf("insertAuditRows publish: %v", err)
		}
		bus.AwaitStreamLastSeq(t, seqBefore+1, 5*time.Second)
		seq := currentStreamLastSeq(t, bus)
		insertAuditRowWithSeq(ctx, t, pool, e, seq)
	}
}

// ensureEventsAuditPartitionForCrossTier creates the month partition covering
// eventMs (BIGINT epoch-ns) if absent. Cross-tier scenarios seed events at
// times well outside the current+2 partitions created by 000052, so the
// covering partition must be created before the direct INSERT.
func ensureEventsAuditPartitionForCrossTier(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventMs int64) {
	t.Helper()
	tm := time.Unix(0, eventMs).UTC()
	start := time.Date(tm.Year(), tm.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	name := fmt.Sprintf("events_audit_%04d_%02d", start.Year(), int(start.Month()))
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF events_audit FOR VALUES FROM (%d) TO (%d)`,
		name, start.UnixNano(), end.UnixNano(),
	)); err != nil {
		t.Fatalf("ensureEventsAuditPartitionForCrossTier: %v", err)
	}
}

// insertAuditRowWithSeq INSERTs one events_audit row with an explicit js_seq.
//
// The envelope column carries a marshaled eventbusv1.Event matching what
// the audit projection would persist (msg.Data() in projection.go:281).
// This is required post-Phase 3d Task 5 because the cold reader unmarshals
// the envelope to recover Subject/Type/Timestamp/Actor.
func insertAuditRowWithSeq(ctx context.Context, t *testing.T, pool *pgxpool.Pool, e eventbus.Event, seq uint64) {
	t.Helper()
	id := e.ID.Bytes()
	// event_ms (000052 partition key) is derived from the event's real ULID
	// (e.ID) — identical to what writeAuditRow would compute, so the seeded row
	// dedups exactly as production. Cold-tier scenarios craft events with OLD
	// embedded times (outside the current partition), so ensure the covering
	// partition exists first.
	eventMS := int64(e.ID.Time()) * int64(time.Millisecond)
	ensureEventsAuditPartitionForCrossTier(t, ctx, pool, eventMS)
	var actorID []byte
	if e.Actor.ID != (ulid.ULID{}) {
		b := e.Actor.ID.Bytes()
		actorID = b
	}
	envelopeBytes, err := proto.Marshal(&eventbusv1.Event{
		Id:        id,
		Subject:   string(e.Subject),
		Type:      string(e.Type),
		Timestamp: timestamppb.New(e.Timestamp),
		Actor: &eventbusv1.Actor{
			Kind: actorKindToProto(e.Actor.Kind),
			Id:   actorID,
		},
		Payload: e.Payload,
	})
	if err != nil {
		t.Fatalf("insertAuditRowWithSeq marshal: %v", err)
	}
	_, qerr := pool.Exec(
		ctx, `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			envelope, schema_ver, codec, js_seq, rendering, event_ms
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id, event_ms) DO NOTHING`,
		id,
		string(e.Subject),
		string(e.Type),
		pgnanos.From(e.Timestamp),
		e.Actor.Kind.String(),
		actorID,
		envelopeBytes,
		int16(1),
		"identity",
		int64(seq), //nolint:gosec // G115: seq is always a positive JetStream sequence; fits safely in int64
		[]byte(`{}`),
		eventMS,
	)
	if qerr != nil {
		t.Fatalf("insertAuditRowWithSeq exec: %v", qerr)
	}
}

// actorKindToProto maps the eventbus.ActorKind enum to its proto twin.
// Mirrors the inverse mapping in actor_from_envelope.go.
func actorKindToProto(k eventbus.ActorKind) eventbusv1.ActorKind {
	switch k {
	case eventbus.ActorKindCharacter:
		return eventbusv1.ActorKind_ACTOR_KIND_CHARACTER
	case eventbus.ActorKindPlayer:
		return eventbusv1.ActorKind_ACTOR_KIND_PLAYER
	case eventbus.ActorKindSystem:
		return eventbusv1.ActorKind_ACTOR_KIND_SYSTEM
	case eventbus.ActorKindPlugin:
		return eventbusv1.ActorKind_ACTOR_KIND_PLUGIN
	default:
		return eventbusv1.ActorKind_ACTOR_KIND_UNSPECIFIED
	}
}

// drainStream pulls every event from stream until io.EOF, failing on any
// other error.
//
// Uses context.Background() rather than t.Context() because callers pass
// suiteT (the Ginkgo suite-level testing.T); if a background goroutine
// invokes drainStream after the suite finishes, t.Context() panics in
// Go 1.21+. The bounded 20s timeout still gives deterministic completion
// (holomush-gfo6.30).
func drainStream(t *testing.T, stream eventbus.HistoryStream) []eventbus.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var out []eventbus.Event
	for {
		e, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("drainStream: %v", err)
		}
		out = append(out, e)
	}
}

// currentStreamLastSeq reads the EVENTS stream's LastSeq for use as a
// barrier snapshot. Returns 0 on error (not fatal — caller treats it as
// "no offset known yet").
func currentStreamLastSeq(t *testing.T, bus *eventbustest.Embedded) uint64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
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

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package history

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/pgnanos"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	"github.com/holomush/holomush/test/testutil"
)

// newErrorSnapshotForTest builds a snapshot whose Get() returns the given
// error. Used to verify that infra failures propagate rather than being
// silently classified as ErrCursorStale or ErrCursorLag. Integration test only.
func newErrorSnapshotForTest(err error) *StreamStateSnapshot {
	s := &StreamStateSnapshot{err: err}
	s.once.Do(func() {}) // mark as populated (err is the result)
	return s
}

// newIntegrationPool opens a pgxpool against a fresh migrated test database.
// Each test gets its own isolated database so rows from one test cannot
// interfere with another.
func newIntegrationPool() *pgxpool.Pool {
	GinkgoHelper()
	connStr := testutil.FreshDatabase(suiteT, sharedPG)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(pool.Close)
	return pool
}

// integrationULID returns a unique ULID for use in tests. The ms parameter is
// used as the timestamp component to create ordered ULIDs; entropy comes from
// crypto/rand.
func integrationULID(ms uint64) ulid.ULID {
	GinkgoHelper()
	u, err := ulid.New(ms, rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	return u
}

// insertIntegrationAuditRow inserts a minimal events_audit row with the given
// id, subject, seq, and current timestamp.
func insertIntegrationAuditRow(pool *pgxpool.Pool, id ulid.ULID, subject eventbus.Subject, seq uint64) {
	GinkgoHelper()
	insertIntegrationAuditRowAt(pool, id, subject, seq, time.Now().UTC())
}

// insertIntegrationAuditRowAt inserts a minimal events_audit row with an
// explicit timestamp.
func insertIntegrationAuditRowAt(pool *pgxpool.Pool, id ulid.ULID, subject eventbus.Subject, seq uint64, ts time.Time) {
	GinkgoHelper()
	envelopeBytes, err := proto.Marshal(&eventbusv1.Event{
		Id:        id[:],
		Subject:   string(subject),
		Type:      "test.event",
		Timestamp: timestamppb.New(ts),
		Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	})
	Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(context.Background(), `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			envelope, schema_ver, codec, js_seq, rendering
		) VALUES ($1, $2, 'test.event', $4, 'system', NULL, $5, 1, 'identity', $3, '{}'::jsonb)
	`, id[:], string(subject), int64(seq), pgnanos.From(ts), envelopeBytes)
	Expect(err).NotTo(HaveOccurred())
}

// insertIntegrationColdRowWithDEK inserts a minimal events_audit row with
// dek_ref and dek_version set. Used by LookupByID tests to verify that the
// DEK columns round-trip through the Envelope accessors.
func insertIntegrationColdRowWithDEK(pool *pgxpool.Pool, id ulid.ULID, subject, evType string, dekRef int64, dekVersion int32) {
	GinkgoHelper()
	envelopeBytes, err := proto.Marshal(&eventbusv1.Event{
		Id:        id[:],
		Subject:   subject,
		Type:      evType,
		Timestamp: timestamppb.New(time.Now().UTC()),
		Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	})
	Expect(err).NotTo(HaveOccurred())
	_, err = pool.Exec(context.Background(), `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			envelope, schema_ver, codec, js_seq, rendering,
			dek_ref, dek_version
		) VALUES ($1, $2, $3, $7, 'system', NULL, $4, 1, 'xchacha20v1', 1, '{}'::jsonb, $5, $6)
	`, id[:], subject, evType, envelopeBytes, dekRef, dekVersion, pgnanos.From(time.Now()))
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("PostgresColdTier", func() {
	Describe("forward read cursor validation and discard", func() {
		It("validates and discards cursor echo, returning rows after cursor", func() {
			pool := newIntegrationPool()
			ctx := context.Background()

			subject := eventbus.Subject("events.main.location.01HXTESTLOCAAA0000000000A")
			id1 := integrationULID(1000001)
			id2 := integrationULID(1000002)
			id3 := integrationULID(1000003)

			insertIntegrationAuditRow(pool, id1, subject, 1)
			insertIntegrationAuditRow(pool, id2, subject, 2)
			insertIntegrationAuditRow(pool, id3, subject, 3)

			tier := newPostgresColdTier(pool)
			q := eventbus.HistoryQuery{
				Subject:   subject,
				AfterSeq:  1,
				AfterID:   id1,
				Direction: eventbus.DirectionForward,
			}
			out, err := tier.Read(ctx, q, time.Time{}, 10, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(HaveLen(2), "should return events after cursor; cursor row is discarded")
			Expect(out[0].Seq).To(Equal(uint64(2)))
			Expect(out[0].ID).To(Equal(id2))
			Expect(out[1].Seq).To(Equal(uint64(3)))
			Expect(out[1].ID).To(Equal(id3))
		})
	})

	Describe("cursor stale and lag detection", func() {
		It("returns ErrCursorStale when cursor seq matches a row but id does not", func() {
			pool := newIntegrationPool()
			ctx := context.Background()

			subject := eventbus.Subject("events.main.location.01HXTESTLOCBBB0000000000B")
			idActual := integrationULID(2000001)
			idCursor := integrationULID(2000099) // different id for the same seq

			insertIntegrationAuditRow(pool, idActual, subject, 1)

			tier := newPostgresColdTier(pool)
			q := eventbus.HistoryQuery{
				Subject:   subject,
				AfterSeq:  1,
				AfterID:   idCursor, // mismatched id for seq 1
				Direction: eventbus.DirectionForward,
			}
			_, err := tier.Read(ctx, q, time.Time{}, 10, nil)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, eventbus.ErrCursorStale)).To(BeTrue())
		})

		It("returns ErrCursorStale when cursor seq does not exist in events_audit", func() {
			pool := newIntegrationPool()
			ctx := context.Background()

			subject := eventbus.Subject("events.main.location.01HXTESTLOCCCC0000000000C")
			insertIntegrationAuditRow(pool, integrationULID(3000001), subject, 1)

			tier := newPostgresColdTier(pool)
			q := eventbus.HistoryQuery{
				Subject:   subject,
				AfterSeq:  99, // seq 99 does not exist
				AfterID:   integrationULID(3000099),
				Direction: eventbus.DirectionForward,
			}
			_, err := tier.Read(ctx, q, time.Time{}, 10, nil)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, eventbus.ErrCursorStale)).To(BeTrue())
		})

		It("validates and discards cursor echo at edge; filters post-edge rows, returning empty page", func() {
			pool := newIntegrationPool()
			ctx := context.Background()

			subject := eventbus.Subject("events.main.location.01HXTESTLOCDDD0000000000D")
			idAtEdge := integrationULID(4000005)
			idAfterEdge := integrationULID(4000006)

			now := time.Now().UTC()
			insertIntegrationAuditRowAt(pool, idAtEdge, subject, 5, now.Add(-1*time.Hour))
			insertIntegrationAuditRowAt(pool, idAfterEdge, subject, 6, now)

			tier := newPostgresColdTier(pool)
			q := eventbus.HistoryQuery{
				Subject:   subject,
				AfterSeq:  5,
				AfterID:   idAtEdge,
				Direction: eventbus.DirectionForward,
			}
			edge := now.Add(-30 * time.Minute)
			out, err := tier.Read(ctx, q, edge, 10, nil)
			Expect(err).NotTo(HaveOccurred())
			// Cursor echo is validated and discarded; idAfterEdge is filtered by edge.
			Expect(out).To(BeEmpty())
		})

		It("returns ErrCursorLag when cursor seq is within JS range but missing from cold", func() {
			pool := newIntegrationPool()
			ctx := context.Background()

			subject := eventbus.Subject("events.main.location.01HXTESTLOCEEE0000000000E")
			// Insert seq=1 only; cursor will point to seq=50 which is absent.
			insertIntegrationAuditRow(pool, integrationULID(5000001), subject, 1)

			tier := newPostgresColdTier(pool)
			q := eventbus.HistoryQuery{
				Subject:   subject,
				AfterSeq:  50,
				AfterID:   integrationULID(5000050),
				Direction: eventbus.DirectionForward,
				PageSize:  10,
			}
			// Snapshot: JS contains seqs 1..100; cursor.Seq=50 is within [1,100] → LAG.
			snap := newSnapshotForTest(1, 100)
			_, err := tier.Read(ctx, q, time.Time{}, q.PageSize, snap)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, eventbus.ErrCursorLag)).To(BeTrue(),
				"cursor seq within JS range must be LAG, not STALE")
		})

		It("returns ErrCursorStale when cursor seq exceeds JS snapshot lastSeq", func() {
			pool := newIntegrationPool()
			ctx := context.Background()

			subject := eventbus.Subject("events.main.location.01HXTESTLOCFFF0000000000F")
			// Insert seq=1 only; cursor will point to seq=200 which exceeds JS.
			insertIntegrationAuditRow(pool, integrationULID(6000001), subject, 1)

			tier := newPostgresColdTier(pool)
			q := eventbus.HistoryQuery{
				Subject:   subject,
				AfterSeq:  200,
				AfterID:   integrationULID(6000200),
				Direction: eventbus.DirectionForward,
				PageSize:  10,
			}
			// Snapshot: JS only has seqs 1..100; cursor.Seq=200 > lastSeq=100 → STALE.
			snap := newSnapshotForTest(1, 100)
			_, err := tier.Read(ctx, q, time.Time{}, q.PageSize, snap)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, eventbus.ErrCursorStale)).To(BeTrue(),
				"cursor seq beyond JS range must be STALE")
		})

		It("returns ErrCursorStale when cursor seq is below JS snapshot firstSeq (retention aged out)", func() {
			pool := newIntegrationPool()
			ctx := context.Background()

			// Snapshot: JS has seq=10..100; cursor points to seq=5 which is below firstSeq.
			subject := eventbus.Subject("events.main.location.01HXTESTLOCHHH0000000000H")
			// No cold row for the cursor seq (absent from cold).
			insertIntegrationAuditRow(pool, integrationULID(8000001), subject, 99)

			tier := newPostgresColdTier(pool)
			q := eventbus.HistoryQuery{
				Subject:   subject,
				AfterSeq:  5,
				AfterID:   integrationULID(8000005),
				Direction: eventbus.DirectionForward,
				PageSize:  10,
			}
			// Snapshot: firstSeq=10 > cursorSeq=5 → not in LAG range → STALE.
			snap := newSnapshotForTest(10, 100)
			_, err := tier.Read(ctx, q, time.Time{}, q.PageSize, snap)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, eventbus.ErrCursorStale)).To(BeTrue(),
				"cursor seq below JS firstSeq must be STALE (retention aged it out)")
		})

		It("propagates snapshot error unchanged, not classified as cursor error", func() {
			pool := newIntegrationPool()
			ctx := context.Background()

			// Insert one row so the query reaches the cursor-validation branch.
			subject := eventbus.Subject("events.main.location.01HXTESTLOCIII0000000000I")
			insertIntegrationAuditRow(pool, integrationULID(9000001), subject, 1)

			tier := newPostgresColdTier(pool)
			q := eventbus.HistoryQuery{
				Subject:   subject,
				AfterSeq:  50,
				AfterID:   integrationULID(9000050),
				Direction: eventbus.DirectionForward,
				PageSize:  10,
			}
			snapErr := oops.Code("EVENTBUS_HISTORY_STREAM_LOOKUP_FAILED").Errorf("js unreachable")
			snap := newErrorSnapshotForTest(snapErr)
			_, err := tier.Read(ctx, q, time.Time{}, q.PageSize, snap)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, eventbus.ErrCursorStale)).To(BeFalse(),
				"snapshot error must not be classified as ErrCursorStale")
			Expect(errors.Is(err, eventbus.ErrCursorLag)).To(BeFalse(),
				"snapshot error must not be classified as ErrCursorLag")
			Expect(errors.Is(err, snapErr)).To(BeTrue())
		})

		It("returns ErrCursorStale when no snapshot and cursor seq is missing", func() {
			pool := newIntegrationPool()
			ctx := context.Background()

			subject := eventbus.Subject("events.main.location.01HXTESTLOCGGG0000000000G")
			insertIntegrationAuditRow(pool, integrationULID(7000001), subject, 1)

			tier := newPostgresColdTier(pool)
			q := eventbus.HistoryQuery{
				Subject:   subject,
				AfterSeq:  50,
				AfterID:   integrationULID(7000050),
				Direction: eventbus.DirectionForward,
				PageSize:  10,
			}
			// No snapshot — falls back to STALE.
			_, err := tier.Read(ctx, q, time.Time{}, q.PageSize, nil)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, eventbus.ErrCursorStale)).To(BeTrue(),
				"without snapshot, missing cursor must be STALE")
		})
	})

	Describe("LookupByID", func() {
		It("returns Envelope with correct EventID, KeyID, and KeyVersion from cold tier row", func() {
			pool := newIntegrationPool()

			eventID := integrationULID(10000001)
			insertIntegrationColdRowWithDEK(pool, eventID, "events.g1.scene.A.ic", "scene.event", 42, 7)

			tier := newPostgresColdTier(pool)
			env, found, err := tier.LookupByID(context.Background(), eventID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(env.EventID()).To(Equal(eventID))
			Expect(env.KeyID()).To(Equal(codec.KeyID(42)))
			Expect(env.KeyVersion()).To(Equal(uint32(7)))
		})

		It("returns (zero, false, nil) when no row exists for the given event ID", func() {
			pool := newIntegrationPool()

			tier := newPostgresColdTier(pool)
			nonExistent := integrationULID(99999999)
			_, found, err := tier.LookupByID(context.Background(), nonExistent)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())
		})
	})

	// holomush-iu8j: cross-tier boundary consistency. Hot tier
	// (matchesQuery via ev.Timestamp.After(NotAfter)) treats the boundary
	// as INCLUSIVE — events with timestamp == NotAfter are returned. Cold
	// tier MUST match (uses SQL `timestamp <= $N` at cold_postgres.go).
	// This Describe pins the cold-tier semantics so a future SQL refactor
	// can't accidentally make NotAfter exclusive without surfacing as a
	// test failure (would manifest in production as a perceptible
	// "missing event" UX on the connect-time backfill path).
	Describe("NotAfter boundary inclusivity (cross-tier consistency, iu8j)", func() {
		It("includes event whose timestamp equals NotAfter (inclusive boundary)", func() {
			pool := newIntegrationPool()
			ctx := context.Background()
			subject := eventbus.Subject("events.main.location.01HXIU8JBNDRY00000000000A")

			// Insert two events: one AT the boundary timestamp (must
			// be included) and one strictly after (must be excluded).
			// INV-STORE-1: events_audit is BIGINT-ns end-to-end, so no µs
			// truncation needed — the boundary value round-trips bit-exact.
			boundary := time.Now().UTC()
			atID := integrationULID(uint64(boundary.UnixMilli()))
			afterID := integrationULID(uint64(boundary.Add(1 * time.Second).UnixMilli()))
			insertIntegrationAuditRowAt(pool, atID, subject, 1, boundary)
			insertIntegrationAuditRowAt(pool, afterID, subject, 2, boundary.Add(1*time.Second))

			tier := newPostgresColdTier(pool)
			events, err := tier.Read(
				ctx,
				eventbus.HistoryQuery{
					Subject:   subject,
					Direction: eventbus.DirectionForward,
					NotAfter:  boundary,
				},
				time.Time{}, // edge unused for cold reads
				10,
				nil,
			)
			Expect(err).NotTo(HaveOccurred())
			gotIDs := make([]string, 0, len(events))
			for _, ev := range events {
				gotIDs = append(gotIDs, ev.ID.String())
			}
			Expect(gotIDs).To(ContainElement(atID.String()),
				"event with timestamp == NotAfter MUST be included (inclusive boundary — iu8j); "+
					"if missing, the cold tier's `timestamp <= $N` clause has drifted to `<` and "+
					"will silently drop events at the connect-time attach moment")
			Expect(gotIDs).NotTo(ContainElement(afterID.String()),
				"event with timestamp > NotAfter MUST be excluded")
		})
	})
})

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package readstream

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	"github.com/holomush/holomush/test/testutil"
)

// newColdReaderTestPool opens a pgxpool against a fresh migrated test database.
func newColdReaderTestPool() *pgxpool.Pool {
	GinkgoHelper()
	connStr := testutil.FreshDatabase(suiteT, sharedPG)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(pool.Close)
	return pool
}

// testULIDAt returns a ULID with the given millisecond timestamp component.
func testULIDAt(ms uint64) ulid.ULID {
	GinkgoHelper()
	u, err := ulid.New(ms, rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	return u
}

// insertEncryptedAuditRow inserts an events_audit row with a non-NULL dek_ref
// (simulating an encrypted/sensitive event).
func insertEncryptedAuditRow(
	pool *pgxpool.Pool,
	id ulid.ULID,
	subject eventbus.Subject,
	seq uint64,
	ts time.Time,
) {
	GinkgoHelper()
	envelopeBytes, err := proto.Marshal(&eventbusv1.Event{
		Id:        id[:],
		Subject:   string(subject),
		Type:      "test.encrypted",
		Timestamp: timestamppb.New(ts),
		Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	})
	Expect(err).NotTo(HaveOccurred())

	// timestamp is BIGINT-ns post-gfo6 (INV-STORE-1).
	_, err = pool.Exec(context.Background(), `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			envelope, schema_ver, codec, js_seq, rendering,
			dek_ref, dek_version
		) VALUES ($1, $2, 'test.encrypted', $3, 'system', NULL,
		          $4, 1, 'aes256-gcm', $5, '{}',
		          42, 1)
	`, id[:], string(subject), pgnanos.From(ts), envelopeBytes, int64(seq))
	Expect(err).NotTo(HaveOccurred())
}

// insertIdentityAuditRow inserts an events_audit row with NULL dek_ref
// (identity-codec, cleartext event — should be excluded by the filter).
func insertIdentityAuditRow(
	pool *pgxpool.Pool,
	id ulid.ULID,
	subject eventbus.Subject,
	seq uint64,
	ts time.Time,
) {
	GinkgoHelper()
	envelopeBytes, err := proto.Marshal(&eventbusv1.Event{
		Id:        id[:],
		Subject:   string(subject),
		Type:      "test.cleartext",
		Timestamp: timestamppb.New(ts),
		Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	})
	Expect(err).NotTo(HaveOccurred())

	// timestamp is BIGINT-ns post-gfo6 (INV-STORE-1).
	_, err = pool.Exec(context.Background(), `
		INSERT INTO events_audit (
			id, subject, type, timestamp, actor_kind, actor_id,
			envelope, schema_ver, codec, js_seq, rendering
		) VALUES ($1, $2, 'test.cleartext', $3, 'system', NULL,
		          $4, 1, 'identity', $5, '{}')
	`, id[:], string(subject), pgnanos.From(ts), envelopeBytes, int64(seq))
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("ColdReader", func() {
	Describe("MultiSubjectUnion", func() {
		It("unions two subjects and returns all rows from both", func() {
			pool := newColdReaderTestPool()
			now := time.Now().UTC()

			subjectA := eventbus.Subject("events.game.scene.AAAAAAA.>")
			subjectB := eventbus.Subject("events.game.scene.BBBBBBB.>")

			for i := range 3 {
				insertEncryptedAuditRow(pool, testULIDAt(uint64(1000+i)), subjectA, uint64(1+i), now.Add(time.Duration(i)*time.Millisecond))
			}
			for i := range 3 {
				insertEncryptedAuditRow(pool, testULIDAt(uint64(2000+i)), subjectB, uint64(4+i), now.Add(time.Duration(3+i)*time.Millisecond))
			}

			cr := NewColdReader(pool)
			rows, err := cr.Read(context.Background(), ColdQuery{
				Subjects: []eventbus.Subject{subjectA, subjectB},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(6), "both subjects combined should yield 6 rows")
		})
	})

	Describe("TimeBoundsApplied", func() {
		It("excludes rows outside the Since/Until window", func() {
			pool := newColdReaderTestPool()

			base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
			subject := eventbus.Subject("events.game.scene.TIMEBND.>")

			// Before window
			insertEncryptedAuditRow(pool, testULIDAt(1001), subject, 1, base.Add(-2*time.Hour))
			// Inside window
			insertEncryptedAuditRow(pool, testULIDAt(1002), subject, 2, base.Add(1*time.Hour))
			insertEncryptedAuditRow(pool, testULIDAt(1003), subject, 3, base.Add(2*time.Hour))
			// After window
			insertEncryptedAuditRow(pool, testULIDAt(1004), subject, 4, base.Add(5*time.Hour))

			cr := NewColdReader(pool)
			rows, err := cr.Read(context.Background(), ColdQuery{
				Subjects: []eventbus.Subject{subject},
				Since:    base,
				Until:    base.Add(3 * time.Hour),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(2), "only rows within [Since, Until] are returned")
		})
	})

	Describe("FiltersByDekRefNotNull", func() {
		It("excludes identity-codec rows with NULL dek_ref", func() {
			pool := newColdReaderTestPool()
			now := time.Now().UTC()

			subject := eventbus.Subject("events.game.scene.DEKFILT.>")

			// Encrypted row (dek_ref NOT NULL) — should be returned
			insertEncryptedAuditRow(pool, testULIDAt(3001), subject, 1, now)
			// Cleartext row (dek_ref NULL, codec=identity) — must NOT be returned
			insertIdentityAuditRow(pool, testULIDAt(3002), subject, 2, now.Add(time.Millisecond))

			cr := NewColdReader(pool)
			rows, err := cr.Read(context.Background(), ColdQuery{
				Subjects: []eventbus.Subject{subject},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1), "only the encrypted row is returned")
			Expect(string(rows[0].Codec)).To(Equal("aes256-gcm"))
		})
	})

	Describe("OrderByTimestampAsc", func() {
		It("returns rows in monotonically ascending timestamp order regardless of insertion order", func() {
			pool := newColdReaderTestPool()

			base := time.Now().UTC().Truncate(time.Millisecond)
			subject := eventbus.Subject("events.game.scene.ORDERBY.>")

			// Insert in reverse time order
			insertEncryptedAuditRow(pool, testULIDAt(5003), subject, 3, base.Add(3*time.Second))
			insertEncryptedAuditRow(pool, testULIDAt(5001), subject, 1, base.Add(1*time.Second))
			insertEncryptedAuditRow(pool, testULIDAt(5002), subject, 2, base.Add(2*time.Second))

			cr := NewColdReader(pool)
			rows, err := cr.Read(context.Background(), ColdQuery{
				Subjects: []eventbus.Subject{subject},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(3))

			// Verify monotonically ascending timestamps
			for i := 1; i < len(rows); i++ {
				Expect(rows[i].Timestamp.Before(rows[i-1].Timestamp)).To(BeFalse(),
					"rows[%d].Timestamp=%v must be >= rows[%d].Timestamp=%v",
					i, rows[i].Timestamp, i-1, rows[i-1].Timestamp)
			}
			// Verify ascending JsSeq as tiebreaker
			Expect(rows[0].JsSeq).To(Equal(uint64(1)))
			Expect(rows[1].JsSeq).To(Equal(uint64(2)))
			Expect(rows[2].JsSeq).To(Equal(uint64(3)))
		})
	})

	Describe("EmptyResultEofTermination", func() {
		It("returns a nil slice with nil error when no rows match", func() {
			pool := newColdReaderTestPool()

			cr := NewColdReader(pool)
			rows, err := cr.Read(context.Background(), ColdQuery{
				Subjects: []eventbus.Subject{"events.game.scene.EMPTY.>"},
				Since:    time.Now().Add(-time.Hour),
				Until:    time.Now(),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(BeNil(), "empty result must be nil slice, not an error")
		})
	})

	Describe("DEKVersionNullWithDEKRefPresent", func() {
		It("returns ADMIN_READSTREAM_COLD_DEK_VERSION_NULL for row with dek_ref NOT NULL but dek_version NULL (INV-49)", func() {
			pool := newColdReaderTestPool()
			now := time.Now().UTC()
			subject := eventbus.Subject("events.game.scene.DEKVNULL.>")
			id := testULIDAt(9001)

			envelopeBytes, err := proto.Marshal(&eventbusv1.Event{
				Id:        id[:],
				Subject:   string(subject),
				Type:      "test.encrypted",
				Timestamp: timestamppb.New(now),
				Actor:     &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
			})
			Expect(err).NotTo(HaveOccurred())

			// Insert a row with dek_ref set but dek_version explicitly NULL.
			// This violates INV-49 and must be detected by the scanner.
			_, err = pool.Exec(context.Background(), `
				INSERT INTO events_audit (
					id, subject, type, timestamp, actor_kind, actor_id,
					envelope, schema_ver, codec, js_seq, rendering,
					dek_ref, dek_version
				) VALUES ($1, $2, 'test.encrypted', $3, 'system', NULL,
				          $4, 1, 'xchacha20poly1305-v1', $5, '{}',
				          42, NULL)
			`, id[:], string(subject), pgnanos.From(now), envelopeBytes, int64(9001))
			Expect(err).NotTo(HaveOccurred())

			cr := NewColdReader(pool)
			_, readErr := cr.Read(context.Background(), ColdQuery{
				Subjects: []eventbus.Subject{subject},
			})
			Expect(readErr).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, readErr, "ADMIN_READSTREAM_COLD_DEK_VERSION_NULL")
		})
	})
})

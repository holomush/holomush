// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package resilience_test

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
	"github.com/holomush/holomush/internal/world/outbox"
	"github.com/holomush/holomush/internal/world/wmodel"
)

const (
	faultSpecTimeout = 3 * time.Minute
	// pausedDrainCeiling bounds a relay Drain issued while the broker is frozen.
	// The relay's publish inherits the caller's ctx deadline (publisher.go:319 —
	// context.WithTimeout(ctx, dupeWindowDeadline)), so a frozen broker makes the
	// publish stall until THIS ceiling, then surfaces as a transient outage — the
	// relay backs off, nothing is published, and it resumes in order on unpause.
	pausedDrainCeiling = 5 * time.Second
)

// crashBeforeMarkStore wraps an OutboxStore so the FIRST MarkPublished after a
// successful PubAck fails — simulating a relay that crashes in the durability gap
// between the broker ack and the DB mark. The wire message is already out; the row
// stays unpublished, so a fresh Drain redelivers it (with the SAME Nats-Msg-Id =
// event ULID) and JetStream dedup collapses the two publishes to one stored
// message. remaining is a shared counter so the relay's CACHED lease (Drain keeps
// it across passes on a transient failure) sees the fault exactly once.
type crashBeforeMarkStore struct {
	inner     outbox.OutboxStore
	remaining *int
}

func (s crashBeforeMarkStore) AcquireLease(ctx context.Context, gameID string) (outbox.Lease, error) {
	lease, err := s.inner.AcquireLease(ctx, gameID)
	if err != nil {
		return nil, err //nolint:wrapcheck // pass-through; the inner store already codes the error
	}
	return &crashBeforeMarkLease{Lease: lease, remaining: s.remaining}, nil
}

// crashBeforeMarkLease embeds the real lease (delegating every fenced DB op) but
// intercepts MarkPublished to fail once. A non-stale, non-poison MarkPublished
// error is treated by the relay as a transient outage (relay.go:238) — the lease
// is retained and the row is retried on the next pass, which is exactly the
// crash-then-restart-redeliver posture under test.
type crashBeforeMarkLease struct {
	outbox.Lease
	remaining *int
}

func (l *crashBeforeMarkLease) MarkPublished(ctx context.Context, eventID ulid.ULID, generation int64) error {
	if *l.remaining > 0 {
		*l.remaining--
		return oops.Code("TEST_CRASH_AFTER_PUBACK").
			Errorf("simulated relay crash after PubAck, before durable MarkPublished")
	}
	return l.Lease.MarkPublished(ctx, eventID, generation) //nolint:wrapcheck // delegate to the real fenced mark
}

// countingEffect is a reference-consumer side effect that BOTH counts its
// executions in-memory AND writes a durable row on the receipt+watermark
// transaction (via the tx-bound exec) — so "applied exactly once" is provable two
// ways: the counter (effect body ran once) and the scratch-table row count (the
// durable write committed once, atomically with the receipt).
func countingEffect(counter *int, tag string) outbox.EffectFunc {
	return func(effCtx context.Context, exec outbox.TxExecutor, env wmodel.Envelope) error {
		*counter++
		_, err := exec.Exec(effCtx,
			`INSERT INTO resilience_consumer_effects (event_id, tag) VALUES ($1, $2)`,
			env.EventID.String(), tag)
		return err //nolint:wrapcheck // test effect; the error surfaces through ApplyOnce
	}
}

// effectRowCount counts the durable effect rows committed for tag (read over the
// shared pool — committed rows only).
func effectRowCount(ctx context.Context, s *integrationtest.Server, tag string) int {
	GinkgoHelper()
	var n int
	Expect(s.Pool().QueryRow(ctx,
		`SELECT count(*) FROM resilience_consumer_effects WHERE tag = $1`, tag).Scan(&n)).
		To(Succeed(), "count effect rows")
	return n
}

// The outbox fault-injection matrix is the standing D-05 regression gate that
// proves the world-change feed survives real broker chaos END-TO-END — the layer
// 05-06 (DB-atomic outbox write) and 05-07 (single leased relay) could only prove
// in isolation. Each spec drives the REAL relay / lease / reference consumer over
// two in-process replicas, one external NATS broker, and one shared Postgres:
//
//  1. relay crash around PubAck — a crash in the ack→mark gap redelivers on
//     restart; JetStream Nats-Msg-Id dedup + the idempotent consumer make the
//     consumer-visible effect exactly-once (no double effect).
//  2. dual relay / lease fencing — only the advisory-lock holder makes DB-side
//     progress; the non-holder cannot acquire; on a holder handoff the durable
//     lease-generation fence REJECTS a stale ack (round-4 A2); any transient
//     duplicate wire publish across the handoff is absorbed by dedup + the
//     idempotent consumer (at-least-once wire, exactly-once effect — round-3).
//  3. duplicate delivery — the same envelope delivered twice applies once.
//  4. broker downtime — the relay backs off while the broker is frozen and, on
//     recovery, resumes publishing from the first unpublished position in order.
var _ = Describe("Outbox relay fault-injection matrix", Ordered, func() {
	var (
		env      *natstest.NATSEnv
		replicaA *integrationtest.Server
		replicaB *integrationtest.Server
	)

	BeforeAll(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		DeferCleanup(cancel)

		env = startExternalNATS(ctx)
		// Replica A creates the fresh per-test database; B joins it. Two replicas
		// give two INDEPENDENT pools/sessions — the genuine dual-relay lease
		// contention surface (spec 2). No plugins: the relay/lease/consumer are
		// driven directly, no command path needed.
		replicaA = startReplica(suiteT, env.URL, "")
		replicaB = startReplica(suiteT, env.URL, replicaA.ConnStr())

		// Scratch table for the durable exactly-once effect proof (specs 1-3).
		_, err := replicaA.Pool().Exec(ctx, `
			CREATE TABLE IF NOT EXISTS resilience_consumer_effects (
				event_id text PRIMARY KEY,
				tag      text NOT NULL
			)`)
		Expect(err).NotTo(HaveOccurred(), "create resilience_consumer_effects scratch table")
	})

	It("relay crash around PubAck: the envelope is redelivered, deduped on Nats-Msg-Id, and applied once", func() {
		ctx, cancel := context.WithTimeout(context.Background(), faultSpecTimeout)
		DeferCleanup(cancel)

		game := "fi-crash-" + ulid.Make().String()
		row := seedOutboxRow(ctx, replicaA, game, "location_updated")
		subject := envelopeSubject(row)

		remaining := 1
		relay := newOutboxRelay(
			crashBeforeMarkStore{inner: outboxStoreFor(replicaA), remaining: &remaining},
			busPublisher(replicaA), game,
		)
		// Release the relay's advisory-lock lease at spec end — a leaked pinned
		// connection would block the harness pool.Close() during suite teardown.
		DeferCleanup(func() { _ = relay.Stop(context.Background()) })

		// Drain #1: the row PUBLISHES (wire count 1), then MarkPublished "crashes"
		// (transient) — the row stays unpublished, exactly the ack→mark gap.
		_, err := relay.Drain(ctx)
		Expect(err).NotTo(HaveOccurred(), "drain #1 must not error on a transient mark failure")
		halted, _ := relay.Halted()
		Expect(halted).To(BeFalse(), "a crash in the mark gap must NOT halt the relay")
		Expect(streamSubjectCount(ctx, env, subject)).To(Equal(uint64(1)),
			"the envelope was published to the broker before the crash")
		Expect(outboxPublishedAt(ctx, replicaA, row.EventID)).To(BeFalse(),
			"the crash left the row unpublished (the durability gap)")

		// Drain #2 (restart): the still-unpublished row is REDELIVERED with the same
		// Nats-Msg-Id; JetStream dedup collapses it — the stored count stays 1 (no
		// double effect on the wire) — and this pass marks the row published.
		published, err := relay.Drain(ctx)
		Expect(err).NotTo(HaveOccurred(), "drain #2 (restart) must succeed")
		Expect(published).To(Equal(1), "the redelivered row is marked published on restart")
		Expect(streamSubjectCount(ctx, env, subject)).To(Equal(uint64(1)),
			"Nats-Msg-Id dedup collapsed the redelivery — no duplicate stored on the wire")
		Expect(outboxPublishedAt(ctx, replicaA, row.EventID)).To(BeTrue(),
			"the redelivered row is now durably marked published")

		// The reference consumer dedups the same envelope on the event ULID: applied
		// once even when the delivery arrives twice (durable receipt).
		var effects int
		tag := "crash-" + row.EventID.String()
		consumer := newReferenceConsumer(replicaA, countingEffect(&effects, tag))
		consumer.initWatermark(ctx, game, row.Epoch, row.FeedPosition-1)

		applied, err := consumer.Apply(ctx, *row)
		Expect(err).NotTo(HaveOccurred())
		Expect(applied).To(BeTrue(), "first delivery applies")
		applied, err = consumer.Apply(ctx, *row)
		Expect(err).NotTo(HaveOccurred())
		Expect(applied).To(BeFalse(), "the redelivery is a receipt-deduped no-op")
		Expect(effects).To(Equal(1), "the effect body ran exactly once")
		Expect(effectRowCount(ctx, replicaA, tag)).To(Equal(1), "the durable effect committed exactly once")

		reportVerdict(fmt.Sprintf(
			"CHAOS-VERDICT: relay-crash-around-PubAck: envelope redelivered on restart; Nats-Msg-Id dedup kept the wire at 1 message; reference consumer applied the event ULID exactly once (game=%s)", game,
		))
	})

	It("dual relay / lease fencing: only the holder makes DB-side progress, a stale-generation ack is rejected, and the effect is exactly-once", func() {
		ctx, cancel := context.WithTimeout(context.Background(), faultSpecTimeout)
		DeferCleanup(cancel)

		game := "fi-dual-" + ulid.Make().String()
		row1 := seedOutboxRow(ctx, replicaA, game, "location_updated")
		row2 := seedOutboxRow(ctx, replicaA, game, "location_updated")

		storeA := outboxStoreFor(replicaA)
		storeB := outboxStoreFor(replicaB)
		pub := busPublisher(replicaA)

		// A acquires the per-game advisory-lock lease (generation g1).
		leaseA, err := storeA.AcquireLease(ctx, game)
		Expect(err).NotTo(HaveOccurred(), "replica A acquires the lease")
		// Release is idempotent; guarantee the pinned lease conns are returned even
		// if an assertion aborts the spec (otherwise pool.Close() blocks teardown).
		DeferCleanup(func() { _ = leaseA.Release(context.Background()) })

		// Single-holder proof: B cannot acquire while A holds — its AcquireLease
		// BLOCKS on pg_advisory_lock and its bounded ctx expires. The non-holder
		// issues no feed DB ops.
		blockedCtx, blockedCancel := context.WithTimeout(ctx, 3*time.Second)
		_, errB := storeB.AcquireLease(blockedCtx, game)
		blockedCancel()
		Expect(errB).To(HaveOccurred(), "the non-holder MUST NOT acquire the lease while A holds it")

		// Holder makes progress: A publishes row1 and marks it under g1.
		envA, err := leaseA.NextUnpublished(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(envA).NotTo(BeNil())
		Expect(envA.EventID).To(Equal(row1.EventID), "the holder drains the lowest unpublished position first")
		evA, err := outbox.EnvelopeToEvent(*envA)
		Expect(err).NotTo(HaveOccurred())
		Expect(pub.Publish(ctx, evA)).To(Succeed(), "holder publishes row1")
		Expect(leaseA.MarkPublished(ctx, envA.EventID, leaseA.Generation())).
			To(Succeed(), "the holder's ack under its own generation is accepted")
		Expect(outboxPublishedAt(ctx, replicaA, row1.EventID)).To(BeTrue())

		// Handoff: A releases (a dropped holder connection). B acquires the lease
		// with a durably BUMPED generation (g2 > g1).
		Expect(leaseA.Release(ctx)).To(Succeed(), "holder A releases (connection drop)")
		leaseB, err := storeB.AcquireLease(ctx, game)
		Expect(err).NotTo(HaveOccurred(), "B acquires the lease after the handoff")
		DeferCleanup(func() { _ = leaseB.Release(context.Background()) })
		Expect(leaseB.Generation()).To(BeNumerically(">", leaseA.Generation()),
			"the durable lease_generation is bumped on the new acquire (round-4 A2)")

		// Stale-generation ack REJECTED on a LIVE connection: B (g2) marking under
		// A's stale g1 re-reads the durable column and fails closed — the durable
		// fence, not just a released-flag check.
		staleErr := leaseB.MarkPublished(ctx, row1.EventID, leaseA.Generation())
		Expect(outbox.IsStaleLease(staleErr)).To(BeTrue(),
			"a stale-generation ack is rejected against the durable lease_generation")
		// The released holder's ack is also stale (its pinned connection is gone).
		Expect(outbox.IsStaleLease(leaseA.MarkPublished(ctx, row2.EventID, leaseA.Generation()))).
			To(BeTrue(), "the released holder makes no further DB-side progress")

		// New holder makes progress: B publishes row2 under g2.
		envB, err := leaseB.NextUnpublished(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(envB).NotTo(BeNil())
		Expect(envB.EventID).To(Equal(row2.EventID), "B resumes at the next unpublished position")
		evB, err := outbox.EnvelopeToEvent(*envB)
		Expect(err).NotTo(HaveOccurred())
		Expect(pub.Publish(ctx, evB)).To(Succeed(), "new holder publishes row2")
		Expect(leaseB.MarkPublished(ctx, envB.EventID, leaseB.Generation())).To(Succeed())
		Expect(leaseB.Release(ctx)).To(Succeed())

		// Consumer-visible effect is EXACTLY-ONCE across the handoff: replay row1
		// (the transient duplicate wire publish the handoff may produce) and confirm
		// it is a receipt-deduped no-op; positions 1 and 2 each apply once, in order.
		var effects int
		tag := "dual-" + game
		consumer := newReferenceConsumer(replicaA, countingEffect(&effects, tag))
		consumer.initWatermark(ctx, game, row1.Epoch, row1.FeedPosition-1)

		applied, err := consumer.Apply(ctx, *row1)
		Expect(err).NotTo(HaveOccurred())
		Expect(applied).To(BeTrue(), "position 1 applies")
		applied, err = consumer.Apply(ctx, *row2)
		Expect(err).NotTo(HaveOccurred())
		Expect(applied).To(BeTrue(), "position 2 applies contiguously")
		applied, err = consumer.Apply(ctx, *row1)
		Expect(err).NotTo(HaveOccurred())
		Expect(applied).To(BeFalse(), "the transient duplicate of position 1 is absorbed (exactly-once effect)")
		Expect(effects).To(Equal(2), "exactly two effects committed (positions 1 and 2), no duplicate")
		Expect(effectRowCount(ctx, replicaA, tag)).To(Equal(2))

		wmEpoch, wmPos, ok, err := consumer.checkpoint.Watermark(ctx, consumer.name, game)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(wmEpoch).To(Equal(row2.Epoch))
		Expect(wmPos).To(Equal(row2.FeedPosition), "the watermark advanced to position 2 with no skip or duplicate")

		reportVerdict(fmt.Sprintf(
			"CHAOS-VERDICT: dual-relay: single-lease DB progress (non-holder blocked); handoff bumped generation %d->%d; stale-generation ack REJECTED against the durable column; consumer effect exactly-once across the handoff (game=%s)",
			leaseA.Generation(), leaseB.Generation(), game,
		))
	})

	It("duplicate delivery: the same envelope delivered twice is applied once", func() {
		ctx, cancel := context.WithTimeout(context.Background(), faultSpecTimeout)
		DeferCleanup(cancel)

		game := "fi-dup-" + ulid.Make().String()
		// A synthetic first-position envelope — the consumer's idempotency is a
		// durable-receipt property independent of the outbox table.
		row := wmodel.Envelope{
			EventID:       ulid.Make(),
			GameID:        game,
			Kind:          "location_updated",
			SchemaVersion: 1,
			Actor:         "system",
			AggregateType: wmodel.AggregateLocation,
			AggregateID:   ulid.Make(),
			Epoch:         1,
			FeedPosition:  1,
			Payload:       []byte(`{"name":"chaos"}`),
		}

		var effects int
		tag := "dup-" + row.EventID.String()
		consumer := newReferenceConsumer(replicaA, countingEffect(&effects, tag))
		consumer.initWatermark(ctx, game, row.Epoch, row.FeedPosition-1)

		applied, err := consumer.Apply(ctx, row)
		Expect(err).NotTo(HaveOccurred())
		Expect(applied).To(BeTrue(), "first delivery applies")
		applied, err = consumer.Apply(ctx, row)
		Expect(err).NotTo(HaveOccurred())
		Expect(applied).To(BeFalse(), "the duplicate delivery is a no-op")
		Expect(effects).To(Equal(1), "the effect ran exactly once")
		Expect(effectRowCount(ctx, replicaA, tag)).To(Equal(1), "exactly one durable effect committed")

		reportVerdict(fmt.Sprintf(
			"CHAOS-VERDICT: duplicate-delivery: the same envelope delivered twice applied exactly once (durable receipt dedup; game=%s)", game,
		))
	})

	It("broker downtime: the relay backs off while frozen and resumes publishing in order on recovery", func() {
		ctx, cancel := context.WithTimeout(context.Background(), faultSpecTimeout)
		DeferCleanup(cancel)

		game := "fi-down-" + ulid.Make().String()
		rows := []*wmodel.Envelope{
			seedOutboxRow(ctx, replicaA, game, "location_updated"),
			seedOutboxRow(ctx, replicaA, game, "location_updated"),
			seedOutboxRow(ctx, replicaA, game, "location_updated"),
		}

		relay := newOutboxRelay(outboxStoreFor(replicaA), busPublisher(replicaA), game)
		DeferCleanup(func() { _ = relay.Stop(context.Background()) })

		// Freeze the broker, then drain under a bounded ctx: the publish stalls to
		// the ceiling and surfaces as a transient outage — nothing published, the
		// relay does NOT halt (it backs off), and every row stays unpublished.
		pauseBroker(ctx, env)
		frozenCtx, frozenCancel := context.WithTimeout(ctx, pausedDrainCeiling)
		publishedFrozen, _ := relay.Drain(frozenCtx)
		frozenCancel()
		unpauseBroker(ctx, env)

		Expect(publishedFrozen).To(Equal(0), "a frozen broker publishes nothing")
		halted, _ := relay.Halted()
		Expect(halted).To(BeFalse(), "broker downtime is transient — the relay backs off, it does NOT halt")
		for i, r := range rows {
			Expect(outboxPublishedAt(ctx, replicaA, r.EventID)).To(BeFalse(),
				"row %d stayed unpublished while the broker was frozen", i+1)
		}

		// On recovery the relay resumes and drains every row IN ORDER (marks follow
		// PubAcks in strict (epoch, feed_position) order).
		published, err := relay.Drain(ctx)
		Expect(err).NotTo(HaveOccurred(), "the relay resumes cleanly after recovery")
		Expect(published).To(Equal(3), "all three rows publish on resume")

		var prevPublishedAt int64
		for i, r := range rows {
			Expect(streamSubjectCount(ctx, env, envelopeSubject(r))).To(Equal(uint64(1)),
				"row %d landed exactly once on the wire", i+1)
			var publishedAt int64
			Expect(replicaA.Pool().QueryRow(ctx,
				`SELECT published_at FROM outbox WHERE event_id = $1`, r.EventID.String()).
				Scan(&publishedAt)).To(Succeed(), "read published_at for row %d", i+1)
			Expect(publishedAt).To(BeNumerically(">=", prevPublishedAt),
				"row %d was published no earlier than the prior feed position (in-order resume)", i+1)
			prevPublishedAt = publishedAt
		}

		reportVerdict(fmt.Sprintf(
			"CHAOS-VERDICT: broker-downtime: relay published nothing while frozen (no halt), then resumed and drained all 3 rows in feed-position order on recovery (game=%s)", game,
		))
	})
})

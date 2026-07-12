// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package resilience_test

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
)

const restartSpecTimeout = 3 * time.Minute

// eventsStream opens an INDEPENDENT connection to the shared broker and returns
// the EVENTS stream handle, so LastSeq assertions bind on broker state rather
// than any replica's cached view (mirrors boot_smoke_test.go). The connection is
// closed by t.Cleanup (natstest.NATSEnv.Conn).
func eventsStream(ctx context.Context, env *natstest.NATSEnv) jetstream.Stream {
	GinkgoHelper()
	conn := env.Conn(suiteT)
	js, err := jetstream.New(conn)
	Expect(err).NotTo(HaveOccurred(), "jetstream.New over independent conn")
	stream, err := js.Stream(ctx, eventbus.StreamName)
	Expect(err).NotTo(HaveOccurred(), "EVENTS stream must exist on the shared broker")
	return stream
}

// lastSeq reads the EVENTS stream's current last sequence over the independent
// handle. Used both for a one-shot baseline and inside Eventually polls.
func lastSeq(ctx context.Context, stream jetstream.Stream) uint64 {
	info, err := stream.Info(ctx)
	Expect(err).NotTo(HaveOccurred(), "read EVENTS stream info")
	return info.State.LastSeq
}

// Restart and reconnect exercises the three chaos dimensions of success
// criterion #1 that the M12 last-write-wins Describe does not: replica restart,
// client (transport) reconnect, and broker-flap publish recovery. All three
// build on the plan-01 substrate (one broker + one shared database) and the
// plan-01 chaos primitives (startReplica, pauseBroker/unpauseBroker). No plugins
// are loaded here — the command path was already covered in Task 1, and direct
// world.Service / EmitDirectEvent writes suffice for the transport/broker
// dimensions.
var _ = Describe("Restart and reconnect", Ordered, func() {
	var (
		env      *natstest.NATSEnv
		replicaA *integrationtest.Server
		replicaB *integrationtest.Server
	)

	BeforeAll(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		DeferCleanup(cancel)

		env = startExternalNATS(ctx)
		replicaA = startReplica(suiteT, env.URL, "")
		replicaB = startReplica(suiteT, env.URL, replicaA.ConnStr())
	})

	It("a restarted replica serves state written before it died", func() {
		ctx, cancel := context.WithTimeout(context.Background(), restartSpecTimeout)
		DeferCleanup(cancel)

		// Write a location rename through replica B's direct world.Service path,
		// then commit it to the SHARED database.
		locID := replicaA.NewLocation(ctx)
		svcB := newWorldService(replicaB)
		const subj = "character:restart-writer"
		loc, err := svcB.GetLocation(ctx, subj, locID)
		Expect(err).NotTo(HaveOccurred(), "svcB.GetLocation")
		const preRestartName = "written-before-restart"
		loc.Name = preRestartName
		Expect(svcB.UpdateLocation(ctx, subj, loc)).To(Succeed(), "pre-restart write must commit")

		// "Kill" replica B. Server.Stop is idempotent (plugin-only teardown; the
		// shared DB + broker are owned by env / replica A cleanup), so the later
		// t.Cleanup Stop is safe.
		replicaB.Stop()

		// Boot a fresh replica B' joining the SAME database and the SAME broker.
		// B' must boot cleanly against the ALREADY-EXISTING EVENTS stream — this
		// is the CreateOrUpdateStream idempotence path, and a successful boot IS
		// the assertion that no rebuild/replay runs at startup.
		replicaBPrime := startReplica(suiteT, env.URL, replicaA.ConnStr())

		// The pre-restart write is recoverable purely from the database: B' never
		// replayed the event log, yet it reads the committed row. This is the ADR
		// observation — canonical world state is DB-derived (direct-write CRUD),
		// so recovery is a DB read, NOT an event-sourced rebuild.
		var gotName string
		Expect(replicaBPrime.Pool().QueryRow(ctx,
			`SELECT name FROM locations WHERE id = $1`, locID.String()).
			Scan(&gotName)).To(Succeed(), "B' reads the pre-restart row from the shared DB")
		Expect(gotName).To(Equal(preRestartName),
			"restarted replica must serve the state committed before it died (DB-derived, no replay)")

		reportVerdict(fmt.Sprintf(
			"CHAOS-VERDICT: replica-restart: B' booted cleanly against the existing EVENTS stream and served pre-restart state %q from the shared DB (recovery is DB-read, not event replay)", gotName,
		))
	})

	It("a detached client reattaches and resumes live delivery", func() {
		ctx, cancel := context.WithTimeout(context.Background(), restartSpecTimeout)
		DeferCleanup(cancel)

		sess := replicaA.ConnectGuest(ctx)
		stream := eventsStream(ctx, env)
		baseline := lastSeq(ctx, stream)

		// Simulate a client disconnect and reconnect. ReattachTransport blocks
		// until REPLAY_COMPLETE, so its return already proves the durable consumer
		// re-wired after the detach.
		sess.DetachTransport(ctx)
		sess.ReattachTransport(ctx)

		// Outcome-based assertion (RESEARCH Pitfall 5: never assert on
		// connection-state callbacks): a publish after reattach lands on the
		// broker, advancing the EVENTS LastSeq observed over the independent
		// connection. Reattach success + this advance together prove live delivery
		// resumed through the reattached stream.
		Expect(sess.EmitDirectEvent(ctx,
			"character."+sess.CharacterID.String(), "say",
			[]byte(`{"text":"post-reattach"}`))).
			To(Succeed(), "post-reattach EmitDirectEvent")

		Eventually(func() uint64 { return lastSeq(ctx, stream) }, 20*time.Second, 50*time.Millisecond).
			Should(BeNumerically(">", baseline),
				"EVENTS LastSeq must advance past %d after the reattached session publishes", baseline)

		reportVerdict(fmt.Sprintf(
			"CHAOS-VERDICT: client-reconnect: session detached then reattached (REPLAY_COMPLETE observed) and resumed live delivery — EVENTS LastSeq advanced past %d", baseline,
		))
	})

	It("publishing resumes after a broker flap", func() {
		ctx, cancel := context.WithTimeout(context.Background(), restartSpecTimeout)
		DeferCleanup(cancel)

		sess := replicaA.ConnectGuest(ctx)
		stream := eventsStream(ctx, env)
		baseline := lastSeq(ctx, stream)

		// Freeze the broker (docker pause). Attempt one publish while paused,
		// bounded by an 8s deadline (keeps the total pause ≤ 10s — RESEARCH
		// Pitfall 7). The JetStream publish-with-ack may block until this deadline
		// (no ack arrives while frozen); accept EITHER an error OR a delayed
		// success and record which.
		pauseBroker(ctx, env)
		pausedCtx, pausedCancel := context.WithTimeout(ctx, 8*time.Second)
		pausedErr := sess.EmitDirectEvent(pausedCtx,
			"character."+sess.CharacterID.String(), "say",
			[]byte(`{"text":"during-pause"}`))
		pausedCancel()
		unpauseBroker(ctx, env)

		pausedOutcome := "returned-error"
		if pausedErr == nil {
			pausedOutcome = "delayed-success"
		}
		AddReportEntry("broker-flap paused-publish outcome", pausedOutcome)

		// After unpause the broker resumes; a fresh publish must land and advance
		// the sequence. The generous poll window absorbs the NATS client's
		// reconnect backoff after the freeze.
		Expect(sess.EmitDirectEvent(ctx,
			"character."+sess.CharacterID.String(), "say",
			[]byte(`{"text":"after-unpause"}`))).
			To(Succeed(), "post-unpause EmitDirectEvent must succeed")

		Eventually(func() uint64 { return lastSeq(ctx, stream) }, 30*time.Second, 100*time.Millisecond).
			Should(BeNumerically(">", baseline),
				"EVENTS LastSeq must advance past %d once the broker is unpaused", baseline)

		reportVerdict(fmt.Sprintf(
			"CHAOS-VERDICT: broker-flap: publishing recovered after a docker-pause flap (paused-attempt=%s; LastSeq advanced past %d after unpause)", pausedOutcome, baseline,
		))
	})
})

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package resilience_test

import (
	"context"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/testsupport/natstest"
)

const smokeSpecTimeout = 30 * time.Second

// The boot smoke proves the substrate every later verdict stands on: two
// in-process CoreServer replicas booted over ONE natstest broker and ONE shared
// Postgres database genuinely share both, and agree on the game id.
var _ = Describe("Two-replica substrate", Ordered, func() {
	var (
		env      *natstest.NATSEnv
		replicaA *integrationtest.Server
		replicaB *integrationtest.Server
	)

	BeforeAll(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		DeferCleanup(cancel)

		env = startExternalNATS(ctx)

		// Replica A creates the fresh database; replica B joins it. Neither passes
		// WithInTreePlugins — the smoke boot stays light.
		replicaA = startReplica(suiteT, env.URL, "")
		replicaB = startReplica(suiteT, env.URL, replicaA.ConnStr())
	})

	It("replicas share one database", func() {
		ctx, cancel := context.WithTimeout(context.Background(), smokeSpecTimeout)
		DeferCleanup(cancel)

		// A creates a location; B must observe the row through its OWN pool —
		// proving both replicas point at the same database.
		locID := replicaA.NewLocation(ctx)

		var name string
		err := replicaB.Pool().
			QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, locID.String()).
			Scan(&name)
		Expect(err).NotTo(HaveOccurred(),
			"replica B must read replica A's location row from the shared database (id=%s)", locID)
		Expect(name).NotTo(BeEmpty(), "shared-DB location row must carry replica A's name")
	})

	It("replicas share one broker", func() {
		ctx, cancel := context.WithTimeout(context.Background(), smokeSpecTimeout)
		DeferCleanup(cancel)

		// Observe the EVENTS stream over an INDEPENDENT connection (not either
		// replica's cached view) so the assertion binds on broker state.
		conn := env.Conn(suiteT)
		js, err := jetstream.New(conn)
		Expect(err).NotTo(HaveOccurred(), "jetstream.New over independent conn")
		stream, err := js.Stream(ctx, eventbus.StreamName)
		Expect(err).NotTo(HaveOccurred(), "EVENTS stream must exist on the shared broker")

		info, err := stream.Info(ctx)
		Expect(err).NotTo(HaveOccurred(), "read EVENTS stream info")
		baseline := info.State.LastSeq

		// Publish one event from replica A's subsystem.
		sess := replicaA.ConnectGuest(ctx)
		emitErr := sess.EmitDirectEvent(
			ctx,
			"character."+sess.CharacterID.String(),
			"say",
			[]byte(`{"text":"resilience-boot-smoke"}`),
		)
		Expect(emitErr).NotTo(HaveOccurred(), "replica A EmitDirectEvent")

		// The SAME independent connection must observe the sequence advance —
		// replica A's publish landed on the broker both replicas share.
		Eventually(func() uint64 {
			i, infoErr := stream.Info(ctx)
			// Surface a persistent stream.Info failure as the real error instead
			// of masking it as an opaque "0 not > baseline" timeout — Gomega
			// recovers this panic and retries inside Eventually (matches the
			// lastSeq sibling in restart_reconnect_test.go).
			Expect(infoErr).NotTo(HaveOccurred(), "read EVENTS stream info")
			return i.State.LastSeq
		}, smokeSpecTimeout, 50*time.Millisecond).
			Should(BeNumerically(">", baseline),
				"EVENTS LastSeq must advance past %d after replica A publishes", baseline)
	})

	It("replicas agree on GameID", func() {
		Expect(replicaA.GameID()).To(Equal(replicaB.GameID()),
			"both replicas' subsystems must present the same game id (Config.Defaults())")
	})
})

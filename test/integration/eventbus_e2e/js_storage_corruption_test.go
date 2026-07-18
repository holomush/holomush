// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
)

// JS storage corruption specs — covers spec §8 "Embedded JS storage
// corruption -> Rebuild from PG audit; ULIDs stable".
//
// Preserved ULIDs is the load-bearing invariant: the PG audit row id MUST
// equal the original publish ULID, so a rebuild that republishes via the
// Publisher with `Nats-Msg-Id = audit.id` will land back on the stream
// with the same seq-semantics AND the same ULID identifier. Consumers
// with pinned ULID cursors survive the corruption event transparently.
//
// The JS-rebuild tool is not yet implemented. This skeleton:
//
//  1. Publishes N events and lets them project into events_audit.
//  2. Simulates JS loss by purging the EVENTS stream (JetStream API).
//  3. TODO: invokes the rebuild tool.
//  4. TODO: asserts the new JS stream has the same N ULIDs in the same order.
//
// Follow-up: holomush-6nds — JS storage rebuild from PG audit.
var _ = Describe("JS storage corruption rebuild from PG audit preserves ULIDs", func() {
	It("rebuild from PG audit preserves original ULIDs in same order", func() {
		Skip("TODO(holomush-6nds): JS storage rebuild tool not yet implemented — skeleton retained for the follow-up bead")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		bus := freshBus()
		pool := freshPool()

		hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
		Expect(hostSub.Prepare(ctx)).To(Succeed())
		Expect(hostSub.Activate(ctx)).To(Succeed())
		DeferCleanup(func() { _ = hostSub.Stop(context.Background()) })

		pub := bus.Bus.Publisher()
		const count = 10
		originalIDs := make([][]byte, 0, count)
		for i := 0; i < count; i++ {
			evt := mintEvent(eventbus.Subject("events.main.jsrebuild.s1"), "scene.pose", `{"n":`+itoa(i)+`}`)
			Expect(pub.Publish(ctx, evt)).To(Succeed())
			originalIDs = append(originalIDs, evt.ID.Bytes())
		}
		hostSub.AwaitDrained(suiteT, 10*time.Second)

		// Purge the EVENTS stream to simulate JS storage loss.
		stream, err := bus.JS.Stream(ctx, eventbus.StreamName)
		Expect(err).NotTo(HaveOccurred())
		Expect(stream.Purge(ctx)).To(Succeed())

		// TODO(holomush-6nds): invoke rebuild tool, e.g.:
		//   Expect(rebuild.FromPGAudit(ctx, pool, bus.Bus.Publisher())).To(Succeed())
		//
		// After rebuild:
		//   Assertion: stream.Info LastSeq == count
		//   Assertion: every original ULID is present (via audit OR via a drain)

		_ = originalIDs
	})
})

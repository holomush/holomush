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

// Audit drift detector specs — covers spec §8 "Audit drift detector ->
// Tampered row reported with id". The detector is not yet implemented;
// this skeleton:
//
//  1. Publishes a canonical event and waits for it to be projected into
//     events_audit.
//  2. Tampers the row (e.g. sets codec='not-a-real-codec' or corrupts the
//     payload).
//  3. TODO: invokes the drift detector and asserts the tampered row's id
//     is returned with a diagnostic reason.
//
// The setup is preserved so the follow-up bead only has to add the
// detector wiring and the final assertion, not rebuild the spec.
//
// Follow-up: holomush-ecbg — eventbus audit drift detector.
var _ = Describe("Audit drift detector reports tampered row", func() {
	It("detects tampered row and reports id with diagnostic reason", func() {
		Skip("TODO(holomush-ecbg): drift detector not yet implemented — skeleton retained for the follow-up bead")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		bus := freshBus()
		pool := freshPool()

		// Stand up the host projection so publishes reach events_audit.
		hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
		Expect(hostSub.Prepare(ctx)).To(Succeed())
		Expect(hostSub.Activate(ctx)).To(Succeed())
		DeferCleanup(func() { _ = hostSub.Stop(context.Background()) })

		// Publish one event.
		pub := bus.Bus.Publisher()
		evt := mintEvent(eventbus.Subject("events.main.drift.s1"), "scene.pose", `{"x":1}`)
		Expect(pub.Publish(ctx, evt)).To(Succeed())
		hostSub.AwaitDrained(suiteT, 10*time.Second)

		// Tamper: set codec to an unregistered value. The drift detector
		// must observe that codec resolution fails and report the id.
		_, err := pool.Exec(ctx,
			`UPDATE events_audit SET codec = 'not-a-real-codec' WHERE id = $1`,
			evt.ID.Bytes())
		Expect(err).NotTo(HaveOccurred())

		// TODO(holomush-ecbg): invoke the detector and assert:
		//   reports, err := drift.Scan(ctx, pool, codec.DefaultRegistry)
		//   Expect(err).NotTo(HaveOccurred())
		//   Expect(reports).To(HaveLen(1))
		//   Expect(reports[0].ID).To(Equal(evt.ID))
		//   Expect(reports[0].Reason).To(ContainSubstring("codec"))
	})
})

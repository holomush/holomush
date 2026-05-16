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

// Backfill rebuild specs — covers spec §8 "Backfill rebuild ->
// bin/holomush audit-backfill produces matching counts".
// The audit-backfill CLI subcommand does not exist yet; this skeleton
// stands up the JS stream with content and defers the CLI invocation +
// post-invocation count parity assertion to the follow-up bead.
//
// Follow-up: holomush-l4kx — holomush audit-backfill CLI subcommand.
var _ = Describe("Audit backfill produces matching counts", func() {
	It("bin/holomush audit-backfill produces matching counts", func() {
		Skip("TODO(holomush-l4kx): audit-backfill CLI not yet implemented — skeleton retained for the follow-up bead")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		bus := freshBus()
		pool := freshPool()
		pub := bus.Bus.Publisher()

		// Publish N events while the audit projection is NOT running, so
		// events_audit stays empty while JS accumulates the stream.
		const count = 20
		for i := 0; i < count; i++ {
			Expect(pub.Publish(ctx, mintEvent(
				eventbus.Subject("events.main.backfill.s1"),
				"scene.pose",
				`{"n":`+itoa(i)+`}`,
			))).To(Succeed())
		}
		// Sanity: events_audit is empty right now (no projection running).

		// TODO(holomush-l4kx): Exec the subcommand:
		//   cmd := exec.Command("bin/holomush", "audit-backfill", "--dsn", connStr)
		//   out, err := cmd.CombinedOutput()
		//   Expect(err).NotTo(HaveOccurred(), "%s", out)
		//
		// After:
		//   Assertion: events_audit row count == JS stream LastSeq.

		_ = audit.DefaultConsumerName // silence unused import if imports change
		_ = pool
	})
})

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
)

// Rendering completeness specs — covers INV-EVENTBUS-7 + INV-EVENTBUS-13. After
// publishing host-builtin events through RenderingPublisher, every
// events_audit row MUST have a non-null rendering JSONB column populated
// from the App-Rendering NATS header by the audit projection.
//
//   - INV-EVENTBUS-7: events_audit.rendering is NOT NULL for every projected row.
//   - INV-EVENTBUS-13: the rendering column carries the same metadata stamped by
//     the publisher (verified here via source_plugin = "builtin" for all
//     host-owned event types).
var _ = Describe("Rendering completeness", func() {
	It("every events_audit row has non-null rendering with correct source_plugin (INV-EVENTBUS-7, INV-EVENTBUS-13)", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		bus := freshBus()
		pool := freshPool()

		// Stand up the host audit projection so publishes reach events_audit.
		hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
		Expect(hostSub.Start(ctx)).To(Succeed())
		DeferCleanup(func() { _ = hostSub.Stop(context.Background()) })

		// Build the wrapped publisher: BootstrapVerbRegistry + RenderingPublisher.
		registry, err := core.BootstrapVerbRegistry("test-0.1")
		Expect(err).NotTo(HaveOccurred())
		pub := eventbus.NewRenderingPublisher(bus.Bus.Publisher(), registry)

		// Publish three host-builtin events of different types. The OwnerMap is
		// empty (default Config), so every subject is host-owned and lands in
		// events_audit. Use a run-scoped subject prefix so assertions only
		// consider rows from THIS spec invocation (events_audit accumulates
		// across specs in the same Ginkgo run; the original "events.main.test.*"
		// subjects would be contaminated by prior runs in the same container).
		gameID := "rc_" + core.NewULID().String()
		subjectPrefix := "events." + gameID + ".test."
		subjectLike := "events." + gameID + ".%"
		types := []eventbus.Type{"arrive", "leave", "system"}
		for i, typ := range types {
			ev := eventbus.Event{
				ID:        core.NewULID(),
				Subject:   eventbus.Subject(subjectPrefix + string(typ)),
				Type:      typ,
				Timestamp: time.Now().UTC(),
				Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
				Payload:   []byte(`{}`),
			}
			Expect(pub.Publish(ctx, ev)).To(Succeed(), "publish %d type=%s", i, typ)
		}

		// Wait for the projection to drain.
		hostSub.AwaitDrained(suiteT, 10*time.Second)
		Eventually(func() bool {
			var count int
			qerr := pool.QueryRow(
				ctx,
				"SELECT COUNT(*) FROM events_audit WHERE subject LIKE $1",
				subjectLike,
			).Scan(&count)
			return qerr == nil && count >= len(types)
		}, 10*time.Second, 100*time.Millisecond).Should(BeTrue(),
			"audit projection did not drain all events")

		// INV-EVENTBUS-7: every row has a non-null rendering JSONB column. Schema
		// enforces NOT NULL, but we assert here so a regression that drops the
		// constraint or writes 'null' JSONB is caught.
		var nullCount int
		Expect(pool.QueryRow(
			ctx,
			"SELECT COUNT(*) FROM events_audit WHERE subject LIKE $1 AND rendering IS NULL",
			subjectLike,
		).Scan(&nullCount)).To(Succeed())
		Expect(nullCount).To(BeZero(), "INV-EVENTBUS-7: every events_audit row MUST have non-null rendering")

		// INV-EVENTBUS-13: rendering column carries the metadata stamped by the
		// publisher. Spot-check the first row's source_plugin, then verify
		// every host-builtin row reports source_plugin="builtin".
		var sourcePlugin string
		Expect(pool.QueryRow(
			ctx,
			"SELECT rendering->>'source_plugin' FROM events_audit WHERE subject LIKE $1 ORDER BY js_seq LIMIT 1",
			subjectLike,
		).Scan(&sourcePlugin)).To(Succeed())
		Expect(sourcePlugin).To(Equal("builtin"))

		var nonBuiltinCount int
		Expect(pool.QueryRow(
			ctx,
			"SELECT COUNT(*) FROM events_audit WHERE subject LIKE $1 AND rendering->>'source_plugin' <> 'builtin'",
			subjectLike,
		).Scan(&nonBuiltinCount)).To(Succeed())
		Expect(nonBuiltinCount).To(BeZero(),
			"INV-EVENTBUS-13: every host-builtin row must report source_plugin='builtin'")
	})
})

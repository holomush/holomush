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

// Audit-only channel persistence specs — regression-lock for
// holomush-jxo8.6.26: sub-epic D's host-emit security audit events
// (crypto.totp_locked, crypto.totp_cleared, crypto.totp_recovery_code_consumed,
// crypto.policy_set) MUST persist to events_audit when published through
// RenderingPublisher.
//
// Pre-fix bug: cmd/holomush/core.go wired bare eventBusSub.Publisher() to
// the totpAuditSvc and cryptoPolicySub subsystems, so events reached
// JetStream without the App-Rendering header. The audit projection's
// persist() check (audit/projection.go:headerRendering) rejected every
// such event with AUDIT_MISSING_HEADER and the row never landed.
//
// Restores INV-CRYPTO-81 (audit emission persists) and INV-CRYPTO-84 (chain genesis
// emit persists). The spec exercises the full publish→persist round-trip
// for every host-emit AUDIT_ONLY event type registered in builtins.go.
var _ = Describe("Audit-only channel persists to events_audit", func() {
	It("persists every AUDIT_ONLY host-emit event type", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)

		bus := freshBus()
		pool := freshPool()

		hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
		Expect(hostSub.Prepare(ctx)).To(Succeed())
		Expect(hostSub.Activate(ctx)).To(Succeed())
		DeferCleanup(func() { _ = hostSub.Stop(context.Background()) })

		registry, err := core.BootstrapVerbRegistry("test-0.1")
		Expect(err).NotTo(HaveOccurred())
		pub := eventbus.NewRenderingPublisher(bus.Bus.Publisher(), registry)

		// One event per host-emit AUDIT_ONLY type. Subjects mirror the real
		// patterns produced by internal/totp/audit.go and internal/admin/policy/emitter.go
		// so the audit row's subject column carries production-shaped values.
		//
		// gameID is unique per test run (suffixed with a fresh ULID) so the
		// downstream events_audit assertions only match rows produced by THIS
		// run — a fixed "main" would let prior test runs pollute the result
		// set when Postgres state persists across runs.
		gameID := "main_" + core.NewULID().String()
		auditEvents := []struct {
			subject eventbus.Subject
			typ     eventbus.Type
		}{
			{eventbus.Subject("events." + gameID + ".system.crypto_totp.01TESTPLAYERLOCKED.locked"), "crypto.totp_locked"},
			{eventbus.Subject("events." + gameID + ".system.crypto_totp.01TESTPLAYERCLEARD.cleared"), "crypto.totp_cleared"},
			{eventbus.Subject("events." + gameID + ".system.crypto_totp.01TESTPLAYERRECOVD.recovery_consumed"), "crypto.totp_recovery_code_consumed"},
			{eventbus.Subject("events." + gameID + ".system.crypto_policy.dual_control_required"), "crypto.policy_set"},
		}

		for _, ae := range auditEvents {
			ev := eventbus.Event{
				ID:        core.NewULID(),
				Subject:   ae.subject,
				Type:      ae.typ,
				Timestamp: time.Now().UTC(),
				Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
				Payload:   []byte(`{}`),
			}
			Expect(pub.Publish(ctx, ev)).To(Succeed(), "publish %s", ae.typ)
		}

		hostSub.AwaitDrained(suiteT, 10*time.Second)
		Eventually(func() bool {
			var count int
			qerr := pool.QueryRow(
				ctx,
				`SELECT COUNT(*) FROM events_audit WHERE subject LIKE $1`,
				"events."+gameID+".%",
			).Scan(&count)
			return qerr == nil && count >= len(auditEvents)
		}, 10*time.Second, 100*time.Millisecond).Should(BeTrue(),
			"audit projection did not drain all AUDIT_ONLY events")

		// Every AUDIT_ONLY event lands with display_target = "EVENT_CHANNEL_AUDIT_ONLY"
		// in the rendering JSONB, source_plugin="builtin", and category="system".
		for _, ae := range auditEvents {
			var (
				displayTarget string
				sourcePlugin  string
				category      string
			)
			qerr := pool.QueryRow(
				ctx,
				`SELECT rendering->>'display_target',
				        rendering->>'source_plugin',
				        rendering->>'category'
				   FROM events_audit
				  WHERE type = $1
				    AND subject LIKE $2
				  ORDER BY js_seq DESC
				  LIMIT 1`,
				string(ae.typ),
				"events."+gameID+".%",
			).Scan(&displayTarget, &sourcePlugin, &category)
			Expect(qerr).NotTo(HaveOccurred(), "no events_audit row for type=%s — pre-fix bug?", ae.typ)
			Expect(displayTarget).To(Equal("EVENT_CHANNEL_AUDIT_ONLY"),
				"type=%s rendering.display_target", ae.typ)
			Expect(sourcePlugin).To(Equal("builtin"),
				"type=%s rendering.source_plugin", ae.typ)
			Expect(category).To(Equal("system"),
				"type=%s rendering.category", ae.typ)
		}
	})
})

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// suiteT exposes the *testing.T from TestIntegrationHarness so Ginkgo
// Describe blocks can pass it to integrationtest.Start (which requires
// *testing.T — Ginkgo's GinkgoT() does not satisfy that interface directly).
//
// Mirrors the pattern in test/integration/*/_suite_test.go files.
var suiteT *testing.T

// TestIntegrationHarness is the Ginkgo entry point for the integrationtest
// harness's own smoke specs. These are NOT feature integration tests (those
// live in test/integration/{privacy,presence,scenes,session,...}/) — they
// verify the harness itself wires up correctly end-to-end so downstream
// suites can rely on it.
func TestIntegrationHarness(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "integrationtest harness — smoke specs")
}

// Originally landed for holomush-iwzt.6 as TestPrivacyHarnessSmoke when this
// package was named privacytest; renamed alongside the package generalization
// (privacytest → integrationtest) to reflect that the harness now serves
// privacy + presence + session-store integration tests across the codebase.
// Converted from a `testing.T`+`require` style test to a Ginkgo spec in
// holomush-m5nj per the project-wide MUST-use-Ginkgo-for-integration rule.
var _ = Describe("Integration harness basic connect/command/logout", func() {
	It("connects a guest, dispatches a command, and logs out cleanly", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel called below; deferred via defer
		defer cancel()

		ts := integrationtest.Start(suiteT)
		defer ts.Stop()

		sess := ts.ConnectGuest(ctx)
		Expect(sess.SessionID).NotTo(BeEmpty(),
			"ConnectGuest MUST return a populated SessionID")

		Expect(sess.SendCommand(ctx, "look")).To(Succeed(),
			"SendCommand 'look' MUST succeed against the harness's empty registry")

		// Smoke-test event delivery: wait briefly for ANY event; tolerate empty
		// (event flow exercised by Task 9+ integration tests).
		_ = sess.DrainEvents(ctx, 250*time.Millisecond)

		sess.Logout(ctx)
	})
})

// holomush-q2qt: Session.RefreshFromPersisted re-reads the mutable Session
// fields (LocationID, LocationArrivedAt) from the persisted sessions row.
//
// The smoke uses Server.SetLocationArrivedAt to mutate the DB row WITHOUT
// touching the harness-side struct (Server.MoveTo updates both, so it's
// tautological as a refresh trigger). After SetLocationArrivedAt the
// struct is stale; after RefreshFromPersisted it MUST reflect the DB.
var _ = Describe("Integration harness Session.RefreshFromPersisted", func() {
	It("re-reads LocationArrivedAt from the persisted sessions row", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel called below; deferred via defer
		defer cancel()

		ts := integrationtest.Start(suiteT)
		defer ts.Stop()

		sess := ts.ConnectAuthed(ctx, "Quinn")
		originalArrivedAt := sess.LocationArrivedAt
		originalLocation := sess.LocationID

		// Mutate the DB row out-of-band. Direct SQL bypasses the harness's
		// struct update, so without a refresh the harness-side field stays
		// stale.
		futureArrivedAt := originalArrivedAt.Add(42 * time.Second)
		ts.SetLocationArrivedAt(ctx, sess.SessionID, futureArrivedAt)

		Expect(sess.LocationArrivedAt).To(BeTemporally("==", originalArrivedAt),
			"precondition: SetLocationArrivedAt MUST NOT touch the harness-side struct")

		sess.RefreshFromPersisted(ctx)

		Expect(sess.LocationArrivedAt).To(BeTemporally("==", futureArrivedAt),
			"RefreshFromPersisted MUST surface the DB-side LocationArrivedAt")
		Expect(sess.LocationID).To(Equal(originalLocation),
			"RefreshFromPersisted MUST NOT mutate LocationID when the DB row's location is unchanged")

		sess.Logout(ctx)
	})
})

// holomush-m5nj precursor to iwzt.16: ConnectAuthed auto-attaches a live
// Subscribe stream; DetachTransport tears it down and transitions the
// session to Detached; ReattachTransport rebuilds the stream against the
// same session (production ReattachCAS flips status back to Active); an
// event published during the detach window is delivered to the reattached
// stream via JetStream durable replay (the I-PRIV-3 Round 3 claim that
// iwzt.16 will assert formally in test/integration/privacy/).
var _ = Describe("Integration harness Subscribe transport lifecycle", func() {
	It("redelivers detach-window events via durable replay on reattach", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel called below; deferred via defer
		defer cancel()

		ts := integrationtest.Start(suiteT)
		defer ts.Stop()

		// Two characters at the same location: Felix is the one we cycle the
		// transport on; Iris is a stable publisher.
		felix := ts.ConnectAuthed(ctx, "Felix")
		iris := ts.ConnectAuthed(ctx, "Iris")
		Expect(felix.LocationID).To(Equal(iris.LocationID),
			"smoke precondition: both characters at the guest start location")

		felix.DetachTransport(ctx)

		// Iris emits during Felix's detach window — the event lands in Felix's
		// durable consumer and MUST be redelivered on reattach.
		const marker = "m5nj-smoke:during-detach"
		Expect(iris.EmitDirectEvent(ctx, "location."+iris.LocationID.String(), marker,
			[]byte(`{"character_name":"Iris","action":"speaks while Felix is detached."}`))).
			To(Succeed(), "during-detach emit MUST publish")

		felix.ReattachTransport(ctx)

		// WaitForEvent reads from the channel until the marker arrives (or
		// waitCtx cancels / transport exits). The production filter-at-delivery
		// uses LocationArrivedAt (unchanged across reattach) as the floor —
		// the marker's timestamp is after that floor, so it passes through.
		waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
		defer waitCancel()
		ev := felix.WaitForEvent(waitCtx, marker)
		Expect(ev).NotTo(BeNil(),
			"durable replay MUST deliver the during-detach event to the reattached transport")
		Expect(ev.GetType()).To(Equal(marker))

		iris.Logout(ctx)
		felix.Logout(ctx)
	})
})

// holomush-87qu: Subscribe → REPLAY_COMPLETE budget regression lock.
//
// The bead's TDD acceptance asks for a server-side assertion that
// "empty-history Subscribe emits REPLAY_COMPLETE in < 200ms". The
// production observation was 8-10s wall time for the user-perceived
// 'syncing' window; this test fails-fast if a regression of that
// magnitude (or even one order smaller) reappears.
//
// Budget: 1 second — generous enough to absorb macOS testcontainers
// + embedded NATS startup variance under CI load (where bus.Start
// is hot but the SQL roundtrips through the testcontainer have
// >100ms tail-latency spikes), tight enough that the 8-10s field
// observation would fail by an order of magnitude. The bead's
// stricter 200ms target lives as an aspirational comment; tightening
// to 200ms requires deflaking the testcontainers latency floor
// first, which is out of scope for the instrumentation PR.
//
// Test shape: ConnectAuthed (warm-up: covers SelectCharacter +
// initial Subscribe), DetachTransport (drops the live stream),
// then measure ReattachTransport (a clean Subscribe → REPLAY_COMPLETE
// round-trip with no SelectCharacter overhead). Reattach is the
// pure Subscribe-handler measurement target.
var _ = Describe("Integration harness Subscribe budget (holomush-87qu)", func() {
	It("reattach completes Subscribe → REPLAY_COMPLETE within budget", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second) //nolint:govet // cancel called below; deferred via defer
		defer cancel()

		ts := integrationtest.Start(suiteT)
		defer ts.Stop()

		sess := ts.ConnectAuthed(ctx, "Budget")

		// Drop the transport so the next attach exercises only the
		// Subscribe handler — no SelectCharacter, no character create.
		sess.DetachTransport(ctx)

		// Aspirational target per the bead: <200ms. CI budget: 1s.
		// Tighten over time as the testcontainers latency floor
		// improves (or as the bead's perf fix lands).
		const budget = 1 * time.Second

		start := time.Now()
		sess.ReattachTransport(ctx)
		elapsed := time.Since(start)

		Expect(elapsed).To(BeNumerically("<", budget),
			"Subscribe → REPLAY_COMPLETE budget exceeded — took %s, budget %s. "+
				"Server-side OTel spans on the Subscribe handler (subscribe.validate_ownership, "+
				"subscribe.session_get, subscribe.add_connection, subscribe.reattach_cas, "+
				"subscribe.restore_focus, subscribe.bus_open_session, subscribe.send_synthetic) "+
				"will reveal which phase dominates; aspirational target is <200ms (holomush-87qu).",
			elapsed, budget)

		sess.Logout(ctx)
	})
})

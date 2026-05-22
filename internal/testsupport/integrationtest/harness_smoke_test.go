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
		Expect(iris.EmitDirectEvent(ctx, "location:"+iris.LocationID.String(), marker,
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

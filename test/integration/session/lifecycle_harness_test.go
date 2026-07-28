// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package session_test

import (
	"context"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
)

// lifecycleServer holds the single in-process stack shared by every
// session-lifecycle spec in this package. It is written once, by
// startLifecycleHarness from the suite's BeforeSuite, and read by
// lifecycleHarness from inside specs.
var lifecycleServer *integrationtest.Server

// lifecycleHarnessStarts counts calls to startLifecycleHarness. A suite-scoped
// harness starts exactly once; see the guard spec at the bottom of this file
// for why that is asserted rather than assumed.
var lifecycleHarnessStarts atomic.Int64

// startLifecycleHarness brings up the shared stack. It is called from the
// suite's BeforeSuite in session_persistence_suite_test.go — NOT lazily from
// lifecycleHarness, and NOT from a BeforeEach.
//
// The placement is the whole point and is not an implementation detail to
// improvise on. Ginkgo attaches a DeferCleanup to the node it was registered
// from: a DeferCleanup registered inside an It or a BeforeEach behaves like an
// AfterEach and fires when THAT SPEC ends. A once-guarded accessor whose first
// call happens inside a spec would therefore tear the harness down at the end
// of that spec and hand every later spec a stopped server. That failure
// presents as unrelated downstream specs failing on a closed connection, which
// is expensive to diagnose and easy to misattribute to flakiness. Suite-scoped
// cleanup requires registration from BeforeSuite, and this suite already has
// both nodes, so the correct shape costs nothing.
func startLifecycleHarness() {
	GinkgoHelper()
	lifecycleHarnessStarts.Add(1)
	lifecycleServer = integrationtest.Start(suiteT, integrationtest.WithBuiltinCommands())
}

// stopLifecycleHarness is called from the suite's AfterSuite.
//
// Note what Server.Stop does and does not do: it stops the plugin subsystem
// and nothing else. Postgres and NATS are released by t.Cleanup handlers that
// Start registered on suiteT. It does NOT log sessions out or tear down their
// transports — which is why per-session teardown is a separate obligation on
// every spec (see lifecycleHarness).
func stopLifecycleHarness() {
	if lifecycleServer != nil {
		lifecycleServer.Stop()
	}
}

// lifecycleHarness returns the ONE in-process stack — production CoreServer,
// event bus and session store — shared by every session-lifecycle spec in this
// package and in the spec plans that follow.
//
// # Why this seam exists
//
// The specs this suite started with are wired against raw stores
// (sessiontest.NewStoreWithPool, env.sessionStore). That is the right shape for
// what they assert — store-level predicates such as ListLapsedConnections — but
// it cannot reach transport attach and detach, history queries, or session
// expiry, because none of those exist below the server. The lifecycle matrix
// needs all three, so it drives the real stack instead. The two styles coexist
// in this package deliberately; neither is a migration away from the other.
//
// # Per-session teardown is the CALLER's obligation
//
// Server.Stop stops only the plugin subsystem. It does not log sessions out and
// it does not tear down their transports. A suite that opens sessions across
// many specs and relies on Stop alone therefore leaks a subscribe goroutine and
// a transport per session for the suite's whole duration, degrading later specs
// and masking real ordering bugs.
//
// So EVERY spec that opens a session MUST register its own teardown, with
// DeferCleanup, from inside that spec:
//
//	sess := lifecycleHarness().ConnectGuest(ctx)
//	DeferCleanup(func() { sess.Logout(ctx) })
//
// DeferCleanup is the correct scope here — a session belongs to one spec, so a
// cleanup that fires at that spec's end is exactly right. This is the one place
// DeferCleanup is appropriate in this file's world; it is emphatically NOT
// appropriate for the harness itself, for the reason startLifecycleHarness
// gives.
//
// # Why these start options, and only these
//
// WithBuiltinCommands: the harness's default command registry is EMPTY. Without
// this option a spec driving `quit` through Session.SendCommand dispatches
// nothing — and because an unknown command is a user-facing error, SendCommand
// returns NO error at all, so such a spec would pass while proving nothing.
// This option registers the compiled-in handlers (quit and shutdown) so the
// dispatch is real. Assert a production effect regardless; the absence of an
// error is not evidence.
//
// NOT WithInTreePlugins: it would register the same compiled-in handlers as a
// side effect, but it also requires built binary plugin artifacts, which these
// specs have no use for.
//
// NOT WithRealABAC: the harness default access engine is permissive, and that
// is what these specs need — they exercise session state, not authorization,
// which the access integration suite covers. The permissive choice is
// deliberate and explicit here so it cannot be inherited silently by a later
// spec that actually needs real policy.
func lifecycleHarness() *integrationtest.Server {
	GinkgoHelper()
	Expect(lifecycleServer).NotTo(BeNil(),
		"the shared lifecycle harness is nil; it is started in this suite's BeforeSuite "+
			"(startLifecycleHarness) and MUST NOT be started lazily from a spec")
	return lifecycleServer
}

// This spec carries no matrix-row marker on purpose: it guards the shared
// harness itself rather than satisfying a cell of the session-lifecycle matrix,
// so the registry has no row for it and the bijection meta-test must not expect
// one.
var _ = Describe("Shared session-lifecycle harness", func() {
	It("starts once for the whole suite and is still serving sessions when a spec runs", func() {
		ctx := context.Background()

		// A suite-scoped harness starts exactly once. If the start ever moves
		// into a BeforeEach — the per-spec fallback shape — this count climbs
		// past one and this assertion fails, forcing that change to be
		// deliberate rather than accidental.
		Expect(lifecycleHarnessStarts.Load()).To(BeEquivalentTo(1),
			"the harness MUST be started once from BeforeSuite, not per spec")

		ts := lifecycleHarness()

		// Liveness, asserted rather than assumed: opening a session and reading
		// the row back exercises the server. A harness torn down by a
		// mis-scoped cleanup in an earlier spec would fail here instead of
		// surfacing later as an unrelated closed-connection error.
		sess := ts.ConnectGuest(ctx)
		DeferCleanup(func() { sess.Logout(ctx) })

		info, err := ts.SessionStore().Get(ctx, sess.SessionID)
		Expect(err).NotTo(HaveOccurred(), "the shared harness MUST still serve session reads")
		Expect(info.Status).To(Equal(session.StatusActive),
			"a freshly connected session MUST be active on the shared harness")
	})
})

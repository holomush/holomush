// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// This file adds the single-principal subject-scoping proof (CLUSTER-02) to the
// external-mode boot suite. It proves scoping from BOTH sides in one CI-backed
// package (gated by `task test:int`, D-06), with no runbook-manual fallback:
//
//   - Case A (over-scoped): a plain NATS node with no account restrictions is
//     default-open, so VerifyAccountScoping refuses it with
//     EVENTBUS_ACCOUNT_OVERSCOPED — the self-check fails closed.
//   - Case B (correctly-scoped, HARD): an in-process nats-server loads the
//     SHIPPED deploy/nats/holomush-server.account.conf scoped account. The
//     server credential PASSES VerifyAccountScoping (its probe beyond the grants
//     is denied → nil), while the non-server credential is DENIED publish AND
//     subscribe on a probe under each of events.>/audit.>/internal.>.
//
// Both cases assert in-Go via nats.go's permissions-violation signal / the
// coded self-check error — never by grepping output for a success string.
package eventbus_external_test

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus"
)

// Credentials for the static-account users shipped in
// deploy/nats/holomush-server.account.conf. These are the file's documented
// placeholders (replaced at deploy time / superseded by nsc creds in
// production); the test uses them to exercise the exact shipped scoped account.
const (
	scopedServerUser     = "holomush-server"
	scopedServerPassword = "holomush-server-CHANGEME"
	scopedVerifyUser     = "holomush-verify"
	scopedVerifyPassword = "holomush-verify-CHANGEME"

	scopeProbeWait = 4 * time.Second
)

// scopedAccountConfPath resolves the shipped scoped-account config relative to
// this test file, so the proof loads the REAL deploy artifact rather than a
// test-local copy.
func scopedAccountConfPath() string {
	_, thisFile, _, ok := runtime.Caller(0)
	Expect(ok).To(BeTrue(), "runtime.Caller must resolve this test file")
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..",
		"deploy", "nats", "holomush-server.account.conf")
}

// startScopedNATSServer boots an in-process nats-server loading the shipped
// scoped account on a random client port and returns its client URL plus a
// shutdown func registered for cleanup.
func startScopedNATSServer() string {
	opts, err := natsserver.ProcessConfigFile(scopedAccountConfPath())
	Expect(err).NotTo(HaveOccurred(), "the shipped deploy/nats scoped account must load")
	opts.Port = -1 // random port avoids collisions with parallel specs
	opts.NoLog = true
	opts.NoSigs = true

	srv, err := natsserver.NewServer(opts)
	Expect(err).NotTo(HaveOccurred())
	go srv.Start()
	Expect(srv.ReadyForConnections(10*time.Second)).
		To(BeTrue(), "in-process scoped NATS must become ready")
	DeferCleanup(srv.Shutdown)
	return srv.ClientURL()
}

// violationSink installs an async error handler on conn that forwards every
// permissions violation to a buffered channel, so a probe can await the denial.
func violationSink(conn *nats.Conn) chan error {
	violations := make(chan error, 8)
	conn.SetErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
		if errors.Is(err, nats.ErrPermissionViolation) {
			select {
			case violations <- err:
			default:
			}
		}
	})
	return violations
}

// drain empties any pending violation so a probe observes only its own.
func drain(ch chan error) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// assertPubSubDenied asserts a non-server connection is denied BOTH publish and
// subscribe on subject, via the permissions-violation signal (not stdout text).
func assertPubSubDenied(conn *nats.Conn, violations chan error, subject string) {
	GinkgoHelper()

	drain(violations)
	Expect(conn.Publish(subject, []byte("scopecheck-probe"))).To(Succeed())
	Expect(conn.Flush()).To(Succeed())
	Eventually(violations, scopeProbeWait).Should(Receive(),
		"publish to %q must be denied for a non-server credential", subject)

	drain(violations)
	sub, err := conn.SubscribeSync(subject)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = sub.Unsubscribe() }()
	Expect(conn.Flush()).To(Succeed())
	Eventually(violations, scopeProbeWait).Should(Receive(),
		"subscribe to %q must be denied for a non-server credential", subject)
}

var _ = Describe("Single-principal subject scoping (CLUSTER-02)", func() {
	Describe("Case A — over-scoped default-open account is refused", func() {
		It("refuses a plain NATS node with EVENTBUS_ACCOUNT_OVERSCOPED", func() {
			ctx, cancel := context.WithTimeout(context.Background(), specTimeout)
			DeferCleanup(cancel)

			env := startExternalNATS(ctx) // no account restrictions == over-scoped
			conn, err := nats.Connect(env.URL)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(conn.Close)

			err = eventbus.VerifyAccountScoping(ctx, conn)
			expectOopsCode(err, "EVENTBUS_ACCOUNT_OVERSCOPED")
		})
	})

	Describe("Case B — shipped scoped account (HARD, no runbook fallback)", func() {
		It("passes VerifyAccountScoping for the server credential and denies the non-server on all three prefixes", func() {
			ctx, cancel := context.WithTimeout(context.Background(), specTimeout)
			DeferCleanup(cancel)

			url := startScopedNATSServer()

			// (1) The correctly-scoped server credential PASSES the self-check:
			// its probe beyond the granted prefixes is denied → nil.
			serverConn, err := nats.Connect(url,
				nats.UserInfo(scopedServerUser, scopedServerPassword))
			Expect(err).NotTo(HaveOccurred(), "server credential must connect")
			DeferCleanup(serverConn.Close)

			Expect(eventbus.VerifyAccountScoping(ctx, serverConn)).To(Succeed(),
				"correctly-scoped server account must pass the self-check")

			// (2) The non-server credential is DENIED publish+subscribe on a
			// probe under EACH of events.>/audit.>/internal.>.
			verifyConn, err := nats.Connect(url,
				nats.UserInfo(scopedVerifyUser, scopedVerifyPassword))
			Expect(err).NotTo(HaveOccurred(), "non-server credential must connect")
			DeferCleanup(verifyConn.Close)

			violations := violationSink(verifyConn)
			for _, prefix := range []string{"events", "audit", "internal"} {
				assertPubSubDenied(verifyConn, violations, prefix+".scopecheck.probe")
			}
		})
	})
})

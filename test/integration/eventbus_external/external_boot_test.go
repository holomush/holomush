// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package eventbus_external_test is the external-mode boot integration suite
// for the event bus (CLUSTER-01). It exercises the real dial + JetStream path
// against a single-node NATS testcontainer (internal/testsupport/natstest) —
// the proofs the embedded eventbustest harness cannot express — covering:
//
//   - external connect: mode=external against a live NATS URL brings Start up,
//     exposes JetStream, and declares the EVENTS stream (provision default);
//   - fail-closed boot (D-02): an unreachable URL refuses to Start with
//     EVENTBUS_EXTERNAL_CONNECT_FAILED and leaves conn/js nil — no embedded
//     fallback;
//   - provision:false verify success (D-03): a pre-existing EVENTS with the
//     expected config passes without any stream admin;
//   - provision:false fail-closed (D-03): a config mismatch or an absent stream
//     refuses to Start with EVENTBUS_STREAM_CONFIG_MISMATCH and never creates
//     the stream.
package eventbus_external_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/testsupport/natstest"
)

// TestEventBusExternalConnect is the Ginkgo entry point for the external-mode
// boot suite. The name is stable so `task test:int -- -run
// TestEventBusExternalConnect ./test/integration/eventbus_external/` selects it.
func TestEventBusExternalConnect(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "EventBus External-Mode Boot Suite")
}

const (
	specTimeout     = 30 * time.Second
	streamMaxAge    = 24 * time.Hour
	altStreamMaxAge = 1 * time.Hour
	dupeWindow      = 30 * time.Minute
)

// startExternalNATS boots a fresh single-node NATS container with NO account
// restrictions and registers its teardown on the spec. Also used by
// scopecheck_test.go's Case A, which deliberately needs a bare/unscoped node
// (see that file) — do NOT switch this helper to the scoped container.
func startExternalNATS(ctx context.Context) *natstest.NATSEnv {
	env, err := natstest.StartNATS(ctx)
	Expect(err).NotTo(HaveOccurred(), "StartNATS should return a running container")
	DeferCleanup(func() {
		_ = env.Terminate(context.Background())
	})
	return env
}

// startScopedExternalNATS boots a fresh single-node NATS container loading
// the shipped CLUSTER-02 scoped account (deploy/nats/cluster-server.conf) and
// registers its teardown on the spec. Scoped (not a bare unscoped node) so
// that a full eventbus.Subsystem.Start — dial + EnsureStream +
// VerifyAccountScoping (07-09) — succeeds against it; a bare node is
// deliberately over-scoped by design (see this package's scopecheck_test.go
// Case A) and would refuse to boot with EVENTBUS_ACCOUNT_OVERSCOPED.
func startScopedExternalNATS(ctx context.Context) *natstest.NATSEnv {
	env, err := natstest.StartScopedNATS(ctx)
	Expect(err).NotTo(HaveOccurred(), "StartScopedNATS should return a running container")
	DeferCleanup(func() {
		_ = env.Terminate(context.Background())
	})
	return env
}

// newExternalSubsystem builds an external-mode subsystem for the scoped
// server credentials at url (see startExternalNATS) with the given provision
// policy and stream retention. FileStorage matches the container's JetStream
// store (store_dir set in cluster-server.conf).
func newExternalSubsystem(url string, provision *bool, maxAge time.Duration) *eventbus.Subsystem {
	cfg := eventbus.Config{
		Mode:         eventbus.ModeExternal,
		URL:          natstest.ScopedURL(url),
		StreamMaxAge: maxAge,
		DupeWindow:   dupeWindow,
		Provision:    provision,
	}.Defaults()
	return eventbus.NewSubsystem(cfg)
}

// boolPtr returns a pointer to b — the *bool shape Config.Provision needs so an
// explicit false survives Defaults().
func boolPtr(b bool) *bool { return &b }

// eventsStreamExists reports whether the EVENTS stream is present on the broker
// at url, dialing an independent connection (authenticated as the scoped
// server principal — js.Stream is a JetStream API call, requiring the
// $JS.API.> grant) so it observes broker state rather than any subsystem's
// cached view.
func eventsStreamExists(ctx context.Context, url string) bool {
	conn, err := nats.Connect(natstest.ScopedURL(url))
	Expect(err).NotTo(HaveOccurred())
	defer conn.Close()
	js, err := jetstream.New(conn)
	Expect(err).NotTo(HaveOccurred())
	_, err = js.Stream(ctx, eventbus.StreamName)
	return err == nil
}

// expectOopsCode asserts err is an oops error carrying the given top-level code.
func expectOopsCode(err error, code string) {
	GinkgoHelper()
	Expect(err).To(HaveOccurred())
	oopsErr, ok := oops.AsOops(err)
	Expect(ok).To(BeTrue(), "expected an oops error, got %T", err)
	Expect(oopsErr.Code()).To(Equal(code))
}

var _ = Describe("External-mode event bus boot (CLUSTER-01)", func() {
	Describe("external connect", func() {
		It("dials the external NATS URL, exposes JetStream, and declares EVENTS", func() {
			ctx, cancel := context.WithTimeout(context.Background(), specTimeout)
			DeferCleanup(cancel)

			env := startScopedExternalNATS(ctx)
			sub := newExternalSubsystem(env.URL, nil, streamMaxAge) // provision default (true)
			Expect(sub.Prepare(ctx)).To(Succeed())
			Expect(sub.Activate(ctx)).To(Succeed())
			DeferCleanup(func() { _ = sub.Stop(context.Background()) })

			Expect(sub.JS()).NotTo(BeNil(), "JetStream context must be live after external Prepare")
			Expect(sub.Conn()).NotTo(BeNil(), "connection must be live after external Prepare")

			_, err := sub.JS().Stream(ctx, eventbus.StreamName)
			Expect(err).NotTo(HaveOccurred(), "EVENTS stream must be declared by EnsureStream")
		})
	})

	Describe("fail-closed boot when unreachable (D-02)", func() {
		// Verifies: INV-EVENTBUS-29
		It("refuses to Start and leaves conn/js nil with no embedded fallback", func() {
			ctx, cancel := context.WithTimeout(context.Background(), specTimeout)
			DeferCleanup(cancel)

			// 127.0.0.1:1 refuses connections immediately; there is no fallback.
			sub := newExternalSubsystem("nats://127.0.0.1:1", nil, streamMaxAge)
			err := sub.Prepare(ctx) // Prepare-only: the dial fails before Activate would ever run
			expectOopsCode(err, "EVENTBUS_EXTERNAL_CONNECT_FAILED")
			Expect(sub.Conn()).To(BeNil(), "no connection after a failed external dial")
			Expect(sub.JS()).To(BeNil(), "no JetStream context after a failed external dial")
		})
	})

	Describe("provision:false verify-or-fail (D-03)", func() {
		It("verifies a matching pre-existing EVENTS stream without stream admin", func() {
			ctx, cancel := context.WithTimeout(context.Background(), specTimeout)
			DeferCleanup(cancel)

			env := startScopedExternalNATS(ctx)

			// Provision the stream first via a provision:true subsystem, then
			// stop it (external Stop drains the conn; the stream persists on the
			// broker).
			provisioner := newExternalSubsystem(env.URL, boolPtr(true), streamMaxAge)
			Expect(provisioner.Prepare(ctx)).To(Succeed())
			Expect(provisioner.Activate(ctx)).To(Succeed())
			Expect(provisioner.Stop(ctx)).To(Succeed())

			// A provision:false subsystem with the SAME config verifies and boots.
			verifier := newExternalSubsystem(env.URL, boolPtr(false), streamMaxAge)
			Expect(verifier.Prepare(ctx)).To(Succeed())
			Expect(verifier.Activate(ctx)).To(Succeed())
			DeferCleanup(func() { _ = verifier.Stop(context.Background()) })
			Expect(verifier.JS()).NotTo(BeNil())
		})

		// Verifies: INV-EVENTBUS-29
		It("fails closed when the pre-existing EVENTS config mismatches", func() {
			ctx, cancel := context.WithTimeout(context.Background(), specTimeout)
			DeferCleanup(cancel)

			env := startScopedExternalNATS(ctx)

			// Provision EVENTS with a DIFFERENT retention than the verifier wants.
			provisioner := newExternalSubsystem(env.URL, boolPtr(true), altStreamMaxAge)
			Expect(provisioner.Prepare(ctx)).To(Succeed())
			Expect(provisioner.Activate(ctx)).To(Succeed())
			Expect(provisioner.Stop(ctx)).To(Succeed())

			verifier := newExternalSubsystem(env.URL, boolPtr(false), streamMaxAge)
			err := verifier.Prepare(ctx) // Prepare-only: the mismatch fails before Activate would ever run
			expectOopsCode(err, "EVENTBUS_STREAM_CONFIG_MISMATCH")
			Expect(verifier.Conn()).To(BeNil(), "fail-closed Prepare leaves no live connection")
			Expect(verifier.JS()).To(BeNil())
		})

		// Verifies: INV-EVENTBUS-29
		It("fails closed and creates nothing when EVENTS is absent", func() {
			ctx, cancel := context.WithTimeout(context.Background(), specTimeout)
			DeferCleanup(cancel)

			env := startExternalNATS(ctx)

			sub := newExternalSubsystem(env.URL, boolPtr(false), streamMaxAge)
			err := sub.Prepare(ctx) // Prepare-only: the absent-stream check fails before Activate would ever run
			expectOopsCode(err, "EVENTBUS_STREAM_CONFIG_MISMATCH")

			// The server MUST NOT have created the stream in provision:false mode.
			Expect(eventsStreamExists(ctx, env.URL)).To(BeFalse(),
				"provision:false must not create EVENTS when it is absent")
		})
	})
})

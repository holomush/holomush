// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package socket

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// TestSocketIntegration is the Ginkgo entry point for the
// internal/admin/socket integration suite. Coexists with the testify-style
// unit tests in subsystem_test.go — Go's test runner invokes both surfaces.
//
// Migrated from testify to Ginkgo/Gomega per project standards (PR #3671
// CodeRabbit feedback; mirrors the precedent at
// internal/admin/policy/subsystem_integration_test.go).
func TestSocketIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "internal/admin/socket Integration Suite")
}

// AdminSocketSubsystem holomush-jxo8.9 integration specs — verify the full
// wire from Start() through the real Server.Serve loop into runErrMonitor's
// Shutdown callback. Without these the four unit tests in subsystem_test.go
// (TestRunErrMonitor_*) verify only the goroutine body in isolation; a
// future refactor that drops the `go runErrMonitor(...)` call would leave
// those green while silently breaking the production contract.
var _ = Describe("AdminSocketSubsystem (integration, holomush-jxo8.9)", func() {
	var (
		dir string
		sub *AdminSocketSubsystem
	)

	BeforeEach(func() {
		var err error
		dir, err = os.MkdirTemp("", "hm-jxo8-9-")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(dir) })
	})

	Context("when the serve loop dies post-startup", func() {
		It("MUST trigger the Shutdown callback with the serve error within 3s", func() {
			// Simulates the production scenarios the bead enumerated: UDS
			// write error, OOM, corrupted listener state — any cause of a
			// post-startup serve-loop death. net.Listener.Close makes
			// srv.Serve(ln) return a wrapped net.ErrClosed which is NOT
			// http.ErrServerClosed (the latter is filtered at server.go:102),
			// so the error flows through to errCh.
			shutdownCh := make(chan error, 1)
			sub = NewAdminSocketSubsystem(AdminSocketSubsystemConfig{
				SocketPath: filepath.Join(dir, "admin.sock"),
				LockPath:   filepath.Join(dir, "admin.lock"),
				Version:    "test-jxo8.9",
				Shutdown:   func(err error) { shutdownCh <- err },
			})
			Expect(sub.Prepare(context.Background())).To(Succeed())
			Expect(sub.Activate(context.Background())).To(Succeed())
			DeferCleanup(func() {
				stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = sub.Stop(stopCtx)
			})

			Expect(sub.server).NotTo(BeNil(), "Start must construct a Server")
			Expect(sub.server.listener).NotTo(BeNil(), "Server must hold a live listener after Start")

			// Force the accept loop to fail. The serve goroutine at
			// server.go:100-106 will deliver the error to errCh,
			// runErrMonitor reads it, and the Shutdown callback should fire.
			Expect(sub.server.listener.Close()).To(Succeed())

			Eventually(shutdownCh, 3*time.Second).Should(Receive(
				WithTransform(func(err error) bool {
					// The wrapped error from net.Listener.Close is one of
					// the two "closed" sentinel forms depending on Go
					// version + protocol stack. Either is acceptable proof
					// that the accept-loop death propagated.
					return err != nil && (errors.Is(err, net.ErrClosed) || errors.Is(err, errClosedConn))
				}, BeTrue()),
			))
		})
	})

	Context("during normal Stop", func() {
		It("MUST NOT fire the Shutdown callback (regression guard for the channel-close branch)", func() {
			// This guards against a regression where runErrMonitor's
			// "channel closed" branch incorrectly invokes Shutdown — which
			// would cause the parent context to cancel on every normal
			// shutdown, taking down the process every time the admin socket
			// exits cleanly.
			shutdownFired := make(chan struct{}, 1)
			sub = NewAdminSocketSubsystem(AdminSocketSubsystemConfig{
				SocketPath: filepath.Join(dir, "admin.sock"),
				LockPath:   filepath.Join(dir, "admin.lock"),
				Version:    "test-jxo8.9-stop",
				Shutdown:   func(error) { shutdownFired <- struct{}{} },
			})
			Expect(sub.Prepare(context.Background())).To(Succeed())
			Expect(sub.Activate(context.Background())).To(Succeed())

			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			Expect(sub.Stop(stopCtx)).To(Succeed())

			// Give the monitor goroutine a moment to observe the channel
			// close and return. Then assert it did NOT invoke Shutdown.
			Consistently(shutdownFired, 200*time.Millisecond).ShouldNot(Receive())
		})
	})
})

// errClosedConn matches the pre-Go-1.16 "use of closed network connection"
// error string. Modern Go wraps as net.ErrClosed; older paths may still
// surface the raw string. Kept as a fallback for robustness.
var errClosedConn = errors.New("use of closed network connection")

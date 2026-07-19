// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// Compile-time interface check: *AdminSocketSubsystem must satisfy lifecycle.Subsystem.
var _ lifecycle.Subsystem = (*AdminSocketSubsystem)(nil)

// stubRekeyRPCHandler is a noop stub for RekeyHandler-forwarding tests. All
// methods return a sentinel error; the tests assert only that the field is
// non-nil on the Server's Config, not that any RPC succeeds.
type stubRekeyRPCHandler struct{}

var errStubRekey = errors.New("stub rekey handler")

func (*stubRekeyRPCHandler) HandleRekey(_ context.Context, _ *connect.Request[adminv1.RekeyRequest], _ *connect.ServerStream[adminv1.RekeyProgress]) error {
	return errStubRekey
}

func (*stubRekeyRPCHandler) HandleRekeyResume(_ context.Context, _ *connect.Request[adminv1.RekeyResumeRequest], _ *connect.ServerStream[adminv1.RekeyProgress]) error {
	return errStubRekey
}

func (*stubRekeyRPCHandler) HandleRekeyAbort(_ context.Context, _ *connect.Request[adminv1.RekeyAbortRequest]) (*connect.Response[adminv1.RekeyAbortResponse], error) {
	return nil, errStubRekey
}

func (*stubRekeyRPCHandler) HandleRekeyStatus(_ context.Context, _ *connect.Request[adminv1.RekeyStatusRequest]) (*connect.Response[adminv1.RekeyStatusResponse], error) {
	return nil, errStubRekey
}

func (*stubRekeyRPCHandler) HandleRekeyList(_ context.Context, _ *connect.Request[adminv1.RekeyListRequest], _ *connect.ServerStream[adminv1.RekeyStatusResponse]) error {
	return errStubRekey
}

func newTestSubsystemConfig(t *testing.T) AdminSocketSubsystemConfig {
	t.Helper()
	dir, err := os.MkdirTemp("", "hm-sub-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return AdminSocketSubsystemConfig{
		SocketPath: filepath.Join(dir, "a.sock"),
		LockPath:   filepath.Join(dir, "a.lock"),
		Version:    "test-subsystem-v1",
	}
}

// TestAdminSocketSubsystemIDReturnsAdminSocket verifies the subsystem ID.
func TestAdminSocketSubsystemIDReturnsAdminSocket(t *testing.T) {
	sub := NewAdminSocketSubsystem(newTestSubsystemConfig(t))
	assert.Equal(t, lifecycle.SubsystemAdminSocket, sub.ID())
}

// TestAdminSocketSubsystemDependsOnCryptoWiringSupersetPlusVerifier asserts
// the exact grown DependsOn set (07-09 items 8 + 9; renamed from
// TestAdminSocketSubsystemDependsOnNone, ACE) — THE RULE's wiring
// consumer superset {Database, Auth, ABAC, EventBus} plus CryptoChainVerifier
// (T-07-51 re-scope: admin.sock binds only after the chain walk has run).
func TestAdminSocketSubsystemDependsOnCryptoWiringSupersetPlusVerifier(t *testing.T) {
	sub := NewAdminSocketSubsystem(newTestSubsystemConfig(t))
	assert.Equal(t, []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemAuth,
		lifecycle.SubsystemABAC,
		lifecycle.SubsystemEventBus,
		lifecycle.SubsystemCryptoChainVerifier,
	}, sub.DependsOn())
}

// TestAdminSocketSubsystemActivateCreatesSocketAndStop verifies Prepare
// constructs the Server and Activate creates admin.sock; Stop removes it
// while admin.lock persists.
func TestAdminSocketSubsystemActivateCreatesSocketAndStop(t *testing.T) {
	cfg := newTestSubsystemConfig(t)
	sub := NewAdminSocketSubsystem(cfg)

	ctx := context.Background()
	require.NoError(t, sub.Prepare(ctx))
	require.NoError(t, sub.Activate(ctx))

	_, err := os.Stat(cfg.SocketPath)
	require.NoError(t, err, "admin.sock must exist after Activate")

	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, sub.Stop(stopCtx))

	_, err = os.Stat(cfg.SocketPath)
	assert.True(t, os.IsNotExist(err), "admin.sock must not exist after Stop")
	_, err = os.Stat(cfg.LockPath)
	assert.NoError(t, err, "admin.lock must persist after Stop")
}

// TestAdminSocketSubsystemActivateIsIdempotentWithFlock verifies that a
// second subsystem on the same paths returns ErrAdminSocketAlreadyHeld from
// Activate — admin.lock acquisition moved there (row 14; Prepare only
// constructs the Server, no live resources).
func TestAdminSocketSubsystemActivateIsIdempotentWithFlock(t *testing.T) {
	cfg := newTestSubsystemConfig(t)
	sub := NewAdminSocketSubsystem(cfg)

	require.NoError(t, sub.Prepare(context.Background()))
	require.NoError(t, sub.Activate(context.Background()))
	defer func() { _ = sub.Stop(context.Background()) }()

	sub2 := NewAdminSocketSubsystem(cfg)
	require.NoError(t, sub2.Prepare(context.Background()))
	err := sub2.Activate(context.Background())
	require.ErrorIs(t, err, ErrAdminSocketAlreadyHeld)
}

// TestAdminSocketSubsystemBothPhasesAreNoopWhenSocketPathEmpty verifies that
// Prepare AND Activate both return nil without constructing/starting a
// server when SocketPath is empty (XDG runtime dir unavailable at startup).
// Migrated to drive BOTH phases (cross-AI round 5): under the two-sweep
// orchestrator, Prepare's early return does NOT skip Activate — the
// orchestrator calls Activate on every subsystem regardless, and without
// the s.server == nil guard, srv.Start() would nil-panic the boot in
// exactly this degraded environment.
func TestAdminSocketSubsystemBothPhasesAreNoopWhenSocketPathEmpty(t *testing.T) {
	sub := NewAdminSocketSubsystem(AdminSocketSubsystemConfig{
		SocketPath: "", // intentionally empty
		LockPath:   "",
		Version:    "test",
	})
	require.NoError(t, sub.Prepare(context.Background()))
	assert.Nil(t, sub.server, "server must remain nil when SocketPath is empty")
	require.NoError(t, sub.Activate(context.Background()))
	assert.Nil(t, sub.server, "server must still be nil after Activate in disabled mode")
}

// TestAdminSocketSubsystemStopBeforePrepareReturnsNil verifies that Stop is
// safe to call on a subsystem that was never prepared (s.server == nil).
func TestAdminSocketSubsystemStopBeforePrepareReturnsNil(t *testing.T) {
	cfg := newTestSubsystemConfig(t)
	sub := NewAdminSocketSubsystem(cfg)
	require.NoError(t, sub.Stop(context.Background()))
}

// TestRunErrMonitor_InvokesShutdownOnServerError verifies that a non-nil error
// arriving on errCh causes the configured shutdown callback to fire with that
// error. This is the holomush-jxo8.9 fix: replace silent log-only behavior
// with parent-context cancel propagation, matching obsServer/controlGRPCServer
// at cmd/holomush/core.go:298,914 via monitorServerErrors.
func TestRunErrMonitor_InvokesShutdownOnServerError(t *testing.T) {
	errCh := make(chan error, 1)
	shutdownCh := make(chan error, 1)

	go runErrMonitor(errCh, func(err error) { shutdownCh <- err })

	sentinel := errors.New("admin socket accept loop died")
	errCh <- sentinel

	select {
	case got := <-shutdownCh:
		require.ErrorIs(t, got, sentinel)
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown callback was not invoked within 2s of error delivery")
	}
}

// TestRunErrMonitor_NilShutdownLogsOnly verifies the monitor tolerates a nil
// shutdown (test/dev wiring) and returns cleanly without panic.
func TestRunErrMonitor_NilShutdownLogsOnly(t *testing.T) {
	errCh := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		runErrMonitor(errCh, nil)
		close(done)
	}()

	errCh <- errors.New("test error with nil shutdown")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runErrMonitor did not return after error with nil shutdown")
	}
}

// TestRunErrMonitor_ChannelCloseReturnsWithoutShutdown verifies that closing
// errCh (the normal Stop path) returns the monitor goroutine without firing
// the shutdown callback. Stop is not a fatal error.
func TestRunErrMonitor_ChannelCloseReturnsWithoutShutdown(t *testing.T) {
	errCh := make(chan error)
	done := make(chan struct{})

	shutdownFired := false
	go func() {
		runErrMonitor(errCh, func(error) { shutdownFired = true })
		close(done)
	}()

	close(errCh)

	select {
	case <-done:
		assert.False(t, shutdownFired, "shutdown must NOT fire on normal channel close (Stop path)")
	case <-time.After(2 * time.Second):
		t.Fatal("runErrMonitor did not return after channel close")
	}
}

// TestAdminSocketSubsystemConfig_ShutdownFieldExists is a compile-time sanity
// check that the production wiring seam exists on the config struct. core.go
// at admin subsystem construction MUST populate this with a cancel-the-parent
// callback so post-startup server errors trigger graceful shutdown.
func TestAdminSocketSubsystemConfig_ShutdownFieldExists(t *testing.T) {
	cfg := AdminSocketSubsystemConfig{
		Shutdown: func(error) {},
	}
	require.NotNil(t, cfg.Shutdown)
}

// TestAdminSocketSubsystemForwardsRekeyHandlerToServerConfig verifies that
// the AdminSocketSubsystemConfig.RekeyHandler field is plumbed through into
// the underlying socket.Config when Prepare constructs the Server. This is
// the sub-epic E T44 production-wiring seam (holomush-jxo8.7.44): without it
// the Rekey RPCs are unreachable even when cmd/holomush builds a
// RekeyHandler. Prepare-only: server construction is enough to assert the
// wiring; Stop tolerates a never-activated server.
func TestAdminSocketSubsystemForwardsRekeyHandlerToServerConfig(t *testing.T) {
	cfg := newTestSubsystemConfig(t)
	cfg.RekeyHandler = &stubRekeyRPCHandler{}
	sub := NewAdminSocketSubsystem(cfg)

	require.NoError(t, sub.Prepare(context.Background()))
	defer func() { _ = sub.Stop(context.Background()) }()

	require.NotNil(t, sub.server, "Prepare must construct a Server")
	require.NotNil(t, sub.server.cfg.RekeyHandler,
		"AdminSocketSubsystemConfig.RekeyHandler must flow into socket.Config.RekeyHandler")
}

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

// TestAdminSocketSubsystemDependsOnNone verifies the substrate declares no
// subsystem dependencies.
func TestAdminSocketSubsystemDependsOnNone(t *testing.T) {
	sub := NewAdminSocketSubsystem(newTestSubsystemConfig(t))
	assert.Empty(t, sub.DependsOn())
}

// TestAdminSocketSubsystemStartCreatesSocketAndStop verifies Start creates
// admin.sock and Stop removes it while admin.lock persists.
func TestAdminSocketSubsystemStartCreatesSocketAndStop(t *testing.T) {
	cfg := newTestSubsystemConfig(t)
	sub := NewAdminSocketSubsystem(cfg)

	ctx := context.Background()
	require.NoError(t, sub.Start(ctx))

	_, err := os.Stat(cfg.SocketPath)
	require.NoError(t, err, "admin.sock must exist after Start")

	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, sub.Stop(stopCtx))

	_, err = os.Stat(cfg.SocketPath)
	assert.True(t, os.IsNotExist(err), "admin.sock must not exist after Stop")
	_, err = os.Stat(cfg.LockPath)
	assert.NoError(t, err, "admin.lock must persist after Stop")
}

// TestAdminSocketSubsystemStartIsIdempotentWithFlock verifies that a second
// subsystem on the same paths returns ErrAdminSocketAlreadyHeld from Start.
func TestAdminSocketSubsystemStartIsIdempotentWithFlock(t *testing.T) {
	cfg := newTestSubsystemConfig(t)
	sub := NewAdminSocketSubsystem(cfg)

	require.NoError(t, sub.Start(context.Background()))
	defer func() { _ = sub.Stop(context.Background()) }()

	sub2 := NewAdminSocketSubsystem(cfg)
	err := sub2.Start(context.Background())
	require.ErrorIs(t, err, ErrAdminSocketAlreadyHeld)
}

// TestAdminSocketSubsystemStartIsNoopWhenSocketPathEmpty verifies that Start
// returns nil without starting a server when SocketPath is empty (XDG runtime
// dir unavailable at startup).
func TestAdminSocketSubsystemStartIsNoopWhenSocketPathEmpty(t *testing.T) {
	sub := NewAdminSocketSubsystem(AdminSocketSubsystemConfig{
		SocketPath: "", // intentionally empty
		LockPath:   "",
		Version:    "test",
	})
	require.NoError(t, sub.Start(context.Background()))
	assert.Nil(t, sub.server, "server must remain nil when SocketPath is empty")
}

// TestAdminSocketSubsystemStopBeforeStartReturnsNil verifies that Stop is
// safe to call on a subsystem that was never started (s.server == nil).
func TestAdminSocketSubsystemStopBeforeStartReturnsNil(t *testing.T) {
	cfg := newTestSubsystemConfig(t)
	sub := NewAdminSocketSubsystem(cfg)
	require.NoError(t, sub.Stop(context.Background()))
}

// TestAdminSocketSubsystemForwardsRekeyHandlerToServerConfig verifies that
// the AdminSocketSubsystemConfig.RekeyHandler field is plumbed through into
// the underlying socket.Config when Start constructs the Server. This is the
// sub-epic E T44 production-wiring seam (holomush-jxo8.7.44): without it the
// Rekey RPCs are unreachable even when cmd/holomush builds a RekeyHandler.
func TestAdminSocketSubsystemForwardsRekeyHandlerToServerConfig(t *testing.T) {
	cfg := newTestSubsystemConfig(t)
	cfg.RekeyHandler = &stubRekeyRPCHandler{}
	sub := NewAdminSocketSubsystem(cfg)

	require.NoError(t, sub.Start(context.Background()))
	defer func() { _ = sub.Stop(context.Background()) }()

	require.NotNil(t, sub.server, "Start must construct a Server")
	require.NotNil(t, sub.server.cfg.RekeyHandler,
		"AdminSocketSubsystemConfig.RekeyHandler must flow into socket.Config.RekeyHandler")
}

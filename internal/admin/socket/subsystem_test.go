// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
)

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

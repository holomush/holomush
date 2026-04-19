// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
)

// TestGRPCSubsystemImplementsSubsystem is a compile-time interface check.
func TestGRPCSubsystemImplementsSubsystem(_ *testing.T) {
	var _ lifecycle.Subsystem = (*grpcSubsystem)(nil)
	// If this compiles, the interface is satisfied.
}

// TestGRPCSubsystemIDReturnsGRPC verifies that ID() returns SubsystemGRPC.
func TestGRPCSubsystemIDReturnsGRPC(t *testing.T) {
	s := newGRPCSubsystem(grpcSubsystemConfig{})

	assert.Equal(t, lifecycle.SubsystemGRPC, s.ID())
}

// TestGRPCSubsystemDependsOnExpectedSubsystems verifies that DependsOn returns
// exactly 4 dependencies: Bootstrap, Sessions, Auth, and EventBus.
// EventBus was added in the F1 cutover: gRPC Start() reads the eventbus
// Publisher when wiring the shared plugin event emitter.
func TestGRPCSubsystemDependsOnExpectedSubsystems(t *testing.T) {
	s := newGRPCSubsystem(grpcSubsystemConfig{})

	deps := s.DependsOn()

	require.Len(t, deps, 4)
	assert.Contains(t, deps, lifecycle.SubsystemBootstrap)
	assert.Contains(t, deps, lifecycle.SubsystemSessions)
	assert.Contains(t, deps, lifecycle.SubsystemAuth)
	assert.Contains(t, deps, lifecycle.SubsystemEventBus)
}

// TestGRPCSubsystemStopBeforeStartIsSafe verifies that calling Stop on a
// subsystem that was never started returns nil without panicking.
func TestGRPCSubsystemStopBeforeStartIsSafe(t *testing.T) {
	s := newGRPCSubsystem(grpcSubsystemConfig{})

	err := s.Stop(context.Background())

	require.NoError(t, err)
}

// TestGRPCSubsystemStopWithTimeoutDoesNotHang verifies that Stop respects
// context deadline and returns before the deadline expires.
func TestGRPCSubsystemStopWithTimeoutDoesNotHang(t *testing.T) {
	s := newGRPCSubsystem(grpcSubsystemConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.Stop(ctx)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("Stop() did not return within context deadline")
	}
}

// TestGRPCSubsystemReaperCancelNilSafe verifies that a nil reaperCancel field
// does not cause a panic when Stop is called.
func TestGRPCSubsystemReaperCancelNilSafe(t *testing.T) {
	s := newGRPCSubsystem(grpcSubsystemConfig{})
	s.reaperCancel = nil

	assert.NotPanics(t, func() {
		_ = s.Stop(context.Background())
	})
}

// TestNewGRPCSubsystemStoresConfig verifies that newGRPCSubsystem stores the
// provided configuration for use by Start.
func TestNewGRPCSubsystemStoresConfig(t *testing.T) {
	cfg := grpcSubsystemConfig{
		GRPCAddr:   "localhost:9000",
		MaxHistory: 42,
	}

	s := newGRPCSubsystem(cfg)

	assert.Equal(t, cfg.GRPCAddr, s.cfg.GRPCAddr)
	assert.Equal(t, cfg.MaxHistory, s.cfg.MaxHistory)
}

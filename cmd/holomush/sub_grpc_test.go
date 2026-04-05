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

// TestGRPCSubsystem_ImplementsSubsystem is a compile-time check that
// grpcSubsystem satisfies the lifecycle.Subsystem interface.
func TestGRPCSubsystem_ImplementsSubsystem(_ *testing.T) {
	var _ lifecycle.Subsystem = (*grpcSubsystem)(nil)
}

// TestGRPCSubsystem_ID verifies that ID() returns SubsystemGRPC.
func TestGRPCSubsystem_ID(t *testing.T) {
	sub := newGRPCSubsystem(grpcSubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemGRPC, sub.ID())
}

// TestGRPCSubsystem_DependsOn verifies the expected dependency set.
func TestGRPCSubsystem_DependsOn(t *testing.T) {
	sub := newGRPCSubsystem(grpcSubsystemConfig{})
	deps := sub.DependsOn()

	require.Len(t, deps, 3, "grpcSubsystem should declare exactly three dependencies")
	assert.Contains(t, deps, lifecycle.SubsystemBootstrap)
	assert.Contains(t, deps, lifecycle.SubsystemSessions)
	assert.Contains(t, deps, lifecycle.SubsystemAuth)
}

// TestGRPCSubsystem_StopBeforeStart is safe and returns nil.
func TestGRPCSubsystem_StopBeforeStart(t *testing.T) {
	sub := newGRPCSubsystem(grpcSubsystemConfig{})
	err := sub.Stop(context.Background())
	assert.NoError(t, err, "Stop before Start must not return an error")
}

// TestGRPCSubsystem_StopWithTimeout verifies Stop respects the context deadline
// and forces a hard Stop when the graceful shutdown window expires.
//
// This test is purely structural — it creates a grpcSubsystem with a fake
// grpcServer field (nil, so nothing is actually listening) and verifies that
// Stop returns without hanging or panicking.
func TestGRPCSubsystem_StopWithTimeout(t *testing.T) {
	sub := newGRPCSubsystem(grpcSubsystemConfig{})
	// grpcServer == nil, so Stop is a no-op — should return quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := sub.Stop(ctx)
	assert.NoError(t, err)
}

// TestGRPCSubsystem_ReaperCancelNilSafe verifies that Stop handles nil reaperCancel gracefully.
func TestGRPCSubsystem_ReaperCancelNilSafe(t *testing.T) {
	sub := newGRPCSubsystem(grpcSubsystemConfig{})
	assert.Nil(t, sub.reaperCancel, "reaperCancel should be nil before Start")

	// Calling Stop must not panic even when reaperCancel is nil.
	require.NotPanics(t, func() {
		_ = sub.Stop(context.Background())
	})
}

// TestNewGRPCSubsystem_ConfigIsStored verifies that the configuration is retained.
func TestNewGRPCSubsystem_ConfigIsStored(t *testing.T) {
	cfg := grpcSubsystemConfig{
		GRPCAddr:       "localhost:1234",
		SessionTTL:     5 * time.Minute,
		ReaperInterval: 30 * time.Second,
		MaxHistory:     100,
	}
	sub := newGRPCSubsystem(cfg)
	require.NotNil(t, sub)
	assert.Equal(t, "localhost:1234", sub.cfg.GRPCAddr)
	assert.Equal(t, 5*time.Minute, sub.cfg.SessionTTL)
	assert.Equal(t, 30*time.Second, sub.cfg.ReaperInterval)
	assert.Equal(t, 100, sub.cfg.MaxHistory)
}
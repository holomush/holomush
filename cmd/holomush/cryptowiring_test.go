// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/admin/policy"
	socket "github.com/holomush/holomush/internal/admin/socket"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
	"github.com/holomush/holomush/internal/lifecycle"
)

// wiringDepSet is THE RULE (07-09 <settlements>): every subsystem whose
// config holds a cryptoWiring provider MUST declare DependsOn as a superset
// of this set — the FIRST consumer to resolve the provider is the one that
// builds it, so a missing edge is a boot panic.
var wiringDepSet = []lifecycle.SubsystemID{
	lifecycle.SubsystemDatabase,
	lifecycle.SubsystemAuth,
	lifecycle.SubsystemABAC,
	lifecycle.SubsystemEventBus,
}

// dependsOnSuperset asserts that got is a superset of want.
func dependsOnSuperset(t *testing.T, name string, got []lifecycle.SubsystemID, want []lifecycle.SubsystemID) {
	t.Helper()
	set := make(map[lifecycle.SubsystemID]bool, len(got))
	for _, id := range got {
		set[id] = true
	}
	for _, id := range want {
		if !set[id] {
			t.Errorf("%s.DependsOn() = %v; missing required cryptoWiring dependency %s", name, got, id.String())
		}
	}
}

// TestCryptoWiringConsumersDeclareRequiredDependsOnSuperset is THE RULE's
// mechanical guard (07-09 item 9): each of the five cryptoWiring consumers
// declares DependsOn ⊇ {Database, Auth, ABAC, EventBus}.
func TestCryptoWiringConsumersDeclareRequiredDependsOnSuperset(t *testing.T) {
	policySub := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{})
	dependsOnSuperset(t, "policy.CryptoPolicySubsystem", policySub.DependsOn(), wiringDepSet)

	sweepSub := dek.NewCheckpointSweepSubsystem(dek.CheckpointSweepConfig{})
	dependsOnSuperset(t, "dek.CheckpointSweepSubsystem", sweepSub.DependsOn(), wiringDepSet)

	verifierSub := chain.NewVerifierSubsystem(chain.VerifierSubsystemConfig{})
	dependsOnSuperset(t, "chain.VerifierSubsystem", verifierSub.DependsOn(), wiringDepSet)

	adminSub := socket.NewAdminSocketSubsystem(socket.AdminSocketSubsystemConfig{})
	dependsOnSuperset(t, "socket.AdminSocketSubsystem", adminSub.DependsOn(), wiringDepSet)

	grpcSub := newGRPCSubsystem(grpcSubsystemConfig{})
	dependsOnSuperset(t, "grpcSubsystem", grpcSub.DependsOn(), wiringDepSet)
}

// fakeCoordinator is a minimal invalidation.Coordinator test double for
// TestStopCoordinatorOnBootFailure*: it only records whether Stop was
// called and lets the test control the returned error.
type fakeCoordinator struct {
	stopCalled bool
	stopErr    error
}

var _ invalidation.Coordinator = (*fakeCoordinator)(nil)

func (f *fakeCoordinator) Start(_ context.Context) error { return nil }

func (f *fakeCoordinator) Stop(_ context.Context) error {
	f.stopCalled = true
	return f.stopErr
}

func (f *fakeCoordinator) RequestInvalidation(_ context.Context, _ dek.ContextID, _ invalidation.Action, _, _ uint32) error {
	return nil
}

// TestStopCoordinatorOnBootFailure covers CR-01 (07-review): the
// invalidation.Coordinator's only orchestrator-owned cleanup path is
// grpcSubsystem.Stop, which is unreachable when a boot failure occurs
// before grpcSubsystem.Prepare runs. stopCoordinatorOnBootFailure is the
// supplementary cleanup runCore invokes on every orch.StartAll failure.
func TestStopCoordinatorOnBootFailure(t *testing.T) {
	t.Run("stops a coordinator that was constructed and started", func(t *testing.T) {
		coord := &fakeCoordinator{}
		holder := &coordHolder{coord: coord}

		stopCoordinatorOnBootFailure(context.Background(), holder)

		assert.True(t, coord.stopCalled, "Stop must be called on the Coordinator held by holder")
	})

	t.Run("does not panic when the coordinator was never started", func(t *testing.T) {
		holder := &coordHolder{}

		assert.NotPanics(t, func() {
			stopCoordinatorOnBootFailure(context.Background(), holder)
		}, "a nil holder.coord (Coordinator never constructed, e.g. no KEK) must be a no-op")
	})

	t.Run("does not panic when holder itself is nil", func(t *testing.T) {
		assert.NotPanics(t, func() {
			stopCoordinatorOnBootFailure(context.Background(), nil)
		}, "a nil holder (cryptoWiringFn never resolved) must be a no-op")
	})

	t.Run("logs but does not propagate a Stop error", func(t *testing.T) {
		stopErr := assert.AnError
		coord := &fakeCoordinator{stopErr: stopErr}
		holder := &coordHolder{coord: coord}

		assert.NotPanics(t, func() {
			stopCoordinatorOnBootFailure(context.Background(), holder)
		})
		assert.True(t, coord.stopCalled, "Stop must still be attempted even though it returns an error")
	})
}

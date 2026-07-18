// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/holomush/holomush/internal/admin/policy"
	socket "github.com/holomush/holomush/internal/admin/socket"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
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

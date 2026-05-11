// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package main

import (
	"context"
	"testing"

	"github.com/holomush/holomush/internal/lifecycle"
)

// stubSubsystem is a minimal lifecycle.Subsystem for testing the
// productionSubsystems helper. Only ID() is read by the test.
type stubSubsystem struct {
	id lifecycle.SubsystemID
}

func (s stubSubsystem) ID() lifecycle.SubsystemID          { return s.id }
func (s stubSubsystem) DependsOn() []lifecycle.SubsystemID { return nil }
func (s stubSubsystem) Start(_ context.Context) error      { return nil }
func (s stubSubsystem) Stop(_ context.Context) error       { return nil }

// allStubs returns the full 15-element stub list in production order.
// Callers that only care about presence can use this; callers that care about
// position should build the slice inline so the ordering is explicit.
//
// Index 14 (SubsystemRekeyCheckpointSweep) was added in sub-epic E Task 6.
// The production-wiring assertion for the sweep subsystem lands in Task 37.
func allStubs() [15]stubSubsystem {
	return [15]stubSubsystem{
		{id: lifecycle.SubsystemDatabase},
		{id: lifecycle.SubsystemABAC},
		{id: lifecycle.SubsystemAuth},
		{id: lifecycle.SubsystemWorld},
		{id: lifecycle.SubsystemSessions},
		{id: lifecycle.SubsystemPlugins},
		{id: lifecycle.SubsystemBootstrap},
		{id: lifecycle.SubsystemCryptoChainVerifier},
		{id: lifecycle.SubsystemEventBus},
		{id: lifecycle.SubsystemCluster},
		{id: lifecycle.SubsystemAuditProjection},
		{id: lifecycle.SubsystemCryptoPolicy},
		{id: lifecycle.SubsystemGRPC},
		{id: lifecycle.SubsystemAdminSocket},
		{id: lifecycle.SubsystemRekeyCheckpointSweep},
	}
}

// TestProductionSubsystemsIncludesCluster asserts that the production
// subsystem slice includes SubsystemCluster.
func TestProductionSubsystemsIncludesCluster(t *testing.T) {
	s := allStubs()
	subs := productionSubsystems(
		s[0], s[1], s[2], s[3], s[4], s[5], s[6],
		s[7], s[8], s[9], s[10], s[11], s[12], s[13],
	)

	found := false
	for _, sub := range subs {
		if sub.ID() == lifecycle.SubsystemCluster {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("productionSubsystems does not include SubsystemCluster")
	}
	if len(subs) != 14 {
		t.Errorf("productionSubsystems returned %d subsystems; want 14 after Phase 5 sub-epic D T22", len(subs))
	}
}

// TestSubsystemAdminSocketConstantExists verifies that SubsystemAdminSocket
// is defined and distinct from all other SubsystemIDs.
func TestSubsystemAdminSocketConstantExists(t *testing.T) {
	ids := []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemTLS,
		lifecycle.SubsystemABAC,
		lifecycle.SubsystemAuth,
		lifecycle.SubsystemWorld,
		lifecycle.SubsystemPlugins,
		lifecycle.SubsystemSessions,
		lifecycle.SubsystemBootstrap,
		lifecycle.SubsystemGRPC,
		lifecycle.SubsystemEventBus,
		lifecycle.SubsystemAuditProjection,
		lifecycle.SubsystemCluster,
		lifecycle.SubsystemAdminSocket,
		lifecycle.SubsystemCryptoChainVerifier,
		lifecycle.SubsystemCryptoPolicy,
		lifecycle.SubsystemRekeyCheckpointSweep,
	}
	seen := make(map[lifecycle.SubsystemID]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate SubsystemID value: %v", id)
		}
		seen[id] = true
	}
	if lifecycle.SubsystemAdminSocket.String() == "" {
		t.Error("SubsystemAdminSocket.String() must not be empty")
	}
}

// TestProductionSubsystemsIncludesAdminSocket verifies AC-C10: the admin
// socket subsystem is present in the production orchestrator slice.
func TestProductionSubsystemsIncludesAdminSocket(t *testing.T) {
	s := allStubs()
	subs := productionSubsystems(
		s[0], s[1], s[2], s[3], s[4], s[5], s[6],
		s[7], s[8], s[9], s[10], s[11], s[12], s[13],
	)

	found := false
	for _, sub := range subs {
		if sub.ID() == lifecycle.SubsystemAdminSocket {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("productionSubsystems does not include SubsystemAdminSocket")
	}
}

// TestProductionSubsystemsIncludesCryptoChainVerifier verifies that
// CryptoChainVerifier appears between Bootstrap and EventBus in the slice.
func TestProductionSubsystemsIncludesCryptoChainVerifier(t *testing.T) {
	s := allStubs()
	subs := productionSubsystems(
		s[0], s[1], s[2], s[3], s[4], s[5], s[6],
		s[7], s[8], s[9], s[10], s[11], s[12], s[13],
	)

	bootstrapIdx := -1
	verifierIdx := -1
	eventBusIdx := -1
	for i, sub := range subs {
		switch sub.ID() {
		case lifecycle.SubsystemBootstrap:
			bootstrapIdx = i
		case lifecycle.SubsystemCryptoChainVerifier:
			verifierIdx = i
		case lifecycle.SubsystemEventBus:
			eventBusIdx = i
		}
	}
	if verifierIdx < 0 {
		t.Fatal("productionSubsystems does not include SubsystemCryptoChainVerifier")
	}
	if bootstrapIdx < 0 || eventBusIdx < 0 {
		t.Fatal("productionSubsystems missing Bootstrap or EventBus for ordering check")
	}
	if bootstrapIdx >= verifierIdx || verifierIdx >= eventBusIdx {
		t.Errorf("ordering violated: Bootstrap(%d) < CryptoChainVerifier(%d) < EventBus(%d)",
			bootstrapIdx, verifierIdx, eventBusIdx)
	}
}

// TestProductionSubsystemsIncludesCryptoPolicy verifies that CryptoPolicy
// appears between AuditProjection and gRPC in the slice.
func TestProductionSubsystemsIncludesCryptoPolicy(t *testing.T) {
	s := allStubs()
	subs := productionSubsystems(
		s[0], s[1], s[2], s[3], s[4], s[5], s[6],
		s[7], s[8], s[9], s[10], s[11], s[12], s[13],
	)

	auditIdx := -1
	policyIdx := -1
	grpcIdx := -1
	for i, sub := range subs {
		switch sub.ID() {
		case lifecycle.SubsystemAuditProjection:
			auditIdx = i
		case lifecycle.SubsystemCryptoPolicy:
			policyIdx = i
		case lifecycle.SubsystemGRPC:
			grpcIdx = i
		}
	}
	if policyIdx < 0 {
		t.Fatal("productionSubsystems does not include SubsystemCryptoPolicy")
	}
	if auditIdx < 0 || grpcIdx < 0 {
		t.Fatal("productionSubsystems missing AuditProjection or GRPC for ordering check")
	}
	if auditIdx >= policyIdx || policyIdx >= grpcIdx {
		t.Errorf("ordering violated: AuditProjection(%d) < CryptoPolicy(%d) < GRPC(%d)",
			auditIdx, policyIdx, grpcIdx)
	}
}

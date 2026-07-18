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

// allStubs returns the full 17-element stub list in production order.
// Callers that only care about presence can use this; callers that care about
// position should build the slice inline so the ordering is explicit.
//
// Index 1 (SubsystemTLS) was added in 07-09 Task 3 (D-12 Wave A, LOW-8):
// productionSubsystems switched from a 16-position positional parameter list
// to the named productionSubsystemSet struct, and TLS finally registers as a
// real subsystem.
// Index 14 (SubsystemRekeyCheckpointSweep) was added in sub-epic E Task 6.
// Index 15 (SubsystemOutboxRelay) was added in Phase 5 05-07 (MODEL-04 relay).
func allStubs() [17]stubSubsystem {
	return [17]stubSubsystem{
		{id: lifecycle.SubsystemDatabase},
		{id: lifecycle.SubsystemTLS},
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
		{id: lifecycle.SubsystemOutboxRelay},
	}
}

// setFromStubs builds a productionSubsystemSet from allStubs()'s 17-element
// array, mirroring the field order documented on allStubs.
func setFromStubs(s [17]stubSubsystem) productionSubsystemSet {
	return productionSubsystemSet{
		Database:             s[0],
		TLS:                  s[1],
		ABAC:                 s[2],
		Auth:                 s[3],
		World:                s[4],
		Sessions:             s[5],
		Plugins:              s[6],
		Bootstrap:            s[7],
		CryptoChainVerifier:  s[8],
		EventBus:             s[9],
		Cluster:              s[10],
		AuditProjection:      s[11],
		CryptoPolicy:         s[12],
		GRPC:                 s[13],
		AdminSocket:          s[14],
		RekeyCheckpointSweep: s[15],
		OutboxRelay:          s[16],
	}
}

// TestProductionSubsystemsIncludesCluster asserts that the production
// subsystem slice includes SubsystemCluster.
func TestProductionSubsystemsIncludesCluster(t *testing.T) {
	subs := productionSubsystems(setFromStubs(allStubs()))

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
	if len(subs) != 17 {
		t.Errorf("productionSubsystems returned %d subsystems; want 17 after 07-09 TLS registration", len(subs))
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
		lifecycle.SubsystemOutboxRelay,
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
	subs := productionSubsystems(setFromStubs(allStubs()))

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

// TestProductionSubsystemsIncludesTLS verifies 07-09 Task 3: TLS is finally
// registered in the production orchestrator slice.
func TestProductionSubsystemsIncludesTLS(t *testing.T) {
	subs := productionSubsystems(setFromStubs(allStubs()))

	found := false
	for _, sub := range subs {
		if sub.ID() == lifecycle.SubsystemTLS {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("productionSubsystems does not include SubsystemTLS")
	}
}

// TestProductionSubsystemsIncludesCryptoChainVerifier verifies that
// CryptoChainVerifier appears between Bootstrap and EventBus in the slice.
func TestProductionSubsystemsIncludesCryptoChainVerifier(t *testing.T) {
	subs := productionSubsystems(setFromStubs(allStubs()))

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
	subs := productionSubsystems(setFromStubs(allStubs()))

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

// TestProductionSubsystemsIncludesRekeyCheckpointSweep verifies that
// RekeyCheckpointSweep is present AND positioned after CryptoChainVerifier,
// EventBus, and AuditProjection per Task 28's DependsOn declaration
// (sub-epic E T37 / holomush-jxo8.7.34).
func TestProductionSubsystemsIncludesRekeyCheckpointSweep(t *testing.T) {
	subs := productionSubsystems(setFromStubs(allStubs()))

	indexOf := func(id lifecycle.SubsystemID) int {
		for i, sub := range subs {
			if sub.ID() == id {
				return i
			}
		}
		return -1
	}
	sweepIdx := indexOf(lifecycle.SubsystemRekeyCheckpointSweep)
	chainIdx := indexOf(lifecycle.SubsystemCryptoChainVerifier)
	eventBusIdx := indexOf(lifecycle.SubsystemEventBus)
	auditProjIdx := indexOf(lifecycle.SubsystemAuditProjection)

	if sweepIdx < 0 {
		t.Fatal("productionSubsystems does not include SubsystemRekeyCheckpointSweep")
	}
	if sweepIdx <= chainIdx {
		t.Errorf("sweep (%d) must run after CryptoChainVerifier (%d)", sweepIdx, chainIdx)
	}
	if sweepIdx <= eventBusIdx {
		t.Errorf("sweep (%d) must run after EventBus (%d)", sweepIdx, eventBusIdx)
	}
	if sweepIdx <= auditProjIdx {
		t.Errorf("sweep (%d) must run after AuditProjection (%d)", sweepIdx, auditProjIdx)
	}
	if len(subs) != 17 {
		t.Errorf("productionSubsystems returned %d subsystems; want 17 after 07-09 TLS registration", len(subs))
	}
}

// TestProductionSubsystemsIncludesOutboxRelayAfterEventBus verifies the
// MODEL-04 relay (05-07) is present AND positioned after EventBus + Database
// (its declared dependencies).
func TestProductionSubsystemsIncludesOutboxRelayAfterEventBus(t *testing.T) {
	subs := productionSubsystems(setFromStubs(allStubs()))

	indexOf := func(id lifecycle.SubsystemID) int {
		for i, sub := range subs {
			if sub.ID() == id {
				return i
			}
		}
		return -1
	}
	relayIdx := indexOf(lifecycle.SubsystemOutboxRelay)
	eventBusIdx := indexOf(lifecycle.SubsystemEventBus)
	dbIdx := indexOf(lifecycle.SubsystemDatabase)

	if relayIdx < 0 {
		t.Fatal("productionSubsystems does not include SubsystemOutboxRelay")
	}
	if eventBusIdx < 0 || dbIdx < 0 {
		t.Fatal("productionSubsystems missing Database or EventBus for ordering check")
	}
	if relayIdx <= eventBusIdx {
		t.Errorf("outbox relay (%d) must run after EventBus (%d)", relayIdx, eventBusIdx)
	}
	if relayIdx <= dbIdx {
		t.Errorf("outbox relay (%d) must run after Database (%d)", relayIdx, dbIdx)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package main

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	abacsetup "github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/admin/socket"
	authsetup "github.com/holomush/holomush/internal/auth/setup"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/lifecycle"
	pluginsetup "github.com/holomush/holomush/internal/plugin/setup"
	sessionsetup "github.com/holomush/holomush/internal/session/setup"
	"github.com/holomush/holomush/internal/store"
	tlscerts "github.com/holomush/holomush/internal/tls"
	worldsetup "github.com/holomush/holomush/internal/world/setup"
)

// stubSubsystem is a minimal lifecycle.Subsystem for testing the
// productionSubsystems helper. Only ID() is read by the test.
type stubSubsystem struct {
	id lifecycle.SubsystemID
}

func (s stubSubsystem) ID() lifecycle.SubsystemID          { return s.id }
func (s stubSubsystem) DependsOn() []lifecycle.SubsystemID { return nil }
func (s stubSubsystem) Prepare(_ context.Context) error    { return nil }
func (s stubSubsystem) Activate(_ context.Context) error   { return nil }
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

// phaseCallLog records a (phase, id) call in the order it happened,
// guarded by a mutex — the property test below asserts over the FULL
// interleaving of Prepare and Activate calls across the real production
// graph, not just per-subsystem ordering.
type phaseCallLog struct {
	mu    sync.Mutex
	calls []phaseCallEntry
}

type phaseCallEntry struct {
	phase string
	id    lifecycle.SubsystemID
}

func (l *phaseCallLog) record(phase string, id lifecycle.SubsystemID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, phaseCallEntry{phase: phase, id: id})
}

func (l *phaseCallLog) snapshot() []phaseCallEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]phaseCallEntry, len(l.calls))
	copy(out, l.calls)
	return out
}

// phaseRecordingStub is a lifecycle.Subsystem whose ID/DependsOn are sourced
// from a REAL production subsystem type (realProductionSubsystemGraph, the
// same sourcing core_topo_order_test.go's pin uses — never a hand-copied
// dep list) and whose Prepare/Activate both record into a SHARED log, so
// the property test below can assert over the full cross-subsystem
// interleaving.
type phaseRecordingStub struct {
	id   lifecycle.SubsystemID
	deps []lifecycle.SubsystemID
	log  *phaseCallLog
}

func (s *phaseRecordingStub) ID() lifecycle.SubsystemID          { return s.id }
func (s *phaseRecordingStub) DependsOn() []lifecycle.SubsystemID { return s.deps }
func (s *phaseRecordingStub) Prepare(_ context.Context) error {
	s.log.record("Prepare", s.id)
	return nil
}

func (s *phaseRecordingStub) Activate(_ context.Context) error {
	s.log.record("Activate", s.id)
	return nil
}
func (s *phaseRecordingStub) Stop(_ context.Context) error { return nil }

// realProductionSubsystemGraphForPropertyTest constructs every one of the 17
// production subsystem types with a minimal/zero-value config and reads
// each one's real DependsOn() LIVE — the identical construction
// realProductionSubsystemGraph (core_topo_order_test.go) uses, duplicated
// here only because that helper returns a plain dep map, not constructed
// subsystem instances, and both call sites independently need "one
// authoritative list of the 17 production types" without hand-copying deps
// between them.
func realProductionSubsystemGraphForPropertyTest(t *testing.T) map[lifecycle.SubsystemID][]lifecycle.SubsystemID {
	t.Helper()

	pill, _ := cluster.NewTestPill()
	clusterSub, err := cluster.NewSubsystem(
		cluster.Config{ClusterID: "core-subsystems-property-test"},
		cluster.Deps{Pill: pill},
	)
	require.NoError(t, err, "cluster.NewSubsystem must construct without touching live resources")

	subs := []lifecycle.Subsystem{
		store.NewSubsystem(store.SubsystemConfig{}),
		tlscerts.NewTLSSubsystem(tlscerts.TLSSubsystemConfig{}),
		abacsetup.NewABACSubsystem(abacsetup.ABACSubsystemConfig{}),
		authsetup.NewAuthSubsystem(authsetup.AuthSubsystemConfig{}),
		worldsetup.NewWorldSubsystem(worldsetup.WorldSubsystemConfig{}),
		sessionsetup.NewSessionSubsystem(sessionsetup.SessionSubsystemConfig{}),
		pluginsetup.NewPluginSubsystem(pluginsetup.PluginSubsystemConfig{}),
		bootstrapsetup.NewBootstrapSubsystem(bootstrapsetup.BootstrapSubsystemConfig{}),
		chain.NewVerifierSubsystem(chain.VerifierSubsystemConfig{}),
		eventbus.NewSubsystem(eventbus.Config{}),
		clusterSub,
		audit.NewSubsystem(nil, nil, audit.Config{}),
		policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{}),
		newGRPCSubsystem(grpcSubsystemConfig{}),
		socket.NewAdminSocketSubsystem(socket.AdminSocketSubsystemConfig{}),
		dek.NewCheckpointSweepSubsystem(dek.CheckpointSweepConfig{}),
		worldsetup.NewOutboxRelaySubsystem(worldsetup.OutboxRelaySubsystemConfig{}),
	}

	graph := make(map[lifecycle.SubsystemID][]lifecycle.SubsystemID, len(subs))
	for _, s := range subs {
		graph[s.ID()] = s.DependsOn()
	}
	require.Len(t, graph, 17,
		"expected exactly the 17 production subsystems (productionSubsystemSet); "+
			"a subsystem was added or removed without updating this test's construction list")
	return graph
}

// TestStartAllActivatesNothingUntilEverySubsystemHasPrepared is D-11's
// whole guarantee, stated as an executable property over the REAL
// production dependency graph rather than per-subsystem inspection.
//
// grpcSubsystem.DependsOn() once excluded AuditProjection, so gRPC could
// serve before the audit projection was up (07-10 fixed that ONE edge).
// This property makes the entire CLASS of bug unrepresentable: a future
// subsystem that forgets a DependsOn edge still cannot serve before every
// subsystem has finished acquiring, because the two-sweep orchestrator
// never calls any Activate until every Prepare in the whole graph has
// returned — independent of which edges exist.
func TestStartAllActivatesNothingUntilEverySubsystemHasPrepared(t *testing.T) {
	graph := realProductionSubsystemGraphForPropertyTest(t)
	log := &phaseCallLog{}

	orch := lifecycle.NewOrchestrator()
	for id, deps := range graph {
		orch.Register(&phaseRecordingStub{id: id, deps: deps, log: log})
	}

	require.NoError(t, orch.StartAll(context.Background()))

	calls := log.snapshot()
	require.Len(t, calls, 2*len(graph), "expected one Prepare + one Activate per registered subsystem")

	lastPrepareIdx := -1
	firstActivateIdx := -1
	for i, c := range calls {
		switch c.phase {
		case "Prepare":
			lastPrepareIdx = i
		case "Activate":
			if firstActivateIdx == -1 {
				firstActivateIdx = i
			}
		}
	}
	require.NotEqual(t, -1, lastPrepareIdx)
	require.NotEqual(t, -1, firstActivateIdx)
	require.Less(t, lastPrepareIdx, firstActivateIdx,
		"no Activate may be observed before the LAST Prepare across the real production graph — "+
			"this is D-11's guarantee as an executable property, not per-subsystem inspection")
}

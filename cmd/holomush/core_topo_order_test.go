// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package main

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
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

// This file exists because of an incident, not a feature. Rev 4 of Phase 7
// shipped a plan pair (07-09 + 07-10) whose combined effect was
// EventBus -> CryptoChainVerifier -> EventBus: 07-09 added
// "Verifier depends on EventBus" (its cryptoWiring consumer rule — the
// verifier's chain-handler set is built from eventBusSub.Publisher()), and
// 07-10's original revision added the reverse edge "EventBus depends on
// CryptoChainVerifier" to encode a since-retracted reading of MEDIUM-11.
// Every existing gate passed: 07-09's superset-DependsOn unit test passed
// (each consumer DID declare the required set), `task test` passed, and the
// plan checker returned 0 blockers. The defect was a CROSS-PLAN interaction
// invisible to any single-plan proof — topoSort refuses the cycle only at
// runtime, and no unit test exercised the REAL, post-07-09 production graph
// through it. See <settlements> § MEDIUM-11 in 07-10-PLAN.md for the full
// derivation.
//
// TestProductionSubsystemsTopologicalStartOrderIsPinned (Task 3) and
// TestProductionSubsystemGraphIsAcyclic (Task 4) are the durable fix: both
// read every subsystem's DependsOn() LIVE from the real production types
// (never a hand-copied list — a hand-copied list is exactly what let rev 4's
// cycle hide) and observe topoSort's behavior only through the public
// Orchestrator.StartAll seam, since topoSort itself is an unexported method
// unreachable from package main (cross-AI round 6, BLOCKER). No exported
// ordering/validation seam was added to internal/lifecycle (D-10: topoSort
// stays the one ordering authority).

// depStubSubsystem is a lifecycle.Subsystem whose ID and DependsOn are
// sourced from a REAL production subsystem type (see
// realProductionSubsystemGraph below), and whose Start records its own ID
// into a shared, mutex-guarded order slice instead of doing any real work.
// Because Orchestrator.StartAll starts subsystems in exactly topoSort's
// order (internal/lifecycle/orchestrator.go), the recorded start order IS
// the topological order, byte for byte.
//
// Deliberately NOT the nil-deps stub type declared in core_subsystems_test.go
// for the productionSubsystems slice-shape tests: that type's DependsOn()
// always returns nil, which would pin a tautological order derived from an
// empty graph.
type depStubSubsystem struct {
	id   lifecycle.SubsystemID
	deps []lifecycle.SubsystemID
	rec  *startRecorder
}

func (s *depStubSubsystem) ID() lifecycle.SubsystemID          { return s.id }
func (s *depStubSubsystem) DependsOn() []lifecycle.SubsystemID { return s.deps }
func (s *depStubSubsystem) Start(_ context.Context) error {
	s.rec.record(s.id)
	return nil
}
func (s *depStubSubsystem) Stop(_ context.Context) error { return nil }

// startRecorder records the order in which depStubSubsystem.Start is
// invoked. Orchestrator.StartAll starts subsystems sequentially (no
// concurrency within a single StartAll call), but the mutex costs nothing
// and removes any doubt.
type startRecorder struct {
	mu    sync.Mutex
	order []lifecycle.SubsystemID
}

func (r *startRecorder) record(id lifecycle.SubsystemID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, id)
}

func (r *startRecorder) snapshot() []lifecycle.SubsystemID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]lifecycle.SubsystemID, len(r.order))
	copy(out, r.order)
	return out
}

// realProductionSubsystemGraph constructs every one of the 17 production
// subsystem types with a minimal/zero-value config and reads each one's
// real DependsOn() LIVE. None of these constructors allocate or touch live
// resources (07-09 D-12 Wave A made every constructor allocate nothing
// precisely so this is possible) — Start is never called on any of them.
// The single exception needing a non-zero argument is cluster.NewSubsystem,
// which validates (but does not use) a ClusterID and a Pill at construction;
// cluster.NewTestPill() is the package's own no-op test double for exactly
// this purpose.
//
// Returns a map keyed by SubsystemID so callers can build depStubSubsystems
// whose deps are never hand-copied — a hand-copied dep list is precisely
// the defect class that let rev 4's cycle hide (see file doc comment).
func realProductionSubsystemGraph(t *testing.T) map[lifecycle.SubsystemID][]lifecycle.SubsystemID {
	t.Helper()

	pill, _ := cluster.NewTestPill()
	clusterSub, err := cluster.NewSubsystem(
		cluster.Config{ClusterID: "core-topo-order-test"},
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

// registerDepStubs builds one depStubSubsystem per entry in graph, all
// sharing rec, and registers each with orch.
func registerDepStubs(orch *lifecycle.Orchestrator, graph map[lifecycle.SubsystemID][]lifecycle.SubsystemID, rec *startRecorder) {
	for id, deps := range graph {
		orch.Register(&depStubSubsystem{id: id, deps: deps, rec: rec})
	}
}

// TestProductionSubsystemsTopologicalStartOrderIsPinned pins the exact
// start order Orchestrator.StartAll produces over the REAL production
// dependency graph (07-10 Task 3). MEDIUM-11 was a comment asserting an
// ordering the graph never made; this test makes the graph's ACTUAL order
// the asserted artifact, so that divergence cannot recur silently. Because
// topoSort's queue tie-break is a deterministic sort.Slice by SubsystemID
// (internal/lifecycle/orchestrator.go), the exact sequence is stable and
// safe to pin — this is not a flaky ordering assumption.
func TestProductionSubsystemsTopologicalStartOrderIsPinned(t *testing.T) {
	graph := realProductionSubsystemGraph(t)
	rec := &startRecorder{}

	orch := lifecycle.NewOrchestrator()
	registerDepStubs(orch, graph, rec)

	require.NoError(t, orch.StartAll(context.Background()))

	got := rec.snapshot()

	want := []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemTLS,
		lifecycle.SubsystemABAC,
		lifecycle.SubsystemAuth,
		lifecycle.SubsystemSessions,
		lifecycle.SubsystemEventBus,
		lifecycle.SubsystemWorld,
		lifecycle.SubsystemCluster,
		lifecycle.SubsystemCryptoChainVerifier,
		lifecycle.SubsystemOutboxRelay,
		lifecycle.SubsystemPlugins,
		lifecycle.SubsystemAdminSocket,
		lifecycle.SubsystemBootstrap,
		lifecycle.SubsystemAuditProjection,
		lifecycle.SubsystemGRPC,
		lifecycle.SubsystemCryptoPolicy,
		lifecycle.SubsystemRekeyCheckpointSweep,
	}

	gotStr := make([]string, len(got))
	for i, id := range got {
		gotStr[i] = id.String()
	}
	wantStr := make([]string, len(want))
	for i, id := range want {
		wantStr[i] = id.String()
	}
	assert.Equal(t, wantStr, gotStr, "the production topological start order has drifted from the pinned sequence — "+
		"if this is an intentional DependsOn change, re-derive the new order from the live graph rather than editing this literal by hand")

	// Named orderings, so a failure says WHICH invariant broke rather than
	// dumping a 17-element diff.
	idx := func(id lifecycle.SubsystemID) int {
		for i, v := range got {
			if v == id {
				return i
			}
		}
		return -1
	}

	dbIdx := idx(lifecycle.SubsystemDatabase)
	busIdx := idx(lifecycle.SubsystemEventBus)
	verifierIdx := idx(lifecycle.SubsystemCryptoChainVerifier)
	auditIdx := idx(lifecycle.SubsystemAuditProjection)
	grpcIdx := idx(lifecycle.SubsystemGRPC)
	adminIdx := idx(lifecycle.SubsystemAdminSocket)

	require.GreaterOrEqual(t, dbIdx, 0)
	require.GreaterOrEqual(t, busIdx, 0)
	require.GreaterOrEqual(t, verifierIdx, 0)
	require.GreaterOrEqual(t, auditIdx, 0)
	require.GreaterOrEqual(t, grpcIdx, 0)
	require.GreaterOrEqual(t, adminIdx, 0)

	// 1. database-first.
	assert.Equal(t, 0, dbIdx, "Database must start first — every other subsystem depends on it, directly or transitively")

	// 2. database-before-eventbus (07-09's round-7 GameIDProvider edge — a
	// real DependsOn, not an ID tie-break).
	assert.Less(t, dbIdx, busIdx, "Database must start before EventBus (eventbus.Subsystem.DependsOn == [SubsystemDatabase])")

	// 3. bus-before-verifier (DATA FLOW: the verifier's handler set is built
	// from the bus's own publisher — core.go / readstream_wiring.go). NOT
	// the reverse — the reverse is the cycle rev 4 shipped and this test
	// exists to prevent.
	assert.Less(t, busIdx, verifierIdx, "EventBus must start before CryptoChainVerifier — the verifier's handler set is built from the bus's publisher")

	// 4. auditprojection-before-grpc (T-07-50, 07-10 Task 2's new edge).
	assert.Less(t, auditIdx, grpcIdx, "AuditProjection must start before GRPC — gRPC must not serve before the audit projection is up")

	// 5 & 6. verifier-before-grpc / verifier-before-admin (DOMAIN-BIND ORDER:
	// the T-07-51 re-scope, 07-09 item 8's edges — fail-closed before either
	// external domain surface binds).
	assert.Less(t, verifierIdx, grpcIdx, "CryptoChainVerifier must start before GRPC — INV-CRYPTO-102 must be proven before gRPC binds its TCP listener")
	assert.Less(t, verifierIdx, adminIdx, "CryptoChainVerifier must start before AdminSocket — INV-CRYPTO-102 must be proven before admin.sock binds")
}

// TestProductionSubsystemGraphIsAcyclic proves the REAL production
// dependency graph — as it exists after 07-09's mutations, not as it
// existed on main before this phase — has no cycle and no dangling edge
// (07-10 Task 4). This is the guard that would have caught rev 4's
// EventBus -> CryptoChainVerifier -> EventBus cycle: every other gate in
// the phase (07-09's superset-DependsOn test, task test, the plan checker)
// passed with that cycle present, because none of them ran the REAL,
// post-07-09 graph through topoSort. See the file-level doc comment.
func TestProductionSubsystemGraphIsAcyclic(t *testing.T) {
	graph := realProductionSubsystemGraph(t)

	// 3. No dangling edges: every ID any subsystem's live DependsOn() names
	// must itself be a registered ID. This needs no StartAll at all — it
	// catches the "missing dependency" half of topoSort's error surface
	// (internal/lifecycle/orchestrator.go) directly over the dep map.
	for id, deps := range graph {
		for _, dep := range deps {
			if _, ok := graph[dep]; !ok {
				t.Fatalf("subsystem %s declares DependsOn %s, which is not a registered production subsystem", id.String(), dep.String())
			}
		}
	}

	rec := &startRecorder{}
	orch := lifecycle.NewOrchestrator()
	registerDepStubs(orch, graph, rec)

	// 1. StartAll must return no error over the real production set — in
	// particular, no cycle refusal and no missing-dependency refusal from
	// the topoSort it runs internally. On failure, print every subsystem's
	// ID -> DependsOn so the offending edge is readable from the test
	// output alone, without re-deriving the graph by hand.
	err := orch.StartAll(context.Background())
	if err != nil {
		t.Logf("production dependency graph (ID -> DependsOn):")
		for id, deps := range graph {
			depStrs := make([]string, len(deps))
			for i, d := range deps {
				depStrs[i] = d.String()
			}
			t.Logf("  %s -> %v", id.String(), depStrs)
		}
		t.Fatalf("Orchestrator.StartAll returned an error over the real production graph: %v", err)
	}

	// 2. Recorded started-stub count == registered set count. Kahn's
	// algorithm (topoSort) emits STRICTLY FEWER nodes than it was given
	// exactly when a cycle is present; StartAll starts one subsystem per
	// emitted node. This is the assertion that would catch a topoSort that
	// ever silently stopped returning an error on a cycle.
	got := rec.snapshot()
	assert.Len(t, got, len(graph), "every registered subsystem must have started exactly once — a shortfall means topoSort emitted fewer nodes than it was given (a cycle), even if it did not return an error")
}

// TestProductionSubsystemGraphAcyclicityCatchesRev4Cycle is the RED proof
// for Task 4 (07-10-PLAN.md): reconstructing rev 4's EXACT cycle
// (eventbus.Subsystem.DependsOn including SubsystemCryptoChainVerifier) must
// fail TestProductionSubsystemGraphIsAcyclic's assertions. This test does
// NOT reconstruct the cycle in production code — <settlements> § MEDIUM-11
// forbids that edge forever — it reconstructs the cycle ONLY in the local
// depStubSubsystem graph, proving the acyclicity test's own machinery would
// have caught rev 4 had it existed then.
func TestProductionSubsystemGraphAcyclicityCatchesRev4Cycle(t *testing.T) {
	graph := realProductionSubsystemGraph(t)

	// Reconstruct rev 4's exact cycle: add the forbidden reverse edge to a
	// COPY of the live graph.
	cyclicGraph := make(map[lifecycle.SubsystemID][]lifecycle.SubsystemID, len(graph))
	for id, deps := range graph {
		cyclicGraph[id] = deps
	}
	cyclicGraph[lifecycle.SubsystemEventBus] = append(
		append([]lifecycle.SubsystemID{}, cyclicGraph[lifecycle.SubsystemEventBus]...),
		lifecycle.SubsystemCryptoChainVerifier,
	)

	rec := &startRecorder{}
	orch := lifecycle.NewOrchestrator()
	registerDepStubs(orch, cyclicGraph, rec)

	err := orch.StartAll(context.Background())
	require.Error(t, err, "rev 4's exact cycle (EventBus -> CryptoChainVerifier -> EventBus) must be rejected by StartAll/topoSort")
	assert.Contains(t, err.Error(), "cycle", "the failure must name the cycle, not just report a generic error")
}

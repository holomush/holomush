// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// seedPolicySetHeadForOrch inserts a single events_audit row for the
// crypto.policy_set chain. The row uses a proto-wrapped envelope following
// the same pattern as insertChainRow in internal/admin/policy.
//
// The stored policy_hash value is selfHash. Phase 1's PolicyHashSource
// calls chain.Emitter.ComputePrevHashFor, which returns the recomputed
// self-hash of the tail entry. Use computeExpectedPolicyHash to obtain
// the matching expected value for test assertions.
func seedPolicySetHeadForOrch(t *testing.T, pool *pgxpool.Pool, gameID, policyName string, selfHash []byte) {
	t.Helper()
	subject := "events." + gameID + ".system.crypto_policy." + policyName

	// Build the payload JSON; self_hash field holds the known bytes.
	payload := map[string]any{
		"policy_name":       policyName,
		"policy_snapshot":   map[string]any{},
		"policy_hash":       selfHash,
		"prev_hash":         nil,
		"server_start_ulid": "01JX00000000000000000000000",
		"server_identity":   "test",
		"timestamp":         "2026-05-10T00:00:00Z",
	}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	// Wrap in proto envelope (same as production audit rows).
	envelope, err := proto.Marshal(&eventbusv1.Event{
		Subject: subject,
		Type:    "crypto.policy_set",
		Payload: payloadBytes,
	})
	require.NoError(t, err)

	_, err = pool.Exec(context.Background(),
		`INSERT INTO events_audit
		   (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
		 VALUES ($1, $2, 'crypto.policy_set', now(), 'system', $3, 1, 'identity', $4, '{}'::jsonb)`,
		[]byte("01JX00000000000000000000001"), subject, envelope, int64(1))
	require.NoError(t, err)
}

// computeExpectedPolicyHash returns the value that PolicyHashSource would
// return for the seeded chain (the recomputed self-hash of the tail entry).
func computeExpectedPolicyHash(t *testing.T, pool *pgxpool.Pool, gameID, policyName string) []byte {
	t.Helper()
	h := policy.PolicySetHandlerFor(gameID)
	repo := chain.NewPostgresRepo(pool)
	em := chain.NewEmitter(repo)
	prevHash, _, err := em.ComputePrevHashFor(context.Background(), h, policyName)
	require.NoError(t, err)
	return prevHash
}

// newTestOrchestrator builds a test-ready Orchestrator backed by pool,
// wiring the policy chain handler for the given gameID.
func newTestOrchestrator(t *testing.T, pool *pgxpool.Pool, gameID string) *dek.Orchestrator {
	t.Helper()
	repo := dek.NewCheckpointRepo(pool)
	chainRepo := chain.NewPostgresRepo(pool)
	policyHandler := policy.PolicySetHandlerFor(gameID)
	policyHashSrc := dek.NewAuditChainPolicyHashSource(chainRepo, policyHandler)
	store := dek.NewStore(pool)
	return dek.NewOrchestrator(store, repo, policyHashSrc)
}

// TestOrchestrator_Phase1_FreshStart_CapturesPolicyHash verifies:
//   - checkpoint status transitions pending → phase1_auth (INV-E1)
//   - policy_hash on the checkpoint row equals the recomputed chain head hash (INV-E25)
//   - RequestID is non-zero
func TestOrchestrator_Phase1_FreshStart_CapturesPolicyHash(t *testing.T) {
	pool := testIntegrationPool(t)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	// Seed: active crypto_keys row for the context.
	seedDEKRow(t, pool, 100, "scene", "01ABC", 3)

	// Seed: policy_set chain head with a known stored policy_hash value.
	storedPolicyHash := make([]byte, 32)
	storedPolicyHash[0] = 0xA1
	storedPolicyHash[1] = 0xB2
	storedPolicyHash[2] = 0xC3
	seedPolicySetHeadForOrch(t, pool, gameID, "dual_control_required", storedPolicyHash)

	// Compute what the emitter would return as the prevHash value.
	wantPolicyHash := computeExpectedPolicyHash(t, pool, gameID, "dual_control_required")
	require.NotNil(t, wantPolicyHash, "chain head must be present after seeding")
	require.Len(t, wantPolicyHash, 32)

	orch := newTestOrchestrator(t, pool, gameID)
	req := dek.RekeyRequest{
		ContextType:   "scene",
		ContextID:     "01ABC",
		Justification: "Forced revocation, ticket #1234",
		Operator:      dek.OperatorIdentity{PlayerID: "01PRIM"},
	}

	rid, err := orch.RunPhase1Fresh(context.Background(), req)
	require.NoError(t, err)
	require.False(t, rid.IsZero(), "RequestID must be non-zero")

	repo := dek.NewCheckpointRepo(pool)
	ckpt, err := repo.Get(context.Background(), rid)
	require.NoError(t, err)

	require.Equal(t, dek.CheckpointStatusPhase1Auth, ckpt.Status,
		"INV-E1: checkpoint must be in phase1_auth after RunPhase1Fresh")

	// INV-E25: policy_hash MUST be captured at Phase 1 from the chain head.
	// Convert wantPolicyHash ([]byte, from ComputePrevHashFor) to [32]byte for comparison.
	var wantArr [32]byte
	copy(wantArr[:], wantPolicyHash)
	require.Equal(t, wantArr, ckpt.PolicyHash(),
		"INV-E25: policy_hash MUST be captured at Phase 1 from the chain head")
}

// TestOrchestrator_Phase1_GenesisPolicy_ZeroHash verifies that when no
// policy_set chain exists yet (genesis), RunPhase1Fresh succeeds and
// captures a 32-byte zero sentinel as policy_hash.
func TestOrchestrator_Phase1_GenesisPolicy_ZeroHash(t *testing.T) {
	pool := testIntegrationPool(t)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	seedDEKRow(t, pool, 100, "scene", "01ABC", 3)
	// No policy_set chain seeded → genesis case.

	orch := newTestOrchestrator(t, pool, gameID)
	req := dek.RekeyRequest{
		ContextType:   "scene",
		ContextID:     "01ABC",
		Justification: "Genesis rekey, no policy chain yet",
		Operator:      dek.OperatorIdentity{PlayerID: "01PRIM"},
	}

	rid, err := orch.RunPhase1Fresh(context.Background(), req)
	require.NoError(t, err)
	require.False(t, rid.IsZero())

	repo := dek.NewCheckpointRepo(pool)
	ckpt, err := repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusPhase1Auth, ckpt.Status)

	// Genesis: PolicyHashSource returns nil → Orchestrator stores 32 zero bytes.
	var want [32]byte // zero array
	require.Equal(t, want, ckpt.PolicyHash(),
		"genesis case: policy_hash must be 32 zero bytes sentinel")
}

// TestOrchestrator_Phase1_ConcurrentSameContext_Rejected verifies:
//   - a second RunPhase1Fresh on the same context while the first is active
//     returns DEK_REKEY_ALREADY_IN_PROGRESS (INV-E5)
func TestOrchestrator_Phase1_ConcurrentSameContext_Rejected(t *testing.T) {
	pool := testIntegrationPool(t)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	seedDEKRow(t, pool, 100, "scene", "01ABC", 3)

	orch := newTestOrchestrator(t, pool, gameID)

	req1 := dek.RekeyRequest{
		ContextType: "scene", ContextID: "01ABC",
		Justification: "first revocation",
		Operator:      dek.OperatorIdentity{PlayerID: "01A"},
	}
	_, err := orch.RunPhase1Fresh(context.Background(), req1)
	require.NoError(t, err, "first call must succeed")

	// Second call: same context, different operator — must be rejected.
	req2 := dek.RekeyRequest{
		ContextType: "scene", ContextID: "01ABC",
		Justification: "second attempt",
		Operator:      dek.OperatorIdentity{PlayerID: "01B"},
	}
	_, err = orch.RunPhase1Fresh(context.Background(), req2)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_ALREADY_IN_PROGRESS")
}

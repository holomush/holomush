// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/pgnanos"
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

	// The id is a synthetic non-ULID byte string, so event_ms (000052 partition
	// key) is the row's own now()-based store-time — lands in the current partition.
	ts := pgnanos.From(time.Now())
	_, err = pool.Exec(context.Background(),
		`INSERT INTO events_audit
		   (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering, event_ms)
		 VALUES ($1, $2, 'crypto.policy_set', $3, 'system', $4, 1, 'identity', $5, '{}'::jsonb, $6)`,
		[]byte("01JX00000000000000000000001"), subject, ts, envelope, int64(1), ts)
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
// wiring the policy chain handler for the given gameID and a real KEK provider.
func newTestOrchestrator(t *testing.T, pool *pgxpool.Pool, gameID string) *dek.Orchestrator {
	t.Helper()
	return newTestOrchestratorWithProvider(t, pool, gameID, newTestProvider(t))
}

// newTestOrchestratorWithProvider builds a test-ready Orchestrator with an
// explicit KEK provider. Use when the same provider must be shared between
// the orchestrator and seeded DEK rows (e.g., Phase 2 test).
func newTestOrchestratorWithProvider(t *testing.T, pool *pgxpool.Pool, gameID string, provider kek.Provider) *dek.Orchestrator {
	t.Helper()
	repo := dek.NewCheckpointRepo(pool)
	chainRepo := chain.NewPostgresRepo(pool)
	policyHandler := policy.PolicySetHandlerFor(gameID)
	policyHashSrc := dek.NewAuditChainPolicyHashSource(chainRepo, policyHandler)
	store := dek.NewStore(pool)
	cfg := dek.CacheConfig{Capacity: 64, TTL: time.Minute}
	mgr, err := dek.NewManager(
		provider, store,
		dek.NewCache(cfg), dek.NewParticipantsCache(cfg),
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&stubBindingResolver{}, // stubBindingResolver defined in manager_test.go
	)
	require.NoError(t, err)
	return dek.NewOrchestrator(store, repo, policyHashSrc, mgr)
}

// TestOrchestrator_Phase1_FreshStart_CapturesPolicyHash verifies:
//   - checkpoint status transitions pending → phase1_auth (INV-CRYPTO-88)
//   - policy_hash on the checkpoint row equals the recomputed chain head hash (INV-CRYPTO-112)
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
		"INV-CRYPTO-88: checkpoint must be in phase1_auth after RunPhase1Fresh")

	// INV-CRYPTO-112: policy_hash MUST be captured at Phase 1 from the chain head.
	// Convert wantPolicyHash ([]byte, from ComputePrevHashFor) to [32]byte for comparison.
	var wantArr [32]byte
	copy(wantArr[:], wantPolicyHash)
	require.Equal(t, wantArr, ckpt.PolicyHash(),
		"INV-CRYPTO-112: policy_hash MUST be captured at Phase 1 from the chain head")
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
//     returns DEK_REKEY_ALREADY_IN_PROGRESS (INV-CRYPTO-92)
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

// seedActiveDEKWithParticipants inserts a crypto_keys row for (ctxType, ctxID)
// at the given version with a participants JSONB column. The wrapped_dek is a
// placeholder — the orchestrator only reads context_type, context_id, version,
// and participants during Phase 2 minting. Returns the assigned row id.
func seedActiveDEKWithParticipants(t *testing.T, pool *pgxpool.Pool, ctxType, ctxID string, version uint32, participants []dek.Participant) int64 {
	t.Helper()
	participantsJSON, err := json.Marshal(participants)
	require.NoError(t, err)
	var id int64
	err = pool.QueryRow(context.Background(), `
        INSERT INTO crypto_keys
            (context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants, created_at)
        VALUES ($1, $2, $3, '\xdeadbeef', 'test', 'test-key-id', $4::jsonb, $5)
        RETURNING id
    `, ctxType, ctxID, version, participantsJSON, pgnanos.From(time.Now())).Scan(&id)
	require.NoError(t, err)
	return id
}

// cryptoKeyRowForTest is the minimal projection of a crypto_keys row
// needed for Phase 2 participant-invariance assertions.
type cryptoKeyRowForTest struct {
	Version          uint32
	ParticipantsJSON []byte
}

// loadCryptoKeyRow loads a crypto_keys row by its primary key id. Used to
// assert INV-CRYPTO-93 (participants byte-equal between old and new DEK rows).
func loadCryptoKeyRow(t *testing.T, pool *pgxpool.Pool, id int64) cryptoKeyRowForTest {
	t.Helper()
	var r cryptoKeyRowForTest
	err := pool.QueryRow(
		context.Background(),
		`SELECT version, participants::text FROM crypto_keys WHERE id = $1`, id,
	).Scan(&r.Version, &r.ParticipantsJSON)
	require.NoError(t, err)
	return r
}

// TestOrchestrator_Phase2_MintsNewDEK_PreservesParticipants verifies:
//   - RunPhase2 advances checkpoint status to phase2_mint_dek (INV-CRYPTO-88)
//   - checkpoint.NewDEKID is populated after RunPhase2
//   - new crypto_keys row has version = old+1
//   - new row's participants JSON is byte-equal to old (INV-CRYPTO-93)
func TestOrchestrator_Phase2_MintsNewDEK_PreservesParticipants(t *testing.T) {
	pool := testIntegrationPool(t)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)
	provider := newTestProvider(t)

	participants := []dek.Participant{
		{PlayerID: "01PA", CharacterID: "01CA"},
		{PlayerID: "01PB", CharacterID: "01CB"},
	}
	seedActiveDEKWithParticipants(t, pool, "scene", "01ABC", 3, participants)

	policyHash := make([]byte, 32)
	seedPolicySetHeadForOrch(t, pool, gameID, "dual_control_required", policyHash)

	orch := newTestOrchestratorWithProvider(t, pool, gameID, provider)
	req := dek.RekeyRequest{
		ContextType:   "scene",
		ContextID:     "01ABC",
		Justification: "x",
		Operator:      dek.OperatorIdentity{PlayerID: "01P"},
	}
	rid, err := orch.RunPhase1Fresh(context.Background(), req)
	require.NoError(t, err)

	require.NoError(t, orch.RunPhase2(context.Background(), rid))

	repo := dek.NewCheckpointRepo(pool)
	ckpt, err := repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusPhase2MintDEK, ckpt.Status,
		"INV-CRYPTO-88: checkpoint must advance to phase2_mint_dek after RunPhase2")
	require.NotNil(t, ckpt.NewDEKID, "NewDEKID must be populated after Phase 2")

	// INV-CRYPTO-93: new DEK row's participants MUST be byte-equal to old.
	oldRow := loadCryptoKeyRow(t, pool, ckpt.OldDEKID)
	newRow := loadCryptoKeyRow(t, pool, *ckpt.NewDEKID)
	require.Equal(t, oldRow.ParticipantsJSON, newRow.ParticipantsJSON,
		"INV-CRYPTO-93: new DEK row participants must be byte-equal to old")
	require.Equal(t, oldRow.Version+1, newRow.Version,
		"new DEK version must be old+1")
}

// TestOrchestrator_Phase2_RequiresPreconditionPhase1Auth verifies:
//   - RunPhase2 returns DEK_REKEY_PHASE_PRECONDITION_FAILED when checkpoint
//     is not in phase1_auth status (INV-CRYPTO-88 / INV-CRYPTO-93 rejection path)
func TestOrchestrator_Phase2_RequiresPreconditionPhase1Auth(t *testing.T) {
	pool := testIntegrationPool(t)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	seedDEKRow(t, pool, 200, "scene", "01DEF", 1)

	orch := newTestOrchestrator(t, pool, gameID)

	// Open a checkpoint manually at 'pending' status — do NOT call RunPhase1Fresh
	// so the status stays at 'pending', not 'phase1_auth'.
	repo := dek.NewCheckpointRepo(pool)
	openReq, err := dek.NewCheckpointOpenRequest(
		"scene", "01DEF",
		make([]byte, 32), make([]byte, 32),
		"01PLAYER", 200,
		"",
	)
	require.NoError(t, err)
	rid, err := repo.Open(context.Background(), openReq)
	require.NoError(t, err)

	// RunPhase2 with checkpoint at 'pending' (not 'phase1_auth') must fail.
	err = orch.RunPhase2(context.Background(), rid)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_PHASE_PRECONDITION_FAILED")
}

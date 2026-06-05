// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
)

// PolicyHashSource abstracts "read the current head of the crypto.policy_set
// chain for a given policyName and return its recomputed self-hash".
//
// Phase 1 calls CurrentPolicyHash once and persists the result on the
// checkpoint row (INV-CRYPTO-112: policy_hash is frozen at Phase 1 and never
// re-queried during later phases).
//
// Returns nil when the chain is empty (genesis: no policy_set event yet).
// The Orchestrator converts a nil result to a 32-byte zero sentinel so
// the NOT NULL column constraint on crypto_rekey_checkpoints.policy_hash
// is satisfied.
type PolicyHashSource interface {
	CurrentPolicyHash(ctx context.Context, policyName string) ([]byte, error)
}

// auditChainPolicyHashSource implements PolicyHashSource by reading the
// tail of the crypto.policy_set chain via chain.Emitter.ComputePrevHashFor.
// The return value is the recomputed self-hash of the tail entry — i.e.,
// the value that would become the next entry's prev_hash. This is what
// Phase 1 freezes on the checkpoint row.
//
// The chain.Handler is provided at construction time by the caller (wiring
// code in cmd/holomush/core.go uses policy.PolicySetHandlerFor). This
// avoids an import cycle: dek → policy → eventbus → dek.
type auditChainPolicyHashSource struct {
	emitter chain.Emitter
	handler chain.Handler
}

// NewAuditChainPolicyHashSource constructs a PolicyHashSource backed by the
// auditchain Emitter. handler is the per-chain descriptor for the
// crypto.policy_set chain (typically policy.PolicySetHandlerFor(gameID));
// it is provided by the caller to avoid an import cycle.
func NewAuditChainPolicyHashSource(repo chain.Repo, handler chain.Handler) PolicyHashSource {
	return &auditChainPolicyHashSource{
		emitter: chain.NewEmitter(repo),
		handler: handler,
	}
}

// CurrentPolicyHash reads the tail of the configured policy chain for
// policyName and returns its recomputed self-hash. Returns nil when the
// chain is empty (genesis).
func (s *auditChainPolicyHashSource) CurrentPolicyHash(ctx context.Context, policyName string) ([]byte, error) {
	prevHash, _, err := s.emitter.ComputePrevHashFor(ctx, s.handler, policyName)
	if err != nil {
		return nil, oops.Code("DEK_REKEY_POLICY_HASH_READ_FAILED").
			With("policy_name", policyName).
			Wrap(err)
	}
	return prevHash, nil
}

// Minter abstracts the Phase 2 DEK-minting operation so that the
// Orchestrator is decoupled from the concrete *manager type.
//
// The sole production implementation is *manager.MintNewDEKForRekey.
// Tests may substitute a fake.
type Minter interface {
	// MintNewDEKForRekey generates a fresh DEK, wraps it via Provider, and
	// INSERTs a new crypto_keys row with version = old.version+1 and
	// byte-equal participants (INV-CRYPTO-93). Returns the new row's primary key id.
	MintNewDEKForRekey(ctx context.Context, oldDEKID int64) (int64, error)
}

// Orchestrator runs the 7-phase Rekey lifecycle. Phases 1, 2, and 3 are
// implemented in rekey_orchestrator.go (Phase 3 in rekey_phase3.go); Phase 5
// in rekey_phase5.go; Phase 6 in rekey_phase6.go; Phase 7 in rekey_phase7.go.
//
// Thread-safety: Orchestrator is safe for concurrent use — all state lives
// in the database (CheckpointRepo) with CAS updates guarding transitions.
//
// Phase-specific collaborators are wired post-construction via additive setter
// methods (SetMaterialResolver, SetPhase5Coordinator, SetDestroyer,
// SetAuditEmitter, SetDataDir). NewOrchestrator's signature is unchanged so
// wiring from all earlier beads continues to compile.
type Orchestrator struct {
	store            *Store
	repo             *CheckpointRepo
	policyHashSrc    PolicyHashSource
	minter           Minter
	materialResolver MaterialResolver
	phase5Coord      Phase5Coordinator
	dekDestroyer     Destroyer
	auditEmitter     AuditEmitter // Phase 7: emit chained audit event (holomush-jxo8.7.24)
	dataDir          string       // Phase 7: fallback log directory (INV-CRYPTO-100)
	serverID         string       // Phase 7: server_identity field in audit payload
	batchHookForTest func(rowsRewrittenSoFar int)
	logger           *slog.Logger
}

// NewOrchestrator constructs an Orchestrator. All four collaborators are
// required; nil arguments panic at construction time rather than surfacing
// as nil-pointer dereferences at call time.
func NewOrchestrator(
	store *Store,
	repo *CheckpointRepo,
	policyHashSrc PolicyHashSource,
	minter Minter,
) *Orchestrator {
	if store == nil {
		panic("dek.NewOrchestrator: store must not be nil")
	}
	if repo == nil {
		panic("dek.NewOrchestrator: CheckpointRepo must not be nil")
	}
	if policyHashSrc == nil {
		panic("dek.NewOrchestrator: PolicyHashSource must not be nil")
	}
	if minter == nil {
		panic("dek.NewOrchestrator: Minter must not be nil")
	}
	return &Orchestrator{
		store:         store,
		repo:          repo,
		policyHashSrc: policyHashSrc,
		minter:        minter,
		logger:        slog.Default(),
	}
}

// RunPhase1Fresh is the entry point for a fresh rekey. It:
//  1. Computes the op_args_hash (INV-CRYPTO-111: idempotency key binding the WORK).
//  2. Captures the current policy_set chain head as policy_hash (INV-CRYPTO-112).
//     If the chain is empty (genesis), stores a 32-byte zero sentinel.
//  3. Reads the active DEK row to obtain old_dek_id.
//  4. Opens (INSERTs) the checkpoint row with status=pending.
//  5. Advances the checkpoint to phase1_auth.
//
// Returns DEK_REKEY_ALREADY_IN_PROGRESS if a non-terminal checkpoint
// already exists for (context_type, context_id) (INV-CRYPTO-92, enforced by the
// partial unique index in the Open step).
//
// Phase 1 does NOT authenticate the operator — that is the admin handler's
// responsibility before calling RunPhase1Fresh.
func (o *Orchestrator) RunPhase1Fresh(ctx context.Context, req RekeyRequest) (RequestID, error) {
	// Step 1: compute op_args_hash.
	argsHash, err := ComputeRekeyArgsHash(req)
	if err != nil {
		return RequestID{}, err
	}

	// Step 2: capture policy_hash from the current policy_set chain head.
	policyHash, err := o.policyHashSrc.CurrentPolicyHash(ctx, "dual_control_required")
	if err != nil {
		return RequestID{}, oops.Code("DEK_REKEY_POLICY_HASH_READ_FAILED").Wrap(err)
	}
	if policyHash == nil {
		// Genesis case: no policy_set chain yet. Persist a 32-byte zero
		// sentinel so the NOT NULL column constraint is satisfied; the
		// audit event will record it as the genesis hash.
		policyHash = make([]byte, 32)
	}

	// Step 3: read the active DEK row to obtain old_dek_id.
	activeRow, err := o.store.selectActive(ctx, ContextID{Type: req.ContextType, ID: req.ContextID})
	if err != nil {
		return RequestID{}, oops.Code("DEK_REKEY_ACTIVE_DEK_LOOKUP_FAILED").
			With("context_type", req.ContextType).
			With("context_id", req.ContextID).
			Wrap(err)
	}

	// Step 4: open the checkpoint row (status=pending). The partial unique
	// index rejects concurrent non-terminal checkpoints for the same context
	// with DEK_REKEY_ALREADY_IN_PROGRESS.
	openReq, err := NewCheckpointOpenRequest(
		req.ContextType,
		req.ContextID,
		argsHash[:],
		policyHash,
		req.Operator.PlayerID,
		activeRow.ID,
		req.Justification,
	)
	if err != nil {
		return RequestID{}, oops.Code("DEK_REKEY_CHECKPOINT_OPEN_ARGS_INVALID").Wrap(err)
	}
	rid, err := o.repo.Open(ctx, openReq)
	if err != nil {
		return RequestID{}, err // DEK_REKEY_ALREADY_IN_PROGRESS propagates as-is
	}

	// Step 5: advance status pending → phase1_auth.
	if err := o.repo.UpdateStatus(ctx, rid, CheckpointStatusPending, CheckpointStatusPhase1Auth); err != nil {
		return RequestID{}, oops.Code("DEK_REKEY_PHASE1_ADVANCE_FAILED").Wrap(err)
	}

	return rid, nil
}

// RunPhase2 mints a new DEK for the rekey context, preserving the old DEK's
// participant set byte-for-byte (INV-CRYPTO-93), and advances
// the checkpoint from phase1_auth to phase2_mint_dek atomically.
//
// Pre-condition: checkpoint status MUST be phase1_auth.
//
// Steps:
//  1. Load the checkpoint row to obtain old_dek_id and verify pre-condition.
//  2. Delegate to Minter.MintNewDEKForRekey to generate a fresh DEK,
//     wrap it, and INSERT a new crypto_keys row (version = old + 1,
//     participants byte-equal to old per INV-CRYPTO-93).
//  3. Atomically persist new_dek_id and advance status to phase2_mint_dek
//     via SetNewDEKAndAdvance (CAS UPDATE on status = 'phase1_auth').
func (o *Orchestrator) RunPhase2(ctx context.Context, rid RequestID) error {
	ckpt, err := o.repo.Get(ctx, rid)
	if err != nil {
		return err
	}
	if ckpt.Status != CheckpointStatusPhase1Auth {
		return oops.Code("DEK_REKEY_PHASE_PRECONDITION_FAILED").
			With("expected", CheckpointStatusPhase1Auth).
			With("actual", ckpt.Status).
			Errorf("Phase 2 requires status=%s", CheckpointStatusPhase1Auth)
	}

	// Mint new DEK via Minter (which calls Provider.Wrap and INSERTs into
	// crypto_keys with participants copied from the old row — INV-CRYPTO-93).
	newDEKID, err := o.minter.MintNewDEKForRekey(ctx, ckpt.OldDEKID)
	if err != nil {
		return oops.Code("DEK_REKEY_MINT_NEW_DEK_FAILED").Wrap(err)
	}

	// Persist new_dek_id and advance status atomically via CAS UPDATE.
	return o.repo.SetNewDEKAndAdvance(ctx, rid, newDEKID)
}

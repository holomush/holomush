// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/pkg/errutil"
)

// fakeAuditEmitter is a test-double for *dek.RekeyAuditEmitter. It captures
// the payloads passed to Emit and can be configured to return an error.
type fakeAuditEmitter struct {
	emitted []dek.RekeyAuditPayload
	failErr error
}

// Emit records the payload and returns the configured error (if any).
func (f *fakeAuditEmitter) Emit(_ context.Context, p dek.RekeyAuditPayload) (ulid.ULID, dek.RekeyAuditPayload, error) {
	if f.failErr != nil {
		return ulid.ULID{}, p, f.failErr
	}
	f.emitted = append(f.emitted, p)
	return ulid.Make(), p, nil
}

// SetEmitErrorForTest makes the next Emit call return err.
func (f *fakeAuditEmitter) SetEmitErrorForTest(err error) { f.failErr = err }

// phase7TestSetupImpl is the Phase 7 integration test harness.
// Builds on the Phase 6 harness shape.
type phase7TestSetupImpl struct {
	pool         *pgxpool.Pool
	Orch         *dek.Orchestrator
	Repo         *dek.CheckpointRepo
	Manager      dek.Manager
	AuditEmitter *fakeAuditEmitter
	DataDir      string
	oldDEKID     int64
	newDEKID     int64
}

// newPhase7TestSetup builds a harness ready to drive RunPhase7.
func newPhase7TestSetup() *phase7TestSetupImpl {
	pool := testIntegrationPool(suiteT)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	provider := newTestProvider(suiteT)
	st := dek.NewStore(pool)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})
	pcache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})

	mgr, err := dek.NewManager(
		provider, st, cache, pcache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&stubBindingResolver{},
	)
	Expect(err).NotTo(HaveOccurred())

	ctxID := dek.ContextID{Type: "scene", ID: "01PH7"}
	_, err = mgr.GetOrCreate(context.Background(), ctxID, nil)
	Expect(err).NotTo(HaveOccurred(), "seed old DEK via GetOrCreate")

	oldRecord, err := mgr.ActiveDEKRow(context.Background(), ctxID)
	Expect(err).NotTo(HaveOccurred())
	newDEKID, err := mgr.MintNewDEKForRekey(context.Background(), oldRecord.ID)
	Expect(err).NotTo(HaveOccurred())

	repo := dek.NewCheckpointRepo(pool)
	orch := dek.NewOrchestrator(st, repo, &nilPolicyHashSource{}, mgr)

	coord := &fakePhase5Coordinator{}
	orch.SetPhase5Coordinator(coord)
	orch.SetDestroyer(mgr)

	auditEmitter := &fakeAuditEmitter{}
	orch.SetAuditEmitter(auditEmitter)

	dataDir := suiteT.TempDir()
	orch.SetDataDir(dataDir)

	return &phase7TestSetupImpl{
		pool:         pool,
		Orch:         orch,
		Repo:         repo,
		Manager:      mgr,
		AuditEmitter: auditEmitter,
		DataDir:      dataDir,
		oldDEKID:     oldRecord.ID,
		newDEKID:     newDEKID,
	}
}

// RunUpToPhase6Complete seeds a checkpoint row at the FSM state that
// Phase 6 leaves behind on success: status=phase6_destroy_old.
// Bypasses RunPhase1Fresh through RunPhase6 since Phase 7's contract
// under test is independent of how the checkpoint reached phase6_destroy_old.
func (s *phase7TestSetupImpl) RunUpToPhase6Complete() dek.RequestID {
	rid := dek.RequestID(idgen.New())
	_, err := s.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id)
        VALUES ($1, 'scene', '01PH7', $2, $3, '01PRIM',
                'phase6_destroy_old', $4, $5)
    `, rid[:], make([]byte, 32), make([]byte, 32), s.oldDEKID, s.newDEKID)
	Expect(err).NotTo(HaveOccurred())
	return rid
}

// RunUpToPhase6CompleteWithForceDestroy seeds a checkpoint row at phase6_destroy_old
// with force_destroy=true and phase5_missing_members populated to simulate
// the force-destroy path for INV-CRYPTO-98 assertions.
func (s *phase7TestSetupImpl) RunUpToPhase6CompleteWithForceDestroy(missingMembers string) dek.RequestID {
	rid := dek.RequestID(idgen.New())
	_, err := s.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id,
           force_destroy, phase5_missing_members)
        VALUES ($1, 'scene', '01PH7FD', $2, $3, '01PRIM',
                'phase6_destroy_old', $4, $5,
                true, $6::jsonb)
    `, rid[:], make([]byte, 32), make([]byte, 32), s.oldDEKID, s.newDEKID, missingMembers)
	Expect(err).NotTo(HaveOccurred())
	return rid
}

// Phase 7 — Orchestrator integration specs.
var _ = Describe("Orchestrator Phase 7", func() {
	// TestOrchestrator_Phase7_EmitsChainedAudit_AdvancesToComplete verifies:
	//   - RunPhase7 emits the rekey audit event via the audit emitter (INV-CRYPTO-101)
	//   - outcome.AuditEventID is non-empty
	//   - checkpoint advances to status=complete (INV-CRYPTO-88)
	//   - completed_at is set
	It("emits chained audit and advances to complete (INV-CRYPTO-101, INV-CRYPTO-88)", func() {
		setup := newPhase7TestSetup()
		rid := setup.RunUpToPhase6Complete()

		outcome, err := setup.Orch.RunPhase7(context.Background(), rid, dek.RekeyRequest{
			ContextType:   "scene",
			ContextID:     "01PH7",
			Justification: "test",
			Operator:      dek.OperatorIdentity{PlayerID: "01PRIM"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(outcome.AuditEventID).NotTo(BeEmpty(),
			"INV-CRYPTO-101: AuditEventID must be populated after Phase 7")

		ckpt, err := setup.Repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Status).To(Equal(dek.CheckpointStatusComplete),
			"INV-CRYPTO-88: checkpoint must advance to complete after RunPhase7")
		Expect(ckpt.CompletedAt).NotTo(BeNil(),
			"completed_at must be set after Phase 7")

		// Verify the audit emitter received the payload.
		Expect(setup.AuditEmitter.emitted).To(HaveLen(1))
		emittedPayload := setup.AuditEmitter.emitted[0]
		Expect(emittedPayload.Context.Type).To(Equal("scene"))
		Expect(emittedPayload.Context.ID).To(Equal("01PH7"))
	})

	// TestOrchestrator_Phase7_AuditEmitFailure_FallbackLog verifies (INV-CRYPTO-100):
	//   - on audit emit failure, RunPhase7 returns DEK_REKEY_PHASE7_AUDIT_FAILED
	//   - the fallback log file is written at <data_dir>/audit-fallback/rekey-<rid>.log
	//   - the checkpoint does NOT advance to complete (DB state is preserved)
	It("audit emit failure writes fallback log and does not advance checkpoint (INV-CRYPTO-100)", func() {
		setup := newPhase7TestSetup()
		rid := setup.RunUpToPhase6Complete()
		setup.AuditEmitter.SetEmitErrorForTest(errors.New("simulated emit failure"))

		_, err := setup.Orch.RunPhase7(context.Background(), rid, dek.RekeyRequest{
			ContextType:   "scene",
			ContextID:     "01PH7",
			Justification: "test",
		})
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "DEK_REKEY_PHASE7_AUDIT_FAILED")

		// INV-CRYPTO-100: fallback log written to <data_dir>/audit-fallback/rekey-<rid>.log.
		logPath := filepath.Join(setup.DataDir, "audit-fallback", "rekey-"+rid.String()+".log")
		Expect(logPath).To(BeAnExistingFile(),
			"INV-CRYPTO-100: fallback log must be written on audit emit failure")

		// The checkpoint must NOT be at complete — rekey DB state is committed,
		// but the phase7_audit status means retry is possible.
		ckpt, cerr := setup.Repo.Get(context.Background(), rid)
		Expect(cerr).NotTo(HaveOccurred())
		Expect(ckpt.Status).NotTo(Equal(dek.CheckpointStatusComplete),
			"INV-CRYPTO-100: checkpoint must not advance to complete on audit emit failure")
	})

	// TestOrchestrator_Phase7_ForceDestroyPath_AuditCaptures verifies (INV-CRYPTO-98):
	//   - when force_destroy=true on the checkpoint, the emitted payload has
	//     ForceDestroy=true and the missing_members list is non-nil.
	It("force-destroy path audit captures ForceDestroy and missing_members (INV-CRYPTO-98)", func() {
		setup := newPhase7TestSetup()
		rid := setup.RunUpToPhase6CompleteWithForceDestroy(`["m1","m2"]`)

		_, err := setup.Orch.RunPhase7(context.Background(), rid, dek.RekeyRequest{
			ContextType:   "scene",
			ContextID:     "01PH7FD",
			Justification: "force destroy test",
			Operator:      dek.OperatorIdentity{PlayerID: "01PRIM"},
		})
		Expect(err).NotTo(HaveOccurred())

		// INV-CRYPTO-98: audit payload must capture force_destroy and missing_members.
		Expect(setup.AuditEmitter.emitted).To(HaveLen(1))
		p := setup.AuditEmitter.emitted[0]
		Expect(p.ForceDestroy).To(BeTrue(),
			"INV-CRYPTO-98: audit payload must have force_destroy=true")
		Expect(p.Phases.Phase5FinalMissingMembers).To(ConsistOf("m1", "m2"),
			"INV-CRYPTO-98: audit payload must embed final_missing_members from the checkpoint")
	})

	// TestOrchestrator_Phase7_RequiresPreconditionPhase6Complete verifies:
	//   - RunPhase7 with a checkpoint not in phase6_destroy_old returns
	//     DEK_REKEY_PHASE_PRECONDITION_FAILED (INV-CRYPTO-88 FSM guard).
	It("requires phase6_destroy_old precondition (INV-CRYPTO-88 FSM guard)", func() {
		pool := testIntegrationPool(suiteT)
		const gameID = "g1"
		dek.SetGameIDForTest(gameID)

		// Seed a DEK row for this test (fixed id, no return value).
		const dekID = int64(500)
		seedDEKRow(suiteT, pool, dekID, "scene", "01PH7PRE", 1)

		// Open a checkpoint at 'phase1_auth' — not a valid Phase 7 entry point.
		repo := dek.NewCheckpointRepo(pool)
		rid := dek.RequestID(idgen.New())
		_, err := pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id)
        VALUES ($1, 'scene', '01PH7PRE', $2, $3, '01PLAYER', 'phase1_auth', $4)
    `, rid[:], make([]byte, 32), make([]byte, 32), dekID)
		Expect(err).NotTo(HaveOccurred())

		st := dek.NewStore(pool)
		provider := newTestProvider(suiteT)
		mgr, merr := dek.NewManager(
			provider, st,
			dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
			&stubBindingResolver{},
		)
		Expect(merr).NotTo(HaveOccurred())

		orch := dek.NewOrchestrator(st, repo, &nilPolicyHashSource{}, mgr)
		orch.SetAuditEmitter(&fakeAuditEmitter{})
		orch.SetDataDir(suiteT.TempDir())

		_, runErr := orch.RunPhase7(context.Background(), rid, dek.RekeyRequest{
			ContextType: "scene", ContextID: "01PH7PRE",
		})
		Expect(runErr).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, runErr, "DEK_REKEY_PHASE_PRECONDITION_FAILED")
	})

	// TestOrchestrator_Phase7_NoAuditEmitter_FailsClosed verifies that calling
	// RunPhase7 without wiring an AuditEmitter returns DEK_REKEY_AUDIT_EMITTER_NIL
	// rather than panicking (fail-closed per the SetPhase5Coordinator pattern).
	It("no AuditEmitter configured fails closed with DEK_REKEY_AUDIT_EMITTER_NIL", func() {
		pool := testIntegrationPool(suiteT)
		dek.SetGameIDForTest("g1")

		provider := newTestProvider(suiteT)
		st := dek.NewStore(pool)
		mgr, merr := dek.NewManager(
			provider, st,
			dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
			func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
			&stubBindingResolver{},
		)
		Expect(merr).NotTo(HaveOccurred())

		repo := dek.NewCheckpointRepo(pool)
		orch := dek.NewOrchestrator(st, repo, &nilPolicyHashSource{}, mgr)
		// Deliberately NOT calling SetAuditEmitter.
		orch.SetDataDir(suiteT.TempDir())

		rid := dek.RequestID(idgen.New())
		_, runErr := orch.RunPhase7(context.Background(), rid, dek.RekeyRequest{})
		Expect(runErr).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, runErr, "DEK_REKEY_AUDIT_EMITTER_NIL")
	})
})

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/pkg/errutil"
)

// runTestSetup wires a fully-collaborator-equipped Orchestrator suitable
// for end-to-end Run() exercise. Builds on the phase5/6/7 harnesses:
// real Manager (satisfies Minter, MaterialResolver, Destroyer), fake
// Phase5Coordinator (success by default), fake AuditEmitter, real
// CheckpointRepo, real Store. Phase 3 sees zero events_audit rows for
// the rekey context (none seeded), so the cold-tier loop is a no-op.
type runTestSetup struct {
	pool        *pgxpool.Pool
	Orch        *dek.Orchestrator
	Repo        *dek.CheckpointRepo
	Manager     dek.Manager
	Coordinator *fakePhase5Coordinator
	Audit       *fakeAuditEmitter
	contextType string
	contextID   string
	oldDEKID    int64
}

// newRunTestSetup builds the harness. ContextID is parameterised so each
// test runs in isolation against the shared pool.
func newRunTestSetup(contextID string) *runTestSetup {
	pool := testIntegrationPool(suiteT)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	provider := newTestProvider(suiteT)
	store := dek.NewStore(pool)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})
	pcache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})

	mgr, err := dek.NewManager(
		provider, store, cache, pcache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&stubBindingResolver{},
	)
	Expect(err).NotTo(HaveOccurred())

	ctxID := dek.ContextID{Type: "scene", ID: contextID}
	_, err = mgr.GetOrCreate(context.Background(), ctxID, nil)
	Expect(err).NotTo(HaveOccurred(), "seed active DEK via GetOrCreate")
	oldRecord, err := mgr.ActiveDEKRow(context.Background(), ctxID)
	Expect(err).NotTo(HaveOccurred())

	repo := dek.NewCheckpointRepo(pool)
	orch := dek.NewOrchestrator(store, repo, &nilPolicyHashSource{}, mgr)
	orch.SetMaterialResolver(mgr.(dek.MaterialResolver))
	coord := &fakePhase5Coordinator{}
	coord.SetSuccess()
	orch.SetPhase5Coordinator(coord)
	orch.SetDestroyer(mgr)
	auditEmitter := &fakeAuditEmitter{}
	orch.SetAuditEmitter(auditEmitter)
	orch.SetDataDir(GinkgoT().TempDir())

	return &runTestSetup{
		pool:        pool,
		Orch:        orch,
		Repo:        repo,
		Manager:     mgr,
		Coordinator: coord,
		Audit:       auditEmitter,
		contextType: "scene",
		contextID:   contextID,
		oldDEKID:    oldRecord.ID,
	}
}

func (s *runTestSetup) BasicRequest(playerID string) dek.RekeyRequest {
	return dek.RekeyRequest{
		ContextType:   s.contextType,
		ContextID:     s.contextID,
		Justification: "test rekey",
		Operator:      dek.OperatorIdentity{PlayerID: playerID},
	}
}

var _ = Describe("Orchestrator_Run_FreshStart_RunsAllPhases (INV-CRYPTO-88)", func() {
	It("drives all phases and lands checkpoint at complete with Resumed=false", func() {
		setup := newRunTestSetup("01RUN1")
		req := setup.BasicRequest("01PRIM")

		out, err := setup.Orch.Run(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Resumed).To(BeFalse(), "fresh-start path: Resumed must be false")
		Expect(out.RequestID.IsZero()).To(BeFalse())

		ckpt, err := setup.Repo.Get(context.Background(), out.RequestID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Status).To(Equal(dek.CheckpointStatusComplete),
			"INV-CRYPTO-88: fresh-start must drive the checkpoint to complete")
		Expect(ckpt.CompletedAt).NotTo(BeNil())
		Expect(setup.Audit.emitted).To(HaveLen(1), "Phase 7 must emit exactly once")
	})
})

var _ = Describe("Orchestrator_Run_Resume_MatchingArgs_BypassesApproval (INV-CRYPTO-103)", func() {
	It("re-uses existing RequestID and signals Resumed=true for same-args same-operator invocation", func() {
		setup := newRunTestSetup("01RUN2")
		req := setup.BasicRequest("01PRIM")

		// First attempt: drive only Phase 1 + Phase 2, then bail. Simulates a
		// crash mid-way through the lifecycle that leaves a checkpoint at
		// phase2_mint_dek (a non-terminal state).
		rid, err := setup.Orch.RunPhase1Fresh(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(setup.Orch.RunPhase2(context.Background(), rid)).To(Succeed())

		// Second call: same context + args + operator → resume path.
		out, err := setup.Orch.Run(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Resumed).To(BeTrue(),
			"INV-CRYPTO-103: same-args same-operator invocation must signal Resumed=true")
		Expect(out.RequestID).To(Equal(rid),
			"INV-CRYPTO-91: resume must re-use the existing RequestID, not allocate a new one")

		ckpt, err := setup.Repo.Get(context.Background(), out.RequestID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Status).To(Equal(dek.CheckpointStatusComplete))
	})
})

var _ = Describe("Orchestrator_Run_Resume_DifferentOperator_Rejected (INV-CRYPTO-103)", func() {
	It("rejects resume with DEK_REKEY_RESUME_OPERATOR_MISMATCH for a different operator", func() {
		setup := newRunTestSetup("01RUN3")
		req := setup.BasicRequest("01PRIM")

		// Seed a non-terminal checkpoint via Phase 1 (status=phase1_auth).
		_, err := setup.Orch.RunPhase1Fresh(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())

		// Different operator, identical args.
		req2 := req
		req2.Operator.PlayerID = "01OTHER"
		_, err = setup.Orch.Run(context.Background(), req2)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "DEK_REKEY_RESUME_OPERATOR_MISMATCH")
	})
})

var _ = Describe("Orchestrator_Run_ArgsConflict", func() {
	It("rejects a fresh-start attempt when a non-terminal checkpoint with different op_args_hash exists", func() {
		setup := newRunTestSetup("01RUN4")
		req := setup.BasicRequest("01PRIM")
		_, err := setup.Orch.RunPhase1Fresh(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())

		// Same context, different justification → different op_args_hash.
		req2 := req
		req2.Justification = "totally different reason"
		_, err = setup.Orch.Run(context.Background(), req2)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "DEK_REKEY_ARGS_CONFLICT")
	})
})

var _ = Describe("Orchestrator_Run_AfterPriorComplete_FreshStartSucceeds (INV-CRYPTO-103)", func() {
	It("succeeds as a new fresh rekey and emits a second audit event after the prior completed rekey", func() {
		setup := newRunTestSetup("01RUN5")
		req := setup.BasicRequest("01PRIM")

		out1, err := setup.Orch.Run(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(out1.Resumed).To(BeFalse())

		// Bump justification so args differ — the active DEK is now the
		// post-rekey one; a same-args second rekey is a separate concern.
		req2 := req
		req2.Justification = "second rekey, different ticket"
		out2, err := setup.Orch.Run(context.Background(), req2)
		Expect(err).NotTo(HaveOccurred())
		Expect(out2.Resumed).To(BeFalse())
		Expect(out2.RequestID).NotTo(Equal(out1.RequestID),
			"second rekey on a completed context must allocate a new RequestID")

		Expect(setup.Audit.emitted).To(HaveLen(2), "each rekey emits its own audit event")
	})
})

var _ = Describe("Orchestrator_Run_NeverReentersPhase1 (INV-CRYPTO-91)", func() {
	It("reuses the original RequestID on resume and does not re-enter Phase 1", func() {
		setup := newRunTestSetup("01RUN6")
		req := setup.BasicRequest("01PRIM")

		rid, err := setup.Orch.RunPhase1Fresh(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())

		out, err := setup.Orch.Run(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(out.RequestID).To(Equal(rid),
			"INV-CRYPTO-91: resume MUST NOT re-enter Phase 1 — RequestID must be stable")
		Expect(out.Resumed).To(BeTrue())
	})
})

var _ = Describe("Orchestrator_Run_ResumeFromAborted_TerminalError", func() {
	It("does not block a fresh rekey when an aborted sibling exists on the same context", func() {
		setup := newRunTestSetup("01RUN7")
		req := setup.BasicRequest("01PRIM")

		// Seed: insert a checkpoint directly at status='aborted' so the
		// terminal predicate matches.
		rid := dek.RequestID(idgen.New())
		_, err := setup.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id,
           started_at, aborted_at, aborted_reason)
        VALUES ($1, $2, $3, $4, $5, '01PRIM', 'aborted', $6,
                (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
                (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
                'test abort')
    `, rid[:], setup.contextType, setup.contextID,
			opArgsHashForReq(req), make([]byte, 32), setup.oldDEKID)
		Expect(err).NotTo(HaveOccurred())

		// FindByContextAndArgs excludes terminal rows, so Run treats this as
		// "no existing non-terminal" and proceeds to fresh-start — but the
		// partial unique index permits only one NON-terminal checkpoint, and
		// terminal rows do not conflict. Hence fresh-start succeeds, which is
		// the operator's natural recovery from a prior abort. Verify the
		// fresh-start path runs cleanly even though an aborted sibling exists
		// on the same context.
		out, err := setup.Orch.Run(context.Background(), req)
		Expect(err).NotTo(HaveOccurred(), "aborted siblings must not block a fresh rekey")
		Expect(out.Resumed).To(BeFalse())
		Expect(out.RequestID).NotTo(Equal(rid),
			"fresh-start after abort allocates a new RequestID")
	})
})

var _ = Describe("Orchestrator_Run_DispatchFromPhase5Timeout_ForceDestroy (INV-CRYPTO-98)", func() {
	It("routes to RunPhase5WithForceDestroy and completes when ForceDestroy=true", func() {
		setup := newRunTestSetup("01RUN8")
		req := setup.BasicRequest("01PRIM")

		// Drive to Phase 5 timeout: seed an active DEK, mint a new one,
		// open a checkpoint at phase5_invalidate with missing_members set.
		oldRecord, err := setup.Manager.ActiveDEKRow(context.Background(),
			dek.ContextID{Type: setup.contextType, ID: setup.contextID})
		Expect(err).NotTo(HaveOccurred())
		newDEKID, err := setup.Manager.MintNewDEKForRekey(context.Background(), oldRecord.ID)
		Expect(err).NotTo(HaveOccurred())

		forceRID := dek.RequestID(idgen.New())
		_, err = setup.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id,
           phase5_missing_members, phase5_attempt_count)
        VALUES ($1, $2, $3, $4, $5, '01PRIM', 'phase5_invalidate', $6, $7,
                $8::jsonb, 1)
    `, forceRID[:], setup.contextType, setup.contextID,
			opArgsHashForReq(req), make([]byte, 32),
			oldRecord.ID, newDEKID, []byte(`["m2"]`))
		Expect(err).NotTo(HaveOccurred())

		// Resume with force-destroy.
		req.ForceDestroy = true
		out, err := setup.Orch.Run(context.Background(), req)
		Expect(err).NotTo(HaveOccurred(), "force-destroy resume must succeed past Phase 5 timeout")
		Expect(out.Resumed).To(BeTrue())
		Expect(out.RequestID).To(Equal(forceRID))

		ckpt, err := setup.Repo.Get(context.Background(), forceRID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Status).To(Equal(dek.CheckpointStatusComplete))
		Expect(ckpt.ForceDestroy).To(BeTrue(),
			"INV-CRYPTO-98: force-destroy must be recorded on the checkpoint")
	})
})

var _ = Describe("Orchestrator_Run_DispatchFromPhase6 (INV-CRYPTO-99)", func() {
	It("skips Phases 1-5 and runs Phase 6 idempotently then Phase 7 to complete", func() {
		setup := newRunTestSetup("01RUN9")
		req := setup.BasicRequest("01PRIM")

		oldRecord, err := setup.Manager.ActiveDEKRow(context.Background(),
			dek.ContextID{Type: setup.contextType, ID: setup.contextID})
		Expect(err).NotTo(HaveOccurred())
		newDEKID, err := setup.Manager.MintNewDEKForRekey(context.Background(), oldRecord.ID)
		Expect(err).NotTo(HaveOccurred())

		ph6RID := dek.RequestID(idgen.New())
		_, err = setup.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id)
        VALUES ($1, $2, $3, $4, $5, '01PRIM', 'phase6_destroy_old', $6, $7)
    `, ph6RID[:], setup.contextType, setup.contextID,
			opArgsHashForReq(req), make([]byte, 32), oldRecord.ID, newDEKID)
		Expect(err).NotTo(HaveOccurred())

		out, err := setup.Orch.Run(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Resumed).To(BeTrue())

		ckpt, err := setup.Repo.Get(context.Background(), ph6RID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Status).To(Equal(dek.CheckpointStatusComplete))
		Expect(setup.Audit.emitted).To(HaveLen(1))
	})
})

var _ = Describe("Orchestrator_Run_ResumeFromPhase7Audit_ManualRecovery (INV-CRYPTO-91, INV-CRYPTO-88)", func() {
	It("surfaces DEK_REKEY_PHASE7_AUDIT_RETRY_REQUIRED for a checkpoint stuck at phase7_audit", func() {
		setup := newRunTestSetup("01RUN10")
		req := setup.BasicRequest("01PRIM")

		oldRecord, err := setup.Manager.ActiveDEKRow(context.Background(),
			dek.ContextID{Type: setup.contextType, ID: setup.contextID})
		Expect(err).NotTo(HaveOccurred())
		newDEKID, err := setup.Manager.MintNewDEKForRekey(context.Background(), oldRecord.ID)
		Expect(err).NotTo(HaveOccurred())

		ph7RID := dek.RequestID(idgen.New())
		_, err = setup.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id)
        VALUES ($1, $2, $3, $4, $5, '01PRIM', 'phase7_audit', $6, $7)
    `, ph7RID[:], setup.contextType, setup.contextID,
			opArgsHashForReq(req), make([]byte, 32), oldRecord.ID, newDEKID)
		Expect(err).NotTo(HaveOccurred())

		_, err = setup.Orch.Run(context.Background(), req)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "DEK_REKEY_PHASE7_AUDIT_RETRY_REQUIRED")
	})
})

var _ = Describe("Orchestrator_RunByRequestID_PreservesJustificationInAudit (holomush-jxo8.7.55)", func() {
	It("rehydrates the original justification into the Phase 7 audit payload on the explicit-resume path", func() {
		setup := newRunTestSetup("01RUN11")
		const originalJustification = "X"

		// Fresh run with justification 'X': drives Phase 1 only so the
		// checkpoint stays non-terminal (status=phase1_auth).
		req := dek.RekeyRequest{
			ContextType:   setup.contextType,
			ContextID:     setup.contextID,
			Justification: originalJustification,
			Operator:      dek.OperatorIdentity{PlayerID: "01PRIM"},
		}
		rid, err := setup.Orch.RunPhase1Fresh(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())

		// Resume via RunByRequestID — operator supplies a DIFFERENT
		// justification on the resume request. This pins the
		// rehydration-wins semantic (INV-CRYPTO-112 analog): the checkpoint row's
		// Justification is authoritative, the resume-call value MUST be
		// ignored. Without this sentinel, a regression that accidentally
		// honored req.Justification would only be caught if the resume
		// value happened to differ from the stored one. (crypto-reviewer
		// finding #1 on PR #3673)
		const overrideAttempt = "ATTEMPT-TO-OVERRIDE-MUST-BE-IGNORED"
		resumeReq := dek.RekeyRequest{
			Operator:      dek.OperatorIdentity{PlayerID: "01PRIM"},
			Justification: overrideAttempt,
		}
		out, err := setup.Orch.RunByRequestID(context.Background(), rid, resumeReq)
		Expect(err).NotTo(HaveOccurred())
		Expect(out.RequestID).To(Equal(rid))

		// Phase 7 must have emitted exactly one audit event.
		Expect(setup.Audit.emitted).To(HaveLen(1),
			"Phase 7 must emit exactly one audit event on the RunByRequestID path")

		// The emitted payload MUST carry the original justification (X)
		// and MUST NOT carry the resume-call override attempt.
		emittedPayload := setup.Audit.emitted[0]
		Expect(emittedPayload.Justification).To(Equal(originalJustification),
			"RunByRequestID must rehydrate justification from checkpoint row into Phase 7 audit payload")
		Expect(emittedPayload.Justification).NotTo(Equal(overrideAttempt),
			"resume-call Justification MUST NOT win over the stored checkpoint value (INV-CRYPTO-112 analog)")

		// Sanity: checkpoint reached complete.
		ckpt, err := setup.Repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Status).To(Equal(dek.CheckpointStatusComplete))
	})
})

// opArgsHashForReq computes the op_args_hash for a request — mirrors the
// dek.ComputeRekeyArgsHash production path so seeded checkpoint rows match
// what FindByContextAndArgs looks up.
func opArgsHashForReq(req dek.RekeyRequest) []byte {
	h, err := dek.ComputeRekeyArgsHash(req)
	Expect(err).NotTo(HaveOccurred())
	return h[:]
}

var _ = Describe("Orchestrator_Phase7_AuditPayloadHasNonZeroPhase3Count (holomush-jxo8.7.54)", func() {
	It("Phase 7 audit payload Phase3RowsRewritten is > 0 and matches actual rewrite count", func() {
		// Use the phase3TestSetup to get a real encrypted-rows harness
		// (real DEK, real events_audit rows encrypted under oldDEK).
		ph3 := newPhase3TestSetup()

		const rowCount = 3
		ph3.InsertEncryptedRows(rowCount)

		// Drive Phase 3 — rewrites the seeded rows under the new DEK.
		// processPhase3Batch calls IncrementPhase3Count INSIDE each batch tx
		// so ckpt.Phase3RowsRewritten accumulates atomically with the row
		// rewrites and cursor advance.
		n, err := ph3.orch.RunPhase3(context.Background(), ph3.RequestID())
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(rowCount), "RunPhase3 must rewrite all seeded rows")

		// Verify the count is persisted on the checkpoint row.
		ckpt, err := ph3.repo.Get(context.Background(), ph3.RequestID())
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Phase3RowsRewritten).To(Equal(rowCount),
			"checkpoint.Phase3RowsRewritten must be persisted after RunPhase3")

		// Wire Phase 5 (fake coordinator) + Phase 6 + Phase 7 onto the same orchestrator
		// to drive to completion and capture the Phase 7 payload.
		coord := &fakePhase5Coordinator{}
		coord.SetSuccess()
		ph3.orch.SetPhase5Coordinator(coord)
		ph3.orch.SetDestroyer(ph3.manager)
		auditEmitter := &fakeAuditEmitter{}
		ph3.orch.SetAuditEmitter(auditEmitter)
		ph3.orch.SetDataDir(GinkgoT().TempDir())

		// RunPhase5 + RunPhase6 + RunPhase7 in sequence.
		Expect(ph3.orch.RunPhase5(context.Background(), ph3.RequestID())).To(Succeed())
		Expect(ph3.orch.RunPhase6(context.Background(), ph3.RequestID())).To(Succeed())

		req := dek.RekeyRequest{
			ContextType:   "scene",
			ContextID:     "01PH3",
			Justification: "test",
			Operator:      dek.OperatorIdentity{PlayerID: "01PLAYER"},
		}
		outcome, err := ph3.orch.RunPhase7(context.Background(), ph3.RequestID(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(outcome.Phase3RowCount).To(Equal(rowCount),
			"RekeyOutcome.Phase3RowCount must equal the number of rows rewritten by Phase 3")

		// The emitted payload must also carry the count.
		Expect(auditEmitter.emitted).To(HaveLen(1))
		payload := auditEmitter.emitted[0]
		Expect(payload.Phases.Phase3RowsRewritten).To(Equal(rowCount),
			"RekeyAuditPayload.Phases.Phase3RowsRewritten must match actual rewrite count")
		Expect(payload.Phases.Phase3RowsRewritten).To(BeNumerically(">", 0),
			"Phase3RowsRewritten must be > 0 (the canonical fix for holomush-jxo8.7.54)")
	})
})

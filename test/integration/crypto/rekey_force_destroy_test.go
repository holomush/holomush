// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_force_destroy_test.go — E2E specs for the --force-destroy escape hatch.
//
// Covers:
//   - INV-CRYPTO-97 (FORCE-DESTROY-GATED): --force-destroy is rejected when the
//     checkpoint is not in the timed-out Phase 5 state (i.e., when
//     phase5_missing_members IS NULL). Verified at phase1_auth (before Phase 5)
//     and at phase5_invalidate without missing members (after Phase 5 success).
//   - INV-CRYPTO-98 (FORCE-DESTROY-AUDITED): when --force-destroy is used after a
//     Phase 5 timeout, the rekey completes and the audit event carries
//     force_destroy=true and final_missing_members populated.
//
// Because the socket layer's RekeyResume RPC does not yet wire request_id
// through to the orchestrator adapter (that wiring lands in the production
// cmd/holomush wiring bead), force-destroy is exercised by calling the
// orchestrator's Run method directly with req.ForceDestroy=true. The same
// operator + context + justification produce the same op_args_hash, so the
// orchestrator correctly resumes the timed-out checkpoint.
//
// Spec: §4.3 Phase 5 (force-destroy path), §4.1 FSM.
//
// Part of holomush-jxo8.7 (bead jxo8.7.38, merged T43+T44).
package crypto_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// assertAuditEventHasForceDestroy queries events_audit for the most recent
// rekey audit event for SceneContext and asserts force_destroy=true and
// final_missing_members contains all expected members (INV-CRYPTO-98).
func assertAuditEventHasForceDestroy(h *Harness, expectedMissing []string) {
	GinkgoHelper()

	var envelopeBytes []byte
	err := h.DB.QueryRow(context.Background(),
		`SELECT envelope FROM events_audit
		  WHERE subject LIKE $1 AND type = 'crypto.system.rekey'
		  ORDER BY js_seq DESC LIMIT 1`,
		"events.g1.system.rekey.scene.%").Scan(&envelopeBytes)
	Expect(err).NotTo(HaveOccurred(),
		"INV-CRYPTO-98: rekey audit event must exist in events_audit")

	var payload dek.RekeyAuditPayload
	Expect(json.Unmarshal(envelopeBytes, &payload)).To(Succeed(),
		"INV-CRYPTO-98: audit event envelope must be valid RekeyAuditPayload JSON")

	Expect(payload.ForceDestroy).To(BeTrue(),
		"INV-CRYPTO-98: audit payload force_destroy must be true")
	for _, m := range expectedMissing {
		Expect(payload.Phases.Phase5FinalMissingMembers).To(ContainElement(m),
			"INV-CRYPTO-98: audit payload final_missing_members must include %q", m)
	}
}

var _ = Describe("Rekey force-destroy", func() {
	It("E2E_ForceDestroyAuditCapture: --force-destroy bypasses invalidation and captures bypass in audit (INV-CRYPTO-97, INV-CRYPTO-98)", func() {
		h := SetupRekeyHarness(suiteT, WithEventCount(50))
		defer h.Cleanup()

		missingMembers := []string{"member-2"}
		coord := newTimeoutCoordinator(missingMembers)
		h.Primary.GetRekeyOrchestrator().SetPhase5Coordinator(coord)

		justification := "force-destroy audit E2E test"

		// Run initial Rekey — must timeout at Phase 5.
		timeoutErr := runRekeyViaUDSExpectError(
			h,
			h.AdminPlayer.PlayerID,
			h.SceneContext.Type,
			h.SceneContext.ID,
			justification,
		)
		Expect(timeoutErr).To(HaveOccurred(), "Phase 5 must timeout")
		Expect(timeoutErr.Error()).To(ContainSubstring("DEK_REKEY_PHASE5_TIMEOUT"))

		// INV-CRYPTO-97: force-destroy is permitted on a timed-out checkpoint.
		// Drive it via the orchestrator directly (ForceDestroy=true + same args
		// → same op_args_hash → auto-resumes the phase5_timeout checkpoint).
		// The coordinator is still in timeout mode; force-destroy skips it.
		forceReq := dek.RekeyRequest{
			ContextType:   h.SceneContext.Type,
			ContextID:     h.SceneContext.ID,
			Justification: justification, // same → same op_args_hash → auto-resume
			Operator:      dek.OperatorIdentity{PlayerID: h.AdminPlayer.PlayerID},
			ForceDestroy:  true,
		}
		outcome, forceErr := h.Primary.GetRekeyOrchestrator().Run(
			context.Background(), forceReq,
		)
		Expect(forceErr).NotTo(HaveOccurred(),
			"INV-CRYPTO-97: force-destroy on a phase5_timeout checkpoint must succeed")
		Expect(outcome.ForceDestroyUsed).To(BeTrue(),
			"INV-CRYPTO-98: RekeyOutcome.ForceDestroyUsed must be true")

		// Checkpoint must be complete.
		h.AssertCheckpointStatus(outcome.RequestID, dek.CheckpointStatusComplete)

		// INV-CRYPTO-98: audit event must capture force_destroy=true + missing members.
		assertAuditEventHasForceDestroy(h, missingMembers)

		// Audit chain intact (INV-CRYPTO-101/INV-CRYPTO-102).
		h.AssertRekeyChainIntactForContext(h.SceneContext)
	})

	It("INV-CRYPTO-97: force-destroy is rejected when Phase 5 has not timed out (pre-timeout)", func() {
		h := SetupRekeyHarness(suiteT, WithEventCount(10))
		defer h.Cleanup()

		// Open a non-terminal checkpoint at phase1_auth (before Phase 5 runs).
		rid := openNonTerminalCheckpoint(h, "pre-timeout force-destroy test")

		// Drive Phase 2 so the checkpoint is at phase2_mint_dek — still before
		// Phase 5. Force-destroy must be rejected at any pre-Phase5-timeout state.
		runErr := h.Primary.GetRekeyOrchestrator().RunPhase2(context.Background(), rid)
		Expect(runErr).NotTo(HaveOccurred(), "Phase 2 must succeed for fixture setup")

		// Attempt force-destroy via RunPhase5WithForceDestroy at a non-timeout state.
		forceErr := h.Primary.GetRekeyOrchestrator().RunPhase5WithForceDestroy(
			context.Background(), rid,
		)
		Expect(forceErr).To(HaveOccurred(),
			"INV-CRYPTO-97: force-destroy must be rejected before phase5_timeout")
		Expect(forceErr.Error()).To(ContainSubstring("phase5_invalidate"),
			"INV-CRYPTO-97: rejection message must reference required state")
	})

	It("INV-CRYPTO-97: force-destroy is rejected after successful Phase 5 (phase5_complete / no missing members)", func() {
		h := SetupRekeyHarness(suiteT, WithEventCount(10))
		defer h.Cleanup()

		// The harness default coordinator is noopPhase5Coordinator (all acks succeed).
		// Open a checkpoint and drive through Phase 5 successfully.
		rid := openNonTerminalCheckpoint(h, "phase5-success force-destroy test")
		runPhase2Err := h.Primary.GetRekeyOrchestrator().RunPhase2(context.Background(), rid)
		Expect(runPhase2Err).NotTo(HaveOccurred())
		_, runPhase3Err := h.Primary.GetRekeyOrchestrator().RunPhase3(context.Background(), rid)
		Expect(runPhase3Err).NotTo(HaveOccurred())
		runPhase5Err := h.Primary.GetRekeyOrchestrator().RunPhase5(context.Background(), rid)
		Expect(runPhase5Err).NotTo(HaveOccurred(), "Phase 5 must succeed with default noop coordinator")

		// Verify the checkpoint is at phase5_invalidate with NO missing members.
		ckpt, err := h.Primary.GetCheckpointRepo().Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Phase5HasMissingMembers()).To(BeFalse(),
			"after successful Phase 5, phase5_missing_members must be NULL")

		// Attempt force-destroy: must be rejected because missing_members IS NULL.
		forceErr := h.Primary.GetRekeyOrchestrator().RunPhase5WithForceDestroy(
			context.Background(), rid,
		)
		Expect(forceErr).To(HaveOccurred(),
			"INV-CRYPTO-97: force-destroy must be rejected after a successful Phase 5")
		Expect(forceErr.Error()).To(ContainSubstring("phase5_invalidate"),
			"INV-CRYPTO-97: rejection message must reference the required phase5_invalidate+missing_members state")
	})
})

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_resume_rpc_test.go — E2E specs for the explicit RekeyResume RPC path.
//
// The existing rekey_resume_test.go covers the auto-resume path: AdminService.Rekey
// with the same args finds the non-terminal checkpoint via FindByContextAndArgs and
// picks it up. This file covers AdminService.RekeyResume, which accepts a request_id
// directly and delegates to Orchestrator.RunByRequestID.
//
// Verifies:
//   - INV-CRYPTO-91 (resume-not-restart): RekeyResume reuses the original RequestID when
//     given the exact request_id bytes from Phase 1.
//   - INV-CRYPTO-103 (operator binding): RekeyResume with a request_id owned by a different
//     player is rejected with DEK_REKEY_RESUME_OPERATOR_MISMATCH.
//   - Bug regression (code-reviewer finding, phase5-sub-epic-e): RekeyRunRequest.RequestID
//     was discarded with `_ = ridFixed` before this fix; Orchestrator.RunByRequestID
//     was never called. This spec fails against the pre-fix handler.
//
// Part of holomush-jxo8.7 (phase5-sub-epic-e code-reviewer fix).
package crypto_test

import (
	"context"
	"time"

	"connectrpc.com/connect"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// runRekeyResumeViaUDS calls AdminService.RekeyResume over the UDS using the
// given sessionToken (which noopRekeySessionStore echoes as the player_id) and
// the explicit request_id. Returns (rekeyCompleted{}, error) on the RekeyError
// path.
func runRekeyResumeViaUDS(
	h *Harness,
	sessionToken string,
	rid dek.RequestID,
	forceDestroy bool,
) (rekeyCompleted, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	stream, err := h.AdminCli.Raw().RekeyResume(ctx, connect.NewRequest(&adminv1.RekeyResumeRequest{
		SessionToken: sessionToken,
		RequestId:    rid[:],
		ForceDestroy: forceDestroy,
	}))
	if err != nil {
		return rekeyCompleted{}, err
	}

	for stream.Receive() {
		msg := stream.Msg()
		switch ev := msg.Event.(type) {
		case *adminv1.RekeyProgress_Completed:
			return rekeyCompleted{
				RequestID:    ev.Completed.GetRequestId(),
				AuditEventID: ev.Completed.GetAuditEventId(),
			}, nil
		case *adminv1.RekeyProgress_Error:
			return rekeyCompleted{}, connect.NewError(
				connect.CodeInternal,
				//nolint:goerr113 // dynamic error; test context, not production
				errorFromString(ev.Error.GetCode()+": "+ev.Error.GetMessage()),
			)
		}
	}
	if err := stream.Err(); err != nil {
		return rekeyCompleted{}, err
	}
	return rekeyCompleted{}, connect.NewError(connect.CodeInternal,
		errorFromString("RekeyResume stream closed without terminal event"))
}

var _ = Describe("RekeyResume RPC explicit request_id path", func() {
	It("resumes by request_id and reuses the original RequestID (INV-CRYPTO-91, bug regression)", func() {
		// Boot the harness with default fixture.
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Open a non-terminal checkpoint via Phase 1 directly. This simulates
		// a partial run that left the checkpoint at phase1_auth, before Phase 2
		// minted the new DEK.
		//
		// Before the fix, RekeyResume discarded ridFixed with `_ = ridFixed`,
		// so RunByRequestID was never called. The orchestrator received an empty
		// ContextType/ContextID/Justification, FindByContextAndArgs returned
		// no match, FindNonTerminalByContext returned no match (different empty
		// context), and RunPhase1Fresh was entered with empty fields —
		// selectActive({Type: "", ID: ""}) failed. This test should fail against
		// the pre-fix handler.
		firstRID := openNonTerminalCheckpoint(h, "explicit resume test")

		// Verify the checkpoint is non-terminal before the resume.
		ckpt0, err := h.Primary.GetCheckpointRepo().Get(context.Background(), firstRID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt0.Status.IsTerminal()).To(BeFalse(),
			"INV-CRYPTO-91: checkpoint must be non-terminal before resume")

		// Call AdminService.RekeyResume with the explicit request_id.
		// noopRekeySessionStore echoes the session token as the player_id.
		out, resumeErr := runRekeyResumeViaUDS(
			h,
			h.AdminPlayer.PlayerID,
			firstRID,
			false,
		)
		Expect(resumeErr).NotTo(HaveOccurred(),
			"RekeyResume with valid request_id must complete without error")

		// Extract the returned RequestID from the completed event.
		var returnedRID dek.RequestID
		copy(returnedRID[:], out.RequestID)

		// INV-CRYPTO-91: RunByRequestID must re-use the existing RequestID, not
		// allocate a new one via RunPhase1Fresh.
		Expect(returnedRID).To(Equal(firstRID),
			"INV-CRYPTO-91: RekeyResume MUST NOT re-enter Phase 1 — RequestID must be stable")

		// Checkpoint must be complete after the explicit resume.
		h.AssertCheckpointStatus(firstRID, dek.CheckpointStatusComplete)

		// Audit chain intact (INV-CRYPTO-101/INV-CRYPTO-102).
		h.AssertRekeyChainIntactForContext(h.SceneContext)
	})

	It("rejects RekeyResume from a different operator (INV-CRYPTO-103)", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Park a non-terminal checkpoint under AdminPlayer.
		firstRID := openNonTerminalCheckpoint(h, "operator binding test")

		// PartnerPlayer attempts to resume using AdminPlayer's request_id.
		// Must be rejected because primary_player_id != PartnerPlayer.PlayerID.
		_, err := runRekeyResumeViaUDS(
			h,
			h.PartnerPlayer.PlayerID, // different player
			firstRID,
			false,
		)
		Expect(err).To(HaveOccurred(), "different-operator RekeyResume must fail")
		Expect(err.Error()).To(ContainSubstring("DEK_REKEY_RESUME_OPERATOR_MISMATCH"),
			"INV-CRYPTO-103: wrong operator must receive DEK_REKEY_RESUME_OPERATOR_MISMATCH")
	})
})

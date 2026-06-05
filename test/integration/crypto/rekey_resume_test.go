// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_resume_test.go — E2E specs for Rekey resume, operator-mismatch, and
// args-conflict paths.
//
// Verifies:
//   - INV-CRYPTO-91 (resume-not-restart): after a non-terminal checkpoint is opened
//     directly via the orchestrator (simulating a pre-Phase-3 crash), the same
//     args UDS invocation auto-resumes and completes with Resumed=true, reusing
//     the original RequestID.
//   - INV-CRYPTO-103 (operator binding): a different operator attempting to resume
//     with the same args is rejected with DEK_REKEY_RESUME_OPERATOR_MISMATCH.
//   - INV-CRYPTO-111 (args-hash idempotency): a concurrent fresh-start attempt with
//     different args is rejected with DEK_REKEY_ARGS_CONFLICT when a
//     non-terminal checkpoint exists for the same context.
//
// Part of holomush-jxo8.7 (bead jxo8.7.37, merged T41+T42).
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

// runRekeyViaUDSWithPlayer calls AdminService.Rekey over the UDS using the
// given sessionToken (which noopRekeySessionStore echoes as the player_id)
// and reads the stream until RekeyCompleted or RekeyError arrives.
//
// Returns (rekeyCompleted{}, error) on the RekeyError path (error carries the
// typed code and message from the progress event).
func runRekeyViaUDSWithPlayer(
	h *Harness,
	sessionToken string,
	contextType, contextID, justification string,
) (rekeyCompleted, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	stream, err := h.AdminCli.Raw().Rekey(ctx, connect.NewRequest(&adminv1.RekeyRequest{
		SessionToken:  sessionToken,
		ContextType:   contextType,
		ContextId:     contextID,
		Justification: justification,
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
		errorFromString("Rekey stream closed without terminal event"))
}

// findActiveCheckpoint returns the non-terminal checkpoint for SceneContext.
// Fails the test if no active checkpoint exists.
func findActiveCheckpoint(h *Harness) dek.RequestID {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ckpt, found, err := h.Primary.GetCheckpointRepo().FindNonTerminalByContext(
		ctx,
		h.SceneContext.Type,
		h.SceneContext.ID,
	)
	Expect(err).NotTo(HaveOccurred(), "FindNonTerminalByContext must not error")
	Expect(found).To(BeTrue(), "expected an active (non-terminal) checkpoint for SceneContext")
	return ckpt.RequestID
}

// openNonTerminalCheckpoint drives Phase 1 of the orchestrator directly (not
// via UDS) to leave a non-terminal checkpoint at status=phase1_auth, simulating
// the state a crashed-mid-Phase-1 or pre-Phase-2 run would leave behind. The
// UDS can then call Rekey with the same operator + args to auto-resume.
//
// Returns the RequestID allocated by Phase 1.
func openNonTerminalCheckpoint(h *Harness, justification string) dek.RequestID {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orch := h.Primary.GetRekeyOrchestrator()
	req := dek.RekeyRequest{
		ContextType:   h.SceneContext.Type,
		ContextID:     h.SceneContext.ID,
		Justification: justification,
		Operator: dek.OperatorIdentity{
			PlayerID:     h.AdminPlayer.PlayerID,
			TOTPVerified: true,
		},
	}
	rid, err := orch.RunPhase1Fresh(ctx, req)
	Expect(err).NotTo(HaveOccurred(), "openNonTerminalCheckpoint: RunPhase1Fresh must succeed")
	return rid
}

var _ = Describe("Rekey resume", func() {
	It("auto-resumes same-args invocation after pre-Phase-2 park (INV-CRYPTO-91, INV-CRYPTO-103)", func() {
		// Boot the harness with default fixture.
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Open a non-terminal checkpoint via Phase 1 directly. This simulates
		// a partial run that left the checkpoint at phase1_auth, before Phase 2
		// minted the new DEK.
		firstRID := openNonTerminalCheckpoint(h, "test reason")

		// Verify the checkpoint is non-terminal before the resume.
		ckpt0, err := h.Primary.GetCheckpointRepo().Get(context.Background(), firstRID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt0.Status.IsTerminal()).To(BeFalse(),
			"INV-CRYPTO-91: checkpoint must be non-terminal before resume")

		// Second call via UDS: same player_id (session token = player ID via
		// noopRekeySessionStore), same context, same justification → same
		// op_args_hash → auto-resume path, bypassing dual-control approval
		// (INV-CRYPTO-103).
		out, resumeErr := runRekeyViaUDSWithPlayer(
			h,
			h.AdminPlayer.PlayerID,
			h.SceneContext.Type,
			h.SceneContext.ID,
			"test reason",
		)
		Expect(resumeErr).NotTo(HaveOccurred(), "same-args invocation must resume without error")

		// Extract RequestID from the completed response.
		var rid dek.RequestID
		copy(rid[:], out.RequestID)

		// INV-CRYPTO-91: dispatcher must re-use the existing RequestID, not allocate a new one.
		Expect(rid).To(Equal(firstRID),
			"INV-CRYPTO-91: resume MUST NOT re-enter Phase 1 — RequestID must be stable")

		// Checkpoint must be complete.
		h.AssertCheckpointStatus(rid, dek.CheckpointStatusComplete)

		// Audit chain intact (INV-CRYPTO-101/INV-CRYPTO-102).
		h.AssertRekeyChainIntactForContext(h.SceneContext)
	})

	It("rejects resume from a different operator (INV-CRYPTO-103)", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Park a non-terminal checkpoint under AdminPlayer.
		openNonTerminalCheckpoint(h, "test")

		// A different operator (PartnerPlayer) attempts to resume with the
		// same context + justification (same op_args_hash). Must be rejected
		// because primary_player_id doesn't match.
		_, err := runRekeyViaUDSWithPlayer(
			h,
			h.PartnerPlayer.PlayerID, // different player_id
			h.SceneContext.Type,
			h.SceneContext.ID,
			"test",
		)
		Expect(err).To(HaveOccurred(), "different-operator resume must fail")
		Expect(err.Error()).To(ContainSubstring("DEK_REKEY_RESUME_OPERATOR_MISMATCH"),
			"INV-CRYPTO-103: wrong operator must receive DEK_REKEY_RESUME_OPERATOR_MISMATCH")
	})

	It("rejects concurrent fresh start with different args (INV-CRYPTO-111)", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Park a non-terminal checkpoint with one justification ("first reason").
		openNonTerminalCheckpoint(h, "first reason")

		// Same operator but different justification → different op_args_hash →
		// args-conflict (the existing non-terminal checkpoint blocks the attempt).
		_, err := runRekeyViaUDSWithPlayer(
			h,
			h.AdminPlayer.PlayerID,
			h.SceneContext.Type,
			h.SceneContext.ID,
			"different reason",
		)
		Expect(err).To(HaveOccurred(), "different-args fresh start must fail")
		Expect(err.Error()).To(ContainSubstring("DEK_REKEY_ARGS_CONFLICT"),
			"INV-CRYPTO-111: different args on same context must receive DEK_REKEY_ARGS_CONFLICT")
	})
})

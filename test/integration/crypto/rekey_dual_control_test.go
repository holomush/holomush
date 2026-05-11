// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_dual_control_test.go — E2E dual-control approval flow for the Rekey
// orchestrator.
//
// Verifies that a rekey requiring a second-operator approval completes when the
// partner approves: the orchestrator is given a DualControlBinding with a
// pre-approved admin_approvals row, the rekey runs all 7 phases, and the
// resulting audit event embeds the partner's identity (dual_control_partner
// field in the rekey audit payload).
//
// Note on UDS-level approval blocking: the CLI-side openApprovalAndWait
// function is not yet implemented (returns NOT_IMPLEMENTED). This test
// therefore exercises the orchestrator-level dual-control path directly,
// using approval.PostgresRepo to open and immediately approve the row.
// The observable invariant under test is the audit-event embedding of the
// dual-control partner, which is what the spec requires the orchestrator to
// enforce.
//
// Part of holomush-jxo8.7 (bead jxo8.7.36, merged T39+T40).
package crypto_test

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/admin/approval"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

var _ = Describe("Rekey with dual-control site policy", func() {
	It("completes with partner identity embedded in the audit event", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		// --- Step 1: open and immediately approve an admin_approvals row ---
		// This simulates the second-operator approving the request before
		// the primary invokes the orchestrator.
		approvalRepo := approval.NewPostgresRepo(h.DB, nil)

		argsHash, err := dek.ComputeRekeyArgsHash(dek.RekeyRequest{
			ContextType:   h.SceneContext.Type,
			ContextID:     h.SceneContext.ID,
			Justification: "dual-control E2E test",
		})
		Expect(err).NotTo(HaveOccurred())

		approvalID, err := approvalRepo.Open(ctx, approval.OpenRequest{
			PrimaryPlayerID: h.AdminPlayer.PlayerID,
			OpKind:          "rekey",
			OpArgsHash:      argsHash[:],
		})
		Expect(err).NotTo(HaveOccurred(), "approvalRepo.Open must succeed")

		// Partner approves the request.
		err = approvalRepo.MarkApproved(ctx, approvalID, h.PartnerPlayer.PlayerID)
		Expect(err).NotTo(HaveOccurred(), "MarkApproved must succeed")

		// --- Step 2: run the orchestrator with the DualControlBinding ---
		orch := h.Primary.GetRekeyOrchestrator()
		req := dek.RekeyRequest{
			ContextType:   h.SceneContext.Type,
			ContextID:     h.SceneContext.ID,
			Justification: "dual-control E2E test",
			Operator: dek.OperatorIdentity{
				PlayerID:     h.AdminPlayer.PlayerID,
				TOTPVerified: true,
			},
			DualControl: &dek.DualControlBinding{
				ApprovalRequestID: approvalID,
				PartnerPlayerID:   h.PartnerPlayer.PlayerID,
			},
		}

		out, err := orch.Run(ctx, req)
		Expect(err).NotTo(HaveOccurred(), "orchestrator Run must succeed")
		Expect(out.AuditEventID.String()).NotTo(BeEmpty(), "AuditEventId must be set")

		// --- Step 3: post-state assertions ---

		// Checkpoint must be complete.
		h.AssertCheckpointStatus(out.RequestID, dek.CheckpointStatusComplete)

		// New DEK is version 2.
		h.AssertCryptoKeysActiveVersion(h.SceneContext, 2)

		// Audit chain is intact.
		h.AssertRekeyChainIntactForContext(h.SceneContext)

		// --- Step 4: verify dual_control_partner is embedded in the audit event ---
		// Fetch the events_audit row for the rekey audit event by subject pattern
		// and event type, then decode the envelope to check the payload.
		var rawEnvelope []byte
		err = h.DB.QueryRow(
			ctx,
			`SELECT envelope FROM events_audit
			  WHERE subject LIKE $1
			    AND type = $2
			  ORDER BY timestamp DESC
			  LIMIT 1`,
			"events.g1.system.rekey.%",
			"crypto.system.rekey",
		).Scan(&rawEnvelope)
		Expect(err).NotTo(HaveOccurred(), "must find system.rekey audit event")

		// The envelope codec for system.rekey is 'identity' (cleartext JSON).
		var payload dek.RekeyAuditPayload
		err = json.Unmarshal(rawEnvelope, &payload)
		Expect(err).NotTo(HaveOccurred(), "audit envelope must unmarshal as RekeyAuditPayload")

		Expect(payload.DualControlPartner).NotTo(BeNil(),
			"dual_control_partner must be non-nil when DualControlBinding was supplied")
		Expect(payload.DualControlPartner.PlayerID).To(Equal(h.PartnerPlayer.PlayerID),
			"dual_control_partner.player_id must match the partner player")
		Expect(payload.DualControlPartner.ApprovalRequestID).To(Equal(approvalID.String()),
			"dual_control_partner.approval_request_id must match the approval row")
	})
})

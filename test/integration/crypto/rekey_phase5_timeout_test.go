// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_phase5_timeout_test.go — E2E spec for Phase 5 cluster-invalidation timeout.
//
// Covers:
//   - Phase 5 partial-ack timeout: the coordinator returns a partial-failure;
//     the checkpoint persists phase5_missing_members; retry after cluster heal
//     increments phase5_attempt_count and completes successfully.
//
// The IsolateReplica/ReconnectReplica harness stubs are no-ops (full cluster
// network isolation is not yet wired; see TODO(holomush-jxo8.7.36)). This
// test injects a timeout coordinator directly onto the orchestrator to
// simulate the same observable behaviour: DEK_REKEY_PHASE5_TIMEOUT on the
// first Rekey call; incremented attempt_count and success on a second Rekey
// invocation with the same args (auto-resume path).
//
// Spec: spec §4.3 Phase 5; INV-CRYPTO-109.
//
// Part of holomush-jxo8.7 (bead jxo8.7.38, merged T43+T44).
package crypto_test

import (
	"context"
	"time"

	"connectrpc.com/connect"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// timeoutPhase5Coordinator always returns a partial-ack INVALIDATION_PARTIAL_FAILURE
// error until SetSuccess is called. Thread-safe: used from a single goroutine
// (the orchestrator) sequentially.
type timeoutPhase5Coordinator struct {
	shouldSucceed bool
	missing       []string
}

func newTimeoutCoordinator(missing []string) *timeoutPhase5Coordinator {
	return &timeoutPhase5Coordinator{missing: missing}
}

func (c *timeoutPhase5Coordinator) SetSuccess() {
	c.shouldSucceed = true
}

func (c *timeoutPhase5Coordinator) RequestInvalidation(
	_ context.Context,
	_ dek.ContextID,
	_ string,
	_, _ uint32,
) error {
	if c.shouldSucceed {
		return nil
	}
	return oops.Code("INVALIDATION_PARTIAL_FAILURE").
		With("missing_members", append([]string(nil), c.missing...)).
		Errorf("simulated partial-ack timeout: missing %d members", len(c.missing))
}

// runRekeyViaUDSExpectError runs Rekey over the UDS and returns the error
// from the terminal RekeyError stream event.
func runRekeyViaUDSExpectError(
	h *Harness,
	sessionToken string,
	contextType, contextID, justification string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	stream, err := h.AdminCli.Raw().Rekey(ctx, connect.NewRequest(&adminv1.RekeyRequest{
		SessionToken:  sessionToken,
		ContextType:   contextType,
		ContextId:     contextID,
		Justification: justification,
	}))
	if err != nil {
		return err
	}

	for stream.Receive() {
		msg := stream.Msg()
		switch ev := msg.Event.(type) {
		case *adminv1.RekeyProgress_Completed:
			_ = ev
			return nil // unexpected success
		case *adminv1.RekeyProgress_Error:
			return errorFromString(ev.Error.GetCode() + ": " + ev.Error.GetMessage())
		}
	}
	if err := stream.Err(); err != nil {
		return err
	}
	return errorFromString("Rekey stream closed without terminal event")
}

// getRekeyStatus calls RekeyStatus over the UDS and returns the response.
func getRekeyStatus(
	h *Harness,
	sessionToken string,
	requestID dek.RequestID,
) (*adminv1.RekeyStatusResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := h.AdminCli.Raw().RekeyStatus(ctx, connect.NewRequest(&adminv1.RekeyStatusRequest{
		SessionToken: sessionToken,
		RequestId:    requestID[:],
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

var _ = Describe("Rekey Phase 5 timeout", func() {
	It("persists missing_members and reattempts succeed on cluster heal", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Inject a coordinator that always returns partial-ack timeout.
		coord := newTimeoutCoordinator([]string{"member-2"})
		h.Primary.GetRekeyOrchestrator().SetPhase5Coordinator(coord)

		justification := "phase5 timeout E2E test"

		// Run initial Rekey — Phase 5 must timeout.
		timeoutErr := runRekeyViaUDSExpectError(
			h,
			h.AdminPlayer.PlayerID,
			h.SceneContext.Type,
			h.SceneContext.ID,
			justification,
		)
		Expect(timeoutErr).To(HaveOccurred(), "Phase 5 must return an error on timeout")
		Expect(timeoutErr.Error()).To(ContainSubstring("DEK_REKEY_PHASE5_TIMEOUT"),
			"error code must be DEK_REKEY_PHASE5_TIMEOUT")

		// Find the active (non-terminal) checkpoint.
		rid := findActiveCheckpoint(h)

		// Verify status via RPC: missing_members populated, attempt_count >= 1.
		status, err := getRekeyStatus(h, h.AdminPlayer.PlayerID, rid)
		Expect(err).NotTo(HaveOccurred(), "RekeyStatus must succeed for timed-out checkpoint")
		Expect(status.GetPhase5MissingMembers()).To(ContainElement("member-2"),
			"phase5_missing_members must contain the missing replica")
		Expect(status.GetPhase5AttemptCount()).To(BeNumerically(">=", 1),
			"phase5_attempt_count must be at least 1 after the first timeout")

		// "Heal" the cluster by switching coordinator to success.
		coord.SetSuccess()

		// Resume by calling Rekey with the SAME args (same context + justification).
		// The orchestrator auto-resumes: same op_args_hash + same operator →
		// driveToCompletion picks up from phase5_invalidate (missing_members set)
		// → runs Phase 5 retry (which now succeeds) → Phase 6 → Phase 7 → complete.
		out, resumeErr := runRekeyViaUDS(
			h,
			h.AdminPlayer.PlayerID,
			h.SceneContext.Type,
			h.SceneContext.ID,
			justification, // same justification → same op_args_hash → auto-resume
		)
		Expect(resumeErr).NotTo(HaveOccurred(),
			"Rekey with same args after cluster heal must complete successfully")
		Expect(out.RequestID).NotTo(BeEmpty(), "RequestId must be set on completion")

		// The resumed run must reuse the SAME request ID (INV-CRYPTO-91).
		var resumedRID dek.RequestID
		copy(resumedRID[:], out.RequestID)
		Expect(resumedRID).To(Equal(rid),
			"INV-CRYPTO-91: same-args invocation after timeout must reuse the existing RequestID")

		// Checkpoint must be complete.
		h.AssertCheckpointStatus(resumedRID, dek.CheckpointStatusComplete)

		// Phase5 attempt count must have incremented (at least 2 after retry).
		finalStatus, statusErr := getRekeyStatus(h, h.AdminPlayer.PlayerID, resumedRID)
		Expect(statusErr).NotTo(HaveOccurred())
		Expect(finalStatus.GetPhase5AttemptCount()).To(BeNumerically(">=", 2),
			"phase5_attempt_count must reflect both the timeout and the retry")

		// Audit chain intact (INV-CRYPTO-101/INV-CRYPTO-102).
		h.AssertRekeyChainIntactForContext(h.SceneContext)
	})
})

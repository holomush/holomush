// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_abort_test.go — E2E spec for the Rekey abort path.
//
// Verifies INV-CRYPTO-104 (ABORT-NO-DUAL-CONTROL): the RekeyAbort RPC accepts a
// single-control invocation; abort is non-destructive (the in-progress
// checkpoint is marked aborted, the old DEK remains valid, reads continue).
// A subsequent fresh-start rekey succeeds after the abort.
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

var _ = Describe("Rekey abort", func() {
	It("aborts an in-flight checkpoint and allows a fresh start (INV-CRYPTO-104)", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Open a non-terminal checkpoint via Phase 1 directly. This represents
		// an in-flight rekey that has not yet completed, available for operator
		// intervention via abort.
		rid := openNonTerminalCheckpoint(h, "first")

		// Verify the checkpoint is non-terminal before the abort.
		ckpt0, err := h.Primary.GetCheckpointRepo().Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt0.Status.IsTerminal()).To(BeFalse(), "checkpoint must be non-terminal before abort")

		// INV-CRYPTO-104: abort must succeed via single-control. The abort handler
		// requires only crypto.operator capability — no dual-control approval
		// is accepted or required. This test verifies that single-control
		// RekeyAbort succeeds even for an in-flight checkpoint.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		abortResp, err := h.AdminCli.Raw().RekeyAbort(ctx, connect.NewRequest(&adminv1.RekeyAbortRequest{
			SessionToken: h.AdminPlayer.PlayerID, // noopRekeySessionStore echoes as player_id
			RequestId:    rid[:],
		}))
		Expect(err).NotTo(HaveOccurred(),
			"INV-CRYPTO-104: RekeyAbort must succeed via single-control")
		Expect(abortResp.Msg.GetAbortedAt()).NotTo(BeNil(),
			"AbortedAt must be populated in the response")
		Expect(abortResp.Msg.GetAuditEventId()).NotTo(BeEmpty(),
			"AuditEventId must be set in the abort response (chained audit emitted)")

		// Verify the checkpoint is now in the aborted terminal state.
		h.AssertCheckpointStatus(rid, dek.CheckpointStatusAborted)

		// A fresh-start rekey on the same context must now succeed (the
		// terminal aborted row no longer blocks via the UNIQUE partial index).
		out, freshErr := runRekeyViaUDSWithPlayer(
			h,
			h.AdminPlayer.PlayerID,
			h.SceneContext.Type,
			h.SceneContext.ID,
			"second",
		)
		Expect(freshErr).NotTo(HaveOccurred(),
			"fresh-start rekey after abort must complete successfully")

		var freshRID dek.RequestID
		copy(freshRID[:], out.RequestID)

		// Fresh start must NOT resume the aborted checkpoint.
		Expect(freshRID).NotTo(Equal(rid),
			"fresh-start must allocate a new RequestID, not reuse the aborted one")

		// New checkpoint must be complete.
		h.AssertCheckpointStatus(freshRID, dek.CheckpointStatusComplete)

		// Audit chain intact after the fresh rekey completed.
		h.AssertRekeyChainIntactForContext(h.SceneContext)
	})
})

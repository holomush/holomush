// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_happy_path_test.go — E2E happy path for the 7-phase Rekey orchestrator.
//
// Verifies: full fresh rekey completes end-to-end, checkpoint advances to
// complete, new DEK is active at version+1, old DEK has destroyed_at set,
// and the rekey audit chain integrity holds (INV-CRYPTO-101/INV-CRYPTO-102).
//
// Part of holomush-jxo8.7 (bead jxo8.7.36, merged T39+T40).
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

// rekeyCompleted collects the terminal outcome from a Rekey server-stream.
type rekeyCompleted struct {
	RequestID    []byte
	AuditEventID []byte
	OldDEKID     int64 // captured from RekeyStatus after completion
}

// runRekeyViaUDS calls AdminService.Rekey over the UDS using sessionToken as
// the bearer (noopRekeySessionStore echoes the token as player_id). It reads
// the stream until RekeyCompleted or RekeyError arrives.
func runRekeyViaUDS(
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

// errorFromString converts a string to an error value (test helper to avoid
// importing errors/fmt into every caller).
func errorFromString(s string) error {
	return rekeyStringError(s)
}

type rekeyStringError string

func (e rekeyStringError) Error() string { return string(e) }

var _ = Describe("Rekey happy path", func() {
	It("completes all 7 phases and advances the chain head", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Run the full rekey via the admin UDS. SessionToken = AdminPlayer.PlayerID
		// because noopRekeySessionStore echoes the token as the player ID.
		out, err := runRekeyViaUDS(
			h,
			h.AdminPlayer.PlayerID,
			h.SceneContext.Type,
			h.SceneContext.ID,
			"Forced revocation, ticket #1234",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(out.AuditEventID).NotTo(BeEmpty(), "AuditEventId must be non-empty")
		Expect(out.RequestID).NotTo(BeEmpty(), "RequestId must be non-empty")

		// Convert the 16-byte request_id to dek.RequestID for assertion helpers.
		var rid dek.RequestID
		copy(rid[:], out.RequestID)

		// Verify checkpoint reached terminal status = complete.
		h.AssertCheckpointStatus(rid, dek.CheckpointStatusComplete)

		// Verify new DEK is version 2 (initial DEK was version 1).
		h.AssertCryptoKeysActiveVersion(h.SceneContext, 2)

		// Retrieve old DEK id from the checkpoint row and verify destroyed_at is set.
		ckpt, err2 := h.Primary.GetCheckpointRepo().Get(context.Background(), rid)
		Expect(err2).NotTo(HaveOccurred())
		h.AssertCryptoKeysDestroyedAtSet(ckpt.OldDEKID)

		// Verify the rekey audit chain is intact (INV-CRYPTO-101/INV-CRYPTO-102).
		h.AssertRekeyChainIntactForContext(h.SceneContext)
	})
})

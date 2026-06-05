// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_phase7_audit_failure_test.go — E2E spec for Phase 7 audit-emit failure.
//
// Covers INV-CRYPTO-100:
//   - When the Phase 7 audit-event emit fails, the rekey DB state (DEK rows) is
//     irreversibly committed (old DEK destroyed, new DEK active at version+1).
//   - A fallback log file is written to <data_dir>/audit-fallback/rekey-<rid>.log.
//   - The checkpoint is left at phase7_audit (not complete), so manual retry is
//     possible.
//   - The Rekey RPC returns DEK_REKEY_PHASE7_AUDIT_FAILED.
//
// Spec: §4.3 Phase 7, INV-CRYPTO-100.
//
// Part of holomush-jxo8.7 (bead jxo8.7.38, merged T43+T44).
package crypto_test

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// alwaysFailAuditEmitter satisfies dek.AuditEmitter and always returns an error.
// Used to simulate Phase 7 audit-emit failure in E2E tests (INV-CRYPTO-100).
type alwaysFailAuditEmitter struct{}

func (*alwaysFailAuditEmitter) Emit(_ context.Context, p dek.RekeyAuditPayload) (ulid.ULID, dek.RekeyAuditPayload, error) {
	return ulid.ULID{}, p, errors.New("simulated Phase 7 audit emit failure (E2E fault injection)")
}

var _ = Describe("Rekey Phase 7 audit failure", func() {
	It("commits DB state and writes fallback log on emit failure (INV-CRYPTO-100)", func() {
		h := SetupRekeyHarness(suiteT, WithEventCount(20))
		defer h.Cleanup()

		// Configure a data directory so writeFallbackLog has somewhere to write.
		dataDir := suiteT.TempDir()
		h.Primary.GetRekeyOrchestrator().SetDataDir(dataDir)

		// Inject a failing audit emitter. The orchestrator invokes this at
		// Phase 7; the DEK state (Phases 1–6) has already been committed.
		h.Primary.GetRekeyOrchestrator().SetAuditEmitter(&alwaysFailAuditEmitter{})

		// Run the full rekey. Phase 7 must return DEK_REKEY_PHASE7_AUDIT_FAILED.
		rekeyErr := runRekeyViaUDSExpectError(
			h,
			h.AdminPlayer.PlayerID,
			h.SceneContext.Type,
			h.SceneContext.ID,
			"audit failure test",
		)
		Expect(rekeyErr).To(HaveOccurred(), "rekey must fail when audit emit fails")
		Expect(rekeyErr.Error()).To(ContainSubstring("DEK_REKEY_PHASE7_AUDIT_FAILED"),
			"INV-CRYPTO-100: error code must be DEK_REKEY_PHASE7_AUDIT_FAILED")

		// INV-CRYPTO-100: DB state must be irreversibly committed.
		// Phase 6 destroyed the old DEK and Phase 2 minted version 2 as the active DEK.
		h.AssertCryptoKeysActiveVersion(h.SceneContext, 2)

		// Retrieve the checkpoint to find the OldDEKID and RequestID.
		ckpt, found, err := h.Primary.GetCheckpointRepo().FindNonTerminalByContext(
			context.Background(),
			h.SceneContext.Type,
			h.SceneContext.ID,
		)
		// The checkpoint is at phase7_audit which is non-terminal.
		Expect(err).NotTo(HaveOccurred(), "FindNonTerminalByContext must not error")
		Expect(found).To(BeTrue(), "checkpoint must exist at phase7_audit after emit failure")

		h.AssertCryptoKeysDestroyedAtSet(ckpt.OldDEKID)

		// INV-CRYPTO-100: fallback log must be written at
		// <data_dir>/audit-fallback/rekey-<request_id>.log.
		logPath := filepath.Join(dataDir, "audit-fallback",
			"rekey-"+ckpt.RequestID.String()+".log")
		Expect(logPath).To(BeAnExistingFile(),
			"INV-CRYPTO-100: fallback log must be written when Phase 7 audit emit fails")

		// Checkpoint must be at phase7_audit (not complete) — retry is possible.
		h.AssertCheckpointStatus(ckpt.RequestID, dek.CheckpointStatusPhase7Audit)
	})
})

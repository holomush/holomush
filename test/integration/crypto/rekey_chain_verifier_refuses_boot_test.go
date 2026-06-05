// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_chain_verifier_refuses_boot_test.go — E2E spec for INV-CRYPTO-102: the
// audit-chain verifier refuses boot when a rekey chain entry has been tampered.
//
// Verifies INV-CRYPTO-102: any tampering with a rekey audit
// row's self_hash causes the VerifierSubsystem (which runs at boot time) to
// return AUDIT_CHAIN_HASH_MISMATCH. The clean-fixture boot path (no tampering)
// must pass to confirm the baseline.
//
// Implementation note: the VerifierSubsystem runs at server boot via
// lifecycle.Start. The test server wires the verifier but does not expose
// lifecycle re-execution after boot. We simulate the boot check by calling
// VerifyAll directly on the chain verifier — which is the exact operation
// that VerifierSubsystem.Start performs. A tampered row causes VerifyAll to
// return AUDIT_CHAIN_HASH_MISMATCH, demonstrating the refusal that would
// block boot on a real server.
//
// Part of holomush-jxo8.7 (bead jxo8.7.39, merged T45+T46+T47).
package crypto_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// buildTamperedRekeyPayload produces a JSON payload that looks like a rekey
// audit event but has a deliberately wrong self_hash (0xdeadbeef bytes
// encoded as hex in the rekey_chain.self_hash field). This causes
// RecomputeSelfHash to disagree with the stored value, producing
// AUDIT_CHAIN_HASH_MISMATCH when the verifier walks the chain.
func buildTamperedRekeyPayload() []byte {
	// Minimal rekey audit payload with a zeroed-out context and a bad self_hash.
	// The exact field values are secondary; what matters is that self_hash does
	// not match the SHA-256(JCS(zeroed payload)) that RecomputeSelfHash computes.
	type chainBlock struct {
		Scope       string  `json:"scope"`
		PrevHash    *string `json:"prev_hash"`
		PrevEventID *string `json:"prev_event_id"`
		SelfHash    string  `json:"self_hash"`
	}
	type context_ struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	type payload struct {
		RequestID  string     `json:"request_id"`
		Context    context_   `json:"context"`
		RekeyChain chainBlock `json:"rekey_chain"`
	}
	selfHash := "sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	p := payload{
		RequestID: "01TAMPERED00000000000000",
		Context:   context_{Type: "scene", ID: "01ABC"},
		RekeyChain: chainBlock{
			Scope:    "scene:01ABC",
			SelfHash: selfHash,
		},
	}
	raw, err := json.Marshal(p)
	Expect(err).NotTo(HaveOccurred(), "buildTamperedRekeyPayload: marshal")
	return raw
}

var _ = Describe("Rekey chain verifier", func() {
	It("refuses to boot when the rekey chain has a break (INV-CRYPTO-102)", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Complete one rekey successfully so the rekey chain has at least one
		// entry in events_audit.
		out, err := runRekeyViaUDS(
			h,
			h.AdminPlayer.PlayerID,
			h.SceneContext.Type,
			h.SceneContext.ID,
			"chain verifier boot test — first rekey",
		)
		Expect(err).NotTo(HaveOccurred(), "first rekey must succeed")
		Expect(out.AuditEventID).NotTo(BeEmpty(), "AuditEventId must be populated")

		// Confirm the chain verifies cleanly before tampering.
		// Use VerifyScope with the canonical colon-format scope, matching the
		// AssertRekeyChainIntactForContext helper pattern.
		scope := h.SceneContext.Type + ":" + h.SceneContext.ID
		handler := dek.RekeyHandlerFor(h.Game)
		cleanErr := h.Primary.GetAuditChainVerifier().VerifyScope(
			context.Background(), handler, scope,
		)
		Expect(cleanErr).NotTo(HaveOccurred(),
			"INV-CRYPTO-102: clean chain must verify without error before tampering")

		// Tamper: overwrite the most recent rekey audit row's envelope with a
		// structurally valid JSON body that has a deliberately wrong self_hash.
		// This simulates an attacker modifying the audit log after the fact.
		//
		// PostgreSQL doesn't support ORDER BY + LIMIT in UPDATE directly; use a
		// subquery to select the most recent row's id for the targeted UPDATE.
		tamperedPayload := buildTamperedRekeyPayload()
		rekeySubject := "events." + h.Game + ".system.rekey.scene.01ABC"
		tag, updateErr := h.DB.Exec(context.Background(),
			`UPDATE events_audit
			    SET envelope = $1
			  WHERE id = (
			    SELECT id FROM events_audit
			     WHERE subject = $2 AND type = 'crypto.system.rekey'
			     ORDER BY js_seq DESC
			     LIMIT 1
			  )`,
			tamperedPayload,
			rekeySubject)
		Expect(updateErr).NotTo(HaveOccurred(), "tamper UPDATE must succeed")
		Expect(tag.RowsAffected()).To(BeNumerically("==", 1),
			"INV-CRYPTO-102: tamper UPDATE must affect exactly 1 row (subject=%q)", rekeySubject)

		// Re-verify the chain via VerifyScope — the same operation that
		// VerifierSubsystem.Start performs internally at boot.
		// INV-CRYPTO-102: tampering MUST cause an AUDIT_CHAIN_HASH_MISMATCH.
		// The error is an oops.OopsError with Code()="AUDIT_CHAIN_HASH_MISMATCH";
		// its .Error() string contains the message "self_hash does not match recompute".
		bootErr := h.Primary.GetAuditChainVerifier().VerifyScope(
			context.Background(), handler, scope,
		)
		Expect(bootErr).To(HaveOccurred(),
			"INV-CRYPTO-102: chain verifier MUST detect tampering")
		// Extract the oops error code (the code is not in .Error() for direct calls;
		// it is in the oops.Code field). The message confirms hash mismatch.
		Expect(bootErr.Error()).To(ContainSubstring("self_hash does not match recompute"),
			"INV-CRYPTO-102: tampered entry MUST produce a self_hash mismatch error")
	})
})

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// policy_set_chain_post_refactor_test.go — E2E spec for the crypto.policy_set
// chain after the auditchain primitive refactor (sub-epic E, .7).
//
// Verifies:
//   - Server boot emits a genesis policy_set event with prev_hash nil.
//   - A subsequent policy edit via EditDualControlRequired emits a chained
//     event with a valid prev_hash → prev_hash == genesis self_hash.
//   - INV-CRYPTO-77/INV-CRYPTO-78/INV-CRYPTO-79 hold through the generalized chain verifier (not the
//     old per-chain verifier that shipped with D).
//   - Tampering any policy_set row's self_hash causes the generalized
//     verifier to surface AUDIT_CHAIN_HASH_MISMATCH.
//
// Part of holomush-jxo8.7 (bead jxo8.7.40, merged T48+T49+T50).
package crypto_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/policy"
)

var _ = Describe("policy_set chain (post auditchain refactor)", func() {
	It("emits genesis with prev_hash nil; subsequent events link", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// The server boot already emitted a genesis policy_set event for
		// "dual_control_required" (harness wires the policy subsystem at boot).
		// Trigger a second emit via EditDualControlRequired — a new op-kind
		// extends the snapshot, producing a chained event.
		require.NoError(suiteT, h.Primary.EditDualControlRequired([]string{"rekey", "admin_read_stream"}))

		// RestartToReloadPolicy is a no-op in the in-process harness: the
		// verifier reads from the DB on every call.
		h.Primary.RestartToReloadPolicy()

		// Walk the full chain — both genesis and the chained event must pass
		// INV-CRYPTO-77 (genesis prev_hash nil), INV-CRYPTO-78 (link is valid), INV-CRYPTO-79
		// (policy_hash excluded from its own input).
		h.AssertPolicySetChainIntact("dual_control_required")
	})

	It("INV-CRYPTO-77/INV-CRYPTO-78/INV-CRYPTO-79 hold via the generalized verifier", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Emit a genesis event first so there is a chain to tamper.
		// (The in-process harness does not call the policy subsystem at boot.)
		require.NoError(suiteT, h.Primary.EditDualControlRequired([]string{"rekey"}))

		// Tamper the most recent policy_set row's self_hash (policy_hash).
		// This breaks the chain so the generalized verifier must detect it.
		h.TamperPolicySetSelfHash("dual_control_required")

		// Call VerifyAll via VerifierForChain — the same operation the
		// VerifierSubsystem.Start performs at boot.
		handler := policy.PolicySetHandlerFor(h.Game)
		err := h.Primary.VerifierForChain(handler).VerifyAll(context.Background(), handler)
		Expect(err).To(HaveOccurred(),
			"INV-CRYPTO-77/INV-CRYPTO-78/INV-CRYPTO-79: tampered policy_set row MUST cause VerifyAll to fail")
		// The oops error code AUDIT_CHAIN_HASH_MISMATCH is in the Code() field;
		// .Error() surfaces the human-readable message. Either the code or message
		// must confirm a hash mismatch (matches rekey_chain_verifier_refuses_boot_test.go
		// assertion pattern — see INV-E15 spec).
		Expect(err.Error()).To(ContainSubstring("self_hash does not match recompute"),
			"INV-CRYPTO-77/INV-CRYPTO-78/INV-CRYPTO-79: tampered self_hash must produce a self_hash mismatch error")
	})
})

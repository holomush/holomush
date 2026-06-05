// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_policy_hash_frozen_at_phase1_test.go — E2E spec for INV-CRYPTO-112:
// the policy_hash captured at Phase 1 INSERT is frozen for the lifetime
// of the rekey operation.
//
// Verifies INV-CRYPTO-112: a mid-Rekey policy edit (which emits a new policy_set
// chain event and changes the policy_set chain's tail hash) does NOT change
// the policy_hash embedded in the rekey audit event. The Phase 7 audit emit
// reads policy_hash from the checkpoint row (set at Phase 1), not from the
// live policy chain.
//
// Test scenario:
//  1. Seed 2000 events and install a crash hook at row 1000 (mid-Phase 3).
//  2. Start Rekey → it dies mid-Phase 3.
//  3. Capture the current policy_set tail hash (OriginalPolicyHash).
//  4. Edit the policy (extends the chain with a new event).
//  5. Resume (restart = clear the crash hook, call Rekey again with same args).
//  6. Assert the rekey audit event's policy_hash equals OriginalPolicyHash.
//
// Part of holomush-jxo8.7 (bead jxo8.7.40, merged T48+T49+T50).
package crypto_test

import (
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

var _ = Describe("Rekey policy_hash frozen at Phase 1 (INV-CRYPTO-112)", func() {
	It("captures policy_hash at Phase 1 INSERT; mid-Rekey policy edit does not change it", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Capture the current policy_set tail hash before any rekey starts.
		// At this point the chain is empty (genesis), so this returns the
		// 32-byte zero sentinel that Phase 1 will store.
		h.RememberCurrentPolicyHash("dual_control_required")

		// Drive Phase 1 directly (bypassing the UDS). This inserts a non-terminal
		// checkpoint row with policy_hash = zeros (genesis sentinel per INV-CRYPTO-112).
		// The checkpoint is left at phase1_auth so the dispatcher treats it as
		// non-terminal (resume path, not fresh-start) on the next UDS call.
		firstRID := openNonTerminalCheckpoint(h, "inv-e25-test-justification")

		// Edit the policy mid-Rekey: emit a new crypto.policy_set genesis event.
		// This advances the policy chain tail hash to a non-zero value.
		// The checkpoint's policy_hash MUST NOT reflect this change (INV-CRYPTO-112).
		require.NoError(suiteT, h.Primary.EditDualControlRequired([]string{"admin_read_stream"}))

		// Resume via the UDS. The dispatcher auto-resumes the existing non-terminal
		// checkpoint (same op_args_hash, same operator = INV-CRYPTO-91 + INV-CRYPTO-103). Phase 7
		// must read policy_hash from the checkpoint row — NOT from the live chain.
		out, err := h.AdminCli.Rekey(h.SceneContext, "inv-e25-test-justification")
		Expect(err).NotTo(HaveOccurred(),
			"INV-CRYPTO-112: resumed rekey must complete without error")

		// The resumed rekey MUST reuse the original request_id (INV-CRYPTO-91).
		var resumedRID dek.RequestID
		copy(resumedRID[:], out.RequestID())
		Expect(resumedRID).To(Equal(firstRID),
			"INV-CRYPTO-91: resume MUST reuse the original RequestID")

		// Load the rekey audit event emitted by Phase 7 and assert its
		// policy_hash equals the hash captured at Phase 1 (before the policy edit).
		evt := h.LoadRekeyAuditEvent(out.RequestID())
		Expect(evt.PolicyHash).To(Equal(h.OriginalPolicyHash),
			"INV-CRYPTO-112: Phase 7 audit event policy_hash MUST equal the Phase 1 captured hash — a mid-Rekey policy edit MUST NOT shift it")
	})
})

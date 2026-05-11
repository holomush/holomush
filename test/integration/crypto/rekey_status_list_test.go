// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_status_list_test.go — E2E spec for the RekeyStatus + RekeyList CLI
// surface, exercised end-to-end from the CLI helpers through the admin UDS to
// the handler layer.
//
// Verifies:
//   - status returns all checkpoint fields including primary_player_id and
//     status string.
//   - list with no flags returns only non-terminal checkpoints.
//   - list with --include-terminal returns both terminal and non-terminal rows.
//   - list with a context_pattern filter returns only matching rows.
//
// Part of holomush-jxo8.7 (bead jxo8.7.40, merged T48+T49+T50).
package crypto_test

import (
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

var _ = Describe("Rekey status + list", func() {
	It("status returns full checkpoint fields", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Run a full rekey so a complete checkpoint exists for SceneContext.
		_, err := h.AdminCli.Rekey(h.SceneContext, "status-test justification")
		Expect(err).NotTo(HaveOccurred(), "Rekey must succeed before testing status")

		// Find the completed checkpoint for the scene context.
		ckpt := h.findCheckpoint(h.SceneContext.Type, h.SceneContext.ID)

		// Retrieve status via the RPC surface (same path as the CLI `status` subcommand).
		status, err := h.AdminCli.RekeyStatus(ckpt.RequestID[:])
		Expect(err).NotTo(HaveOccurred(), "RekeyStatus must succeed")
		Expect(status.GetStatus()).To(Equal("complete"),
			"status field must be 'complete' after successful rekey")
		Expect(status.GetPrimaryPlayerId()).NotTo(BeEmpty(),
			"primary_player_id must be populated in the status response")
		Expect(status.GetContextType()).To(Equal(h.SceneContext.Type),
			"context_type must round-trip correctly")
		Expect(status.GetContextId()).To(Equal(h.SceneContext.ID),
			"context_id must round-trip correctly")
	})

	It("list filters by include-terminal and context-pattern", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Seed one completed (terminal) checkpoint for 01ABC and one
		// non-terminal (pending) checkpoint for 01DEF.
		h.SeedCompletedCheckpoint("scene", "01ABC")
		h.SeedActiveCheckpoint("scene", "01DEF")

		// Default: non-terminal only — should return exactly one row (01DEF).
		rows, err := h.AdminCli.RekeyList(false, "")
		Expect(err).NotTo(HaveOccurred(), "RekeyList (non-terminal only) must succeed")
		Expect(rows).To(HaveLen(1),
			"non-terminal filter must return exactly 1 row; got %d", len(rows))
		Expect(rows[0].GetContextId()).To(Equal("01DEF"),
			"non-terminal row must be the seeded active checkpoint")

		// With --include-terminal: both rows should appear.
		rows, err = h.AdminCli.RekeyList(true, "")
		Expect(err).NotTo(HaveOccurred(), "RekeyList (include-terminal) must succeed")
		Expect(rows).To(HaveLen(2),
			"include-terminal must return 2 rows; got %d", len(rows))

		// Filtered by context_id pattern: only the 01DEF row should match.
		rows, err = h.AdminCli.RekeyList(true, "01DEF")
		Expect(err).NotTo(HaveOccurred(), "RekeyList (filtered) must succeed")
		Expect(rows).To(HaveLen(1),
			"context_pattern filter for '01DEF' must return 1 row; got %d", len(rows))
		Expect(rows[0].GetContextId()).To(Equal("01DEF"),
			"filtered row must be the 01DEF checkpoint")
	})
})

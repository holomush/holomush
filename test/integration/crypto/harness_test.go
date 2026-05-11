// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Self-test for RekeyTestHarness. Verifies that SetupRekeyHarness boots
// cleanly and seeds the default 1000-event fixture.
//
// TDD acceptance criterion for holomush-jxo8.7.35:
//
//	TestRekeyHarness runs via: task test:int -- -run TestRekeyHarness ./test/integration/crypto/
package crypto_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

var _ = Describe("RekeyTestHarness", func() {
	It("boots successfully with default fixture", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		// Both replicas must be wired.
		Expect(h.Primary).NotTo(BeNil())
		Expect(h.Secondary).NotTo(BeNil())

		// AdminCli must be connected to the primary UDS.
		Expect(h.AdminCli).NotTo(BeNil())
		Expect(h.Primary.UDSPath()).NotTo(BeEmpty())

		// Default fixture: 1000 events on events.g1.scene.01ABC.ic.
		var count int
		err := h.DB.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM events_audit WHERE subject = $1`,
			"events.g1.scene.01ABC.ic").Scan(&count)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(1000))
	})

	It("configures EventCount via WithEventCount option", func() {
		h := SetupRekeyHarness(suiteT, WithEventCount(50))
		defer h.Cleanup()

		var count int
		err := h.DB.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM events_audit WHERE subject = $1`,
			"events.g1.scene.01ABC.ic").Scan(&count)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(50))
	})

	It("exposes GetRekeyOrchestrator + GetCheckpointRepo", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		Expect(h.Primary.GetRekeyOrchestrator()).NotTo(BeNil())
		Expect(h.Primary.GetCheckpointRepo()).NotTo(BeNil())
	})

	It("exposes GetAuditChainVerifier", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		Expect(h.Primary.GetAuditChainVerifier()).NotTo(BeNil())
	})

	It("seeds two operator players with distinct IDs", func() {
		h := SetupRekeyHarness(suiteT)
		defer h.Cleanup()

		Expect(h.AdminPlayer.PlayerID).NotTo(BeEmpty())
		Expect(h.PartnerPlayer.PlayerID).NotTo(BeEmpty())
		Expect(h.AdminPlayer.PlayerID).NotTo(Equal(h.PartnerPlayer.PlayerID))
	})

	It("cleans up without error", func() {
		h := SetupRekeyHarness(suiteT)
		// Cleanup is safe to call explicitly even after t.Cleanup fires.
		Expect(func() { h.Cleanup() }).NotTo(Panic())
	})
})

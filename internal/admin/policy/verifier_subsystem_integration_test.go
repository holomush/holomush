// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package policy_test

import (
	"context"
	"time"

	"github.com/samber/oops"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/admin/policy"
)

// CryptoChainVerifierSubsystem integration specs covering Start's
// fail-closed posture on chain integrity violations and the happy path.
// Migrated from testify to Ginkgo/Gomega per project standards
// (CodeRabbit #10).
var _ = Describe("CryptoChainVerifierSubsystem (integration)", func() {
	Context("when seeded with a chain that has a corrupt prev_hash linkage", func() {
		It("MUST fail-close at Start with a POLICY_CHAIN_* code", func() {
			gameID := "verifierbroken"
			subject := "events." + gameID + ".system.crypto_policy.dual_control_required"
			DeferCleanup(func() {
				_, _ = testPool.Exec(context.Background(),
					`DELETE FROM events_audit WHERE subject = $1`, subject)
			})
			chainStateCleanupGinkgo("dual_control_required")

			// Seed a valid genesis row.
			gen := policy.PolicySetPayload{
				PolicyName:      "dual_control_required",
				PolicySnapshot:  map[string]any{"required_op_kinds": []any{"rekey"}},
				PrevHash:        nil,
				ServerStartULID: "01HZSEED0000000000000000",
				ServerIdentity:  "holomush@seed",
				Timestamp:       time.Unix(1700000000, 0).UTC(),
			}
			genHash, err := policy.ComputePolicyHash(&gen)
			Expect(err).NotTo(HaveOccurred())
			gen.PolicyHash = genHash
			insertChainRowGinkgo(subject, 1, gen)

			// Seed a broken-link extension: prev_hash is wrong.
			ext := policy.PolicySetPayload{
				PolicyName:      "dual_control_required",
				PolicySnapshot:  map[string]any{"required_op_kinds": []any{"rekey", "admin_read_stream"}},
				PrevHash:        []byte{0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef},
				ServerStartULID: "01HZSEED0000000000000000",
				ServerIdentity:  "holomush@seed",
				Timestamp:       time.Unix(1700000060, 0).UTC(),
			}
			extHash, err := policy.ComputePolicyHash(&ext)
			Expect(err).NotTo(HaveOccurred())
			ext.PolicyHash = extHash
			insertChainRowGinkgo(subject, 2, ext)

			s := policy.NewCryptoChainVerifierSubsystem(policy.CryptoChainVerifierConfig{
				Pool:        testPool,
				GameID:      gameID,
				PolicyNames: []string{"dual_control_required"},
			})
			err = s.Start(context.Background())
			Expect(err).To(HaveOccurred())
			o, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			// oops.AsOops returns the deepest oops code; the verifier's code is
			// POLICY_CHAIN_BROKEN_LINK or POLICY_CHAIN_HASH_MISMATCH depending on
			// which fired first. Either is a valid fail-closed signal.
			Expect([]string{"POLICY_CHAIN_BROKEN_LINK", "POLICY_CHAIN_HASH_MISMATCH"}).
				To(ContainElement(o.Code()),
					"verifier should fail-closed; got %s", o.Code())
		})
	})

	Context("when seeded with a single valid genesis row", func() {
		It("Start returns nil (happy path)", func() {
			gameID := "verifierok"
			subject := "events." + gameID + ".system.crypto_policy.dual_control_required"
			DeferCleanup(func() {
				_, _ = testPool.Exec(context.Background(),
					`DELETE FROM events_audit WHERE subject = $1`, subject)
			})
			chainStateCleanupGinkgo("dual_control_required")

			gen := policy.PolicySetPayload{
				PolicyName:      "dual_control_required",
				PolicySnapshot:  map[string]any{"required_op_kinds": []any{"rekey"}},
				PrevHash:        nil,
				ServerStartULID: "01HZSEED0000000000000000",
				ServerIdentity:  "holomush@seed",
				Timestamp:       time.Unix(1700000000, 0).UTC(),
			}
			genHash, err := policy.ComputePolicyHash(&gen)
			Expect(err).NotTo(HaveOccurred())
			gen.PolicyHash = genHash
			insertChainRowGinkgo(subject, 1, gen)

			s := policy.NewCryptoChainVerifierSubsystem(policy.CryptoChainVerifierConfig{
				Pool:        testPool,
				GameID:      gameID,
				PolicyNames: []string{"dual_control_required"},
			})
			Expect(s.Start(context.Background())).To(Succeed())
		})
	})
})

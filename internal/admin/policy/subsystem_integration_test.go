// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package policy_test

import (
	"context"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/admin/policy"
)

// CryptoPolicySubsystem integration specs (INV-CRYPTO-84 fail-closed posture +
// happy-path Start emits one event per configured policy_name). Migrated
// from testify to Ginkgo/Gomega per project standards (CodeRabbit #8).
var _ = Describe("CryptoPolicySubsystem (integration)", func() {
	Context("when the publisher fails", func() {
		It("MUST short-circuit Start with the wrapped publish-failure code", func() {
			pub := &fakePublisher{err: errors.New("simulated publish failure")}
			s := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{
				EmitDeps: policy.EmitDeps{
					GameID:          "subsysfail",
					ServerStartULID: ulid.Make().String(),
					ServerIdentity:  "holomush@test",
					Pool:            testPool,
					Publisher:       pub,
					Clock:           fixedClock{t: time.Unix(1700000000, 0).UTC()},
					Config:          policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey"}},
				},
				PolicyNames: []string{"dual_control_required"},
			})
			err := s.Start(context.Background())
			Expect(err).To(HaveOccurred())
			o, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			// oops.Code() returns the deepest code in the chain. The outer wrap is
			// CRYPTO_POLICY_EMIT_FAILED; the inner cause is POLICY_EMIT_PUBLISH_FAILED.
			// Both confirm fail-closed per INV-CRYPTO-84 — assert the deepest (publish failure).
			Expect(o.Code()).To(Equal("POLICY_EMIT_PUBLISH_FAILED"))
		})
	})

	Context("when configured with one policy_name and a healthy publisher", func() {
		It("emits exactly one event per configured policy_name", func() {
			pub := &fakePublisher{}
			gameID := "subsysok"
			subject := "events." + gameID + ".system.crypto_policy.dual_control_required"
			cleanupSubjectGinkgo(subject)
			chainStateCleanupGinkgo(gameID, "dual_control_required")

			s := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{
				EmitDeps: policy.EmitDeps{
					GameID:          gameID,
					ServerStartULID: ulid.Make().String(),
					ServerIdentity:  "holomush@test",
					Pool:            testPool,
					Publisher:       pub,
					Clock:           fixedClock{t: time.Unix(1700000000, 0).UTC()},
					Config:          policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey"}},
				},
				PolicyNames: []string{"dual_control_required"},
			})
			Expect(s.Start(context.Background())).To(Succeed())
			Expect(pub.Events()).To(HaveLen(1), "should emit exactly one event for the single policy_name")
		})
	})

	Context("when configured with an unsupported policy_name", func() {
		It("MUST fail-closed at Start with POLICY_EMIT_UNKNOWN_POLICY", func() {
			pub := &fakePublisher{}
			gameID := "subsysunknown"
			s := policy.NewCryptoPolicySubsystem(policy.CryptoPolicySubsystemConfig{
				EmitDeps: policy.EmitDeps{
					GameID:          gameID,
					ServerStartULID: ulid.Make().String(),
					ServerIdentity:  "holomush@test",
					Pool:            testPool,
					Publisher:       pub,
					Clock:           fixedClock{t: time.Unix(1700000000, 0).UTC()},
					Config:          policy.CryptoEffectiveConfig{DualControlRequired: []string{"rekey"}},
				},
				PolicyNames: []string{"definitely_not_a_real_policy"},
			})
			err := s.Start(context.Background())
			Expect(err).To(HaveOccurred())
			o, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			Expect(o.Code()).To(Equal("POLICY_EMIT_UNKNOWN_POLICY"))
			Expect(pub.Events()).To(BeEmpty(),
				"unknown policy_name MUST NOT publish any events")
		})
	})
})

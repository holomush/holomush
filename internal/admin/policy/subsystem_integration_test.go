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
// happy-path Activate emits one event per configured policy_name). Migrated
// from testify to Ginkgo/Gomega per project standards (CodeRabbit #8).
var _ = Describe("CryptoPolicySubsystem (integration)", func() {
	Context("when the publisher fails", func() {
		It("MUST short-circuit Activate with the wrapped publish-failure code", func() {
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
			Expect(s.Prepare(context.Background())).To(Succeed())
			err := s.Activate(context.Background())
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
			Expect(s.Prepare(context.Background())).To(Succeed())
			Expect(s.Activate(context.Background())).To(Succeed())
			Expect(pub.Events()).To(HaveLen(1), "should emit exactly one event for the single policy_name")
		})
	})

	Context("when configured with an unsupported policy_name", func() {
		It("MUST fail-closed at Activate with POLICY_EMIT_UNKNOWN_POLICY", func() {
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
			Expect(s.Prepare(context.Background())).To(Succeed())
			err := s.Activate(context.Background())
			Expect(err).To(HaveOccurred())
			o, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			Expect(o.Code()).To(Equal("POLICY_EMIT_UNKNOWN_POLICY"))
			Expect(pub.Events()).To(BeEmpty(),
				"unknown policy_name MUST NOT publish any events")
		})
	})

	Context("when Activate is called a second time with all policies already emitted", func() {
		It("MUST NOT re-emit — the per-policy-name idempotency guard (D-13.2 row 15)", func() {
			pub := &fakePublisher{}
			gameID := "subsysrepeat"
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
			Expect(s.Prepare(context.Background())).To(Succeed())
			Expect(s.Activate(context.Background())).To(Succeed())
			Expect(pub.Events()).To(HaveLen(1))

			Expect(s.Activate(context.Background())).To(Succeed())
			Expect(pub.Events()).To(HaveLen(1), "second Activate must not re-publish an already-emitted policy snapshot")
		})
	})

	Context("when a mid-loop failure is followed by a retry", func() {
		It("MUST re-emit ONLY the not-yet-emitted names — never the successful prefix, never suppressing the failed suffix (round 6 WARNING)", func() {
			pub := &fakePublisher{}
			gameID := "subsysretry"
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
				// The first name is supported and succeeds; the second is
				// unsupported and fails deterministically every time
				// (POLICY_EMIT_UNKNOWN_POLICY) — the mid-loop failure.
				PolicyNames: []string{"dual_control_required", "definitely_not_a_real_policy"},
			})
			Expect(s.Prepare(context.Background())).To(Succeed())

			err := s.Activate(context.Background())
			Expect(err).To(HaveOccurred())
			o, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			Expect(o.Code()).To(Equal("POLICY_EMIT_UNKNOWN_POLICY"))
			Expect(pub.Events()).To(HaveLen(1), "the first (supported) policy name must have emitted before the failure")

			// Retry: the successful prefix must NOT re-emit; the failed
			// suffix is attempted again (and fails again, deterministically).
			err = s.Activate(context.Background())
			Expect(err).To(HaveOccurred())
			Expect(pub.Events()).To(HaveLen(1),
				"retry must not re-emit the already-successful dual_control_required policy")
		})
	})
})

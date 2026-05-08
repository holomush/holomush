// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package totp_test

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/pquerna/otp/hotp"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/test/testutil"
)

// envKEK is the test KEK env var; the test sets it to a random KEK so each
// run is hermetic.
const envKEK = "HOLOMUSH_TOTP_E2E_KEK"

var _ = Describe("TOTP substrate E2E (PG + KEK; no eventbus)", func() {
	var (
		ctx   context.Context
		pool  *pgxpool.Pool
		svc   totp.Service
		alice string
		bob   string
	)

	BeforeEach(func() {
		ctx = context.Background()

		shared := testutil.SharedPostgres(suiteT)
		connStr := testutil.FreshDatabase(suiteT, shared)

		var err error
		pool, err = pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { pool.Close() })

		// Real KEK source — env-source with prodMode=false (test convention).
		suiteT.Setenv(envKEK, testutil.RandomKEKHex(suiteT))
		kekSource := kek.NewEnvSource(envKEK, false)
		kekProvider, err := kek.NewLocalAEADProvider(ctx, kekSource, pool)
		Expect(err).NotTo(HaveOccurred())

		repo := totp.NewRepository(pool)
		hasher := auth.NewArgon2idHasher()
		svc, err = totp.NewService(
			totp.Config{GameID: "default"}, repo, kekProvider, totp.NewRealClock(), hasher,
		)
		Expect(err).NotTo(HaveOccurred())

		alice = ulid.Make().String()
		bob = ulid.Make().String()
		insertPlayer(ctx, pool, alice, "alice-"+alice[:8])
		insertPlayer(ctx, pool, bob, "bob-"+bob[:8])
	})

	AfterEach(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM player_totp_recovery_codes`)
		_, _ = pool.Exec(ctx, `DELETE FROM player_totp`)
		_, _ = pool.Exec(ctx, `DELETE FROM crypto_bootstrap_state`)
		_, _ = pool.Exec(ctx, `DELETE FROM players WHERE id IN ($1, $2)`, alice, bob)
	})

	It("supports the full bootstrap → verify → reuse → recover → re-enroll cycle", func() {
		alicePID := ulid.MustParse(alice)
		bobPID := ulid.MustParse(bob)

		By("bootstrap-enrolling alice")
		bRes, err := svc.BootstrapEnroll(ctx, alicePID)
		Expect(err).NotTo(HaveOccurred())
		Expect(bRes.Enrollment.Secret).NotTo(BeEmpty())
		Expect(bRes.Enrollment.RecoveryCodes).To(HaveLen(10))
		Expect(bRes.BootstrapKey).To(Equal("totp_v1"))

		By("refusing a second bootstrap-enroll")
		_, err = svc.BootstrapEnroll(ctx, bobPID)
		Expect(err).To(MatchError(totp.ErrBootstrapAlreadyConsumed))

		By("verifying alice's TOTP at the current step")
		now := time.Now().UTC()
		code, err := hotp.GenerateCode(bRes.Enrollment.Secret, uint64(now.Unix()/30)) //nolint:gosec // G115 unix timestamp positive
		Expect(err).NotTo(HaveOccurred())
		vRes, err := svc.Verify(ctx, alicePID, code)
		Expect(err).NotTo(HaveOccurred())
		Expect(vRes.Outcome).To(Equal(totp.OutcomeOK))

		By("rejecting code reuse")
		vRes2, err := svc.Verify(ctx, alicePID, code)
		Expect(err).NotTo(HaveOccurred())
		Expect(vRes2.Outcome).To(Equal(totp.OutcomeCodeReuse))

		By("recovering alice with a recovery code")
		consRes, err := svc.ConsumeRecoveryCode(ctx, alicePID, bRes.Enrollment.RecoveryCodes[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(consRes.RecoveryCodeID).NotTo(Equal(ulid.ULID{}))

		clrRes, err := svc.ClearTOTP(ctx, alicePID, totp.ClearReasonRecoveryCode)
		Expect(err).NotTo(HaveOccurred())
		Expect(clrRes.WasEnrolled).To(BeTrue())

		ok, err := svc.IsEnrolled(ctx, alicePID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())

		By("re-enrolling alice via Enroll")
		eRes, err := svc.Enroll(ctx, alicePID)
		Expect(err).NotTo(HaveOccurred())
		Expect(eRes.Enrollment.RecoveryCodes).To(HaveLen(10))
		Expect(eRes.Enrollment.RecoveryCodes).NotTo(ContainElement(bRes.Enrollment.RecoveryCodes[0]))

		// NOTE: no events_audit assertions — sub-epic A emits nothing per
		// R5 Option Y. Sub-epic D's E2E covers the audit table once the
		// OperatorAuthProvider ships.
	})
})

// insertPlayer inserts a minimal player row for use as a foreign-key anchor.
// Mirrors the helper in internal/totp/repo_integration_test.go but takes
// ctx (matching the BeforeEach pattern).
func insertPlayer(ctx context.Context, pool *pgxpool.Pool, id, username string) {
	_, err := pool.Exec(ctx,
		`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, $3)`,
		id, username, "hash-placeholder",
	)
	Expect(err).NotTo(HaveOccurred())
}

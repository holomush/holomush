// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package totp_test

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

// newTOTPPool opens a pgxpool against a fresh migrated test database.
// Each spec gets its own isolated database.
func newTOTPPool() *pgxpool.Pool {
	GinkgoHelper()
	connStr := testutil.FreshDatabase(suiteT, sharedPG)
	pool, err := pgxpool.New(context.Background(), connStr)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(pool.Close)
	return pool
}

// insertTOTPPlayer inserts a minimal player row for use as a foreign-key anchor.
func insertTOTPPlayer(pool *pgxpool.Pool, id, username string) {
	GinkgoHelper()
	ctx := context.Background()
	_, err := pool.Exec(
		ctx,
		`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, $3)`,
		id, username, "hash-placeholder",
	)
	Expect(err).NotTo(HaveOccurred(), "insertTOTPPlayer")
	DeferCleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM players WHERE id = $1`, id)
	})
}

// fakeHasher is a minimal RecoveryCodeHasher for integration tests that
// stores raw codes as hashes (identity). Suitable only for controlled tests
// where we control the stored hash value.
type fakeHasher struct{}

func (fakeHasher) Verify(rawCode, encodedHash string) (bool, error) {
	return rawCode == encodedHash, nil
}

var _ = Describe("TOTPRepository", func() {
	Describe("BootstrapClaim concurrent atomicity (INV-A2)", func() {
		It("exactly one goroutine wins when N goroutines race to claim the same bootstrap key", func() {
			pool := newTOTPPool()
			repo := totp.NewRepository(pool)
			ctx := context.Background()

			const N = 8
			players := make([]string, N)
			for i := range players {
				players[i] = ulid.Make().String()
				insertTOTPPlayer(pool, players[i], "u"+players[i])
			}

			now := time.Now().UTC()
			key := "totp_v1_concurrent_" + ulid.Make().String() // unique per test run
			var (
				wg        sync.WaitGroup
				successes int
				errs      []error
				mu        sync.Mutex
			)
			wg.Add(N)
			for i := 0; i < N; i++ {
				pid := players[i]
				go func() {
					defer wg.Done()
					ok, err := repo.BootstrapClaim(ctx, key, pid, now)
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						errs = append(errs, err)
						return
					}
					if ok {
						successes++
					}
				}()
			}
			wg.Wait()
			Expect(errs).To(BeEmpty(), "BootstrapClaim goroutines returned errors")
			Expect(successes).To(Equal(1), "exactly one BootstrapClaim must win (INV-A2)")
		})
	})

	Describe("BootstrapEnrollAtomic rollback", func() {
		It("rolls back BootstrapClaim when InsertEnrollment fails with duplicate player_id", func() {
			pool := newTOTPPool()
			repo := totp.NewRepository(pool)
			ctx := context.Background()

			pid := ulid.Make().String()
			insertTOTPPlayer(pool, pid, "uatm"+pid)

			now := time.Now().UTC()
			key := "totp_atomic_rollback_" + ulid.Make().String()

			rec := totp.EnrollmentRecord{
				PlayerID:      pid,
				WrappedSecret: []byte("secret"),
				WrapKeyID:     "wk1",
				EnrolledAt:    now,
				RecoveryCodes: []totp.HashedRecoveryCode{
					{ID: ulid.Make(), CodeHash: "code1", CreatedAt: now},
				},
			}

			// Pre-enroll so that BootstrapEnrollAtomic's InsertEnrollment
			// step fails on duplicate player_id (PK violation on
			// totp_enrollments.player_id). This forces the rollback path.
			Expect(repo.InsertEnrollment(ctx, rec)).NotTo(HaveOccurred(),
				"setup: pre-enrolling player_id should succeed")

			// BootstrapEnrollAtomic must fail during the InsertEnrollment
			// step (duplicate PK) and roll back the BootstrapClaim it just
			// inserted in the same transaction.
			err := repo.BootstrapEnrollAtomic(ctx, key, pid, rec)
			Expect(err).To(HaveOccurred(),
				"BootstrapEnrollAtomic must fail when InsertEnrollment hits duplicate player_id")

			// Rollback assertion: the bootstrap claim was NOT persisted,
			// so a different player can still successfully claim the same
			// key. If the rollback were broken, the claim row would
			// survive and this call would return TOTP_BOOTSTRAP_CONSUMED.
			otherPID := ulid.Make().String()
			insertTOTPPlayer(pool, otherPID, "uatm_other_"+otherPID)
			ok, claimErr := repo.BootstrapClaim(ctx, key, otherPID, now)
			Expect(claimErr).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue(),
				"BootstrapClaim should still win for a different player after the prior atomic call rolled back")
		})
	})

	Describe("PlayerExists", func() {
		It("returns true for an existing player", func() {
			pool := newTOTPPool()
			repo := totp.NewRepository(pool)
			ctx := context.Background()

			pid := ulid.Make().String()
			insertTOTPPlayer(pool, pid, "upex"+pid)

			exists, err := repo.PlayerExists(ctx, pid)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeTrue())
		})

		It("returns false for a missing player", func() {
			pool := newTOTPPool()
			repo := totp.NewRepository(pool)
			ctx := context.Background()

			exists, err := repo.PlayerExists(ctx, ulid.Make().String())
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
		})
	})

	Describe("PlayerIDFromUsername", func() {
		It("returns player ID for a known username", func() {
			pool := newTOTPPool()
			repo := totp.NewRepository(pool)
			ctx := context.Background()

			pid := ulid.Make().String()
			username := "uname_" + pid[:6]
			insertTOTPPlayer(pool, pid, username)

			got, err := repo.PlayerIDFromUsername(ctx, username)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(pid))
		})

		It("returns an error for an unknown username", func() {
			pool := newTOTPPool()
			repo := totp.NewRepository(pool)
			ctx := context.Background()

			_, err := repo.PlayerIDFromUsername(ctx, "no_such_user_ever")
			Expect(err).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, err, "TOTP_REPO_PLAYER_NOT_FOUND")
		})
	})

	Describe("InsertEnrollment round-trip", func() {
		It("enrollment written by InsertEnrollment is readable via IsEnrolled and LoadEnrollment", func() {
			pool := newTOTPPool()
			repo := totp.NewRepository(pool)
			ctx := context.Background()

			pid := ulid.Make().String()
			insertTOTPPlayer(pool, pid, "uins"+pid)

			now := time.Now().UTC().Truncate(time.Microsecond)
			rec := totp.EnrollmentRecord{
				PlayerID:      pid,
				WrappedSecret: []byte("mysecret"),
				WrapKeyID:     "wk-test",
				EnrolledAt:    now,
				RecoveryCodes: []totp.HashedRecoveryCode{
					{ID: ulid.Make(), CodeHash: "h1", CreatedAt: now},
					{ID: ulid.Make(), CodeHash: "h2", CreatedAt: now},
				},
			}
			err := repo.InsertEnrollment(ctx, rec)
			Expect(err).NotTo(HaveOccurred())

			enrolled, err := repo.IsEnrolled(ctx, pid)
			Expect(err).NotTo(HaveOccurred())
			Expect(enrolled).To(BeTrue())

			// LoadEnrollment requires a transaction (FOR UPDATE).
			var state totp.VerifyState
			txErr := repo.InTransaction(ctx, func(txCtx context.Context) error {
				var e error
				state, e = repo.LoadEnrollment(txCtx, pid)
				return e
			})
			Expect(txErr).NotTo(HaveOccurred())
			Expect(state.PlayerID).To(Equal(pid))
			Expect(state.WrappedSecret).To(Equal([]byte("mysecret")))
			Expect(state.WrapKeyID).To(Equal("wk-test"))
			Expect(state.FailedAttempts).To(Equal(0))
			Expect(state.LockedUntil).To(BeNil())
		})
	})

	Describe("LoadEnrollment ErrNotEnrolled", func() {
		It("returns TOTP_NOT_ENROLLED when no enrollment exists for the player", func() {
			pool := newTOTPPool()
			repo := totp.NewRepository(pool)
			ctx := context.Background()

			pid := ulid.Make().String()
			insertTOTPPlayer(pool, pid, "ulen"+pid)

			txErr := repo.InTransaction(ctx, func(txCtx context.Context) error {
				_, err := repo.LoadEnrollment(txCtx, pid)
				return err
			})
			errutil.AssertErrorCode(suiteT, txErr, "TOTP_NOT_ENROLLED")
		})
	})

	Describe("MarkVerified", func() {
		It("zeroes failed_attempts, clears locked_until, and sets last_used_step", func() {
			pool := newTOTPPool()
			repo := totp.NewRepository(pool)
			ctx := context.Background()

			pid := ulid.Make().String()
			insertTOTPPlayer(pool, pid, "umv"+pid)

			now := time.Now().UTC().Truncate(time.Microsecond)
			rec := totp.EnrollmentRecord{
				PlayerID:      pid,
				WrappedSecret: []byte("s"),
				WrapKeyID:     "k",
				EnrolledAt:    now,
			}
			Expect(repo.InsertEnrollment(ctx, rec)).NotTo(HaveOccurred())

			// Artificially set failed_attempts and locked_until.
			_, err := pool.Exec(
				ctx,
				`UPDATE player_totp SET failed_attempts = 5, locked_until = $1 WHERE player_id = $2`,
				now.Add(15*time.Minute), pid,
			)
			Expect(err).NotTo(HaveOccurred())

			step := int64(42)
			Expect(repo.MarkVerified(ctx, pid, step, now)).NotTo(HaveOccurred())

			var state totp.VerifyState
			txErr := repo.InTransaction(ctx, func(txCtx context.Context) error {
				var e error
				state, e = repo.LoadEnrollment(txCtx, pid)
				return e
			})
			Expect(txErr).NotTo(HaveOccurred())
			Expect(state.FailedAttempts).To(Equal(0))
			Expect(state.LockedUntil).To(BeNil())
			Expect(state.LastUsedStep).NotTo(BeNil())
			Expect(*state.LastUsedStep).To(Equal(step))
		})
	})

	Describe("ConsumeRecoveryCode single-use", func() {
		It("recovery code can only be consumed once; second attempt returns TOTP_INVALID_RECOVERY_CODE", func() {
			pool := newTOTPPool()
			repo := totp.NewRepository(pool)
			ctx := context.Background()

			pid := ulid.Make().String()
			insertTOTPPlayer(pool, pid, "ucrc"+pid)

			now := time.Now().UTC().Truncate(time.Microsecond)
			rawCode := "test-recovery-code"
			codeID := ulid.Make()
			rec := totp.EnrollmentRecord{
				PlayerID:      pid,
				WrappedSecret: []byte("s"),
				WrapKeyID:     "k",
				EnrolledAt:    now,
				RecoveryCodes: []totp.HashedRecoveryCode{
					// fakeHasher stores the raw value as the hash.
					{ID: codeID, CodeHash: rawCode, CreatedAt: now},
				},
			}
			Expect(repo.InsertEnrollment(ctx, rec)).NotTo(HaveOccurred())

			hasher := fakeHasher{}

			// First consume succeeds and returns the right ULID.
			gotID, err := repo.ConsumeRecoveryCode(ctx, pid, rawCode, hasher, now)
			Expect(err).NotTo(HaveOccurred())
			Expect(gotID).To(Equal(codeID))

			// Second consume on the same code must fail.
			_, err = repo.ConsumeRecoveryCode(ctx, pid, rawCode, hasher, now)
			errutil.AssertErrorCode(suiteT, err, "TOTP_INVALID_RECOVERY_CODE")
		})
	})

	Describe("ClearEnrollment", func() {
		It("returns wasEnrolled=true for enrolled player and false for non-enrolled player", func() {
			pool := newTOTPPool()
			repo := totp.NewRepository(pool)
			ctx := context.Background()

			// Enrolled player.
			pid := ulid.Make().String()
			insertTOTPPlayer(pool, pid, "uclea"+pid)
			now := time.Now().UTC().Truncate(time.Microsecond)
			Expect(repo.InsertEnrollment(ctx, totp.EnrollmentRecord{
				PlayerID:      pid,
				WrappedSecret: []byte("s"),
				WrapKeyID:     "k",
				EnrolledAt:    now,
			})).NotTo(HaveOccurred())

			wasEnrolled, err := repo.ClearEnrollment(ctx, pid)
			Expect(err).NotTo(HaveOccurred())
			Expect(wasEnrolled).To(BeTrue(), "ClearEnrollment should return wasEnrolled=true")

			// Confirm enrollment is gone.
			enrolled, err := repo.IsEnrolled(ctx, pid)
			Expect(err).NotTo(HaveOccurred())
			Expect(enrolled).To(BeFalse())

			// Not-enrolled player.
			pid2 := ulid.Make().String()
			insertTOTPPlayer(pool, pid2, "ucleb"+pid2)

			wasEnrolled2, err := repo.ClearEnrollment(ctx, pid2)
			Expect(err).NotTo(HaveOccurred())
			Expect(wasEnrolled2).To(BeFalse(), "ClearEnrollment on non-enrolled player should return wasEnrolled=false")
		})
	})
})

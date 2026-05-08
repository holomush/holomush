// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package totp_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/test/testutil"
)

// testPool is the shared database pool for integration tests.
var testPool *pgxpool.Pool

// TestMain sets up a PostgreSQL testcontainer for integration tests.
func TestMain(m *testing.M) {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	if err != nil {
		panic("failed to start postgres container: " + err.Error())
	}

	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	if err != nil {
		_ = pgEnv.Terminate(ctx)
		panic("failed to create migrator: " + err.Error())
	}
	if err := migrator.Up(); err != nil {
		_ = migrator.Close()
		_ = pgEnv.Terminate(ctx)
		panic("failed to run migrations: " + err.Error())
	}
	_ = migrator.Close()

	pool, err := pgxpool.New(ctx, pgEnv.ConnStr)
	if err != nil {
		_ = pgEnv.Terminate(ctx)
		panic("failed to create pool: " + err.Error())
	}

	testPool = pool

	code := m.Run()

	pool.Close()
	_ = pgEnv.Terminate(ctx)

	os.Exit(code)
}

// insertPlayer inserts a minimal player row for use as a foreign-key anchor.
func insertPlayer(t *testing.T, pool *pgxpool.Pool, id, username string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, $3)`,
		id, username, "hash-placeholder",
	)
	require.NoError(t, err, "insertPlayer")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM players WHERE id = $1`, id)
	})
}

// fakeHasher is a minimal RecoveryCodeHasher for integration tests that
// stores raw codes as hashes (identity). Suitable only for controlled tests
// where we control the stored hash value.
type fakeHasher struct{}

func (fakeHasher) Verify(rawCode, encodedHash string) (bool, error) {
	return rawCode == encodedHash, nil
}

// --- INV-A2: concurrent BootstrapClaim atomicity ---

// TestRepoBootstrapClaimConcurrentExactlyOneSucceeds verifies INV-A2: when N
// goroutines race to claim the same bootstrap key, exactly one succeeds.
func TestRepoBootstrapClaimConcurrentExactlyOneSucceeds(t *testing.T) {
	repo := totp.NewRepository(testPool)
	ctx := context.Background()

	const N = 8
	players := make([]string, N)
	for i := range players {
		players[i] = ulid.Make().String()
		insertPlayer(t, testPool, players[i], "u"+players[i])
	}

	now := time.Now().UTC()
	key := "totp_v1_concurrent_" + ulid.Make().String() // unique per test run
	var (
		wg        sync.WaitGroup
		successes int
		mu        sync.Mutex
	)
	wg.Add(N)
	for i := 0; i < N; i++ {
		pid := players[i]
		go func() {
			defer wg.Done()
			ok, err := repo.BootstrapClaim(ctx, key, pid, now)
			require.NoError(t, err)
			if ok {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, successes, "exactly one BootstrapClaim must win (INV-A2)")
}

// --- BootstrapEnrollAtomic rollback ---

// TestRepoBootstrapEnrollAtomicRollsBackOnInsertError verifies that when
// InsertEnrollment fails (duplicate player_id), the BootstrapClaim row is
// also rolled back, leaving the key available for re-claim.
func TestRepoBootstrapEnrollAtomicRollsBackOnInsertError(t *testing.T) {
	repo := totp.NewRepository(testPool)
	ctx := context.Background()

	pid := ulid.Make().String()
	insertPlayer(t, testPool, pid, "uatm"+pid)

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

	// First call succeeds.
	err := repo.BootstrapEnrollAtomic(ctx, key, pid, rec)
	require.NoError(t, err, "first BootstrapEnrollAtomic should succeed")

	// Second call with same key must fail with ErrBootstrapAlreadyConsumed.
	err = repo.BootstrapEnrollAtomic(ctx, key, pid, rec)
	require.Error(t, err)
	require.ErrorIs(t, err, totp.ErrBootstrapAlreadyConsumed)
}

// --- PlayerExists ---

// TestRepoPlayerExistsReturnsTrueForExistingPlayer verifies the happy path.
func TestRepoPlayerExistsReturnsTrueForExistingPlayer(t *testing.T) {
	repo := totp.NewRepository(testPool)
	ctx := context.Background()

	pid := ulid.Make().String()
	insertPlayer(t, testPool, pid, "upex"+pid)

	exists, err := repo.PlayerExists(ctx, pid)
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestRepoPlayerExistsReturnsFalseForMissingPlayer verifies the not-found path.
func TestRepoPlayerExistsReturnsFalseForMissingPlayer(t *testing.T) {
	repo := totp.NewRepository(testPool)
	ctx := context.Background()

	exists, err := repo.PlayerExists(ctx, ulid.Make().String())
	require.NoError(t, err)
	assert.False(t, exists)
}

// --- PlayerIDFromUsername ---

// TestRepoPlayerIDFromUsernameReturnsIDForKnownUsername verifies the happy path.
func TestRepoPlayerIDFromUsernameReturnsIDForKnownUsername(t *testing.T) {
	repo := totp.NewRepository(testPool)
	ctx := context.Background()

	pid := ulid.Make().String()
	username := "uname_" + pid[:6]
	insertPlayer(t, testPool, pid, username)

	got, err := repo.PlayerIDFromUsername(ctx, username)
	require.NoError(t, err)
	assert.Equal(t, pid, got)
}

// TestRepoPlayerIDFromUsernameReturnsErrorForUnknownUsername verifies error on miss.
func TestRepoPlayerIDFromUsernameReturnsErrorForUnknownUsername(t *testing.T) {
	repo := totp.NewRepository(testPool)
	ctx := context.Background()

	_, err := repo.PlayerIDFromUsername(ctx, "no_such_user_ever")
	require.Error(t, err)
}

// --- InsertEnrollment round-trip ---

// TestRepoInsertEnrollmentRoundTrip verifies that an enrollment written by
// InsertEnrollment is subsequently readable via IsEnrolled and LoadEnrollment.
func TestRepoInsertEnrollmentRoundTrip(t *testing.T) {
	repo := totp.NewRepository(testPool)
	ctx := context.Background()

	pid := ulid.Make().String()
	insertPlayer(t, testPool, pid, "uins"+pid)

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
	require.NoError(t, err)

	enrolled, err := repo.IsEnrolled(ctx, pid)
	require.NoError(t, err)
	assert.True(t, enrolled)

	// LoadEnrollment requires a transaction (FOR UPDATE).
	var state totp.VerifyState
	txErr := repo.InTransaction(ctx, func(txCtx context.Context) error {
		var e error
		state, e = repo.LoadEnrollment(txCtx, pid)
		return e
	})
	require.NoError(t, txErr)
	assert.Equal(t, pid, state.PlayerID)
	assert.Equal(t, []byte("mysecret"), state.WrappedSecret)
	assert.Equal(t, "wk-test", state.WrapKeyID)
	assert.Equal(t, 0, state.FailedAttempts)
	assert.Nil(t, state.LockedUntil)
}

// --- LoadEnrollment ErrNotEnrolled ---

// TestRepoLoadEnrollmentReturnsErrNotEnrolled verifies that LoadEnrollment
// returns ErrNotEnrolled when no enrollment exists for the player.
func TestRepoLoadEnrollmentReturnsErrNotEnrolled(t *testing.T) {
	repo := totp.NewRepository(testPool)
	ctx := context.Background()

	pid := ulid.Make().String()
	insertPlayer(t, testPool, pid, "ulen"+pid)

	txErr := repo.InTransaction(ctx, func(txCtx context.Context) error {
		_, err := repo.LoadEnrollment(txCtx, pid)
		return err
	})
	require.ErrorIs(t, txErr, totp.ErrNotEnrolled)
}

// --- MarkVerified ---

// TestRepoMarkVerifiedResetsLockoutFields verifies that MarkVerified zeroes
// failed_attempts, clears locked_until, and sets last_used_step.
func TestRepoMarkVerifiedResetsLockoutFields(t *testing.T) {
	repo := totp.NewRepository(testPool)
	ctx := context.Background()

	pid := ulid.Make().String()
	insertPlayer(t, testPool, pid, "umv"+pid)

	now := time.Now().UTC().Truncate(time.Microsecond)
	rec := totp.EnrollmentRecord{
		PlayerID:      pid,
		WrappedSecret: []byte("s"),
		WrapKeyID:     "k",
		EnrolledAt:    now,
	}
	require.NoError(t, repo.InsertEnrollment(ctx, rec))

	// Artificially set failed_attempts and locked_until.
	_, err := testPool.Exec(ctx,
		`UPDATE player_totp SET failed_attempts = 5, locked_until = $1 WHERE player_id = $2`,
		now.Add(15*time.Minute), pid,
	)
	require.NoError(t, err)

	step := int64(42)
	require.NoError(t, repo.MarkVerified(ctx, pid, step, now))

	var state totp.VerifyState
	txErr := repo.InTransaction(ctx, func(txCtx context.Context) error {
		var e error
		state, e = repo.LoadEnrollment(txCtx, pid)
		return e
	})
	require.NoError(t, txErr)
	assert.Equal(t, 0, state.FailedAttempts)
	assert.Nil(t, state.LockedUntil)
	require.NotNil(t, state.LastUsedStep)
	assert.Equal(t, step, *state.LastUsedStep)
}

// --- ConsumeRecoveryCode single-use ---

// TestRepoConsumeRecoveryCodeSingleUse verifies that a recovery code can only
// be consumed once; the second attempt returns ErrInvalidRecoveryCode.
func TestRepoConsumeRecoveryCodeSingleUse(t *testing.T) {
	repo := totp.NewRepository(testPool)
	ctx := context.Background()

	pid := ulid.Make().String()
	insertPlayer(t, testPool, pid, "ucrc"+pid)

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
	require.NoError(t, repo.InsertEnrollment(ctx, rec))

	hasher := fakeHasher{}

	// First consume succeeds and returns the right ULID.
	gotID, err := repo.ConsumeRecoveryCode(ctx, pid, rawCode, hasher, now)
	require.NoError(t, err)
	assert.Equal(t, codeID, gotID)

	// Second consume on the same code must fail.
	_, err = repo.ConsumeRecoveryCode(ctx, pid, rawCode, hasher, now)
	require.ErrorIs(t, err, totp.ErrInvalidRecoveryCode)
}

// --- ClearEnrollment ---

// TestRepoClearEnrollmentReturnsWasEnrolled verifies that ClearEnrollment
// returns wasEnrolled=true for an enrolled player and false for a non-enrolled player.
func TestRepoClearEnrollmentReturnsWasEnrolled(t *testing.T) {
	repo := totp.NewRepository(testPool)
	ctx := context.Background()

	// Enrolled player.
	pid := ulid.Make().String()
	insertPlayer(t, testPool, pid, "uclea"+pid)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.InsertEnrollment(ctx, totp.EnrollmentRecord{
		PlayerID:      pid,
		WrappedSecret: []byte("s"),
		WrapKeyID:     "k",
		EnrolledAt:    now,
	}))

	wasEnrolled, err := repo.ClearEnrollment(ctx, pid)
	require.NoError(t, err)
	assert.True(t, wasEnrolled, "ClearEnrollment should return wasEnrolled=true")

	// Confirm enrollment is gone.
	enrolled, err := repo.IsEnrolled(ctx, pid)
	require.NoError(t, err)
	assert.False(t, enrolled)

	// Not-enrolled player.
	pid2 := ulid.Make().String()
	insertPlayer(t, testPool, pid2, "ucleb"+pid2)

	wasEnrolled2, err := repo.ClearEnrollment(ctx, pid2)
	require.NoError(t, err)
	assert.False(t, wasEnrolled2, "ClearEnrollment on non-enrolled player should return wasEnrolled=false")
}

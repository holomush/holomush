// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// pgxmock-driven unit tests for repo.go error paths. The integration
// suite at repo_integration_test.go covers happy paths against real PG;
// these tests cover the error-wrapping branches that real PG would only
// hit under pathological conditions (driver-level failures, type
// mismatches, transient connection drops).

func newMockedRepo(t *testing.T) (*repo, pgxmock.PgxPoolIface) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)
	return newRepoForTest(mock), mock
}

// stubHasher is a minimal RecoveryCodeHasher; selects which hashes match.
type stubHasher struct {
	matches   map[string]bool
	verifyErr error
}

func (s stubHasher) Verify(_, encodedHash string) (bool, error) {
	if s.verifyErr != nil {
		return false, s.verifyErr
	}
	return s.matches[encodedHash], nil
}

// --- BootstrapClaim ---

func TestRepoBootstrapClaimWrapsDriverError(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectQuery(`INSERT INTO crypto_bootstrap_state`).
		WithArgs("totp_v1", pgxmock.AnyArg(), "01HZ").
		WillReturnError(errors.New("conn refused"))

	_, err := r.BootstrapClaim(context.Background(), "totp_v1", "01HZ", time.Now())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_BOOTSTRAP_CLAIM")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoBootstrapClaimReturnsFalseOnConflict(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectQuery(`INSERT INTO crypto_bootstrap_state`).
		WithArgs("totp_v1", pgxmock.AnyArg(), "01HZ").
		WillReturnError(pgx.ErrNoRows)

	claimed, err := r.BootstrapClaim(context.Background(), "totp_v1", "01HZ", time.Now())
	require.NoError(t, err)
	assert.False(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoBootstrapClaimReturnsTrueOnInsert(t *testing.T) {
	r, mock := newMockedRepo(t)
	rows := pgxmock.NewRows([]string{"key"}).AddRow("totp_v1")
	mock.ExpectQuery(`INSERT INTO crypto_bootstrap_state`).
		WithArgs("totp_v1", pgxmock.AnyArg(), "01HZ").
		WillReturnRows(rows)

	claimed, err := r.BootstrapClaim(context.Background(), "totp_v1", "01HZ", time.Now())
	require.NoError(t, err)
	assert.True(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- InTransaction ---

func TestRepoInTransactionWrapsBeginError(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectBegin().WillReturnError(errors.New("begin failed"))

	err := r.InTransaction(context.Background(), func(_ context.Context) error {
		t.Fatal("fn should not be called when Begin fails")
		return nil
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_TX_BEGIN_FAILED")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoInTransactionRollsBackOnFnError(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	want := errors.New("fn-side failure")
	err := r.InTransaction(context.Background(), func(_ context.Context) error {
		return want
	})
	assert.ErrorIs(t, err, want)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoInTransactionWrapsCommitError(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectBegin()
	mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
	mock.ExpectRollback() // deferred rollback after commit failure is a no-op in pgx but still expected

	err := r.InTransaction(context.Background(), func(_ context.Context) error {
		return nil
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_TX_COMMIT_FAILED")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoInTransactionCommitsHappyPath(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectBegin()
	mock.ExpectCommit()

	called := false
	err := r.InTransaction(context.Background(), func(ctx context.Context) error {
		called = true
		// Verify the active tx is stashed on context.
		assert.NotNil(t, txFromContext(ctx))
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- PlayerExists / PlayerIDFromUsername / IsEnrolled (parameterized) ---

func TestRepoPlayerLookupWrapping(t *testing.T) {
	cases := []struct {
		name         string
		invoke       func(r *repo) error
		expectQuery  string
		args         []any
		failureCode  string
		notFoundCode string // "" if pgx.ErrNoRows is a non-error response (returns false/zero)
	}{
		{
			name: "PlayerExists driver error",
			invoke: func(r *repo) error {
				_, err := r.PlayerExists(context.Background(), "01HZ")
				return err
			},
			expectQuery: `SELECT 1 FROM players WHERE id`,
			args:        []any{"01HZ"},
			failureCode: "TOTP_REPO_PLAYER_EXISTS",
		},
		{
			name: "PlayerIDFromUsername driver error",
			invoke: func(r *repo) error {
				_, err := r.PlayerIDFromUsername(context.Background(), "alice")
				return err
			},
			expectQuery: `SELECT id FROM players WHERE username`,
			args:        []any{"alice"},
			failureCode: "TOTP_REPO_PLAYER_LOOKUP",
		},
		{
			name: "IsEnrolled driver error",
			invoke: func(r *repo) error {
				_, err := r.IsEnrolled(context.Background(), "01HZ")
				return err
			},
			expectQuery: `SELECT 1 FROM player_totp WHERE player_id`,
			args:        []any{"01HZ"},
			failureCode: "TOTP_REPO_IS_ENROLLED",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, mock := newMockedRepo(t)
			mock.ExpectQuery(tc.expectQuery).
				WithArgs(tc.args...).
				WillReturnError(errors.New("driver fail"))

			err := tc.invoke(r)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, tc.failureCode)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepoPlayerExistsReturnsFalseOnNoRows(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectQuery(`SELECT 1 FROM players WHERE id`).
		WithArgs("ghost").
		WillReturnError(pgx.ErrNoRows)

	exists, err := r.PlayerExists(context.Background(), "ghost")
	require.NoError(t, err)
	assert.False(t, exists)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoIsEnrolledReturnsFalseOnNoRows(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectQuery(`SELECT 1 FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnError(pgx.ErrNoRows)

	enrolled, err := r.IsEnrolled(context.Background(), "01HZ")
	require.NoError(t, err)
	assert.False(t, enrolled)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoPlayerIDFromUsernameReturnsCodedNotFound(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectQuery(`SELECT id FROM players WHERE username`).
		WithArgs("ghost").
		WillReturnError(pgx.ErrNoRows)

	_, err := r.PlayerIDFromUsername(context.Background(), "ghost")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_PLAYER_NOT_FOUND")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- InsertEnrollment ---

func TestRepoInsertEnrollmentWrapsTotpInsertError(t *testing.T) {
	r, mock := newMockedRepo(t)
	rec := EnrollmentRecord{
		PlayerID:      "01HZ",
		WrappedSecret: []byte("w"),
		WrapKeyID:     "kek-v1",
		EnrolledAt:    time.Now(),
	}
	mock.ExpectExec(`INSERT INTO player_totp`).
		WithArgs("01HZ", []byte("w"), "kek-v1", pgxmock.AnyArg()).
		WillReturnError(errors.New("unique violation"))

	err := r.InsertEnrollment(context.Background(), rec)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_INSERT_TOTP")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoInsertEnrollmentWrapsRecoveryCodeInsertError(t *testing.T) {
	r, mock := newMockedRepo(t)
	codeID := ulid.Make()
	rec := EnrollmentRecord{
		PlayerID:      "01HZ",
		WrappedSecret: []byte("w"),
		WrapKeyID:     "kek-v1",
		EnrolledAt:    time.Now(),
		RecoveryCodes: []HashedRecoveryCode{
			{ID: codeID, CodeHash: "$argon2id$...", CreatedAt: time.Now()},
		},
	}
	mock.ExpectExec(`INSERT INTO player_totp`).
		WithArgs("01HZ", []byte("w"), "kek-v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(`INSERT INTO player_totp_recovery_codes`).
		WithArgs(codeID.String(), "01HZ", "$argon2id$...", pgxmock.AnyArg()).
		WillReturnError(errors.New("recovery code insert failed"))

	err := r.InsertEnrollment(context.Background(), rec)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_INSERT_RECOVERY_CODE")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- LoadEnrollment ---

func TestRepoLoadEnrollmentWrapsDriverError(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectQuery(`SELECT wrapped_secret, wrap_key_id, last_used_step, failed_attempts, locked_until`).
		WithArgs("01HZ").
		WillReturnError(errors.New("conn lost"))

	_, err := r.LoadEnrollment(context.Background(), "01HZ")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_LOAD_ENROLLMENT")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoLoadEnrollmentReturnsErrNotEnrolledOnNoRows(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectQuery(`SELECT wrapped_secret, wrap_key_id, last_used_step, failed_attempts, locked_until`).
		WithArgs("01HZ").
		WillReturnError(pgx.ErrNoRows)

	_, err := r.LoadEnrollment(context.Background(), "01HZ")
	errutil.AssertErrorCode(t, err, "TOTP_NOT_ENROLLED")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- IncrementFailedAttempts ---

func TestRepoIncrementFailedAttemptsWrapsDriverError(t *testing.T) {
	r, mock := newMockedRepo(t)
	// $3 = now.UnixNano() (BIGINT epoch-ns); $4 = lockoutDuration.Nanoseconds()
	mock.ExpectQuery(`UPDATE player_totp\s+SET failed_attempts`).
		WithArgs("01HZ", 5, pgxmock.AnyArg(), (15 * time.Minute).Nanoseconds()).
		WillReturnError(errors.New("update failed"))

	_, err := r.IncrementFailedAttempts(context.Background(), "01HZ", 5, 15*time.Minute, time.Now())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_INCREMENT_FAILED")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoIncrementFailedAttemptsHappyPath(t *testing.T) {
	r, mock := newMockedRepo(t)
	now := time.Now()
	rows := pgxmock.NewRows([]string{
		"wrapped_secret", "wrap_key_id", "last_used_step", "failed_attempts", "locked_until",
	}).AddRow([]byte("w"), "kek-v1", (*int64)(nil), 3, nil)
	mock.ExpectQuery(`UPDATE player_totp\s+SET failed_attempts`).
		WithArgs("01HZ", 5, pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	state, err := r.IncrementFailedAttempts(context.Background(), "01HZ", 5, 15*time.Minute, now)
	require.NoError(t, err)
	assert.Equal(t, "01HZ", state.PlayerID)
	assert.Equal(t, "kek-v1", state.WrapKeyID)
	assert.Equal(t, 3, state.FailedAttempts)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoLoadEnrollmentHappyPath(t *testing.T) {
	r, mock := newMockedRepo(t)
	step := int64(42)
	rows := pgxmock.NewRows([]string{
		"wrapped_secret", "wrap_key_id", "last_used_step", "failed_attempts", "locked_until",
	}).AddRow([]byte("w"), "kek-v1", &step, 0, nil)
	mock.ExpectQuery(`SELECT wrapped_secret, wrap_key_id, last_used_step, failed_attempts, locked_until`).
		WithArgs("01HZ").
		WillReturnRows(rows)

	state, err := r.LoadEnrollment(context.Background(), "01HZ")
	require.NoError(t, err)
	assert.Equal(t, "01HZ", state.PlayerID)
	assert.Equal(t, "kek-v1", state.WrapKeyID)
	require.NotNil(t, state.LastUsedStep)
	assert.Equal(t, int64(42), *state.LastUsedStep)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoInsertEnrollmentHappyPath(t *testing.T) {
	r, mock := newMockedRepo(t)
	codeID := ulid.Make()
	rec := EnrollmentRecord{
		PlayerID:      "01HZ",
		WrappedSecret: []byte("w"),
		WrapKeyID:     "kek-v1",
		EnrolledAt:    time.Now(),
		RecoveryCodes: []HashedRecoveryCode{
			{ID: codeID, CodeHash: "$argon2id$h", CreatedAt: time.Now()},
		},
	}
	mock.ExpectExec(`INSERT INTO player_totp`).
		WithArgs("01HZ", []byte("w"), "kek-v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(`INSERT INTO player_totp_recovery_codes`).
		WithArgs(codeID.String(), "01HZ", "$argon2id$h", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	require.NoError(t, r.InsertEnrollment(context.Background(), rec))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoClearEnrollmentNotEnrolled(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT 1 FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectExec(`DELETE FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec(`DELETE FROM player_totp_recovery_codes`).
		WithArgs("01HZ").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectCommit()

	wasEnrolled, err := r.ClearEnrollment(context.Background(), "01HZ")
	require.NoError(t, err)
	assert.False(t, wasEnrolled)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoClearEnrollmentWrapsRecoveryDeleteError(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT 1 FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnRows(pgxmock.NewRows([]string{"x"}).AddRow(1))
	mock.ExpectExec(`DELETE FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(`DELETE FROM player_totp_recovery_codes`).
		WithArgs("01HZ").
		WillReturnError(errors.New("delete recovery failed"))
	mock.ExpectRollback()

	_, err := r.ClearEnrollment(context.Background(), "01HZ")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_CLEAR_RECOVERY")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- MarkVerified ---

func TestRepoMarkVerifiedWrapsDriverError(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectExec(`UPDATE player_totp SET last_used_step`).
		WithArgs("01HZ", int64(42), pgxmock.AnyArg()).
		WillReturnError(errors.New("update failed"))

	err := r.MarkVerified(context.Background(), "01HZ", 42, time.Now())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_MARK_VERIFIED")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoMarkVerifiedHappyPath(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectExec(`UPDATE player_totp SET last_used_step`).
		WithArgs("01HZ", int64(42), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	require.NoError(t, r.MarkVerified(context.Background(), "01HZ", 42, time.Now()))
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- ConsumeRecoveryCode (txn-wrapped) ---

func TestRepoConsumeRecoveryCodeReturnsErrInvalidWhenNoMatch(t *testing.T) {
	r, mock := newMockedRepo(t)
	codeID := ulid.Make()
	mock.ExpectBegin()
	rows := pgxmock.NewRows([]string{"id", "code_hash"}).
		AddRow(codeID.String(), "$hash-1")
	mock.ExpectQuery(`SELECT id, code_hash FROM player_totp_recovery_codes`).
		WithArgs("01HZ").
		WillReturnRows(rows)
	mock.ExpectRollback()

	hasher := stubHasher{matches: map[string]bool{}} // no matches

	_, err := r.ConsumeRecoveryCode(context.Background(), "01HZ", "wrong", hasher, time.Now())
	errutil.AssertErrorCode(t, err, "TOTP_INVALID_RECOVERY_CODE")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRepoConsumeRecoveryCodeWrapsRowsErr verifies that an error surfaced
// through rows.Err() (driver-level read error after Next() loop) is
// wrapped with TOTP_REPO_RECOVERY_SCAN, NOT silently surfaced as
// ErrInvalidRecoveryCode (which would mask infrastructure failures as
// user-input errors).
func TestRepoConsumeRecoveryCodeWrapsRowsErr(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectBegin()
	rows := pgxmock.NewRows([]string{"id", "code_hash"}).RowError(0, errors.New("read error mid-iteration"))
	rows.AddRow(ulid.Make().String(), "$argon2id$h")
	mock.ExpectQuery(`SELECT id, code_hash FROM player_totp_recovery_codes`).
		WithArgs("01HZ").
		WillReturnRows(rows)
	mock.ExpectRollback()

	_, err := r.ConsumeRecoveryCode(context.Background(), "01HZ", "x", stubHasher{}, time.Now())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_RECOVERY_SCAN")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoConsumeRecoveryCodeWrapsScanError(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, code_hash FROM player_totp_recovery_codes`).
		WithArgs("01HZ").
		WillReturnError(errors.New("query failed"))
	mock.ExpectRollback()

	_, err := r.ConsumeRecoveryCode(context.Background(), "01HZ", "x", stubHasher{}, time.Now())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_RECOVERY_SCAN")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoConsumeRecoveryCodeHappyPath(t *testing.T) {
	r, mock := newMockedRepo(t)
	codeID := ulid.Make()
	matchHash := "$argon2id$matchHash"
	mock.ExpectBegin()
	rows := pgxmock.NewRows([]string{"id", "code_hash"}).
		AddRow(codeID.String(), matchHash)
	mock.ExpectQuery(`SELECT id, code_hash FROM player_totp_recovery_codes`).
		WithArgs("01HZ").
		WillReturnRows(rows)
	mock.ExpectExec(`UPDATE player_totp_recovery_codes SET consumed_at`).
		WithArgs(codeID.String(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	hasher := stubHasher{matches: map[string]bool{matchHash: true}}

	consumedID, err := r.ConsumeRecoveryCode(context.Background(), "01HZ", "right", hasher, time.Now())
	require.NoError(t, err)
	assert.Equal(t, codeID, consumedID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoConsumeRecoveryCodeWrapsConsumeUpdateError(t *testing.T) {
	r, mock := newMockedRepo(t)
	codeID := ulid.Make()
	matchHash := "$argon2id$matchHash"
	mock.ExpectBegin()
	rows := pgxmock.NewRows([]string{"id", "code_hash"}).
		AddRow(codeID.String(), matchHash)
	mock.ExpectQuery(`SELECT id, code_hash FROM player_totp_recovery_codes`).
		WithArgs("01HZ").
		WillReturnRows(rows)
	mock.ExpectExec(`UPDATE player_totp_recovery_codes SET consumed_at`).
		WithArgs(codeID.String(), pgxmock.AnyArg()).
		WillReturnError(errors.New("update failed"))
	mock.ExpectRollback()

	hasher := stubHasher{matches: map[string]bool{matchHash: true}}

	_, err := r.ConsumeRecoveryCode(context.Background(), "01HZ", "right", hasher, time.Now())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_RECOVERY_CONSUME")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- ClearEnrollment (txn-wrapped) ---

func TestRepoClearEnrollmentWrapsTotpDeleteError(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT 1 FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnRows(pgxmock.NewRows([]string{"x"}).AddRow(1))
	mock.ExpectExec(`DELETE FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnError(errors.New("delete failed"))
	mock.ExpectRollback()

	_, err := r.ClearEnrollment(context.Background(), "01HZ")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_CLEAR_TOTP")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoClearEnrollmentHappyPath(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT 1 FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnRows(pgxmock.NewRows([]string{"x"}).AddRow(1))
	mock.ExpectExec(`DELETE FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(`DELETE FROM player_totp_recovery_codes`).
		WithArgs("01HZ").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectCommit()

	wasEnrolled, err := r.ClearEnrollment(context.Background(), "01HZ")
	require.NoError(t, err)
	assert.True(t, wasEnrolled)
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- BootstrapEnrollAtomic ---

func TestRepoBootstrapEnrollAtomicReturnsErrAlreadyConsumedOnConflict(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO crypto_bootstrap_state`).
		WithArgs("totp_v1", pgxmock.AnyArg(), "01HZ").
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectRollback()

	rec := EnrollmentRecord{PlayerID: "01HZ", EnrolledAt: time.Now()}
	err := r.BootstrapEnrollAtomic(context.Background(), "totp_v1", "01HZ", rec)
	errutil.AssertErrorCode(t, err, "TOTP_BOOTSTRAP_CONSUMED")
	require.NoError(t, mock.ExpectationsWereMet())
}

// --- RecoverAndClearAtomic ---

func TestRepoRecoverAndClearAtomicHappyPath(t *testing.T) {
	r, mock := newMockedRepo(t)
	codeID := ulid.Make()
	matchHash := "$argon2id$matchHash"
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, code_hash FROM player_totp_recovery_codes`).
		WithArgs("01HZ").
		WillReturnRows(pgxmock.NewRows([]string{"id", "code_hash"}).AddRow(codeID.String(), matchHash))
	mock.ExpectExec(`UPDATE player_totp_recovery_codes SET consumed_at`).
		WithArgs(codeID.String(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectQuery(`SELECT 1 FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnRows(pgxmock.NewRows([]string{"x"}).AddRow(1))
	mock.ExpectExec(`DELETE FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(`DELETE FROM player_totp_recovery_codes`).
		WithArgs("01HZ").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectCommit()

	hasher := stubHasher{matches: map[string]bool{matchHash: true}}

	consumedID, wasEnrolled, err := r.RecoverAndClearAtomic(context.Background(), "01HZ", "right", hasher, time.Now())
	require.NoError(t, err)
	assert.Equal(t, codeID, consumedID)
	assert.True(t, wasEnrolled)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Atomicity assertion: if Clear fails after Consume succeeds, the whole
// transaction rolls back so the recovery code is NOT actually consumed.
// Without RecoverAndClearAtomic this would leave a spent code + active
// TOTP — exactly the failure mode CodeRabbit flagged.
func TestRepoRecoverAndClearAtomicRollsBackOnClearFailure(t *testing.T) {
	r, mock := newMockedRepo(t)
	codeID := ulid.Make()
	matchHash := "$argon2id$matchHash"
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, code_hash FROM player_totp_recovery_codes`).
		WithArgs("01HZ").
		WillReturnRows(pgxmock.NewRows([]string{"id", "code_hash"}).AddRow(codeID.String(), matchHash))
	mock.ExpectExec(`UPDATE player_totp_recovery_codes SET consumed_at`).
		WithArgs(codeID.String(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectQuery(`SELECT 1 FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnRows(pgxmock.NewRows([]string{"x"}).AddRow(1))
	mock.ExpectExec(`DELETE FROM player_totp WHERE player_id`).
		WithArgs("01HZ").
		WillReturnError(errors.New("clear failed"))
	mock.ExpectRollback()

	hasher := stubHasher{matches: map[string]bool{matchHash: true}}

	_, _, err := r.RecoverAndClearAtomic(context.Background(), "01HZ", "right", hasher, time.Now())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "TOTP_REPO_CLEAR_TOTP")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoRecoverAndClearAtomicReturnsErrInvalidOnNoMatch(t *testing.T) {
	r, mock := newMockedRepo(t)
	codeID := ulid.Make()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, code_hash FROM player_totp_recovery_codes`).
		WithArgs("01HZ").
		WillReturnRows(pgxmock.NewRows([]string{"id", "code_hash"}).AddRow(codeID.String(), "$other"))
	mock.ExpectRollback()

	hasher := stubHasher{matches: map[string]bool{}} // no matches

	_, _, err := r.RecoverAndClearAtomic(context.Background(), "01HZ", "wrong", hasher, time.Now())
	errutil.AssertErrorCode(t, err, "TOTP_INVALID_RECOVERY_CODE")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepoBootstrapEnrollAtomicHappyPath(t *testing.T) {
	r, mock := newMockedRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO crypto_bootstrap_state`).
		WithArgs("totp_v1", pgxmock.AnyArg(), "01HZ").
		WillReturnRows(pgxmock.NewRows([]string{"key"}).AddRow("totp_v1"))
	mock.ExpectExec(`INSERT INTO player_totp`).
		WithArgs("01HZ", []byte("w"), "kek-v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	rec := EnrollmentRecord{
		PlayerID:      "01HZ",
		WrappedSecret: []byte("w"),
		WrapKeyID:     "kek-v1",
		EnrolledAt:    time.Now(),
	}
	require.NoError(t, r.BootstrapEnrollAtomic(context.Background(), "totp_v1", "01HZ", rec))
	require.NoError(t, mock.ExpectationsWereMet())
}

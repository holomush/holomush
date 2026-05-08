// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// txKey is the context key for an active pgx.Tx stored by InTransaction.
// Pattern follows internal/world/postgres/helpers.go.
type txKey struct{}

func txFromContext(ctx context.Context) pgx.Tx {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if !ok {
		return nil
	}
	return tx
}

// dbtx is the interface satisfied directly by both *pgxpool.Pool and pgx.Tx.
// No wrapper structs needed — both types already have these exact signatures.
type dbtx interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// pgPool is the pool-shaped interface used by the repo's transaction
// scaffolding. *pgxpool.Pool satisfies it directly; tests can inject
// pgxmock.PgxPoolIface (also satisfies it) for unit-level coverage of
// error paths that real PG would only hit in pathological conditions.
type pgPool interface {
	dbtx
	Begin(ctx context.Context) (pgx.Tx, error)
}

func dbFromCtx(ctx context.Context, pool pgPool) dbtx {
	if tx := txFromContext(ctx); tx != nil {
		return tx
	}
	return pool
}

type repo struct{ pool pgPool }

// NewRepository constructs a Repository backed by the given connection pool.
func NewRepository(pool *pgxpool.Pool) Repository { return &repo{pool: pool} }

// newRepoForTest constructs a *repo with an arbitrary pgPool. Used by
// the pgxmock-driven unit tests in repo_unit_test.go to exercise error
// paths without standing up a real testcontainer.
func newRepoForTest(pool pgPool) *repo { return &repo{pool: pool} }

// InTransaction begins a txn, stores it on context via txKey{}, and runs fn.
// fn returning nil commits; non-nil rolls back.
// Pattern follows internal/world/postgres/transactor.go.
func (r *repo) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return oops.Code("TOTP_TX_BEGIN_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	txCtx := context.WithValue(ctx, txKey{}, tx)
	if err := fn(txCtx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return oops.Code("TOTP_TX_COMMIT_FAILED").Wrap(err)
	}
	return nil
}

// BootstrapClaim inserts a row into crypto_bootstrap_state.
// Returns (true, nil) if the row was inserted; (false, nil) if the key already exists.
func (r *repo) BootstrapClaim(ctx context.Context, key, playerID string, at time.Time) (bool, error) {
	const q = `INSERT INTO crypto_bootstrap_state (key, consumed_at, consumed_by_player_id)
	           VALUES ($1, $2, $3) ON CONFLICT (key) DO NOTHING RETURNING key`
	var got string
	err := dbFromCtx(ctx, r.pool).QueryRow(ctx, q, key, at, playerID).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, oops.Code("TOTP_REPO_BOOTSTRAP_CLAIM").Wrap(err)
	}
	return true, nil
}

// BootstrapEnrollAtomic wraps BootstrapClaim + InsertEnrollment in one PG transaction.
// Returns ErrBootstrapAlreadyConsumed if the claim key is already taken.
func (r *repo) BootstrapEnrollAtomic(ctx context.Context, key, playerID string, rec EnrollmentRecord) error {
	return r.InTransaction(ctx, func(txCtx context.Context) error {
		claimed, err := r.BootstrapClaim(txCtx, key, playerID, rec.EnrolledAt)
		if err != nil {
			return err
		}
		if !claimed {
			return ErrBootstrapAlreadyConsumed
		}
		return r.InsertEnrollment(txCtx, rec)
	})
}

// PlayerExists returns true if a player row with the given ID exists.
func (r *repo) PlayerExists(ctx context.Context, playerID string) (bool, error) {
	var x int
	err := dbFromCtx(ctx, r.pool).QueryRow(ctx,
		`SELECT 1 FROM players WHERE id = $1`, playerID).Scan(&x)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, oops.Code("TOTP_REPO_PLAYER_EXISTS").Wrap(err)
	}
	return true, nil
}

// PlayerIDFromUsername looks up a player's ID by their username.
// Returns an oops error with code TOTP_REPO_PLAYER_NOT_FOUND if not found.
func (r *repo) PlayerIDFromUsername(ctx context.Context, username string) (string, error) {
	var id string
	err := dbFromCtx(ctx, r.pool).QueryRow(ctx,
		`SELECT id FROM players WHERE username = $1`, username).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", oops.Code("TOTP_REPO_PLAYER_NOT_FOUND").
			With("username", username).Errorf("player not found")
	}
	if err != nil {
		return "", oops.Code("TOTP_REPO_PLAYER_LOOKUP").Wrap(err)
	}
	return id, nil
}

// IsEnrolled returns true if a player_totp row exists for the given player.
func (r *repo) IsEnrolled(ctx context.Context, playerID string) (bool, error) {
	var x int
	err := dbFromCtx(ctx, r.pool).QueryRow(ctx,
		`SELECT 1 FROM player_totp WHERE player_id = $1`, playerID).Scan(&x)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, oops.Code("TOTP_REPO_IS_ENROLLED").Wrap(err)
	}
	return true, nil
}

// InsertEnrollment inserts a player_totp row and all associated recovery codes.
func (r *repo) InsertEnrollment(ctx context.Context, e EnrollmentRecord) error {
	db := dbFromCtx(ctx, r.pool)
	if _, err := db.Exec(ctx,
		`INSERT INTO player_totp (player_id, wrapped_secret, wrap_key_id, enrolled_at)
		 VALUES ($1, $2, $3, $4)`,
		e.PlayerID, e.WrappedSecret, e.WrapKeyID, e.EnrolledAt,
	); err != nil {
		return oops.Code("TOTP_REPO_INSERT_TOTP").Wrap(err)
	}
	const insCode = `INSERT INTO player_totp_recovery_codes (id, player_id, code_hash, created_at)
	                 VALUES ($1, $2, $3, $4)`
	for _, c := range e.RecoveryCodes {
		if _, err := db.Exec(ctx, insCode, c.ID.String(), e.PlayerID, c.CodeHash, c.CreatedAt); err != nil {
			return oops.Code("TOTP_REPO_INSERT_RECOVERY_CODE").Wrap(err)
		}
	}
	return nil
}

// LoadEnrollment fetches the current TOTP enrollment state for a player.
// Uses SELECT FOR UPDATE when inside a transaction for concurrency safety.
// Returns ErrNotEnrolled if no enrollment exists.
func (r *repo) LoadEnrollment(ctx context.Context, playerID string) (VerifyState, error) {
	var s VerifyState
	s.PlayerID = playerID
	err := dbFromCtx(ctx, r.pool).QueryRow(ctx,
		`SELECT wrapped_secret, wrap_key_id, last_used_step, failed_attempts, locked_until
		 FROM player_totp WHERE player_id = $1 FOR UPDATE`, playerID,
	).Scan(&s.WrappedSecret, &s.WrapKeyID, &s.LastUsedStep, &s.FailedAttempts, &s.LockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return VerifyState{}, ErrNotEnrolled
	}
	if err != nil {
		return VerifyState{}, oops.Code("TOTP_REPO_LOAD_ENROLLMENT").Wrap(err)
	}
	return s, nil
}

// IncrementFailedAttempts increments failed_attempts and conditionally sets
// locked_until when the threshold is reached. Returns the post-update state.
func (r *repo) IncrementFailedAttempts(
	ctx context.Context, playerID string,
	threshold int, lockoutDuration time.Duration, now time.Time,
) (VerifyState, error) {
	const q = `
		UPDATE player_totp
		SET failed_attempts = failed_attempts + 1,
		    locked_until    = CASE
		      WHEN failed_attempts + 1 >= $2 THEN $3 + ($4 || ' microseconds')::INTERVAL
		      ELSE locked_until
		    END
		WHERE player_id = $1
		RETURNING wrapped_secret, wrap_key_id, last_used_step, failed_attempts, locked_until`
	var s VerifyState
	s.PlayerID = playerID
	err := dbFromCtx(ctx, r.pool).QueryRow(ctx, q,
		playerID, threshold, now, lockoutDuration.Microseconds(),
	).Scan(&s.WrappedSecret, &s.WrapKeyID, &s.LastUsedStep, &s.FailedAttempts, &s.LockedUntil)
	if err != nil {
		return VerifyState{}, oops.Code("TOTP_REPO_INCREMENT_FAILED").Wrap(err)
	}
	return s, nil
}

// MarkVerified resets failed_attempts and locked_until, and records the last
// used TOTP step to prevent replay.
func (r *repo) MarkVerified(ctx context.Context, playerID string, step int64, at time.Time) error {
	_, err := dbFromCtx(ctx, r.pool).Exec(ctx,
		`UPDATE player_totp SET last_used_step = $2, last_verified_at = $3,
		   failed_attempts = 0, locked_until = NULL
		 WHERE player_id = $1`, playerID, step, at,
	)
	if err != nil {
		return oops.Code("TOTP_REPO_MARK_VERIFIED").Wrap(err)
	}
	return nil
}

// ConsumeRecoveryCode scans unused recovery codes for the player, verifies the
// raw code against each hash in constant time, marks the matching code consumed,
// and returns its ULID. Returns ErrInvalidRecoveryCode if no code matches.
func (r *repo) ConsumeRecoveryCode(
	ctx context.Context, playerID, rawCode string, hasher RecoveryCodeHasher, at time.Time,
) (ulid.ULID, error) {
	var consumedID ulid.ULID
	err := r.InTransaction(ctx, func(txCtx context.Context) error {
		var inner error
		consumedID, inner = r.consumeRecoveryCodeInTx(txCtx, playerID, rawCode, hasher, at)
		return inner
	})
	if err != nil {
		return ulid.ULID{}, err
	}
	return consumedID, nil
}

// consumeRecoveryCodeInTx is the inner-txn helper. Caller MUST be running
// inside an outer InTransaction (txCtx carries the active pgx.Tx). Used
// by ConsumeRecoveryCode (own txn) and RecoverAndClearAtomic (shared txn).
func (r *repo) consumeRecoveryCodeInTx(
	txCtx context.Context, playerID, rawCode string, hasher RecoveryCodeHasher, at time.Time,
) (ulid.ULID, error) {
	rows, qErr := dbFromCtx(txCtx, r.pool).Query(txCtx,
		`SELECT id, code_hash FROM player_totp_recovery_codes
		 WHERE player_id = $1 AND consumed_at IS NULL FOR UPDATE`, playerID)
	if qErr != nil {
		return ulid.ULID{}, oops.Code("TOTP_REPO_RECOVERY_SCAN").Wrap(qErr)
	}
	type cand struct {
		id   string
		hash string
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.hash); err != nil {
			rows.Close()
			return ulid.ULID{}, oops.Code("TOTP_REPO_RECOVERY_SCAN").Wrap(err)
		}
		cands = append(cands, c)
	}
	rows.Close()
	for _, c := range cands {
		ok, vErr := hasher.Verify(rawCode, c.hash)
		if vErr != nil || !ok {
			continue // timing-safe: continue on any mismatch
		}
		if _, err := dbFromCtx(txCtx, r.pool).Exec(txCtx,
			`UPDATE player_totp_recovery_codes SET consumed_at = $2 WHERE id = $1`,
			c.id, at); err != nil {
			return ulid.ULID{}, oops.Code("TOTP_REPO_RECOVERY_CONSUME").Wrap(err)
		}
		parsed, perr := ulid.Parse(c.id)
		if perr != nil {
			return ulid.ULID{}, oops.Code("TOTP_REPO_RECOVERY_ULID_PARSE").Wrap(perr)
		}
		return parsed, nil
	}
	return ulid.ULID{}, ErrInvalidRecoveryCode
}

// ClearEnrollment deletes the player_totp row and all unconsumed recovery codes.
// Returns wasEnrolled=true if the player had an active enrollment.
func (r *repo) ClearEnrollment(ctx context.Context, playerID string) (bool, error) {
	var wasEnrolled bool
	err := r.InTransaction(ctx, func(txCtx context.Context) error {
		var inner error
		wasEnrolled, inner = r.clearEnrollmentInTx(txCtx, playerID)
		return inner
	})
	if err != nil {
		return false, err
	}
	return wasEnrolled, nil
}

// clearEnrollmentInTx is the inner-txn helper. Caller MUST be running
// inside an outer InTransaction. Used by ClearEnrollment (own txn) and
// RecoverAndClearAtomic (shared txn).
func (r *repo) clearEnrollmentInTx(txCtx context.Context, playerID string) (bool, error) {
	wasEnrolled, err := r.IsEnrolled(txCtx, playerID)
	if err != nil {
		return false, err
	}
	if _, err := dbFromCtx(txCtx, r.pool).Exec(txCtx,
		`DELETE FROM player_totp WHERE player_id = $1`, playerID); err != nil {
		return false, oops.Code("TOTP_REPO_CLEAR_TOTP").Wrap(err)
	}
	if _, err := dbFromCtx(txCtx, r.pool).Exec(txCtx,
		`DELETE FROM player_totp_recovery_codes WHERE player_id = $1 AND consumed_at IS NULL`,
		playerID); err != nil {
		return false, oops.Code("TOTP_REPO_CLEAR_RECOVERY").Wrap(err)
	}
	return wasEnrolled, nil
}

// RecoverAndClearAtomic combines ConsumeRecoveryCode + ClearEnrollment into
// a single transaction so that a partial failure can never leave the player
// with a spent recovery code AND an active TOTP enrollment. This is the
// canonical path for the `holomush admin totp recover` CLI; spec INV-A6
// (recovery single-use) and INV-A7 (clear deletes both tables) hold
// jointly under the shared txn.
func (r *repo) RecoverAndClearAtomic(
	ctx context.Context, playerID, rawCode string, hasher RecoveryCodeHasher, at time.Time,
) (consumedID ulid.ULID, wasEnrolled bool, err error) {
	txErr := r.InTransaction(ctx, func(txCtx context.Context) error {
		id, cerr := r.consumeRecoveryCodeInTx(txCtx, playerID, rawCode, hasher, at)
		if cerr != nil {
			return cerr
		}
		consumedID = id
		we, clrErr := r.clearEnrollmentInTx(txCtx, playerID)
		if clrErr != nil {
			return clrErr
		}
		wasEnrolled = we
		return nil
	})
	if txErr != nil {
		return ulid.ULID{}, false, txErr
	}
	return consumedID, wasEnrolled, nil
}

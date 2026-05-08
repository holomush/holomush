// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
)

// Service provides per-player TOTP enrollment, verification, and recovery.
// Persists to PostgreSQL only. Audit emission is the caller's responsibility
// (R5 Option Y).
type Service interface {
	BootstrapEnroll(ctx context.Context, playerID ulid.ULID) (BootstrapResult, error)
	Enroll(ctx context.Context, playerID ulid.ULID) (EnrollResult, error)
	Verify(ctx context.Context, playerID ulid.ULID, code string) (VerifyResult, error)
	IsEnrolled(ctx context.Context, playerID ulid.ULID) (bool, error)
	ConsumeRecoveryCode(ctx context.Context, playerID ulid.ULID, code string) (ConsumeRecoveryResult, error)
	ClearTOTP(ctx context.Context, playerID ulid.ULID, clearedBy ClearReason) (ClearResult, error)
}

// Enrollment holds the one-time secrets presented to the player at enroll time.
type Enrollment struct {
	Secret          string   // base32
	ProvisioningURI string   // otpauth://totp/holomush-<game>:<player>?...
	RecoveryCodes   []string // 10 codes, "xxxx-xxxx-xxxx-xxxx"; printed once
}

// BootstrapResult is returned by Service.BootstrapEnroll.
type BootstrapResult struct {
	Enrollment      Enrollment
	AuditConsumedAt time.Time
	AuditPlayerID   ulid.ULID
	BootstrapKey    string
}

// EnrollResult is returned by Service.Enroll.
type EnrollResult struct {
	Enrollment      Enrollment
	AuditEnrolledAt time.Time
	AuditPlayerID   ulid.ULID
}

// VerifyOutcome is the result classification of a TOTP verification attempt.
type VerifyOutcome int

// VerifyOutcome values.
const (
	OutcomeOK          VerifyOutcome = iota
	OutcomeNotEnrolled               // player has no active TOTP enrollment
	OutcomeLocked                    // player is locked out
	OutcomeInvalidCode               // code did not match
	OutcomeCodeReuse                 // step already consumed (replay prevention)
)

// VerifyResult is returned by Service.Verify.
type VerifyResult struct {
	Outcome           VerifyOutcome
	LockedUntil       *time.Time // set when Outcome == OutcomeLocked OR a lockout transition just fired
	LockoutTransition bool       // true iff this Verify call transitioned NULL→locked
	AuditAt           time.Time  // = clock.Now()
}

// ConsumeRecoveryResult is returned by Service.ConsumeRecoveryCode.
type ConsumeRecoveryResult struct {
	RecoveryCodeID  ulid.ULID
	AuditConsumedAt time.Time
	AuditPlayerID   ulid.ULID
}

// ClearResult is returned by Service.ClearTOTP.
type ClearResult struct {
	ClearedBy      ClearReason
	AuditClearedAt time.Time
	AuditPlayerID  ulid.ULID
	WasEnrolled    bool // false if call was a no-op; callers should skip emit
}

// Repository provides PG persistence for TOTP data. Methods take ctx; if ctx
// carries an active pgx.Tx (via totp.txKey, set by Repository.InTransaction),
// methods participate in that txn. Otherwise they use the pool.
// Pattern matches internal/world/postgres/transactor.go.
type Repository interface {
	BootstrapClaim(ctx context.Context, key, playerID string, at time.Time) (claimed bool, err error)
	BootstrapEnrollAtomic(ctx context.Context, key, playerID string, rec EnrollmentRecord) error
	PlayerExists(ctx context.Context, playerID string) (bool, error)
	PlayerIDFromUsername(ctx context.Context, username string) (string, error)
	IsEnrolled(ctx context.Context, playerID string) (bool, error)
	InsertEnrollment(ctx context.Context, rec EnrollmentRecord) error
	LoadEnrollment(ctx context.Context, playerID string) (VerifyState, error)
	IncrementFailedAttempts(ctx context.Context, playerID string, lockoutThreshold int, lockoutDuration time.Duration, now time.Time) (postState VerifyState, err error)
	MarkVerified(ctx context.Context, playerID string, step int64, at time.Time) error
	ConsumeRecoveryCode(ctx context.Context, playerID, rawCode string, hasher RecoveryCodeHasher, at time.Time) (consumedID ulid.ULID, err error)
	ClearEnrollment(ctx context.Context, playerID string) (wasEnrolled bool, err error)
	InTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// EnrollmentRecord is the persisted form of a TOTP enrollment.
type EnrollmentRecord struct {
	PlayerID      string
	WrappedSecret []byte
	WrapKeyID     string
	EnrolledAt    time.Time
	RecoveryCodes []HashedRecoveryCode
}

// HashedRecoveryCode is a single hashed recovery code stored in the DB.
type HashedRecoveryCode struct {
	ID        ulid.ULID
	CodeHash  string
	CreatedAt time.Time
}

// VerifyState is the enrollment state loaded for a verification attempt.
type VerifyState struct {
	PlayerID       string
	WrappedSecret  []byte
	WrapKeyID      string
	LastUsedStep   *int64
	FailedAttempts int
	LockedUntil    *time.Time
}

// RecoveryCodeHasher is a subset of internal/auth.PasswordHasher used at
// verify time. Service uses the full PasswordHasher at enroll time.
type RecoveryCodeHasher interface {
	Verify(rawCode, encodedHash string) (bool, error)
}

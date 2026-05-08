// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/pquerna/otp/hotp"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/samber/oops"
)

const totpStepSeconds = 30

// errCodeReuseRollback is an unexported sentinel returned from inside
// Repository.InTransaction to force a ROLLBACK on replay detection
// (Service.Verify, OutcomeCodeReuse path). Never returned by Service to
// callers — the caller-facing surface is VerifyResult.Outcome.
var errCodeReuseRollback = errors.New("totp: rollback for code reuse")

// isNotEnrolledErr reports whether err carries the TOTP_NOT_ENROLLED
// oops code. Used in place of errors.Is(err, ErrNotEnrolled), which is
// unreliable: oops.OopsError.Is returns true for any OopsError target,
// so errors.Is would classify wrapped repo errors as "not enrolled".
// Matches the pattern in internal/auth/session_ownership.go.
func isNotEnrolledErr(err error) bool {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return false
	}
	return oopsErr.Code() == "TOTP_NOT_ENROLLED"
}

// Config bundles tunables.
type Config struct {
	GameID            string        // required
	LockoutThreshold  int           // default 5
	LockoutDuration   time.Duration // default 15min
	SkewSteps         int           // default 1
	RecoveryCodeCount int           // default 10
}

func (c *Config) applyDefaults() {
	if c.LockoutThreshold == 0 {
		c.LockoutThreshold = 5
	}
	if c.LockoutDuration == 0 {
		c.LockoutDuration = 15 * time.Minute
	}
	if c.SkewSteps == 0 {
		c.SkewSteps = 1
	}
	if c.RecoveryCodeCount == 0 {
		c.RecoveryCodeCount = recoveryCodesPerEnrollment
	}
}

// service is the production Service implementation. NO AuditPublisher
// field — emission is the caller's responsibility (R5 Option Y).
type service struct {
	cfg              Config
	repo             Repository
	kek              kek.Provider
	clock            Clock
	verifyHasher     RecoveryCodeHasher
	enrollmentHasher auth.PasswordHasher
}

// NewService constructs a Service. cfg.GameID is required; all other
// Config fields default if zero.
func NewService(
	cfg Config,
	repo Repository,
	kekProvider kek.Provider,
	clock Clock,
	enrollmentHasher auth.PasswordHasher,
) (Service, error) {
	if cfg.GameID == "" {
		return nil, oops.Code("TOTP_CFG_GAME_ID_REQUIRED").Errorf("Config.GameID is required")
	}
	cfg.applyDefaults()
	return &service{
		cfg:              cfg,
		repo:             repo,
		kek:              kekProvider,
		clock:            clock,
		verifyHasher:     enrollmentHasher, // same hasher serves both roles
		enrollmentHasher: enrollmentHasher,
	}, nil
}

func (s *service) IsEnrolled(ctx context.Context, playerID ulid.ULID) (bool, error) {
	enrolled, err := s.repo.IsEnrolled(ctx, playerID.String())
	if err != nil {
		return false, oops.With("player_id", playerID.String()).Wrap(err)
	}
	return enrolled, nil
}

// BootstrapEnroll: per spec §"CLI commands" / "bootstrap-enroll" + §"Bootstrap closure mechanism".
// Per R5 Option Y: PG-only; returns BootstrapResult for caller emission.
func (s *service) BootstrapEnroll(ctx context.Context, playerID ulid.ULID) (BootstrapResult, error) {
	exists, err := s.repo.PlayerExists(ctx, playerID.String())
	if err != nil {
		return BootstrapResult{}, oops.With("player_id", playerID.String()).Wrap(err)
	}
	if !exists {
		return BootstrapResult{}, oops.Code("TOTP_PLAYER_NOT_FOUND").
			With("player_id", playerID.String()).Errorf("player not found")
	}

	now := s.clock.Now().UTC()
	enr, rec, err := s.buildEnrollment(ctx, playerID.String(), now)
	if err != nil {
		return BootstrapResult{}, err
	}
	if err := s.repo.BootstrapEnrollAtomic(ctx, "totp_v1", playerID.String(), rec); err != nil {
		return BootstrapResult{}, oops.With("player_id", playerID.String()).Wrap(err) // includes ErrBootstrapAlreadyConsumed
	}
	return BootstrapResult{
		Enrollment:      enr,
		AuditConsumedAt: now,
		AuditPlayerID:   playerID,
		BootstrapKey:    "totp_v1",
	}, nil
}

// buildEnrollment generates a fresh secret + URI + recovery codes,
// wraps the secret with KEK, hashes the codes with Argon2id, and
// returns the public Enrollment + persistable EnrollmentRecord.
func (s *service) buildEnrollment(ctx context.Context, playerID string, now time.Time) (Enrollment, EnrollmentRecord, error) {
	secret, err := generateSecret()
	if err != nil {
		return Enrollment{}, EnrollmentRecord{}, err
	}
	wrapped, kekKeyID, err := s.kek.Wrap(ctx, []byte(secret))
	if err != nil {
		return Enrollment{}, EnrollmentRecord{}, oops.Code("TOTP_KEK_WRAP_FAILED").Wrap(err)
	}
	uri, err := buildProvisioningURI(playerID, s.cfg.GameID, secret) // playerID as account label; see spec
	if err != nil {
		return Enrollment{}, EnrollmentRecord{}, err
	}
	codes, err := generateRecoveryCodes(s.cfg.RecoveryCodeCount)
	if err != nil {
		return Enrollment{}, EnrollmentRecord{}, err
	}
	hashed := make([]HashedRecoveryCode, len(codes))
	for i, c := range codes {
		h, hErr := s.enrollmentHasher.Hash(c)
		if hErr != nil {
			return Enrollment{}, EnrollmentRecord{}, oops.Code("TOTP_RECOVERY_HASH_FAILED").Wrap(hErr)
		}
		hashed[i] = HashedRecoveryCode{ID: idgen.New(), CodeHash: h, CreatedAt: now}
	}
	return Enrollment{Secret: secret, ProvisioningURI: uri, RecoveryCodes: codes},
		EnrollmentRecord{
			PlayerID: playerID, WrappedSecret: wrapped, WrapKeyID: kekKeyID,
			EnrolledAt: now, RecoveryCodes: hashed,
		}, nil
}

// Enroll self-enrolls a player in TOTP. Returns ErrAlreadyEnrolled if the
// player already has an active enrollment.
func (s *service) Enroll(ctx context.Context, playerID ulid.ULID) (EnrollResult, error) {
	enrolled, err := s.repo.IsEnrolled(ctx, playerID.String())
	if err != nil {
		return EnrollResult{}, oops.With("player_id", playerID.String()).Wrap(err)
	}
	if enrolled {
		return EnrollResult{}, ErrAlreadyEnrolled
	}
	now := s.clock.Now().UTC()
	enr, rec, err := s.buildEnrollment(ctx, playerID.String(), now)
	if err != nil {
		return EnrollResult{}, err
	}
	if err := s.repo.InsertEnrollment(ctx, rec); err != nil {
		return EnrollResult{}, oops.With("player_id", playerID.String()).Wrap(err)
	}
	return EnrollResult{Enrollment: enr, AuditEnrolledAt: now, AuditPlayerID: playerID}, nil
}

// Verify implements Service.Verify with replay defense (INV-A3), lockout (INV-A4),
// success counter reset (INV-A5), skew window (INV-A12), and constant-time comparison.
func (s *service) Verify(ctx context.Context, playerID ulid.ULID, code string) (VerifyResult, error) {
	now := s.clock.Now().UTC()
	var result VerifyResult
	result.AuditAt = now

	txErr := s.repo.InTransaction(ctx, func(txCtx context.Context) error {
		state, err := s.repo.LoadEnrollment(txCtx, playerID.String())
		if err != nil {
			// Compare oops code, NOT errors.Is — oops.OopsError.Is returns
			// true for any OopsError target, so errors.Is(err, ErrNotEnrolled)
			// would silently classify ALL DB errors (including the wrapped
			// TOTP_REPO_LOAD_ENROLLMENT path) as "not enrolled". See
			// internal/auth/session_ownership.go::isSessionNotFound for the
			// canonical precedent of this pattern.
			if isNotEnrolledErr(err) {
				result.Outcome = OutcomeNotEnrolled
				return nil
			}
			return oops.With("player_id", playerID.String()).Wrap(err)
		}
		if state.LockedUntil != nil && state.LockedUntil.After(now) {
			result.Outcome = OutcomeLocked
			result.LockedUntil = state.LockedUntil
			return nil
		}
		secret, err := s.kek.Unwrap(txCtx, state.WrappedSecret, state.WrapKeyID)
		if err != nil {
			return oops.Code("TOTP_KEK_UNWRAP_FAILED").Wrap(err)
		}
		step := now.Unix() / totpStepSeconds
		matchedStep := int64(-1)
		for offset := -s.cfg.SkewSteps; offset <= s.cfg.SkewSteps; offset++ {
			tryStep := step + int64(offset)
			expected, genErr := hotp.GenerateCode(string(secret), uint64(tryStep)) //nolint:gosec // G115: tryStep is derived from a UNIX timestamp divided by 30; always positive in practice and cannot overflow uint64
			if genErr != nil {
				continue
			}
			if subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
				matchedStep = tryStep
				// do NOT break — iterate all steps to avoid timing-leak
			}
		}
		if matchedStep == -1 {
			post, incErr := s.repo.IncrementFailedAttempts(txCtx,
				playerID.String(), s.cfg.LockoutThreshold, s.cfg.LockoutDuration, now)
			if incErr != nil {
				return oops.With("player_id", playerID.String()).Wrap(incErr)
			}
			result.Outcome = OutcomeInvalidCode
			result.LockedUntil = post.LockedUntil
			result.LockoutTransition = (state.LockedUntil == nil &&
				post.LockedUntil != nil && post.LockedUntil.After(now))
			return nil
		}
		if state.LastUsedStep != nil && matchedStep <= *state.LastUsedStep {
			result.Outcome = OutcomeCodeReuse
			return errCodeReuseRollback // typed sentinel: triggers ROLLBACK without surfacing as a real error
		}
		if err := s.repo.MarkVerified(txCtx, playerID.String(), matchedStep, now); err != nil {
			return oops.With("player_id", playerID.String()).Wrap(err)
		}
		result.Outcome = OutcomeOK
		return nil
	})
	if errors.Is(txErr, errCodeReuseRollback) {
		return result, nil
	}
	if txErr != nil {
		return VerifyResult{}, oops.With("player_id", playerID.String()).Wrap(txErr)
	}
	return result, nil
}

// ConsumeRecoveryCode delegates to the repo's atomic single-use update;
// surfaces ErrInvalidRecoveryCode unchanged so callers can branch on it.
func (s *service) ConsumeRecoveryCode(ctx context.Context, playerID ulid.ULID, code string) (ConsumeRecoveryResult, error) {
	now := s.clock.Now().UTC()
	id, err := s.repo.ConsumeRecoveryCode(ctx, playerID.String(), code, s.verifyHasher, now)
	if err != nil {
		return ConsumeRecoveryResult{}, oops.With("player_id", playerID.String()).Wrap(err)
	}
	return ConsumeRecoveryResult{
		RecoveryCodeID:  id,
		AuditConsumedAt: now,
		AuditPlayerID:   playerID,
	}, nil
}

// RecoverAndClear is the atomic break-glass flow used by the
// `holomush admin totp recover` CLI: consume one recovery code AND clear
// the player's TOTP enrollment in a single transaction. Either both
// commit or both roll back.
func (s *service) RecoverAndClear(ctx context.Context, playerID ulid.ULID, code string) (RecoverAndClearResult, error) {
	now := s.clock.Now().UTC()
	consumedID, wasEnrolled, err := s.repo.RecoverAndClearAtomic(
		ctx, playerID.String(), code, s.verifyHasher, now,
	)
	if err != nil {
		return RecoverAndClearResult{}, oops.With("player_id", playerID.String()).Wrap(err)
	}
	return RecoverAndClearResult{
		RecoveryCodeID:  consumedID,
		WasEnrolled:     wasEnrolled,
		AuditConsumedAt: now,
		AuditClearedAt:  now,
		AuditPlayerID:   playerID,
	}, nil
}

// ClearTOTP deletes the player's enrollment + active recovery codes
// (per spec §"ClearTOTP"). MUST NOT touch crypto_bootstrap_state — INV-A8.
func (s *service) ClearTOTP(ctx context.Context, playerID ulid.ULID, clearedBy ClearReason) (ClearResult, error) {
	wasEnrolled, err := s.repo.ClearEnrollment(ctx, playerID.String())
	if err != nil {
		return ClearResult{}, oops.With("player_id", playerID.String()).Wrap(err)
	}
	now := s.clock.Now().UTC()
	return ClearResult{
		ClearedBy:      clearedBy,
		AuditClearedAt: now,
		AuditPlayerID:  playerID,
		WasEnrolled:    wasEnrolled,
	}, nil
}

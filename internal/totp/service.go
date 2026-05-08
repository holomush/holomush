// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/samber/oops"
)

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

// Enroll lands in T8.
func (s *service) Enroll(_ context.Context, _ ulid.ULID) (EnrollResult, error) {
	panic("not yet implemented: Enroll lands in T8")
}

// Verify lands in T9.
func (s *service) Verify(_ context.Context, _ ulid.ULID, _ string) (VerifyResult, error) {
	panic("not yet implemented: Verify lands in T9")
}

// ConsumeRecoveryCode lands in T10.
func (s *service) ConsumeRecoveryCode(_ context.Context, _ ulid.ULID, _ string) (ConsumeRecoveryResult, error) {
	panic("not yet implemented: ConsumeRecoveryCode lands in T10")
}

// ClearTOTP lands in T10.
func (s *service) ClearTOTP(_ context.Context, _ ulid.ULID, _ ClearReason) (ClearResult, error) {
	panic("not yet implemented: ClearTOTP lands in T10")
}

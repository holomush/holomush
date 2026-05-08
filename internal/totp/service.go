// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
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

// BootstrapEnroll lands in T7.
func (s *service) BootstrapEnroll(_ context.Context, _ ulid.ULID) (BootstrapResult, error) {
	panic("not yet implemented: BootstrapEnroll lands in T7")
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

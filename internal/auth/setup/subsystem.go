// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package setup provides the auth subsystem lifecycle wrapper.
package setup

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	authpostgres "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/store"
)

// PoolProvider provides a database connection pool. Implemented by the
// database subsystem without requiring a direct import.
type PoolProvider interface {
	Pool() *pgxpool.Pool
}

// AuthSubsystemConfig configures the auth subsystem.
type AuthSubsystemConfig struct {
	DB PoolProvider

	// MaxSessionsPerPlayer caps concurrent authenticated PlayerSessions per
	// player. A value <= 0 disables enforcement. On login exceeding the cap,
	// the oldest active PlayerSession is evicted before the new session is
	// persisted; the sessions.player_session_id FK cascade then removes the
	// evicted session's game sessions and terminates their Subscribe streams.
	MaxSessionsPerPlayer int
}

// AuthSubsystem manages authentication services and repositories.
type AuthSubsystem struct {
	cfg                AuthSubsystemConfig
	playerRepo         *authpostgres.PlayerRepository
	resetRepo          *authpostgres.PasswordResetRepository
	playerSessionStore *store.PostgresPlayerSessionStore
	hasher             auth.PasswordHasher
	authService        *auth.Service
	resetService       *auth.PasswordResetService
}

// NewAuthSubsystem creates an AuthSubsystem configured with cfg.
// It does not allocate live resources; Start must be called to initialize repositories, stores, the password hasher, and services.
func NewAuthSubsystem(cfg AuthSubsystemConfig) *AuthSubsystem {
	return &AuthSubsystem{cfg: cfg}
}

// ID returns SubsystemAuth.
func (s *AuthSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemAuth }

// DependsOn returns [SubsystemDatabase].
func (s *AuthSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}
}

// Start creates auth repositories, hasher, and services.
// Start is idempotent: if the subsystem is already started, it returns nil
// immediately. This allows the auth subsystem to be pre-started in core
// boot when admin handler construction needs Hasher() / AuthService()
// before the orchestrator drives StartAll. Mirrors store.DatabaseSubsystem.Start.
// codecov:ignore — tested by integration and E2E tests
func (s *AuthSubsystem) Start(ctx context.Context) error {
	// Idempotency guard: only short-circuit if BOTH services are constructed.
	// If a previous Start partially failed (e.g., resetSvc construction
	// errored after authSvc was assigned), s.authService would be non-nil
	// and the next Start would return nil while s.resetService remained
	// nil — locking the subsystem into a partially-initialized state.
	// Defer all field assignments to the end so partial failure leaves
	// the subsystem in its initial zero state and a retry runs cleanly.
	if s.authService != nil && s.resetService != nil {
		return nil // already started
	}
	pool := s.cfg.DB.Pool()

	playerRepo := authpostgres.NewPlayerRepository(pool)
	resetRepo := authpostgres.NewPasswordResetRepository(pool)
	playerSessionStore := store.NewPostgresPlayerSessionStore(pool)
	hasher := auth.NewArgon2idHasher()

	authSvc, err := auth.NewAuthServiceWithLogger(playerRepo, playerSessionStore, hasher, slog.Default())
	if err != nil {
		return oops.Code("AUTH_SETUP_FAILED").Wrap(err)
	}
	authSvc.SetMaxSessionsPerPlayer(s.cfg.MaxSessionsPerPlayer)

	resetSvc, err := auth.NewPasswordResetServiceWithLogger(playerRepo, resetRepo, playerSessionStore, hasher, slog.Default())
	if err != nil {
		return oops.Code("AUTH_SETUP_FAILED").Wrap(err)
	}

	// Commit all fields atomically only after every dependency built cleanly.
	s.playerRepo = playerRepo
	s.resetRepo = resetRepo
	s.playerSessionStore = playerSessionStore
	s.hasher = hasher
	s.authService = authSvc
	s.resetService = resetSvc

	slog.InfoContext(ctx, "auth subsystem started")
	return nil
}

// Stop is a no-op — auth services are stateless after init.
// codecov:ignore — tested by integration and E2E tests
func (s *AuthSubsystem) Stop(_ context.Context) error { return nil }

// PlayerRepo returns the player repository. Panics if called before Start().
func (s *AuthSubsystem) PlayerRepo() auth.PlayerRepository {
	if s.playerRepo == nil {
		panic("auth/setup: PlayerRepo() called before Start()")
	}
	return s.playerRepo
}

// ResetRepo returns the password reset repository. Panics if called before Start().
func (s *AuthSubsystem) ResetRepo() *authpostgres.PasswordResetRepository {
	if s.resetRepo == nil {
		panic("auth/setup: ResetRepo() called before Start()")
	}
	return s.resetRepo
}

// PlayerSessionStore returns the player session store. Panics if called before Start().
func (s *AuthSubsystem) PlayerSessionStore() *store.PostgresPlayerSessionStore {
	if s.playerSessionStore == nil {
		panic("auth/setup: PlayerSessionStore() called before Start()")
	}
	return s.playerSessionStore
}

// Hasher returns the password hasher. Panics if called before Start().
func (s *AuthSubsystem) Hasher() auth.PasswordHasher {
	if s.hasher == nil {
		panic("auth/setup: Hasher() called before Start()")
	}
	return s.hasher
}

// AuthService returns the authentication service. Panics if called before Start().
func (s *AuthSubsystem) AuthService() *auth.Service {
	if s.authService == nil {
		panic("auth/setup: AuthService() called before Start()")
	}
	return s.authService
}

// ResetService returns the password reset service. Panics if called before Start().
func (s *AuthSubsystem) ResetService() *auth.PasswordResetService {
	if s.resetService == nil {
		panic("auth/setup: ResetService() called before Start()")
	}
	return s.resetService
}

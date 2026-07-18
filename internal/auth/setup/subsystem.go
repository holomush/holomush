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

// Prepare creates auth repositories, hasher, and services — construction
// only, no external surface (D-13.3 row 4).
// Prepare is idempotent: if the subsystem is already prepared, it returns nil
// immediately.
// codecov:ignore — tested by integration and E2E tests
func (s *AuthSubsystem) Prepare(ctx context.Context) error {
	// Idempotency guard: only short-circuit if BOTH services are constructed.
	// If a previous Prepare partially failed (e.g., resetSvc construction
	// errored after authSvc was assigned), s.authService would be non-nil
	// and the next Prepare would return nil while s.resetService remained
	// nil — locking the subsystem into a partially-initialized state.
	// Defer all field assignments to the end so partial failure leaves
	// the subsystem in its initial zero state and a retry runs cleanly.
	if s.authService != nil && s.resetService != nil {
		return nil // already prepared
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

	slog.InfoContext(ctx, "auth subsystem prepared")
	return nil
}

// Activate is a no-op — auth serves nothing of its own; its services are
// invoked synchronously by callers (D-13.3 row 4).
func (s *AuthSubsystem) Activate(_ context.Context) error { return nil }

// Stop is a no-op — auth services are stateless after init.
// codecov:ignore — tested by integration and E2E tests
func (s *AuthSubsystem) Stop(_ context.Context) error { return nil }

// PlayerRepo returns the player repository. Panics if called before Prepare().
func (s *AuthSubsystem) PlayerRepo() auth.PlayerRepository {
	if s.playerRepo == nil {
		panic("auth/setup: PlayerRepo() called before Prepare()")
	}
	return s.playerRepo
}

// ResetRepo returns the password reset repository. Panics if called before Prepare().
func (s *AuthSubsystem) ResetRepo() *authpostgres.PasswordResetRepository {
	if s.resetRepo == nil {
		panic("auth/setup: ResetRepo() called before Prepare()")
	}
	return s.resetRepo
}

// PlayerSessionStore returns the player session store. Panics if called before Prepare().
func (s *AuthSubsystem) PlayerSessionStore() *store.PostgresPlayerSessionStore {
	if s.playerSessionStore == nil {
		panic("auth/setup: PlayerSessionStore() called before Prepare()")
	}
	return s.playerSessionStore
}

// Hasher returns the password hasher. Panics if called before Prepare().
func (s *AuthSubsystem) Hasher() auth.PasswordHasher {
	if s.hasher == nil {
		panic("auth/setup: Hasher() called before Prepare()")
	}
	return s.hasher
}

// AuthService returns the authentication service. Panics if called before Prepare().
func (s *AuthSubsystem) AuthService() *auth.Service {
	if s.authService == nil {
		panic("auth/setup: AuthService() called before Prepare()")
	}
	return s.authService
}

// ResetService returns the password reset service. Panics if called before Prepare().
func (s *AuthSubsystem) ResetService() *auth.PasswordResetService {
	if s.resetService == nil {
		panic("auth/setup: ResetService() called before Prepare()")
	}
	return s.resetService
}

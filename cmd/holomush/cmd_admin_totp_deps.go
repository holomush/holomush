// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/totp"
)

// adminTOTPDeps bundles dependencies the admin totp CLIs need.
// Cleanup MUST be invoked by the caller via the returned func to close
// the PG pool.
//
//nolint:unused // forward-declared for T13/T14 admin CLI handlers (Phase 5 sub-epic A)
type adminTOTPDeps struct {
	pool     *pgxpool.Pool
	totpSvc  totp.Service
	totpRepo totp.Repository
	authSvc  *auth.Service // for `enroll` CLI's ValidateCredentials
	gameID   string
}

const (
	envKEKFile       = "HOLOMUSH_KEK_FILE"       //nolint:unused // forward-declared for T13/T14 admin CLI handlers
	envKEKPassphrase = "HOLOMUSH_KEK_PASSPHRASE" //nolint:gosec,unused // G101 false positive: env var name, not a credential value; forward-declared for T13/T14
	envGameID        = "HOLOMUSH_GAME_ID"        //nolint:unused // forward-declared for T13/T14 admin CLI handlers
)

// buildAdminTOTPDeps assembles the deps used by `holomush admin totp` CLIs.
// First production KEK wiring lives here (Phase 5 sub-epic A T12); when
// the core server wires KEK into runCoreWithDeps, both paths MUST use the
// same file-source pattern so wrapped TOTP secrets remain interoperable.
//
//nolint:unused // forward-declared for T13/T14 admin CLI handlers (Phase 5 sub-epic A)
func buildAdminTOTPDeps(ctx context.Context) (*adminTOTPDeps, func(), error) {
	url, err := getDatabaseURL()
	if err != nil {
		return nil, nil, oops.Wrap(err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, nil, oops.Code("ADMIN_PG_POOL_FAILED").Wrap(err)
	}

	gameID := os.Getenv(envGameID)
	if gameID == "" {
		pool.Close()
		return nil, nil, oops.Code("ADMIN_GAME_ID_MISSING").
			Errorf("environment variable %s is required", envGameID)
	}

	kekProvider, err := buildKEKProviderFromConfig(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}

	playerRepo := authpg.NewPlayerRepository(pool)
	sessionStore := store.NewPostgresPlayerSessionStore(pool)
	hasher := auth.NewArgon2idHasher()
	authSvc, err := auth.NewAuthService(playerRepo, sessionStore, hasher)
	if err != nil {
		pool.Close()
		return nil, nil, oops.Code("ADMIN_AUTH_SERVICE_FAILED").Wrap(err)
	}

	totpRepo := totp.NewRepository(pool)
	totpSvc, err := totp.NewService(
		totp.Config{GameID: gameID},
		totpRepo, kekProvider, totp.NewRealClock(), hasher,
	)
	if err != nil {
		pool.Close()
		return nil, nil, oops.Code("ADMIN_TOTP_SERVICE_FAILED").Wrap(err)
	}

	cleanup := func() { pool.Close() }
	return &adminTOTPDeps{
		pool:     pool,
		totpSvc:  totpSvc,
		totpRepo: totpRepo,
		authSvc:  authSvc,
		gameID:   gameID,
	}, cleanup, nil
}

// buildKEKProviderFromConfig constructs the production KEK provider from
// HOLOMUSH_KEK_FILE + HOLOMUSH_KEK_PASSPHRASE env vars. File-source is
// the production-grade path (env-source is dev/test only — see
// kek.NewEnvSource prodMode rejection at internal/eventbus/crypto/kek/source_env.go).
//
// When core.go wires KEK into runCoreWithDeps in a future bead, it MUST
// use the same construction pattern so wrapped DEKs and TOTP secrets
// remain interoperable across server and CLI.
//
//nolint:unused // forward-declared for T13/T14 admin CLI handlers (Phase 5 sub-epic A)
func buildKEKProviderFromConfig(ctx context.Context, pool *pgxpool.Pool) (kek.Provider, error) {
	keyFile := os.Getenv(envKEKFile)
	if keyFile == "" {
		return nil, oops.Code("ADMIN_KEK_FILE_MISSING").
			With("env_var", envKEKFile).
			Errorf("environment variable %s is required", envKEKFile)
	}
	passphrase := os.Getenv(envKEKPassphrase)
	if passphrase == "" {
		return nil, oops.Code("ADMIN_KEK_PASSPHRASE_MISSING").
			With("env_var", envKEKPassphrase).
			Errorf("environment variable %s is required", envKEKPassphrase)
	}
	source, err := kek.NewFileSource(keyFile, func(_ context.Context) ([]byte, error) {
		return []byte(passphrase), nil
	})
	if err != nil {
		return nil, oops.Code("ADMIN_KEK_FILE_SOURCE_FAILED").Wrap(err)
	}
	provider, err := kek.NewLocalAEADProvider(ctx, source, pool)
	if err != nil {
		return nil, oops.Code("ADMIN_KEK_PROVIDER_FAILED").Wrap(err)
	}
	return provider, nil
}

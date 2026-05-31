// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package sessiontest provides a Postgres-backed session.Store helper for
// unit and integration tests. It is the deliberate exception to the repo
// convention that SharedPostgres-using tests carry //go:build integration:
// unit tests in internal/grpc/, internal/grpc/focus/,
// internal/command/handlers/, and internal/auth/ exercise session-touching
// handler logic and require a real session.Store. Per the holomush-9mxr
// design spec, this package replaces the deleted in-memory session store.
//
// Docker is required at test runtime. Developers without Docker will see
// testcontainers container-start errors, not compile failures — the
// helper imports compile fine without Docker.
package sessiontest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/pgnanos"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

// NewStore returns a session.Store backed by a fresh Postgres database
// on the shared test container. The database is dropped via t.Cleanup
// when the test ends (registered by testutil.FreshDatabase). Each call
// returns a fully isolated store.
func NewStore(t *testing.T) session.Store {
	t.Helper()
	s, _ := NewStoreWithPool(t)
	return s
}

// NewStoreWithPool returns a session.Store and the underlying *pgxpool.Pool
// backed by a fresh Postgres database on the shared test container.
// The pool is closed and the database is dropped via t.Cleanup.
//
// Use the pool to seed FK-prerequisite rows (e.g. players, player_sessions)
// before calling Set with a non-zero PlayerSessionID.
func NewStoreWithPool(t *testing.T) (session.Store, *pgxpool.Pool) {
	t.Helper()

	env := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, env)

	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err, "sessiontest.NewStoreWithPool: connect to fresh test database")
	t.Cleanup(pool.Close)

	return store.NewPostgresSessionStore(pool), pool
}

// NewPlayerSession creates an *auth.PlayerSession suitable for seeding FK
// prerequisites in integration tests. The PlayerID and ID are fresh ULIDs;
// TokenHash is a deterministic placeholder derived from the ID.
func NewPlayerSession() *auth.PlayerSession {
	playerID := ulid.Make()
	ps := &auth.PlayerSession{
		ID:        ulid.Make(),
		PlayerID:  playerID,
		TokenHash: "placeholder-test-token-hash-" + playerID.String(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	return ps
}

// NewActiveSession constructs a *session.Info in StatusActive state that
// references the given PlayerSession for FK seeding. The session ID is a
// fresh ULID string; CharacterID is a fresh random ULID; ExpiresAt is set
// one hour in the future so the session is not expired.
func NewActiveSession(ps *auth.PlayerSession) *session.Info {
	charID := ulid.Make()
	locID := ulid.Make()
	expiresAt := time.Now().Add(time.Hour)
	return &session.Info{
		ID:              ulid.Make().String(),
		CharacterID:     charID,
		PlayerID:        ps.PlayerID,
		PlayerSessionID: ps.ID,
		CharacterName:   "TestChar-" + charID.String(),
		LocationID:      locID,
		Status:          session.StatusActive,
		GridPresent:     true,
		ExpiresAt:       &expiresAt,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}

// SeedPlayerSession inserts the minimal FK chain required to store a game
// session whose PlayerSessionID references ps.ID:
//
//  1. A player row for ps.PlayerID (username derived from the ULID so it is
//     unique per test; password_hash is a placeholder).
//  2. A player_sessions row for ps.ID / ps.PlayerID / ps.TokenHash.
//
// Call this after NewStoreWithPool and before any Set that carries a non-zero
// PlayerSessionID. Safe to call multiple times for different PlayerSession
// values within the same test as long as ps.PlayerID differs (or the same
// player is re-used — the INSERT is ON CONFLICT DO NOTHING for players).
func SeedPlayerSession(t *testing.T, pool *pgxpool.Pool, ps *auth.PlayerSession) {
	t.Helper()
	require.NotNil(t, ps, "sessiontest.SeedPlayerSession: ps must not be nil")
	ctx := context.Background()

	// Insert the player row. ON CONFLICT DO NOTHING so the same player can be
	// seeded multiple times (e.g. two PlayerSessions for one player).
	_, err := pool.Exec(
		ctx,
		`INSERT INTO players (id, username, password_hash)
		 VALUES ($1, $2, 'x')
		 ON CONFLICT (id) DO NOTHING`,
		ps.PlayerID.String(),
		"test-player-"+ps.PlayerID.String(),
	)
	require.NoError(t, err, "sessiontest.SeedPlayerSession: insert player")

	expiresAt := ps.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(24 * time.Hour)
	}
	tokenHash := ps.TokenHash
	if tokenHash == "" {
		tokenHash = "placeholder-" + ps.ID.String()
	}

	_, err = pool.Exec(
		ctx,
		`INSERT INTO player_sessions (id, player_id, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO NOTHING`,
		ps.ID.String(),
		ps.PlayerID.String(),
		tokenHash,
		// player_sessions.expires_at became BIGINT-ns in migration 000041; encode
		// via pgnanos.From like the production repo (raw time.Time would fail to encode).
		pgnanos.From(expiresAt),
	)
	require.NoError(t, err, "sessiontest.SeedPlayerSession: insert player_session")
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package sessiontest provides a Postgres-backed session.Store helper for
// unit and integration tests. It is the deliberate exception to the repo
// convention that SharedPostgres-using tests carry //go:build integration:
// unit tests in internal/grpc/, internal/grpc/focus/,
// internal/command/handlers/, and internal/auth/ exercise session-touching
// handler logic and require a real session.Store. Per the holomush-9mxr
// design spec, this package replaces the deleted internal/session.MemStore.
//
// Docker is required at test runtime. Developers without Docker will see
// testcontainers container-start errors, not compile failures — the
// helper imports compile fine without Docker.
package sessiontest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

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

	env := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, env)

	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err, "sessiontest.NewStore: connect to fresh test database")
	t.Cleanup(pool.Close)

	return store.NewPostgresSessionStore(pool)
}

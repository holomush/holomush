//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package session_test contains integration tests for the persistent
// game session lifecycle: reconnect/replay, command history persistence,
// and reaper-driven TTL expiration.
//
// These tests intentionally exercise real components end-to-end (real
// PostgreSQL via testcontainers, real gRPC over a loopback listener, real
// reaper goroutine) so they catch driver-level behavior, schema drift, and
// race conditions that unit tests cannot.
package session_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"

	authpg "github.com/holomush/holomush/internal/auth/postgres"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/store"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

func TestSessionPersistence(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Session Persistence Integration Suite")
}

// suiteEnv holds the resources shared across all specs in the suite.
// The Postgres container, pool, and stateless repositories are created
// once in BeforeSuite. Per-spec state (engine, gRPC server, reaper) is
// constructed in BeforeEach against the shared pool.
type suiteEnv struct {
	ctx       context.Context
	container testcontainers.Container
	pool      *pgxpool.Pool

	eventStore         *store.PostgresEventStore
	sessionStore       *store.PostgresSessionStore
	playerSessionStore *store.PostgresPlayerSessionStore
	playerRepo         *authpg.PlayerRepository
	charRepo           *bootstrapsetup.CharRepoAdapter
	locRepo            *worldpg.LocationRepository
}

var env *suiteEnv

var _ = BeforeSuite(func() {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	Expect(err).NotTo(HaveOccurred())

	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	Expect(err).NotTo(HaveOccurred())
	Expect(migrator.Up()).To(Succeed())
	_ = migrator.Close()

	eventStore, err := store.NewPostgresEventStore(ctx, pgEnv.ConnStr)
	Expect(err).NotTo(HaveOccurred())

	pool := eventStore.Pool()
	Expect(pool).NotTo(BeNil())

	env = &suiteEnv{
		ctx:                ctx,
		container:          pgEnv.Container,
		pool:               pool,
		eventStore:         eventStore,
		sessionStore:       store.NewPostgresSessionStore(pool),
		playerSessionStore: store.NewPostgresPlayerSessionStore(pool),
		playerRepo:         authpg.NewPlayerRepository(pool),
		charRepo:           bootstrapsetup.NewCharRepoAdapter(pool, worldpg.NewCharacterRepository(pool)),
		locRepo:            worldpg.NewLocationRepository(pool),
	}
})

var _ = AfterSuite(func() {
	if env == nil {
		return
	}
	if env.eventStore != nil {
		env.eventStore.Close()
	}
	if env.container != nil {
		_ = env.container.Terminate(env.ctx)
	}
})

// cleanupTestData removes all test data between specs in FK-safe order.
// Order: dependents → parents to honor FK constraints.
func cleanupTestData(ctx context.Context, pool *pgxpool.Pool) {
	tables := []string{
		"session_connections",
		"player_sessions",
		"sessions",
		"events",
		"characters",
		"exits",
		"locations",
		"players",
	}
	for _, table := range tables {
		_, err := pool.Exec(ctx, "DELETE FROM "+table)
		Expect(err).NotTo(HaveOccurred(), "failed to clean table %s", table)
	}
}

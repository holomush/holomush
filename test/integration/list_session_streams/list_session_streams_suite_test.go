// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package list_session_streams_test contains integration tests for
// CoreService.ListSessionStreams, exercising the full handler flow against
// real PostgreSQL (via testcontainers) and a real PostgresSessionStore.
package list_session_streams_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

var suiteT *testing.T

// TestListSessionStreams is the Ginkgo entry point for the suite.
func TestListSessionStreams(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "ListSessionStreams Integration Suite")
}

// suiteEnv holds resources shared across all specs in the suite. The
// PostgreSQL container is started once per test binary (via
// testutil.SharedPostgres); each suite gets its own fresh database from a
// pre-migrated template (via testutil.FreshDatabase).
type suiteEnv struct {
	ctx                context.Context
	pool               *pgxpool.Pool
	eventStore         *store.PostgresEventStore
	sessionStore       *store.PostgresSessionStore
	playerSessionStore *store.PostgresPlayerSessionStore
}

var env *suiteEnv

var _ = BeforeSuite(func() {
	ctx := context.Background()

	shared := testutil.SharedPostgres(suiteT)
	connStr := testutil.FreshDatabase(suiteT, shared)

	eventStore, err := store.NewPostgresEventStore(ctx, connStr)
	Expect(err).NotTo(HaveOccurred())

	pool := eventStore.Pool()
	Expect(pool).NotTo(BeNil())

	env = &suiteEnv{
		ctx:                ctx,
		pool:               pool,
		eventStore:         eventStore,
		sessionStore:       store.NewPostgresSessionStore(pool),
		playerSessionStore: store.NewPostgresPlayerSessionStore(pool),
	}
})

var _ = AfterSuite(func() {
	if env == nil {
		return
	}
	if env.eventStore != nil {
		env.eventStore.Close()
	}
})

// cleanupTestData removes all sessions and events between specs.
func cleanupTestData(ctx context.Context, pool *pgxpool.Pool) {
	tables := []string{
		"session_connections",
		"sessions",
		"player_sessions",
		"players",
		"events",
	}
	for _, table := range tables {
		_, err := pool.Exec(ctx, "DELETE FROM "+table)
		Expect(err).NotTo(HaveOccurred(), "failed to clean table %s", table)
	}
}

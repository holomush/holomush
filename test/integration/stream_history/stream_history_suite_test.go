// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package stream_history_test contains integration tests for
// CoreService.QueryStreamHistory, exercising the full handler flow against
// real PostgreSQL (via testcontainers), a real PostgresSessionStore, and a
// real PostgresEventStore.
package stream_history_test

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

// TestStreamHistory is the Ginkgo entry point for the suite.
func TestStreamHistory(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "QueryStreamHistory Integration Suite")
}

// suiteEnv holds resources shared across all specs in the suite. The
// PostgreSQL container is started once per test binary (via
// testutil.SharedPostgres); each suite gets its own fresh database from a
// pre-migrated template (via testutil.FreshDatabase) so schema work in
// other suites cannot leak into this one.
type suiteEnv struct {
	ctx          context.Context
	pool         *pgxpool.Pool
	eventStore   *store.PostgresEventStore
	sessionStore *store.PostgresSessionStore
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
		ctx:          ctx,
		pool:         pool,
		eventStore:   eventStore,
		sessionStore: store.NewPostgresSessionStore(pool),
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

// cleanupTestData removes all events and sessions between specs. Sessions
// has no FK to events, and events has no FK to sessions, so order does not
// matter — but we keep a deterministic ordering for clarity.
func cleanupTestData(ctx context.Context, pool *pgxpool.Pool) {
	tables := []string{
		"session_connections",
		"sessions",
		"events",
	}
	for _, table := range tables {
		_, err := pool.Exec(ctx, "DELETE FROM "+table)
		Expect(err).NotTo(HaveOccurred(), "failed to clean table %s", table)
	}
}

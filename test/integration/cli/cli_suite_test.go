// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package cli_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestCLI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CLI Integration Suite")
}

// testEnv holds all resources needed for CLI integration tests.
type testEnv struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	container testcontainers.Container
	connStr   string
}

var env *testEnv

var _ = BeforeSuite(func() {
	var err error
	env, err = setupCLITestEnv()
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	if env != nil {
		env.cleanup()
	}
})

func setupCLITestEnv() (*testEnv, error) {
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("holomush_test"),
		postgres.WithUsername("holomush"),
		postgres.WithPassword("holomush"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		return nil, err
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, err
	}

	return &testEnv{
		ctx:       ctx,
		pool:      pool,
		container: container,
		connStr:   connStr,
	}, nil
}

func (e *testEnv) cleanup() {
	if e.pool != nil {
		e.pool.Close()
	}
	if e.container != nil {
		_ = e.container.Terminate(e.ctx)
	}
}

// cleanupDatabase removes all data from the test database.
func cleanupDatabase(ctx context.Context, pool *pgxpool.Pool) {
	// Drop all tables to start fresh (migrations will recreate them)
	tables := []string{
		"scene_participants",
		"sessions",
		"characters",
		"exits",
		"objects",
		"scenes",
		"locations",
		"players",
		"events",
	}
	for _, table := range tables {
		_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS "+table+" CASCADE")
	}
}

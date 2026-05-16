// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/holomush/holomush/internal/bootstrap"
	"github.com/holomush/holomush/internal/store"
)

// startPostgresContainer starts a PostgreSQL container for testing.
// Uses postgres.BasicWaitStrategies() which combines the log wait with
// wait.ForListeningPort to avoid the Docker port-mapping race documented
// in holomush-bmcq. Called from both Ginkgo specs (passing suiteT) and
// plain testing.T helpers (e.g., bootstrap_orphan_test.go).
func startPostgresContainer(t *testing.T) (string, func()) {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(
		ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("startPostgresContainer: run: %v", err)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("startPostgresContainer: connection string: %v", err)
	}

	cleanup := func() {
		_ = container.Terminate(ctx)
	}

	return connStr, cleanup
}

// schemaTableExists checks if the schema_migrations table exists.
func schemaTableExists(ctx context.Context, connStr string) (bool, error) {
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return false, err
	}
	defer conn.Close(ctx) //nolint:errcheck // best-effort cleanup

	var exists bool
	query := `SELECT EXISTS (
		SELECT FROM information_schema.tables
		WHERE table_schema = 'public'
		AND table_name = 'schema_migrations'
	)`
	err = conn.QueryRow(ctx, query).Scan(&exists)
	return exists, err
}

// getMigrationVersion gets the current migration version from schema_migrations.
func getMigrationVersion(ctx context.Context, connStr string) (int, bool, error) {
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return 0, false, err
	}
	defer conn.Close(ctx) //nolint:errcheck // best-effort cleanup

	var version int
	var dirty bool
	query := `SELECT version, dirty FROM schema_migrations LIMIT 1`
	err = conn.QueryRow(ctx, query).Scan(&version, &dirty)
	if err != nil {
		return 0, false, err
	}
	return version, dirty, nil
}

var _ = Describe("autoMigration", func() {
	Describe("runAutoMigration integration", func() {
		It("runs on startup: creates schema_migrations and records a version > 0", func() {
			ctx := context.Background()
			connStr, cleanup := startPostgresContainer(adminAuthSuiteT)
			DeferCleanup(cleanup)

			exists, err := schemaTableExists(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse(), "schema_migrations should not exist before auto-migrate")

			Expect(runAutoMigration(connStr, func(url string) (bootstrap.AutoMigrator, error) {
				return store.NewMigrator(url)
			})).NotTo(HaveOccurred())

			exists, err = schemaTableExists(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeTrue(), "schema_migrations should exist after auto-migrate")

			version, dirty, err := getMigrationVersion(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(BeNumerically(">", 0), "migration version should be > 0 after auto-migrate")
			Expect(dirty).To(BeFalse(), "database should not be in dirty state after successful migration")
		})

		It("is skipped when auto-migrate is disabled: schema_migrations is not created", func() {
			ctx := context.Background()
			connStr, cleanup := startPostgresContainer(adminAuthSuiteT)
			DeferCleanup(cleanup)

			exists, err := schemaTableExists(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse(), "schema_migrations should not exist initially")

			deps := &CoreDeps{
				DatabaseURLGetter: func() string { return connStr },
				AutoMigrateGetter: func() bool { return false },
				MigratorFactory: func(url string) (bootstrap.AutoMigrator, error) {
					Fail("MigratorFactory should not be called when auto-migrate is disabled")
					return store.NewMigrator(url)
				},
			}

			if deps.AutoMigrateGetter() {
				Expect(runAutoMigration(connStr, deps.MigratorFactory)).NotTo(HaveOccurred())
			}

			exists, err = schemaTableExists(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse(), "schema_migrations should not exist when auto-migrate is disabled")
		})

		It("is idempotent: re-running does not change migration version or dirty flag", func() {
			ctx := context.Background()
			connStr, cleanup := startPostgresContainer(adminAuthSuiteT)
			DeferCleanup(cleanup)

			migratorFactory := func(url string) (bootstrap.AutoMigrator, error) {
				return store.NewMigrator(url)
			}

			Expect(runAutoMigration(connStr, migratorFactory)).NotTo(HaveOccurred())

			versionAfterFirst, dirtyAfterFirst, err := getMigrationVersion(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(versionAfterFirst).To(BeNumerically(">", 0))
			Expect(dirtyAfterFirst).To(BeFalse())

			Expect(runAutoMigration(connStr, migratorFactory)).NotTo(HaveOccurred())

			versionAfterSecond, dirtyAfterSecond, err := getMigrationVersion(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(versionAfterSecond).To(Equal(versionAfterFirst),
				"version should be unchanged after idempotent re-run")
			Expect(dirtyAfterSecond).To(BeFalse())
		})
	})
})

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package testutil_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/test/testutil"
)

var suiteT *testing.T

func TestPostgresIntegration(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Postgres Test Helpers Integration Suite")
}

var _ = Describe("testutil.Postgres helpers", func() {
	Describe("SharedPostgres", func() {
		It("returns the same instance across calls", func() {
			env1 := testutil.SharedPostgres(suiteT)
			env2 := testutil.SharedPostgres(suiteT)
			Expect(env1).To(BeIdenticalTo(env2))
		})

		It("has a populated AdminConnStr connecting as postgres superuser", func() {
			env := testutil.SharedPostgres(suiteT)
			Expect(env.AdminConnStr).NotTo(BeEmpty())

			ctx := context.Background()
			conn, err := pgx.Connect(ctx, env.AdminConnStr)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close(ctx)

			var user string
			err = conn.QueryRow(ctx, "SELECT current_user").Scan(&user)
			Expect(err).NotTo(HaveOccurred())
			Expect(user).To(Equal("postgres"))
		})
	})

	Describe("FreshDatabase", func() {
		It("returns a migrated database connecting as holomush", func() {
			env := testutil.SharedPostgres(suiteT)
			connStr := testutil.FreshDatabase(suiteT, env)

			ctx := context.Background()
			conn, err := pgx.Connect(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close(ctx)

			var user string
			err = conn.QueryRow(ctx, "SELECT current_user").Scan(&user)
			Expect(err).NotTo(HaveOccurred())
			Expect(user).To(Equal("holomush"))

			var exists bool
			err = conn.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.tables
					WHERE table_name = 'players'
				)
			`).Scan(&exists)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeTrue(), "players table should exist after migration")
		})

		It("returns isolated databases that do not share data", func() {
			env := testutil.SharedPostgres(suiteT)
			connStr1 := testutil.FreshDatabase(suiteT, env)
			connStr2 := testutil.FreshDatabase(suiteT, env)

			Expect(connStr1).NotTo(Equal(connStr2))

			ctx := context.Background()

			// Capture baseline count (migrations may seed data into the template).
			conn2, err := pgx.Connect(ctx, connStr2)
			Expect(err).NotTo(HaveOccurred())
			defer conn2.Close(ctx)
			var baselineCount int
			err = conn2.QueryRow(ctx, "SELECT count(*) FROM players").Scan(&baselineCount)
			Expect(err).NotTo(HaveOccurred())

			// Insert a row into db1.
			conn1, err := pgx.Connect(ctx, connStr1)
			Expect(err).NotTo(HaveOccurred())
			defer conn1.Close(ctx)
			_, err = conn1.Exec(ctx, "INSERT INTO players (id, username, password_hash) VALUES ('test-id-1', 'isolation_probe', 'hash1')")
			Expect(err).NotTo(HaveOccurred())

			// Verify db2 still has only baseline rows.
			var afterCount int
			err = conn2.QueryRow(ctx, "SELECT count(*) FROM players").Scan(&afterCount)
			Expect(err).NotTo(HaveOccurred())
			Expect(afterCount).To(Equal(baselineCount), "insert in db1 must not appear in db2")
		})
	})

	Describe("RawDatabase", func() {
		It("returns a superuser connection to a blank database with no migrations", func() {
			env := testutil.SharedPostgres(suiteT)
			connStr := testutil.RawDatabase(suiteT, env)

			ctx := context.Background()
			conn, err := pgx.Connect(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close(ctx)

			var user string
			err = conn.QueryRow(ctx, "SELECT current_user").Scan(&user)
			Expect(err).NotTo(HaveOccurred())
			Expect(user).To(Equal("postgres"))

			var exists bool
			err = conn.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.tables
					WHERE table_name = 'players'
				)
			`).Scan(&exists)
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse(), "raw database should have no migrations")
		})
	})
})

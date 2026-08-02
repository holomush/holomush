//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/test/testutil"
)

// INV-STORE-1: After all migrations run, no holomush-owned schema may contain
// a TIMESTAMPTZ or TIMESTAMP column. All pre-gfo6 gap tables (bootstrap_metadata,
// crypto_rekey_checkpoints, holomush_system_info, setting_bootstrap_state) were
// migrated in 000044 (holomush-gfo6.34).
//
// Suite-registered with the store package's Ginkgo runner in
// store_suite_test.go::TestStore. The Describe string is the literal pinned in
// spec_meta_test.go cases (INV-TS-META meta-test).
var _ = Describe("INV-STORE-1: no TIMESTAMPTZ columns after migration", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		pool   *pgxpool.Pool
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		env := testutil.SharedPostgres(suiteT)
		connStr := testutil.FreshDatabase(suiteT, env)
		var err error
		pool, err = pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cancel()
		if pool != nil {
			pool.Close()
		}
	})

	It("contains no TIMESTAMPTZ/TIMESTAMP columns in public or plugin_core_scenes schemas", func() {
		// goose's own bookkeeping table carries `tstamp timestamp NOT NULL DEFAULT now()`.
		// That DDL is authored by the migration engine, not by HoloMUSH, and cannot be
		// changed without forking goose. INV-STORE-1 is about HoloMUSH's PERSISTENT
		// DOMAIN data being BIGINT epoch-ns, so the engine's ledger is out of its scope —
		// the same reason D-15's schema comparison excludes it. The exclusion is by exact
		// table name, so any HoloMUSH table that grows a timestamp column still trips this.
		const migrationBookkeepingTable = "goose_db_version"

		// Guard the exclusion against silently becoming a no-op: if the table were ever
		// renamed, an exclusion keyed on a stale name would quietly stop excluding
		// anything AND stop protecting anything. Assert it is really there.
		var bookkeepingExists bool
		Expect(pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = $1
			)`, migrationBookkeepingTable).Scan(&bookkeepingExists)).To(Succeed())
		Expect(bookkeepingExists).To(BeTrue(),
			"%s must exist, or the exclusion below is masking nothing and hiding nothing",
			migrationBookkeepingTable)

		rows, err := pool.Query(ctx, `
			SELECT table_schema, table_name, column_name, data_type
			FROM information_schema.columns
			WHERE table_schema = ANY($1)
			  AND NOT (table_schema = 'public' AND table_name = $2)
			  AND data_type IN ('timestamp without time zone', 'timestamp with time zone')
			ORDER BY table_schema, table_name, column_name
		`, []string{"public", "plugin_core_scenes"}, migrationBookkeepingTable)
		Expect(err).NotTo(HaveOccurred())
		defer rows.Close()

		var violations []string
		for rows.Next() {
			var schema, table, col, dataType string
			Expect(rows.Scan(&schema, &table, &col, &dataType)).To(Succeed())
			violations = append(violations, fmt.Sprintf("%s.%s.%s (%s)", schema, table, col, dataType))
		}
		Expect(rows.Err()).NotTo(HaveOccurred())

		Expect(violations).To(BeEmpty(),
			"INV-STORE-1: holomush schemas MUST NOT contain TIMESTAMPTZ/TIMESTAMP columns after migration. Violations: %v",
			violations)
	})
})

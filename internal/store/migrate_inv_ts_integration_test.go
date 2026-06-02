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
		rows, err := pool.Query(ctx, `
			SELECT table_schema, table_name, column_name, data_type
			FROM information_schema.columns
			WHERE table_schema = ANY($1)
			  AND data_type IN ('timestamp without time zone', 'timestamp with time zone')
			ORDER BY table_schema, table_name, column_name
		`, []string{"public", "plugin_core_scenes"})
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

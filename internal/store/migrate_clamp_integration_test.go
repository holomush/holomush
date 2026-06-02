//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/test/testutil"
)

// INV-STORE-9: The TIMESTAMPTZ→BIGINT (epoch-nanosecond) conversion migrations
// (000038, 000041, 000042, 000043, 000044) MUST NOT error on pre-existing
// data outside the int64-nanosecond range or on ±infinity sentinels. Such
// values saturate to the int64 bounds (max ≈ 2262-04-11, min ≈ 1677-09-21);
// NULL passes through unchanged; in-range values convert to exact UnixNano.
//
// Regression for holomush-0b3ec: the uniform `(EXTRACT(EPOCH FROM col)*1e9)
// ::BIGINT` USING-clause overflowed (SQLSTATE 22003) on a real out-of-range
// /infinity row in the sandbox, wedging the deploy. CI passed because seeded
// test data was always in range — this test closes that coverage gap.
//
// Suite-registered with the store package's Ginkgo runner in
// store_suite_test.go::TestStore. The Describe string is the literal pinned in
// spec_meta_test.go cases (INV-TS-META meta-test).

// convCase describes one conversion migration plus a low-FK representative
// table whose TIMESTAMPTZ columns it converts.
type convCase struct {
	// targetBefore is the schema version at which the representative table's
	// timestamp columns are still TIMESTAMPTZ (one step before conversion).
	targetBefore uint
	// target is the conversion migration's version.
	target uint
	table  string
	// tsCol is the NOT NULL column seeded with boundary/in-range values.
	tsCol string
	// nullCol is a nullable converted column used for the NULL-passthrough row.
	nullCol string
	// labelCol is a text column used to address each seeded row.
	labelCol string
	// insert builds a boundary row: labelCol=label, tsCol=tsExpr (a SQL
	// TIMESTAMPTZ literal expression).
	insert func(label, tsExpr string) string
	// insertNull builds a row with nullCol = NULL.
	insertNull func(label string) string
}

var _ = Describe("INV-STORE-9: TIMESTAMPTZ→BIGINT conversion saturates out-of-range and infinity to int64-ns bounds and preserves NULL", func() {
	// boundary rows: SQL literal → expected post-conversion BIGINT.
	type boundaryRow struct {
		label string
		tsLit string
		want  int64
	}
	rows := []boundaryRow{
		{"ts9_normal", "'2026-05-23T12:00:00.123456Z'", 1779537600123456000},
		{"ts9_farfut", "'3000-01-01T00:00:00Z'", math.MaxInt64},
		{"ts9_posinf", "'infinity'", math.MaxInt64},
		{"ts9_neginf", "'-infinity'", math.MinInt64},
	}

	verify := func(c convCase) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		pool, err := pgxpool.New(ctx, testutil.FreshDatabase(suiteT, sharedPG))
		Expect(err).NotTo(HaveOccurred())
		defer pool.Close()

		// Migrate to just before the conversion: representative columns are
		// still TIMESTAMPTZ and can hold infinity / far-future values.
		Expect(runMigrations(ctx, pool, c.targetBefore)).To(Succeed())

		for _, r := range rows {
			_, err := pool.Exec(ctx, c.insert(r.label, r.tsLit))
			Expect(err).NotTo(HaveOccurred(), "seed boundary row %s", r.label)
		}
		_, err = pool.Exec(ctx, c.insertNull("ts9_null"))
		Expect(err).NotTo(HaveOccurred(), "seed NULL row")

		// The conversion migration MUST complete — this is the regression:
		// the unfixed USING-clause errors with SQLSTATE 22003 here.
		Expect(runMigrations(ctx, pool, c.target)).To(Succeed(),
			"conversion migration %d must not overflow on out-of-range/infinity data", c.target)

		for _, r := range rows {
			var got int64
			q := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1", c.tsCol, c.table, c.labelCol)
			Expect(pool.QueryRow(ctx, q, r.label).Scan(&got)).To(Succeed(), "read %s", r.label)
			Expect(got).To(Equal(r.want),
				"%s.%s for %s: in-range exact, out-of-range/infinity saturated", c.table, c.tsCol, r.label)
		}

		var isNull bool
		q := fmt.Sprintf("SELECT %s IS NULL FROM %s WHERE %s = $1", c.nullCol, c.table, c.labelCol)
		Expect(pool.QueryRow(ctx, q, "ts9_null").Scan(&isNull)).To(Succeed(), "read NULL row")
		Expect(isNull).To(BeTrue(), "%s.%s NULL must pass through unchanged", c.table, c.nullCol)
	}

	DescribeTable(
		"conversion migration is overflow-safe", verify,
		Entry("000038 eventbus_crypto (crypto_keys)", convCase{
			targetBefore: 37, target: 38,
			table: "crypto_keys", tsCol: "created_at", nullCol: "destroyed_at", labelCol: "context_id",
			insert: func(label, ts string) string {
				return fmt.Sprintf(`INSERT INTO crypto_keys
					(context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants, created_at)
					VALUES ('test', '%s', 1, '\x00'::bytea, 'none', 'k', '[]'::jsonb, %s)`, label, ts)
			},
			insertNull: func(label string) string {
				return fmt.Sprintf(`INSERT INTO crypto_keys
					(context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants, created_at, destroyed_at)
					VALUES ('test', '%s', 1, '\x00'::bytea, 'none', 'k', '[]'::jsonb, now(), NULL)`, label)
			},
		}),
		Entry("000041 auth (players)", convCase{
			targetBefore: 40, target: 41,
			table: "players", tsCol: "created_at", nullCol: "locked_until", labelCol: "id",
			insert: func(label, ts string) string {
				return fmt.Sprintf(`INSERT INTO players (id, username, password_hash, created_at)
					VALUES ('%s', '%s', 'h', %s)`, label, label, ts)
			},
			insertNull: func(label string) string {
				return fmt.Sprintf(`INSERT INTO players (id, username, password_hash, locked_until)
					VALUES ('%s', '%s', 'h', NULL)`, label, label)
			},
		}),
		Entry("000042 world (locations)", convCase{
			targetBefore: 41, target: 42,
			table: "locations", tsCol: "created_at", nullCol: "archived_at", labelCol: "id",
			insert: func(label, ts string) string {
				return fmt.Sprintf(`INSERT INTO locations (id, name, description, created_at)
					VALUES ('%s', '%s', 'd', %s)`, label, label, ts)
			},
			insertNull: func(label string) string {
				return fmt.Sprintf(`INSERT INTO locations (id, name, description, archived_at)
					VALUES ('%s', '%s', 'd', NULL)`, label, label)
			},
		}),
		Entry("000043 totp_misc (plugins)", convCase{
			targetBefore: 42, target: 43,
			table: "plugins", tsCol: "first_seen_at", nullCol: "gc_at", labelCol: "name",
			insert: func(label, ts string) string {
				return fmt.Sprintf(`INSERT INTO plugins (id, name, display_name, version, manifest_hash, first_seen_at)
					VALUES ('%s'::bytea, '%s', 'd', 'v', '\x00'::bytea, %s)`, label, label, ts)
			},
			insertNull: func(label string) string {
				return fmt.Sprintf(`INSERT INTO plugins (id, name, display_name, version, manifest_hash, gc_at)
					VALUES ('%s'::bytea, '%s', 'd', 'v', '\x00'::bytea, NULL)`, label, label)
			},
		}),
		Entry("000044 pregfo6_gap (holomush_system_info)", convCase{
			targetBefore: 43, target: 44,
			table: "holomush_system_info", tsCol: "created_at", nullCol: "updated_at", labelCol: "key",
			insert: func(label, ts string) string {
				return fmt.Sprintf(`INSERT INTO holomush_system_info (key, value, created_at)
					VALUES ('%s', 'v', %s)`, label, ts)
			},
			insertNull: func(label string) string {
				return fmt.Sprintf(`INSERT INTO holomush_system_info (key, value, updated_at)
					VALUES ('%s', 'v', NULL)`, label)
			},
		}),
	)
})

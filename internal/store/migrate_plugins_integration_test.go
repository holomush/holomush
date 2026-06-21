//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

var _ = Describe("Migration 000018", func() {
	Describe("plugins table", func() {
		It("creates plugins table with exactly 9 expected columns and partial unique index", func() {
			ctx := context.Background()
			pool := rawPool(suiteT)

			Expect(runMigrations(ctx, pool, 18)).To(Succeed())

			// Assert each expected column is present...
			var matched int
			Expect(pool.QueryRow(ctx, `
				SELECT COUNT(*) FROM information_schema.columns
				WHERE table_name = 'plugins'
				  AND column_name IN ('id','name','display_name','version',
				                      'manifest_hash','content_hash',
				                      'first_seen_at','last_seen_at','gc_at')
			`).Scan(&matched)).To(Succeed())
			Expect(matched).To(Equal(9), "plugins must have all 9 expected columns")

			// ...and assert there are NO extras (catches contract drift if a future
			// migration adds a column without updating the named-column whitelist).
			var total int
			Expect(pool.QueryRow(ctx, `
				SELECT COUNT(*) FROM information_schema.columns
				WHERE table_name = 'plugins'
			`).Scan(&total)).To(Succeed())
			Expect(total).To(Equal(9), "plugins MUST have exactly 9 columns (no extras)")

			var indexExists bool
			Expect(pool.QueryRow(ctx, `
				SELECT EXISTS (
				    SELECT 1 FROM pg_indexes
				    WHERE indexname = 'plugins_name_active'
				      AND indexdef LIKE '%WHERE (gc_at IS NULL)%'
				)
			`).Scan(&indexExists)).To(Succeed())
			Expect(indexExists).To(BeTrue(), "partial UNIQUE index plugins_name_active must exist")
		})
	})

	Describe("events_audit truncation", func() {
		It("truncates events_audit when applying migration 000018", func() {
			ctx := context.Background()
			pool := rawPool(suiteT)

			Expect(runMigrations(ctx, pool, 17)).To(Succeed())
			_, err := pool.Exec(ctx, `
				INSERT INTO events_audit (id, subject, type, timestamp, actor_kind,
				                         envelope, schema_ver, codec, js_seq, rendering)
				VALUES ($1, 'test', 'test', now(), 'plugin', '\x00', 1, 'identity', 1, '{}'::jsonb)
			`, []byte("0123456789abcdef"))
			Expect(err).NotTo(HaveOccurred())

			Expect(runMigrations(ctx, pool, 18)).To(Succeed())

			var n int
			Expect(pool.QueryRow(ctx, `SELECT COUNT(*) FROM events_audit`).Scan(&n)).To(Succeed())
			Expect(n).To(Equal(0), "events_audit MUST be empty after migration 000018")
		})
	})
})

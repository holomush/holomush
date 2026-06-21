//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/store"
)

var _ = Describe("RoleStore", func() {
	Describe("PlayerHasRole", func() {
		It("returns true for player with admin character", func() {
			ctx := context.Background()
			pool := rawPool(suiteT)
			Expect(runMigrations(ctx, pool, 20)).To(Succeed())

			playerID := idgen.New().String()
			charID := idgen.New().String()
			_, err := pool.Exec(ctx, `INSERT INTO players (id, username, password_hash, created_at, updated_at)
				VALUES ($1, $2, $3, now(), now())`, playerID, "alice-"+playerID[:8], "hash")
			Expect(err).NotTo(HaveOccurred())
			_, err = pool.Exec(ctx, `INSERT INTO characters (id, player_id, name)
				VALUES ($1, $2, $3)`, charID, playerID, "Alice-"+charID[:8])
			Expect(err).NotTo(HaveOccurred())

			rs := store.NewPostgresRoleStore(pool)
			Expect(rs.AddRole(ctx, charID, access.RoleAdmin)).To(Succeed())

			has, err := rs.PlayerHasRole(ctx, playerID, access.RoleAdmin)
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeTrue())
		})

		It("returns false for player without any admin character", func() {
			ctx := context.Background()
			pool := rawPool(suiteT)
			Expect(runMigrations(ctx, pool, 20)).To(Succeed())

			playerID := idgen.New().String()
			charID := idgen.New().String()
			_, err := pool.Exec(ctx, `INSERT INTO players (id, username, password_hash, created_at, updated_at)
				VALUES ($1, $2, $3, now(), now())`, playerID, "bob-"+playerID[:8], "hash")
			Expect(err).NotTo(HaveOccurred())
			_, err = pool.Exec(ctx, `INSERT INTO characters (id, player_id, name)
				VALUES ($1, $2, $3)`, charID, playerID, "Bob-"+charID[:8])
			Expect(err).NotTo(HaveOccurred())

			rs := store.NewPostgresRoleStore(pool)
			// Add and then remove to assert the negative path explicitly.
			Expect(rs.AddRole(ctx, charID, access.RoleAdmin)).To(Succeed())
			Expect(rs.RemoveRole(ctx, charID, access.RoleAdmin)).To(Succeed())

			has, err := rs.PlayerHasRole(ctx, playerID, access.RoleAdmin)
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeFalse())
		})

		It("returns false for unknown player", func() {
			ctx := context.Background()
			pool := rawPool(suiteT)
			Expect(runMigrations(ctx, pool, 20)).To(Succeed())

			rs := store.NewPostgresRoleStore(pool)
			has, err := rs.PlayerHasRole(ctx, idgen.New().String(), access.RoleAdmin)
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeFalse())
		})
	})
})

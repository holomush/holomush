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

// These specs stage the schema at version 20 (runMigrations(ctx, pool, 20)),
// which is BEFORE migration 000054 adds characters.normalized_name /
// name_skeleton / name_skeleton_unicode_version. Their direct-SQL character
// INSERTs therefore do NOT supply the identity columns, and must not: at
// version 20 those columns do not exist, and 000056's NOT NULL + UNIQUE
// constraints are not in force. This is the one deliberate exception to the
// rule that every character fixture goes through internal/testsupport/chartest.
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

	// PlayerRoles generalizes PlayerHasRole's any-character-of-the-player
	// semantics from one role to the whole set. It lands on the CONCRETE
	// *PostgresRoleStore, never on the RoleStore interface — see
	// role_store_player_roles_test.go for the interface method-set guard.
	Describe("PlayerRoles", func() {
		It("returns the deduplicated union of roles held by any character of the player", func() {
			ctx := context.Background()
			pool := rawPool(suiteT)
			Expect(runMigrations(ctx, pool, 20)).To(Succeed())

			playerID := idgen.New().String()
			charA := idgen.New().String()
			charB := idgen.New().String()
			_, err := pool.Exec(ctx, `INSERT INTO players (id, username, password_hash, created_at, updated_at)
				VALUES ($1, $2, $3, now(), now())`, playerID, "carol-"+playerID[:8], "hash")
			Expect(err).NotTo(HaveOccurred())
			for _, c := range []string{charA, charB} {
				_, err = pool.Exec(ctx, `INSERT INTO characters (id, player_id, name)
					VALUES ($1, $2, $3)`, c, playerID, "Carol-"+c[:8])
				Expect(err).NotTo(HaveOccurred())
			}

			rs := store.NewPostgresRoleStore(pool)
			// charA and charB BOTH hold admin — the union must dedupe it to one
			// entry, not report it twice.
			Expect(rs.AddRole(ctx, charA, access.RoleAdmin)).To(Succeed())
			Expect(rs.AddRole(ctx, charB, access.RoleAdmin)).To(Succeed())
			Expect(rs.AddRole(ctx, charB, access.RoleBuilder)).To(Succeed())

			roles, err := rs.PlayerRoles(ctx, playerID)
			Expect(err).NotTo(HaveOccurred())
			// ORDER BY role makes the slice stable across calls: a policy verdict
			// is reproducible either way, but its failure messages are not.
			Expect(roles).To(Equal([]string{access.RoleAdmin, access.RoleBuilder}))
		})

		It("returns an empty slice and no error for a player whose characters hold no roles", func() {
			ctx := context.Background()
			pool := rawPool(suiteT)
			Expect(runMigrations(ctx, pool, 20)).To(Succeed())

			playerID := idgen.New().String()
			charID := idgen.New().String()
			_, err := pool.Exec(ctx, `INSERT INTO players (id, username, password_hash, created_at, updated_at)
				VALUES ($1, $2, $3, now(), now())`, playerID, "dave-"+playerID[:8], "hash")
			Expect(err).NotTo(HaveOccurred())
			_, err = pool.Exec(ctx, `INSERT INTO characters (id, player_id, name)
				VALUES ($1, $2, $3)`, charID, playerID, "Dave-"+charID[:8])
			Expect(err).NotTo(HaveOccurred())

			rs := store.NewPostgresRoleStore(pool)
			roles, err := rs.PlayerRoles(ctx, playerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(roles).To(BeEmpty())
		})

		It("returns an empty slice and no error for a player id matching no row at all", func() {
			ctx := context.Background()
			pool := rawPool(suiteT)
			Expect(runMigrations(ctx, pool, 20)).To(Succeed())

			rs := store.NewPostgresRoleStore(pool)
			// Absence of roles is not an error — only MALFORMED input is (asserted
			// in the untagged role_store_player_roles_test.go).
			roles, err := rs.PlayerRoles(ctx, idgen.New().String())
			Expect(err).NotTo(HaveOccurred())
			Expect(roles).To(BeEmpty())
		})

		It("does not leak roles held by another player's characters", func() {
			ctx := context.Background()
			pool := rawPool(suiteT)
			Expect(runMigrations(ctx, pool, 20)).To(Succeed())

			mine := idgen.New().String()
			theirs := idgen.New().String()
			myChar := idgen.New().String()
			theirChar := idgen.New().String()
			for _, p := range []string{mine, theirs} {
				// Full id in the username, not a prefix: two ULIDs minted in the
				// same millisecond share their leading timestamp characters, so
				// a truncated prefix collides on players_username_key.
				_, err := pool.Exec(ctx, `INSERT INTO players (id, username, password_hash, created_at, updated_at)
					VALUES ($1, $2, $3, now(), now())`, p, "erin-"+p, "hash")
				Expect(err).NotTo(HaveOccurred())
			}
			_, err := pool.Exec(ctx, `INSERT INTO characters (id, player_id, name)
				VALUES ($1, $2, $3)`, myChar, mine, "Erin-"+myChar[:8])
			Expect(err).NotTo(HaveOccurred())
			_, err = pool.Exec(ctx, `INSERT INTO characters (id, player_id, name)
				VALUES ($1, $2, $3)`, theirChar, theirs, "Erin-"+theirChar[:8])
			Expect(err).NotTo(HaveOccurred())

			rs := store.NewPostgresRoleStore(pool)
			Expect(rs.AddRole(ctx, theirChar, access.RoleAdmin)).To(Succeed())

			roles, err := rs.PlayerRoles(ctx, mine)
			Expect(err).NotTo(HaveOccurred())
			Expect(roles).To(BeEmpty())
		})
	})
})

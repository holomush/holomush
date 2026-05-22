// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package access_test

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
)

var _ = Describe("Seed Policy Behavior", func() {
	var (
		charID1 ulid.ULID
		charID2 ulid.ULID
		locID1  ulid.ULID
		locID2  ulid.ULID
	)

	BeforeEach(func() {
		ctx := context.Background()

		_, err := env.pool.Exec(ctx, "DELETE FROM player_character_bindings")
		Expect(err).NotTo(HaveOccurred())
		// Must precede characters/locations: held_by_character_id and
		// location_id FKs are ON DELETE SET NULL, which would clear all
		// three containment fields on any leftover object and violate
		// chk_exactly_one_containment. Per holomush-k3ud regression test.
		_, err = env.pool.Exec(ctx, "DELETE FROM objects")
		Expect(err).NotTo(HaveOccurred())
		_, err = env.pool.Exec(ctx, "DELETE FROM characters")
		Expect(err).NotTo(HaveOccurred())
		_, err = env.pool.Exec(ctx, "DELETE FROM players")
		Expect(err).NotTo(HaveOccurred())
		_, err = env.pool.Exec(ctx, "DELETE FROM locations")
		Expect(err).NotTo(HaveOccurred())

		locID1 = core.NewULID()
		locID2 = core.NewULID()
		_, err = env.pool.Exec(ctx, `
			INSERT INTO locations (id, name, description, type, replay_policy)
			VALUES ($1, 'Town Square', 'A bustling square.', 'persistent', 'last:0')`,
			locID1.String())
		Expect(err).NotTo(HaveOccurred())

		_, err = env.pool.Exec(ctx, `
			INSERT INTO locations (id, name, description, type, replay_policy)
			VALUES ($1, 'Dark Forest', 'A gloomy forest.', 'persistent', 'last:0')`,
			locID2.String())
		Expect(err).NotTo(HaveOccurred())

		playerID1 := core.NewULID()
		playerID2 := core.NewULID()
		_, err = env.pool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash)
			VALUES ($1, $2, 'hash1')`,
			playerID1.String(), "player1_"+time.Now().Format("150405.000"))
		Expect(err).NotTo(HaveOccurred())

		_, err = env.pool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash)
			VALUES ($1, $2, 'hash2')`,
			playerID2.String(), "player2_"+time.Now().Format("150405.000"))
		Expect(err).NotTo(HaveOccurred())

		charID1 = core.NewULID()
		charID2 = core.NewULID()
		_, err = env.pool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, location_id)
			VALUES ($1, $2, 'Alice', $3)`,
			charID1.String(), playerID1.String(), locID1.String())
		Expect(err).NotTo(HaveOccurred())

		_, err = env.pool.Exec(ctx, `
			INSERT INTO characters (id, player_id, name, location_id)
			VALUES ($1, $2, 'Bob', $3)`,
			charID2.String(), playerID2.String(), locID1.String())
		Expect(err).NotTo(HaveOccurred())

		env.auditWriter.Reset()
	})

	Describe("Self-access", func() {
		It("allows a character to read their own character", func() {
			decision := evalAccess("character:"+charID1.String(), "read", "character:"+charID1.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})

		It("allows a character to write their own character", func() {
			decision := evalAccess("character:"+charID1.String(), "write", "character:"+charID1.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})
	})

	Describe("Location read", func() {
		It("allows a character to read their current location", func() {
			decision := evalAccess("character:"+charID1.String(), "read", "location:"+locID1.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})

		It("denies a character reading a different location", func() {
			decision := evalAccess("character:"+charID1.String(), "read", "location:"+locID2.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
		})
	})

	Describe("Co-location", func() {
		It("allows reading a co-located character", func() {
			decision := evalAccess("character:"+charID1.String(), "read", "character:"+charID2.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})

		It("denies reading a character in a different location", func() {
			ctx := context.Background()
			_, err := env.pool.Exec(ctx, `UPDATE characters SET location_id = $1 WHERE id = $2`,
				locID2.String(), charID2.String())
			Expect(err).NotTo(HaveOccurred())

			decision := evalAccess("character:"+charID1.String(), "read", "character:"+charID2.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
		})
	})

	Describe("Default deny", func() {
		It("denies when no policies match", func() {
			decision := evalAccess("character:"+charID1.String(), "admin_nuke", "location:"+locID1.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
		})
	})

	// holomush-k3ud: regression lock for seed:player-object-colocation,
	// exercised through the REAL ObjectProvider (transitive location walk
	// included). Pins the bug fingerprint: without ObjectProvider
	// registered, both 'allows reading a co-located object' AND 'allows
	// reading an object held by a co-located character' would silently
	// default-deny — the same default-deny shape as the original g776
	// bug. The privacytest harness uses allowAllPolicyEngine and cannot
	// catch this class of regression.
	Describe("Object co-location (holomush-k3ud)", func() {
		var (
			objIDDirect ulid.ULID
			objIDHeld   ulid.ULID
			objIDNested ulid.ULID
			containerID ulid.ULID
		)

		BeforeEach(func() {
			ctx := context.Background()
			_, err := env.pool.Exec(ctx, "DELETE FROM objects")
			Expect(err).NotTo(HaveOccurred())

			// 1. Object directly in locID1 (same as charID1's location).
			objIDDirect = core.NewULID()
			_, err = env.pool.Exec(ctx, `
				INSERT INTO objects (id, name, description, location_id, is_container)
				VALUES ($1, 'Lantern', 'A brass lantern.', $2, false)`,
				objIDDirect.String(), locID1.String())
			Expect(err).NotTo(HaveOccurred())

			// 2. Object held by charID2 (who is at locID1, same as charID1).
			objIDHeld = core.NewULID()
			_, err = env.pool.Exec(ctx, `
				INSERT INTO objects (id, name, description, held_by_character_id, is_container)
				VALUES ($1, 'Note', 'A folded note.', $2, false)`,
				objIDHeld.String(), charID2.String())
			Expect(err).NotTo(HaveOccurred())

			// 3. Object inside a container at locID1 — exercises the
			//    container-chain walk (a nested case the LocationProvider
			//    fix did not cover).
			containerID = core.NewULID()
			_, err = env.pool.Exec(ctx, `
				INSERT INTO objects (id, name, description, location_id, is_container)
				VALUES ($1, 'Chest', 'A wooden chest.', $2, true)`,
				containerID.String(), locID1.String())
			Expect(err).NotTo(HaveOccurred())

			objIDNested = core.NewULID()
			_, err = env.pool.Exec(ctx, `
				INSERT INTO objects (id, name, description, contained_in_object_id, is_container)
				VALUES ($1, 'Coin', 'A gold coin.', $2, false)`,
				objIDNested.String(), containerID.String())
			Expect(err).NotTo(HaveOccurred())
		})

		It("allows reading a co-located object", func() {
			decision := evalAccess("character:"+charID1.String(), "read", "object:"+objIDDirect.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})

		It("allows reading an object held by a co-located character", func() {
			decision := evalAccess("character:"+charID1.String(), "read", "object:"+objIDHeld.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})

		It("allows reading an object inside a co-located container (container-chain walk)", func() {
			decision := evalAccess("character:"+charID1.String(), "read", "object:"+objIDNested.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})

		It("denies reading an object in a different location", func() {
			ctx := context.Background()
			otherObj := core.NewULID()
			_, err := env.pool.Exec(ctx, `
				INSERT INTO objects (id, name, description, location_id, is_container)
				VALUES ($1, 'Distant rock', '', $2, false)`,
				otherObj.String(), locID2.String())
			Expect(err).NotTo(HaveOccurred())

			decision := evalAccess("character:"+charID1.String(), "read", "object:"+otherObj.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
		})
	})
})

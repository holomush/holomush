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
	// bug. The integrationtest harness uses allowAllPolicyEngine and cannot
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

	// holomush-9gtl: regression lock for the empty-string-equality
	// permissive-match gap. Before this fix, CharacterProvider and
	// ObjectProvider emitted location="" when has_location=false, and
	// PropertyProvider emitted parent_location="" when
	// has_parent_location=false. The colocation/colocation-style seeds
	// gate via `resource.X.location == principal.character.location`,
	// and the DSL evaluator treats `"" == ""` as true (both values are
	// present strings; only missing keys short-circuit to false per ADR
	// 0010 / holomush-iv43). Result: any character without a location
	// could read any object/character/property also in an un-locatable
	// state — a narrow but real fail-open.
	//
	// Fix per ADR holomush-ti1b: providers MUST omit the optional
	// `location` / `parent_location` key when unresolved. DSL missing-
	// attribute semantics then short-circuit the comparison to false,
	// preserving default-deny.
	Describe("Un-locatable empty-string equality is NOT permissive (holomush-9gtl)", func() {
		var (
			unlocChar1 ulid.ULID
			unlocChar2 ulid.ULID
		)

		BeforeEach(func() {
			ctx := context.Background()

			// Two characters with NO location. The pre-9gtl bug:
			// CharacterProvider emits "location": "" for both, then
			// `"" == ""` matches → seed:player-character-colocation
			// permits the read between them.
			unlocChar1 = core.NewULID()
			unlocChar2 = core.NewULID()
			playerA := core.NewULID()
			playerB := core.NewULID()
			// Use the ULID itself as the username-uniqueness anchor —
			// time.Now() in two adjacent inserts can collide on the
			// 1ms-precision UNIQUE(username) index.
			_, err := env.pool.Exec(ctx, `
				INSERT INTO players (id, username, password_hash)
				VALUES ($1, $2, 'hashA')`,
				playerA.String(), "unlocA_"+playerA.String())
			Expect(err).NotTo(HaveOccurred())
			_, err = env.pool.Exec(ctx, `
				INSERT INTO players (id, username, password_hash)
				VALUES ($1, $2, 'hashB')`,
				playerB.String(), "unlocB_"+playerB.String())
			Expect(err).NotTo(HaveOccurred())

			_, err = env.pool.Exec(ctx, `
				INSERT INTO characters (id, player_id, name, location_id)
				VALUES ($1, $2, 'Drifter1', NULL)`,
				unlocChar1.String(), playerA.String())
			Expect(err).NotTo(HaveOccurred())
			_, err = env.pool.Exec(ctx, `
				INSERT INTO characters (id, player_id, name, location_id)
				VALUES ($1, $2, 'Drifter2', NULL)`,
				unlocChar2.String(), playerB.String())
			Expect(err).NotTo(HaveOccurred())
		})

		It("denies reading another un-locatable character (character-character)", func() {
			decision := evalAccess("character:"+unlocChar1.String(), "read", "character:"+unlocChar2.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny),
				"un-locatable characters MUST NOT see each other via seed:player-character-colocation. "+
					"The 'location' attr must be omitted (not emitted as empty string) when has_location=false.")
		})

		It("denies reading an object in a containment cycle (chain walk returns un-locatable)", func() {
			ctx := context.Background()
			// Construct a circular containment: A contained in B, B
			// contained in A. ObjectProvider's chain walk detects the
			// cycle via visited-set and returns un-locatable
			// (has_location=false, no `location` attr emitted under the
			// 9gtl fix). Combined with an un-locatable character
			// principal, the colocation seed must NOT permit-match via
			// empty-string equality.
			objA := core.NewULID()
			objB := core.NewULID()

			// Seed an anchor location for A so the chk_exactly_one
			// constraint is satisfied at INSERT time.
			anchor := core.NewULID()
			_, err := env.pool.Exec(ctx, `
				INSERT INTO locations (id, name, description, type, replay_policy)
				VALUES ($1, 'Anchor', '', 'persistent', 'last:0')`,
				anchor.String())
			Expect(err).NotTo(HaveOccurred())

			_, err = env.pool.Exec(ctx, `
				INSERT INTO objects (id, name, description, location_id, is_container)
				VALUES ($1, 'A', '', $2, true)`,
				objA.String(), anchor.String())
			Expect(err).NotTo(HaveOccurred())

			_, err = env.pool.Exec(ctx, `
				INSERT INTO objects (id, name, description, contained_in_object_id, is_container)
				VALUES ($1, 'B', '', $2, true)`,
				objB.String(), objA.String())
			Expect(err).NotTo(HaveOccurred())

			// Close the cycle: A now contained in B (was in a location).
			// This direct UPDATE bypasses ObjectRepository.Move's
			// cycle-detection guard, which is the corruption case we
			// want to defend against at resolve time.
			_, err = env.pool.Exec(ctx, `
				UPDATE objects SET location_id = NULL, contained_in_object_id = $1
				WHERE id = $2`,
				objB.String(), objA.String())
			Expect(err).NotTo(HaveOccurred())

			decision := evalAccess("character:"+unlocChar1.String(), "read", "object:"+objA.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny),
				"un-locatable character + cycle-broken object MUST NOT permit-match via empty-string equality")
		})
	})
})

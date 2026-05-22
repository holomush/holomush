// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package access_test

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2"      //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"         //nolint:revive // gomega convention
	. "github.com/onsi/gomega/gstruct" //nolint:revive // gstruct matchers convention

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
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
		// entity_properties references characters, locations, and objects via
		// parent_id (enforced at the application layer). Delete before
		// characters/locations to avoid orphaned rows from FK cascade surprises.
		_, err = env.pool.Exec(ctx, "DELETE FROM entity_properties")
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

	// holomush-72ou: regression lock for the six property visibility seeds
	// (seed:property-public-read / private-read / admin-read / owner-write /
	// restricted-visible-to / restricted-excluded). All scenarios exercise the
	// REAL ABAC engine + REAL PropertyProvider + REAL ParentLocationResolver
	// registered in setupAccessTestEnv. The integrationtest harness uses
	// allowAllPolicyEngine and cannot catch this class of regression.
	Describe("Property visibility seeds (holomush-72ou)", func() {
		var (
			adminChar ulid.ULID
			targetID  ulid.ULID // a "third character" used as parent for several property tests
		)

		BeforeEach(func() {
			ctx := context.Background()
			// Admin character with the "admin" role for S6/S8.
			adminPlayerID := core.NewULID()
			_, err := env.pool.Exec(ctx, `INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'h')`,
				adminPlayerID.String(), "admin_"+adminPlayerID.String())
			Expect(err).NotTo(HaveOccurred())
			adminChar = core.NewULID()
			_, err = env.pool.Exec(ctx, `INSERT INTO characters (id, player_id, name, location_id) VALUES ($1, $2, 'Admin', $3)`,
				adminChar.String(), adminPlayerID.String(), locID1.String())
			Expect(err).NotTo(HaveOccurred())
			// Wire the "admin" role on the static role resolver exposed via env.
			// The subject ID format matches what CharacterProvider passes to RoleResolver.GetRoles.
			env.roleResolver.roles[access.CharacterSubject(adminChar.String())] = []string{"admin"}

			// Third character "target" at loc2 for various tests.
			targetPlayerID := core.NewULID()
			_, err = env.pool.Exec(ctx, `INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'h')`,
				targetPlayerID.String(), "target_"+targetPlayerID.String())
			Expect(err).NotTo(HaveOccurred())
			targetID = core.NewULID()
			_, err = env.pool.Exec(ctx, `INSERT INTO characters (id, player_id, name, location_id) VALUES ($1, $2, 'Target', $3)`,
				targetID.String(), targetPlayerID.String(), locID2.String())
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			// Clean up admin role assignment so it doesn't leak between specs.
			delete(env.roleResolver.roles, access.CharacterSubject(adminChar.String()))
		})

		It("S1: allows reading a public property on a co-located character", func() {
			propID := insertProperty("character", charID2, "bio", "hello", "public", nil, nil, nil)
			decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})

		It("S2: allows reading a public property on a co-located location", func() {
			propID := insertProperty("location", locID1, "greeting", "welcome", "public", nil, nil, nil)
			decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})

		It("S3: denies reading a public property on a different-location parent", func() {
			propID := insertProperty("character", targetID, "bio", "secret", "public", nil, nil, nil)
			decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
		})

		It("S4: allows owner to read their own private property (self-as-parent path)", func() {
			propID := insertProperty("character", charID1, "diary", "secret", "private", &charID1, nil, nil)
			decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})

		It("S5: denies non-owner reading a private property", func() {
			propID := insertProperty("character", charID2, "note", "hush", "private", &charID2, nil, nil)
			decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
		})

		It("S6: allows admin to read an admin-visibility property", func() {
			propID := insertProperty("character", targetID, "secret", "admin-only", "admin", nil, nil, nil)
			decision := evalAccess("character:"+adminChar.String(), "read", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})

		It("S7: denies non-admin reading an admin-visibility property", func() {
			propID := insertProperty("character", targetID, "secret", "admin-only", "admin", nil, nil, nil)
			decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
		})

		It("S8: allows admins to read a system-visibility property (ADR 087)", func() {
			// seed:property-system-forbid was removed per ADR 087. Without an explicit
			// forbid, seed:admin-full-access (blanket permit for admins) is the only
			// matching policy, so admins DO get access to system properties.
			// Non-admin characters still default-deny (no matching permit) — see S7.
			// The spec test matrix comment "no seed permits system" is inaccurate;
			// admin-full-access is unconditional and matches all resources.
			propID := insertProperty("character", targetID, "internal", "system-only", "system", nil, nil, nil)
			decision := evalAccess("character:"+adminChar.String(), "read", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow),
				"admin-full-access permits admins to read system properties (ADR 087 removed the forbid)")
		})

		It("S9: allows visible_to-listed character to read a restricted property", func() {
			// char1 at loc1; target at loc2 (NOT co-located). visible_to includes char1.
			propID := insertProperty("character", targetID, "memo", "x", "restricted", nil, []ulid.ULID{charID1}, nil)
			decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})

		It("S10: excluded_from beats visible_to (deny-overrides)", func() {
			// char1 in BOTH lists → forbid wins via deny-overrides.
			// seed:property-restricted-excluded fires as a FORBID → EffectDeny.
			// seed:property-restricted-visible-to fires as a PERMIT → EffectAllow.
			// Under deny-overrides, EffectDeny wins over EffectAllow.
			// Raw-SQL insertProperty bypasses the production overlap validator
			// (worldpg.validateVisibilityLists); this test exercises the engine's
			// deny-override semantics directly, not the write-path validator.
			env.auditWriter.Reset()
			propID := insertProperty("character", targetID, "split", "x", "restricted",
				nil, []ulid.ULID{charID1}, []ulid.ULID{charID1})
			decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectDeny))

			// Lock the audit-contract: an explicit forbid MUST emit one
			// audit entry carrying EffectDeny + a non-empty PolicyID. The
			// PolicyID is a ULID PK from the policy store, not the seed
			// slug — name-level binding would require an extra store lookup.
			Eventually(func() []audit.Event {
				return env.auditWriter.Entries()
			}).WithTimeout(2*time.Second).WithPolling(10*time.Millisecond).
				ShouldNot(BeEmpty(), "deny audit entry MUST be recorded")
			Expect(env.auditWriter.Entries()).To(ContainElement(MatchFields(IgnoreExtras, Fields{
				"Effect": Equal(types.EffectDeny),
				"ID":     Not(BeEmpty()),
			})), "audit entry MUST carry EffectDeny + non-empty PolicyID")
		})

		It("S11: allows owner to write their own property", func() {
			propID := insertProperty("character", charID1, "writable", "x", "private", &charID1, nil, nil)
			decision := evalAccess("character:"+charID1.String(), "write", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectAllow))
		})

		It("S12: denies non-owner writing a property", func() {
			propID := insertProperty("character", charID2, "owned-by-2", "x", "private", &charID2, nil, nil)
			decision := evalAccess("character:"+charID1.String(), "write", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny))
		})

		It("S13: denies reading on an un-locatable property (ti1b reinforcement)", func() {
			// Construct an object containment cycle so parent_location resolves to nil.
			anchor := core.NewULID()
			_, err := env.pool.Exec(context.Background(),
				`INSERT INTO locations (id, name, description, type, replay_policy)
				 VALUES ($1, 'Anchor', '', 'persistent', 'last:0')`,
				anchor.String())
			Expect(err).NotTo(HaveOccurred())
			objA := core.NewULID()
			objB := core.NewULID()
			_, err = env.pool.Exec(context.Background(),
				`INSERT INTO objects (id, name, description, location_id, is_container)
				 VALUES ($1, 'A', '', $2, true)`, objA.String(), anchor.String())
			Expect(err).NotTo(HaveOccurred())
			_, err = env.pool.Exec(context.Background(),
				`INSERT INTO objects (id, name, description, contained_in_object_id, is_container)
				 VALUES ($1, 'B', '', $2, true)`, objB.String(), objA.String())
			Expect(err).NotTo(HaveOccurred())
			_, err = env.pool.Exec(context.Background(),
				`UPDATE objects SET location_id = NULL, contained_in_object_id = $1 WHERE id = $2`,
				objB.String(), objA.String())
			Expect(err).NotTo(HaveOccurred())

			propID := insertProperty("object", objA, "name", "x", "public", nil, nil, nil)
			decision := evalAccess("character:"+charID1.String(), "read", "property:"+propID.String())
			Expect(decision.Effect()).To(Equal(types.EffectDefaultDeny),
				"parent_location omitted (ti1b) → seed:property-public-read can't match")
		})
	})

	// holomush-72ou: regression lock for ParentLocationResolver contract.
	// Exercises the postgres-layer resolver directly (not via the ABAC engine)
	// to pin location/character/object parent semantics including edge cases.
	Describe("ParentLocationResolver (holomush-72ou)", func() {
		It("R1: location parent returns parent_id directly", func() {
			got, err := env.parentLocResolver.ResolveParentLocation(context.Background(), "location", locID1)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(*got).To(Equal(locID1))
		})

		It("R2: character parent JOINs current location_id", func() {
			got, err := env.parentLocResolver.ResolveParentLocation(context.Background(), "character", charID1)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(*got).To(Equal(locID1))
		})

		It("R3: character parent with NULL location returns nil", func() {
			ctx := context.Background()
			unlocPlayer := core.NewULID()
			_, err := env.pool.Exec(ctx, `INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'h')`,
				unlocPlayer.String(), "unloc_"+unlocPlayer.String())
			Expect(err).NotTo(HaveOccurred())
			unlocChar := core.NewULID()
			_, err = env.pool.Exec(ctx, `INSERT INTO characters (id, player_id, name, location_id) VALUES ($1, $2, 'Drifter', NULL)`,
				unlocChar.String(), unlocPlayer.String())
			Expect(err).NotTo(HaveOccurred())

			got, err := env.parentLocResolver.ResolveParentLocation(ctx, "character", unlocChar)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
		})

		It("R4: object parent at direct location returns that location", func() {
			ctx := context.Background()
			objID := core.NewULID()
			_, err := env.pool.Exec(ctx,
				`INSERT INTO objects (id, name, description, location_id, is_container)
				 VALUES ($1, 'Tome', '', $2, false)`, objID.String(), locID1.String())
			Expect(err).NotTo(HaveOccurred())

			got, err := env.parentLocResolver.ResolveParentLocation(ctx, "object", objID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(*got).To(Equal(locID1))
		})

		It("R5: object parent held by character resolves via character location", func() {
			ctx := context.Background()
			objID := core.NewULID()
			_, err := env.pool.Exec(ctx,
				`INSERT INTO objects (id, name, description, held_by_character_id, is_container)
				 VALUES ($1, 'Trinket', '', $2, false)`, objID.String(), charID1.String())
			Expect(err).NotTo(HaveOccurred())

			got, err := env.parentLocResolver.ResolveParentLocation(ctx, "object", objID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(*got).To(Equal(locID1))
		})

		It("R6: object parent contained in object resolves recursively", func() {
			ctx := context.Background()
			chest := core.NewULID()
			_, err := env.pool.Exec(ctx,
				`INSERT INTO objects (id, name, description, location_id, is_container)
				 VALUES ($1, 'Chest', '', $2, true)`, chest.String(), locID1.String())
			Expect(err).NotTo(HaveOccurred())
			coin := core.NewULID()
			_, err = env.pool.Exec(ctx,
				`INSERT INTO objects (id, name, description, contained_in_object_id, is_container)
				 VALUES ($1, 'Coin', '', $2, false)`, coin.String(), chest.String())
			Expect(err).NotTo(HaveOccurred())

			got, err := env.parentLocResolver.ResolveParentLocation(ctx, "object", coin)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(*got).To(Equal(locID1))
		})

		It("R7: object parent in containment cycle returns nil", func() {
			ctx := context.Background()
			anchor := core.NewULID()
			_, err := env.pool.Exec(ctx,
				`INSERT INTO locations (id, name, description, type, replay_policy)
				 VALUES ($1, 'Anchor2', '', 'persistent', 'last:0')`, anchor.String())
			Expect(err).NotTo(HaveOccurred())
			objA := core.NewULID()
			_, err = env.pool.Exec(ctx,
				`INSERT INTO objects (id, name, description, location_id, is_container)
				 VALUES ($1, 'A', '', $2, true)`, objA.String(), anchor.String())
			Expect(err).NotTo(HaveOccurred())
			objB := core.NewULID()
			_, err = env.pool.Exec(ctx,
				`INSERT INTO objects (id, name, description, contained_in_object_id, is_container)
				 VALUES ($1, 'B', '', $2, true)`, objB.String(), objA.String())
			Expect(err).NotTo(HaveOccurred())
			// Close the cycle: A now contained in B.
			_, err = env.pool.Exec(ctx,
				`UPDATE objects SET location_id = NULL, contained_in_object_id = $1 WHERE id = $2`,
				objB.String(), objA.String())
			Expect(err).NotTo(HaveOccurred())

			got, err := env.parentLocResolver.ResolveParentLocation(ctx, "object", objA)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
		})

		It("R8: object parent at max depth exceeded returns nil", func() {
			ctx := context.Background()
			prev := core.NewULID()
			_, err := env.pool.Exec(ctx,
				`INSERT INTO objects (id, name, description, location_id, is_container)
				 VALUES ($1, 'obj0', '', $2, true)`, prev.String(), locID1.String())
			Expect(err).NotTo(HaveOccurred())
			// Build a chain of 20 levels. The resolver's maxParentChainDepth=20
			// means depth 20 is the last level it scans; depth 21 would be the
			// object that must be unreachable. We insert 20 additional objects
			// so the leaf is at depth 21 from the starting object.
			for i := 1; i <= 20; i++ {
				next := core.NewULID()
				_, err := env.pool.Exec(ctx,
					`INSERT INTO objects (id, name, description, contained_in_object_id, is_container)
					 VALUES ($1, $2, '', $3, true)`, next.String(), fmt.Sprintf("obj%d", i), prev.String())
				Expect(err).NotTo(HaveOccurred())
				prev = next
			}

			got, err := env.parentLocResolver.ResolveParentLocation(ctx, "object", prev)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil(), "depth bound (20) MUST be enforced")
		})

		It("R9: unknown parent_type returns nil", func() {
			got, err := env.parentLocResolver.ResolveParentLocation(context.Background(), "exit", ulid.Make())
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
		})
	})

	// holomush-72ou: regression lock for Service.ListPropertiesByParent's
	// per-property filter loop. Exercises the real ABAC engine end-to-end
	// to prove the visible subset returned is correct.
	Describe("Service.ListPropertiesByParent filter (holomush-72ou)", func() {
		It("F1: returns only properties the principal can read", func() {
			// 3 props on char2 (co-located with char1 at loc1):
			//   - public → visible to char1
			//   - private-owned-by-char2 → invisible to char1
			//   - private-owned-by-other → invisible to char1
			public := insertProperty("character", charID2, "bio", "x", "public", nil, nil, nil)
			_ = insertProperty("character", charID2, "diary", "x", "private", &charID2, nil, nil)
			otherPlayer := core.NewULID()
			_, err := env.pool.Exec(context.Background(),
				`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'h')`,
				otherPlayer.String(), "p_"+otherPlayer.String())
			Expect(err).NotTo(HaveOccurred())
			otherChar := core.NewULID()
			_, err = env.pool.Exec(context.Background(),
				`INSERT INTO characters (id, player_id, name, location_id) VALUES ($1, $2, 'Other', $3)`,
				otherChar.String(), otherPlayer.String(), locID1.String())
			Expect(err).NotTo(HaveOccurred())
			_ = insertProperty("character", charID2, "thirdparty", "x", "private", &otherChar, nil, nil)

			got, err := env.worldService.ListPropertiesByParent(context.Background(),
				"character:"+charID1.String(), "character", charID2)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(1))
			Expect(got[0].ID).To(Equal(public))
		})

		It("F2: returns empty list when zero properties are visible (no error)", func() {
			// char1 at loc1; targetID at loc2 (different location — charID2 is at loc1 per outer BeforeEach).
			// All props on charID2 are private with charID2 as owner.
			_ = insertProperty("character", charID2, "p1", "x", "private", &charID2, nil, nil)
			_ = insertProperty("character", charID2, "p2", "x", "private", &charID2, nil, nil)

			// Use a fresh player+char at loc2 so they're not co-located with char1.
			remotePlayer := core.NewULID()
			_, err := env.pool.Exec(context.Background(),
				`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'h')`,
				remotePlayer.String(), "remote_"+remotePlayer.String())
			Expect(err).NotTo(HaveOccurred())
			remoteChar := core.NewULID()
			_, err = env.pool.Exec(context.Background(),
				`INSERT INTO characters (id, player_id, name, location_id) VALUES ($1, $2, 'Remote', $3)`,
				remoteChar.String(), remotePlayer.String(), locID2.String())
			Expect(err).NotTo(HaveOccurred())
			_ = insertProperty("character", remoteChar, "hidden", "x", "private", &remoteChar, nil, nil)

			got, err := env.worldService.ListPropertiesByParent(context.Background(),
				"character:"+charID1.String(), "character", remoteChar)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("F3: returns empty list when parent has no properties", func() {
			got, err := env.worldService.ListPropertiesByParent(context.Background(),
				"character:"+charID1.String(), "character", charID2)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeEmpty())
		})

		It("F4: infra-failure path is covered by Task 3 unit test", func() {
			Skip("F4 covered by the Task 3 unit test TestService_ListPropertiesByParent. " +
				"The integration variant requires mock-injection at the repo layer, " +
				"which the access-suite real-stack pattern does not support. " +
				"See spec INV-2b for the unit-test coverage reference.")
		})

		It("F5: restricted+excluded_from beats visible_to (deny-overrides)", func() {
			// char1 in BOTH visible_to and excluded_from → forbid wins.
			// Raw-SQL insertProperty bypasses worldpg.validateVisibilityLists;
			// this test exercises the filter-loop's deny-override path, not the
			// production write-path overlap validator.
			_ = insertProperty("character", charID2, "split", "x", "restricted",
				nil, []ulid.ULID{charID1}, []ulid.ULID{charID1})
			// Also insert a public prop so there's something to return.
			public := insertProperty("character", charID2, "bio", "x", "public", nil, nil, nil)

			got, err := env.worldService.ListPropertiesByParent(context.Background(),
				"character:"+charID1.String(), "character", charID2)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(1))
			Expect(got[0].ID).To(Equal(public),
				"the restricted prop with char1 in excluded_from MUST be filtered out (forbid wins)")
		})
	})
})

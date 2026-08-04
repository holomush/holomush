// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
	"context"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

// cleanupCharacterLifecycle empties the tables these specs touch.
//
// The ORDER mirrors test/integration/access/seed_policies_test.go's BeforeEach,
// including its documented reason: entity_properties references characters,
// locations and objects through parent_id with the relationship enforced at the
// APPLICATION layer (there is no FK — RESEARCH P-11), so it is deleted BEFORE
// characters to avoid leaving orphaned property rows behind. objects likewise
// precedes characters/locations because its held_by_character_id and location_id
// FKs are ON DELETE SET NULL and would otherwise clear all three containment
// fields on a leftover object, violating chk_exactly_one_containment.
func cleanupCharacterLifecycle(ctx context.Context) {
	GinkgoHelper()
	for _, stmt := range []string{
		// locations.owner_id references characters, so a scene left owned by an
		// earlier spec's character blocks the characters delete below. Clear the
		// back-reference first rather than reordering: characters must still be
		// deleted before locations for characters.location_id.
		"UPDATE locations SET owner_id = NULL",
		"DELETE FROM outbox",
		"DELETE FROM scene_participants",
		"DELETE FROM sessions",
		"DELETE FROM player_character_bindings",
		"DELETE FROM exits",
		"DELETE FROM objects",
		"DELETE FROM entity_properties",
		"DELETE FROM characters",
		"DELETE FROM players",
		"DELETE FROM locations",
	} {
		_, err := env.pool.Exec(ctx, stmt)
		Expect(err).NotTo(HaveOccurred(), stmt)
	}
}

// seedLifecycleLocation inserts a location row and returns its id.
func seedLifecycleLocation(ctx context.Context) ulid.ULID {
	GinkgoHelper()
	locID := core.NewULID()
	_, err := env.pool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy)
		VALUES ($1, 'Lifecycle Hall', 'Where lifecycle specs live.', 'persistent', 'last:0')`,
		locID.String())
	Expect(err).NotTo(HaveOccurred())
	return locID
}

// seedLifecyclePlayer inserts a player row and returns its id. isGuest drives
// players.is_guest, which the guest-reaping path requires.
func seedLifecyclePlayer(ctx context.Context, isGuest bool) ulid.ULID {
	GinkgoHelper()
	playerID := core.NewULID()
	_, err := env.pool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, is_guest)
		VALUES ($1, $2, 'test_hash', $3)`,
		playerID.String(), "lifecycle_"+playerID.String(), isGuest)
	Expect(err).NotTo(HaveOccurred())
	return playerID
}

// insertCharacterWithStatus INSERTs a character row directly, carrying the given
// lifecycle status verbatim. Direct SQL is the point rather than a shortcut: the
// 'idle' value ships in v0.13 with NO transition into it, so no fixture built on
// the production write path can reach that state — and a value no test can reach
// is a value no test covers.
func insertCharacterWithStatus(ctx context.Context, playerID, locID ulid.ULID, name, status string) ulid.ULID {
	GinkgoHelper()
	charID := core.NewULID()
	_, err := env.pool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, description, location_id, status)
		VALUES ($1, $2, $3, '', $4, $5)`,
		charID.String(), playerID.String(), name, locID.String(), status)
	Expect(err).NotTo(HaveOccurred())
	return charID
}

var _ = Describe("Character lifecycle columns", func() {
	var ctx context.Context
	var locID, playerID ulid.ULID

	BeforeEach(func() {
		ctx = context.Background()
		cleanupCharacterLifecycle(ctx)
		locID = seedLifecycleLocation(ctx)
		playerID = seedLifecyclePlayer(ctx, false)
	})

	Describe("full-entity reads carry the lifecycle columns", func() {
		It("reads a retired row back as StatusRetired through Get, GetByLocation and ListByPlayer", func() {
			charID := insertCharacterWithStatus(ctx, playerID, locID, "Retired Reader", "retired")

			got, err := env.Characters.Get(ctx, charID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(world.StatusRetired))
			Expect(got.LastActiveAt).To(Equal(world.NeverActive))

			atLocation, err := env.Characters.GetByLocation(ctx, locID, world.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(atLocation).To(HaveLen(1))
			Expect(atLocation[0].Status).To(Equal(world.StatusRetired))
			Expect(atLocation[0].LastActiveAt).To(Equal(world.NeverActive))

			owned, err := env.Characters.ListByPlayer(ctx, playerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(owned).To(HaveLen(1))
			Expect(owned[0].Status).To(Equal(world.StatusRetired))
			Expect(owned[0].LastActiveAt).To(Equal(world.NeverActive))
		})

		// ListByPlayer is the ONLY read CoreServer.SelectCharacter performs, so a
		// blank status there would put every selection decision on the call site's
		// fail-closed branch instead of the lifecycle column (B-21).
		It("reads an active row back as StatusActive through ListByPlayer, never blank", func() {
			insertCharacterWithStatus(ctx, playerID, locID, "Active Reader", "active")

			owned, err := env.Characters.ListByPlayer(ctx, playerID)
			Expect(err).NotTo(HaveOccurred())
			Expect(owned).To(HaveLen(1))
			Expect(owned[0].Status).To(Equal(world.StatusActive))
			Expect(string(owned[0].Status)).NotTo(BeEmpty())
		})

		It("reads an idle row back as StatusIdle even though nothing can write one", func() {
			charID := insertCharacterWithStatus(ctx, playerID, locID, "Idle Reader", "idle")

			got, err := env.Characters.Get(ctx, charID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(world.StatusIdle))
		})
	})

	Describe("the id/name projections stay projections", func() {
		// ListAll is a deliberate id+name projection. Its elements carry a ZERO
		// Status because the column is not read — not because the row says
		// 'active'. Asserting that here is what keeps the exhaustive-read rule
		// from being quietly satisfied by a zero-valued Status somewhere else.
		//
		// No matching assertion exists for GetNamesByIDs: it returns
		// map[ulid.ULID]string, so there is no Character to inspect and a zero
		// Status is not merely redundant there, it does not compile. Its half of
		// the rule is discharged by its doc comment and by its column list being
		// unchanged.
		It("returns a zero Status from ListAll for every element", func() {
			insertCharacterWithStatus(ctx, playerID, locID, "Directory One", "active")
			insertCharacterWithStatus(ctx, playerID, locID, "Directory Two", "retired")

			all, err := env.Characters.ListAll(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(all).To(HaveLen(2))
			for _, c := range all {
				Expect(string(c.Status)).To(BeEmpty(),
					"ListAll is an id/name projection and MUST NOT carry lifecycle state")
				Expect(c.LastActiveAt).To(Equal(world.NeverActive))
			}
		})
	})
})

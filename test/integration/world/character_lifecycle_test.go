// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package world_test

import (
	"context"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
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

// seedLifecycleSession creates a player session for playerID and returns the
// RAW token SelectCharacter expects. The row is written through the production
// player-session store so resolvePlayerSession's lookup path is the real one.
func seedLifecycleSession(ctx context.Context, playerID ulid.ULID) string {
	GinkgoHelper()
	rawToken := "lifecycle-token-" + core.NewULID().String()
	ps, err := auth.NewPlayerSession(playerID, auth.HashSessionToken(rawToken), "", "", auth.PlayerSessionTTL)
	Expect(err).NotTo(HaveOccurred())
	Expect(env.playerSessionStore.Create(ctx, ps)).To(Succeed())
	return rawToken
}

var _ = Describe("INV-WORLD-5: lifecycle-exhaustive character selection", func() {
	var ctx context.Context
	var locID, playerID ulid.ULID
	var token string

	BeforeEach(func() {
		ctx = context.Background()
		cleanupCharacterLifecycle(ctx)
		locID = seedLifecycleLocation(ctx)
		playerID = seedLifecyclePlayer(ctx, false)
		token = seedLifecycleSession(ctx, playerID)
	})

	// Verifies: INV-WORLD-5
	//
	// The assertion drives CoreServer.SelectCharacter — the ONLY character
	// selection path in the tree — rather than re-invoking the selectability
	// predicate in the spec. A spec that called the predicate itself would prove
	// the predicate and pass with the handler wiring entirely absent, which is
	// the false-green the registry's binding ratchet exists to catch. That is
	// why this file names the predicate nowhere.
	//
	// The 'idle' fixture is INSERTed directly because v0.13 ships that value
	// with no transition into it: a value no test can reach is a value no test
	// covers. Each denial is paired on the same fixture with an 'active' control
	// that IS selected, so a green exclusion cannot mean selection is simply
	// broken for everyone.
	It("excludes idle and retired characters from selection while selecting active ones", func() {
		idleID := insertCharacterWithStatus(ctx, playerID, locID, "Idle Candidate", "idle")
		retiredID := insertCharacterWithStatus(ctx, playerID, locID, "Retired Candidate", "retired")
		activeID := insertCharacterWithStatus(ctx, playerID, locID, "Active Candidate", "active")

		idleResp, err := env.coreServer.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
			PlayerSessionToken: token,
			CharacterId:        idleID.String(),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(idleResp.GetSuccess()).To(BeFalse(),
			"a character constructed directly in the otherwise-unreachable idle state MUST be excluded from selection")

		retiredResp, err := env.coreServer.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
			PlayerSessionToken: token,
			CharacterId:        retiredID.String(),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(retiredResp.GetSuccess()).To(BeFalse(),
			"a retired character MUST be excluded by the SAME code path, with no second predicate")

		activeResp, err := env.coreServer.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
			PlayerSessionToken: token,
			CharacterId:        activeID.String(),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(activeResp.GetSuccess()).To(BeTrue(),
			"the paired active control MUST still be selectable — otherwise the exclusions above prove nothing")
		Expect(activeResp.GetCharacterName()).To(Equal("Active Candidate"))
	})
})

var _ = Describe("INV-WORLD-6: retirement preserves the name reservation", func() {
	var ctx context.Context
	var locID, playerID ulid.ULID
	var adminSubject string

	BeforeEach(func() {
		ctx = context.Background()
		cleanupCharacterLifecycle(ctx)
		locID = seedLifecycleLocation(ctx)
		*env.startLocationID = locID
		playerID = seedLifecyclePlayer(ctx, false)

		// The delete below is authorized by the REAL engine against the REAL
		// seeded corpus: seed.go's admin permit
		// (`permit(principal is character, action, resource) when { "admin" in
		// principal.character.roles }`). The role source is the fixture; the
		// evaluation is not.
		adminPlayerID := seedLifecyclePlayer(ctx, false)
		adminID := insertCharacterWithStatus(ctx, adminPlayerID, locID, "Lifecycle Admin", "active")
		adminSubject = access.CharacterSubject(adminID.String())
		env.roleResolver.Grant(adminSubject, "admin")
	})

	// nameIsTaken reports whether the real creation path refuses the name.
	// The reclaim attempt goes through auth.CharacterService.Create so the
	// assertion is about the system's behaviour, not a hand-written query.
	reclaim := func(name string) error {
		_, err := env.characters.Create(ctx, playerID, name)
		return err
	}

	// Verifies: INV-WORLD-6
	//
	// The paired form 01-SPEC.md §13 requires: retire-then-reclaim FAILS, and
	// BOTH sanctioned tombstone-emitting hard-delete paths release the name.
	// Asserting only the retire half would pin one side of the boundary and
	// leave "does anything ever release it?" unanswered; asserting only
	// world.Service.DeleteCharacter would leave the registry entry claiming an
	// enumeration of two while proving one.
	It("keeps the name reserved through retirement and releases it on both sanctioned deletes", func() {
		By("retiring a character leaves its row, its name and the reservation intact")
		retiredID := insertCharacterWithStatus(ctx, playerID, locID, "Persistent Name", "active")
		_, err := env.pool.Exec(ctx, `UPDATE characters SET status = 'retired' WHERE id = $1`, retiredID.String())
		Expect(err).NotTo(HaveOccurred())

		stillThere, err := env.Characters.Get(ctx, retiredID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stillThere.Status).To(Equal(world.StatusRetired))
		Expect(stillThere.Name).To(Equal("Persistent Name"))

		Expect(reclaim("Persistent Name")).To(HaveOccurred(),
			"retirement MUST NOT release the name — a freed name inherits every display name already denormalized into immutable payloads")

		By("the irreversible world.Service.DeleteCharacter DOES release it")
		Expect(env.worldService.DeleteCharacter(ctx, adminSubject, retiredID)).To(Succeed())
		Expect(reclaim("Persistent Name")).NotTo(HaveOccurred(),
			"the sanctioned hard delete MUST release the name — this is the paired half that pins the boundary")

		By("guest reaping, the SECOND sanctioned deleter, releases it too")
		guestPlayerID := seedLifecyclePlayer(ctx, true)
		insertCharacterWithStatus(ctx, guestPlayerID, locID, "Reaped Name", "active")
		Expect(reclaim("Reaped Name")).To(HaveOccurred(),
			"control: the guest's name is reserved while its row exists")

		Expect(env.reaping.DeleteGuestPlayer(ctx, guestPlayerID)).To(Succeed())
		Expect(reclaim("Reaped Name")).NotTo(HaveOccurred(),
			"auth.CharacterReapingService is the second name-releasing path INV-WORLD-6 enumerates")
	})
})

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

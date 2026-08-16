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

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/testsupport/chartest"
)

// This file pins ONE property of the shipped corpus: the viewer read path never
// widens a character-keyed grant into a grant to the player behind it.
//
// It is a REGRESSION PIN, not a red-first gate. Every assertion below is green
// on the day it lands, because the property it asserts is one Phase 2 already
// shipped (02-CONTEXT.md D-27). What it defends against is a future one-line
// "simplification": resolveDerivedPlayerPeers computes the permit-side peers in
// the ALL direction and the forbid-side peer in the ANY direction, an asymmetry
// that reads like an oversight to anyone who has not read D-27. Flattening the
// permit side to ANY for symmetry compiles, passes every other test in the
// repo, and silently broadcasts alt linkage across every character a player
// owns. The plan's SUMMARY carries the verbatim failure that flip produces.
//
// WHY THE TWO SPECS DIFFER ONLY IN CHARACTER COUNT. entity_properties.owner is
// a SCALAR column, so "every character of this player appears in the row's
// owner field" reduces to "the owning character is that player's only
// character". A player with one character therefore DOES reach their own
// private row through a viewer principal, and a player with two does NOT. That
// contrast is the whole evidence that the denial comes from the derivation
// DIRECTION rather than from a blanket viewer-side denial, an empty fixture or
// a broken harness — so the two specs seed the identical row shape and vary
// nothing but the roster size.

var _ = Describe("INV-ACCESS-15: the viewer read path never widens a character-keyed grant to the player", func() {
	var (
		// The two-alt player and its roster. The private row hangs off
		// twoAltFirst; twoAltSecond exists only to make the roster size two.
		twoAltPlayer ulid.ULID
		twoAltFirst  ulid.ULID
		twoAltSecond ulid.ULID

		// The single-alt player and its one character, seeded identically.
		soloPlayer ulid.ULID
		soloChar   ulid.ULID

		hall ulid.ULID
	)

	// seedPlayerWithCharacters inserts one player and n characters owned by it,
	// all standing in hall. It is the ONLY difference between the two fixtures
	// below: same table, same columns, same values, different n.
	seedPlayerWithCharacters := func(ctx context.Context, label string, n int) (ulid.ULID, []ulid.ULID) {
		playerID := core.NewULID()
		_, err := env.pool.Exec(ctx, `
			INSERT INTO players (id, username, password_hash)
			VALUES ($1, $2, 'hash')`,
			playerID.String(), label+"_"+time.Now().Format("150405.000000"))
		Expect(err).NotTo(HaveOccurred())

		chars := make([]ulid.ULID, 0, n)
		for i := 0; i < n; i++ {
			charID := core.NewULID()
			name := uniqueCharFixtureName(label, charID)
			_, err = env.pool.Exec(ctx, `
				INSERT INTO characters (id, player_id, name, location_id, normalized_name, name_skeleton, name_skeleton_unicode_version)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				append([]any{charID.String(), playerID.String(), name, hall.String()},
					chartest.Columns(name)...)...)
			Expect(err).NotTo(HaveOccurred())
			chars = append(chars, charID)
		}
		return playerID, chars
	}

	BeforeEach(func() {
		ctx := context.Background()

		// Cleanup order is seed_policies_test.go's, for its documented reasons:
		// bindings first; objects before characters/locations because their
		// containment FKs are ON DELETE SET NULL and would violate
		// chk_exactly_one_containment; entity_properties before characters and
		// locations because it references them by parent_id at the APPLICATION
		// layer with no FK, so nothing cascades it.
		for _, table := range []string{
			"player_character_bindings",
			"objects",
			"entity_properties",
			"characters",
			"players",
			"locations",
		} {
			_, err := env.pool.Exec(ctx, "DELETE FROM "+table)
			Expect(err).NotTo(HaveOccurred())
		}

		hall = core.NewULID()
		_, err := env.pool.Exec(ctx, `
			INSERT INTO locations (id, name, description, type, replay_policy)
			VALUES ($1, 'Linkage Hall', 'A hall.', 'persistent', 'last:0')`,
			hall.String())
		Expect(err).NotTo(HaveOccurred())

		var twoAlt []ulid.ULID
		twoAltPlayer, twoAlt = seedPlayerWithCharacters(ctx, "TwoAlt", 2)
		twoAltFirst, twoAltSecond = twoAlt[0], twoAlt[1]

		var solo []ulid.ULID
		soloPlayer, solo = seedPlayerWithCharacters(ctx, "Solo", 1)
		soloChar = solo[0]

		env.auditWriter.Reset()
	})

	// Verifies: INV-ACCESS-15
	It("L1: a two-alt player reading through their viewer principal does NOT receive a private row owned by one of their own characters", func() {
		privateID := insertProperty("character", twoAltFirst, "profile.secret_note", "hush", "private", &twoAltFirst, nil, nil)

		viewer := access.ViewerSubject(access.ViewerTierPlayer, twoAltPlayer.String())

		Expect(evalAccess(viewer, "read", access.PropertyResource(privateID.String())).Effect()).
			To(Equal(types.EffectDefaultDeny),
				"the permit-side owner peer resolves in the ALL direction: twoAltPlayer holds a second character the row never named, so no player-keyed owner is emitted and the viewer twin cannot match")

		// The row is not simply unreadable by everyone: its own owning
		// CHARACTER still reads it through the grid-side twin, which is exactly
		// the path the owner audience uses (internal/grpc/characteraccess_owner.go).
		// Without this leg the denial above could equally be an unreadable
		// fixture.
		Expect(evalAccess(access.CharacterSubject(twoAltFirst.String()), "read", access.PropertyResource(privateID.String())).Effect()).
			To(Equal(types.EffectAllow),
				"seed:property-private-read still permits the owning character — the audience split, not a blanket denial")

		// And the SECOND alt gets nothing, which is the disclosure the ALL
		// direction exists to prevent: a grant made to a named character must
		// not become reachable from that player's other characters.
		Expect(evalAccess(access.CharacterSubject(twoAltSecond.String()), "read", access.PropertyResource(privateID.String())).Effect()).
			To(Equal(types.EffectDefaultDeny),
				"a row owned by one character is not readable from its player's other character")
	})

	It("L2: the SAME viewer principal does receive a PUBLIC row on the SAME character — the denial is the derived peer, not a dead harness", func() {
		publicID := insertProperty("character", twoAltFirst, "profile.pronouns", "they/them", "public", nil, nil, nil)

		viewer := access.ViewerSubject(access.ViewerTierPlayer, twoAltPlayer.String())

		Expect(evalAccess(viewer, "read", access.PropertyResource(publicID.String())).Effect()).
			To(Equal(types.EffectAllow),
				"seed:viewer-property-public-read carries no owner clause, so a two-alt viewer reads a public row normally — L1's denial is specific to the private twin")
	})

	It("L3: a single-alt player DOES receive their own private row through the same viewer principal", func() {
		// Identical row shape to L1: same parent kind, same name, same value,
		// same visibility, same owner-is-the-parent-character relationship. The
		// ONLY difference between this spec and L1 is that soloPlayer holds one
		// character and twoAltPlayer holds two.
		privateID := insertProperty("character", soloChar, "profile.secret_note", "hush", "private", &soloChar, nil, nil)

		viewer := access.ViewerSubject(access.ViewerTierPlayer, soloPlayer.String())

		Expect(evalAccess(viewer, "read", access.PropertyResource(privateID.String())).Effect()).
			To(Equal(types.EffectAllow),
				"for a single-character player the ALL direction is satisfied, so owner_player_id IS emitted and seed:viewer-property-private-read matches — the two-alt denial is a property of the DIRECTION, not a blanket viewer denial")
	})
})

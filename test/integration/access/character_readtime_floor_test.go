// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package access_test

import (
	"context"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/access/policy"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/access/profilevis"
	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/core"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/testsupport/chartest"
	"github.com/holomush/holomush/internal/world"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// seededGuestFloorPolicyName is the shipped term-A tier floor for the guest rung
// (internal/access/policy/seed.go, "seed:profile-tier-floor-guest"). This spec
// excludes it and substitutes a one-name stand-in whose CLEARING SET is the only
// thing it varies between the two reads.
const seededGuestFloorPolicyName = "seed:profile-tier-floor-guest"

// mutableFloorPolicyName is the stand-in. It governs exactly one attribute name,
// so moving its clearing set moves that one attribute and nothing else — which
// is what lets the untouched-second-character control mean something.
const mutableFloorPolicyName = "test:profile-tier-floor-biography-mutable"

// readtimeBiographySentinel is the value whose PRESENCE flips between the two
// reads. Long, hyphenated and uppercase so a match is the seeded value rather
// than framing or a field name, and distinct from
// integrationBiographySentinel so a cross-file fixture leak is visible.
const readtimeBiographySentinel = "SENTINEL-READTIME-FLOOR-BIOGRAPHY-4f1ad0c2"

// readtimePronounsSentinel rides on the SECOND character, at the untouched
// anonymous floor. It is the targeting control: a mutation that moved every
// viewer rung rather than one attribute's floor would change this too.
const readtimePronounsSentinel = "they/them-READTIME-CONTROL-8ce27b19"

// biographyFloorPolicy builds the stand-in term-A floor over an explicit
// clearing SET — never an ordinal bound. §8.2.1 forbids comparing the tier token
// ordinally, and TestNoTierFloorPolicyUsesAnOrdinalTierComparison
// (internal/access/policy/seed_profile_visibility_test.go:260) already pins that
// for the shipped corpus; this helper follows the same shape so the spec is not
// the one place a `>=` creeps in.
func biographyFloorPolicy(rungs ...string) *policystore.StoredPolicy {
	quoted := make([]string, len(rungs))
	for i, rung := range rungs {
		quoted[i] = `"` + rung + `"`
	}
	return &policystore.StoredPolicy{
		ID:      core.NewULID().String(),
		Name:    mutableFloorPolicyName,
		Effect:  types.PolicyEffectPermit,
		Source:  "admin",
		Enabled: true,
		DSLText: `permit(principal is viewer, action in ["read_profile_attribute"], resource is property) when { principal.viewer.tier in [` +
			strings.Join(quoted, ", ") +
			`] && resource.property.name in ["profile.biography"] };`,
	}
}

// PROFILE-01 / ROADMAP criterion 4. The property under test is that the
// viewer-tier floor is consulted DURING the read, so a change to the game's
// viewer-tier configuration changes what the same logged-out visitor is shown
// without anything being written to the character or to its property rows.
//
// WHAT THIS SPEC DELIBERATELY DOES NOT CLAIM. It says nothing about WHEN a
// corpus edit becomes visible. The compiled snapshot is refreshed by a poller
// whose interval defaults to ten seconds when unset
// (internal/access/policy/poller.go:95-96), so propagation is bounded by that
// interval, not by the next request. This spec drives policy.Cache.Reload
// explicitly for exactly that reason: the property it can honestly prove is
// READ-TIME EVALUATION — that nothing is stamped and no backfill exists — and a
// sleep-and-poll shape would prove a latency the system does not offer.
//
// ALREADY PROVEN ELSEWHERE, CITED HERE RATHER THAN REBUILT (rule 7zy1161fh1):
//   - clearing a floor is set membership, never ordinal —
//     TestNoTierFloorPolicyUsesAnOrdinalTierComparison
//     (internal/access/policy/seed_profile_visibility_test.go:260) and
//     TestASyntheticFourthRungClearsNeitherShippedTierFloor
//     (internal/access/profilevis/tierfloor_test.go:173);
//   - the floor is evaluated twice per attribute, separated by the action token —
//     TestAttributeVisibleIssuesExactlyTwoEvaluationsSeparatedByTheActionToken
//     (internal/access/profilevis/profilevis_test.go:113);
//   - an infrastructure failure in that evaluation resolves DENY —
//     TestVisibleAttributesAbortsTheWholeCallWhenAnyEvaluationFails
//     (internal/access/profilevis/profilevis_test.go:344).
//
// Verifies: INV-ACCESS-10
var _ = Describe("PROFILE-01: the viewer-tier floor is evaluated at read time and never stamped onto a row", func() {
	const anonToken = ""

	var (
		charID    ulid.ULID
		otherID   ulid.ULID
		locID     ulid.ULID
		psRepo    auth.PlayerSessionRepository
		playerRep auth.PlayerRepository
		corpus    *profileCorpusStore
		cache     *policy.Cache
		srv       *holoGRPC.CharacterAccessServer
	)

	// newServer is the same constructor every character spec in this package
	// drives, wired to the same production adapters cmd/holomush passes.
	newServer := func(engine *policy.Engine) *holoGRPC.CharacterAccessServer {
		worldSvc := world.NewService(world.ServiceConfig{
			CharacterRepo: env.charRepo,
			PropertyRepo:  env.propRepo,
			Engine:        engine,
		})
		return holoGRPC.NewCharacterAccessServer(
			worldSvc,
			worldSvc,
			&profilevis.Evaluator{Engine: engine},
			engine,
			setup.NewCharRepoAdapter(env.pool, env.charRepo),
			psRepo,
			playerRep,
			setup.NewCharRepoAdapter(env.pool, env.charRepo),
			// No spec here seats a character, so the create pipeline fails the
			// spec rather than answering with a zero value.
			failOnCallCreator{},
		)
	}

	read := func(id string) map[string]string {
		resp, err := srv.GetCharacterProfile(context.Background(), &characteraccessv1.GetCharacterProfileRequest{
			CharacterId:        id,
			PlayerSessionToken: anonToken,
		})
		Expect(err).NotTo(HaveOccurred())
		return resp.GetCharacter().GetProfile()
	}

	// characterVersion and propertyRows are the discriminating observations. A
	// stamped floor — one cached onto entity_properties.visibility or onto the
	// character row when the corpus changed — would have to move one of them to
	// change the answer, so asserting both are byte-identical across the two
	// reads is what separates "the floor is read at read time" from "the anonymous
	// read happens to omit field X", which is true in the shipped state either way.
	characterVersion := func(ctx context.Context, id ulid.ULID) int32 {
		var v int32
		Expect(env.pool.QueryRow(ctx, `SELECT version FROM characters WHERE id = $1`, id.String()).
			Scan(&v)).To(Succeed())
		return v
	}

	propertyRows := func(ctx context.Context, id ulid.ULID) []string {
		rows, err := env.pool.Query(ctx, `
			SELECT id, name, value, visibility
			FROM entity_properties
			WHERE parent_id = $1
			ORDER BY name, id`, id.String())
		Expect(err).NotTo(HaveOccurred())
		defer rows.Close()

		var out []string
		for rows.Next() {
			var rowID, name, value, visibility string
			Expect(rows.Scan(&rowID, &name, &value, &visibility)).To(Succeed())
			out = append(out, strings.Join([]string{rowID, name, value, visibility}, "\x1f"))
		}
		Expect(rows.Err()).NotTo(HaveOccurred())
		return out
	}

	BeforeEach(func() {
		ctx := context.Background()

		// Cleanup order is seed_policies_test.go's, for its documented reasons.
		for _, table := range []string{
			"player_character_bindings",
			"objects",
			"entity_properties",
			"characters",
			"player_sessions",
			"players",
			"locations",
		} {
			_, err := env.pool.Exec(ctx, "DELETE FROM "+table)
			Expect(err).NotTo(HaveOccurred())
		}

		psRepo = store.NewPostgresPlayerSessionStore(env.pool)
		playerRep = authpg.NewPlayerRepository(env.pool)

		locID = core.NewULID()
		_, err := env.pool.Exec(ctx, `
			INSERT INTO locations (id, name, description, type, replay_policy)
			VALUES ($1, 'Read-Time Hall', 'A hall.', 'persistent', 'last:0')`,
			locID.String())
		Expect(err).NotTo(HaveOccurred())

		ownerID := core.NewULID()
		Expect(playerRep.Create(ctx, &auth.Player{
			ID:           ownerID,
			Username:     "owner_" + ownerID.String()[20:],
			PasswordHash: "hash",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		})).To(Succeed())

		insertChar := func(id ulid.ULID, name, description string) {
			_, insErr := env.pool.Exec(ctx, `
				INSERT INTO characters (id, player_id, name, description, location_id, normalized_name, name_skeleton, name_skeleton_unicode_version)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				append([]any{id.String(), ownerID.String(), name, description, locID.String()},
					chartest.Columns(name)...)...)
			Expect(insErr).NotTo(HaveOccurred())
		}

		charID = core.NewULID()
		insertChar(charID, uniqueCharFixtureName("Subject", charID), "A tall figure wrapped in a travelling cloak.")

		otherID = core.NewULID()
		insertChar(otherID, uniqueCharFixtureName("Bystander", otherID), "A bystander.")

		// The subject row sits at the stand-in floor; the control row sits at the
		// untouched anonymous floor on a DIFFERENT character.
		insertProperty("character", charID, "profile.biography", readtimeBiographySentinel, "public", nil, nil, nil)
		insertProperty("character", otherID, "profile.pronouns", readtimePronounsSentinel, "public", nil, nil, nil)

		// The corpus under test: the shipped guest floor swapped for a stand-in
		// governing one name. Corpus A places that name at the guest rung, which
		// is where §8.6 seeds it, so read A is the shipped answer.
		var engine *policy.Engine
		engine, cache, corpus = newReloadableCorpusEngine(ctx,
			[]string{seededGuestFloorPolicyName},
			biographyFloorPolicy("guest", "player"))

		// The server is built ONCE, over that engine. Nothing below rebuilds it,
		// so the corpus is the only variable between the two reads.
		srv = newServer(engine)
		env.auditWriter.Reset()
	})

	Describe("a change to the game's viewer-tier configuration changes the logged-out answer with no write to any row", func() {
		It("R1: the same anonymous viewer, the same server and the same rows yield a different field set after the floor moves", func() {
			ctx := context.Background()

			versionBefore := characterVersion(ctx, charID)
			rowsBefore := propertyRows(ctx, charID)
			Expect(rowsBefore).To(HaveLen(1), "the subject must own exactly the one seeded biography row")

			// Read A — the attribute sits at the guest rung, so the anonymous
			// viewer does not clear it.
			before := read(charID.String())
			Expect(before).NotTo(HaveKey("profile.biography"),
				"at the guest rung the anonymous viewer must not reach the biography")

			// The targeting control, read under the SAME corpus: a second
			// character whose only row sits at the untouched anonymous floor.
			otherBefore := read(otherID.String())
			Expect(otherBefore).To(HaveKeyWithValue("profile.pronouns", readtimePronounsSentinel),
				"the control row sits at the untouched anonymous floor and must be readable to begin with")

			// The configuration change, and NOTHING else: the same policy name,
			// the same governed attribute, one more rung in the clearing set.
			corpus.appended = []*policystore.StoredPolicy{
				biographyFloorPolicy("anonymous", "guest", "player"),
			}
			Expect(cache.Reload(ctx)).To(Succeed())

			// Read B — the SAME viewer, the SAME server value, the SAME rows.
			after := read(charID.String())
			Expect(after).To(HaveKeyWithValue("profile.biography", readtimeBiographySentinel),
				"once the anonymous rung clears the floor the same viewer reaches the same untouched row")

			// The two field sets DIFFER, which is the whole claim.
			Expect(after).NotTo(Equal(before))

			// The control is UNCHANGED, so the mutation moved one attribute's
			// floor rather than every rung.
			Expect(read(otherID.String())).To(Equal(otherBefore),
				"the mutation names one attribute; a change here would mean it moved every rung rather than one floor")

			// And nothing was written. A floor stamped onto a row — or onto the
			// character — would have had to move one of these to move the answer.
			Expect(characterVersion(ctx, charID)).To(Equal(versionBefore),
				"characters.version must be untouched: the floor is evaluated, not stamped")
			Expect(propertyRows(ctx, charID)).To(Equal(rowsBefore),
				"every (id, name, value, visibility) tuple must be untouched: there is no backfill")
		})
	})
})

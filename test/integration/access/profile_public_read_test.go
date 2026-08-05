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
	"github.com/holomush/holomush/internal/access/policy"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/testsupport/chartest"
)

// widenedPolicyName is the single seed entry this spec proves is load-bearing.
//
// ONE name, not two. An earlier revision of plan 02-10 excluded a second
// `seed:profile-public-read-*` entry alongside it — the character-resource-type
// half — but 02-CONTEXT.md D-29 defers that permit to Phase 4, because it gates
// a projection returning PlayerId and LocationId to every character including
// every guest, so plan 02-07 never seeded it. Its name is deliberately not
// written anywhere in this file: excluding a name that is not in the corpus
// would silently no-op, and the control would then be identical to the full
// corpus while still LOOKING like a control.
//
// excludingPolicyStore guards that directly: it counts what it removed and the
// spec asserts the count, so a name that stops matching fails loudly instead of
// quietly disarming the control.
const widenedPolicyName = "seed:profile-public-read-property"

// excludingPolicyStore is the paired-positive-control corpus: the real seeded
// corpus minus an explicitly named policy.
//
// Cache.Reload reads the corpus through ListEnabled and nothing else, so
// overriding that one method is the whole mechanism; every other PolicyStore
// method is delegated to the embedded real store. The exclusion is BY NAME on
// purpose — a filter written as "drop the last one added" or "drop anything
// matching profile" would drift as the corpus grows, and this control has to
// stay readable for as long as the widening ships.
type excludingPolicyStore struct {
	policystore.PolicyStore

	excluded map[string]bool
	// removed counts what ListEnabled actually dropped on its last call. A
	// control that excludes nothing is not a control, and this is what lets the
	// spec prove it excluded something rather than assuming it.
	removed int
}

func (s *excludingPolicyStore) ListEnabled(ctx context.Context) ([]*policystore.StoredPolicy, error) {
	all, err := s.PolicyStore.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}

	kept := make([]*policystore.StoredPolicy, 0, len(all))
	s.removed = 0
	for _, p := range all {
		if s.excluded[p.Name] {
			s.removed++
			continue
		}
		kept = append(kept, p)
	}
	return kept, nil
}

var _ = Describe("PROFILE-11: the seed:profile-public-read-property widening", func() {
	var (
		readerID ulid.ULID // A — at locFar, deliberately NOT co-located with the subject
		subjectA ulid.ULID // B — the character whose public rows are being read
		neighbor ulid.ULID // C — co-located with B, so the shipped colocation permit still applies
		locNear  ulid.ULID // where B and C stand
		locFar   ulid.ULID // where A stands

		controlEngine *policy.Engine
		controlStore  *excludingPolicyStore
	)

	// evalControl mirrors evalAccess but runs against the control corpus — the
	// same real provider stack, the same fixtures, the widening removed.
	evalControl := func(subject, action, resource string) types.Decision {
		decision, err := controlEngine.Evaluate(env.ctx, types.AccessRequest{
			Subject:  subject,
			Action:   action,
			Resource: resource,
		})
		Expect(err).NotTo(HaveOccurred())
		return decision
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

		locNear = core.NewULID()
		locFar = core.NewULID()
		for _, loc := range []struct {
			id   ulid.ULID
			name string
		}{{locNear, "Near Hall"}, {locFar, "Far Hall"}} {
			_, err := env.pool.Exec(ctx, `
				INSERT INTO locations (id, name, description, type, replay_policy)
				VALUES ($1, $2, 'A hall.', 'persistent', 'last:0')`,
				loc.id.String(), loc.name)
			Expect(err).NotTo(HaveOccurred())
		}

		newChar := func(label string, loc ulid.ULID) ulid.ULID {
			playerID := core.NewULID()
			_, err := env.pool.Exec(ctx, `
				INSERT INTO players (id, username, password_hash)
				VALUES ($1, $2, 'hash')`,
				playerID.String(), label+"_"+time.Now().Format("150405.000000"))
			Expect(err).NotTo(HaveOccurred())

			charID := core.NewULID()
			name := uniqueCharFixtureName(label, charID)
			_, err = env.pool.Exec(ctx, `
				INSERT INTO characters (id, player_id, name, location_id, normalized_name, name_skeleton, name_skeleton_unicode_version)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				append([]any{charID.String(), playerID.String(), name, loc.String()},
					chartest.Columns(name)...)...)
			Expect(err).NotTo(HaveOccurred())
			return charID
		}

		subjectA = newChar("Subject", locNear)
		neighbor = newChar("Neighbor", locNear)
		readerID = newChar("Reader", locFar)

		// The control corpus: everything the real engine has, minus the widening.
		controlStore = &excludingPolicyStore{
			PolicyStore: env.pStore,
			excluded:    map[string]bool{widenedPolicyName: true},
		}
		controlCache := policy.NewCache(controlStore, env.compiler)
		Expect(controlCache.Reload(ctx)).To(Succeed())
		controlEngine = policy.NewEngine(env.resolver, controlCache, &noopSessionResolver{}, env.auditLogger)

		// NON-VACUITY OF THE CONTROL ITSELF. If the name ever stops matching a
		// seeded policy, the control silently becomes a copy of the full corpus
		// and every "denied" assertion below would be asserting nothing. Pin it.
		Expect(controlStore.removed).To(Equal(1),
			"the control corpus must exclude exactly one policy, %q — if this is 0 the name no longer matches anything in the seeded corpus and the control is disarmed", widenedPolicyName)

		env.auditWriter.Reset()
	})

	Describe("the widening permits the off-location read the colocation policy denied", func() {
		It("W1: allows an off-location character to read a public property on another character, and DENIES it without the widening", func() {
			propID := insertProperty("character", subjectA, "profile.pronouns", "they/them", "public", nil, nil, nil)

			reader := access.CharacterSubject(readerID.String())
			resource := access.PropertyResource(propID.String())

			// The widening's whole point.
			Expect(evalAccess(reader, "read", resource).Effect()).
				To(Equal(types.EffectAllow), "the full seeded corpus must permit the off-location read")

			// THE PAIRED POSITIVE CONTROL, and the RED demonstration required by
			// the plan's verification-integrity rule 4: this is the tree exactly
			// as it stood before plan 02-07 seeded the widening. Without it the
			// permit above cannot be distinguished from "the two characters are
			// accidentally co-located" or "the engine permits everything".
			Expect(evalControl(reader, "read", resource).Effect()).
				To(Equal(types.EffectDefaultDeny), "without %s the same read must be denied", widenedPolicyName)
		})

		It("W2: the control corpus is not simply denying everything — it still permits the CO-LOCATED read", func() {
			propID := insertProperty("character", subjectA, "profile.pronouns", "they/them", "public", nil, nil, nil)
			resource := access.PropertyResource(propID.String())

			// seed:property-public-read survives in the control corpus and is
			// still colocation-gated, so C (beside B) is permitted there. This
			// is what proves W1's control denial is specific to the widening
			// rather than an artifact of a broken control engine.
			Expect(evalControl(access.CharacterSubject(neighbor.String()), "read", resource).Effect()).
				To(Equal(types.EffectAllow), "the control corpus must still permit a co-located public read")
		})

		It("W3: the widening is ADDITIVE — the co-located read that already worked still works", func() {
			propID := insertProperty("character", subjectA, "profile.pronouns", "they/them", "public", nil, nil, nil)

			Expect(evalAccess(access.CharacterSubject(neighbor.String()), "read", access.PropertyResource(propID.String())).Effect()).
				To(Equal(types.EffectAllow), "the shipped colocation permit must be undisturbed")
		})
	})

	Describe("the widening's boundary is pinned on both sides", func() {
		It("W4: denies an off-location read of a PRIVATE property, while the same reader may read a public one", func() {
			privateID := insertProperty("character", subjectA, "profile.secret_note", "hush", "private", &subjectA, nil, nil)
			publicID := insertProperty("character", subjectA, "profile.pronouns", "they/them", "public", nil, nil, nil)

			reader := access.CharacterSubject(readerID.String())

			// The denial under test. The widening carries `visibility ==
			// "public"`; drop that clause and this goes RED.
			Expect(evalAccess(reader, "read", access.PropertyResource(privateID.String())).Effect()).
				To(Equal(types.EffectDefaultDeny), "the widening must reach public rows only")

			// Paired positive control on the SAME fixture, per the plan's rule 2:
			// without it, a denial is indistinguishable from a reader, a parent
			// or a corpus that cannot permit anything at all.
			Expect(evalAccess(reader, "read", access.PropertyResource(publicID.String())).Effect()).
				To(Equal(types.EffectAllow), "the same off-location reader must still reach the public row")
		})

		It("W5: denies an off-location read of a public property on a LOCATION parent, while the same reader may read a public one on a character", func() {
			locProp := insertProperty("location", locNear, "profile.pronouns", "n/a", "public", nil, nil, nil)
			charProp := insertProperty("character", subjectA, "profile.pronouns", "they/them", "public", nil, nil, nil)

			reader := access.CharacterSubject(readerID.String())

			// The parent_type guard. Only seed:property-public-read can reach a
			// location-parented row and it stays colocation-gated, so an
			// off-location reader is denied. Drop `parent_type == "character"`
			// from the widening and this goes RED.
			Expect(evalAccess(reader, "read", access.PropertyResource(locProp.String())).Effect()).
				To(Equal(types.EffectDefaultDeny), "the widening must not reach non-character parents")

			Expect(evalAccess(reader, "read", access.PropertyResource(charProp.String())).Effect()).
				To(Equal(types.EffectAllow), "the same reader on a character-parented public row must be permitted")
		})
	})

	Describe("profile reachability is evaluated independently of any per-field result (§8.4.2)", func() {
		It("W6: a character with ZERO property rows still resolves its profile at every viewer rung", func() {
			ctx := context.Background()

			// Establish the fixture is genuinely degenerate in the way the
			// must-have describes: no rows at all for this parent. Asserting
			// reachability without this would prove nothing — a character that
			// happened to own rows would satisfy it just as well.
			var propCount int
			err := env.pool.QueryRow(ctx,
				`SELECT count(*) FROM entity_properties WHERE parent_id = $1`,
				subjectA.String()).Scan(&propCount)
			Expect(err).NotTo(HaveOccurred())
			Expect(propCount).To(Equal(0), "the reachability fixture must own no property rows")

			profile := access.ProfileResource(subjectA.String())
			playerID := core.NewULID().String()

			for _, viewer := range []string{
				access.ViewerSubject(access.ViewerTierAnonymous, ""),
				access.ViewerSubject(access.ViewerTierGuest, playerID),
				access.ViewerSubject(access.ViewerTierPlayer, playerID),
			} {
				Expect(evalAccess(viewer, "read", profile).Effect()).
					To(Equal(types.EffectAllow),
						"seed:profile-reachable must permit %s regardless of any per-field result", viewer)
			}
		})
	})
})

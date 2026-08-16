// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package abactest_test

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/testsupport/abactest"
	"github.com/holomush/holomush/internal/world"
)

// reflectFieldNames returns the exported field names of a struct value.
func reflectFieldNames(v any) []string {
	typ := reflect.TypeOf(v)
	out := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		out = append(out, typ.Field(i).Name)
	}
	return out
}

// --- Schema parity ---

// TestEachDoubleDeclaresExactlyItsRealCounterpartsSchemaKeys is the drift gate.
// A double declaring a key the real provider does not emit — or missing one it
// does — turns every downstream behavioural assertion into a test of the
// double. Symmetric difference, so drift in EITHER direction is RED.
func TestEachDoubleDeclaresExactlyItsRealCounterpartsSchemaKeys(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		double map[string]types.AttrType
		real   *types.NamespaceSchema
	}{
		"viewer": {
			double: abactest.ViewerSchemaKeys,
			real:   attribute.NewViewerTierProvider().Schema(),
		},
		"player": {
			double: abactest.PlayerSchemaKeys,
			real:   attribute.NewPlayerAttributeProvider(nil).Schema(),
		},
		"property": {
			double: abactest.PropertySchemaKeys,
			real:   attribute.NewPropertyProvider(nil, nil, nil).Schema(),
		},
	}

	for namespace, tc := range cases {
		t.Run(namespace, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.real.Attributes, tc.double,
				"the %s double's schema MUST equal the real provider's, key for key AND type for type",
				namespace)
		})
	}
}

// --- Derivation parity (02-CONTEXT.md D-27, cross-AI review C2-12) ---

// TestTheDerivedPeersAgreeWithTheRealPropertyProvider is the reason
// PropertyFixture takes CHARACTER-keyed inputs and a character→player map
// rather than the three derived peers directly.
//
// Schema parity proves only that a key is DECLARED; it says nothing about how
// the value was DERIVED. A double that let a caller set visible_to_players
// directly would let a privacy suite assert D-27 while never exercising it —
// the fixture would simply state whatever answer the test wanted. So the
// double derives, and this test drives ONE fixture through BOTH the double and
// the real attribute.PropertyProvider and asserts the three values agree.
//
// The fixture is the one that SEPARATES the two candidate rules: player P holds
// TWO characters, and only one of them is named in each list. Under D-27's
// directions P is ABSENT from the permit-side peers and PRESENT in the
// forbid-side one. Under the plain union that was proposed and declined, P
// would appear in all three.
func TestTheDerivedPeersAgreeWithTheRealPropertyProvider(t *testing.T) {
	t.Parallel()

	var (
		propID  = ulid.MustParse("01JQ0000000000000000000001")
		parent  = ulid.MustParse("01JQ0000000000000000000002")
		charA   = "01JQ00000000000000000000C1"
		charB   = "01JQ00000000000000000000C2"
		charSol = "01JQ00000000000000000000C3"
		playerP = "01JQ00000000000000000000P1"
		playerQ = "01JQ00000000000000000000P2"
	)

	fixture := abactest.PropertyFixture{
		ID:               propID.String(),
		Name:             "profile.biography",
		ParentType:       "character",
		ParentID:         parent.String(),
		Visibility:       "restricted",
		OwnerCharacterID: charSol,
		// charA is named; charB (same player P) is not.
		VisibleTo: []string{charA},
		// charB is named, so P IS excluded — the ANY direction.
		ExcludedFrom: []string{charB},
		CharacterOwners: map[string]string{
			charA:   playerP,
			charB:   playerP,
			charSol: playerQ,
		},
	}

	// --- the double ---
	doubleAttrs := map[string]any{}
	abactest.DerivePlayerPeers(fixture, doubleAttrs)

	// --- the real provider ---
	owner := fixture.OwnerCharacterID
	realProvider := attribute.NewPropertyProvider(
		&stubPropertyRepo{prop: &world.EntityProperty{
			ID:           propID,
			ParentType:   fixture.ParentType,
			ParentID:     parent,
			Name:         fixture.Name,
			Visibility:   fixture.Visibility,
			Owner:        &owner,
			VisibleTo:    fixture.VisibleTo,
			ExcludedFrom: fixture.ExcludedFrom,
		}},
		stubParentLocationResolver{},
		stubOwnerResolver{owners: fixture.CharacterOwners},
	)
	realAttrs, err := realProvider.ResolveResource(context.Background(), "property:"+propID.String())
	require.NoError(t, err)

	for _, key := range []string{
		"owner_player_id", "has_owner_player_id",
		"visible_to_players", "has_visible_to_players",
		"excluded_from_players", "has_excluded_from_players",
	} {
		realValue, realPresent := realAttrs[key]
		doubleValue, doublePresent := doubleAttrs[key]

		assert.Equal(t, realPresent, doublePresent,
			"key %q: the double and the real provider MUST agree on PRESENCE — an omitted key and "+
				"a present-but-empty one are different verdicts under the DSL's missing-attr semantics", key)
		assert.Equal(t, realValue, doubleValue,
			"key %q: the double and the real provider MUST derive the same value (D-27)", key)
	}

	// The fixture's whole point, stated so a future reader sees what it
	// separates rather than only that two implementations agree.
	assert.False(t, doubleAttrs["has_visible_to_players"].(bool),
		"D-27 ALL direction: player P holds a second character the row did NOT name, so P MUST NOT "+
			"enter visible_to_players. Under the declined plain union P would be present here.")
	require.True(t, doubleAttrs["has_excluded_from_players"].(bool),
		"D-27 ANY direction: one of P's characters IS excluded, so P MUST be excluded — an exclusion "+
			"is never lost")
	assert.Equal(t, []any{playerP}, doubleAttrs["excluded_from_players"],
		"charB is player P's character and IS in excluded_from, so P — not the row's owner — is the "+
			"excluded player")
	assert.Equal(t, playerQ, doubleAttrs["owner_player_id"],
		"charSol is player Q's ONLY character, so the ALL direction admits Q as the owning player")
}

// TestThePropertyDoubleExposesNoSetterForTheDerivedPeers is the structural half
// of C2-12: a fixture built from character-keyed inputs ALONE must still
// produce the derived peers, and there must be no field a caller could use to
// state them directly.
func TestThePropertyDoubleExposesNoSetterForTheDerivedPeers(t *testing.T) {
	t.Parallel()

	charA := "01JQ00000000000000000000C1"
	playerP := "01JQ00000000000000000000P1"

	provider := abactest.PropertyProvider(abactest.PropertyFixture{
		ID:               "01JQ0000000000000000000001",
		Name:             "profile.biography",
		ParentType:       "character",
		Visibility:       "private",
		OwnerCharacterID: charA,
		CharacterOwners:  map[string]string{charA: playerP},
	})

	attrs, err := provider.ResolveResource(context.Background(), "property:01JQ0000000000000000000001")
	require.NoError(t, err)

	assert.Equal(t, playerP, attrs["owner_player_id"],
		"the peer MUST be DERIVED from OwnerCharacterID through CharacterOwners — the fixture carries "+
			"no field that could have stated it directly")
	assert.Equal(t, charA, attrs["owner"],
		"the character-keyed original stays intact so the shipped character-flavored seeds still resolve")

	fields := reflectFieldNames(abactest.PropertyFixture{})
	for _, forbidden := range []string{"OwnerPlayerID", "VisibleToPlayers", "ExcludedFromPlayers"} {
		assert.NotContains(t, fields, forbidden,
			"PropertyFixture MUST NOT expose %s — a settable derived peer lets a suite assert D-27 "+
				"while never exercising it", forbidden)
	}
}

// --- The engine builder ---

// TestNewSeedEngineLoadsTheWholeCorpusThroughTheExportedPath is the B-6
// closure: if the exported NewCompiler → NewCache → Reload → NewEngine path
// could not build an equivalent engine over the seed corpus, this goes RED here
// rather than three downstream plans discovering it.
func TestNewSeedEngineLoadsTheWholeCorpusThroughTheExportedPath(t *testing.T) {
	t.Parallel()

	engine := abactest.NewSeedEngine(
		t,
		abactest.ViewerProvider(abactest.Viewer{Tier: "anonymous"}),
		abactest.PlayerProvider(abactest.Player{ID: "01JQ00000000000000000000P1"}),
		abactest.PropertyProvider(abactest.PropertyFixture{
			ID:         "01JQ0000000000000000000001",
			Name:       "profile.pronouns",
			ParentType: "character",
			Visibility: "public",
		}),
	)

	require.NotNil(t, engine)
	assert.False(t, engine.IsDegraded(),
		"a freshly built seed engine MUST NOT be degraded")
}

// TestNewSeedEngineOmitsPlayerIDOnTheAnonymousRung pins the omit-don't-sentinel
// guarantee IN THE DOUBLE. Every identity-bearing viewer twin is unsatisfiable
// on the anonymous rung because the key is ABSENT — not because it holds an
// empty string, which `"" == ""` would satisfy against any other unresolved
// peer.
func TestNewSeedEngineOmitsPlayerIDOnTheAnonymousRung(t *testing.T) {
	t.Parallel()

	anon := abactest.ViewerProvider(abactest.Viewer{Tier: "anonymous"})
	attrs, err := anon.ResolveSubject(context.Background(), "viewer:anonymous")
	require.NoError(t, err)

	_, present := attrs["player_id"]
	assert.False(t, present,
		"player_id MUST be ABSENT on the anonymous rung, never an empty-string sentinel")
	assert.Equal(t, false, attrs["has_player_id"],
		"the has_X witness MUST be present on every code path")

	// Paired positive control: the same double DOES emit it on a rung that has one.
	guest := abactest.ViewerProvider(abactest.Viewer{Tier: "guest", PlayerID: "01JQ00000000000000000000P1"})
	guestAttrs, err := guest.ResolveSubject(context.Background(), "viewer:guest:01JQ00000000000000000000P1")
	require.NoError(t, err)
	assert.Equal(t, "01JQ00000000000000000000P1", guestAttrs["player_id"])
	assert.Equal(t, true, guestAttrs["has_player_id"])
}

// --- stubs for the real provider ---

type stubPropertyRepo struct {
	prop *world.EntityProperty
}

func (s *stubPropertyRepo) Get(context.Context, ulid.ULID) (*world.EntityProperty, error) {
	return s.prop, nil
}

func (s *stubPropertyRepo) ListByParent(context.Context, string, ulid.ULID) ([]*world.EntityProperty, error) {
	return []*world.EntityProperty{s.prop}, nil
}
func (s *stubPropertyRepo) Create(context.Context, *world.EntityProperty) error { return nil }
func (s *stubPropertyRepo) Update(context.Context, *world.EntityProperty) error { return nil }
func (s *stubPropertyRepo) Delete(context.Context, ulid.ULID) error             { return nil }
func (s *stubPropertyRepo) DeleteByParent(context.Context, string, ulid.ULID) error {
	return nil
}

type stubParentLocationResolver struct{}

func (stubParentLocationResolver) ResolveParentLocation(context.Context, string, ulid.ULID) (*ulid.ULID, error) {
	return nil, nil
}

type stubOwnerResolver struct {
	owners map[string]string // character id → player id
}

// ResolveOwnerScopes mirrors postgres.CharacterOwnerResolver: for every player
// owning at least one of the supplied characters, return that player's COMPLETE
// character set.
func (s stubOwnerResolver) ResolveOwnerScopes(_ context.Context, characterIDs []string) (map[string][]string, error) {
	wanted := make(map[string]struct{})
	for _, id := range characterIDs {
		if playerID, ok := s.owners[id]; ok {
			wanted[playerID] = struct{}{}
		}
	}
	out := make(map[string][]string, len(wanted))
	for charID, playerID := range s.owners {
		if _, ok := wanted[playerID]; !ok {
			continue
		}
		out[playerID] = append(out[playerID], charID)
	}
	for playerID := range out {
		sort.Strings(out[playerID])
	}
	return out, nil
}

// TestNewSeedEngineLoadsTheCorpusWithTheActionGateLive is the site-4 regression
// half of 02.2-04: registering `action` on NewSeedEngine's registry turns a
// previously-skipped hard-error branch live over the WHOLE shipped corpus, so
// this asserts the corpus survives that.
//
// The job provider is the pointed choice: plan 02.2-01's
// seed:job-fixture-instance-scoped is the seed that binds action.job.trigger_*,
// so it is the corpus entry most likely to trip the newly-live gate. If this goes
// red, a shipped seed references an action.* key the audit missed — a REAL
// finding to report, NOT a reason to widen ActionNamespaceSchema() until it
// passes.
func TestNewSeedEngineLoadsTheCorpusWithTheActionGateLive(t *testing.T) {
	t.Parallel()

	// Control: the seed this test exists to protect is actually in the corpus.
	// Without it the assertion below would still pass while proving nothing.
	var found bool
	for _, seed := range policy.SeedPolicies() {
		if seed.Name == "seed:job-fixture-instance-scoped" {
			found = true
			break
		}
	}
	require.True(t, found,
		"control: seed:job-fixture-instance-scoped MUST be in the corpus — it is the seed "+
			"binding action.job.trigger_*, and this test is vacuous without it")

	engine := abactest.NewSeedEngine(t, attribute.NewJobProvider(nil))

	require.NotNil(t, engine)
	assert.False(t, engine.IsDegraded(),
		"the whole corpus MUST still compile and load with the action gate live")
}

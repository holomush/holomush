// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package profilevis

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/testsupport/abactest"
)

// 01-SPEC §8.2.1 fixes the ABAC form of "at or above the floor": explicit SET
// MEMBERSHIP over the tier token, never an ordinal string comparison and never
// a numeric rank. This file is the behavioural gate on that, driven against the
// REAL engine loaded with the FULL seed corpus.
//
// WHY THE REAL ENGINE. The canned-decision engine doubles that live beside the
// policy engine — the grant, allow-all and deny-all fakes — answer from a fake,
// so an assertion written against one asserts the FAKE's behaviour and not the
// POLICY's. This package deliberately references none of them; two grep gates
// in 02-08-PLAN.md's acceptance criteria hold that line. Loading the WHOLE
// corpus is what makes the paired positive controls meaningful: a denial that
// survives every shipped policy is a real denial.
//
// The equivalent unexported engine builder declared in
// internal/access/policy/seed_smoke_test.go is NOT reachable from here — it is
// unexported, in a _test.go file, in package policy, and _test.go symbols do
// not cross package boundaries. abactest.NewSeedEngine is the importable
// builder, and it compiles the corpus through the exported
// NewCompiler → NewCache → Reload → NewEngine path.

const (
	tierFloorPlayerID = "01J0TIERPLAYER0000000000AA"
	tierFloorRowID    = "01J0TIERROW00000000000000AA"
	tierFloorParentID = "01J0TIERPARENT000000000AA"

	// The §8.6 rows that isolate each shipped floor. profile.pronouns appears
	// only in the anonymous floor's literal name list; profile.rumors only in
	// the guest floor's.
	nameAtAnonymousFloor = "profile.pronouns"
	nameAtGuestFloor     = "profile.rumors"

	// tierSpectator is the SYNTHETIC fourth rung §8.2.1's Phase-2 obligation
	// mandates. It is not a shipped tier and there is no constructor for it —
	// that is the point: §8.2's own next move is an append, and this token is
	// what an append looks like the day before anyone edits a clearing set.
	//
	// It is the discriminator because Go byte order puts it ABOVE both shipped
	// floors: "spectator" >= "guest" and "spectator" >= "anonymous" are both
	// TRUE (compareStrings implements >= as Go's l >= r on the raw strings), so
	// an ordinal clearing test PERMITS it at both floors while literal set
	// membership DENIES it at both.
	tierSpectator = "spectator"
)

// tierFloorRow is the row every assertion in this file evaluates term A
// against. Only its Name matters to the tier-floor family — that family is
// keyed on resource.property.name and never inspects visibility.
func tierFloorRow(name string) abactest.PropertyFixture {
	return abactest.PropertyFixture{
		ID:         tierFloorRowID,
		Name:       name,
		ParentType: "character",
		ParentID:   tierFloorParentID,
		Visibility: "public",
	}
}

// viewerSubjectForTier builds the subject string for a rung.
//
// access.ViewerSubject PANICS on an unrecognized tier, which is §8.2.1's
// fail-closed-on-append property expressed one layer out at the constructor. So
// the synthetic rung's subject is built literally here: what is under test is
// the POLICY's clearing test, not the constructor's guard, and routing the
// synthetic rung through the constructor would abort the test instead of
// producing a verdict to assert on.
func viewerSubjectForTier(t *testing.T, tier string) string {
	t.Helper()
	switch tier {
	case access.ViewerTierAnonymous:
		return access.ViewerSubject(tier, "")
	case access.ViewerTierGuest, access.ViewerTierPlayer:
		return access.ViewerSubject(tier, tierFloorPlayerID)
	default:
		return access.SubjectViewer + tier + ":" + tierFloorPlayerID
	}
}

// clearsFloor issues TERM A ALONE against the real engine and reports the
// verdict. Term A is isolated by its action token: plan 02-07 seeded exactly
// the tier-floor family with ActionTierFloor, so this request reaches those two
// policies and nothing else in the corpus.
func clearsFloor(t *testing.T, engine *policy.Engine, subject, propertyName string) bool {
	t.Helper()

	req, err := types.NewAccessRequest(subject, ActionTierFloor, access.PropertyResource(tierFloorRowID), nil)
	require.NoError(t, err)

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err, "evaluating %q at %q", propertyName, subject)
	require.False(t, decision.IsInfraFailure(),
		"an infrastructure failure would make every verdict in this file meaningless: %s", decision.Reason())

	return decision.IsAllowed()
}

// tierClears builds a one-viewer, one-row engine and returns term A's verdict.
func tierClears(t *testing.T, tier, propertyName string) bool {
	t.Helper()

	engine := abactest.NewSeedEngine(
		t,
		abactest.ViewerProvider(abactest.Viewer{Tier: tier, PlayerID: playerIDForTier(tier)}),
		abactest.PropertyProvider(tierFloorRow(propertyName)),
	)
	return clearsFloor(t, engine, viewerSubjectForTier(t, tier), propertyName)
}

// playerIDForTier mirrors §8.4.1's subject-form table: the anonymous rung
// carries no player identifier, so the double must OMIT player_id there rather
// than emit an empty-string sentinel.
func playerIDForTier(tier string) string {
	if tier == access.ViewerTierAnonymous {
		return ""
	}
	return tierFloorPlayerID
}

// --- The precondition: what the corpus actually seeds ---

func TestTheSeedCorpusCarriesExactlyTwoTierFloorPoliciesAtTheAnonymousAndGuestRungs(t *testing.T) {
	var got []string
	for _, seed := range policy.SeedPolicies() {
		if strings.HasPrefix(seed.Name, "seed:profile-tier-floor-") {
			got = append(got, seed.Name)
		}
	}

	// This is asserted BEFORE any floor assertion below, and it is why no
	// assertion in this file is written against a third rung. Locked decision
	// D-03 originally mandated one policy per rung; it was AMENDED on
	// 2026-08-04 to one policy per rung THAT HAS AT LEAST ONE SEEDED §8.6
	// MEMBER. §8.6's seeded-default column places every governed row at
	// anonymous or guest, so the highest rung has no member, and the DSL's list
	// grammar requires at least one literal — an empty `in []` does not parse.
	//
	// The consequence for THIS file is sharp: a clearing assertion phrased
	// against a policy the corpus does not seed asks the engine about something
	// that does not exist and gets DENY for EVERY tier, including the one that
	// should clear. It would be a gate that cannot fail, wearing the clothes of
	// the sharpest gate in the phase (cross-AI review C2-11). The
	// discriminator below is the GUEST floor for exactly that reason.
	//
	// A future third policy makes this assertion RED rather than leaving the
	// new rung silently uncovered.
	assert.ElementsMatch(t, []string{
		"seed:profile-tier-floor-anonymous",
		"seed:profile-tier-floor-guest",
	}, got, "the shipped tier-floor family is exactly two policies")
}

// --- §8.2.1's Phase-2 obligation: the fourth-rung gate ---

func TestASyntheticFourthRungClearsNeitherShippedTierFloor(t *testing.T) {
	// THE DISCRIMINATING ASSERTION. Under literal set membership `spectator`
	// appears in neither clearing list, so it clears nothing. Under an ordinal
	// implementation — `principal.viewer.tier >= "guest"` — "spectator" >=
	// "guest" is TRUE in Go byte order and this PERMITS. Observed RED against
	// exactly that mutation before the assertion was committed; the recorded
	// failure is in 02-08-SUMMARY.md.
	assert.False(t, tierClears(t, tierSpectator, nameAtGuestFloor),
		"a newly appended rung MUST NOT clear the guest floor: %q sorts above %q in Go byte order, "+
			"so an ordinal clearing test would hand it clearance on the day the token is added",
		tierSpectator, access.ViewerTierGuest)

	// The property is "a newly appended token clears NOTHING", not "it fails to
	// clear one particular rung" — so the same assertion is made at every rung
	// the corpus seeds. "spectator" >= "anonymous" is also true.
	assert.False(t, tierClears(t, tierSpectator, nameAtAnonymousFloor),
		"a newly appended rung MUST NOT clear the anonymous floor either")

	// PAIRED POSITIVE CONTROLS on the same fixtures. Without these, both
	// denials above would be satisfied by a tier-floor family that denies
	// everyone — a broken family and a fail-closed-on-append family look
	// identical from the denial side alone.
	assert.True(t, tierClears(t, access.ViewerTierGuest, nameAtGuestFloor),
		"paired control: the guest rung DOES clear the guest floor")
	assert.True(t, tierClears(t, access.ViewerTierAnonymous, nameAtAnonymousFloor),
		"paired control: the anonymous rung DOES clear the anonymous floor")
}

func TestEachShippedRungClearsExactlyTheTierFloorsSpec821AssignsIt(t *testing.T) {
	tests := []struct {
		name          string
		tier          string
		clearsAnon    bool
		clearsGuest   bool
		whyNotCleared string
	}{
		{
			name:          "an anonymous viewer clears the anonymous floor and not the guest floor",
			tier:          access.ViewerTierAnonymous,
			clearsAnon:    true,
			clearsGuest:   false,
			whyNotCleared: `anonymous is not a member of ["guest", "player"]`,
		},
		{
			name:        "a guest viewer clears both shipped floors",
			tier:        access.ViewerTierGuest,
			clearsAnon:  true,
			clearsGuest: true,
		},
		{
			name:        "a player viewer clears both shipped floors",
			tier:        access.ViewerTierPlayer,
			clearsAnon:  true,
			clearsGuest: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.clearsAnon, tierClears(t, tt.tier, nameAtAnonymousFloor),
				"the anonymous floor, for tier %q", tt.tier)
			assert.Equal(t, tt.clearsGuest, tierClears(t, tt.tier, nameAtGuestFloor),
				"the guest floor, for tier %q (%s)", tt.tier, tt.whyNotCleared)
		})
	}
}

func TestAViewerWithNoTierAttributeClearsNoTierFloor(t *testing.T) {
	subject := access.ViewerSubject(access.ViewerTierPlayer, tierFloorPlayerID)

	for _, name := range []string{nameAtAnonymousFloor, nameAtGuestFloor} {
		engine := abactest.NewSeedEngine(
			t,
			tierlessViewerProvider{},
			abactest.PropertyProvider(tierFloorRow(name)),
		)
		assert.False(t, clearsFloor(t, engine, subject, name),
			"an unresolved tier clears nothing (evalInList is false on an unresolved LHS): %s", name)
	}

	// PAIRED CONTROL on the same subject string: the ONLY difference is that
	// the provider supplies a tier. Without it, the denials above would be
	// satisfied by a subject the corpus rejects for some unrelated reason.
	for _, name := range []string{nameAtAnonymousFloor, nameAtGuestFloor} {
		engine := abactest.NewSeedEngine(
			t,
			abactest.ViewerProvider(abactest.Viewer{
				Tier:     access.ViewerTierPlayer,
				PlayerID: tierFloorPlayerID,
			}),
			abactest.PropertyProvider(tierFloorRow(name)),
		)
		assert.True(t, clearsFloor(t, engine, subject, name),
			"paired control: the same subject WITH a tier clears %s", name)
	}
}

// tierlessViewerProvider is a viewer-namespace provider that OMITS `tier`
// entirely.
//
// abactest.ViewerProvider cannot express this shape — it always writes the key,
// so an empty Tier yields `tier: ""`. That is an empty-string SENTINEL, and
// asserting against it would test compareStrings on "" rather than the property
// §8.2.1 actually cares about: that an UNRESOLVED left-hand side makes
// evalInList false. The schema is pinned to abactest's, so this stays a real
// provider shape rather than an invented one.
type tierlessViewerProvider struct{}

func (tierlessViewerProvider) Namespace() string { return "viewer" }

func (tierlessViewerProvider) ResolveSubject(context.Context, string) (map[string]any, error) {
	return map[string]any{"has_player_id": false, "has_roles": false}, nil
}

func (tierlessViewerProvider) ResolveResource(context.Context, string) (map[string]any, error) {
	return nil, nil
}

func (tierlessViewerProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{Attributes: abactest.ViewerSchemaKeys}
}

var _ attribute.AttributeProvider = tierlessViewerProvider{}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package profilevis

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/testsupport/abactest"
	"github.com/holomush/holomush/pkg/errutil"
)

// The interface this package consumes MUST be the real engine's shape. This
// binding is what makes the recording double below a double for the ENGINE
// rather than for an invented API: a signature drift in *policy.Engine breaks
// this line at compile time, and the double breaks with it.
var _ PolicyEvaluator = (*policy.Engine)(nil)

const (
	testPropertyID  = "01J0PROPERTY000000000000AA"
	testCharacterID = "01J0CHARACTER00000000000AA"
	testPlayerID    = "01J0PLAYER00000000000000AA"
)

func testViewer(t *testing.T) string {
	t.Helper()
	return access.ViewerSubject(access.ViewerTierPlayer, testPlayerID)
}

// --- The recording engine double ---

// verdict is one scripted engine answer. A zero verdict is a policy denial.
type verdict struct {
	decision types.Decision
	err      error
}

func allowVerdict() verdict {
	return verdict{decision: types.NewDecision(types.EffectAllow, "scripted permit", "test:permit")}
}

func denyVerdict() verdict {
	return verdict{decision: types.NewDecision(types.EffectDefaultDeny, "scripted deny", "")}
}

// infraVerdict is the shape the engine returns for degraded mode and session
// failures: a DENY decision with an "infra:"-prefixed policy id and a NIL
// error. Reading it as an ordinary denial is exactly the §8.10 failure — the
// profile then renders as legitimately sparse when nothing was evaluated.
func infraVerdict() verdict {
	return verdict{decision: types.NewDecision(types.EffectDefaultDeny, "scripted infra failure", "infra:test")}
}

func errVerdict(err error) verdict {
	return verdict{err: err}
}

// scriptedEngine records every request and answers per TERM. It routes on the
// same two discriminators the production code uses — the profile resource type
// for reachability, and the action token for term A versus term B — so a
// production change that collapsed the two per-attribute calls into one would
// show up here as a call count, not as a silently reinterpreted answer.
type scriptedEngine struct {
	calls     []types.AccessRequest
	reachable verdict
	tierFloor verdict
	rowKeyed  verdict
}

func (s *scriptedEngine) Evaluate(_ context.Context, req types.AccessRequest) (types.Decision, error) {
	s.calls = append(s.calls, req)
	switch {
	case strings.HasPrefix(req.Resource, access.ResourceProfile):
		return s.reachable.decision, s.reachable.err
	case req.Action == ActionTierFloor:
		return s.tierFloor.decision, s.tierFloor.err
	default:
		return s.rowKeyed.decision, s.rowKeyed.err
	}
}

// perAttributeCalls returns the calls that were NOT the reachability
// evaluation, which is what the short-circuit assertion counts.
func (s *scriptedEngine) perAttributeCalls() []types.AccessRequest {
	out := make([]types.AccessRequest, 0, len(s.calls))
	for _, c := range s.calls {
		if !strings.HasPrefix(c.Resource, access.ResourceProfile) {
			out = append(out, c)
		}
	}
	return out
}

func newScriptedEngine() *scriptedEngine {
	return &scriptedEngine{
		reachable: allowVerdict(),
		tierFloor: allowVerdict(),
		rowKeyed:  allowVerdict(),
	}
}

// --- Task 1: the conjunction ---

// Verifies: INV-ACCESS-10
func TestAttributeVisibleIssuesExactlyTwoEvaluationsSeparatedByTheActionToken(t *testing.T) {
	engine := newScriptedEngine()
	e := &Evaluator{Engine: engine}
	viewer := testViewer(t)

	visible, err := e.AttributeVisible(context.Background(), viewer, testPropertyID, "profile.rumors")
	require.NoError(t, err)
	assert.True(t, visible, "both terms permit, so the attribute publishes")

	require.Len(t, engine.calls, 2,
		"§8.5.1 is TWO evaluations ANDed by the caller — one evaluation would be additive, not conjunctive")

	// Both terms address the SAME row. A conjunction over two different rows
	// would prove nothing about either.
	wantResource := access.PropertyResource(testPropertyID)
	for i, call := range engine.calls {
		assert.Equal(t, wantResource, call.Resource, "call %d resource", i)
		assert.Equal(t, viewer, call.Subject, "call %d subject", i)
	}

	// The separator is the ACTION, and it is what keeps the two families in
	// separate evaluations. Two calls that BOTH matched both families would
	// reduce to the additive shape §8.5.1.1 exists to prevent, and the
	// call-count assertion alone would not catch that.
	assert.Equal(t, ActionTierFloor, engine.calls[0].Action, "term A carries the tier-floor action")
	assert.Equal(t, ActionRowKeyed, engine.calls[1].Action, "term B carries the row-keyed action")
	assert.NotEqual(t, engine.calls[0].Action, engine.calls[1].Action,
		"the two terms MUST NOT share an action — a shared action puts both families in one evaluation")
}

func TestAttributeVisiblePublishesOnlyWhenBothTermsPermit(t *testing.T) {
	tests := []struct {
		name      string
		tierFloor verdict
		rowKeyed  verdict
		want      bool
	}{
		{"publishes when the tier floor and the row both permit", allowVerdict(), allowVerdict(), true},
		{"withholds when the tier floor denies and the row permits", denyVerdict(), allowVerdict(), false},
		{"withholds when the tier floor permits and the row denies", allowVerdict(), denyVerdict(), false},
		{"withholds when both the tier floor and the row deny", denyVerdict(), denyVerdict(), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newScriptedEngine()
			engine.tierFloor = tt.tierFloor
			engine.rowKeyed = tt.rowKeyed

			e := &Evaluator{Engine: engine}
			got, err := e.AttributeVisible(context.Background(), testViewer(t), testPropertyID, "profile.rumors")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAttributeVisiblePropagatesAnEvaluationErrorRatherThanReportingWithheld(t *testing.T) {
	boom := errors.New("policy store unreachable")

	tests := []struct {
		name  string
		apply func(*scriptedEngine)
	}{
		{"term A errors", func(s *scriptedEngine) { s.tierFloor = errVerdict(boom) }},
		{"term B errors", func(s *scriptedEngine) { s.rowKeyed = errVerdict(boom) }},
		{"term A reports an infra-failure decision with a nil error", func(s *scriptedEngine) {
			s.tierFloor = infraVerdict()
		}},
		{"term B reports an infra-failure decision with a nil error", func(s *scriptedEngine) {
			s.rowKeyed = infraVerdict()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := newScriptedEngine()
			tt.apply(engine)

			e := &Evaluator{Engine: engine}
			visible, err := e.AttributeVisible(context.Background(), testViewer(t), testPropertyID, "profile.rumors")

			require.Error(t, err, "an infra failure MUST NOT be reported as (false, nil) — the caller could not then tell 'withheld' from 'not evaluated'")
			assert.False(t, visible)
			assert.ErrorIs(t, err, ErrEvaluationFailed)
			errutil.AssertErrorCode(t, err, CodeEvaluationFailed)
		})
	}

	t.Run("paired control: no error when both terms answer normally", func(t *testing.T) {
		engine := newScriptedEngine()
		engine.rowKeyed = denyVerdict()

		e := &Evaluator{Engine: engine}
		visible, err := e.AttributeVisible(context.Background(), testViewer(t), testPropertyID, "profile.rumors")
		require.NoError(t, err, "a policy denial is not an error")
		assert.False(t, visible)
	})
}

func TestReachableIssuesExactlyOneEvaluationAgainstTheProfileResource(t *testing.T) {
	engine := newScriptedEngine()
	e := &Evaluator{Engine: engine}
	viewer := testViewer(t)

	reachable, err := e.Reachable(context.Background(), viewer, testCharacterID)
	require.NoError(t, err)
	assert.True(t, reachable)

	require.Len(t, engine.calls, 1, "reachability is ONE evaluation (§8.4.2)")
	assert.Equal(t, access.ProfileResource(testCharacterID), engine.calls[0].Resource,
		"reachability is its own resource TYPE — not character:<id>, whose read is already permitted elsewhere")
	assert.Equal(t, ActionReachable, engine.calls[0].Action)
	assert.Equal(t, viewer, engine.calls[0].Subject)

	t.Run("paired control: a denying engine reports unreachable without erroring", func(t *testing.T) {
		denying := newScriptedEngine()
		denying.reachable = denyVerdict()

		got, err := (&Evaluator{Engine: denying}).Reachable(context.Background(), viewer, testCharacterID)
		require.NoError(t, err)
		assert.False(t, got)
	})
}

func TestVisibleAttributesPerformsNoPerAttributeEvaluationWhenReachabilityDenies(t *testing.T) {
	engine := newScriptedEngine()
	engine.reachable = denyVerdict()

	e := &Evaluator{Engine: engine}
	got, err := e.VisibleAttributes(context.Background(), testViewer(t), testCharacterID, []Property{
		{ID: testPropertyID, Name: "profile.pronouns"},
		{ID: "01J0PROPERTY000000000000BB", Name: "profile.rumors"},
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProfileUnreachable,
		"§8.7's not-found-equivalent is signalled as its own outcome, distinguishable from an infra failure")
	errutil.AssertErrorCode(t, err, CodeProfileUnreachable)
	assert.Nil(t, got)

	assert.Empty(t, engine.perAttributeCalls(),
		"§8.4.2: a reachability DENY short-circuits BEFORE any per-field evaluation runs")
	require.Len(t, engine.calls, 1, "the reachability evaluation itself is the only call")

	t.Run("paired control: a permitting reachability does evaluate the fields", func(t *testing.T) {
		permitting := newScriptedEngine()
		e := &Evaluator{Engine: permitting}
		out, err := e.VisibleAttributes(context.Background(), testViewer(t), testCharacterID, []Property{
			{ID: testPropertyID, Name: "profile.pronouns"},
		})
		require.NoError(t, err)
		assert.Contains(t, out, "profile.pronouns")
		assert.Len(t, permitting.perAttributeCalls(), 2, "one reachable profile, one attribute, two terms")
	})
}

func TestVisibleAttributesReturnsOnlyThePermittedSubset(t *testing.T) {
	const (
		clearedID  = "01J0PROPERTY000000000000CC"
		withheldID = "01J0PROPERTY000000000000DD"
	)

	// Withhold exactly one row on term B, by id.
	rowKeyedByID := &perRowEngine{
		reachable: allowVerdict(),
		tierFloor: allowVerdict(),
		rowKeyed: map[string]verdict{
			access.PropertyResource(clearedID):  allowVerdict(),
			access.PropertyResource(withheldID): denyVerdict(),
		},
	}

	e := &Evaluator{Engine: rowKeyedByID}
	got, err := e.VisibleAttributes(context.Background(), testViewer(t), testCharacterID, []Property{
		{ID: clearedID, Name: "profile.pronouns"},
		{ID: withheldID, Name: "profile.rumors"},
	})
	require.NoError(t, err)

	// §8.9's ancestor discipline: absence is asserted by KEY PRESENCE, never by
	// comparing a value to its zero. `got["profile.rumors"].Name == ""` would be
	// satisfied by a struct that was never populated.
	assert.Contains(t, got, "profile.pronouns")
	assert.NotContains(t, got, "profile.rumors")
	assert.Len(t, got, 1)
}

func TestVisibleAttributesReturnsTheSamePermittedSetWhenTheInputOrderIsReversed(t *testing.T) {
	const (
		aID = "01J0PROPERTY000000000000EE"
		bID = "01J0PROPERTY000000000000FF"
		cID = "01J0PROPERTY000000000000GG"
	)

	props := []Property{
		{ID: aID, Name: "profile.pronouns"},
		{ID: bID, Name: "profile.rumors"},
		{ID: cID, Name: "profile.concept"},
	}
	reversed := make([]Property, len(props))
	for i, p := range props {
		reversed[len(props)-1-i] = p
	}

	newEngine := func() *perRowEngine {
		return &perRowEngine{
			reachable: allowVerdict(),
			tierFloor: allowVerdict(),
			rowKeyed: map[string]verdict{
				access.PropertyResource(aID): allowVerdict(),
				access.PropertyResource(bID): denyVerdict(),
				access.PropertyResource(cID): allowVerdict(),
			},
		}
	}

	forward, err := (&Evaluator{Engine: newEngine()}).
		VisibleAttributes(context.Background(), testViewer(t), testCharacterID, props)
	require.NoError(t, err)

	backward, err := (&Evaluator{Engine: newEngine()}).
		VisibleAttributes(context.Background(), testViewer(t), testCharacterID, reversed)
	require.NoError(t, err)

	assert.Equal(t, forward, backward,
		"submission order MUST NOT change any attribute's published-or-withheld verdict")
	assert.Contains(t, forward, "profile.pronouns")
	assert.NotContains(t, forward, "profile.rumors")
}

// Verifies: INV-ACCESS-10
func TestVisibleAttributesAbortsTheWholeCallWhenAnyEvaluationFails(t *testing.T) {
	const (
		okID   = "01J0PROPERTY000000000000HH"
		badID  = "01J0PROPERTY000000000000II"
		lastID = "01J0PROPERTY000000000000JJ"
	)

	engine := &perRowEngine{
		reachable: allowVerdict(),
		tierFloor: allowVerdict(),
		rowKeyed: map[string]verdict{
			access.PropertyResource(okID):   allowVerdict(),
			access.PropertyResource(badID):  errVerdict(errors.New("resolver timeout")),
			access.PropertyResource(lastID): allowVerdict(),
		},
	}

	got, err := (&Evaluator{Engine: engine}).
		VisibleAttributes(context.Background(), testViewer(t), testCharacterID, []Property{
			{ID: okID, Name: "profile.pronouns"},
			{ID: badID, Name: "profile.rumors"},
			{ID: lastID, Name: "profile.concept"},
		})

	require.Error(t, err, "§8.10: an infra failure aborts; it MUST NOT read as a legitimately sparse profile")
	assert.ErrorIs(t, err, ErrEvaluationFailed)
	assert.Nil(t, got, "a partially-populated set is the ghost-data shape ListPropertiesByParent's third branch exists to prevent")
}

func TestEvaluatorRefusesAMalformedCallRatherThanEvaluatingIt(t *testing.T) {
	viewer := testViewer(t)

	t.Run("a nil engine is refused", func(t *testing.T) {
		_, err := (&Evaluator{}).Reachable(context.Background(), viewer, testCharacterID)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, CodeEvaluationFailed)
	})

	t.Run("an empty viewer subject is refused", func(t *testing.T) {
		engine := newScriptedEngine()
		_, err := (&Evaluator{Engine: engine}).Reachable(context.Background(), "", testCharacterID)
		require.Error(t, err)
		assert.Empty(t, engine.calls, "a subject that would bypass access control never reaches the engine")
	})

	t.Run("an empty character id is refused", func(t *testing.T) {
		engine := newScriptedEngine()
		_, err := (&Evaluator{Engine: engine}).Reachable(context.Background(), viewer, "")
		require.Error(t, err)
		assert.Empty(t, engine.calls)
	})

	t.Run("an empty property id is refused", func(t *testing.T) {
		engine := newScriptedEngine()
		_, err := (&Evaluator{Engine: engine}).AttributeVisible(context.Background(), viewer, "", "profile.rumors")
		require.Error(t, err)
		assert.Empty(t, engine.calls)
	})

	t.Run("paired control: a well-formed call does reach the engine", func(t *testing.T) {
		engine := newScriptedEngine()
		_, err := (&Evaluator{Engine: engine}).AttributeVisible(context.Background(), viewer, testPropertyID, "profile.rumors")
		require.NoError(t, err)
		assert.Len(t, engine.calls, 2)
	})
}

// perRowEngine answers term B per resource, so a suite can withhold one row
// while permitting its neighbours.
type perRowEngine struct {
	calls     []types.AccessRequest
	reachable verdict
	tierFloor verdict
	rowKeyed  map[string]verdict
}

func (p *perRowEngine) Evaluate(_ context.Context, req types.AccessRequest) (types.Decision, error) {
	p.calls = append(p.calls, req)
	switch {
	case strings.HasPrefix(req.Resource, access.ResourceProfile):
		return p.reachable.decision, p.reachable.err
	case req.Action == ActionTierFloor:
		return p.tierFloor.decision, p.tierFloor.err
	default:
		v, ok := p.rowKeyed[req.Resource]
		if !ok {
			return denyVerdict().decision, nil
		}
		return v.decision, v.err
	}
}

// ============================================================================
// Task 3 — D-04's additive-permit regression and §8.6's totality rule
//
// Everything below drives the REAL engine over the FULL seed corpus. The
// scripted doubles above prove the evaluator's SHAPE; these prove the shipped
// POLICIES' verdicts through it, which is the only thing that can catch term B
// being dropped or a name match being widened to a prefix.
//
// FIXTURE RULE — the falsely-green trap this suite exists on the right side of.
// Every row fixture writes CHARACTER ids into OwnerCharacterID, VisibleTo and
// ExcludedFrom, because that is what an entity_properties row actually carries.
// The player-keyed peers the viewer twins read are DERIVED by the provider
// double from those character ids through CharacterOwners — never written by a
// fixture. abactest.PropertyFixture has no field for them, by construction.
//
// If a viewer twin will not permit where it should, the POLICY or the
// DERIVATION is wrong. Writing a player id into a character-keyed field to make
// a test pass would make this suite assert an identity model production does
// not have, over the most exposed boundary in the milestone.
// ============================================================================

const (
	// The viewing player and BOTH of their characters. D-27's permit-side ALL
	// direction asks whether EVERY character of a player appears in a field, so
	// a player with two characters is the shape that can distinguish ALL from a
	// plain union — a single-character player cannot.
	viewingPlayerID = "01J0PLAYERP00000000000000A"
	viewerCharC1    = "01J0CHARC1000000000000000A"
	viewerCharC2    = "01J0CHARC2000000000000000A"

	// A DIFFERENT player and their character, for the negative half of the
	// derivation pair.
	otherPlayerID = "01J0PLAYERQ00000000000000A"
	otherCharC3   = "01J0CHARC3000000000000000A"

	// The character whose profile is being read.
	profileCharacterID = "01J0PROFILECHAR000000000A"

	profileRowID = "01J0PROFILEROW0000000000A"
)

// characterOwners is the fixture's COMPLETE character universe: character id →
// owning player id. Completeness is load-bearing, not tidiness — the ALL
// direction asks whether EVERY character of a player is named, so an incomplete
// map silently turns an ALL test into an ANY test and the D-27 assertions below
// would pass against a plain union.
func characterOwners() map[string]string {
	return map[string]string{
		viewerCharC1: viewingPlayerID,
		viewerCharC2: viewingPlayerID,
		otherCharC3:  otherPlayerID,
	}
}

func playerViewer() abactest.Viewer {
	return abactest.Viewer{Tier: access.ViewerTierPlayer, PlayerID: viewingPlayerID}
}

func anonymousViewer() abactest.Viewer {
	return abactest.Viewer{Tier: access.ViewerTierAnonymous}
}

// subjectForViewer builds the §8.4.1 subject string matching a viewer fixture,
// so the subject and the attribute bag cannot drift apart.
func subjectForViewer(t *testing.T, v abactest.Viewer) string {
	t.Helper()
	if v.Tier == access.ViewerTierAnonymous {
		return access.ViewerSubject(v.Tier, "")
	}
	return access.ViewerSubject(v.Tier, v.PlayerID)
}

// profileRow builds a character-keyed entity_properties fixture.
func profileRow(name, visibility string) abactest.PropertyFixture {
	return abactest.PropertyFixture{
		ID:              profileRowID,
		Name:            name,
		ParentType:      "character",
		ParentID:        profileCharacterID,
		Visibility:      visibility,
		CharacterOwners: characterOwners(),
	}
}

// published runs the FULL conjunction for one row against the real engine and
// reports whether the attribute appears in the published set.
//
// It goes through VisibleAttributes rather than AttributeVisible so the
// assertion is made on the SET the caller receives — §8.9's discipline one
// layer in: absence is key absence, never a value compared to its zero.
func published(t *testing.T, v abactest.Viewer, row abactest.PropertyFixture) bool {
	t.Helper()

	e := &Evaluator{Engine: abactest.NewSeedEngine(
		t,
		abactest.ViewerProvider(v),
		abactest.PropertyProvider(row),
	)}

	got, err := e.VisibleAttributes(context.Background(), subjectForViewer(t, v), profileCharacterID,
		[]Property{{ID: row.ID, Name: row.Name}})
	require.NoError(t, err, "evaluating %q at visibility=%q", row.Name, row.Visibility)

	_, ok := got[row.Name]
	return ok
}

// --- D-04: the additive-permit regression ---

func TestARowTheViewerClearsTheTierFloorForIsStillWithheldWhenTheRowItselfDenies(t *testing.T) {
	// profile.rumors sits at the GUEST floor, and the viewer is at tier player,
	// so TERM A PERMITS in every case below. What varies is term B alone.
	//
	// This is D-04, and it is the only assertion in the suite that
	// distinguishes the conjunction from the additive shape. If term B is ever
	// dropped, the tier permit alone publishes the private row and this goes
	// RED — which is the whole point, because the additive shape has no symptom
	// of its own. Observed RED against exactly that mutation; the recorded
	// failure is in 02-08-SUMMARY.md.
	tests := []struct {
		name       string
		visibility string
		want       bool
		why        string
	}{
		{
			name:       "a private row is absent because the viewer is not the owning player",
			visibility: "private",
			want:       false,
			why:        "the row is owned by a character of a DIFFERENT player, so the derived owner peer does not match",
		},
		{
			name:       "an admin row is absent because the viewer holds no admin role",
			visibility: "admin",
			want:       false,
			why:        "the viewer-flavored admin twin requires \"admin\" in principal.viewer.roles",
		},
		{
			name:       "paired control: the same row at public visibility IS published",
			visibility: "public",
			want:       true,
			why:        "without this the absences above could pass because the whole read is broken",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := profileRow("profile.rumors", tt.visibility)
			row.OwnerCharacterID = otherCharC3

			assert.Equal(t, tt.want, published(t, playerViewer(), row), tt.why)
		})
	}
}

// --- §8.6: the totality rule ---

func TestAProfileNameInNoSpec86RowIsDeniedRatherThanDefaulted(t *testing.T) {
	// Every case runs at the HIGHEST rung and at PUBLIC visibility — the most
	// permissive combination the system offers. A denial that survives that is
	// a denial about the ENUMERATION and nothing else. §8.6: there is no
	// residual floor for an unenumerated name to fall back to.
	tests := []struct {
		name         string
		attributeRow string
		want         bool
		why          string
	}{
		{
			name:         "the twelfth gallery name is denied — the eleven media names are a closed set",
			attributeRow: "profile.image.gallery.10",
			want:         false,
			why:          "§7.3 fixes eleven exact names; index 10 is not a member and no glob covers it",
		},
		{
			name:         "paired control: the eleventh gallery name IS published",
			attributeRow: "profile.image.gallery.09",
			want:         true,
			why:          "without this the denial above could be about the media names generally rather than about enumeration",
		},
		{
			name:         "an arbitrary unenumerated profile name is denied",
			attributeRow: "profile.somethingnobodyconsidered",
			want:         false,
			why:          "§7.1 makes a new profile field an INSERT, so the namespace is open — a residual permit would publish a name nobody has considered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want,
				published(t, playerViewer(), profileRow(tt.attributeRow, "public")), tt.why)
		})
	}
}

// --- §8.8: the hard floor D-02 protects ---

func TestAnAnonymousViewerReachingAProfileReceivesItsPublicPronouns(t *testing.T) {
	// Written as a POSITIVE on purpose. This is the case §8.5.1.1's option 2
	// would break, and D-02 rejected option 2 precisely to protect it: under
	// option 2 an anonymous viewer has no co-located character, term B denies,
	// and a reachable profile carries `name` without `pronouns`. That failure
	// is CLOSED — the profile just looks bare — so nothing else in this suite
	// would catch a regression to it.
	assert.True(t, published(t, anonymousViewer(), profileRow("profile.pronouns", "public")),
		"§8.8: if a viewer can reach the profile at all, that viewer sees name and pronouns")

	// Paired control on the same rung: the anonymous viewer does NOT get a
	// guest-floor name, so the positive above is not "anonymous sees
	// everything".
	assert.False(t, published(t, anonymousViewer(), profileRow("profile.rumors", "public")),
		"paired control: the anonymous rung does not clear the guest floor")
}

// --- D-27: the derivation directions, and why they lean opposite ways ---
//
// The two peer directions look inconsistent side by side, and the DSL text
// cannot show either. They are each computed in the direction that CANNOT WIDEN
// ITS OWN POLICY'S EFFECT:
//
//   - The PERMIT side is ALL: a player enters visible_to_players only when EVERY
//     character of theirs is named, so a viewer never obtains a permit that one
//     of that player's characters lacked.
//   - The FORBID side is ANY: a player enters excluded_from_players when any
//     character of theirs is named, so an exclusion is never lost.
//
// Permits and forbids widen OPPOSITELY, which is why the conservative direction
// differs between them. Do not "simplify" them into one direction for symmetry.

func TestARestrictedRowIsPermittedOnlyWhenEveryCharacterOfTheViewingPlayerIsNamed(t *testing.T) {
	tests := []struct {
		name      string
		visibleTo []string
		want      bool
		why       string
	}{
		{
			name:      "naming every character of the viewing player permits",
			visibleTo: []string{viewerCharC1, viewerCharC2},
			want:      true,
			why:       "D-27's ALL direction is satisfied, so the derived visible_to peer holds the viewing player",
		},
		{
			name:      "naming only one of the viewing player's two characters DENIES",
			visibleTo: []string{viewerCharC1},
			want:      false,
			why:       "this is the assertion that fails if the derivation ever drifts back to a plain player union",
		},
		{
			name:      "naming a character belonging to a different player denies",
			visibleTo: []string{otherCharC3},
			want:      false,
			why:       "proves the character to player derivation is real and not a pass-through of whatever the fixture wrote",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := profileRow("profile.rumors", "restricted")
			row.VisibleTo = tt.visibleTo

			assert.Equal(t, tt.want, published(t, playerViewer(), row), tt.why)
		})
	}
}

func TestTheExcludedFromForbidIsWhatDeniesARowWhoseVisibleToPermitDoesFire(t *testing.T) {
	// THE DISCRIMINATING FIXTURE, and it is NOT the obvious one (cross-AI
	// review cycle 3, B-15).
	//
	// The natural-looking fixture — VisibleTo = [C2], ExcludedFrom = [C1] — is a
	// test that CANNOT FAIL. D-27's ALL direction is unsatisfied, so the derived
	// visible_to peer is empty, so the permit twin never matches, so
	// combineDecisions returns default-deny WHETHER OR NOT the forbid derivation
	// exists at all. Delete the exclusion derivation entirely and that subtest
	// stays green while proving nothing about it.
	//
	// The fixture below names BOTH characters, so the permit genuinely fires and
	// deny-overrides means the forbid is the ONLY thing standing between the
	// viewer and the row.
	excluded := profileRow("profile.rumors", "restricted")
	excluded.VisibleTo = []string{viewerCharC1, viewerCharC2}
	excluded.ExcludedFrom = []string{viewerCharC1}

	assert.False(t, published(t, playerViewer(), excluded),
		"the forbid twin denies a row whose permit twin fires — D-27's ANY direction")

	// PAIRED CONTROL on the same fixture minus the exclusion. Without it the
	// denial above could pass because the permit never fired.
	permitted := profileRow("profile.rumors", "restricted")
	permitted.VisibleTo = []string{viewerCharC1, viewerCharC2}

	assert.True(t, published(t, playerViewer(), permitted),
		"paired control: the identical fixture with no exclusion IS published")
}

func TestTheDiscriminatingForbidFixtureSatisfiesThePermitBeforeTheForbidDeniesIt(t *testing.T) {
	// The assertion that makes the subtest above discriminating rather than
	// merely denying. It reads the DERIVED output directly, which is the only
	// place in this file those keys appear — never in a fixture.
	discriminating := abactest.PropertyFixture{
		ID:              profileRowID,
		Name:            "profile.rumors",
		ParentType:      "character",
		ParentID:        profileCharacterID,
		Visibility:      "restricted",
		VisibleTo:       []string{viewerCharC1, viewerCharC2},
		ExcludedFrom:    []string{viewerCharC1},
		CharacterOwners: characterOwners(),
	}

	derived := map[string]any{}
	abactest.DerivePlayerPeers(discriminating, derived)

	require.Contains(t, derived, "visible_to_players",
		"the permit-side peer MUST be present, or the forbid below is not what denies the row")
	assert.Contains(t, derived["visible_to_players"], viewingPlayerID)
	assert.Equal(t, true, derived["has_visible_to_players"])

	require.Contains(t, derived, "excluded_from_players")
	assert.Contains(t, derived["excluded_from_players"], viewingPlayerID)

	// The near-miss, asserted so a later simplification cannot quietly adopt it:
	// with only ONE of the two characters named, the permit-side peer is ABSENT
	// and the row default-denies for a reason that has nothing to do with the
	// exclusion.
	nearMiss := discriminating
	nearMiss.VisibleTo = []string{viewerCharC2}

	nearMissDerived := map[string]any{}
	abactest.DerivePlayerPeers(nearMiss, nearMissDerived)

	assert.NotContains(t, nearMissDerived, "visible_to_players",
		"the near-miss fixture never satisfies the permit, so it could not detect a missing forbid")
}

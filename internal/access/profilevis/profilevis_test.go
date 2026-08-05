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

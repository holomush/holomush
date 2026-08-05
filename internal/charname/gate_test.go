// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/charname/syntax"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/pkg/errutil"
)

// fakeLookup is a SkeletonLookup double. It records the arguments it was
// called with so the self-exclusion channel can be asserted structurally
// rather than inferred from the verdict.
type fakeLookup struct {
	known        map[string]ulid.ULID // skeleton -> the id of the row holding it
	unverifiable bool
	err          error

	gotSkeleton string
	gotExcluded *ulid.ULID
	calls       int
}

func (f *fakeLookup) SkeletonExists(_ context.Context, skeleton string, excluding *ulid.ULID) (bool, bool, error) {
	f.calls++
	f.gotSkeleton = skeleton
	f.gotExcluded = excluding

	if f.err != nil {
		return false, false, f.err
	}
	if f.unverifiable {
		return false, true, nil
	}
	owner, ok := f.known[skeleton]
	if ok && excluding != nil && owner.Compare(*excluding) == 0 {
		return false, false, nil // the only match is the caller's own row
	}
	return ok, false, nil
}

func newGate(f *fakeLookup) *charname.Gate { return &charname.Gate{Skeletons: f} }

func TestGateCheckAdmitsAWellFormedNameWithNoSkeletonCollision(t *testing.T) {
	g := newGate(&fakeLookup{known: map[string]ulid.ULID{}})

	got, skel, err := g.Check(t.Context(), "Alaric")

	require.NoError(t, err)
	assert.Equal(t, "Alaric", got.Display)
	assert.Equal(t, "alaric", got.Key)
	assert.NotEmpty(t, skel)
}

func TestGateCheckRefusesANameWhoseSkeletonMatchesAnExistingCharacter(t *testing.T) {
	seededID := idgen.New()
	seededName := "cocoa"
	lookup := &fakeLookup{known: map[string]ulid.ULID{
		charname.Skeleton(mustKey(t, seededName)): seededID,
	}}
	g := newGate(lookup)

	// A WHOLE-script homoglyph: every letter is Cyrillic — U+0441, U+043E and
	// U+0430 — and the string skeletons to "cocoa". Mechanism A permits it
	// because it is single-script, and Mechanism B is what catches it; that
	// division of labour is exactly what §6.1.2 says the second mechanism
	// exists for. A Latin+Cyrillic splice is now refused one step earlier and
	// would leave this assertion proving nothing about skeletons.
	_, _, err := g.Check(t.Context(), "\u0441\u043E\u0441\u043E\u0430")

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "NAME_CONFUSABLE")

	// Paired positive control on the SAME lookup: a non-confusable name is
	// admitted, so the refusal above cannot be "the gate refuses everything".
	_, _, ok := g.Check(t.Context(), "Brenna")
	require.NoError(t, ok)
}

func TestGateCheckConfusableRefusalNamesNeitherTheCollidingCharacterNorItsID(t *testing.T) {
	seededID := idgen.New()
	seededName := "Cocoa"
	g := newGate(&fakeLookup{known: map[string]ulid.ULID{
		charname.Skeleton(mustKey(t, seededName)): seededID,
	}})

	// Whole-script again, for the same reason: this test must exercise the
	// CONFUSABLE refusal message, and a mixed-script fixture would silently
	// retarget it at the mixed-script message instead.
	_, _, err := g.Check(t.Context(), "\u0421\u043E\u0441\u043E\u0430")

	require.Error(t, err)
	msg := err.Error()
	assert.NotContains(t, msg, seededName, "the message must not name the colliding character")
	assert.NotContains(t, msg, strings.ToLower(seededName), "nor its case-folded form")
	assert.NotContains(t, msg, seededID.String(), "the message must not carry the colliding id")
}

func TestGateCheckRefusesNamesTheSyntacticRulesReject(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "a digit", input: "Alaric2"},
		{name: "punctuation", input: "Al!aric"},
		{name: "a single rune", input: "A"},
		{name: "more runes than the maximum", input: strings.Repeat("a", syntax.MaxNameLength+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := &fakeLookup{known: map[string]ulid.ULID{}}
			g := newGate(lookup)

			_, _, err := g.Check(t.Context(), tt.input)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "NAME_INVALID_SYNTAX")

			var verr *syntax.ValidationError
			assert.True(t, errors.As(err, &verr), "the underlying *syntax.ValidationError survives in the chain")
			assert.Zero(t, lookup.calls, "the syntactic rule runs before the skeleton lookup")

			// Paired positive control on the SAME gate: an in-bounds
			// letters-and-spaces name is admitted.
			_, _, ok := g.Check(t.Context(), "Alaric")
			require.NoError(t, ok)
		})
	}
}

func TestGateCheckRefusesAMixedScriptNameWithoutEverReachingTheCorpus(t *testing.T) {
	lookup := &fakeLookup{known: map[string]ulid.ULID{}}
	g := newGate(lookup)

	// §6.1.2 Mechanism A. Cyrillic р (U+0440) and а (U+0430) spliced into an
	// otherwise Latin word — the cross-script splice the skeleton check alone
	// would only catch after a database round trip.
	_, _, err := g.Check(t.Context(), "\u0440\u0430ypal")

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "NAME_MIXED_SCRIPT")
	assert.Zero(t, lookup.calls, "a mixed-script name never reaches the corpus")

	// Paired positive control on the SAME gate, and the prohibition's guard:
	// a name written WHOLLY in Cyrillic is single-script, so Mechanism A must
	// admit it — and it must reach the corpus, because Mechanism B is what
	// covers a whole-script confusable.
	_, _, ok := g.Check(t.Context(), "\u0418\u0432\u0430\u043D")
	require.NoError(t, ok, "Mechanism A is not an English-only name policy")
	assert.Equal(t, 1, lookup.calls, "the permitted name does reach the corpus")
}

func TestGateCheckRunsTheSyntacticRuleOnThePostNormalizeDisplayForm(t *testing.T) {
	// Leading, trailing and doubled spaces are all things syntax.ValidateName
	// rejects on a RAW submission. They must be accepted here, because
	// normalization has already collapsed them by the time the rule sees the
	// string — proving the ordering rather than asserting it.
	require.Error(t, syntax.ValidateName("  John   Smith  "), "the raw submission is syntactically invalid")

	g := newGate(&fakeLookup{known: map[string]ulid.ULID{}})
	got, _, err := g.Check(t.Context(), "  John   Smith  ")

	require.NoError(t, err)
	assert.Equal(t, "John Smith", got.Display)
}

func TestGateCheckRefusesASubmissionWhoseNormalFormIsEmpty(t *testing.T) {
	lookup := &fakeLookup{known: map[string]ulid.ULID{}}
	g := newGate(lookup)

	_, _, err := g.Check(t.Context(), "\u200B\u200C\u200D")

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "NAME_EMPTY_NORMAL_FORM")
	assert.Zero(t, lookup.calls, "an empty normal form never reaches the corpus")

	// The invisible-only wording reaches the CALLER, not just Normalize: the
	// gate returns the pipeline's error unwrapped, so a player who submitted
	// three zero-width codepoints is not told to fill in the box they filled.
	assert.Contains(t, err.Error(), "no visible characters")

	_, _, blankErr := g.Check(t.Context(), "   ")
	require.Error(t, blankErr)
	assert.NotContains(t, blankErr.Error(), "no visible characters",
		"a genuinely blank submission still reads as a blank submission")
}

func TestGateCheckExcludesTheCallersOwnCharacterFromTheSkeletonLookup(t *testing.T) {
	// 01-SPEC.md §702-706: a request whose uniqueness key matches the current
	// one but whose display form differs is a REAL rename, and it does not
	// collide with itself. Both directions are asserted on ONE fixture, so the
	// success cannot pass because the seeded row was never written.
	seededID := idgen.New()
	lookup := &fakeLookup{known: map[string]ulid.ULID{
		charname.Skeleton(mustKey(t, "Alaric")): seededID,
	}}
	g := newGate(lookup)

	_, _, err := g.Check(t.Context(), "ALARIC", charname.ExcludingCharacter(seededID))
	require.NoError(t, err, "renaming a character to a case variant of its own name is not a collision")
	require.NotNil(t, lookup.gotExcluded, "the excluded id reaches the lookup")
	assert.Equal(t, seededID, *lookup.gotExcluded)

	_, _, err = g.Check(t.Context(), "ALARIC")
	require.Error(t, err, "the same submission with no exclusion collides")
	errutil.AssertErrorCode(t, err, "NAME_CONFUSABLE")

	_, _, err = g.Check(t.Context(), "ALARIC", charname.ExcludingCharacter(idgen.New()))
	require.Error(t, err, "excluding a DIFFERENT character does not excuse the collision")
	errutil.AssertErrorCode(t, err, "NAME_CONFUSABLE")
}

func TestGateCheckFailsClosedWhenTheSkeletonCorpusIsUnverifiable(t *testing.T) {
	// D-30: a gate that adjudicates against a half-populated name_skeleton
	// column admits a confusable of an existing row. Both directions asserted
	// on one fixture.
	lookup := &fakeLookup{known: map[string]ulid.ULID{}}
	g := newGate(lookup)

	lookup.unverifiable = false
	_, _, err := g.Check(t.Context(), "Alaric")
	require.NoError(t, err, "a verifiable corpus with no collision admits the name")

	lookup.unverifiable = true
	_, _, err = g.Check(t.Context(), "Alaric")
	require.Error(t, err, "the same name is refused once the corpus is unverifiable")
	errutil.AssertErrorCode(t, err, "NAME_SKELETON_UNVERIFIABLE")
}

func TestGateCheckSurfacesALookupFailureRatherThanTreatingItAsNoCollision(t *testing.T) {
	g := newGate(&fakeLookup{err: errors.New("connection refused")})

	_, _, err := g.Check(t.Context(), "Alaric")

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "NAME_SKELETON_LOOKUP_FAILED")
}

func TestGateCheckPassesTheSkeletonOfTheCaseFoldedKeyToTheLookup(t *testing.T) {
	lookup := &fakeLookup{known: map[string]ulid.ULID{}}
	g := newGate(lookup)

	_, skel, err := g.Check(t.Context(), "Alaric")

	require.NoError(t, err)
	assert.Equal(t, charname.Skeleton("alaric"), lookup.gotSkeleton)
	assert.Equal(t, lookup.gotSkeleton, skel, "the returned skeleton is the one that was adjudicated")
	assert.Nil(t, lookup.gotExcluded, "no exclusion is passed when no option was given")
}

// recordingBlockList is a charname.BlockList double. It records the string it
// was handed so the "which form is evaluated?" question is asserted
// structurally rather than inferred from a verdict — a list that happened to
// match both the display form and the key would prove nothing.
type recordingBlockList struct {
	blockedIndex int    // -1 means "match nothing"
	blocks       string // the exact input that is considered blocked

	got   string
	calls int
}

func (r *recordingBlockList) Match(normalized string) (bool, int) {
	r.calls++
	r.got = normalized
	if r.blocks != "" && normalized == r.blocks {
		return true, r.blockedIndex
	}
	return false, -1
}

func TestGateCheckEvaluatesTheBlockListAgainstTheCaseFoldedKey(t *testing.T) {
	lookup := &fakeLookup{known: map[string]ulid.ULID{}}
	list := &recordingBlockList{blockedIndex: -1}
	g := &charname.Gate{Skeletons: lookup, BlockList: list}

	_, _, err := g.Check(t.Context(), "Alaric")

	require.NoError(t, err)
	assert.Equal(t, 1, list.calls, "the block list is consulted exactly once")
	assert.Equal(t, mustKey(t, "Alaric"), list.got,
		"the key, never the display form and never the raw submission")
	assert.NotEqual(t, "Alaric", list.got, "the display form must NOT be what is evaluated")
}

func TestGateCheckRefusesABlockedNameWithoutEverReachingTheCorpus(t *testing.T) {
	lookup := &fakeLookup{known: map[string]ulid.ULID{}}
	list := &recordingBlockList{blockedIndex: 3, blocks: mustKey(t, "Admin")}
	g := &charname.Gate{Skeletons: lookup, BlockList: list}

	_, _, err := g.Check(t.Context(), "Admin")

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "NAME_BLOCKED")
	assert.Zero(t, lookup.calls, "a blocked name costs no database round trip")
}

func TestGateCheckBlockedRefusalEchoesNeitherThePatternNorItsIndex(t *testing.T) {
	lookup := &fakeLookup{known: map[string]ulid.ULID{}}
	list := &recordingBlockList{blockedIndex: 7, blocks: mustKey(t, "Admin")}
	g := &charname.Gate{Skeletons: lookup, BlockList: list}

	_, _, err := g.Check(t.Context(), "Admin")

	require.Error(t, err)
	msg := err.Error()
	assert.NotContains(t, msg, "7", "the matched pattern's index is operator configuration")
	assert.NotContains(t, strings.ToLower(msg), "pattern")
	assert.NotContains(t, strings.ToLower(msg), "block list")
	// Non-vacuity: the message is not empty, so NotContains cannot pass by
	// asserting over nothing.
	assert.NotEmpty(t, msg)
}

func TestGateCheckAdmitsANameOnTheSameFixtureThatTheBlockListDoesNotMatch(t *testing.T) {
	// Paired positive control (PORTAL-10 rule 2): without it, the refusal
	// above cannot distinguish "blocked by the list" from "the gate rejects
	// everything".
	lookup := &fakeLookup{known: map[string]ulid.ULID{}}
	list := &recordingBlockList{blockedIndex: 0, blocks: mustKey(t, "Admin")}
	g := &charname.Gate{Skeletons: lookup, BlockList: list}

	normalized, _, err := g.Check(t.Context(), "Alaric")

	require.NoError(t, err)
	assert.Equal(t, "Alaric", normalized.Display)
}

func TestGateCheckTreatsANilBlockListAsNoListConfiguredRatherThanBlockEverything(t *testing.T) {
	g := &charname.Gate{Skeletons: &fakeLookup{known: map[string]ulid.ULID{}}}

	_, _, err := g.Check(t.Context(), "Alaric")

	require.NoError(t, err)
}

func TestGateCheckEvaluatesTheBlockListAfterTheMixedScriptRule(t *testing.T) {
	// A cross-script splice is decidable from the submission alone, so it is
	// refused before the list is even consulted. Asserted on the double's call
	// counter rather than on the returned code, which both orderings satisfy.
	list := &recordingBlockList{blockedIndex: -1}
	g := &charname.Gate{Skeletons: &fakeLookup{known: map[string]ulid.ULID{}}, BlockList: list}

	_, _, err := g.Check(t.Context(), "раypal")

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "NAME_MIXED_SCRIPT")
	assert.Zero(t, list.calls)
}

func mustKey(t *testing.T, name string) string {
	t.Helper()
	n, err := charname.Normalize(name)
	require.NoError(t, err)
	return n.Key
}

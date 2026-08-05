// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/pkg/errutil"
)

// recordingRenamer records every Rename it is handed, so a test can assert that
// a refused name performed NO write rather than merely that the command exited
// non-zero.
type recordingRenamer struct {
	calls []string
}

func (r *recordingRenamer) Rename(
	_ context.Context,
	id ulid.ULID,
	name charname.Admitted,
	_ int,
	_ wmodel.EnvelopeIntent,
) (*wmodel.MutationDelta, error) {
	r.calls = append(r.calls, id.String()+"="+name.Display())
	return &wmodel.MutationDelta{}, nil
}

// emptyCorpusSkeletons is a SkeletonLookup over an empty, VERIFIABLE corpus: no
// collisions and nothing unverifiable, so the gate's verdict on a submitted name
// is decided entirely by the pipeline, the syntax rules and the block list.
type emptyCorpusSkeletons struct{}

func (emptyCorpusSkeletons) SkeletonExists(
	_ context.Context, _ string, _ *ulid.ULID,
) (bool, bool, error) {
	return false, false, nil
}

// noBlockList matches nothing, mirroring an operator who configured no list.
type noBlockList struct{}

func (noBlockList) Match(string) (bool, int) { return false, -1 }

// newTestCharacterNameEnv builds a command environment around a REAL
// charname.Gate — there is no test escape hatch for charname.Admitted by design
// — and a recording writer.
func newTestCharacterNameEnv() (*characterNameEnv, *recordingRenamer) {
	writer := &recordingRenamer{}
	return &characterNameEnv{
		gate: &charname.Gate{Skeletons: emptyCorpusSkeletons{}, BlockList: noBlockList{}},
		repo: writer,
	}, writer
}

// runCharacterNameSet drives the real `name set` command with an injected
// environment and returns its error plus captured stdout.
func runCharacterNameSet(t *testing.T, env *characterNameEnv, args ...string) (string, error) {
	t.Helper()
	cmd := newCharacterNameCmd(func(context.Context) (*characterNameEnv, func(), error) {
		return env, func() {}, nil
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(append([]string{"set"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

// TestCharacterNameSetRefusesASyntacticallyInvalidReplacementWithoutWriting is
// the direct cover for the C2-1 defect.
//
// Gate.Admit is this command's ONLY validity check, and it is sufficient only
// because plan 02-01's Gate.Check runs world.ValidateCharacterName on the
// normalized display form. Were the token to prove LESS than the writer
// requires, an operator could seat a name carrying digits, punctuation or 200
// runes onto a player's character. Each refusal is paired on the same fixture
// with an ordinary replacement that DOES write, so a refusal cannot pass because
// renaming is broken.
func TestCharacterNameSetRefusesASyntacticallyInvalidReplacementWithoutWriting(t *testing.T) {
	characterID := ulid.Make()

	tests := []struct {
		name        string
		replacement string
	}{
		{"refuses a replacement carrying digits", "Ariel2"},
		{"refuses a replacement carrying punctuation", "Ariel!"},
		{"refuses an over-long replacement", strings.Repeat("a", 200)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, writer := newTestCharacterNameEnv()

			_, err := runCharacterNameSet(t, env, characterID.String(), tt.replacement)
			require.Error(t, err, "the gate must refuse this replacement")
			// The gate's own code, surfaced unchanged: all three are syntactic
			// rule violations, and Gate.Check subsumes world.ValidateCharacterName
			// (02-01's C2-1 settlement) which is what makes Admit sufficient as
			// this command's ONLY validity check.
			errutil.AssertErrorCode(t, err, "NAME_INVALID_SYNTAX")
			assert.Empty(t, writer.calls,
				"a refused replacement must never reach the writer")

			// Paired positive control on the SAME fixture.
			out, err := runCharacterNameSet(t, env, characterID.String(), "Ariel")
			require.NoError(t, err, "an ordinary replacement must succeed")
			require.Len(t, writer.calls, 1, "the accepted replacement must reach the writer exactly once")
			assert.Equal(t, characterID.String()+"=Ariel", writer.calls[0])
			assert.Contains(t, out, "renamed")
		})
	}
}

// TestCharacterNameSetRejectsAMalformedCharacterIDBeforeTouchingTheDatabase
// asserts argument validation precedes any environment construction — an
// operator typo must not open a pool.
func TestCharacterNameSetRejectsAMalformedCharacterIDBeforeTouchingTheDatabase(t *testing.T) {
	factoryCalls := 0
	cmd := newCharacterNameCmd(func(context.Context) (*characterNameEnv, func(), error) {
		factoryCalls++
		env, _ := newTestCharacterNameEnv()
		return env, func() {}, nil
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"set", "not-a-ulid", "Ariel"})

	err := cmd.Execute()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CHARACTER_ID_INVALID")
	assert.Equal(t, 0, factoryCalls, "a malformed id must be refused before a pool is opened")
}

// TestDuplicateReportDistinguishesNormalizedNameFromSkeletonCollisions asserts
// the two kinds are labelled differently.
//
// Reporting them undifferentiated is the failure mode: a skeleton set is two
// rows migration 000056's UNIQUE index will happily let coexist — their
// normalized names DIFFER, which is exactly what makes them confusable — so an
// operator shown it beside a normalized-name set with no distinction reads it as
// a false positive and dismisses it.
func TestDuplicateReportDistinguishesNormalizedNameFromSkeletonCollisions(t *testing.T) {
	sets := []postgres.IdentityCollisionSet{
		{
			Kind: postgres.CollisionNormalizedName,
			Key:  "cocoa",
			Members: []postgres.IdentityCollisionMember{
				{ID: "01A", Name: "Cocoa", PlayerID: "p1", CreatedAt: 1},
				{ID: "01B", Name: "COCOA", PlayerID: "p2", CreatedAt: 2},
			},
		},
		{
			Kind: postgres.CollisionSkeleton,
			Key:  "cocoa",
			Members: []postgres.IdentityCollisionMember{
				{ID: "01C", Name: "cocoa", PlayerID: "p3", CreatedAt: 3},
				{ID: "01D", Name: "сосоа", PlayerID: "p4", CreatedAt: 4},
			},
		},
	}

	var out bytes.Buffer
	printDuplicateReport(&out, sets)
	got := out.String()

	assert.Contains(t, got, "NORMALIZED-NAME")
	assert.Contains(t, got, "SKELETON")
	assert.Contains(t, got, "would NOT catch this",
		"the skeleton label must say the unique index does not reach these rows")
	// D-17: enough context to decide which character keeps the name.
	for _, want := range []string{"01A", "01B", "01C", "01D", "p1", "p4", "created_at=4"} {
		assert.Contains(t, got, want)
	}
	assert.Contains(t, got, "holomush character name set")
}

// TestDuplicateReportSaysNothingIsWrongWhenThereAreNoCollisions is the paired
// control: the report distinguishes "clean" from "found some", so the assertions
// above cannot pass because the renderer prints everything unconditionally.
func TestDuplicateReportSaysNothingIsWrongWhenThereAreNoCollisions(t *testing.T) {
	var out bytes.Buffer
	printDuplicateReport(&out, nil)
	got := out.String()

	assert.Contains(t, got, "No character-name collisions found")
	assert.NotContains(t, got, "NORMALIZED-NAME")
	assert.NotContains(t, got, "SKELETON")
}

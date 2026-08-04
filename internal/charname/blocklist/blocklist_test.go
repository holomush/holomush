// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package blocklist_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname/blocklist"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestCompileTreatsAnAbsentListAsAValidEmptySnapshotRatherThanBlockEverything(t *testing.T) {
	for _, tt := range []struct {
		name     string
		patterns []string
	}{
		{"a nil list", nil},
		{"an explicitly empty list", []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			snap, err := blocklist.Compile(tt.patterns)

			require.NoError(t, err)
			require.NotNil(t, snap)

			blocked, idx := snap.Match("alaric")
			assert.False(t, blocked, "an empty list rejects nothing")
			assert.Equal(t, -1, idx)
		})
	}
}

func TestCompileNamesTheOffendingPatternVerbatimWhenItDoesNotCompile(t *testing.T) {
	const bad = "(unclosed"

	_, err := blocklist.Compile([]string{bad})

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "BLOCKLIST_PATTERN_INVALID")
	errutil.AssertErrorContext(t, err, "pattern", bad)
	assert.Contains(t, err.Error(), bad,
		"the operator who wrote the pattern must be able to find it from the message alone")
}

func TestCompileReportsWhichListEntryFailedRatherThanMerelyThatOneDid(t *testing.T) {
	const bad = "(unclosed"

	// The offending entry sits between two valid ones, so "a pattern failed"
	// would leave the operator grepping three candidates.
	_, err := blocklist.Compile([]string{"^ok$", bad, "^also$"})

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "BLOCKLIST_PATTERN_INVALID")
	errutil.AssertErrorContext(t, err, "pattern", bad)
	errutil.AssertErrorContext(t, err, "index", 1)
}

func TestCompileAcceptsAValidListAndMatchesInStoredOrder(t *testing.T) {
	snap, err := blocklist.Compile([]string{"^ok$", "^blocked$"})
	require.NoError(t, err)

	blocked, idx := snap.Match("blocked")
	assert.True(t, blocked)
	assert.Equal(t, 1, idx)

	blocked, idx = snap.Match("alaric")
	assert.False(t, blocked)
	assert.Equal(t, -1, idx)
}

func TestMatchReturnsTheFirstMatchingPatternWhenTwoEntriesMatchTheSameName(t *testing.T) {
	// Both patterns match "admin". Neither is merged nor deduplicated: the
	// name is rejected once, on the FIRST match, in stored list order.
	snap, err := blocklist.Compile([]string{"adm", "^admin$"})
	require.NoError(t, err)

	blocked, idx := snap.Match("admin")

	assert.True(t, blocked)
	assert.Equal(t, 0, idx, "evaluation is in stored order and the first match decides")
}

func TestCompilingTheSameListTwiceYieldsSnapshotsThatAgreeOnEveryInput(t *testing.T) {
	patterns := []string{"^admin$", "moderator", "^s+t?aff$"}
	corpus := []string{"admin", "Admin", "moderator", "the moderator", "staff", "saff", "alaric", ""}

	first, err := blocklist.Compile(patterns)
	require.NoError(t, err)
	second, err := blocklist.Compile(patterns)
	require.NoError(t, err)

	for _, in := range corpus {
		gotBlocked, gotIdx := first.Match(in)
		wantBlocked, wantIdx := second.Match(in)
		assert.Equal(t, wantBlocked, gotBlocked, "verdicts disagree for %q", in)
		assert.Equal(t, wantIdx, gotIdx, "indices disagree for %q", in)
	}
}

func TestMatchEvaluatesTheCallerSuppliedStringExactlyAsGiven(t *testing.T) {
	// The snapshot performs no folding of its own; the GATE is what supplies
	// the case-folded key (asserted in the charname package's gate tests).
	snap, err := blocklist.Compile([]string{"^admin$"})
	require.NoError(t, err)

	blocked, _ := snap.Match("admin")
	assert.True(t, blocked)

	blocked, _ = snap.Match("ADMIN")
	assert.False(t, blocked, "Match does not fold; that is the gate's job")
}

func TestMatchDoesNotHandTheCallerThePatternTextItMatched(t *testing.T) {
	// Match's signature is the whole assertion: it returns (bool, int) and no
	// string, so a rejection path physically cannot echo operator
	// configuration back to a submitter.
	snap, err := blocklist.Compile([]string{"^s3cr3t-policy$"})
	require.NoError(t, err)

	blocked, idx := snap.Match("s3cr3t-policy")

	require.True(t, blocked)
	assert.Equal(t, 0, idx)
	// Belt and braces: everything a caller can obtain from the verdict, rendered,
	// still names no pattern — so the assertion runs against a real value rather
	// than a constant.
	rendered := fmt.Sprintf("blocked=%v index=%d", blocked, idx)
	assert.NotContains(t, strings.ToLower(rendered), "s3cr3t")
	assert.NotEmpty(t, rendered)
}

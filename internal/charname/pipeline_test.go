// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname_test

import (
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/pkg/errutil"
)

// Every invisible codepoint in this file is spelled as a \uXXXX escape rather
// than as a literal character. A literal zero-width joiner in source is
// indistinguishable from a typo, and a fixture nobody can read is a fixture
// nobody can audit.

func TestNormalizeFoldsCompatibilityFormsCollapsesWhitespaceAndCaseFoldsTheKey(t *testing.T) {
	// U+FF21 U+FF22 fullwidth A and B, U+3000 ideographic space, U+FF43
	// fullwidth c, padded with two ASCII spaces on each side. NFKC turns the
	// fullwidth forms into their ASCII counterparts and the ideographic space
	// into U+0020; whitespace canonicalization then collapses the padding.
	got, err := charname.Normalize("  \uFF21\uFF22\u3000\uFF43  ")
	require.NoError(t, err)

	assert.Equal(t, "AB c", got.Display)
	assert.Equal(t, "ab c", got.Key)
}

func TestNormalizeRemovesFormatCodepointsRatherThanPreservingThem(t *testing.T) {
	// U+200D ZERO WIDTH JOINER renders as nothing, so "a<ZWJ>b" and "ab" are
	// visually identical. Step 2 strips every Cf codepoint so the two collapse
	// to one name rather than seating two.
	got, err := charname.Normalize("a\u200Db")
	require.NoError(t, err)

	assert.Equal(t, "ab", got.Display)
	assert.Equal(t, "ab", got.Key)
}

func TestNormalizeRejectsSubmissionsWhoseNormalFormIsEmpty(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "only invisible format codepoints", input: "\u200B\u200C\u200D"},
		{name: "only ASCII spaces", input: "   "},
		{name: "the empty string", input: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := charname.Normalize(tt.input)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "NAME_EMPTY_NORMAL_FORM")
		})
	}
}

func TestNormalizeTellsAnInvisibleOnlyNameApartFromAnEmptyBox(t *testing.T) {
	// The 009 name-pipeline sketch finding: a name of only invisibles LOOKS
	// like an empty box to the player, so "please enter a name" is the one
	// message that cannot help them — they did enter something. The two cases
	// share a code (both normalize to nothing) but must not share wording.
	_, blankErr := charname.Normalize("   ")
	require.Error(t, blankErr)
	errutil.AssertErrorCode(t, blankErr, "NAME_EMPTY_NORMAL_FORM")

	_, invisibleErr := charname.Normalize("\u200B\u200C\u200D")
	require.Error(t, invisibleErr)
	errutil.AssertErrorCode(t, invisibleErr, "NAME_EMPTY_NORMAL_FORM")

	assert.NotEqual(t, blankErr.Error(), invisibleErr.Error(),
		"a name of only invisibles and a blank box must not read identically")
	assert.Contains(t, invisibleErr.Error(), "no visible characters",
		"the invisible-only message says what is actually wrong")
	assert.NotContains(t, blankErr.Error(), "no visible characters",
		"the blank-box message keeps the wording that fits a blank box")
}

func TestNormalizeLeavesNoFormatCodepointInTheStoredDisplayForm(t *testing.T) {
	// Step 2's guarantee stated as a property over the OUTPUT rather than as a
	// spot check on one codepoint: whatever the submission padded the name
	// with, nothing of general category Cf survives into the value that gets
	// stored and rendered.
	got, err := charname.Normalize("Al\u200Baric\u200D")
	require.NoError(t, err)

	for _, r := range got.Display {
		assert.False(t, unicode.Is(unicode.Cf, r),
			"U+%04X is a format codepoint and must not survive into the display form", r)
	}
	assert.Equal(t, "Alaric", got.Display)
	assert.Equal(t, "alaric", got.Key)
}

func TestNormalizeUsesUnicodeFullCaseFoldingRatherThanASCIILowercasing(t *testing.T) {
	// An ASCII-oriented lowercase leaves "straße" unchanged; Unicode FULL
	// case folding expands U+00DF to "ss", which is what makes "Straße" and
	// "STRASSE" one name rather than two.
	got, err := charname.Normalize("stra\u00DFe")
	require.NoError(t, err)

	assert.Equal(t, "strasse", got.Key)
}

func TestNormalizePreservesThePlayersOwnCapitalizationInTheDisplayForm(t *testing.T) {
	got, err := charname.Normalize("Alaric")
	require.NoError(t, err)

	assert.Equal(t, "Alaric", got.Display, "the display name is never case-folded")
	assert.Equal(t, "alaric", got.Key)
}

func TestNormalizeMapsCaseVariantsOfOneNameOntoOneKey(t *testing.T) {
	lower, err := charname.Normalize("alaric")
	require.NoError(t, err)
	upper, err := charname.Normalize("ALARIC")
	require.NoError(t, err)

	assert.Equal(t, lower.Key, upper.Key)
	assert.NotEqual(t, lower.Display, upper.Display, "the display forms stay distinct")
}

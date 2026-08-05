// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/pkg/errutil"
)

// Every non-ASCII codepoint in this file is spelled as a \uXXXX escape. A
// fixture whose Cyrillic and Latin letters are visually identical is a fixture
// nobody can audit by reading it — which is precisely the attack this file is
// about.

func TestMixedScriptAppliesTheModeratelyRestrictiveVerdictTable(t *testing.T) {
	// One row per 01-SPEC.md §6.1.2 Mechanism A verdict. The permitted and the
	// rejected rows live in ONE table on purpose (PORTAL-10 rule 2): a suite
	// that asserted only rejections would pass against a function that rejects
	// every name, which is exactly the failure mode the prohibition names.
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// A single script — permitted, whichever script it is.
		{name: "a name written wholly in Latin", input: "Alaric"},
		{name: "a name written wholly in Cyrillic", input: "\u0418\u0432\u0430\u043D"},
		{name: "a name written wholly in Greek", input: "\u0391\u03BB\u03B5\u03BE"},
		{name: "a name written wholly in Han", input: "\u5C71\u7530"},
		{name: "a name written wholly in Hiragana", input: "\u3055\u304F\u3089"},
		{name: "a name written wholly in Katakana", input: "\u30B5\u30AF\u30E9"},
		{name: "a name written wholly in Hangul", input: "\uD55C\uAD6D"},
		{name: "a name written wholly in Arabic", input: "\u0645\u062D\u0645\u062F"},

		// Latin plus any of Han, Hiragana, Katakana — the Japanese row.
		{name: "Latin with Hiragana", input: "Ken\u3055\u3093"},
		{name: "Latin with Katakana", input: "Ken\u30B5\u30F3"},
		{name: "Latin with Han", input: "Ken\u5C71"},
		{name: "Han with Hiragana and no Latin at all", input: "\u5C71\u3055\u3093"},

		// Latin plus Han plus Bopomofo — the Chinese row.
		{name: "Latin with Han and Bopomofo", input: "Ken\u5C71\u3105"},

		// Latin plus Han plus Hangul — the Korean row.
		{name: "Latin with Han and Hangul", input: "Ken\u5C71\uD55C"},

		// The three explicitly named rejections. U+0440 and U+0430 are the
		// Cyrillic homoglyphs of Latin p and a; U+03BF is the Greek homoglyph
		// of Latin o.
		{name: "Latin spliced with Cyrillic", input: "\u0440\u0430ypal", wantErr: true},
		{name: "Latin spliced with Greek", input: "Al\u03BFric", wantErr: true},
		{name: "Cyrillic spliced with Greek", input: "\u0418\u03BB", wantErr: true},

		// Any other combination of two or more scripts.
		{name: "Latin spliced with Arabic", input: "Ali\u0645", wantErr: true},
		{name: "Cyrillic spliced with Hangul", input: "\u0418\uD55C", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := charname.MixedScript(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, "NAME_MIXED_SCRIPT")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMixedScriptTreatsCommonAndInheritedCodepointsAsScriptNeutral(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		// Hyphen, apostrophe and space are all script Common.
		{name: "a Latin name carrying ASCII punctuation and spaces", input: "Anne-Marie O'Neill"},
		// U+0301 COMBINING ACUTE ACCENT is script Inherited.
		{name: "a Latin name carrying a combining mark", input: "Alaric\u0301"},
		// Digits are Common too, even though the syntactic rules reject them.
		{name: "a Latin name carrying an ASCII digit", input: "Alaric2"},
		// A Cyrillic name plus Common punctuation stays single-script. Every
		// letter here is Cyrillic: an ASCII apostrophe-O would have smuggled a
		// Latin letter into the fixture and made this row assert the opposite
		// of what it claims.
		{name: "a Cyrillic name carrying a hyphen and a space", input: "\u0418\u0432\u0430\u043D \u0410-\u041F\u0435\u0442\u0440"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, charname.MixedScript(tt.input))
		})
	}
}

func TestMixedScriptRejectsARuneOutsideEveryStdlibScriptRange(t *testing.T) {
	// P-12, the Unicode version skew: Go's unicode.Scripts is generated from
	// Unicode 15.0.0 while the confusables data this package generates is
	// 17.0.0. A rune assigned after 15.0.0 belongs to no stdlib script range,
	// so treating it as script-neutral would let a genuinely two-script name
	// read as single-script. It is rejected instead.
	tests := []struct {
		name  string
		input string
	}{
		// U+0378 is permanently reserved: no Unicode version assigns it.
		{name: "a permanently reserved codepoint", input: "Alaric\u0378"},
		// U+105C0 TODHRI LETTER A was assigned in Unicode 16.0.0. When Go's
		// tables catch up this row goes red — that is the version-skew drift
		// signal this test exists to raise, not a flake. Re-point the fixture
		// at a codepoint from a newer release and re-read ScriptSet's doc
		// comment before changing anything else.
		{name: "a letter assigned after the stdlib tables were generated", input: "Alaric\U000105C0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := charname.MixedScript(tt.input)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "NAME_UNASSIGNED_SCRIPT")

			// Paired positive control on the same shape: the identical name
			// without the unassigned rune is permitted, so the rejection above
			// is attributable to that rune rather than to "Alaric" itself.
			require.NoError(t, charname.MixedScript("Alaric"))
		})
	}
}

func TestMixedScriptRejectionNamesTheScriptCombinationButNoExistingCharacter(t *testing.T) {
	// PORTAL-10 rule 5, asserted over the message the caller receives. The
	// combination is derived entirely from the submitter's own input, so
	// naming it discloses nothing about the corpus; naming a character would.
	err := charname.MixedScript("\u0440\u0430ypal")

	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "Cyrillic")
	assert.Contains(t, msg, "Latin")
}

func TestScriptSetReturnsASortedDeduplicatedSetWithNeutralCodepointsExcluded(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "a single-script Latin name", input: "Alaric", want: []string{"Latin"}},
		{name: "a name whose runes repeat one script", input: "aaa", want: []string{"Latin"}},
		{
			name:  "a name whose scripts appear out of alphabetical order",
			input: "ypal\u0440\u0430",
			want:  []string{"Cyrillic", "Latin"},
		},
		{
			name:  "a name whose only extra codepoints are Common and Inherited",
			input: "Anne-Marie\u0301 O'Neill",
			want:  []string{"Latin"},
		},
		{name: "a string of nothing but script-neutral codepoints", input: "- '", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := charname.ScriptSet(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestScriptSetIsOrderIndependent(t *testing.T) {
	forward, err := charname.ScriptSet("\u0440\u0430ypal")
	require.NoError(t, err)
	reversed, err := charname.ScriptSet("ypal\u0430\u0440")
	require.NoError(t, err)

	assert.Equal(t, forward, reversed, "the set is sorted, so the verdict cannot depend on rune order")
}

func TestScriptSetSurfacesAnUnassignedRuneRatherThanSkippingIt(t *testing.T) {
	_, err := charname.ScriptSet("Alaric\u0378")

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "NAME_UNASSIGNED_SCRIPT")
}

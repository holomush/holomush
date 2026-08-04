// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname

import (
	"strings"
	"unicode"

	"github.com/samber/oops"
	"golang.org/x/text/cases"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Normalized is the result of the §6.1.1 pipeline: the name as it will be
// displayed, and the key equality is decided on.
//
// Display preserves the capitalization the player chose. Key is the
// case-folded uniqueness key. They are deliberately separate values — a system
// that stores only one of them either denies players their own capitalization
// or decides equality on a form that is not equality.
type Normalized struct {
	// Display is the name as it will be shown: NFKC-folded, format codepoints
	// stripped, whitespace canonicalized, capitalization untouched.
	Display string

	// Key is Display put through Unicode FULL case folding. Two names are the
	// same name exactly when their Keys are byte-equal.
	Key string
}

// nameTransform is steps 1 and 2 of §6.1.1, in that order.
//
// Step 2 (stripping general-category Cf) is the reason "a<ZWJ>b" and "ab"
// cannot both be seated: Cf codepoints render as nothing, so they are pure
// padding for producing two byte-distinct strings that look identical.
var nameTransform = transform.Chain(
	norm.NFKC,                          // step 1: compatibility composition
	runes.Remove(runes.In(unicode.Cf)), // step 2: strip format codepoints
)

// caseFolder performs Unicode FULL case folding, not an ASCII-oriented
// lowercase.
//
// The difference is load-bearing: lowercasing "straße" leaves it unchanged, while full
// folding expands U+00DF to "ss", so "Straße" and "STRASSE" collapse onto one
// key. An ASCII-oriented lowercase leaves that pair as two distinct names.
var caseFolder = cases.Fold()

// Normalize runs the §6.1.1 pipeline over a submitted character name:
//
//  1. NFKC
//  2. remove every general-category Cf codepoint
//  3. canonicalize whitespace — trim, and collapse every run to a single U+0020
//  4. Unicode full case fold, producing the uniqueness key
//
// Steps 1-3 produce Display; step 4 additionally produces Key.
//
// A submission whose normal form is empty — all whitespace, all Cf, or
// anything else the pipeline removes — is rejected with NAME_EMPTY_NORMAL_FORM
// and is never stored. Without that check an invisible-only submission would
// seat a character whose name renders as nothing.
func Normalize(submitted string) (Normalized, error) {
	folded, _, err := transform.String(nameTransform, submitted)
	if err != nil {
		return Normalized{}, oops.
			Code("NAME_EMPTY_NORMAL_FORM").
			With("reason", "normalization transform failed").
			Wrap(err)
	}

	// Step 3. strings.Fields splits on every Unicode whitespace run and drops
	// empties, so Join with a single space trims and collapses in one pass.
	display := strings.Join(strings.Fields(folded), " ")
	if display == "" {
		return Normalized{}, oops.
			Code("NAME_EMPTY_NORMAL_FORM").
			Errorf("name normalizes to the empty string")
	}

	return Normalized{
		Display: display,
		Key:     caseFolder.String(display),
	}, nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname

import (
	"strings"
	"unicode"

	"github.com/samber/oops"
	"golang.org/x/text/cases"
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

// # No package-level x/text transformer lives here, and that is deliberate
//
// Steps 1, 2 and 4 below were once a shared `transform.Chain(norm.NFKC,
// runes.Remove(runes.In(unicode.Cf)))` and a shared `cases.Fold()`, held in
// package-level vars. Both are STATEFUL, and Normalize is called concurrently
// in production — two players creating characters at the same moment is the
// ordinary case, and the guest path retries in a loop.
//
//   - cases.Caser's own doc: "A Caser may be stateful and should therefore not
//     be shared between goroutines."
//   - transform.Chain returns a *chain carrying mutable `link`, `err` and
//     `errStart` fields plus a read/write buffer per link.
//
// Sharing either interleaves one call's Reset with another's Transform. The
// observable results were a truncated or garbled display form — which the
// syntactic rule then rejects, on a name that is unambiguously letters-only —
// and, when the link buffers' indices crossed, `slice bounds out of range`
// inside transform.String. Neither is theoretical: both were reproduced, and
// they are what made 02-12's concurrent-claim spec intermittent.
//
// So this pipeline uses only forms that carry no shared state:
//
//   - norm.Form is a `type Form int` whose String() allocates its own buffer.
//   - stripFormatRunes is a pure strings.Map.
//   - the Caser is constructed PER CALL and never escapes.
//
// Do not hoist any of them back into a package-level var for the allocation.
// Normalize runs on character create, rename, guest-name generation and the
// 000055 backfill — none of them hot enough to trade correctness for.

// stripFormatRunes removes every general-category Cf codepoint.
//
// This is step 2 of §6.1.1 and the reason "a<ZWJ>b" and "ab" cannot both be
// seated: Cf codepoints render as nothing, so they are pure padding for
// producing two byte-distinct strings that look identical.
func stripFormatRunes(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

// foldCase performs Unicode FULL case folding, not an ASCII-oriented lowercase.
//
// The difference is load-bearing: lowercasing "straße" leaves it unchanged, while full
// folding expands U+00DF to "ss", so "Straße" and "STRASSE" collapse onto one
// key. An ASCII-oriented lowercase leaves that pair as two distinct names.
//
// The Caser is built here rather than reused: see the note above.
func foldCase(s string) string {
	return cases.Fold().String(s)
}

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
	// Steps 1 and 2, in that order. norm.Form.String allocates its own buffer
	// and stripFormatRunes is a pure map, so neither carries state between
	// calls — see the note above stripFormatRunes for why that matters.
	folded := stripFormatRunes(norm.NFKC.String(submitted))

	// Step 3. strings.Fields splits on every Unicode whitespace run and drops
	// empties, so Join with a single space trims and collapses in one pass.
	display := strings.Join(strings.Fields(folded), " ")
	if display == "" {
		// One code, two messages, and the split is not cosmetic.
		//
		// A submission of nothing but invisibles LOOKS to the player exactly
		// like a blank box: they typed something, they can see nothing, and
		// "please enter a name" tells them to do the thing they believe they
		// already did. The 009 name-pipeline sketch finding names this as the
		// case that needs its own wording. The code stays shared because the
		// server-side fact is the same — the name has no normal form and is
		// never stored — so callers matching on the code keep working.
		if strings.TrimSpace(submitted) == "" {
			return Normalized{}, oops.
				Code("NAME_EMPTY_NORMAL_FORM").
				With("reason", "blank submission").
				Errorf("please enter a character name")
		}

		return Normalized{}, oops.
			Code("NAME_EMPTY_NORMAL_FORM").
			With("reason", "no visible codepoints survived normalization").
			Errorf("that name contains no visible characters; please use letters")
	}

	return Normalized{
		Display: display,
		Key:     foldCase(display),
	}, nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname

import "context"

// Admitted is the admission token: proof that a character display name passed
// the WHOLE Gate.Check decision, carried as a value a writer can require.
//
// # What the token attests
//
// Gate.Check subsumes its sub-rules deliberately (see its doc comment), so one
// successful Admit proves, in a single value:
//
//   - §6.1.1 normalization — NFKC, format-codepoint removal, whitespace
//     canonicalization, and Unicode full case folding
//   - the syntactic rules — rune-length bounds, letters and spaces only, no
//     leading, trailing or doubled spaces
//   - §6.1.2 Mechanism A — no cross-script splice
//   - IDENT-07 — no operator-configured block-list match
//   - §6.1.3/D-30 — no UTS #39 skeleton collision against the stored corpus,
//     adjudicated against a corpus that could actually answer the question
//
// The consequence is the point: a writer holding an Admitted needs NO second
// check. Adding one at a call site is a signal that either the gate or the
// writer is wrong — it is not a belt-and-braces improvement, because it
// re-establishes exactly the two-proofs convention this type exists to replace,
// and the next writer added elsewhere copies the shape without the gate.
//
// # Why the type exists at all
//
// ROADMAP success criterion 1 and IDENT-09 require the name checks at create
// AND at rename. In this phase no rename path exists — RenameCharacter is
// Phase 3's — so the rename half cannot be discharged by a test. It is
// discharged STRUCTURALLY instead: every name-writing method in the sanctioned
// writer boundary takes an Admitted, and (*Gate).Admit is its only constructor,
// so a Phase 3 write site that does not run the gate is not expressible in Go
// rather than merely being caught by a review or a census.
//
// That guarantee IS the single constructor. A convenience escape hatch —
// FromString, Unchecked, Raw, or an exported field admitting a struct literal —
// would void it at zero apparent cost and would read as a small refactor.
// test/meta/character_name_admission_test.go rule C asserts mechanically that
// exactly one function under internal/charname returns this type.
//
// The zero value is the only Admitted another package can build, and it is
// detectable: IsZero reports true for it, and every writer rejects it before
// issuing SQL.
type Admitted struct {
	display        string
	key            string
	skeleton       string
	unicodeVersion string

	// admitted distinguishes a minted token from the zero value. It is a
	// separate field rather than a display != "" inference so the distinction
	// survives any future change to what an empty display means.
	admitted bool
}

// IsZero reports whether this is the zero Admitted — the value a caller gets
// from a rejected Admit, and the only one another package can construct.
func (a Admitted) IsZero() bool { return !a.admitted }

// Display is the name as it will be stored and shown: the §6.1.1 display form,
// with the capitalization the player chose preserved.
func (a Admitted) Display() string { return a.display }

// Key is the §6.1.1 uniqueness key — Display put through Unicode full case
// folding. It is what characters.normalized_name stores.
func (a Admitted) Key() string { return a.key }

// Skeleton is the UTS #39 skeleton of Key — what characters.name_skeleton
// stores, and what the confusability decision was made against.
func (a Admitted) Skeleton() string { return a.skeleton }

// UnicodeVersion is the Unicode version of the confusables data that produced
// Skeleton — what characters.name_skeleton_unicode_version stores, so a later
// version upgrade can find every row whose skeleton needs recomputing.
func (a Admitted) UnicodeVersion() string { return a.unicodeVersion }

// Admit runs the full admission decision and, on acceptance, mints the token.
//
// It is the SOLE constructor of Admitted. On rejection it returns the zero
// value and Check's error verbatim — unwrapped and un-re-coded — so
// NAME_INVALID_SYNTAX, NAME_EMPTY_NORMAL_FORM, the mixed-script code,
// NAME_BLOCKED, NAME_SKELETON_UNVERIFIABLE and NAME_CONFUSABLE all survive
// Admit unchanged and every existing caller-side code mapping keeps working.
//
// # Admit is additive; Check is unchanged
//
// Check remains the verdict function: it answers "may this name be admitted?"
// and is what the gate's own tests exercise. Admit is the admission TOKEN
// minted from a successful verdict. The split matters because Check has callers
// and behaviour that predate this type, and changing its signature to return a
// token would have rippled through them for no gain.
//
// # Why the variadic tail
//
// The options are forwarded to Check verbatim so a rename call site can pass
// ExcludingCharacter(id). Without it, 01-SPEC.md §702-706's settled case-variant
// rename could not mint a token AT ALL — and since Admit is the type's only
// constructor, there would be no way around that (B-18).
func (g *Gate) Admit(ctx context.Context, submitted string, opts ...CheckOption) (Admitted, error) {
	normalized, skeleton, err := g.Check(ctx, submitted, opts...)
	if err != nil {
		return Admitted{}, err
	}

	return Admitted{
		display:        normalized.Display,
		key:            normalized.Key,
		skeleton:       skeleton,
		unicodeVersion: UnicodeVersion,
		admitted:       true,
	}, nil
}

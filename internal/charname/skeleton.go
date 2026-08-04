// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Skeleton computes the UTS #39 confusable skeleton of s: NFD, then substitute
// each rune's confusable prototype from the generated table, then NFD again.
// Two strings are confusable exactly when their skeletons are equal.
//
// # Scope and the honest gap
//
// The current UTS #39 revision defines skeleton(X) = bidiSkeleton(LTR, X).
// This function implements bidiSkeleton(LTR, ·) reduced for same-direction
// comparison — the classic three-step core that every surveyed Go
// implementation ships. NO surveyed Go package implements the RTL form.
//
// That reduction is adequate here because the comparison in this system is
// ALWAYS same-direction: a candidate's skeleton is compared against stored
// skeletons computed by this same function. The bidi-aware form would only be
// needed to catch a reordering-based confusable, which this threat model does
// not cover. Recorded as a known limitation rather than claimed as full §4
// conformance.
//
// # Why the trailing NFD is not optional
//
// A confusable prototype may itself be a multi-codepoint sequence in composed
// form. Without the second NFD pass, two inputs that differ only in
// composition would produce unequal skeletons and a genuine confusable pair
// would slip through. It is required by UTS #39 §4 and is present below.
func Skeleton(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFD.String(s) {
		if prototype, ok := confusables[r]; ok {
			b.WriteString(prototype)
			continue
		}
		b.WriteRune(r)
	}
	return norm.NFD.String(b.String())
}

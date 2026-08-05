// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package chartest supplies the character identity columns that direct-SQL test
// fixtures must write.
//
// Migration 000056 makes characters.normalized_name NOT NULL and UNIQUE, so a
// fixture that inserts `(id, player_id, name, location_id)` and nothing else now
// violates the constraint — and it fails at SETUP time in whatever unrelated
// suite happens to own the fixture, a long way from the cause. This package
// exists so every such fixture computes the identity triple ONE way, through the
// same §6.1.1 pipeline the gated writer uses, and so a fixture written after
// this phase is correct by default rather than broken by default.
//
// It is deliberately NOT a test escape hatch for charname.Admitted. There is no
// way to mint an admission token here; this only computes the DERIVED column
// values for fixtures that write raw SQL. Anything exercising the production
// create path still runs a real charname.Gate.
//
// internal/testsupport/ and test/ are outside the world-SQL fence's Go scan, so
// those fixtures keep their raw INSERTs legitimately.
package chartest

import "github.com/holomush/holomush/internal/charname"

// Identity is the derived triple that characters rows carry alongside the
// display name: the §6.1.1 uniqueness key, the UTS #39 confusable skeleton, and
// the Unicode version the skeleton was computed under.
type Identity struct {
	// NormalizedName is the case-folded uniqueness key. It is the column
	// migration 000056 constrains NOT NULL and UNIQUE.
	NormalizedName string
	// Skeleton is the UTS #39 confusable skeleton of NormalizedName.
	Skeleton string
	// UnicodeVersion is the version of the confusables data Skeleton was
	// computed from, recorded per row (D-23) so a Unicode upgrade is visible.
	UnicodeVersion string
}

// IdentityFor computes the identity triple for a display name.
//
// It falls back to the submitted name as its own key when the name does not
// survive normalization (an empty normal form, which the production gate
// refuses outright). A fixture using such a name is already testing something
// other than a normal character, and returning zero values would make it
// violate the NOT NULL constraint for a second, unrelated reason.
func IdentityFor(name string) Identity {
	key := name
	if normalized, err := charname.Normalize(name); err == nil {
		key = normalized.Key
	}
	return Identity{
		NormalizedName: key,
		Skeleton:       charname.Skeleton(key),
		UnicodeVersion: charname.UnicodeVersion,
	}
}

// Columns returns the three derived values in the order
// (normalized_name, name_skeleton, name_skeleton_unicode_version), ready to
// splat into an INSERT's argument list:
//
//	args := append([]any{id, playerID, name}, chartest.Columns(name)...)
func Columns(name string) []any {
	id := IdentityFor(name)
	return []any{id.NormalizedName, id.Skeleton, id.UnicodeVersion}
}

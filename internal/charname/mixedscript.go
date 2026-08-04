// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/samber/oops"
)

// This file implements 01-SPEC.md §6.1.2 Mechanism A — UTS #39's Moderately
// Restrictive profile — as a closed enumeration of verdicts over a name's
// script set.
//
// # It is computed over Script, not Script_Extensions, and that is an approximation
//
// UTS #39 defines Moderately Restrictive over AUGMENTED script sets, which are
// derived from the Script_Extensions (scx) property. No Go package exposes scx:
// the standard library's unicode.Scripts table is generated from Scripts.txt —
// the Script (sc) property — and golang.org/x/text has no script package at
// all. So the verdict below is computed from sc.
//
// The direction of the approximation is what makes it acceptable. Under UAX #24
// §3.3, a codepoint not listed in ScriptExtensions.txt takes its Script value as
// its Script_Extensions value; the two therefore differ only for codepoints
// whose sc is Common or Inherited but whose scx names specific scripts —
// U+30FC KATAKANA-HIRAGANA PROLONGED SOUND MARK (scx = {Hira, Kana}) is the
// canonical example. Those codepoints are script-neutral here and script-bearing
// under scx, so this implementation can only ever see FEWER scripts in the set
// than the faithful profile would. It can MISS a rejection; it can never invent
// one. All three of §6.1.2's explicitly named rejections — Latin+Cyrillic,
// Latin+Greek, Cyrillic+Greek — are unaffected, because Cyrillic and Greek
// letters carry real sc values. Mechanism B (the skeleton check) is the defense
// against whole-script confusables either way.
//
// The divergence is recorded here rather than only in a planning document
// because this file is where a future reader will look for it, and it is queued
// for the 01-SPEC.md §14 amendment pass.
//
// # The two Unicode versions in play are different, which is why an unclassifiable rune is refused
//
// unicode.Scripts is generated at the standard library's Unicode version
// (unicode.Version), while this package's confusables table is generated at the
// version pinned by cmd/internal/gen-confusables. Those versions are not the
// same and are not expected to become the same. A codepoint assigned in the
// newer release therefore belongs to NO range in unicode.Scripts, and treating
// it as script-neutral would let a genuinely two-script name read as
// single-script — the exact splicing Mechanism A exists to catch. It is refused
// instead. Refusing is also the choice consistent with what already happens: the
// syntactic rules admit only Unicode letters, and Go's regexp draws \p{L} from
// the same tables, so such a rune is already rejected one layer up. This branch
// is the belt to that layer's braces, and it is stated rather than left to be
// inferred.

// The three permitted multi-script families of §6.1.2's table, each expressed
// as the full set a name's scripts must be contained within. Membership is
// tested as subset-of-family rather than as a script count, because a count
// re-derives the table in a second place and the two copies then drift.
//
// Containment, not equality: {Han, Hiragana} is a perfectly ordinary Japanese
// name and {Han, Hangul} a perfectly ordinary Korean one. Requiring Latin to be
// present would reject both, which the plan's prohibition forbids — no rule
// here may reject a non-Latin name that its Latin-script equivalent would pass.
var permittedScriptFamilies = []map[string]struct{}{
	// Japanese.
	{"Latin": {}, "Han": {}, "Hiragana": {}, "Katakana": {}},
	// Chinese.
	{"Latin": {}, "Han": {}, "Bopomofo": {}},
	// Korean.
	{"Latin": {}, "Han": {}, "Hangul": {}},
}

// ScriptSet resolves the set of Unicode scripts a name is written in, as a
// sorted, deduplicated list of unicode.Scripts key names.
//
// Codepoints of script Common and Inherited — spaces, punctuation, digits,
// combining marks — are script-neutral and are excluded from the set, so they
// can never make a single-script name look mixed.
//
// A rune belonging to no unicode.Scripts range cannot be classified at the
// standard library's Unicode version. That is refused with
// NAME_UNASSIGNED_SCRIPT rather than skipped; see this file's header for why
// fail-closed is the right direction there.
//
// The result is sorted so that a verdict — and the message a rejected submitter
// reads — cannot depend on the order the scripts happened to appear in.
func ScriptSet(s string) ([]string, error) {
	seen := make(map[string]struct{}, 4)

	for _, r := range s {
		if unicode.Is(unicode.Common, r) || unicode.Is(unicode.Inherited, r) {
			continue
		}

		name, ok := scriptOf(r)
		if !ok {
			return nil, oops.
				Code("NAME_UNASSIGNED_SCRIPT").
				With("codepoint", fmt.Sprintf("U+%04X", r)).
				With("unicode_version", unicode.Version).
				Errorf("character name contains a character this server cannot classify")
		}

		seen[name] = struct{}{}
	}

	set := make([]string, 0, len(seen))
	for name := range seen {
		set = append(set, name)
	}
	sort.Strings(set)

	return set, nil
}

// MixedScript applies §6.1.2 Mechanism A's verdict table to a name.
//
// A name whose script set is empty or holds a single script is permitted,
// whichever script that is — a name written wholly in Cyrillic, Greek, Arabic
// or Han is a legitimate name, and Mechanism B is what covers the whole-script
// confusable it might be. A name whose scripts are contained in one of the
// three CJK families is permitted. Every other set of two or more scripts is
// refused with NAME_MIXED_SCRIPT.
//
// The refusal names the script combination, which is derived entirely from the
// submitter's own input and so discloses nothing about any existing character.
func MixedScript(s string) error {
	set, err := ScriptSet(s)
	if err != nil {
		return err
	}

	if len(set) <= 1 {
		return nil
	}

	for _, family := range permittedScriptFamilies {
		if containedIn(set, family) {
			return nil
		}
	}

	return oops.
		Code("NAME_MIXED_SCRIPT").
		With("scripts", strings.Join(set, "+")).
		Errorf("character names cannot mix the %s scripts; use one writing system", strings.Join(set, " and "))
}

// scriptOf returns the unicode.Scripts key naming the script r belongs to.
//
// The Script property partitions the assigned codepoints, so at most one table
// can match and returning on the first hit is exact rather than a first-wins
// tiebreak. ok is false when r belongs to no table at all.
func scriptOf(r rune) (string, bool) {
	for name, table := range unicode.Scripts {
		if unicode.Is(table, r) {
			return name, true
		}
	}
	return "", false
}

// containedIn reports whether every script in set is a member of family.
func containedIn(set []string, family map[string]struct{}) bool {
	for _, name := range set {
		if _, ok := family[name]; !ok {
			return false
		}
	}
	return true
}

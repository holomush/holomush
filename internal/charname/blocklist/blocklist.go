// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package blocklist compiles an operator-configured list of regular
// expressions into an immutable snapshot that the character-name admission
// gate evaluates against the normalized (case-folded) name.
//
// # Three properties this list deliberately does NOT have
//
// A later reader will assume these unless told otherwise, so they are stated
// here rather than left to be discovered:
//
//   - It is NOT versioned. There is no history of what the list said at the
//     moment a given name was admitted.
//   - It is NOT audited. In v0.13 the only edit path is direct SQL against
//     holomush_system_info (01-SPEC §8.12 ships no editing surface), which
//     leaves no trail in this system.
//   - It is NOT retroactive. A name admitted under an older list is never
//     re-validated. The list governs ADMISSIONS, not the existing corpus.
//
// # No ReDoS machinery belongs here
//
// Go's regexp is RE2: linear time in the input, with no backtracking. An
// operator-supplied pattern therefore carries no catastrophic-backtracking
// risk, and timeouts, complexity analysis or any other backtracking defense
// would be machinery guarding a threat that does not exist. The residual risk
// is a WRONG (over-broad) pattern rejecting legitimate names, which no amount
// of execution-time defense addresses — boot validation surfaces a malformed
// pattern to the operator who wrote it, and that is the whole of the defense.
package blocklist

import (
	"regexp"

	"github.com/samber/oops"
)

// Snapshot is an immutable, compiled view of a block list.
//
// Like policy.Snapshot, it is safe for concurrent reads without locking: no
// method mutates it, and the compiled regexps it holds are themselves
// documented safe for concurrent use. A reload therefore replaces the whole
// value rather than editing one in place, which is what lets a reader hold one
// snapshot for the whole of an evaluation and never observe a half-applied
// list.
type Snapshot struct {
	patterns []*regexp.Regexp
}

// Compile compiles patterns, in list order, into an immutable Snapshot.
//
// A nil or empty list is VALID and produces an empty snapshot that rejects
// nothing. The absence of configuration is not "block everything" and is not a
// startup failure.
//
// The first pattern that fails to compile aborts the whole compilation with
// BLOCKLIST_PATTERN_INVALID, carrying the offending pattern text and its list
// index — so the operator who wrote it can find it (D-15). Compilation is
// all-or-nothing: a partially compiled list is never returned.
func Compile(patterns []string) (*Snapshot, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for i, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, oops.
				Code("BLOCKLIST_PATTERN_INVALID").
				With("pattern", p).
				With("index", i).
				Wrap(err)
		}
		compiled = append(compiled, re)
	}
	return &Snapshot{patterns: compiled}, nil
}

// Match reports whether normalized matches any pattern, and the index of the
// first matching entry (-1 when nothing matched).
//
// Evaluation is in stored list order and the FIRST match decides: two patterns
// matching the same name are neither merged nor deduplicated, and the name is
// rejected once.
//
// It deliberately returns the INDEX rather than the pattern text, so a
// rejection path cannot echo operator configuration back to a submitter even
// by accident. The index is for operator-side diagnostics, never for the
// caller-visible message.
//
// Match performs no normalization of its own. It evaluates the caller-supplied
// string exactly as given; supplying the case-folded key is the GATE's
// responsibility (charname.Gate.Check).
//
// A nil receiver matches nothing, so an unconfigured cache is indistinguishable
// from an empty list.
func (s *Snapshot) Match(normalized string) (blocked bool, index int) {
	if s == nil {
		return false, -1
	}
	for i, re := range s.patterns {
		if re.MatchString(normalized) {
			return true, i
		}
	}
	return false, -1
}

// Len returns the number of compiled patterns. It exists for operator-facing
// logging and for tests that need to distinguish an empty snapshot from a
// populated one without reaching into unexported state.
func (s *Snapshot) Len() int {
	if s == nil {
		return 0
	}
	return len(s.patterns)
}

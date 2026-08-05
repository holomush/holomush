// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import "github.com/samber/oops"

// Status is the character lifecycle vocabulary stored in characters.status
// (migration 000054). It is a CLOSED set of exactly three values, compared as
// exact lowercase string literals: the column's CHECK constraint admits nothing
// else, so a reader that case-folds or trims is accepting spellings the writer
// can never produce and is therefore comparing wrong.
type Status string

// The three — and only three — lifecycle values. Their string forms are the
// stored column values verbatim.
const (
	// StatusActive is a playable character. It is the DB column default and the
	// state every newly created character starts in.
	StatusActive Status = "active"
	// StatusRetired is a character its player has withdrawn from play. The row
	// and its name reservation both survive retirement (INV-WORLD-6).
	StatusRetired Status = "retired"
	// StatusIdle is shipped in v0.13 with NO transition into it. It exists so
	// the vocabulary is closed at the schema layer before the transition lands,
	// and it is covered by the exhaustive-read rule below rather than by being
	// unreachable — an unreachable value guarded by a non-exhaustive check reads
	// correct only until someone makes it reachable.
	StatusIdle Status = "idle"
)

// NeverActive is the characters.last_active_at sentinel for "has never been
// active": the column is BIGINT epoch-NANOSECONDS, NOT NULL, defaulting to 0
// (migration 000054), so there is no null lifecycle state to reason about.
//
// Two consequences callers must carry. First, 0 is the minimum, so a
// most-recent-first sort places never-active rows last with no NULLS LAST
// clause. Second, any "last active" rendering MUST special-case this value
// rather than formatting it — an unguarded render displays 1970 (D-25).
const NeverActive int64 = 0

// ParseStatus converts a stored column value into a Status, rejecting anything
// outside the closed vocabulary with INVALID_CHARACTER_STATUS.
//
// The comparison is deliberately exact: no case folding, no trimming, no
// alternative spelling. "Retired", "RETIRED" and " retired" are values the
// column's CHECK constraint rejects at write time, so a reader that accepts
// them is accepting a row shape the database cannot hold.
func ParseStatus(s string) (Status, error) {
	switch Status(s) {
	case StatusActive:
		return StatusActive, nil
	case StatusRetired:
		return StatusRetired, nil
	case StatusIdle:
		return StatusIdle, nil
	default:
		return "", oops.Code("INVALID_CHARACTER_STATUS").
			With("status", s).
			Errorf("character status %q is not one of active, retired, idle", s)
	}
}

// Selectable reports whether a character in the given lifecycle state may be
// selected for play. It is the ONE predicate every selectability decision routes
// through (INV-WORLD-5).
//
// The body is an exhaustive switch whose default arm DENIES, and that shape is
// load-bearing rather than stylistic. Written as `s == StatusActive` it would be
// behaviourally identical today and would still deny an unknown value — but the
// enumeration would be invisible, so a fourth value added to the vocabulary
// would silently acquire whichever answer the shorthand happens to give,
// wherever a second predicate had drifted. Written as `s != StatusRetired` it
// would fail OPEN the moment `idle` became reachable. The switch names all three
// values at the decision point and hands anything else to a denying default, so
// a value added later is excluded by the same code path that excludes retired,
// with no second predicate to keep in sync.
//
// The empty Status is likewise denied here. It is not a lifecycle state — it is
// the zero value of a partial projection that did not read the column — and a
// caller that can observe it MUST treat it as a programming error at the call
// site rather than softening this default.
func Selectable(s Status) bool {
	switch s {
	case StatusActive:
		return true
	case StatusRetired, StatusIdle:
		return false
	default:
		return false
	}
}

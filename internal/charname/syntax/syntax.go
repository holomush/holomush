// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package syntax holds the syntactic character-name rules — the ones that
// decide whether a name is spelled acceptably at all, independent of Unicode
// normalization, confusability or any policy list.
//
// # Why this package exists (D-28)
//
// Two demands pull in opposite directions:
//
//   - charname.Gate.Check must run the syntactic rules itself, so a single
//     gate verdict proves the WHOLE character-name validity contract and no
//     writer has to remember a second call.
//   - world.CharacterRepository.Create must take a charname.Admitted token, so
//     the write path is unrepresentable without a gate verdict.
//
// Naively those are charname -> world and world -> charname: an import cycle
// Go rejects. Copying the rules into charname is the other obvious escape and
// is worse — two implementations of one rule drift silently, because each side
// has its own passing table.
//
// So the rules live HERE, in a dependency-free leaf. This package MUST import
// neither internal/world nor internal/charname; its imports are stdlib only.
// charname.Gate calls ValidateName directly and world.ValidateCharacterName is
// a thin wrapper over it, so there is exactly one implementation of the rules
// and exactly one surviving edge (world -> charname).
package syntax

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Character-name rune-count bounds. These are RUNE counts, not byte lengths,
// so a Cyrillic or accented name is measured the way a player perceives it.
const (
	MinNameLength = 2
	MaxNameLength = 32
)

// nameRegex matches names made only of Unicode letters, with single spaces
// between words. It anchors both ends, so a control character, digit or
// punctuation mark anywhere in the string fails the match.
var nameRegex = regexp.MustCompile(`^[\p{L}]+( [\p{L}]+)*$`)

// ValidationError reports a syntactic character-name rule violation. It carries
// the same Field/Message pair world.ValidationError carries, so
// world.ValidateCharacterName can convert one into the other without losing
// information its existing callers and tests assert against.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateName checks that a character name satisfies the syntactic rules:
//
//   - non-empty and valid UTF-8
//   - Unicode letters and spaces only (no digits, no punctuation, no controls)
//   - MinNameLength..MaxNameLength runes
//   - no leading or trailing spaces
//   - no consecutive spaces
//
// It says nothing about normalization, confusability or block lists — those are
// charname's, and charname.Gate.Check composes all of them into one verdict.
func ValidateName(name string) error {
	if name == "" {
		return &ValidationError{Field: "name", Message: "cannot be empty"}
	}

	// Validate UTF-8 before any other processing: rune counting and the regex
	// both behave surprisingly on invalid bytes.
	if !utf8.ValidString(name) {
		return &ValidationError{Field: "name", Message: "must be valid UTF-8"}
	}

	if name != strings.TrimSpace(name) {
		return &ValidationError{Field: "name", Message: "cannot have leading or trailing spaces"}
	}

	if strings.Contains(name, "  ") {
		return &ValidationError{Field: "name", Message: "cannot have consecutive spaces"}
	}

	runeCount := utf8.RuneCountInString(name)
	if runeCount < MinNameLength {
		return &ValidationError{Field: "name", Message: fmt.Sprintf("must be at least %d characters", MinNameLength)}
	}
	if runeCount > MaxNameLength {
		return &ValidationError{Field: "name", Message: fmt.Sprintf("must be at most %d characters", MaxNameLength)}
	}

	if !nameRegex.MatchString(name) {
		return &ValidationError{Field: "name", Message: "must contain letters and spaces only"}
	}

	return nil
}

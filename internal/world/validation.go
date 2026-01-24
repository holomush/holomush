// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"encoding/json"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

// Validation limits for domain types.
const (
	MaxNameLength        = 100
	MaxDescriptionLength = 4000
	MaxAliasCount        = 10
	MaxAliasLength       = 50
	MaxVisibleToCount    = 100
	MaxLockDataKeys      = 20
)

// ValidationError represents an input validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateName checks that a name is valid.
// Names must be non-empty, valid UTF-8, no control characters, and within length limit.
func ValidateName(name string) error {
	if name == "" {
		return &ValidationError{Field: "name", Message: "cannot be empty"}
	}
	if !utf8.ValidString(name) {
		return &ValidationError{Field: "name", Message: "must be valid UTF-8"}
	}
	if len(name) > MaxNameLength {
		return &ValidationError{Field: "name", Message: fmt.Sprintf("exceeds maximum length of %d", MaxNameLength)}
	}
	if hasControlChars(name) {
		return &ValidationError{Field: "name", Message: "cannot contain control characters"}
	}
	return nil
}

// ValidateDescription checks that a description is valid.
// Descriptions may be empty, must be valid UTF-8, no control characters (except newline/tab), and within length limit.
func ValidateDescription(desc string) error {
	if desc == "" {
		return nil // Description may be empty
	}
	if !utf8.ValidString(desc) {
		return &ValidationError{Field: "description", Message: "must be valid UTF-8"}
	}
	if len(desc) > MaxDescriptionLength {
		return &ValidationError{Field: "description", Message: fmt.Sprintf("exceeds maximum length of %d", MaxDescriptionLength)}
	}
	if hasControlCharsExceptWhitespace(desc) {
		return &ValidationError{Field: "description", Message: "cannot contain control characters (except newline/tab)"}
	}
	return nil
}

// ValidateAliases checks that aliases are valid.
// Each alias must be non-empty, valid UTF-8, no control characters, and within length limit.
// Total number of aliases must be within limit.
func ValidateAliases(aliases []string) error {
	if len(aliases) > MaxAliasCount {
		return &ValidationError{Field: "aliases", Message: fmt.Sprintf("exceeds maximum count of %d", MaxAliasCount)}
	}
	for i, alias := range aliases {
		if alias == "" {
			return &ValidationError{Field: "aliases", Message: fmt.Sprintf("alias %d cannot be empty", i)}
		}
		if !utf8.ValidString(alias) {
			return &ValidationError{Field: "aliases", Message: fmt.Sprintf("alias %d must be valid UTF-8", i)}
		}
		if len(alias) > MaxAliasLength {
			return &ValidationError{Field: "aliases", Message: fmt.Sprintf("alias %d exceeds maximum length of %d", i, MaxAliasLength)}
		}
		if hasControlChars(alias) {
			return &ValidationError{Field: "aliases", Message: fmt.Sprintf("alias %d cannot contain control characters", i)}
		}
	}
	return nil
}

// ValidateVisibleTo checks that a visible-to list is valid.
// Must have no duplicates and be within size limit.
func ValidateVisibleTo(visibleTo []ulid.ULID) error {
	if len(visibleTo) > MaxVisibleToCount {
		return &ValidationError{Field: "visible_to", Message: fmt.Sprintf("exceeds maximum count of %d", MaxVisibleToCount)}
	}
	seen := make(map[ulid.ULID]bool, len(visibleTo))
	for _, id := range visibleTo {
		if seen[id] {
			return &ValidationError{Field: "visible_to", Message: fmt.Sprintf("duplicate ID: %s", id)}
		}
		seen[id] = true
	}
	return nil
}

// ValidateLockData checks that lock data is valid.
// Must have reasonable number of keys, keys must be valid identifiers,
// and values must be JSON-serializable (for deep copy in ReverseExit).
func ValidateLockData(lockData map[string]any) error {
	if lockData == nil {
		return nil
	}
	if len(lockData) > MaxLockDataKeys {
		return &ValidationError{Field: "lock_data", Message: fmt.Sprintf("exceeds maximum key count of %d", MaxLockDataKeys)}
	}
	for key := range lockData {
		if key == "" {
			return &ValidationError{Field: "lock_data", Message: "key cannot be empty"}
		}
		if !isValidIdentifier(key) {
			return &ValidationError{Field: "lock_data", Message: fmt.Sprintf("key %q is not a valid identifier", key)}
		}
	}
	// Verify JSON-serializable (required for deep copy in ReverseExit)
	if _, err := json.Marshal(lockData); err != nil {
		return &ValidationError{Field: "lock_data", Message: "not JSON-serializable: " + err.Error()}
	}
	return nil
}

// hasControlChars returns true if the string contains control characters.
func hasControlChars(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// hasControlCharsExceptWhitespace returns true if the string contains control characters
// other than newline, carriage return, and tab.
func hasControlCharsExceptWhitespace(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return true
		}
	}
	return false
}

// isValidIdentifier returns true if s is a valid identifier (alphanumeric + underscore, starting with letter).
func isValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return false
			}
		}
	}
	return true
}

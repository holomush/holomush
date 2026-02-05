// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"regexp"
	"strings"

	"github.com/samber/oops"
)

const (
	// MaxNameLength is the maximum length for command and alias names.
	MaxNameLength = 20
)

// namePattern validates command/alias names: must start with letter,
// followed by letters, digits, or special chars: _!?@#$%^+-
// Pattern: ^[a-zA-Z][a-zA-Z0-9_!?@#$%^+\-]{0,19}$
// Note: The hyphen is escaped (\-) in the regex for clarity, though it's
// optional at the end of a character class.
var namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_!?@#$%^+\-]{0,19}$`)

// ValidateCommandName validates a command name.
func ValidateCommandName(name string) error {
	return validateName(name, "command")
}

// ValidateAliasName validates an alias name.
func ValidateAliasName(name string) error {
	return validateName(name, "alias")
}

func validateName(name, kind string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return oops.Code(CodeInvalidName).
			With("kind", kind).
			Errorf("%s name cannot be empty", kind)
	}

	if len(trimmed) > MaxNameLength {
		return oops.Code(CodeInvalidName).
			With("kind", kind).
			With("length", len(trimmed)).
			With("max", MaxNameLength).
			Errorf("%s name exceeds maximum length of %d", kind, MaxNameLength)
	}

	if !namePattern.MatchString(trimmed) {
		return oops.Code(CodeInvalidName).
			With("kind", kind).
			With("name", trimmed).
			Errorf("%s name must start with a letter and contain only letters, digits, or _!?@#$%%^+-", kind)
	}

	return nil
}

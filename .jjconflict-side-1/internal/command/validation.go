// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"fmt"
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
		msg := kind + " name cannot be empty"
		return oops.Code(CodeInvalidName).
			With("kind", kind).
			With("message", msg).
			Errorf("%s", msg)
	}

	if len(trimmed) > MaxNameLength {
		msg := fmt.Sprintf("%s name exceeds maximum length of %d", kind, MaxNameLength)
		return oops.Code(CodeInvalidName).
			With("kind", kind).
			With("length", len(trimmed)).
			With("max", MaxNameLength).
			With("message", msg).
			Errorf("%s", msg)
	}

	if !namePattern.MatchString(trimmed) {
		msg := kind + " name must start with a letter and contain only letters, digits, or _!?@#$%^+-"
		return oops.Code(CodeInvalidName).
			With("kind", kind).
			With("name", trimmed).
			With("message", msg).
			Errorf("%s", msg)
	}

	return nil
}

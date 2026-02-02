// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"strings"

	"github.com/samber/oops"
)

// ParsedCommand represents a parsed command input.
type ParsedCommand struct {
	Name string // command name (first whitespace-delimited token)
	Args string // unparsed argument string (preserves internal whitespace)
	Raw  string // original input
}

// Parse splits raw input into command name and arguments.
// The command name is the first whitespace-delimited token.
// Arguments preserve internal whitespace.
func Parse(input string) (*ParsedCommand, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, oops.Code("EMPTY_INPUT").Errorf("no command provided")
	}

	// Find first whitespace (space or tab)
	idx := strings.IndexAny(trimmed, " \t")
	if idx == -1 {
		// No whitespace, entire input is command name
		return &ParsedCommand{
			Name: trimmed,
			Args: "",
			Raw:  input,
		}, nil
	}

	name := trimmed[:idx]
	// Trim leading whitespace from args but preserve internal whitespace
	args := strings.TrimLeft(trimmed[idx+1:], " \t")

	return &ParsedCommand{
		Name: name,
		Args: args,
		Raw:  input,
	}, nil
}

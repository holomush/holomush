// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corealiases

import (
	"fmt"
	"regexp"
	"strings"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// aliasNameRe matches valid alias names: alphanumeric plus a few MUSH-convention characters.
var aliasNameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-+]+$`)

// parseAliasDefinition parses "alias=command" format.
func parseAliasDefinition(args string) (alias, command string, err error) {
	args = strings.TrimSpace(args)

	idx := strings.Index(args, "=")
	if idx == -1 {
		return "", "", fmt.Errorf("usage: <alias>=<command>")
	}

	alias = strings.TrimSpace(args[:idx])
	command = strings.TrimSpace(args[idx+1:])

	if alias == "" {
		return "", "", fmt.Errorf("alias name cannot be empty")
	}
	if command == "" {
		return "", "", fmt.Errorf("command cannot be empty")
	}

	return alias, command, nil
}

// validateAliasName checks that an alias name is well-formed.
func validateAliasName(name string) error {
	if !aliasNameRe.MatchString(name) {
		return fmt.Errorf("invalid alias name %q: must contain only letters, digits, hyphens, underscores, or plus signs", name)
	}
	return nil
}

// findAlias searches a slice of AliasEntry for a matching alias name (case-sensitive).
func findAlias(entries []plugins.AliasEntry, alias string) (string, bool) {
	for _, e := range entries {
		if e.Alias == alias {
			return e.Command, true
		}
	}
	return "", false
}

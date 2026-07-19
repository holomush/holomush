// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package cmdparse is the dependency-free command-grammar leaf: splitting
// raw telnet input into a verb and argument string.
package cmdparse

import "strings"

// ParseCommand splits input into command and arguments.
func ParseCommand(input string) (cmd, arg string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}

	parts := strings.SplitN(input, " ", 2)
	cmd = strings.ToLower(parts[0])
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}
	return cmd, arg
}

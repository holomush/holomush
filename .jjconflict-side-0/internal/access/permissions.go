// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

// Permission groups define reusable sets of permissions.
// Roles compose these groups rather than inheriting.

var playerPowers = []string{
	// Self access
	"read:character:$self",
	"write:character:$self",

	// Current location access
	"read:location:$here",
	"read:character:$here:*",
	"read:object:$here:*",
	"emit:stream:location:$here",

	// Basic commands
	"execute:command:say",
	"execute:command:pose",
	"execute:command:look",
	"execute:command:go",
}

var builderPowers = []string{
	// World modification
	"write:location:*",
	"write:object:*",
	"delete:object:*",

	// Builder commands
	"execute:command:dig",
	"execute:command:create",
	"execute:command:describe",
	"execute:command:link",
}

var adminPowers = []string{
	// Full access
	"read:**",
	"write:**",
	"delete:**",
	"emit:**",
	"execute:**",
	"grant:**",
}

// DefaultRoles returns the default role definitions.
// Roles compose permission groups explicitly (no inheritance).
func DefaultRoles() map[string][]string {
	return map[string][]string{
		"player":  playerPowers,
		"builder": compose(playerPowers, builderPowers),
		"admin":   compose(playerPowers, builderPowers, adminPowers),
	}
}

// compose merges multiple permission slices into one.
func compose(groups ...[]string) []string {
	total := 0
	for _, g := range groups {
		total += len(g)
	}
	result := make([]string, 0, total)
	for _, g := range groups {
		result = append(result, g...)
	}
	return result
}

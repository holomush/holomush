// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

const (
	// RolePlayer is the default role for all characters.
	RolePlayer = "player"
	// RoleBuilder grants world-building permissions.
	RoleBuilder = "builder"
	// RoleAdmin grants full access to everything.
	RoleAdmin = "admin"
)

// SystemRoles returns all system roles in privilege order (lowest first).
func SystemRoles() []string {
	return []string{RolePlayer, RoleBuilder, RoleAdmin}
}

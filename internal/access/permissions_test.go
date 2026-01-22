// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
	"testing"

	"github.com/holomush/holomush/internal/access"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRoles(t *testing.T) {
	roles := access.DefaultRoles()

	require.Contains(t, roles, "player")
	require.Contains(t, roles, "builder")
	require.Contains(t, roles, "admin")

	// Player has basic permissions
	assert.Contains(t, roles["player"], "read:character:$self")
	assert.Contains(t, roles["player"], "emit:stream:location:$here")

	// Builder has world modification
	assert.Contains(t, roles["builder"], "write:location:*")
	assert.Contains(t, roles["builder"], "execute:command:@dig")

	// Admin has full access
	assert.Contains(t, roles["admin"], "read:**")
	assert.Contains(t, roles["admin"], "grant:**")
}

func TestRoleComposition(t *testing.T) {
	roles := access.DefaultRoles()

	// Builder includes player permissions
	for _, perm := range []string{"read:character:$self", "emit:stream:location:$here"} {
		assert.Contains(t, roles["builder"], perm, "builder should include player permission: %s", perm)
	}

	// Admin includes all permissions
	for _, perm := range roles["builder"] {
		assert.Contains(t, roles["admin"], perm, "admin should include builder permission: %s", perm)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParticipantRole_String(t *testing.T) {
	tests := []struct {
		name     string
		role     ParticipantRole
		expected string
	}{
		{"owner", RoleOwner, "owner"},
		{"member", RoleMember, "member"},
		{"invited", RoleInvited, "invited"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.role.String())
		})
	}
}

func TestParticipantRole_Validate(t *testing.T) {
	t.Run("valid roles", func(t *testing.T) {
		for _, role := range ValidParticipantRoles() {
			assert.NoError(t, role.Validate(), "role %q should be valid", role)
		}
	})

	t.Run("invalid role", func(t *testing.T) {
		err := ParticipantRole("admin").Validate()
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidParticipantRole)
	})

	t.Run("empty role", func(t *testing.T) {
		err := ParticipantRole("").Validate()
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidParticipantRole)
	})
}

func TestValidParticipantRoles(t *testing.T) {
	roles := ValidParticipantRoles()
	assert.Len(t, roles, 3)
	assert.Contains(t, roles, RoleOwner)
	assert.Contains(t, roles, RoleMember)
	assert.Contains(t, roles, RoleInvited)
}

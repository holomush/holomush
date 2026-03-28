// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoleConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"player role", RolePlayer, "player"},
		{"builder role", RoleBuilder, "builder"},
		{"admin role", RoleAdmin, "admin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.got)
		})
	}
}

func TestSystemRoles(t *testing.T) {
	roles := SystemRoles()
	assert.Equal(t, []string{"player", "builder", "admin"}, roles)
}

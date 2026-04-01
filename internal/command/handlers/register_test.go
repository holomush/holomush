// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
)

func TestRegisterAll(t *testing.T) {
	reg := command.NewRegistry()
	RegisterAll(reg)

	all := reg.All()
	names := make([]string, len(all))
	for i, e := range all {
		names[i] = e.Name
	}

	assert.Contains(t, names, "quit")
	assert.Contains(t, names, "shutdown")
}

func TestRegisterAdmin(t *testing.T) {
	reg := command.NewRegistry()
	s := newResetTestSetup(t)
	RegisterAdmin(reg, s.deps())

	entry, found := reg.Get("resetpassword")
	require.True(t, found, "resetpassword should be registered")
	assert.Equal(t, "resetpassword", entry.Name)
	assert.Equal(t, []string{"admin:password.reset"}, entry.GetCapabilities())
	assert.Equal(t, "core", entry.Source)
	assert.NotNil(t, entry.Handler())
}

func TestRegisterAdmin_PanicsOnNilDeps(t *testing.T) {
	reg := command.NewRegistry()
	assert.PanicsWithValue(t, "missing admin dependency: PlayerRepo", func() {
		RegisterAdmin(reg, AdminDeps{})
	})
}

func TestRegisterAdmin_OverwritesOnDuplicate(t *testing.T) {
	reg := command.NewRegistry()
	s := newResetTestSetup(t)
	deps := s.deps()
	RegisterAdmin(reg, deps)

	assert.NotPanics(t, func() {
		RegisterAdmin(reg, deps)
	})

	entry, found := reg.Get("resetpassword")
	require.True(t, found)
	assert.Equal(t, "resetpassword", entry.Name)
}

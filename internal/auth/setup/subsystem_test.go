// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/auth/setup"
	"github.com/holomush/holomush/internal/lifecycle"
)

// Compile-time interface check: *setup.AuthSubsystem must satisfy lifecycle.Subsystem.
var _ lifecycle.Subsystem = (*setup.AuthSubsystem)(nil)

func TestAuthSubsystemIDReturnsAuth(t *testing.T) {
	sub := setup.NewAuthSubsystem(setup.AuthSubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemAuth, sub.ID())
}

func TestAuthSubsystemDependsOnDatabase(t *testing.T) {
	sub := setup.NewAuthSubsystem(setup.AuthSubsystemConfig{})
	assert.Equal(t, []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}, sub.DependsOn())
}

func TestAuthSubsystemServicePanicsBeforeStart(t *testing.T) {
	sub := setup.NewAuthSubsystem(setup.AuthSubsystemConfig{})
	assert.Panics(t, func() { sub.AuthService() })
}

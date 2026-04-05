// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/auth/setup"
	"github.com/holomush/holomush/internal/lifecycle"
)

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

func TestAuthSubsystemImplementsSubsystem(_ *testing.T) {
	sub := setup.NewAuthSubsystem(setup.AuthSubsystemConfig{})
	var _ lifecycle.Subsystem = sub
}

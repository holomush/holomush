// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/lifecycle"
)

// Compile-time interface check: *setup.BootstrapSubsystem must satisfy lifecycle.Subsystem.
var _ lifecycle.Subsystem = (*setup.BootstrapSubsystem)(nil)

func TestBootstrapSubsystemIDReturnsBootstrap(t *testing.T) {
	sub := setup.NewBootstrapSubsystem(setup.BootstrapSubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemBootstrap, sub.ID())
}

func TestBootstrapSubsystemDependsOnRequiredSubsystems(t *testing.T) {
	sub := setup.NewBootstrapSubsystem(setup.BootstrapSubsystemConfig{})
	deps := sub.DependsOn()

	assert.Contains(t, deps, lifecycle.SubsystemDatabase)
	assert.Contains(t, deps, lifecycle.SubsystemABAC)
	assert.Contains(t, deps, lifecycle.SubsystemWorld)
	assert.Contains(t, deps, lifecycle.SubsystemAuth)
	assert.Contains(t, deps, lifecycle.SubsystemPlugins)
	assert.Contains(t, deps, lifecycle.SubsystemSessions)
	assert.Len(t, deps, 6)
}

func TestBootstrapSubsystemStartLocationIDPanicsBeforeStart(t *testing.T) {
	sub := setup.NewBootstrapSubsystem(setup.BootstrapSubsystemConfig{})
	assert.Panics(t, func() { sub.StartLocationID() })
}

func TestBootstrapSubsystemStopIsNoop(t *testing.T) {
	sub := setup.NewBootstrapSubsystem(setup.BootstrapSubsystemConfig{})
	assert.NoError(t, sub.Stop(t.Context()))
}

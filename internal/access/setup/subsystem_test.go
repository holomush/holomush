// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/lifecycle"
)

func TestABACSubsystemIDReturnsABAC(t *testing.T) {
	sub := setup.NewABACSubsystem(setup.ABACSubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemABAC, sub.ID())
}

func TestABACSubsystemDependsOnDatabase(t *testing.T) {
	sub := setup.NewABACSubsystem(setup.ABACSubsystemConfig{})
	assert.Equal(t, []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}, sub.DependsOn())
}

func TestABACSubsystemEnginePanicsBeforeStart(t *testing.T) {
	sub := setup.NewABACSubsystem(setup.ABACSubsystemConfig{})
	assert.Panics(t, func() { sub.Engine() })
}

func TestABACSubsystemImplementsSubsystem(_ *testing.T) {
	sub := setup.NewABACSubsystem(setup.ABACSubsystemConfig{})
	var _ lifecycle.Subsystem = sub
}

// TestABACSubsystemPolicyStorePanicsBeforeStart verifies that PolicyStore panics before Start.
func TestABACSubsystemPolicyStorePanicsBeforeStart(t *testing.T) {
	sub := setup.NewABACSubsystem(setup.ABACSubsystemConfig{})
	assert.Panics(t, func() { sub.PolicyStore() })
}

// TestABACSubsystemPolicyInstallerPanicsBeforeStart verifies that PolicyInstaller panics before Start.
func TestABACSubsystemPolicyInstallerPanicsBeforeStart(t *testing.T) {
	sub := setup.NewABACSubsystem(setup.ABACSubsystemConfig{})
	assert.Panics(t, func() { sub.PolicyInstaller() })
}

// TestABACSubsystemPluginProviderPanicsBeforeStart verifies that PluginProvider panics before Start.
func TestABACSubsystemPluginProviderPanicsBeforeStart(t *testing.T) {
	sub := setup.NewABACSubsystem(setup.ABACSubsystemConfig{})
	assert.Panics(t, func() { sub.PluginProvider() })
}

// TestABACSubsystemHealthTrackerPanicsBeforeStart verifies that HealthTracker panics before Start.
func TestABACSubsystemHealthTrackerPanicsBeforeStart(t *testing.T) {
	sub := setup.NewABACSubsystem(setup.ABACSubsystemConfig{})
	assert.Panics(t, func() { sub.HealthTracker() })
}

// TestABACSubsystemStopBeforeStartIsNoOp verifies that Stop before Start returns nil and does not panic.
func TestABACSubsystemStopBeforeStartIsNoOp(t *testing.T) {
	sub := setup.NewABACSubsystem(setup.ABACSubsystemConfig{})
	assert.NotPanics(t, func() {
		err := sub.Stop(nil) //nolint:staticcheck // nil context is intentional for this unit test
		assert.NoError(t, err)
	})
}
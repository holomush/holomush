// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/plugin/setup"
)

func TestPluginSubsystemIDReturnsPlugins(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemPlugins, sub.ID())
}

func TestPluginSubsystemDependsOnRequiredSubsystems(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	deps := sub.DependsOn()

	assert.Contains(t, deps, lifecycle.SubsystemDatabase)
	assert.Contains(t, deps, lifecycle.SubsystemABAC)
	assert.Contains(t, deps, lifecycle.SubsystemWorld)
	assert.Contains(t, deps, lifecycle.SubsystemAuth)
}

func TestPluginSubsystemManagerPanicsBeforeStart(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	assert.Panics(t, func() { sub.Manager() })
}

func TestPluginSubsystemCommandRegistryPanicsBeforeStart(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	assert.Panics(t, func() { sub.CommandRegistry() })
}

func TestPluginSubsystemServiceProxyPanicsBeforeStart(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	assert.Panics(t, func() { sub.ServiceProxy() })
}

func TestPluginSubsystemImplementsSubsystem(_ *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	var _ lifecycle.Subsystem = sub
}

func TestPluginSubsystemStopBeforeStartIsNoop(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	assert.NoError(t, sub.Stop(t.Context()))
}

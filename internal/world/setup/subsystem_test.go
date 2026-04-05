// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/world/setup"
)

func TestWorldSubsystemIDReturnsWorld(t *testing.T) {
	sub := setup.NewWorldSubsystem(setup.WorldSubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemWorld, sub.ID())
}

func TestWorldSubsystemDependsOnDatabaseAndABAC(t *testing.T) {
	sub := setup.NewWorldSubsystem(setup.WorldSubsystemConfig{})
	assert.Equal(t, []lifecycle.SubsystemID{lifecycle.SubsystemDatabase, lifecycle.SubsystemABAC}, sub.DependsOn())
}

func TestWorldSubsystemServicePanicsBeforeStart(t *testing.T) {
	sub := setup.NewWorldSubsystem(setup.WorldSubsystemConfig{})
	assert.Panics(t, func() { sub.Service() })
}

func TestWorldSubsystemImplementsSubsystem(_ *testing.T) {
	sub := setup.NewWorldSubsystem(setup.WorldSubsystemConfig{})
	var _ lifecycle.Subsystem = sub
}

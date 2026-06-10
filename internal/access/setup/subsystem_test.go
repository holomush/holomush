// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/lifecycle"
)

// Compile-time interface check: *setup.ABACSubsystem must satisfy lifecycle.Subsystem.
var _ lifecycle.Subsystem = (*setup.ABACSubsystem)(nil)

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


func TestABACSubsystemAttributeResolverPanicsBeforeStart(t *testing.T) {
	sub := setup.NewABACSubsystem(setup.ABACSubsystemConfig{})
	assert.Panics(t, func() { sub.AttributeResolver() })
}

func TestABACSubsystemAuditLoggerPanicsBeforeStart(t *testing.T) {
	sub := setup.NewABACSubsystem(setup.ABACSubsystemConfig{})
	assert.Panics(t, func() { sub.AuditLogger() })
}

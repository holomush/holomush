// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/session/setup"
)

func TestSessionSubsystemIDReturnsSessions(t *testing.T) {
	sub := setup.NewSessionSubsystem(setup.SessionSubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemSessions, sub.ID())
}

func TestSessionSubsystemDependsOnDatabase(t *testing.T) {
	sub := setup.NewSessionSubsystem(setup.SessionSubsystemConfig{})
	assert.Equal(t, []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}, sub.DependsOn())
}

func TestSessionSubsystemStorePanicsBeforeStart(t *testing.T) {
	sub := setup.NewSessionSubsystem(setup.SessionSubsystemConfig{})
	assert.Panics(t, func() { sub.Store() })
}

func TestSessionSubsystemImplementsSubsystem(_ *testing.T) {
	sub := setup.NewSessionSubsystem(setup.SessionSubsystemConfig{})
	var _ lifecycle.Subsystem = sub
}

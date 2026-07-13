// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
)

// TestOutboxRelaySubsystemIdentity gives the subsystem wiring gating-CI seam
// coverage (no containers): its SubsystemID and declared dependencies, plus the
// GameID default. A regression in the wiring is caught in the normal `task test`
// run rather than only under integration.
func TestOutboxRelaySubsystemIdentity(t *testing.T) {
	s := NewOutboxRelaySubsystem(OutboxRelaySubsystemConfig{})

	assert.Equal(t, lifecycle.SubsystemOutboxRelay, s.ID())
	require.Equal(t,
		[]lifecycle.SubsystemID{lifecycle.SubsystemDatabase, lifecycle.SubsystemEventBus},
		s.DependsOn(),
		"the relay DependsOn Database + EventBus")
	assert.Equal(t, defaultRelayGameID, s.cfg.GameID, "GameID defaults to the world default")
}

// TestOutboxRelaySubsystemGameIDOverride verifies an explicit GameID is honored.
func TestOutboxRelaySubsystemGameIDOverride(t *testing.T) {
	s := NewOutboxRelaySubsystem(OutboxRelaySubsystemConfig{GameID: "alt-game"})
	assert.Equal(t, "alt-game", s.cfg.GameID)
}

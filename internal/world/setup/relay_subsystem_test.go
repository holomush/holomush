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
// coverage (no containers): its SubsystemID and declared dependencies. A
// regression in the wiring is caught in the normal `task test` run rather
// than only under integration.
func TestOutboxRelaySubsystemIdentity(t *testing.T) {
	s := NewOutboxRelaySubsystem(OutboxRelaySubsystemConfig{})

	assert.Equal(t, lifecycle.SubsystemOutboxRelay, s.ID())
	require.Equal(t,
		[]lifecycle.SubsystemID{lifecycle.SubsystemDatabase, lifecycle.SubsystemEventBus},
		s.DependsOn(),
		"the relay DependsOn Database + EventBus")
}

// TestOutboxRelayResolvedGameIDDefaultsAtStart proves the "main" default is
// applied at Start-time resolution (07-09 item 7), not at construction — a nil
// GameID provider resolves to defaultRelayGameID.
func TestOutboxRelayResolvedGameIDDefaultsAtStart(t *testing.T) {
	s := NewOutboxRelaySubsystem(OutboxRelaySubsystemConfig{})
	assert.Nil(t, s.cfg.GameID, "constructor must not synthesize a default provider")

	got := s.resolveGameID()
	assert.Equal(t, defaultRelayGameID, got, "resolveGameID defaults to the world default when the provider is nil/empty")
}

// TestOutboxRelayResolvedGameIDOverride verifies an explicit GameID provider
// is honored at Start-time resolution.
func TestOutboxRelayResolvedGameIDOverride(t *testing.T) {
	s := NewOutboxRelaySubsystem(OutboxRelaySubsystemConfig{GameID: func() string { return "alt-game" }})
	assert.Equal(t, "alt-game", s.resolveGameID())
}

// TestOutboxRelayResolvedGameIDEmptyProviderFallsBackToDefault verifies a
// non-nil provider that resolves to "" still falls back to the default.
func TestOutboxRelayResolvedGameIDEmptyProviderFallsBackToDefault(t *testing.T) {
	s := NewOutboxRelaySubsystem(OutboxRelaySubsystemConfig{GameID: func() string { return "" }})
	assert.Equal(t, defaultRelayGameID, s.resolveGameID())
}

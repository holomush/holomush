// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/charname/blocklist"
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
	// IDENT-07 (02-05). Asserted as MEMBERSHIP, not by an arity: bootstrap's
	// Prepare constructs a CharacterService and may create the initial admin
	// character, so removing this edge lets that admission run against an
	// uncompiled block list. An arity-only check would go green again the
	// moment someone swapped this entry for any other.
	assert.Contains(t, deps, lifecycle.SubsystemCharacterNameBlockList)
	assert.Len(t, deps, 7)
}

func TestBootstrapSubsystemCarriesTheBlockListTransportAsALiveMatcher(t *testing.T) {
	// The transport plan 02-06 consumes: the whole subsystem pointer, matching
	// how every other collaborator is supplied. Naming the composition roots is
	// not wiring them — this field is the declared path from the one
	// blocklist.Subsystem cmd/holomush constructs to the gate this subsystem
	// builds. What is asserted here is that what arrives is the LIVE matcher,
	// available before anything has been prepared.
	blockList := blocklist.NewSubsystem(blocklist.SubsystemConfig{})
	sub := setup.NewBootstrapSubsystem(setup.BootstrapSubsystemConfig{BlockList: blockList})

	require.Same(t, blockList, sub.BlockList())
	matcher := sub.BlockList().Matcher()
	require.NotNil(t, matcher)
	assert.Same(t, matcher, blockList.Matcher(), "Matcher is a stable live value, not a fresh snapshot per call")
}

func TestBootstrapSubsystemStartLocationIDPanicsBeforeStart(t *testing.T) {
	sub := setup.NewBootstrapSubsystem(setup.BootstrapSubsystemConfig{})
	assert.Panics(t, func() { sub.StartLocationID() })
}

func TestBootstrapSubsystemStopIsNoop(t *testing.T) {
	sub := setup.NewBootstrapSubsystem(setup.BootstrapSubsystemConfig{})
	assert.NoError(t, sub.Stop(t.Context()))
}

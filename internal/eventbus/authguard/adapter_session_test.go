// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/authguard"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// TestSessionBridgeGuardConvertsIdentityAndDelegatesToGuard verifies that
// SessionBridgeGuard converts an eventbus.SessionCheckRequest to an
// authguard.CheckRequest, delegates to the wrapped Guard, and returns the
// expected permit decision.
func TestSessionBridgeGuardConvertsIdentityAndDelegatesToGuard(t *testing.T) {
	parts := []dek.Participant{{PlayerID: "01ABC", CharacterID: "01XYZ", BindingID: "01DEF"}}

	guard, err := authguard.New(
		&fakeParticipants{list: parts},
		&fakeManifest{},
		policytest.AllowAllEngine(),
		&fakeBackpressure{},
	)
	require.NoError(t, err)

	bridge := authguard.NewSessionBridgeGuard(guard)

	req := eventbus.SessionCheckRequest{
		Identity: eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    "01ABC",
			CharacterID: "01XYZ",
			BindingID:   "01DEF",
		},
		KeyID:      codec.KeyID(42),
		KeyVersion: 1,
	}

	decision, err := bridge.Check(t.Context(), req)
	require.NoError(t, err)
	assert.True(t, decision.Permit, "character with matching binding should be permitted")
}

// TestSessionBridgeGuardDeniesWhenUnderlyingGuardDenies verifies that when
// the underlying Guard denies a request, SessionBridgeGuard propagates the
// deny decision without error.
func TestSessionBridgeGuardDeniesWhenUnderlyingGuardDenies(t *testing.T) {
	// Guard with no participants — any character check will deny.
	guard, err := authguard.New(
		&fakeParticipants{list: nil},
		&fakeManifest{},
		policytest.DenyAllEngine(),
		&fakeBackpressure{},
	)
	require.NoError(t, err)

	bridge := authguard.NewSessionBridgeGuard(guard)

	req := eventbus.SessionCheckRequest{
		Identity: eventbus.SessionIdentity{
			Kind:        eventbus.IdentityKindCharacter,
			PlayerID:    "01ABC",
			CharacterID: "01XYZ",
			BindingID:   "01DEF",
		},
		KeyID:      codec.KeyID(42),
		KeyVersion: 1,
	}

	decision, err := bridge.Check(t.Context(), req)
	require.NoError(t, err)
	assert.False(t, decision.Permit, "non-participant should be denied")
}

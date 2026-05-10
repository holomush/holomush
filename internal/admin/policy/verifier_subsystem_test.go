// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/lifecycle"
)

func TestCryptoChainVerifierSubsystemIDReturnsCorrectID(t *testing.T) {
	s := policy.NewCryptoChainVerifierSubsystem(policy.CryptoChainVerifierConfig{})
	assert.Equal(t, lifecycle.SubsystemCryptoChainVerifier, s.ID())
}

func TestCryptoChainVerifierSubsystemDependsOnDatabaseOnly(t *testing.T) {
	s := policy.NewCryptoChainVerifierSubsystem(policy.CryptoChainVerifierConfig{})
	deps := s.DependsOn()
	require.Len(t, deps, 1)
	assert.Equal(t, lifecycle.SubsystemDatabase, deps[0])
}

func TestCryptoChainVerifierSubsystemStopIsNoOp(t *testing.T) {
	s := policy.NewCryptoChainVerifierSubsystem(policy.CryptoChainVerifierConfig{})
	require.NoError(t, s.Stop(context.Background()))
}

// TestCryptoChainVerifierSubsystemStartReturnsNilOnEmptyPolicyNames is a unit
// test using a nil pool but empty PolicyNames so the loop never enters.
func TestCryptoChainVerifierSubsystemStartReturnsNilOnEmptyPolicyNames(t *testing.T) {
	s := policy.NewCryptoChainVerifierSubsystem(policy.CryptoChainVerifierConfig{
		Pool:        nil,
		GameID:      "testgame",
		PolicyNames: nil,
	})
	require.NoError(t, s.Start(context.Background()))
}

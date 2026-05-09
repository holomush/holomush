// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package policy_test

import (
	"context"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/policy"
)

// TestCryptoChainVerifierSubsystemFailsStartOnBrokenChain seeds events_audit
// with a chain that has a corrupt prev_hash linkage; Start must return a
// non-nil error wrapping the verifier's POLICY_CHAIN_BROKEN_LINK code.
//
// Note: oops.AsOops returns the DEEPEST code in the chain. The outer
// wrap added by Start is CRYPTO_CHAIN_VERIFY_FAILED but the assertion
// targets the inner POLICY_CHAIN_* code via oops's behavior.
func TestCryptoChainVerifierSubsystemFailsStartOnBrokenChain(t *testing.T) {
	gameID := "verifierbroken"
	subject := "events." + gameID + ".system.crypto_policy.dual_control_required"
	defer testPool.Exec(context.Background(), `DELETE FROM events_audit WHERE subject = $1`, subject) //nolint:errcheck

	// Seed a valid genesis row.
	gen := policy.PolicySetPayload{
		PolicyName:      "dual_control_required",
		PolicySnapshot:  map[string]any{"required_op_kinds": []any{"rekey"}},
		PrevHash:        nil,
		ServerStartULID: "01HZSEED0000000000000000",
		ServerIdentity:  "holomush@seed",
		Timestamp:       time.Unix(1700000000, 0).UTC(),
	}
	genHash, err := policy.ComputePolicyHash(&gen)
	require.NoError(t, err)
	gen.PolicyHash = genHash
	insertChainRow(t, subject, 1, gen)

	// Seed a broken-link extension: prev_hash is wrong.
	ext := policy.PolicySetPayload{
		PolicyName:      "dual_control_required",
		PolicySnapshot:  map[string]any{"required_op_kinds": []any{"rekey", "admin_read_stream"}},
		PrevHash:        []byte{0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef},
		ServerStartULID: "01HZSEED0000000000000000",
		ServerIdentity:  "holomush@seed",
		Timestamp:       time.Unix(1700000060, 0).UTC(),
	}
	extHash, err := policy.ComputePolicyHash(&ext)
	require.NoError(t, err)
	ext.PolicyHash = extHash
	insertChainRow(t, subject, 2, ext)

	s := policy.NewCryptoChainVerifierSubsystem(policy.CryptoChainVerifierConfig{
		Pool:        testPool,
		GameID:      gameID,
		PolicyNames: []string{"dual_control_required"},
	})
	err = s.Start(context.Background())
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	// oops.AsOops returns the deepest oops code; the verifier's code is
	// POLICY_CHAIN_BROKEN_LINK or POLICY_CHAIN_HASH_MISMATCH depending on
	// which fired first. Either is a valid fail-closed signal.
	assert.Contains(t, []string{"POLICY_CHAIN_BROKEN_LINK", "POLICY_CHAIN_HASH_MISMATCH"}, o.Code(),
		"verifier should fail-closed; got %s", o.Code())
}

// TestCryptoChainVerifierSubsystemAcceptsValidChain — happy path.
func TestCryptoChainVerifierSubsystemAcceptsValidChain(t *testing.T) {
	gameID := "verifierok"
	subject := "events." + gameID + ".system.crypto_policy.dual_control_required"
	defer testPool.Exec(context.Background(), `DELETE FROM events_audit WHERE subject = $1`, subject) //nolint:errcheck

	gen := policy.PolicySetPayload{
		PolicyName:      "dual_control_required",
		PolicySnapshot:  map[string]any{"required_op_kinds": []any{"rekey"}},
		PrevHash:        nil,
		ServerStartULID: "01HZSEED0000000000000000",
		ServerIdentity:  "holomush@seed",
		Timestamp:       time.Unix(1700000000, 0).UTC(),
	}
	genHash, err := policy.ComputePolicyHash(&gen)
	require.NoError(t, err)
	gen.PolicyHash = genHash
	insertChainRow(t, subject, 1, gen)

	s := policy.NewCryptoChainVerifierSubsystem(policy.CryptoChainVerifierConfig{
		Pool:        testPool,
		GameID:      gameID,
		PolicyNames: []string{"dual_control_required"},
	})
	require.NoError(t, s.Start(context.Background()))
}

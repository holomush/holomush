// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helperPayload constructs a deterministic PolicySetPayload for chain tests.
// PrevHash and PolicyHash are NOT set by this helper; callers fill them in.
// name is used as PolicyName so that tests covering multiple policy names
// (e.g. "crypto.operators", "crypto.admins") can differ only in that field.
func helperPayload(name string, prev []byte, ts int64) PolicySetPayload {
	return PolicySetPayload{
		PolicyName:      name,
		PolicySnapshot:  map[string]any{"members": []any{}},
		PrevHash:        prev,
		ServerStartULID: "01HZSTART0000000000000000",
		ServerIdentity:  "holomush@host",
		Timestamp:       time.Unix(ts, 0).UTC(),
	}
}

// helperEntry computes the policy_hash and returns a chainEntry with it set.
func helperEntry(t *testing.T, seq int64, p PolicySetPayload) chainEntry {
	t.Helper()
	h, err := ComputePolicyHash(&p)
	require.NoError(t, err)
	p.PolicyHash = h
	return chainEntry{Seq: seq, Payload: p}
}

// TestVerifyChainEntriesAcceptsEmptyChain verifies that an empty slice
// returns nil (fresh DB; genesis is emitted by the CryptoPolicySubsystem).
func TestVerifyChainEntriesAcceptsEmptyChain(t *testing.T) {
	require.NoError(t, verifyChainEntries(nil, "crypto.operators"))
}

// TestVerifyChainEntriesAcceptsValidGenesis verifies a single-row chain
// whose prev_hash is nil and whose policy_hash matches its own payload.
func TestVerifyChainEntriesAcceptsValidGenesis(t *testing.T) {
	gen := helperEntry(t, 1, helperPayload("crypto.operators", nil, 1700000000))
	require.NoError(t, verifyChainEntries([]chainEntry{gen}, "crypto.operators"))
}

// TestVerifyChainEntriesAcceptsValidExtension verifies a two-row chain
// where the second row's prev_hash matches the first row's policy_hash.
func TestVerifyChainEntriesAcceptsValidExtension(t *testing.T) {
	gen := helperEntry(t, 1, helperPayload("crypto.operators", nil, 1700000000))
	ext := helperEntry(t, 2, helperPayload("crypto.operators", gen.Payload.PolicyHash, 1700000060))
	require.NoError(t, verifyChainEntries([]chainEntry{gen, ext}, "crypto.operators"))
}

// TestVerifyChainEntriesRejectsBrokenGenesis verifies INV-D10: a genesis
// row with non-nil prev_hash produces POLICY_CHAIN_BROKEN_GENESIS.
func TestVerifyChainEntriesRejectsBrokenGenesis(t *testing.T) {
	bad := helperEntry(t, 1, helperPayload("crypto.operators", []byte{0xff}, 1700000000))
	err := verifyChainEntries([]chainEntry{bad}, "crypto.operators")
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "POLICY_CHAIN_BROKEN_GENESIS", o.Code())
}

// TestVerifyChainEntriesRejectsBrokenLink verifies INV-D11: a non-genesis
// row whose prev_hash does not match the predecessor produces
// POLICY_CHAIN_BROKEN_LINK.
func TestVerifyChainEntriesRejectsBrokenLink(t *testing.T) {
	gen := helperEntry(t, 1, helperPayload("crypto.operators", nil, 1700000000))
	// Build ext with a wrong prev_hash so the link is broken.
	corrupt := helperPayload("crypto.operators", []byte{0xde, 0xad, 0xbe, 0xef}, 1700000060)
	corruptEntry := helperEntry(t, 2, corrupt)
	err := verifyChainEntries([]chainEntry{gen, corruptEntry}, "crypto.operators")
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "POLICY_CHAIN_BROKEN_LINK", o.Code())
}

// TestVerifyChainEntriesRejectsHashMismatch verifies INV-D12: a row whose
// stored policy_hash does not match its recomputed hash produces
// POLICY_CHAIN_HASH_MISMATCH (payload tampering after storage).
func TestVerifyChainEntriesRejectsHashMismatch(t *testing.T) {
	gen := helperEntry(t, 1, helperPayload("crypto.operators", nil, 1700000000))
	ext := helperEntry(t, 2, helperPayload("crypto.operators", gen.Payload.PolicyHash, 1700000060))
	// Tamper with ext's payload AFTER its hash was computed, simulating
	// a storage-layer mutation of the policy_snapshot.
	ext.Payload.PolicySnapshot = map[string]any{"members": []any{"tampered"}}
	err := verifyChainEntries([]chainEntry{gen, ext}, "crypto.operators")
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "POLICY_CHAIN_HASH_MISMATCH", o.Code())
}

// TestVerifyChainEntriesDecodesEnvelopeAndJSON documents the two-step
// decode shape (proto Event -> JSON PolicySetPayload) at the unit level by
// round-tripping a payload through JSON marshal/unmarshal and asserting
// field equality after the round-trip.
func TestVerifyChainEntriesDecodesEnvelopeAndJSON(t *testing.T) {
	original := helperPayload("crypto.operators", nil, 1700000000)
	h, err := ComputePolicyHash(&original)
	require.NoError(t, err)
	original.PolicyHash = h

	// JSON marshal -> unmarshal round-trip (the inner stage of loadChainEntries).
	bodyJSON, err := json.Marshal(&original)
	require.NoError(t, err)
	var decoded PolicySetPayload
	require.NoError(t, json.Unmarshal(bodyJSON, &decoded))
	assert.Equal(t, original.PolicyName, decoded.PolicyName)
	assert.Equal(t, original.PolicyHash, decoded.PolicyHash)
	assert.Equal(t, original.PrevHash, decoded.PrevHash)
	assert.Equal(t, original.ServerStartULID, decoded.ServerStartULID)
}

// TestVerifyChainEntriesAcceptsMultiplePolicyNames verifies that the
// verifier is policy-name agnostic — the policyName arg is used only for
// error context and does not affect integrity checking.
func TestVerifyChainEntriesAcceptsMultiplePolicyNames(t *testing.T) {
	for _, name := range []string{"crypto.operators", "crypto.admins", "crypto.auditors"} {
		gen := helperEntry(t, 1, helperPayload(name, nil, 1700000000))
		require.NoError(t, verifyChainEntries([]chainEntry{gen}, name),
			"expected valid genesis chain for policy %s", name)
	}
}

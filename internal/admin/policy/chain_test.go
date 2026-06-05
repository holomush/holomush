// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy_test

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/policy"
)

// fixedPayload returns a deterministic PolicySetPayload for golden-vector
// testing. PolicyHash field is intentionally non-nil to verify it is
// excluded from the canonicalized hash input (INV-CRYPTO-79).
func fixedPayload() *policy.PolicySetPayload {
	return &policy.PolicySetPayload{
		PolicyName: "crypto.operators",
		PolicySnapshot: map[string]any{
			"members":        []any{"01HZA000000000000000000000"},
			"dual_control":   "lax+warn",
			"approval_ttl_s": float64(300),
		},
		PolicyHash:      []byte("not-the-real-hash-zeroed-on-input"),
		PrevHash:        nil,
		ServerStartULID: "01HZSTART0000000000000000",
		ServerIdentity:  "holomush@hostname",
		Timestamp:       time.Unix(1700000000, 0).UTC(),
	}
}

// TestComputePolicyHashGoldenValue locks the canonicalization output to
// a known SHA-256 value. INV-CRYPTO-79. If this test starts failing, the JCS
// canonicalizer or json.Marshal output changed shape — treat as a
// chain-breaking master-spec amendment per INV-CRYPTO-80.
//
// The expected hex is computed from the fixedPayload above with
// PolicyHash zeroed; if you change the fixture, recompute via:
//
//	go run scripts/cmd/compute-policy-hash/main.go
//
// and update both fixture + expected together.
func TestComputePolicyHashGoldenValue(t *testing.T) {
	got, err := policy.ComputePolicyHash(fixedPayload())
	require.NoError(t, err)
	require.Len(t, got, 32, "SHA-256 output must be 32 bytes")
	// Golden vector — locked. If this fails, the JCS canonicalizer or
	// json.Marshal output changed; treat as a chain-breaking amendment (INV-CRYPTO-80).
	const expectedHex = "032be94de2221bf7643d5c1ecdf07e7da5ac203d82d8cd3aefc0a72efbde096c"
	assert.Equal(t, expectedHex, hex.EncodeToString(got))
}

// TestComputePolicyHashExcludesPolicyHashField verifies the PolicyHash
// field is zeroed before canonicalization, so two payloads differing
// only in PolicyHash produce the same hash. INV-CRYPTO-79.
func TestComputePolicyHashExcludesPolicyHashField(t *testing.T) {
	p1 := fixedPayload()
	p1.PolicyHash = []byte("AAA")
	h1, err := policy.ComputePolicyHash(p1)
	require.NoError(t, err)

	p2 := fixedPayload()
	p2.PolicyHash = []byte("BBB-totally-different-bytes")
	h2, err := policy.ComputePolicyHash(p2)
	require.NoError(t, err)

	assert.Equal(t, h1, h2, "PolicyHash field must not bleed into its own input")
}

// TestComputePolicyHashNormalizesEmptyPrevHashToNil verifies that a
// genesis-shaped payload with PrevHash=[]byte{} produces the same hash as
// PrevHash=nil. INV-CRYPTO-77 says genesis prev_hash is nil; this guards against
// json.Marshal's `null` vs `""` divergence silently producing two distinct
// genesis hashes for callers that initialize PrevHash differently.
func TestComputePolicyHashNormalizesEmptyPrevHashToNil(t *testing.T) {
	pNil := fixedPayload()
	pNil.PrevHash = nil
	hNil, err := policy.ComputePolicyHash(pNil)
	require.NoError(t, err)

	pEmpty := fixedPayload()
	pEmpty.PrevHash = []byte{}
	hEmpty, err := policy.ComputePolicyHash(pEmpty)
	require.NoError(t, err)

	assert.Equal(t, hNil, hEmpty,
		"nil and []byte{} PrevHash MUST produce the same hash (canonical absent form is nil)")
}

// TestComputePolicyHashStableUnderJSONFieldReorder verifies JCS sorts
// keys deterministically. INV-CRYPTO-80 (the canonicalizer's own contract;
// guards against future field-order changes in PolicySetPayload struct
// silently breaking chain integrity).
func TestComputePolicyHashStableUnderJSONFieldReorder(t *testing.T) {
	p1 := fixedPayload()
	p1.PolicySnapshot = map[string]any{
		"members":        []any{"01HZA000000000000000000000"},
		"dual_control":   "lax+warn",
		"approval_ttl_s": float64(300),
	}
	h1, err := policy.ComputePolicyHash(p1)
	require.NoError(t, err)

	p2 := fixedPayload()
	// Same logical contents, different Go map iteration order is irrelevant
	// (Go randomizes anyway); JCS sorts keys lexicographically.
	p2.PolicySnapshot = map[string]any{
		"approval_ttl_s": float64(300),
		"dual_control":   "lax+warn",
		"members":        []any{"01HZA000000000000000000000"},
	}
	h2, err := policy.ComputePolicyHash(p2)
	require.NoError(t, err)

	assert.Equal(t, h1, h2, "JCS must sort keys; logically-equal payloads must hash to the same value")
}

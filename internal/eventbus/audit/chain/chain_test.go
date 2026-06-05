// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package chain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
)

// ---------------------------------------------------------------------------
// ZeroField tests
// ---------------------------------------------------------------------------

// TestZeroField_TopLevel verifies that a top-level field is set to nil in the
// returned copy and that the original map is unchanged.
func TestZeroField_TopLevel(t *testing.T) {
	payload := map[string]any{
		"self_hash": []byte("somehash"),
		"scope":     "events.game.system.rekey",
		"count":     42,
	}
	got := chain.ZeroField(payload, "self_hash")
	assert.Nil(t, got["self_hash"], "ZeroField must set the top-level field to nil")
	assert.Equal(t, "events.game.system.rekey", got["scope"], "other fields must be preserved")
	// original must be unchanged
	assert.NotNil(t, payload["self_hash"], "ZeroField must not mutate the original map")
}

// TestZeroField_NestedDotPath verifies that a dot-delimited path zeroes the
// target leaf field within its parent map without disturbing siblings.
func TestZeroField_NestedDotPath(t *testing.T) {
	payload := map[string]any{
		"meta": map[string]any{
			"self_hash": []byte("nestedhash"),
			"scope":     "events.game.system.rekey",
		},
		"count": 7,
	}
	got := chain.ZeroField(payload, "meta.self_hash")
	meta, ok := got["meta"].(map[string]any)
	require.True(t, ok, "meta field must remain a map[string]any")
	assert.Nil(t, meta["self_hash"], "nested self_hash must be nil after ZeroField")
	assert.Equal(t, "events.game.system.rekey", meta["scope"], "sibling field must be preserved")
	assert.Equal(t, 7, got["count"], "top-level sibling must be preserved")
}

// TestZeroField_MissingField_NoOp verifies that ZeroField is a no-op when the
// named field does not exist in the payload — no panic, payload returned intact.
func TestZeroField_MissingField_NoOp(t *testing.T) {
	payload := map[string]any{
		"scope": "events.game.system.rekey",
	}
	got := chain.ZeroField(payload, "nonexistent")
	assert.Equal(t, payload, got, "ZeroField on missing field must return payload unchanged")
}

// ---------------------------------------------------------------------------
// RecomputeSelfHash tests
// ---------------------------------------------------------------------------

// TestRecomputeSelfHash_DeterministicOverPayloadOrder verifies that two
// logically-equivalent payloads (same keys and values, different insertion
// order) produce the same SHA-256 hash — proving JCS key-sort is applied.
// INV-CRYPTO-115: hash = SHA-256(JCS(zero(payload, SelfHashFieldName))).
func TestRecomputeSelfHash_DeterministicOverPayloadOrder(t *testing.T) {
	// Build two maps with identical contents but different key insertion order.
	p1 := map[string]any{
		"self_hash": []byte("ignored"),
		"scope":     "events.game.system.rekey",
		"version":   "1",
		"operator":  "alice",
	}
	p2 := map[string]any{
		"operator":  "alice",
		"self_hash": []byte("also-ignored"),
		"version":   "1",
		"scope":     "events.game.system.rekey",
	}

	h1, err := chain.RecomputeSelfHash(p1, "self_hash")
	require.NoError(t, err)
	h2, err := chain.RecomputeSelfHash(p2, "self_hash")
	require.NoError(t, err)

	assert.Equal(t, h1, h2, "JCS must normalize key order so logically-equal payloads hash identically")
	// Verify the self_hash field itself is excluded from the input (h1 must
	// equal what we'd get with self_hash=nil).
	p3 := map[string]any{
		"self_hash": []byte("completely-different"),
		"scope":     "events.game.system.rekey",
		"version":   "1",
		"operator":  "alice",
	}
	h3, err := chain.RecomputeSelfHash(p3, "self_hash")
	require.NoError(t, err)
	assert.Equal(t, h1, h3, "self_hash field value must not affect the computed hash (excluded from JCS input)")
}

// ---------------------------------------------------------------------------
// ValidateRegistration tests
// ---------------------------------------------------------------------------

// TestValidateRegistration_RejectsNonEventsPrefix verifies INV-CRYPTO-113: a Chain
// whose SubjectPrefix does not start with "events." is rejected.
func TestValidateRegistration_RejectsNonEventsPrefix(t *testing.T) {
	bad := chain.Chain{
		SubjectPrefix:     "audit.game.system.rekey",
		SelfHashField:     "self_hash",
		PrevHashField:     "prev_hash",
		ScopePayloadField: "scope",
	}
	err := chain.ValidateRegistration(bad)
	require.Error(t, err, "INV-CRYPTO-113: non-events. prefix must be rejected")
	assert.Contains(t, err.Error(), "events.")
}

// TestValidateRegistration_RejectsMissingScopeFromPayload verifies INV-CRYPTO-114:
// a Chain with an empty ScopePayloadField is rejected.
func TestValidateRegistration_RejectsMissingScopeFromPayload(t *testing.T) {
	bad := chain.Chain{
		SubjectPrefix:     "events.game.system.rekey",
		SelfHashField:     "self_hash",
		PrevHashField:     "prev_hash",
		ScopePayloadField: "", // missing
	}
	err := chain.ValidateRegistration(bad)
	require.Error(t, err, "INV-CRYPTO-114: empty ScopePayloadField must be rejected")
}

// TestValidateRegistration_AcceptsValidChain verifies that a well-formed Chain
// descriptor passes validation without error.
func TestValidateRegistration_AcceptsValidChain(t *testing.T) {
	good := chain.Chain{
		SubjectPrefix:     "events.game.system.rekey",
		SelfHashField:     "self_hash",
		PrevHashField:     "prev_hash",
		ScopePayloadField: "scope",
	}
	require.NoError(t, chain.ValidateRegistration(good))
}

// TestValidateRegistration_RejectsEmptySelfHashField verifies that a Chain
// missing a SelfHashField name is rejected.
func TestValidateRegistration_RejectsEmptySelfHashField(t *testing.T) {
	bad := chain.Chain{
		SubjectPrefix:     "events.game.system.rekey",
		SelfHashField:     "", // missing
		PrevHashField:     "prev_hash",
		ScopePayloadField: "scope",
	}
	err := chain.ValidateRegistration(bad)
	require.Error(t, err, "empty SelfHashField must be rejected")
}

// TestValidateRegistration_RejectsEmptyPrevHashField verifies that a Chain
// missing a PrevHashField name is rejected.
func TestValidateRegistration_RejectsEmptyPrevHashField(t *testing.T) {
	bad := chain.Chain{
		SubjectPrefix:     "events.game.system.rekey",
		SelfHashField:     "self_hash",
		PrevHashField:     "", // missing
		ScopePayloadField: "scope",
	}
	err := chain.ValidateRegistration(bad)
	require.Error(t, err, "empty PrevHashField must be rejected")
}

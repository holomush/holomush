// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/policy"
	"github.com/holomush/holomush/internal/eventbus/audit/chain"
)

// TestPolicySetChain_ReducibleToDComputePolicyHash asserts the new
// generalized chain.RecomputeSelfHash composition (driven by the policy
// chain handler's Canonicalize callback) equals D's legacy
// policy.ComputePolicyHash for the full fixture set, including the
// PrevHash: []byte{} empty-form case (D's
// TestComputePolicyHashNormalizesEmptyPrevHashToNil).
//
// This is the structural guarantee that motivates the Phase 5 sub-epic E
// refactor: the policy_set chain rides the generalized auditchain primitive
// without altering the hash that's stored in events_audit. D's INV-CRYPTO-77
// PrevHash empty-form → nil normalization is preserved by
// canonicalizePolicySetPayload (chain.go) — a part of the
// PolicySetHandlerFor.Canonicalize contract.
func TestPolicySetChain_ReducibleToDComputePolicyHash(t *testing.T) {
	cases := []struct {
		name    string
		payload policy.PolicySetPayload
	}{
		{
			name: "genesis_nil_prev",
			payload: policy.PolicySetPayload{
				PolicyName:      "dual_control_required",
				PolicySnapshot:  map[string]any{"required_op_kinds": []any{"rekey"}},
				PrevHash:        nil,
				ServerStartULID: "01HZSTART0000000000000000",
				ServerIdentity:  "holomush@host",
				Timestamp:       time.Unix(1700000000, 0).UTC(),
			},
		},
		{
			name: "genesis_empty_prev",
			payload: policy.PolicySetPayload{
				PolicyName:      "dual_control_required",
				PolicySnapshot:  map[string]any{"required_op_kinds": []any{"rekey"}},
				PrevHash:        []byte{},
				ServerStartULID: "01HZSTART0000000000000000",
				ServerIdentity:  "holomush@host",
				Timestamp:       time.Unix(1700000000, 0).UTC(),
			},
		},
		{
			name: "linked_with_prev",
			payload: policy.PolicySetPayload{
				PolicyName:      "dual_control_required",
				PolicySnapshot:  map[string]any{"required_op_kinds": []any{"rekey", "admin_read_stream"}},
				PrevHash:        []byte{0x01, 0x02, 0x03, 0x04},
				ServerStartULID: "01HZSTART0000000000000001",
				ServerIdentity:  "holomush@host",
				Timestamp:       time.Unix(1700000060, 0).UTC(),
			},
		},
		{
			name: "empty_byte_prev_norm_with_snapshot",
			payload: policy.PolicySetPayload{
				PolicyName:      "policy_X",
				PolicySnapshot:  map[string]any{"members": []any{"alice", "bob"}},
				PrevHash:        []byte{},
				ServerStartULID: "abc-server-start",
				ServerIdentity:  "holomush@aux",
				Timestamp:       time.Unix(1700000000, 0).UTC(),
			},
		},
	}

	h := policy.PolicySetHandlerFor("main")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Legacy path: D's typed-struct canonicalizer.
			legacyHash, err := policy.ComputePolicyHash(&tc.payload)
			require.NoError(t, err)

			// Generalized path: marshal raw bytes, hand to the chain handler.
			// The handler's Canonicalize step replays D's PrevHash empty-form
			// → nil normalization on the JSON level. chain.RecomputeSelfHash
			// then zeroes the SelfHashField and SHA-256s the JCS bytes.
			payloadBytes, err := json.Marshal(&tc.payload)
			require.NoError(t, err)

			canonical, err := h.Canonicalize(payloadBytes)
			require.NoError(t, err)

			var m map[string]any
			require.NoError(t, json.Unmarshal(canonical, &m))

			newHash, err := chain.RecomputeSelfHash(m, h.Chain.SelfHashField)
			require.NoError(t, err)

			require.Equal(t, legacyHash, newHash,
				"reduction must hold for case %s: legacy ComputePolicyHash and "+
					"generalized auditchain.RecomputeSelfHash must produce byte-identical hashes",
				tc.name)
		})
	}
}

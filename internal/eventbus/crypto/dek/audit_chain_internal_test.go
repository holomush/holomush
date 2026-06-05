// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Internal-package tests for rekey audit-chain helpers. These tests access
// unexported functions (canonicalizeRekeyPayload, parseRekeyScopeFromPayload,
// extractRekeyPrevHash, extractRekeySelfHash) and are therefore in package dek.
// The companion external test file (audit_chain_test.go in dek_test package)
// covers the exported surface (RekeyChainFor, ParseRekeyScopeFromSubject,
// INV-CRYPTO-113/INV-CRYPTO-114/INV-CRYPTO-115 validation).
package dek

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
)

func TestCanonicalizeRekeyPayload_DeterministicAcrossKeyOrder(t *testing.T) {
	a := []byte(`{"context":{"type":"scene","id":"01ABC"},"justification":"test","force_destroy":false}`)
	b := []byte(`{"justification":"test","force_destroy":false,"context":{"id":"01ABC","type":"scene"}}`)
	ca, err := canonicalizeRekeyPayload(a)
	require.NoError(t, err)
	cb, err := canonicalizeRekeyPayload(b)
	require.NoError(t, err)
	require.Equal(t, ca, cb, "JCS produces same output regardless of key order")
}

func TestParseRekeyScopeFromPayload(t *testing.T) {
	payload := []byte(`{"context":{"type":"scene","id":"01ABC"},"other":1}`)
	scope, err := parseRekeyScopeFromPayload(payload)
	require.NoError(t, err)
	require.Equal(t, "scene:01ABC", scope)
}

func TestExtractRekeyPrevHash_AndSelfHash(t *testing.T) {
	// Hashes are stored as "sha256:<hex>" strings; extractors must decode them
	// back to raw bytes so bytes.Equal comparisons against RecomputeSelfHash
	// output (raw 32-byte SHA-256) hold correctly.
	prevHex := "aa" + "bb" + "cc" + "dd" + // 32 bytes = 64 hex chars
		"00" + "11" + "22" + "33" +
		"44" + "55" + "66" + "77" +
		"88" + "99" + "aa" + "bb" +
		"cc" + "dd" + "ee" + "ff" +
		"00" + "11" + "22" + "33" +
		"44" + "55" + "66" + "77" +
		"88" + "99" + "aa" + "bb"
	selfHex := "ff" + "ee" + "dd" + "cc" +
		"bb" + "aa" + "99" + "88" +
		"77" + "66" + "55" + "44" +
		"33" + "22" + "11" + "00" +
		"aa" + "bb" + "cc" + "dd" +
		"ee" + "ff" + "00" + "11" +
		"22" + "33" + "44" + "55" +
		"66" + "77" + "88" + "99"
	payload := []byte(`{"rekey_chain":{"prev_hash":"sha256:` + prevHex + `","self_hash":"sha256:` + selfHex + `"},"other":1}`)

	prev, err := extractRekeyPrevHash(payload)
	require.NoError(t, err)
	require.Len(t, prev, 32, "prev_hash must decode to 32 raw bytes")

	self, err := extractRekeySelfHash(payload)
	require.NoError(t, err)
	require.Len(t, self, 32, "self_hash must decode to 32 raw bytes")
}

func TestExtractRekeyPrevHash_GenesisReturnsNil(t *testing.T) {
	selfHex := "aabbccdd" + "00112233" + "44556677" + "8899aabb" +
		"ccddeeff" + "00112233" + "44556677" + "8899aabb"
	payload := []byte(`{"rekey_chain":{"prev_hash":null,"self_hash":"sha256:` + selfHex + `"}}`)
	prev, err := extractRekeyPrevHash(payload)
	require.NoError(t, err)
	require.Nil(t, prev)
}

// TestRekeyChain_INV_E28_RecomputeSelfHashRoundTrip verifies INV-CRYPTO-115:
// RecomputeSelfHash over two logically-identical payloads (different key
// order, different self_hash value) produces the same SHA-256 output.
func TestRekeyChain_INV_E28_RecomputeSelfHashRoundTrip(t *testing.T) {
	const selfHashField = "rekey_chain.self_hash"
	p1 := []byte(`{"context":{"type":"scene","id":"01ABC"},"justification":"test","rekey_chain":{"prev_hash":null,"self_hash":"XXXX"},"other":1}`)
	var m1 map[string]any
	require.NoError(t, json.Unmarshal(p1, &m1))
	h1, err := chain.RecomputeSelfHash(m1, selfHashField)
	require.NoError(t, err)

	p2 := []byte(`{"other":1,"context":{"id":"01ABC","type":"scene"},"justification":"test","rekey_chain":{"self_hash":"YYYY","prev_hash":null}}`)
	var m2 map[string]any
	require.NoError(t, json.Unmarshal(p2, &m2))
	h2, err := chain.RecomputeSelfHash(m2, selfHashField)
	require.NoError(t, err)

	require.Equal(t, h1, h2, "JCS + self_hash zeroing → same hash regardless of key order or self_hash value")
}

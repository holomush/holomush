// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Internal-package coverage tests for audit_chain.go error paths.
// Companion to audit_chain_internal_test.go — happy paths there, error
// paths here. All tests are pure-logic (no DB, no NATS).
package dek

import (
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"
)

// TestCanonicalizeRekeyPayload_InvalidJSON verifies that malformed input
// surfaces the typed DEK_REKEY_CANONICALIZE_FAILED code rather than a raw
// json.Unmarshal error. The chain verifier relies on the typed code to
// distinguish canonicalization failures from downstream JCS bugs.
func TestCanonicalizeRekeyPayload_InvalidJSON(t *testing.T) {
	_, err := canonicalizeRekeyPayload([]byte("not json"))
	require.Error(t, err)
	oerr, ok := oops.AsOops(err)
	require.True(t, ok, "must be oops error")
	require.Equal(t, "DEK_REKEY_CANONICALIZE_FAILED", oerr.Code())
}

// TestParseRekeyScopeFromPayload_BadJSONAndEmptyContext covers the three
// failure modes: malformed JSON, missing context.type, missing context.id.
// All three must produce DEK_REKEY_SCOPE_FROM_PAYLOAD_FAILED so the chain
// emitter / verifier can fail closed when payloads are incomplete.
func TestParseRekeyScopeFromPayload_BadJSONAndEmptyContext(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		{"invalid JSON", []byte("not json")},
		{"empty object", []byte(`{}`)},
		{"empty type", []byte(`{"context":{"type":"","id":"x"}}`)},
		{"empty id", []byte(`{"context":{"type":"x","id":""}}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseRekeyScopeFromPayload(tc.payload)
			require.Error(t, err)
			oerr, ok := oops.AsOops(err)
			require.True(t, ok)
			require.Equal(t, "DEK_REKEY_SCOPE_FROM_PAYLOAD_FAILED", oerr.Code())
		})
	}
}

// TestParseRekeyScopeFromSubject_ErrorPaths covers (1) prefix mismatch
// (subject doesn't start with events.<game>.system.rekey.) and (2) missing
// "<ct>.<cid>" tail. Uses the SetGameIDForTest/Cleanup pattern from
// audit_test.go to ensure the global currentGameIDForRekey is restored.
func TestParseRekeyScopeFromSubject_ErrorPaths(t *testing.T) {
	prevGameID := GameIDForTest()
	SetGameIDForTest("g1")
	t.Cleanup(func() { SetGameIDForTest(prevGameID) })

	cases := []struct {
		name    string
		subject string
	}{
		{"prefix mismatch", "events.other.system.rekey.scene.01ABC"},
		{"missing ct.cid tail", "events.g1.system.rekey.scene"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRekeyScopeFromSubject(tc.subject)
			require.Error(t, err)
			oerr, ok := oops.AsOops(err)
			require.True(t, ok)
			require.Equal(t, "DEK_REKEY_SCOPE_FROM_SUBJECT_FAILED", oerr.Code())
		})
	}
}

// TestDecodeHashString_Errors covers: empty string → (nil, nil); no
// "sha256:" prefix → error; bad hex after prefix → error. Both error paths
// surface DEK_REKEY_HASH_DECODE_FAILED so callers can wrap consistently.
func TestDecodeHashString_Errors(t *testing.T) {
	// Empty is the genesis sentinel and MUST NOT error.
	b, err := decodeHashString("")
	require.NoError(t, err)
	require.Nil(t, b)

	cases := []struct {
		name  string
		input string
	}{
		{"no prefix", "aabbccdd"},
		{"bad hex", "sha256:zzzz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeHashString(tc.input)
			require.Error(t, err)
			oerr, ok := oops.AsOops(err)
			require.True(t, ok)
			require.Equal(t, "DEK_REKEY_HASH_DECODE_FAILED", oerr.Code())
		})
	}
}

// TestExtractRekeyHashes_BadJSONAndBadHex verifies that both extractors
// wrap their errors under their respective typed codes:
// DEK_REKEY_EXTRACT_PREV_HASH_FAILED and DEK_REKEY_EXTRACT_SELF_HASH_FAILED.
func TestExtractRekeyHashes_BadJSONAndBadHex(t *testing.T) {
	// Malformed JSON: both extractors must surface their typed code.
	_, err := extractRekeyPrevHash([]byte("not json"))
	require.Error(t, err)
	oerr, ok := oops.AsOops(err)
	require.True(t, ok)
	require.Equal(t, "DEK_REKEY_EXTRACT_PREV_HASH_FAILED", oerr.Code())

	_, err = extractRekeySelfHash([]byte("not json"))
	require.Error(t, err)
	oerr, ok = oops.AsOops(err)
	require.True(t, ok)
	require.Equal(t, "DEK_REKEY_EXTRACT_SELF_HASH_FAILED", oerr.Code())

	// Bad-hex prev_hash: extractRekeyPrevHash wraps decodeHashString's
	// DEK_REKEY_HASH_DECODE_FAILED under its own code, but oops error-code
	// propagation preserves the innermost code; the test asserts that
	// invariant rather than the outer wrapping intent.
	badPrev := []byte(`{"rekey_chain":{"prev_hash":"notprefix"}}`)
	_, err = extractRekeyPrevHash(badPrev)
	require.Error(t, err)
	oerr, ok = oops.AsOops(err)
	require.True(t, ok)
	require.Equal(t, "DEK_REKEY_HASH_DECODE_FAILED", oerr.Code())

	// Bad-hex self_hash: same code-propagation behavior — innermost wins.
	badSelf := []byte(`{"rekey_chain":{"self_hash":"notprefix"}}`)
	_, err = extractRekeySelfHash(badSelf)
	require.Error(t, err)
	oerr, ok = oops.AsOops(err)
	require.True(t, ok)
	require.Equal(t, "DEK_REKEY_HASH_DECODE_FAILED", oerr.Code())
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Internal-package coverage tests for audit.go unexported helpers.
package dek

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEncodeHashPtr_NilAndNonNil verifies the two arms:
//   - nil input represents the genesis prev_hash; encoded as a nil *string so
//     json.Marshal emits `"prev_hash": null`.
//   - non-nil 32-byte input emits "sha256:<hex>" via encodeHash, matching the
//     format decodeHashString reverses (audit_chain.go).
func TestEncodeHashPtr_NilAndNonNil(t *testing.T) {
	require.Nil(t, encodeHashPtr(nil), "nil input → nil pointer (genesis)")

	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	got := encodeHashPtr(raw)
	require.NotNil(t, got)
	require.True(t, strings.HasPrefix(*got, "sha256:"), "encoded as sha256:<hex>")
	// 32 bytes → 64 hex chars + 7-char "sha256:" prefix.
	require.Len(t, *got, 7+64)
}

// TestRekeyHandlerFor_RoundTrip verifies that SubjectFor and
// ScopeFromSubject round-trip via the constructed handler. Uses the
// SetGameIDForTest/Cleanup pattern from audit_test.go so the global
// currentGameIDForRekey is restored after the test.
func TestRekeyHandlerFor_RoundTrip(t *testing.T) {
	prevGameID := GameIDForTest()
	SetGameIDForTest("g1")
	t.Cleanup(func() { SetGameIDForTest(prevGameID) })

	h := RekeyHandlerFor("g1")
	subject := h.SubjectFor("scene:01ABC")
	require.Equal(t, "events.g1.system.rekey.scene.01ABC", subject)

	scope, err := h.ScopeFromSubject(subject)
	require.NoError(t, err)
	require.Equal(t, "scene:01ABC", scope)
}

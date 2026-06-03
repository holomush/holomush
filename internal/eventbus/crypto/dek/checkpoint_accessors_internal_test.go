// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Internal-package coverage tests for Checkpoint accessors that read
// unexported byte-slice fields (policyHash, lastProcessedEventID,
// phase5MissingMembers). INV-CRYPTO-16 forbids exporting []byte from this package,
// so these accessors gate the only legal read paths and MUST be tested
// directly against the unexported fields.
package dek

import (
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"
)

// TestCheckpoint_PolicyHashAccessor verifies PolicyHash returns the stored
// 32-byte hash as a [32]byte array (INV-CRYPTO-16 — no exported []byte). The
// accessor zero-pads if the stored slice is shorter; we exercise the
// full-length path that production hits.
func TestCheckpoint_PolicyHashAccessor(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(0xA0 + i%16)
	}
	c := Checkpoint{policyHash: raw}
	got := c.PolicyHash()
	require.Equal(t, raw, got[:])
}

// TestCheckpoint_LastProcessedEventID_UnsetVsSet verifies the bool return
// distinguishes "no batches yet committed" (unset) from "cursor present".
// Production callers (RunPhase3) branch on this bool to decide between
// "scan from genesis" and "resume after cursor".
func TestCheckpoint_LastProcessedEventID_UnsetVsSet(t *testing.T) {
	// Unset: nil slice → ([16]byte{}, false).
	cUnset := Checkpoint{}
	id, ok := cUnset.LastProcessedEventID()
	require.False(t, ok)
	require.Equal(t, [16]byte{}, id)

	// Set: 16-byte slice → (copy, true).
	raw := make([]byte, 16)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	cSet := Checkpoint{lastProcessedEventID: raw}
	id, ok = cSet.LastProcessedEventID()
	require.True(t, ok)
	require.Equal(t, raw, id[:])
}

// TestCheckpoint_Phase5MissingMembers_InvalidJSON verifies the decode-error
// path: malformed JSON in the persisted column surfaces the typed
// DEK_REKEY_PHASE5_MISSING_MEMBERS_DECODE_FAILED code. The happy and NULL
// paths are covered by TestCheckpoint_Phase5MissingMembers_Decodes in
// rekey_phase5_internal_test.go; this test pins the error branch.
func TestCheckpoint_Phase5MissingMembers_InvalidJSON(t *testing.T) {
	c := Checkpoint{phase5MissingMembers: []byte("not-json")}
	_, err := c.Phase5MissingMembers()
	require.Error(t, err)
	oerr, ok := oops.AsOops(err)
	require.True(t, ok)
	require.Equal(t, "DEK_REKEY_PHASE5_MISSING_MEMBERS_DECODE_FAILED", oerr.Code())
}

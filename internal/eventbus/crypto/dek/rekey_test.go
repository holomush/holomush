// SPDX-License-Identifier: Apache-2.0
package dek_test

import (
	"testing"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/stretchr/testify/require"
)

// TestRequestID_StringAndIsZero covers the two RequestID accessors:
//   - zero value: IsZero==true, String returns the 26-char zero-ULID.
//   - non-zero (minted via idgen.New per CLAUDE.md "ULID Generation"):
//     IsZero==false, String is 26 chars.
func TestRequestID_StringAndIsZero(t *testing.T) {
	var zero dek.RequestID
	require.True(t, zero.IsZero())
	require.Len(t, zero.String(), 26, "zero ULID is 26 chars (all zeroes encoded)")

	nonZero := dek.RequestID(idgen.New())
	require.False(t, nonZero.IsZero())
	require.Len(t, nonZero.String(), 26)
}

func TestComputeRekeyArgsHash_StableAcrossEncodings(t *testing.T) {
	req := dek.RekeyRequest{
		ContextType:   "scene",
		ContextID:     "01ABC",
		Justification: "Banned user retroactive access removal, ticket #1234",
	}
	h1, err := dek.ComputeRekeyArgsHash(req)
	require.NoError(t, err)
	require.Len(t, h1[:], 32)

	// Different field order construction → same hash (proto deterministic marshal).
	req2 := dek.RekeyRequest{Justification: req.Justification, ContextID: req.ContextID, ContextType: req.ContextType}
	h2, err := dek.ComputeRekeyArgsHash(req2)
	require.NoError(t, err)
	require.Equal(t, h1, h2)
}

func TestComputeRekeyArgsHash_DiffersOnContextID(t *testing.T) {
	h1, _ := dek.ComputeRekeyArgsHash(dek.RekeyRequest{ContextType: "scene", ContextID: "01ABC"})
	h2, _ := dek.ComputeRekeyArgsHash(dek.RekeyRequest{ContextType: "scene", ContextID: "01DEF"})
	require.NotEqual(t, h1, h2)
}

func TestComputeRekeyArgsHash_DiffersOnJustification(t *testing.T) {
	h1, _ := dek.ComputeRekeyArgsHash(dek.RekeyRequest{ContextType: "scene", ContextID: "01ABC", Justification: "x"})
	h2, _ := dek.ComputeRekeyArgsHash(dek.RekeyRequest{ContextType: "scene", ContextID: "01ABC", Justification: "y"})
	require.NotEqual(t, h1, h2)
}

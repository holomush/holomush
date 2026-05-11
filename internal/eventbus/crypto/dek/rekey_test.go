// SPDX-License-Identifier: Apache-2.0
package dek_test

import (
	"testing"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/stretchr/testify/require"
)

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

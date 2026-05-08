// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp

import (
	"encoding/base32"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSecretIs32CharBase32(t *testing.T) {
	s, err := generateSecret()
	require.NoError(t, err)
	assert.Len(t, s, 32)
	_, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	require.NoError(t, err)
}

func TestGenerateSecretIsRandom(t *testing.T) {
	a, err := generateSecret()
	require.NoError(t, err)
	b, err := generateSecret()
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestBuildProvisioningURI(t *testing.T) {
	u, err := buildProvisioningURI("alice", "default", "JBSWY3DPEHPK3PXP")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(u, "otpauth://totp/"))
	// Per Google Key Uri Format spec, label prefix == issuer.
	assert.Contains(t, u, "issuer=holomush-default")
	assert.Contains(t, u, "holomush-default:alice")
	assert.Contains(t, u, "secret=JBSWY3DPEHPK3PXP")
}

func TestGenerateRecoveryCodes(t *testing.T) {
	codes, err := generateRecoveryCodes(10)
	require.NoError(t, err)
	require.Len(t, codes, 10)
	for _, c := range codes {
		assert.Len(t, c, 19) // 16 hex + 3 hyphens
		parts := strings.Split(c, "-")
		assert.Len(t, parts, 4)
		for _, p := range parts {
			assert.Len(t, p, 4)
		}
	}
	// uniqueness
	seen := map[string]bool{}
	for _, c := range codes {
		assert.False(t, seen[c])
		seen[c] = true
	}
}

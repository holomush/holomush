// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"
)

// TestDummyPasswordHashMatchesArgon2Constants guards the timing-attack mitigation
// in ValidateCredentials. The dummy hash is verified against an attacker-supplied
// password when the user does not exist so the response time matches the real
// Verify path. If argon2Memory, argon2Time, or argon2Threads ever drift from the
// parameters baked into dummyPasswordHash, the mitigation silently fails and
// user existence becomes observable via timing.
//
// This test parses the PHC string and asserts parameter equality with the
// constants in hasher.go. It also asserts the version matches argon2.Version so
// a library upgrade surfaces here rather than at runtime.
func TestDummyPasswordHashMatchesArgon2Constants(t *testing.T) {
	parts := strings.Split(dummyPasswordHash, "$")
	require.Len(t, parts, 6, "dummyPasswordHash must be a valid PHC string")
	assert.Equal(t, "argon2id", parts[1], "dummy hash must use argon2id")

	var version, memory, time uint32
	var threads uint8
	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	require.NoError(t, err, "dummyPasswordHash must have a parseable version")

	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads)
	require.NoError(t, err, "dummyPasswordHash must have parseable parameters")

	tests := []struct {
		name     string
		got      uint32
		expected uint32
	}{
		{"memory parameter matches argon2Memory", memory, uint32(argon2Memory)},
		{"iterations parameter matches argon2Time", time, uint32(argon2Time)},
		{"parallelism parameter matches argon2Threads", uint32(threads), uint32(argon2Threads)},
		{"version matches argon2.Version", version, uint32(argon2.Version)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.got,
				"dummyPasswordHash parameter drift breaks timing-attack mitigation; "+
					"update the constant in auth_service.go to match hasher.go")
		})
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseEntityResource pins the three branches the helper unifies for
// LocationProvider, CharacterProvider, PropertyProvider, ObjectProvider
// (holomush-o8g6). Each provider's ResolveResource encodes the same three
// failure modes; centralizing them here eliminates the divergence that
// existed prior to o8g6 (Location/Character: strict-SplitN; Property:
// parseEntityID-then-strict-ULID).
func TestParseEntityResource(t *testing.T) {
	t.Parallel()
	wellFormedID := ulid.Make()

	tests := []struct {
		name         string
		resourceID   string
		expectedType string
		wantID       ulid.ULID
		wantOK       bool
		wantErr      bool
		errSubstring string
	}{
		{
			name:         "matching type with valid ULID returns (id, true, nil)",
			resourceID:   "object:" + wellFormedID.String(),
			expectedType: "object",
			wantID:       wellFormedID,
			wantOK:       true,
		},
		{
			// Branch (a) — wrong type prefix. The resource belongs to a peer
			// provider's namespace; the engine routes it via target-type
			// match. Returning (false, nil) lets the next provider try.
			name:         "wrong type prefix returns (zero, false, nil)",
			resourceID:   "location:" + wellFormedID.String(),
			expectedType: "object",
			wantOK:       false,
		},
		{
			// Branch (b) — wildcard tolerance, per holomush-g776. Capability
			// grants emit "<type>:*" (e.g. CreateObject at service.go:449).
			// The engine evaluates wildcard patterns without per-instance
			// attributes; failing closed here would break every CreateX call.
			name:         "matching type with literal wildcard returns (zero, false, nil)",
			resourceID:   "object:*",
			expectedType: "object",
			wantOK:       false,
		},
		{
			// Same branch (b), different non-ULID input shape.
			name:         "matching type with non-ULID returns (zero, false, nil)",
			resourceID:   "object:not-a-ulid",
			expectedType: "object",
			wantOK:       false,
		},
		{
			// Branch (c) — malformed format (no colon delimiter at all).
			// Distinct from branch (a)/(b): this resource ID does not match
			// the grammar at all and reflects a caller bug, not a peer-type
			// or wildcard pattern.
			name:         "missing colon delimiter returns error",
			resourceID:   "object" + wellFormedID.String(),
			expectedType: "object",
			wantErr:      true,
			errSubstring: "invalid entity ID format",
		},
		{
			name:         "empty resource ID returns error",
			resourceID:   "",
			expectedType: "object",
			wantErr:      true,
			errSubstring: "invalid entity ID format",
		},
		{
			// Edge case: type prefix present but ID part empty after colon.
			// Treated like an invalid ULID (branch b), not a malformed
			// grammar error — the grammar is satisfied; the ULID isn't.
			name:         "matching type with empty ID part returns (zero, false, nil)",
			resourceID:   "object:",
			expectedType: "object",
			wantOK:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok, err := parseEntityResource(tt.resourceID, tt.expectedType)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstring != "" {
					assert.Contains(t, err.Error(), tt.errSubstring)
				}
				assert.False(t, ok, "ok MUST be false on error")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantID, id)
			}
		})
	}
}

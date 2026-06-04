// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// TestValidateEmitTypeSetEquality covers the validator's mismatch detection
// across the documented scenario matrix (matching sets, both directions of
// diff, both empty, host-owned filter per INV-PLUGIN-34). Each case exercises the
// same call shape — declared/registered inputs, three-field mismatch
// assertion — so a table form makes future scenario additions cheap.
func TestValidateEmitTypeSetEquality(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                    string
		declared                []string
		registered              []string
		wantMismatch            bool
		wantDeclaredButUnreg    []string
		wantRegisteredButUndecl []string
		assertMsg               string
	}{
		{
			name:       "matching sets produce no mismatch",
			declared:   []string{"alpha", "beta"},
			registered: []string{"alpha", "beta"},
		},
		{
			name:                 "declared-but-unregistered surfaces extras (manifest dead-declarations)",
			declared:             []string{"alpha", "beta", "gamma"},
			registered:           []string{"alpha"},
			wantMismatch:         true,
			wantDeclaredButUnreg: []string{"beta", "gamma"},
		},
		{
			name:                    "registered-but-undeclared surfaces extras (silent-plaintext risk)",
			declared:                []string{"alpha"},
			registered:              []string{"alpha", "beta", "gamma"},
			wantMismatch:            true,
			wantRegisteredButUndecl: []string{"beta", "gamma"},
		},
		{
			name:                    "both-directions diff surfaces both extra lists",
			declared:                []string{"alpha", "beta"},
			registered:              []string{"alpha", "gamma"},
			wantMismatch:            true,
			wantDeclaredButUnreg:    []string{"beta"},
			wantRegisteredButUndecl: []string{"gamma"},
		},
		{
			name:       "both-empty produces no mismatch",
			declared:   nil,
			registered: nil,
		},
		{
			name:       "host-owned types are filtered from registered before comparison (INV-PLUGIN-34)",
			declared:   []string{"plugin_alpha", "plugin_beta"},
			registered: []string{"plugin_alpha", "plugin_beta", "system", "move", "arrive"},
			assertMsg:  "host-owned types should be filtered out of registered before comparison",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mismatch := plugins.ValidateEmitTypeSetEquality(tt.declared, tt.registered)

			if tt.wantMismatch {
				require.True(t, mismatch.HasMismatch())
			} else {
				require.False(t, mismatch.HasMismatch(), tt.assertMsg)
			}
			if len(tt.wantDeclaredButUnreg) == 0 {
				require.Empty(t, mismatch.DeclaredButUnregistered)
			} else {
				require.Equal(t, tt.wantDeclaredButUnreg, mismatch.DeclaredButUnregistered)
			}
			if len(tt.wantRegisteredButUndecl) == 0 {
				require.Empty(t, mismatch.RegisteredButUndeclared)
			} else {
				require.Equal(t, tt.wantRegisteredButUndecl, mismatch.RegisteredButUndeclared)
			}
		})
	}
}

func TestValidateEmitTypeSetEquality_DoesNotMutateRegisteredInput(t *testing.T) {
	t.Parallel()

	// Validator's internal host-owned filter MUST NOT mutate the caller's
	// registered slice. Mirror the documented contract on EmitRegistry.Registered
	// (pkg/plugin/emit_registry.go) — callers may retain or mutate the returned
	// slice without affecting the registry, and symmetrically the validator
	// must not mutate slices its callers pass in.
	registered := []string{"plugin_alpha", "system", "plugin_beta", "move", "plugin_gamma"}
	snapshot := append([]string(nil), registered...)

	_ = plugins.ValidateEmitTypeSetEquality(
		[]string{"plugin_alpha", "plugin_beta", "plugin_gamma"},
		registered,
	)

	require.Equal(t, snapshot, registered,
		"ValidateEmitTypeSetEquality must not mutate the registered argument")
}

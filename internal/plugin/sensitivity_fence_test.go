// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestEnforceSensitivity(t *testing.T) {
	tests := []struct {
		name     string
		manifest plugins.Sensitivity
		claimed  bool
		want     plugins.Sensitivity
		wantErr  string
	}{
		{"never + claim=false → never", plugins.SensitivityNever, false, plugins.SensitivityNever, ""},
		{"never + claim=true → INV-PLUGIN-29 reject", plugins.SensitivityNever, true, "", "EVENT_SENSITIVITY_NOT_DECLARED"},
		{"may + claim=false → never (plaintext)", plugins.SensitivityMay, false, plugins.SensitivityNever, ""},
		{"may + claim=true → always (encrypt)", plugins.SensitivityMay, true, plugins.SensitivityAlways, ""},
		{"always + claim=false → INV-PLUGIN-30 reject", plugins.SensitivityAlways, false, "", "EVENT_SENSITIVITY_REQUIRED"},
		{"always + claim=true → always", plugins.SensitivityAlways, true, plugins.SensitivityAlways, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := plugins.EnforceSensitivity(tt.manifest, tt.claimed)
			if tt.wantErr != "" {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnforceSensitivityRejectsUnknownManifestValue(t *testing.T) {
	_, err := plugins.EnforceSensitivity(plugins.Sensitivity("garbage"), false)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SENSITIVITY_INVALID")
}

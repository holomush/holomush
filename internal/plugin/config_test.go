// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestValidateConfigSchema(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]ConfigParam
		wantErr string // exact oops code (errutil.AssertErrorCode); "" = no error
	}{
		{"valid duration with default", map[string]ConfigParam{"w": {Type: "duration", Default: "30m"}}, ""},
		{"valid int", map[string]ConfigParam{"n": {Type: "int", Default: "3"}}, ""},
		{"valid bool with default", map[string]ConfigParam{"b": {Type: "bool", Default: "true"}}, ""},
		{"valid string with default", map[string]ConfigParam{"s": {Type: "string", Default: "anything"}}, ""},
		{"string type with no default", map[string]ConfigParam{"s": {Type: "string"}}, ""},
		{"unknown type", map[string]ConfigParam{"x": {Type: "float"}}, "PLUGIN_CONFIG_SCHEMA_INVALID"},
		{"bad default for type", map[string]ConfigParam{"w": {Type: "duration", Default: "banana"}}, "PLUGIN_CONFIG_SCHEMA_INVALID"},
		{"bad bool default", map[string]ConfigParam{"b": {Type: "bool", Default: "banana"}}, "PLUGIN_CONFIG_SCHEMA_INVALID"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateConfigSchema(tc.cfg)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			errutil.AssertErrorCode(t, err, tc.wantErr)
		})
	}
}

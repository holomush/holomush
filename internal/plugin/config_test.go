// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestMergePluginConfig(t *testing.T) {
	schema := map[string]ConfigParam{
		"vote_window":    {Type: "duration", Default: "168h", Required: true},
		"cooloff_window": {Type: "duration", Default: "30m"},
		"needs_override": {Type: "int", Required: true}, // no default
		"optional_note":  {Type: "string"},              // optional, no default
	}
	t.Run("override wins per key and manifest defaults fill the rest (INV-PLUGIN-2)", func(t *testing.T) {
		got, err := MergePluginConfig(schema, map[string]string{"cooloff_window": "5s", "needs_override": "1"})
		require.NoError(t, err)
		require.Equal(t, map[string]string{"vote_window": "168h", "cooloff_window": "5s", "needs_override": "1"}, got)
	})
	t.Run("omits an optional key with no default and no override", func(t *testing.T) {
		got, err := MergePluginConfig(schema, map[string]string{"needs_override": "1"})
		require.NoError(t, err)
		require.NotContains(t, got, "optional_note")
	})
	t.Run("rejects a required key with no default and no override (INV-PLUGIN-4)", func(t *testing.T) {
		_, err := MergePluginConfig(schema, map[string]string{})
		errutil.AssertErrorCode(t, err, "PLUGIN_CONFIG_MISSING_REQUIRED")
	})
	t.Run("rejects an override value that fails its declared type (INV-PLUGIN-5)", func(t *testing.T) {
		_, err := MergePluginConfig(schema, map[string]string{"vote_window": "banana", "needs_override": "1"})
		errutil.AssertErrorCode(t, err, "PLUGIN_CONFIG_TYPE_INVALID")
	})
	t.Run("rejects an override key not declared in the schema (INV-PLUGIN-6)", func(t *testing.T) {
		_, err := MergePluginConfig(schema, map[string]string{"needs_override": "1", "bogus": "x"})
		errutil.AssertErrorCode(t, err, "PLUGIN_CONFIG_UNKNOWN_KEY")
	})
}

// TestMergePluginConfigProducesSingleMergedMap asserts that MergePluginConfig
// returns one deterministic merged map — the same map both binary and Lua
// delivery paths receive — and that calling it twice with the same inputs
// yields equal maps.
//
// INV-PLUGIN-3: both delivery paths (binary gRPC, Lua return-value) receive the
// same merged map from MergePluginConfig; there is no per-runtime fork of the
// config computation.
func TestMergePluginConfigProducesSingleMergedMap(t *testing.T) {
	schema := map[string]ConfigParam{
		"vote_window":    {Type: "duration", Default: "168h", Required: true},
		"cooloff_window": {Type: "duration", Default: "30m"},
	}
	override := map[string]string{"cooloff_window": "5s"}

	first, err := MergePluginConfig(schema, override)
	require.NoError(t, err)

	second, err := MergePluginConfig(schema, override)
	require.NoError(t, err)

	// Both calls must produce identical output (deterministic).
	require.Equal(t, first, second, "MergePluginConfig must be deterministic across calls")
	// Override wins for cooloff_window; default fills vote_window.
	require.Equal(t, map[string]string{"vote_window": "168h", "cooloff_window": "5s"}, first)
}

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

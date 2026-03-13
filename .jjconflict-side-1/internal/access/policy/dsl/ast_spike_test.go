// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl_test

import (
	"encoding/json"
	"testing"

	"github.com/alecthomas/participle/v2"
	"github.com/holomush/holomush/internal/access/policy/dsl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAST_JSONRoundTrip_Spike validates that parsed ASTs survive JSON serialization.
// This is the key spike validation from T0 (Phase 7.1). Test cases here are limited
// to what the current parser grammar supports — full grammar coverage comes in T9.
func TestAST_JSONRoundTrip_Spike(t *testing.T) {
	tests := []struct {
		name string
		dsl  string
	}{
		{
			"simple permit",
			`permit(principal is character, action in ["read"], resource is location);`,
		},
		{
			"multiple actions",
			`permit(principal is character, action in ["read", "write"], resource is location);`,
		},
		{
			"resource equality",
			`permit(principal is admin, action in ["delete"], resource == "system:config");`,
		},
		{
			"wildcard principal",
			`permit(principal, action, resource);`,
		},
	}

	parser := newTestParser(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parser.ParseString("", tt.dsl)
			require.NoError(t, err, "parsing %q should succeed", tt.dsl)

			jsonBytes, err := json.Marshal(ast)
			require.NoError(t, err, "marshal should succeed")

			t.Logf("JSON output: %s", string(jsonBytes))

			var roundTripped dsl.Policy
			err = json.Unmarshal(jsonBytes, &roundTripped)
			require.NoError(t, err, "unmarshal should succeed")

			roundTrippedJSON, err := json.Marshal(roundTripped)
			require.NoError(t, err, "re-marshal should succeed")

			assert.JSONEq(t, string(jsonBytes), string(roundTrippedJSON),
				"round-trip JSON should be identical")
		})
	}
}

func TestAST_JSONRoundTrip_PositionExcluded(t *testing.T) {
	parser := newTestParser(t)

	ast, err := parser.ParseString("", `permit(principal, action, resource);`)
	require.NoError(t, err)

	jsonBytes, err := json.Marshal(ast)
	require.NoError(t, err)

	var raw map[string]interface{}
	err = json.Unmarshal(jsonBytes, &raw)
	require.NoError(t, err)

	_, hasPos := raw["Pos"]
	assert.False(t, hasPos, "position should be excluded from JSON")
}

func TestAST_ParserBuilds(t *testing.T) {
	parser, err := dsl.NewParser()
	require.NoError(t, err, "parser should build without error")
	require.NotNil(t, parser, "parser should not be nil")
}

func TestAST_ParseErrors(t *testing.T) {
	parser := newTestParser(t)

	tests := []struct {
		name string
		dsl  string
	}{
		{"empty input", ""},
		{"missing semicolon", `permit(principal, action, resource)`},
		{"invalid effect", `allow(principal, action, resource);`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parser.ParseString("", tt.dsl)
			assert.Error(t, err, "parsing %q should fail", tt.dsl)
		})
	}
}

func newTestParser(t *testing.T) *participle.Parser[dsl.Policy] {
	t.Helper()
	p, err := dsl.NewParser()
	require.NoError(t, err)
	return p
}

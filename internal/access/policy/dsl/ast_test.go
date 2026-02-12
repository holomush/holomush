// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrammarVersion(t *testing.T) {
	assert.Equal(t, 1, GrammarVersion)
}

func TestCompilePolicy(t *testing.T) {
	policy := &Policy{
		Effect: "permit",
		Target: &Target{
			Principal: &PrincipalClause{Type: "character"},
			Action:    &ActionClause{Actions: []string{"read"}},
			Resource:  &ResourceClause{Type: "location"},
		},
	}

	result, err := CompilePolicy(policy)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the result is valid JSON
	var parsed map[string]any
	err = json.Unmarshal(result, &parsed)
	require.NoError(t, err)

	// Verify grammar_version is present and is a number
	gv, ok := parsed["grammar_version"]
	assert.True(t, ok, "grammar_version should be present")
	assert.Equal(t, float64(GrammarVersion), gv, "grammar_version should match constant")

	// Verify policy fields are present
	assert.Equal(t, "permit", parsed["effect"])
	target, ok := parsed["target"].(map[string]any)
	require.True(t, ok, "target should be an object")
	assert.NotNil(t, target["principal"])
}

func TestWrapAST(t *testing.T) {
	t.Run("adds grammar_version to empty map", func(t *testing.T) {
		result := WrapAST(nil)
		assert.Equal(t, 1, result["grammar_version"])
		assert.Len(t, result, 1)
	})

	t.Run("adds grammar_version to existing map", func(t *testing.T) {
		input := map[string]any{"type": "policy", "effect": "permit"}
		result := WrapAST(input)
		assert.Equal(t, 1, result["grammar_version"])
		assert.Equal(t, "policy", result["type"])
		assert.Equal(t, "permit", result["effect"])
		assert.Len(t, result, 3)
	})

	t.Run("does not mutate original map", func(t *testing.T) {
		input := map[string]any{"type": "policy"}
		result := WrapAST(input)
		_, hasVersion := input["grammar_version"]
		assert.False(t, hasVersion, "original map should not have grammar_version")
		assert.Equal(t, 1, result["grammar_version"])
	})
}

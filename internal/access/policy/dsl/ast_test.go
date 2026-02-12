// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGrammarVersion(t *testing.T) {
	assert.Equal(t, 1, GrammarVersion)
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

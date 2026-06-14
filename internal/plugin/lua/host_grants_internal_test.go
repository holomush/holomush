// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGrantedSubsetReturnsOnlyGrantedTokens is a direct unit test of the
// grantedSubset helper (host.go). It is NON-VACUOUS: if the filter were
// removed and grantedSubset simply returned requested unchanged, the
// assertion that "world.mutation" is absent would fail.
func TestGrantedSubsetReturnsOnlyGrantedTokens(t *testing.T) {
	t.Run("excludes ungranted token", func(t *testing.T) {
		got := grantedSubset(
			[]string{"world.query", "world.mutation"},
			[]string{"world.query"},
		)
		assert.ElementsMatch(t, []string{"world.query"}, got,
			"only the granted token must appear; world.mutation must be excluded")
	})

	t.Run("nil granted returns nil", func(t *testing.T) {
		got := grantedSubset([]string{"world.query"}, nil)
		assert.Empty(t, got, "nil granted must yield no caps (fail-closed)")
	})

	t.Run("empty granted returns nil", func(t *testing.T) {
		got := grantedSubset([]string{"world.query"}, []string{})
		assert.Empty(t, got, "empty granted must yield no caps (fail-closed)")
	})

	t.Run("empty requested returns empty", func(t *testing.T) {
		got := grantedSubset([]string{}, []string{"world.query"})
		assert.Empty(t, got, "no requested tokens => nothing to return")
	})

	t.Run("granted token not in requested is not added", func(t *testing.T) {
		got := grantedSubset(
			[]string{"world.query"},
			[]string{"world.query", "world.mutation"},
		)
		// granted has extra token not requested — only requested tokens that
		// are also granted appear; extra grants do not expand the result.
		assert.ElementsMatch(t, []string{"world.query"}, got,
			"extra granted token absent from requested must not be injected")
	})
}

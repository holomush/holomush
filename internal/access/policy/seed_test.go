// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedPolicies_Count(t *testing.T) {
	seeds := SeedPolicies()
	// 17 permit + 1 forbid = 18 total
	assert.Len(t, seeds, 18, "expected 18 seed policies (17 permit, 1 forbid)")
}

func TestSeedPolicies_AllNamesHaveSeedPrefix(t *testing.T) {
	for _, s := range SeedPolicies() {
		assert.True(t, strings.HasPrefix(s.Name, "seed:"),
			"seed policy %q must start with seed: prefix", s.Name)
	}
}

func TestSeedPolicies_AllHaveSeedVersion1(t *testing.T) {
	for _, s := range SeedPolicies() {
		assert.Equal(t, 1, s.SeedVersion,
			"seed policy %q must have SeedVersion 1", s.Name)
	}
}

func TestSeedPolicies_NoDuplicateNames(t *testing.T) {
	seen := make(map[string]bool)
	for _, s := range SeedPolicies() {
		assert.False(t, seen[s.Name],
			"duplicate seed policy name: %q", s.Name)
		seen[s.Name] = true
	}
}

func TestSeedPolicies_AllHaveDescriptions(t *testing.T) {
	for _, s := range SeedPolicies() {
		assert.NotEmpty(t, s.Description,
			"seed policy %q must have a description", s.Name)
	}
}

func TestSeedPolicies_AllCompileWithoutError(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	for _, s := range SeedPolicies() {
		t.Run(s.Name, func(t *testing.T) {
			compiled, _, err := compiler.Compile(s.DSLText)
			require.NoError(t, err, "seed policy %q failed to compile", s.Name)
			assert.NotNil(t, compiled)
		})
	}
}

func TestSeedPolicies_EffectDistribution(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	var permitCount, forbidCount int
	for _, s := range SeedPolicies() {
		compiled, _, err := compiler.Compile(s.DSLText)
		require.NoError(t, err)
		switch compiled.Effect {
		case "permit":
			permitCount++
		case "forbid":
			forbidCount++
		}
	}
	assert.Equal(t, 17, permitCount, "expected 17 permit policies")
	assert.Equal(t, 1, forbidCount, "expected 1 forbid policy")
}

func TestSeedPolicies_ExpectedNames(t *testing.T) {
	expectedNames := []string{
		"seed:player-self-access",
		"seed:player-location-read",
		"seed:player-character-colocation",
		"seed:player-object-colocation",
		"seed:player-stream-emit",
		"seed:player-movement",
		"seed:player-exit-use",
		"seed:player-basic-commands",
		"seed:builder-location-write",
		"seed:builder-object-write",
		"seed:builder-commands",
		"seed:admin-full-access",
		"seed:property-public-read",
		"seed:property-private-read",
		"seed:property-admin-read",
		"seed:property-owner-write",
		"seed:property-restricted-visible-to",
		"seed:property-restricted-excluded",
	}

	seeds := SeedPolicies()
	seedNames := make([]string, len(seeds))
	for i, s := range seeds {
		seedNames[i] = s.Name
	}
	assert.ElementsMatch(t, expectedNames, seedNames)
}

func TestSeedPolicies_ForbidPolicyIsPropertyRestrictedExcluded(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	for _, s := range SeedPolicies() {
		compiled, _, err := compiler.Compile(s.DSLText)
		require.NoError(t, err)
		if compiled.Effect == "forbid" {
			assert.Equal(t, "seed:property-restricted-excluded", s.Name,
				"only seed:property-restricted-excluded should be a forbid policy")
		}
	}
}

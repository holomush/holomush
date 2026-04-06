// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedPoliciesCount(t *testing.T) {
	seeds := SeedPolicies()
	// 33 permit + 5 forbid = 38 total (18 base + 5 gap-fill from T22b + 2 phase-2 commands + 2 system bootstrap + 11 channel policies)
	assert.Len(t, seeds, 38, "expected 38 seed policies (33 permit, 5 forbid)")
}

func TestSeedPoliciesAllNamesHaveSeedPrefix(t *testing.T) {
	for _, s := range SeedPolicies() {
		assert.True(t, strings.HasPrefix(s.Name, "seed:"),
			"seed policy %q must start with seed: prefix", s.Name)
	}
}

func TestSeedPoliciesAllHavePositiveSeedVersion(t *testing.T) {
	for _, s := range SeedPolicies() {
		assert.GreaterOrEqual(t, s.SeedVersion, 1,
			"seed policy %q must have SeedVersion >= 1", s.Name)
	}
}

func TestSeedPoliciesNoDuplicateNames(t *testing.T) {
	seen := make(map[string]bool)
	for _, s := range SeedPolicies() {
		assert.False(t, seen[s.Name],
			"duplicate seed policy name: %q", s.Name)
		seen[s.Name] = true
	}
}

func TestSeedPoliciesAllHaveDescriptions(t *testing.T) {
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

func TestSeedPoliciesEffectDistribution(t *testing.T) {
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
	assert.Equal(t, 33, permitCount, "expected 33 permit policies")
	assert.Equal(t, 5, forbidCount, "expected 5 forbid policies")
}

func TestSeedPoliciesExpectedNames(t *testing.T) {
	expectedNames := []string{
		// Base policies (T22)
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
		// Gap-fill policies (T22b)
		"seed:player-exit-read",                // G1
		"seed:builder-exit-write",              // G2
		"seed:player-location-list-characters", // G3
		"seed:player-scene-participant",        // G4
		"seed:player-scene-read",               // G4
		// Phase-2 command policies
		"seed:player-teleport",   // all players can execute home and teleport
		"seed:pemit-storyteller", // storyteller/admin can execute pemit
		// System bootstrap policies
		"seed:system-bootstrap-world",
		"seed:system-bootstrap-exits",
		// Channel policies
		"seed:channel-list",
		"seed:channel-join-public",
		"seed:channel-member-actions",
		"seed:channel-admin-create",
		"seed:channel-admin-delete",
		"seed:channel-admin-moderate",
		"seed:channel-guest-seeded-only",
		"seed:channel-forbid-banned",
		"seed:channel-forbid-muted",
		"seed:channel-forbid-archived",
		"seed:channel-guest-forbid-create",
	}

	seeds := SeedPolicies()
	seedNames := make([]string, len(seeds))
	for i, s := range seeds {
		seedNames[i] = s.Name
	}
	assert.ElementsMatch(t, expectedNames, seedNames)
}

func TestSeedPoliciesForbidPoliciesAreExpected(t *testing.T) {
	expectedForbids := map[string]bool{
		"seed:property-restricted-excluded": true,
		"seed:channel-forbid-banned":        true,
		"seed:channel-forbid-muted":         true,
		"seed:channel-forbid-archived":      true,
		"seed:channel-guest-forbid-create":  true,
	}
	compiler := NewCompiler(emptySchema())
	for _, s := range SeedPolicies() {
		compiled, _, err := compiler.Compile(s.DSLText)
		require.NoError(t, err)
		if compiled.Effect == "forbid" {
			assert.True(t, expectedForbids[s.Name],
				"unexpected forbid policy: %q", s.Name)
		}
	}
}

// T22b gap-specific tests

func TestSeedPoliciesPlayerExitReadPolicyExists(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:player-exit-read" {
			found = true
			compiler := NewCompiler(emptySchema())
			compiled, _, err := compiler.Compile(s.DSLText)
			require.NoError(t, err)
			assert.Equal(t, "permit", string(compiled.Effect))
			assert.Contains(t, compiled.Target.ActionList, "read")
			rType := "exit"
			assert.Equal(t, &rType, compiled.Target.ResourceType)
		}
	}
	assert.True(t, found, "seed:player-exit-read policy must exist (G1)")
}

func TestSeedPoliciesBuilderExitWritePolicyExists(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:builder-exit-write" {
			found = true
			compiler := NewCompiler(emptySchema())
			compiled, _, err := compiler.Compile(s.DSLText)
			require.NoError(t, err)
			assert.Equal(t, "permit", string(compiled.Effect))
			assert.Contains(t, compiled.Target.ActionList, "write")
			assert.Contains(t, compiled.Target.ActionList, "delete")
			rType := "exit"
			assert.Equal(t, &rType, compiled.Target.ResourceType)
		}
	}
	assert.True(t, found, "seed:builder-exit-write policy must exist (G2)")
}

func TestSeedPoliciesPlayerLocationListCharactersPolicyExists(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:player-location-list-characters" {
			found = true
			compiler := NewCompiler(emptySchema())
			compiled, _, err := compiler.Compile(s.DSLText)
			require.NoError(t, err)
			assert.Equal(t, "permit", string(compiled.Effect))
			assert.Contains(t, compiled.Target.ActionList, "list_characters")
			rType := "location"
			assert.Equal(t, &rType, compiled.Target.ResourceType)
		}
	}
	assert.True(t, found, "seed:player-location-list-characters policy must exist (G3)")
}

func TestSeedPoliciesScenePoliciesExist(t *testing.T) {
	seeds := SeedPolicies()
	var participantFound, readFound bool
	compiler := NewCompiler(emptySchema())
	for _, s := range seeds {
		switch s.Name {
		case "seed:player-scene-participant":
			participantFound = true
			compiled, _, err := compiler.Compile(s.DSLText)
			require.NoError(t, err)
			assert.Equal(t, "permit", string(compiled.Effect))
			assert.Contains(t, compiled.Target.ActionList, "write")
			rType := "scene"
			assert.Equal(t, &rType, compiled.Target.ResourceType)
		case "seed:player-scene-read":
			readFound = true
			compiled, _, err := compiler.Compile(s.DSLText)
			require.NoError(t, err)
			assert.Equal(t, "permit", string(compiled.Effect))
			assert.Contains(t, compiled.Target.ActionList, "read")
			rType := "scene"
			assert.Equal(t, &rType, compiled.Target.ResourceType)
		}
	}
	assert.True(t, participantFound, "seed:player-scene-participant policy must exist (G4)")
	assert.True(t, readFound, "seed:player-scene-read policy must exist (G4)")
}

// Phase-2 command policy tests

func TestSeedPoliciesPlayerTeleportPolicyExists(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:player-teleport" {
			found = true
			compiler := NewCompiler(emptySchema())
			compiled, _, err := compiler.Compile(s.DSLText)
			require.NoError(t, err)
			assert.Equal(t, "permit", string(compiled.Effect),
				"seed:player-teleport must be a permit policy")
			assert.Contains(t, compiled.Target.ActionList, "execute",
				"seed:player-teleport must include execute action")
			rType := "command"
			assert.Equal(t, &rType, compiled.Target.ResourceType,
				"seed:player-teleport must target command resources")
		}
	}
	assert.True(t, found, "seed:player-teleport policy must exist")
}

func TestSeedPoliciesPemitStorytellerPolicyExists(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:pemit-storyteller" {
			found = true
			compiler := NewCompiler(emptySchema())
			compiled, _, err := compiler.Compile(s.DSLText)
			require.NoError(t, err)
			assert.Equal(t, "permit", string(compiled.Effect),
				"seed:pemit-storyteller must be a permit policy")
			assert.Contains(t, compiled.Target.ActionList, "execute",
				"seed:pemit-storyteller must include execute action")
			rType := "command"
			assert.Equal(t, &rType, compiled.Target.ResourceType,
				"seed:pemit-storyteller must target command resources")
		}
	}
	assert.True(t, found, "seed:pemit-storyteller policy must exist")
}

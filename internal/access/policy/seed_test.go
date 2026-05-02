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
	// 25 permit + 3 forbid = 28 total (18 base − 2 removed command policies + 5 gap-fill from T22b + 1 phase-2 command + 2 system bootstrap + 1 location-stream read + 2 phase-3b audit deny)
	assert.Len(t, seeds, 28, "expected 28 seed policies (25 permit, 3 forbid)")
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
	assert.Equal(t, 25, permitCount, "expected 25 permit policies")
	assert.Equal(t, 3, forbidCount, "expected 3 forbid policies")
}

func TestSeedPoliciesExpectedNames(t *testing.T) {
	expectedNames := []string{
		// Base policies (T22)
		"seed:player-self-access",
		"seed:player-location-read",
		"seed:player-character-colocation",
		"seed:player-object-colocation",
		"seed:player-stream-emit",
		"seed:player-location-stream-read",
		"seed:player-movement",
		"seed:player-exit-use",
		"seed:player-basic-commands",
		"seed:builder-location-write",
		"seed:builder-object-write",
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
		"seed:player-teleport", // all players can execute home and teleport
		// System bootstrap policies
		"seed:system-bootstrap-world",
		"seed:system-bootstrap-exits",
		// Phase-3b audit deny policies
		"seed:deny-audit-read-character",
		"seed:deny-audit-read-plugin",
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
		"seed:deny-audit-read-character":    true,
		"seed:deny-audit-read-plugin":       true,
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

// Phase-3b audit deny policy tests

func TestSeedPoliciesIncludesAuditSubscribeDenyForCharacter(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:deny-audit-read-character" {
			found = true
			assert.Contains(t, s.DSLText, "forbid")
			assert.Contains(t, s.DSLText, "audit.")
			assert.Contains(t, s.DSLText, "principal is character")
			break
		}
	}
	assert.True(t, found, "audit.> deny seed policy for character MUST be present")
}

func TestSeedPoliciesIncludesAuditSubscribeDenyForPlugin(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:deny-audit-read-plugin" {
			found = true
			assert.Contains(t, s.DSLText, "forbid")
			assert.Contains(t, s.DSLText, "audit.")
			assert.Contains(t, s.DSLText, "principal is plugin")
			break
		}
	}
	assert.True(t, found, "audit.> deny seed policy for plugin MUST be present")
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

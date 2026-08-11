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
	// 49 seed policies total: 40 permit + 9 forbid. TestSeedPoliciesExpectedNames
	// below is the authoritative per-name inventory; this count is the coarse
	// guard against an accidental add/remove. holomush-8m01u removed the vestigial
	// unconditional seed:player-scene-participant write permit (50 → 49), and
	// holomush-sjtlz removed its read twin seed:player-scene-read (49 → 48);
	// scene reads/writes are now gated solely by the core-scenes plugin's
	// read-scene-as-* / write-scene-as-participant policies. Phase-1 channels
	// added seed:plugin-stream-subscribe (48 → 49) — the instance-level write
	// analogue of seed:plugin-stream-read (HIGH-3). v0.13 phase 2 plan 02-07
	// added the two viewer-tier floors and profile reachability (49 → 52), then
	// the five viewer read twins and the PROFILE-11 property widening (52 → 58)
	// — one of the five twins is a forbid (9 → 10) — and seed:admin-section-access
	// (58 → 59). v0.13 phase 02.2 plan 01 added the background-job fixture grant
	// seed:job-fixture-instance-scoped (59 → 60). v0.13 phase 03 plan 04 added
	// the first REAL job grant, seed:job-retirement-instance-scoped (60 → 61).
	// v0.13 phase 04 plan 01 discharged D-29's deferral with the two narrow
	// read_description permits, seed:character-description-read and
	// seed:viewer-character-description-read (61 → 63). v0.13 phase 04 plan 07
	// added the viewer twin of the shipped character-directory permit,
	// seed:viewer-directory-list-characters — 01-SPEC §9.2's tier floor on the
	// directory resource (63 → 64).
	assert.Len(t, seeds, 64, "expected 64 seed policies (54 permit, 10 forbid)")
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
	assert.Equal(t, 54, permitCount, "expected 54 permit policies (+1 v0.13 phase-04 §9.2 directory tier floor seed:viewer-directory-list-characters, +2 v0.13 phase-04 read_description permits discharging D-29: seed:character-description-read and seed:viewer-character-description-read,+11 holomush-kplrr plugin host-capability default-permit seeds, +1 holomush-xakba plugin instance-level stream read, +1 phase-1 channels plugin instance-level stream write HIGH-3, +1 character-directory INV-ACCESS-9, +3 v0.13 phase-2 profile visibility: two viewer-tier floors and profile reachability, +4 viewer read twins, +1 PROFILE-11 property widening, +1 EXT-07 admin-section access, +1 v0.13 phase-02.2 background-job fixture grant AUTHZ-02, +1 v0.13 phase-03 retirement-reactor job grant IDENT-04, −1 holomush-8m01u removed vestigial seed:player-scene-participant, −1 holomush-sjtlz removed vestigial seed:player-scene-read)")
	assert.Equal(t, 10, forbidCount, "expected 10 forbid policies (+2 phase-5 sub-epic A events.*.system.crypto_totp.* denies + 2 phase-5 sub-epic D events.*.system.crypto_policy.* denies + 2 phase-5 sub-epic E events.*.system.* broad denies + 1 v0.13 phase-2 seed:viewer-property-restricted-excluded)")
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
		// G4 seed:player-scene-participant removed (holomush-8m01u) and
		// seed:player-scene-read removed (holomush-sjtlz): both were
		// unconditional permit(character, <action>, scene) stubs that subsumed
		// the plugin's membership-conditioned gates. The plugin policies
		// (plugins/core-scenes/plugin.yaml read-scene-as-* /
		// write-scene-as-participant) are now the sole scene gates.
		"seed:player-location-list-presence", // G5
		// Phase-2 command policies
		"seed:player-teleport", // all players can execute home and teleport
		// System bootstrap policies
		"seed:system-bootstrap-world",
		"seed:system-bootstrap-exits",
		// Phase-3b audit deny policies
		"seed:deny-audit-read-character",
		"seed:deny-audit-read-plugin",
		// Phase-5 sub-epic A TOTP-substrate audit deny policies (INV-ACCESS-8)
		"seed:deny-events-system-crypto-totp-read-character",
		"seed:deny-events-system-crypto-totp-read-plugin",
		// Phase-5 sub-epic D crypto-policy audit deny policies
		"seed:deny-events-system-crypto-policy-read-character",
		"seed:deny-events-system-crypto-policy-read-plugin",
		// Phase-5 sub-epic E broad events.*.system.* deny policies (A16 future-proof + rekey namespace)
		"seed:deny-events-system-read-character",
		"seed:deny-events-system-read-plugin",
		// Phase-5 iwzt history-scope-privacy staff override policy (INV-PRIVACY-6)
		"seed:staff-read-unrestricted-history",
		// Plugin host-capability scope policy (eykuh.3; INV-PLUGIN-50)
		"seed:plugin-world-mutation-own-location",
		// Plugin host-capability default-permit seeds (holomush-kplrr; INV-PLUGIN-50)
		"seed:plugin-cap-eval",
		"seed:plugin-cap-settings",
		"seed:plugin-cap-kv",
		"seed:plugin-cap-world-location",
		"seed:plugin-cap-world-query-character",
		"seed:plugin-cap-world-query-object",
		"seed:plugin-cap-property",
		"seed:plugin-cap-session",
		"seed:plugin-cap-focus",
		"seed:plugin-cap-stream",
		"seed:plugin-cap-audit",
		"seed:plugin-stream-read",
		// Plugin instance-level stream write (phase-1 channels; HIGH-3)
		"seed:plugin-stream-subscribe",
		// Background-job fixture (v0.13 phase 02.2, AUTHZ-02). A FIXTURE grant:
		// no job named `fixture` is ever registered in production, so the
		// liveness gate keeps it unmatchable. The real consumers
		// (job:retirement, job:activity-flush) get their own grants in Phase 3.
		"seed:job-fixture-instance-scoped",
		// The retirement reactor's REAL job grant (v0.13 phase 03, IDENT-04).
		// Same instance fence as the fixture; read is included because the
		// reactor's status guard crosses the chokepoint too.
		"seed:job-retirement-instance-scoped",
		// Character directory (INV-ACCESS-9)
		"seed:directory-list-characters",
		"seed:viewer-directory-list-characters",
		// Profile visibility: viewer-tier floors (01-SPEC §8.2.1, §8.6; D-03).
		// TWO, not three: §8.6 seeds no name at the `player` rung and the DSL's
		// list grammar forbids an empty `in []`. The conditional re-entry guard
		// lives in seed_profile_visibility_test.go.
		"seed:profile-tier-floor-anonymous",
		"seed:profile-tier-floor-guest",
		// Profile reachability (01-SPEC §8.4.2)
		"seed:profile-reachable",
		// Viewer-flavored row-keyed reads — term B of §8.5.1's conjunction
		// (D-01). seed:property-owner-write deliberately has NO twin.
		"seed:viewer-property-public-read",
		"seed:viewer-property-private-read",
		"seed:viewer-property-admin-read",
		"seed:viewer-property-restricted-visible-to",
		"seed:viewer-property-restricted-excluded",
		// PROFILE-11 widening (D-10, D-11). The property half.
		"seed:profile-public-read-property",
		// PROFILE-11's characters.description half, landing in v0.13 phase 04
		// (D-75, D-76). D-29 deferred the character half out of Phase 2 because
		// the shape proposed there was `action in ["read"]`, which gates
		// world.Service.GetCharacter and hands over the whole CharacterInfo
		// projection. These two carry a DISTINCT NARROW ACTION instead, reaching
		// only world.Service.GetCharacterDescription, whose return type has no
		// field for a player id or a location id. The deferred name
		// (seed:profile-public-read-character) is deliberately NOT what shipped.
		"seed:character-description-read",
		"seed:viewer-character-description-read",
		// Admin sections (EXT-07, 01-SPEC §10.4, §10.5)
		"seed:admin-section-access",
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
		"seed:property-restricted-excluded":                    true,
		"seed:deny-audit-read-character":                       true,
		"seed:deny-audit-read-plugin":                          true,
		"seed:deny-events-system-crypto-totp-read-character":   true,
		"seed:deny-events-system-crypto-totp-read-plugin":      true,
		"seed:deny-events-system-crypto-policy-read-character": true,
		"seed:deny-events-system-crypto-policy-read-plugin":    true,
		"seed:deny-events-system-read-character":               true,
		"seed:deny-events-system-read-plugin":                  true,
		// v0.13 phase 2 (D-01): the viewer twin of
		// seed:property-restricted-excluded, the family's one forbid.
		"seed:viewer-property-restricted-excluded": true,
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

func TestSeedPoliciesVestigialSceneSeedsStayRemoved(t *testing.T) {
	seeds := SeedPolicies()
	var participantFound, readFound bool
	for _, s := range seeds {
		switch s.Name {
		case "seed:player-scene-participant":
			participantFound = true
		case "seed:player-scene-read":
			readFound = true
		}
	}
	// Regression guard (holomush-8m01u): the write seed MUST NOT be re-added.
	// It was an unconditional permit(character, write, scene) that subsumed the
	// plugin's participant-conditioned write-scene-as-participant gate in the
	// OR-of-permits engine, letting a non-participant emit IC/OOC into any
	// scene. Scene-write authorization now lives solely in the plugin policy
	// (plugins/core-scenes/plugin.yaml write-scene-as-participant), evaluated
	// against the plugin's SceneResolver participants attribute.
	assert.False(t, participantFound,
		"seed:player-scene-participant MUST NOT exist — removed in holomush-8m01u; "+
			"scene writes are gated by the plugin's write-scene-as-participant policy")
	// Regression guard (holomush-sjtlz): the read twin MUST NOT be re-added
	// either. Same vestigial pattern — an unconditional permit(character, read,
	// scene) that subsumed the plugin's membership-conditioned read policies,
	// letting any character `scene info` any scene's metadata. Scene-read
	// authorization now lives solely in the plugin policies
	// (read-scene-as-participant / read-scene-as-invitee / read-open-scene).
	assert.False(t, readFound,
		"seed:player-scene-read MUST NOT exist — removed in holomush-sjtlz; "+
			"scene reads are gated by the plugin's read-scene-as-* policies")
}

// Phase-3b audit deny policy tests
//
// INV-ACCESS-7 (post-Phase-3d Decision 4 reword): ABAC denies subscribe to
// audit.* streams for kind={plugin|character}. Per master spec §7.7
// (amended via Phase 3d grounding doc Appendix A), ABAC at the gRPC
// subscribe handler boundary is the authoritative isolation gate. The
// `action in ["read"]` clause covers subscribe — subscribe is logically
// a read against the stream resource. NATS-level deny rules do not
// apply (game-topic NATS is single-principal by architectural design).
//
// The two test cases below verify both seed policies exist with the
// correct DSL — they are the Phase-3d-touchable coverage of INV-ACCESS-7.
//
// Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 3 + Decision 4)

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

// Phase-5 sub-epic A TOTP-substrate audit deny policy tests (INV-ACCESS-8)
//
// INV-ACCESS-8: ABAC denies subscribe to events.*.system.crypto_totp.* streams
// for kind={plugin|character}. Sub-epic D emits these events; sub-epic A
// reserves the namespace via these seeds.

func TestSeedPoliciesIncludesEventsSystemCryptoTotpDenyForCharacter(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:deny-events-system-crypto-totp-read-character" {
			found = true
			assert.Contains(t, s.DSLText, "forbid")
			assert.Contains(t, s.DSLText, "events.*.system.crypto_totp.*")
			assert.Contains(t, s.DSLText, "principal is character")
			break
		}
	}
	assert.True(t, found, "events.*.system.crypto_totp.* deny seed for character MUST be present (INV-ACCESS-8)")
}

func TestSeedPoliciesIncludesEventsSystemCryptoTotpDenyForPlugin(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:deny-events-system-crypto-totp-read-plugin" {
			found = true
			assert.Contains(t, s.DSLText, "forbid")
			assert.Contains(t, s.DSLText, "events.*.system.crypto_totp.*")
			assert.Contains(t, s.DSLText, "principal is plugin")
			break
		}
	}
	assert.True(t, found, "events.*.system.crypto_totp.* deny seed for plugin MUST be present (INV-ACCESS-8)")
}

// Phase-5 sub-epic D crypto-policy audit deny policy tests
//
// Mirrors INV-ACCESS-8 for the events.*.system.crypto_policy.* namespace.
// Sub-epic D emits crypto.policy_set audit events on this subject; these
// seeds are the ABAC-layer gate parallel to the dispatchDelivery
// AUDIT_ONLY filter at internal/grpc/server.go (~line 1019).

func TestSeedPoliciesIncludesEventsSystemCryptoPolicyDenyForCharacter(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:deny-events-system-crypto-policy-read-character" {
			found = true
			assert.Contains(t, s.DSLText, "forbid")
			assert.Contains(t, s.DSLText, "events.*.system.crypto_policy.*")
			assert.Contains(t, s.DSLText, "principal is character")
			break
		}
	}
	assert.True(t, found, "events.*.system.crypto_policy.* deny seed for character MUST be present (Phase 5 sub-epic D)")
}

func TestSeedPoliciesIncludesEventsSystemCryptoPolicyDenyForPlugin(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:deny-events-system-crypto-policy-read-plugin" {
			found = true
			assert.Contains(t, s.DSLText, "forbid")
			assert.Contains(t, s.DSLText, "events.*.system.crypto_policy.*")
			assert.Contains(t, s.DSLText, "principal is plugin")
			break
		}
	}
	assert.True(t, found, "events.*.system.crypto_policy.* deny seed for plugin MUST be present (Phase 5 sub-epic D)")
}

// Phase-5 sub-epic E broad events.*.system.* deny policy tests (A16 / INV-ACCESS-7 extension)
//
// A16 extended INV-ACCESS-7 to cover all events.*.system.* namespaces, explicitly
// including the rekey audit chain (events.<gameID>.system.rekey.<ct>.<cid>).
// The narrow per-namespace seeds (crypto_totp, crypto_policy) are subsumed by
// these broad seeds, which future-proof against subsequent audit chains.
// Refs: master spec amendment A16, §4.6, §7.7.

func TestSeedPoliciesIncludesEventsSystemRekeyDenyForCharacter(t *testing.T) {
	// Verifies the broad seed covers the rekey namespace (events.*.system.*)
	// which subsumes events.*.system.rekey.*.
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:deny-events-system-read-character" {
			found = true
			assert.Contains(t, s.DSLText, "forbid")
			assert.Contains(t, s.DSLText, "events.*.system.*")
			assert.Contains(t, s.DSLText, "principal is character")
			break
		}
	}
	assert.True(t, found, "events.*.system.* broad deny seed for character MUST be present (A16 / INV-ACCESS-7)")
}

func TestSeedPoliciesIncludesEventsSystemRekeyDenyForPlugin(t *testing.T) {
	// Verifies the broad seed covers the rekey namespace (events.*.system.*)
	// which subsumes events.*.system.rekey.*.
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:deny-events-system-read-plugin" {
			found = true
			assert.Contains(t, s.DSLText, "forbid")
			assert.Contains(t, s.DSLText, "events.*.system.*")
			assert.Contains(t, s.DSLText, "principal is plugin")
			break
		}
	}
	assert.True(t, found, "events.*.system.* broad deny seed for plugin MUST be present (A16 / INV-ACCESS-7)")
}

// Phase-5 iwzt history-scope-privacy policy tests

func TestSeed_IncludesStaffUnrestrictedHistoryPolicy(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:staff-read-unrestricted-history" {
			found = true
			break
		}
	}
	require.True(t, found, "Phase 5 must seed seed:staff-read-unrestricted-history policy")
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

// Character directory policy tests (INV-ACCESS-9)

func TestSeedPoliciesDirectoryListCharactersPolicyExists(t *testing.T) {
	seeds := SeedPolicies()
	var found bool
	for _, s := range seeds {
		if s.Name == "seed:directory-list-characters" {
			found = true
			compiler := NewCompiler(emptySchema())
			compiled, _, err := compiler.Compile(s.DSLText)
			require.NoError(t, err)
			assert.Equal(t, "permit", string(compiled.Effect),
				"seed:directory-list-characters must be a permit policy")
			assert.Contains(t, compiled.Target.ActionList, "list_character_directory",
				"seed:directory-list-characters must include list_character_directory action")
			rType := "character_directory"
			assert.Equal(t, &rType, compiled.Target.ResourceType,
				"seed:directory-list-characters must target character_directory resources")
		}
	}
	assert.True(t, found, "seed:directory-list-characters policy must exist (INV-ACCESS-9)")
}

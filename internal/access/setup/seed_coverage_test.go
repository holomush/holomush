// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/access/policy"
)

// TestValidateSeedProviderCoverage_AllRegistered pins the green path: when
// every namespace referenced by the DSL has a registered provider, the
// missing-set is empty.
func TestValidateSeedProviderCoverage_AllRegistered(t *testing.T) {
	t.Parallel()
	seeds := []policy.SeedPolicy{
		{
			Name:    "test:location-read",
			DSLText: `permit(principal is character, action in ["read"], resource is location) when { resource.location.id == principal.character.location };`,
		},
	}
	registered := []string{"character", "location"}

	missing := validateSeedProviderCoverage(registered, seeds)
	assert.Empty(t, missing,
		"all referenced namespaces are registered; missing set MUST be empty")
}

// TestValidateSeedProviderCoverage_MissingNamespace is the holomush-g776
// regression case: a seed gates on resource.location.id but LocationProvider
// is not registered. The validator MUST surface this gap with the affected
// seed names — without this surfacing, the bug stays silent at startup and
// only manifests via e2e symptoms (as g776 did for 6+ weeks).
func TestValidateSeedProviderCoverage_MissingNamespace(t *testing.T) {
	t.Parallel()
	seeds := []policy.SeedPolicy{
		{
			Name:    "seed:player-location-read",
			DSLText: `permit(principal is character, action in ["read"], resource is location) when { resource.location.id == principal.character.location };`,
		},
		{
			Name:    "seed:player-location-list-presence",
			DSLText: `permit(principal is character, action in ["list_presence"], resource is location) when { resource.location.id == principal.character.location };`,
		},
		{
			Name:    "seed:unaffected",
			DSLText: `permit(principal is character, action in ["read"], resource is character) when { resource.character.id == principal.character.id };`,
		},
	}
	// CharacterProvider is registered, LocationProvider is NOT (the g776 bug).
	registered := []string{"character"}

	missing := validateSeedProviderCoverage(registered, seeds)
	assert.Len(t, missing, 1,
		"only the location namespace is missing; character is registered")
	assert.ElementsMatch(t, []string{
		"seed:player-location-read",
		"seed:player-location-list-presence",
	}, missing["location"],
		"both affected seeds MUST appear in the missing-namespace's seed list")
}

// TestValidateSeedProviderCoverage_PrincipalReferenceCounts asserts that
// principal.<ns>.<attr> references count for coverage the same as resource
// references. PlayerProvider is the canonical case: principal.player.grants
// is the only access path to the player namespace from a seed condition.
func TestValidateSeedProviderCoverage_PrincipalReferenceCounts(t *testing.T) {
	t.Parallel()
	seeds := []policy.SeedPolicy{
		{
			Name:    "test:player-grants",
			DSLText: `permit(principal is player, action in ["totp_initialize"], resource is character) when { "crypto.operator" in principal.player.grants };`,
		},
	}
	// PlayerProvider not registered — should surface even though the
	// reference is on the principal (subject) side, not resource.
	registered := []string{"character"}

	missing := validateSeedProviderCoverage(registered, seeds)
	assert.Contains(t, missing, "player",
		"missing-set MUST include namespaces referenced via principal.X.Y, not just resource.X.Y")
	assert.Equal(t, []string{"test:player-grants"}, missing["player"])
}

// TestValidateSeedProviderCoverage_TopLevelIDsNotFlagged asserts that
// `principal.id` and `resource.id` (no namespace, injected directly by the
// resolver at resolver.go:182-184, 197-199) are NOT misidentified as missing
// namespace references. The regex requires a `.<attr>` after the namespace
// segment, which `principal.id` lacks.
func TestValidateSeedProviderCoverage_TopLevelIDsNotFlagged(t *testing.T) {
	t.Parallel()
	seeds := []policy.SeedPolicy{
		{
			Name:    "test:self",
			DSLText: `permit(principal is character, action in ["read"], resource is character) when { resource.id == principal.id };`,
		},
	}
	registered := []string{"character"}

	missing := validateSeedProviderCoverage(registered, seeds)
	assert.Empty(t, missing,
		"top-level `principal.id` / `resource.id` MUST NOT be flagged as missing — they are injected by the resolver, no provider needed")
}

// AcknowledgedMissingSeedNamespaces lists production-seed namespaces that
// have NO registered provider today, each tied to a tracking bead. When a
// bead closes (provider wired or seed removed), REMOVE the entry here AND
// verify the validator's runtime WARN no longer fires at server boot.
//
// EXPORTED so an integration test exercising the REAL BuildABACStack can
// share this list with the seed-corpus regression below — the alternative
// (hardcoded mirror of "what BuildABACStack registers") suffers silent
// drift if a future refactor drops a registration. Per abac-reviewer
// finding on holomush-xxel.
var AcknowledgedMissingSeedNamespaces = map[string]string{}

// TestValidateSeedProviderCoverage_ProductionCorpusIsCovered is the
// load-bearing regression lock at the UNIT level: it verifies the validator
// CORRECTLY identifies the known-missing namespaces (property, object) for
// the production seed corpus. The companion integration test
// TestBuildABACStack_SeedCoverageMatchesAcknowledged
// (internal/access/setup/buildabacstack_seed_coverage_integ_test.go) is the
// drift-detector: it builds the REAL BuildABACStack and asserts its actual
// registered namespaces leave EXACTLY the acknowledged-missing set
// uncovered. The unit + integration pair together close the gap that a
// hardcoded mirror leaves open.
//
// If this test fails: (a) a NEW namespace appeared in seeds → add the
// provider to BuildABACStack and to (or remove from) AcknowledgedMissing
// above, or (b) you intentionally deleted a seed → remove its namespace
// from AcknowledgedMissing if no other seed needs that namespace.
func TestValidateSeedProviderCoverage_ProductionCorpusIsCovered(t *testing.T) {
	t.Parallel()
	// productionRegistered MUST stay in sync with BuildABACStack's actual
	// registrations. The integration test asserts no drift.
	productionRegistered := []string{
		"character", "location", "object", "property", "player", "command", "stream", "plugin",
	}

	missing := validateSeedProviderCoverage(productionRegistered, policy.SeedPolicies())

	for ns := range missing {
		bead, known := AcknowledgedMissingSeedNamespaces[ns]
		assert.True(t, known,
			"NEW namespace %q referenced by seeds but no provider registered. "+
				"Either add the provider to internal/access/setup/setup.go::BuildABACStack, "+
				"or file a follow-up bead and add `%q: \"holomush-<id>\"` to "+
				"AcknowledgedMissingSeedNamespaces in this file. Per holomush-xxel.",
			ns, ns)
		t.Logf("seed-coverage gap acknowledged for namespace %q: tracked by %s; seeds: %v",
			ns, bead, missing[ns])
	}
	for ns, bead := range AcknowledgedMissingSeedNamespaces {
		if _, stillMissing := missing[ns]; !stillMissing {
			t.Errorf("namespace %q is in AcknowledgedMissingSeedNamespaces (bead %s) but "+
				"is no longer flagged by the validator. If %s landed, REMOVE it from "+
				"AcknowledgedMissingSeedNamespaces.", ns, bead, bead)
		}
	}
}

// TestValidateSeedProviderCoverage_TargetOnlyMatchesNotFlagged pins the
// intentional regex semantics per abac-reviewer finding #4: seeds that
// reference a namespace ONLY via `principal is X` or `resource is X`
// target-type matchers (no `principal.X.attr` / `resource.X.attr` when-clause
// references) are NOT flagged as missing. The bug class this PR catches is
// "WHEN clause accesses a namespace attribute that no provider populates";
// target-only matchers don't access attributes, so the engine doesn't need
// the provider to evaluate them. Without this test, a well-meaning future
// edit that "broadens the regex to also catch target types" would silently
// start producing false positives for plugin / scene / exit seeds that
// intentionally use target-only matching.
func TestValidateSeedProviderCoverage_TargetOnlyMatchesNotFlagged(t *testing.T) {
	t.Parallel()
	seeds := []policy.SeedPolicy{
		{
			Name:    "test:target-only-plugin",
			DSLText: `permit(principal is plugin, action in ["emit_event"], resource is event);`,
		},
		{
			Name:    "test:target-only-scene",
			DSLText: `permit(principal is character, action in ["write"], resource is scene);`,
		},
	}
	// Neither "plugin" nor "scene" namespace registered, but the seeds
	// don't reference any plugin.X.Y / scene.X.Y in a when clause — they
	// only target-match. The validator MUST NOT flag them.
	registered := []string{"character"}

	missing := validateSeedProviderCoverage(registered, seeds)
	assert.Empty(t, missing,
		"target-only matchers (resource is X, principal is X) do NOT need a provider; "+
			"only when-clause attribute references do. Regex must not flag them.")
}

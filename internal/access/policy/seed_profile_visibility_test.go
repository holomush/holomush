// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/dsl"
)

// --- 01-SPEC §8.6, transcribed ---

// spec86SeededFloors is 01-SPEC §8.6's "Seeded v0.13 default" column, keyed by
// rung, restricted to the rows that are `entity_properties` rows.
//
// The two `characters`-column rows §8.6 also lists — name (`characters.name`)
// and the in-world description (`characters.description`) — are deliberately
// ABSENT. §7.1 puts them in columns, not rows, so they carry a tier floor but
// have no row-keyed peer decision and no `resource.property.name` to match
// (§8.6's own closing note). The *profile reachability* row is likewise absent:
// §8.4.2 gives it its own resource, action and policy.
//
// The media line is eleven names, not a pattern: §7.3 fixes them as exact
// bytes, so the set is closed and enumerable. Index 10 is NOT a member.
//
// This map is the ORACLE for the seeded tier-floor family. It is transcribed
// from the SPEC, never from `seed.go` — a test that derived its expectation
// from the artifact under test could not detect a transcription error.
//
// D-03's re-entry condition rides on it: the moment §8.6 seeds any name at the
// `player` rung, a "player" key appears here and
// TestAPlayerRungTierFloorPolicyIsRequiredExactlyWhenSpec86SeedsAName turns RED
// until seed:profile-tier-floor-player is written.
var spec86SeededFloors = map[string][]string{
	access.ViewerTierAnonymous: {
		"profile.pronouns",
	},
	access.ViewerTierGuest: {
		"profile.rumors",
		"profile.currently",
		"profile.rp_preferences",
		"profile.timezone",
		"profile.concept",
		"profile.species",
		"profile.age",
		"profile.faction",
		"profile.appearance",
		"profile.personality",
		"profile.biography",
		"profile.image.primary",
		"profile.image.gallery.00",
		"profile.image.gallery.01",
		"profile.image.gallery.02",
		"profile.image.gallery.03",
		"profile.image.gallery.04",
		"profile.image.gallery.05",
		"profile.image.gallery.06",
		"profile.image.gallery.07",
		"profile.image.gallery.08",
		"profile.image.gallery.09",
	},
}

// spec821ClearingTests is 01-SPEC §8.2.1's clearing-test table, verbatim. Each
// rung's floor is cleared by exactly the tiers listed, as explicit set
// membership — never an ordinal comparison, never a numeric rank.
var spec821ClearingTests = map[string][]string{
	access.ViewerTierAnonymous: {"anonymous", "guest", "player"},
	access.ViewerTierGuest:     {"guest", "player"},
	access.ViewerTierPlayer:    {"player"},
}

// tierFloorPolicyName returns the tier-floor policy name for a rung.
func tierFloorPolicyName(rung string) string {
	return "seed:profile-tier-floor-" + rung
}

// --- helpers ---

// seedPolicyByName returns the seed policy with the given name, and whether it
// exists.
func seedPolicyByName(name string) (SeedPolicy, bool) {
	for _, s := range SeedPolicies() {
		if s.Name == name {
			return s, true
		}
	}
	return SeedPolicy{}, false
}

// requireSeedPolicy fails the test when the named seed policy is absent.
func requireSeedPolicy(t *testing.T, name string) SeedPolicy {
	t.Helper()
	s, ok := seedPolicyByName(name)
	require.True(t, ok, "seed policy %q MUST exist", name)
	return s
}

// parseSeed parses a seed policy's DSL text into an AST.
func parseSeed(t *testing.T, s SeedPolicy) *dsl.Policy {
	t.Helper()
	parsed, err := dsl.Parse(s.DSLText)
	require.NoError(t, err, "seed policy %q MUST parse", s.Name)
	return parsed
}

// inListLiterals returns the literal string set of the `<attrPath> in [...]`
// condition in the policy's top-level conjunction, or nil when the policy
// carries no such condition.
//
// attrPath is the full dotted reference, e.g. "principal.viewer.tier".
func inListLiterals(t *testing.T, parsed *dsl.Policy, attrPath string) []string {
	t.Helper()
	if parsed.Conditions == nil {
		return nil
	}
	for _, conj := range parsed.Conditions.Disjunctions {
		for _, cond := range conj.Conditions {
			if cond.InList == nil || cond.InList.Left == nil || cond.InList.Left.AttrRef == nil {
				continue
			}
			ref := cond.InList.Left.AttrRef
			got := ref.Root + "." + strings.Join(ref.Path, ".")
			if got != attrPath {
				continue
			}
			out := make([]string, 0, len(cond.InList.List.Values))
			for _, v := range cond.InList.List.Values {
				require.NotNil(t, v.Str, "list literal in %q MUST be a string", attrPath)
				out = append(out, *v.Str)
			}
			return out
		}
	}
	return nil
}

// --- Tier floors (§8.2.1, §8.6, D-03) ---

func TestSeededTierFloorPoliciesCoverExactlyTheSpec86NamesAtEachRung(t *testing.T) {
	t.Parallel()

	for rung, want := range spec86SeededFloors {
		t.Run(rung, func(t *testing.T) {
			t.Parallel()

			s := requireSeedPolicy(t, tierFloorPolicyName(rung))
			parsed := parseSeed(t, s)

			got := inListLiterals(t, parsed, "resource.property.name")
			assert.ElementsMatch(t, want, got,
				"%s MUST enumerate exactly §8.6's seeded-%s names as whole strings — "+
					"a name in no list is denied, not defaulted (§8.6 totality rule)",
				s.Name, rung)
		})
	}
}

func TestSeededTierFloorPoliciesCarryTheVerbatimSpec821ClearingTest(t *testing.T) {
	t.Parallel()

	for rung := range spec86SeededFloors {
		t.Run(rung, func(t *testing.T) {
			t.Parallel()

			s := requireSeedPolicy(t, tierFloorPolicyName(rung))
			parsed := parseSeed(t, s)

			got := inListLiterals(t, parsed, "principal.viewer.tier")
			assert.Equal(t, spec821ClearingTests[rung], got,
				"%s MUST carry §8.2.1's clearing set for the %s floor verbatim and in order",
				s.Name, rung)
		})
	}
}

// TestAPlayerRungTierFloorPolicyIsRequiredExactlyWhenSpec86SeedsAName is D-03's
// re-entry guard.
//
// D-03 as originally written mandated THREE tier-floor policies, one per rung.
// The v0.13 seed ships TWO: §8.6 places every governed row at `anonymous` or
// `guest`, so the `player` rung has no seeded member, and the DSL's list grammar
// (`'[' @@ (',' @@)* ']'`, internal/access/policy/dsl/ast.go) requires at least
// one literal — an empty `in []` does not parse. A third policy cannot be
// written at all without inventing a member.
//
// This test is GREEN today because its antecedent is false. It turns RED the
// moment someone raises a §8.6 row to the `player` rung without seeding the
// rung — which is exactly the failure the omission creates.
func TestAPlayerRungTierFloorPolicyIsRequiredExactlyWhenSpec86SeedsAName(t *testing.T) {
	t.Parallel()

	playerNames := spec86SeededFloors[access.ViewerTierPlayer]
	_, exists := seedPolicyByName(tierFloorPolicyName(access.ViewerTierPlayer))

	if len(playerNames) == 0 {
		assert.False(t, exists,
			"§8.6 seeds NO name at the `player` rung, so %q MUST NOT exist — an empty "+
				"`in []` does not parse and a policy with an invented member would be worse "+
				"than the recorded D-03 deviation",
			tierFloorPolicyName(access.ViewerTierPlayer))
		return
	}

	require.True(t, exists,
		"§8.6 now seeds %v at the `player` rung, so D-03's re-entry condition has fired: "+
			"%q MUST be written, carrying §8.2.1's `principal.viewer.tier in [\"player\"]` "+
			"clearing test and exactly those names",
		playerNames, tierFloorPolicyName(access.ViewerTierPlayer))

	s := requireSeedPolicy(t, tierFloorPolicyName(access.ViewerTierPlayer))
	parsed := parseSeed(t, s)
	assert.ElementsMatch(t, playerNames, inListLiterals(t, parsed, "resource.property.name"))
	assert.Equal(t, spec821ClearingTests[access.ViewerTierPlayer],
		inListLiterals(t, parsed, "principal.viewer.tier"))
}

// TestExactlyTheRungsWithASeededMemberCarryATierFloorPolicy pins the COUNT
// D-03's amendment records (two), derived from §8.6 rather than asserted as a
// bare number. Plan 02-11 asserts the count recorded in 02-CONTEXT.md equals the
// count actually in seed.go; this is the seed-side half of that pair.
func TestExactlyTheRungsWithASeededMemberCarryATierFloorPolicy(t *testing.T) {
	t.Parallel()

	var seeded []string
	for _, s := range SeedPolicies() {
		if strings.HasPrefix(s.Name, "seed:profile-tier-floor-") {
			seeded = append(seeded, s.Name)
		}
	}

	want := make([]string, 0, len(spec86SeededFloors))
	for rung := range spec86SeededFloors {
		want = append(want, tierFloorPolicyName(rung))
	}
	sort.Strings(want)
	sort.Strings(seeded)

	assert.Equal(t, want, seeded,
		"one tier-floor policy per rung that HAS at least one seeded §8.6 member — no more, no fewer (D-03 as amended)")
}

// TestNoTierFloorPolicyUsesAnOrdinalTierComparison is the mechanical form of
// §8.2.1's prohibition. The DSL's only string ordering is Go byte order
// (compareStrings, internal/access/policy/dsl/evaluator.go): the three v0.13
// tokens sort in ladder order by alphabetical accident, and `spectator`,
// `unverified` and `visitor` all sort ABOVE `player` — so a `>=` test would hand
// a newly appended fourth rung the highest clearance in the system on the day
// the token is added, with no policy edit anywhere.
func TestNoTierFloorPolicyUsesAnOrdinalTierComparison(t *testing.T) {
	t.Parallel()

	for _, s := range SeedPolicies() {
		if !strings.Contains(s.DSLText, "principal.viewer.tier") {
			continue
		}
		t.Run(s.Name, func(t *testing.T) {
			t.Parallel()
			parsed := parseSeed(t, s)
			require.NotNil(t, parsed.Conditions)
			for _, conj := range parsed.Conditions.Disjunctions {
				for _, cond := range conj.Conditions {
					if cond.Comparison == nil {
						continue
					}
					for _, side := range []*dsl.Expr{cond.Comparison.Left, cond.Comparison.Right} {
						if side == nil || side.AttrRef == nil {
							continue
						}
						ref := side.AttrRef.Root + "." + strings.Join(side.AttrRef.Path, ".")
						if ref != "principal.viewer.tier" {
							continue
						}
						assert.Equal(t, "==", cond.Comparison.Comparator,
							"%s compares the tier with %q — §8.2.1 forbids ordinal comparison on the tier token",
							s.Name, cond.Comparison.Comparator)
					}
				}
			}
		})
	}
}

// TestTheTierFloorFamilyOwnsTheReadProfileAttributeActionAlone pins §8.5.1's
// term-A/term-B separator. If both evaluations carried the action `read`
// against the same `property:<id>` resource, each would match BOTH families and
// the caller's conjunction would silently reduce to the additive shape §8.5.1.1
// exists to prevent.
func TestTheTierFloorFamilyOwnsTheReadProfileAttributeActionAlone(t *testing.T) {
	t.Parallel()

	const termAAction = "read_profile_attribute"

	var carriers []string
	for _, s := range SeedPolicies() {
		parsed := parseSeed(t, s)
		if parsed.Target.Action == nil {
			continue
		}
		for _, a := range parsed.Target.Action.Actions {
			if a == termAAction {
				carriers = append(carriers, s.Name)
				break
			}
		}
	}

	want := make([]string, 0, len(spec86SeededFloors))
	for rung := range spec86SeededFloors {
		want = append(want, tierFloorPolicyName(rung))
	}
	sort.Strings(want)
	sort.Strings(carriers)

	assert.Equal(t, want, carriers,
		"the %q action separates term A (tier floors) from term B (the row-keyed family, action `read`); "+
			"exactly the tier-floor family may carry it", termAAction)
}

// --- Profile reachability (§8.4.2) ---

// TestSeedProfileReachableIsTranscribedFromSpec842 asserts the policy text
// against §8.4.2's verbatim block. Reachability reads no resource attributes, so
// it needs no `profile`-namespace AttributeProvider; raising the floor is an
// edit to the clearing set and nothing else.
func TestSeedProfileReachableIsTranscribedFromSpec842(t *testing.T) {
	t.Parallel()

	s := requireSeedPolicy(t, "seed:profile-reachable")

	const want = `permit(principal is viewer, action in ["read"], resource is profile) ` +
		`when { principal.viewer.tier in ["anonymous", "guest", "player"] };`
	assert.Equal(t, want, s.DSLText,
		"seed:profile-reachable MUST be §8.4.2's seeded policy verbatim")

	parsed := parseSeed(t, s)
	require.NotNil(t, parsed.Conditions)
	refs := collectAttrRefs(parsed.Conditions)
	for _, ref := range refs {
		assert.NotEqual(t, "resource", ref.namespace,
			"seed:profile-reachable MUST read no resource attributes (§8.4.2) — it references %s.%s",
			ref.namespace, ref.key)
	}
}

// --- Totality (§8.6) ---

// TestNoTierFloorPolicyMatchesAProfileNameByAnythingButAWholeString is §8.6's
// totality rule, mechanically. A name-keyed residual permit would be MORE
// permissive than the engine's own default-deny: §7.1 makes a new profile field
// an INSERT, so the namespace is open.
func TestNoTierFloorPolicyMatchesAProfileNameByAnythingButAWholeString(t *testing.T) {
	t.Parallel()

	for rung := range spec86SeededFloors {
		t.Run(rung, func(t *testing.T) {
			t.Parallel()
			s := requireSeedPolicy(t, tierFloorPolicyName(rung))
			parsed := parseSeed(t, s)
			require.NotNil(t, parsed.Conditions)

			for _, conj := range parsed.Conditions.Disjunctions {
				for _, cond := range conj.Conditions {
					assert.Nil(t, cond.Like,
						"%s uses a glob match — §8.6 forbids glob, prefix, wildcard and catch-all over the profile namespace", s.Name)
					assert.Nil(t, cond.Contains,
						"%s uses containsAll/containsAny — §8.6 requires whole-string name matching", s.Name)
				}
			}

			for _, name := range inListLiterals(t, parsed, "resource.property.name") {
				assert.NotContains(t, name, "*",
					"%s enumerates %q, which carries a wildcard — §8.6 matches names as whole strings", s.Name, name)
			}
		})
	}
}

// TestTheElevenMediaNamesAreEnumeratedAndTheTwelfthIsNot pins §7.3's closed set.
// The eleventh gallery index is deliberately constructed rather than written as
// a literal, so this test names the excluded member without seeding the string
// anywhere a grep for it could find.
func TestTheElevenMediaNamesAreEnumeratedAndTheTwelfthIsNot(t *testing.T) {
	t.Parallel()

	s := requireSeedPolicy(t, tierFloorPolicyName(access.ViewerTierGuest))
	names := inListLiterals(t, parseSeed(t, s), "resource.property.name")

	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		seen[n] = struct{}{}
	}

	assert.Contains(t, seen, "profile.image.primary")
	for i := range 10 {
		want := fmt.Sprintf("profile.image.gallery.%02d", i)
		assert.Contains(t, seen, want, "§7.3 fixes the media names as exact bytes")
	}

	excluded := fmt.Sprintf("profile.image.gallery.%d", 10)
	assert.NotContains(t, seen, excluded,
		"%q is an unenumerated name and is therefore DENIED, not defaulted (§8.6)", excluded)
}

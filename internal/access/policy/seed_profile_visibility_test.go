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
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/dsl"
	"github.com/holomush/holomush/internal/access/policy/types"
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

// --- Viewer-flavored row-keyed reads (D-01, §8.5.1) ---

// viewerTwins maps each viewer twin to the character-flavored policy it twins.
// The read-side subset of the shipped seed:property-* family gains twins;
// seed:property-owner-write does NOT (D-01 — a `viewer:` subject must never hold
// a write permit).
var viewerTwins = map[string]string{
	"seed:viewer-property-public-read":           "seed:property-public-read",
	"seed:viewer-property-private-read":          "seed:property-private-read",
	"seed:viewer-property-admin-read":            "seed:property-admin-read",
	"seed:viewer-property-restricted-visible-to": "seed:property-restricted-visible-to",
	"seed:viewer-property-restricted-excluded":   "seed:property-restricted-excluded",
}

func TestExactlyTheFiveReadSideRowKeyedPoliciesHaveAViewerTwin(t *testing.T) {
	t.Parallel()

	var got []string
	for _, s := range SeedPolicies() {
		if strings.HasPrefix(s.Name, "seed:viewer-property-") {
			got = append(got, s.Name)
		}
	}
	want := make([]string, 0, len(viewerTwins))
	for name := range viewerTwins {
		want = append(want, name)
	}
	sort.Strings(want)
	sort.Strings(got)

	assert.Equal(t, want, got,
		"exactly the five READ-side seed:property-* policies gain viewer twins (D-01)")
}

// TestSeedPropertyOwnerWriteHasNoViewerTwin is D-01's write prohibition, stated
// as its own assertion rather than left implicit in the count above: a `viewer:`
// subject must never hold a write permit on a property.
func TestSeedPropertyOwnerWriteHasNoViewerTwin(t *testing.T) {
	t.Parallel()

	for _, s := range SeedPolicies() {
		if !strings.HasPrefix(s.Name, "seed:viewer-") {
			continue
		}
		parsed := parseSeed(t, s)
		require.NotNil(t, parsed.Target.Action, "%s MUST scope its actions", s.Name)
		for _, action := range parsed.Target.Action.Actions {
			assert.NotContains(t, []string{"write", "delete"}, action,
				"%s carries the %q action — a viewer: subject must never hold a write permit (D-01)",
				s.Name, action)
		}
	}
}

func TestEachViewerTwinMirrorsItsOriginalsEffectAndActions(t *testing.T) {
	t.Parallel()

	compiler := NewCompiler(emptySchema())
	for twinName, originalName := range viewerTwins {
		t.Run(twinName, func(t *testing.T) {
			t.Parallel()

			twin := requireSeedPolicy(t, twinName)
			original := requireSeedPolicy(t, originalName)

			compiledTwin, _, err := compiler.Compile(twin.DSLText)
			require.NoError(t, err)
			compiledOriginal, _, err := compiler.Compile(original.DSLText)
			require.NoError(t, err)

			assert.Equal(t, compiledOriginal.Effect, compiledTwin.Effect,
				"%s MUST carry the same effect as %s — the excluded_from twin is the family's one forbid",
				twinName, originalName)
			assert.Equal(t, compiledOriginal.Target.ActionList, compiledTwin.Target.ActionList,
				"%s MUST carry the same actions as %s", twinName, originalName)

			require.NotNil(t, compiledTwin.Target.PrincipalType)
			assert.Equal(t, "viewer", *compiledTwin.Target.PrincipalType)
			require.NotNil(t, compiledTwin.Target.ResourceType)
			assert.Equal(t, "property", *compiledTwin.Target.ResourceType)
		})
	}
}

// TestNoViewerTwinReferencesACharacterKeyedRowField is the mechanical guard on
// the defect cross-AI review found: `owner`, `visible_to` and `excluded_from`
// hold CHARACTER ids, and a player id compared against them never matches. A
// non-matching key evaluates FALSE (dsl/evaluator.go), so a twin keyed on one
// would make every private/restricted/admin field permanently invisible —
// silently, fail-closed, with no error and no failing behavioural test.
//
// The comparison is on the EXACT attribute key: `owner_player_id` is a legal
// reference that merely shares a prefix with the forbidden `owner`.
func TestNoViewerTwinReferencesACharacterKeyedRowField(t *testing.T) {
	t.Parallel()

	characterKeyed := map[string]struct{}{
		"owner":         {},
		"visible_to":    {},
		"excluded_from": {},
	}

	for twinName := range viewerTwins {
		t.Run(twinName, func(t *testing.T) {
			t.Parallel()

			parsed := parseSeed(t, requireSeedPolicy(t, twinName))
			require.NotNil(t, parsed.Conditions)

			for _, ref := range collectAttrRefs(parsed.Conditions) {
				if ref.namespace != "resource" {
					continue
				}
				key, ok := strings.CutPrefix(ref.key, "property.")
				if !ok {
					continue
				}
				_, forbidden := characterKeyed[key]
				assert.False(t, forbidden,
					"%s references the CHARACTER-keyed resource.property.%s — a viewer: subject is "+
						"player-flavored, so this compares a player id against character ids and can only "+
						"ever deny. Use the derived player-keyed peer (02-CONTEXT.md D-27).",
					twinName, key)
			}
		})
	}
}

// TestTheIdentityBearingViewerTwinsKeyOnTheDerivedPlayerPeers is the positive
// control paired with the absence assertion above: proving the character-keyed
// fields are gone proves nothing unless the derived peers are actually there.
func TestTheIdentityBearingViewerTwinsKeyOnTheDerivedPlayerPeers(t *testing.T) {
	t.Parallel()

	want := map[string][]string{
		"seed:viewer-property-private-read":          {"resource.property.owner_player_id", "principal.viewer.player_id"},
		"seed:viewer-property-restricted-visible-to": {"resource.property.visible_to_players", "principal.viewer.player_id"},
		"seed:viewer-property-restricted-excluded":   {"resource.property.excluded_from_players", "principal.viewer.player_id"},
		"seed:viewer-property-admin-read":            {"principal.viewer.roles"},
	}

	for twinName, refs := range want {
		t.Run(twinName, func(t *testing.T) {
			t.Parallel()
			s := requireSeedPolicy(t, twinName)
			for _, ref := range refs {
				assert.Contains(t, s.DSLText, ref,
					"%s MUST key its identity term on %s", twinName, ref)
			}
		})
	}
}

// TestTheRestrictedViewerTwinsKeepTheirHasGuardRetargeted asserts the
// `resource has property.<field>` guard survives the retarget, so a row whose
// derived list did not resolve is SKIPPED rather than compared against nothing.
func TestTheRestrictedViewerTwinsKeepTheirHasGuardRetargeted(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"seed:viewer-property-restricted-visible-to": "property.visible_to_players",
		"seed:viewer-property-restricted-excluded":   "property.excluded_from_players",
	}

	for twinName, guarded := range want {
		t.Run(twinName, func(t *testing.T) {
			t.Parallel()

			parsed := parseSeed(t, requireSeedPolicy(t, twinName))
			require.NotNil(t, parsed.Conditions)

			var found bool
			for _, conj := range parsed.Conditions.Disjunctions {
				for _, cond := range conj.Conditions {
					if cond.Has == nil {
						continue
					}
					if cond.Has.Root == "resource" && strings.Join(cond.Has.Path, ".") == guarded {
						found = true
					}
				}
			}
			assert.True(t, found,
				"%s MUST guard on `resource has %s` so an unresolved list is skipped, not compared against nothing",
				twinName, guarded)
		})
	}
}

// TestNoViewerTwinAndNoPublicReadWideningIsLocationGated: the viewer path is a
// web read, not a grid read, and colocation has no meaning for a viewer with no
// character. The widening's whole purpose is to drop the colocation clause.
func TestNoViewerTwinAndNoPublicReadWideningIsLocationGated(t *testing.T) {
	t.Parallel()

	names := make([]string, 0, len(viewerTwins)+1)
	for twinName := range viewerTwins {
		names = append(names, twinName)
	}
	names = append(names, "seed:profile-public-read-property")

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := requireSeedPolicy(t, name)
			assert.NotContains(t, s.DSLText, "principal.character.location",
				"%s MUST NOT be location-gated", name)
			assert.NotContains(t, s.DSLText, "parent_location",
				"%s MUST NOT be location-gated", name)
		})
	}
}

// --- The seed:profile-public-read widening (PROFILE-11, D-10, D-11) ---

func TestSeedProfilePublicReadPropertyIsAnAdditivePermitGuardedOnCharacterRows(t *testing.T) {
	t.Parallel()

	s := requireSeedPolicy(t, "seed:profile-public-read-property")

	const want = `permit(principal is character, action in ["read"], resource is property) ` +
		`when { resource.property.visibility == "public" && resource.property.parent_type == "character" };`
	assert.Equal(t, want, s.DSLText,
		"the widening is seed:property-public-read minus its colocation clause, guarded on "+
			"parent_type == \"character\" so it reaches character rows only (D-10)")
}

// TestTheShippedRowKeyedFamilyIsUntouchedByTheWidening pins that the widening is
// ADDITIVE. Permits combine disjunctively (combineDecisions, engine.go), so a new
// permit widens without editing a shipped policy — and therefore without a
// SeedVersion bump that could collide with an admin-customized row.
func TestTheShippedRowKeyedFamilyIsUntouchedByTheWidening(t *testing.T) {
	t.Parallel()

	untouched := map[string]struct {
		dsl         string
		seedVersion int
	}{
		"seed:player-character-colocation": {
			dsl:         `permit(principal is character, action in ["read"], resource is character) when { resource.character.location == principal.character.location };`,
			seedVersion: 2,
		},
		"seed:property-public-read": {
			dsl:         `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "public" && principal.character.location == resource.property.parent_location };`,
			seedVersion: 2,
		},
	}

	for name, want := range untouched {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := requireSeedPolicy(t, name)
			assert.Equal(t, want.dsl, s.DSLText, "%s MUST be untouched by the additive widening", name)
			assert.Equal(t, want.seedVersion, s.SeedVersion,
				"%s MUST keep its shipped SeedVersion — a bump on a shipped policy triggers the "+
					"upgrade path and can collide with an admin-customized row", name)
		})
	}
}

// TestNoPhase2SeedIntroducesACharacterResourceTypePermit is D-29's mechanical
// gate.
//
// The plan's own acceptance criterion for this was a file-wide grep asserting
// ZERO occurrences of the string `resource is character` in non-comment lines of
// seed.go. That grep cannot pass against this file and never could: the shipped
// seed:player-self-access and seed:player-character-colocation both carry
// `resource is character)`, and seed:directory-list-characters' `resource is
// character_directory` matches the same substring. Satisfying it literally would
// mean deleting shipped policies.
//
// What D-29 actually forbids is SEEDING A NEW ONE. This test is that gate,
// stated on the compiled target rather than on file text: the set of seed
// policies whose resource clause is the `character` TYPE is pinned, so adding
// one turns this RED by name. `character_directory` is a different resource type
// and `resource == "character:*"` is an exact-match clause, so neither is
// swept in by accident — both failure modes of the grep.
func TestNoPhase2SeedIntroducesACharacterResourceTypePermit(t *testing.T) {
	t.Parallel()

	// The complete pre-Phase-2 set. Each is CONDITIONED (self-access, and
	// colocation); neither is the unconditional shape D-29 defers.
	//
	// seed:job-fixture-instance-scoped (v0.13 phase 02.2, AUTHZ-02) is the one
	// addition, and it does not instantiate the risk D-29 names. That risk has
	// three parts and this seed misses all three:
	//
	//   - PRINCIPAL. D-29's concern is `principal is character`, which admits
	//     every ephemeral guest. This seed's principal is `job` — a disjoint
	//     namespace (D-48) whose subjects are constructible only via
	//     access.JobSubject / world.JobCaller. No human, guest or otherwise,
	//     can hold a job: subject.
	//   - ACTION. The leak D-29 describes is through world.Service.GetCharacter,
	//     which checks `read`. This seed permits `write` only, so it gates no
	//     read path and reaches no characterToProto projection.
	//   - MATCHABILITY. The `when` clause pins principal.job.name == "fixture",
	//     and no job by that name is registered in production, so the liveness
	//     gate in attribute.JobProvider leaves principal.job.* absent and the
	//     permit cannot match at all.
	//
	// The gate keeps its teeth on all three counts, but NOT from one assertion:
	// the name-set equality below turns RED only when the MEMBERSHIP of the
	// `resource is character` family changes, so it alone would stay GREEN if
	// this seed's action list were widened to `read` or its principal swapped to
	// `character` (neither edit changes a name or a resource type). The
	// per-entry target-shape equality that follows it is what pins PRINCIPAL and
	// ACTION, and the exact-DSL assertion after that is what pins MATCHABILITY —
	// the `principal.job.name == "fixture"` clause. All three assertions are
	// load-bearing; deleting any one re-opens the corresponding half of D-29.
	//
	// seed:job-retirement-instance-scoped (v0.13 phase 03, IDENT-04) is the
	// second addition and it is a DIFFERENT argument, not a copy of the
	// fixture's. State the difference plainly: it is a LIVE grant, so the
	// fixture's third leg (unmatchability) does not apply to it, and it does
	// permit `read`, which the paragraph above names as a widening. What makes
	// that safe is the leg the fixture shares — the instance fence:
	//
	//   - PRINCIPAL is still `job`. Same disjoint namespace; still no human,
	//     guest or otherwise, can hold a job: subject.
	//   - THE READ IS NOT AN ENUMERATION PRIMITIVE, which is the actual risk
	//     D-29 defers. `action.job.trigger_subject == resource.id` binds the
	//     readable character to the one the TRIGGERING event names, and
	//     `action.job.trigger_event_type == "character_retired"` binds which
	//     event may do the triggering. The reactor can therefore read exactly
	//     the character that has just been retired, one per delivered message,
	//     and no other — it cannot walk the roster, and there is no request on
	//     whose behalf it could render what it read. characterToProto is not on
	//     this path: the caller is an in-process host subsystem, not a gRPC
	//     handler.
	//   - THE PROVENANCE IS NOT CALLER-CHOSEN. world.JobCaller derives the
	//     triple from the delivered message and exposes no attribute-map
	//     parameter, and the reactor derives the RESOURCE independently (from
	//     the message body, while the provenance comes from the transport
	//     subject). A handler that derives the wrong aggregate is therefore
	//     denied by this very clause rather than authorized by it.
	//   - LIVENESS still gates it (D-49): principal.job.name resolves only
	//     while internal/jobs.Registry reports "retirement" running, which the
	//     reactor declares in Activate and retracts in Stop.
	//
	// Its own exact-DSL pin below is what keeps that fence from being relaxed.
	// If a future edit drops either `when` conjunct, this seed becomes a job-
	// wide character read — which IS the D-29 risk, reached by a door the name
	// set and the shape map would both stay green for.
	// v0.13 phase 04 plan 01 is the phase this test's own doc comment names, and
	// these two entries are the discharge. Read them against the three-part risk
	// above: the PRINCIPAL of the second one is `viewer` and of the first is
	// `character` — so unlike the job seeds, D-29's "every ephemeral guest"
	// concern DOES apply to the principal. What answers it is the ACTION.
	//
	// `read_description` is a token no other policy and no other call site
	// carries. It reaches exactly one method, world.Service.GetCharacterDescription,
	// whose return type world.CharacterDescription has exactly two fields, Name
	// and Description. The leak D-29 describes runs through
	// world.Service.GetCharacter → characterToProto → {Id, PlayerId, Name,
	// Description, LocationId}; that method still checks `read`, and both
	// `read`-carrying character-resource seeds below are still the conditioned
	// pre-Phase-2 pair. So the roster-enumeration primitive D-29 defers is not
	// reachable from either new entry: there is no field for a player id or a
	// location id to land in, and the compiler enforces that rather than a
	// reviewer.
	//
	// THE ACTION IS THE WHOLE FENCE, so it is pinned twice — in wantShapes below
	// (compiled target) and in the exact-DSL assertions after it. Widening either
	// entry's action list to include "read" IS the D-29 leak, and it would leave
	// this family's name set unchanged.
	// v0.13 phase 06 plan 05 adds the SIXTH argument to this family, and it is a
	// different one again: seed:admin-character-administration. Read it against
	// the three-part risk above.
	//
	//   - PRINCIPAL is `player`, not `character` — so D-29's "every ephemeral
	//     guest" concern does not reach it at all. A guest holds no player:
	//     subject on this surface, and the `when` clause narrows further to
	//     players carrying the `admin` role.
	//   - It is the WORLD-LAYER half of a gate the caller has ALREADY passed. An
	//     admin portal RPC is authorized by the section interceptor against
	//     `admin_section:characters` before any handler runs; this policy is what
	//     lets the same caller then pass world.Service's own checkAccess on
	//     `character:<id>`, which every reused world method runs. Without it the
	//     admin write path is default-denied one layer down.
	//   - `delete` IS ABSENT, deliberately, and its own test asserts both that
	//     the action list has exactly four members and that a real engine denies
	//     a player-admin `delete` on a character. world.Service.DeleteCharacter
	//     is irreversible and cascades entity_properties (§4.4), so the RPC-level
	//     absence of AdminDeleteCharacter is backed at the policy layer too.
	//
	// It DOES carry `read`, which the paragraphs above name as the widening that
	// matters — and unlike the two `read_description` entries, `read` here does
	// reach world.Service.GetCharacter and therefore characterToProto's PlayerId
	// and LocationId. What bounds it is the principal: an admin operator, not a
	// guest. The action list is pinned in wantShapes below, so narrowing or
	// widening it turns this RED by shape even though the name set is unchanged.
	want := []string{
		"seed:admin-character-administration",
		"seed:character-description-read",
		"seed:job-fixture-instance-scoped",
		"seed:job-retirement-instance-scoped",
		"seed:player-character-colocation",
		"seed:player-self-access",
		"seed:viewer-character-description-read",
	}

	// The compiled target shape of every member, so a widening of an EXISTING
	// member is caught even though the name set is unchanged.
	wantShapes := map[string]characterSeedTargetShape{
		"seed:admin-character-administration":    {principal: "player", actions: []string{"read", "write", "retire", "unretire"}},
		"seed:character-description-read":        {principal: "character", actions: []string{"read_description"}},
		"seed:job-fixture-instance-scoped":       {principal: "job", actions: []string{"write"}},
		"seed:job-retirement-instance-scoped":    {principal: "job", actions: []string{"read", "write"}},
		"seed:player-character-colocation":       {principal: "character", actions: []string{"read"}},
		"seed:player-self-access":                {principal: "character", actions: []string{"read", "write"}},
		"seed:viewer-character-description-read": {principal: "viewer", actions: []string{"read_description"}},
	}

	compiler := NewCompiler(emptySchema())
	var got []string
	gotShapes := map[string]characterSeedTargetShape{}
	for _, s := range SeedPolicies() {
		compiled, _, err := compiler.Compile(s.DSLText)
		require.NoError(t, err, "seed %q MUST compile", s.Name)
		if compiled.Target.ResourceType != nil && *compiled.Target.ResourceType == "character" {
			got = append(got, s.Name)
			shape := characterSeedTargetShape{actions: compiled.Target.ActionList}
			if compiled.Target.PrincipalType != nil {
				shape.principal = *compiled.Target.PrincipalType
			}
			gotShapes[s.Name] = shape
		}
	}
	sort.Strings(got)

	assert.Equal(t, want, got,
		"D-29: Phase 2 seeds NO policy whose resource clause is the `character` type. Such a permit "+
			"gates world.Service.GetCharacter, whose characterToProto projection returns PlayerId and "+
			"LocationId, and `principal is character` admits every ephemeral guest — so it would let any "+
			"guest enumerate alt-to-player linkage and live grid position for the whole roster. It moves "+
			"to Phase 4 with the projection narrowing that makes it safe. This is NOT an instance of "+
			"D-10/D-11: `characters` has no `visibility` column, so D-11's mandated remedy does not exist "+
			"for that resource.")

	assert.Equal(t, wantShapes, gotShapes,
		"D-29's exemption for seed:job-fixture-instance-scoped was granted on PRINCIPAL (`job`, a namespace "+
			"no human can hold) and ACTION (`write` only, so it gates no read path and reaches no "+
			"characterToProto projection). Widening either — action to include `read`, or principal to "+
			"`character` — instantiates exactly the risk D-29 defers to Phase 4, WITHOUT changing this "+
			"family's name set. That is why the shape is pinned here and not left to the name-set equality "+
			"above. The same pin covers the two shipped seeds: widening either of those is the same leak "+
			"by a different door. seed:job-retirement-instance-scoped is the ONE member permitted to carry "+
			"`read`, and only because its instance fence makes it a single-row lookup rather than an "+
			"enumeration primitive (see this test's doc comment); a THIRD job seed carrying `read` is not "+
			"covered by that reasoning and must argue its own case here.")

	// MATCHABILITY, the third leg of the exemption. Pinned as exact DSL because
	// the clause that makes the permit unmatchable in production
	// (`principal.job.name == "fixture"`, no such job registered) lives in the
	// `when` body, which the compiled Target does not carry. SeedVersion rides
	// along for the same reason the `untouched` table above pins it: a bump on a
	// shipped policy triggers the upgrade path.
	jobSeed := requireSeedPolicy(t, "seed:job-fixture-instance-scoped")
	const wantJobDSL = `permit(principal is job, action in ["write"], resource is character) ` +
		`when { principal.job.name == "fixture" && principal.job.writes.containsAll(["character"]) && ` +
		`action.job.trigger_event_type == "fixture_triggered" && action.job.trigger_subject == resource.id };`
	assert.Equal(t, wantJobDSL, jobSeed.DSLText,
		"the fixture seed's third exemption leg is that it cannot match in production: no job named "+
			"\"fixture\" is registered, so attribute.JobProvider's liveness gate leaves principal.job.* "+
			"absent and the permit default-denies. Relaxing or deleting the name clause makes this seed "+
			"live against whatever job IS registered — an unreviewed grant of character writes.")
	assert.Equal(t, 1, jobSeed.SeedVersion,
		"seed:job-fixture-instance-scoped ships at SeedVersion 1 (v0.13 phase 02.2, AUTHZ-02)")

	// The retirement grant's exemption rests ENTIRELY on its instance fence
	// (it is live, so it has no unmatchability leg to fall back on), and the
	// fence lives in the `when` body the compiled Target does not carry. Pinned
	// as exact DSL for that reason: dropping `action.job.trigger_subject ==
	// resource.id` turns a single-row lookup into a job-wide character read,
	// and dropping `action.job.trigger_event_type == "character_retired"` lets
	// every other character event the consumer's filter delivers carry the same
	// authority.
	retirementSeed := requireSeedPolicy(t, "seed:job-retirement-instance-scoped")
	const wantRetirementDSL = `permit(principal is job, action in ["read", "write"], resource is character) ` +
		`when { principal.job.name == "retirement" && principal.job.writes.containsAll(["character"]) && ` +
		`action.job.trigger_event_type == "character_retired" && action.job.trigger_subject == resource.id };`
	assert.Equal(t, wantRetirementDSL, retirementSeed.DSLText,
		"seed:job-retirement-instance-scoped is a LIVE grant whose only fence is its `when` clause. Both "+
			"action.job.* conjuncts are load-bearing: trigger_subject bounds WHICH character may be read "+
			"or written (to the one the triggering event names), and trigger_event_type bounds WHAT may "+
			"trigger it (the reactor's consumer filter is the whole character aggregate, so without this "+
			"conjunct a character_created or character_moved delivery would carry retirement's authority). "+
			"Relaxing either is the D-29 leak by a different door.")
	assert.Equal(t, 1, retirementSeed.SeedVersion,
		"seed:job-retirement-instance-scoped ships at SeedVersion 1 (v0.13 phase 03, IDENT-04)")

	// The two Phase-4 read_description permits, pinned as exact DSL for the same
	// reason the job seeds are: their whole fence is a target-clause detail the
	// name set does not carry. The viewer twin's `when` body additionally carries
	// the tier clearing list §7.4 says a game edits to raise the description's
	// floor — dropping it entirely would make the permit unconditional across
	// every future rung, and rewriting it as an ordinal comparison would hand a
	// newly appended fourth rung the highest clearance in the system (§8.2.1).
	descSeed := requireSeedPolicy(t, "seed:character-description-read")
	assert.Equal(t,
		`permit(principal is character, action in ["read_description"], resource is character);`,
		descSeed.DSLText,
		"seed:character-description-read is the grid-side off-location description read (D-29's literal "+
			"deferral, D-75). Its safety rests entirely on the ACTION token: `read_description` reaches only "+
			"world.Service.GetCharacterDescription, whose return type carries no player id and no location "+
			"id. Widening it to `read` reaches GetCharacter and its full CharacterInfo projection — which IS "+
			"the leak D-29 deferred.")
	assert.Equal(t, 1, descSeed.SeedVersion,
		"seed:character-description-read ships at SeedVersion 1 (v0.13 phase 04, PROFILE-11's character half)")

	viewerDescSeed := requireSeedPolicy(t, "seed:viewer-character-description-read")
	assert.Equal(t,
		`permit(principal is viewer, action in ["read_description"], resource is character) `+
			`when { principal.viewer.tier in ["anonymous", "guest", "player"] };`,
		viewerDescSeed.DSLText,
		"seed:viewer-character-description-read is the D-76 viewer twin. It carries its own tier clearing "+
			"test because the tier-floor family targets `resource is property` and this resource is a "+
			"CHARACTER, so no tier-floor policy governs it. SET MEMBERSHIP, never ordinal comparison "+
			"(§8.2.1). The list is what a game edits to raise the description's floor (§7.4).")
	assert.Equal(t, 1, viewerDescSeed.SeedVersion,
		"seed:viewer-character-description-read ships at SeedVersion 1 (v0.13 phase 04, D-76)")

	_, exists := seedPolicyByName("seed:profile-public-read-character")
	assert.False(t, exists,
		"seed:profile-public-read-character — the `action in [\"read\"]` shape D-29 deferred — MUST NOT be "+
			"seeded. Phase 4 discharged the deferral with the two narrow read_description permits above "+
			"instead; the deferred NAME must stay absent so a future edit cannot resurrect the deferred SHAPE "+
			"under cover of \"Phase 4 shipped it\".")
}

// characterSeedTargetShape is the slice of a compiled Target that D-29 cares
// about for the `resource is character` family: WHO may act (principal type)
// and WHAT they may do (action list). Resource type is not a member — every
// entry in the family has it equal to "character" by construction.
type characterSeedTargetShape struct {
	principal string
	actions   []string
}

// --- Admin sections (EXT-07, §10.4, §10.5) ---

func TestSeedAdminSectionAccessIsTypeScopedAndPlayerFlavored(t *testing.T) {
	t.Parallel()

	s := requireSeedPolicy(t, "seed:admin-section-access")

	const want = `permit(principal is player, action in ["read", "write"], resource is admin_section) ` +
		`when { "admin" in principal.player.roles };`
	assert.Equal(t, want, s.DSLText)

	compiled, _, err := NewCompiler(emptySchema()).Compile(s.DSLText)
	require.NoError(t, err)

	require.NotNil(t, compiled.Target.PrincipalType)
	assert.Equal(t, "player", *compiled.Target.PrincipalType,
		"§10.5's verdict is normative — the admin gate is evaluated PER PLAYER. A character-flavored "+
			"principal would put two different answers to \"is this caller an admin\" over one table, the "+
			"operator socket saying yes and the web saying no for the same human at the same moment.")

	require.NotNil(t, compiled.Target.ResourceType)
	assert.Equal(t, "admin_section", *compiled.Target.ResourceType,
		"scoped by resource TYPE, so all seven registered sections and every future section are covered "+
			"at zero additional policy cost (EXT-07)")

	assert.Nil(t, compiled.Target.ResourceExact,
		"an enumerated admin_section:<id> would leave an eighth section uncovered — the exact gap EXT-07 closes")
	assert.NotContains(t, s.DSLText, "admin_section:",
		"no literal section id may appear in the DSL — the scoping is by type")

	assert.Equal(t, []string{"read", "write"}, compiled.Target.ActionList,
		"§10.4: `read` to reach a section, `write` for a mutation within it")
}

// TestSeedAdminCharacterAdministrationIsCharacterScopedAndExcludesDelete pins
// the WORLD-LAYER gate that lets the D-104 player-flavoured admin caller reach
// world.Service's own checkAccess.
//
// Without it every admin character write is DEFAULT-DENIED one layer below the
// section interceptor: seed:admin-full-access requires `principal is character`
// and never fires for a player principal, and seed:admin-section-access is
// scoped `resource is admin_section` and does not reach a `character:` resource.
//
// `delete` is DELIBERATELY absent from the action list. world.Service's
// DeleteCharacter is irreversible and cascades entity_properties (§4.4); there is
// no AdminDeleteCharacter RPC, and this omission makes the same guarantee hold at
// the POLICY layer, where an RPC-level omission cannot be quietly undone.
func TestSeedAdminCharacterAdministrationIsCharacterScopedAndExcludesDelete(t *testing.T) {
	t.Parallel()

	s := requireSeedPolicy(t, "seed:admin-character-administration")

	const want = `permit(principal is player, action in ["read", "write", "retire", "unretire"], resource is character) ` +
		`when { "admin" in principal.player.roles };`
	assert.Equal(t, want, s.DSLText)

	compiled, _, err := NewCompiler(emptySchema()).Compile(s.DSLText)
	require.NoError(t, err)

	require.NotNil(t, compiled.Target.PrincipalType)
	assert.Equal(t, "player", *compiled.Target.PrincipalType,
		"D-104 forces a player-flavoured caller so the envelope Actor is player:<id>; a character-flavoured "+
			"one would put the acting-character id back into the retained audit trail")

	require.NotNil(t, compiled.Target.ResourceType)
	assert.Equal(t, "character", *compiled.Target.ResourceType,
		"narrower than seed:admin-full-access's unrestricted resource")

	assert.Nil(t, compiled.Target.ResourceExact,
		"scoped by resource TYPE, like its admin_section sibling")

	require.Len(t, compiled.Target.ActionList, 4,
		"exactly the four actions the admin RPCs traverse, and no more")
	assert.Equal(t, []string{"read", "write", "retire", "unretire"}, compiled.Target.ActionList)
	assert.NotContains(t, compiled.Target.ActionList, "delete",
		"world.Service.DeleteCharacter MUST stay unreachable from the admin boundary at the POLICY layer too")
}

// --- Attribute-reference coverage ---

// phase02SeedNames is the complete set of seed policies plan 02-07 adds. Every
// attribute reference in each of them is resolved against a registered
// provider's declared schema below.
var phase02SeedNames = []string{
	"seed:profile-tier-floor-anonymous",
	"seed:profile-tier-floor-guest",
	"seed:profile-reachable",
	"seed:viewer-property-public-read",
	"seed:viewer-property-private-read",
	"seed:viewer-property-admin-read",
	"seed:viewer-property-restricted-visible-to",
	"seed:viewer-property-restricted-excluded",
	"seed:profile-public-read-property",
	"seed:admin-section-access",
}

// TestEveryAttributeAnewSeedReferencesIsSuppliedByARegisteredProvider is the
// mechanical guard against the whole class of defect cross-AI review found on
// this plan.
//
// A policy naming an attribute NOTHING supplies denies forever, silently: the
// key is simply absent from the bag, and a missing key evaluates FALSE for
// every operator (dsl/evaluator.go). No behavioural test can distinguish that
// from a policy that is correctly denying — the visible symptom is a bare
// profile, which §8.5.1.1 records as the symptom that provokes the forbidden
// repair of dropping term B. So the reference set is checked against the
// providers' DECLARED schemas rather than assumed.
//
// The schemas come from the REAL providers, not from a transcription: a
// transcribed expectation would go stale in the safe-looking direction the day
// a provider drops a key.
func TestEveryAttributeAnewSeedReferencesIsSuppliedByARegisteredProvider(t *testing.T) {
	t.Parallel()

	// The providers BuildABACStack registers that this plan's seeds read.
	supplied := map[string]map[string]types.AttrType{
		"viewer":   attribute.NewViewerTierProvider().Schema().Attributes,
		"player":   attribute.NewPlayerAttributeProvider(nil).Schema().Attributes,
		"property": attribute.NewPropertyProvider(nil, nil, nil).Schema().Attributes,
	}

	for _, name := range phase02SeedNames {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			parsed := parseSeed(t, requireSeedPolicy(t, name))
			if parsed.Conditions == nil {
				return
			}

			for _, ref := range collectAttrRefs(parsed.Conditions) {
				// A reference is `<principal|resource>.<namespace>.<key>`;
				// collectAttrRefs reports namespace="principal"|"resource" and
				// key="<namespace>.<key>".
				namespace, key, ok := strings.Cut(ref.key, ".")
				require.True(t, ok,
					"%s references %s.%s, which names no attribute namespace",
					name, ref.namespace, ref.key)

				attrs, known := supplied[namespace]
				require.True(t, known,
					"%s references %s.%s.%s, but no provider in this plan's set supplies the %q "+
						"namespace — the attribute would be absent from the bag and the policy would "+
						"deny forever, silently",
					name, ref.namespace, namespace, key, namespace)

				_, declared := attrs[key]
				assert.True(t, declared,
					"%s references %s.%s.%s, which the real %s provider does NOT declare. "+
						"A missing key evaluates FALSE for every operator, so this policy can only "+
						"ever deny — with no error and no failing behavioural test. Supplied keys: %v",
					name, ref.namespace, namespace, key, namespace, sortedAttrKeys(attrs))
			}
		})
	}
}

func sortedAttrKeys(m map[string]types.AttrType) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestOwnerMapResolve exercises every branch of the longest-prefix-wins +
// literal-tiebreak resolver via table-driven cases.
func TestOwnerMapResolve(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		decls    []audit.SubjectOwner
		subject  string
		wantName string // empty => host fallback
	}{
		{
			name: "longer-prefix wins over shorter-prefix at different depths",
			decls: []audit.SubjectOwner{
				{PluginName: "core-scenes", Pattern: "events.*.scene.>"},
				{PluginName: "scene-lifecycle", Pattern: "events.*.scene.*.lifecycle"},
			},
			subject:  "events.main.scene.01ABC.lifecycle",
			wantName: "scene-lifecycle",
		},
		{
			name: "literal token beats wildcard at same depth",
			decls: []audit.SubjectOwner{
				{PluginName: "scenes-wild", Pattern: "events.main.scene.*"},
				{PluginName: "scenes-literal", Pattern: "events.main.scene.literal"},
			},
			subject:  "events.main.scene.literal",
			wantName: "scenes-literal",
		},
		{
			name: "literal token beats > wildcard at matching subject",
			decls: []audit.SubjectOwner{
				{PluginName: "scenes-wild", Pattern: "events.main.scene.>"},
				{PluginName: "scenes-literal", Pattern: "events.main.scene.literal"},
			},
			subject:  "events.main.scene.literal",
			wantName: "scenes-literal",
		},
		{
			name: "unmatched subject falls back to host",
			decls: []audit.SubjectOwner{
				{PluginName: "scenes", Pattern: "events.*.scene.>"},
			},
			subject:  "events.main.location.01ABC",
			wantName: "",
		},
		{
			name: "star matches exactly one token",
			decls: []audit.SubjectOwner{
				{PluginName: "scenes", Pattern: "events.*.scene.*"},
			},
			subject:  "events.main.scene.01ABC",
			wantName: "scenes",
		},
		{
			name: "star does not match zero tokens",
			decls: []audit.SubjectOwner{
				{PluginName: "scenes", Pattern: "events.*.scene.*"},
			},
			subject:  "events.main.scene", // missing terminal token
			wantName: "",
		},
		{
			name: "greater-than matches one or more remaining tokens",
			decls: []audit.SubjectOwner{
				{PluginName: "scenes", Pattern: "events.main.scene.>"},
			},
			subject:  "events.main.scene.01ABC.ic.reply",
			wantName: "scenes",
		},
		{
			name: "greater-than requires at least one remaining token",
			decls: []audit.SubjectOwner{
				{PluginName: "scenes", Pattern: "events.main.scene.>"},
			},
			subject:  "events.main.scene", // nothing after `scene`
			wantName: "",
		},
		{
			name:     "empty OwnerMap resolves everything to host",
			decls:    nil,
			subject:  "events.main.scene.01ABC.ic",
			wantName: "",
		},
		{
			name: "same plugin declaring duplicate pattern is tolerated",
			decls: []audit.SubjectOwner{
				{PluginName: "scenes", Pattern: "events.*.scene.>"},
				{PluginName: "scenes", Pattern: "events.*.scene.>"},
			},
			subject:  "events.main.scene.01ABC",
			wantName: "scenes",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, err := audit.NewOwnerMap(tc.decls)
			require.NoError(t, err)
			owner := m.Resolve(tc.subject)
			assert.Equal(t, tc.wantName, owner.PluginName)
		})
	}
}

// TestNilOwnerMapResolveReturnsHost verifies the nil-safe Resolve path.
// A nil *OwnerMap is the sentinel for "no plugins declared ownership" —
// the projection passes nil in Phase A and expects host-everything.
func TestNilOwnerMapResolveReturnsHost(t *testing.T) {
	t.Parallel()
	var m *audit.OwnerMap
	owner := m.Resolve("events.main.scene.01ABC")
	assert.Empty(t, owner.PluginName)
}

// TestNilOwnerMapHostExcludedSubjectsReturnsNil asserts defensive behavior
// for the accessor on a nil receiver — it MUST NOT panic.
func TestNilOwnerMapHostExcludedSubjectsReturnsNil(t *testing.T) {
	t.Parallel()
	var m *audit.OwnerMap
	assert.Nil(t, m.HostExcludedSubjects())
}

// TestHostExcludedSubjectsOmitsHostEntries confirms the accessor only
// returns plugin-owned entries. Plugin loaders will feed only plugin
// owners into the map, but an empty-PluginName entry MUST be treated as
// host fallback regardless.
func TestHostExcludedSubjectsOmitsHostEntries(t *testing.T) {
	t.Parallel()
	m, err := audit.NewOwnerMap([]audit.SubjectOwner{
		{PluginName: "scenes", Pattern: "events.*.scene.>"},
		{PluginName: "channels", Pattern: "events.*.channel.>"},
		{PluginName: "", Pattern: "events.*.location.>"}, // host fallback entry
	})
	require.NoError(t, err)
	excluded := m.HostExcludedSubjects()
	require.Len(t, excluded, 2)
	names := []string{excluded[0].PluginName, excluded[1].PluginName}
	assert.Contains(t, names, "scenes")
	assert.Contains(t, names, "channels")
}

// TestNewOwnerMapRejectsDuplicatePatternFromDifferentPlugins exercises
// the startup-conflict fail-fast — two distinct plugins MUST NOT share
// a pattern. Silently picking one would be a data-loss bug.
func TestNewOwnerMapRejectsDuplicatePatternFromDifferentPlugins(t *testing.T) {
	t.Parallel()
	_, err := audit.NewOwnerMap([]audit.SubjectOwner{
		{PluginName: "a", Pattern: "events.*.scene.>"},
		{PluginName: "b", Pattern: "events.*.scene.>"},
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_SUBJECT_OWNERSHIP_CONFLICT")
	require.ErrorIs(t, err, eventbus.ErrSubjectOwnershipConflict)
}

// TestNewOwnerMapRejectsGreaterThanInNonTerminalPosition asserts that
// patterns like "events.>.scene" fail at construction. NATS would reject
// this at subscribe time; we surface it earlier so bad manifests never
// reach the consumer config.
func TestNewOwnerMapRejectsGreaterThanInNonTerminalPosition(t *testing.T) {
	t.Parallel()
	_, err := audit.NewOwnerMap([]audit.SubjectOwner{
		{PluginName: "broken", Pattern: "events.>.scene"},
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_INVALID_SUBJECT_PATTERN")
}

// TestNewOwnerMapRejectsEmptyPattern guards against a zero-value
// SubjectOwner slipping through — an empty pattern would match nothing
// useful and likely signals a manifest bug.
func TestNewOwnerMapRejectsEmptyPattern(t *testing.T) {
	t.Parallel()
	_, err := audit.NewOwnerMap([]audit.SubjectOwner{
		{PluginName: "broken", Pattern: ""},
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_INVALID_SUBJECT_PATTERN")
}

// TestNewOwnerMapRejectsEmptyToken rejects patterns like "events..scene"
// which would split to a zero-length token and match nothing. NATS also
// rejects these; mirroring at construction keeps manifests honest.
func TestNewOwnerMapRejectsEmptyToken(t *testing.T) {
	t.Parallel()
	_, err := audit.NewOwnerMap([]audit.SubjectOwner{
		{PluginName: "broken", Pattern: "events..scene"},
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_INVALID_SUBJECT_PATTERN")
}

// TestOwnerMapConflictErrorIsErrSubjectOwnershipConflict confirms
// errors.Is semantics — downstream callers (the audit subsystem's Start
// path in F5) MUST be able to match this sentinel.
func TestOwnerMapConflictErrorIsErrSubjectOwnershipConflict(t *testing.T) {
	t.Parallel()
	_, err := audit.NewOwnerMap([]audit.SubjectOwner{
		{PluginName: "a", Pattern: "events.alpha"},
		{PluginName: "b", Pattern: "events.alpha"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, eventbus.ErrSubjectOwnershipConflict))
}

// ----------------------------------------------------------------------
// Property test — rapid
// ----------------------------------------------------------------------

// patternGen generates a well-formed (construction-valid) subject pattern:
//   - 2-5 tokens
//   - each token is either a literal from a small alphabet, "*", or ">"
//   - ">" only appears in the terminal position
//
// The small literal alphabet guarantees patterns occasionally collide on
// concrete subjects, which is the interesting regime for the property.
func patternGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		depth := rapid.IntRange(2, 5).Draw(t, "depth")
		tokens := make([]string, depth)
		literals := []string{"events", "main", "scene", "location", "lifecycle", "ic", "ooc"}
		for i := 0; i < depth; i++ {
			isLast := i == depth-1
			// 0=literal, 1=*, 2=> (only if last).
			maxKind := 2
			if isLast {
				maxKind = 3
			}
			kind := rapid.IntRange(0, maxKind-1).Draw(t, "kind")
			switch {
			case kind == 1:
				tokens[i] = "*"
			case isLast && kind == 2:
				tokens[i] = ">"
			default:
				tokens[i] = rapid.SampledFrom(literals).Draw(t, "tok")
			}
		}
		return strings.Join(tokens, ".")
	})
}

// concreteGen generates a concrete subject (no wildcards), 2-6 tokens
// drawn from the same small alphabet as patternGen so matches are
// non-trivially common.
func concreteGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		depth := rapid.IntRange(2, 6).Draw(t, "depth")
		literals := []string{"events", "main", "scene", "location", "lifecycle", "ic", "ooc", "01ABC", "01XYZ"}
		tokens := make([]string, depth)
		for i := 0; i < depth; i++ {
			tokens[i] = rapid.SampledFrom(literals).Draw(t, "tok")
		}
		return strings.Join(tokens, ".")
	})
}

// TestOwnerMapResolveIsDeterministicAndMonotonic is the rapid property
// test: for any set of patterns and concrete subjects, Resolve must:
//  1. be deterministic — same input ⇒ same output across invocations.
//  2. respect depth monotonicity — the returned owner's pattern token
//     depth MUST be ≥ every other matching pattern's depth.
//  3. respect literal-score at equal depth — at the same depth,
//     literal-heavier patterns MUST win over wildcard-heavier ones.
func TestOwnerMapResolveIsDeterministicAndMonotonic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		rawDecls := rapid.SliceOfN(patternGen(), 1, 8).Draw(t, "patterns")

		// Deduplicate same-plugin patterns, then assign each unique
		// pattern to its own plugin name so we never synthesize a
		// conflict-triggering duplicate. Conflicts are covered by the
		// dedicated fail-fast test above.
		seen := make(map[string]struct{}, len(rawDecls))
		decls := make([]audit.SubjectOwner, 0, len(rawDecls))
		for i, p := range rawDecls {
			if _, dup := seen[p]; dup {
				continue
			}
			seen[p] = struct{}{}
			decls = append(decls, audit.SubjectOwner{
				PluginName: patternPlugin(i),
				Pattern:    p,
			})
		}

		m, err := audit.NewOwnerMap(decls)
		require.NoError(t, err)

		subject := concreteGen().Draw(t, "subject")

		first := m.Resolve(subject)
		second := m.Resolve(subject)
		if first != second {
			t.Fatalf("Resolve not deterministic: %+v vs %+v for %q", first, second, subject)
		}

		if first.PluginName == "" {
			// Host fallback — no declared pattern matched. Nothing to
			// assert about depth/literal-score ranking.
			return
		}

		// Monotonicity: the winning pattern must have at least as many
		// tokens as every other matching pattern, and at equal depth,
		// at least as many literals.
		winnerTokens := strings.Split(first.Pattern, ".")
		winnerDepth := len(winnerTokens)
		winnerLiterals := countLiterals(winnerTokens)

		for _, d := range decls {
			if !patternMatchesForProperty(d.Pattern, subject) {
				continue
			}
			candTokens := strings.Split(d.Pattern, ".")
			candDepth := len(candTokens)
			if candDepth > winnerDepth {
				t.Fatalf(
					"longer match lost: winner=%q depth=%d, candidate=%q depth=%d, subject=%q",
					first.Pattern, winnerDepth, d.Pattern, candDepth, subject,
				)
			}
			if candDepth == winnerDepth && countLiterals(candTokens) > winnerLiterals {
				t.Fatalf(
					"literal-heavier match lost: winner=%q lits=%d, candidate=%q lits=%d, subject=%q",
					first.Pattern, winnerLiterals, d.Pattern, countLiterals(candTokens), subject,
				)
			}
		}
	})
}

// patternPlugin returns a distinct synthetic plugin name per pattern
// index so the rapid generator can declare every unique pattern without
// colliding.
func patternPlugin(i int) string {
	return "plugin-" + string(rune('a'+i%26))
}

// countLiterals is a test-local copy of the production literalScore so
// the property test doesn't depend on an unexported function.
func countLiterals(tokens []string) int {
	n := 0
	for _, tok := range tokens {
		if tok != "*" && tok != ">" {
			n++
		}
	}
	return n
}

// patternMatchesForProperty is a test-local re-implementation of the
// NATS match semantics. Keeping the oracle independent of the
// production matcher is deliberate — it catches bugs where both the
// production and oracle drift together.
func patternMatchesForProperty(pattern, subject string) bool {
	pt := strings.Split(pattern, ".")
	ct := strings.Split(subject, ".")
	for i, p := range pt {
		if p == ">" {
			return i < len(ct)
		}
		if i >= len(ct) {
			return false
		}
		if p == "*" {
			continue
		}
		if p != ct[i] {
			return false
		}
	}
	return len(pt) == len(ct)
}

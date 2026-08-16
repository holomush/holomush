// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// fakeJobRegistry RECORDS the names it was asked about. The recording is what
// lets a test assert the provider's prefix guard short-circuits BEFORE the
// registry lookup — an assertion on the registry's ANSWER cannot distinguish
// "the guard fired" from "the registry happened to say no".
type fakeJobRegistry struct {
	running map[string]bool
	writes  map[string][]string

	askedRunning []string
	askedWrites  []string
}

func (f *fakeJobRegistry) IsJobRunning(name string) bool {
	f.askedRunning = append(f.askedRunning, name)
	return f.running[name]
}

func (f *fakeJobRegistry) DeclaredWrites(name string) ([]string, bool) {
	f.askedWrites = append(f.askedWrites, name)
	w, ok := f.writes[name]
	return w, ok
}

func TestJobProviderContract(t *testing.T) {
	assertProviderContract(t, NewJobProvider(&fakeJobRegistry{}))
}

// keysOf returns the attribute map's key set, for SET-EQUALITY assertions.
// Containment is not good enough here: an extra key is the failure mode.
func keysOf(attrs map[string]any) []string {
	out := make([]string, 0, len(attrs))
	for k := range attrs {
		out = append(out, k)
	}
	return out
}

// TestJobProviderEmitsWritesOnlyWhenDeclared covers BOTH emission paths, and
// the pair is what proves no fail-open sentinel is emitted.
//
// An empty or nil `writes` VALUE would be a RESOLVED value that a
// containsAll-shaped condition evaluates against, which is the list-flavored
// empty-string sentinel .claude/rules/abac-providers.md forbids. The omit path
// must therefore drop the key entirely — asserted with a two-value map read,
// because `attrs["writes"] == nil` and a length check both pass against a
// present-but-empty value.
//
// has_writes appears in BOTH key sets on purpose: the has_X witness convention
// requires the witness on EVERY code path; only the VALUE key is omitted.
func TestJobProviderEmitsWritesOnlyWhenDeclared(t *testing.T) {
	t.Run("success path emits name, writes and the witness", func(t *testing.T) {
		reg := &fakeJobRegistry{
			running: map[string]bool{"fixture": true},
			writes:  map[string][]string{"fixture": {"character"}},
		}
		p := NewJobProvider(reg)

		attrs, err := p.ResolveSubject(context.Background(), "job:fixture")
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"name", "writes", "has_writes"}, keysOf(attrs))
		assert.Equal(t, "fixture", attrs["name"])
		assert.Equal(t, []string{"character"}, attrs["writes"])
		assert.Equal(t, true, attrs["has_writes"])
	})

	omitCases := map[string]*fakeJobRegistry{
		"registry reports no declaration": {
			running: map[string]bool{"fixture": true},
			writes:  map[string][]string{},
		},
		"registry reports an empty declaration": {
			running: map[string]bool{"fixture": true},
			writes:  map[string][]string{"fixture": {}},
		},
	}

	for name, reg := range omitCases {
		t.Run("omit path: "+name, func(t *testing.T) {
			p := NewJobProvider(reg)

			attrs, err := p.ResolveSubject(context.Background(), "job:fixture")
			require.NoError(t, err)

			assert.ElementsMatch(t, []string{"name", "has_writes"}, keysOf(attrs))
			assert.Equal(t, "fixture", attrs["name"])
			assert.Equal(t, false, attrs["has_writes"])

			_, present := attrs["writes"]
			require.False(t, present,
				"an undeclared capability class MUST omit the writes key entirely — a "+
					"present-but-empty list is the fail-open sentinel")
		})
	}
}

// TestJobProviderSchemaDeclaresWritesAsAStringList asserts the declared TYPE
// directly. Declaring writes as AttrTypeString still registers successfully and
// then misbehaves silently under containsAll, so the registration succeeding is
// not evidence the type is right.
func TestJobProviderSchemaDeclaresWritesAsAStringList(t *testing.T) {
	schema := NewJobProvider(&fakeJobRegistry{}).Schema()
	require.NotNil(t, schema)

	assert.Equal(t, types.AttrTypeString, schema.Attributes["name"])
	assert.Equal(t, types.AttrTypeStringList, schema.Attributes["writes"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["has_writes"])
}

// --- Fail-closed fences (AUTHZ-02) -----------------------------------------

// TestJobProviderResolvesNothingForAJobThatIsNotRunning is D-49's proof:
// authority is tied to LIVENESS. A job that is not running stamps NO attributes
// at all, so every attribute-conditioned permit fails to match — a missing
// attribute is false for every operator (ADR holomush-iv43).
//
// BOTH returns must be nil. A non-nil empty map is not the same thing: it would
// still be merged into the bag, and the resolver's cache would store it as a
// resolved answer. And if this test could be made to pass while the provider
// returned a placeholder VALUE, D-49 would not be proven at all — a sentinel is
// fail-OPEN under .claude/rules/abac-providers.md.
// Verifies: INV-ACCESS-13
func TestJobProviderResolvesNothingForAJobThatIsNotRunning(t *testing.T) {
	reg := &fakeJobRegistry{
		running: map[string]bool{"running-job": true},
		writes:  map[string][]string{"running-job": {"character"}},
	}
	p := NewJobProvider(reg)

	attrs, err := p.ResolveSubject(context.Background(), "job:stopped-job")
	require.NoError(t, err, "a stopped job is not an ERROR — it simply resolves to nothing")
	assert.Nil(t, attrs, "a job that is not running MUST stamp no attributes")
}

// TestJobProviderNilRegistryResolvesNothing is D-49's degenerate case: an
// entrypoint that wires no job registry must fail closed for every job rather
// than panicking or resolving a bare name.
// Verifies: INV-ACCESS-13
func TestJobProviderNilRegistryResolvesNothing(t *testing.T) {
	p := NewJobProvider(nil)

	attrs, err := p.ResolveSubject(context.Background(), "job:fixture")
	require.NoError(t, err)
	assert.Nil(t, attrs, "a nil registry MUST deny every job (fail-closed)")
}

// TestJobProviderIgnoresForeignRefsWithoutConsultingTheRegistry is the
// spoofing fence (T-02.2-03).
//
// Resolver.resolveEntity hands the FULL entity ref to EVERY registered
// provider, so this provider is called with every character, plugin, player and
// system subject in the system. Without a prefix guard, a registry that
// happened to answer true for one of those refs would stamp job.* keys onto
// another principal's bag — forging a job identity.
//
// The assertion is on the CALL COUNT, not on the answer. Asserting only that
// the result is nil cannot distinguish "the guard fired" from "the registry
// happened to say no", and the second is not a fence.
func TestJobProviderIgnoresForeignRefsWithoutConsultingTheRegistry(t *testing.T) {
	foreign := []string{
		"character:01ARZ3NDEKTSV4RRFFQ69G5FB1",
		"plugin:builder-bot",
		"player:01ARZ3NDEKTSV4RRFFQ69G5FB2",
		// The bare system subject carries no ':' at all.
		"system",
	}

	for _, ref := range foreign {
		t.Run(ref, func(t *testing.T) {
			// The registry answers TRUE for everything, so a missing guard
			// would visibly stamp attributes rather than failing quietly.
			reg := &alwaysRunningJobRegistry{}
			p := NewJobProvider(reg)

			attrs, err := p.ResolveSubject(context.Background(), ref)
			require.NoError(t, err)
			assert.Nil(t, attrs, "the job provider MUST NOT stamp attributes onto a foreign ref")
			assert.Zero(t, reg.calls,
				"the prefix guard MUST short-circuit BEFORE the registry lookup")
		})
	}
}

// alwaysRunningJobRegistry answers true for every name and counts its calls, so
// a missing prefix guard produces attributes rather than a quiet nil.
type alwaysRunningJobRegistry struct{ calls int }

func (r *alwaysRunningJobRegistry) IsJobRunning(string) bool {
	r.calls++
	return true
}

func (r *alwaysRunningJobRegistry) DeclaredWrites(string) ([]string, bool) {
	r.calls++
	return []string{"character"}, true
}

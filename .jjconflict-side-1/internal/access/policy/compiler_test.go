// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/dsl"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// helper to build a schema with registered namespaces.
func schemaWith(namespaces map[string]map[string]types.AttrType) *types.AttributeSchema {
	s := types.NewAttributeSchema()
	for ns, attrs := range namespaces {
		_ = s.Register(ns, &types.NamespaceSchema{Attributes: attrs})
	}
	return s
}

// emptySchema returns a schema with no registered namespaces.
func emptySchema() *types.AttributeSchema {
	return types.NewAttributeSchema()
}

// --- JSON round-trip (serialization test — written first per risk note) ---

func TestCompiledPolicy_JSONRoundTrip(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	policy, warnings, err := compiler.Compile(`permit(principal, action, resource) when { resource.owner == principal.id };`)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.NotNil(t, policy.Conditions)

	data, err := json.Marshal(policy.Conditions)
	require.NoError(t, err)

	var roundTripped dsl.ConditionBlock
	err = json.Unmarshal(data, &roundTripped)
	require.NoError(t, err)

	assert.Len(t, roundTripped.Disjunctions, 1)
	assert.Len(t, roundTripped.Disjunctions[0].Conditions, 1)
	cond := roundTripped.Disjunctions[0].Conditions[0]
	require.NotNil(t, cond.Comparison, "expected comparison condition after round-trip")
	assert.Equal(t, "==", cond.Comparison.Comparator)
}

// --- Valid compilation ---

func TestCompile_SimplePermitPolicy(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	policy, warnings, err := compiler.Compile(`permit(principal, action, resource);`)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	assert.Equal(t, dsl.GrammarVersion, policy.GrammarVersion)
	assert.Equal(t, types.PolicyEffectPermit, policy.Effect)
	assert.Nil(t, policy.Target.PrincipalType)
	assert.Nil(t, policy.Target.ActionList)
	assert.Nil(t, policy.Target.ResourceType)
	assert.Nil(t, policy.Target.ResourceExact)
	assert.Nil(t, policy.Conditions)
	assert.Contains(t, policy.DSLText, "permit")
}

func TestCompile_ForbidPolicy(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	policy, warnings, err := compiler.Compile(`forbid(principal, action, resource);`)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, types.PolicyEffectForbid, policy.Effect)
}

func TestCompile_PolicyWithConditions(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	policy, warnings, err := compiler.Compile(`permit(principal, action, resource) when { resource.owner == principal.id };`)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	assert.Equal(t, types.PolicyEffectPermit, policy.Effect)
	assert.NotNil(t, policy.Conditions)
}

func TestCompile_MultipleActions(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	policy, warnings, err := compiler.Compile(`permit(principal, action in ["read", "write"], resource);`)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	require.NotNil(t, policy.Target.ActionList)
	assert.Equal(t, []string{"read", "write"}, policy.Target.ActionList)
}

func TestCompile_ResourceEquality(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	policy, warnings, err := compiler.Compile(`permit(principal, action, resource == "location:01ABC");`)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	assert.Nil(t, policy.Target.ResourceType)
	require.NotNil(t, policy.Target.ResourceExact)
	assert.Equal(t, "location:01ABC", *policy.Target.ResourceExact)
}

func TestCompile_PrincipalType(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	policy, warnings, err := compiler.Compile(`permit(principal is character, action, resource);`)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	require.NotNil(t, policy.Target.PrincipalType)
	assert.Equal(t, "character", *policy.Target.PrincipalType)
}

func TestCompile_ResourceType(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	policy, warnings, err := compiler.Compile(`permit(principal, action, resource is location);`)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	require.NotNil(t, policy.Target.ResourceType)
	assert.Equal(t, "location", *policy.Target.ResourceType)
	assert.Nil(t, policy.Target.ResourceExact)
}

func TestCompile_WildcardAllTargets(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	policy, warnings, err := compiler.Compile(`permit(principal, action, resource);`)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	assert.Nil(t, policy.Target.PrincipalType)
	assert.Nil(t, policy.Target.ActionList)
	assert.Nil(t, policy.Target.ResourceType)
	assert.Nil(t, policy.Target.ResourceExact)
}

// --- Validation errors ---

func TestCompile_EmptyDSL(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	_, _, err := compiler.Compile("")
	assert.Error(t, err)
}

func TestCompile_InvalidDSL(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	_, _, err := compiler.Compile("not a valid policy")
	assert.Error(t, err)
}

func TestCompile_UnregisteredActionAttribute(t *testing.T) {
	schema := schemaWith(map[string]map[string]types.AttrType{
		"action": {
			"name": types.AttrTypeString,
		},
	})
	compiler := NewCompiler(schema)

	// action.name is registered — should succeed
	_, warnings, err := compiler.Compile(`permit(principal, action, resource) when { action.name == "read" };`)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	// action.bogus is NOT registered but namespace is — should error
	_, _, err = compiler.Compile(`permit(principal, action, resource) when { action.bogus == "read" };`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "action.bogus")
}

// --- Validation warnings ---

func TestCompile_UnknownAttributeInRegisteredNamespace(t *testing.T) {
	schema := schemaWith(map[string]map[string]types.AttrType{
		"resource": {
			"owner": types.AttrTypeString,
		},
	})
	compiler := NewCompiler(schema)

	// resource.bogus is NOT registered — should produce warning (not error) for non-action namespace
	policy, warnings, err := compiler.Compile(`permit(principal, action, resource) when { resource.bogus == "test" };`)
	require.NoError(t, err)
	require.NotNil(t, policy)
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0].Message, "resource.bogus")
}

func TestCompile_UnregisteredNamespaceSkipsValidation(t *testing.T) {
	// principal namespace not registered — attributes should not produce warnings
	schema := schemaWith(map[string]map[string]types.AttrType{
		"resource": {
			"owner": types.AttrTypeString,
		},
	})
	compiler := NewCompiler(schema)

	policy, warnings, err := compiler.Compile(`permit(principal, action, resource) when { principal.role == "admin" };`)
	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Empty(t, warnings)
}

func TestCompile_UnreachableCondition(t *testing.T) {
	// NOTE: The parser has a known issue where @('true'|'false') on *bool
	// always captures as true, regardless of the actual token. This means
	falseStr := "false"
	cb := &dsl.ConditionBlock{
		Disjunctions: []*dsl.Conjunction{{
			Conditions: []*dsl.Condition{
				{BoolLiteral: &falseStr},
				{Comparison: &dsl.Comparison{
					Left:       &dsl.Expr{Literal: &dsl.Literal{Str: strPtr("a")}},
					Comparator: "==",
					Right:      &dsl.Expr{Literal: &dsl.Literal{Str: strPtr("b")}},
				}},
			},
		}},
	}

	warnings := detectConditionWarnings(cb)
	require.NotEmpty(t, warnings)

	found := false
	for _, w := range warnings {
		if strings.Contains(w.Message, "unreachable") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected 'unreachable' warning, got: %v", warnings)
}

func strPtr(s string) *string { return &s }

func TestCompile_AlwaysTrueCondition(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	policy, warnings, err := compiler.Compile(`permit(principal, action, resource) when { true };`)
	require.NoError(t, err)
	require.NotNil(t, policy)
	require.NotEmpty(t, warnings)

	found := false
	for _, w := range warnings {
		if strings.Contains(w.Message, "always true") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected 'always true' warning, got: %v", warnings)
}

// --- Glob pre-compilation ---

func TestCompile_GlobCachePopulated(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	policy, warnings, err := compiler.Compile(`permit(principal, action, resource) when { resource.id like "location:*" };`)
	require.NoError(t, err)
	assert.Empty(t, warnings)

	require.NotNil(t, policy.GlobCache)
	_, exists := policy.GlobCache["location:*"]
	assert.True(t, exists, "expected GlobCache to contain 'location:*'")
}

func TestCompile_GlobPatternWithBrackets(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	_, _, err := compiler.Compile(`permit(principal, action, resource) when { resource.id like "loc[a-z]tion:*" };`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bracket")
}

func TestCompile_GlobPatternWithBraces(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	_, _, err := compiler.Compile(`permit(principal, action, resource) when { resource.id like "loc{a,b}tion:*" };`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "brace")
}

func TestCompile_GlobPatternGlobstar(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	_, _, err := compiler.Compile(`permit(principal, action, resource) when { resource.id like "location:**" };`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "globstar")
}

func TestCompile_GlobPatternTooLong(t *testing.T) {
	longPattern := strings.Repeat("a", 101)
	dslText := `permit(principal, action, resource) when { resource.id like "` + longPattern + `" };`
	compiler := NewCompiler(emptySchema())
	_, _, err := compiler.Compile(dslText)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "length")
}

func TestCompile_GlobPatternTooManyWildcards(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	_, _, err := compiler.Compile(`permit(principal, action, resource) when { resource.id like "a*b*c*d*e*f*" };`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wildcard")
}

// --- Concurrency safety ---

func TestCompile_ConcurrentSafety(t *testing.T) {
	compiler := NewCompiler(emptySchema())
	const goroutines = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make([]error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			_, _, errs[idx] = compiler.Compile(`permit(principal, action, resource) when { resource.owner == principal.id };`)
		}(i)
	}

	wg.Wait()
	for i, err := range errs {
		assert.NoError(t, err, "goroutine %d failed", i)
	}
}

// --- DSLText preserved ---

func TestCompile_DSLTextPreserved(t *testing.T) {
	dslText := `permit(principal is character, action in ["read"], resource is location) when { resource.owner == principal.id };`
	compiler := NewCompiler(emptySchema())
	policy, _, err := compiler.Compile(dslText)
	require.NoError(t, err)
	assert.Equal(t, dslText, policy.DSLText)
}

// --- Attribute validation in various condition types ---

func TestCompile_ValidatesAttributesInHasCondition(t *testing.T) {
	schema := schemaWith(map[string]map[string]types.AttrType{
		"resource": {
			"owner": types.AttrTypeString,
		},
	})
	compiler := NewCompiler(schema)

	// "has" uses root + path, e.g. resource has bogus → resource.bogus
	policy, warnings, err := compiler.Compile(`permit(principal, action, resource) when { resource has bogus };`)
	require.NoError(t, err)
	require.NotNil(t, policy)
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0].Message, "resource.bogus")
}

func TestCompile_ValidatesAttributesInLikeCondition(t *testing.T) {
	schema := schemaWith(map[string]map[string]types.AttrType{
		"action": {
			"name": types.AttrTypeString,
		},
	})
	compiler := NewCompiler(schema)

	// action.bogus in like condition → compile error (action namespace)
	_, _, err := compiler.Compile(`permit(principal, action, resource) when { action.bogus like "read*" };`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "action.bogus")
}

func TestCompile_ValidatesAttributesInContainsCondition(t *testing.T) {
	schema := schemaWith(map[string]map[string]types.AttrType{
		"resource": {
			"tags": types.AttrTypeStringList,
		},
	})
	compiler := NewCompiler(schema)

	// resource.bogus in contains → warning (resource namespace, not action)
	policy, warnings, err := compiler.Compile(`permit(principal, action, resource) when { resource.bogus.containsAll(["tag1"]) };`)
	require.NoError(t, err)
	require.NotNil(t, policy)
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0].Message, "resource.bogus")
}

func TestCompile_ValidatesFullDottedPath(t *testing.T) {
	schema := schemaWith(map[string]map[string]types.AttrType{
		"resource": {
			"metadata.owner": types.AttrTypeString,
		},
	})
	compiler := NewCompiler(schema)

	// resource.metadata.owner is registered with full dotted key — should succeed
	policy, warnings, err := compiler.Compile(`permit(principal, action, resource) when { resource.metadata.owner == principal.id };`)
	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Empty(t, warnings)

	// resource.metadata.bogus is NOT registered — should warn with full dotted path
	policy, warnings, err = compiler.Compile(`permit(principal, action, resource) when { resource.metadata.bogus == "test" };`)
	require.NoError(t, err)
	require.NotNil(t, policy)
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0].Message, "resource.metadata.bogus")
}

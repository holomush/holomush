// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl_test

import (
	"testing"

	"github.com/holomush/holomush/internal/access/policy/dsl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_SeedPolicies(t *testing.T) {
	seeds := []struct {
		name string
		dsl  string
	}{
		{"self-read character", `permit(principal is character, action in ["read", "write"], resource is character) when { resource.id == principal.id };`},
		{"read location by location", `permit(principal is character, action in ["read"], resource is location) when { resource.id == principal.location };`},
		{"read character same location", `permit(principal is character, action in ["read"], resource is character) when { resource.location == principal.location };`},
		{"read object same location", `permit(principal is character, action in ["read"], resource is object) when { resource.location == principal.location };`},
		{"emit to location stream", `permit(principal is character, action in ["emit"], resource is stream) when { resource.name like "location:*" && resource.location == principal.location };`},
		{"enter location", `permit(principal is character, action in ["enter"], resource is location);`},
		{"use exit", `permit(principal is character, action in ["use"], resource is exit);`},
		{"execute basic commands", `permit(principal is character, action in ["execute"], resource is command) when { resource.name in ["say", "pose", "look", "go"] };`},
		{"builder write/delete location", `permit(principal is character, action in ["write", "delete"], resource is location) when { principal.role in ["builder", "admin"] };`},
		{"builder write/delete object", `permit(principal is character, action in ["write", "delete"], resource is object) when { principal.role in ["builder", "admin"] };`},
		{"builder execute commands", `permit(principal is character, action in ["execute"], resource is command) when { principal.role in ["builder", "admin"] && resource.name in ["dig", "create", "describe", "link"] };`},
		{"admin wildcard", `permit(principal is character, action, resource) when { principal.role == "admin" };`},
		{"public property read", `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "public" && principal.location == resource.parent_location };`},
		{"private property read", `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "private" && resource.owner == principal.id };`},
		{"admin property read", `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "admin" && principal.role == "admin" };`},
		{"property owner write", `permit(principal is character, action in ["write", "delete"], resource is property) when { resource.owner == principal.id };`},
		{"restricted property visible_to", `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "restricted" && resource has visible_to && principal.id in resource.visible_to };`},
		{"restricted property excluded_from", `forbid(principal is character, action in ["read"], resource is property) when { resource.visibility == "restricted" && resource has excluded_from && principal.id in resource.excluded_from };`},
	}

	for _, tt := range seeds {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := dsl.Parse(tt.dsl)
			require.NoError(t, err, "seed policy should parse: %s", tt.dsl)
			require.NotNil(t, policy)

			// Verify round-trip via String()
			rendered := policy.String()
			reparsed, err := dsl.Parse(rendered)
			require.NoError(t, err, "round-trip should parse: %s", rendered)
			assert.Equal(t, policy.Effect, reparsed.Effect)
		})
	}
}

func TestParse_Operators(t *testing.T) {
	tests := []struct {
		name string
		dsl  string
	}{
		{"equals", `permit(principal, action, resource) when { principal.role == "admin" };`},
		{"not equals", `permit(principal, action, resource) when { principal.role != "guest" };`},
		{"greater than", `permit(principal, action, resource) when { principal.level > 5 };`},
		{"greater or equal", `permit(principal, action, resource) when { principal.level >= 5 };`},
		{"less than", `permit(principal, action, resource) when { principal.level < 10 };`},
		{"less or equal", `permit(principal, action, resource) when { principal.level <= 10 };`},
		{"in list", `permit(principal, action, resource) when { resource.name in ["say", "pose"] };`},
		{"in expr", `permit(principal, action, resource) when { principal.id in resource.visible_to };`},
		{"like", `permit(principal, action, resource) when { resource.name like "location:*" };`},
		{"has simple", `permit(principal, action, resource) when { principal has faction };`},
		{"has dotted", `permit(principal, action, resource) when { resource has metadata.tags };`},
		{"containsAll", `permit(principal, action, resource) when { principal.flags.containsAll(["vip", "beta"]) };`},
		{"containsAny", `permit(principal, action, resource) when { principal.flags.containsAny(["admin", "builder"]) };`},
		{"negation", `permit(principal, action, resource) when { !(principal.role == "banned") };`},
		{"and", `permit(principal, action, resource) when { principal.role == "admin" && resource.type == "location" };`},
		{"or", `permit(principal, action, resource) when { principal.role == "admin" || principal.role == "builder" };`},
		{"if-then-else", `permit(principal, action, resource) when { if principal has faction then principal.faction == resource.faction else true };`},
		{"resource exact match", `permit(principal, action, resource == "location:01XYZ") when { principal.role == "admin" };`},
		{"parenthesized", `permit(principal, action, resource) when { (principal.role == "admin") };`},
		{"bare true", `permit(principal, action, resource) when { true };`},
		{"bare false", `permit(principal, action, resource) when { false };`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := dsl.Parse(tt.dsl)
			require.NoError(t, err, "should parse: %s", tt.dsl)
			require.NotNil(t, policy)
		})
	}
}

func TestParse_InvalidPolicies(t *testing.T) {
	tests := []struct {
		name    string
		dsl     string
		errText string
	}{
		{"empty input", "", ""},
		{"missing semicolon", `permit(principal, action, resource)`, ""},
		{"unknown effect", `allow(principal, action, resource);`, ""},
		{"entity reference syntax", `permit(principal in Group::"admins", action, resource);`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dsl.Parse(tt.dsl)
			assert.Error(t, err, "should fail: %s", tt.dsl)
			if tt.errText != "" {
				assert.Contains(t, err.Error(), tt.errText)
			}
		})
	}
}

func TestParse_NestingDepthLimit(t *testing.T) {
	// Build a deeply nested expression: (((((...(true)...))))
	deep := "permit(principal, action, resource) when { "
	for range 33 {
		deep += "("
	}
	deep += "true"
	for range 33 {
		deep += ")"
	}
	deep += " };"

	_, err := dsl.Parse(deep)
	assert.Error(t, err, "nesting depth >32 should error")
}

func TestParse_StructuralChecks(t *testing.T) {
	t.Run("permit effect", func(t *testing.T) {
		p, err := dsl.Parse(`permit(principal is character, action in ["read"], resource is location);`)
		require.NoError(t, err)
		assert.Equal(t, "permit", p.Effect)
		assert.Equal(t, "character", p.Target.Principal.Type)
		assert.Equal(t, []string{"read"}, p.Target.Action.Actions)
		assert.Equal(t, "location", p.Target.Resource.Type)
		assert.Nil(t, p.Conditions)
	})

	t.Run("forbid effect", func(t *testing.T) {
		p, err := dsl.Parse(`forbid(principal, action, resource);`)
		require.NoError(t, err)
		assert.Equal(t, "forbid", p.Effect)
		assert.Empty(t, p.Target.Principal.Type)
	})

	t.Run("condition structure", func(t *testing.T) {
		p, err := dsl.Parse(`permit(principal, action, resource) when { principal.role == "admin" };`)
		require.NoError(t, err)
		require.NotNil(t, p.Conditions)
		require.Len(t, p.Conditions.Disjunctions, 1)
		conj := p.Conditions.Disjunctions[0]
		require.Len(t, conj.Conditions, 1)
		cond := conj.Conditions[0]
		require.NotNil(t, cond.Comparison)
		assert.Equal(t, "==", cond.Comparison.Comparator)
	})

	t.Run("disjunction structure", func(t *testing.T) {
		p, err := dsl.Parse(`permit(principal, action, resource) when { principal.role == "admin" || principal.role == "builder" };`)
		require.NoError(t, err)
		require.NotNil(t, p.Conditions)
		require.Len(t, p.Conditions.Disjunctions, 2)
	})

	t.Run("conjunction structure", func(t *testing.T) {
		p, err := dsl.Parse(`permit(principal, action, resource) when { principal.role == "admin" && resource.type == "location" };`)
		require.NoError(t, err)
		require.Len(t, p.Conditions.Disjunctions, 1)
		conj := p.Conditions.Disjunctions[0]
		require.Len(t, conj.Conditions, 2)
	})

	t.Run("has condition structure", func(t *testing.T) {
		p, err := dsl.Parse(`permit(principal, action, resource) when { principal has faction };`)
		require.NoError(t, err)
		cond := p.Conditions.Disjunctions[0].Conditions[0]
		require.NotNil(t, cond.Has)
		assert.Equal(t, "principal", cond.Has.Root)
		assert.Equal(t, []string{"faction"}, cond.Has.Path)
	})

	t.Run("like condition structure", func(t *testing.T) {
		p, err := dsl.Parse(`permit(principal, action, resource) when { resource.name like "location:*" };`)
		require.NoError(t, err)
		cond := p.Conditions.Disjunctions[0].Conditions[0]
		require.NotNil(t, cond.Like)
		assert.Equal(t, "location:*", cond.Like.Pattern)
	})

	t.Run("in list condition structure", func(t *testing.T) {
		p, err := dsl.Parse(`permit(principal, action, resource) when { resource.name in ["say", "pose", "look"] };`)
		require.NoError(t, err)
		cond := p.Conditions.Disjunctions[0].Conditions[0]
		require.NotNil(t, cond.InList)
		require.Len(t, cond.InList.List.Values, 3)
	})

	t.Run("in expr condition structure", func(t *testing.T) {
		p, err := dsl.Parse(`permit(principal, action, resource) when { principal.id in resource.visible_to };`)
		require.NoError(t, err)
		cond := p.Conditions.Disjunctions[0].Conditions[0]
		require.NotNil(t, cond.InExpr)
		require.NotNil(t, cond.InExpr.Right.AttrRef)
	})

	t.Run("if-then-else structure", func(t *testing.T) {
		p, err := dsl.Parse(`permit(principal, action, resource) when { if principal has faction then principal.faction == resource.faction else true };`)
		require.NoError(t, err)
		cond := p.Conditions.Disjunctions[0].Conditions[0]
		require.NotNil(t, cond.IfThenElse)
		require.NotNil(t, cond.IfThenElse.If.Has)
		require.NotNil(t, cond.IfThenElse.Then.Comparison)
		require.NotNil(t, cond.IfThenElse.Else.BoolLiteral)
		assert.True(t, *cond.IfThenElse.Else.BoolLiteral)
	})
}

func TestParse_ReservedWordAsAttribute(t *testing.T) {
	// Using a reserved word as an attribute segment should be rejected.
	// "permit" is reserved and should not appear as an attribute name.
	_, err := dsl.Parse(`permit(principal, action, resource) when { principal.permit == "x" };`)
	assert.Error(t, err, "reserved word as attribute should be rejected")
}

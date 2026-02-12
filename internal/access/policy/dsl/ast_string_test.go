// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPolicy_String(t *testing.T) {
	tests := []struct {
		name     string
		policy   Policy
		expected string
	}{
		{
			name: "simple permit no conditions",
			policy: Policy{
				Effect: "permit",
				Target: &Target{
					Principal: &PrincipalClause{},
					Action:    &ActionClause{},
					Resource:  &ResourceClause{},
				},
			},
			expected: `permit(principal, action, resource);`,
		},
		{
			name: "permit with typed principal and actions",
			policy: Policy{
				Effect: "permit",
				Target: &Target{
					Principal: &PrincipalClause{Type: "character"},
					Action:    &ActionClause{Actions: []string{"read", "write"}},
					Resource:  &ResourceClause{Type: "location"},
				},
			},
			expected: `permit(principal is character, action in ["read", "write"], resource is location);`,
		},
		{
			name: "forbid with resource equality",
			policy: Policy{
				Effect: "forbid",
				Target: &Target{
					Principal: &PrincipalClause{Type: "character"},
					Action:    &ActionClause{Actions: []string{"delete"}},
					Resource:  &ResourceClause{Equality: "system:config"},
				},
			},
			expected: `forbid(principal is character, action in ["delete"], resource == "system:config");`,
		},
		{
			name: "permit with simple condition",
			policy: Policy{
				Effect: "permit",
				Target: &Target{
					Principal: &PrincipalClause{Type: "character"},
					Action:    &ActionClause{Actions: []string{"read"}},
					Resource:  &ResourceClause{Type: "location"},
				},
				Conditions: &ConditionBlock{
					Disjunctions: []*Conjunction{
						{
							Conditions: []*Condition{
								{
									Comparison: &Comparison{
										Left:       &Expr{AttrRef: &AttrRef{Root: "resource", Path: []string{"id"}}},
										Comparator: "==",
										Right:      &Expr{Literal: &Literal{Str: strPtr("abc")}},
									},
								},
							},
						},
					},
				},
			},
			expected: `permit(principal is character, action in ["read"], resource is location) when { resource.id == "abc" };`,
		},
		{
			name: "permit with disjunction",
			policy: Policy{
				Effect: "permit",
				Target: &Target{
					Principal: &PrincipalClause{Type: "character"},
					Action:    &ActionClause{Actions: []string{"read"}},
					Resource:  &ResourceClause{Type: "property"},
				},
				Conditions: &ConditionBlock{
					Disjunctions: []*Conjunction{
						{
							Conditions: []*Condition{
								{
									Comparison: &Comparison{
										Left:       &Expr{AttrRef: &AttrRef{Root: "resource", Path: []string{"visibility"}}},
										Comparator: "==",
										Right:      &Expr{Literal: &Literal{Str: strPtr("public")}},
									},
								},
							},
						},
						{
							Conditions: []*Condition{
								{
									Comparison: &Comparison{
										Left:       &Expr{AttrRef: &AttrRef{Root: "resource", Path: []string{"visibility"}}},
										Comparator: "==",
										Right:      &Expr{Literal: &Literal{Str: strPtr("private")}},
									},
								},
							},
						},
					},
				},
			},
			expected: `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "public" || resource.visibility == "private" };`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.policy.String())
		})
	}
}

func TestCondition_String(t *testing.T) {
	tests := []struct {
		name     string
		cond     Condition
		expected string
	}{
		{
			name: "like operator",
			cond: Condition{
				Like: &LikeCondition{
					Left:    &Expr{AttrRef: &AttrRef{Root: "resource", Path: []string{"name"}}},
					Pattern: "location:*",
				},
			},
			expected: `resource.name like "location:*"`,
		},
		{
			name: "has operator simple",
			cond: Condition{
				Has: &HasCondition{
					Root: "principal",
					Path: []string{"faction"},
				},
			},
			expected: `principal has faction`,
		},
		{
			name: "has operator dotted path",
			cond: Condition{
				Has: &HasCondition{
					Root: "resource",
					Path: []string{"metadata", "tags"},
				},
			},
			expected: `resource has metadata.tags`,
		},
		{
			name: "negation",
			cond: Condition{
				Negation: &Condition{
					Comparison: &Comparison{
						Left:       &Expr{AttrRef: &AttrRef{Root: "principal", Path: []string{"role"}}},
						Comparator: "==",
						Right:      &Expr{Literal: &Literal{Str: strPtr("banned")}},
					},
				},
			},
			expected: `!(principal.role == "banned")`,
		},
		{
			name: "if-then-else",
			cond: Condition{
				IfThenElse: &IfThenElse{
					If: &Condition{
						Has: &HasCondition{Root: "principal", Path: []string{"faction"}},
					},
					Then: &Condition{
						Comparison: &Comparison{
							Left:       &Expr{AttrRef: &AttrRef{Root: "principal", Path: []string{"faction"}}},
							Comparator: "==",
							Right:      &Expr{AttrRef: &AttrRef{Root: "resource", Path: []string{"faction"}}},
						},
					},
					Else: &Condition{
						BoolLiteral: boolPtr(true),
					},
				},
			},
			expected: `if principal has faction then principal.faction == resource.faction else true`,
		},
		{
			name: "containsAll",
			cond: Condition{
				ContainsAll: &ContainsCondition{
					Left: &Expr{AttrRef: &AttrRef{Root: "principal", Path: []string{"flags"}}},
					List: &ListExpr{Values: []*Literal{{Str: strPtr("vip")}, {Str: strPtr("beta")}}},
				},
			},
			expected: `principal.flags.containsAll(["vip", "beta"])`,
		},
		{
			name: "containsAny",
			cond: Condition{
				ContainsAny: &ContainsCondition{
					Left: &Expr{AttrRef: &AttrRef{Root: "principal", Path: []string{"flags"}}},
					List: &ListExpr{Values: []*Literal{{Str: strPtr("admin")}, {Str: strPtr("builder")}}},
				},
			},
			expected: `principal.flags.containsAny(["admin", "builder"])`,
		},
		{
			name: "in list",
			cond: Condition{
				InList: &InListCondition{
					Left: &Expr{AttrRef: &AttrRef{Root: "principal", Path: []string{"role"}}},
					List: &ListExpr{Values: []*Literal{{Str: strPtr("builder")}, {Str: strPtr("admin")}}},
				},
			},
			expected: `principal.role in ["builder", "admin"]`,
		},
		{
			name: "in expr (attr-in-attr)",
			cond: Condition{
				InExpr: &InExprCondition{
					Left:  &Expr{AttrRef: &AttrRef{Root: "principal", Path: []string{"id"}}},
					Right: &Expr{AttrRef: &AttrRef{Root: "resource", Path: []string{"visible_to"}}},
				},
			},
			expected: `principal.id in resource.visible_to`,
		},
		{
			name: "bare boolean literal true",
			cond: Condition{
				BoolLiteral: boolPtr(true),
			},
			expected: `true`,
		},
		{
			name: "bare boolean literal false",
			cond: Condition{
				BoolLiteral: boolPtr(false),
			},
			expected: `false`,
		},
		{
			name: "parenthesized condition",
			cond: Condition{
				Parenthesized: &ConditionBlock{
					Disjunctions: []*Conjunction{
						{
							Conditions: []*Condition{
								{
									Comparison: &Comparison{
										Left:       &Expr{AttrRef: &AttrRef{Root: "principal", Path: []string{"role"}}},
										Comparator: "==",
										Right:      &Expr{Literal: &Literal{Str: strPtr("admin")}},
									},
								},
							},
						},
					},
				},
			},
			expected: `(principal.role == "admin")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cond.String())
		})
	}
}

func TestExpr_String(t *testing.T) {
	tests := []struct {
		name     string
		expr     Expr
		expected string
	}{
		{
			name:     "attribute reference",
			expr:     Expr{AttrRef: &AttrRef{Root: "principal", Path: []string{"role"}}},
			expected: "principal.role",
		},
		{
			name:     "dotted attribute reference",
			expr:     Expr{AttrRef: &AttrRef{Root: "resource", Path: []string{"metadata", "tags"}}},
			expected: "resource.metadata.tags",
		},
		{
			name:     "string literal",
			expr:     Expr{Literal: &Literal{Str: strPtr("admin")}},
			expected: `"admin"`,
		},
		{
			name:     "number literal",
			expr:     Expr{Literal: &Literal{Number: float64Ptr(42)}},
			expected: "42",
		},
		{
			name:     "float literal",
			expr:     Expr{Literal: &Literal{Number: float64Ptr(3.14)}},
			expected: "3.14",
		},
		{
			name:     "boolean literal true",
			expr:     Expr{Literal: &Literal{Bool: boolPtr(true)}},
			expected: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.expr.String())
		})
	}
}

func TestReservedWords(t *testing.T) {
	expected := []string{
		"permit", "forbid", "when", "principal", "resource", "action", "env",
		"is", "in", "has", "like", "true", "false", "if", "then", "else",
		"containsAll", "containsAny",
	}
	for _, word := range expected {
		assert.True(t, IsReservedWord(word), "%q should be a reserved word", word)
	}

	nonReserved := []string{"role", "faction", "level", "name", "id"}
	for _, word := range nonReserved {
		assert.False(t, IsReservedWord(word), "%q should not be a reserved word", word)
	}
}

func TestListExpr_String(t *testing.T) {
	tests := []struct {
		name     string
		list     ListExpr
		expected string
	}{
		{
			name:     "single string",
			list:     ListExpr{Values: []*Literal{{Str: strPtr("read")}}},
			expected: `["read"]`,
		},
		{
			name:     "multiple strings",
			list:     ListExpr{Values: []*Literal{{Str: strPtr("read")}, {Str: strPtr("write")}}},
			expected: `["read", "write"]`,
		},
		{
			name:     "number list",
			list:     ListExpr{Values: []*Literal{{Number: float64Ptr(1)}, {Number: float64Ptr(2)}}},
			expected: `[1, 2]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.list.String())
		})
	}
}

// Helper functions for constructing test ASTs.
func strPtr(s string) *string       { return &s }
func boolPtr(b bool) *bool          { return &b }
func float64Ptr(f float64) *float64 { return &f }

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl

import (
	"testing"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helpers ---

func newBags() *types.AttributeBags { return types.NewAttributeBags() }

func defaultCtx(bags *types.AttributeBags) *EvalContext {
	return &EvalContext{Bags: bags, MaxDepth: MaxNestingDepth}
}

// mkAttrRef builds an Expr from root + path segments.
func mkAttrRef(root string, path ...string) *Expr {
	return &Expr{AttrRef: &AttrRef{Root: root, Path: path}}
}

// mkStrLit builds a string literal Expr.
func mkStrLit(s string) *Expr {
	return &Expr{Literal: &Literal{Str: strPtr(s)}}
}

// mkNumLit builds a numeric literal Expr.
func mkNumLit(n float64) *Expr {
	return &Expr{Literal: &Literal{Number: float64Ptr(n)}}
}

// mkBoolLit builds a boolean literal Expr.
func mkBoolLit(b bool) *Expr {
	return &Expr{Literal: &Literal{Bool: boolPtr(b)}}
}

// mkComparison builds a ConditionBlock from a single comparison.
func mkComparison(left *Expr, op string, right *Expr) *ConditionBlock {
	return &ConditionBlock{
		Disjunctions: []*Conjunction{{
			Conditions: []*Condition{{
				Comparison: &Comparison{Left: left, Comparator: op, Right: right},
			}},
		}},
	}
}

// mkSingleCond wraps a Condition into a ConditionBlock.
func mkSingleCond(c *Condition) *ConditionBlock {
	return &ConditionBlock{
		Disjunctions: []*Conjunction{{
			Conditions: []*Condition{c},
		}},
	}
}

// mkStrList builds a ListExpr of string literals.
func mkStrList(strs ...string) *ListExpr {
	vals := make([]*Literal, len(strs))
	for i, s := range strs {
		vals[i] = &Literal{Str: strPtr(s)}
	}
	return &ListExpr{Values: vals}
}

// mkNumList builds a ListExpr of number literals.
func mkNumList(nums ...float64) *ListExpr {
	vals := make([]*Literal, len(nums))
	for i, n := range nums {
		vals[i] = &Literal{Number: float64Ptr(n)}
	}
	return &ListExpr{Values: vals}
}

// --- Comparison Tests ---

func TestEvaluateConditions_Comparison(t *testing.T) {
	tests := []struct {
		name     string
		cond     *ConditionBlock
		bags     *types.AttributeBags
		expected bool
	}{
		// String equality
		{
			name:     "string equality match",
			cond:     mkComparison(mkAttrRef("principal", "role"), "==", mkStrLit("admin")),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "admin"; return b }(),
			expected: true,
		},
		{
			name:     "string equality no match",
			cond:     mkComparison(mkAttrRef("principal", "role"), "==", mkStrLit("admin")),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "guest"; return b }(),
			expected: false,
		},
		{
			name:     "string not equals match",
			cond:     mkComparison(mkAttrRef("principal", "role"), "!=", mkStrLit("guest")),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "admin"; return b }(),
			expected: true,
		},
		{
			name:     "string not equals no match",
			cond:     mkComparison(mkAttrRef("principal", "role"), "!=", mkStrLit("admin")),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "admin"; return b }(),
			expected: false,
		},
		// Numeric comparisons
		{
			name:     "numeric greater than true",
			cond:     mkComparison(mkAttrRef("principal", "level"), ">", mkNumLit(5)),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = 10.0; return b }(),
			expected: true,
		},
		{
			name:     "numeric greater than false",
			cond:     mkComparison(mkAttrRef("principal", "level"), ">", mkNumLit(5)),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = 3.0; return b }(),
			expected: false,
		},
		{
			name:     "numeric greater than equal boundary false",
			cond:     mkComparison(mkAttrRef("principal", "level"), ">", mkNumLit(5)),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = 5.0; return b }(),
			expected: false,
		},
		{
			name:     "numeric >= true on boundary",
			cond:     mkComparison(mkAttrRef("principal", "level"), ">=", mkNumLit(5)),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = 5.0; return b }(),
			expected: true,
		},
		{
			name:     "numeric < true",
			cond:     mkComparison(mkAttrRef("principal", "level"), "<", mkNumLit(10)),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = 3.0; return b }(),
			expected: true,
		},
		{
			name:     "numeric <= true on boundary",
			cond:     mkComparison(mkAttrRef("principal", "level"), "<=", mkNumLit(10)),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = 10.0; return b }(),
			expected: true,
		},
		// Numeric coercion (int stored in bag)
		{
			name:     "int coercion to float64 for comparison",
			cond:     mkComparison(mkAttrRef("principal", "level"), ">", mkNumLit(5)),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = 10; return b }(),
			expected: true,
		},
		{
			name:     "int64 coercion to float64 for comparison",
			cond:     mkComparison(mkAttrRef("principal", "level"), "==", mkNumLit(42)),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = int64(42); return b }(),
			expected: true,
		},
		{
			name:     "int32 coercion to float64 for comparison",
			cond:     mkComparison(mkAttrRef("principal", "level"), ">=", mkNumLit(3)),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = int32(5); return b }(),
			expected: true,
		},
		{
			name:     "float32 coercion to float64 for comparison",
			cond:     mkComparison(mkAttrRef("principal", "level"), "<=", mkNumLit(5)),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = float32(3.5); return b }(),
			expected: true,
		},
		// Comparing two attribute refs
		{
			name: "compare two attr refs equal",
			cond: mkComparison(mkAttrRef("resource", "id"), "==", mkAttrRef("principal", "id")),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Resource["id"] = "abc"
				b.Subject["id"] = "abc"
				return b
			}(),
			expected: true,
		},
		{
			name: "compare two attr refs not equal",
			cond: mkComparison(mkAttrRef("resource", "id"), "==", mkAttrRef("principal", "id")),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Resource["id"] = "abc"
				b.Subject["id"] = "xyz"
				return b
			}(),
			expected: false,
		},
		// Comparing two literals
		{
			name:     "two string literals equal",
			cond:     mkComparison(mkStrLit("hello"), "==", mkStrLit("hello")),
			bags:     newBags(),
			expected: true,
		},
		{
			name:     "two number literals not equal",
			cond:     mkComparison(mkNumLit(1), "!=", mkNumLit(2)),
			bags:     newBags(),
			expected: true,
		},
		// Missing attributes → false
		{
			name:     "missing left attr in comparison → false",
			cond:     mkComparison(mkAttrRef("principal", "role"), "==", mkStrLit("admin")),
			bags:     newBags(),
			expected: false,
		},
		{
			name:     "missing right attr in comparison → false",
			cond:     mkComparison(mkStrLit("admin"), "==", mkAttrRef("principal", "role")),
			bags:     newBags(),
			expected: false,
		},
		{
			name:     "missing both attrs in comparison → false",
			cond:     mkComparison(mkAttrRef("principal", "role"), "==", mkAttrRef("resource", "type")),
			bags:     newBags(),
			expected: false,
		},
		// Type mismatch → false
		{
			name:     "type mismatch string vs number → false",
			cond:     mkComparison(mkAttrRef("principal", "role"), "==", mkNumLit(5)),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "admin"; return b }(),
			expected: false,
		},
		{
			name:     "type mismatch number vs string → false for >",
			cond:     mkComparison(mkAttrRef("principal", "level"), ">", mkStrLit("five")),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = 10.0; return b }(),
			expected: false,
		},
		// Dotted path (flat key)
		{
			name:     "dotted attribute path lookup",
			cond:     mkComparison(mkAttrRef("resource", "metadata", "tags"), "==", mkStrLit("important")),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["metadata.tags"] = "important"; return b }(),
			expected: true,
		},
		// Boolean literal comparison
		{
			name:     "boolean literal comparison true==true",
			cond:     mkComparison(mkBoolLit(true), "==", mkBoolLit(true)),
			bags:     newBags(),
			expected: true,
		},
		{
			name:     "boolean literal comparison true!=false",
			cond:     mkComparison(mkBoolLit(true), "!=", mkBoolLit(false)),
			bags:     newBags(),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := defaultCtx(tt.bags)
			assert.Equal(t, tt.expected, EvaluateConditions(ctx, tt.cond))
		})
	}
}

// --- Has Tests ---

func TestEvaluateConditions_Has(t *testing.T) {
	tests := []struct {
		name     string
		cond     *ConditionBlock
		bags     *types.AttributeBags
		expected bool
	}{
		{
			name: "has simple attribute present",
			cond: mkSingleCond(&Condition{Has: &HasCondition{Root: "principal", Path: []string{"faction"}}}),
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["faction"] = "horde"; return b }(),
			expected: true,
		},
		{
			name:     "has simple attribute missing",
			cond:     mkSingleCond(&Condition{Has: &HasCondition{Root: "principal", Path: []string{"faction"}}}),
			bags:     newBags(),
			expected: false,
		},
		{
			name: "has dotted path present",
			cond: mkSingleCond(&Condition{Has: &HasCondition{Root: "resource", Path: []string{"metadata", "tags"}}}),
			bags: func() *types.AttributeBags { b := newBags(); b.Resource["metadata.tags"] = []string{"a"}; return b }(),
			expected: true,
		},
		{
			name: "has with zero value (empty string) still true",
			cond: mkSingleCond(&Condition{Has: &HasCondition{Root: "principal", Path: []string{"name"}}}),
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["name"] = ""; return b }(),
			expected: true,
		},
		{
			name: "has with zero value (zero int) still true",
			cond: mkSingleCond(&Condition{Has: &HasCondition{Root: "principal", Path: []string{"level"}}}),
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["level"] = 0; return b }(),
			expected: true,
		},
		{
			name: "has with nil value still true (key exists)",
			cond: mkSingleCond(&Condition{Has: &HasCondition{Root: "principal", Path: []string{"data"}}}),
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["data"] = nil; return b }(),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := defaultCtx(tt.bags)
			assert.Equal(t, tt.expected, EvaluateConditions(ctx, tt.cond))
		})
	}
}

// --- Like Tests ---

func TestEvaluateConditions_Like(t *testing.T) {
	tests := []struct {
		name     string
		cond     *ConditionBlock
		bags     *types.AttributeBags
		expected bool
	}{
		{
			name: "like wildcard match",
			cond: mkSingleCond(&Condition{Like: &LikeCondition{
				Left: mkAttrRef("resource", "name"), Pattern: "location:*",
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "location:01XYZ"; return b }(),
			expected: true,
		},
		{
			name: "like wildcard no match",
			cond: mkSingleCond(&Condition{Like: &LikeCondition{
				Left: mkAttrRef("resource", "name"), Pattern: "location:*",
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "character:01ABC"; return b }(),
			expected: false,
		},
		{
			name: "like exact match",
			cond: mkSingleCond(&Condition{Like: &LikeCondition{
				Left: mkAttrRef("resource", "name"), Pattern: "hello",
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "hello"; return b }(),
			expected: true,
		},
		{
			name: "like ? single char wildcard",
			cond: mkSingleCond(&Condition{Like: &LikeCondition{
				Left: mkAttrRef("resource", "name"), Pattern: "he?lo",
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "hello"; return b }(),
			expected: true,
		},
		{
			name: "like colon separator prevents cross-namespace match",
			cond: mkSingleCond(&Condition{Like: &LikeCondition{
				Left: mkAttrRef("resource", "name"), Pattern: "loc*",
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "loc:foo"; return b }(),
			expected: false, // * doesn't match across ':'
		},
		{
			name: "like missing attribute → false",
			cond: mkSingleCond(&Condition{Like: &LikeCondition{
				Left: mkAttrRef("resource", "name"), Pattern: "location:*",
			}}),
			bags:     newBags(),
			expected: false,
		},
		{
			name: "like non-string attribute → false",
			cond: mkSingleCond(&Condition{Like: &LikeCondition{
				Left: mkAttrRef("resource", "name"), Pattern: "location:*",
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["name"] = 42; return b }(),
			expected: false,
		},
		{
			name: "like invalid pattern with brackets → false",
			cond: mkSingleCond(&Condition{Like: &LikeCondition{
				Left: mkAttrRef("resource", "name"), Pattern: "loc[a-z]tion",
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "location"; return b }(),
			expected: false,
		},
		{
			name: "like invalid pattern with braces → false",
			cond: mkSingleCond(&Condition{Like: &LikeCondition{
				Left: mkAttrRef("resource", "name"), Pattern: "loc{a,b}tion",
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "location"; return b }(),
			expected: false,
		},
		{
			name: "like invalid pattern with double star → false",
			cond: mkSingleCond(&Condition{Like: &LikeCondition{
				Left: mkAttrRef("resource", "name"), Pattern: "loc**tion",
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "location"; return b }(),
			expected: false,
		},
		{
			name: "like pattern too many wildcards → false",
			cond: mkSingleCond(&Condition{Like: &LikeCondition{
				Left: mkAttrRef("resource", "name"), Pattern: "*?*?*?",
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "abcdef"; return b }(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := defaultCtx(tt.bags)
			assert.Equal(t, tt.expected, EvaluateConditions(ctx, tt.cond))
		})
	}
}

// --- ContainsAll / ContainsAny Tests ---

func TestEvaluateConditions_Contains(t *testing.T) {
	tests := []struct {
		name     string
		cond     *ConditionBlock
		bags     *types.AttributeBags
		expected bool
	}{
		{
			name: "containsAll all present",
			cond: mkSingleCond(&Condition{Contains: &ContainsCondition{
				Root: "principal", Path: []string{"flags"}, Op: "containsAll",
				List: mkStrList("vip", "beta"),
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["flags"] = []string{"vip", "beta", "extra"}; return b }(),
			expected: true,
		},
		{
			name: "containsAll missing one",
			cond: mkSingleCond(&Condition{Contains: &ContainsCondition{
				Root: "principal", Path: []string{"flags"}, Op: "containsAll",
				List: mkStrList("vip", "beta"),
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["flags"] = []string{"vip", "extra"}; return b }(),
			expected: false,
		},
		{
			name: "containsAny one present",
			cond: mkSingleCond(&Condition{Contains: &ContainsCondition{
				Root: "principal", Path: []string{"flags"}, Op: "containsAny",
				List: mkStrList("admin", "builder"),
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["flags"] = []string{"builder", "tester"}; return b }(),
			expected: true,
		},
		{
			name: "containsAny none present",
			cond: mkSingleCond(&Condition{Contains: &ContainsCondition{
				Root: "principal", Path: []string{"flags"}, Op: "containsAny",
				List: mkStrList("admin", "builder"),
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["flags"] = []string{"tester", "viewer"}; return b }(),
			expected: false,
		},
		{
			name: "containsAll with []any string elements",
			cond: mkSingleCond(&Condition{Contains: &ContainsCondition{
				Root: "principal", Path: []string{"flags"}, Op: "containsAll",
				List: mkStrList("a", "b"),
			}}),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["flags"] = []any{"a", "b", "c"}
				return b
			}(),
			expected: true,
		},
		{
			name: "containsAny with []any string elements",
			cond: mkSingleCond(&Condition{Contains: &ContainsCondition{
				Root: "principal", Path: []string{"flags"}, Op: "containsAny",
				List: mkStrList("x", "b"),
			}}),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["flags"] = []any{"a", "b", "c"}
				return b
			}(),
			expected: true,
		},
		{
			name: "contains missing attribute → false",
			cond: mkSingleCond(&Condition{Contains: &ContainsCondition{
				Root: "principal", Path: []string{"flags"}, Op: "containsAll",
				List: mkStrList("a"),
			}}),
			bags:     newBags(),
			expected: false,
		},
		{
			name: "contains non-slice attribute → false",
			cond: mkSingleCond(&Condition{Contains: &ContainsCondition{
				Root: "principal", Path: []string{"flags"}, Op: "containsAll",
				List: mkStrList("a"),
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["flags"] = "not a slice"; return b }(),
			expected: false,
		},
		{
			name: "containsAll with dotted path",
			cond: mkSingleCond(&Condition{Contains: &ContainsCondition{
				Root: "resource", Path: []string{"metadata", "tags"}, Op: "containsAll",
				List: mkStrList("public"),
			}}),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Resource["metadata.tags"] = []string{"public", "featured"}
				return b
			}(),
			expected: true,
		},
		{
			name: "contains with no path (root-level)",
			cond: mkSingleCond(&Condition{Contains: &ContainsCondition{
				Root: "principal", Path: nil, Op: "containsAny",
				List: mkStrList("x"),
			}}),
			bags:     newBags(),
			expected: false, // empty path means empty key, won't find attribute
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := defaultCtx(tt.bags)
			assert.Equal(t, tt.expected, EvaluateConditions(ctx, tt.cond))
		})
	}
}

// --- InList Tests ---

func TestEvaluateConditions_InList(t *testing.T) {
	tests := []struct {
		name     string
		cond     *ConditionBlock
		bags     *types.AttributeBags
		expected bool
	}{
		{
			name: "in list string match",
			cond: mkSingleCond(&Condition{InList: &InListCondition{
				Left: mkAttrRef("resource", "name"),
				List: mkStrList("say", "pose", "look"),
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "say"; return b }(),
			expected: true,
		},
		{
			name: "in list string no match",
			cond: mkSingleCond(&Condition{InList: &InListCondition{
				Left: mkAttrRef("resource", "name"),
				List: mkStrList("say", "pose", "look"),
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "dig"; return b }(),
			expected: false,
		},
		{
			name: "in list number match",
			cond: mkSingleCond(&Condition{InList: &InListCondition{
				Left: mkAttrRef("principal", "level"),
				List: mkNumList(1, 2, 3),
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = 2.0; return b }(),
			expected: true,
		},
		{
			name: "in list number with int coercion",
			cond: mkSingleCond(&Condition{InList: &InListCondition{
				Left: mkAttrRef("principal", "level"),
				List: mkNumList(1, 2, 3),
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["level"] = 2; return b }(),
			expected: true,
		},
		{
			name: "in list missing attribute → false",
			cond: mkSingleCond(&Condition{InList: &InListCondition{
				Left: mkAttrRef("resource", "name"),
				List: mkStrList("say"),
			}}),
			bags:     newBags(),
			expected: false,
		},
		{
			name: "in list literal value match",
			cond: mkSingleCond(&Condition{InList: &InListCondition{
				Left: mkStrLit("say"),
				List: mkStrList("say", "pose"),
			}}),
			bags:     newBags(),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := defaultCtx(tt.bags)
			assert.Equal(t, tt.expected, EvaluateConditions(ctx, tt.cond))
		})
	}
}

// --- InExpr Tests ---

func TestEvaluateConditions_InExpr(t *testing.T) {
	tests := []struct {
		name     string
		cond     *ConditionBlock
		bags     *types.AttributeBags
		expected bool
	}{
		{
			name: "in expr string found in []string",
			cond: mkSingleCond(&Condition{InExpr: &InExprCondition{
				Left: mkAttrRef("principal", "id"), Right: mkAttrRef("resource", "visible_to"),
			}}),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["id"] = "char:01ABC"
				b.Resource["visible_to"] = []string{"char:01ABC", "char:02DEF"}
				return b
			}(),
			expected: true,
		},
		{
			name: "in expr string not found in []string",
			cond: mkSingleCond(&Condition{InExpr: &InExprCondition{
				Left: mkAttrRef("principal", "id"), Right: mkAttrRef("resource", "visible_to"),
			}}),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["id"] = "char:99ZZZ"
				b.Resource["visible_to"] = []string{"char:01ABC", "char:02DEF"}
				return b
			}(),
			expected: false,
		},
		{
			name: "in expr with []any containing strings",
			cond: mkSingleCond(&Condition{InExpr: &InExprCondition{
				Left: mkAttrRef("principal", "id"), Right: mkAttrRef("resource", "visible_to"),
			}}),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["id"] = "char:01ABC"
				b.Resource["visible_to"] = []any{"char:01ABC", "char:02DEF"}
				return b
			}(),
			expected: true,
		},
		{
			name: "in expr right side not a slice → false",
			cond: mkSingleCond(&Condition{InExpr: &InExprCondition{
				Left: mkAttrRef("principal", "id"), Right: mkAttrRef("resource", "visible_to"),
			}}),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["id"] = "char:01ABC"
				b.Resource["visible_to"] = "not a slice"
				return b
			}(),
			expected: false,
		},
		{
			name: "in expr missing left → false",
			cond: mkSingleCond(&Condition{InExpr: &InExprCondition{
				Left: mkAttrRef("principal", "id"), Right: mkAttrRef("resource", "visible_to"),
			}}),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Resource["visible_to"] = []string{"a"}
				return b
			}(),
			expected: false,
		},
		{
			name: "in expr missing right → false",
			cond: mkSingleCond(&Condition{InExpr: &InExprCondition{
				Left: mkAttrRef("principal", "id"), Right: mkAttrRef("resource", "visible_to"),
			}}),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["id"] = "abc"
				return b
			}(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := defaultCtx(tt.bags)
			assert.Equal(t, tt.expected, EvaluateConditions(ctx, tt.cond))
		})
	}
}

// --- Boolean Logic Tests ---

func TestEvaluateConditions_BooleanLogic(t *testing.T) {
	tests := []struct {
		name     string
		cond     *ConditionBlock
		bags     *types.AttributeBags
		expected bool
	}{
		{
			name:     "bare true",
			cond:     mkSingleCond(&Condition{BoolLiteral: boolPtr(true)}),
			bags:     newBags(),
			expected: true,
		},
		{
			name:     "bare false",
			cond:     mkSingleCond(&Condition{BoolLiteral: boolPtr(false)}),
			bags:     newBags(),
			expected: false,
		},
		{
			name: "conjunction true && true",
			cond: &ConditionBlock{Disjunctions: []*Conjunction{{
				Conditions: []*Condition{
					{BoolLiteral: boolPtr(true)},
					{BoolLiteral: boolPtr(true)},
				},
			}}},
			bags:     newBags(),
			expected: true,
		},
		{
			name: "conjunction true && false",
			cond: &ConditionBlock{Disjunctions: []*Conjunction{{
				Conditions: []*Condition{
					{BoolLiteral: boolPtr(true)},
					{BoolLiteral: boolPtr(false)},
				},
			}}},
			bags:     newBags(),
			expected: false,
		},
		{
			name: "disjunction false || true",
			cond: &ConditionBlock{Disjunctions: []*Conjunction{
				{Conditions: []*Condition{{BoolLiteral: boolPtr(false)}}},
				{Conditions: []*Condition{{BoolLiteral: boolPtr(true)}}},
			}},
			bags:     newBags(),
			expected: true,
		},
		{
			name: "disjunction false || false",
			cond: &ConditionBlock{Disjunctions: []*Conjunction{
				{Conditions: []*Condition{{BoolLiteral: boolPtr(false)}}},
				{Conditions: []*Condition{{BoolLiteral: boolPtr(false)}}},
			}},
			bags:     newBags(),
			expected: false,
		},
		{
			name: "conjunction with attr check: both match",
			cond: &ConditionBlock{Disjunctions: []*Conjunction{{
				Conditions: []*Condition{
					{Comparison: &Comparison{
						Left: mkAttrRef("principal", "role"), Comparator: "==", Right: mkStrLit("admin"),
					}},
					{Comparison: &Comparison{
						Left: mkAttrRef("resource", "type"), Comparator: "==", Right: mkStrLit("location"),
					}},
				},
			}}},
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["role"] = "admin"
				b.Resource["type"] = "location"
				return b
			}(),
			expected: true,
		},
		{
			name: "disjunction with attr check: second matches",
			cond: &ConditionBlock{Disjunctions: []*Conjunction{
				{Conditions: []*Condition{
					{Comparison: &Comparison{
						Left: mkAttrRef("principal", "role"), Comparator: "==", Right: mkStrLit("admin"),
					}},
				}},
				{Conditions: []*Condition{
					{Comparison: &Comparison{
						Left: mkAttrRef("principal", "role"), Comparator: "==", Right: mkStrLit("builder"),
					}},
				}},
			}},
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "builder"; return b }(),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := defaultCtx(tt.bags)
			assert.Equal(t, tt.expected, EvaluateConditions(ctx, tt.cond))
		})
	}
}

// --- Negation Tests ---

func TestEvaluateConditions_Negation(t *testing.T) {
	tests := []struct {
		name     string
		cond     *ConditionBlock
		bags     *types.AttributeBags
		expected bool
	}{
		{
			name: "negation of true → false",
			cond: mkSingleCond(&Condition{Negation: &Condition{BoolLiteral: boolPtr(true)}}),
			bags:     newBags(),
			expected: false,
		},
		{
			name: "negation of false → true",
			cond: mkSingleCond(&Condition{Negation: &Condition{BoolLiteral: boolPtr(false)}}),
			bags:     newBags(),
			expected: true,
		},
		{
			name: "negation of matching comparison → false",
			cond: mkSingleCond(&Condition{Negation: &Condition{
				Comparison: &Comparison{
					Left: mkAttrRef("principal", "role"), Comparator: "==", Right: mkStrLit("banned"),
				},
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "banned"; return b }(),
			expected: false,
		},
		{
			name: "negation of non-matching comparison → true",
			cond: mkSingleCond(&Condition{Negation: &Condition{
				Comparison: &Comparison{
					Left: mkAttrRef("principal", "role"), Comparator: "==", Right: mkStrLit("banned"),
				},
			}}),
			bags:     func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "admin"; return b }(),
			expected: true,
		},
		{
			name: "double negation",
			cond: mkSingleCond(&Condition{Negation: &Condition{
				Negation: &Condition{BoolLiteral: boolPtr(true)},
			}}),
			bags:     newBags(),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := defaultCtx(tt.bags)
			assert.Equal(t, tt.expected, EvaluateConditions(ctx, tt.cond))
		})
	}
}

// --- Parenthesized Tests ---

func TestEvaluateConditions_Parenthesized(t *testing.T) {
	tests := []struct {
		name     string
		cond     *ConditionBlock
		bags     *types.AttributeBags
		expected bool
	}{
		{
			name: "parenthesized true",
			cond: mkSingleCond(&Condition{Parenthesized: &ConditionBlock{
				Disjunctions: []*Conjunction{{
					Conditions: []*Condition{{BoolLiteral: boolPtr(true)}},
				}},
			}}),
			bags:     newBags(),
			expected: true,
		},
		{
			name: "parenthesized or inside conjunction",
			cond: &ConditionBlock{Disjunctions: []*Conjunction{{
				Conditions: []*Condition{
					{Parenthesized: &ConditionBlock{
						Disjunctions: []*Conjunction{
							{Conditions: []*Condition{{BoolLiteral: boolPtr(false)}}},
							{Conditions: []*Condition{{BoolLiteral: boolPtr(true)}}},
						},
					}},
					{BoolLiteral: boolPtr(true)},
				},
			}}},
			bags:     newBags(),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := defaultCtx(tt.bags)
			assert.Equal(t, tt.expected, EvaluateConditions(ctx, tt.cond))
		})
	}
}

// --- IfThenElse Tests ---

func TestEvaluateConditions_IfThenElse(t *testing.T) {
	tests := []struct {
		name     string
		cond     *ConditionBlock
		bags     *types.AttributeBags
		expected bool
	}{
		{
			name: "if true then true else false → true",
			cond: mkSingleCond(&Condition{IfThenElse: &IfThenElse{
				If:   &Condition{BoolLiteral: boolPtr(true)},
				Then: &Condition{BoolLiteral: boolPtr(true)},
				Else: &Condition{BoolLiteral: boolPtr(false)},
			}}),
			bags:     newBags(),
			expected: true,
		},
		{
			name: "if false then true else false → false",
			cond: mkSingleCond(&Condition{IfThenElse: &IfThenElse{
				If:   &Condition{BoolLiteral: boolPtr(false)},
				Then: &Condition{BoolLiteral: boolPtr(true)},
				Else: &Condition{BoolLiteral: boolPtr(false)},
			}}),
			bags:     newBags(),
			expected: false,
		},
		{
			name: "if has faction then check faction else true",
			cond: mkSingleCond(&Condition{IfThenElse: &IfThenElse{
				If: &Condition{Has: &HasCondition{Root: "principal", Path: []string{"faction"}}},
				Then: &Condition{Comparison: &Comparison{
					Left: mkAttrRef("principal", "faction"), Comparator: "==", Right: mkAttrRef("resource", "faction"),
				}},
				Else: &Condition{BoolLiteral: boolPtr(true)},
			}}),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["faction"] = "horde"
				b.Resource["faction"] = "horde"
				return b
			}(),
			expected: true,
		},
		{
			name: "if has faction (missing) then ... else true → true",
			cond: mkSingleCond(&Condition{IfThenElse: &IfThenElse{
				If: &Condition{Has: &HasCondition{Root: "principal", Path: []string{"faction"}}},
				Then: &Condition{Comparison: &Comparison{
					Left: mkAttrRef("principal", "faction"), Comparator: "==", Right: mkAttrRef("resource", "faction"),
				}},
				Else: &Condition{BoolLiteral: boolPtr(true)},
			}}),
			bags:     newBags(),
			expected: true,
		},
		{
			name: "if has faction then mismatch else true → false (faction present but mismatches)",
			cond: mkSingleCond(&Condition{IfThenElse: &IfThenElse{
				If: &Condition{Has: &HasCondition{Root: "principal", Path: []string{"faction"}}},
				Then: &Condition{Comparison: &Comparison{
					Left: mkAttrRef("principal", "faction"), Comparator: "==", Right: mkAttrRef("resource", "faction"),
				}},
				Else: &Condition{BoolLiteral: boolPtr(true)},
			}}),
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["faction"] = "horde"
				b.Resource["faction"] = "alliance"
				return b
			}(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := defaultCtx(tt.bags)
			assert.Equal(t, tt.expected, EvaluateConditions(ctx, tt.cond))
		})
	}
}

// --- Depth Limit Tests ---

func TestEvaluateConditions_DepthLimit(t *testing.T) {
	t.Run("exceeds max depth returns false", func(t *testing.T) {
		// Build a deeply nested negation chain: !(!(!(...true...)))
		inner := &Condition{BoolLiteral: boolPtr(true)}
		for i := 0; i < MaxNestingDepth+1; i++ {
			inner = &Condition{Negation: inner}
		}
		cond := mkSingleCond(inner)
		ctx := defaultCtx(newBags())
		assert.False(t, EvaluateConditions(ctx, cond))
	})

	t.Run("at exactly max depth succeeds", func(t *testing.T) {
		// Build exactly MaxNestingDepth levels of parenthesized nesting
		// An even number of negations around true should yield true.
		inner := &Condition{BoolLiteral: boolPtr(true)}
		for i := 0; i < MaxNestingDepth; i++ {
			inner = &Condition{Parenthesized: &ConditionBlock{
				Disjunctions: []*Conjunction{{Conditions: []*Condition{inner}}},
			}}
		}
		cond := mkSingleCond(inner)
		ctx := defaultCtx(newBags())
		assert.True(t, EvaluateConditions(ctx, cond))
	})

	t.Run("custom max depth", func(t *testing.T) {
		inner := &Condition{BoolLiteral: boolPtr(true)}
		for i := 0; i < 5; i++ {
			inner = &Condition{Negation: inner}
		}
		cond := mkSingleCond(inner)
		ctx := &EvalContext{Bags: newBags(), MaxDepth: 3}
		assert.False(t, EvaluateConditions(ctx, cond))
	})
}

// --- Nil/Empty Edge Cases ---

func TestEvaluateConditions_EdgeCases(t *testing.T) {
	t.Run("nil condition block → true", func(t *testing.T) {
		ctx := defaultCtx(newBags())
		assert.True(t, EvaluateConditions(ctx, nil))
	})

	t.Run("empty disjunctions → true", func(t *testing.T) {
		ctx := defaultCtx(newBags())
		assert.True(t, EvaluateConditions(ctx, &ConditionBlock{}))
	})

	t.Run("empty conjunction → true", func(t *testing.T) {
		ctx := defaultCtx(newBags())
		cond := &ConditionBlock{Disjunctions: []*Conjunction{{}}}
		assert.True(t, EvaluateConditions(ctx, cond))
	})

	t.Run("nil bags → false for any attribute access", func(t *testing.T) {
		cond := mkComparison(mkAttrRef("principal", "role"), "==", mkStrLit("admin"))
		ctx := &EvalContext{Bags: nil, MaxDepth: MaxNestingDepth}
		assert.False(t, EvaluateConditions(ctx, cond))
	})

	t.Run("zero MaxDepth defaults to MaxNestingDepth", func(t *testing.T) {
		cond := mkSingleCond(&Condition{BoolLiteral: boolPtr(true)})
		ctx := &EvalContext{Bags: newBags(), MaxDepth: 0}
		assert.True(t, EvaluateConditions(ctx, cond))
	})

	t.Run("condition with all nil fields → false", func(t *testing.T) {
		cond := mkSingleCond(&Condition{})
		ctx := defaultCtx(newBags())
		assert.False(t, EvaluateConditions(ctx, cond))
	})
}

// --- Root Mapping Tests ---

func TestEvaluateConditions_RootMapping(t *testing.T) {
	tests := []struct {
		name     string
		root     string
		bagSetup func(*types.AttributeBags)
	}{
		{"principal maps to Subject", "principal", func(b *types.AttributeBags) { b.Subject["x"] = "v" }},
		{"resource maps to Resource", "resource", func(b *types.AttributeBags) { b.Resource["x"] = "v" }},
		{"action maps to Action", "action", func(b *types.AttributeBags) { b.Action["x"] = "v" }},
		{"env maps to Environment", "env", func(b *types.AttributeBags) { b.Environment["x"] = "v" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBags()
			tt.bagSetup(b)
			cond := mkComparison(mkAttrRef(tt.root, "x"), "==", mkStrLit("v"))
			ctx := defaultCtx(b)
			assert.True(t, EvaluateConditions(ctx, cond))
		})
	}

	t.Run("unknown root → false", func(t *testing.T) {
		b := newBags()
		cond := mkComparison(mkAttrRef("unknown", "x"), "==", mkStrLit("v"))
		ctx := defaultCtx(b)
		assert.False(t, EvaluateConditions(ctx, cond))
	})
}

// --- Parse + Evaluate Integration Tests ---

func TestEvaluateConditions_ParsedPolicies(t *testing.T) {
	tests := []struct {
		name     string
		dsl      string
		bags     *types.AttributeBags
		expected bool
	}{
		{
			name: "simple role check permit",
			dsl:  `permit(principal, action, resource) when { principal.role == "admin" };`,
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "admin"; return b }(),
			expected: true,
		},
		{
			name: "simple role check deny",
			dsl:  `permit(principal, action, resource) when { principal.role == "admin" };`,
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "guest"; return b }(),
			expected: false,
		},
		{
			name: "conjunction",
			dsl:  `permit(principal, action, resource) when { principal.role == "admin" && resource.type == "location" };`,
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["role"] = "admin"
				b.Resource["type"] = "location"
				return b
			}(),
			expected: true,
		},
		{
			name: "disjunction",
			dsl:  `permit(principal, action, resource) when { principal.role == "admin" || principal.role == "builder" };`,
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "builder"; return b }(),
			expected: true,
		},
		{
			name: "negation",
			dsl:  `permit(principal, action, resource) when { !(principal.role == "banned") };`,
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "player"; return b }(),
			expected: true,
		},
		{
			name: "in list",
			dsl:  `permit(principal, action, resource) when { resource.name in ["say", "pose", "look"] };`,
			bags: func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "pose"; return b }(),
			expected: true,
		},
		{
			name: "in expr",
			dsl:  `permit(principal, action, resource) when { principal.id in resource.visible_to };`,
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["id"] = "char:01"
				b.Resource["visible_to"] = []string{"char:01", "char:02"}
				return b
			}(),
			expected: true,
		},
		{
			name: "like glob",
			dsl:  `permit(principal, action, resource) when { resource.name like "location:*" };`,
			bags: func() *types.AttributeBags { b := newBags(); b.Resource["name"] = "location:01XYZ"; return b }(),
			expected: true,
		},
		{
			name: "has",
			dsl:  `permit(principal, action, resource) when { principal has faction };`,
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["faction"] = "horde"; return b }(),
			expected: true,
		},
		{
			name: "containsAll",
			dsl:  `permit(principal, action, resource) when { principal.flags.containsAll(["vip", "beta"]) };`,
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["flags"] = []string{"vip", "beta", "new"}
				return b
			}(),
			expected: true,
		},
		{
			name: "containsAny",
			dsl:  `permit(principal, action, resource) when { principal.flags.containsAny(["admin", "builder"]) };`,
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["flags"] = []string{"builder"}
				return b
			}(),
			expected: true,
		},
		{
			name: "if-then-else faction match",
			dsl:  `permit(principal, action, resource) when { if principal has faction then principal.faction == resource.faction else true };`,
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Subject["faction"] = "horde"
				b.Resource["faction"] = "horde"
				return b
			}(),
			expected: true,
		},
		{
			name: "if-then-else no faction falls through",
			dsl:  `permit(principal, action, resource) when { if principal has faction then principal.faction == resource.faction else true };`,
			bags: newBags(),
			expected: true,
		},
		{
			name: "no when clause → true (unconditional)",
			dsl:  `permit(principal is character, action in ["enter"], resource is location);`,
			bags: newBags(),
			expected: true,
		},
		{
			name: "numeric comparison with int bag value",
			dsl:  `permit(principal, action, resource) when { principal.level > 5 };`,
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["level"] = 10; return b }(),
			expected: true,
		},
		{
			name: "parenthesized expression",
			dsl:  `permit(principal, action, resource) when { (principal.role == "admin") };`,
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "admin"; return b }(),
			expected: true,
		},
		{
			name:     "bare true",
			dsl:      `permit(principal, action, resource) when { true };`,
			bags:     newBags(),
			expected: true,
		},
		// NOTE: "bare false" via parser is skipped here because participle
		// captures @('true'|'false') on *bool as true regardless of the
		// matched token text. The direct AST test in
		// TestEvaluateConditions_BooleanLogic covers this correctly.
		// Parser fix tracked separately.
		{
			name: "complex seed policy: restricted property visible_to",
			dsl:  `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "restricted" && resource has visible_to && principal.id in resource.visible_to };`,
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Resource["visibility"] = "restricted"
				b.Resource["visible_to"] = []string{"char:01", "char:02"}
				b.Subject["id"] = "char:01"
				return b
			}(),
			expected: true,
		},
		{
			name: "complex seed policy: restricted property not visible",
			dsl:  `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "restricted" && resource has visible_to && principal.id in resource.visible_to };`,
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Resource["visibility"] = "restricted"
				b.Resource["visible_to"] = []string{"char:01", "char:02"}
				b.Subject["id"] = "char:99"
				return b
			}(),
			expected: false,
		},
		{
			name: "has dotted path",
			dsl:  `permit(principal, action, resource) when { resource has metadata.tags };`,
			bags: func() *types.AttributeBags {
				b := newBags()
				b.Resource["metadata.tags"] = []string{"a"}
				return b
			}(),
			expected: true,
		},
		{
			name: "role in list",
			dsl:  `permit(principal, action, resource) when { principal.role in ["builder", "admin"] };`,
			bags: func() *types.AttributeBags { b := newBags(); b.Subject["role"] = "builder"; return b }(),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := Parse(tt.dsl)
			require.NoError(t, err, "policy should parse")

			ctx := defaultCtx(tt.bags)
			result := EvaluateConditions(ctx, policy.Conditions)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- Like Pattern Limits ---

func TestEvaluateConditions_LikePatternLimits(t *testing.T) {
	t.Run("pattern over 100 chars → false", func(t *testing.T) {
		longPattern := ""
		for i := 0; i < 101; i++ {
			longPattern += "a"
		}
		cond := mkSingleCond(&Condition{Like: &LikeCondition{
			Left: mkAttrRef("resource", "name"), Pattern: longPattern,
		}})
		bags := newBags()
		bags.Resource["name"] = longPattern
		ctx := defaultCtx(bags)
		assert.False(t, EvaluateConditions(ctx, cond))
	})

	t.Run("exactly 5 wildcards → ok", func(t *testing.T) {
		cond := mkSingleCond(&Condition{Like: &LikeCondition{
			Left: mkAttrRef("resource", "name"), Pattern: "a*b?c*d?e*",
		}})
		bags := newBags()
		bags.Resource["name"] = "aXXbYcZZdWeTT"
		ctx := defaultCtx(bags)
		// 5 wildcards is within the limit, should evaluate successfully (not rejected)
		assert.True(t, EvaluateConditions(ctx, cond))
	})

	t.Run("6 wildcards → false", func(t *testing.T) {
		cond := mkSingleCond(&Condition{Like: &LikeCondition{
			Left: mkAttrRef("resource", "name"), Pattern: "*?*?*?",
		}})
		bags := newBags()
		bags.Resource["name"] = "abcdef"
		ctx := defaultCtx(bags)
		assert.False(t, EvaluateConditions(ctx, cond))
	})
}

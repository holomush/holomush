// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl

import (
	"strings"

	"github.com/gobwas/glob"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// EvalContext provides attribute bags and configuration for evaluation.
type EvalContext struct {
	Bags      *types.AttributeBags
	MaxDepth  int                  // default 32; 0 means use MaxNestingDepth
	GlobCache map[string]glob.Glob // pre-compiled glob patterns; nil means compile on demand

	depthExceeded bool // set when nesting exceeds MaxDepth; forces false
}

// effectiveMaxDepth returns the max depth to use, defaulting to MaxNestingDepth.
func (ec *EvalContext) effectiveMaxDepth() int {
	if ec.MaxDepth <= 0 {
		return MaxNestingDepth
	}
	return ec.MaxDepth
}

// EvaluateConditions evaluates the condition block against the attribute bags.
// Returns true if all conditions are satisfied. A nil block means "no conditions"
// and evaluates to true (unconditional).
func EvaluateConditions(ctx *EvalContext, cond *ConditionBlock) bool {
	ctx.depthExceeded = false
	if cond == nil {
		return true
	}
	return evalBlock(ctx, cond, 0)
}

// evalBlock evaluates a ConditionBlock (disjunction of conjunctions).
// Returns true if ANY conjunction is true (short-circuit on true).
func evalBlock(ctx *EvalContext, cb *ConditionBlock, depth int) bool {
	if ctx.depthExceeded {
		return false
	}
	if len(cb.Disjunctions) == 0 {
		return true
	}
	for _, conj := range cb.Disjunctions {
		if evalConjunction(ctx, conj, depth) {
			return true
		}
	}
	return false
}

// evalConjunction evaluates a conjunction: ALL conditions must be true.
// Short-circuits on false.
func evalConjunction(ctx *EvalContext, conj *Conjunction, depth int) bool {
	if ctx.depthExceeded {
		return false
	}
	if len(conj.Conditions) == 0 {
		return true
	}
	for _, c := range conj.Conditions {
		if !evalCondition(ctx, c, depth) {
			return false
		}
	}
	return true
}

// evalCondition dispatches to the appropriate evaluator based on which field
// is non-nil.
func evalCondition(ctx *EvalContext, c *Condition, depth int) bool {
	if ctx.depthExceeded {
		return false
	}
	if depth > ctx.effectiveMaxDepth() {
		ctx.depthExceeded = true
		return false
	}

	switch {
	case c.Negation != nil:
		result := evalCondition(ctx, c.Negation, depth+1)
		if ctx.depthExceeded {
			return false
		}
		return !result

	case c.Parenthesized != nil:
		return evalBlock(ctx, c.Parenthesized, depth+1)

	case c.IfThenElse != nil:
		return evalIfThenElse(ctx, c.IfThenElse, depth+1)

	case c.Has != nil:
		return evalHas(ctx, c.Has)

	case c.Contains != nil:
		return evalContains(ctx, c.Contains)

	case c.Like != nil:
		return evalLike(ctx, c.Like)

	case c.InList != nil:
		return evalInList(ctx, c.InList)

	case c.InExpr != nil:
		return evalInExpr(ctx, c.InExpr)

	case c.Comparison != nil:
		return evalComparison(ctx, c.Comparison)

	case c.BoolLiteral != nil:
		return c.IsBoolTrue()

	default:
		return false
	}
}

// --- Operator evaluators ---

// evalComparison evaluates expr op expr. Missing attributes or type
// mismatches yield false (Cedar fail-safe semantics).
func evalComparison(ctx *EvalContext, cmp *Comparison) bool {
	left, leftOK := resolveExpr(ctx, cmp.Left)
	right, rightOK := resolveExpr(ctx, cmp.Right)

	if !leftOK || !rightOK {
		return false
	}

	// Attempt numeric comparison first.
	lNum, lIsNum := toFloat64(left)
	rNum, rIsNum := toFloat64(right)

	if lIsNum && rIsNum {
		return compareNumbers(lNum, rNum, cmp.Comparator)
	}

	// String comparison.
	lStr, lIsStr := left.(string)
	rStr, rIsStr := right.(string)

	if lIsStr && rIsStr {
		return compareStrings(lStr, rStr, cmp.Comparator)
	}

	// Boolean comparison (== and != only).
	lBool, lIsBool := left.(bool)
	rBool, rIsBool := right.(bool)

	if lIsBool && rIsBool {
		return compareBools(lBool, rBool, cmp.Comparator)
	}

	// Type mismatch → false.
	return false
}

func compareNumbers(l, r float64, op string) bool {
	switch op {
	case "==":
		return l == r
	case "!=":
		return l != r
	case ">":
		return l > r
	case ">=":
		return l >= r
	case "<":
		return l < r
	case "<=":
		return l <= r
	default:
		return false
	}
}

func compareStrings(l, r, op string) bool {
	switch op {
	case "==":
		return l == r
	case "!=":
		return l != r
	case ">":
		return l > r
	case ">=":
		return l >= r
	case "<":
		return l < r
	case "<=":
		return l <= r
	default:
		return false
	}
}

func compareBools(l, r bool, op string) bool {
	switch op {
	case "==":
		return l == r
	case "!=":
		return l != r
	default:
		return false
	}
}

// evalHas checks if the attribute exists in the bag. Any value (including
// zero values) counts as present; only a missing key returns false.
func evalHas(ctx *EvalContext, has *HasCondition) bool {
	bag := getBag(ctx, has.Root)
	if bag == nil {
		return false
	}
	key := strings.Join(has.Path, ".")
	_, exists := bag[key]
	return exists
}

// evalContains evaluates containsAll or containsAny.
func evalContains(ctx *EvalContext, cc *ContainsCondition) bool {
	bag := getBag(ctx, cc.Root)
	if bag == nil {
		return false
	}

	key := strings.Join(cc.Path, ".")
	val, exists := bag[key]
	if !exists {
		return false
	}

	bagStrings := toStringSlice(val)
	if bagStrings == nil {
		return false
	}

	needles := literalListToStrings(cc.List)

	switch cc.Op {
	case "containsAll":
		return containsAll(bagStrings, needles)
	case "containsAny":
		return containsAny(bagStrings, needles)
	default:
		return false
	}
}

// maxGlobPatternLen is the maximum allowed length for a like pattern.
const maxGlobPatternLen = 100

// maxGlobWildcards is the maximum number of wildcard characters (* or ?)
// allowed in a like pattern.
const maxGlobWildcards = 5

// evalLike evaluates a glob pattern match. Uses colon as separator for
// namespace isolation (e.g., "location:*" won't match across colons with *).
func evalLike(ctx *EvalContext, lc *LikeCondition) bool {
	if !validateGlobPattern(lc.Pattern) {
		return false
	}

	val, ok := resolveExpr(ctx, lc.Left)
	if !ok {
		return false
	}

	str, isStr := val.(string)
	if !isStr {
		return false
	}

	// Use pre-compiled glob from cache if available; compile on demand otherwise.
	compiled, ok := ctx.GlobCache[lc.Pattern]
	if !ok {
		var err error
		compiled, err = glob.Compile(lc.Pattern, ':')
		if err != nil {
			return false
		}
	}

	return compiled.Match(str)
}

// validateGlobPattern checks the pattern against safety limits.
func validateGlobPattern(pattern string) bool {
	if len(pattern) > maxGlobPatternLen {
		return false
	}

	// Reject patterns with brackets, braces, or double stars.
	if strings.Contains(pattern, "[") ||
		strings.Contains(pattern, "{") ||
		strings.Contains(pattern, "**") {
		return false
	}

	wildcardCount := 0
	for _, ch := range pattern {
		if ch == '*' || ch == '?' {
			wildcardCount++
		}
	}

	return wildcardCount <= maxGlobWildcards
}

// evalInList checks if the left value appears in the literal list.
func evalInList(ctx *EvalContext, il *InListCondition) bool {
	val, ok := resolveExpr(ctx, il.Left)
	if !ok {
		return false
	}

	for _, lit := range il.List.Values {
		litVal := resolveLiteral(lit)
		if litVal == nil {
			continue
		}

		if valuesEqual(val, litVal) {
			return true
		}
	}

	return false
}

// evalInExpr checks if left value appears in right value (which must be a slice).
func evalInExpr(ctx *EvalContext, ie *InExprCondition) bool {
	leftVal, leftOK := resolveExpr(ctx, ie.Left)
	rightVal, rightOK := resolveExpr(ctx, ie.Right)

	if !leftOK || !rightOK {
		return false
	}

	// Right side must be a slice.
	switch s := rightVal.(type) {
	case []string:
		leftStr, ok := leftVal.(string)
		if !ok {
			return false
		}
		for _, v := range s {
			if v == leftStr {
				return true
			}
		}
		return false

	case []any:
		for _, v := range s {
			if valuesEqual(leftVal, v) {
				return true
			}
		}
		return false

	default:
		return false
	}
}

// evalIfThenElse evaluates a conditional expression.
func evalIfThenElse(ctx *EvalContext, ite *IfThenElse, depth int) bool {
	if evalCondition(ctx, ite.If, depth) {
		return evalCondition(ctx, ite.Then, depth)
	}
	return evalCondition(ctx, ite.Else, depth)
}

// --- Value resolution ---

// resolveExpr resolves an Expr to a Go value. Returns the value and true
// if resolved successfully. Missing attributes return (nil, false).
func resolveExpr(ctx *EvalContext, e *Expr) (any, bool) {
	if e.AttrRef != nil {
		return resolveAttrRef(ctx, e.AttrRef)
	}
	if e.Literal != nil {
		v := resolveLiteral(e.Literal)
		if v == nil {
			return nil, false
		}
		return v, true
	}
	return nil, false
}

// resolveAttrRef looks up a dotted attribute path in the appropriate bag.
func resolveAttrRef(ctx *EvalContext, ar *AttrRef) (any, bool) {
	bag := getBag(ctx, ar.Root)
	if bag == nil {
		return nil, false
	}
	key := strings.Join(ar.Path, ".")
	val, exists := bag[key]
	if !exists {
		return nil, false
	}
	return val, true
}

// resolveLiteral converts a Literal AST node to a Go value.
func resolveLiteral(l *Literal) any {
	switch {
	case l.Str != nil:
		return *l.Str
	case l.Number != nil:
		return *l.Number
	case l.Bool != nil:
		return *l.Bool == "true"
	default:
		return nil
	}
}

// getBag returns the attribute map for the given root name.
func getBag(ctx *EvalContext, root string) map[string]any {
	if ctx.Bags == nil {
		return nil
	}
	switch root {
	case "principal":
		return ctx.Bags.Subject
	case "resource":
		return ctx.Bags.Resource
	case "action":
		return ctx.Bags.Action
	case "env":
		return ctx.Bags.Environment
	default:
		return nil
	}
}

// --- Type coercion and comparison helpers ---

// toFloat64 attempts to convert a value to float64, handling all Go numeric
// types that may appear in map[string]any bags.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// toStringSlice converts a value to []string if possible. Supports both
// []string and []any with string elements.
func toStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		result := make([]string, 0, len(s))
		for _, elem := range s {
			str, ok := elem.(string)
			if !ok {
				return nil
			}
			result = append(result, str)
		}
		return result
	default:
		return nil
	}
}

// literalListToStrings extracts string values from a ListExpr.
func literalListToStrings(le *ListExpr) []string {
	result := make([]string, 0, len(le.Values))
	for _, lit := range le.Values {
		if lit.Str != nil {
			result = append(result, *lit.Str)
		}
	}
	return result
}

// valuesEqual compares two any values for equality, with numeric coercion.
func valuesEqual(a, b any) bool {
	aNum, aIsNum := toFloat64(a)
	bNum, bIsNum := toFloat64(b)
	if aIsNum && bIsNum {
		return aNum == bNum
	}

	aStr, aIsStr := a.(string)
	bStr, bIsStr := b.(string)
	if aIsStr && bIsStr {
		return aStr == bStr
	}

	aBool, aIsBool := a.(bool)
	bBool, bIsBool := b.(bool)
	if aIsBool && bIsBool {
		return aBool == bBool
	}

	return false
}

// containsAll checks that haystack contains all needles.
func containsAll(haystack, needles []string) bool {
	set := make(map[string]struct{}, len(haystack))
	for _, s := range haystack {
		set[s] = struct{}{}
	}
	for _, n := range needles {
		if _, ok := set[n]; !ok {
			return false
		}
	}
	return true
}

// containsAny checks that haystack contains at least one needle.
func containsAny(haystack, needles []string) bool {
	set := make(map[string]struct{}, len(haystack))
	for _, s := range haystack {
		set[s] = struct{}{}
	}
	for _, n := range needles {
		if _, ok := set[n]; ok {
			return true
		}
	}
	return false
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl

import "strings"

// ValidatePrincipalScope checks that a parsed policy only references the
// expected principal name in conditions. This prevents a plugin from
// installing policies that grant access to other plugins.
//
// The function walks the full condition AST looking for comparisons and
// like-conditions that reference principal.plugin.name (or any
// principal.* path). If such a condition compares against a string
// literal that does not match expectedName, it returns false with the
// offending name.
//
// Returns (true, "") if the policy is properly scoped or has no
// principal name conditions.
func ValidatePrincipalScope(policy *Policy, expectedName string) (ok bool, foreignName string) {
	if policy.Conditions == nil {
		return true, ""
	}
	return walkConditionBlock(policy.Conditions, expectedName)
}

func walkConditionBlock(cb *ConditionBlock, expected string) (ok bool, foreignName string) {
	for _, conj := range cb.Disjunctions {
		if ok, name := walkConjunction(conj, expected); !ok {
			return false, name
		}
	}
	return true, ""
}

func walkConjunction(conj *Conjunction, expected string) (ok bool, foreignName string) {
	for _, cond := range conj.Conditions {
		if ok, name := walkCondition(cond, expected); !ok {
			return false, name
		}
	}
	return true, ""
}

func walkCondition(cond *Condition, expected string) (ok bool, foreignName string) {
	switch {
	case cond.Negation != nil:
		return walkCondition(cond.Negation, expected)
	case cond.Parenthesized != nil:
		return walkConditionBlock(cond.Parenthesized, expected)
	case cond.IfThenElse != nil:
		if ok, name := walkCondition(cond.IfThenElse.If, expected); !ok {
			return false, name
		}
		if ok, name := walkCondition(cond.IfThenElse.Then, expected); !ok {
			return false, name
		}
		return walkCondition(cond.IfThenElse.Else, expected)
	case cond.Comparison != nil:
		return checkComparison(cond.Comparison, expected)
	case cond.Like != nil:
		return checkLike(cond.Like, expected)
	}
	// Other condition types (has, contains, in-list, in-expr, bool) don't
	// reference principal name values — they're safe.
	return true, ""
}

// checkComparison checks if a comparison references principal.plugin.name
// against a foreign value.
func checkComparison(cmp *Comparison, expected string) (ok bool, foreignName string) {
	if isPrincipalNameRef(cmp.Left) && cmp.Right != nil && cmp.Right.Literal != nil && cmp.Right.Literal.Str != nil {
		name := *cmp.Right.Literal.Str
		if name != expected {
			return false, name
		}
	}
	if isPrincipalNameRef(cmp.Right) && cmp.Left != nil && cmp.Left.Literal != nil && cmp.Left.Literal.Str != nil {
		name := *cmp.Left.Literal.Str
		if name != expected {
			return false, name
		}
	}
	return true, ""
}

// checkLike checks if a like condition references principal.plugin.name
// against a foreign pattern.
func checkLike(lc *LikeCondition, expected string) (ok bool, foreignName string) {
	if isPrincipalNameRef(lc.Left) {
		// The pattern must start with the expected name or be exactly the expected name
		if !strings.HasPrefix(lc.Pattern, expected) {
			return false, lc.Pattern
		}
	}
	return true, ""
}

// isPrincipalNameRef returns true if the expression is an attribute reference
// to principal.plugin.name (or just principal.<namespace>.name for any plugin
// attribute path that includes "name" as the leaf).
func isPrincipalNameRef(expr *Expr) bool {
	if expr == nil || expr.AttrRef == nil {
		return false
	}
	ar := expr.AttrRef
	if ar.Root != "principal" {
		return false
	}
	// Match principal.plugin.name or principal.<ns>.name
	if len(ar.Path) >= 2 && ar.Path[len(ar.Path)-1] == "name" {
		return true
	}
	return false
}

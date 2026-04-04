// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl

// ReferencesResourceAttrs reports whether any condition in the block
// references a resource.* attribute path. This is used by CanPerformAction
// to distinguish subject-only conditions (definitively evaluable without a
// resource instance) from resource-dependent conditions (which must be
// treated optimistically during type-level pre-flight).
func ReferencesResourceAttrs(block *ConditionBlock) bool {
	if block == nil {
		return false
	}
	for _, disj := range block.Disjunctions {
		for _, cond := range disj.Conditions {
			if conditionRefsResource(cond) {
				return true
			}
		}
	}
	return false
}

func conditionRefsResource(c *Condition) bool {
	if c == nil {
		return false
	}
	if c.Negation != nil {
		return conditionRefsResource(c.Negation)
	}
	if c.Parenthesized != nil {
		return ReferencesResourceAttrs(c.Parenthesized)
	}
	if c.IfThenElse != nil {
		return conditionRefsResource(c.IfThenElse.If) ||
			conditionRefsResource(c.IfThenElse.Then) ||
			conditionRefsResource(c.IfThenElse.Else)
	}
	if c.Has != nil {
		return c.Has.Root == "resource"
	}
	if c.Contains != nil {
		return c.Contains.Root == "resource"
	}
	if c.Like != nil {
		return exprRefsResource(c.Like.Left)
	}
	if c.InList != nil {
		return exprRefsResource(c.InList.Left)
	}
	if c.InExpr != nil {
		return exprRefsResource(c.InExpr.Left) || exprRefsResource(c.InExpr.Right)
	}
	if c.Comparison != nil {
		return exprRefsResource(c.Comparison.Left) || exprRefsResource(c.Comparison.Right)
	}
	return false
}

func exprRefsResource(e *Expr) bool {
	if e == nil {
		return false
	}
	return e.AttrRef != nil && e.AttrRef.Root == "resource"
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl

// ExtractCommandNames extracts command name string literals from a condition
// block. It looks for comparisons like `resource.command.name == "widget"` and
// in-list conditions like `resource.command.name in ["say", "pose"]`.
//
// Returns nil when the block is nil or contains no command name references.
func ExtractCommandNames(block *ConditionBlock) []string {
	if block == nil {
		return nil
	}
	var names []string
	for _, disj := range block.Disjunctions {
		for _, cond := range disj.Conditions {
			names = extractFromCondition(cond, names)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func extractFromCondition(c *Condition, acc []string) []string {
	if c == nil {
		return acc
	}
	switch {
	case c.Negation != nil, c.Parenthesized != nil, c.IfThenElse != nil:
		// Negated, parenthesized, and branched (if/then/else) conditions are
		// NOT descended into. A literal like "widget" inside !(...) or
		// if/then/else does not constrain the policy to that command, so
		// extracting it would be unsafe for trust-boundary validation —
		// foreign-command execute policies could slip through if we treated
		// the literal as a constraint.
		return acc
	case c.Comparison != nil:
		return extractFromComparison(c.Comparison, acc)
	case c.InList != nil:
		return extractFromInList(c.InList, acc)
	}
	return acc
}

func extractFromComparison(cmp *Comparison, acc []string) []string {
	// resource.command.name == "widget"
	if isCommandNameRef(cmp.Left) && cmp.Right != nil && cmp.Right.Literal != nil && cmp.Right.Literal.Str != nil {
		return append(acc, *cmp.Right.Literal.Str)
	}
	// "widget" == resource.command.name
	if isCommandNameRef(cmp.Right) && cmp.Left != nil && cmp.Left.Literal != nil && cmp.Left.Literal.Str != nil {
		return append(acc, *cmp.Left.Literal.Str)
	}
	return acc
}

func extractFromInList(il *InListCondition, acc []string) []string {
	if !isCommandNameRef(il.Left) || il.List == nil {
		return acc
	}
	for _, lit := range il.List.Values {
		if lit.Str != nil {
			acc = append(acc, *lit.Str)
		}
	}
	return acc
}

func isCommandNameRef(e *Expr) bool {
	if e == nil || e.AttrRef == nil {
		return false
	}
	return e.AttrRef.Root == "resource" &&
		len(e.AttrRef.Path) == 2 &&
		e.AttrRef.Path[0] == "command" &&
		e.AttrRef.Path[1] == "name"
}

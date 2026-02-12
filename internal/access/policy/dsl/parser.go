// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl

import (
	"fmt"

	"github.com/alecthomas/participle/v2"
	"github.com/samber/oops"
)

// MaxNestingDepth is the maximum allowed nesting depth for conditions.
const MaxNestingDepth = 32

// parser is the singleton participle parser instance.
var parser *participle.Parser[Policy]

func init() {
	var err error
	parser, err = NewParser()
	if err != nil {
		panic(fmt.Sprintf("failed to build DSL parser: %v", err))
	}
}

// Parse parses a DSL policy string into an AST.
// Returns a descriptive error with position info on failure.
func Parse(dslText string) (*Policy, error) {
	policy, err := parser.ParseString("", dslText)
	if err != nil {
		return nil, oops.Wrapf(err, "parsing DSL policy")
	}

	if err := validatePolicy(policy); err != nil {
		return nil, err
	}

	return policy, nil
}

// validatePolicy performs post-parse validation checks.
func validatePolicy(p *Policy) error {
	if p.Conditions != nil {
		if err := validateConditionBlock(p.Conditions, 0); err != nil {
			return err
		}
	}
	return nil
}

// validateConditionBlock checks nesting depth and reserved words.
func validateConditionBlock(cb *ConditionBlock, depth int) error {
	if depth > MaxNestingDepth {
		return fmt.Errorf("nesting depth exceeds maximum of %d", MaxNestingDepth)
	}
	for _, conj := range cb.Disjunctions {
		for _, cond := range conj.Conditions {
			if err := validateCondition(cond, depth); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateCondition validates a single condition node.
func validateCondition(c *Condition, depth int) error {
	switch {
	case c.Negation != nil:
		return validateCondition(c.Negation, depth+1)
	case c.Parenthesized != nil:
		return validateConditionBlock(c.Parenthesized, depth+1)
	case c.IfThenElse != nil:
		if err := validateCondition(c.IfThenElse.If, depth+1); err != nil {
			return err
		}
		if err := validateCondition(c.IfThenElse.Then, depth+1); err != nil {
			return err
		}
		return validateCondition(c.IfThenElse.Else, depth+1)
	case c.Has != nil:
		return validateHasPaths(c.Has.Path)
	case c.Comparison != nil:
		return validateExprs(c.Comparison.Left, c.Comparison.Right)
	case c.Like != nil:
		return validateExpr(c.Like.Left)
	case c.InList != nil:
		return validateExpr(c.InList.Left)
	case c.InExpr != nil:
		return validateExprs(c.InExpr.Left, c.InExpr.Right)
	case c.Contains != nil:
		return validateHasPaths(c.Contains.Path)
	}
	return nil
}

func validateExprs(exprs ...*Expr) error {
	for _, e := range exprs {
		if err := validateExpr(e); err != nil {
			return err
		}
	}
	return nil
}

func validateExpr(e *Expr) error {
	if e.AttrRef != nil {
		return validateAttrPaths(e.AttrRef.Path)
	}
	return nil
}

func validateAttrPaths(path []string) error {
	for _, seg := range path {
		if IsReservedWord(seg) {
			return fmt.Errorf("reserved word %q cannot be used as an attribute name", seg)
		}
	}
	return nil
}

func validateHasPaths(path []string) error {
	for _, seg := range path {
		if IsReservedWord(seg) {
			return fmt.Errorf("reserved word %q cannot be used as an attribute name", seg)
		}
	}
	return nil
}

// ParseError wraps a parse error with additional context.
type ParseError struct {
	Line    int
	Column  int
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Message)
}


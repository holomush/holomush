// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package policy provides a compiler that parses, validates, and optimizes
// ABAC policy DSL text into an executable form.
package policy

import (
	"fmt"
	"strings"

	"github.com/gobwas/glob"

	"github.com/holomush/holomush/internal/access/policy/dsl"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// maxGlobPatternLen is the maximum allowed length for a like pattern.
const maxGlobPatternLen = 100

// maxGlobWildcards is the maximum number of wildcard characters (* or ?)
// allowed in a like pattern.
const maxGlobWildcards = 5

// Compiler parses and validates DSL policy text.
type Compiler struct {
	schema *types.AttributeSchema
}

// NewCompiler creates a Compiler with the given schema.
func NewCompiler(schema *types.AttributeSchema) *Compiler {
	return &Compiler{schema: schema}
}

// ValidationWarning is a non-blocking issue found during compilation.
type ValidationWarning struct {
	Message string
}

// CompiledPolicy is the parsed, validated, and optimized form of a policy.
type CompiledPolicy struct {
	GrammarVersion int                `json:"grammar_version"`
	Effect         types.PolicyEffect `json:"effect"`
	Target         CompiledTarget     `json:"target"`
	Conditions     *dsl.ConditionBlock `json:"conditions,omitempty"`
	GlobCache      map[string]glob.Glob `json:"-"`
	DSLText        string             `json:"dsl_text"`
}

// CompiledTarget is the parsed target clause.
type CompiledTarget struct {
	PrincipalType *string  `json:"principal_type,omitempty"`
	ActionList    []string `json:"action_list,omitempty"`
	ResourceType  *string  `json:"resource_type,omitempty"`
	ResourceExact *string  `json:"resource_exact,omitempty"`
}

// Compile parses DSL text, validates it, and returns a compiled policy.
func (c *Compiler) Compile(dslText string) (*CompiledPolicy, []ValidationWarning, error) {
	// Step 1: Parse
	parsed, err := dsl.Parse(dslText)
	if err != nil {
		return nil, nil, fmt.Errorf("parse error: %w", err)
	}

	// Step 2: Build CompiledTarget
	target := buildTarget(parsed.Target)

	// Step 3: Map effect
	effect, err := mapEffect(parsed.Effect)
	if err != nil {
		return nil, nil, err
	}

	// Step 4: Validate attributes and collect warnings
	var warnings []ValidationWarning
	if parsed.Conditions != nil {
		var attrErr error
		warnings, attrErr = c.validateAttributes(parsed.Conditions)
		if attrErr != nil {
			return nil, nil, attrErr
		}
	}

	// Step 5: Detect unreachable and always-true conditions
	if parsed.Conditions != nil {
		warnings = append(warnings, detectConditionWarnings(parsed.Conditions)...)
	}

	// Step 6: Pre-compile glob patterns
	globCache, err := precompileGlobs(parsed.Conditions)
	if err != nil {
		return nil, nil, err
	}

	result := &CompiledPolicy{
		GrammarVersion: dsl.GrammarVersion,
		Effect:         effect,
		Target:         target,
		Conditions:     parsed.Conditions,
		GlobCache:      globCache,
		DSLText:        dslText,
	}

	return result, warnings, nil
}

// buildTarget converts a parsed Target AST into a CompiledTarget.
func buildTarget(t *dsl.Target) CompiledTarget {
	ct := CompiledTarget{}

	if t.Principal != nil && t.Principal.Type != "" {
		s := t.Principal.Type
		ct.PrincipalType = &s
	}

	if t.Action != nil && len(t.Action.Actions) > 0 {
		ct.ActionList = t.Action.Actions
	}

	if t.Resource != nil {
		if t.Resource.Type != "" {
			s := t.Resource.Type
			ct.ResourceType = &s
		}
		if t.Resource.Equality != "" {
			s := t.Resource.Equality
			ct.ResourceExact = &s
		}
	}

	return ct
}

// mapEffect converts a DSL effect string to PolicyEffect.
func mapEffect(effect string) (types.PolicyEffect, error) {
	switch effect {
	case "permit":
		return types.PolicyEffectPermit, nil
	case "forbid":
		return types.PolicyEffectForbid, nil
	default:
		return "", fmt.Errorf("unknown effect: %q", effect)
	}
}

// validateAttributes walks all condition nodes and validates attribute references
// against the schema.
func (c *Compiler) validateAttributes(cb *dsl.ConditionBlock) ([]ValidationWarning, error) {
	refs := collectAttrRefs(cb)
	warnings := make([]ValidationWarning, 0, len(refs))

	for _, ref := range refs {
		if !c.schema.HasNamespace(ref.namespace) {
			continue
		}
		if c.schema.IsRegistered(ref.namespace, ref.key) {
			continue
		}

		fqn := ref.namespace + "." + ref.key
		if ref.namespace == "action" {
			return nil, fmt.Errorf("unregistered action attribute %q: action namespace is registered but attribute is not", fqn)
		}
		warnings = append(warnings, ValidationWarning{
			Message: fmt.Sprintf("unknown attribute %q in registered namespace %q", fqn, ref.namespace),
		})
	}

	return warnings, nil
}

// attrRefInfo holds a normalized attribute reference for validation.
type attrRefInfo struct {
	namespace string
	key       string
}

// collectAttrRefs walks the condition tree and extracts all attribute references.
func collectAttrRefs(cb *dsl.ConditionBlock) []attrRefInfo {
	var refs []attrRefInfo
	for _, conj := range cb.Disjunctions {
		for _, cond := range conj.Conditions {
			refs = append(refs, collectFromCondition(cond)...)
		}
	}
	return refs
}

// collectFromCondition recursively extracts attribute references from a condition.
func collectFromCondition(c *dsl.Condition) []attrRefInfo {
	var refs []attrRefInfo

	switch {
	case c.Negation != nil:
		refs = append(refs, collectFromCondition(c.Negation)...)

	case c.Parenthesized != nil:
		refs = append(refs, collectAttrRefs(c.Parenthesized)...)

	case c.IfThenElse != nil:
		refs = append(refs, collectFromCondition(c.IfThenElse.If)...)
		refs = append(refs, collectFromCondition(c.IfThenElse.Then)...)
		refs = append(refs, collectFromCondition(c.IfThenElse.Else)...)

	case c.Has != nil:
		if len(c.Has.Path) > 0 {
			refs = append(refs, attrRefInfo{
				namespace: c.Has.Root,
				key:       c.Has.Path[0],
			})
		}

	case c.Contains != nil:
		if len(c.Contains.Path) > 0 {
			refs = append(refs, attrRefInfo{
				namespace: c.Contains.Root,
				key:       c.Contains.Path[0],
			})
		}

	case c.Comparison != nil:
		refs = append(refs, collectFromExpr(c.Comparison.Left)...)
		refs = append(refs, collectFromExpr(c.Comparison.Right)...)

	case c.Like != nil:
		refs = append(refs, collectFromExpr(c.Like.Left)...)

	case c.InList != nil:
		refs = append(refs, collectFromExpr(c.InList.Left)...)

	case c.InExpr != nil:
		refs = append(refs, collectFromExpr(c.InExpr.Left)...)
		refs = append(refs, collectFromExpr(c.InExpr.Right)...)
	}

	return refs
}

// collectFromExpr extracts attribute references from an expression.
func collectFromExpr(e *dsl.Expr) []attrRefInfo {
	if e.AttrRef != nil && len(e.AttrRef.Path) > 0 {
		return []attrRefInfo{{
			namespace: e.AttrRef.Root,
			key:       e.AttrRef.Path[0],
		}}
	}
	return nil
}

// detectConditionWarnings checks for unreachable and always-true conditions.
func detectConditionWarnings(cb *dsl.ConditionBlock) []ValidationWarning {
	var warnings []ValidationWarning

	// Check for always-true: single disjunction, single conjunction, single condition = true
	if len(cb.Disjunctions) == 1 {
		conj := cb.Disjunctions[0]
		if len(conj.Conditions) == 1 {
			cond := conj.Conditions[0]
			if cond.IsBoolTrue() {
				warnings = append(warnings, ValidationWarning{
					Message: "condition block is always true; consider removing the when clause",
				})
			}
		}
	}

	// Check for unreachable: any conjunction starting with false && ...
	for _, conj := range cb.Disjunctions {
		if len(conj.Conditions) > 1 {
			first := conj.Conditions[0]
			if first.IsBoolFalse() {
				warnings = append(warnings, ValidationWarning{
					Message: "unreachable conditions: conjunction starts with false",
				})
			}
		}
	}

	return warnings
}

// precompileGlobs walks all LikeConditions, validates patterns, and compiles them.
func precompileGlobs(cb *dsl.ConditionBlock) (map[string]glob.Glob, error) {
	if cb == nil {
		return nil, nil
	}

	patterns := collectLikePatterns(cb)
	if len(patterns) == 0 {
		return nil, nil
	}

	cache := make(map[string]glob.Glob, len(patterns))
	for _, pattern := range patterns {
		if _, exists := cache[pattern]; exists {
			continue
		}
		if err := validateGlobPattern(pattern); err != nil {
			return nil, err
		}
		compiled, err := glob.Compile(pattern, ':')
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
		}
		cache[pattern] = compiled
	}

	return cache, nil
}

// validateGlobPattern checks a glob pattern against safety limits.
func validateGlobPattern(pattern string) error {
	if len(pattern) > maxGlobPatternLen {
		return fmt.Errorf("glob pattern exceeds maximum length of %d: %d characters", maxGlobPatternLen, len(pattern))
	}

	if strings.Contains(pattern, "[") {
		return fmt.Errorf("glob pattern contains bracket character class (not allowed): %q", pattern)
	}
	if strings.Contains(pattern, "{") {
		return fmt.Errorf("glob pattern contains brace alternation (not allowed): %q", pattern)
	}
	if strings.Contains(pattern, "**") {
		return fmt.Errorf("glob pattern contains globstar (not allowed): %q", pattern)
	}

	wildcardCount := 0
	for _, ch := range pattern {
		if ch == '*' || ch == '?' {
			wildcardCount++
		}
	}
	if wildcardCount > maxGlobWildcards {
		return fmt.Errorf("glob pattern has %d wildcard characters (maximum %d)", wildcardCount, maxGlobWildcards)
	}

	return nil
}

// collectLikePatterns walks the condition tree and extracts all like patterns.
func collectLikePatterns(cb *dsl.ConditionBlock) []string {
	var patterns []string
	for _, conj := range cb.Disjunctions {
		for _, cond := range conj.Conditions {
			patterns = append(patterns, collectPatternsFromCondition(cond)...)
		}
	}
	return patterns
}

// collectPatternsFromCondition recursively extracts like patterns from a condition.
func collectPatternsFromCondition(c *dsl.Condition) []string {
	var patterns []string

	switch {
	case c.Negation != nil:
		patterns = append(patterns, collectPatternsFromCondition(c.Negation)...)
	case c.Parenthesized != nil:
		patterns = append(patterns, collectLikePatterns(c.Parenthesized)...)
	case c.IfThenElse != nil:
		patterns = append(patterns, collectPatternsFromCondition(c.IfThenElse.If)...)
		patterns = append(patterns, collectPatternsFromCondition(c.IfThenElse.Then)...)
		patterns = append(patterns, collectPatternsFromCondition(c.IfThenElse.Else)...)
	case c.Like != nil:
		patterns = append(patterns, c.Like.Pattern)
	}

	return patterns
}

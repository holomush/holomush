// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dsl defines the AST types for the ABAC policy DSL and provides
// a parser built with participle. The AST nodes are designed to survive
// JSON serialization round-trips for policy storage.
package dsl

import (
	"encoding/json"
	"fmt"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// dslLexer defines the token types for the policy DSL.
// It handles multi-character operators (==, !=, &&) that the default
// text/scanner lexer would split into individual characters.
var dslLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "String", Pattern: `"[^"]*"`},
	{Name: "OpEq", Pattern: `==`},
	{Name: "OpNe", Pattern: `!=`},
	{Name: "OpAnd", Pattern: `&&`},
	{Name: "Dot", Pattern: `\.`},
	{Name: "Ident", Pattern: `[a-zA-Z_]\w*`},
	{Name: "Punct", Pattern: `[(){}\[\],;]`},
	{Name: "whitespace", Pattern: `\s+`},
})

// Policy represents a single ABAC policy statement.
//
// Grammar: effect "(" target ")" [ "when" "{" conditions "}" ] ";"
type Policy struct {
	Pos        lexer.Position  `parser:"" json:"-"`
	Effect     string          `parser:"@('permit' | 'forbid')" json:"effect"`
	Target     *Target         `parser:"'(' @@ ')'" json:"target"`
	Conditions *ConditionBlock `parser:"('when' '{' @@ '}')?" json:"conditions,omitempty"`
	Semi       string          `parser:"';'" json:"-"`
}

// Target contains the principal, action, and resource clauses.
type Target struct {
	Pos       lexer.Position   `parser:"" json:"-"`
	Principal *PrincipalClause `parser:"@@ ','" json:"principal"`
	Action    *ActionClause    `parser:"@@ ','" json:"action"`
	Resource  *ResourceClause  `parser:"@@" json:"resource"`
}

// PrincipalClause matches: "principal" [ "is" type_name ]
type PrincipalClause struct {
	Pos  lexer.Position `parser:"" json:"-"`
	Type string         `parser:"'principal' ('is' @Ident)?" json:"type,omitempty"`
}

// ActionClause matches: "action" [ "in" list ]
type ActionClause struct {
	Pos     lexer.Position `parser:"" json:"-"`
	Actions []string       `parser:"'action' ('in' '[' @String (',' @String)* ']')?" json:"actions,omitempty"`
}

// ResourceClause matches: "resource" [ "is" type_name | "==" string_literal ]
type ResourceClause struct {
	Pos      lexer.Position `parser:"" json:"-"`
	Type     string         `parser:"'resource' ( ('is' @Ident)" json:"type,omitempty"`
	Equality string         `parser:"              | ('==' @String) )?" json:"equality,omitempty"`
}

// ConditionBlock holds one or more conditions separated by "&&".
type ConditionBlock struct {
	Pos        lexer.Position `parser:"" json:"-"`
	Conditions []*Condition   `parser:"@@ ('&&' @@)*" json:"conditions"`
}

// Condition represents a single boolean expression: lhs comparator rhs.
type Condition struct {
	Pos        lexer.Position `parser:"" json:"-"`
	Left       *Expression    `parser:"@@" json:"left"`
	Comparator string         `parser:"@('==' | '!=' | 'in')" json:"comparator"`
	Right      *Expression    `parser:"@@" json:"right"`
}

// Expression is either an attribute reference (e.g. resource.id) or a literal value.
type Expression struct {
	Pos     lexer.Position `parser:"" json:"-"`
	AttrRef *AttrRef       `parser:"  @@" json:"attr_ref,omitempty"`
	Literal *Literal       `parser:"| @@" json:"literal,omitempty"`
}

// AttrRef represents a dotted attribute path like "resource.id" or "principal.role".
type AttrRef struct {
	Pos   lexer.Position `parser:"" json:"-"`
	Parts []string       `parser:"@Ident (Dot @Ident)+" json:"parts"`
}

// Literal represents a string literal value.
type Literal struct {
	Pos   lexer.Position `parser:"" json:"-"`
	Value string         `parser:"@String" json:"value"`
}

// GrammarVersion is the current version of the DSL grammar.
// This must be included in compiled_ast for forward-compatible evolution.
const GrammarVersion = 1

// WrapAST wraps a parsed Policy AST with grammar_version for storage.
// The spec requires compiled_ast to include grammar_version (02-policy-dsl.md:224).
func WrapAST(ast map[string]any) map[string]any {
	if ast == nil {
		return map[string]any{"grammar_version": GrammarVersion}
	}
	result := make(map[string]any, len(ast)+1)
	for k, v := range ast {
		result[k] = v
	}
	result["grammar_version"] = GrammarVersion
	return result
}

// CompilePolicy serializes a parsed Policy to JSON with grammar_version for storage.
// Callers MUST use this instead of json.Marshal(policy) directly to ensure the
// resulting AST includes the required grammar_version field.
func CompilePolicy(policy *Policy) (json.RawMessage, error) {
	// Marshal the policy to get raw JSON
	data, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("marshal policy: %w", err)
	}

	// Unmarshal into map to wrap with grammar_version
	var ast map[string]any
	if err := json.Unmarshal(data, &ast); err != nil {
		return nil, fmt.Errorf("unmarshal policy: %w", err)
	}

	// Wrap with grammar_version
	wrapped := WrapAST(ast)

	// Marshal back to JSON
	result, err := json.Marshal(wrapped)
	if err != nil {
		return nil, fmt.Errorf("marshal wrapped AST: %w", err)
	}

	return json.RawMessage(result), nil
}

// NewParser constructs a participle parser for the Policy grammar.
func NewParser() (*participle.Parser[Policy], error) {
	return participle.Build[Policy](
		participle.Lexer(dslLexer),
		participle.Unquote("String"),
	)
}

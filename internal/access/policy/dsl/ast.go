// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dsl defines the AST types for the ABAC policy DSL and provides
// a parser built with participle. The AST nodes are designed to survive
// JSON serialization round-trips for policy storage.
package dsl

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// dslLexer defines the token types for the policy DSL.
// Order matters: longer patterns must come before shorter ones that share
// a prefix (e.g., ">=" before ">", "&&" before individual chars).
var dslLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "String", Pattern: `"[^"]*"`},
	{Name: "Number", Pattern: `-?[0-9]+(\.[0-9]+)?`},
	{Name: "OpAnd", Pattern: `&&`},
	{Name: "OpOr", Pattern: `\|\|`},
	{Name: "OpEq", Pattern: `==`},
	{Name: "OpNe", Pattern: `!=`},
	{Name: "OpGe", Pattern: `>=`},
	{Name: "OpLe", Pattern: `<=`},
	{Name: "OpGt", Pattern: `>`},
	{Name: "OpLt", Pattern: `<`},
	{Name: "Bang", Pattern: `!`},
	{Name: "Dot", Pattern: `\.`},
	{Name: "Ident", Pattern: `[a-zA-Z_][a-zA-Z0-9_-]*`},
	{Name: "Punct", Pattern: `[(){}\[\],;]`},
	{Name: "whitespace", Pattern: `\s+`},
})

// GrammarVersion is the current version of the DSL grammar.
// This must be included in compiled_ast for forward-compatible evolution.
const GrammarVersion = 1

// reservedWords are words that MUST NOT appear as attribute names.
var reservedWords = map[string]bool{
	"permit": true, "forbid": true, "when": true,
	"principal": true, "resource": true, "action": true, "env": true,
	"is": true, "in": true, "has": true, "like": true,
	"true": true, "false": true,
	"if": true, "then": true, "else": true,
	"containsAll": true, "containsAny": true,
}

// IsReservedWord returns true if the given word is a DSL reserved word.
func IsReservedWord(word string) bool {
	return reservedWords[word]
}

// --- AST Node Types ---

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
	Equality string         `parser:"              | (OpEq @String) )?" json:"equality,omitempty"`
}

// ConditionBlock is the top-level condition: a disjunction (|| chain).
//
// Grammar: disjunction = conjunction { "||" conjunction }
type ConditionBlock struct {
	Pos          lexer.Position `parser:"" json:"-"`
	Disjunctions []*Conjunction `parser:"@@ (OpOr @@)*" json:"disjunctions"`
}

// Conjunction is a chain of conditions joined by &&.
//
// Grammar: conjunction = condition { "&&" condition }
type Conjunction struct {
	Pos        lexer.Position `parser:"" json:"-"`
	Conditions []*Condition   `parser:"@@ (OpAnd @@)*" json:"conditions"`
}

// Condition represents a single boolean expression in the DSL.
// Exactly one field is non-nil, representing the matched alternative.
//
// The parser tries alternatives in order (PEG ordered choice):
// 1. Unique-prefix forms: negation (!), parenthesized, if-then-else
// 2. has (starts with attribute_root keyword)
// 3. Expression-starting forms: comparison, like, in-list, in-expr, containsAll/Any
// 4. Bare boolean literal (fallback)
type Condition struct {
	Pos           lexer.Position  `parser:"" json:"-"`
	Negation      *Condition      `parser:"  Bang @@" json:"negation,omitempty"`
	Parenthesized *ConditionBlock `parser:"| '(' @@ ')'" json:"parenthesized,omitempty"`
	IfThenElse    *IfThenElse     `parser:"| @@" json:"if_then_else,omitempty"`
	Has           *HasCondition   `parser:"| @@" json:"has,omitempty"`
	ContainsAll   *ContainsCondition `parser:"| @@" json:"contains_all,omitempty"`
	ContainsAny   *ContainsCondition `parser:"| @@" json:"contains_any,omitempty"`
	Like          *LikeCondition  `parser:"| @@" json:"like,omitempty"`
	InList        *InListCondition `parser:"| @@" json:"in_list,omitempty"`
	InExpr        *InExprCondition `parser:"| @@" json:"in_expr,omitempty"`
	Comparison    *Comparison     `parser:"| @@" json:"comparison,omitempty"`
	BoolLiteral   *bool           `parser:"| @('true' | 'false')" json:"bool_literal,omitempty"`
}

// Comparison represents an expression with a comparison operator (==, !=, >, >=, <, <=).
type Comparison struct {
	Pos        lexer.Position `parser:"" json:"-"`
	Left       *Expr          `parser:"@@" json:"left"`
	Comparator string         `parser:"@(OpEq | OpNe | OpGe | OpLe | OpGt | OpLt)" json:"comparator"`
	Right      *Expr          `parser:"@@" json:"right"`
}

// LikeCondition represents a glob pattern match (expr "like" string_literal).
type LikeCondition struct {
	Pos     lexer.Position `parser:"" json:"-"`
	Left    *Expr          `parser:"@@" json:"left"`
	Keyword string         `parser:"'like'" json:"-"`
	Pattern string         `parser:"@String" json:"pattern"`
}

// HasCondition represents an attribute existence check (attribute_root "has" path).
type HasCondition struct {
	Pos  lexer.Position `parser:"" json:"-"`
	Root string         `parser:"@('principal' | 'resource' | 'action' | 'env')" json:"root"`
	Has  string         `parser:"'has'" json:"-"`
	Path []string       `parser:"@Ident (Dot @Ident)*" json:"path"`
}

// ContainsCondition represents a containsAll or containsAny list method call.
type ContainsCondition struct {
	Pos  lexer.Position `parser:"" json:"-"`
	Left *Expr          `parser:"@@" json:"left"`
	Dot  string         `parser:"Dot" json:"-"`
	Op   string         `parser:"@('containsAll' | 'containsAny')" json:"op"`
	List *ListExpr      `parser:"'(' @@ ')'" json:"list"`
}

// InListCondition represents scalar-in-set membership (expr "in" list).
type InListCondition struct {
	Pos  lexer.Position `parser:"" json:"-"`
	Left *Expr          `parser:"@@" json:"left"`
	In   string         `parser:"'in'" json:"-"`
	List *ListExpr      `parser:"@@" json:"list"`
}

// InExprCondition represents value-in-attribute-array membership (expr "in" expr).
type InExprCondition struct {
	Pos   lexer.Position `parser:"" json:"-"`
	Left  *Expr          `parser:"@@" json:"left"`
	In    string         `parser:"'in'" json:"-"`
	Right *Expr          `parser:"@@" json:"right"`
}

// IfThenElse represents a conditional expression ("if" cond "then" cond "else" cond).
type IfThenElse struct {
	Pos  lexer.Position `parser:"" json:"-"`
	If   *Condition     `parser:"'if' @@" json:"if"`
	Then *Condition     `parser:"'then' @@" json:"then"`
	Else *Condition     `parser:"'else' @@" json:"else"`
}

// Expr is either an attribute reference or a literal value.
//
// Grammar: expr = attribute_ref | literal
type Expr struct {
	Pos     lexer.Position `parser:"" json:"-"`
	AttrRef *AttrRef       `parser:"  @@" json:"attr_ref,omitempty"`
	Literal *Literal       `parser:"| @@" json:"literal,omitempty"`
}

// AttrRef represents a dotted attribute path like "resource.id".
//
// Grammar: attribute_ref = ("principal"|"resource"|"action"|"env") "." identifier { "." identifier }
type AttrRef struct {
	Pos  lexer.Position `parser:"" json:"-"`
	Root string         `parser:"@('principal' | 'resource' | 'action' | 'env')" json:"root"`
	Dot  string         `parser:"Dot" json:"-"`
	Path []string       `parser:"@Ident (Dot @Ident)*" json:"path"`
}

// Literal represents a scalar value: string, number, or boolean.
type Literal struct {
	Pos    lexer.Position `parser:"" json:"-"`
	Str    *string        `parser:"  @String" json:"str,omitempty"`
	Number *float64       `parser:"| @Number" json:"number,omitempty"`
	Bool   *bool          `parser:"| @('true' | 'false')" json:"bool,omitempty"`
}

// ListExpr represents a bracketed list of literals: "[" literal { "," literal } "]"
type ListExpr struct {
	Pos    lexer.Position `parser:"" json:"-"`
	Values []*Literal     `parser:"'[' @@ (',' @@)* ']'" json:"values"`
}

// --- String() methods for readable DSL rendering ---

func (p *Policy) String() string {
	var b strings.Builder
	b.WriteString(p.Effect)
	b.WriteByte('(')
	b.WriteString(p.Target.String())
	b.WriteByte(')')
	if p.Conditions != nil {
		b.WriteString(" when { ")
		b.WriteString(p.Conditions.String())
		b.WriteString(" }")
	}
	b.WriteByte(';')
	return b.String()
}

func (t *Target) String() string {
	return t.Principal.String() + ", " + t.Action.String() + ", " + t.Resource.String()
}

func (pc *PrincipalClause) String() string {
	if pc.Type != "" {
		return "principal is " + pc.Type
	}
	return "principal"
}

func (ac *ActionClause) String() string {
	if len(ac.Actions) > 0 {
		return "action in " + formatStringList(ac.Actions)
	}
	return "action"
}

func (rc *ResourceClause) String() string {
	if rc.Type != "" {
		return "resource is " + rc.Type
	}
	if rc.Equality != "" {
		return `resource == "` + rc.Equality + `"`
	}
	return "resource"
}

func (cb *ConditionBlock) String() string {
	parts := make([]string, len(cb.Disjunctions))
	for i, conj := range cb.Disjunctions {
		parts[i] = conj.String()
	}
	return strings.Join(parts, " || ")
}

func (conj *Conjunction) String() string {
	parts := make([]string, len(conj.Conditions))
	for i, c := range conj.Conditions {
		parts[i] = c.String()
	}
	return strings.Join(parts, " && ")
}

func (c *Condition) String() string {
	switch {
	case c.Negation != nil:
		return "!(" + c.Negation.String() + ")"
	case c.Parenthesized != nil:
		return "(" + c.Parenthesized.String() + ")"
	case c.IfThenElse != nil:
		return c.IfThenElse.String()
	case c.Has != nil:
		return c.Has.String()
	case c.ContainsAll != nil:
		return c.ContainsAll.StringWith("containsAll")
	case c.ContainsAny != nil:
		return c.ContainsAny.StringWith("containsAny")
	case c.Like != nil:
		return c.Like.String()
	case c.InList != nil:
		return c.InList.String()
	case c.InExpr != nil:
		return c.InExpr.String()
	case c.Comparison != nil:
		return c.Comparison.String()
	case c.BoolLiteral != nil:
		if *c.BoolLiteral {
			return "true"
		}
		return "false"
	default:
		return "<empty>"
	}
}

func (cmp *Comparison) String() string {
	return cmp.Left.String() + " " + cmp.Comparator + " " + cmp.Right.String()
}

func (lc *LikeCondition) String() string {
	return lc.Left.String() + ` like "` + lc.Pattern + `"`
}

func (hc *HasCondition) String() string {
	return hc.Root + " has " + strings.Join(hc.Path, ".")
}

// StringWith renders the condition using the given operator name ("containsAll" or "containsAny").
func (cc *ContainsCondition) StringWith(op string) string {
	return cc.Left.String() + "." + op + "(" + cc.List.String() + ")"
}

func (il *InListCondition) String() string {
	return il.Left.String() + " in " + il.List.String()
}

func (ie *InExprCondition) String() string {
	return ie.Left.String() + " in " + ie.Right.String()
}

func (ite *IfThenElse) String() string {
	return "if " + ite.If.String() + " then " + ite.Then.String() + " else " + ite.Else.String()
}

func (e *Expr) String() string {
	if e.AttrRef != nil {
		return e.AttrRef.String()
	}
	if e.Literal != nil {
		return e.Literal.String()
	}
	return "<empty>"
}

func (ar *AttrRef) String() string {
	return ar.Root + "." + strings.Join(ar.Path, ".")
}

func (l *Literal) String() string {
	switch {
	case l.Str != nil:
		return `"` + *l.Str + `"`
	case l.Number != nil:
		v := *l.Number
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case l.Bool != nil:
		if *l.Bool {
			return "true"
		}
		return "false"
	default:
		return "<empty>"
	}
}

func (le *ListExpr) String() string {
	parts := make([]string, len(le.Values))
	for i, v := range le.Values {
		parts[i] = v.String()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// formatStringList renders a Go string slice as DSL list syntax.
func formatStringList(items []string) string {
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = `"` + s + `"`
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// --- Compilation helpers ---

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
	data, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("marshal policy: %w", err)
	}

	var ast map[string]any
	if err = json.Unmarshal(data, &ast); err != nil {
		return nil, fmt.Errorf("unmarshal policy: %w", err)
	}

	wrapped := WrapAST(ast)

	result, err := json.Marshal(wrapped)
	if err != nil {
		return nil, fmt.Errorf("marshal wrapped AST: %w", err)
	}

	return json.RawMessage(result), nil
}

// NewParser constructs a participle parser for the Policy grammar.
// Note: Parser configuration will be refined in T9 (Build DSL parser).
func NewParser() (*participle.Parser[Policy], error) {
	return participle.Build[Policy](
		participle.Lexer(dslLexer),
		participle.Unquote("String"),
		participle.UseLookahead(2),
	)
}

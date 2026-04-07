// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractCommandNames covers every behavior of the ExtractCommandNames
// AST walker that the trust-boundary validator depends on. The walker MUST
// only collect command name literals from conditions that unambiguously
// constrain the policy to those commands — anything inside negations,
// parenthesized blocks, or if/then/else branches is dropped on the floor
// because such literals do not constrain (a negation inverts the meaning,
// a branch is conditional). Returning the literal in those cases would let
// foreign-command execute policies slip through trust validation.
func TestExtractCommandNames(t *testing.T) {
	tests := []struct {
		name string // ACE: Action — Condition — Expectation
		dsl  string // policy DSL; empty string ⇒ pass nil ConditionBlock
		want []string
	}{
		{
			name: "extracts command name when condition compares resource.command.name to a string literal",
			dsl:  `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name == "widget" };`,
			want: []string{"widget"},
		},
		{
			name: "extracts every command name when condition uses an in-list of literals",
			dsl:  `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name in ["say", "pose", "look"] };`,
			want: []string{"say", "pose", "look"},
		},
		{
			name: "returns nil when the condition block is nil",
			dsl:  "",
			want: nil,
		},
		{
			name: "returns nil when conditions reference unrelated attributes",
			dsl:  `permit(principal is character, action in ["read"], resource is widget) when { resource.widget.owner == "admin" };`,
			want: nil,
		},
		{
			name: "extracts command name when comparison places literal on the left side",
			dsl:  `permit(principal is character, action in ["execute"], resource is command) when { "widget" == resource.command.name };`,
			want: []string{"widget"},
		},
		{
			name: "returns nil when the command name reference is wrapped in parentheses (parenthesized branch is not descended into)",
			dsl:  `permit(principal is character, action in ["execute"], resource is command) when { (resource.command.name == "bar") };`,
			want: nil,
		},
		{
			name: "returns nil when the command name reference is inside a negation (negated branch is not descended into)",
			dsl:  `permit(principal is character, action in ["execute"], resource is command) when { !(resource.command.name == "foo") };`,
			want: nil,
		},
		{
			name: "extracts only the relevant command name when conjoined with an unrelated condition",
			dsl:  `permit(principal is character, action in ["execute"], resource is command) when { "admin" in principal.character.roles && resource.command.name == "shutdown" };`,
			want: []string{"shutdown"},
		},
		{
			name: "returns nil when a reversed comparison references an attribute other than resource.command.name",
			dsl:  `permit(principal is character, action in ["read"], resource is widget) when { "x" == resource.widget.color };`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.dsl == "" {
				assert.Equal(t, tt.want, ExtractCommandNames(nil))
				return
			}
			parsed, err := Parse(tt.dsl)
			require.NoError(t, err, "test fixture DSL must parse")
			got := ExtractCommandNames(parsed.Conditions)
			assert.Equal(t, tt.want, got)
		})
	}
}

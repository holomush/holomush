// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/dsl"
)

func TestValidatePrincipalScope(t *testing.T) {
	tests := []struct {
		name         string
		dslText      string
		pluginName   string
		expectOK     bool
		expectForeig string
	}{
		{
			name:       "matching principal name passes",
			dslText:    `permit(principal is plugin, action, resource) when { principal.plugin.name == "echo-bot" };`,
			pluginName: "echo-bot",
			expectOK:   true,
		},
		{
			name:         "foreign principal name rejected",
			dslText:      `permit(principal is plugin, action, resource) when { principal.plugin.name == "other-plugin" };`,
			pluginName:   "echo-bot",
			expectOK:     false,
			expectForeig: "other-plugin",
		},
		{
			name:       "no principal name condition passes",
			dslText:    `permit(principal is plugin, action in ["read"], resource);`,
			pluginName: "echo-bot",
			expectOK:   true,
		},
		{
			name:       "no conditions passes",
			dslText:    `permit(principal is plugin, action, resource);`,
			pluginName: "echo-bot",
			expectOK:   true,
		},
		{
			name:       "principal name with like pattern matching self",
			dslText:    `permit(principal is plugin, action, resource) when { principal.plugin.name like "echo-bot*" };`,
			pluginName: "echo-bot",
			expectOK:   true,
		},
		{
			name:         "principal name with like pattern matching other",
			dslText:      `permit(principal is plugin, action, resource) when { principal.plugin.name like "other*" };`,
			pluginName:   "echo-bot",
			expectOK:     false,
			expectForeig: "other*",
		},
		{
			name:         "nested in conjunction — foreign rejected",
			dslText:      `permit(principal is plugin, action, resource) when { principal.plugin.name == "echo-bot" && principal.plugin.name == "foreign" };`,
			pluginName:   "echo-bot",
			expectOK:     false,
			expectForeig: "foreign",
		},
		{
			name:         "nested in disjunction — foreign rejected",
			dslText:      `permit(principal is plugin, action, resource) when { principal.plugin.name == "echo-bot" || principal.plugin.name == "foreign" };`,
			pluginName:   "echo-bot",
			expectOK:     false,
			expectForeig: "foreign",
		},
		{
			name:       "reversed comparison order (literal on left)",
			dslText:    `permit(principal is plugin, action, resource) when { "echo-bot" == principal.plugin.name };`,
			pluginName: "echo-bot",
			expectOK:   true,
		},
		{
			name:         "reversed comparison with foreign name",
			dslText:      `permit(principal is plugin, action, resource) when { "other" == principal.plugin.name };`,
			pluginName:   "echo-bot",
			expectOK:     false,
			expectForeig: "other",
		},
		{
			name:       "resource condition with name does not trigger",
			dslText:    `permit(principal is plugin, action, resource) when { resource.kv.name == "anything" };`,
			pluginName: "echo-bot",
			expectOK:   true,
		},
		{
			name:         "negated foreign principal rejected",
			dslText:      `permit(principal is plugin, action, resource) when { !(principal.plugin.name == "other") };`,
			pluginName:   "echo-bot",
			expectOK:     false,
			expectForeig: "other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := dsl.Parse(tt.dslText)
			require.NoError(t, err)

			ok, foreign := dsl.ValidatePrincipalScope(parsed, tt.pluginName)
			assert.Equal(t, tt.expectOK, ok, "ValidatePrincipalScope result")
			if !tt.expectOK {
				assert.Equal(t, tt.expectForeig, foreign, "foreign name")
			}
		})
	}
}

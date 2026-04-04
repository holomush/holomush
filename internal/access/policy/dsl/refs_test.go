// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReferencesResourceAttrs(t *testing.T) {
	tests := []struct {
		name string
		dsl  string
		want bool
	}{
		{
			name: "nil block",
			dsl:  "",
			want: false,
		},
		{
			name: "subject-only condition",
			dsl:  `"admin" in principal.character.roles`,
			want: false,
		},
		{
			name: "resource attribute in comparison",
			dsl:  `resource.stream.location == principal.character.location`,
			want: true,
		},
		{
			name: "resource attribute in like",
			dsl:  `resource.stream.name like "location:*"`,
			want: true,
		},
		{
			name: "resource attribute in has",
			dsl:  `resource has stream.name`,
			want: true,
		},
		{
			name: "subject has condition only",
			dsl:  `principal has character.roles`,
			want: false,
		},
		{
			name: "mixed subject and resource",
			dsl:  `"admin" in principal.character.roles && resource.location.id == "01ABC"`,
			want: true,
		},
		{
			name: "negated resource condition",
			dsl:  `!(resource.location.id == "restricted")`,
			want: true,
		},
		{
			name: "resource in containsAll",
			dsl:  `resource.location.tags.containsAll(["safe", "public"])`,
			want: true,
		},
		{
			name: "principal containsAny only",
			dsl:  `principal.character.roles.containsAny(["admin", "builder"])`,
			want: false,
		},
		{
			name: "in-expr with resource on right",
			dsl:  `"visitor" in resource.location.allowed_roles`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.dsl == "" {
				assert.Equal(t, tt.want, ReferencesResourceAttrs(nil))
				return
			}

			// Parse a full policy to extract the condition block
			policyText := `permit(principal is character, action in ["test"], resource is location) when { ` + tt.dsl + ` };`
			policy, err := Parse(policyText)
			require.NoError(t, err, "failed to parse DSL: %s", tt.dsl)
			require.NotNil(t, policy.Conditions, "expected conditions in policy")

			got := ReferencesResourceAttrs(policy.Conditions)
			assert.Equal(t, tt.want, got)
		})
	}
}

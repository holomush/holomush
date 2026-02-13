// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
)

func TestExitProvider_Namespace(t *testing.T) {
	provider := NewExitProvider()
	assert.Equal(t, "exit", provider.Namespace())
}

func TestExitProvider_ResolveSubject(t *testing.T) {
	provider := NewExitProvider()
	attrs, err := provider.ResolveSubject(context.Background(), "exit:01XYZ")
	require.NoError(t, err)
	assert.Nil(t, attrs)
}

func TestExitProvider_ResolveResource(t *testing.T) {
	tests := []struct {
		name       string
		resourceID string
		expected   map[string]any
		wantNil    bool
	}{
		{
			name:       "exit resource type",
			resourceID: "exit:01XYZ",
			expected: map[string]any{
				"type": "exit",
				"id":   "01XYZ",
			},
		},
		{
			name:       "exit with different ID",
			resourceID: "exit:01ABC",
			expected: map[string]any{
				"type": "exit",
				"id":   "01ABC",
			},
		},
		{
			name:       "non-exit resource type",
			resourceID: "command:say",
			wantNil:    true,
		},
		{
			name:       "object resource type",
			resourceID: "object:01ABC",
			wantNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewExitProvider()
			attrs, err := provider.ResolveResource(context.Background(), tt.resourceID)
			require.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, attrs)
			} else {
				assert.Equal(t, tt.expected, attrs)
			}
		})
	}
}

func TestExitProvider_Schema(t *testing.T) {
	provider := NewExitProvider()
	schema := provider.Schema()

	expected := &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"type": types.AttrTypeString,
			"id":   types.AttrTypeString,
		},
	}

	assert.Equal(t, expected, schema)
}

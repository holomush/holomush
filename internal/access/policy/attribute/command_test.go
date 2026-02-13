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

func TestCommandProvider_Namespace(t *testing.T) {
	provider := NewCommandProvider()
	assert.Equal(t, "command", provider.Namespace())
}

func TestCommandProvider_ResolveSubject(t *testing.T) {
	provider := NewCommandProvider()
	attrs, err := provider.ResolveSubject(context.Background(), "command:say")
	require.NoError(t, err)
	assert.Nil(t, attrs)
}

func TestCommandProvider_ResolveResource(t *testing.T) {
	tests := []struct {
		name       string
		resourceID string
		expected   map[string]any
		wantNil    bool
	}{
		{
			name:       "command resource type",
			resourceID: "command:say",
			expected: map[string]any{
				"type": "command",
				"name": "say",
			},
		},
		{
			name:       "command with multi-part name",
			resourceID: "command:@dig",
			expected: map[string]any{
				"type": "command",
				"name": "@dig",
			},
		},
		{
			name:       "non-command resource type",
			resourceID: "stream:location:01XYZ",
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
			provider := NewCommandProvider()
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

func TestCommandProvider_Schema(t *testing.T) {
	provider := NewCommandProvider()
	schema := provider.Schema()

	expected := &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"type": types.AttrTypeString,
			"name": types.AttrTypeString,
		},
	}

	assert.Equal(t, expected, schema)
}

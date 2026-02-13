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

func TestStreamProvider_Namespace(t *testing.T) {
	provider := NewStreamProvider()
	assert.Equal(t, "stream", provider.Namespace())
}

func TestStreamProvider_ResolveSubject(t *testing.T) {
	provider := NewStreamProvider()
	attrs, err := provider.ResolveSubject(context.Background(), "stream:location:01XYZ")
	require.NoError(t, err)
	assert.Nil(t, attrs)
}

func TestStreamProvider_ResolveResource(t *testing.T) {
	tests := []struct {
		name       string
		resourceID string
		expected   map[string]any
		wantNil    bool
	}{
		{
			name:       "location stream with location ID",
			resourceID: "stream:location:01XYZ",
			expected: map[string]any{
				"type":     "stream",
				"name":     "location:01XYZ",
				"location": "01XYZ",
			},
		},
		{
			name:       "simple stream name",
			resourceID: "stream:global",
			expected: map[string]any{
				"type": "stream",
				"name": "global",
			},
		},
		{
			name:       "stream with colon but not location prefix",
			resourceID: "stream:scene:01ABC",
			expected: map[string]any{
				"type": "stream",
				"name": "scene:01ABC",
			},
		},
		{
			name:       "non-stream resource type",
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
			provider := NewStreamProvider()
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

func TestStreamProvider_Schema(t *testing.T) {
	provider := NewStreamProvider()
	schema := provider.Schema()

	expected := &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"type":     types.AttrTypeString,
			"name":     types.AttrTypeString,
			"location": types.AttrTypeString,
		},
	}

	assert.Equal(t, expected, schema)
}

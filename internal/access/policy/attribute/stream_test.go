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

func TestStreamProviderContract(t *testing.T) {
	assertProviderContract(t, NewStreamProvider())
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
			resourceID: "stream:events.main.location.01XYZ",
			expected: map[string]any{
				"type":         "stream",
				"name":         "events.main.location.01XYZ",
				"location":     "01XYZ",
				"has_location": true,
			},
		},
		{
			name:       "simple stream name",
			resourceID: "stream:global",
			expected: map[string]any{
				"type":         "stream",
				"name":         "global",
				"has_location": false,
			},
		},
		{
			name:       "stream with colon but not location prefix",
			resourceID: "stream:scene:01ABC",
			expected: map[string]any{
				"type":         "stream",
				"name":         "scene:01ABC",
				"has_location": false,
			},
		},
		{
			// INV-EVENTBUS-23: a qualified non-location stream omits the location
			// key entirely (not an empty-string sentinel) and witnesses false.
			// assert.Equal on the full expected map verifies the key is absent.
			name:       "qualified character stream omits location, witness false",
			resourceID: "stream:events.main.character.01CHR",
			expected: map[string]any{
				"type":         "stream",
				"name":         "events.main.character.01CHR",
				"has_location": false,
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

func TestStreamProviderSchema(t *testing.T) {
	provider := NewStreamProvider()
	schema := provider.Schema()

	expected := &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"type":         types.AttrTypeString,
			"name":         types.AttrTypeString,
			"location":     types.AttrTypeString,
			"has_location": types.AttrTypeBool,
		},
	}

	assert.Equal(t, expected, schema)
}

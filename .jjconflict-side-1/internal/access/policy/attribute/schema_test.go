// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"testing"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaRegistry_Register(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		schema    *types.NamespaceSchema
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid namespace registration",
			namespace: "character",
			schema: &types.NamespaceSchema{
				Attributes: map[string]types.AttrType{
					"name":  types.AttrTypeString,
					"level": types.AttrTypeFloat,
				},
			},
			wantErr: false,
		},
		{
			name:      "empty namespace",
			namespace: "",
			schema: &types.NamespaceSchema{
				Attributes: map[string]types.AttrType{
					"name": types.AttrTypeString,
				},
			},
			wantErr: true,
			errMsg:  "namespace cannot be empty",
		},
		{
			name:      "duplicate namespace",
			namespace: "character",
			schema: &types.NamespaceSchema{
				Attributes: map[string]types.AttrType{
					"name": types.AttrTypeString,
				},
			},
			wantErr: true,
			errMsg:  "namespace already registered",
		},
		{
			name:      "nil schema",
			namespace: "location",
			schema:    nil,
			wantErr:   true,
			errMsg:    "schema cannot be nil",
		},
		{
			name:      "empty attributes",
			namespace: "object",
			schema: &types.NamespaceSchema{
				Attributes: map[string]types.AttrType{},
			},
			wantErr: true,
			errMsg:  "schema must have at least one attribute",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewSchemaRegistry()

			// Pre-register "character" namespace for duplicate test
			if tt.name == "duplicate namespace" {
				err := registry.Register("character", &types.NamespaceSchema{
					Attributes: map[string]types.AttrType{
						"id": types.AttrTypeString,
					},
				})
				require.NoError(t, err)
			}

			err := registry.Register(tt.namespace, tt.schema)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSchemaRegistry_IsRegistered(t *testing.T) {
	registry := NewSchemaRegistry()

	// Register a namespace with some attributes
	err := registry.Register("character", &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"name":  types.AttrTypeString,
			"level": types.AttrTypeFloat,
		},
	})
	require.NoError(t, err)

	err = registry.Register("location", &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"id": types.AttrTypeString,
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		namespace string
		key       string
		expected  bool
	}{
		{
			name:      "registered namespace and key",
			namespace: "character",
			key:       "name",
			expected:  true,
		},
		{
			name:      "registered namespace, different key",
			namespace: "character",
			key:       "level",
			expected:  true,
		},
		{
			name:      "registered namespace, unregistered key",
			namespace: "character",
			key:       "faction",
			expected:  false,
		},
		{
			name:      "unregistered namespace",
			namespace: "plugin",
			key:       "score",
			expected:  false,
		},
		{
			name:      "empty namespace",
			namespace: "",
			key:       "name",
			expected:  false,
		},
		{
			name:      "empty key",
			namespace: "character",
			key:       "",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := registry.IsRegistered(tt.namespace, tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSchemaRegistry_HasNamespace(t *testing.T) {
	registry := NewSchemaRegistry()

	// Register some namespaces
	err := registry.Register("character", &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"name": types.AttrTypeString,
		},
	})
	require.NoError(t, err)

	err = registry.Register("location", &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"id": types.AttrTypeString,
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		namespace string
		expected  bool
	}{
		{
			name:      "registered namespace",
			namespace: "character",
			expected:  true,
		},
		{
			name:      "different registered namespace",
			namespace: "location",
			expected:  true,
		},
		{
			name:      "unregistered namespace",
			namespace: "plugin",
			expected:  false,
		},
		{
			name:      "empty namespace",
			namespace: "",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := registry.HasNamespace(tt.namespace)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSchemaRegistry_Schema(t *testing.T) {
	registry := NewSchemaRegistry()

	// Register a namespace
	err := registry.Register("character", &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"name": types.AttrTypeString,
		},
	})
	require.NoError(t, err)

	// Get the underlying schema
	schema := registry.Schema()
	require.NotNil(t, schema)

	// Verify it has the registered namespace
	assert.True(t, schema.HasNamespace("character"))
	assert.True(t, schema.IsRegistered("character", "name"))
}

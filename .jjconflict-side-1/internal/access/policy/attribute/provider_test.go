// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"testing"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock implementation of AttributeProvider for testing
type mockAttributeProvider struct {
	namespace     string
	subjectAttrs  map[string]any
	resourceAttrs map[string]any
	subjectErr    error
	resourceErr   error
	schema        *types.NamespaceSchema
}

func (m *mockAttributeProvider) Namespace() string {
	return m.namespace
}

func (m *mockAttributeProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	if m.subjectErr != nil {
		return nil, m.subjectErr
	}
	return m.subjectAttrs, nil
}

func (m *mockAttributeProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	if m.resourceErr != nil {
		return nil, m.resourceErr
	}
	return m.resourceAttrs, nil
}

func (m *mockAttributeProvider) Schema() *types.NamespaceSchema {
	return m.schema
}

// Mock implementation of EnvironmentProvider for testing
type mockEnvironmentProvider struct {
	namespace string
	attrs     map[string]any
	err       error
	schema    *types.NamespaceSchema
}

func (m *mockEnvironmentProvider) Namespace() string {
	return m.namespace
}

func (m *mockEnvironmentProvider) Resolve(_ context.Context) (map[string]any, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.attrs, nil
}

func (m *mockEnvironmentProvider) Schema() *types.NamespaceSchema {
	return m.schema
}

func TestAttributeProvider_Interface(t *testing.T) {
	provider := &mockAttributeProvider{
		namespace: "character",
		subjectAttrs: map[string]any{
			"name":  "Alice",
			"level": float64(5),
		},
		resourceAttrs: map[string]any{
			"type": "location",
		},
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"name":  types.AttrTypeString,
				"level": types.AttrTypeFloat,
			},
		},
	}

	// Test Namespace method
	assert.Equal(t, "character", provider.Namespace())

	// Test ResolveSubject method
	ctx := context.Background()
	subjectAttrs, err := provider.ResolveSubject(ctx, "01ABC")
	require.NoError(t, err)
	assert.Equal(t, "Alice", subjectAttrs["name"])
	assert.Equal(t, float64(5), subjectAttrs["level"])

	// Test ResolveResource method
	resourceAttrs, err := provider.ResolveResource(ctx, "01XYZ")
	require.NoError(t, err)
	assert.Equal(t, "location", resourceAttrs["type"])

	// Test Schema method
	schema := provider.Schema()
	require.NotNil(t, schema)
	assert.Len(t, schema.Attributes, 2)
	assert.Equal(t, types.AttrTypeString, schema.Attributes["name"])
	assert.Equal(t, types.AttrTypeFloat, schema.Attributes["level"])
}

func TestEnvironmentProvider_Interface(t *testing.T) {
	provider := &mockEnvironmentProvider{
		namespace: "env",
		attrs: map[string]any{
			"time":        "2026-02-05T14:30:00Z",
			"maintenance": false,
		},
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"time":        types.AttrTypeString,
				"maintenance": types.AttrTypeBool,
			},
		},
	}

	// Test Namespace method
	assert.Equal(t, "env", provider.Namespace())

	// Test Resolve method
	ctx := context.Background()
	attrs, err := provider.Resolve(ctx)
	require.NoError(t, err)
	assert.Equal(t, "2026-02-05T14:30:00Z", attrs["time"])
	assert.Equal(t, false, attrs["maintenance"])

	// Test Schema method
	schema := provider.Schema()
	require.NotNil(t, schema)
	assert.Len(t, schema.Attributes, 2)
	assert.Equal(t, types.AttrTypeString, schema.Attributes["time"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["maintenance"])
}

func TestAttributeProvider_NumericTypesAsFloat64(t *testing.T) {
	// This test verifies that providers return numeric attributes as float64
	// as per the spec requirement
	provider := &mockAttributeProvider{
		namespace: "character",
		subjectAttrs: map[string]any{
			"level": float64(10),   // MUST be float64, not int
			"score": float64(85.5), // already float64
		},
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"level": types.AttrTypeFloat,
				"score": types.AttrTypeFloat,
			},
		},
	}

	ctx := context.Background()
	attrs, err := provider.ResolveSubject(ctx, "01ABC")
	require.NoError(t, err)

	// Verify that numeric values are float64
	level, ok := attrs["level"].(float64)
	require.True(t, ok, "level should be float64")
	assert.Equal(t, float64(10), level)

	score, ok := attrs["score"].(float64)
	require.True(t, ok, "score should be float64")
	assert.Equal(t, float64(85.5), score)
}

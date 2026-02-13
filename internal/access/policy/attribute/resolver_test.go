// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"errors"
	"testing"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Extended mock providers for resolver testing
// These extend the basic mocks from provider_test.go with additional capabilities

type resolverMockAttributeProvider struct {
	namespace     string
	subjectData   map[string]map[string]any
	resourceData  map[string]map[string]any
	subjectError  error
	resourceError error
	shouldPanic   bool
	callCount     map[string]int
	schema        *types.NamespaceSchema
}

func newResolverMockAttributeProvider(namespace string) *resolverMockAttributeProvider {
	return &resolverMockAttributeProvider{
		namespace:    namespace,
		subjectData:  make(map[string]map[string]any),
		resourceData: make(map[string]map[string]any),
		callCount:    make(map[string]int),
		schema:       newResolverMockSchema(),
	}
}

func (m *resolverMockAttributeProvider) Namespace() string {
	return m.namespace
}

func (m *resolverMockAttributeProvider) ResolveSubject(_ context.Context, subjectID string) (map[string]any, error) {
	m.callCount["subject:"+subjectID]++
	if m.shouldPanic {
		panic("mock provider panic")
	}
	if m.subjectError != nil {
		return nil, m.subjectError
	}
	if data, ok := m.subjectData[subjectID]; ok {
		return data, nil
	}
	return nil, nil
}

func (m *resolverMockAttributeProvider) ResolveResource(_ context.Context, resourceID string) (map[string]any, error) {
	m.callCount["resource:"+resourceID]++
	if m.shouldPanic {
		panic("mock provider panic")
	}
	if m.resourceError != nil {
		return nil, m.resourceError
	}
	if data, ok := m.resourceData[resourceID]; ok {
		return data, nil
	}
	return nil, nil
}

func (m *resolverMockAttributeProvider) Schema() *types.NamespaceSchema {
	return m.schema
}

func newResolverMockSchema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role": types.AttrTypeString,
		},
	}
}

func TestNewResolver(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)
	require.NotNil(t, resolver)
}

func TestResolver_RegisterProvider(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*Resolver)
		provider    AttributeProvider
		wantErr     bool
		errContains string
	}{
		{
			name:     "register single provider",
			provider: newResolverMockAttributeProvider("character"),
			wantErr:  false,
		},
		{
			name: "register duplicate namespace",
			setup: func(r *Resolver) {
				_ = r.RegisterProvider(newResolverMockAttributeProvider("character"))
			},
			provider:    newResolverMockAttributeProvider("character"),
			wantErr:     true,
			errContains: "already registered",
		},
		{
			name:        "register empty namespace",
			provider:    newResolverMockAttributeProvider(""),
			wantErr:     true,
			errContains: "namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewSchemaRegistry()
			resolver := NewResolver(registry)

			if tt.setup != nil {
				tt.setup(resolver)
			}

			err := resolver.RegisterProvider(tt.provider)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestResolver_RegisterEnvironmentProvider(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := &mockEnvironmentProvider{
		namespace: "environment",
		attrs: map[string]any{
			"time": "2026-02-12T12:00:00Z",
		},
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"time": types.AttrTypeString,
			},
		},
	}

	err := resolver.RegisterEnvironmentProvider(provider)
	require.NoError(t, err)

	// Duplicate should error
	err = resolver.RegisterEnvironmentProvider(provider)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestResolver_Resolve_SingleProvider(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.schema = &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role":  types.AttrTypeString,
			"level": types.AttrTypeInt,
			"owner": types.AttrTypeString,
			"type":  types.AttrTypeString,
		},
	}
	provider.subjectData["01ABC"] = map[string]any{
		"role":  "admin",
		"level": 5,
	}
	provider.resourceData["01XYZ"] = map[string]any{
		"owner": "01ABC",
		"type":  "room",
	}

	require.NoError(t, resolver.RegisterProvider(provider))

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "room:01XYZ",
	}

	bags, err := resolver.Resolve(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, bags)

	// Check subject attributes with namespace prefix
	assert.Equal(t, "admin", bags.Subject["character.role"])
	assert.Equal(t, 5, bags.Subject["character.level"])

	// Check resource attributes with namespace prefix
	assert.Equal(t, "01ABC", bags.Resource["character.owner"])
	assert.Equal(t, "room", bags.Resource["character.type"])

	// Check action
	assert.Equal(t, "read", bags.Action["name"])
}

func TestResolver_Resolve_MultipleProviders(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// First provider (character namespace)
	provider1 := newResolverMockAttributeProvider("character")
	provider1.schema = &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role":  types.AttrTypeString,
			"level": types.AttrTypeInt,
		},
	}
	provider1.subjectData["01ABC"] = map[string]any{
		"role":  "admin",
		"level": 5,
	}

	// Second provider (permissions namespace)
	provider2 := newResolverMockAttributeProvider("permissions")
	provider2.schema = &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role":        types.AttrTypeString,
			"permissions": types.AttrTypeStringList,
		},
	}
	provider2.subjectData["01ABC"] = map[string]any{
		"role":        "superuser", // Duplicate key - should override
		"permissions": []string{"read", "write"},
	}

	require.NoError(t, resolver.RegisterProvider(provider1))
	require.NoError(t, resolver.RegisterProvider(provider2))

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "room:01XYZ",
	}

	bags, err := resolver.Resolve(context.Background(), req)
	require.NoError(t, err)

	// First provider's role
	assert.Equal(t, "admin", bags.Subject["character.role"])
	assert.Equal(t, 5, bags.Subject["character.level"])

	// Second provider's role (last-registered wins for scalars)
	assert.Equal(t, "superuser", bags.Subject["permissions.role"])
	assert.Equal(t, []string{"read", "write"}, bags.Subject["permissions.permissions"])
}

func TestResolver_Resolve_ListMerging(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider1 := newResolverMockAttributeProvider("provider1")
	provider1.schema = &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role": types.AttrTypeString,
			"tags": types.AttrTypeStringList,
		},
	}
	provider1.subjectData["01ABC"] = map[string]any{
		"tags": []string{"admin", "moderator"},
	}

	provider2 := newResolverMockAttributeProvider("provider2")
	provider2.schema = &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role": types.AttrTypeString,
			"tags": types.AttrTypeStringList,
		},
	}
	provider2.subjectData["01ABC"] = map[string]any{
		"tags": []string{"verified", "premium"},
	}

	require.NoError(t, resolver.RegisterProvider(provider1))
	require.NoError(t, resolver.RegisterProvider(provider2))

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "room:01XYZ",
	}

	bags, err := resolver.Resolve(context.Background(), req)
	require.NoError(t, err)

	// Both providers' tags should be in separate namespace keys
	assert.Equal(t, []string{"admin", "moderator"}, bags.Subject["provider1.tags"])
	assert.Equal(t, []string{"verified", "premium"}, bags.Subject["provider2.tags"])
}

func TestResolver_Resolve_ProviderError(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Provider that errors
	provider1 := newResolverMockAttributeProvider("failing")
	provider1.subjectError = errors.New("database error")

	// Provider that works
	provider2 := newResolverMockAttributeProvider("working")
	provider2.subjectData["01ABC"] = map[string]any{
		"role": "user",
	}

	require.NoError(t, resolver.RegisterProvider(provider1))
	require.NoError(t, resolver.RegisterProvider(provider2))

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "room:01XYZ",
	}

	bags, err := resolver.Resolve(context.Background(), req)
	require.NoError(t, err) // Should not error, just skip failing provider

	// Working provider's data should be present
	assert.Equal(t, "user", bags.Subject["working.role"])

	// Failing provider's data should not be present
	_, exists := bags.Subject["failing.role"]
	assert.False(t, exists)
}

func TestResolver_Resolve_ProviderPanic(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Provider that panics
	provider1 := newResolverMockAttributeProvider("panicking")
	provider1.shouldPanic = true

	// Provider that works
	provider2 := newResolverMockAttributeProvider("working")
	provider2.subjectData["01ABC"] = map[string]any{
		"role": "user",
	}

	require.NoError(t, resolver.RegisterProvider(provider1))
	require.NoError(t, resolver.RegisterProvider(provider2))

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "room:01XYZ",
	}

	// Should not panic, just skip the panicking provider
	bags, err := resolver.Resolve(context.Background(), req)
	require.NoError(t, err)

	// Working provider's data should be present
	assert.Equal(t, "user", bags.Subject["working.role"])
}

func TestResolver_Resolve_PerRequestCache(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.subjectData["01ABC"] = map[string]any{
		"role": "admin",
	}
	provider.resourceData["01ABC"] = map[string]any{
		"role": "admin",
	}

	require.NoError(t, resolver.RegisterProvider(provider))

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "character:01ABC", // Same entity as subject
	}

	bags, err := resolver.Resolve(context.Background(), req)
	require.NoError(t, err)

	// Should be called once each for subject and resource
	// (different resolve types, separate cache keys)
	subjectCalls := provider.callCount["subject:01ABC"]
	resourceCalls := provider.callCount["resource:01ABC"]
	assert.Equal(t, 1, subjectCalls)
	assert.Equal(t, 1, resourceCalls)

	// Both should have the data
	assert.Equal(t, "admin", bags.Subject["character.role"])
	assert.Equal(t, "admin", bags.Resource["character.role"])
}

func TestResolver_Resolve_ReEntranceGuard(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Create a provider that tries to call resolver again
	reentrantProvider := &reentrantMockProvider{
		namespace: "reentrant",
		resolver:  resolver,
	}

	require.NoError(t, resolver.RegisterProvider(reentrantProvider))

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "room:01XYZ",
	}

	// Re-entrance panic is caught by safeResolve's recovery — outer call succeeds
	bags, err := resolver.Resolve(context.Background(), req)
	require.NoError(t, err)

	// Reentrant provider's attributes should be empty (its call was recovered)
	_, exists := bags.Subject["reentrant.test"]
	assert.False(t, exists)
}

// Mock provider that attempts re-entrance
type reentrantMockProvider struct {
	namespace string
	resolver  *Resolver
}

func (r *reentrantMockProvider) Namespace() string {
	return r.namespace
}

func (r *reentrantMockProvider) ResolveSubject(ctx context.Context, _ string) (map[string]any, error) {
	// Try to call resolver again (re-entrance)
	req := types.AccessRequest{
		Subject:  "character:01XYZ",
		Action:   "read",
		Resource: "room:01ABC",
	}
	_, _ = r.resolver.Resolve(ctx, req)
	return nil, nil
}

func (r *reentrantMockProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

func (r *reentrantMockProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"test": types.AttrTypeString,
		},
	}
}

func TestResolver_Resolve_UnknownEntityType(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	require.NoError(t, resolver.RegisterProvider(provider))

	req := types.AccessRequest{
		Subject:  "unknown:01ABC",
		Action:   "read",
		Resource: "room:01XYZ",
	}

	bags, err := resolver.Resolve(context.Background(), req)
	require.NoError(t, err)

	// Should have empty subject bag (no provider for "unknown" type)
	assert.Empty(t, bags.Subject)
	// Should still have action
	assert.Equal(t, "read", bags.Action["name"])
}

func TestResolver_Resolve_EnvironmentProvider(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	envProvider := &mockEnvironmentProvider{
		namespace: "environment",
		attrs: map[string]any{
			"time":       "2026-02-12T12:00:00Z",
			"ip_address": "192.168.1.1",
		},
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"time":       types.AttrTypeString,
				"ip_address": types.AttrTypeString,
			},
		},
	}

	require.NoError(t, resolver.RegisterEnvironmentProvider(envProvider))

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "room:01XYZ",
	}

	bags, err := resolver.Resolve(context.Background(), req)
	require.NoError(t, err)

	// Check environment attributes with namespace prefix
	assert.Equal(t, "2026-02-12T12:00:00Z", bags.Environment["environment.time"])
	assert.Equal(t, "192.168.1.1", bags.Environment["environment.ip_address"])
}

func TestResolver_Resolve_InvalidEntityIDFormat(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	require.NoError(t, resolver.RegisterProvider(provider))

	req := types.AccessRequest{
		Subject:  "invalid_no_colon",
		Action:   "read",
		Resource: "room:01XYZ",
	}

	bags, err := resolver.Resolve(context.Background(), req)
	require.NoError(t, err)

	// Should have empty subject bag (invalid format)
	assert.Empty(t, bags.Subject)
}

func TestResolver_Resolve_S6_NamespaceValidation(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Provider schema only registers "role" — "unregistered_key" is NOT in schema
	provider := newResolverMockAttributeProvider("character")
	provider.schema = &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role": types.AttrTypeString,
		},
	}
	provider.subjectData["01ABC"] = map[string]any{
		"role":             "admin",
		"unregistered_key": "should be rejected",
	}

	require.NoError(t, resolver.RegisterProvider(provider))

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "room:01XYZ",
	}

	bags, err := resolver.Resolve(context.Background(), req)
	require.NoError(t, err)

	// Registered key should be present
	assert.Equal(t, "admin", bags.Subject["character.role"])

	// Unregistered key MUST be rejected (S6)
	_, exists := bags.Subject["character.unregistered_key"]
	assert.False(t, exists, "unregistered key must be rejected per Spec S6")
}

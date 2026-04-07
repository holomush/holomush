// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/pkg/errutil"
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

func TestResolverUnregisterProviderRemovesRegisteredProviderAndFreesNamespaceForReregistration(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Register two providers so we can assert selective removal and
	// preservation of providerOrder for the survivor.
	widgetProvider := newResolverMockAttributeProvider("widget")
	characterProvider := newResolverMockAttributeProvider("character")
	require.NoError(t, resolver.RegisterProvider(widgetProvider))
	require.NoError(t, resolver.RegisterProvider(characterProvider))

	// Unregister widget: should return true, remove from providers map,
	// remove from providerOrder, and drop the circuit breaker.
	removed := resolver.UnregisterProvider("widget")
	assert.True(t, removed, "UnregisterProvider should return true for a registered namespace")
	assert.NotContains(t, resolver.providers, "widget")
	assert.NotContains(t, resolver.circuitBreakers, "widget")
	assert.NotContains(t, resolver.providerOrder, "widget")
	assert.Contains(t, resolver.providers, "character", "unrelated provider must remain registered")
	assert.Contains(t, resolver.providerOrder, "character")

	// Unregister a namespace that was never registered: should return false
	// and be a safe no-op.
	removed = resolver.UnregisterProvider("nonexistent")
	assert.False(t, removed, "UnregisterProvider should return false for an unknown namespace")

	// After rollback, the namespace must be free for re-registration —
	// otherwise a retry after a failed load would collide with the stale
	// entry and produce a confusing "already registered" error.
	replacementProvider := newResolverMockAttributeProvider("widget")
	require.NoError(t, resolver.RegisterProvider(replacementProvider),
		"namespace must be available for re-registration after UnregisterProvider")
}

func TestResolverRegisterEnvironmentProvider(t *testing.T) {
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

func TestResolverResolveSingleProvider(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.schema = &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role":  types.AttrTypeString,
			"level": types.AttrTypeFloat,
			"owner": types.AttrTypeString,
			"type":  types.AttrTypeString,
		},
	}
	provider.subjectData["character:01ABC"] = map[string]any{
		"role":  "admin",
		"level": 5,
	}
	provider.resourceData["room:01XYZ"] = map[string]any{
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

func TestResolverResolveMultipleProviders(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// First provider (character namespace)
	provider1 := newResolverMockAttributeProvider("character")
	provider1.schema = &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role":  types.AttrTypeString,
			"level": types.AttrTypeFloat,
		},
	}
	provider1.subjectData["character:01ABC"] = map[string]any{
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
	provider2.subjectData["character:01ABC"] = map[string]any{
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

func TestResolverResolveListMerging(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider1 := newResolverMockAttributeProvider("provider1")
	provider1.schema = &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role": types.AttrTypeString,
			"tags": types.AttrTypeStringList,
		},
	}
	provider1.subjectData["character:01ABC"] = map[string]any{
		"tags": []string{"admin", "moderator"},
	}

	provider2 := newResolverMockAttributeProvider("provider2")
	provider2.schema = &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role": types.AttrTypeString,
			"tags": types.AttrTypeStringList,
		},
	}
	provider2.subjectData["character:01ABC"] = map[string]any{
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

func TestResolverResolveProviderError(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Provider that errors
	provider1 := newResolverMockAttributeProvider("failing")
	provider1.subjectError = errors.New("database error")

	// Provider that works
	provider2 := newResolverMockAttributeProvider("working")
	provider2.subjectData["character:01ABC"] = map[string]any{
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
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database error")

	// Working provider's data should be present
	assert.Equal(t, "user", bags.Subject["working.role"])

	// Failing provider's data should not be present
	_, exists := bags.Subject["failing.role"]
	assert.False(t, exists)
}

func TestResolverResolveProviderPanic(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Provider that panics
	provider1 := newResolverMockAttributeProvider("panicking")
	provider1.shouldPanic = true

	// Provider that works
	provider2 := newResolverMockAttributeProvider("working")
	provider2.subjectData["character:01ABC"] = map[string]any{
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
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panicked")

	// Working provider's data should be present
	assert.Equal(t, "user", bags.Subject["working.role"])
}

func TestResolverResolvePerRequestCache(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.subjectData["character:01ABC"] = map[string]any{
		"role": "admin",
	}
	provider.resourceData["character:01ABC"] = map[string]any{
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
	subjectCalls := provider.callCount["subject:character:01ABC"]
	resourceCalls := provider.callCount["resource:character:01ABC"]
	assert.Equal(t, 1, subjectCalls)
	assert.Equal(t, 1, resourceCalls)

	// Both should have the data
	assert.Equal(t, "admin", bags.Subject["character.role"])
	assert.Equal(t, "admin", bags.Resource["character.role"])
}

func TestResolverResolveReEntranceGuard(t *testing.T) {
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

	// Re-entrance panic is caught by safeResolve's recovery — outer call returns error
	bags, err := resolver.Resolve(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panicked")

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

func TestResolverResolveUnknownEntityType(t *testing.T) {
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

func TestResolverResolveEnvironmentProvider(t *testing.T) {
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

func TestResolverResolveInvalidEntityIDFormat(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	require.NoError(t, resolver.RegisterProvider(provider))

	req := types.AccessRequest{
		Subject:  "invalid_no_colon",
		Action:   "read",
		Resource: "room:01XYZ",
	}

	_, err := resolver.Resolve(context.Background(), req)
	require.Error(t, err)
}

func TestResolverResolveNamespaceValidationRejectsUnregisteredKeys(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Provider schema only registers "role" — "unregistered_key" is NOT in schema
	provider := newResolverMockAttributeProvider("character")
	provider.schema = &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"role": types.AttrTypeString,
		},
	}
	provider.subjectData["character:01ABC"] = map[string]any{
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

func TestResolverResolveSubjectAttributesPopulatesSubjectActionAndEnvironment(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Subject provider returns {role: "admin"} for character:01ABC.
	subjectProvider := newResolverMockAttributeProvider("character")
	subjectProvider.subjectData["character:01ABC"] = map[string]any{"role": "admin"}
	require.NoError(t, resolver.RegisterProvider(subjectProvider))

	// Environment provider returns {hour: 14.0} — type matches the
	// AttrTypeFloat schema declaration so the test still passes if
	// mergeAttributes ever adds runtime type enforcement.
	envProvider := &mockEnvironmentProvider{
		namespace: "env",
		attrs:     map[string]any{"hour": float64(14)},
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{"hour": types.AttrTypeFloat},
		},
	}
	require.NoError(t, resolver.RegisterEnvironmentProvider(envProvider))

	bags, err := resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "read")
	require.NoError(t, err)
	require.NotNil(t, bags)

	assert.Equal(t, "admin", bags.Subject["character.role"])
	assert.Equal(t, float64(14), bags.Environment["env.hour"])
	assert.Equal(t, "read", bags.Action["name"])
	assert.Empty(t, bags.Resource, "resource bag must be empty at preflight")
}

func TestResolverResolveSubjectAttributesReturnsErrorForInvalidSubjectRef(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Bags may be non-nil here (validateEntityRef is invoked after the
	// resolution scope is entered), but the contract is the error is
	// non-nil. Callers MUST fail closed regardless of bag state.
	_, err := resolver.ResolveSubjectAttributes(context.Background(), "malformed", "read")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_ENTITY_REF")
}

func TestResolverResolveSubjectAttributesReturnsErrorWhenSubjectProviderFails(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.subjectError = errors.New("database unavailable")
	require.NoError(t, resolver.RegisterProvider(provider))

	_, err := resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "read")
	require.Error(t, err)
	assert.ErrorContains(t, err, "database unavailable")
}

func TestResolverResolveSubjectAttributesDoesNotInvokeResourceProviderWhenResourceProviderExists(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Subject provider in character namespace — contributes the subject bag.
	subjectProvider := newResolverMockAttributeProvider("character")
	subjectProvider.subjectData["character:01ABC"] = map[string]any{"role": "admin"}
	require.NoError(t, resolver.RegisterProvider(subjectProvider))

	// Resource provider in a different namespace. It has resource data that
	// would be returned if ResolveResource were called — we use the mock's
	// per-key callCount (keyed "resource:<id>") to assert zero calls.
	resourceProvider := newResolverMockAttributeProvider("widget")
	resourceProvider.resourceData["widget:restricted-1"] = map[string]any{"type": "restricted"}
	require.NoError(t, resolver.RegisterProvider(resourceProvider))

	bags, err := resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "read")
	require.NoError(t, err)

	// Output invariant: the returned bags.Resource MUST be empty. This
	// guards against a regression that populates the resource bag through
	// some path other than ResolveResource (e.g., a future batch API).
	assert.Empty(t, bags.Resource, "bags.Resource must be empty after preflight resolution")

	// Mechanism invariant: the resource provider's ResolveResource MUST
	// NOT have been invoked. Use a positive per-key assertion rather than
	// a filter-loop so the check fails loudly if the mock is refactored.
	assert.Zero(t, resourceProvider.callCount["resource:widget:restricted-1"],
		"ResolveResource must not be called during subject-only resolution")
	// Defense in depth: scan for any other "resource:" keys that may
	// have been introduced. The positive assertion above is the load-bearing
	// check; this catches the case where a future code path sends a
	// different resource ID we didn't predict.
	for key, n := range resourceProvider.callCount {
		if strings.HasPrefix(key, "resource:") {
			assert.Zero(t, n, "unexpected resource-provider call: %s", key)
		}
	}
}

func TestResolverResolveSubjectAttributesReturnsErrorWhenSubjectIsEmpty(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	bags, err := resolver.ResolveSubjectAttributes(context.Background(), "", "read")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_ENTITY_REF")
	errutil.AssertErrorContext(t, err, "field", "subject")
	// Empty-subject validation runs before the resolution scope is entered,
	// so bags is nil (no partial work to return).
	assert.Nil(t, bags)
}

func TestResolverResolveSubjectAttributesPopulatesActionNameEvenWhenEmpty(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.subjectData["character:01ABC"] = map[string]any{"role": "admin"}
	require.NoError(t, resolver.RegisterProvider(provider))

	bags, err := resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "")
	require.NoError(t, err)
	require.NotNil(t, bags)

	// Verify the action name key is actually PRESENT (not just zero-value
	// from a missing key lookup). The contract is "populates action name
	// even when empty", so the key must exist.
	actionName, ok := bags.Action["name"]
	require.True(t, ok, "action name key must be present in bags.Action")
	assert.Equal(t, "", actionName)
}

func TestResolverResolveSubjectAttributesReturnsErrorWhenContextAlreadyCancelled(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	cancelAware := &ctxAwareSubjectProvider{}
	require.NoError(t, resolver.RegisterProvider(cancelAware))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := resolver.ResolveSubjectAttributes(ctx, "character:01ABC", "read")
	require.Error(t, err, "cancelled context must produce an error via context-aware provider")
	assert.ErrorIs(t, err, context.Canceled,
		"error chain must include context.Canceled so callers can distinguish cancellation")
}

// ctxAwareSubjectProvider honors context cancellation in ResolveSubject.
// Lives in resolver_test.go next to the tests that use it.
type ctxAwareSubjectProvider struct{}

func (p *ctxAwareSubjectProvider) Namespace() string { return "character" }

func (p *ctxAwareSubjectProvider) ResolveSubject(ctx context.Context, _ string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (p *ctxAwareSubjectProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

func (p *ctxAwareSubjectProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{"role": types.AttrTypeString},
	}
}

func TestResolverResolveSubjectAttributesRecoversFromPanickingProvider(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.shouldPanic = true
	require.NoError(t, resolver.RegisterProvider(provider))

	_, err := resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "read")
	require.Error(t, err, "panic must be recovered and returned as error")
	assert.Contains(t, err.Error(), "panicked")
	// Pin the panic to the right provider and resolve type so a future
	// refactor that reroutes panics to a different namespace would fail
	// this test loudly.
	errutil.AssertErrorContext(t, err, "namespace", "character")
	errutil.AssertErrorContext(t, err, "resolve_type", "subject")
}

func TestResolverResolveSubjectAttributesDetectsReentranceAndReturnsErrorNotInfiniteLoop(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Provider that re-calls the resolver from inside ResolveSubject.
	// The inner call will hit the re-entrance guard and panic; safeResolve
	// catches the panic and converts it to an error. The test verifies
	// this chain works and the resolver does not infinite-loop.
	reentrant := &reentrantSubjectProvider{resolver: resolver}
	require.NoError(t, resolver.RegisterProvider(reentrant))

	_, err := resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "read")
	require.Error(t, err, "re-entrance should be detected and converted to an error via safeResolve")
	assert.Contains(t, err.Error(), "panicked",
		"the re-entrance panic should be caught by safeResolve and surfaced as a provider-panic error")
	// Pin the surfaced error to the namespace and resolve type so a
	// future refactor cannot silently route the re-entrance error
	// somewhere else.
	errutil.AssertErrorContext(t, err, "namespace", "character")
	errutil.AssertErrorContext(t, err, "resolve_type", "subject")
}

// reentrantSubjectProvider intentionally recurses into the resolver from
// within ResolveSubject to exercise the re-entrance guard. Used only by T20.
type reentrantSubjectProvider struct {
	resolver *Resolver
}

func (p *reentrantSubjectProvider) Namespace() string { return "character" }

func (p *reentrantSubjectProvider) ResolveSubject(ctx context.Context, _ string) (map[string]any, error) {
	// This recursive call is illegal. The resolver's re-entrance guard
	// at the top of ResolveSubjectAttributes will panic; safeResolve
	// (the caller of this method) will catch it.
	//
	// The "character:02XYZ" subject is incidental — any well-formed
	// entity ref triggers the recursive entry, and the test does not
	// assert anything about it.
	_, _ = p.resolver.ResolveSubjectAttributes(ctx, "character:02XYZ", "read")
	return nil, nil
}

func (p *reentrantSubjectProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

func (p *reentrantSubjectProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{"role": types.AttrTypeString},
	}
}

func TestResolverResolveSubjectAttributesProducesSameSubjectBagAsResolveForSameInput(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.subjectData["character:01ABC"] = map[string]any{"role": "admin"}
	// Also give the provider resource data so Resolve doesn't error.
	// ResolveSubjectAttributes ignores this.
	provider.resourceData["character:test-id"] = map[string]any{"role": "ignored"}
	require.NoError(t, resolver.RegisterProvider(provider))

	// Register an environment provider so the cross-check covers the
	// environment bag in addition to subject and action. Both methods
	// use the same r.resolveEnvironment helper, so any divergence here
	// would represent a regression in C1's behavior preservation.
	envProvider := &mockEnvironmentProvider{
		namespace: "env",
		attrs:     map[string]any{"hour": float64(14)},
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{"hour": types.AttrTypeFloat},
		},
	}
	require.NoError(t, resolver.RegisterEnvironmentProvider(envProvider))

	// Call ResolveSubjectAttributes.
	preflightBags, preflightErr := resolver.ResolveSubjectAttributes(
		context.Background(), "character:01ABC", "read")
	require.NoError(t, preflightErr)

	// Call Resolve with an access request that includes a resource.
	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "character:test-id",
	}
	fullBags, fullErr := resolver.Resolve(context.Background(), req)
	require.NoError(t, fullErr)

	// Subject bags MUST be identical between the two paths — this is the
	// C1 invariant that ResolveSubjectAttributes is behavior-preserving
	// for the subject path, differing only by not calling resource
	// providers.
	assert.Equal(t, fullBags.Subject, preflightBags.Subject,
		"ResolveSubjectAttributes and Resolve must produce identical Subject bags for the same (subject, action)")
	assert.Equal(t, fullBags.Action["name"], preflightBags.Action["name"],
		"action name must match between the two paths")
	// Environment bags MUST also be identical — both methods route
	// environment resolution through the same r.resolveEnvironment helper.
	assert.Equal(t, fullBags.Environment, preflightBags.Environment,
		"environment bags must be identical between Resolve and ResolveSubjectAttributes")
}

func TestResolverResolveSubjectAttributesIsSafeForConcurrentCalls(t *testing.T) {
	// concurrentCallCount goroutines drives the smoke test. Kept as a
	// constant so the channel buffer and the loop bound can never drift.
	const concurrentCallCount = 100

	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Use a dedicated concurrency-safe provider rather than
	// resolverMockAttributeProvider, which mutates an unsynchronized
	// callCount map on every call and would itself race under -race.
	// This test targets resolver concurrency safety, not mock bookkeeping.
	provider := &concurrentSubjectProvider{role: "admin"}
	require.NoError(t, resolver.RegisterProvider(provider))

	var wg sync.WaitGroup
	errCh := make(chan error, concurrentCallCount)

	for i := 0; i < concurrentCallCount; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			subject := fmt.Sprintf("character:%03d", n)
			bags, err := resolver.ResolveSubjectAttributes(context.Background(), subject, "read")
			if err != nil {
				errCh <- err
				return
			}
			if bags.Subject["character.role"] != "admin" {
				errCh <- fmt.Errorf("subject bag mismatch for %s: got %v", subject, bags.Subject["character.role"])
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent ResolveSubjectAttributes error: %v", err)
	}
}

// concurrentSubjectProvider is a read-only, allocation-per-call provider
// used by the concurrency test. It deliberately performs no shared-state
// writes so that any race reported under -race must originate in the
// resolver itself, which is what the test exists to verify.
type concurrentSubjectProvider struct {
	role string
}

func (p *concurrentSubjectProvider) Namespace() string { return "character" }

func (p *concurrentSubjectProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return map[string]any{"role": p.role}, nil
}

func (p *concurrentSubjectProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

func (p *concurrentSubjectProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{"role": types.AttrTypeString},
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/samber/oops"
)

// Resolver resolves attributes for access requests
type Resolver struct {
	registry         *SchemaRegistry
	providers        map[string]AttributeProvider
	envProviders     map[string]EnvironmentProvider
	providerOrder    []string // Track registration order
	envProviderOrder []string
	logger           *slog.Logger
}

// NewResolver creates a new attribute resolver
func NewResolver(registry *SchemaRegistry) *Resolver {
	return &Resolver{
		registry:         registry,
		providers:        make(map[string]AttributeProvider),
		envProviders:     make(map[string]EnvironmentProvider),
		providerOrder:    make([]string, 0),
		envProviderOrder: make([]string, 0),
		logger:           slog.Default(),
	}
}

// RegisterProvider registers an attribute provider
func (r *Resolver) RegisterProvider(provider AttributeProvider) error {
	namespace := provider.Namespace()
	if namespace == "" {
		return oops.Errorf("provider namespace cannot be empty")
	}

	if _, exists := r.providers[namespace]; exists {
		return oops.Errorf("provider for namespace %q already registered", namespace)
	}

	r.providers[namespace] = provider
	r.providerOrder = append(r.providerOrder, namespace)

	// Register schema
	schema := provider.Schema()
	if schema != nil {
		if err := r.registry.Register(namespace, schema); err != nil {
			// If already registered, that's OK
			if !strings.Contains(err.Error(), "already registered") {
				return oops.Wrapf(err, "failed to register schema for namespace %q", namespace)
			}
		}
	}

	return nil
}

// RegisterEnvironmentProvider registers an environment provider
func (r *Resolver) RegisterEnvironmentProvider(provider EnvironmentProvider) error {
	namespace := provider.Namespace()
	if namespace == "" {
		return oops.Errorf("environment provider namespace cannot be empty")
	}

	if _, exists := r.envProviders[namespace]; exists {
		return oops.Errorf("environment provider for namespace %q already registered", namespace)
	}

	r.envProviders[namespace] = provider
	r.envProviderOrder = append(r.envProviderOrder, namespace)

	// Register schema
	schema := provider.Schema()
	if schema != nil {
		if err := r.registry.Register(namespace, schema); err != nil {
			// If already registered, that's OK
			if !strings.Contains(err.Error(), "already registered") {
				return oops.Wrapf(err, "failed to register schema for namespace %q", namespace)
			}
		}
	}

	return nil
}

// Resolve resolves all attributes for an access request
func (r *Resolver) Resolve(ctx context.Context, req types.AccessRequest) (*types.AttributeBags, error) {
	// Check re-entrance guard
	if isInResolution(ctx) {
		panic("resolver re-entrance detected: resolver cannot be called recursively")
	}

	// Mark as in resolution and attach cache
	ctx = markInResolution(ctx)
	ctx = withCache(ctx)

	bags := &types.AttributeBags{
		Subject:     make(map[string]any),
		Resource:    make(map[string]any),
		Action:      make(map[string]any),
		Environment: make(map[string]any),
	}

	// Parse subject and resource IDs
	subjectType, subjectID := splitEntityID(req.Subject)
	resourceType, resourceID := splitEntityID(req.Resource)

	// Set action name
	bags.Action["name"] = req.Action

	// Resolve subject attributes
	if subjectID != "" {
		r.resolveEntity(ctx, "subject", subjectType, subjectID, bags.Subject)
	}

	// Resolve resource attributes
	if resourceID != "" {
		r.resolveEntity(ctx, "resource", resourceType, resourceID, bags.Resource)
	}

	// Resolve environment attributes
	r.resolveEnvironment(ctx, bags.Environment)

	return bags, nil
}

// splitEntityID splits an entity ID in the format "type:id" into its components.
func splitEntityID(entityID string) (entityType, id string) {
	parts := strings.SplitN(entityID, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// resolveEntity resolves attributes for a single entity (subject or resource)
func (r *Resolver) resolveEntity(ctx context.Context, resolveType, _, entityID string, bag map[string]any) {
	cache := getCacheFromContext(ctx)

	// Try each provider in registration order
	for _, namespace := range r.providerOrder {
		provider := r.providers[namespace]

		// Build cache key
		cacheKey := fmt.Sprintf("%s:%s:%s", resolveType, namespace, entityID)

		// Check cache first
		if cached, found := cache.Get(cacheKey); found {
			r.mergeAttributes(namespace, cached, bag)
			continue
		}

		// Resolve from provider with panic recovery
		attrs := r.safeResolve(ctx, provider, resolveType, entityID)
		if attrs != nil {
			// Cache the result
			cache.Put(cacheKey, attrs)

			// Merge into bag
			r.mergeAttributes(namespace, attrs, bag)
		}
	}
}

// resolveEnvironment resolves environment attributes
func (r *Resolver) resolveEnvironment(ctx context.Context, bag map[string]any) {
	for _, namespace := range r.envProviderOrder {
		provider := r.envProviders[namespace]

		// Resolve with panic recovery
		attrs := r.safeResolveEnvironment(ctx, provider)
		if attrs != nil {
			r.mergeAttributes(namespace, attrs, bag)
		}
	}
}

// safeResolve calls a provider with error and panic recovery
func (r *Resolver) safeResolve(ctx context.Context, provider AttributeProvider, resolveType, entityID string) map[string]any {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.logger.Error("provider panicked during resolution",
				"namespace", provider.Namespace(),
				"resolve_type", resolveType,
				"entity_id", entityID,
				"panic", recovered,
			)
		}
	}()

	var attrs map[string]any
	var err error

	switch resolveType {
	case "subject":
		attrs, err = provider.ResolveSubject(ctx, entityID)
	case "resource":
		attrs, err = provider.ResolveResource(ctx, entityID)
	default:
		return nil
	}

	if err != nil {
		r.logger.Error("provider error during resolution",
			"namespace", provider.Namespace(),
			"resolve_type", resolveType,
			"entity_id", entityID,
			"error", err,
		)
		return nil
	}

	return attrs
}

// safeResolveEnvironment calls an environment provider with error and panic recovery
func (r *Resolver) safeResolveEnvironment(ctx context.Context, provider EnvironmentProvider) map[string]any {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.logger.Error("environment provider panicked during resolution",
				"namespace", provider.Namespace(),
				"panic", recovered,
			)
		}
	}()

	attrs, err := provider.Resolve(ctx)
	if err != nil {
		r.logger.Error("environment provider error during resolution",
			"namespace", provider.Namespace(),
			"error", err,
		)
		return nil
	}

	return attrs
}

// mergeAttributes merges attributes from a provider into a bag with namespace prefix
func (r *Resolver) mergeAttributes(namespace string, attrs, bag map[string]any) {
	for key, value := range attrs {
		// Use namespace.key format
		bagKey := fmt.Sprintf("%s.%s", namespace, key)
		bag[bagKey] = value
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/samber/oops"
)

// rejectedAttributesCounter counts provider attributes rejected because
// the key was not registered in the provider's namespace schema (S6).
var rejectedAttributesCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "abac_rejected_provider_attributes_total",
	Help: "Total number of provider attributes rejected due to namespace validation (S6)",
}, []string{"namespace", "key"})

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

	// Register schema (skip if namespace already registered to avoid fragile string matching)
	schema := provider.Schema()
	if schema != nil && !r.registry.HasNamespace(namespace) {
		if err := r.registry.Register(namespace, schema); err != nil {
			return oops.Wrapf(err, "failed to register schema for namespace %q", namespace)
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

	// Register schema (skip if namespace already registered to avoid fragile string matching)
	schema := provider.Schema()
	if schema != nil && !r.registry.HasNamespace(namespace) {
		if err := r.registry.Register(namespace, schema); err != nil {
			return oops.Wrapf(err, "failed to register schema for namespace %q", namespace)
		}
	}

	return nil
}

// Resolve resolves all attributes for an access request.
//
// On success, returns fully populated bags and nil error.
// On provider failure, returns partial bags alongside an error. The partial bags
// contain results from providers that succeeded; they are intended for diagnostics
// only. Callers MUST NOT use partial bags for policy evaluation — the engine
// discards them and fails closed when Resolve returns a non-nil error.
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

	// Set action name
	bags.Action["name"] = req.Action

	var errs []error

	// Resolve subject attributes
	if req.Subject != "" {
		if err := validateEntityRef(req.Subject); err != nil {
			errs = append(errs, oops.With("field", "subject").Wrap(err))
		} else if err := r.resolveEntity(ctx, "subject", req.Subject, bags.Subject); err != nil {
			errs = append(errs, err)
		}
	}

	// Resolve resource attributes
	if req.Resource != "" {
		if err := validateEntityRef(req.Resource); err != nil {
			errs = append(errs, oops.With("field", "resource").Wrap(err))
		} else if err := r.resolveEntity(ctx, "resource", req.Resource, bags.Resource); err != nil {
			errs = append(errs, err)
		}
	}

	// Resolve environment attributes
	if err := r.resolveEnvironment(ctx, bags.Environment); err != nil {
		errs = append(errs, err)
	}

	return bags, errors.Join(errs...)
}


// validateEntityRef checks that an entity reference is in "type:id" format
// with both parts non-empty. This ensures all providers receive validated refs
// and the ABAC fail-closed guarantee is preserved.
func validateEntityRef(ref string) error {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return oops.Code("INVALID_ENTITY_REF").
			With("entity_ref", ref).
			Errorf("invalid entity ref format: expected 'type:id'")
	}
	return nil
}

// resolveEntity resolves attributes for a single entity (subject or resource).
// It iterates all registered providers and merges their attributes into bag.
// Returns an error wrapping all individual provider errors; partial results
// from successful providers are still written to bag before the error is returned.
func (r *Resolver) resolveEntity(ctx context.Context, resolveType, entityRef string, bag map[string]any) error {
	cache := getCacheFromContext(ctx)

	var errs []error

	// Try each provider in registration order
	for _, namespace := range r.providerOrder {
		provider := r.providers[namespace]

		// Build cache key
		cacheKey := fmt.Sprintf("%s:%s:%s", resolveType, namespace, entityRef)

		// Check cache first
		if cached, found := cache.Get(cacheKey); found {
			r.mergeAttributes(namespace, cached, bag)
			continue
		}

		// Resolve from provider with panic recovery
		attrs, err := r.safeResolve(ctx, provider, resolveType, entityRef)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if attrs != nil {
			// Cache the result
			cache.Put(cacheKey, attrs)

			// Merge into bag
			r.mergeAttributes(namespace, attrs, bag)
		}
	}

	return errors.Join(errs...)
}

// resolveEnvironment resolves environment attributes
func (r *Resolver) resolveEnvironment(ctx context.Context, bag map[string]any) error {
	var errs []error

	for _, namespace := range r.envProviderOrder {
		provider := r.envProviders[namespace]

		// Resolve with panic recovery
		attrs, err := r.safeResolveEnvironment(ctx, provider)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if attrs != nil {
			r.mergeAttributes(namespace, attrs, bag)
		}
	}

	return errors.Join(errs...)
}

// safeResolve calls a provider with error and panic recovery
func (r *Resolver) safeResolve(ctx context.Context, provider AttributeProvider, resolveType, entityID string) (attrs map[string]any, retErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			attrs = nil // defense-in-depth: discard partially-mutated map
			r.logger.Error("provider panicked during resolution",
				"namespace", provider.Namespace(),
				"resolve_type", resolveType,
				"entity_id", entityID,
				"panic", recovered,
			)
			retErr = oops.
				With("namespace", provider.Namespace()).
				With("resolve_type", resolveType).
				With("entity_id", entityID).
				Errorf("provider %s panicked during %s resolution", provider.Namespace(), resolveType)
		}
	}()

	var err error

	switch resolveType {
	case "subject":
		attrs, err = provider.ResolveSubject(ctx, entityID)
	case "resource":
		attrs, err = provider.ResolveResource(ctx, entityID)
	default:
		return nil, nil
	}

	if err != nil {
		return nil, oops.With("namespace", provider.Namespace()).With("resolve_type", resolveType).With("entity_id", entityID).Wrap(err)
	}

	return attrs, nil
}

// safeResolveEnvironment calls an environment provider with error and panic recovery
func (r *Resolver) safeResolveEnvironment(ctx context.Context, provider EnvironmentProvider) (attrs map[string]any, retErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			attrs = nil // defense-in-depth: discard partially-mutated map
			r.logger.Error("environment provider panicked during resolution",
				"namespace", provider.Namespace(),
				"provider_type", "environment",
				"panic", recovered,
			)
			retErr = oops.
				With("namespace", provider.Namespace()).
				With("provider_type", "environment").
				Errorf("environment provider %s panicked during resolution", provider.Namespace())
		}
	}()

	var err error
	attrs, err = provider.Resolve(ctx)
	if err != nil {
		return nil, oops.With("namespace", provider.Namespace()).Wrap(err)
	}

	return attrs, nil
}

// mergeAttributes merges attributes from a provider into a bag with namespace prefix.
// Validates each key against the schema registry per Spec S6: keys not registered
// in the provider's namespace are rejected with warning logging and metric emission.
func (r *Resolver) mergeAttributes(namespace string, attrs, bag map[string]any) {
	for key, value := range attrs {
		// S6: Validate key is registered in the provider's namespace schema
		if !r.registry.IsRegistered(namespace, key) {
			r.logger.Warn("provider returned attribute not in registered schema",
				"namespace", namespace,
				"key", key,
			)
			rejectedAttributesCounter.WithLabelValues(namespace, key).Inc()
			continue // reject unregistered key
		}

		// Use namespace.key format
		bagKey := fmt.Sprintf("%s.%s", namespace, key)
		bag[bagKey] = value
	}
}

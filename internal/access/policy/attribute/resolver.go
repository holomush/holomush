// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
	circuitBreakers  map[string]*CircuitBreaker
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
		circuitBreakers:  make(map[string]*CircuitBreaker),
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
	r.circuitBreakers[namespace] = NewCircuitBreaker(namespace, DefaultCircuitBreakerConfig(), nil)

	// Register schema (skip if namespace already registered to avoid fragile string matching)
	schema := provider.Schema()
	if schema != nil && !r.registry.HasNamespace(namespace) {
		if err := r.registry.Register(namespace, schema); err != nil {
			return oops.Wrapf(err, "failed to register schema for namespace %q", namespace)
		}
	}

	return nil
}

// UnregisterProvider removes a provider from the resolver by namespace.
// Used during plugin load rollback to clean up a provider that was
// registered before a later load-time step (schema validation, policy
// install) failed.
//
// Removes:
//   - the provider from r.providers
//   - the namespace from r.providerOrder
//   - the per-namespace circuit breaker
//   - the schema from r.registry (via UnregisterForRollback)
//
// The schema cleanup is critical: without it, a replacement provider
// for the same namespace with a DIFFERENT schema would have its schema
// silently dropped because RegisterProvider's HasNamespace check would
// short-circuit re-registration.
//
// Returns true if a provider was removed, false if the namespace had
// no registered provider.
//
// Thread safety: Resolver register/unregister paths are not mutex-guarded.
// Callers MUST only invoke this during plugin load/unload, before Resolve
// is called concurrently. This matches the RegisterProvider contract.
func (r *Resolver) UnregisterProvider(namespace string) bool {
	if _, exists := r.providers[namespace]; !exists {
		return false
	}
	delete(r.providers, namespace)
	for i, ns := range r.providerOrder {
		if ns == namespace {
			r.providerOrder = append(r.providerOrder[:i], r.providerOrder[i+1:]...)
			break
		}
	}
	delete(r.circuitBreakers, namespace)
	// Remove the schema from the registry so a replacement provider with
	// a different schema can register cleanly. Safe here because rollback
	// only runs before any policies that reference this namespace could
	// have been installed.
	r.registry.UnregisterForRollback(namespace)
	return true
}

// RegisteredNamespaces returns the namespaces registered by RegisterProvider,
// in registration order. The returned slice is a defensive copy — caller may
// modify it freely. Environment-provider namespaces are NOT included; use
// RegisteredEnvironmentNamespaces for those.
//
// Used by internal/access/setup.warnOnMissingSeedCoverage (holomush-xxel) to
// validate that every namespace referenced by seed-policy DSL has a
// registered provider — without this introspection seam, a typo or missed
// wire silently default-denies every check against the affected seeds.
func (r *Resolver) RegisteredNamespaces() []string {
	out := make([]string, len(r.providerOrder))
	copy(out, r.providerOrder)
	return out
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
		} else {
			// Inject the raw entity ID so policies can compare via principal.id.
			// This allows policies like `resource.scene.owner == principal.id`
			// to work without requiring a provider to set the id explicitly.
			if idx := strings.Index(req.Subject, ":"); idx >= 0 {
				bags.Subject["id"] = req.Subject[idx+1:]
			}
			if err := r.resolveEntity(ctx, "subject", req.Subject, bags.Subject); err != nil {
				errs = append(errs, err)
			}
		}
	}

	// Resolve resource attributes
	if req.Resource != "" {
		if err := validateEntityRef(req.Resource); err != nil {
			errs = append(errs, oops.With("field", "resource").Wrap(err))
		} else {
			// Inject the raw entity ID so policies can compare via resource.id.
			if idx := strings.Index(req.Resource, ":"); idx >= 0 {
				bags.Resource["id"] = req.Resource[idx+1:]
			}
			if err := r.resolveEntity(ctx, "resource", req.Resource, bags.Resource); err != nil {
				errs = append(errs, err)
			}
		}
	}

	// Resolve environment attributes
	if err := r.resolveEnvironment(ctx, bags.Environment); err != nil {
		errs = append(errs, err)
	}

	return bags, errors.Join(errs...)
}

// ResolveSubjectAttributes resolves subject, action, and environment attributes
// for a type-level capability check (preflight). It never calls resource
// providers, which makes it safe to use when no resource instance is available.
//
// Returns AttributeBags with Subject/Action/Environment populated and Resource
// empty. Error semantics match Resolve: partial success returns both bags and
// error; callers MUST fail closed on non-nil error and MUST NOT use partial
// bags for policy evaluation.
func (r *Resolver) ResolveSubjectAttributes(ctx context.Context, subject, action string) (*types.AttributeBags, error) {
	return r.resolveSubjectAttributes(ctx, subject, action, false)
}

// resolveSubjectAttributes is the shared core for ResolveSubjectAttributes and
// ResolveSubject. When skipEnv is true the environment providers are not called,
// so a subject-only caller (host dispatch stamping via ResolveSubject) does not
// fail closed on an unrelated environment-provider error (holomush-eykuh.3).
func (r *Resolver) resolveSubjectAttributes(ctx context.Context, subject, action string, skipEnv bool) (*types.AttributeBags, error) {
	// Input validation runs BEFORE entering the resolution scope. Empty
	// subject is rejected with nil bags — no work has started, so there
	// is nothing partial to return. Resolve tolerates empty subjects, but
	// a type-level capability check always has a principal.
	if subject == "" {
		return nil, oops.Code("INVALID_ENTITY_REF").
			With("field", "subject").
			Errorf("subject is required for ResolveSubjectAttributes")
	}

	// Check re-entrance guard (shared with Resolve).
	if isInResolution(ctx) {
		panic("resolver re-entrance detected: resolver cannot be called recursively")
	}

	// Mark as in resolution and attach request-scoped cache.
	ctx = markInResolution(ctx)
	ctx = withCache(ctx)

	bags := &types.AttributeBags{
		Subject:     make(map[string]any),
		Resource:    make(map[string]any),
		Action:      make(map[string]any),
		Environment: make(map[string]any),
	}

	// Set action name — matches Resolve's contract for bags.Action.
	bags.Action["name"] = action

	var errs []error

	// Resolve subject attributes. Reuses validateEntityRef for format
	// consistency with Resolve.
	if err := validateEntityRef(subject); err != nil {
		errs = append(errs, oops.With("field", "subject").Wrap(err))
	} else {
		// Inject the raw entity ID so policies can compare via principal.id.
		// Mirrors Resolve's behavior to preserve the C1 invariant that both
		// methods produce identical Subject bags for the same (subject, action).
		if idx := strings.Index(subject, ":"); idx >= 0 {
			bags.Subject["id"] = subject[idx+1:]
		}
		if err := r.resolveEntity(ctx, "subject", subject, bags.Subject); err != nil {
			errs = append(errs, err)
		}
	}

	// Resolve environment attributes. Skipped for the subject-only path
	// (ResolveSubject): that caller consumes only the Subject bag, so coupling
	// it to environment-provider health would fail dispatch stamping closed on
	// an unrelated env-provider error (holomush-eykuh.3).
	if !skipEnv {
		if err := r.resolveEnvironment(ctx, bags.Environment); err != nil {
			errs = append(errs, err)
		}
	}

	// Resource providers are intentionally NOT called. The optimistic-permit
	// branch in engine.CanPerformAction handles permits whose conditions
	// reference resource attributes.

	return bags, errors.Join(errs...)
}

// ResolveSubject resolves the subject attribute bag for a host-vouched ABAC
// subject (e.g. "character:01ABC"), satisfying
// pluginauthz.AttributeResolver. It is the plugin hosts' delivery-time entry
// point for populating DispatchContext.Attributes (notably "location"); see
// holomush-eykuh.3. It delegates to ResolveSubjectAttributes with an empty
// action — only the subject bag is consumed — and returns just that bag so the
// caller never sees the action/environment/resource sub-bags. Environment
// providers are NOT consulted on this path (skipEnv): the subject bag is all the
// caller needs, so an unrelated environment-provider failure must not fail
// dispatch stamping closed (holomush-eykuh.3).
//
// A subject-provider failure (or an invalid ref) IS returned to the caller,
// which MUST treat it as fail-closed (leave Attributes nil). A subject with no
// registered provider resolves to a bag carrying only the raw "id" — harmless,
// since the host projects only string-valued keys it cares about.
func (r *Resolver) ResolveSubject(ctx context.Context, subject string) (map[string]any, error) {
	bags, err := r.resolveSubjectAttributes(ctx, subject, "", true)
	if err != nil {
		return nil, err
	}
	return bags.Subject, nil
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

		// Check circuit breaker — skip open-circuit providers
		if cb := r.circuitBreakers[namespace]; cb != nil && cb.ShouldSkip() {
			continue
		}

		// Build cache key
		cacheKey := fmt.Sprintf("%s:%s:%s", resolveType, namespace, entityRef)

		// Check cache first
		if cached, found := cache.Get(cacheKey); found {
			r.mergeAttributes(namespace, cached, bag)
			continue
		}

		// Resolve from provider with panic recovery and circuit breaker recording
		start := time.Now()
		attrs, err := r.safeResolve(ctx, provider, resolveType, entityRef)
		if cb := r.circuitBreakers[namespace]; cb != nil {
			if err != nil {
				// Record as high-budget call to push toward tripping
				cb.RecordCall(cb.config.OpenDuration, cb.config.OpenDuration/2)
			} else {
				cb.RecordCall(time.Since(start), cb.config.OpenDuration)
			}
		}
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
func (r *Resolver) safeResolve(ctx context.Context, provider AttributeProvider, resolveType, entityRef string) (attrs map[string]any, retErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			attrs = nil // defense-in-depth: discard partially-mutated map
			r.logger.Error(
				"provider panicked during resolution",
				"namespace", provider.Namespace(),
				"resolve_type", resolveType,
				"entity_ref", entityRef,
				"panic", recovered,
			)
			retErr = oops.
				With("namespace", provider.Namespace()).
				With("resolve_type", resolveType).
				With("entity_ref", entityRef).
				Errorf("provider %s panicked during %s resolution", provider.Namespace(), resolveType)
		}
	}()

	var err error

	switch resolveType {
	case "subject":
		attrs, err = provider.ResolveSubject(ctx, entityRef)
	case "resource":
		attrs, err = provider.ResolveResource(ctx, entityRef)
	default:
		return nil, oops.Code("INVALID_RESOLVE_TYPE").With("resolve_type", resolveType).Errorf("unknown resolve type")
	}

	if err != nil {
		return nil, oops.With("namespace", provider.Namespace()).With("resolve_type", resolveType).With("entity_ref", entityRef).Wrap(err)
	}

	return attrs, nil
}

// safeResolveEnvironment calls an environment provider with error and panic recovery
func (r *Resolver) safeResolveEnvironment(ctx context.Context, provider EnvironmentProvider) (attrs map[string]any, retErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			attrs = nil // defense-in-depth: discard partially-mutated map
			r.logger.Error(
				"environment provider panicked during resolution",
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
			r.logger.Warn(
				"provider returned attribute not in registered schema",
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

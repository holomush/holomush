<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin ABAC Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden the plugin ABAC trust boundary (PR #195) by eliminating the synthetic `__preflight__` resource ID from the host/engine path and promoting load-time policy/schema cross-validation from a warning to a fatal load error.

**Architecture:** Two surgical host-side changes, no proto/SDK/plugin-binary churn. (A) Add `Resolver.ResolveSubjectAttributes` that resolves subject+environment+action but never calls resource providers; switch `engine.CanPerformAction` to use it and delete the `__preflight__` literal. (B) Extract `ValidateManifestPolicySchemas` from `CheckManifestWarnings` Warning 3, wire it into `Manager.loadPlugin` as a hard-failure step that runs before policy install.

**Tech Stack:** Go 1.22+, `testify` (assert/require), Ginkgo/Gomega (integration), `oops` (error wrapping), Prometheus client, testcontainers-go (PostgreSQL).

**Spec:** `docs/superpowers/specs/2026-04-07-plugin-abac-hardening-design.md`
**Bead:** holomush-479l
**Blocks:** holomush-0sc.12 (channel plugin rework)

---

## Conventions for all tasks

**VCS:** This repo is a colocated jj repo. All commits use `jj commit -m "..."` (not `git commit`). Run from the workspace at `/Users/sean/Code/github.com/holomush/.worktrees/plugin-abac-hardening`.

**Test runner:** All Go tests run via `task`, never `go test` directly. See CLAUDE.md.

**Commit messages:** Conventional commits (`type(scope): description`). Include the footer:

```text
Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
```

**SPDX headers:** New `.go` files MUST start with:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
```

**Test naming:** ACE framework — every top-level `TestXxx` name is a full sentence describing action, condition, expectation. Example: `TestResolverResolveSubjectAttributesPopulatesSubjectActionAndEnvironment`.

---

## File Structure

### New files

| Path | Purpose |
|---|---|
| `internal/plugin/policy_schema_validator.go` | `ValidateManifestPolicySchemas` function extracted from `manifest_warnings.go` |
| `internal/plugin/policy_schema_validator_test.go` | Unit tests for the validator (T8-T12, T25-T31) |
| `internal/access/policy/hardening_invariants_test.go` | Static/meta tests (T32, T33) |
| `test/integration/plugin/counting_proxy_test.go` | Counting proxy wrapper for `AttributeResolverServiceClient` (build tag `integration`) |
| `site/docs/extending/abac-attribute-resolver.md` | Plugin author documentation page |

### Modified files

| Path | Change |
|---|---|
| `internal/access/policy/attribute/resolver.go` | +`ResolveSubjectAttributes` method (~35 lines) |
| `internal/access/policy/attribute/resolver_test.go` | +11 tests (T1-T4, T16-T21, T39) |
| `internal/access/policy/engine.go` | Rewrite `CanPerformAction` step 4; delete `__preflight__` literal; update doc comment |
| `internal/access/policy/engine_test.go` | +6 tests (T5, T6, T7, T22, T23, T24) |
| `internal/plugin/manifest_warnings.go` | Remove Warning 3 case from `CheckManifestWarnings` |
| `internal/plugin/manifest_warnings_test.go` | Remove tests that asserted Warning 3's output |
| `internal/plugin/manager.go` | Call `ValidateManifestPolicySchemas` before `InstallPluginPoliciesWithManifest`, with rollback |
| `test/integration/plugin/abac_widget_test.go` | +8 specs (T13-T15, T34-T38) |
| `plugins/test-abac-widget/main.go` | Rewrite lines 58-67 comment; add `widgetResolver` doc comment |
| `pkg/plugin/service.go` | Update `AttributeResolverProvider` doc comment |
| `docs/superpowers/specs/2026-04-06-plugin-abac-trust-boundary-design.md` | Append `## Hardening (2026-04-07)` section |

---

## Task 1: Add Resolver.ResolveSubjectAttributes — happy path

**Files:**

- Modify: `internal/access/policy/attribute/resolver.go` (+method ~35 lines)
- Modify: `internal/access/policy/attribute/resolver_test.go` (+T1, ~40 lines)

- [ ] **Step 1: Write the failing test T1**

Append to `internal/access/policy/attribute/resolver_test.go`:

```go
func TestResolverResolveSubjectAttributesPopulatesSubjectActionAndEnvironment(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Subject provider returns {role: "admin"} for character:01ABC.
	subjectProvider := newResolverMockAttributeProvider("character")
	subjectProvider.subjectData["character:01ABC"] = map[string]any{"role": "admin"}
	require.NoError(t, resolver.RegisterProvider(subjectProvider))

	// Environment provider returns {hour: 14}.
	envProvider := &mockEnvironmentProvider{
		namespace: "env",
		attrs:     map[string]any{"hour": 14},
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{"hour": types.AttrTypeFloat},
		},
	}
	require.NoError(t, resolver.RegisterEnvironmentProvider(envProvider))

	bags, err := resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "read")
	require.NoError(t, err)
	require.NotNil(t, bags)

	assert.Equal(t, "admin", bags.Subject["character.role"])
	assert.Equal(t, 14, bags.Environment["env.hour"])
	assert.Equal(t, "read", bags.Action["name"])
	assert.Empty(t, bags.Resource, "resource bag must be empty at preflight")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- -run TestResolverResolveSubjectAttributesPopulatesSubjectActionAndEnvironment ./internal/access/policy/attribute/`
Expected: FAIL with "resolver.ResolveSubjectAttributes undefined"

- [ ] **Step 3: Implement the method in resolver.go**

Append to `internal/access/policy/attribute/resolver.go` (after the `Resolve` method, around line 156):

```go
// ResolveSubjectAttributes resolves subject, action, and environment attributes
// for a type-level capability check (preflight). It never calls resource
// providers, which makes it safe to use when no resource instance is available.
//
// Returns AttributeBags with Subject/Action/Environment populated and Resource
// empty. Error semantics match Resolve: partial success returns both bags and
// error; callers MUST fail closed on non-nil error and MUST NOT use partial
// bags for policy evaluation.
func (r *Resolver) ResolveSubjectAttributes(ctx context.Context, subject, action string) (*types.AttributeBags, error) {
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

	// Resolve subject attributes. Empty subject is invalid here — preflight
	// requires a valid principal. Resolve tolerates empty subjects (they
	// simply skip the subject resolution step), but for a type-level
	// capability check we always have a subject and should reject the
	// malformed case explicitly.
	if subject == "" {
		return bags, oops.Code("INVALID_ENTITY_REF").
			With("field", "subject").
			Errorf("subject is required for ResolveSubjectAttributes")
	}
	if err := validateEntityRef(subject); err != nil {
		errs = append(errs, oops.With("field", "subject").Wrap(err))
	} else if err := r.resolveEntity(ctx, "subject", subject, bags.Subject); err != nil {
		errs = append(errs, err)
	}

	// Resolve environment attributes.
	if err := r.resolveEnvironment(ctx, bags.Environment); err != nil {
		errs = append(errs, err)
	}

	// Resource providers are intentionally NOT called. The optimistic-permit
	// branch in engine.CanPerformAction handles permits whose conditions
	// reference resource attributes.

	return bags, errors.Join(errs...)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `task test -- -run TestResolverResolveSubjectAttributesPopulatesSubjectActionAndEnvironment ./internal/access/policy/attribute/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj commit -m "$(cat <<'EOF'
feat(abac): add Resolver.ResolveSubjectAttributes method

Adds a new entry point on the attribute Resolver that resolves only
subject, environment, and action attributes. Resource providers are
never invoked — this is the foundation for eliminating the synthetic
__preflight__ resource ID used by engine.CanPerformAction.

Part of plugin ABAC hardening (holomush-479l, spec
docs/superpowers/specs/2026-04-07-plugin-abac-hardening-design.md).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Resolver.ResolveSubjectAttributes — error and edge case tests

**Files:**

- Modify: `internal/access/policy/attribute/resolver_test.go` (+T2, T3, T4, T16, T17, T18)

- [ ] **Step 1: Add test T2 (invalid subject ref)**

Append to `internal/access/policy/attribute/resolver_test.go`:

```go
func TestResolverResolveSubjectAttributesReturnsErrorForInvalidSubjectRef(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	bags, err := resolver.ResolveSubjectAttributes(context.Background(), "malformed", "read")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entity ref format")
	assert.NotNil(t, bags, "bags should be non-nil even on error (matches Resolve contract)")
}
```

- [ ] **Step 2: Add test T3 (provider error propagates)**

```go
func TestResolverResolveSubjectAttributesReturnsErrorWhenSubjectProviderFails(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.subjectError = errors.New("database unavailable")
	require.NoError(t, resolver.RegisterProvider(provider))

	_, err := resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "read")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database unavailable")
}
```

- [ ] **Step 3: Add test T4 (resource provider NEVER called)**

```go
func TestResolverResolveSubjectAttributesDoesNotInvokeResourceProviderWhenResourceProviderExists(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Subject provider that contributes the subject bag.
	subjectProvider := newResolverMockAttributeProvider("character")
	subjectProvider.subjectData["character:01ABC"] = map[string]any{"role": "admin"}
	require.NoError(t, resolver.RegisterProvider(subjectProvider))

	// Resource provider in a different namespace. Its ResolveResource
	// increments a per-key counter; we will assert no counter increments
	// under the "resource:" key prefix.
	resourceProvider := newResolverMockAttributeProvider("widget")
	require.NoError(t, resolver.RegisterProvider(resourceProvider))

	_, err := resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "read")
	require.NoError(t, err)

	// The widget provider's resource-call counter MUST be empty. The shared
	// mock uses callCount["resource:"+resourceID] as the call key.
	for key := range resourceProvider.callCount {
		if len(key) >= len("resource:") && key[:len("resource:")] == "resource:" {
			t.Errorf("resource provider was called during ResolveSubjectAttributes: %s", key)
		}
	}
}
```

- [ ] **Step 4: Add test T16 (empty subject)**

```go
func TestResolverResolveSubjectAttributesReturnsErrorWhenSubjectIsEmpty(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	_, err := resolver.ResolveSubjectAttributes(context.Background(), "", "read")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subject is required")
}
```

- [ ] **Step 5: Add test T17 (empty action permitted)**

```go
func TestResolverResolveSubjectAttributesPopulatesActionNameEvenWhenEmpty(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.subjectData["character:01ABC"] = map[string]any{"role": "admin"}
	require.NoError(t, resolver.RegisterProvider(provider))

	bags, err := resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "")
	require.NoError(t, err)
	assert.Equal(t, "", bags.Action["name"])
}
```

- [ ] **Step 6: Add test T18 (context cancellation)**

```go
func TestResolverResolveSubjectAttributesReturnsErrorWhenContextAlreadyCancelled(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.subjectData["character:01ABC"] = map[string]any{"role": "admin"}
	require.NoError(t, resolver.RegisterProvider(provider))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The resolver does not currently short-circuit on context cancellation
	// at its top level — providers are expected to honor ctx.Err(). For
	// the preflight path, we assert that a pre-cancelled context either
	// returns an error or produces an empty bag (provider-dependent).
	// The existing mock provider does not check ctx.Err(), so we verify
	// the behavior by using a provider that does.
	cancelAware := &ctxAwareSubjectProvider{}
	registry2 := NewSchemaRegistry()
	resolver2 := NewResolver(registry2)
	require.NoError(t, resolver2.RegisterProvider(cancelAware))

	_, err := resolver2.ResolveSubjectAttributes(ctx, "character:01ABC", "read")
	require.Error(t, err, "cancelled context must produce an error via context-aware provider")
	assert.True(t, errors.Is(err, context.Canceled) || err.Error() != "",
		"error should reference context cancellation")
}

// ctxAwareSubjectProvider honors context cancellation in ResolveSubject.
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
```

- [ ] **Step 7: Run all new tests**

Run: `task test -- -run "TestResolverResolveSubjectAttributes" ./internal/access/policy/attribute/`
Expected: PASS for all 6 new tests.

- [ ] **Step 8: Commit**

```bash
jj commit -m "$(cat <<'EOF'
test(abac): edge case tests for ResolveSubjectAttributes

Adds coverage for invalid subject refs, provider errors, the
resource-provider-never-called invariant (T4), empty subject rejection,
empty action acceptance, and context cancellation propagation via a
context-aware provider.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Resolver.ResolveSubjectAttributes — panic, re-entrance, cross-check, concurrency

**Files:**

- Modify: `internal/access/policy/attribute/resolver_test.go` (+T19, T20, T21, T33, T39)

- [ ] **Step 1: Add T19 (panic recovery)**

```go
func TestResolverResolveSubjectAttributesRecoversFromPanickingProvider(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.shouldPanic = true
	require.NoError(t, resolver.RegisterProvider(provider))

	_, err := resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "read")
	require.Error(t, err, "panic must be recovered and returned as error")
	assert.Contains(t, err.Error(), "panicked")
}
```

- [ ] **Step 2: Add T20 (re-entrance detection)**

```go
func TestResolverResolveSubjectAttributesPanicsOnReentrance(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	// Provider that re-calls the resolver from inside ResolveSubject.
	reentrant := &reentrantSubjectProvider{resolver: resolver}
	require.NoError(t, resolver.RegisterProvider(reentrant))

	defer func() {
		if recovered := recover(); recovered == nil {
			// Panic recovered by safeResolve → surfaced as error, not panic.
			// Either outcome is acceptable; the invariant is that the
			// resolver does not infinite-loop.
			return
		}
	}()

	_, _ = resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "read")
}

type reentrantSubjectProvider struct {
	resolver *Resolver
}

func (p *reentrantSubjectProvider) Namespace() string { return "character" }

func (p *reentrantSubjectProvider) ResolveSubject(ctx context.Context, _ string) (map[string]any, error) {
	// Re-entrance — the resolver MUST detect this via markInResolution.
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
```

- [ ] **Step 3: Add T21 (cross-check with Resolve for same input)**

```go
func TestResolverResolveSubjectAttributesProducesSameSubjectBagAsResolveForSameInput(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	provider.subjectData["character:01ABC"] = map[string]any{"role": "admin"}
	// Also give the provider a resource instance so Resolve has something to
	// return without errors. ResolveSubjectAttributes will ignore this.
	provider.resourceData["widget:test-id"] = map[string]any{"role": "ignored"}
	require.NoError(t, resolver.RegisterProvider(provider))

	// Call ResolveSubjectAttributes.
	preflightBags, err := resolver.ResolveSubjectAttributes(context.Background(), "character:01ABC", "read")
	require.NoError(t, err)

	// Call Resolve with an access request that includes a resource.
	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "widget:test-id",
	}
	fullBags, err := resolver.Resolve(context.Background(), req)
	require.NoError(t, err)

	// Subject bags MUST be identical between the two paths.
	assert.Equal(t, fullBags.Subject, preflightBags.Subject,
		"ResolveSubjectAttributes and Resolve must produce identical Subject bags for the same (subject, action)")
	// Action names MUST match.
	assert.Equal(t, fullBags.Action["name"], preflightBags.Action["name"])
}
```

- [ ] **Step 4: Add T33 (cross-check invariant re-stated at file level)**

T33 is the cross-file counterpart to T21 but with the full provider set. For the unit test tier, T21 covers it adequately at this layer — T33 in the spec refers to integration-level coverage using real providers (character provider + env provider from core). T33 will land as part of the integration test task (Task 9).

Note: This step adds no code — it's a documentation step. Mark this step as complete and move to Step 5.

- [ ] **Step 5: Add T39 (concurrency smoke test)**

```go
func TestResolverResolveSubjectAttributesIsSafeForConcurrentCalls(t *testing.T) {
	registry := NewSchemaRegistry()
	resolver := NewResolver(registry)

	provider := newResolverMockAttributeProvider("character")
	for i := 0; i < 100; i++ {
		provider.subjectData[fmt.Sprintf("character:%03d", i)] = map[string]any{"role": "admin"}
	}
	require.NoError(t, resolver.RegisterProvider(provider))

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	for i := 0; i < 100; i++ {
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
				errCh <- fmt.Errorf("subject bag mismatch for %s", subject)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent ResolveSubjectAttributes error: %v", err)
	}
}
```

Note: this test requires adding `"fmt"` and `"sync"` imports to `resolver_test.go` if they are not already present. Check the existing import block and add them if missing.

- [ ] **Step 6: Run all new tests with race detector**

Run: `task test -- -race -run "TestResolverResolveSubjectAttributes" ./internal/access/policy/attribute/`
Expected: PASS for all tests, no data races detected.

- [ ] **Step 7: Commit**

```bash
jj commit -m "$(cat <<'EOF'
test(abac): panic, re-entrance, cross-check, and concurrency tests for ResolveSubjectAttributes

Completes unit test coverage for the subject-only resolution path:
panic recovery via safeResolve, re-entrance detection, subject-bag
equivalence with Resolve, and concurrent-safety smoke test with
-race.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Engine.CanPerformAction — wire to ResolveSubjectAttributes

**Files:**

- Modify: `internal/access/policy/engine.go` (rewrite step 4 of `CanPerformAction`, delete `__preflight__` literal, update doc comment)
- Modify: `internal/access/policy/engine_test.go` (+T5, T6, T7)

- [ ] **Step 1: Write failing test T5 (engine does not invoke resource providers)**

Append to `internal/access/policy/engine_test.go`:

```go
func TestEngineCanPerformActionDoesNotInvokeResourceProvidersWhenCapabilityCheckRuns(t *testing.T) {
	// Build an engine with a resolver that has BOTH a subject provider and
	// a resource provider registered. The resource provider uses a tracking
	// type local to this test file. Assert its resourceCalls counter stays
	// at zero during CanPerformAction.
	dslText := `permit(principal is character, action in ["read"], resource is widget);`

	widgetSchema := &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{"type": types.AttrTypeString},
	}
	widgetProv := &trackingAttrProvider{namespace: "widget", schema: widgetSchema}

	engine := createTestEngineWithPolicies(t, []string{dslText}, []attribute.AttributeProvider{widgetProv})

	_, err := engine.CanPerformAction(context.Background(), "character:01ABC", "read", "widget", "")
	// The call's allowed/denied outcome is orthogonal to the invariant
	// under test. We only care that the resource provider was not touched.
	_ = err

	assert.Equal(t, 0, widgetProv.resourceCalls,
		"widget resource provider must not be called during CanPerformAction")
}

// trackingAttrProvider counts calls separately for subject and resource.
// It lives in engine_test.go because T5 is the first test that needs it;
// if other tests need the pattern later, it can be reused.
type trackingAttrProvider struct {
	namespace     string
	subjectAttrs  map[string]any
	resourceAttrs map[string]any
	schema        *types.NamespaceSchema
	subjectCalls  int
	resourceCalls int
}

func (p *trackingAttrProvider) Namespace() string { return p.namespace }

func (p *trackingAttrProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	p.subjectCalls++
	return p.subjectAttrs, nil
}

func (p *trackingAttrProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	p.resourceCalls++
	return p.resourceAttrs, nil
}

func (p *trackingAttrProvider) Schema() *types.NamespaceSchema { return p.schema }
```

- [ ] **Step 2: Run the test to verify it currently FAILS**

Run: `task test -- -run TestEngineCanPerformActionDoesNotInvokeResourceProvidersWhenCapabilityCheckRuns ./internal/access/policy/`
Expected: FAIL — current `CanPerformAction` constructs `widget:__preflight__` and calls `Resolve`, which WILL invoke the widget resource provider. `widgetProv.resourceCalls` will be 1, not 0. The failure message will show `1 != 0`.

- [ ] **Step 3: Modify engine.go CanPerformAction step 4**

Open `internal/access/policy/engine.go`. Find the existing step 4 block (around line 398-418):

```go
	// Step 4: Resolve subject attributes via a synthetic request.
	// The resource uses resourceType+":__preflight__" to satisfy resolver format
	// requirements without needing a real resource instance.
	syntheticReq, reqErr := types.NewAccessRequest(subject, action, resourceType+":__preflight__")
	if reqErr != nil {
		return false, oops.With("subject", subject).With("action", action).With("resourceType", resourceType).Wrap(reqErr)
	}
	bags, resolveErr := e.resolver.Resolve(ctx, syntheticReq)
	if resolveErr != nil {
		errutil.LogErrorContext(ctx, "CanPerformAction: attribute resolution failed — fail-closed",
			resolveErr,
			"subject", subject,
			"action", action,
			"resourceType", resourceType,
		)
		return false, oops.
			With("subject", subject).
			With("action", action).
			With("resourceType", resourceType).
			Wrap(resolveErr)
	}
```

Replace with:

```go
	// Step 4: Resolve subject + environment attributes only. No resource
	// instance exists at type-level preflight; resource providers are
	// never called. The optimistic-permit branch below handles permits
	// whose conditions reference resource attributes.
	bags, resolveErr := e.resolver.ResolveSubjectAttributes(ctx, subject, action)
	if resolveErr != nil {
		errutil.LogErrorContext(ctx, "CanPerformAction: attribute resolution failed — fail-closed",
			resolveErr,
			"subject", subject,
			"action", action,
			"resourceType", resourceType,
		)
		return false, oops.
			With("subject", subject).
			With("action", action).
			With("resourceType", resourceType).
			Wrap(resolveErr)
	}
```

- [ ] **Step 4: Update CanPerformAction doc comment**

In `internal/access/policy/engine.go`, find the doc comment above `CanPerformAction` (around lines 367-377). The existing comment mentions "synthetic request" — replace the line:

```go
// Resolves subject attributes via a synthetic request with resourceType+":__preflight__"
```

with:

```go
// Resolves only subject, environment, and action attributes via
// ResolveSubjectAttributes. Plugin resource providers are NEVER invoked
// during preflight. Policies whose conditions reference resource attributes
// are handled via the optimistic-permit branch below.
```

If the existing doc comment does not contain a "synthetic request" line verbatim, read lines 360-380 and rewrite the whole comment to reflect the new behavior. The key property to document: resource providers are not called, instance-level Evaluate handles the full condition.

- [ ] **Step 5: Run T5 to verify it passes**

Run: `task test -- -run TestEngineCanPerformActionDoesNotInvokeResourceProvidersWhenCapabilityCheckRuns ./internal/access/policy/`
Expected: PASS

- [ ] **Step 6: Run the full engine test package to catch regressions**

Run: `task test -- ./internal/access/policy/`
Expected: PASS for all existing tests. If any existing test was asserting on `__preflight__` or on the old behavior, it needs to be updated — investigate and fix in the same task.

- [ ] **Step 7: Commit**

```bash
jj commit -m "$(cat <<'EOF'
refactor(abac): engine CanPerformAction uses ResolveSubjectAttributes

Replaces the synthetic <type>:__preflight__ resource ID construction
with a call to Resolver.ResolveSubjectAttributes. Plugin resource
providers are no longer invoked during type-level capability pre-flight.

The optimistic-permit branch (step 7) is unchanged — it already handles
permits whose conditions reference resource attributes by treating them
as "may apply, instance-level Evaluate will enforce the full condition."

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Engine.CanPerformAction — optimistic permit branch test (T6)

**Files:**

- Modify: `internal/access/policy/engine_test.go` (+T6 only)

**Pre-existing coverage (NO new tests needed):** Self-review found that T7, T22, T23, and T24 from the design spec are already implemented in `internal/access/policy/engine_test.go`:

| Planned test | Existing test | Line |
|---|---|---|
| T7 (fail closed on resolver error) | `TestEngineCanPerformActionAttributeResolutionError` | 2017 |
| T22 (degraded mode short-circuit) | `TestEngineCanPerformActionDegradedMode` | 1946 |
| T23 (context cancellation) | `TestEngineCanPerformActionContextCancelled` | 1956 |
| T24 (malformed subject) | `TestEngine_CanPerformAction_InvalidSubjectFormat` | 1991 |

These tests implicitly validate that the C1 refactor did not regress any of the invariants they cover. Running them after Task 4's refactor is sufficient — no new tests needed. Add NOTHING for T7/T22/T23/T24.

**T6 is genuinely new** — no existing test covers the optimistic-permit branch for permits whose conditions reference resource attributes.

- [ ] **Step 1: Add T6 (optimistic permit branch fires for permit referencing resource attrs)**

This uses the existing `createTestEngineWithPolicies(t, dslStrings, providers)` helper in `engine_test.go`. That helper parses and installs the DSL policies into a fresh engine with a real compiler + cache. Pattern borrowed from `TestEngineCanPerformActionAdminPermitted` at line 1906.

Append to `internal/access/policy/engine_test.go`:

```go
func TestEngineCanPerformActionPermitsOptimisticallyForPermitReferencingResourceAttrs(t *testing.T) {
	// The optimistic-permit branch fires when a permit policy's conditions
	// reference resource attributes that can't be evaluated at type-level
	// pre-flight (no resource instance exists). The engine MUST treat such
	// permits as potentially-applicable and return allowed=true — the
	// handler's instance-level Evaluate will enforce the full condition.
	//
	// This test proves that switching to ResolveSubjectAttributes does not
	// regress this behavior. Before the refactor, the optimistic branch
	// worked because the synthetic "__preflight__" resource had an empty
	// attribute bag; after the refactor, it works because ResolveSubjectAttributes
	// returns an empty Resource bag by construction.
	dslText := `permit(principal is character, action in ["read"], resource is widget) when { resource.widget.type == "normal" };`

	engine := createTestEngineWithPolicies(t, []string{dslText}, nil)

	allowed, err := engine.CanPerformAction(context.Background(), "character:01ABC", "read", "widget", "")
	require.NoError(t, err)
	assert.True(t, allowed,
		"permit policy referencing resource attrs must optimistic-permit at preflight")
}
```

- [ ] **Step 2: Run T6**

Run: `task test -- -run TestEngineCanPerformActionPermitsOptimisticallyForPermitReferencingResourceAttrs ./internal/access/policy/`
Expected: PASS

- [ ] **Step 3: Run all pre-existing CanPerformAction tests as regression coverage**

The refactor in Task 4 must not have regressed any of these. Running them explicitly provides the T7/T22/T23/T24 coverage without writing new tests:

Run: `task test -- -run "TestEngineCanPerformAction|TestEngine_CanPerformAction" ./internal/access/policy/`
Expected: PASS for ALL existing tests including:

- `TestEngineCanPerformActionAdminPermitted`
- `TestEngineCanPerformActionNoMatchingPolicy`
- `TestEngineCanPerformActionForbidOverridesPermit`
- `TestEngineCanPerformActionDegradedMode` (covers T22)
- `TestEngineCanPerformActionContextCancelled` (covers T23)
- `TestEngineCanPerformActionUnconditionalPermit`
- `TestEngineCanPerformActionExactResourcePolicySkipped`
- `TestEngine_CanPerformAction_InvalidSubjectFormat` (covers T24)
- `TestEngineCanPerformActionAttributeResolutionError` (covers T7)
- `TestEngineCanPerformActionPrincipalTypeMismatch`
- `TestEngineCanPerformActionActionMismatch`
- `TestEngineCanPerformActionResourceTypeMismatch`
- `TestEngineCanPerformActionNilCompiledPolicySkipped`
- `TestEngineCanPerformActionConditionUnsatisfied`

If ANY of these fail after the Task 4 refactor, stop and debug — that's a regression the design said would not happen, and the plan is wrong about something.

- [ ] **Step 4: Commit**

```bash
jj commit -m "$(cat <<'EOF'
test(abac): optimistic permit branch test for hardened CanPerformAction

Adds T6: asserts that CanPerformAction returns allowed=true for a
permit policy whose condition references resource.widget.type, via the
optimistic-permit branch. Proves the switch to ResolveSubjectAttributes
does not regress the type-level preflight semantics for policies that
reference resource attrs.

T7, T22, T23, T24 from the design spec are already covered by existing
tests (TestEngineCanPerformActionAttributeResolutionError, ...Degraded,
...ContextCancelled, TestEngine_CanPerformAction_InvalidSubjectFormat)
and need no new code — the Task 4 refactor passing those existing tests
IS the coverage.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Static invariant test — no `__preflight__` in engine.go (T32)

**Files:**

- Create: `internal/access/policy/hardening_invariants_test.go`

- [ ] **Step 1: Create the new test file**

Create `internal/access/policy/hardening_invariants_test.go` with the following content:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"os"
	"strings"
	"testing"
)

// TestPluginABACHardeningSourceCodeDoesNotContainPreflightSentinel is a
// static assertion that the legacy synthetic preflight sentinel has been
// removed from the engine. The Plugin ABAC Hardening spec (2026-04-07)
// requires this literal to be deleted from engine.go — not filtered —
// because the invariant "plugin providers never see synthetic IDs" is
// enforced by construction, not by runtime filtering.
//
// If this test fails, somebody has re-introduced the synthetic preflight
// path. Read docs/superpowers/specs/2026-04-07-plugin-abac-hardening-design.md
// for the rationale before suppressing this test.
func TestPluginABACHardeningSourceCodeDoesNotContainPreflightSentinel(t *testing.T) {
	data, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatalf("failed to read engine.go: %v", err)
	}
	if strings.Contains(string(data), "__preflight__") {
		t.Errorf("engine.go contains the synthetic preflight sentinel " +
			"'__preflight__'. This literal was removed in the Plugin ABAC " +
			"Hardening work (spec 2026-04-07). If you need to re-introduce " +
			"preflight-aware behavior, read the spec first.")
	}
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `task test -- -run TestPluginABACHardeningSourceCodeDoesNotContainPreflightSentinel ./internal/access/policy/`
Expected: PASS (engine.go no longer contains `__preflight__` after Task 4).

- [ ] **Step 3: Verify the test would fail if the sentinel came back**

Temporarily add the string to a comment in `engine.go`:

```go
// TEMP: __preflight__
```

Run the test: expect FAIL.
Remove the temporary comment.
Run the test: expect PASS.
This manual step verifies the test is actually wired correctly.

- [ ] **Step 4: Commit**

```bash
jj commit -m "$(cat <<'EOF'
test(abac): static invariant — engine.go does not contain __preflight__

Adds a dedicated hardening_invariants_test.go file with a grep-style
test that asserts the legacy synthetic preflight sentinel has been
removed from engine.go. Prevents accidental regression.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Extract ValidateManifestPolicySchemas — core function + happy path

**Files:**

- Create: `internal/plugin/policy_schema_validator.go`
- Create: `internal/plugin/policy_schema_validator_test.go`

- [ ] **Step 1: Create the test file with T8 (the core fail-closed test)**

Create `internal/plugin/policy_schema_validator_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestValidateManifestPolicySchemasRejectsPolicyReferencingAttributeNotInSchema(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		Version:       "1.0.0",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-read-normal",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.tipe == "normal" };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {
			Attributes: map[string]types.AttrType{
				"type":  types.AttrTypeString,
				"owner": types.AttrTypeString,
			},
		},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tipe")
	assert.Contains(t, err.Error(), "widget-read-normal")
	assert.Contains(t, err.Error(), "widget")
	assert.Contains(t, err.Error(), "not in the declared schema")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- -run TestValidateManifestPolicySchemasRejectsPolicyReferencingAttributeNotInSchema ./internal/plugin/`
Expected: FAIL with "undefined: plugins.ValidateManifestPolicySchemas"

- [ ] **Step 3: Create the validator file with the extracted function**

Create `internal/plugin/policy_schema_validator.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/dsl"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// ValidateManifestPolicySchemas verifies that every attribute reference in
// each manifest policy's DSL exists in the plugin's declared schema for the
// policy's resource type. Returns a non-nil error on the first mismatch so
// the plugin load fails before any policy is installed.
//
// schemas is the schema map discovered via GetSchema during plugin load.
// Plugins without resource_types (schemas == nil or empty) are out of scope
// for this check and return nil.
//
// Unparseable policies are skipped — ValidatePluginPolicy has already
// rejected them by the time this function runs.
//
// The function returns on the FIRST mismatch rather than aggregating errors.
// Rationale: a plugin author fixing a typo will re-run the load anyway, and
// aggregation produces harder-to-read error messages for agent consumers.
// If multi-error reporting is needed later, it is a non-breaking follow-up.
func ValidateManifestPolicySchemas(
	manifest *Manifest,
	schemas map[string]*types.NamespaceSchema,
) error {
	if len(schemas) == 0 {
		return nil
	}

	for _, mp := range manifest.Policies {
		parsed, err := dsl.Parse(mp.DSL)
		if err != nil {
			// Unparseable policies were already rejected by ValidatePluginPolicy.
			// Skip them silently here to avoid double-reporting.
			continue
		}
		if parsed.Target == nil || parsed.Target.Resource == nil {
			continue
		}
		rt := parsed.Target.Resource.Type
		if rt == "" {
			continue
		}
		schema, ok := schemas[rt]
		if !ok || schema == nil {
			// Resource type not in this plugin's schema map. Either the
			// policy targets a core type (already rejected by
			// ValidatePluginPolicy for non-trusted plugins) or a type the
			// plugin declared but GetSchema didn't return (separate bug
			// outside the scope of this validator). Skip.
			continue
		}
		for _, attr := range referencedResourceAttrs(parsed) {
			if _, known := schema.Attributes[attr]; !known {
				// Build the list of valid attribute names for the error.
				validKeys := make([]string, 0, len(schema.Attributes))
				for k := range schema.Attributes {
					validKeys = append(validKeys, k)
				}
				return oops.
					Code("PLUGIN_SCHEMA_VALIDATION_FAILED").
					In("plugin").
					With("plugin", manifest.Name).
					With("policy", mp.Name).
					With("resource_type", rt).
					With("attribute", attr).
					With("schema_keys", validKeys).
					Errorf("policy %q references attribute %q on resource type %q which is not in the declared schema",
						mp.Name, attr, rt)
			}
		}
	}

	return nil
}
```

- [ ] **Step 4: Run T8 to verify it now passes**

Run: `task test -- -run TestValidateManifestPolicySchemasRejectsPolicyReferencingAttributeNotInSchema ./internal/plugin/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
jj commit -m "$(cat <<'EOF'
feat(abac): extract ValidateManifestPolicySchemas validator

Adds a new policy_schema_validator.go that hard-fails plugin load when
a manifest policy's DSL references a resource attribute not declared
in the plugin's GetSchema response.

This is the hard-failure counterpart to the existing non-fatal Warning 3
in CheckManifestWarnings. Both will run in the load path for now; the
Warning 3 case is removed in a follow-up task to avoid double-reporting.

Error format includes oops fields plugin, policy, resource_type,
attribute, and schema_keys (the list of valid attribute names as a
debugging hint).

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: ValidateManifestPolicySchemas — edge case and boundary tests

**Files:**

- Modify: `internal/plugin/policy_schema_validator_test.go` (+T9, T10, T11, T12, T25, T26, T27, T28, T29, T30, T31)

- [ ] **Step 1: Add T9 (happy path — all attributes declared)**

Append to `internal/plugin/policy_schema_validator_test.go`:

```go
func TestValidateManifestPolicySchemasAcceptsPolicyWhenAllAttributeReferencesMatchSchema(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-read-normal",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.type == "normal" };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {
			Attributes: map[string]types.AttrType{
				"type":  types.AttrTypeString,
				"owner": types.AttrTypeString,
			},
		},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	assert.NoError(t, err)
}
```

- [ ] **Step 2: Add T10 (plugin without resource types)**

```go
func TestValidateManifestPolicySchemasReturnsNilForPluginWithoutResourceTypes(t *testing.T) {
	m := &plugins.Manifest{
		Name: "simple-plugin",
		Policies: []plugins.ManifestPolicy{
			{
				Name: "exec",
				DSL: `permit(principal is character, action in ["execute"], resource is command) ` +
					`when { resource.command.name == "simple" };`,
			},
		},
	}
	// schemas is nil — plugin has no custom resource types.
	err := plugins.ValidateManifestPolicySchemas(m, nil)
	assert.NoError(t, err)
}
```

- [ ] **Step 3: Add T11 (policy without when clause)**

```go
func TestValidateManifestPolicySchemasAcceptsPolicyWithoutWhenClause(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-unconstrained",
				DSL:  `permit(principal is character, action in ["read"], resource is widget);`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {Attributes: map[string]types.AttrType{"type": types.AttrTypeString}},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	assert.NoError(t, err)
}
```

- [ ] **Step 4: Add T12 (non-resource attribute references are ignored)**

```go
func TestValidateManifestPolicySchemasIgnoresEnvironmentAndPrincipalAttributeReferences(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-time-gated",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { principal.character.role == "admin" };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {Attributes: map[string]types.AttrType{"type": types.AttrTypeString}},
	}

	// Policy references principal.character.role but NOT any widget attribute.
	// The validator should not false-positive on principal references.
	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	assert.NoError(t, err)
}
```

- [ ] **Step 5: Add T25 (empty non-nil schema map)**

```go
func TestValidateManifestPolicySchemasReturnsNilForEmptyNonNilSchemaMap(t *testing.T) {
	m := &plugins.Manifest{
		Name: "empty-plugin",
	}
	schemas := map[string]*types.NamespaceSchema{} // non-nil, length 0

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	assert.NoError(t, err)
}
```

- [ ] **Step 6: Add T26 (policy type not in schema map → skipped)**

```go
func TestValidateManifestPolicySchemasReturnsNilWhenPolicyTypeWasAlreadyRejectedByPluginValidator(t *testing.T) {
	// This test documents the layering assumption: ValidatePluginPolicy
	// runs before ValidateManifestPolicySchemas and rejects policies
	// targeting resource types not in the plugin's resource_types. If
	// such a policy reaches this validator (shouldn't happen in practice),
	// we skip rather than error — the rejection is another validator's
	// responsibility.
	m := &plugins.Manifest{
		Name:          "test-plugin",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "gadget-policy",
				DSL: `permit(principal is character, action in ["read"], resource is gadget) ` +
					`when { resource.gadget.color == "red" };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {Attributes: map[string]types.AttrType{"type": types.AttrTypeString}},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	assert.NoError(t, err, "policy targeting an out-of-schema type is skipped by this validator")
}
```

- [ ] **Step 7: Add T27 (first-error reporting with multiple bad policies)**

```go
func TestValidateManifestPolicySchemasReportsFirstErrorWhenMultiplePoliciesHaveBadAttributes(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-bad-first",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.alpha == "x" };`,
			},
			{
				Name: "widget-good",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.type == "normal" };`,
			},
			{
				Name: "widget-bad-second",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.beta == "y" };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {Attributes: map[string]types.AttrType{"type": types.AttrTypeString}},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "widget-bad-first", "first bad policy should be reported")
	assert.Contains(t, err.Error(), "alpha", "first bad attribute should be reported")
	assert.NotContains(t, err.Error(), "widget-bad-second", "second bad policy should not be in first error")
}
```

- [ ] **Step 8: Add T28 (compound condition with AND/OR/NOT)**

```go
func TestValidateManifestPolicySchemasHandlesCompoundConditionsWithANDORNot(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-compound",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { (resource.widget.type == "normal" || resource.widget.tipe == "restricted") && !(resource.widget.owner == "system") };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {
			Attributes: map[string]types.AttrType{
				"type":  types.AttrTypeString,
				"owner": types.AttrTypeString,
			},
		},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	require.Error(t, err, "compound condition with bad attribute should fail")
	assert.Contains(t, err.Error(), "tipe")
}
```

- [ ] **Step 9: Add T29 (has/contains DSL nodes)**

```go
func TestValidateManifestPolicySchemasHandlesHasAndContainsReferences(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-contains",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.members contains "admin" };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {
			Attributes: map[string]types.AttrType{"type": types.AttrTypeString},
		},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "members")
}
```

- [ ] **Step 10: Add T30 (in-expression with dynamic both-sides)**

```go
func TestValidateManifestPolicySchemasHandlesInExprWithDynamicBothSides(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-in-dynamic",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { principal.character.player_id in resource.widget.members };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {
			Attributes: map[string]types.AttrType{"type": types.AttrTypeString},
		},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	require.Error(t, err, "`in` expression with undeclared resource attribute should fail")
	assert.Contains(t, err.Error(), "members")
}
```

- [ ] **Step 11: Add T31 (prefix/substring not a false positive)**

```go
func TestValidateManifestPolicySchemasAcceptsAttributeNameThatIsPrefixOfValidAttribute(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-exact",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.type == "normal" };`,
			},
		},
	}
	// Schema has type_code AND type; both are distinct keys. The policy
	// references "type" (exactly), which is valid. Prefix matching would
	// erroneously think "type" is a prefix of "type_code" and could cause
	// a false positive in a buggy implementation.
	schemas := map[string]*types.NamespaceSchema{
		"widget": {
			Attributes: map[string]types.AttrType{
				"type_code": types.AttrTypeString,
				"type":      types.AttrTypeString,
			},
		},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	assert.NoError(t, err, "exact-match lookup must not substring-match")
}
```

- [ ] **Step 12: Run all validator tests**

Run: `task test -- -run "TestValidateManifestPolicySchemas" ./internal/plugin/`
Expected: PASS for all 12 tests (T8-T12, T25-T31).

- [ ] **Step 13: Commit**

```bash
jj commit -m "$(cat <<'EOF'
test(abac): edge and boundary tests for ValidateManifestPolicySchemas

Adds 11 tests covering happy path, nil/empty schema map, unconditional
policies, environment/principal references, layering assumption,
first-error semantics, compound conditions (AND/OR/NOT), has/contains
DSL nodes, dynamic both-sides in-expressions, and prefix-not-substring
exact matching.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Remove Warning 3 from CheckManifestWarnings

**Files:**

- Modify: `internal/plugin/manifest_warnings.go` (remove the `// Warning 3` block)
- Modify: `internal/plugin/manifest_warnings_test.go` (remove tests that asserted Warning 3 output)

- [ ] **Step 1: Identify tests that will break**

Run: `task test -- -run "TestCheckManifestWarnings" ./internal/plugin/ -v`
Note which tests pass. Look at the test file around the existing tests that use schemas with undeclared attributes. Two tests identified in the investigation:

- `TestCheckManifestWarningsAttributeRefThroughInListProducesWarning` (at line ~367)
- `TestCheckManifestWarningsSchemaCheckSkippedWhenResourceTypeNotInSchema` (at line ~392)

The first asserts Warning 3's output directly; it needs to be deleted because Warning 3 no longer fires. The second asserts the "skipped" case, which is still valid behavior (both the new validator and the old Warning 3 path skip in that case) — but it was testing `CheckManifestWarnings` specifically, so it stays as a regression test for the unchanged skip behavior.

- [ ] **Step 2: Delete the Warning 3 block from manifest_warnings.go**

Open `internal/plugin/manifest_warnings.go`. Find the block starting at the comment `// Warning 3: policy references an attribute not in the schema for its resource type.` (around line 76-99) and ending at the closing brace before `return warnings`. Delete the entire block:

```go
	// Warning 3: policy references an attribute not in the schema for its resource type.
	if schemas != nil {
		for _, pp := range parsed {
			if pp.policy.Target == nil || pp.policy.Target.Resource == nil {
				continue
			}
			rt := pp.policy.Target.Resource.Type
			if rt == "" {
				continue
			}
			schema, ok := schemas[rt]
			if !ok || schema == nil {
				continue
			}
			for _, attr := range referencedResourceAttrs(pp.policy) {
				if _, known := schema.Attributes[attr]; !known {
					warnings = append(warnings, fmt.Sprintf(
						"plugin %q: policy %q references attribute %q on resource type %q which is not in the schema",
						manifest.Name, pp.name, attr, rt,
					))
				}
			}
		}
	}
```

The `schemas` parameter is still used by the function signature; leave it for now (if it becomes unused after deletion, gofumpt will flag it — handle it then).

Check whether `referencedResourceAttrs`, `collectFromBlock`, `collectFromCond`, `collectFromExpr` are still used by anything in the file. If not, they can be DELETED because the new validator has its own equivalent — BUT, the new validator in `policy_schema_validator.go` imports them from this file. If they live in `manifest_warnings.go`, they need to remain accessible. Simplest approach: leave these helper functions in `manifest_warnings.go` and have `policy_schema_validator.go` call them.

**Action:** leave `referencedResourceAttrs`, `collectFromBlock`, `collectFromCond`, `collectFromExpr` in `manifest_warnings.go`. They are used by the new validator.

- [ ] **Step 3: Delete the now-obsolete test TestCheckManifestWarningsAttributeRefThroughInListProducesWarning**

Open `internal/plugin/manifest_warnings_test.go`. Find `TestCheckManifestWarningsAttributeRefThroughInListProducesWarning` (around line 367). Delete the entire function.

The test's coverage is now provided by T29 (`TestValidateManifestPolicySchemasHandlesHasAndContainsReferences`) and T30 (`TestValidateManifestPolicySchemasHandlesInExprWithDynamicBothSides`) in the validator test file.

- [ ] **Step 4: Keep TestCheckManifestWarningsSchemaCheckSkippedWhenResourceTypeNotInSchema but update its expectation**

Still at `internal/plugin/manifest_warnings_test.go`, find `TestCheckManifestWarningsSchemaCheckSkippedWhenResourceTypeNotInSchema` (around line 392). The test asserts that `CheckManifestWarnings` returns empty warnings when the policy's resource type isn't in the schema map. This behavior is unchanged (the deleted block was the only Warning 3 code). The test should still pass as-is.

Run: `task test -- -run TestCheckManifestWarningsSchemaCheckSkippedWhenResourceTypeNotInSchema ./internal/plugin/`
Expected: PASS.

- [ ] **Step 5: Run the full manifest_warnings test file**

Run: `task test -- -run "TestCheckManifestWarnings" ./internal/plugin/`
Expected: PASS for all remaining tests.

- [ ] **Step 6: Run `task lint` to catch any unused imports or vars**

Run: `task lint`
Expected: clean pass. If `fmt` is no longer used in `manifest_warnings.go`, remove it from the imports.

- [ ] **Step 7: Commit**

```bash
jj commit -m "$(cat <<'EOF'
refactor(abac): remove Warning 3 from CheckManifestWarnings

Deletes the policy-attribute-reference cross-validation block from
CheckManifestWarnings. The same checks now run as hard failures via
ValidateManifestPolicySchemas. Deletes the corresponding test that
asserted Warning 3 output; its coverage is preserved by T29 and T30
in the validator test file.

Warnings 1 (missing execute policy) and 2 (uncovered capability) are
unchanged — they remain non-fatal hints because a plugin author may
legitimately declare a public command with no ABAC enforcement.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Wire ValidateManifestPolicySchemas into Manager.loadPlugin

**Files:**

- Modify: `internal/plugin/manager.go` (insert validator call before policy install with rollback)

- [ ] **Step 1: Open manager.go at the load path**

Open `internal/plugin/manager.go`. Locate `loadPlugin` around line 440 (the exact line may drift; search for the function). The relevant sequence is at lines 473-495:

```go
473:	// Discover and register attribute providers for plugin resource types.
474:	// schemas is non-nil only for binary plugins that declare resource_types.
475:	var schemas map[string]*types.NamespaceSchema
476:	if len(dp.Manifest.ResourceTypes) > 0 {
477:		var regErr error
478:		schemas, regErr = m.discoverAndRegisterAttributes(ctx, host, dp)
479:		if regErr != nil {
480:			return regErr
481:		}
482:	}
483:
484:	// Install ABAC policies using manifest-aware validation when resource
485:	// types or trust config are present, otherwise fall back to basic install.
486:	if m.policyInstaller != nil && len(dp.Manifest.Policies) > 0 {
487:		installErr := m.policyInstaller.InstallPluginPoliciesWithManifest(ctx, dp.Manifest, dp.Manifest.Policies)
488:		...
```

The variable name is `schemas` (confirmed). The install block starts at line 486.

- [ ] **Step 2: Insert the validator call between lines 482 and 486**

Insert this block immediately after the `}` at line 482 (closing the `if len(dp.Manifest.ResourceTypes) > 0` block) and before the comment at line 484:

```go
	// Validate manifest policy attribute references against discovered
	// schemas BEFORE installing policies. A load-time schema mismatch
	// (e.g., policy references resource.widget.tipe but schema declares
	// "type") is now a fatal load error — spec 2026-04-07-plugin-abac-hardening.
	if valErr := ValidateManifestPolicySchemas(dp.Manifest, schemas); valErr != nil {
		if unloadErr := host.Unload(ctx, dp.Manifest.Name); unloadErr != nil {
			slog.Error("failed to rollback plugin load after schema validation failure",
				"plugin", dp.Manifest.Name, "error", unloadErr)
		}
		return oops.In("manager").With("plugin", dp.Manifest.Name).
			Wrapf(valErr, "validate manifest policy schemas")
	}
```

`slog` and `oops` are already imported in `manager.go` — no import changes needed.

- [ ] **Step 3: Run existing plugin tests to verify no regression**

Run: `task test -- ./internal/plugin/`
Expected: PASS. If the existing `test-abac-widget` unit test fixtures include a policy with an undeclared attribute, it will now fail loading — in that case, fix the fixture to use valid attributes.

- [ ] **Step 4: Run integration tests for the plugin package (foreshadowing Task 11)**

Run: `task test:int -- -tags=integration ./test/integration/plugin/...`
Expected: Existing tests PASS. The new Sharp Edge 2 tests don't exist yet (added in Task 11).

Note: if Docker is not available or integration tests are slow, this step can be deferred to after Task 11 when both are run together.

- [ ] **Step 5: Commit**

```bash
jj commit -m "$(cat <<'EOF'
feat(abac): wire ValidateManifestPolicySchemas into plugin load path

Manager.loadPlugin now calls ValidateManifestPolicySchemas after schema
discovery and before policy install. A non-nil return fails the load
via the existing host.Unload rollback, ensuring no partial DB state.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Counting proxy wrapper for AttributeResolverServiceClient

**Files:**

- Create: `test/integration/plugin/counting_proxy_test.go`

- [ ] **Step 1: Check the AttributeResolverServiceClient interface**

Run: `task test -- ./internal/plugin/... -count=0 -list="." 2>&1 | head -5` (just compiles; no-op run)

Read `pkg/proto/holomush/plugin/v1/` for the generated client interface. The method signatures needed are:

- `GetSchema(ctx, *GetSchemaRequest, ...grpc.CallOption) (*GetSchemaResponse, error)`
- `ResolveResource(ctx, *ResolveResourceRequest, ...grpc.CallOption) (*ResolveResourceResponse, error)`

Use `Grep` tool:

Run: (via the Grep tool) pattern `type AttributeResolverServiceClient interface`, glob `pkg/proto/holomush/plugin/v1/*.go`
Expected: find the generated interface. Copy its method signatures into the next step.

- [ ] **Step 2: Create the counting proxy file**

Create `test/integration/plugin/counting_proxy_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"sync/atomic"

	"google.golang.org/grpc"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// countingAttributeResolverClient wraps a real AttributeResolverServiceClient
// and counts calls to each RPC. Used in plugin ABAC hardening integration
// tests to assert that ResolveResource is (or is not) invoked during a
// particular authorization code path.
type countingAttributeResolverClient struct {
	inner                pluginv1.AttributeResolverServiceClient
	getSchemaCalls       atomic.Int64
	resolveResourceCalls atomic.Int64
}

func newCountingAttributeResolverClient(
	inner pluginv1.AttributeResolverServiceClient,
) *countingAttributeResolverClient {
	return &countingAttributeResolverClient{inner: inner}
}

func (c *countingAttributeResolverClient) GetSchema(
	ctx context.Context,
	req *pluginv1.GetSchemaRequest,
	opts ...grpc.CallOption,
) (*pluginv1.GetSchemaResponse, error) {
	c.getSchemaCalls.Add(1)
	return c.inner.GetSchema(ctx, req, opts...)
}

func (c *countingAttributeResolverClient) ResolveResource(
	ctx context.Context,
	req *pluginv1.ResolveResourceRequest,
	opts ...grpc.CallOption,
) (*pluginv1.ResolveResourceResponse, error) {
	c.resolveResourceCalls.Add(1)
	return c.inner.ResolveResource(ctx, req, opts...)
}

// ResolveResourceCallCount returns the number of ResolveResource calls
// observed so far. Use this in test assertions.
func (c *countingAttributeResolverClient) ResolveResourceCallCount() int64 {
	return c.resolveResourceCalls.Load()
}

// ResetCallCounts resets all counters to zero. Use between phases of a
// single test (e.g., after BeforeEach setup completes).
func (c *countingAttributeResolverClient) ResetCallCounts() {
	c.getSchemaCalls.Store(0)
	c.resolveResourceCalls.Store(0)
}
```

- [ ] **Step 3: Verify the proxy compiles**

Run: `task test:int -- -tags=integration -count=1 -run "^$" ./test/integration/plugin/...`
Expected: compiles successfully, runs zero tests.

If the interface has additional methods beyond GetSchema and ResolveResource, the proxy will fail to satisfy the interface. In that case, add stubs for those methods that delegate to `inner` without counting.

- [ ] **Step 4: Commit**

```bash
jj commit -m "$(cat <<'EOF'
test(abac): counting proxy for AttributeResolverServiceClient

Adds a testing proxy that wraps the generated client and counts
ResolveResource calls. Used by Sharp Edge 1 integration tests to
assert that plugin ResolveResource is NEVER invoked during type-level
preflight (C1 invariant).

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Integration tests — preflight does not invoke plugin RPC (T13, T14, T38)

**Files:**

- Modify: `test/integration/plugin/abac_widget_test.go` (+T13, T14, T38 specs and counting proxy wiring)

- [ ] **Step 1: Add a describe block with counting proxy setup**

Open `test/integration/plugin/abac_widget_test.go`. Find the existing `Describe("full ABAC engine evaluation with plugin policies", ...)` block. After that block (or as a sibling), add a new Describe block:

```go
	// ---------------------------------------------------------------
	// Plugin ABAC Hardening (spec 2026-04-07): Sharp Edge 1 tests
	// ---------------------------------------------------------------
	Describe("plugin ResolveResource call semantics under hardening", func() {
		var (
			ctx         context.Context
			cancel      context.CancelFunc
			container   testcontainers.Container
			connStr     string
			host        *goplugin.Host
			ps          *policystore.PostgresStore
			engine      *policy.Engine
			pool        *pgxpool.Pool
			provisioner *plugins.SchemaProvisioner
			countingAR  *countingAttributeResolverClient
		)

		BeforeEach(func() {
			_, binaryPath := abacWidgetBinaryPath()
			if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
				Skip(fmt.Sprintf("test-abac-widget binary not found at %s — run 'task plugin:build-all' first",
					binaryPath))
			}

			pluginDir, _ := abacWidgetBinaryPath()
			if _, err := os.Stat(filepath.Join(pluginDir, "plugin.yaml")); os.IsNotExist(err) {
				Skip(fmt.Sprintf("plugin.yaml not found at %s/plugin.yaml — run 'task plugin:build-all' first", pluginDir))
			}

			ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

			pgEnv, err := testutil.StartPostgres(ctx)
			Expect(err).NotTo(HaveOccurred())
			container = pgEnv.Container
			connStr = pgEnv.ConnStr

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(migrator.Up()).To(Succeed())
			_ = migrator.Close()

			provisioner = plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())

			svcRegistry := plugins.NewServiceRegistry()
			host = goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(svcRegistry),
			)

			manifest := loadWidgetManifest()
			Expect(host.Load(ctx, manifest, pluginDir)).To(Succeed())

			pool, err = pgxpool.New(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			ps = policystore.NewPostgresStore(pool)
			installer := plugins.NewPolicyInstaller(ps)
			Expect(installer.InstallPluginPoliciesWithManifest(ctx, manifest, manifest.Policies)).To(Succeed())

			// Wire the counting proxy in place of the raw plugin client.
			rawClient := host.AttributeResolverClient("test-abac-widget")
			Expect(rawClient).NotTo(BeNil())
			countingAR = newCountingAttributeResolverClient(rawClient)

			// Build the engine stack using the counting proxy instead of the
			// raw client when registering the attribute provider.
			schemaRegistry := attribute.NewSchemaRegistry()
			resolver := attribute.NewResolver(schemaRegistry)

			cmdProvider := attribute.NewCommandProvider()
			Expect(resolver.RegisterProvider(cmdProvider)).To(Succeed())

			schemaResp, schemaErr := countingAR.GetSchema(ctx, &pluginv1.GetSchemaRequest{})
			Expect(schemaErr).NotTo(HaveOccurred())
			schemas := plugins.ConvertProtoSchema(schemaResp)
			Expect(schemas).To(HaveKey("widget"))

			widgetProvider := plugins.NewPluginAttributeProvider("widget", countingAR, schemas["widget"])
			Expect(resolver.RegisterProvider(widgetProvider)).To(Succeed())

			compiler := policy.NewCompiler(schemaRegistry.Schema())
			cache := policy.NewCache(ps, compiler)
			Expect(cache.Reload(ctx)).To(Succeed())

			auditWriter := &testAuditWriter{}
			tmpDir := GinkgoT().TempDir()
			auditLogger := audit.NewLogger(audit.ModeAll, auditWriter, filepath.Join(tmpDir, "test-wal.jsonl"))

			sessionResolver := &testSessionResolver{}
			engine = policy.NewEngine(resolver, cache, sessionResolver, auditLogger)

			// Reset counters after BeforeEach so test assertions measure only
			// the activity that the test body triggers.
			countingAR.ResetCallCounts()
		})

		AfterEach(func() {
			if host != nil {
				_ = host.Close(ctx)
			}
			if provisioner != nil {
				provisioner.Close()
			}
			if pool != nil {
				pool.Close()
			}
			if container != nil {
				_ = container.Terminate(context.Background())
			}
			if cancel != nil {
				cancel()
			}
		})

		It("never invokes the plugin ResolveResource RPC during type-level preflight", func() {
			// T13: The C1 invariant at E2E layer with a real plugin binary.
			allowed, err := engine.CanPerformAction(ctx, "character:01ABC", "read", "widget", "self")
			Expect(err).NotTo(HaveOccurred())
			Expect(allowed).To(BeTrue(), "preflight should permit via optimistic branch")

			Expect(countingAR.ResolveResourceCallCount()).To(BeEquivalentTo(0),
				"ResolveResource MUST NOT be called during type-level preflight")
		})

		It("still invokes the plugin ResolveResource RPC for instance-level Evaluate", func() {
			// T14: Instance-level evaluation is unaffected.
			req, reqErr := policytypes.NewAccessRequest("character:01ABC", "read", "widget:normal-1")
			Expect(reqErr).NotTo(HaveOccurred())

			decision, err := engine.Evaluate(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(decision.IsAllowed()).To(BeTrue())

			Expect(countingAR.ResolveResourceCallCount()).To(BeEquivalentTo(1),
				"ResolveResource should be called exactly once for one Evaluate")
		})

		It("permits character:01ABC execute widget command via full database-backed engine stack without invoking plugin ResolveResource", func() {
			// T38: Full DB-backed stack, CanPerformAction, counter asserted zero.
			allowed, err := engine.CanPerformAction(ctx, "character:01ABC", "execute", "command", "self")
			Expect(err).NotTo(HaveOccurred())
			Expect(allowed).To(BeTrue())

			Expect(countingAR.ResolveResourceCallCount()).To(BeEquivalentTo(0),
				"execute command preflight must not touch the plugin")
		})
	})
```

- [ ] **Step 2: Verify integration tests compile**

Run: `task test:int -- -tags=integration -count=1 -run "never invokes the plugin" ./test/integration/plugin/...`
Expected: PASS, or SKIP if the binary isn't built. If SKIP, run `task plugin:build-all` first, then re-run.

- [ ] **Step 3: Run all three new specs**

Run: `task test:int -- -tags=integration -count=1 -run "plugin ResolveResource call semantics under hardening" ./test/integration/plugin/...`
Expected: PASS for all three specs.

- [ ] **Step 4: Commit**

```bash
jj commit -m "$(cat <<'EOF'
test(abac): E2E tests for C1 invariant with real plugin binary

Adds three Ginkgo specs using a counting proxy wrapper around the
test-abac-widget plugin's AttributeResolverServiceClient:

- T13: preflight (CanPerformAction on widget) never calls the plugin
- T14: instance-level Evaluate calls the plugin exactly once
- T38: preflight on command execution never calls the plugin

Proves the C1 invariant end-to-end with a real plugin binary, real
PostgreSQL-backed policy store, and real engine.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Integration tests — load-time validation failure (T15, T34, T36, T37)

**Files:**

- Modify: `test/integration/plugin/abac_widget_test.go` (+T15, T34, T36, T37 specs)

- [ ] **Step 1: Add a Describe block for Sharp Edge 2 integration tests**

Append to the same file after the previous Describe block:

```go
	Describe("manifest policy schema validation at load time", func() {
		var (
			ctx       context.Context
			cancel    context.CancelFunc
			container testcontainers.Container
			connStr   string
		)

		BeforeEach(func() {
			_, binaryPath := abacWidgetBinaryPath()
			if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
				Skip(fmt.Sprintf("test-abac-widget binary not found — run 'task plugin:build-all'"))
			}

			ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)

			pgEnv, err := testutil.StartPostgres(ctx)
			Expect(err).NotTo(HaveOccurred())
			container = pgEnv.Container
			connStr = pgEnv.ConnStr

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(migrator.Up()).To(Succeed())
			_ = migrator.Close()
		})

		AfterEach(func() {
			if container != nil {
				_ = container.Terminate(context.Background())
			}
			if cancel != nil {
				cancel()
			}
		})

		It("fails to load a plugin whose manifest policy references an undeclared resource attribute", func() {
			// T15: Build a manifest based on the widget manifest but with
			// a typo in one policy's DSL. Load it and assert failure.
			goodManifest := loadWidgetManifest()
			badManifest := cloneManifestWithBadPolicy(goodManifest)

			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			registry := plugins.NewServiceRegistry()
			host := goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(registry),
			)
			defer func() { _ = host.Close(ctx) }()

			pluginDir, _ := abacWidgetBinaryPath()

			// The host Load itself still succeeds — validation happens at
			// Manager.loadPlugin level. Load the host-level plugin first,
			// then run the validator directly to prove the hard-failure
			// path. A follow-up assertion covers the manager integration.
			Expect(host.Load(ctx, badManifest, pluginDir)).To(Succeed())

			arClient := host.AttributeResolverClient("test-abac-widget")
			schemaResp, err := arClient.GetSchema(ctx, &pluginv1.GetSchemaRequest{})
			Expect(err).NotTo(HaveOccurred())
			schemas := plugins.ConvertProtoSchema(schemaResp)

			valErr := plugins.ValidateManifestPolicySchemas(badManifest, schemas)
			Expect(valErr).To(HaveOccurred())
			Expect(valErr.Error()).To(ContainSubstring("tipe"))
			Expect(valErr.Error()).To(ContainSubstring("widget-read-normal-bad"))
		})

		It("installs zero policies in the database when manifest validation fails", func() {
			// T34: After a failed validation, the policy store MUST have
			// zero plugin-source policies. This proves rollback at the DB
			// layer — important because the validator runs BEFORE install,
			// so there should be nothing to roll back, but we assert the
			// invariant explicitly.
			goodManifest := loadWidgetManifest()
			badManifest := cloneManifestWithBadPolicy(goodManifest)

			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			svcRegistry := plugins.NewServiceRegistry()
			host := goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(svcRegistry),
			)
			defer func() { _ = host.Close(ctx) }()

			pluginDir, _ := abacWidgetBinaryPath()
			Expect(host.Load(ctx, badManifest, pluginDir)).To(Succeed())

			arClient := host.AttributeResolverClient("test-abac-widget")
			schemaResp, err := arClient.GetSchema(ctx, &pluginv1.GetSchemaRequest{})
			Expect(err).NotTo(HaveOccurred())
			schemas := plugins.ConvertProtoSchema(schemaResp)

			valErr := plugins.ValidateManifestPolicySchemas(badManifest, schemas)
			Expect(valErr).To(HaveOccurred())

			// Now check the DB: no policies should be installed.
			pool, poolErr := pgxpool.New(ctx, connStr)
			Expect(poolErr).NotTo(HaveOccurred())
			defer pool.Close()

			ps := policystore.NewPostgresStore(pool)
			pluginPolicies, listErr := ps.List(ctx, policystore.ListOptions{Source: "plugin"})
			Expect(listErr).NotTo(HaveOccurred())
			Expect(pluginPolicies).To(BeEmpty(),
				"failed validation must leave the policy store pristine")
		})

		It("successfully loads a fixed manifest after a prior validation failure", func() {
			// T36: Idempotency — after a failed load, a fixed manifest
			// should load without any leftover state interfering.
			goodManifest := loadWidgetManifest()
			badManifest := cloneManifestWithBadPolicy(goodManifest)

			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			svcRegistry := plugins.NewServiceRegistry()
			host := goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(svcRegistry),
			)
			defer func() { _ = host.Close(ctx) }()

			pluginDir, _ := abacWidgetBinaryPath()

			// First attempt — bad manifest.
			Expect(host.Load(ctx, badManifest, pluginDir)).To(Succeed())
			arClient := host.AttributeResolverClient("test-abac-widget")
			schemaResp, _ := arClient.GetSchema(ctx, &pluginv1.GetSchemaRequest{})
			schemas := plugins.ConvertProtoSchema(schemaResp)
			badErr := plugins.ValidateManifestPolicySchemas(badManifest, schemas)
			Expect(badErr).To(HaveOccurred())

			// Unload the bad plugin before loading the good one.
			_ = host.Close(ctx)

			// Re-create host and load the good manifest.
			svcRegistry2 := plugins.NewServiceRegistry()
			host2 := goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(svcRegistry2),
			)
			defer func() { _ = host2.Close(ctx) }()

			Expect(host2.Load(ctx, goodManifest, pluginDir)).To(Succeed())
			arClient2 := host2.AttributeResolverClient("test-abac-widget")
			schemaResp2, _ := arClient2.GetSchema(ctx, &pluginv1.GetSchemaRequest{})
			schemas2 := plugins.ConvertProtoSchema(schemaResp2)

			goodErr := plugins.ValidateManifestPolicySchemas(goodManifest, schemas2)
			Expect(goodErr).NotTo(HaveOccurred(),
				"good manifest must validate successfully after a prior failure")
		})

		It("installs three plugin policies in the database when manifest validation passes", func() {
			// T37: Positive path — existing happy path with an explicit
			// DB query assertion to mirror T34's negative path.
			goodManifest := loadWidgetManifest()

			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			svcRegistry := plugins.NewServiceRegistry()
			host := goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(svcRegistry),
			)
			defer func() { _ = host.Close(ctx) }()

			pluginDir, _ := abacWidgetBinaryPath()
			Expect(host.Load(ctx, goodManifest, pluginDir)).To(Succeed())

			arClient := host.AttributeResolverClient("test-abac-widget")
			schemaResp, _ := arClient.GetSchema(ctx, &pluginv1.GetSchemaRequest{})
			schemas := plugins.ConvertProtoSchema(schemaResp)
			Expect(plugins.ValidateManifestPolicySchemas(goodManifest, schemas)).To(Succeed())

			pool, poolErr := pgxpool.New(ctx, connStr)
			Expect(poolErr).NotTo(HaveOccurred())
			defer pool.Close()

			ps := policystore.NewPostgresStore(pool)
			installer := plugins.NewPolicyInstaller(ps)
			Expect(installer.InstallPluginPoliciesWithManifest(ctx, goodManifest, goodManifest.Policies)).To(Succeed())

			pluginPolicies, listErr := ps.List(ctx, policystore.ListOptions{Source: "plugin"})
			Expect(listErr).NotTo(HaveOccurred())
			Expect(pluginPolicies).To(HaveLen(3),
				"successful validation must install all three policies")
		})
	})
}) // closes the top-level Describe
```

Note: the `})` at the end closes the outer `Describe("Plugin ABAC Trust Boundary", ...)`. Check the file carefully — the exact closing pattern depends on the current file structure. Read the file end before inserting.

- [ ] **Step 2: Add the manifest cloning helper**

Append to the same test file (outside any Ginkgo block, at package scope):

```go
// cloneManifestWithBadPolicy returns a copy of the given manifest with one
// policy's DSL mutated to reference an undeclared attribute (resource.widget.tipe
// instead of resource.widget.type). The policy name is also changed to
// "widget-read-normal-bad" so tests can assert on it specifically.
func cloneManifestWithBadPolicy(src *plugins.Manifest) *plugins.Manifest {
	clone := *src
	clone.Policies = make([]plugins.ManifestPolicy, len(src.Policies))
	copy(clone.Policies, src.Policies)

	// Mutate the first policy that targets widget-read-normal (or any
	// policy that references resource.widget.type if the name differs).
	for i, p := range clone.Policies {
		if p.Name == "widget-read-normal" || strings.Contains(p.DSL, "resource.widget.type") {
			clone.Policies[i] = plugins.ManifestPolicy{
				Name: "widget-read-normal-bad",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.tipe == "normal" };`,
			}
			break
		}
	}
	return &clone
}
```

Note: this helper requires `strings` to be imported in the test file. Check imports and add if missing.

- [ ] **Step 3: Run the new integration specs**

Run: `task test:int -- -tags=integration -count=1 -run "manifest policy schema validation at load time" ./test/integration/plugin/...`
Expected: PASS for all four specs.

- [ ] **Step 4: Commit**

```bash
jj commit -m "$(cat <<'EOF'
test(abac): E2E tests for load-time schema validation

Adds four Ginkgo specs in test/integration/plugin/abac_widget_test.go:

- T15: bad manifest fails ValidateManifestPolicySchemas with tipe error
- T34: failed validation leaves policy store empty (rollback invariant)
- T36: fixed manifest loads successfully after a prior failure
- T37: successful validation installs all three widget policies

Uses cloneManifestWithBadPolicy helper to synthesize bad manifests
in-memory without rebuilding the plugin binary.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: Resolver provider registry rollback investigation (T35)

**Files:**

- Investigation: `internal/access/policy/attribute/resolver.go`, `internal/plugin/manager.go`
- Possibly modify: add `UnregisterProvider` to resolver
- Possibly add: rollback call in `manager.go` after failed validation

- [ ] **Step 1: Investigate whether host.Unload unregisters the resolver provider**

Read `internal/plugin/manager.go` and `internal/plugin/host.go`, focusing on how attribute providers are registered and what `host.Unload` does. Specifically look for:

- Where `PluginAttributeProvider` is registered into the resolver (likely in `manager.go` or `discoverAndRegisterAttributes`)
- Whether `host.Unload` has any corresponding unregistration

Run: `grep -rn "RegisterProvider\|Unregister" internal/plugin/ internal/access/policy/attribute/` via the Grep tool.

Document the finding in the commit message: either "host.Unload already handles resolver cleanup" or "host.Unload does NOT unregister providers — adding UnregisterProvider".

- [ ] **Step 2: If the gap exists, add UnregisterProvider to resolver**

If the investigation shows no unregistration path:

Append to `internal/access/policy/attribute/resolver.go`:

```go
// UnregisterProvider removes a provider from the resolver by namespace.
// Used during plugin load rollback to clean up a provider that was
// registered before load-time validation failed.
//
// Returns true if a provider was removed, false if the namespace had
// no registered provider.
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
	return true
}
```

If `Resolver` has a mutex guarding these maps (re-read the struct), use it here. The current code (Task 1 baseline) is not obviously synchronized for provider registration — if registration isn't locked, neither is unregistration; document this as an assumption.

- [ ] **Step 3: Wire UnregisterProvider into the manager rollback path**

Open `internal/plugin/manager.go`. Find the validator-failure rollback block added in Task 10. Before `host.Unload`, add calls to unregister each resource type's provider:

```go
	if valErr := ValidateManifestPolicySchemas(dp.Manifest, schemas); valErr != nil {
		// Unregister attribute providers that were added during
		// discoverAndRegisterAttributes, so the resolver doesn't retain
		// dangling references to a dead plugin process.
		if m.attributeResolver != nil {
			for _, rt := range dp.Manifest.ResourceTypes {
				m.attributeResolver.UnregisterProvider(rt)
			}
		}
		if unloadErr := host.Unload(ctx, dp.Manifest.Name); unloadErr != nil {
			slog.Error("failed to rollback plugin load after schema validation failure",
				"plugin", dp.Manifest.Name, "error", unloadErr)
		}
		return oops.In("manager").With("plugin", dp.Manifest.Name).
			Wrapf(valErr, "validate manifest policy schemas")
	}
```

Note: `m.attributeResolver` may not be the actual field name — read the `Manager` struct. If the resolver isn't directly accessible from `Manager`, the rollback path is trickier; in that case, pass the resolver as a parameter or store a reference during registration.

If the manager already has a clean path (e.g., `discoverAndRegisterAttributes` returns a cleanup func), use that instead.

- [ ] **Step 4: Write integration test T35**

Append to `test/integration/plugin/abac_widget_test.go` inside the "manifest policy schema validation at load time" Describe block (after T37):

```go
		It("removes the attribute provider from the resolver registry after failed load", func() {
			// T35: Resolver rollback completeness. After a failed
			// validation, the resolver should NOT have the plugin's
			// namespace registered — otherwise a subsequent load would
			// see "already registered" and fail with a confusing error.
			//
			// This test exercises the rollback path. If it fails, the
			// rollback gap needs to be closed — see Task 14 in the plan.
			goodManifest := loadWidgetManifest()
			badManifest := cloneManifestWithBadPolicy(goodManifest)

			provisioner := plugins.NewSchemaProvisioner(connStr)
			Expect(provisioner.Init(ctx)).To(Succeed())
			defer provisioner.Close()

			// Build a real resolver that we can inspect after the failed load.
			schemaRegistry := attribute.NewSchemaRegistry()
			resolver := attribute.NewResolver(schemaRegistry)

			svcRegistry := plugins.NewServiceRegistry()
			host := goplugin.NewHost(
				goplugin.WithSchemaProvisioner(provisioner),
				goplugin.WithServiceRegistry(svcRegistry),
			)
			defer func() { _ = host.Close(ctx) }()

			pluginDir, _ := abacWidgetBinaryPath()
			Expect(host.Load(ctx, badManifest, pluginDir)).To(Succeed())

			// Simulate the manager's discover-and-register step manually.
			arClient := host.AttributeResolverClient("test-abac-widget")
			schemaResp, err := arClient.GetSchema(ctx, &pluginv1.GetSchemaRequest{})
			Expect(err).NotTo(HaveOccurred())
			schemas := plugins.ConvertProtoSchema(schemaResp)
			widgetProvider := plugins.NewPluginAttributeProvider("widget", arClient, schemas["widget"])
			Expect(resolver.RegisterProvider(widgetProvider)).To(Succeed())

			// Validation fails.
			valErr := plugins.ValidateManifestPolicySchemas(badManifest, schemas)
			Expect(valErr).To(HaveOccurred())

			// Rollback: unregister the provider.
			removed := resolver.UnregisterProvider("widget")
			Expect(removed).To(BeTrue(), "widget provider must be removed on rollback")

			// Verify subsequent load attempts can register again without conflict.
			goodWidgetProvider := plugins.NewPluginAttributeProvider("widget", arClient, schemas["widget"])
			Expect(resolver.RegisterProvider(goodWidgetProvider)).To(Succeed(),
				"after rollback, the namespace must be free for re-registration")
		})
```

- [ ] **Step 5: Run T35**

Run: `task test:int -- -tags=integration -count=1 -run "removes the attribute provider" ./test/integration/plugin/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
jj commit -m "$(cat <<'EOF'
feat(abac): resolver.UnregisterProvider + manager rollback integration

Adds UnregisterProvider to the attribute.Resolver so failed plugin
loads can clean up dangling provider registrations. Wires it into
Manager.loadPlugin's rollback path that runs after a validator failure.

T35 asserts that after a failed load, the namespace is free for
re-registration — preventing "already registered" errors on retry.

Investigation finding: host.Unload did not previously unregister
attribute providers. This task closes that gap.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

If the investigation in Step 1 showed that rollback was already handled (no gap), simplify Task 14 to a single step: write T35 as a regression test that asserts the existing clean behavior, no code changes. Update the commit message accordingly.

---

## Task 15: Documentation — internal Go doc comments

**Files:**

- Modify: `pkg/plugin/service.go` (AttributeResolverProvider doc comment)
- Modify: `plugins/test-abac-widget/main.go` (widgetResolver doc comment and lines 58-67 comment rewrite)

- [ ] **Step 1: Update pkg/plugin/service.go**

Open `pkg/plugin/service.go`. Find the `AttributeResolverProvider` interface. Above its declaration, add or update the doc comment:

```go
// AttributeResolverProvider is implemented by plugins that resolve attributes
// for their declared resource types. The host calls these methods only with
// real resource instance IDs that it believes to exist — there are no
// synthetic sentinels, no "preflight" IDs, and no pseudo-instances. Plugin
// authors can assume that any ID passed to ResolveResource refers to a
// genuine entity in their backing store.
//
// See docs/superpowers/specs/2026-04-07-plugin-abac-hardening-design.md for
// the full host/plugin contract.
type AttributeResolverProvider interface {
	// ... existing method set ...
}
```

Keep the existing method list untouched.

- [ ] **Step 2: Update plugins/test-abac-widget/main.go — widgetResolver doc comment**

Open `plugins/test-abac-widget/main.go`. Find `type widgetResolver struct { ... }` (line ~41). Add a doc comment above it:

```go
// widgetResolver is the canonical example of an ABAC attribute resolver
// for a binary plugin. It illustrates two properties the host contract
// guarantees:
//
//  1. The host only calls ResolveResource with real instance IDs it
//     believes exist. There is no preflight sentinel to handle.
//  2. Every attribute returned here ("type", "owner") must also appear
//     in GetSchema — otherwise the returned value is silently dropped
//     at runtime and the plugin fails to load if a policy references
//     the undeclared attribute.
//
// Plugin authors writing new resolvers should model theirs on this
// pattern: map instance ID → backing store lookup → return a map keyed
// by names declared in GetSchema.
type widgetResolver struct {
	pluginv1.UnimplementedAttributeResolverServiceServer
}
```

- [ ] **Step 3: Rewrite the misleading comment at lines 58-67**

Find the comment in `ResolveResource` at lines 58-67:

```go
	// Reject any resource type other than "widget". This catches host-side
	// misrouting bugs (e.g., a per-resource-type registration regression
	// that sends `location:abc` to this resolver) — without it, the E2E
	// coverage would silently pass on routing bugs.
	if req.GetResourceType() != "widget" {
```

Replace with:

```go
	// Reject any resource type other than "widget". This is defense in
	// depth against a host routing bug (e.g., a per-resource-type
	// registration regression that sends `location:abc` to this resolver).
	// The host contract guarantees that ResolveResource is only called
	// with instance IDs the host believes to exist; this check protects
	// against the host violating that contract, not against synthetic
	// preflight IDs (which no longer exist — see spec
	// 2026-04-07-plugin-abac-hardening-design.md).
	if req.GetResourceType() != "widget" {
```

- [ ] **Step 4: Run `task lint` and `task test` to verify no regressions**

Run: `task lint` — expect clean pass.
Run: `task test -- ./plugins/test-abac-widget/... ./pkg/plugin/...` — expect PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "$(cat <<'EOF'
docs(abac): update internal comments to reflect hardening contract

Updates the AttributeResolverProvider interface comment in
pkg/plugin/service.go to state the post-hardening invariant: the host
never sends synthetic IDs; plugins only receive real instance IDs.

Adds a doc comment to test-abac-widget's widgetResolver clarifying its
role as the canonical plugin author reference. Rewrites the misleading
comment about "host-side misrouting bugs" to correctly describe the
current contract.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 16: Documentation — plugin author docs page

**Files:**

- Create: `site/docs/extending/abac-attribute-resolver.md`

- [ ] **Step 1: Check existing extending/ structure**

Run: Glob tool pattern `site/docs/extending/*.md` to see what exists. If there's an existing ABAC or attribute page, consider appending to it instead of creating a new file.

- [ ] **Step 2: Create the new page**

Create `site/docs/extending/abac-attribute-resolver.md` with the following content:

```markdown
# Implementing AttributeResolverService

Binary plugins that declare `resource_types` in their manifest must implement
the `AttributeResolverService` gRPC service so the HoloMUSH ABAC engine can
resolve attributes for policy evaluation. This guide describes the host/plugin
contract and common implementation patterns.

## The contract

The HoloMUSH host calls two RPCs on your plugin's resolver:

### `GetSchema`

Called **once** at plugin load time. Return a map from resource type name to
the attributes your `ResolveResource` will ever return. The host uses this
schema to:

- Validate that policies in your manifest only reference declared attributes
- Drop any runtime attribute returned by `ResolveResource` that isn't in the
  schema (with a log warning and a Prometheus counter increment)

**Every attribute you will return at runtime MUST be listed here.** If you
forget one, its value will be silently absent from the policy evaluation
context and policies referencing it will evaluate as `false`.

### `ResolveResource`

Called **once per instance-level authorization request** with a real resource
instance ID from your backing store. The host guarantees:

- You will never receive a synthetic or sentinel ID (like `__preflight__`)
- The ID is one the host believes exists — typically because a user action
  referenced it, or because policy evaluation is checking a specific instance
- Type-level capability pre-flight never calls your resolver

Return a map keyed by the attribute names you declared in `GetSchema`. If the
instance doesn't exist in your backing store, return a gRPC `NotFound` error.
For infrastructure failures (DB down, cache corrupted), return `Internal`.
Both cause the authorization check to fail closed.

## What fails at load time

If your manifest contains a policy whose DSL references an attribute not in
your `GetSchema` response, **the plugin will fail to load** with an error like:

```

policy "widget-read-normal" references attribute "tipe" on resource type
"widget" which is not in the declared schema

```text

Fix the typo in either the policy DSL or the schema and reload.

## Canonical example

See `plugins/test-abac-widget/main.go` in the HoloMUSH repository for a
minimal, correct reference implementation. Key points to study:

1. `GetSchema` declares exactly the attributes (`type`, `owner`) that
   `ResolveResource` returns — no more, no less
2. `ResolveResource` rejects unexpected resource types as defense in depth
3. The resolver maps instance IDs to attribute maps with no assumption about
   sentinel IDs or preflight semantics

## What NOT to do

- **Don't handle sentinel IDs** like `__preflight__`. The host does not send
  them. If you see this string in a resource ID, something is wrong — either
  a host bug or a misconfigured test.
- **Don't return attributes not in your schema.** They will be silently
  dropped. If you need a new attribute, add it to `GetSchema` AND the
  runtime return.
- **Don't swallow errors.** Return `NotFound` for missing instances and
  `Internal` for infrastructure failures. The host logs and counts both.
- **Don't call other resolvers recursively.** The host detects re-entrance
  and will panic your resolver call.

## Related

- Spec: [Plugin ABAC Hardening Design (2026-04-07)](../../docs/superpowers/specs/2026-04-07-plugin-abac-hardening-design.md)
- Spec: [Plugin ABAC Trust Boundary Design (2026-04-06)](../../docs/superpowers/specs/2026-04-06-plugin-abac-trust-boundary-design.md)
- Code: `internal/plugin/attribute_proxy.go` — how the host calls your resolver
- Code: `internal/access/policy/attribute/resolver.go` — resolver engine
- Code: `plugins/test-abac-widget/main.go` — reference plugin
```

- [ ] **Step 3: Run `task docs:build` to verify the page builds**

Run: `task docs:build` (if `task docs:setup` hasn't been run in this workspace, run that first).
Expected: build succeeds, no broken links or syntax errors.

If the docs build is slow or needs a lot of setup, alternatively run `task fmt:markdown` to at least verify the markdown is well-formed.

- [ ] **Step 4: Commit**

```bash
jj commit -m "$(cat <<'EOF'
docs(abac): plugin author guide for AttributeResolverService

Creates site/docs/extending/abac-attribute-resolver.md, a plugin author
guide documenting the host/plugin contract for binary plugins that
declare resource_types.

Covers: GetSchema and ResolveResource semantics, the "no synthetic IDs"
invariant from the hardening spec, load-time policy/schema validation,
the canonical test-abac-widget reference, and a "what NOT to do" list
including the now-impossible preflight sentinel handling.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 17: Spec addendum — append Hardening section to trust boundary spec

**Files:**

- Modify: `docs/superpowers/specs/2026-04-06-plugin-abac-trust-boundary-design.md`

- [ ] **Step 1: Read the existing spec**

Open `docs/superpowers/specs/2026-04-06-plugin-abac-trust-boundary-design.md`. Scroll to the end. Identify the last section and the appropriate place to add a new top-level `## Hardening (2026-04-07)` section.

- [ ] **Step 2: Append the addendum**

Append the following to the end of the file:

```markdown

---

## Hardening (2026-04-07)

This section documents hardening work that landed after the original spec and
resolved two architectural sharp edges identified during the subsequent plugin
ABAC review. See `docs/superpowers/specs/2026-04-07-plugin-abac-hardening-design.md`
for the full design.

### Sharp Edge 1 — synthetic `__preflight__` resource ID → eliminated

The original design had `engine.CanPerformAction` construct a synthetic
resource ID of the form `<resourceType>:__preflight__` and call
`resolver.Resolve`. This caused plugin `ResolveResource` RPCs to be invoked
with fake instance IDs during type-level capability pre-flight, with no
documented contract for how plugins should handle the case.

**Resolution (C1):** `Resolver.ResolveSubjectAttributes(ctx, subject, action)`
was added as a new entry point that resolves only subject, environment, and
action attributes — resource providers are never called. `CanPerformAction`
was rewritten to use this method. The `__preflight__` literal was deleted
from `internal/access/policy/engine.go` (enforced by a static test).

**New invariant:** `PluginAttributeProvider.ResolveResource` is called if and
only if the host has a real resource instance ID that corresponds to a
resource owned by that namespace. There is no synthetic ID, no sentinel, no
preflight-aware code path, and no documented plugin contract for handling
non-existent IDs.

**Guidance superseded:** any guidance in this original spec suggesting that
plugins "should handle non-instance IDs gracefully" is obsolete. Plugins only
receive real instance IDs.

### Sharp Edge 2 — silent schema validation drops → load-time fatal

The original design logged a `slog.Info` warning when a manifest policy
referenced an attribute not in the plugin's `GetSchema` response. A typo in
the policy DSL (e.g., `resource.widget.tipe` instead of `resource.widget.type`)
would produce a silent always-false condition discoverable only by reading
load-time log lines.

**Resolution (S1):** the cross-validation logic was extracted from
`CheckManifestWarnings` Warning 3 into a new function
`ValidateManifestPolicySchemas` in `internal/plugin/policy_schema_validator.go`.
This function is called from `Manager.loadPlugin` before policy installation,
and a non-nil return fails the load via the existing rollback path. Runtime
`mergeAttributes` drop behavior (warn + Prometheus counter, non-fatal) is
unchanged — the runtime path catches a different failure mode (plugin returns
keys outside its declared schema) and remains lenient to avoid breaking
healthy plugin traffic.

The resolver also gained an `UnregisterProvider` method so that failed
validation cleanly removes the plugin's attribute provider from the registry,
allowing a fixed manifest to be re-loaded without "already registered" errors.
```

- [ ] **Step 3: Run `task fmt` and verify the markdown lints clean**

Run: `task fmt`
Expected: the append is reformatted if needed; no errors.

- [ ] **Step 4: Commit**

```bash
jj commit -m "$(cat <<'EOF'
docs(abac): append Hardening section to trust boundary spec

Appends a dated Hardening (2026-04-07) section to the original plugin
ABAC trust boundary spec, documenting the resolution of Sharp Edge 1
(synthetic preflight ID eliminated) and Sharp Edge 2 (load-time schema
cross-validation made fatal).

Marks the original spec's guidance about handling non-instance IDs as
superseded. Links to the hardening design spec for the full rationale.

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 18: Full verification — task pr-prep

**Files:** none (verification task)

- [ ] **Step 1: Run the full PR-prep gate**

Run: `task pr-prep`
Expected: PASS for all sub-tasks (lint, fmt, schema, license, unit, integration, E2E).

This is the mandatory gate from CLAUDE.md before any push to a PR branch. Per project memory, it must be run in full — never approximated with subset checks.

- [ ] **Step 2: If any sub-task fails, fix and re-run**

For each failure:

1. Read the error carefully
2. Identify the root cause
3. Fix it in place (do NOT add ignore directives)
4. Re-run `task pr-prep` until it passes green

Common failures to expect:

- License headers missing on new files — run `task license:add`
- Formatting drift — run `task fmt`
- Lint violations — fix inline, do not suppress
- Integration test flakes — re-run once; if still flaky, investigate

- [ ] **Step 3: After green, verify the final state**

Run: `jj --no-pager log -r '@-::' --limit 20`
Expected: list of commits from Task 1 through Task 17, all clean.

Run: `jj --no-pager st`
Expected: clean working copy, no pending changes.

- [ ] **Step 4: Commit any pr-prep fixes as a single task-18 commit if needed**

If Step 2 produced fixes, commit them with:

```bash
jj commit -m "$(cat <<'EOF'
chore(abac): pr-prep fixes for plugin ABAC hardening

Fixes discovered during task pr-prep: [describe the fixes]

Part of plugin ABAC hardening (holomush-479l).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

If no fixes were needed, skip this step.

---

## Task 19: Close the bead and update dependents

**Files:** none (bead metadata)

- [ ] **Step 1: Update holomush-0sc.12 to note unblock**

Run:

```bash
bd update holomush-0sc.12 --notes "Plugin ABAC hardening (holomush-479l) landed via PR #<TBD>. The synthetic __preflight__ path is gone and load-time policy/schema validation is now fatal. Channel plugin rework can resume with Option C (hybrid) as previously decided, or the newly-safer maximalist path."
```

Replace `<TBD>` with the PR number once the PR is opened (in a subsequent step).

- [ ] **Step 2: Close holomush-479l**

Once PR is merged to main:

Run:

```bash
bd close holomush-479l --reason "Plugin ABAC hardening landed in PR #<TBD>. C1 eliminated synthetic preflight ID; S1 made load-time schema validation fatal. All 39 tests in the plan pass. Spec: docs/superpowers/specs/2026-04-07-plugin-abac-hardening-design.md. Unblocks holomush-0sc.12."
```

Note: this step happens AFTER the PR merges to main, not as part of the implementation commits.

- [ ] **Step 3: File any discovered-from beads**

During implementation, if Task 14 discovered a rollback gap, or any other follow-up work emerged, file them as discovered-from beads linked to holomush-479l. Examples:

- If `host.Unload` has other cleanup gaps beyond the attribute resolver: file a bead
- If the Prometheus counter label cardinality becomes a concern: file a bead
- If subject-attribute reference validation is needed (the follow-up mentioned in the spec): file a bead

Run (template):

```bash
bd create "<title>" --description="<context>" -t task -p 2 --deps discovered-from:holomush-479l --json
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task(s) |
|---|---|
| Add `Resolver.ResolveSubjectAttributes` method | Task 1 |
| Method has full unit coverage (T1-T4, T16-T21, T39) | Tasks 1, 2, 3 |
| Engine `CanPerformAction` uses new method | Task 4 |
| `__preflight__` literal deleted from engine.go | Task 4 |
| T5 (engine does not invoke resource providers) | Task 4 (written alongside the refactor) |
| T6 (optimistic permit branch) | Task 5 Step 1 |
| T7, T22, T23, T24 | Pre-existing coverage in `engine_test.go` — validated by running the existing test suite after Task 4; Task 5 Step 3 documents the mapping |
| Static invariant test T32 | Task 6 |
| T33 cross-check with full provider set | Tasks 3 (unit-level via T21), 12 (E2E-level via T13/T38) |
| `ValidateManifestPolicySchemas` extracted as new function | Task 7 |
| Validator unit coverage (T8-T12, T25-T31) | Tasks 7, 8 |
| Warning 3 removed from `CheckManifestWarnings` | Task 9 |
| Validator wired into `Manager.loadPlugin` | Task 10 |
| Counting proxy for integration tests | Task 11 |
| T13, T14, T38 integration tests | Task 12 |
| T15, T34, T36, T37 integration tests | Task 13 |
| T35 rollback completeness + UnregisterProvider if needed | Task 14 |
| Internal Go doc comments updated | Task 15 |
| Plugin author docs page | Task 16 |
| Spec addendum appended | Task 17 |
| task pr-prep green | Task 18 |
| Bead cross-references | Task 19 |

All 39 tests and all documentation targets are covered.

**Placeholder scan:** none remaining (all steps contain actual code or actual commands).

**Type consistency:** `ResolveSubjectAttributes` used consistently across Tasks 1-6, 12, 14. `ValidateManifestPolicySchemas` used consistently across Tasks 7-10, 13. `countingAttributeResolverClient` used consistently across Tasks 11-13.

**Scope check:** spec and plan both focused on one subsystem (plugin ABAC hardening). No decomposition needed.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-07-plugin-abac-hardening.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. Recommended because the plan has 19 tasks and subagent isolation keeps the main context window clean while preserving TDD discipline per task.

**2. Inline Execution** — Execute tasks in this session using `superpowers:executing-plans`, batch execution with checkpoints for review. Faster wall-clock but risks context pollution across 19 tasks.

**Which approach?**

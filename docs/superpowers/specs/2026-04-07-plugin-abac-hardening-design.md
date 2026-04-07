# Plugin ABAC Hardening Design

**Status:** Approved
**Date:** 2026-04-07
**Author:** seanb4t (via Claude Opus 4.6)
**Bead:** holomush-479l
**Blocks:** holomush-0sc.12 (channel plugin rework)
**Related:** docs/superpowers/specs/2026-04-06-plugin-abac-trust-boundary-design.md (establishes the trust boundary this spec hardens)

## RFC2119 Keywords

| Keyword        | Meaning                                    |
| -------------- | ------------------------------------------ |
| **MUST**       | Absolute requirement                       |
| **MUST NOT**   | Absolute prohibition                       |
| **SHOULD**     | Recommended, may ignore with justification |
| **SHOULD NOT** | Not recommended                            |
| **MAY**        | Optional                                   |

## Problem

The plugin ABAC trust boundary from PR #195 introduces `AttributeResolverService` for binary plugins to resolve resource attributes for their declared `resource_types`. Two architectural sharp edges in that design are untested and present operational risk for any plugin that adopts the maximalist ABAC pattern (returning list-typed resource attributes and relying on policy evaluation at preflight time).

This spec describes the hardening that closes both sharp edges. It MUST land before plugins like `core-channels` (holomush-0sc.12) can safely use list-typed resource attributes via the resolver.

### Sharp Edge 1: Synthetic preflight resource ID flows into plugin RPC

Layer 2 capability pre-flight (`internal/command/access.go:CheckCapabilityPreFlight`) calls `engine.CanPerformAction(subject, action, resourceType, scope)` for each declared command capability. `internal/access/policy/engine.go:401` constructs a synthetic resource ID of the form `<resourceType>:__preflight__` and passes it to `resolver.Resolve`. The resolver iterates all registered providers, including any `PluginAttributeProvider` for the matching namespace.

For a plugin declaring `resource_types: [channel]`, this means the plugin's `AttributeResolverService.ResolveResource(channel, "__preflight__")` RPC is invoked once per pre-flight check, every time anyone runs the channel command, with a non-real resource ID. The plugin has no documented contract for what to do with this.

If the plugin:

- **Errors out** (e.g., "channel not found in DB") → `safeResolve` returns the error, the resolver returns non-nil, `CanPerformAction` step 6 fails closed, pre-flight fails for every caller. Plugin command unusable in production.
- **Returns "real" attributes for `__preflight__`** → policies evaluate against synthetic state, may pass or fail incorrectly.
- **Returns empty/nil attributes** → optimistic-permit branch at `engine.go:490` fires, pre-flight passes, instance-level `Evaluate` enforces the real check. This is the correct behavior — but there is no documentation telling plugin authors this is what they MUST do, no host-side defense against the other two cases, and `plugins/test-abac-widget/main.go` only satisfies the contract by accident (its default `widgetType := "normal"` covers `__preflight__` coincidentally).

### Sharp Edge 2: Schema validation silently drops unknown attribute keys

`internal/access/policy/attribute/resolver.go:312-327` `mergeAttributes` validates each returned attribute against the namespace's registered schema. Keys not in the schema are dropped with a `slog.Warn` log and a Prometheus counter increment (`abac_rejected_provider_attributes_total{namespace,key}`), but the resolver returns success.

If a plugin returns an attribute that isn't declared in its `GetSchema` response, OR if a policy references an attribute the plugin forgot to declare, the attribute is silently absent from the bag. Policies that depend on it evaluate as false (or true via optimistic preflight), with no surfaced error.

Additionally, `internal/plugin/manifest_warnings.go:CheckManifestWarnings` contains a load-time cross-validation (Warning 3) that walks each policy's AST, collects `resource.<type>.<attr>` references, and warns when `<attr>` isn't in the plugin's `GetSchema` response. This cross-validation IS wired in at `internal/plugin/manager.go:498` but logs via `slog.Info` — a plugin author who typos `resource.widget.tipe == "normal"` in a policy gets a silent always-false condition discoverable only by reading load-time log lines.

## Decisions

### Sharp Edge 1 resolution: C1 — eliminate the synthetic preflight ID at the host layer

**Decision:** Add a new method `Resolver.ResolveSubjectAttributes(ctx, subject, action)` that resolves only subject, environment, and action attributes. `engine.CanPerformAction` MUST call this method instead of constructing `<type>:__preflight__` and calling `Resolve`. The `__preflight__` literal MUST be deleted from the codebase.

**Alternatives considered:**

| Option | Description | Rejected because |
|---|---|---|
| **(i)** Documentation only | Add prominent docs requiring plugin authors to handle synthetic IDs gracefully | Pushes correctness burden onto every plugin author forever; leaky abstraction; no defense in depth |
| **(ii)** Host-side filter in `PluginAttributeProvider` | Detect synthetic IDs and short-circuit before calling plugin RPC | Papers over the abstraction bug; still has `__preflight__` literal in the engine |
| **(iii-literal)** New plugin proto RPC `GetDefaultAttributes` | Plugin RPC for type-level defaults | Proto change, SDK churn, forces plugin migration, solves no real use case — type-level attributes are a mis-modeling; preflight needs only subject attributes |
| **(C1, chosen)** Subject-only resolution path | Engine never resolves resource attributes at preflight | Deletes the bug rather than filtering it; no proto change; plugin authors never need to know preflight exists; invariant is enforced by construction |

**Rationale:** Type-level preflight only needs subject attributes. Resource attributes are intentionally absent — the optimistic-permit branch at `engine.go:490` already handles permits whose conditions reference resource attrs by treating them as "may apply, instance-level `Evaluate` will enforce the full condition." The synthetic `__preflight__` exists only because `Resolver.Resolve(ctx, AccessRequest)` requires a well-formed access request including a resource. A separate entry point eliminates the need.

### Sharp Edge 2 resolution: S1 — load-time cross-validation becomes fatal

**Decision:** Extract the policy/schema cross-validation from `CheckManifestWarnings` Warning 3 into a new function `ValidateManifestPolicySchemas(manifest, schemas) error`. This function MUST be called during `Manager.loadPlugin`, before policy installation, and a non-nil return MUST cause the plugin load to fail via the existing rollback path. The runtime `mergeAttributes` drop behavior (warn + counter, non-fatal) is unchanged.

**Alternatives considered:**

| Option | Description | Rejected because |
|---|---|---|
| **S2** Runtime strict (fail `mergeAttributes` on unknown key) | Resolver returns error when a provider returns an undeclared attribute | Introduces a new failure path that could take down healthy plugin traffic over one extra undeclared attribute; typos are better caught at load time |
| **S3** Both load-time and runtime strict | Defense in depth at both layers | Same runtime risk as S2 with no additional benefit beyond S1 — load-time typo catches are the real win |
| **S4** Load-time fatal + probe-based runtime validation | Load-time sample-ID probe to verify `GetSchema` matches `ResolveResource` | New contract surface (sample IDs); marginal benefit over S1; too clever |
| **S1 (chosen)** Load-time strict only | Policy DSL typos fail at load, runtime stays lenient | Closes the dangerous gap (typos in policy DSL silently evaluating as false) without breaking live traffic for honest plugin bugs |

**Rationale:** The dangerous failure mode in Sharp Edge 2 is a plugin author typoing a policy attribute name and not noticing. S1 catches this at deploy time. The "runtime returns keys not in schema" failure mode is a plugin implementation bug best caught by tests and the existing Prometheus counter, not by failing live authorization requests.

## Architecture

The hardening lands as two host-side surgical changes confined to `internal/access/policy/` and `internal/plugin/`. No proto, SDK, or plugin binary changes.

| File | Change | Lines |
|---|---|---|
| `internal/access/policy/attribute/resolver.go` | Add `ResolveSubjectAttributes` method | ~30 |
| `internal/access/policy/engine.go` | `CanPerformAction` calls new method; delete `__preflight__` literal | ~15 |
| `internal/plugin/manifest_warnings.go` or new `policy_schema_validator.go` | Extract `ValidateManifestPolicySchemas`; remove Warning 3 case | ~60 |
| `internal/plugin/manager.go` | Call validator before policy install; fail load on error | ~15 |
| `pkg/plugin/service.go` | Doc comment update on `AttributeResolverProvider` | ~10 |
| `plugins/test-abac-widget/main.go` | Doc comment update on `widgetResolver` | ~15 |
| Test files | See Section 4 | ~600 |

### Invariant established

After this change, `PluginAttributeProvider.ResolveResource` MUST be called if and only if the host has a real resource instance ID that it believes corresponds to a resource owned by that namespace. There MUST be no synthetic ID, no sentinel, no preflight-aware code path, and no documented plugin contract for handling "fake" IDs. This invariant MUST be enforced by construction (the code path does not exist), not by filtering.

## Change A: Subject-only resolution (Sharp Edge 1, C1)

### New method signature

```go
// ResolveSubjectAttributes resolves subject, action, and environment attributes
// for a type-level capability check (preflight). It never calls resource
// providers, which makes it safe to use when no resource instance is available.
//
// Returns AttributeBags with Subject/Action/Environment populated and Resource
// empty. Error semantics match Resolve: partial success returns both bags and
// error; callers MUST fail closed on non-nil error and MUST NOT use partial
// bags for policy evaluation.
func (r *Resolver) ResolveSubjectAttributes(
    ctx context.Context,
    subject, action string,
) (*types.AttributeBags, error)
```

### Internals

The method MUST:

- Validate `subject` via the existing `validateEntityRef` (empty or malformed → error with `INVALID_ENTITY_REF`)
- Mark the context with the existing re-entrance guard (`markInResolution`) and cache scope (`withCache`)
- Populate `bags.Action["name"] = action` (matching `Resolve`)
- Call the unexported `resolveEntity(ctx, "subject", subject, bags.Subject)` exactly as `Resolve` does
- Call `resolveEnvironment(ctx, bags.Environment)` exactly as `Resolve` does
- NOT call `resolveEntity(ctx, "resource", ...)`
- Return joined errors via `errors.Join` matching `Resolve`'s semantics

### Engine call site changes

`engine.go:CanPerformAction` Step 4 changes from:

```go
// Step 4: Resolve subject attributes via a synthetic request.
syntheticReq, reqErr := types.NewAccessRequest(subject, action, resourceType+":__preflight__")
if reqErr != nil { ... }
bags, resolveErr := e.resolver.Resolve(ctx, syntheticReq)
```

to:

```go
// Step 4: Resolve subject + environment attributes only. No resource
// instance exists at type-level preflight; resource providers are never
// called. The optimistic-permit branch below handles permits whose
// conditions reference resource attributes.
bags, resolveErr := e.resolver.ResolveSubjectAttributes(ctx, subject, action)
```

Steps 1-3 (context cancellation, degraded mode, subject format validation) MUST remain identical. Steps 5-10 (cache snapshot, policy filtering, optimistic permit branch, forbid-overrides-permit) MUST remain unchanged — they already work correctly when `bags.Resource` is empty.

The `resourceType+":__preflight__"` literal MUST be deleted. The `resourceType` parameter is still used by Step 6 to filter candidate policies by resource type and remains.

### Inherited safety properties

| Mechanism | Behavior | How it's preserved |
|---|---|---|
| Re-entrance guard | Panic on recursive resolver call | Uses same context key as `Resolve` |
| Request-scoped attribute cache | Fresh cache per call | Uses same `withCache(ctx)` |
| Circuit breakers | Per-namespace failure tracking | Only subject providers' CBs trip, because resource providers are never called |
| Provider panic recovery | Panic → error with provider context | Reuses `safeResolve` path |
| Subject ref validation | Fail with `INVALID_ENTITY_REF` on malformed refs | Reuses `validateEntityRef` |
| Context cancellation | Fail closed when ctx is cancelled | Same short-circuit as `Resolve` |

## Change B: Load-time cross-validation hardening (Sharp Edge 2, S1)

### New function signature

```go
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
func ValidateManifestPolicySchemas(
    manifest *Manifest,
    schemas map[string]*types.NamespaceSchema,
) error
```

### Error format

The returned error MUST wrap a descriptive message with `oops` context. Concrete format:

```text
policy "widget-read-normal" references attribute "tipe" on resource type "widget" which is not in the declared schema
```

with oops fields:

| Field | Value |
|---|---|
| `plugin` | The plugin name from the manifest |
| `policy` | The offending policy name |
| `resource_type` | The resource type in the policy target |
| `attribute` | The undeclared attribute name |
| `schema_keys` | `[]string` of attributes that WERE declared, as a debugging hint |

Error code (via `oops.Code`): `PLUGIN_SCHEMA_VALIDATION_FAILED`.

### Single error vs. aggregated errors

The function MUST return on the first mismatch rather than aggregating. A plugin author fixing one typo will re-run the load anyway; aggregation only helps for multi-typo fixes, which are rare. A single clear error is more agent-friendly and simpler to test. If experience shows aggregation is wanted later, it's a non-breaking follow-up.

### Load path sequence

Current sequence in `Manager.loadPlugin`:

```text
1. host.Load(ctx, manifest, pluginDir)
2. discoverAndRegisterAttributes(ctx, host, dp)  // GetSchema, returns schemas
3. InstallPluginPoliciesWithManifest(ctx, manifest, manifest.Policies)
4. CheckManifestWarnings(manifest, schemas)  // non-fatal; Warning 3 is Sharp Edge 2's current defense
```

New sequence (as shipped):

```text
1. host.Load(ctx, manifest, pluginDir)
2. discoverAndRegisterAttributes(ctx, host, dp)        // registers PluginAttributeProvider on shared resolver
3. ValidateManifestPolicySchemas(dp.Manifest, schemas) // hard failure here, runs before any DB write
4. InstallPluginPoliciesWithManifest(ctx, dp.Manifest, dp.Manifest.Policies)
5. CheckManifestWarnings(dp.Manifest)                  // single-arg; Warning 3 removed; schemas no longer needed
```

Validation MUST run before policy install so the database stays clean on failure.

### Rollback on validation failure

The validator-failure branch in `Manager.loadPlugin` unwinds the provider registrations performed in step 2 BEFORE calling `host.Unload`:

```go
if valErr := ValidateManifestPolicySchemas(dp.Manifest, schemas); valErr != nil {
    m.unregisterPluginProviders(dp.Manifest.Name, dp.Manifest.ResourceTypes, len(dp.Manifest.ResourceTypes))
    if unloadErr := host.Unload(ctx, dp.Manifest.Name); unloadErr != nil {
        slog.Error("failed to rollback plugin load after schema validation failure",
            "plugin", dp.Manifest.Name, "error", unloadErr)
    }
    return oops.In("manager").With("plugin", dp.Manifest.Name).
        Wrapf(valErr, "validate manifest policy schemas")
}
```

**Implementation finding (Task 14):** The Task 14 investigation found that `host.Unload` does NOT unregister `PluginAttributeProvider` from the shared `attribute.Resolver`, AND it found four additional rollback leak paths beyond the validator-failure branch (partial-loop failure inside `discoverAndRegisterAttributes`, policy-install failure, and three service-registration failures). All five gaps are now closed by:

- New `Resolver.UnregisterProvider(namespace) bool` — removes the provider, providerOrder entry, circuit breaker, AND the schema from the underlying registry (the schema cleanup uses `SchemaRegistry.UnregisterForRollback`, which bypasses the policy-reference safety check that `RemoveNamespace` performs because rollback runs before any policies could possibly reference the namespace).
- New `plugins.UnregisterPluginProviderFunc` type + `WithAttributeProviderUnregistrar` Manager option — symmetric with the existing registrar option.
- New `Manager.unregisterPluginProviders(pluginName, resourceTypes, upTo int)` helper — supports partial unwinds via `upTo` for the in-loop failure case.

T35 (`TestManagerLoadAllUnregistersAttributeProviderWhenSchemaValidationFailsAfterRegistration`) lives at the manager level and asserts the registrar+unregistrar pair is invoked correctly when validation fails.

### Edge cases

| Edge case | Behavior |
|---|---|
| Plugin without `resource_types` | `schemas == nil` or empty → validator returns nil immediately |
| Trust-escalated plugin | No special treatment — typos still fail the load |
| Policy without `when` clause | `referencedResourceAttrs` returns empty slice → nil error |
| Policy referencing `environment.*` or `principal.*` | Not a resource reference → ignored by validator |
| Policy targeting resource type not in `ResourceTypes` | Already rejected by `ValidatePluginPolicy.validateCharacterPolicy` before this validator runs |
| Multiple bad policies | Return on first mismatch (documented in tests) |

### What stays a warning

`CheckManifestWarnings` continues to run in step 5 with Warning 3's case removed:

- **Warning 1** — "command X has no execute policy covering it" stays as warning. A plugin author MAY intentionally declare a public command with no ABAC enforcement.
- **Warning 2** — "command X declares capability with no permit policy" stays as warning. A command MAY require capabilities granted by server-installed policies.

These are *coverage gap* hints, not *correctness bugs*, and hard-failing them would break legitimate plugin patterns.

## Test Plan

Tests are named per the ACE framework from `CLAUDE.md` (every test name is a sentence: Action + Condition + Expectation).

### Layer 1 — Resolver unit tests

Location: `internal/access/policy/attribute/resolver_test.go`

| ID | Name | What it proves |
|---|---|---|
| T1 | `TestResolverResolveSubjectAttributesPopulatesSubjectActionAndEnvironment` | Happy path: subject/env/action populated, resource empty |
| T2 | `TestResolverResolveSubjectAttributesReturnsErrorForInvalidSubjectRef` | Subject validation consistent with `Resolve` |
| T3 | `TestResolverResolveSubjectAttributesReturnsErrorWhenSubjectProviderFails` | Fail-closed semantics preserved |
| T4 | `TestResolverResolveSubjectAttributesDoesNotInvokeResourceProviderWhenResourceProviderExists` | Core C1 invariant — resource providers never called even when registered |
| T16 | `TestResolverResolveSubjectAttributesReturnsErrorWhenSubjectIsEmpty` | Empty subject rejected |
| T17 | `TestResolverResolveSubjectAttributesPopulatesActionNameEvenWhenEmpty` | Empty action permitted (matches `Resolve`) |
| T18 | `TestResolverResolveSubjectAttributesReturnsErrorWhenContextAlreadyCancelled` | Context cancellation honored |
| T19 | `TestResolverResolveSubjectAttributesRecoversFromPanickingProvider` | Panic safety via `safeResolve` reuse |
| T20 | `TestResolverResolveSubjectAttributesDetectsReentranceAndReturnsErrorNotInfiniteLoop` | Re-entrance guard fires; the panic is caught by `safeResolve` and surfaced as a provider-panic error (not propagated to the caller) |
| T21 | `TestResolverResolveSubjectAttributesProducesSameSubjectBagAsResolveForSameInput` | Cross-check invariant — C1 is behavior-preserving for subject path |
| T39 | `TestResolverResolveSubjectAttributesIsSafeForConcurrentCalls` | Thread safety smoke test with `-race` |

### Layer 2 — Engine unit tests

Location: `internal/access/policy/engine_test.go`

| ID | Name | What it proves |
|---|---|---|
| T5 | `TestEngineCanPerformActionDoesNotInvokeResourceProvidersWhenCapabilityCheckRuns` | Engine wired to new path — uses a tracking provider to assert resource provider calls stay at zero, AND asserts subject resolution did run (guards against silent no-op regression) |
| T6 | `TestEngineCanPerformActionPermitsOptimisticallyForPermitReferencingResourceAttrs` | Optimistic permit branch still fires |
| T7 | `TestEngineCanPerformActionAttributeResolutionError` (pre-existing) | Fail-closed via new path — pre-existing test passes after refactor as regression coverage |
| T22 | `TestEngineCanPerformActionDegradedMode` (pre-existing) | Degraded mode short-circuits — pre-existing test passes after refactor as regression coverage |
| T23 | `TestEngineCanPerformActionContextCancelled` (pre-existing) | Cancellation short-circuits — pre-existing test passes after refactor as regression coverage |
| T24 | `TestEngine_CanPerformAction_InvalidSubjectFormat` (pre-existing) | Format validation precedes resolver call — pre-existing test passes after refactor as regression coverage |

### Layer 3 — Manifest validator unit tests

Location: `internal/plugin/manifest_warnings_test.go` or new `policy_schema_validator_test.go`

| ID | Name | What it proves |
|---|---|---|
| T8 | `TestValidateManifestPolicySchemasRejectsPolicyReferencingAttributeNotInSchema` | Core S1 fail-closed path |
| T9 | `TestValidateManifestPolicySchemasAcceptsPolicyWhenAllAttributeReferencesMatchSchema` | Happy path; no false positives |
| T10 | `TestValidateManifestPolicySchemasReturnsNilForPluginWithoutResourceTypes` | Plugins without resource types bypass cleanly |
| T11 | `TestValidateManifestPolicySchemasAcceptsPolicyWithoutWhenClause` | Unconditional policies accepted |
| T12 | `TestValidateManifestPolicySchemasIgnoresEnvironmentAndPrincipalAttributeReferences` | Validator scope is resource-only |
| T25 | `TestValidateManifestPolicySchemasReturnsNilForEmptyNonNilSchemaMap` | Boundary: empty map vs nil map |
| T26 | `TestValidateManifestPolicySchemasReturnsNilWhenPolicyTypeWasAlreadyRejectedByPluginValidator` | Documents the layering assumption |
| T27 | `TestValidateManifestPolicySchemasReportsFirstErrorWhenMultiplePoliciesHaveBadAttributes` | Documents first-error policy |
| T28 | `TestValidateManifestPolicySchemasHandlesCompoundConditionsWithANDORNot` | AST walker recurses into all boolean ops |
| T29 | `TestValidateManifestPolicySchemasHandlesHasAndContainsReferences` | `has` and `contains` DSL nodes covered |
| T30 | `TestValidateManifestPolicySchemasHandlesInExprWithDynamicBothSides` | `in` expression with bilateral dynamic refs (from validated DSL feature in the bead) |
| T31 | `TestValidateManifestPolicySchemasAcceptsAttributeNameThatIsPrefixOfValidAttribute` | Exact-match lookup, no substring matching |

### Layer 4 — Integration tests with test-abac-widget plugin

Location: `test/integration/plugin/abac_widget_test.go` (build tag `integration`)

These tests MUST use the real plugin binary and the full engine stack with a real PostgreSQL (via testcontainers).

| ID | Name | What it proves |
|---|---|---|
| T13 | `"never invokes the plugin ResolveResource RPC during type-level preflight"` | C1 invariant at E2E layer with real plugin binary |
| T14 | `"still invokes the plugin ResolveResource RPC for instance-level Evaluate"` | Instance-level evaluation unaffected; plugin sees real IDs only |
| T15 | `"fails to load a plugin whose manifest policy references an undeclared resource attribute"` | Sharp Edge 2 end-to-end; rollback works; no partial state |
| T34 | `"installs zero policies in the database when manifest validation fails"` | Direct DB query after failed load; rollback completeness |
| T35 | `"removes the attribute provider from the resolver registry after failed load"` | Resolver rollback (may reveal existing rollback gap) |
| T36 | `"successfully loads a fixed manifest after a prior validation failure"` | Idempotency — failed loads don't poison future loads |
| T37 | `"installs three plugin policies in the database when manifest validation passes"` | Existing happy path + explicit DB query assertion |
| T38 | `"permits character:01ABC execute widget command via full database-backed engine stack without invoking plugin ResolveResource"` | C1 holds with full stack including DB-backed policy cache |

**T13 implementation note:** Wrap the plugin's `AttributeResolverClient` with a counting proxy (instrumented `pluginv1.AttributeResolverServiceClient` that tracks `ResolveResource` call counts). The proxy MUST be installed during `BeforeEach` before the engine is assembled.

**T15 implementation note:** Construct an in-memory `*Manifest` cloned from `loadWidgetManifest()` but with one policy's DSL mutated to reference `resource.widget.tipe` instead of `resource.widget.type`. No new plugin binary is needed — the test uses the existing binary for schema discovery and a synthetic manifest for policy validation.

### Layer 5 — Static/meta tests

Location: New test file `internal/access/policy/hardening_invariants_test.go`. Using a dedicated file (rather than inlining into `engine_test.go`) keeps the "static assertion about the codebase" tests grouped and easy to discover.

| ID | Name | What it proves |
|---|---|---|
| T32 | `TestPluginABACHardeningSourceCodeDoesNotContainPreflightSentinel` | The `__preflight__` string MUST NOT appear in `internal/access/policy/engine.go` — prevents regression |
| T33 | `TestResolverResolveSubjectAttributesWhenCombinedWithResolveProducesIdenticalSubjectBag` | Cross-check with full provider set (not just stubs) |

### Test coverage mapping to bead requirements

| Bead test | Disposition | Replacement |
|---|---|---|
| #1 CanPerformAction preflight invokes plugin RPC | Inverted | T4 (unit), T13 (E2E) — assert plugin RPC is NEVER invoked |
| #2 Plugin error during preflight | Removed (unreachable after C1) | — |
| #3 Plugin empty attrs during preflight | Removed (unreachable after C1) | — |
| #4 Runtime drops unknown key | Existing coverage (`resolver_test.go:573-575`) | — |
| #5 Load-time cross-validation | Promoted from warning to fatal | T8, T15, T34 |

### Out of test scope

- **Plugin author ergonomics.** UX of error messages is tested manually, not automated.
- **Prometheus counter label cardinality.** The `abac_rejected_provider_attributes_total` counter is labeled by `(namespace, key)` — a misbehaving plugin could pump unbounded cardinality. Operational concern, separate bead.
- **Subject attribute reference validation in policies.** A policy typoing `principal.character.roless contains "admin"` still silently evaluates as false. Flagged as follow-up.
- **Full load/request concurrency race.** T39 is a smoke test for resolver thread safety. Full race coverage between in-flight requests and plugin load is out of scope.

## Documentation Updates

### Spec addendum

`docs/superpowers/specs/2026-04-06-plugin-abac-trust-boundary-design.md` MUST gain an appended section `## Hardening (2026-04-07)` that:

- Documents Sharp Edge 1 resolution: C1 chosen, cite new file/line locations
- Documents Sharp Edge 2 resolution: S1 chosen
- Marks the original spec's "plugins should handle non-instance IDs" guidance as superseded
- States the new invariant: plugins receive only real resource instance IDs

The original spec MUST NOT be rewritten; the addendum preserves the historical record.

### Plugin author documentation

`site/docs/extending/` MUST gain a new page titled `Implementing AttributeResolverService`. The preferred file path is `site/docs/extending/abac-attribute-resolver.md`; if an existing ABAC/plugin page is a better home, the content MAY be appended to it. The page MUST cover:

1. What the host calls and when (GetSchema once at load, ResolveResource once per instance-level request)
2. What the plugin MUST declare (every attribute ResolveResource will ever return MUST appear in GetSchema)
3. What fails at load time (policy DSL referencing undeclared attributes fails load)
4. Error handling contract (NotFound vs Internal gRPC codes)
5. Canonical code example pointing to `plugins/test-abac-widget/main.go`
6. What NOT to do (don't handle sentinel IDs, don't return undeclared attributes, don't swallow errors)

### Reference widget plugin annotation

`plugins/test-abac-widget/main.go` MUST gain a doc comment on `widgetResolver` clarifying its role as the canonical reference. The existing misleading comment at lines 59-62 about "host-side misrouting bugs" MUST be rewritten to reflect the post-C1 contract. The rejection code itself MUST be retained — it's still correct defense in depth against host routing bugs.

### Internal Go documentation

| File | Update |
|---|---|
| `pkg/plugin/service.go` | `AttributeResolverProvider` comment: state the host invariant that `ResolveResource` is only called with real instance IDs |
| `internal/access/policy/engine.go` | `CanPerformAction` doc comment: remove "synthetic request" language; add "resource providers are never invoked" |
| `internal/access/policy/attribute/resolver.go` | `ResolveSubjectAttributes` doc comment per Section 4a; emphasize "never calls resource providers". Also `UnregisterProvider` doc comment covers the schema-cleanup invariant. |
| `internal/plugin/policy_schema_validator.go` | `ValidateManifestPolicySchemas` doc comment with rationale for first-error return (extracted from `manifest_warnings.go` Warning 3) |

### Bead cross-references

- `holomush-479l` close reason MUST cite the PR number and link to this spec
- `holomush-0sc.12` MUST be updated noting this hardening has landed; the channel rework can proceed with Option C (hybrid) or the newly-safer maximalist path
- `holomush-2smp` (P0 policy cache read barrier) — unaffected
- Any findings during implementation (e.g., rollback gaps from T35, missing `UnregisterProvider`) MUST be filed as discovered-from beads

## Out of Scope

- Implementing the channel plugin (holomush-0sc.12)
- Performance optimization of plugin attribute resolution (cross-call caching)
- Server-side trust escalation allowlist UX
- Refactoring `core-channels` to use maximalist ABAC after this lands
- Subject attribute reference validation at load time (follow-up)
- Concurrent load/request race hardening beyond the smoke test
- Prometheus counter label cardinality mitigation

## Acceptance Criteria

- All 39 tests in the plan land and pass
- `task pr-prep` passes green (lint, fmt, schema, license, unit, integration, E2E)
- `__preflight__` literal is absent from `internal/access/policy/engine.go` (verified by T32)
- `plugins/test-abac-widget/main.go` binary still loads and passes existing E2E tests without modification
- Documentation updates land in the same PR as the code changes
- `holomush-0sc.12` (channel plugin rework) is updated to note this hardening has landed and can proceed

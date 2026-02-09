<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

## Attribute Resolution

The engine uses eager resolution: all attributes are collected before any policy
is evaluated. This provides a complete attribute snapshot for every decision,
which powers audit logging and the `policy test` debugging command.

### Resolution Flow

```text
Evaluate(ctx, AccessRequest{Subject: "character:01ABC", Action: "enter", Resource: "location:01XYZ"})

1. Parse subject → type="character", id="01ABC"
2. Parse resource → type="location", id="01XYZ"
3. Resolve subject attributes:
   CharacterProvider.ResolveSubject("character", "01ABC")
     → {type: "character", id: "01ABC", faction: "rebels", level: 7, role: "player"}
   PluginProvider("reputation").ResolveSubject("character", "01ABC")
     → {reputation.score: 85}
4. Resolve resource attributes:
   LocationProvider.ResolveResource("location", "01XYZ")
     → {type: "location", id: "01XYZ", faction: "rebels", restricted: true}
5. Resolve environment attributes:
   EnvironmentProvider.Resolve()
     → {time: "2026-02-05T14:30:00Z", maintenance: false}
6. Assemble AttributeBags and proceed to policy evaluation
```

**Note:** This example is illustrative, not exhaustive. Only providers matching
the parsed entity types are called. When the resource is `object:01DEF`,
`ObjectProvider.ResolveResource()` is called instead of `LocationProvider`.
All registered plugin providers are called regardless of entity type — they
return `(nil, nil)` for entity types they don't handle.

### Provider Registration

Plugins register attribute providers at startup. The engine calls all registered
providers during eager resolution. Provider namespaces MUST be unique to prevent
collisions.

**Registration order constraint:** The engine MUST enforce core-first registration
order. All core attribute providers (SubjectProvider, ResourceProvider,
EnvironmentProvider) MUST register before any plugin providers. The provider
registry MUST reject plugin provider registration attempts if any core provider
has not yet registered. This prevents plugin providers from consuming timeout
budget before core providers execute under fair-share scheduling (ADR
[#82](../decisions/epic7/phase-7.3/082-core-first-provider-registration-order.md)).

```go
engine.RegisterAttributeProvider(reputationProvider)
engine.RegisterEnvironmentProvider(weatherProvider)
```

### Attribute Schema Registry

The `AttributeSchema` is a **mandatory** component provided at engine
initialization. All attribute providers MUST register their attribute schemas
(namespace, keys, types) during the registration phase. The schema registry serves
three purposes:

1. **Validation**: The `PolicyCompiler` MUST reject policies that reference
   attribute namespaces not present in the registry. Unknown namespace references
   cause compile-time errors, not runtime failures.
2. **Discovery**: The `policy attributes` admin command lists all registered
   attributes, their types, and source providers for operator inspection.
3. **Version tracking**: Plugins declare their schema version in their manifest.
   The engine detects schema changes on plugin reload and logs warnings when
   existing policies reference deprecated attributes.

**Schema registration example:**

```go
schema.RegisterNamespace("reputation", []AttributeDef{
    {Key: "score", Type: "number", Description: "Player reputation score"},
    {Key: "tier", Type: "string", Description: "Reputation tier: bronze/silver/gold"},
}, "reputation-plugin-v2")
```

**`policy attributes` command:** Operators can run `policy attributes` to view
all registered attributes. Output format:

```text
Core Attributes:
  character.id          ULID     (core)
  character.level       number   (core)
  character.faction     string   (core)
  location.restricted   boolean  (core)

Plugin Attributes:
  reputation.score      number   (reputation-plugin-v2)
  reputation.tier       string   (reputation-plugin-v2)
  guilds.primary        string   (guild-system-v1)
```

Optional filtering: `policy attributes --namespace reputation` shows only
attributes in the `reputation` namespace.

### Schema Validation and Evolution

#### Registration-Time Validation

Attribute providers MUST register schemas during plugin initialization. The
schema registry enforces the following validation rules at registration time:

| Validation Rule            | Behavior                                           |
| -------------------------- | -------------------------------------------------- |
| Missing namespace          | Reject registration, return error to plugin loader |
| Empty namespace            | Reject registration, return error to plugin loader |
| Duplicate namespace        | Reject registration, return error to plugin loader |
| Empty attribute definition | Reject registration, return error to plugin loader |
| Invalid type (not in enum) | Reject registration, return error to plugin loader |
| Duplicate attribute keys   | Reject registration, return error to plugin loader |

**Registration failure behavior:** If a plugin fails to register a valid schema,
the plugin loader MUST fail plugin initialization and log the error. The plugin
MUST NOT be loaded. This prevents runtime surprises where attributes are
returned without schema definitions.

```go
// Example registration validation
if err := schema.RegisterNamespace("reputation", attrs, "reputation-plugin-v2"); err != nil {
    return oops.Code("SCHEMA_REGISTRATION_FAILED").
        With("plugin", pluginName).
        With("namespace", "reputation").
        Wrap(err)
}
```

#### Runtime Behavior for Unregistered Attributes

If an attribute provider returns attributes at runtime that were not registered
in its schema, the engine MUST handle this gracefully:

1. **Log a warning** for each unregistered attribute key encountered (rate-limited
   to 1 log per minute per namespace+key combination to prevent spam)
2. **Include the attribute** in the evaluation context anyway (fail-open for
   attribute availability to avoid breaking policies)
3. **Emit a metric** `abac_unregistered_attributes_total{namespace="X",key="Y"}`
   to track schema drift

**Security requirement (S6):** Plugin attribute providers MUST have their
return values validated at runtime against the registered namespace. Providers
returning attribute keys outside their registered namespace MUST have those
keys rejected, with error logging and metric emission. Tests MUST verify that
namespace enforcement occurs at runtime, not just during registration.

This approach balances safety (logging the schema violation) with availability
(policies continue working even with schema drift).

```go
// Example runtime handling
if !schema.IsRegistered(namespace, key) {
    if rateLimiter.Allow(namespace, key) {
        slog.Warn("attribute returned without registered schema",
            "namespace", namespace,
            "key", key,
            "provider", provider.Name())
    }
    metrics.UnregisteredAttributes.WithLabelValues(namespace, key).Inc()
}
// Continue evaluation with the attribute
```

#### Schema Evolution on Plugin Reload

When a plugin is reloaded, the engine MUST compare the new schema against the
previous schema version and log warnings for breaking changes:

| Schema Change          | Behavior                                                                        |
| ---------------------- | ------------------------------------------------------------------------------- |
| Attribute added        | Info log, no action required                                                    |
| Attribute type changed | Warn log, existing policies may break                                           |
| Attribute removed      | Warn log, scan policies for references                                          |
| Namespace removed      | Error log, scan policies for references, reject reload if policies reference it |

**Schema evolution example:**

```go
// Plugin reloaded: reputation-plugin-v2 → reputation-plugin-v3
oldSchema := schema.GetNamespace("reputation") // v2
newSchema := incomingRegistration              // v3

// Detect removals
for _, oldAttr := range oldSchema.Attributes {
    if !newSchema.HasAttribute(oldAttr.Key) {
        affectedPolicies := policyIndex.FindPoliciesReferencing("reputation", oldAttr.Key)
        if len(affectedPolicies) > 0 {
            slog.Warn("attribute removed from schema, policies may be affected",
                "namespace", "reputation",
                "key", oldAttr.Key,
                "version_old", oldSchema.Version,
                "version_new", newSchema.Version,
                "affected_policies", len(affectedPolicies))
        }
    }
}

// Detect type changes
for _, newAttr := range newSchema.Attributes {
    if oldAttr, exists := oldSchema.GetAttribute(newAttr.Key); exists {
        if oldAttr.Type != newAttr.Type {
            slog.Warn("attribute type changed",
                "namespace", "reputation",
                "key", newAttr.Key,
                "type_old", oldAttr.Type,
                "type_new", newAttr.Type)
        }
    }
}
```

**Policy reference scanning:** The engine SHOULD maintain a reverse index
mapping `(namespace, attribute key) → []PolicyID` to efficiently find policies
affected by schema changes. This index is updated when policies are compiled and
removed when policies are deleted.

**Namespace removal:** If a plugin unregisters its namespace entirely (plugin
unloaded or replaced), the engine MUST scan all active policies for references
to that namespace. If any policies reference the removed namespace, the reload
SHOULD be rejected unless the operator provides a `--force` flag. Forced removal
logs an error and leaves the policies in a broken state (compile errors on next
evaluation).

### Error Handling

**Core provider errors:** The engine returns the error alongside a default-deny
decision. Direct callers can distinguish "denied by policy" from "system
failure":

```go
return Decision{Allowed: false, Effect: EffectDefaultDeny}, err
```

Callers SHOULD log the error and treat the response as denied (fail-closed).
The audit log records the `error_message` field for these cases.

**Plugin provider errors:** The engine logs an error via slog and continues
evaluation with the remaining providers. Missing plugin attributes cause
conditions referencing them to evaluate to `false` (fail-safe). The audit log
records plugin provider errors in a `provider_errors` JSONB field to aid
debugging "why was I denied?" investigations. The field is an array of error
objects with schema: `[{"namespace": "string", "error": "string", "timestamp":
"RFC3339", "duration_us": "int"}]`. For example:
`[{"namespace": "reputation", "error": "connection refused", "timestamp":
"2026-02-06T12:00:00Z", "duration_us": 1500}]`. Logging is rate-limited to 1
error per minute per `(namespace, error_hash)` tuple to control spam while
preserving visibility of distinct failure modes. If a provider has two
different error types (e.g., DB timeout and network error), both are logged
independently. The rate limiter uses a bounded LRU cache (capacity: 256 entries,
per-engine-instance, in-memory) keyed by namespace and error message hash.

```text
slog.Error("plugin attribute provider failed",
    "namespace", provider.Namespace(), "error", err)
```

**Provider health monitoring:** In addition to per-error rate-limited
logging, the engine **SHOULD** export Prometheus counter metrics per
provider:
`abac_provider_errors_total{namespace="reputation",error_type="timeout"}`.
This provides aggregate visibility into chronic provider failures that
individual log entries cannot. Implementation **SHOULD** include a circuit
breaker per provider with the following parameters:

| Parameter          | Default | Description                              |
| ------------------ | ------- | ---------------------------------------- |
| Failure threshold  | 10      | Consecutive errors to open the circuit   |
| Open duration      | 30s     | Time to skip provider while circuit open |
| Half-open attempts | 1       | Single probe request to test recovery    |

When the circuit opens, the engine logs at WARN level with the provider
namespace and failure count. During the open period, the provider is
skipped (attributes missing, conditions fail-safe). **Circuit breaker
skip behavior:** Skipping a circuit-opened provider is an immediate
check with no I/O and does **NOT** consume evaluation time budget.
The per-provider timeout fair-share calculation excludes circuit-opened
providers from the remaining provider count. All active providers use
the same fair-share formula: `remaining_budget / remaining_providers`.
For example, with a 100ms total budget and 5 registered providers where
2 have open circuits, the first active provider receives 33ms because
there are 3 remaining active providers (100ms / 3), not 20ms (100ms / 5).
Each subsequent active provider receives its fair share using the same
formula with updated remaining budget and provider counts. This ensures
that functioning providers receive equitable time allocations and are
not penalized by systematic failures in other providers.

After the open duration, a single "half-open" probe request tests whether
the provider has recovered. On success, the circuit closes (INFO log); on
failure, it re-opens for another cycle.

**Error classification:** All errors result in fail-closed (deny) behavior.
No errors are retryable within a single `Evaluate()` call.

| Error Type                | Fail Mode              | Caller Action                                             |
| ------------------------- | ---------------------- | --------------------------------------------------------- |
| Core provider failure     | Deny + return error    | Log and deny; callers inspect `err`                       |
| Plugin provider failure   | Deny (conditions fail) | Automatic — plugin attrs missing                          |
| Policy compilation error  | Deny + return error    | Should not occur (compiled at store time)                 |
| Corrupted compiled policy | Deny + skip policy     | Log CRITICAL, disable in cache, continue eval (see below) |
| Session invalid           | Deny + return error    | Log, deny, session expired/missing/no character           |
| Session store error       | Deny + return error    | Log, deny, infrastructure failure                         |
| Context cancelled         | Deny + return error    | Request was cancelled upstream                            |

**Corrupted compiled policy** means the `CompiledPolicy` JSONB fails to
unmarshal into the AST struct, or the unmarshaled AST violates structural
invariants (e.g., a binary operator node missing a required operand). This
should not occur in normal operation because policies are compiled at
`PolicyStore.Create()` time. Corruption indicates data-level issues
(direct DB edits, storage failures). When detected, the engine:

1. Logs at CRITICAL level with the policy name and corruption details
2. Sets `enabled = false` on the policy row in the database (persisted,
   not just in-memory) to prevent the corrupted policy from being
   reloaded on subsequent cache refreshes
3. Continues evaluating remaining policies

**Recovery:** The `policy repair <name>` admin command (part of the
policy management CLI) re-compiles the policy from its `dsl_text` column,
overwrites the `compiled_ast` JSONB, and re-enables the policy. If
`dsl_text` is also corrupted, the operator must `policy edit <name>` to
provide corrected DSL text. After repairing or disabling the corrupted
policy, use `policy clear-degraded-mode` to restore normal evaluation.
The `policy test --suite` validation workflow **SHOULD** include a
corruption detection pass that unmarshals every `compiled_ast` and
verifies structural invariants.

**Security note (Degraded Mode):** If a corrupted policy has effect
`forbid` or `deny`, silently skipping it creates a security gap. When such
a policy is detected as corrupted, the engine **MUST** enter **degraded
mode** by setting a global flag (`abac_degraded_mode` boolean) that
persists until administratively cleared. In degraded mode:

- All access evaluation requests receive `EffectDefaultDeny` without
  evaluating any policies (fail-closed for all subjects)
- The CRITICAL log entry **MUST** include the policy name, effect, and
  degraded mode activation message
- All deny decisions during degraded mode **MUST** be audited with the reason
  `degraded_mode` to ensure forensic visibility
- A Prometheus gauge `abac_degraded_mode` (0=normal, 1=degraded) **MUST**
  be exposed for alerting
- Administrators **MUST** use CLI access or direct database access to
  resolve the corruption; the policy engine is unavailable to all subjects
  during degraded mode

**Recovery:** The `policy clear-degraded-mode` admin command clears the
degraded mode flag and allows normal evaluation to resume. Operators
**SHOULD** configure alerting on CRITICAL-level ABAC log entries and the
`abac_degraded_mode` gauge. Policies with effect `permit` do **not**
trigger degraded mode when corrupted, as skipping a permit policy defaults
to deny, which is fail-safe.

**Future work:** The current seed policy `seed:admin-full-access` grants
unrestricted access. A future iteration **SHOULD** split admin privileges
into scoped roles (e.g., `admin:policy`, `admin:world`, `admin:player`)
rather than a blanket admin role, reducing risk from compromised credentials.

Callers of `AccessPolicyEngine.Evaluate()` can distinguish "denied by
policy" (`err == nil, Decision.Effect == EffectDeny`) from "system failure"
(`err != nil, Decision.Effect == EffectDefaultDeny`). Both result in access
denied, but callers MAY handle system failures differently (e.g., retry,
alert).

## Evaluation Algorithm

```text
Evaluate(ctx, AccessRequest{Subject, Action, Resource})
│
├─ 1. System bypass
│    subject == "system" → return Decision{Allowed: true, Effect: SystemBypass}
│
├─ 2. Session resolution
│    subject starts with "session:" → resolve to character ID
│    (subject string is mutated to "character:<id>" before step 3)
│
├─ 3. Resolve attributes (eager)
│    ├─ Parse subject type/ID from subject string
│    ├─ Parse resource type/ID from resource string
│    ├─ Call all registered AttributeProviders
│    └─ Assemble AttributeBags{Subject, Resource, Action, Environment}
│
├─ 4. Find applicable policies
│    ├─ Load from in-memory cache
│    └─ Filter: policy target matches request
│         ├─ principal: "principal is T" matches when parsed subject
│         │   prefix equals T (e.g., "character:", "plugin:").
│         │   Valid types: character, plugin. "session" is never
│         │   valid (resolved in step 2). Bare "principal"
│         │   matches all subject types.
│         ├─ action: "action in [...]" matches when request action
│         │   is in the list. Bare "action" matches all actions.
│         └─ resource: "resource is T" matches when parsed resource
│             prefix equals T. "resource == X" matches exact string.
│             Bare "resource" matches all resource types.
│
├─ 5. Evaluate conditions
│    For each candidate policy:
│    ├─ Evaluate DSL conditions against AttributeBags
│    ├─ If all conditions true → policy is "satisfied"
│    └─ If any condition false or attribute missing → policy does not apply
│
├─ 6. Combine decisions (deny-overrides)
│    ├─ Any satisfied forbid → Decision{Allowed: false, Effect: Deny}
│    ├─ Any satisfied permit → Decision{Allowed: true, Effect: Allow}
│    └─ No policies satisfied → Decision{Allowed: false, Effect: DefaultDeny}
│
└─ 7. Audit
     ├─ Log system bypasses in ALL modes (off, denials_only, all)
     ├─ Log denials (forbid + default deny) in denials_only and all modes
     ├─ Log allows only in all mode
     └─ Include: decision, matched policies, attribute snapshot
```

### Key Behaviors

- **Missing attributes:** If a condition references an attribute that does not
  exist, the condition evaluates to `false`. A missing attribute can never grant
  access (fail-safe).
- **No short-circuit (default):** The engine evaluates all candidate policies
  so the `Decision` records all matches. This powers `policy test` debugging.
  Implementations MAY optimize by short-circuiting after the first satisfied
  `forbid` policy when audit mode is `denials_only` and no `policy test`
  command is active, provided the triggering forbid is still recorded in
  `Decision.Policies`. Full evaluation MUST be used when `policy test` is
  active or audit mode is `all`.
- **Cache invalidation:** The engine subscribes to PostgreSQL LISTEN/NOTIFY on
  the `policy_changed` channel. The Go policy store calls `pg_notify` after any
  Create/Update/Delete operation. On notification, the engine reloads all enabled
  policies before the next evaluation. On reconnect after a connection drop, the
  engine MUST perform a full policy reload to account for any missed
  notifications.
- **Concurrency:** Policy evaluations use a snapshot of the in-memory policy
  cache at the start of `Evaluate()`. If a policy changes during evaluation, the
  decision reflects the pre-change policy. This is acceptable for MUSH workloads
  where the stale window is <100ms.

### Performance Targets

| Metric                              | Target | Notes                             |
| ----------------------------------- | ------ | --------------------------------- |
| `Evaluate()` p99 latency (cached)   | <5ms   | Policy and attribute cache warm   |
| `Evaluate()` p99 latency (cold)     | <10ms  | Cache miss, DB roundtrip required |
| Attribute resolution (cold)         | <2ms   | All providers combined            |
| Attribute resolution (warm, cached) | <100μs | Map lookup only                   |
| DSL condition evaluation            | <1ms   | Per policy                        |
| Cache reload                        | <50ms  | Full policy set reload on NOTIFY  |

**Benchmark scenario:** Targets assume 50 active policies (25 permit, 25
forbid), average condition complexity of 3 operators per policy, 10 attributes
per entity (subject + resource). "Cached" means policy cache and attribute
cache are warm (majority of requests). "Cold" means cache miss requiring
database roundtrip (recursive CTE for container hierarchy, policy store fetch).
Implementation MUST include both `BenchmarkEvaluate_ColdCache` and
`BenchmarkEvaluate_WarmCache` tests.

**Worst-case bounds:**

| Scenario                             | Bound  | Handling                                  |
| ------------------------------------ | ------ | ----------------------------------------- |
| All 50 policies match (pathological) | <10ms  | Linear scan is acceptable at this scale   |
| Provider timeout                     | 100ms  | Context deadline; return deny + error     |
| Cache miss storm (post-NOTIFY flood) | <100ms | Lock during reload; stale reads tolerable |
| Plugin provider slow                 | 50ms   | Per-provider context deadline             |
| 32-level nested if-then-else         | <5ms   | Recursive evaluator with depth limit      |
| 20-level containment CTE             | <10ms  | Recursive SQL with depth limit            |
| Provider starvation (80ms + 80ms)    | 100ms  | Second provider gets cancelled context    |

Implementation MUST include benchmark tests for these pathological cases.
The 32-level nesting and 20-level containment scenarios MUST be included in
`BenchmarkEvaluate_WorstCase` as acceptance criteria. Provider starvation
(one slow provider consuming most of the 100ms budget) MUST be tested to
verify subsequent providers receive cancelled contexts and return promptly.

The `Evaluate()` context MUST carry a 100ms deadline. If attribute resolution
exceeds this, the engine returns `EffectDefaultDeny` with a timeout error.

**Provider resolution is sequential.** Core providers are called in registration
order, then plugin providers. This is a deliberate choice: sequential resolution
enables deterministic merge semantics (last-registered provider wins on key
collisions), simpler error attribution (the failing provider is immediately
identifiable), and straightforward debugging (provider order in audit logs
matches registration order). At ~200 concurrent users with providers each
completing in <1ms, parallel resolution would save negligible latency while
complicating the merge strategy and error handling. If profiling shows provider
resolution as a bottleneck (unlikely at this scale), parallel resolution MAY
be introduced — this would require changing the merge semantics from
"last-registered wins" to a priority-based merge, since goroutine completion
order is non-deterministic.

The 100ms deadline is the total budget for all providers combined.
To prevent priority inversion (where early providers starve later ones),
the engine **MUST** enforce per-provider timeouts within the total budget.
Each provider receives a **fair-share timeout** calculated dynamically as
`max(remaining_budget / remaining_providers, 5ms)`, recalculated after each
provider completes. For example, with 5 providers and a 100ms total budget,
the first provider receives 20ms (100ms / 5). If it completes in 5ms, the
second provider receives 23.75ms ((100-5)ms / 4), and so on. This ensures
that unused time from fast providers is redistributed to later providers.
The 5ms floor prevents sub-millisecond timeouts if early providers consume
most of the budget.

**Tradeoff:** Fair-share prevents priority inversion while maximizing
budget utilization. Fast early providers create larger budgets for later
providers, reducing timeout-induced failures. However, it requires dynamic
recalculation after each provider and makes per-provider budgets less
predictable for monitoring. This is acceptable because:

1. Core providers (registered first during server startup) no longer
   squeeze plugin providers that register later
2. Fast providers benefit the entire evaluation chain
3. The 100ms total budget remains a hard backstop regardless of distribution

**Registration order dependency:** While fair-share prevents priority
inversion, registration order still matters for slow providers. If a slow
provider registers early, it will consume budget before faster providers
can run. Plugin authors **SHOULD** register attribute providers at server
startup (in plugin `Init()`) rather than lazily, to ensure predictable
ordering. Core providers **SHOULD** be lightweight and register first to
avoid delaying plugin providers.

**Example calculation:**

- Total budget: 100ms, 4 providers (core, plugin A, plugin B, plugin C)
- Provider 1 timeout: `100ms / 4 = 25ms`, actual: 5ms
- Provider 2 timeout: `(100-5)ms / 3 = 31.67ms`, actual: 10ms
- Provider 3 timeout: `(100-5-10)ms / 2 = 42.5ms`, actual: 25ms (timeout)
- Provider 4 timeout: `(100-5-10-25)ms / 1 = 60ms`, actual: 15ms
- Total evaluation time: 5+10+25+15 = 55ms (providers 1-2 finished early, giving provider 3 a larger 42.5ms budget which it fully consumed before timing out)

If the parent context is cancelled during evaluation (e.g., client
disconnect), all remaining providers receive the cancelled context
immediately and the evaluation terminates with `EffectDefaultDeny`.
Fair-share timeout allocation applies only when the parent context is
active.

The engine wraps each provider call with
`context.WithTimeout(ctx, perProviderTimeout)` before calling
`ResolveSubject` or `ResolveResource`. If a provider exceeds its
per-provider timeout, it is cancelled individually and treated as a
plugin provider failure (logged, attributes missing, conditions
fail-safe). The overall 100ms deadline on `Evaluate()` still applies as
a hard backstop. A slow or misbehaving plugin cannot block the entire
evaluation pipeline because both the per-provider and overall deadlines
expire regardless of what the current provider is doing.

#### Circuit Breaker Summary

The ABAC engine uses three distinct circuit breaker designs, each tuned for
different failure modes:

| Component        | Trigger                                                 | Window | Behavior              | Metric                                               | Rationale                                                                                                                                                               |
| ---------------- | ------------------------------------------------------- | ------ | --------------------- | ---------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| General provider | >80% budget utilization in >50% of calls (min 10 calls) | 60s    | Skip provider for 60s | `abac_provider_circuit_breaker_trips_total`          | Higher threshold because some transient slowness is expected; detects systematic performance degradation                                                                |
| PropertyProvider | 5 timeout errors                                        | 60s    | Skip queries for 60s  | `abac_property_provider_circuit_breaker_trips_total` | More tolerant of transient DB load (see [Decision #83](../decisions/epic7/phase-7.3/083-circuit-breaker-threshold-increase.md)) while still detecting systematic issues |

**General provider circuit breaker:** Providers that stay just
under the timeout but consistently consume >80% of their allocated budget
cause cumulative performance degradation. The engine MUST track per-provider
lifetime budget utilization. If a provider exceeds 80% of its allocated
budget in more than 50% of calls over a 60-second rolling window (minimum
10 calls), the engine MUST trip a circuit breaker for that provider. Once
tripped, the provider is skipped for 60 seconds and returns empty attributes
(logged once at ERROR level: `"Provider {name} circuit breaker tripped:
exceeded 80% budget in {N}% of calls"`). A Prometheus counter
`abac_provider_circuit_breaker_trips_total{provider="name"}` MUST be
incremented. After the 60-second cooldown, the provider is re-enabled with
reset counters. The engine SHOULD expose a provider metrics endpoint
(`/debug/abac/providers`) showing per-provider call counts, average latency,
timeout rate, and circuit breaker status for operational visibility.

**Operational limits:**

| Limit                        | Value | Rationale                       |
| ---------------------------- | ----- | ------------------------------- |
| Maximum registered providers | 20    | 8 core + 12 plugins (headroom)  |
| Maximum active policies      | 500   | Linear scan acceptable at scale |
| Maximum condition nesting    | 32    | Prevents stack overflow         |
| Provider timeout (total)     | 100ms | Hard deadline on `Evaluate()`   |
| Provider timeout (per)       | fair  | `remaining_budget / remaining`  |

**Measurement strategy:**

- Export a Prometheus histogram metric for `Evaluate()` latency
  (e.g., `abac_evaluate_duration_seconds`)
- Add `BenchmarkEvaluate_*` tests with targets as failure thresholds (CI
  fails if benchmarks regress >10% from baseline). Specifically: CI MUST fail
  if any benchmark exceeds 110% of its documented target value (e.g., cold
  `Evaluate()` p99 must stay under 5.5ms, warm under 3.3ms). Benchmark
  failures MUST be treated as build failures - PRs cannot merge with
  performance regressions exceeding the 10% headroom
- Staging monitoring alerts on p99 > 10ms (2x target)
- Implementation SHOULD add `slog.Debug()` timers in `engine.Evaluate()` for
  attribute resolution, policy filtering, condition evaluation, and audit
  logging to enable performance profiling during development
- Monitoring SHOULD export per-provider latency metrics (not just aggregate
  `Evaluate()` latency) to identify slow providers independently
- Monitoring SHOULD export `policy_cache_last_update` gauge (Unix timestamp) to
  verify the LISTEN/NOTIFY connection is alive and detect cache staleness
- Monitoring SHOULD export per-policy evaluation counts
  (`abac_policy_evaluations_total{name, effect}`) to identify hot policies
- Operators SHOULD configure alerting when
  `time.Now() - policy_cache_last_update > cache_staleness_threshold` to detect
  prolonged LISTEN/NOTIFY disconnections

**`Decision.Policies` allocation note:** The `Policies []PolicyMatch` slice is
populated for every `Evaluate()` call to support `policy test` debugging. At 50
policies per evaluation and 120 evaluations/sec, this produces ~6,000
`PolicyMatch` allocations/sec. If benchmarking shows allocation pressure,
consider lazy population: only populate `Decision.Policies` when audit mode is
`all` or the caller explicitly requests it (e.g., via a context flag set by
`policy test`). For `denials_only` mode, `Decision.PolicyID` and
`Decision.Reason` are sufficient.

### Attribute Caching

The `AttributeResolver` SHOULD implement per-request caching from the start.
When a single user action triggers multiple `Evaluate()` calls (e.g., check
command permission, then check location entry, then check property read), the
same subject attributes are resolved repeatedly.

**Caching strategy:**

- **Scope:** Per `context.Context`. A shared `AttributeCache` is attached to
  the context by the request handler. Multiple `Evaluate()` calls within the
  same request context share the cache.
- **Key:** `{entityType, entityID}` tuple (e.g., `{"character", "01ABC"}`).
- **Invalidation:** Cache is garbage-collected when the context is cancelled
  (end of request). No cross-request caching for MVP.
- **Fault tolerance:** The cache stores the merged attribute bag per entity. A
  plugin provider failure during a first `Evaluate()` call produces a partial
  bag (missing that plugin's attributes). Subsequent `Evaluate()` calls in the
  same request reuse the cached (partial) bag — the failing provider is NOT
  retried. This is correct because provider failures are not transient within a
  single request, and the fail-safe missing-attribute semantics ensure conditions
  referencing the absent plugin attributes evaluate to `false`.

**Request duration guidance:** The per-request cache assumes request
processing completes within milliseconds. Request handlers SHOULD complete
within 1 second. For long-running operations (batch processing, multi-step
commands exceeding 1s), callers SHOULD create a fresh context with a new
`AttributeCache` rather than reusing a stale cache. The cache is designed
for single-user command execution latencies (~10ms), not batch workloads.

```go
// AttributeCache provides per-request attribute caching with LRU eviction.
// Attach to context via WithAttributeCache(ctx) at the request boundary.
type AttributeCache struct {
    mu           sync.RWMutex
    items        map[cacheKey]map[string]any
    accessOrder  []cacheKey  // LRU tracking
    maxEntries   int         // Default: 100
}

type cacheKey struct {
    entityType string // "character", "location", etc.
    entityID   string // "01ABC"
}

// WithAttributeCache attaches a new cache to the context.
// Call this at the request boundary (e.g., command handler entry point).
func WithAttributeCache(ctx context.Context) context.Context

// GetAttributeCache retrieves the cache from context, or nil if none attached.
func GetAttributeCache(ctx context.Context) *AttributeCache
```

**Cache size limit:** The `maxEntries` field (default: 100) limits the number
of cached entity attribute bags. When the cache reaches capacity and a new
entity is added, the least-recently-used entry is evicted. The `accessOrder`
slice tracks access timestamps for LRU eviction. Typical commands access 2-10
entities (actor, target, location, a few objects), so 100 entries provides
substantial headroom. Commands that iterate over large entity sets (e.g., admin
batch operations) MAY exceed this limit, triggering LRU eviction — this is
acceptable since the cache is per-request and batch operations are rare.

The cache assumes **read-only world state** during request processing. If a
command modifies character location, subsequent authorization checks in the
same request use the pre-modification snapshot. This is consistent with the
eager resolution model — attributes represent a per-request snapshot, though
not necessarily a consistent cross-provider snapshot (see Known Limitations).

**Multi-step commands:** For commands that modify an entity AND then check
access to the modified state (e.g., a builder command that moves a character
and checks their access to the new location), callers SHOULD call
`WithAttributeCache(ctx)` again to create a fresh cache for the post-
modification checks. This pattern is documented here rather than enforced
by the cache itself, since most commands do not modify and re-check.

**Future optimization:** If profiling shows cache misses dominate, consider a
short-TTL cache (100ms) for read-only attributes like character roles. This
requires careful invalidation and is deferred until profiling demonstrates the
need.

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 7.3: Policy Engine & Attribute Providers

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)** | **[Next: Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)**

## Task 13: Attribute provider interface and schema registry

**Spec References:** [01-core-types.md#attribute-providers](../specs/abac/01-core-types.md#attribute-providers) (was lines 567-658), [04-resolution-evaluation.md#attribute-schema-registry](../specs/abac/04-resolution-evaluation.md#attribute-schema-registry) (was lines 1393-1436), [04-resolution-evaluation.md#provider-registration](../specs/abac/04-resolution-evaluation.md#provider-registration) (lines 37-49)

**ADR References:** [082-core-first-provider-registration-order.md](../specs/decisions/epic7/phase-7.3/082-core-first-provider-registration-order.md)

> **Design note:** `AttributeSchema` and `AttrType` are defined in `internal/access/policy/types/` (Task 5 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md))) to prevent circular imports. The `policy` package (compiler) needs `AttributeSchema`, and the `attribute` package (resolver) needs `types.AccessRequest` and `types.AttributeBags`. Both import from `types` package.

**Acceptance Criteria:**

- [ ] `AttributeProvider` interface: `Namespace()`, `ResolveSubject()`, `ResolveResource()`, `LockTokens()`
- [ ] `EnvironmentProvider` interface: `Namespace()`, `Resolve()`
- [ ] `AttributeSchema` supports: `Register()`, `IsRegistered()` (uses type definition from Task 5 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)))
- [ ] Engine enforces core-first provider registration order (ADR #82)
- [ ] Duplicate namespace registration → error
- [ ] Empty namespace → error
- [ ] Duplicate attribute key within namespace → error
- [ ] Invalid attribute type → error
- [ ] Providers MUST return all numeric attributes as `float64` (per spec [01-core-types.md#core-attribute-schema](../specs/abac/01-core-types.md#core-attribute-schema) (was lines 659-785))
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/attribute/provider.go`
- Create: `internal/access/policy/attribute/schema.go`
- Test: `internal/access/policy/attribute/schema_test.go`

**Step 1: Write failing tests for schema registration**

Test cases: register namespace, duplicate namespace → error, empty namespace → error, duplicate attribute key → error, invalid type → error, `IsRegistered()` returns correct results.

**Step 2: Implement**

```go
// internal/access/policy/attribute/provider.go
package attribute

import "context"

// AttributeProvider resolves attributes for a specific namespace.
type AttributeProvider interface {
    Namespace() string
    ResolveSubject(ctx context.Context, subjectType, subjectID string) (map[string]any, error)
    ResolveResource(ctx context.Context, resourceType, resourceID string) (map[string]any, error)
    LockTokens() []LockTokenDef
}

// EnvironmentProvider resolves environment attributes (no entity context).
type EnvironmentProvider interface {
    Namespace() string
    Resolve(ctx context.Context) (map[string]any, error)
}

// LockTokenType determines how the lock compiler generates DSL conditions.
type LockTokenType string

const (
    LockTokenEquality   LockTokenType = "equality"   // e.g., faction:rebels → principal.faction == "rebels"
    LockTokenMembership LockTokenType = "membership"  // e.g., flag:storyteller → "storyteller" in principal.flags
    LockTokenNumeric    LockTokenType = "numeric"     // e.g., level>5 → principal.level > 5
)

// LockTokenDef defines a lock token contributed by a provider.
type LockTokenDef struct {
    Name          string
    Description   string
    AttributePath string
    Type          LockTokenType
}
```

```go
// internal/access/policy/attribute/schema.go
package attribute

import (
    "github.com/holomush/holomush/internal/access/policy/types"
    "github.com/samber/oops"
)

// SchemaRegistry wraps types.AttributeSchema with registration logic.
// AttributeSchema and AttrType are defined in internal/access/policy/types/
// to prevent circular imports. SchemaRegistry provides the actual implementation.
type SchemaRegistry struct {
    schema *types.AttributeSchema
}

// Register adds a namespace schema. Returns error if namespace is empty,
// already registered, or contains duplicate keys.
func (r *SchemaRegistry) Register(namespace string, schema *types.NamespaceSchema) error {
    if namespace == "" {
        return oops.Code("INVALID_NAMESPACE").Errorf("namespace cannot be empty")
    }
    // Check for existing namespace
    // Check for duplicate attribute keys
    // Add to schema
    return nil
}

// IsRegistered checks if a namespace and attribute key are registered.
func (r *SchemaRegistry) IsRegistered(namespace, key string) bool {
    // Implementation
    return false
}
```

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/attribute/
git commit -m "feat(access): add AttributeProvider interface and schema registry"
```

---

### Task 14: Attribute resolver with per-request caching

**Spec References:** [04-resolution-evaluation.md#resolution-flow](../specs/abac/04-resolution-evaluation.md#resolution-flow) (was lines 1355-1381), [04-resolution-evaluation.md#performance-targets](../specs/abac/04-resolution-evaluation.md#performance-targets) (was lines 1769-1945), [04-resolution-evaluation.md#attribute-caching](../specs/abac/04-resolution-evaluation.md#attribute-caching) (was lines 1946-2025), ADR 0012 (Eager attribute resolution)

> **Note (Bug I10):** [04-resolution-evaluation.md#attribute-caching](../specs/abac/04-resolution-evaluation.md#attribute-caching) (was lines 1976-2006) explicitly specifies LRU eviction with `maxEntries` default of 100. Reviewer concern about missing LRU/size spec was incorrect — spec clearly defines both semantics and default value.

**Acceptance Criteria:**

- [ ] Single provider → correct attribute bags returned
- [ ] Multiple providers → merge semantics (last-registered wins for scalars, concatenate for lists)
- [ ] Multi-provider list concatenation: two providers contributing to same list attribute are merged (e.g., Provider A: factions:[rebels], Provider B: factions:[traders] → merged: factions:[rebels,traders]). Verify order determinism.
- [ ] Core-to-plugin key collision → reject plugin registration at startup
- [ ] Plugin-to-plugin key collision → warn, last registered wins
- [ ] Provider error → skip provider, continue, record `ProviderError`
- [ ] Per-request cache → second `Resolve()` with same entity reuses cached result
- [ ] Fair-share budget: `max(remainingBudget / remainingProviders, 5ms)`
- [ ] Provider exceeding fair-share timeout → cancelled
- [ ] **Re-entrance guard:** Provider calling `Evaluate()` during attribute resolution → panic with descriptive error. Implementation: store `inResolution` flag in context at resolver entry, check flag before calling providers, panic if flag is true. Guards against deadlock (Engine → Resolver → Provider → Engine). See [ADR #31](../specs/decisions/epic7/phase-7.3/031-provider-re-entrance-prohibition.md) (Provider Re-Entrance Prohibition)
- [ ] **Panic recovery:** Plugin provider panics → recovered with error logging, evaluation continues, error recorded in decision
- [ ] **Security (S6):** Runtime namespace validation — provider return keys MUST match registered namespace, invalid keys rejected with error logging and metric emission
- [ ] **Panic recovery test case:** Provider `ResolveSubject()` panics → evaluator catches panic via `defer func() { if r := recover()... }`, logs error, continues with next provider
- [ ] `AttributeCache` is LRU with max 100 entries, attached to context (per [04-resolution-evaluation.md#attribute-caching](../specs/abac/04-resolution-evaluation.md#attribute-caching) (was lines 1976-2006))
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/attribute/resolver.go`
- Create: `internal/access/policy/attribute/cache.go`
- Test: `internal/access/policy/attribute/resolver_test.go`

**Step 1: Write failing tests**

- Resolve with single provider → correct bags
- Resolve with multiple providers → merge semantics (last-registered wins for scalars, concatenate for lists)
- Core-to-plugin key collision → reject plugin registration at startup
- Plugin-to-plugin key collision → warn, last registered wins
- Provider error → skip provider, continue, record `ProviderError`
- Panic recovery → plugin provider panics, recovered with error logging, evaluation continues
- Per-request cache → second `Resolve()` with same entity reuses cached result
- Timeout enforcement → provider exceeding fair-share timeout is cancelled
- Fair-share budget: 4 providers with 100ms total → ~25ms each initially
- Re-entrance detection → provider calling `Evaluate()` on same context → panic

**Step 2: Implement resolver**

```go
// internal/access/policy/attribute/resolver.go
package attribute

import (
    "context"
    "time"

    "github.com/holomush/holomush/internal/access/policy"
    "github.com/holomush/holomush/internal/access/policy/types"
)

// ProviderError records a provider failure during resolution.
type ProviderError struct {
    Namespace  string
    Error      string
    DurationUS int
    Panicked   bool // true if provider panicked (recovered)
}

// AttributeResolver orchestrates attribute providers with caching and timeouts.
type AttributeResolver struct {
    providers    []AttributeProvider
    envProviders []EnvironmentProvider
    schema       *types.AttributeSchema
    totalBudget  time.Duration // default 100ms
    logger       Logger         // for panic error logging
}

func NewAttributeResolver(budget time.Duration, logger Logger) *AttributeResolver
func (r *AttributeResolver) RegisterProvider(p AttributeProvider) error
func (r *AttributeResolver) RegisterEnvironmentProvider(p EnvironmentProvider)
func (r *AttributeResolver) Resolve(ctx context.Context, req types.AccessRequest) (*types.AttributeBags, []ProviderError, error)

// Example panic recovery in Resolve():
// for each provider:
//   func(provider AttributeProvider) {
//       defer func() {
//           if r := recover(); r != nil {
//               r.logger.Error("provider panicked",
//                   slog.String("namespace", provider.Namespace()),
//                   slog.Any("panic", r))
//               providerErrors = append(providerErrors, ProviderError{
//                   Namespace: provider.Namespace(),
//                   Error:     fmt.Sprintf("panic: %v", r),
//                   Panicked:  true,
//               })
//           }
//       }()
//       // Call provider methods (ResolveSubject, ResolveResource, etc.)
//   }(provider)
```

Key: fair-share timeout is `max(remainingBudget / remainingProviders, 5ms)`.

**Re-entrance guard implementation:**

1. Define context key for resolution flag: `type resolutionKey struct{}`
2. At resolver entry (`Resolve()` method start), check context for `resolutionKey`
3. If flag is true, panic with descriptive error: `"provider re-entrance detected: Evaluate() called during attribute resolution (deadlock prevention)"`
4. Set flag in context before calling providers: `ctx = context.WithValue(ctx, resolutionKey{}, true)`
5. Pass flagged context to all provider calls (`ResolveSubject`, `ResolveResource`, `Resolve`)
6. Providers attempting to call `Evaluate()` with flagged context will trigger panic in step 2 of their own Evaluate() call (when resolver is entered again)

See [ADR #31](../specs/decisions/epic7/phase-7.3/031-provider-re-entrance-prohibition.md) (Provider Re-Entrance Prohibition) for rationale.

```go
// internal/access/policy/attribute/cache.go
package attribute

// AttributeCache is a per-request LRU cache for resolved attributes.
type AttributeCache struct {
    entries map[string]map[string]any // key: "subject:character:01ABC" → attrs
    maxSize int                       // default 100
}

type cacheContextKey struct{}

// WithAttributeCache attaches a cache to the context.
func WithAttributeCache(ctx context.Context) context.Context
// FromContext retrieves the cache from context (nil if absent).
func FromContext(ctx context.Context) *AttributeCache
```

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/attribute/
git commit -m "feat(access): add AttributeResolver with fair-share timeouts and per-request caching"
```

---

### Task 15: Core attribute providers (character, location, object)

**Spec References:** [01-core-types.md#core-attribute-schema](../specs/abac/01-core-types.md#core-attribute-schema) (was lines 659-785) — character, location, and object attributes are in the table

**Acceptance Criteria:**

- [ ] CharacterProvider resolves: `type`, `id`, `name`, `role`, `faction`, `level`, `flags`, `location`
- [ ] CharacterProvider only resolves `character` type subjects/resources (returns nil for others)
- [ ] LocationProvider resolves: `type`, `id`, `name`, `faction`, `restricted`
- [ ] LocationProvider only resolves `location` type resources
- [ ] ObjectProvider resolves: `type`, `id`, `name`, `location`, `owner`, `flags`
- [ ] ObjectProvider only resolves `object` type resources
- [ ] Each provider uses mockery-generated mocks for world model repositories
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/attribute/character.go`
- Create: `internal/access/policy/attribute/location.go`
- Create: `internal/access/policy/attribute/object.go`
- Test: `internal/access/policy/attribute/character_test.go`
- Test: `internal/access/policy/attribute/location_test.go`
- Test: `internal/access/policy/attribute/object_test.go`

**Step 1: Write failing tests for each provider**

CharacterProvider:

- `ResolveSubject("character", "01ABC")` → `{"type": "character", "id": "01ABC", "name": "...", "role": "player", "faction": "rebels", "level": 5, "flags": ["vip"], "location": "01XYZ"}`
- `ResolveSubject("plugin", "...")` → `(nil, nil)` — character provider only resolves characters
- `ResolveResource("character", "01ABC")` → same attrs (for `resource is character` policies)
- `ResolveResource("location", "...")` → `(nil, nil)` — wrong resource type

LocationProvider:

- `ResolveResource("location", "01XYZ")` → `{"type": "location", "id": "01XYZ", "name": "Town Square", "faction": "neutral", "restricted": false}`
- `ResolveSubject(...)` → `(nil, nil)` — resource-only provider

ObjectProvider:

- `ResolveResource("object", "01DEF")` → `{"type": "object", "id": "01DEF", "name": "Sword", "location": "01XYZ", "owner": "01ABC", "flags": ["weapon"]}`

Each provider takes world model repositories as constructor dependencies. Use mockery-generated mocks for tests.

**Step 2: Implement providers**

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/attribute/character.go internal/access/policy/attribute/location.go
git add internal/access/policy/attribute/object.go
git add internal/access/policy/attribute/*_test.go
git commit -m "feat(access): add core attribute providers (character, location, object)"
```

---

### Task 16a: Simple providers (environment, command, stream)

**Spec References:** [01-core-types.md#core-attribute-schema](../specs/abac/01-core-types.md#core-attribute-schema) (was lines 659-785) — environment, command, stream attributes are in the table

**Acceptance Criteria:**

- [ ] EnvironmentProvider implements `EnvironmentProvider` interface; resolves `time`, `hour`, `minute`, `day_of_week`, `maintenance`
- [ ] CommandProvider resolves `type`, `name` for `command` resources only
- [ ] StreamProvider resolves `type`, `name`, `location` for `stream` resources only
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/attribute/environment.go`
- Create: `internal/access/policy/attribute/command.go`
- Create: `internal/access/policy/attribute/stream.go`
- Test files for each

**Step 1: Write failing tests**

EnvironmentProvider (implements `EnvironmentProvider` interface):

- `Resolve()` → `{"time": "2026-02-06T14:30:00Z", "hour": 14, "minute": 30, "day_of_week": "friday", "maintenance": false}`

CommandProvider:

- `ResolveResource("command", "say")` → `{"type": "command", "name": "say"}`
- `ResolveResource("location", "...")` → `(nil, nil)` — wrong type

StreamProvider:

- `ResolveResource("stream", "location:01XYZ")` → `{"type": "stream", "name": "location:01XYZ", "location": "01XYZ"}`

**Step 2: Implement simple providers**

EnvironmentProvider, CommandProvider, StreamProvider are straightforward mappings with no database queries or complex logic.

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/attribute/environment.go internal/access/policy/attribute/command.go
git add internal/access/policy/attribute/stream.go internal/access/policy/attribute/*_test.go
git commit -m "feat(access): add simple providers (environment, command, stream)"
```

> **Known Limitation:** Sequential provider execution allows one slow provider to starve others. This is acceptable for MVP scale (~200 users). Future optimization: parallel provider execution if profiling reveals bottlenecks.

---

### Task 16b: PropertyProvider with recursive CTE

> **Note:** This task depends on Task 4a ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) — PropertyRepository (Task 4a) must exist before PropertyProvider (Task 16b).

**Spec References:** [03-property-model.md#property-attributes](../specs/abac/03-property-model.md#property-attributes) (was lines 1188-1203), ADR 0013 (Properties as first-class entities)

**Acceptance Criteria:**

- [ ] PropertyProvider resolves all property attributes including `parent_location`
- [ ] `parent_location` uses recursive CTE covering all three placement scenarios: direct location (location_id), held by character (held_by_character_id), contained in object (contained_in_object_id)
- [ ] `parent_location` CTE depth limit: 20 levels
- [ ] `parent_location` resolution timeout: 100ms (circuit breaker deferred to Task 34, see Decision #74)
- [ ] Test case: Object at location (location_id non-NULL) → resolves `parent_location`
- [ ] Test case: Object held by character (held_by_character_id non-NULL) → resolves to character's location
- [ ] Test case: Object inside object inside room (contained_in_object_id) → resolves `parent_location` to room
- [ ] Cycle detection → error before depth limit
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/attribute/property.go`
- Test: `internal/access/policy/attribute/property_test.go`

**Step 1: Write failing tests**

PropertyProvider:

- `ResolveResource("property", "01GHI")` → all property attributes including `parent_location`
- Nested containment: object in object in room → resolves `parent_location` to room
- `parent_location` resolution timeout (100ms) → error returned (circuit breaker behavior deferred to Task 34, [Phase 7.7](./2026-02-06-full-abac-phase-7.7.md) — see [Decision #74](../specs/decisions/epic7/phase-7.7/074-unified-circuit-breaker-task-34.md))
- Cycle detection → error before depth limit (20 levels)

**Step 2: Implement PropertyProvider**

PropertyProvider's `parent_location` uses recursive CTE covering all three placement scenarios:

```sql
WITH RECURSIVE containment AS (
    SELECT parent_type, parent_id, ARRAY[parent_id] AS path, 0 AS depth
    FROM entity_properties WHERE id = $1
    UNION ALL
    -- Path 1: Direct location (location_id non-NULL)
    SELECT 'location', o.location_id::text, c.path || o.id::text, c.depth + 1
    FROM containment c
    JOIN objects o ON c.parent_type = 'object' AND c.parent_id = o.id::text
    WHERE c.depth < 20
      AND o.location_id IS NOT NULL
      AND NOT o.id::text = ANY(c.path)
    UNION ALL
    -- Path 2: Held by character (held_by_character_id non-NULL)
    SELECT 'character', o.held_by_character_id::text, c.path || o.id::text, c.depth + 1
    FROM containment c
    JOIN objects o ON c.parent_type = 'object' AND c.parent_id = o.id::text
    WHERE c.depth < 20
      AND o.held_by_character_id IS NOT NULL
      AND NOT o.id::text = ANY(c.path)
    UNION ALL
    -- Path 3: Contained in another object (contained_in_object_id non-NULL)
    SELECT 'object', o.contained_in_object_id::text, c.path || o.id::text, c.depth + 1
    FROM containment c
    JOIN objects o ON c.parent_type = 'object' AND c.parent_id = o.id::text
    WHERE c.depth < 20
      AND o.contained_in_object_id IS NOT NULL
      AND NOT o.id::text = ANY(c.path)
)
SELECT parent_id FROM containment
WHERE parent_type = 'location'
ORDER BY depth ASC LIMIT 1;
```

**Note:** Circuit breaker behavior is deferred to Task 34 (Phase 7.7). See Decision #74 (Unified Circuit Breaker in Task 34).

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/attribute/property.go internal/access/policy/attribute/property_test.go
git commit -m "feat(access): add PropertyProvider with recursive CTE for parent_location"
```

---

### Task 17: Build AccessPolicyEngine

**Spec References:** Evaluation Algorithm (lines 1696-1745), Core Interfaces > Session Subject Resolution (lines 348-414), ADR 0009 (Custom Go-Native ABAC Engine), ADR 0011 (Deny-overrides), ADR 0012 (Eager attribute resolution)

> **Performance Targets (Decision #23):** Evaluate() p99 <5ms, attribute resolution <2ms, DSL evaluation <1ms, cache reload <50ms (200 concurrent users). See [Decision #23](../specs/decisions/epic7/general/023-performance-targets.md).

**Acceptance Criteria:**

- [ ] Implements the 7-step evaluation algorithm from the spec exactly
- [ ] Engine MUST call Decision.Validate() before returning any Decision
- [ ] Step 1: System bypass — subject `"system"` → `types.NewDecision(SystemBypass, "system bypass", "")`
  - [ ] System bypass decisions MUST be audited in ALL modes (including off), even though Evaluate() short-circuits at step 1
  - [ ] System bypass audit writes MUST use sync write path (same as denials) per [ADR 66](../specs/decisions/epic7/phase-7.5/066-sync-audit-system-bypass.md) — guarantees audit trail for privileged operations
  - [ ] Engine implementation MUST call audit logger synchronously before returning from step 1
  - [ ] Test case: system bypass subject with audit mode=off still produces audit entry (via sync write)
  - [ ] Test case: system bypass audit write failure triggers WAL fallback (same flow as denials)
- [ ] Step 2: Session resolution — subject `"session:web-123"` → resolved to `"character:01ABC"` via SessionResolver
  - [ ] Invalid session → `types.NewDecision(DefaultDeny, "session invalid", "infra:session-invalid")`
  - [ ] Session store error → `types.NewDecision(DefaultDeny, "session store error", "infra:session-store-error")`
  - [ ] PostgreSQL SessionResolver implementation queries session store for character ID
  - [ ] Character deletion handling: deleted characters return SESSION_INVALID error code
  - [ ] All SessionResolver error codes tested: SESSION_INVALID, SESSION_STORE_ERROR
- [ ] Step 3: Eager attribute resolution (all attributes collected before evaluation)
- [ ] Step 4: Engine loads matching policies from the in-memory cache
- [ ] Step 5: Engine evaluates each policy's conditions against the attribute bags
- [ ] Step 6: Deny-overrides — forbid + permit both match → forbid wins (ADR 0011)
  - [ ] No policies match → `types.NewDecision(DefaultDeny, "no policies matched", "")`
- [ ] Step 7: Audit logger records the decision, matched policies, and attribute snapshot per configured mode
- [ ] Full policy evaluation (no short-circuit) when policy test active or audit mode is all ([04-resolution-evaluation.md#key-behaviors](../specs/abac/04-resolution-evaluation.md#key-behaviors), was spec lines 1751-1757)
- [ ] Provider error → evaluation continues, error recorded in decision
- [ ] Per-request cache → second call reuses cached attributes
- [ ] Test verifies Validate() is called on every engine return path
- [ ] Test for Decision invariant violation (allowed=true but effect=deny) is rejected by Validate()
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/engine.go`
- Test: `internal/access/policy/engine_test.go`
- Create: `internal/access/policy/session_resolver.go` (PostgreSQL SessionResolver implementation)
- Test: `internal/access/policy/session_resolver_test.go`

**Step 1: Write failing tests**

Table-driven tests covering the 7-step evaluation algorithm (spec Evaluation Algorithm, lines 1696-1745):

1. **System bypass:** Subject `"system"` → `types.NewDecision(SystemBypass, "system bypass", "")`
2. **Session resolution:** Subject `"session:web-123"` → resolved to `"character:01ABC"`, then evaluated
3. **Session invalid:** Subject `"session:expired"` → `types.NewDecision(DefaultDeny, "session invalid", "infra:session-invalid")`
4. **Session store error:** DB failure → `types.NewDecision(DefaultDeny, "session store error", "infra:session-store-error")`
5. **Eager attribute resolution:** All providers called before evaluation
6. **Policy matching:** Target filtering — principal type, action list, resource type/exact
7. **Condition evaluation:** Policies with satisfied conditions
8. **Deny-overrides:** Both permit and forbid match → forbid wins
9. **Default deny:** No policies match → `types.NewDecision(DefaultDeny, "no policies matched", "")`
10. **Audit logging:** Audit entry logged per configured mode
11. **Provider error:** Provider fails → evaluation continues, error recorded in decision
12. **Cache warmth:** Second call in same request reuses per-request attribute cache

**Step 2: Implement engine**

```go
// internal/access/policy/engine.go
package policy

import (
    "context"

    "github.com/holomush/holomush/internal/access/policy/types"
)

// AccessPolicyEngine is the main entry point for policy-based authorization.
type AccessPolicyEngine interface {
    Evaluate(ctx context.Context, request types.AccessRequest) (types.Decision, error)
}

// SessionResolver resolves session: subjects to character: subjects.
type SessionResolver interface {
    ResolveSession(ctx context.Context, sessionID string) (characterID string, err error)
}

// AuditLogger logs access decisions.
type AuditLogger interface {
    Log(entry AuditEntry)
}

// Engine implements AccessPolicyEngine.
type Engine struct {
    resolver     *attribute.AttributeResolver
    policyCache  *PolicyCache
    sessions     SessionResolver
    auditLogger  AuditLogger
}

func NewEngine(resolver *attribute.AttributeResolver, cache *PolicyCache, sessions SessionResolver, audit AuditLogger) *Engine

func (e *Engine) Evaluate(ctx context.Context, req types.AccessRequest) (types.Decision, error) {
    // Step 1: System bypass
    // Step 2: Session resolution
    // Step 3: Resolve attributes (eager)
    // Step 4: Load applicable policies (from cache snapshot)
    // Step 5: Evaluate conditions per policy
    // Step 6: Combine decisions (deny-overrides)
    // Step 7: Audit
}
```

**Step 3: Run tests**

Run: `task test`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/access/policy/engine.go internal/access/policy/engine_test.go
git commit -m "feat(access): add AccessPolicyEngine with deny-overrides evaluation"
```

---

### Task 18: Policy cache with LISTEN/NOTIFY invalidation

**Spec References:** Cache Invalidation (lines 2170-2215) — cache staleness threshold (lines 2191-2215), ADR 0016 (LISTEN/NOTIFY cache invalidation)

**Acceptance Criteria:**

- [ ] `Snapshot()` returns read-only copy safe for concurrent use
- [ ] `Reload()` fetches all enabled policies from store, recompiles, swaps snapshot atomically
- [ ] `Reload()` uses write lock only for swapping compiled policies; `Evaluate()` uses read lock during reload (no blocking)
- [ ] `Start()` method spawns LISTEN/NOTIFY goroutine; context cancellation stops goroutine
- [ ] `Listen()` subscribes to PostgreSQL `NOTIFY` on `policy_changed` channel using dedicated (non-pooled) connection
- [ ] NOTIFY event → cache reloads before next evaluation
- [ ] Concurrent reads during reload → stale reads tolerable (snapshot semantics)
- [ ] Connection drop + reconnect → full reload with exponential backoff (initial 100ms, max 30s, 2x backoff)
- [ ] Reconnect backoff resets after successful NOTIFY receipt
- [ ] Health check for subscription liveness (verify connection is alive and listening)
- [ ] Staleness detection: if no reload occurs within configurable threshold, system detects stale cache state
- [ ] Reload latency <50ms (benchmark test)
- [ ] Cache staleness threshold: configurable limit (default 30s) on time since last successful reload
- [ ] When staleness threshold exceeded → fail-closed (return `EffectDefaultDeny`) without evaluating policies
- [ ] Prometheus gauge `policy_cache_last_update` (Unix timestamp) updated on every successful reload
- [ ] **Graceful shutdown:** LISTEN/NOTIFY goroutine stops via context cancellation; shutdown test verifies goroutine exits cleanly
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/cache.go`
- Test: `internal/access/policy/cache_test.go`

**Step 1: Write failing tests**

- Load policies from store → cache snapshot available
- NOTIFY event on `policy_changed` → cache reloads before next evaluation
- Concurrent reads during reload → use snapshot semantics (stale reads tolerable)
- Connection drop → exponential backoff reconnect (100ms, 200ms, 400ms, ..., max 30s)
- Successful NOTIFY receipt → backoff timer resets
- Context cancellation → goroutine exits cleanly
- Cache staleness exceeds threshold → `Evaluate()` returns `EffectDefaultDeny` for all requests (fail-closed)
- Reload latency benchmark: <50ms target

**Step 2: Implement**

```go
// internal/access/policy/cache.go
package policy

import (
    "context"
    "sync"
    "time"
)

// PolicyCache holds compiled policies in memory with LISTEN/NOTIFY refresh.
type PolicyCache struct {
    mu       sync.RWMutex
    policies []*CachedPolicy
    store    store.PolicyStore
    compiler *PolicyCompiler
    lastUpdate time.Time
}

type CachedPolicy struct {
    Stored   store.StoredPolicy
    Compiled *CompiledPolicy
}

// Snapshot returns a read-only copy of current policies (safe for concurrent use).
// Uses read lock to allow concurrent reads during reload.
func (c *PolicyCache) Snapshot() []*CachedPolicy {
    c.mu.RLock()
    defer c.mu.RUnlock()
    // Return copy
}

// Reload fetches all enabled policies from store, recompiles, and swaps snapshot.
// Uses write lock only for final swap; compilation happens without lock.
func (c *PolicyCache) Reload(ctx context.Context) error {
    // Fetch policies (no lock held)
    // Compile policies (no lock held)
    // Swap snapshot (write lock)
    c.mu.Lock()
    c.policies = compiled
    c.lastUpdate = time.Now()
    c.mu.Unlock()
}

// Start spawns LISTEN/NOTIFY goroutine. Context cancellation stops goroutine.
func (c *PolicyCache) Start(ctx context.Context, pool *pgxpool.Pool) {
    go c.listenForNotifications(ctx, pool)
}

// listenForNotifications subscribes to PostgreSQL NOTIFY on 'policy_changed' channel.
// Reconnects with exponential backoff on connection loss (100ms initial, 30s max, 2x backoff).
// Exits cleanly on context cancellation.
func (c *PolicyCache) listenForNotifications(ctx context.Context, pool *pgxpool.Pool) {
    backoff := 100 * time.Millisecond
    const maxBackoff = 30 * time.Second

    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        // Use pgx.Connect() instead of pool.Acquire() because LISTEN requires
        // a persistent dedicated connection that cannot be recycled by the pool.
        conn, err := pgx.Connect(ctx, connStr)
        if err != nil {
            time.Sleep(backoff)
            backoff = min(backoff*2, maxBackoff)
            continue
        }

        _, err = conn.Exec(ctx, "LISTEN policy_changed")
        if err != nil {
            conn.Release()
            time.Sleep(backoff)
            backoff = min(backoff*2, maxBackoff)
            continue
        }

        // Wait for NOTIFY
        for {
            notification, err := conn.Conn().WaitForNotification(ctx)
            if err != nil {
                conn.Release()
                c.Reload(context.Background()) // Full reload on reconnect
                time.Sleep(backoff)
                backoff = min(backoff*2, maxBackoff)
                break
            }

            // Reset backoff on successful receipt
            backoff = 100 * time.Millisecond

            // Trigger reload
            c.Reload(context.Background())
        }
    }
}
```

**Goroutine Lifecycle:**

1. **Start:** `PolicyCache.Start(ctx, pool)` spawns the LISTEN/NOTIFY goroutine
2. **Reconnect:** Exponential backoff on connection loss (initial 100ms, max 30s, 2x backoff)
3. **Reset:** Backoff timer resets to 100ms after successful NOTIFY receipt
4. **Shutdown:** Context cancellation causes goroutine to exit cleanly

**Concurrency Pattern:**

- **Read lock:** `Evaluate()` calls acquire read lock during policy evaluation (non-blocking during reload)
- **Write lock:** `Reload()` acquires write lock only for swapping compiled policies (~50μs)
- **No lock during compilation:** Fetch from DB and compile policies without holding lock (~50ms)

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/cache.go internal/access/policy/cache_test.go
git commit -m "feat(access): add policy cache with LISTEN/NOTIFY invalidation"
```

---

### Task 19: Audit logger

**Spec References:** Audit Log Serialization (lines 2216-2234), Audit Log Configuration (lines 2248-2325), Audit Log Retention (lines 2326-2423)

**Acceptance Criteria:**

- [ ] Three audit modes: `off` (system bypasses only), `denials_only`, `all`
- [ ] Mode `off`: only system bypasses logged
- [ ] Mode `denials_only`: denials + default deny + system bypass logged, allows skipped
- [ ] Mode `all`: everything logged
- [ ] **Sync write for denials and system bypasses:** `deny`, `default_deny`, and `system_bypass` events written synchronously to PostgreSQL before `Evaluate()` returns

> **Note:** Denials elevated from spec SHOULD (line 2293) to MUST. Rationale: denial audit integrity is critical for security forensics. The ~1-2ms latency per denial is acceptable given denial events are uncommon in normal operation.
>
> **Note:** System bypasses use sync path per [ADR 66](../specs/decisions/epic7/phase-7.5/066-sync-audit-system-bypass.md). Rationale: Privileged operations require guaranteed audit trails. System bypasses are rare (server startup, admin maintenance) so sync write cost is negligible. Prevents gaps in audit trail for privilege escalation.
>
> **Security requirement (S3):** If audit mode `off` is implemented to suppress
> denial logging (creating a security blind spot), the mode name and
> documentation MUST include clear warnings. Tests MUST verify denial logging
> behavior matches documented semantics.

- [ ] **Async write for regular allows:** `allow` events (non-system-bypass) written asynchronously via buffered channel
- [ ] Channel full → entry dropped, `abac_audit_channel_full_total` metric incremented
- [ ] **WAL fallback:** If sync write fails, denial entry written to WAL path from `internal/xdg` package (append-only, O_SYNC)
- [ ] **ReplayWAL():** Method reads WAL entries, batch-inserts to PostgreSQL, truncates file on success
- [ ] Catastrophic failure (DB + WAL fail) → log to stderr at ERROR, increment `abac_audit_failures_total{reason="wal_failed"}`, drop entry
- [ ] Entry includes: subject, action, resource, effect, policy\_id, policy\_name, attributes snapshot, duration\_us
- [ ] `audit/postgres.go` batch-inserts from channel (async) and handles sync writes (denials)
- [ ] **Graceful shutdown:** Async channel consumer goroutine stops accepting new entries, drains buffered channel, closes WAL file, exits cleanly
- [ ] **Shutdown order:** Stop accepting new entries → drain channel → flush to DB → close WAL file → stop consumer goroutine
- [ ] Shutdown test verifies all buffered entries written before goroutine exits
- [ ] **WAL monitoring:** `abac_audit_wal_entries` Prometheus gauge tracks current WAL entry count (registered by Task 20, updated by Task 19)
- [ ] **WAL threshold:** Configurable threshold for WAL size (default 10MB or 10,000 entries) for alerting on persistent DB failures (Task 19 ownership)
- [ ] **WAL metric updates:** WAL gauge incremented/decremented by audit logger (Task 19) during WAL write and replay operations
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/audit/logger.go`
- Create: `internal/access/policy/audit/postgres.go`
- Test: `internal/access/policy/audit/logger_test.go`

**Step 1: Write failing tests**

- Mode `off`: only system bypasses logged
  - [ ] Test: off mode + system_bypass → written
  - [ ] Test: off mode + allow → dropped
  - [ ] Test: off mode + deny → dropped
- Mode `denials_only`: denials + default deny + system bypass logged, allows skipped
- Mode `all`: everything logged
- **Sync write for denials and system bypasses:** `deny`, `default_deny`, and `system_bypass` events written synchronously, `Evaluate()` blocks until write completes
- **Async write for regular allows:** `allow` events (non-system-bypass) submitted via buffered channel, doesn't block `Evaluate()`
- Channel full: entry dropped, `abac_audit_channel_full_total` metric incremented
- **WAL fallback:** If sync write fails, denial entry written to WAL path from `internal/xdg` package
- **ReplayWAL():** Reads WAL file, batch-inserts to PostgreSQL, truncates on success
- Catastrophic failure: DB + WAL fail → stderr log, `abac_audit_failures_total{reason="wal_failed"}` incremented, entry dropped
- Verify entry contains: subject, action, resource, effect, policy_id, policy_name, attributes snapshot, duration_us

**Step 2: Implement**

```go
// internal/access/policy/audit/logger.go
package audit

// AuditMode controls what decisions are logged.
type AuditMode string

const (
    AuditOff         AuditMode = "off"            // system bypasses only
    AuditDenialsOnly AuditMode = "denials_only"   // denials + default deny + system bypass
    AuditAll         AuditMode = "all"            // everything
)

// Logger writes audit entries with sync (denials, system bypasses) or async (regular allows) paths.
// Denials and system bypasses are written synchronously to prevent evidence erasure.
type Logger struct {
    mode      AuditMode
    entryCh   chan Entry         // async channel for regular allow events only
    writer    Writer             // PostgreSQL writer
    walPath   string             // Write-Ahead Log path for sync write fallback
    walFile   *os.File           // WAL file handle (opened with O_APPEND | O_SYNC)
}

// Writer persists audit entries (PostgreSQL implementation in postgres.go).
type Writer interface {
    Write(ctx context.Context, entries []Entry) error       // batch writes (async)
    WriteSync(ctx context.Context, entry Entry) error       // single sync write (denials)
}

// ReplayWAL reads entries from the WAL file, batch-inserts to PostgreSQL,
// and truncates the file on success. Called on startup and periodically
// during recovery from transient database failures.
func (l *Logger) ReplayWAL(ctx context.Context) error

// Entry is a single audit log record.
type Entry struct {
    ID             string
    Timestamp      time.Time
    Subject        string
    Action         string
    Resource       string
    Effect         string
    PolicyID       string
    PolicyName     string
    Attributes     *types.AttributeBags
    ErrorMessage   string
    ProviderErrors []attribute.ProviderError
    DurationUS     int
}
```

`audit/postgres.go` implements both async batch-inserts from the channel and synchronous single writes for security-significant decisions. The logger distinguishes between effects:

- **`deny`, `default_deny`, and `system_bypass`:** Call `writer.WriteSync()` synchronously before returning from `Log()`. If the write fails, append the entry to `$XDG_STATE_HOME/holomush/audit-wal.jsonl` (opened with `O_APPEND | O_SYNC`). If both DB and WAL fail, log to stderr, increment `abac_audit_failures_total{reason="wal_failed"}`, and drop the entry.
- **`allow` (regular, non-system-bypass):** Send to `entryCh` buffered channel for async batch writes. If channel is full, drop entry and increment `abac_audit_channel_full_total`.

**Audit Path Summary:**

| Effect            | Write Path | Rationale                                      |
| ----------------- | ---------- | ---------------------------------------------- |
| `deny`            | Sync       | Security forensics — evidence of denials       |
| `default_deny`    | Sync       | Security forensics — evidence of denials       |
| `system_bypass`   | Sync       | Privileged operations — guaranteed audit trail |
| `allow` (regular) | Async      | Performance — high-volume routine operations   |

`ReplayWAL()` reads JSON-encoded entries from the WAL file, batch-inserts them to PostgreSQL, and truncates the file on success. The server calls this on startup and MAY call it periodically (e.g., every 5 minutes) during recovery.

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/audit/
git commit -m "feat(access): add async audit logger with mode control"
```

---

### Task 19b: Audit log retention and partition management

**Spec References:** Policy Storage > Audit Log Retention (lines 2326-2423)

**Acceptance Criteria:**

- [ ] `AuditConfig` struct with `RetainDenials` (90 days), `RetainAllows` (7 days), `PurgeInterval` (24h)
- [ ] Background goroutine for partition lifecycle: create future partitions, detach/drop expired partitions
- [ ] Partition creation: pre-create next 3 months of partitions
- [ ] Two-tier retention strategy: partition drops at 90-day threshold (longer retention), row-level DELETE for allow rows older than 7 days within active partitions
- [ ] Partition drops use 90-day threshold (denial retention period) since monthly partitions contain mixed effects
- [ ] Row-level DELETE removes allow-effect rows older than 7 days within still-attached partitions
- [ ] Partition expiration: detach partitions older than retention period, drop after 7-day grace period
- [ ] Health check endpoint: verify current month's partition exists and is attached
- [ ] Health check alerts if no valid partition for current timestamp
- [ ] `PurgeInterval` configurable via flag (default 24h)
- [ ] Startup: create missing partitions, schedule first purge cycle
- [ ] Graceful shutdown: stop background goroutine
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/audit/retention.go`
- Modify: `internal/access/policy/audit/postgres.go` (add partition management)
- Test: `internal/access/policy/audit/retention_test.go`

**Step 1: Write failing tests**

- `AuditConfig` struct with retention periods (denials: 90d, allows: 7d)
- Background goroutine creates future partitions (next 3 months)
- Two-tier retention: partition drops at 90-day threshold, row-level DELETE for allow-effect rows older than 7 days
- Background goroutine detaches expired partitions based on retention period
- Health check: returns error if current partition missing
- Purge cycle runs every `PurgeInterval` (default 24h)

**Note:** Monthly partitions contain mixed effects (allows and denials). Partition-level drops use the longer retention period (90 days for denials). Within active partitions, row-level DELETE removes allow-effect rows older than 7 days.

**Step 2: Implement**

```go
// internal/access/policy/audit/retention.go
package audit

import (
    "context"
    "time"
)

// AuditConfig defines audit log retention and purge settings.
type AuditConfig struct {
    Mode          AuditMode     // Default: AuditDenialsOnly
    RetainDenials time.Duration // Default: 90 days
    RetainAllows  time.Duration // Default: 7 days
    PurgeInterval time.Duration // Default: 24 hours
}

// DefaultAuditConfig returns sensible defaults.
func DefaultAuditConfig() AuditConfig {
    return AuditConfig{
        Mode:          AuditDenialsOnly,
        RetainDenials: 90 * 24 * time.Hour,
        RetainAllows:  7 * 24 * time.Hour,
        PurgeInterval: 24 * time.Hour,
    }
}

// PartitionManager handles partition lifecycle.
type PartitionManager struct {
    db     *pgxpool.Pool
    config AuditConfig
}

// Start begins background partition management.
func (pm *PartitionManager) Start(ctx context.Context) {
    // Create missing partitions on startup
    pm.createFuturePartitions(ctx)

    // Schedule purge cycle
    ticker := time.NewTicker(pm.config.PurgeInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            pm.purgeExpiredPartitions(ctx)
        case <-ctx.Done():
            return
        }
    }
}

// createFuturePartitions pre-creates partitions for the next 3 months.
func (pm *PartitionManager) createFuturePartitions(ctx context.Context) {
    // Create partitions for current month + next 3 months
    now := time.Now()
    for i := 0; i < 4; i++ {
        month := now.AddDate(0, i, 0)
        pm.ensurePartition(ctx, month)
    }
}

// purgeExpiredPartitions detaches and drops partitions older than retention period.
func (pm *PartitionManager) purgeExpiredPartitions(ctx context.Context) {
    // Detach partitions older than RetainDenials (90 days)
    // Drop detached partitions after 7-day grace period
}

// HealthCheck verifies current partition exists.
func (pm *PartitionManager) HealthCheck(ctx context.Context) error {
    // Check if partition for current timestamp exists and is attached
    // Return error if missing
}
```

Partition lifecycle ([05-storage-audit.md#audit-log-retention](../specs/abac/05-storage-audit.md#audit-log-retention), was spec lines 2326-2373):

1. Pre-create partitions for next 3 months
2. Detach partitions older than `RetainDenials` (90 days for denials, 7 days for allows)
3. Drop detached partitions after 7-day grace period
4. Health check ensures current partition exists

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/audit/retention.go internal/access/policy/audit/retention_test.go
git commit -m "feat(access): add audit log retention and partition management"
```

---

### Task 20: Prometheus metrics for ABAC

**Spec References:** Evaluation Algorithm > Performance Targets (lines 1769-1945) — observability metrics are part of the performance targets section

**Acceptance Criteria:**

- [ ] `abac_evaluate_duration_seconds` histogram recorded after each `Evaluate()`
- [ ] `abac_policy_evaluations_total` counter with `source` (values: seed/lock/admin/plugin) and `effect` labels — avoids unbounded cardinality from admin-created policy names
- [ ] Cardinality concern documented: `source` label preferred over `name` label to prevent metric explosion from admin policy names
- [ ] All metrics reviewed for unbounded label values (no `name`, `subject_id`, `resource_id` labels)
- [ ] `abac_audit_channel_full_total` counter for dropped audit entries (Task 20 ownership)
- [ ] `abac_audit_failures_total` counter with `reason` label (see spec Evaluation Algorithm > Performance Targets) (Task 20 ownership)
- [ ] `abac_audit_wal_entries` gauge for current WAL entry count (Task 20 ownership - registered here, updated by Task 19 audit logger)
- [ ] `abac_degraded_mode` gauge (0=normal, 1=degraded) (see spec Attribute Resolution > Error Handling for degraded mode) (Task 20 ownership)
- [ ] **Degraded mode alerting (Review Finding I3):** Alert configuration documented for `abac_degraded_mode == 1` with critical severity, includes recovery procedure reference to `policy clear-degraded-mode` command (Task 33, Phase 7.7)
- [ ] Alert fires when `abac_degraded_mode` gauge equals 1 for >1 minute, indicating prolonged degraded state
- [ ] `abac_provider_circuit_breaker_trips_total` counter with `provider` label (registered here, tripped by Task 34's general circuit breaker — see [Decision #74](../specs/decisions/epic7/phase-7.7/074-unified-circuit-breaker-task-34.md))
- [ ] `abac_provider_errors_total` counter with `namespace` and `error_type` labels
- [ ] `abac_policy_cache_last_update` gauge with Unix timestamp (updated on every successful cache reload — tracks LISTEN/NOTIFY connection freshness)
- [ ] **LISTEN/NOTIFY staleness monitoring (Review Finding H1):** Alert threshold for LISTEN/NOTIFY connection health documented (alert if `time.Now() - policy_cache_last_update > 5 minutes` indicates prolonged disconnection)
- [ ] Staleness monitoring rationale: LISTEN/NOTIFY connection drop causes silent cache staleness without indication — gauge enables alerting on prolonged disconnection before cache becomes dangerously outdated
- [ ] Recovery procedure reference: On prolonged disconnection, manual cache reload triggers full policy refresh (see Task 17 LISTEN/NOTIFY reconnect logic with exponential backoff)
- [ ] `abac_unregistered_attributes_total` counter vec with `namespace` and `key` labels (schema drift indicator)
- [ ] `RegisterMetrics()` follows existing pattern from `internal/observability/server.go`
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/metrics.go`
- Test: `internal/access/policy/metrics_test.go`

**Step 1: Write tests verifying metrics are recorded**

After `Evaluate()`, verify the relevant counters/histograms are updated.

**Step 2: Implement**

Follow existing observability pattern from `internal/observability/server.go`:

```go
// internal/access/policy/metrics.go
package policy

import "github.com/prometheus/client_golang/prometheus"

var (
    evaluateDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
        Name:    "abac_evaluate_duration_seconds",
        Help:    "Time spent in AccessPolicyEngine.Evaluate()",
        Buckets: prometheus.DefBuckets,
    })
    policyEvaluations = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "abac_policy_evaluations_total",
        Help: "Total policy evaluations by source and effect (source: seed/lock/admin/plugin)",
    }, []string{"source", "effect"})
    auditChannelFull = prometheus.NewCounter(prometheus.CounterOpts{
        Name: "abac_audit_channel_full_total",
        Help: "Audit entries dropped due to full channel",
    })
    auditFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "abac_audit_failures_total",
        Help: "Failed audit writes by reason",
    }, []string{"reason"})
    auditWALEntries = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "abac_audit_wal_entries",
        Help: "Current number of entries in audit WAL file (updated by Task 19 audit logger)",
    })
    degradedMode = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "abac_degraded_mode",
        Help: "ABAC engine degraded mode status (0=normal, 1=degraded)",
    })
    providerCircuitBreakerTrips = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "abac_provider_circuit_breaker_trips_total",
        Help: "Circuit breaker trips by provider namespace",
    }, []string{"provider"})
    providerErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "abac_provider_errors_total",
        Help: "Provider errors by namespace and type",
    }, []string{"namespace", "error_type"})
    policyCacheLastUpdate = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "abac_policy_cache_last_update",
        Help: "Unix timestamp of last policy cache update (LISTEN/NOTIFY connection health indicator)",
    })
    unregisteredAttributes = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "abac_unregistered_attributes_total",
        Help: "References to unregistered attributes (schema drift indicator)",
    }, []string{"namespace", "key"})
)

// RegisterMetrics registers all ABAC metrics with the given registerer.
func RegisterMetrics(reg prometheus.Registerer) {
    reg.MustRegister(evaluateDuration, policyEvaluations, auditChannelFull,
        auditFailures, auditWALEntries, degradedMode, providerCircuitBreakerTrips,
        providerErrorsTotal, policyCacheLastUpdate, unregisteredAttributes)
}
```

**LISTEN/NOTIFY Staleness Monitoring (Review Finding H1):**

The `policy_cache_last_update` gauge tracks LISTEN/NOTIFY connection health. If the LISTEN connection drops silently, the cache becomes stale without indication. Alert configuration:

```yaml
# Prometheus alert rule (example)
- alert: ABACPolicyCacheStale
  expr: time() - abac_policy_cache_last_update > 300  # 5 minutes
  for: 1m
  severity: warning
  summary: "ABAC policy cache potentially stale (LISTEN/NOTIFY connection may be down)"
  description: "Policy cache last updated {{ $value | humanizeDuration }} ago. Check LISTEN/NOTIFY connection health."
```

**Recovery procedure:** Task 17's LISTEN/NOTIFY goroutine automatically reconnects with exponential backoff (initial 100ms, max 30s) and triggers full cache reload on reconnect. Manual intervention: restart server or trigger cache reload via admin API (future Task 37, Phase 7.7).

**Degraded Mode Alerting (Review Finding I3):**

The `abac_degraded_mode` gauge tracks engine degraded mode state (0=normal, 1=degraded). When a corrupted forbid/deny policy is detected (Task 33, Phase 7.7), the engine enters degraded mode and returns system-wide denials to prevent security bypass. Alert configuration:

```yaml
# Prometheus alert rule (example)
- alert: ABACDegradedMode
  expr: abac_degraded_mode == 1
  for: 1m
  severity: critical
  summary: "ABAC engine in degraded mode (system-wide deny active)"
  description: "ABAC engine detected corrupted deny/forbid policy and entered degraded mode. All requests are being denied. Use policy clear-degraded-mode command to recover after fixing the corrupted policy."
```

**Recovery procedure:** Task 33 (Phase 7.7) implements degraded mode behavior and the `policy clear-degraded-mode` command. Operator workflow: 1) Identify and fix/disable the corrupted policy, 2) Run `policy clear-degraded-mode` to restore normal operation, 3) Verify `abac_degraded_mode` gauge returns to 0.

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/metrics.go internal/access/policy/metrics_test.go
git commit -m "feat(access): add Prometheus metrics for ABAC engine"
```

---

### Task 21: Performance benchmarks

**Spec References:** Evaluation Algorithm > Performance Targets (lines 1769-1945)

> **Performance Targets (Decision #23):** Evaluate() p99 <5ms, attribute resolution <2ms, DSL evaluation <1ms, cache reload <50ms (200 concurrent users). See [Decision #23](../specs/decisions/epic7/general/023-performance-targets.md).

**Acceptance Criteria:**

- [ ] **DB-inclusive benchmarks** (with PostgreSQL queries):
  - [ ] `BenchmarkEvaluate_ColdCache` — p99 <10ms
  - [ ] `BenchmarkEvaluate_WarmCache` — p99 <5ms
  - [ ] `BenchmarkAttributeResolution_Cold` — <2ms
  - [ ] `BenchmarkAttributeResolution_Warm` — <100μs
  - [ ] `BenchmarkCacheReload` — <50ms
  - [ ] `BenchmarkPropertyProvider_ParentLocation` — recursive CTE with varying depths (1, 5, 10, 20 levels)
  - [ ] PropertyProvider benchmark validates 100ms timeout appropriateness
  - [ ] PropertyProvider benchmark verifies timeout behavior under load (circuit breaker logic tested in Task 34)
  - [ ] `BenchmarkProviderStarvation` — slow first provider consuming ~80ms of 100ms budget, verifies subsequent providers receive cancelled contexts (per spec fair-share timeout requirement)
- [ ] **Pure computation benchmarks** (no I/O, in-memory only):
  - [ ] `BenchmarkConditionEvaluation` — <1ms per policy (pure computation)
  - [ ] `BenchmarkWorstCase_NestedIf` — 32-level nesting <5ms (pure computation)
  - [ ] `BenchmarkWorstCase_AllPoliciesMatch` — 50 policies <10ms (pure computation)
  - [ ] Pure/no-IO microbenchmarks: single-policy evaluation <10μs
  - [ ] Pure/no-IO microbenchmarks: 50-policy set evaluation <100μs
  - [ ] Pure/no-IO microbenchmarks: attribute resolution <50μs
- [ ] Setup: 50 active policies, 3 operators per condition avg, 10 attributes per entity
- [ ] All benchmarks run without errors

**Files:**

- Create: `internal/access/policy/engine_bench_test.go`

**Step 1: Write benchmarks per spec performance targets (lines 1769-1795)**

```go
func BenchmarkEvaluate_ColdCache(b *testing.B)         // target: <10ms p99
func BenchmarkEvaluate_WarmCache(b *testing.B)          // target: <5ms p99
func BenchmarkAttributeResolution_Cold(b *testing.B)    // target: <2ms
func BenchmarkAttributeResolution_Warm(b *testing.B)    // target: <100μs
func BenchmarkConditionEvaluation(b *testing.B)         // target: <1ms per policy
func BenchmarkCacheReload(b *testing.B)                 // target: <50ms
func BenchmarkWorstCase_NestedIf(b *testing.B)          // 32-level nesting <5ms
func BenchmarkWorstCase_AllPoliciesMatch(b *testing.B)  // 50 policies <10ms
func BenchmarkProviderStarvation(b *testing.B)          // slow first provider ~80ms, subsequent providers get cancelled contexts
```

Setup: 50 active policies (25 permit, 25 forbid), 3 operators per condition average, 10 attributes per entity.

**BenchmarkProviderStarvation implementation:**

Simulates a slow first provider consuming ~80ms of the 100ms total budget. Verifies that:

- Subsequent providers receive contexts with `ctx.Err() == context.DeadlineExceeded`
- Fair-share timeout calculation correctly allocates remaining budget
- Provider starvation is observable and measurable

**Step 2: Run benchmarks**

Run: `go test -bench=. -benchmem ./internal/access/policy/`
Expected: All within spec targets

**Note:** Direct `go test` is intentional here — benchmark testing is not covered by `task test` runner.

**Step 3: Commit**

```bash
git add internal/access/policy/engine_bench_test.go
git commit -m "test(access): add ABAC engine benchmarks for performance targets"
```

---

### Task 21b: CI benchmark enforcement

> **Note:** This task depends on Task 21 — benchmarks must exist before CI can enforce regression limits.

**Spec References:** Evaluation Algorithm > Performance Targets (lines 579-580) — "CI MUST fail if any benchmark exceeds 110% of its documented target value"

**Acceptance Criteria:**

- [ ] GitHub Actions workflow configured to run benchmarks on pull requests
- [ ] Benchmark baseline file stored in repository (e.g., `.benchmarks/baseline.txt`)
- [ ] CI step compares current benchmark results against baseline values
- [ ] CI fails if ANY benchmark exceeds 110% of documented target (e.g., cold Evaluate p99 >11ms, warm >5.5ms)
- [ ] Baseline update strategy documented: manual update via `make update-benchmark-baseline` or similar
- [ ] Benchmark regression failures treated as build failures (PRs cannot merge)
- [ ] Test: Simulate benchmark regression → verify CI fails with clear error message
- [ ] Documentation added to contributor guide explaining benchmark enforcement

**Files:**

- Create: `.github/workflows/benchmark-check.yml`
- Create: `scripts/check-benchmark-regression.sh`
- Create: `.benchmarks/baseline.txt` (baseline values)
- Modify: `docs/contributors/testing.md` or similar (document baseline update process)

**Step 1: Write benchmark comparison script**

Create `scripts/check-benchmark-regression.sh`:

```bash
#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

# Compares current benchmark results against baseline.
# Exits 1 if any benchmark exceeds 110% of baseline.

set -euo pipefail

BASELINE_FILE="${1:-.benchmarks/baseline.txt}"
CURRENT_FILE="${2:-/dev/stdin}"

# Parse benchmark results and compare
# Exit 1 if regression detected
```

**Step 2: Create GitHub Actions workflow**

Create `.github/workflows/benchmark-check.yml`:

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

name: Benchmark Regression Check

on:
  pull_request:
    paths:
      - 'internal/access/policy/**'
      - '.benchmarks/**'

jobs:
  benchmark:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - name: Run benchmarks
        run: |
          go test -bench=. -benchmem ./internal/access/policy/ > current-benchmarks.txt
      - name: Check for regressions
        run: |
          ./scripts/check-benchmark-regression.sh .benchmarks/baseline.txt current-benchmarks.txt
```

**Step 3: Create baseline file with documented targets**

Create `.benchmarks/baseline.txt` with values from Task 21 acceptance criteria:

```text
BenchmarkEvaluate_ColdCache: 10ms p99
BenchmarkEvaluate_WarmCache: 5ms p99
BenchmarkAttributeResolution_Cold: 2ms
BenchmarkAttributeResolution_Warm: 100μs
BenchmarkConditionEvaluation: 1ms
BenchmarkCacheReload: 50ms
```

**Step 4: Document baseline update process**

Add section to contributor docs explaining when and how to update baseline (e.g., after intentional performance improvements or architecture changes).

**Step 5: Test regression detection**

Temporarily modify a benchmark to exceed 110% threshold, verify CI fails with clear error message.

**Step 6: Commit**

```bash
git add .github/workflows/benchmark-check.yml scripts/check-benchmark-regression.sh .benchmarks/
git commit -m "ci(access): enforce benchmark regression limits in CI"
```

**Estimated Effort:** 4 hours

---

### Task 21a: Remove @-prefix from command names

> **Note:** The @-prefix exists only in access control permission strings (e.g., `"execute:command:@dig"`), not in actual command registrations. Command validation explicitly rejects `@` as a leading character.

**Spec References:** Replacing Static Roles > Seed Policies (lines 2984-3061) — seed policies reference command names without @ prefix

**Acceptance Criteria:**

- [ ] All command name handling removes @ prefix
- [ ] No `@`-prefixed command names remain in codebase
- [ ] Command lock expressions reference bare command names (e.g., `dig`, `create`, not `@dig`, `@create`)
- [ ] `task test` passes
- [ ] `task lint` passes

> **Verified (2026-02-07):** @-prefixed command names confirmed in `permissions.go` (4), `permissions_test.go` (1), `static_test.go` (4). Total: 9 occurrences. Command validation rejects `@` as leading character — the `@` exists only in permission string encoding, not in actual command names.
>
> **Note:** 4 of 9 @-prefixed name occurrences are in `static_test.go` which is deleted by Task 29 ([Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)). Only 5 occurrences in `permissions.go` and `permissions_test.go` need modification in this task.
>
> **Note:** This task could be submitted as an independent pre-ABAC PR. It only modifies `internal/access/permissions.go` and has no ABAC dependencies.

**Files:**

- `internal/access/permissions.go` (4 instances)
- `internal/access/permissions_test.go` (1 instance)
- `internal/access/static_test.go` (4 instances)

**Step 1: Search for @-prefixed command usage**

```bash
rg 'command:@' --type go internal/access/
```

**Step 2: Remove @ prefix from all command name handling**

Update command name parsing, lock expressions, and any references to strip or avoid the @ prefix.

**Step 3: Run tests**

```bash
task test
```

**Step 4: Commit**

```bash
git add internal/access/permissions.go internal/access/permissions_test.go internal/access/static_test.go
git commit -m "refactor(commands): remove @ prefix from command names"
```

---

**Cross-Phase Gate:** Task 18 (policy cache with LISTEN/NOTIFY invalidation) gates Phase 7.4. Task 18 is engine infrastructure that logically belongs here, even though T18→T23 creates a Phase 7.4 dependency.

---

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)** | **[Next: Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)**

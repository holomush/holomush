<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)** | **[Next: Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)**

## Phase 7.3: Policy Engine & Attribute Providers

### Task 13: Attribute provider interface and schema registry

**Spec References:** Core Interfaces > Attribute Providers (lines 513-604), Attribute Resolution > Attribute Schema Registry (lines 1339-1382)

> **Design note:** `AttributeSchema` and `AttrType` are defined in `internal/access/policy/types/` (Task 5 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md))) to prevent circular imports. The `policy` package (compiler) needs `AttributeSchema`, and the `attribute` package (resolver) needs `types.AccessRequest` and `types.AttributeBags`. Both import from `types` package.

**Acceptance Criteria:**

- [ ] `AttributeProvider` interface: `Namespace()`, `ResolveSubject()`, `ResolveResource()`, `LockTokens()`
- [ ] `EnvironmentProvider` interface: `Namespace()`, `Resolve()`
- [ ] `AttributeSchema` supports: `Register()`, `IsRegistered()` (uses type definition from Task 5 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)))
- [ ] Duplicate namespace registration → error
- [ ] Empty namespace → error
- [ ] Duplicate attribute key within namespace → error
- [ ] Invalid attribute type → error
- [ ] Providers MUST return all numeric attributes as `float64` (per spec Core Interfaces > Core Attribute Schema, lines 605-731)
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

**Spec References:** Attribute Resolution > Resolution Flow (lines 1301-1327), Evaluation Algorithm > Performance Targets (lines 1715-1822), Evaluation Algorithm > Attribute Caching (lines 1891-1970), ADR 0012 (Eager attribute resolution)

> **Note (Bug I10):** Spec lines 1976-2005 explicitly specify LRU eviction with `maxEntries` default of 100 (line 1982). Reviewer concern about missing LRU/size spec was incorrect — spec clearly defines both semantics and default value.

**Acceptance Criteria:**

- [ ] Single provider → correct attribute bags returned
- [ ] Multiple providers → merge semantics (last-registered wins for scalars, concatenate for lists)
- [ ] Core-to-plugin key collision → reject plugin registration at startup
- [ ] Plugin-to-plugin key collision → warn, last registered wins
- [ ] Provider error → skip provider, continue, record `ProviderError`
- [ ] Per-request cache → second `Resolve()` with same entity reuses cached result
- [ ] Fair-share budget: `max(remainingBudget / remainingProviders, 5ms)`
- [ ] Provider exceeding fair-share timeout → cancelled
- [ ] Re-entrance detection → provider calling `Evaluate()` on same context → panic
- [ ] `AttributeCache` is LRU with max 100 entries, attached to context (per spec lines 1976-2005)
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
}

// AttributeResolver orchestrates attribute providers with caching and timeouts.
type AttributeResolver struct {
    providers    []AttributeProvider
    envProviders []EnvironmentProvider
    schema       *types.AttributeSchema
    totalBudget  time.Duration // default 100ms
}

func NewAttributeResolver(budget time.Duration) *AttributeResolver
func (r *AttributeResolver) RegisterProvider(p AttributeProvider) error
func (r *AttributeResolver) RegisterEnvironmentProvider(p EnvironmentProvider)
func (r *AttributeResolver) Resolve(ctx context.Context, req types.AccessRequest) (*types.AttributeBags, []ProviderError, error)
```

Key: fair-share timeout is `max(remainingBudget / remainingProviders, 5ms)`.

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

**Spec References:** Core Interfaces > Core Attribute Schema (lines 605-731) — character, location, and object attributes are in the table

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

**Spec References:** Core Interfaces > Core Attribute Schema (lines 605-731) — environment, command, stream attributes are in the table

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

> **Note:** This task depends on Task 4a ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) (PropertyRepository must exist before PropertyProvider).

**Spec References:** Property Model > Property Attributes (lines 1134-1149), ADR 0013 (Properties as first-class entities)

**Acceptance Criteria:**

- [ ] PropertyProvider resolves all property attributes including `parent_location`
- [ ] `parent_location` uses recursive CTE covering all three placement scenarios: direct location (location_id), held by character (held_by_character_id), contained in object (contained_in_object_id)
- [ ] `parent_location` CTE depth limit: 20 levels
- [ ] `parent_location` resolution timeout: 100ms
- [ ] Circuit breaker: 3 timeout errors in 60s → skip queries for 60s
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
- `parent_location` resolution timeout (100ms) → error, circuit breaker trips after 3 timeouts in 60s
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

Circuit breaker: 3 timeout errors in 60s → skip queries for 60s.

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/attribute/property.go internal/access/policy/attribute/property_test.go
git commit -m "feat(access): add PropertyProvider with recursive CTE for parent_location"
```

---

### Task 17: Build AccessPolicyEngine

**Spec References:** Evaluation Algorithm (lines 1642-1690), Core Interfaces > Session Subject Resolution (lines 326-392), ADR 0011 (Deny-overrides), ADR 0012 (Eager attribute resolution)

**Acceptance Criteria:**

- [ ] Implements the 7-step evaluation algorithm from the spec exactly
- [ ] Step 1: System bypass — subject `"system"` → `types.NewDecision(SystemBypass, "system bypass", "")`
  - [ ] System bypass decisions MUST be audited in ALL modes (including off), even though Evaluate() short-circuits at step 1
  - [ ] System bypass audit writes MUST use sync write path (same as denials) per ADR 66 — guarantees audit trail for privileged operations
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
- [ ] Full policy evaluation (no short-circuit) when policy test active or audit mode is all (spec lines 1697-1703)
- [ ] Provider error → evaluation continues, error recorded in decision
- [ ] Per-request cache → second call reuses cached attributes
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/engine.go`
- Test: `internal/access/policy/engine_test.go`
- Create: `internal/access/policy/session_resolver.go` (PostgreSQL SessionResolver implementation)
- Test: `internal/access/policy/session_resolver_test.go`

**Step 1: Write failing tests**

Table-driven tests covering the 7-step evaluation algorithm (spec Evaluation Algorithm, lines 1642-1690):

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

**Spec References:** Cache Invalidation (lines 2115-2159) — cache staleness threshold (lines 2136-2159), ADR 0016 (LISTEN/NOTIFY cache invalidation)

**Acceptance Criteria:**

- [ ] `Snapshot()` returns read-only copy safe for concurrent use
- [ ] `Reload()` fetches all enabled policies from store, recompiles, swaps snapshot atomically
- [ ] `Listen()` subscribes to PostgreSQL `NOTIFY` on `policy_changed` channel using dedicated (non-pooled) connection
- [ ] NOTIFY event → cache reloads before next evaluation
- [ ] Concurrent reads during reload → stale reads tolerable (snapshot semantics)
- [ ] Connection drop + reconnect → full reload with exponential backoff
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
- Connection drop + reconnect → full reload
- Cache staleness exceeds threshold → `Evaluate()` returns `EffectDefaultDeny` for all requests (fail-closed)
- Reload latency benchmark: <50ms target

**Step 2: Implement**

```go
// internal/access/policy/cache.go
package policy

import "sync"

// PolicyCache holds compiled policies in memory with LISTEN/NOTIFY refresh.
type PolicyCache struct {
    mu       sync.RWMutex
    policies []*CachedPolicy
    store    store.PolicyStore
    compiler *PolicyCompiler
}

type CachedPolicy struct {
    Stored   store.StoredPolicy
    Compiled *CompiledPolicy
}

// Snapshot returns a read-only copy of current policies (safe for concurrent use).
func (c *PolicyCache) Snapshot() []*CachedPolicy

// Reload fetches all enabled policies from store, recompiles, and swaps snapshot.
func (c *PolicyCache) Reload(ctx context.Context) error

// Listen subscribes to PostgreSQL NOTIFY on 'policy_changed' channel.
// On notification, triggers Reload(). On reconnect after drop, triggers full Reload().
func (c *PolicyCache) Listen(ctx context.Context, pool *pgxpool.Pool) error
```

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/cache.go internal/access/policy/cache_test.go
git commit -m "feat(access): add policy cache with LISTEN/NOTIFY invalidation"
```

---

### Task 19: Audit logger

**Spec References:** Audit Log Serialization (lines 2161-2192), Audit Log Configuration (lines 2193-2269), Audit Log Retention (lines 2271-2310)

**Acceptance Criteria:**

- [ ] Three audit modes: `off` (system bypasses only), `denials_only`, `all`
- [ ] Mode `off`: only system bypasses logged
- [ ] Mode `denials_only`: denials + default deny + system bypass logged, allows skipped
- [ ] Mode `all`: everything logged
- [ ] **Sync write for denials and system bypasses:** `deny`, `default_deny`, and `system_bypass` events written synchronously to PostgreSQL before `Evaluate()` returns

> **Note:** Denials elevated from spec SHOULD (line 2238) to MUST. Rationale: denial audit integrity is critical for security forensics. The ~1-2ms latency per denial is acceptable given denial events are uncommon in normal operation.

> **Note:** System bypasses use sync path per ADR 66. Rationale: Privileged operations require guaranteed audit trails. System bypasses are rare (server startup, admin maintenance) so sync write cost is negligible. Prevents gaps in audit trail for privilege escalation.

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
- [ ] **WAL monitoring:** `abac_audit_wal_entries` Prometheus gauge tracks current WAL entry count
- [ ] **WAL threshold:** Configurable threshold for WAL size (default 10MB or 10,000 entries) for alerting on persistent DB failures
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

| Effect           | Write Path | Rationale                                        |
|------------------|------------|--------------------------------------------------|
| `deny`           | Sync       | Security forensics — evidence of denials         |
| `default_deny`   | Sync       | Security forensics — evidence of denials         |
| `system_bypass`  | Sync       | Privileged operations — guaranteed audit trail   |
| `allow` (regular)| Async      | Performance — high-volume routine operations     |

`ReplayWAL()` reads JSON-encoded entries from the WAL file, batch-inserts them to PostgreSQL, and truncates the file on success. The server calls this on startup and MAY call it periodically (e.g., every 5 minutes) during recovery.

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/audit/
git commit -m "feat(access): add async audit logger with mode control"
```

---

### Task 19b: Audit log retention and partition management

**Spec References:** Policy Storage > Audit Log Retention (lines 2271-2368)

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

Partition lifecycle (spec lines 2271-2318):

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

**Spec References:** Evaluation Algorithm > Performance Targets (lines 1715-1822) — observability metrics are part of the performance targets section

**Acceptance Criteria:**

- [ ] `abac_evaluate_duration_seconds` histogram recorded after each `Evaluate()`
- [ ] `abac_policy_evaluations_total` counter with `source` (values: seed/lock/admin/plugin) and `effect` labels — avoids unbounded cardinality from admin-created policy names
- [ ] Cardinality concern documented: `source` label preferred over `name` label to prevent metric explosion from admin policy names
- [ ] All metrics reviewed for unbounded label values (no `name`, `subject_id`, `resource_id` labels)
- [ ] `abac_audit_channel_full_total` counter for dropped audit entries
- [ ] `abac_audit_failures_total` counter with `reason` label (see spec Evaluation Algorithm > Performance Targets)
- [ ] `abac_degraded_mode` gauge (0=normal, 1=degraded) (see spec Attribute Resolution > Error Handling for degraded mode)
- [ ] `abac_provider_circuit_breaker_trips_total` counter with `provider` label
- [ ] `abac_property_provider_circuit_breaker_trips_total` counter (PropertyProvider-specific circuit breaker, distinct from general provider metric — see spec line 1283)
- [ ] `abac_provider_errors_total` counter with `namespace` and `error_type` labels
- [ ] `abac_policy_cache_last_update` gauge with Unix timestamp
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
        Help: "Unix timestamp of last policy cache update",
    })
    unregisteredAttributes = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "abac_unregistered_attributes_total",
        Help: "References to unregistered attributes (schema drift indicator)",
    }, []string{"namespace", "key"})
)

// RegisterMetrics registers all ABAC metrics with the given registerer.
func RegisterMetrics(reg prometheus.Registerer) {
    reg.MustRegister(evaluateDuration, policyEvaluations, auditChannelFull,
        auditFailures, degradedMode, providerCircuitBreakerTrips,
        providerErrorsTotal, policyCacheLastUpdate, unregisteredAttributes)
}
```

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/metrics.go internal/access/policy/metrics_test.go
git commit -m "feat(access): add Prometheus metrics for ABAC engine"
```

---

### Task 21: Performance benchmarks

**Spec References:** Evaluation Algorithm > Performance Targets (lines 1715-1822)

**Acceptance Criteria:**

- [ ] `BenchmarkEvaluate_ColdCache` — p99 <10ms
- [ ] `BenchmarkEvaluate_WarmCache` — p99 <5ms
- [ ] `BenchmarkAttributeResolution_Cold` — <2ms
- [ ] `BenchmarkAttributeResolution_Warm` — <100μs
- [ ] `BenchmarkConditionEvaluation` — <1ms per policy
- [ ] `BenchmarkCacheReload` — <50ms
- [ ] `BenchmarkWorstCase_NestedIf` — 32-level nesting <5ms
- [ ] `BenchmarkWorstCase_AllPoliciesMatch` — 50 policies <10ms
- [ ] **`BenchmarkPropertyProvider_ParentLocation`** — recursive CTE with varying depths (1, 5, 10, 20 levels)
- [ ] PropertyProvider benchmark validates 100ms timeout appropriateness
- [ ] PropertyProvider benchmark verifies circuit breaker behavior under load (3 timeouts in 60s)
- [ ] **`BenchmarkProviderStarvation`** — slow first provider consuming ~80ms of 100ms budget, verifies subsequent providers receive cancelled contexts (per spec fair-share timeout requirement)
- [ ] Pure/no-IO microbenchmarks: single-policy evaluation <10μs
- [ ] Pure/no-IO microbenchmarks: 50-policy set evaluation <100μs
- [ ] Pure/no-IO microbenchmarks: attribute resolution <50μs
- [ ] Setup: 50 active policies, 3 operators per condition avg, 10 attributes per entity
- [ ] All benchmarks run without errors

**Files:**

- Create: `internal/access/policy/engine_bench_test.go`

**Step 1: Write benchmarks per spec performance targets (lines 1715-1741)**

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

### Task 21a: Remove @-prefix from command names

> **Note:** The @-prefix exists only in access control permission strings (e.g., `"execute:command:@dig"`), not in actual command registrations. Command validation explicitly rejects `@` as a leading character.

**Spec References:** Replacing Static Roles > Seed Policies (lines 2929-3006) — seed policies reference command names without @ prefix

**Acceptance Criteria:**

- [ ] All command name handling removes @ prefix
- [ ] No `@`-prefixed command names remain in codebase
- [ ] Command lock expressions reference bare command names (e.g., `dig`, `create`, not `@dig`, `@create`)
- [ ] `task test` passes
- [ ] `task lint` passes

> **Verified (2026-02-07):** @-prefixed command names confirmed in `permissions.go` (4), `permissions_test.go` (1), `static_test.go` (4). Total: 9 occurrences. Command validation rejects `@` as leading character — the `@` exists only in permission string encoding, not in actual command names.

> **Note:** 4 of 9 @-prefixed name occurrences are in `static_test.go` which is deleted by Task 29 ([Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)). Only 5 occurrences in `permissions.go` and `permissions_test.go` need modification in this task.

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
git add .
git commit -m "refactor(commands): remove @ prefix from command names"
```

---


---

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)** | **[Next: Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)**

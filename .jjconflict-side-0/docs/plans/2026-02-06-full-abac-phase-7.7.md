<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 7.7: Resilience, Observability & Integration Tests

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)**

## Task 30: Integration tests for full ABAC flow

**Spec References:** [Testing Strategy](../specs/abac/08-testing-appendices.md#testing-strategy), [Integration Tests](../specs/abac/08-testing-appendices.md#integration-tests-ginkgogomega), ADR 0011 (Deny-overrides), ADR 0013 (Properties)

**Dependencies:**

- Task 23b (Phase 7.4) — seed validation must be in place before integration tests exercise seed policy behavior

**Acceptance Criteria:**

- [ ] Ginkgo/Gomega BDD-style tests with `//go:build integration` tag
- [ ] testcontainers for PostgreSQL (pattern from `test/integration/world/`)
- [ ] Seed policy behavior: self-access, location read, co-location, admin full access, deny-overrides, default deny
- [ ] Property visibility: public co-located, private owner-only, admin-only, restricted with visible\_to
- [ ] **Canary test (full ABAC evaluation path):** End-to-end test covering subject resolution, attribute resolution, policy evaluation, deny-overrides, and audit logging with seed policies → validates complete system integration
- [ ] Re-entrance guard: synchronous re-entry panics, goroutine-based re-entry NOT detected ([01-core-types.md#attribute-providers](../specs/abac/01-core-types.md#attribute-providers), prevented by convention)
- [ ] Cache invalidation: NOTIFY after create, NOTIFY after delete → cache reloads
- [ ] Cache invalidation: Policy UPDATE operations trigger pg_notify and cache invalidation (not just CREATE/DELETE). All three CRUD operations verified.
- [ ] Cache invalidation: Multi-step commands (e.g., "go north && look") invalidate cache between steps to prevent stale permission data from persisting across evaluations within same request
- [ ] Audit logging: denials\_only mode, all mode, minimal mode
- [ ] ~~Lock system: apply lock → permit policy, remove lock → allow~~ — deferred to Epic 8 (Phase 7.5 dependency)
- [ ] All integration tests pass: `go test -race -v -tags=integration ./test/integration/access/...`

**Files:**

- Create: `test/integration/access/access_suite_test.go`
- Create: `test/integration/access/evaluation_test.go`
- Create: `test/integration/access/seed_policies_test.go`
- Create: `test/integration/access/property_visibility_test.go`

**Step 1: Write Ginkgo/Gomega integration tests**

Use testcontainers for PostgreSQL (pattern from `test/integration/world/world_suite_test.go`).

**Benchmark scenario:** Include a cache thrashing test case with high-cardinality policy sets (e.g., 10,000 unique policy sets rotating through a 256-entry LRU cache) to verify performance under cache pressure.

```go
//go:build integration

var _ = Describe("Access Policy Engine", func() {
    Describe("Seed policy behavior", func() {
        It("allows players to read their own character", func() { })
        It("allows players to read their current location", func() { })
        It("denies players reading other locations", func() { })
        It("allows admins full access", func() { })
        It("denies when forbid and permit both match (deny-overrides)", func() { })
        It("denies when no policy matches (default deny)", func() { })
    })

    Describe("Property visibility", func() {
        It("allows public property read by co-located character", func() { })
        It("denies public property read by distant character", func() { })
        It("allows private property read by owner only", func() { })
        It("allows admin property read by admin only", func() { })
        It("handles restricted visibility with visible_to list", func() { })
    })

    Describe("Re-entrance guard", func() {
        It("panics when provider calls Evaluate() synchronously", func() { })
        It("does NOT detect goroutine-based re-entry (prevented by convention, not runtime checks)", func() { })
    })

    Describe("Cache invalidation", func() {
        It("reloads policies when NOTIFY fires after create", func() { })
        It("reloads policies when NOTIFY fires after delete", func() { })
        It("invalidates cache between multi-step command evaluations to prevent stale permissions", func() {
            // Test scenario: "go north && look" where first command changes permissions
            // that affect second command. Verify second evaluation sees updated state.
        })
    })

    Describe("Audit logging", func() {
        It("logs denials in denials_only mode", func() { })
        It("logs everything in all mode", func() { })
        It("only logs system bypasses in minimal mode", func() { })
    })

    // Lock system tests deferred to Epic 8 (Phase 7.5 dependency)
    // Describe("Lock system", func() {
    //     It("applies lock to resource via permit policy", func() { })
    //     It("removes lock via unlock command", func() { })
    // })

    Describe("Session resolution", func() {
        It("denies access when session is expired", func() { })
        It("denies access when character is deleted", func() { })
        It("denies access when database fails during session lookup", func() { })
    })
})
```

**Step 2: Run integration tests**

Run: `go test -race -v -tags=integration ./test/integration/access/...`
Expected: PASS

**Step 3: Commit**

```bash
git add test/integration/access/
git commit -m "test(access): add ABAC integration tests with seed policies and property visibility"
```

---

### Task 31: Degraded mode implementation

**Spec References:** [Degraded Mode](../specs/abac/04-resolution-evaluation.md#error-handling) (Security note section)

**Dependencies:**

- Task 17.4 (Phase 7.3) — engine must be operational before degraded mode can be added

**Acceptance Criteria:**

- [ ] Engine MUST enter degraded mode when corrupted forbid/deny policy detected
- [ ] Engine MUST auto-disable corrupted permit policies (set enabled=false in DB) without entering degraded mode
- [ ] Degraded mode flag (`abac_degraded_mode` boolean) persists until administratively cleared
- [ ] In degraded mode: all `Evaluate()` calls return `EffectDefaultDeny` without policy evaluation
- [ ] In degraded mode: log CRITICAL level message on every evaluation attempt
- [ ] Corrupted policy detection: unmarshal `compiled_ast` fails or structural invariants violated
- [ ] Only forbid/deny policies trigger degraded mode (permit policies auto-disabled instead)
- [ ] Degraded mode clearing in Epic 7 follows [Decision #96](../specs/decisions/epic7/general/096-defer-phase-7-5-to-epic-8.md): use temporary operational recovery (direct DB flag reset or server restart) until `policy clear-degraded-mode` ships in Epic 8 (Task 27b-3)
- [ ] Policy reload command MUST bypass ABAC, restricted to local/system callers, with test for stale cache recovery (references ADR #107 and security requirement S5 from holomush-5k1.346)
- [ ] Prometheus gauge `abac_degraded_mode` (0=normal, 1=degraded) exported (already added to Task 19 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)))
- [ ] All tests pass via `task test`

**Degraded Mode Triggers:**

| Trigger                          | Cause                                                | Recovery                                                             |
| -------------------------------- | ---------------------------------------------------- | -------------------------------------------------------------------- |
| Compile-time corruption (forbid) | `compiled_ast` unmarshal fails on forbid policy load | Disable corrupted policy, then clear degraded mode via DB flag reset or server restart |
| Compile-time corruption (permit) | `compiled_ast` unmarshal fails on permit policy load | Policy auto-disabled, normal evaluation continues (no degraded mode) |
| Runtime evaluation error         | AST structural invariants violated during evaluation | Disable corrupted policy; if forbid/deny, clear degraded mode via DB flag reset or server restart |
| Transient database error         | PostgreSQL unavailable during policy load            | Fix DB connectivity, restart server or reload policies               |

**Files:**

- Modify: `internal/access/policy/engine.go` (add degraded mode state and check)
- Test: `internal/access/policy/engine_test.go`

**TDD Test List:**

- Engine detects corrupted forbid policy → enters degraded mode
- Engine detects corrupted deny policy → enters degraded mode
- Engine detects corrupted permit policy → auto-disables policy (set enabled=false), logs ERROR, no degraded mode
- In degraded mode → all evaluations return default deny
- In degraded mode → CRITICAL log on every evaluation
- In degraded mode → audit entry written with reason='degraded_mode' and effect default_deny
- Auto-disabled permit policy → subsequent loads skip disabled policy, normal evaluation continues
- Clear degraded mode via DB flag reset or server restart (Decision #96 workaround) → normal evaluation resumes
- Degraded mode gauge metric → 0 when normal, 1 when degraded

---

### Task 32: Schema evolution on plugin reload

**Spec References:** [Schema Evolution on Plugin Reload](../specs/abac/04-resolution-evaluation.md#schema-evolution-on-plugin-reload)

**Dependencies:**

- Task 7 (Phase 7.1) — PolicyStore interface needed for schema evolution policy scanning

**Acceptance Criteria:**

- [ ] When plugin reloaded, compare new schema against previous schema version
- [ ] Attribute added → INFO log, no action required
- [ ] Attribute type changed → WARN log, note existing policies may break
- [ ] Attribute removed → WARN log, scan policies for references, mark affected policies for review
- [ ] Attribute removal handling: scan all enabled policies' `dsl_text` for removed attribute keys, log references at WARN level, mark affected policies with metadata flag for administrator review (soft-delete approach — attribute removal doesn't block reload, but triggers operator notification)
- [ ] Namespace removed → ERROR log, scan policies for references, reject reload if policies reference it
- [ ] Schema change detection: compare attribute keys, types, namespaces between old and new schemas
- [ ] Policy reference scan: grep all enabled policies' `dsl_text` for removed namespace or attribute keys
- [ ] All tests pass via `task test`

**Files:**

- Modify: `internal/access/policy/schema_registry.go` (add schema versioning and comparison)
- Test: `internal/access/policy/schema_registry_test.go`

**TDD Test List:**

- Plugin reload with added attribute → INFO log, no error
- Plugin reload with changed attribute type → WARN log, no error
- Plugin reload with removed attribute → WARN log, scan policies, mark affected policies for review
- Plugin reload with removed namespace → ERROR, reject if policies reference it
- Schema comparison detects all change types correctly
- Policy scan correctly identifies references to removed attributes

---

### Task 33: Lock tokens discovery command — **BLOCKED: Deferred to Epic 8**

> **Blocked:** This task depends on Task 24 (Lock token registry) which is part of Phase 7.5,
> deferred to Epic 8 ([Decision #96](../specs/decisions/epic7/general/096-defer-phase-7-5-to-epic-8.md)).
> Task 33 cannot proceed until Task 24 is implemented in Epic 8.

**Spec References:** [Lock Token Discovery](../specs/abac/06-layers-commands.md#lock-token-discovery)

**Dependencies:**

- Task 24 (Phase 7.5) — lock registry must exist before discovery command can query it (deferred to Epic 8)

**Acceptance Criteria:**

- [ ] `lock tokens` command → lists all registered lock tokens (faction, flag, level, etc.)
- [ ] `lock tokens --namespace character` → filters to specific namespace
- [ ] `lock tokens --verbose` → shows underlying DSL attribute path for each token (for debugging)
- [ ] Display format: token name, type, description, example
- [ ] Discovery sources: schema registry (plugin-provided attributes) + core tokens (faction, flag, level, role)
- [ ] All tests pass via `task test`

**Files:**

- Modify: `internal/command/handlers/lock.go`
- Test: `internal/command/handlers/lock_test.go`

**TDD Test List:**

- `lock tokens` → lists all available lock tokens
- `lock tokens --namespace character` → filters to character namespace
- `lock tokens --verbose` → shows DSL attribute path
- Core tokens (faction, flag, level, role) always present
- Plugin-provided attributes appear in token list
- Display format includes name, type, description, example

---

### Task 34: General provider circuit breaker

**Spec References:** [Provider Circuit Breaker](../specs/abac/04-resolution-evaluation.md#circuit-breaker-summary)

**Dependencies:**

- Task 14 (Phase 7.3) — resolver must exist before circuit breaker logic can be added to it

> **Note:** This task's circuit breaker also covers PropertyProvider (formerly a
> separate circuit breaker in Task 16b). See [Decision #74](../specs/decisions/epic7/phase-7.7/074-unified-circuit-breaker-task-34.md).
>
> **Note:** The spec defines two circuit breaker parameter sets: a SHOULD-level simpler version (10 consecutive errors, 30s open) and a MUST-level budget-utilization version (>80% budget in >50% of calls). This task implements the MUST-level version as it provides better detection of chronic performance degradation vs. hard failures. The simpler parameters are subsumed by the budget-utilization approach.

**Acceptance Criteria:**

- [ ] Track budget utilization per provider over 60-second rolling window
- [ ] Trigger: >80% budget utilization in >50% of calls (minimum 10 calls) over 60-second rolling window → circuit opens
- [ ] Circuit open → skip provider for 60s, return empty attributes
- [ ] Log at WARN level: "Provider {name} circuit breaker opened: budget utilization threshold exceeded"
- [ ] Prometheus counter `abac_provider_circuit_breaker_trips_total{provider="name"}` incremented on trip
- [ ] After 60s → half-open state with single probe request
- [ ] Probe success → close circuit (INFO log); probe failure → re-open for another 60s
- [ ] Circuit-opened providers do NOT consume evaluation time budget (immediate skip, no I/O)
- [ ] Fair-share timeout calculation excludes circuit-opened providers from remaining provider count
- [ ] Provider metrics endpoint `/debug/abac/providers` shows: call counts, avg latency, timeout rate, circuit status
- [ ] PropertyProvider circuit breaker behavior covered (replaces Task 16b's deferred circuit breaker)
- [ ] All tests pass via `task test`

**Files:**

- Modify: `internal/access/policy/attribute/resolver.go` (add general circuit breaker logic)
- Test: `internal/access/policy/attribute/resolver_test.go`

**TDD Test List:**

- 80% budget utilization in >50% of calls (minimum 10 calls) over 60-second rolling window → circuit opens
- Circuit open → provider skipped for 60s, empty attributes returned
- WARN log on circuit open with provider name and budget utilization details
- Prometheus counter incremented on trip
- After 60s → half-open probe sent (single request)
- Probe success → circuit closes, normal operation resumes (INFO log)
- Probe failure → circuit re-opens for another 60s
- Skipping circuit-opened provider does not consume evaluation budget
- Fair-share timeout excludes circuit-opened providers from denominator
- Metrics endpoint shows circuit status

---

### Task 35: Property orphan cleanup goroutine

**Spec References:** [05-storage-audit.md#schema](../specs/abac/05-storage-audit.md#schema) — entity_properties lifecycle section

> **Note:** This task was moved from Phase 7.1 (Task 4c) because orphan cleanup is a resilience concern, not a core schema concern. Cascade deletion remains in Phase 7.1.

**Dependencies:**

- Task 4c (Phase 7.1) — cascade deletion logic must exist before orphan cleanup can build on it

**Acceptance Criteria:**

- [ ] Orphan cleanup goroutine: runs on configurable timer (default: daily) to detect orphaned properties (parent entity no longer exists)
- [ ] Orphan cleanup: detected orphans logged at WARN level on first discovery
- [ ] Orphan cleanup: configurable grace period (default: 24h, configured via `world.orphan_grace_period` in server YAML)
- [ ] Orphan cleanup: orphans persisting across two consecutive runs are actively deleted with batch `DELETE` and logged at INFO level with count
- [ ] Startup integrity check: count orphaned properties on server startup
- [ ] Startup integrity check: if orphan count exceeds configurable threshold (default: 100), log at ERROR level but continue starting (not fail-fast)
- [ ] Integration tests with real database: entity deletion (character/location/object) cascades to entity_properties table (verifies Task 4c cascade deletion with PostgreSQL)
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/world/property_lifecycle.go` (orphan cleanup goroutine, startup integrity check)
- Test: `internal/world/property_lifecycle_test.go` (orphan cleanup tests)

**TDD Test List:**

- Background goroutine runs on configurable timer (default: daily)
- Detected orphans logged at WARN level on first discovery
- Grace period configurable via YAML (default: 24h)
- Orphans persisting across two consecutive runs are deleted
- Batch DELETE logged at INFO level with count
- Startup integrity check counts orphaned properties
- If orphan count exceeds threshold (default: 100), log at ERROR but continue starting
- No fail-fast on orphan detection (resilience requirement)

**Step 1: Write failing tests**

- Orphan cleanup goroutine runs on timer
- Orphans logged at WARN on first detection
- Grace period enforced before deletion
- Batch deletion after grace period
- Startup integrity check logs orphan count
- Threshold-based ERROR logging

**Step 2: Implement orphan cleanup**

Create `internal/world/property_lifecycle.go`:

```go
// StartPropertyLifecycleManager starts background goroutine for orphan cleanup
func (s *WorldService) StartPropertyLifecycleManager(ctx context.Context, interval time.Duration) {
    go s.cleanupOrphansLoop(ctx, interval)
}

func (s *WorldService) cleanupOrphansLoop(ctx context.Context, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if err := s.cleanupOrphanedProperties(ctx); err != nil {
                errutil.LogError(s.logger, "orphan cleanup failed", err)
            }
        }
    }
}

func (s *WorldService) cleanupOrphanedProperties(ctx context.Context) error {
    // Query for properties where parent entity no longer exists
    // Delete orphaned properties after grace period
    // Log count at WARN level if orphans found
    // Log count at INFO level after deletion
}

func (s *WorldService) StartupIntegrityCheck(ctx context.Context) error {
    orphanCount, err := s.countOrphanedProperties(ctx)
    if err != nil {
        return err
    }
    if orphanCount > 100 {
        s.logger.Error("orphaned properties exceed threshold", "count", orphanCount, "threshold", 100)
    } else if orphanCount > 0 {
        s.logger.Warn("orphaned properties detected", "count", orphanCount)
    }
    return nil
}
```

**Step 3: Run tests, commit**

```bash
task test
git add internal/world/property_lifecycle.go internal/world/property_lifecycle_test.go
git commit -m "feat(world): add property orphan cleanup goroutine and startup integrity check"
```

---

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)**

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 7.7: Resilience, Observability & Integration Tests

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)**

## Task 30: Integration tests for full ABAC flow

**Spec References:** Testing Strategy > Integration Tests (lines 3370-3405), ADR 0011 (Deny-overrides), ADR 0013 (Properties)

**Acceptance Criteria:**

- [ ] Ginkgo/Gomega BDD-style tests with `//go:build integration` tag
- [ ] testcontainers for PostgreSQL (pattern from `test/integration/world/`)
- [ ] Seed policy behavior: self-access, location read, co-location, admin full access, deny-overrides, default deny
- [ ] Property visibility: public co-located, private owner-only, admin-only, restricted with visible\_to
- [ ] Re-entrance guard: synchronous re-entry panics, goroutine-based re-entry NOT detected ([01-core-types.md#attribute-providers](../specs/abac/01-core-types.md#attribute-providers), was spec lines 612-620, prevented by convention)
- [ ] Cache invalidation: NOTIFY after create, NOTIFY after delete → cache reloads
- [ ] Cache invalidation: Policy UPDATE operations trigger pg_notify and cache invalidation (not just CREATE/DELETE). All three CRUD operations verified.
- [ ] Audit logging: denials\_only mode, all mode, off mode
- [ ] Lock system: apply lock → permit policy, remove lock → allow
- [ ] All integration tests pass: `go test -race -v -tags=integration ./test/integration/access/...`

**Files:**

- Create: `test/integration/access/access_suite_test.go`
- Create: `test/integration/access/evaluation_test.go`
- Create: `test/integration/access/seed_policies_test.go`
- Create: `test/integration/access/property_visibility_test.go`

**Step 1: Write Ginkgo/Gomega integration tests**

Use testcontainers for PostgreSQL (pattern from `test/integration/world/world_suite_test.go`):

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
        It("does NOT detect goroutine-based re-entry (01-core-types.md#attribute-providers, was spec lines 612-620: prevented by convention, not runtime checks)", func() { })
    })

    Describe("Cache invalidation", func() {
        It("reloads policies when NOTIFY fires after create", func() { })
        It("reloads policies when NOTIFY fires after delete", func() { })
    })

    Describe("Audit logging", func() {
        It("logs denials in denials_only mode", func() { })
        It("logs everything in all mode", func() { })
        It("only logs system bypasses in off mode", func() { })
    })

    Describe("Lock system", func() {
        It("applies lock to resource via permit policy", func() { })
        It("removes lock via unlock command", func() { })
    })

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

**Spec References:** Degraded Mode (lines 1660-1683)

**Acceptance Criteria:**

- [ ] Engine MUST enter degraded mode when corrupted forbid/deny policy detected
- [ ] Engine MUST auto-disable corrupted permit policies (set enabled=false in DB) without entering degraded mode
- [ ] Degraded mode flag (`abac_degraded_mode` boolean) persists until administratively cleared
- [ ] In degraded mode: all `Evaluate()` calls return `EffectDefaultDeny` without policy evaluation
- [ ] In degraded mode: log CRITICAL level message on every evaluation attempt
- [ ] Corrupted policy detection: unmarshal `compiled_ast` fails or structural invariants violated
- [ ] Only forbid/deny policies trigger degraded mode (permit policies auto-disabled instead)
- [ ] `policy clear-degraded-mode` command clears flag and resumes normal evaluation (implemented in Task 27b ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)))
- [ ] Prometheus gauge `abac_degraded_mode` (0=normal, 1=degraded) exported (already added to Task 19 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)))
- [ ] All tests pass via `task test`

**Degraded Mode Triggers:**

| Trigger                          | Cause                                                   | Recovery                                                                |
| -------------------------------- | ------------------------------------------------------- | ----------------------------------------------------------------------- |
| Compile-time corruption (forbid) | `compiled_ast` unmarshal fails on forbid policy load    | Disable corrupted policy, run `policy clear-degraded-mode`              |
| Compile-time corruption (permit) | `compiled_ast` unmarshal fails on permit policy load    | Policy auto-disabled, normal evaluation continues (no degraded mode)    |
| Runtime evaluation error         | AST structural invariants violated during evaluation    | Disable corrupted policy, run `policy clear-degraded-mode` if forbid    |
| Transient database error         | PostgreSQL unavailable during policy load               | Fix DB connectivity, restart server or reload policies                  |

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
- Clear degraded mode → normal evaluation resumes
- Degraded mode gauge metric → 0 when normal, 1 when degraded

---

### Task 32: Schema evolution on plugin reload

**Spec References:** Schema Evolution on Plugin Reload (lines 1497-1556)

**Acceptance Criteria:**

- [ ] When plugin reloaded, compare new schema against previous schema version
- [ ] Attribute added → INFO log, no action required
- [ ] Attribute type changed → WARN log, note existing policies may break
- [ ] Attribute removed → WARN log, scan policies for references
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
- Plugin reload with removed attribute → WARN log, scan policies
- Plugin reload with removed namespace → ERROR, reject if policies reference it
- Schema comparison detects all change types correctly
- Policy scan correctly identifies references to removed attributes

---

### Task 33: Lock tokens discovery command

**Spec References:** Lock Token Discovery (lines 2668-2693)

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

**Spec References:** Provider Circuit Breaker (lines 1594-1622, 1884-1900)

> **Note:** This task's circuit breaker also covers PropertyProvider (formerly a
> separate circuit breaker in Task 16b). See [Decision #74](../specs/decisions/epic7/phase-7.7/074-unified-circuit-breaker-task-34.md).

> **Note:** The spec defines two circuit breaker parameter sets: a SHOULD-level simpler version (lines 1598-1602: 10 consecutive errors, 30s open) and a MUST-level budget-utilization version (lines 1884-1900: >80% budget in >50% of calls). This task implements the MUST-level version as it provides better detection of chronic performance degradation vs. hard failures. The simpler parameters from lines 1598-1602 are subsumed by the budget-utilization approach.

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

### Task 35: CLI flag --validate-seeds (MOVED to Task 23b ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)))

> **Note:** This task was moved to Phase 7.4 (Task 23b ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md))) to enable CI validation during later phases. See Task 23b ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)) for implementation details.

**Rationale:** `--validate-seeds` only depends on the DSL compiler (Task 12 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md))) and seed policy definitions (Task 22 ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md))), both available after Phase 7.4. Moving it earlier allows CI pipelines to validate seed policies during later phase development, catching compilation errors sooner.

---

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)**

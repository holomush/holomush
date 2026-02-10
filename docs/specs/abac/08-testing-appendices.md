<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

## Testing Strategy

### Unit Tests

```text
internal/access/policy/dsl/
  parser_test.go        — Parse valid/invalid DSL, verify AST
  evaluator_test.go     — Evaluate conditions against attribute bags
                          Table-driven: each operator, edge cases,
                          missing attributes, type mismatches

internal/access/policy/
  engine_test.go        — Full evaluation flow with mock providers
                          System bypass, deny-overrides, default deny,
                          provider errors, cache invalidation

internal/access/policy/attribute/
  resolver_test.go      — Orchestrates multiple providers, timeout enforcement
  character_test.go     — Resolves character attrs from mock world service
  location_test.go      — Resolves location attrs from mock world service
  property_test.go      — Resolves property attrs including visibility/lists
  environment_test.go   — Time, maintenance mode

internal/access/policy/store/
  postgres_test.go      — CRUD, versioning, LISTEN/NOTIFY dispatch

internal/access/policy/audit/
  logger_test.go        — Mode control (minimal/denials_only/all), attribute snapshots
```

### DSL Evaluator Coverage

Table-driven tests MUST cover every operator with valid inputs, invalid inputs,
missing attributes, and type mismatches.

### Fuzz Testing

The DSL parser MUST include fuzz tests using Go's native fuzzing (`go test
-fuzz`). Parser bugs are security bugs — a malformed input that causes a panic,
infinite loop, or incorrect parse result could grant or deny access incorrectly.

```go
func FuzzParseDSL(f *testing.F) {
    // Seed with valid and near-valid policies
    f.Add(`permit(principal, action, resource);`)
    f.Add(`forbid(principal is character, action in ["read"], resource is location)
        when { principal.level >= 5 };`)
    f.Add(`permit(principal, action, resource) when { if principal has faction
        then principal.faction != "enemy" else false };`)
    f.Fuzz(func(t *testing.T, input string) {
        // Must not panic, must terminate within timeout
        _, _, _ = compiler.Compile(input)
    })
}
```

Fuzz targets SHOULD also cover the evaluator (random ASTs against random
attribute bags) and the lock expression parser. CI SHOULD run a short fuzz
cycle (30 seconds) on every build; extended fuzzing (hours) SHOULD run as a
scheduled nightly job. Crash-inducing inputs discovered by the scheduled
fuzzer MUST be added as regression test cases in the unit test suite.

**Fuzz corpus strategy:**

- **Seeds:** All example policies from this spec + all seed policies + known
  edge cases (empty input, max nesting, Unicode identifiers, reserved words
  as attribute names)
- **Structured mutation:** Valid DSL with randomized attribute paths, random
  operators, random nesting depths (up to 32), and random literal values
- **Crash storage:** Crash-inducing inputs MUST be stored in the Go standard
  fuzz corpus directory `testdata/fuzz/<FuzzTestName>/` (e.g.,
  `testdata/fuzz/FuzzParseDSL/`) per Go 1.18+ conventions. Each crash input
  is saved as a separate file in the corpus. CI **MUST** include a regression
  test step that runs `go test -fuzz=. -fuzztime=1x` to verify all corpus
  entries pass without panics or hangs before merging code changes.
- **Coverage target:** Fuzzing SHOULD achieve >80% code coverage of the
  parser and evaluator packages before being considered sufficient

### Integration Tests (Ginkgo/Gomega)

```go
Describe("AccessPolicyEngine", func() {
    Describe("Policy evaluation with real PostgreSQL", func() {
        It("denies by default when no policies exist", func() { ... })
        It("allows when a permit policy matches", func() { ... })
        It("deny overrides permit", func() { ... })
        It("resolves character attributes from world model", func() { ... })
        It("handles property visibility with visible_to lists", func() { ... })
        It("plugin attribute providers contribute to evaluation", func() { ... })
    })

    Describe("Re-entrance guard", func() {
        It("panics when provider calls Evaluate() synchronously", func() { ... })
        It("detects same-goroutine re-entry attempts", func() { ... })
    })

    Describe("Lock-generated policies", func() {
        It("creates a scoped policy from lock syntax", func() { ... })
        It("rejects locks on resources without write access", func() { ... })
        It("admin forbid overrides player lock permit", func() { ... })
    })

    Describe("Audit logging", func() {
        It("logs denials in denials_only mode", func() { ... })
        It("logs all decisions in all mode", func() { ... })
        It("logs system bypasses + denials in minimal mode", func() { ... })
    })

    Describe("Cache invalidation via LISTEN/NOTIFY", func() {
        It("reloads policies when notification received", func() { ... })
    })
})
```

## Known Limitations — Testing

### Property Location Resolution Transaction Consistency

At `READ COMMITTED` isolation level, the `PropertyProvider`'s recursive CTE
query and the `CharacterProvider`'s subject attribute resolution run in
**separate transactions**. This means attribute resolution does not provide
point-in-time snapshot consistency across providers.

**Scenario:** A character holding an object moves from room A to room B while
an authorization check is in progress. The check may observe:

- Character location resolved from room B (new state)
- Object's `parent_location` chain still showing room A (old state)

This creates a brief inconsistency where the character and their held object
appear to be in different locations during policy evaluation.

**Why this is acceptable:**

1. **Low frequency:** MUSH character movement is infrequent (seconds to minutes
   between moves), making the race condition window rare in practice.
2. **Bounded window:** The 100ms `PropertyProvider` query timeout limits the
   maximum duration of inconsistency. Most queries complete in <10ms.
3. **Fail-safe behavior:** Authorization policies that depend on
   location-hierarchy containment (e.g., "can access object if it's in your
   location") will fail-safe to deny when inconsistencies occur, erring on the
   side of security.
4. **Circuit breaker protection:** The `PropertyProvider` circuit breaker (5
   timeouts in 60s) prevents systematic timeout issues from overwhelming the
   database, treating the operational symptom even if not the consistency root
   cause.

**What this means:** The system provides **eventual consistency** for property
location resolution, not strict snapshot consistency. Authorization decisions
reflect a consistent view within each provider's transaction, but not
necessarily across all providers involved in a single `Evaluate()` call.

### Attribute Schema Versioning

Plugins that change attribute types (e.g., `reputation.score` from number to
string) could silently break existing policies. Full schema versioning—where
policies declare minimum schema versions and the engine enforces
compatibility—would prevent this.

**Deferred to future iteration.** The existing schema evolution infrastructure
(registration-time validation, reload-time schema comparison, runtime warnings
for unregistered attributes) is sufficient for MVP. With few plugins and active
development, type changes trigger immediate warnings visible to operators.
Manual policy updates are acceptable at this scale. Policy-level schema version
pinning adds complexity without proportional benefit until the plugin ecosystem
grows.

## Acceptance Criteria

- [ ] ABAC policy data model documented (subjects, resources, actions, conditions)
- [ ] Attribute schema defined for subjects (players, plugins, connections)
- [ ] Attribute schema defined for resources (objects, rooms, commands, properties)
- [ ] Environment attributes defined (time, maintenance mode)
- [ ] Policy DSL grammar specified with full expression language
- [ ] Policy storage format designed (PostgreSQL schema with versioning)
- [ ] Policy evaluation algorithm documented (deny-overrides, no priority)
- [ ] Audit log with configurable modes (minimal, denials-only, all)
- [ ] Plugin attribute contribution interface designed (registration-based)
- [ ] Admin commands documented for policy management
- [ ] Player lock system designed with write-access verification
- [ ] Lock syntax compiles to scoped policies
- [ ] Property model designed as first-class entities
- [ ] Direct replacement of StaticAccessControl documented (no adapter)
- [ ] Microbenchmarks (pure evaluator, no I/O): single-policy <10μs,
      50-policy set <100μs, attribute resolution <50μs (via `go test -bench`)
- [ ] Integration benchmarks (with real providers): `Evaluate()` p99 <10ms cached,
      <25ms cold, matching the performance targets in the spec
- [ ] Cache invalidation via LISTEN/NOTIFY reloads policies on change
- [ ] System subject bypass returns allow without policy evaluation
- [ ] Subject type prefix-to-DSL-type mapping documented
- [ ] Provider timeout and operational limits defined

## Future Commands (Deferred)

The following commands are not part of the MVP but are natural extensions of the
policy management system:

- **`policy diff <name> [<version>]`** — Show what changed between policy
  versions. Useful for audit investigation. Reads from `access_policy_versions`.
- **`policy export [--source=X]`** — Export policies as DSL text files for
  backup and environment promotion (dev → staging). The deterministic naming
  convention already supports round-trip export/import.
- **`policy import <file>`** — Import policies from a DSL text file. Validates
  each policy via `PolicyCompiler` before persisting. Existing policies with the
  same name are skipped unless `--overwrite` is specified.
- **`policy lint [<name>]`** — Semantic analysis of policies for common
  mistakes. Unlike `policy validate` (syntax only), lint checks for: negation
  without `has` guard (`principal.faction != "enemy"` without
  `principal has faction`), overly broad `forbid` policies without conditions,
  and `permit` policies logically subsumed by existing policies. Available to
  admins and builders.

## References

- [Design Decision Log](../decisions/epic7/README.md) — Rationale
  for key design choices made during review
- [Core Access Control Design](../2026-01-21-access-control-design.md) — Current
  static role implementation (Epic 3)
- [HoloMUSH Roadmap](../../plans/2026-01-18-holomush-roadmap-design.md) — Epic 7
  definition
- [Cedar Language Specification](https://docs.cedarpolicy.com/) — DSL inspiration
- [Commands & Behaviors Design](../2026-02-02-commands-behaviors-design.md) —
  Command system integration

### Related ADRs

- [ADR 0009: Policy Engine Approach](../decisions/epic7/general/001-policy-engine-approach.md)
- [ADR 0010: Cedar-Aligned Missing Attribute Semantics](../decisions/epic7/phase-7.3/028-cedar-aligned-missing-attribute-semantics.md)
- [ADR 0011: Conflict Resolution](../decisions/epic7/general/004-conflict-resolution.md)
- [ADR 0012: Attribute Resolution Strategy](../decisions/epic7/general/003-attribute-resolution-strategy.md)
- [ADR 0013: Property Model](../decisions/epic7/phase-7.1/009-property-model.md)
- [ADR 0014: Direct Replacement (No Adapter)](../decisions/epic7/phase-7.6/036-direct-replacement-no-adapter.md)
- [ADR 0015: Player Access Control Layers](../decisions/epic7/phase-7.1/012-player-access-control-layers.md)
- [ADR 0016: Cache Invalidation](../decisions/epic7/phase-7.3/011-cache-invalidation.md)

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Property Authz Redesign — Design Spec (holomush-72ou)

**Date:** 2026-05-22
**Status:** Draft
**Design bead:** holomush-rmsi
**Implementation bead:** holomush-72ou
**Authors:** HoloMUSH Contributors
**Related:**

- ADR [holomush-iv43](../../adr/holomush-iv43-cedar-aligned-fail-safe-type-semantics.md) — DSL fail-safe semantics (parent contract)
- ADR [holomush-ti1b](../../adr/holomush-ti1b-providers-omit-optional-attrs.md) — providers omit optional attrs
- bead `holomush-g776` — LocationProvider wiring (template for the wiring-and-WARN pattern)
- bead `holomush-k3ud` — ObjectProvider wiring (template)
- bead `holomush-o8g6` — `parseEntityResource` parser unification
- Property model spec: [`docs/specs/abac/03-property-model.md`](../../specs/abac/03-property-model.md)

## Background

The PropertyProvider at `internal/access/policy/attribute/property.go` was designed to resolve attributes for a single property by its ULID — matching the per-property authz model in `docs/specs/abac/03-property-model.md`. The six property visibility seeds in `internal/access/policy/seed.go:108-141` gate on per-property attributes (`visibility`, `owner`, `parent_location`, `visible_to`, `excluded_from`).

The bug: `Service.ListPropertiesByParent` at `internal/world/service.go:1068` emits a PARENT-shaped resource (`property:<parentType>:<parentID>`) and performs a SINGLE ABAC check against it. This shape never matches any property seed — every call default-denies.

The bug is currently **dormant**. `Service.ListPropertiesByParent` has no production caller today: the Go web/grpc layer does not invoke it, and the Lua `PropertyCapability` at `internal/plugin/hostfunc/cap_property.go` is wired only from tests. The fix is preventive — when a feature surface lands (plugin command, web UI, etc.) the per-property authz must already work.

## Goals

1. `Service.ListPropertiesByParent` returns only properties the principal can read, per the spec's per-property authz model.
2. `PropertyProvider` is registered in `BuildABACStack` so the six visibility seeds evaluate against real per-property attrs. The seed-coverage validator (`holomush-xxel`) and its drift-detector pass post-wiring.
3. Per-property `parent_location` resolves correctly for all three parent types (`location` / `character` / `object`) including the recursive object-containment chain.

## Non-goals

- New `Service.GetProperty/CreateProperty/UpdateProperty/DeleteProperty` API surface (no caller needs them yet; deferred to follow-up).
- Circuit breaker (`03-property-model.md:208-218`) and `statement_timeout` operational hardening (dormant surface; deferred).
- `FindPropertyByPrefix` Service implementation (off critical path).
- E2E tests (no UX surface exercises this path today; file with the future feature-surface bead).

## Architecture

The fix is concentrated in three places: the caller, the wiring, and the resolver implementation.

### Component changes

| Component | File | Change |
| --------- | ---- | ------ |
| `Service.ListPropertiesByParent` | `internal/world/service.go:1062-1077` | Replace single parent-shaped ABAC check with `propertyRepo.ListByParent` → per-property loop: `s.checkAccess(ctx, subjectID, "read", access.PropertyResource(prop.ID.String()), prefixProperty)`. Outcome per call: nil → property included; `errors.Is(err, ErrPermissionDenied)` → property filtered out SILENTLY (normal default-deny — most properties on another principal are correctly hidden); `errors.Is(err, ErrAccessEvaluationFailed)` → propagate the wrapped error to abort the whole call (infra failure MUST be visible to the caller, NOT silently masked as "no visible properties"). `propertyRepo.ListByParent` errors propagate. |
| `ABACConfig` | `internal/access/setup/setup.go` | Add `PropertyRepo world.PropertyRepository` and `ParentLocationResolver attribute.ParentLocationResolver` fields. |
| `BuildABACStack` | `internal/access/setup/setup.go` | New section `8d. Property provider`, mirroring `8b` (location) and `8c` (object): if both `PropertyRepo` and `ParentLocationResolver` are non-nil, construct and register `attribute.NewPropertyProvider(...)`. Emit loud WARN at construction time when either is missing — match the `holomush-g776` precedent. |
| `subsystem.go` | `internal/access/setup/subsystem.go` | Wire `postgres.NewPropertyRepository(pool)` and `postgres.NewParentLocationResolver(pool)` into `ABACConfig`. |
| **New** `postgres.ParentLocationResolver` | `internal/world/postgres/parent_location_resolver.go` | Implements `attribute.ParentLocationResolver`. Per the spec at `docs/specs/abac/03-property-model.md:165-225`: `location` → return `parent_id` directly; `character` → JOIN `characters.location_id`; `object` → recursive CTE walking `held_by_character_id` / `contained_in_object_id` chain. The new file MUST define a package-level constant `const maxParentChainDepth = 20` (the value cited in `03-property-model.md:188`); the CTE's `WHERE cc.depth < maxParentChainDepth` clause is the **sole cycle defense**, matching `object_repo.go::checkCircularContainmentTx`'s precedent (it also uses depth-only, NOT a visited-set). A true cycle (e.g., `obj1.contained_in=obj2`, `obj2.contained_in=obj1`) terminates the CTE at the depth bound rather than by detecting the revisit — the resolver's contract (nil on un-resolvable) holds either way. Implementers MAY add an explicit `path` array column for cycle short-circuit (purely a performance optimization, not a correctness requirement). Unknown `parent_type` returns nil. |
| `seed_coverage_test.go::AcknowledgedMissingSeedNamespaces` | `internal/access/setup/seed_coverage_test.go` | Remove `"property"`. Add `"property"` to `productionRegistered`. |
| `buildabacstack_seed_coverage_integ_test.go` | `internal/access/setup/buildabacstack_seed_coverage_integ_test.go` | Pass `PropertyRepo` and `ParentLocationResolver` so the drift-detector includes property. |

### Sequence: list-properties-by-parent (post-fix)

```text
Caller → Service.ListPropertiesByParent(subj, parentType, parentID)
       → propertyRepo.ListByParent(parentType, parentID)        // unfiltered, N rows
       → for each prop:
           checkAccess(subj, "read", PropertyResource(prop.ID), prefixProperty)
             → engine.Evaluate(subj, "read", "property:<ulid>")
               → resolver.Resolve(...)
                 → PropertyProvider.ResolveResource("property:<ulid>")
                   → propertyRepo.Get(ulid)
                   → parentLocationResolver.ResolveParentLocation(parentType, parentID)
                     → CTE walks character/object containment to find effective location
               → policy compiler evaluates 6 seeds against resource bag
       → return [props that permit]
```

The N+1 cost is per-property: one `propertyRepo.Get` + one resolver call + DSL eval. The per-request attribute cache (ADR `holomush-fvn5`) deduplicates within a single Evaluate call but not across the loop. Acceptable for the dormant surface; profile and consider batch resolution if a future feature surface hits cardinality > 50 properties per call.

### Sequence: parent_location resolution by parent_type

```text
ResolveParentLocation(parent_type, parent_id):
  case "location": return parent_id                       // direct
  case "character":
    SELECT location_id FROM characters WHERE id = $parent_id
    return location_id  (may be NULL → return nil)
  case "object":
    WITH RECURSIVE chain AS (
      SELECT id, location_id, held_by_character_id, contained_in_object_id, 1 AS depth
        FROM objects WHERE id = $parent_id
      UNION ALL
      SELECT o.id, o.location_id, o.held_by_character_id, o.contained_in_object_id, c.depth + 1
        FROM objects o JOIN chain c ON o.id = c.contained_in_object_id
        WHERE c.depth < maxParentChainDepth
    )
    -- terminator: first row in the chain with location_id non-NULL → return that location_id
    -- OR a row with held_by_character_id non-NULL → JOIN characters → return characters.location_id
    -- OR depth exhausted / no terminator row reached → return nil
  default: return nil
```

The `WHERE c.depth < maxParentChainDepth` bound is the sole cycle defense. A true containment cycle terminates at the depth bound rather than via revisit-detection — both produce nil per the resolver contract. The new `parent_location_resolver.go` defines `const maxParentChainDepth = 20` matching the value cited in `03-property-model.md:188`. The existing `object_repo.go::checkCircularContainmentTx` (lines 251-296) uses the same depth-only pattern via its private `maxCTERecursionDepth` constant — this is project precedent, not novel. Note that the *application-layer* `ObjectProvider.resolveEffectiveLocation` at `internal/access/policy/attribute/object.go` uses both depth AND a visited-set, because it walks the chain in Go and a visited-set is cheap there; the *postgres-layer* CTE uses depth alone because the analogous SQL guard adds query complexity for no correctness gain when the depth bound already terminates cycles. The new `ParentLocationResolver` is a postgres-layer resolver, so it follows the postgres-layer precedent.

## Invariants

Each invariant is RFC2119-keyed and tied to a test that pins it.

| # | Invariant | Test |
| - | --------- | ---- |
| INV-1 | `Service.ListPropertiesByParent` MUST emit per-property `PropertyResource(prop.ID.String())` checks. It MUST NOT emit a parent-shaped resource like `property:<parentType>:<parentID>`. | `Service.ListPropertiesByParent filter` Describe block; unit test asserts the resource shape passed to `engine.Evaluate`. |
| INV-2 | `Service.ListPropertiesByParent` MUST return only properties for which the per-property `read` check returns `EffectAllow`. Properties whose check returns `ErrPermissionDenied` MUST be filtered out silently (NOT propagated as an error — default-deny on most-properties is the normal case for cross-principal reads). | Service-filter test F1 (mixed permit/deny → filtered subset); F2 (all-deny → empty list, no error). |
| INV-2b | `Service.ListPropertiesByParent` MUST propagate `ErrAccessEvaluationFailed` (infra failure: engine error, resolver timeout, session-resolution failure) from any per-property check as a wrapped error, aborting the call. Infra failures MUST NOT be silently masked as "no visible properties" — that would hide operational issues and create ghost-data scenarios for callers. | Unit test `TestService_ListPropertiesByParent` "infra failure" case (mock engine returns `EffectInfraFailure` on one prop, expects wrapped `ErrAccessEvaluationFailed` from the call). Integration F4 covers the `repo.ListByParent` error path. |
| INV-3 | `BuildABACStack` MUST register `PropertyProvider` when both `PropertyRepo` and `ParentLocationResolver` are non-nil in `ABACConfig`. | `TestBuildABACStack_SeedCoverageMatchesAcknowledged` (existing drift-detector) passes after the wiring lands. |
| INV-4 | `BuildABACStack` MUST emit a loud WARN at construction time when either `PropertyRepo` or `ParentLocationResolver` is missing, naming the affected seeds (`seed:property-public-read` etc.). | Unit test capturing slog output for the missing-repo + missing-resolver branches. |
| INV-5 | `postgres.ParentLocationResolver` MUST resolve `parent_type=location` via direct `parent_id` return without database query. `character` MUST use `JOIN characters` on `location_id`. `object` MUST use a recursive CTE with `WHERE depth < maxParentChainDepth` (defined as `const maxParentChainDepth = 20`). The depth bound is the sole cycle defense — terminates a true containment cycle by exhaustion (project precedent: `object_repo.go::checkCircularContainmentTx`). Implementers MAY add an explicit visited-path array as a performance optimization; this is NOT required for correctness. | Resolver-scenario tests (9 cases), including R7 (cycle → nil via depth exhaustion) and R8 (chain of 21 objects → nil via depth bound). |
| INV-6 | `postgres.ParentLocationResolver` MUST return nil (not error) for: character with NULL `location_id`; orphaned object (no placement column non-NULL); cycle in containment chain; depth exceeded; unknown `parent_type`. | Resolver-scenario tests. |
| INV-7 | `PropertyProvider` MUST follow ADR `holomush-ti1b`: omit `parent_location` key from the attribute bag when un-resolved. The `has_parent_location` boolean witness MUST always be present. | Unit test in `property_test.go` (already covered by `holomush-9gtl`); resolver-error case re-pins post-wiring. |
| INV-8 | After this work lands, `AcknowledgedMissingSeedNamespaces` MUST NOT contain `"property"`; `productionRegistered` MUST contain `"property"`. | `seed_coverage_test.go::TestValidateSeedProviderCoverage_ProductionCorpusIsCovered` (existing unit test). |

## Test plan

All integration tests live under `test/integration/access/` and extend the existing `seed_policies_test.go` Ginkgo suite (which already builds a real ABAC stack from scratch). The `setupAccessTestEnv` function in `access_suite_test.go` is extended to also construct a `world.Service` instance against the same Postgres pool — this is a small addition (one `world.NewService(...)` call) that unlocks the service-layer tests without contributing to the `integrationtest` harness.

### Unit tests (no database)

Location: `internal/access/policy/attribute/property_test.go`, `internal/world/service_test.go`.

- PropertyProvider parser cases (covered by `o8g6`): `property:<ulid>` happy path, `property:*` wildcard returns `(nil, nil)`, `property:not-a-ulid` returns `(nil, nil)`, missing colon returns `INVALID_RESOURCE_ID`.
- PropertyProvider ti1b cases (covered by `9gtl`): un-resolved `parent_location` → key OMITTED, `has_parent_location=false`.
- New: `parent_type=location` short-circuits without calling the resolver (asserts `mockParentLocationResolver` was NOT called).
- New: `TestService_ListPropertiesByParent` table-driven with a mock `AccessPolicyEngine`:
  - empty parent → empty list, nil error
  - all-permit → full list, nil error
  - all-deny (engine returns deny decisions) → empty list, nil error
  - mixed permit/deny → filtered subset, nil error
  - `propertyRepo.ListByParent` returns error → propagated wrapped error
  - **infra failure**: engine returns `EffectInfraFailure` (or `Evaluate` returns error) on one of the properties → call returns wrapped `ErrAccessEvaluationFailed` with oops code `PROPERTY_ACCESS_EVALUATION_FAILED` (INV-2b). Caller MUST NOT see partial success.
- Extend `internal/world/service_helpers_test.go::TestCheckAccess`'s prefix-to-error-code table to include `prefixProperty` so the canonical `PROPERTY_ACCESS_DENIED` and `PROPERTY_ACCESS_EVALUATION_FAILED` oops codes are pinned the same way other prefixes already are. Without this, the new per-property filter loop emits codes that no existing test verifies.

### Integration tests (build tag `integration`)

Location: `test/integration/access/seed_policies_test.go`. Extends existing patterns. Outer `BeforeEach` gains `DELETE FROM entity_properties` (placed before characters/locations to satisfy FK ordering).

#### `Describe("Property visibility seeds (holomush-72ou)")` — 13 cases

| # | Test name | Setup | Expected |
| - | --------- | ----- | -------- |
| S1 | `allows reading a public property on a co-located character` | char1@loc1, char2@loc1, char2.props.public-bio | `EffectAllow` |
| S2 | `allows reading a public property on a co-located location` | char1@loc1, loc1.props.public-greeting | `EffectAllow` |
| S3 | `denies reading a public property on a different-location parent` | char1@loc1, char2@loc2, char2.props.public-bio | `EffectDefaultDeny` |
| S4 | `allows owner to read their own private property (self-as-parent path)` | property with `parent_type=character, parent_id=char1.id, owner=char1.id, visibility=private` — char1's location is `loc1`. Exercises the self-as-parent resolver path (char1 reading a property whose parent IS char1). The resolver MUST resolve `parent_location=loc1` via R2's character→JOIN path. | `EffectAllow` |
| S5 | `denies non-owner reading a private property` | char1@loc1, char2.props.private-note (owner=char2) | `EffectDefaultDeny` |
| S6 | `allows admin to read an admin-visibility property` | admin-char, target.props.admin-secret | `EffectAllow` (via `seed:admin-full-access` or `seed:property-admin-read`) |
| S7 | `denies non-admin reading an admin-visibility property` | player-char, target.props.admin-secret | `EffectDefaultDeny` |
| S8 | `denies even admins reading a system-visibility property` | admin-char, target.props.system-internal | `EffectDefaultDeny` (no seed permits system) |
| S9 | `allows visible-to-listed character to read a restricted property` | char1 NOT co-located, char2.props.restricted with `visible_to=[char1.id]` | `EffectAllow` |
| S10 | `excluded_from beats visible_to (deny-overrides)` | char1 in BOTH visible_to and excluded_from for the same prop | `EffectDefaultDeny` (`seed:property-restricted-excluded` forbid wins) |
| S11 | `allows owner to write their own property` | char1.props (owner=char1) | `EffectAllow` |
| S12 | `denies non-owner writing a property` | char2.props (owner=char2), principal=char1 | `EffectDefaultDeny` |
| S13 | `denies reading on an un-locatable property (ti1b reinforcement)` | prop.parent=object with broken-chain | `EffectDefaultDeny` |

#### `Describe("ParentLocationResolver (holomush-72ou)")` — 9 cases

| # | Test name | Setup | Expected |
| - | --------- | ----- | -------- |
| R1 | `location parent returns parent_id directly` | `parent_type=location`, parent=loc1 | returns loc1, no DB query |
| R2 | `character parent JOINs current location_id` | parent=char1 at loc1 | returns loc1 |
| R3 | `character parent with NULL location returns nil` | parent=char1, `location_id IS NULL` | returns nil |
| R4 | `object parent at direct location returns that location` | parent=obj1, `obj1.location_id=loc1` | returns loc1 |
| R5 | `object parent held by character resolves via character` | parent=obj1, `obj1.held_by_character_id=char1@loc1` | returns loc1 |
| R6 | `object parent contained in object resolves recursively` | parent=obj1 in obj2 in loc1 | returns loc1 |
| R7 | `object parent in containment cycle returns nil` | obj1.contained_in=obj2, obj2.contained_in=obj1 (constructed via direct UPDATE per the holomush-9gtl pattern) | returns nil |
| R8 | `object parent at max depth exceeded returns nil` | chain of 21 objects | returns nil |
| R9 | `unknown parent_type returns nil` | `parent_type="exit"` (not supported) | returns nil |

#### `Describe("Service.ListPropertiesByParent filter (holomush-72ou)")` — 5 cases

All cases use the access-suite real ABAC stack (`access_suite_test.go`'s engine + seeds + extended with a `world.Service`). Infra-failure propagation (INV-2b) is exercised by a unit test with a mock engine — see Unit tests above.

| # | Test name | Setup | Expected |
| - | --------- | ----- | -------- |
| F1 | `returns only properties the principal can read` | char1@loc1, char2@loc1 with 3 props (public, private-self, private-other) | returns only the public prop |
| F2 | `returns empty list when zero properties are visible (no error)` | char1@loc2 (different), char2@loc1 with public+private props | returns `[]`, error is nil |
| F3 | `returns empty list when parent has no properties` | char1, char2 with no properties | returns `[]`, error is nil |
| F4 | `surfaces repository errors` | mock `repo.ListByParent` returns DB error | wrapped error propagates |
| F5 | `filters cumulatively under restricted+excluded_from` | char1 in both `visible_to` and `excluded_from` for a restricted prop | the prop is filtered out (forbid wins) |

### Excluded from this bead

- E2E browser/telnet tests: no UX surface exercises `ListPropertiesByParent` today.
- Performance/load tests: N+1 cost acceptable for dormant surface; profile + batch-resolve in a future bead if cardinality grows.
- Concurrent-access tests: no shared mutable state introduced by this change beyond what already exists.
- Plugin Lua-path integration: `NewPropertyCapability` is not wired in production; out of scope until a plugin manifest references property hostfunc.

## Risks

1. **Recursive CTE correctness.** `ParentLocationResolver` for objects requires a CTE that walks both `held_by_character_id` (terminates via JOIN on characters) and `contained_in_object_id` (recursive) branches. The terminator condition (which column ends the walk) needs careful encoding. Mitigation: mirror the `object_repo.go::checkCircularContainmentTx` pattern (lines 251-296) which is already in production for circular-containment detection; add R5–R8 integration tests; pair-review the SQL.
2. **N+1 ABAC cost.** Bounded by per-parent property count. Typical: 5–20 properties per parent. Edge case: a builder configuring 100+ properties on a location. Mitigation: documented in spec as accepted trade-off for dormant surface; profile + batch-resolve when first prod caller surfaces cardinality issues.
3. **Test fixture interference with other Describe blocks.** Adding `DELETE FROM entity_properties` to the outer `BeforeEach` is the FK-ordering-safe approach. Verified against migration `000001_baseline.up.sql`: `entity_properties` has no FKs referencing other tables that would force a different order, but `entity_properties` does reference `characters/locations/objects`. Deleting properties FIRST (before deleting the referenced rows) avoids any FK violations.
4. **Drift-detector failure during incremental landing.** Until both the wiring AND the test-list updates are in the same commit, `TestBuildABACStack_SeedCoverageMatchesAcknowledged` will fail. Mitigation: land both halves in the same commit; the drift detector explicitly documents this as expected.

## Open follow-ups

These spawn at spec-finalize time:

| Bead | Topic | Priority |
| ---- | ----- | -------- |
| (new) | `WithRealABAC` option for `integrationtest` harness to bridge the access-suite + integrationtest patterns | P3 |
| (new) | Circuit breaker + `statement_timeout` for PropertyProvider per `03-property-model.md:208-218` (file when first prod caller lands or under operational hardening sweep) | P3 |
| (new) | `Service.GetProperty/CreateProperty/UpdateProperty/DeleteProperty` API surface (file when feature surface needs property mutation) | P3 |

## References

- `docs/specs/abac/03-property-model.md` — property model + visibility seeds
- ADR [holomush-iv43](../../adr/holomush-iv43-cedar-aligned-fail-safe-type-semantics.md) — DSL fail-safe semantics
- ADR [holomush-ti1b](../../adr/holomush-ti1b-providers-omit-optional-attrs.md) — provider omit-not-sentinel rule
- bead `holomush-g776` — LocationProvider wiring template
- bead `holomush-k3ud` — ObjectProvider wiring template
- bead `holomush-o8g6` — parser unification (`parseEntityResource` helper)
- bead `holomush-xxel` — seed-coverage validator (catches this gap automatically when the wiring lands)
- `internal/world/postgres/object_repo.go:251-296` — `checkCircularContainmentTx` (recursive CTE precedent)

<!-- adr-capture: sha256=ca54aacb33cbc21e; session=cli; ts=2026-05-22T12:40:32Z; adrs= -->

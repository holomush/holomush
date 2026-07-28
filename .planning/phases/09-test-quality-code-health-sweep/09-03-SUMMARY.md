---
phase: 09-test-quality-code-health-sweep
plan: 03
subsystem: access-control
tags: [abac, security, fail-open, attribute-provider, tdd]
status: complete

requires:
  - "09-01 (wave 1 — coverage lane repair; no code dependency)"
provides:
  - "ABAC attribute bags that omit unresolved optional attributes on all five providers in internal/access/policy/attribute/"
  - "A cross-resource regression test pinning the fail-open equality closed"
affects:
  - "internal/access/policy/attribute/ (location, object, property providers)"
  - "any future seed policy comparing resource.{location,object,property}.<optional attr>"

tech-stack:
  added: []
  patterns:
    - "omit-don't-sentinel for optional ABAC attributes (ADR holomush-ti1b)"
    - "boolean has_X / is_X witness emitted on every code path"

key-files:
  created: []
  modified:
    - internal/access/policy/attribute/location.go
    - internal/access/policy/attribute/object.go
    - internal/access/policy/attribute/property.go
    - internal/access/policy/attribute/location_test.go
    - internal/access/policy/attribute/object_test.go
    - internal/access/policy/attribute/property_test.go
    - .planning/phases/09-test-quality-code-health-sweep/deferred-items.md

decisions:
  - "Used object_test.go's pre-existing expectAbsent idiom for the one expectSubset row, rather than silently dropping the key — the plan's claim that no absence idiom exists in this package is false, and dropping the key would have weakened the assertion."
  - "Ran ./internal/access/... recursively rather than the plan's non-recursive ./internal/access/, which tests only the top-level package and would not have exercised the 1232-test policy tree."
  - "Left the wildcard-bypass doc comment in location.go untouched and surfaced it for abac-reviewer instead of editing security guidance outside the plan's scope."
  - "QUAL-05 NOT marked complete — plans 09-04/05/06 also carry it."

metrics:
  duration_minutes: 22
  tasks: 2
  commits: 3
  files_modified: 6
  tests_before: 277
  tests_after: 278
  completed: 2026-07-26
---

# Phase 9 Plan 03: Remove Empty-String Sentinels from ABAC Attribute Providers Summary

Seven `attrs["x"] = ""` sentinels removed from the location, object, and property
AttributeProviders, closing a latent fail-open equality in a default-deny
authorization system (issue #4793, ADR holomush-ti1b).

## What Was Built

The DSL evaluator treats a **missing** attribute as `false` for every operator
(ADR holomush-iv43) but does **not** treat `""` as missing — `"" == ""` is
`true`. Three of the five providers in `internal/access/policy/attribute/`
emitted an empty-string sentinel alongside a `false` witness when an optional
attribute was unresolved, so two independently unresolved resources compared
**equal** on that attribute. Two providers (character, stream) already carried
the correct omit form and the ADR-citing comment; three did not.

### Task 1 — sentinel removal (TDD)

RED (`d5d441313`): removed the sentinel entries from the expected attribute maps
in the three sibling test files. 9 failures, each naming the extra key in the
whole-map diff.

GREEN (`84094e79d`): deleted the seven sentinel assignments. Every fixed
conditional carries an ADR-citing comment block mirroring the character
provider's.

| File | Line (post-fix) | Attribute removed | Witness retained |
|------|-----------------|-------------------|------------------|
| `internal/access/policy/attribute/location.go` | 75-80 | `owner_id` | `has_owner` (77, 79) |
| `internal/access/policy/attribute/location.go` | 89-94 | `shadows_id` | `is_shadow` (91, 93) |
| `internal/access/policy/attribute/object.go` | 120-125 | `owner_id` | `has_owner` (122, 124) |
| `internal/access/policy/attribute/object.go` | 132-137 | `held_by_character_id` | `is_held` (134, 136) |
| `internal/access/policy/attribute/object.go` | 144-149 | `contained_in_object_id` | `is_contained` (146, 148) |
| `internal/access/policy/attribute/property.go` | 97-102 | `value` | `has_value` (99, 101) |
| `internal/access/policy/attribute/property.go` | 113-118 | `owner` | `has_owner` (115, 117) |

**No sentinel was replaced with another placeholder** — the prohibition holds.
Declared schemas in all three `Schema()` methods are untouched: omission is a
runtime bag property, not a schema change.

### Task 2 — cross-resource regression test

`ef012e364` adds, at `internal/access/policy/attribute/location_test.go:288`:

```
TestTwoLocationsWithUnresolvedOptionalAttributesDoNotCompareEqualToEachOther
```

**Plan 09-18's naming sweep must not rename this test.** It has no underscore,
so its final underscore-delimited segment is the whole multi-token name.

It resolves two distinct unowned, non-shadow locations and asserts the security
property directly — that the two bags do not **both** carry `owner_id` /
`shadows_id`, which is the state a colocation-style permit seed would match on.
It carries a comment naming issue #4793 and both ADRs at the assertion site, and
ends with a positive control proving a resolved `owner_id` is still present.

## Exploitability (for abac-reviewer)

Honest assessment, not a claim of a live exploit:

- **`property.owner` is the load-bearing one.** `seed:property-private-read` and
  `seed:property-owner-write` both gate on
  `resource.property.owner == principal.character.id`
  (`internal/access/policy/seed.go:119,131`). This pair was **not** directly
  exploitable, because `principal.character.id` is always a populated ULID and
  never `""`. The sentinel was one seed away from being live.
- **No shipped seed compares two optional attributes** from the affected set —
  `owner_id`, `shadows_id`, `held_by_character_id`, `contained_in_object_id`,
  and `property.value` appear in no DSL text in `seed.go` and in no policy YAML.
- The defect was therefore **latent**, matching issue #4793's own framing
  ("latent fail-open"). The fix removes the class, not a live breach.

## Verification Performed

| Check | Command | Result |
|-------|---------|--------|
| Package tests | `task test -- ./internal/access/policy/attribute/` | exit 0, 278 tests (was 277) |
| Full access tree | `task test -- ./internal/access/...` | exit 0, 1232 tests |
| Sentinel guard | `rg 'attrs\["[a-z_]+"\] = ""' internal/access/policy/attribute/` | exit 1 (no matches) |
| Witness survival | witnesses on both branches, all 7 sites | verified, table above |
| Lint | `task lint` | exit 0 |
| Format | `task fmt` | exit 0, no residual diff |
| Integration (full) | `task test:int` | exit 0, 10787 tests, 7 skipped |
| Integration (scoped) | `task test:int -- ./test/integration/access/...` | exit 0 |

### Falsifiability of every guard used

Per this phase's recurring defect class, each guard was proven able to fail:

1. **The `rg` sentinel guard** matched 7 sites and exited 0 *before* the fix, and
   exits 1 after. Natural negative control — it was not a can't-fail check.
2. **The test guard** was proven by the RED phase: 9 real failures naming the
   extra keys, exit 201.
3. **The Task 2 regression test** was proven by an explicit negative control:
   `attrs["owner_id"] = ""` was temporarily reintroduced into `location.go`, the
   test failed with the message *"both bags carry \"owner_id\" while unresolved"*
   at exit 201, and the provider was restored via
   `git checkout -- internal/access/policy/attribute/location.go`.
4. **The `-run` invocation reported `DONE 1 tests`**, i.e. a non-zero matched
   count. A `-run` pattern matching nothing reports `DONE 0 tests` and exits 0,
   which would have been a vacuous pass.
5. **A self-inflicted evidence failure was caught and corrected.** The first
   `task test:int` run was piped through `tail -40`, which truncated the package
   list; a subsequent search for `test/integration/access` returned zero hits.
   Rather than read that zero as "the package did not run", the suite was re-run
   scoped. It passed at exit 0 and instrumented 8.1% of repo statements from that
   suite alone — a vacuous suite cannot instrument 8.1% of the repo.

### What the integration run does and does not prove

- **Proves:** no seed policy, evaluator path, or integration expectation
  anywhere in the tree depended on the sentinel. 10787 integration tests pass.
- **Does not prove:** that the integration suite *guards* the omit behavior. The
  suite passed both before and after the change, so it is agnostic to this
  attribute-shape difference. The Task 2 unit regression test is the only guard.

## Deviations from Plan

**1. [Rule 1 - Falsified plan premise] `expectAbsent` idiom already exists**

- **Found during:** Task 1, RED phase.
- **Plan asserted:** *"This package asserts attribute absence by whole-map
  equality... there is no NotContains idiom here; do not introduce one."*
- **Reality:** `object_test.go` already declares `expectAbsent []string`
  (lines 156-161) with a runner at 486-490 that asserts key absence with an
  ADR-citing message. It is used by four existing rows.
- **Why it mattered:** the plan's instruction ("delete that map entry") is
  correct for whole-map rows but wrong for `object_test.go`'s
  `expectSubset` row pinning `"owner_id": ""`. Deleting the entry there would
  have left the key asserted by nothing — a silent weakening.
- **Fix:** moved `owner_id` from `expectSubset` into the file's existing
  `expectAbsent` list. Not a new idiom; the file's own.
- **Files:** `internal/access/policy/attribute/object_test.go:204-212`
- **Commit:** `d5d441313`

**2. [Rule 3 - Non-falsifiable verification replaced] Recursive access tree**

- **Found during:** Task 1 acceptance.
- **Plan specified:** `task test -- ./internal/access/` — the non-recursive form,
  which tests only the top-level `internal/access` package and would not compile
  or run a single test in `internal/access/policy/...`, where every consumer of
  the changed bags lives. It could not have detected a regression in the surface
  it was written to protect.
- **Fix:** ran `task test -- ./internal/access/...` (1232 tests, exit 0).
- **Commit:** verification-only; no code change.

**3. [Scope boundary] Wildcard-bypass doc comment left in place, flagged**

- **Found during:** Task 1 `read_first`.
- **Issue:** `internal/access/policy/attribute/location.go:40-42` — the
  `ResolveResource` doc comment covering the `location:*` wildcard bypass
  (holomush-g776) still reads: *"the provider MUST populate sentinel values for
  X (or the seed MUST narrow its target via `resource ==`)"*. The first clause
  instructs a future contributor to do exactly what this plan removed.
- **Why not fixed:** it governs a different code path — the wildcard bypass
  returns `(nil, nil)` and builds no bag at all — with its own design rationale.
  Rewriting security guidance outside the plan's authorized seven sites without
  the domain gate's input is the wrong order. Logged to `deferred-items.md`.
- **Action required:** `abac-reviewer` should rule on this. Suggested wording:
  replace the sentinel clause with "gate on the `has_X` witness — populating a
  sentinel is forbidden by ADR holomush-ti1b".

## For the abac-reviewer Gate

This plan touches `internal/access/`, so the domain gate must run before push.

- **Sentinel sites removed:** all seven, enumerated with `path:line` in the table
  above. `rg 'attrs\["[a-z_]+"\] = ""' internal/access/policy/attribute/` now
  returns no matches across the whole directory.
- **Sentinel sites left in place:** none. No empty-value attribute assignment
  remains anywhere under `internal/access/policy/attribute/`.
- **Witnesses:** every `has_X` / `is_X` witness is emitted on both branches of
  all seven conditionals — verified line-by-line, table above. No branch omits a
  witness.
- **Schemas:** unchanged in all three `Schema()` methods. Every optional
  attribute is still declared; the existing schema assertions
  (`location_test.go:74-79`, `object_test.go:120-128`, `property_test.go`) were
  deliberately not touched.
- **Positive paths:** at least one table row per provider still asserts the
  resolved value is present with its identifier — `location_test.go` ("persistent
  location with owner" → `owner_id`; "shadow location (archived)" →
  `shadows_id`), `object_test.go` (held → `held_by_character_id`; contained →
  `contained_in_object_id`), `property_test.go` (first row → `value` + `owner`).
  The attributes were not dropped unconditionally.
- **One item needs your ruling:** the wildcard-bypass doc comment at
  `location.go:40-42` (deviation 3 above).

## Requirements

`QUAL-05` is **not** marked complete. Plans 09-04, 09-05, and 09-06 also carry
it; this plan delivers only its share. Following the carry-forward discipline
from 09-01/09-02, no requirement is checked off on this plan's evidence alone.

## Known Stubs

None. No stub, placeholder, skipped test, or unrun `<verify>` was introduced.

## Threat Flags

None. No new network endpoint, auth path, file-access pattern, or schema change
at a trust boundary. `T-09-03-01` (Elevation of Privilege) and `T-09-03-02`
(Spoofing) from the plan's register are both mitigated as specified;
`T-09-03-03` and `T-09-03-SC` were accepted with no action, as planned — no
package was installed and no dependency manifest touched.

## Self-Check: PASSED

- `internal/access/policy/attribute/location.go` — FOUND
- `internal/access/policy/attribute/object.go` — FOUND
- `internal/access/policy/attribute/property.go` — FOUND
- `internal/access/policy/attribute/location_test.go` — FOUND
- `internal/access/policy/attribute/object_test.go` — FOUND
- `internal/access/policy/attribute/property_test.go` — FOUND
- commit `d5d441313` — FOUND
- commit `84094e79d` — FOUND
- commit `ef012e364` — FOUND

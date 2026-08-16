---
created: 2026-08-08T23:44:51.692Z
title: Harden world.Caller bypass visibility and opacity guarantees
area: auth
severity: major
files:
  - internal/world/caller.go:50,88-90,104
  - internal/grpc/location_follow.go:200,213
  - internal/plugin/hostfunc/cap_property.go:74,140
  - internal/plugin/hostfunc/cap_world_query.go:84,119
  - test/meta/world_caller_census_test.go
---

## Problem

Three security-adjacent weakenings surfaced by the Phase 02.1 gate (2026-08-08).
None is a live defect; each removes or erodes a guarantee something later will
lean on. Grouped because they share a fix window (before/with Phase 02.2).

### 1. `SystemCaller()` is a single-token ABAC bypass with no census pinning it

Raised independently by BOTH reviewers — `abac-reviewer` Medium #2 and code
review WR-06.

`internal/world/caller.go:88-90`:

```go
func SystemCaller() Caller {
    return Caller{subject: systemSubject, system: true}
}
```

Before Phase 02.1, a total ABAC bypass at the world boundary required TWO
visible, independently greppable acts: passing the literal `"system"` AND
calling `access.WithSystemSubject(ctx)`. `docs/specs/abac/01-core-types.md:166`
states that marker "MUST be restricted to internal-only".

After 02.1 the bypass is a single zero-argument exported call from any package
that can import `internal/world`. `rg WithSystemSubject` over non-test code now
returns only `internal/world/caller.go:104` — it **no longer enumerates the
bypass sites**. `test/meta/world_caller_census_test.go` pins the caller
PARAMETER SHAPE; it does not pin the `SystemCaller()` CALL-SITE SET.

Current production count is exactly 2, both READS, both in
`internal/grpc/location_follow.go:200,213`.

Risk: a future `SystemCaller()` on a WRITE command (`CreateLocation`,
`DeleteCharacter`, `MoveCharacter`) would be a silent, total, un-audited-by-policy
world write — and the historical grep that reviewers and the ABAC spec point at
would not surface it.

Mitigating context worth keeping: `SystemCaller()` takes no parameters, so no
request-derived value can ever *select* it. The bypass can only be chosen by a
hardcoded call site, which is strictly better than the old string-comparison
shape. This is a visibility/audit gap, not an injection gap.

### 2. `Caller.attrs` is a shared-reference map on a type documented immutable

Code review WR-07. `internal/world/caller.go:50` declares `attrs` as a map —
a reference type — on a value type whose whole contract is opacity and
immutability. Whoever constructs a `Caller` with attributes retains a live handle
and can mutate the map AFTER construction, behind the opaque façade.

Inert in 02.1 (both exported constructors leave `attrs` nil). Arms with 02.2.

### 3. Lua capability façades still take a script-supplied bare subject

`abac-reviewer` Low #4. `internal/plugin/hostfunc/cap_property.go:74,140` and
`cap_world_query.go:84,119` take the ABAC subject from `L.CheckString(1)` — i.e.
chosen by the Lua script itself.

These are the four remaining bare-subject-string sites (rows 28-31 of
`.planning/phases/02.1-world-caller-model/02.1-RESEARCH.md` §Q2) and were
deliberately left out of Phase 02.1's scope. They are UNWIRED scaffolding —
`rg 'NewPropertyCapability|NewWorldQueryCapability|PropertyAccess|WorldQueryAccess'`
returns only definitions and `_test.go` files, no production implementer — so
they are not exploitable today and do not reach `world.Service`.

They are also outside `test/meta/world_caller_census_test.go`'s universe (which
is `world.Service` commands), so nothing will catch them if they are wired.

## Solution

1. **SystemCaller census.** Add a meta-test in `test/meta/` — sibling shape to
   `world_caller_census_test.go` — enumerating non-test `SystemCaller()` call
   sites and asserting the set equals a documented allowlist, using the same
   one-comment-per-justification idiom as `worldCallerExemptCommands()`. Cheap,
   structural, makes any future bypass a visible diff. Seed the allowlist with
   the two current `location_follow.go` reads.

2. **attrs ownership.** Either defensive-copy the map at construction, or
   document an explicit ownership rule on the type. Decide BEFORE 02.2 populates
   it — retrofitting a copy after callers exist is harder.

3. **Lua façades.** Either delete the dead scaffolding or migrate it to a
   host-derived subject BEFORE it is wired. Do not leave a script-chosen ABAC
   subject sitting in the tree waiting for a caller.

## Provenance

- Code review: `.planning/phases/02.1-world-caller-model/02.1-REVIEW.md` (WR-06, WR-07)
- ABAC review: `.claude/agent-memory/abac-reviewer/reports/2026-08-08-1923-v013-phase-02-1-world-caller-model.md` (Medium #2, Low #4)
- Both reviewers returned no blocking findings; these are forward-looking.

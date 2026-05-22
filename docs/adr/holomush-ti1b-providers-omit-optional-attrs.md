<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# AttributeProviders MUST Omit Optional Attributes (Not Emit Empty-String Sentinels)

**Date:** 2026-05-22
**Status:** Accepted
**Decision:** holomush-ti1b
**Motivating bug:** holomush-9gtl
**Deciders:** HoloMUSH Contributors
**Related:** [holomush-iv43](holomush-iv43-cedar-aligned-fail-safe-type-semantics.md) (Cedar fail-safe DSL semantics — required parent)

## Context

ADR [holomush-iv43](holomush-iv43-cedar-aligned-fail-safe-type-semantics.md) (originally
ADR 0010) established that the DSL evaluator treats **missing attributes** as `false` for
every operator (Cedar-aligned fail-safe semantics). This is the DSL-side guarantee.

The **provider side** of the same contract was not stated explicitly until this ADR. As
the codebase grew, providers diverged in how they represented "this attribute is not
applicable for this entity":

| Provider          | When the optional attr is unresolved | Behavior |
| ----------------- | ------------------------------------ | -------- |
| `StreamProvider`  | Stream is not a `location:*` stream  | OMIT `attrs["location"]` entirely |
| `CharacterProvider` | Character has no `LocationID`      | Emitted `attrs["location"] = ""` (empty-string sentinel) |
| `ObjectProvider`  | Container chain cannot resolve to a location | Emitted `attrs["location"] = ""` |
| `PropertyProvider` | `ParentLocationResolver` returns nil / errors | Emitted `attrs["parent_location"] = ""` |

The empty-string-sentinel approach silently broke fail-safe semantics because the DSL
evaluator treats `"" == ""` as `true` (both values are *present* strings). The Cedar
fail-safe guarantee from [holomush-iv43](holomush-iv43-cedar-aligned-fail-safe-type-semantics.md)
ONLY fires for MISSING attributes (see [`internal/access/policy/dsl/evaluator.go:128-136`](../../internal/access/policy/dsl/evaluator.go),
function `evalComparison`).

### The bug class this ADR closes

Two seed policies in [`internal/access/policy/seed.go`](../../internal/access/policy/seed.go)
use the colocation pattern:

```text
seed:player-character-colocation:
  permit when { resource.character.location == principal.character.location };
seed:player-object-colocation:
  permit when { resource.object.location == principal.character.location };
```

With empty-string sentinels:

- Two un-locatable characters → `"" == ""` → **permit** (fail-open)
- Un-locatable character + un-locatable object (cycle/depth-exceeded/broken-chain) →
  `"" == ""` → **permit** (fail-open)

The window is narrow in practice (characters routinely have `LocationID` set after spawn),
but the bug class extends to any future seed gating on `resource.X.location` or
`resource.X.parent_location`. `StreamProvider` already avoided the bug by following the
correct pattern.

### Why the existing DSL-level fix (ADR holomush-iv43) is necessary but insufficient

Cedar fail-safe semantics on the DSL evaluator are correct: a **missing** attribute
short-circuits comparisons to `false`. But the evaluator cannot distinguish "missing"
from "present-with-empty-string-sentinel." The provider must cooperate by genuinely
omitting the attribute, not encoding "absent" as `""`.

## Decision

**`AttributeProvider` implementations MUST omit optional attribute keys from the
returned bag when the value is unresolved or not applicable. They MUST NOT emit
empty-string (or any other type-checking-passable) sentinel values.**

### Required form

```go
// Correct: omit on unresolved
if char.LocationID != nil {
    attrs["location"] = char.LocationID.String()
    attrs["has_location"] = true
} else {
    attrs["has_location"] = false
    // `location` key is INTENTIONALLY ABSENT
}
```

### Forbidden form

```go
// WRONG: emits empty-string sentinel
if char.LocationID != nil {
    attrs["location"] = char.LocationID.String()
    attrs["has_location"] = true
} else {
    attrs["location"] = ""   // ← creates a permit-match against any other un-locatable peer
    attrs["has_location"] = false
}
```

### Companion `has_X` boolean witness

A provider SHOULD emit a `has_X` boolean witness alongside every optional attribute. The
witness lets seeds explicitly check existence via DSL `has` or
`principal.X.has_Y &&` conjunctions without relying on a sentinel value. When emitted,
the witness MUST always be present (true or false on every code path) — omission applies
only to the value attribute, never to the witness.

A provider MAY skip the witness only when no seed could plausibly want to disambiguate
"absent" from "present-but-empty" for that attribute (e.g., enum-typed attrs whose
absence has a distinct defined value rather than an absent-data signal). In practice
every current provider in `internal/access/policy/attribute/` carries the witness, and
any new optional attribute SHOULD ship with its witness on day one.

## Scope

The invariant applies to **every** optional `AttributeProvider` attribute, not only
location-related ones. This ADR was motivated by the colocation bug (`holomush-9gtl`),
but the principle is general — any optional attribute that could be the right-hand side
of a `==` comparison against another un-resolved peer attribute is vulnerable.

Initial application in `holomush-9gtl`:

- `CharacterProvider.location` / `location_id` — fixed
- `ObjectProvider.location` — fixed
- `PropertyProvider.parent_location` — fixed

Follow-up `holomush-awb3` extends the pattern to the remaining optional attrs across
all four providers (e.g., `owner_id`, `held_by_character_id`, `contained_in_object_id`,
`shadows_id`, property `value`/`owner`).

## Rationale

**Defense-in-depth over single-layer safety.** ADR holomush-iv43 made the DSL evaluator
fail-safe for missing attrs. This ADR makes the provider side cooperate. Combined, the
fail-safe guarantee holds end-to-end. Either layer alone is insufficient: a permissive
provider defeats a strict evaluator, and a strict provider can't fix a permissive
evaluator.

**Consistency with existing precedent.** `StreamProvider` already follows this pattern
(`internal/access/policy/attribute/stream.go:46-48` sets `attrs["location"]` only when
the stream name has the `location:` prefix). Choosing this approach unifies practice
rather than introducing a third pattern.

**No DSL-side footgun for future seed authors.** The alternative — add explicit
`has_location` guards in every colocation seed — would work but creates a footgun: a
new seed author who forgets the guard re-introduces the bug. Provider-level omission is
automatic and applies to all future seeds.

**Schema declaration is decoupled from runtime emission.** The provider's `Schema()`
method declares attribute *types* (used at DSL compile time per the eager-resolution
ADR). Whether the attribute is *present* at runtime is independent. Omitting a
schema-declared attr at runtime is contract-safe — the DSL compiler still accepts
references to it, and the evaluator handles missing keys via the iv43 contract.

## Consequences

**Positive:**

- Default-deny preserved end-to-end across colocation and any future seed gating on
  an optional attribute equality.
- Provider behavior unified — same pattern across `Character`, `Object`, `Property`,
  `Stream`, and future providers.
- Lower cognitive load on seed authors — they don't need to remember to add `has_X`
  guards for every optional reference.

**Negative:**

- Provider implementations slightly more verbose (must remember to omit, not just emit
  an empty-string default).
- A future maintainer who reads only the provider code may assume the attribute is
  always present (mitigated by the always-present `has_X` witness convention and the
  rule file at [`.claude/rules/abac-providers.md`](../../.claude/rules/abac-providers.md)
  that auto-loads on the `internal/access/policy/attribute/` directory).

**Neutral:**

- Tests that explicitly assert `attrs["X"] == ""` need to be updated to assert the key
  is absent via `_, present := attrs["X"]; assert.False(t, present)`. Existing tests
  updated as part of `holomush-9gtl`.

## Enforcement

Enforcement is **convention-with-documentation** (no custom linter today):

1. **Rule file at [`.claude/rules/abac-providers.md`](../../.claude/rules/abac-providers.md)**
   auto-loads when editing the `internal/access/policy/attribute/` directory and
   restates this invariant inline for any contributor or AI assistant working in that
   tree.
2. **Pre-push `abac-reviewer` sub-agent** ([`.claude/agents/abac-reviewer.md`](../../.claude/agents/abac-reviewer.md))
   is briefed via the rule file and CLAUDE.md gates and MUST flag any new
   `attrs["X"] = ""` followed by `attrs["has_X"] = false` pattern.
3. **A static analyzer to enforce this mechanically** is tracked as a future follow-up
   (`holomush-awb3` may surface or spawn the lint bead). The convention-and-review
   approach is sufficient for the four-provider population; a custom analyzer is
   justified only if the provider count grows or if a regression slips past review.

## Alternatives Considered

### Option (a): DSL-side `has_location` guards on every colocation seed

```text
permit when {
    principal.character.has_location &&
    resource.character.has_location &&
    resource.character.location == principal.character.location
};
```

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Localized to the seeds; no provider changes |
| Weaknesses | Footgun for future seed authors; doesn't generalize to other optional attrs; creates a third pattern (Stream uses omission, providers use sentinel, seeds add guards) |

Rejected: the footgun is the deciding factor. A single forgotten `has_X` clause re-introduces the bug class.

### Option (b): Provider-level omission (this ADR)

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Unifies practice with `StreamProvider`; automatic for all future seeds; smaller surface |
| Weaknesses | Provider implementations marginally more verbose |

Accepted.

### Option (c): Belt-and-suspenders (both (a) and (b))

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Defense-in-depth |
| Weaknesses | Redundant given (b) already guarantees the property at the evaluator/provider boundary; adds DSL complexity for no incremental safety |

Rejected: the redundancy is unmotivated. The provider-side fix is the load-bearing one.

## References

- [ADR holomush-iv43: Cedar-Aligned Fail-Safe Type Semantics](holomush-iv43-cedar-aligned-fail-safe-type-semantics.md) — parent decision establishing DSL missing-attr semantics
- [`internal/access/policy/dsl/evaluator.go`](../../internal/access/policy/dsl/evaluator.go) — `evalComparison` (line 128), `resolveAttrRef` (line 401)
- [`internal/access/policy/attribute/stream.go`](../../internal/access/policy/attribute/stream.go) — existing precedent for the pattern (lines 46-48)
- [`internal/access/policy/seed.go`](../../internal/access/policy/seed.go) — colocation seeds affected by the bug class (lines 50, 56, 110)
- bead `holomush-9gtl` — original bug report and fix bead
- bead `holomush-awb3` (filed concurrent with this ADR) — sweep remaining optional attrs across the four providers

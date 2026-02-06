# ADR 0010: Cedar-Aligned Fail-Safe Type Semantics

**Date:** 2026-02-05
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

The ABAC policy DSL uses dynamic typing — attribute values can be strings, numbers, booleans,
or arrays, and attributes may be absent entirely (e.g., a character has no faction, or a
plugin is not loaded). The DSL evaluator must define what happens when a condition references
a missing attribute or compares values of different types.

This decision is security-critical because it determines whether missing data can
accidentally grant access.

### The Hazard

Consider a policy intended to block enemies:

```text
forbid(principal is character, action in ["enter"], resource is location)
when { principal.faction == "enemy" };
```

An admin might write the inverse — "allow anyone who is NOT an enemy":

```text
permit(principal is character, action in ["enter"], resource is location)
when { principal.faction != "enemy" };
```

If a character has no `faction` attribute at all (e.g., a new character, or faction data
failed to load), the behavior of `principal.faction != "enemy"` determines whether they
gain access.

**Why this is safe:** With fail-safe semantics, when the `faction` attribute is missing,
ALL comparisons (including `!=`) evaluate to `false`. So `principal.faction != "enemy"`
evaluates to `false` → the permit never matches → access is denied by default (safe).
However, this may not be what the admin intended. For the correct pattern that explicitly
checks for attribute existence, see the **Defensive Patterns for Negation** section below.

### Options Considered

**Option A: SQL-style NULL semantics**

Missing attributes produce NULL. Comparisons with NULL return NULL (unknown), which is
treated as `false` for `==` but `true` for `!=`.

| Aspect     | Assessment                                                                                                            |
| ---------- | --------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Familiar to SQL developers                                                                                            |
| Weaknesses | `!= "enemy"` returns `true` when attribute is missing — accidentally grants access to characters without faction data |

**Option B: JavaScript-style coercion**

Missing attributes become `undefined`, which coerces to `false` for equality but can produce
surprising results with other operators.

| Aspect     | Assessment                                                                                         |
| ---------- | -------------------------------------------------------------------------------------------------- |
| Strengths  | Familiar to web developers                                                                         |
| Weaknesses | Inconsistent behavior across operators; `undefined != "enemy"` is `true` — same hazard as Option A |

**Option C: Cedar-aligned fail-safe semantics**

Missing attributes cause ALL comparisons to evaluate to `false`, regardless of operator.
This matches Cedar's behavior where a missing attribute produces an error value that makes
the entire condition unsatisfied.

| Aspect     | Assessment                                                                                                                                     |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Missing attributes can never grant access; proven safe by Cedar's formal model; consistent across all operators                                |
| Weaknesses | Counterintuitive — `principal.faction != "enemy"` is `false` when faction is missing; requires explicit existence checks for negation patterns |

## Decision

**Option C: Cedar-aligned fail-safe semantics.**

ALL comparisons involving a missing attribute evaluate to `false`, including `!=`. ALL
comparisons involving a type mismatch evaluate to `false`. The `has` operator is the only
way to check attribute existence.

### Complete Type Behavior

| Scenario                                 | Result  |
| ---------------------------------------- | ------- |
| Attribute missing (any operator)         | `false` |
| Type mismatch (e.g., string > int)       | `false` |
| `!=` with missing attribute              | `false` |
| `==` with missing attribute              | `false` |
| `>`, `>=`, `<`, `<=` on non-number       | `false` |
| `containsAll`/`containsAny` on non-array | `false` |
| `has` on non-existent attribute          | `false` |
| `in` (list) with missing left operand    | `false` |
| `in` (expr) with missing right array     | `false` |

### Defensive Patterns for Negation

```text
// CORRECT: explicitly check existence first
when { principal has faction && principal.faction != "enemy" };

// ALSO CORRECT: use if-then-else with safe default
when { if principal has faction then principal.faction != "enemy" else false };

// WRONG: missing faction silently evaluates to false, denying non-faction characters
// This may be intentional (deny unknowns) but must be a conscious choice
when { principal.faction != "enemy" };
```

## Rationale

**Security over convenience:** The naive `!=` behavior (Option A/B) creates a class of
policies that silently grant access when attributes are absent. A plugin being unloaded, a
database query failing, or a character missing optional data should never widen access. The
fail-safe behavior ensures that missing data always results in denial, which aligns with the
default-deny posture.

**Cedar's formal model validates this:** Cedar's type system was formally verified to
prevent this exact class of vulnerability. By aligning with Cedar's semantics, we inherit
the safety guarantees of their formal model without needing our own verification.

**Plugin safety:** When a plugin providing `reputation.score` is unloaded, any policy
referencing `principal.reputation.score >= 50` silently evaluates to `false`. No access is
granted. No error is thrown. The system degrades safely.

## Consequences

**Positive:**

- Missing attributes can never accidentally grant access
- Plugin unloading or provider failures are automatically fail-safe
- Behavior is consistent and predictable across all operators
- Aligns with Cedar's formally verified semantics

**Negative:**

- `!=` with missing attributes is counterintuitive — admins may expect `!= "enemy"` to be
  `true` when faction is absent
- Negation patterns require explicit `has` checks, adding verbosity
- The `policy test` command becomes essential for admins to verify policy behavior

**Neutral:**

- The `has` operator provides a clear, explicit mechanism for existence checks
- Documentation and the `policy test` command mitigate the learning curve
- This behavior is identical to Cedar, so Cedar documentation serves as supplementary
  reference

## Testing Requirements

Every DSL evaluator test case for every operator MUST include a "missing attribute" variant
that asserts `false`. This is a non-negotiable testing requirement — the fail-safe behavior
is the primary security guarantee of the type system.

## References

- [Full ABAC Architecture Design — Type System](../specs/2026-02-05-full-abac-design.md)
- [Design Decision #28: Cedar-Aligned Missing Attribute Semantics](../specs/2026-02-05-full-abac-design-decisions.md#28-cedar-aligned-missing-attribute-semantics)
- [Cedar Language Specification — Type System](https://docs.cedarpolicy.com/)

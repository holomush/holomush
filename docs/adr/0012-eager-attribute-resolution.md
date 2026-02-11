# ADR 0012: Eager Attribute Resolution with Per-Request Caching

**Date:** 2026-02-05
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

The ABAC engine evaluates policy conditions against attribute bags containing subject,
resource, action, and environment attributes. These attributes come from multiple providers
(character data, location data, plugin data, environment state). The engine must decide
WHEN to resolve attributes: all up front before any policy is evaluated, or on demand as
individual policies reference them.

A single user action (e.g., typing a command) may trigger multiple `Evaluate()` calls:
check command permission, check location entry, check property read. Without caching,
the same subject attributes are re-resolved for each call.

### Options Considered

**Option A: Eager resolution (collect-then-evaluate)**

Call all registered attribute providers before evaluating any policy. Assemble complete
`AttributeBags` with every known attribute for the subject, resource, and environment.
Then evaluate all candidate policies against these bags.

| Aspect     | Assessment                                                                                                                                                        |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Simple, predictable; complete attribute snapshot for every decision; powers audit logging and `policy test` debugging; no ordering dependencies between providers |
| Weaknesses | May fetch attributes that no policy references; all providers called even if no policy needs their data                                                           |

**Option B: Lazy resolution (resolve-on-reference)**

Resolve attributes only when a policy condition references them. Cache resolved values
for subsequent references within the same evaluation.

| Aspect     | Assessment                                                                                                                                                                         |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | More efficient when few attributes are actually needed; avoids unused provider calls                                                                                               |
| Weaknesses | Harder to audit (snapshot is incomplete); provider call ordering depends on policy conditions; cache invalidation is complex; `policy test` cannot show "all available attributes" |

## Decision

**Option A: Eager resolution** with **per-request caching** across multiple `Evaluate()`
calls within the same request context.

### Resolution Flow

1. Parse subject and resource type/ID from `AccessRequest` strings
2. Call all registered `AttributeProvider` implementations for subject resolution
3. Call all registered `AttributeProvider` implementations for resource resolution
4. Call all `EnvironmentProvider` implementations
5. Construct `AttributeBags.Action` from `AccessRequest.Action`
6. Assemble complete `AttributeBags`
7. Evaluate all candidate policies against the bags

### Per-Request Caching

An `AttributeCache` is attached to the request context at the command handler entry point.
Multiple `Evaluate()` calls within the same context share cached provider results.

```go
type AttributeCache struct {
    mu    sync.RWMutex
    items map[cacheKey]map[string]any
}

type cacheKey struct {
    entityType string
    entityID   string
}
```

- **Scope:** Per `context.Context` — garbage-collected when the context is cancelled
- **Key:** `{entityType, entityID}` tuple
- **Invalidation:** None within a request. The cache assumes read-only world state during
  request processing
- **Provider isolation:** Each provider's results are cached independently. A plugin
  provider failure does not invalidate cached core provider results

## Rationale

**Audit completeness:** Every `Decision` includes a complete `AttributeBags` snapshot. The
audit log records exactly what the engine knew when it made the decision. With lazy
resolution, the snapshot only contains attributes that happened to be referenced — missing
context that makes "why was I denied?" investigations incomplete.

**`policy test` debugging:** The `policy test` command shows all resolved attributes before
evaluating policies. Admins see every attribute available for the subject and resource, not
just the ones current policies reference. This is essential for authoring new policies —
admins need to know what attributes exist.

**Scale appropriateness:** At ~200 concurrent users with ~10 core attributes per entity and
~20 providers total, eager resolution adds <2ms per evaluation. The per-request cache
eliminates redundant resolution when a single user action triggers multiple authorization
checks (common: 3-5 checks per command).

**Simplicity:** Eager resolution has no ordering dependencies between providers. Provider A
does not need to run before Provider B. All providers are called, results are merged, and
evaluation proceeds. Lazy resolution introduces implicit ordering based on which policy
conditions are evaluated first.

### Stale Data Assumption

The cache assumes world state does not change during request processing. If a command
modifies a character's location, subsequent authorization checks in the same request use
the pre-modification location. This is consistent with the eager resolution model —
attributes are a point-in-time snapshot.

For MUSH workloads, this stale window (~10ms per request) is acceptable. A character
moving between rooms and immediately checking access at the new location within the same
request cycle is an edge case that resolves on the next request.

## Consequences

**Positive:**

- Complete attribute snapshot for every decision — full audit trail
- `policy test` shows all available attributes, not just referenced ones
- No provider ordering dependencies — simpler to reason about and test
- Per-request cache eliminates redundant provider calls within a user action
- Provider errors are detected up front, not mid-evaluation

**Negative:**

- Unused attributes are resolved (wasted work for providers nobody references)
- Per-request cache assumes read-only world state during request processing
- All providers are called even if the policy set only uses core attributes

**Neutral:**

- Performance impact is negligible at MUSH scale (<2ms cold, <100μs warm)
- Future optimization: if profiling shows waste, introduce provider-to-policy affinity
  (only call providers whose namespace appears in active policies). Deferred until needed

## References

- [Full ABAC Architecture Design — Attribute Resolution](../specs/2026-02-05-full-abac-design.md)
- [Design Decision #3: Attribute Resolution Strategy](../specs/decisions/epic7/general/003-attribute-resolution-strategy.md)
- [Design Decision #26: Per-Request Attribute Caching](../specs/decisions/epic7/phase-7.3/026-per-request-attribute-caching.md)

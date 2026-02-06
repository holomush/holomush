# ADR 0009: Custom Go-Native ABAC Engine

**Date:** 2026-02-05
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

HoloMUSH needs to evolve from static role-based access control (Epic 3) to a full
Attribute-Based Access Control (ABAC) system. The domain is heavily attribute-driven:
faction gates, level requirements, property visibility, time-of-day restrictions, and
plugin-contributed attributes. At the expected scale of ~200 concurrent users, the system
must be expressive enough for game administrators while remaining maintainable by a small
contributor team.

Several existing authorization frameworks were evaluated:

### Options Considered

**Option A: Embed OpenFGA (Zanzibar-style ReBAC)**

OpenFGA is a mature Go-native authorization engine with a PostgreSQL backend, designed for
relationship-based access control (ReBAC). It excels at graph traversal queries like
"is user X a member of group Y?"

| Aspect     | Assessment                                                                                                                                                   |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Strengths  | Mature engine, Go-native, PostgreSQL backend, good for org charts                                                                                            |
| Weaknesses | ReBAC-first model is awkward for attribute comparisons (`level >= 5`, `faction == resource.faction`); limited condition language; heavyweight for ~200 users |

**Option B: OPA with Rego**

The Open Policy Agent uses the Rego language, a Turing-complete policy language based on
Datalog. It can express any authorization model.

| Aspect     | Assessment                                                                                                                                        |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Extremely flexible, large ecosystem, well-tested                                                                                                  |
| Weaknesses | Rego is hard for non-engineer game admins to read and write; overkill for MUSH domain; sidecar or embedded Go library adds operational complexity |

**Option C: Cedar (AWS Verified Permissions)**

Cedar is a purpose-built policy language with formal verification. Its model directly
inspired the HoloMUSH DSL design.

| Aspect     | Assessment                                                                                                                   |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Clean, readable syntax; formal model; well-documented semantics                                                              |
| Weaknesses | Rust-only implementation; Go integration requires CGO bridge or gRPC sidecar; cannot tightly integrate with Go plugin system |

**Option D: Custom Go-native ABAC engine with Cedar-inspired DSL**

Build a custom authorization engine in Go with a policy language inspired by Cedar's syntax
and semantics, tailored to the MUSH domain.

| Aspect     | Assessment                                                                                                                                        |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Full control over DSL; tight plugin integration via `AttributeProvider`; no impedance mismatch; readable by game admins; no external dependencies |
| Weaknesses | Parser and evaluator must be built and maintained; no formal verification; smaller community than established frameworks                          |

## Decision

**Option D: Custom Go-native ABAC engine with Cedar-inspired DSL.**

The engine implements `AccessPolicyEngine.Evaluate(ctx, AccessRequest) (Decision, error)` as
the single authorization entry point. Policies are authored in a Cedar-inspired DSL stored in
PostgreSQL, compiled to an AST at creation time, and evaluated against dynamically resolved
attribute bags.

## Rationale

**Domain fit:** HoloMUSH authorization is attribute-driven, not relationship-driven. The
primary patterns are attribute comparisons (`principal.faction == resource.faction`,
`principal.level >= 5`, `resource.visibility == "private"`), set membership
(`principal.flags.containsAny(["healer"])`), and conditional logic
(`if resource.restricted then principal.level >= 5 else true`). OpenFGA's graph traversal
model would require modeling every attribute comparison as a synthetic relationship, creating
impedance mismatch.

**Plugin integration:** Attribute providers register directly with the engine and contribute
attributes synchronously during eager resolution. With an external framework, plugin
attributes would need to cross a serialization boundary (gRPC, JSON) on every evaluation,
adding latency and complexity.

**Admin readability:** The Cedar-inspired DSL reads close to English:
`permit(principal is character, action in ["enter"], resource is location) when { ... }`.
Game administrators (non-engineers) can read and author policies. Rego's Datalog-based syntax
would require training that is disproportionate for a MUSH community.

**Scale appropriateness:** At ~200 concurrent users with ~50 active policies, a custom engine
with in-memory policy caching achieves <5ms p99 evaluation latency. The operational overhead
of OpenFGA or OPA (separate processes, health monitoring, version management) is not justified
at this scale.

**Key insight:** Relationships that frameworks like OpenFGA model as graph edges can be
modeled as attributes resolved at evaluation time. The existing `LocationResolver` already
performs this pattern — resolving `$here` to a location ID dynamically. The ABAC engine's
`AttributeProvider` generalizes this: instead of replacing tokens in strings, providers
resolve full attribute bags for any entity.

## Consequences

**Positive:**

- Full control over DSL syntax and semantics — can evolve the language as MUSH needs emerge
- Zero serialization overhead between authorization and the rest of the Go codebase
- Plugin attribute providers use the same Go interface as core providers
- No external process to deploy, monitor, or version-manage
- Policy language tailored to MUSH domain (factions, levels, flags, properties)

**Negative:**

- Parser and evaluator must be built from scratch:
  - ~2,000 lines for core engine (parser + evaluator)
  - ~1,000 additional for lock compilation, token registry, and audit infrastructure
  - Total estimate: 3,000-3,500 lines
- No formal verification of policy correctness (Cedar has this; we rely on testing)
- Smaller community — bugs and edge cases must be found by HoloMUSH contributors
- Future contributors must learn a custom DSL rather than an industry-standard language

**Neutral:**

- The Cedar-inspired syntax is documented publicly and follows Cedar's formal model closely
- Migration to Cedar proper remains possible if a Go-native Cedar implementation emerges
- The `policy test` command provides runtime policy debugging in lieu of formal verification

## References

- [Full ABAC Architecture Design](../specs/2026-02-05-full-abac-design.md)
- [Design Decision #1: Policy Engine Approach](../specs/2026-02-05-full-abac-design-decisions.md#1-policy-engine-approach)
- [Design Decision #2: Policy Definition Format](../specs/2026-02-05-full-abac-design-decisions.md#2-policy-definition-format)
- [Cedar Language Specification](https://docs.cedarpolicy.com/)

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 1. Policy Engine Approach

> [Back to Decision Index](../README.md)

**Question:** Should HoloMUSH adopt an existing authorization framework or build
a custom engine?

**Options considered:**

| Option | Description                            | Pros                                                                                   | Cons                                                                                                           |
| ------ | -------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| A      | Embed OpenFGA + custom attribute layer | Mature engine, Go-native, PostgreSQL backend, good for relationship graphs             | ReBAC-first model is awkward for attribute comparisons; limited condition language; heavyweight for ~200 users |
| B      | Custom ABAC engine, Cedar-inspired DSL | Full control, tight plugin integration, no impedance mismatch, readable by game admins | More upfront work to build and maintain                                                                        |

**Also evaluated:** Cedar (Rust-only, would need CGO/sidecar), OPA/Rego
(Turing-complete but hard for game admins to read/write).

**Decision:** **Option B — Custom ABAC engine.**

**Rationale:** HoloMUSH's domain is heavily attribute-driven (faction checks,
level gates, property visibility), not relationship-driven. OpenFGA's strength
is graph traversal (org hierarchies, document sharing), which isn't the primary
pattern here. At ~200 concurrent users, we don't need Zanzibar-scale
infrastructure. A custom engine gives full control over the DSL, tight
integration with the plugin system, and a policy language readable by non-engineer
game admins.

**Key insight:** Relationships can be modeled as attributes that get resolved at
evaluation time. The existing `LocationResolver` performs token replacement for
`$here` in glob patterns — a form of dynamic context resolution. The ABAC
engine's `AttributeProvider` generalizes this concept: instead of replacing
tokens in strings, providers resolve full attribute bags for any entity.

# Scope plugin Evaluate entitlement to owned resource types

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-61rdl
**Deciders:** Sean Brandt

## Context

A plugin that can call `Evaluate` with an arbitrary resource type could use the host ABAC engine as a cross-domain policy oracle — probing decisions for resources outside its concern (e.g., a scene plugin asking about server-admin resources), leaking policy shape and attribute existence across domain boundaries. The 2026-04-06 trust-boundary work enforced resource-type ownership for policy *installation* but not for *evaluation* calls.

## Decision

The host rejects any `Evaluate` call whose resource type is not declared in the requesting plugin's manifest `resource_types`, with a `command` carve-out for the plugin's own commands. The check runs before the engine. The rule is one line of host code applied **identically** to both runtimes; the behavioral difference between binary and Lua plugins is purely an artifact of manifest data (Lua plugins structurally cannot declare `resource_types` or implement `AttributeResolver`, so their effective entitlement degrades to the `command` carve-out).

## Rationale

- The same trust boundary already governs policy installation; extending it to evaluation is the consistent move (contributors reason about one model).
- A plugin with full data access to its own resources gains no new information from "can actor X do Y on *my* resource" — that answer is exactly what it needs to gate. Cross-domain probing is what leaks.
- Lua degradation falls out of the existing architecture without a runtime-specific trust rule, preserving the plugin-runtime-symmetry invariant ("runtime-specific code is acceptable for runtime-specific concerns … but MUST NOT differ in policy / trust / manifest-gate dimensions").

## Alternatives Considered

- **Free-form resource type (no entitlement check on Evaluate).** Rejected — turns the plugin into a policy oracle for any resource type, enabling cross-domain information leakage. The legitimate cross-domain-query use case is deferred to a future higher-risk design.

## Consequences

- **Positive:** cross-domain oracle attacks are structurally blocked before the engine runs; one entitlement rule spans both runtimes; consistent with the 2026-04-06 installation boundary.
- **Negative:** instance-level `Evaluate` is effectively binary-plugin-only in v1; a plugin cannot evaluate against a sibling plugin's resource even when legitimate.
- **Neutral:** action strings stay free-form; an unmatched action default-denies via the engine.

## References

- Spec: `docs/superpowers/specs/2026-05-25-plugin-host-evaluate-design.md` §3
- Design bead: holomush-8kkv5
- Related: `docs/superpowers/specs/2026-04-06-plugin-abac-trust-boundary-design.md` §2.1; `.claude/rules/plugin-runtime-symmetry.md`

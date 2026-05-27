<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Separate authorization (WHO) from business-state validity (WHEN) in scene policies

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-sqpnv
**Deciders:** Sean Brandt

## Context

PR #4266 removed `resource.scene.state in [...]` conditions from five core-scenes per-action ABAC policies (end/pause/resume/update/transfer-ownership) in `plugins/core-scenes/plugin.yaml`. Security and ABAC review confirmed this is safe because business-state validity is independently enforced by the store layer: every affected store method executes SQL `WHERE state IN ('active','paused')` and `classifyTransitionMiss` converts any state mismatch into a typed error at the persistence boundary.

The removal was introduced as an E2E fix with no accompanying architectural record, leaving the separation principle implicit. Readers of `plugin.yaml` see policies without state clauses and must independently discover the store enforcement to understand why those clauses are absent.

## Decision

ABAC policies (defined in `plugin.yaml` per-action blocks) gate **WHO** — identity conditions such as ownership, participant status, and role. They MUST NOT duplicate business-state validity checks that the store layer already owns. State enforcement lives exclusively in the store (SQL guards + `classifyTransitionMiss`), which is the single authoritative boundary for **WHEN** a transition is valid.

Two exceptions hold: `invite` and `kick` policies retain their `resource.scene.state in [...]` conditions because their corresponding store methods do not enforce state via SQL guards. For those actions the policy is the only enforcement point, so the clause is load-bearing.

## Rationale

- The store SQL guard is the natural locus for state enforcement: it executes inside the same transaction as the mutation, cannot be bypassed by a policy gap, and is exercised on every call path regardless of the authorization layer above it.
- Double-enforcement creates drift risk: a future policy edit that broadens or narrows a state clause would silently diverge from the actual store constraint, producing misleading policy text without changing real behavior.
- DSL policies are most readable when they express identity intent cleanly. State clauses that mirror a store constraint add noise without adding safety.
- The invite/kick carve-out is principled: when a store method does not enforce state, the policy is the only guard, and the clause is genuinely load-bearing rather than duplicative.

## Alternatives Considered

- **A: Retain state clauses in all per-action policies (belt-and-suspenders).** Rejected. Duplicating the store constraint creates two authoritative sources for the same invariant. When they diverge (e.g., a policy clause is edited but the SQL is not), the store constraint wins silently — making the policy text misleading without preventing incorrect transitions. Double-enforcement adds maintenance surface with no safety gain for actions whose stores already guard state.
- **B: Remove state clauses from all per-action policies including invite/kick.** Rejected. Invite and kick store methods do not run SQL state guards, so removing their policy clauses would leave those transitions ungated on scene state. The store must be the definitive enforcement point; when it is absent, the policy clause is the enforcement point.

## Consequences

- **Positive:** DSL policies cleanly express identity (WHO); the store is the unambiguous authority for state (WHEN). No drift can occur between a policy state clause and the real store constraint for the guarded actions, because there is no policy state clause to drift.
- **Positive:** New per-action policies added in the future have a clear rule: add state clauses only when the corresponding store method does not enforce state.
- **Negative/neutral:** A reader of `plugin.yaml` who is unaware of this ADR may wonder why state conditions are absent from end/pause/resume/update/transfer-ownership. The rule must be consulted (or commented inline) to understand the design intent.
- **Neutral:** The invite/kick exception must be tracked: if those store methods are later refactored to add SQL state guards, their policy clauses become duplicative and should be removed.

## References

- PR #4266 (E2E fix that removed the state clauses)
- `plugins/core-scenes/plugin.yaml` — per-action policy definitions
- `plugins/core-scenes/store.go` — SQL state guards and `classifyTransitionMiss`
- Spec: `docs/superpowers/specs/2026-05-25-plugin-host-evaluate-design.md`

<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-ptf7b; do not edit manually; use `/adr update holomush-ptf7b` -->

# o262d DAG-resolution failures stay always-fatal behind a named policy-function seam

**Date:** 2026-06-14
**Status:** Accepted
**Decision:** holomush-ptf7b
**Deciders:** Sean Brandt

## Context

With INV-PLUGIN-43 live, DAG-resolution failures are already fatal at boot (`internal/plugin/manager.go` `resolveLoadOrder`). The remaining question for o262d was whether to let a `gracefulDegradation` flag quarantine individual plugins with unsatisfied dependencies (making fatal a policy choice) or keep fatal unconditional while making the decision point a named function. The `holomush-dfkca` ADR settled the structured-result + fail-fast default in sub-spec 1; this decision settles the factoring of the fatal decision itself.

## Decision

DAG-resolution failures remain unconditionally fatal. The fatal decision is extracted into an explicit policy function (`defaultResolvePolicy`) over the structured `ResolveResult`, so a future `gracefulDegradation`-gated per-plugin quarantine is a one-point swap, not a resolver rewrite. `holomush-o262d` is closed.

## Rationale

- `gracefulDegradation`'s scope is defined as per-plugin LOAD failures (`manager.go ~575-585`); silently extending it to DAG resolution would cross a documented boundary without a spec.
- A named policy function costs nothing structurally and eliminates the future resolver-rewrite risk.
- All four `ResolveDependencyOrder` error classes mapping to fatal with test coverage — the seam also improves testability of the policy decision.

## Alternatives Considered

- **Always-fatal with policy-function seam (chosen):** Keeps INV-PLUGIN-43 intact; the named function is a clean swap point for a future quarantine. Mild over-engineering risk (forward-looking affordance, no immediate consumer).
- **gracefulDegradation-gated now (rejected):** Gives operators per-plugin quarantine immediately, but complicates the loader before the use-case is validated and widens `gracefulDegradation`'s documented scope as a design change, not a bug fix.

## Consequences

- Positive: future gracefulDegradation-gated quarantine is a one-function swap; all error-class-to-behavior mappings are explicitly tested; the scope boundary is documented, not silently widened.
- Negative: per-plugin quarantine for unsatisfied deps remains unavailable until the future swap.
- Neutral: the INV-PLUGIN-43 binding is unchanged; the seam is additive. Distinct from `holomush-dfkca` (which settled fail-fast); this settles the seam factoring.

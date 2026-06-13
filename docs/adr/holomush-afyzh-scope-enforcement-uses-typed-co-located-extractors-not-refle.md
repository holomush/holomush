<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-afyzh; do not edit manually; use `/adr update holomush-afyzh` -->

# Scope enforcement uses typed co-located extractors, not reflective broker introspection

**Date:** 2026-06-13
**Status:** Accepted
**Decision:** holomush-afyzh
**Deciders:** Sean Brandt

## Context

scope: instance narrowing must extract the concrete resource of a call (e.g. which location a world.mutation touches) to compare against the dispatch-context attribute. Two topologies were candidates: a reflective broker interceptor parsing request fields via proto reflection, or typed accessor functions co-located with each scope-eligible handler in the in-tree host.v1 servers.

## Decision

Each scope-eligible capability method carries a typed ScopedResourceFn extractor registered in the CapabilityDescriptor. The shared host.v1 interceptor denies any scoped call whose method lacks a wired extractor (fail-closed). A CI meta-test enumerates every scope-eligible method and fails the build if any lacks an extractor. Binds INV-PLUGIN-52.

## Rationale

- Reflective extraction in the broker fails OPEN on a missing field-mapping — the worst failure mode for a security gate.
- Typed extractors are co-located with handlers; they cannot drift, and the compiler catches type mismatches.
- All host.v1 servers are in-tree, so the meta-test has complete visibility; out-of-tree plugins cannot introduce unguarded scope-eligible methods.
- Three-way fail-closed (runtime deny + CI meta-test + INV-PLUGIN-52) ensures no silent un-enforcement across refactors.

## Alternatives Considered

- **Unified reflective broker interceptor:** single interception point, but relocates per-method extraction into a reflective seam + out-of-band field-mapping registry that drifts and fails open. Rejected.
- **Typed co-located ScopedResource extractor (chosen):** typed, drift-proof, fail-closed by default, exhaustively meta-testable because host.v1 is in-tree.

## Consequences

Positive: no silent fail-open — a missing extractor denies, never leaks unscoped; extractor completeness is a build-time guarantee not a review concern; no proto-reflection fragility in the security path. Negative: every new scope-eligible method must register an extractor (omission is a deny/outage, not a silent hole); the CapabilityDescriptor must stay current. Neutral: the descriptor is also the single host-owned source for M1 action/resource and M2 operation class — one table serves all three mechanisms.

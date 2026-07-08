<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-cfqxp; do not edit manually; use `/adr update holomush-cfqxp` -->

# Use a caching session.Store decorator for focus, not registry or session Info

**Date:** 2026-07-08
**Status:** Accepted
**Decision:** holomush-cfqxp
**Deciders:** Sean Brandt

## Context

The dispatch hot path (holomush-wm0fi) issues a synchronous Postgres GetConnection per pose/say/ooc/emit to read a connection's focus kind. Two existing in-memory structures were evaluated as reuse candidates before committing to a new caching layer: the per-connection SessionStreamRegistry (internal/grpc/stream_registry.go) and the already-loaded session Info.PresentingFocus.

## Decision

Add a caching session.Store decorator (cachingSessionStore) plus a cache-first FocusReader over a shared ConnectionFocusCache, rather than reusing SessionStreamRegistry or session Info.

## Rationale

- Both rejected alternatives are structurally the wrong layer to hold a per-connection focus projection consulted by the dispatcher.
- Design C's substitution would reintroduce the exact routing-bug class the fail-closed focus-read fix (INV-SCENE-67, ADR holomush-pbp9j) just closed.
- The decorator keeps the invalidation invariant at the store layer where every focus write is already chokepointed, with a single shared *ConnectionFocusCache pointer as the only coupling — no cross-package eviction wiring.

## Alternatives Considered

- **Design B — reuse SessionStreamRegistry (rejected):** already in-memory and per-connection, but its entry exists only while a live Subscribe transport is open, is not seeded with focus at registration, and leaves a dispatch-without-live-Subscribe gap needing a DB fallback — so it neither cleanly eliminates the read nor keeps the invariant in the right layer.
- **Design C — reuse session Info.PresentingFocus (rejected):** already loaded per session, but PresentingFocus is per-session and D9-gated, not the per-connection Connection.FocusKey the redirect needs; substituting it reintroduces the INV-SCENE-67 routing-bug class.
- **Design A — caching session.Store decorator (chosen):** single coupling point via a shared cache pointer; keeps the invariant at the write-chokepoint layer; no cross-package eviction wiring.

## Consequences

- Positive: dispatch performs no synchronous DB round-trip for focus on a cache hit (spec G1).
- Negative: introduces a new stateful component (mutex + size cap) that must be unit-tested in isolation.
- Neutral: cachingSessionStore and cachingFocusReader share one *ConnectionFocusCache pointer as their only coupling, injected once at CoreServer setup.

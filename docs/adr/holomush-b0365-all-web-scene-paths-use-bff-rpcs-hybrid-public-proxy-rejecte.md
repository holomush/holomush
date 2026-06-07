<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-b0365; do not edit manually; use `/adr update holomush-b0365` -->

# All Web Scene Paths Use BFF RPCs; Hybrid Public-Proxy Rejected

**Date:** 2026-06-07
**Status:** Accepted
**Decision:** holomush-b0365
**Deciders:** Sean Brandt (seanb4t)

## Context

SceneService RPCs accept explicit character_id/player_id fields and trust the caller to have enforced ABAC (service.go: "the caller (host) is responsible"). A raw ConnectRPC proxy on the web mux would let a browser act as any character — privilege escalation. The brainstorm initially chose a hybrid: thin proxy for the status-gated public-archive RPCs + Web* BFF for authed flows. The auth-boundary decision ("public" = authenticated non-guest players, never anonymous) eliminated the hybrid's premise.

## Decision

All web scene reads and writes go through Web* BFF RPCs on WebService, each a thin passthrough to a core-side scene-access facade (internal/grpc) that resolves the session to an authentic player, verifies alt ownership, overrides any client-supplied character_id/player_id (INV-SCENE-63), applies the guest gate (INV-SCENE-64), and only then calls SceneService.

## Rationale

- SceneService documents that callers own ABAC; a raw proxy bypasses that contract.
- Once "public" meant authenticated non-guest, the hybrid was two mechanisms for one guarantee.
- Mirrors the established WebListFocusPresence→CoreClient / WebGetContent→ContentClient precedent; the gateway stays a translator (gateway-boundary rule).
- INV-SCENE-63 is only enforceable at a facade that owns identity derivation.

## Alternatives Considered

**Raw ConnectRPC proxy (UnknownServiceHandler)** — rejected: zero new proto but client-supplied character_id with zero ABAC = privesc.

**Hybrid thin-proxy(public) + BFF(authed)** — initially chosen, then superseded: its unauthenticated-public premise was removed by the auth-boundary decision.

**Uniform Web* BFF (chosen)** — single authorization pattern, uniform error opacity; costs more proto surface and a facade to test.

## Consequences

- Positive: INV-SCENE-63 structurally guaranteed on every path; single error-opacity pattern; gateway boundary upheld.
- Negative: ~eight new Web* RPCs with full doc comments; facade layer to build and review.
- Neutral: Handler gains a SceneAccessClient mirroring WithContentClient.

Spec: 2026-06-07-web-portal-scenes-design.md §3 D5, §5.2. Bead: holomush-5rh.8.

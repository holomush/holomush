<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-l5bqb; do not edit manually; use `/adr update holomush-l5bqb` -->

# Extract HostCapabilities port; relocate host.v1 servers to runtime-neutral package

**Date:** 2026-06-12
**Status:** Accepted
**Decision:** holomush-l5bqb
**Deciders:** Sean Brandt

## Context

Nine host.v1 server impls lived inside internal/plugin/goplugin/host_capability_servers.go, embedding hostCapabilityBase{ host *goplugin.Host }. INV-PLUGIN-49 requires both runtimes to consume the SAME server implementations, not merely the same proto contract. The lua runtime does not import goplugin (clean sibling layering) and holds a parallel backing on hostfunc.Functions. Surfaced at plan-time for sub-spec 3 (holomush-eykuh.2).

## Decision

Define a method-narrow HostCapabilities port (only the operations the 9+3 servers actually call, derived from the s.host.*/b.host.* call inventory). Relocate all host.v1 server impls to a runtime-neutral package internal/plugin/hostcap/. Provide two adapters: goplugin.Host (binary) and a new hostfunc.Functions-backed adapter (Lua). Both runtimes construct the same hostcap servers through their adapter. The dispatch-token concern stays adapter-specific (binary wires emitTokenStore; Lua supplies connection-scoped identity via core.ActorFromContext).

## Rationale

INV-PLUGIN-49 at the server level (not just contract level) requires a single handler body — ports & adapters is the minimal change that achieves it. A method-narrow port keeps the decoupling real rather than nominal (no full goplugin.Host surface leaks into hostcap). Behavior-preserving for binary: the existing goplugin host-capability tests pass unchanged after relocation. Asymmetry exists only where a forgery surface exists, honoring the plugin-runtime-symmetry rule.

## Alternatives Considered

Second server impl wrapping hostfunc.Functions (Lua-specific) — REJECTED: exactly the runtime-specific capability surface INV-PLUGIN-49 forbids; duplicates handler logic across two drift-prone impls. Lua runtime imports goplugin and reuses servers directly — REJECTED: wrong dependency direction (sibling lua → goplugin) that violates the clean layering keeping the two runtimes independently testable/deployable.

## Consequences

Positive: INV-PLUGIN-49 holds at the server-impl level; a future third runtime needs only a new adapter, not new server impls; hostcap depends only on the port. Negative: ~14-method interface extracted from goplugin.Host (wide refactor surface); two private Host methods (identityRegistrySnapshot, ownedResourceTypes) promoted to the interface. Neutral: three new servers (Session/Property/World) are added in the same move — they are the first Lua consumers with no prior binary impl to displace.

<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-e9go5; do not edit manually; use `/adr update holomush-e9go5` -->

# Dedicated holomush.plugin.host.v1 namespace; WorldService kept as coexisting domain service

**Date:** 2026-06-12
**Status:** Accepted
**Decision:** holomush-e9go5
**Deciders:** Sean Brandt

## Context

Decomposed capability services need a proto namespace. The existing holomush.world.v1.WorldService already exposes host-to-binary world reads (4 RPCs, different names and surface from the Lua query hostfuncs). The namespace decision determines auditability and whether the Lua world-query surface and WorldService are unified or coexist.

## Decision

All 14 capability services live in a fresh dedicated proto package holomush.plugin.host.v1 (api/proto/holomush/plugin/host/v1/). holomush.world.v1.WorldService is left unchanged and reconciliation of the two world-read surfaces is explicitly deferred to the migration sub-spec (5).

## Rationale

- A dedicated namespace makes the full host-capability surface auditable in one directory without scanning plugin.v1 for mixed responsibilities.
- The Lua world-query surface (QueryLocation, QueryCharacter, QueryLocationCharacters, QueryObject, FindLocation) is richer and differently named than WorldService; fresh contracts avoid a rename-and-migration in this sub-spec.
- WorldService reconciliation belongs to sub-spec 5 (manifest migration), which already touches core-scenes broker consumption.
- INV-PLUGIN-47 becomes a testable namespace assertion: no host.v1 service spans two domains and PluginHostService does not exist.

## Alternatives Considered

**Reuse holomush.world.v1.WorldService as world.query** — REJECTED: differing RPC names/surface would force a rename (breaking core-scenes) or leave the Lua surface inconsistent; couples taxonomy work to an out-of-scope migration. **Extend the existing plugin.v1 namespace** — REJECTED: mixes plugin-implemented contracts (PluginService, PluginAuditService) with host-provided capability services, making the host-capability family non-auditable as a unit.

## Consequences

Positive: all capability services auditable in one directory; no forced core-scenes changes during taxonomy work; INV-PLUGIN-47/48 are trivial namespace-level structural assertions. Negative: a temporary dual world-read surface (host.v1 WorldQueryService and world.v1 WorldService) that contributors must understand; reconciliation deferred, not avoided. Neutral: the world.v1.WorldService proto is retained unchanged; only its forgeable registry injection was removed earlier (holomush-c6oo8).

<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-2fb90; do not edit manually; use `/adr update holomush-2fb90` -->

# Clean cutover: delete PluginHostService and rewire the binary SDK in sub-spec 2

**Date:** 2026-06-12
**Status:** Accepted
**Decision:** holomush-2fb90
**Deciders:** Sean Brandt

## Context

The PluginHostService god-service must be replaced by 14 per-capability services. Two disposition strategies: add the new services alongside the god-service and delete it later (add-alongside), or delete the god-service and rewire its only binary consumer atomically in the same sub-spec (clean cutover). The binary SDK already speaks gRPC through the broker; Lua reaches host functions through direct hostfunc injection and is unaffected.

## Decision

Clean cutover: each capability service's RPC set is the union of today's Lua + binary surface; PluginHostService is deleted; and the binary SDK (pkg/plugin/*_client.go, event_sink.go, audit.go, sdk.go; plugins/core-scenes; cmd/holomush/core.go) is rewired to per-capability clients atomically within sub-spec 2. The Lua hostfunc surface is left intact for sub-spec 3 (its gRPC transport) and sub-spec 5 (its gating).

## Rationale

- The binary side already speaks gRPC through the broker, so the rewire is mechanical and MUST move with the server to keep the build green; deferring it would leave the build broken.
- Deleting the god-service is the only way to make INV-PLUGIN-47 (PluginHostService MUST NOT exist) immediately testable; add-alongside defers the invariant indefinitely.
- Lua hostfuncs are an independent path (not gRPC through the broker), so leaving them intact for sub-spec 3 is safe.
- Clean cutover matches the structural-elimination precedent of holomush-c6oo8 (remove forgeable injection), not incremental patching.

## Alternatives Considered

**Add-alongside (new services coexist with the god-service; delete deferred)** — REJECTED: any plugin targeting the broker during the dual-surface window can still be granted the full god-service surface, defeating least-privilege; the deletion sub-spec must track which consumers migrated; INV-PLUGIN-47 cannot go green until the deferred delete lands.

## Consequences

Positive: INV-PLUGIN-47 testable immediately after sub-spec 2; no window where the full god-service surface is still grantable; the binary SDK is updated once. Negative: binary SDK rewire adds scope to sub-spec 2 and must preserve authorization semantics verbatim; Lua hostfuncs and host.v1 contracts coexist until sub-spec 3. Neutral: the 22-rehomed + 1-retired RPC accounting is a required completeness deliverable.

<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-ws2mi; do not edit manually; use `/adr update holomush-ws2mi` -->

# Split Lua bridge by proto ownership: codegen host caps, descriptor-driven plugin services

**Date:** 2026-06-12
**Status:** Accepted
**Decision:** holomush-ws2mi
**Deciders:** Sean Brandt

## Context

Lua plugins need a typed call surface to both host.v1 capabilities (host-owned, committed proto) and third-party plugin services (provider-owned, discovered at runtime). A single reflective bridge (dynamicpb everywhere) was the original design; a consumer-side codegen approach was also evaluated (holomush-eykuh.2, sub-spec 3).

## Decision

Split the bridge by proto ownership. HOST CAPABILITIES: build-time codegen over host.v1 descriptors producing concrete typed Go marshalers + the generated typed client stubs. PLUGIN SERVICES: load-time descriptor-driven dynamicpb tables built from the provider's registered FileDescriptors. Both Lua surfaces present the identical namespace.Method{...} call shape; unknown methods fail at load, not first call.

## Rationale

Host.v1 protos are committed and host-owned — codegen is safe and drift-proof (proto changes break the build immediately). Third-party plugin proto is physically unavailable at host build time, so reflective marshaling is the only correct option, not a degraded one. Lua is non-compiled, so consumer-side codegen yields no real type enforcement while regressing the "drop a script, no build step" value proposition. Uniform call shape preserves dev-ex parity regardless of which bridge backs the call.

## Alternatives Considered

Single reflective dynamicpb bridge for all calls — REJECTED: stringly-typed host-cap calls; proto drift not caught at build time; wastes the committed host.v1 contracts as a real type boundary. Consumer-side codegen generating Lua stubs — REJECTED: illusory typing for a non-compiled runtime; breaks the no-build-step value prop; cannot cover arbitrary third-party services unknown at host build.

## Consequences

Positive: host-cap proto drift caught at build time; Lua no-build-step preserved; binary consumers of plugin services keep full static typing (.pb.go import), no regression. Negative: two marshaling paths (codegen + dynamicpb) to maintain/test; a codegen step added to the host build. Neutral: both paths converge on the same per-plugin bufconn transport — the split is in marshaling only, not transport or identity.

<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-vpg8l; do not edit manually; use `/adr update holomush-vpg8l` -->

# Resolver Grants set as the single least-privilege gate authority

**Date:** 2026-06-14
**Status:** Accepted
**Decision:** holomush-vpg8l
**Deciders:** Sean Brandt

## Context

The declaration gate was split per-runtime: the Lua runtime filtered capability injection inside `RegisterHostCaps` (`internal/plugin/lua/host.go:460`) while the binary runtime derived wiring from manifest accessors independently (`internal/plugin/goplugin/host.go:806,847`). Both read the same manifest data but enforced separately, which prevented INV-PLUGIN-45 (single least-privilege gate) from being bound. Three gate architectures were evaluated.

## Decision

`ResolveDependencyOrder` emits a structured per-plugin `Grants[pluginName]` field (granted capabilities + plugin services). The Lua `RegisterHostCaps` shim and the binary broker-wiring loop both consume `Grants[pluginName]` instead of independently re-deriving from the manifest. The per-plugin gRPC server interceptor (R4) is identity-only, not a second gate.

## Rationale

- A single grant-computation point means both runtimes are denied identically on the same data — the structural requirement for INV-PLUGIN-45.
- Moving derivation to the resolver (which already owns dependency validation) avoids duplicating manifest-reading logic across two runtime-specific sites.
- The hostcap gRPC servers already enforce ABAC subject authorization at call time; a second runtime-side gate is redundant and creates two surfaces to keep in sync.

## Alternatives Considered

- **Approach A — resolver emits Grants[plugin] (chosen):** Single computation; both shims consume the same set; INV-PLUGIN-45 binds to a test driving both runtimes through the shared resolver result. Cost: a new `ResolveResult` field + two consumer updates.
- **Interceptor-as-gate, per-call (rejected):** Defense-in-depth at call time, but does not address load-time structural asymmetry; R4 fixes the interceptor's role as identity-only, so using it as a gate creates two gates for one concern.
- **Hybrid — Approach A + interceptor as second gate (rejected):** Recreates the split-enforcement problem the consolidation aims to remove; per-call enforcement is already provided by ABAC at the hostcap servers.

## Consequences

- Positive: INV-PLUGIN-45 becomes testable and bindable; manifest-reading for grants done once; future runtimes only consume `Grants[plugin]`.
- Negative: `ResolveResult` gains a field; both shim call sites must change; the resolver now owns order and grants (scope must stay cohesive).
- Neutral: the interceptor role is narrowed to identity-only (R4), a complementary clarification.

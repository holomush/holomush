<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-vhz3h; do not edit manually; use `/adr update holomush-vhz3h` -->

# Scope Lua stub generator to static build-time surfaces only

**Date:** 2026-06-16
**Status:** Accepted
**Decision:** holomush-vhz3h
**Deciders:** Sean Brandt

## Context

The Lua host-call surface has two distinct populations: statically-registered host.v1 capability namespaces (visible at build time via protoregistry) and provider / plugin→plugin services that only exist at load time, per-plugin. A third population — capability-gated hostfunc families like holo.session.* and the cap_*.go surfaces — is neither descriptor-driven nor always-on ambient. The editor-stub generator (holomush-eykuh.9) must commit to a scope before its architecture is fixed.

## Decision

The stub generator is scoped to statically-visible surfaces only: descriptor-driven host.v1 namespaces and the always-on ambient stdlib (holomush.* / holomush.config / holo.fmt / holo.emit). Provider service stubs and capability-gated hostfunc families (holo.session.*, world_query, property) are explicitly deferred to a future load-time path.

## Rationale

- Provider descriptors are visible only at plugin load time, per-plugin — a static generator structurally cannot see them without a separate runtime mechanism.
- Capability-gated hostfunc families are neither descriptor-driven nor always-on; covering them needs the same load-time path and raises the same drift risk with no parity-test anchor.
- Static scope enables the committed-artifact model (pkg/plugin/luastubs/holomush.lua in VCS) with deterministic regenerate-and-diff drift checking in pr-prep.
- YAGNI: no concrete consumer need for provider or capability-gated stubs exists today.

## Alternatives Considered

- **Static build-time generator only (chosen):** reuses the existing luabridge/gen descriptor walk; no runtime wiring; mechanically drift-checkable. Cannot cover provider or capability-gated surfaces.
- **Load-time / per-plugin generation (rejected):** could cover provider descriptors, but requires a fundamentally different architecture (runtime hook, per-plugin dynamic output, no single committed artifact) — heavy infrastructure for a dev-aid.
- **Single hand-authored .lua file, no generator (rejected):** zero tooling, but drifts immediately as host.v1 evolves with no automated detection.

## Consequences

- Positive: generator is a simple build-time go:generate with no runtime wiring; committed artifact enables a stable LuaLS workspace.library path; drift fully detectable by pr-prep.
- Negative: authors writing against provider services or capability-gated hostfuncs get no autocomplete there; a future load-time path will be a materially different architecture.
- Neutral: the descriptor-driven session host.v1 namespace IS covered (it is a statically-registered host.v1 service); only the hand-written holo.session.* hostfuncs are excluded.

<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-gtkzy; do not edit manually; use `/adr update holomush-gtkzy` -->

# Lua plugin capability/service transport via in-process gRPC over the host broker

**Date:** 2026-06-11
**Status:** Accepted
**Decision:** holomush-gtkzy
**Deciders:** Sean Brandt

## Context

Lua plugins receive most host functions unconditionally via direct VM injection (hostfunc/functions.go:253-265), bypassing any declaration gate; binary plugins consume host capabilities and plugin services via the grpcbroker over mTLS (carrying the plugin ABAC subject). The plugin-runtime-symmetry rule mandates both runtimes share one gate path, and the plugin→host→plugin path (a Lua plugin reaching another plugin's service) has no Go-shim solution.

## Decision

Lua plugins obtain access to host capabilities AND plugin services exclusively through an injected in-process gRPC proxy over the host broker — identical in contract, declaration-gating, and PluginSubject authorization to the binary path. Transport wiring/performance (bufconn, batching, hot-path fast lane) is deferred to sub-spec 3 but cannot reopen this invariant.

## Rationale

- The plugin→host→plugin path requires Lua to reach arbitrary plugin-provided services; no Go shim can exist for an arbitrary plugin's proto contract, so Lua MUST have a brokered gRPC path.
- Once that path exists for plugin services, host capabilities use the same path — one gate, not two.
- A Go-shim transport would split the least-privilege gate (broker for binary, injection for Lua), recreating the privilege asymmetry plugin-runtime-symmetry exists to prevent.
- It makes 'every capability is a gRPC contract' the REAL consumption path for both runtimes, not nominal.

## Alternatives Considered

**Go shim per capability injected into the Lua VM** — rejected: cannot serve plugin→host→plugin (no shim for an arbitrary plugin service); splits the gate across two paths (drift hazard); makes the gRPC contract nominal for Lua.
**In-process gRPC proxy over the host broker** — CHOSEN.

## Consequences

**Positive:** single least-privilege gate at the broker/registry common path covers both runtimes; Lua can call plugin services (plugin→host→plugin unblocked); symmetry satisfied structurally. **Negative:** gRPC serialization overhead on Lua calls until sub-spec 3 adds a fast lane; unconditional Lua injection must be removed — a breaking change requiring all Lua plugins to declare capabilities (sub-spec 5). **Neutral:** the binary grpcbroker transport is reused unchanged; only the Lua side gains a path.

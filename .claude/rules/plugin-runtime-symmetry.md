<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

---
paths:
  - "internal/plugin/**"
  - "pkg/plugin/**"
  - "plugins/**"
  - "internal/access/**"
---

# Plugin Runtime Symmetry

**Project invariant: Binary and Lua plugins MUST be treated identically by the host.**

Any host-side trust check, validation, or feature MUST apply to both binary and Lua plugins. Asymmetric behavior between plugin runtimes is forbidden — it creates a privilege gradient that violates the core plugin-system design.

## When designing security or authorization features that touch plugins

1. **Find the common code path** that handles both runtimes (e.g., `internal/plugin/event_emitter.go::Emit` is the shared emit boundary for both Lua return-value emits and binary gRPC emits).
2. **Place the gate at the common path** so both runtimes are enforced uniformly.
3. **Runtime-specific code is acceptable for runtime-specific concerns** (e.g., the gRPC token mechanism for binary plugins, Lua state lifecycle), but MUST NOT differ in policy / trust / manifest-gate dimensions.

## Reference example

`holomush-ec22.1`: the `actor_kinds_claimable` manifest gate fires at `event_emitter.go::Emit` for both runtimes; the supplemental token-authentication mechanism applies only to the binary gRPC `EmitEvent` boundary because that's where the forgery surface exists. Both runtimes reach the same policy enforcement.

## Host RPC parity

Every new `PluginHostService` RPC MUST ship both the Go SDK method and the Lua hostfunc together. Adding only one creates a privilege gradient: the runtime that has the method gets the capability, the other doesn't. Same root cause as the broader symmetry invariant.

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

## Permitted asymmetry: same capability, different transport

The invariant is about **trust, policy, and manifest-gate enforcement** — NOT about the wire transport a runtime uses to reach a capability. Two runtimes may reach the *same* ABAC-gated capability through *different* mechanisms without creating a privilege gradient, **provided the policy chokepoint is identical**. This is a permitted asymmetry, not a violation.

**Canonical case — world reads (`WorldQuerier`).** The two runtimes reach world-model reads (`GetLocation` / `GetCharacter` / `GetCharactersByLocation` / `GetObject`) by different transports:

| Runtime | Transport | Reaches |
| --- | --- | --- |
| **Lua** | `world.query` **host capability** (`hostfunc.WorldQuerierAdapter`) | `world.Service.*` → `checkAccess(subject, …)` |
| **Binary** | `holomush.world.v1.WorldService` **service** (broker-dialed from manifest `requires:`) | `world.Service.*` → `checkAccess(subject, …)` |

Both funnel through the **same** `world.Service` ABAC chokepoint with the same plugin-subject stamping. So `goplugin.Host.WorldQuerier()` returning **nil** (binary host exposes no world *host-capability* surface) is **intentional and correct** — binary plugins use the service transport. It is NOT a parity gap.

> **Do NOT "fix" the nil binary `WorldQuerier` by building a binary world *host-capability* surface.** That re-opens deliberately-retired epic `holomush-q42fh` ("Binary World-Query Parity"), whose founding premise — "binary plugins cannot query the world" — is false. A reviewer/agent seeing the nil stub and concluding "binary has no world access" is the exact trap this section exists to prevent (June 2026: a scene-name-resolution design briefly filed a duplicate parity bead off this misread).

**How to tell a permitted asymmetry from a violation:** ask *"does either runtime reach a different policy/trust outcome?"* If both hit the same gate (ABAC chokepoint, manifest capability check, emit fence) and differ only in transport or availability-shape, it is permitted. If one runtime gets a capability, trust level, or gate-bypass the other does not, it is a violation — fix it at the common path.

The fuller catalogue of permitted Lua/binary asymmetries (e.g. manifest-validation gradients: `audit` / `provides` / `storage: postgres` / `resource_types` are Lua-rejected) is tracked by `holomush-dj95.10`.

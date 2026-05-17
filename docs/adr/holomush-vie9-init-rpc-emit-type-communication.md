<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Use Init-RPC Protocol Extension to Communicate Code-Registered Emit Types

**Date:** 2026-05-17
**Status:** Accepted
**Decision:** holomush-vie9
**Deciders:** HoloMUSH Contributors

## Context

The parent substrate-contract ADR [`holomush-3vsb`](holomush-3vsb-manifest-emit-type-startup-validation.md) (INV-S5)
established that the substrate MUST validate set-equality between a
plugin's manifest-declared `crypto.emits` set and the set of types the
plugin's code actually registers, in both directions. That ADR settled
the WHAT. This ADR settles the HOW: by what runtime mechanism does the
substrate learn the plugin's code-registered set?

The mechanism question is non-obvious because the substrate has no
existing seam:

- **Binary plugins** run out-of-process via `hashicorp/go-plugin`
  ([`internal/plugin/goplugin/host.go:420`](../../internal/plugin/goplugin/host.go)).
  Host-side Go method calls on the plugin's struct are unreachable;
  the only communication is gRPC.
- **Lua plugins** have no persistent init phase. Today's Lua Host
  ([`internal/plugin/lua/host.go:111`](../../internal/plugin/lua/host.go))
  does a syntax-check pass in a throwaway state at `Load`, then creates
  a fresh Lua state per `DeliverEvent`/`DeliverCommand`. There is no
  long-lived state in which top-level `register_emit_type` calls could
  accumulate for the substrate to read.

This gap was surfaced by plan-reviewer round 1 of the parent spec's
implementation plan (the original plan handwaved the mechanism with a
TODO placeholder).

## Decision

**Approach X — Init-RPC-driven symmetric mechanism.**

Extend the existing plugin Init lifecycle on both runtimes to carry
the code-registered emit-type set:

**Binary side:** extend `pluginv1.InitResponse` proto with
`registered_emit_types: repeated string` (field 2). Binary plugins with
non-empty `crypto.emits` implement a new opt-in
`pluginsdk.EmitTypeRegistrar` interface; the SDK adapter
(`pkg/plugin/sdk.go::pluginServerAdapter.Init`) auto-populates the
field from the plugin's `EmitRegistry`.

**Lua side:** extend `internal/plugin/lua/host.go::Load` with a SECOND
pass that runs the plugin's top-level code in a stateful Lua state
with a new `holomush.register_emit_type(type)` hostfunc registered.
The hostfunc accumulates calls into a per-plugin `LuaEmitRegistry`
which is stored on the `luaPlugin` struct. The pass fires only for
plugins with non-empty `crypto.emits` (per
[`holomush-7h0c`](holomush-7h0c-lua-load-pass-optin-scope.md)).

**Validator wiring:** add a new `Host.PluginEmitRegistry(name) ([]string, bool)`
method to the substrate's `Host` interface. Both runtimes implement it
(Lua returns from `luaPlugin.emitRegistry`; binary returns from
`loadedPlugin.registeredEmitTypes`). The validator in
`internal/plugin/manager.go::loadPlugin` calls it after `host.Load`
succeeds, runs `ValidateEmitTypeSetEquality`, and fails plugin load
on mismatch (fail-closed from day one).

## Rationale

**Reuses existing Init lifecycle on both runtimes.** No new
inter-process RPC, no build-time codegen pipeline, no static analysis.
The proto field extension is additive; the Lua Load second-pass is a
new lifecycle phase but reuses the existing `factory.NewState` +
`DoString` + hostFuncs.Register infrastructure.

**Symmetric across runtimes per parent ADR's INV-S3** (Go+Lua parity
invariant). Both runtimes expose the same `PluginEmitRegistry` Host
interface method to the validator. The validator is single-source;
runtime asymmetry is confined to the Load-pass shape and the
InitResponse population path.

**Fail-closed from day one.** Per
[`feedback_no_prod_shape_for_undeployed`](../../.claude/projects/-Volumes-Code-github-com-holomush-holomush/memory/feedback_no_prod_shape_for_undeployed.md):
HoloMUSH has no releases, no external users, all plugins in-tree. A
phased "warn first, fail-close later" rollout would be wasted complexity.
Substrate cap and plugin adoptions land in a single coherent change.

**Field extension is backward-additive.** Plugins without `crypto.emits`
leave `RegisteredEmitTypes` empty (proto3 default); the validator's
`INV-M1` gate (`manifest.Crypto != nil && len(manifest.Crypto.Emits) > 0`)
ensures they're never validated. Adding more fields to `InitResponse`
later for other purposes does not conflict.

## Alternatives Considered

**Option X: Init-RPC-driven symmetric mechanism (chosen)**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Reuses existing Init lifecycle on both runtimes. No new IPC. Symmetric across runtimes. Single validator function. Proto extension is additive |
| Weaknesses | Lua plugins with non-empty crypto.emits execute top-level twice at Load. Binary plugins with non-empty crypto.emits must implement EmitTypeRegistrar (catches omission as mismatch, not compile error) |

**Option Y: Manifest sidecar**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | No runtime execution required; a generated file alongside the manifest could list registered types |
| Weaknesses | Requires a build-time codegen step for both Lua and binary plugins. Sidecar can drift from code. Does not reuse any existing lifecycle. Introduces a new file artifact to track and review |

**Option Z: Manifest-only (trust declaration)**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | No new mechanism at all; treat the manifest as ground truth |
| Weaknesses | **Does not detect registered-but-undeclared** — the dangerous failure mode where plugin code emits an event type the manifest never declared and the runtime silently treats it as plaintext. This is the primary failure mode `holomush-3vsb` was designed to catch. Defeats the purpose of INV-S5 |

## Consequences

**Positive:**

- Single validator function (`ValidateEmitTypeSetEquality`) handles both runtimes; runtime asymmetry confined to Load-pass shape + InitResponse population.
- Fail-closed from day one: mismatch fails plugin load before plugin enters the manager's ready cache. No silent drift between manifest and code.
- Proto extension is additive — field 2 on `InitResponse` is optional; plugins without `crypto.emits` ignore it.
- Both failure modes from `holomush-3vsb` are mechanically caught: declared-but-unregistered (dead manifest entry) AND registered-but-undeclared (silently plaintext emit).

**Negative:**

- Lua plugins with non-empty `crypto.emits` execute top-level code twice at Load (existing syntax-check pass + new capture pass), imposing a documented idempotency requirement on plugin authors (see `holomush-7h0c`).
- Binary plugins with non-empty `crypto.emits` must implement `EmitTypeRegistrar` interface; omission is detected as a mismatch (empty registered set vs non-empty declared set) rather than a compile-time error.
- The proto extension requires regenerating `pkg/proto/holomush/plugin/v1/plugin.pb.go` via `task proto`.

**Neutral:**

- Host-owned event types (e.g., `pluginsdk.HostEventTypeSystem` at `pkg/plugin/event.go`) must be filtered before comparison; the filter list is centrally maintained.
- The `Host` interface gains one method (`PluginEmitRegistry`); both existing host implementations (`lua.Host`, `goplugin.Host`) implement it.

## References

- [Substrate Contract Spec — INV-S5](../superpowers/specs/2026-05-16-social-spaces-substrate-contract.md)
- [INV-S5 Mechanism Design Spec](../superpowers/specs/2026-05-17-inv-s5-mechanism-design.md)
- [Parent ADR: Startup-Time Set-Equality Validation (`holomush-3vsb`)](holomush-3vsb-manifest-emit-type-startup-validation.md) — the WHAT this ADR settles the HOW for.
- [Sibling ADR: Lua Load Pass Opt-In Scope (`holomush-7h0c`)](holomush-7h0c-lua-load-pass-optin-scope.md) — scope decision for the Lua Load second-pass introduced by this ADR.
- [`.claude/rules/plugin-runtime-symmetry.md`](../../.claude/rules/plugin-runtime-symmetry.md) — parent INV-S3 Go+Lua parity invariant.
- `internal/plugin/lua/host.go:111` — Load entry point modified by this design.
- `internal/plugin/goplugin/host.go:528` — binary Init RPC call site that consumes the new proto field.
- `pkg/plugin/sdk.go:152` — SDK adapter Init method modified to populate `RegisteredEmitTypes`.

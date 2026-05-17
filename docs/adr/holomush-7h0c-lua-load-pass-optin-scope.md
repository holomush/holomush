<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Scope Lua Load Second Pass to crypto.emits-Declaring Plugins Only

**Date:** 2026-05-17
**Status:** Accepted
**Decision:** holomush-7h0c
**Deciders:** HoloMUSH Contributors

## Context

The Init-RPC mechanism for INV-S5 ([`holomush-vie9`](holomush-vie9-init-rpc-emit-type-communication.md))
introduces a new Lua Host lifecycle phase: a Load-time second pass
that runs the plugin's top-level code in a stateful Lua state with a
new `holomush.register_emit_type` hostfunc, capturing registrations
into a per-plugin `LuaEmitRegistry`.

Before this change, the Lua Host's lifecycle was:

1. `Load` — syntax-check the code in a throwaway state.
2. `DeliverEvent` / `DeliverCommand` — create a fresh state, register hostFuncs, `DoString` the code (which executes top-level), call `on_event` / `on_command`.

The new second pass adds Load-time execution of top-level code with a
capture hostfunc — a meaningful change to the Lua plugin model.

The scope question: should ALL Lua plugins run the new second pass at
Load (uniform model), or only plugins that opt in by declaring
non-empty `crypto.emits` (two-tier model)?

The answer determines whether top-level idempotency becomes a
universal Lua plugin constraint or a scoped one.

## Decision

**Opt-in scope.** The Lua Load second pass (capture pass) fires only
when `manifest.Crypto != nil && len(manifest.Crypto.Emits) > 0`
(INV-M1 in the mechanism spec). Plugins without `crypto.emits` skip
the pass entirely and are unaffected by the new lifecycle phase.

Implementation: the gating check lives in both
`internal/plugin/lua/host.go::Load` (skips the second-pass state
creation) and `internal/plugin/manager.go::loadPlugin` (skips the
validator call). The two checks are redundant by design — defense in
depth — but the lua/host.go check is the load-bearing one (avoids the
extra state creation).

## Rationale

**Minimizes new substrate surface** per the mechanism spec's SHOULD
goal (reuse existing lifecycle hooks; don't extend them universally).
Plugins without `crypto.emits` see zero behavior change from this
mechanism. The vast majority of in-tree plugins fall into this case
(verified at spec time: only `core-communication`, `core-objects`,
and `core-scenes` declare `crypto:` at all; only the first two have
non-empty emits).

**Scopes the idempotency constraint to plugins that need it.** The
Lua second pass executes top-level code, so non-idempotent top-level
hostfunc calls (e.g., `holomush.kv_set(...)`, `holomush.create_location(...)`)
would fire BOTH at Load AND per-delivery. This is a meaningful
constraint on plugin authors. Scoping it to crypto.emits-declaring
plugins prevents it from becoming a universal Lua plugin invariant.

**INV-M1 is the gating check at both sites.** The same condition
(`manifest.Crypto != nil && len(manifest.Crypto.Emits) > 0`)
appears in `lua/host.go::Load` (skip the pass) and
`manager.go::loadPlugin` (skip the validator). Making the boundary
explicit and auditable matches the spec's invariant-with-named-enforcement
pattern.

**Two-tier model is self-documenting.** Presence of `crypto.emits` in
a Lua plugin manifest now signals to plugin authors that their
top-level code will run twice at Load and must be idempotent. The
boundary is visible in the manifest, not hidden in a separate config.

## Alternatives Considered

**Option A: Opt-in scope (chosen)**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Zero impact on majority of plugins (no crypto.emits). Idempotency constraint scoped to plugins that opt in. Self-documenting via manifest presence. INV-M1 gate is explicit and auditable |
| Weaknesses | Two-tier Lua plugin lifecycle: plugins with vs without crypto.emits behave differently at Load. Plugin authors adding crypto.emits post-hoc must audit top-level code for idempotency |

**Option B: Universal scope — all Lua plugins always run the capture pass**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Uniform model — all Lua plugins behave identically at Load regardless of crypto.emits presence |
| Weaknesses | Imposes universal idempotency constraint on ALL Lua plugin top-level code even when there's no validation to perform. Slows plugin loading (extra state creation) for the majority that have no crypto.emits. Exposes the capture hostfunc to all plugins, polluting the namespace for the non-validating case |

## Consequences

**Positive:**

- No change to load path for plugins without `crypto.emits` — most current and future plugins unaffected.
- Self-documenting: presence of `crypto.emits` signals to plugin authors that top-level code will run twice at Load.
- INV-M1 gate is explicit; reviewers can audit by grepping for the gate condition.
- The same gate applies to binary plugins (per `holomush-vie9`): plugins without `crypto.emits` don't need to implement `EmitTypeRegistrar` either. Cross-runtime consistency.

**Negative:**

- Two-tier Lua plugin lifecycle: plugins with `crypto.emits` have a different Load lifecycle than those without. Must be documented in `site/docs/extending/` so plugin authors understand the implication of adding `crypto.emits` to an existing plugin.
- Post-hoc `crypto.emits` addition is a non-obvious migration: existing top-level code must be audited for idempotency before adding the manifest declarations.

**Neutral:**

- Defense in depth: the gating check exists in both `lua/host.go::Load` and `manager.go::loadPlugin`. Either check alone would suffice; both together prevent accidental regression.
- The Lua second pass's hostfunc surface is narrower than the per-delivery surface (only `holomush.register_emit_type` is added on top of the standard `Register`).

## References

- [INV-S5 Mechanism Design Spec — §2.2, INV-M1, INV-M4](../superpowers/specs/2026-05-17-inv-s5-mechanism-design.md)
- [Sibling ADR: Init-RPC Emit-Type Communication (`holomush-vie9`)](holomush-vie9-init-rpc-emit-type-communication.md) — the parent mechanism this ADR scopes.
- [Parent ADR: Startup-Time Set-Equality Validation (`holomush-3vsb`)](holomush-3vsb-manifest-emit-type-startup-validation.md) — the original INV-S5 decision.
- `internal/plugin/lua/host.go:111` — Load method gaining the second pass.
- `internal/plugin/manager.go:849` — `loadPlugin` gaining the validator call.

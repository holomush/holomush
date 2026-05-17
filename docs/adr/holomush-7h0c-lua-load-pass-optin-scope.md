<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Scope Lua Load Capture Pass to crypto.emits-Declaring Plugins Only

**Date:** 2026-05-17
**Status:** Accepted
**Decision:** holomush-7h0c
**Deciders:** HoloMUSH Contributors

## Context

The Init-RPC mechanism for INV-S5 ([`holomush-vie9`](holomush-vie9-init-rpc-emit-type-communication.md))
requires Lua plugins to register their emit-type set during plugin
Load so the substrate validator can compare it against the manifest's
declared `crypto.emits` set.

The existing Lua Host Load lifecycle runs a syntax-check pass in a
throwaway state at `internal/plugin/lua/host.go:147-155` —
`L.DoString(string(code))` without `h.hostFuncs.Register(L, ...)`.
Per-delivery, `DeliverEvent` / `DeliverCommand` create a fresh state,
register hostFuncs, `DoString` the code (which executes top-level),
and call `on_event` / `on_command`.

For the capture mechanism to work, the Load pass needs hostfuncs
registered so a top-level `holomush.register_emit_type(...)` call
resolves to the capture-side hostfunc. Plan-reviewer round 1 caught
that the original "add a second pass" framing was wrong: the existing
syntax-check pass cannot tolerate the new hostfunc calls (would crash
with `attempt to index nil value (global 'holomush')`), and adding a
second pass alongside it would double Load-time execution for
affected plugins. The fix per spec §2.2: **branch** the existing
Load pass — non-crypto plugins continue to use the syntax-check
variant; crypto.emits-declaring plugins use a capture variant that
registers hostfuncs and accumulates registrations.

The scope question this ADR settles: should ALL Lua plugins switch to
the new capture variant (uniform model), or only plugins that opt in
by declaring non-empty `crypto.emits` (two-tier model)?

The answer determines whether top-level hostfunc visibility at Load
becomes a universal Lua plugin property or a scoped one.

## Decision

**Opt-in scope.** The Lua Load *capture* variant runs only when
`manifest.Crypto != nil && len(manifest.Crypto.Emits) > 0`
(INV-M1 in the mechanism spec). Plugins without `crypto.emits` fall
through to the existing syntax-check variant and see no behavior
change at Load — the hostfunc surface stays the same as today (none
during Load).

Implementation: the branching condition lives in both
`internal/plugin/lua/host.go::Load` (selects syntax-check vs capture
branch) and `internal/plugin/manager.go::loadPlugin` (skips the
validator entirely for non-crypto plugins). The two checks are
redundant by design — defense in depth — but the lua/host.go check
is the load-bearing one (governs the hostfunc-registration shape of
the Load pass).

Total Load-time execution count stays at **one per plugin** regardless
of branch — same as today. Only the hostfunc surface and the
post-pass state of the `luaPlugin.emitRegistry` field differ between
branches.

## Rationale

**Minimizes new substrate surface** per the mechanism spec's SHOULD
goal (reuse existing lifecycle hooks; don't extend them universally).
Plugins without `crypto.emits` see zero behavior change from this
mechanism. The vast majority of in-tree plugins fall into this case
(verified at spec time: only `core-communication`, `core-objects`,
and `core-scenes` declare `crypto:` at all; only the first two have
non-empty emits).

**Scopes the idempotency constraint to plugins that need it.** The
Lua capture branch registers hostfuncs at Load, so any top-level
hostfunc calls (e.g., `holomush.kv_set(...)`,
`holomush.create_location(...)`) fire at Load in addition to
per-delivery — same per-delivery behavior as today, plus one extra
execution at Load. For plugins on the syntax-check branch
(no crypto.emits), top-level hostfunc calls fail-fast at Load
(no `holomush` global) exactly as they do today. Scoping the capture
branch to crypto.emits-declaring plugins prevents the idempotency
constraint from becoming a universal Lua plugin invariant.

**INV-M1 is the branching/gating check at both sites.** The same
condition (`manifest.Crypto != nil && len(manifest.Crypto.Emits) > 0`)
appears in `lua/host.go::Load` (selects capture vs syntax-check
branch) and `manager.go::loadPlugin` (skips the validator entirely).
Making the boundary explicit and auditable matches the spec's
invariant-with-named-enforcement pattern.

**Two-tier model is self-documenting.** Presence of `crypto.emits` in
a Lua plugin manifest now signals to plugin authors that their
top-level code will run inside the capture branch at Load with
hostfuncs registered (in addition to per-delivery) and must be
idempotent. The boundary is visible in the manifest, not hidden in a
separate config.

## Alternatives Considered

**Option A: Opt-in scope (chosen)**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Zero observable change to non-crypto plugins at Load (same syntax-check semantics as today). Idempotency constraint scoped to plugins that opt in. Self-documenting via manifest presence. INV-M1 gate is explicit and auditable |
| Weaknesses | Two-branch Lua Load shape: plugins with vs without crypto.emits exercise different branches. Plugin authors adding crypto.emits post-hoc must audit top-level code for idempotency |

**Option B: Universal scope — all Lua plugins always run the capture branch**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Uniform model — all Lua plugins use the same Load branch regardless of crypto.emits presence; one code path |
| Weaknesses | Imposes universal idempotency constraint on ALL Lua plugin top-level code even when there's no validation to perform. Exposes the capture hostfunc surface to all plugins, polluting the namespace for the non-validating case. Existing plugins with top-level hostfunc calls (none today, but possible) would silently change behavior at Load |

## Consequences

**Positive:**

- No change to Load path for plugins without `crypto.emits` — most current and future plugins unaffected; same syntax-check semantics as today.
- Self-documenting: presence of `crypto.emits` signals to plugin authors that top-level code will run inside the capture branch at Load with hostfuncs registered.
- INV-M1 gate is explicit; reviewers can audit by grepping for the gate condition.
- The same gate applies to binary plugins (per `holomush-vie9`): plugins without `crypto.emits` don't need to implement `EmitTypeRegistrar` either. Cross-runtime consistency.

**Negative:**

- Two-branch Lua Load shape: plugins with `crypto.emits` exercise the capture branch (hostfuncs registered, registrations accumulated); plugins without exercise the syntax-check branch (no hostfuncs, throwaway state). Must be documented in `site/docs/extending/` so plugin authors understand the implication of adding `crypto.emits` to an existing plugin.
- Post-hoc `crypto.emits` addition is a non-obvious migration: existing top-level code must be audited for idempotency before adding the manifest declarations (top-level hostfunc calls that crashed at Load yesterday will start succeeding).

**Neutral:**

- Defense in depth: the branching check exists in both `lua/host.go::Load` and `manager.go::loadPlugin`. Either check alone would suffice; both together prevent accidental regression.
- The Lua capture branch's hostfunc surface is narrower than the per-delivery surface (only `holomush.register_emit_type` is added on top of the standard `Register`).
- Total Load-time execution count stays at **one per plugin** regardless of branch — no perf regression vs today's syntax-check-only path.

## References

- [INV-S5 Mechanism Design Spec — §2.2, INV-M1, INV-M4](../superpowers/specs/2026-05-17-inv-s5-mechanism-design.md)
- [Sibling ADR: Init-RPC Emit-Type Communication (`holomush-vie9`)](holomush-vie9-init-rpc-emit-type-communication.md) — the parent mechanism this ADR scopes.
- [Parent ADR: Startup-Time Set-Equality Validation (`holomush-3vsb`)](holomush-3vsb-manifest-emit-type-startup-validation.md) — the original INV-S5 decision.
- `internal/plugin/lua/host.go:111` — Load method gaining the branched syntax-check vs capture pass.
- `internal/plugin/manager.go:849` — `loadPlugin` gaining the validator call.

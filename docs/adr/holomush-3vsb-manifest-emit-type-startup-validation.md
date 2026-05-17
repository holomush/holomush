<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Startup-Time Set-Equality Validation of crypto.emits Declarations

**Date:** 2026-05-16
**Status:** Accepted
**Decision:** holomush-3vsb
**Deciders:** HoloMUSH Contributors

## Context

Plugins declare the sensitivity classification of each event type they
emit in their manifest's `crypto.emits` block (e.g.,
[`plugins/core-communication/plugin.yaml:272-297`](../../plugins/core-communication/plugin.yaml)
classifies 8 event types). The substrate enforces these declarations at
emit time via [`internal/plugin/event_emitter.go::Emit`](../../internal/plugin/event_emitter.go),
which dispatches to the truth table in
[`internal/plugin/sensitivity_fence.go:23-48`](../../internal/plugin/sensitivity_fence.go).

The runtime gate's truth table:

| manifest | claimed Sensitive | result |
| -------- | ----------------- | ------ |
| `never`  | false             | accepted, plaintext |
| `never`  | true              | **REJECT** (INV-6, `EVENT_SENSITIVITY_NOT_DECLARED`) |
| `may`    | false             | accepted, plaintext |
| `may`    | true              | accepted, encrypted |
| `always` | false             | **REJECT** (INV-7, `EVENT_SENSITIVITY_REQUIRED`) |
| `always` | true              | accepted, encrypted |

Two failure modes are silent under this gate:

1. **Declared-but-unregistered:** A `crypto.emits` entry the plugin's
   code never emits. Dead declaration / typo in event type name. No
   runtime symptom; the entry exists in the manifest but never matches
   any emit.

2. **Registered-but-undeclared (with `Sensitive=false`):** Plugin code
   emits an event type the manifest never declared. `LookupEmitSensitivity`
   falls through to `SensitivityNever`. If the emit uses `Sensitive=false`,
   it is silently accepted as plaintext — without any indication that
   the manifest is incomplete.

For sensitive data, the second mode is dangerous: an event type the
plugin author *intended* to declare as `always` or `may` (sensitive)
might be emitted plaintext by mistake, and the runtime never warns.

Scenes Phase 4 will add 4-6 new entries to `core-scenes`'s `crypto.emits`
(`scene_ic`, `scene_ooc`, `scene_join`, `scene_leave`, etc.). The
typo-and-misclassification surface area increases substantially.

## Decision

Substrate adds a startup-time validator that requires set-equality
between the manifest-declared emit-type set and the code-registered
emit-type set, in **both directions**. Mismatch fails plugin startup.

New SDK API:

- `pkg/plugin/RegisterEmitType(eventType string)` — plugin's init code
  registers each event type it can emit.
- `pkg/plugin/RegisterEmitTypes(eventTypes []string)` — batch variant.
- Lua hostfunc parity: `holomush.register_emit_type(name)`.

Before plugin readiness is signaled, substrate compares manifest's
declared set against the plugin's registered set. Any difference fails
the plugin load with a clear error naming the missing or extra event
types.

Rollout (4 beads):

1. Substrate capability lands with no-op default for unregistered plugins
2. Plugin adoption bead: `core-communication` adopts `RegisterEmitTypes`
3. Plugin adoption bead: `core-scenes` adopts `RegisterEmitTypes` (empty set initially; populates with Phase 4 entries)
4. Substrate flip: validation becomes fail-closed (unregistered = error)

## Rationale

**Both silent failure modes are dangerous.** Declared-but-unregistered
implies the manifest has a typo or stale entry; the runtime gate never
notices. Registered-but-undeclared-with-`Sensitive=false` implies
sensitive data is being emitted plaintext; the runtime never warns.

**Startup is the earliest catch point.** A static lint check (CI only)
could catch some cases, but dynamic emit registrations (e.g., via
`init()`) are invisible to static analysis. Startup-time check sees
the runtime state of the plugin, after all `init()` has run.

**Phased rollout prevents breakage.** Flipping fail-closed immediately
would break existing plugins (`core-communication`, `core-scenes`)
before they have a chance to adopt the registration API. The
capability-first / adoption-second / flip-last pattern is well-established
for substrate-wide additions.

**This is substrate work, not SDK work.** The validator applies to
every plugin declaring `crypto.emits`, not just stateful-group plugins.
It belongs in `pkg/plugin/` (consumed by all plugins) and
`internal/plugin/manifest.go` (substrate-side compare), not in
`pkg/plugin/eventkit/cryptoemit/`. (`eventkit/cryptoemit` is a
*complementary* call-site assertion layer; the two layers catch
different failure modes — startup-set-equality catches manifest/code
drift; cryptoemit catches per-emit classification drift.)

## Alternatives Considered

**Option A: Keep runtime-only gate (current baseline)**

| Aspect     | Assessment                                                                                                                                                                       |
| ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | No new substrate code; simpler plugin startup                                                                                                                                    |
| Weaknesses | Dead declarations and undeclared-plaintext emits silently accepted; classification drift only caught on visible symptom; risk grows with Phase 4's 4-6 new crypto.emits entries |

**Option B: Startup set-equality check (chosen)**

| Aspect     | Assessment                                                                                                                                                                                              |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Catches both failure modes at load time; makes `crypto.emits` authoritative; eliminates silent drift; ~320 LOC + per-plugin 5-10 LOC; precedent established for future plugins                          |
| Weaknesses | Requires plugins to register emit types explicitly (new API surface); rollout needs adoption beads before flipping fail-closed; one-time migration cost                                                |

**Option C: Lint-time static check (CI only, not startup)**

| Aspect     | Assessment                                                                                                                                                |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | No runtime cost; catches mismatches before deployment                                                                                                     |
| Weaknesses | Only catches statically-analyzable mismatches; dynamic emit registrations invisible to static checker; false sense of security; CI lint can be skipped |

## Consequences

**Positive:**

- Dead declarations and undeclared-plaintext emits caught at plugin load time
- `crypto.emits` manifest section becomes authoritative and machine-verified
- Phase 4's new entries (`scene_ic`, `scene_ooc`, etc.) are startup-validated from day one
- Future plugins inherit validation automatically
- Complements `eventkit/cryptoemit` (SDK call-site assertion layer)

**Negative:**

- Existing plugins (`core-communication`, `core-scenes`) must adopt `RegisterEmitTypes` before fail-closed flip
- Plugin authors must keep manifest and code registration in sync (new discipline)
- 4-bead rollout (capability → adoption × N → flip) instead of single landing

**Neutral:**

- The validator is substrate-side; `eventkit/cryptoemit` is plugin-side SDK; two complementary layers
- No-op default during rollout means existing plugins work unchanged until they explicitly adopt
- Adoption beads under `holomush-jg9b` (parent design bead) chain

## References

- [Substrate Contract Spec — §1.2, INV-S5](../superpowers/specs/2026-05-16-social-spaces-substrate-contract.md)
- [Master Crypto Design](../superpowers/specs/2026-04-25-event-payload-crypto-design.md)
- [`.claude/rules/event-conventions.md`](../../.claude/rules/event-conventions.md)
- `internal/plugin/crypto_manifest.go:14-21` (sensitivity enum)
- `internal/plugin/sensitivity_fence.go:23-48` (runtime truth table)

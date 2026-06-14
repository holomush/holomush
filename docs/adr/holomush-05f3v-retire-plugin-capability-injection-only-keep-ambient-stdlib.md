<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-05f3v; do not edit manually; use `/adr update holomush-05f3v` -->

# Retire plugin capability injection only; keep ambient stdlib ungated

**Date:** 2026-06-14
**Status:** Accepted
**Decision:** holomush-05f3v
**Deciders:** Sean Brandt

## Context

At cutover, `hostfunc.Register` (`internal/plugin/hostfunc/functions.go:271`) must be partially dismantled. The question was which host functions belong behind the declaration gate (capabilities) and which remain unconditionally injected. `holomush-cryy2` established that logging and core stdlib are ambient substrate, but did not enumerate the full retirement boundary — specifically `register_emit_type` and the handler return-value emit path, which could be confused with the `emit` host capability (the brokered `EmitService` RPC).

## Decision

Only the 10 capability host functions (`kv`, `world.query`, `world.mutation`, `property`, `session`, `session.admin`, `focus`, `eval`, `emit`, `settings`) are stripped from `hostfunc.Register`. `holomush.log`, `holomush.new_request_id`, `holo.fmt`/`RegisterStdlib`, `register_emit_type`, and the handler return-value emit path remain unconditional and ungated.

## Rationale

- The ambient/capability boundary test (`cryy2`): a function is ambient if it does NOT cross the host trust boundary to touch something the plugin does not already own.
- `register_emit_type` is a startup-time registration tied to INV-PLUGIN-32; gating it behind a capability declaration would prevent plugins from declaring their own event types.
- The return-value emit path (`event_emitter.go::Emit`) is distinct from the brokered `EmitService` RPC; conflating them would break all event emission for plugins that have not declared the `emit` capability.

## Alternatives Considered

- **Capabilities-only retirement; keep ambient stdlib (chosen):** Preserves full plugin observability; `register_emit_type` stays unconditional (INV-PLUGIN-32); the return-value emit path stays separate from the `emit` capability. Cost: a maintained boundary heuristic for future host functions.
- **Gate everything through the broker, including stdlib (rejected):** Uniform model, but gating logging incentivizes suppressing observability; `register_emit_type` is startup registration, not a runtime capability call; the return-value path is the plugin's own emission, not a host authority grant.

## Consequences

- Positive: all plugins remain fully observable with no manifest change; INV-PLUGIN-32 cannot be accidentally broken by the cutover; the vocabulary stays at 10 genuine authority grants.
- Negative: the ambient/capability boundary must be documented for future host-function additions; the two emit surfaces must be kept explicitly distinct in code and tests.
- Neutral: extends `holomush-cryy2`'s principle to the full retirement enumeration without superseding it.

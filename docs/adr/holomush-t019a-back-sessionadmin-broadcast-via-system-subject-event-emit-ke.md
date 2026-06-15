<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-t019a; do not edit manually; use `/adr update holomush-t019a` -->

# Back SessionAdmin broadcast via the system-subject event emit; keep disconnect unimplemented

**Date:** 2026-06-14
**Status:** Accepted
**Decision:** holomush-t019a
**Deciders:** Sean Brandt

## Context

The plugin-capability atomic cutover (sub-spec 5, epic `holomush-eykuh.4`) requires, per spec R5, that the host-brokered `SessionAdminService` (broadcast/disconnect) have a real backing before the atomic flip (`eykuh.4.9`), because the gated path fails closed (`codes.Unimplemented`) without one. Bead `eykuh.4.2` was blocked: no production implementor of `hostcap.SessionAdmin` (`BroadcastSystemMessage(ctx,message)` + `DisconnectSession(ctx,sessionID,reason)`) was reachable from the plugin-subsystem construction site.

Grounding established the precise state:

- `wall` (core-communication, the only broadcast caller) is **already non-functional in production**. It calls `session.list_active()` / `session.broadcast()` on a top-level `session` Lua global that production never registers: `SessionCapability` (`internal/plugin/hostfunc/cap_session.go`) is the only registrant and its constructor `NewSessionCapability` is **test-only**; the production `CapabilityRegistry` (`internal/plugin/setup/subsystem.go:177-178`) wires only `AuditService`; the stdlib `holo.session` namespace exposes only `find_by_name`/`set_last_whispered`. No test exercises `wall`. Consequently the flip regresses nothing.
- `session.disconnect` has **zero callers** anywhere, and there is no core-side forcible-disconnect mechanism. Forcible connection teardown is structurally a gateway/connection-layer concern (`.claude/rules/gateway-boundary.md`).
- A real broadcast sink **does** exist: `core.EventAppender.Append` emitting a `system`-actor `core.EventTypeSystem` event to the reserved `"system"` subject — exactly `command.Services.BroadcastSystemMessage(ctx,"system",msg)` (`internal/command/types.go:619`), used by the `shutdown` command (`internal/command/handlers/shutdown.go:42`). The appender (`busn`) is built top-level in `cmd/holomush/sub_grpc.go` and shared to the command layer and plugin manager, but is **not** threaded into `PluginSubsystemConfig`. The gap is a missing dependency *edge*, not a missing *mechanism*.

## Decision

1. **Broadcast — wire it via the system-subject event emit.** Back `SessionAdmin.BroadcastSystemMessage(ctx,message)` with a thin adapter over a `core.EventAppender`, threaded as a new dependency edge into `PluginSubsystemConfig`. The backing emits a `system`-actor `core.EventTypeSystem` event (built with `core.NewEvent`) to the reserved `"system"` subject — identical to `command.Services.BroadcastSystemMessage` / `shutdown`. The **host** stamps the system actor, so the plugin does not need `system` in `actor_kinds_claimable`; the privileged emit is gated by the `session.admin` capability declaration. The single backing is provided to the shared `sessionAdminServer`, so the Lua and binary runtimes reach it identically (plugin-runtime-symmetry — the gate stays at the common path). Implemented in bead `holomush-eykuh.4.2`. This makes `wall` functional for the first time once `eykuh.4.9` reconciles the Lua surface names.

2. **Disconnect — keep it fail-closed `Unimplemented` plus a follow-up bead.** `SessionAdmin.DisconnectSession(ctx,sessionID,reason)` retains the existing nil-guard (`Unimplemented`). No production forcible-disconnect mechanism exists and it has zero callers; building one now is unjustified. A follow-up bead tracks a future gateway-observed eviction mechanism. The `session.admin` capability and the `SessionAdminService.Disconnect` RPC are retained (no proto churn).

## Rationale

- The only real broadcast sink in the codebase is the event-appender emit to the `"system"` subject; reusing it keeps broadcast event-sourced and consistent with `shutdown`, rather than reinventing a session-store fan-out that would couple the plugin subsystem to session delivery and duplicate the bus.
- Routing through `core.EventAppender` means the dependency is an *existing* top-level object (`busn`), so the "new plumbing" is a single threaded edge, not a new mechanism.
- A host-stamped system actor respects the `actor_kinds_claimable` gate: a plugin cannot claim `system`, so the privileged broadcast must be performed by the host on the plugin's behalf — exactly what a host-brokered capability server is for.
- Disconnect has neither a sink nor a caller; per `gateway-boundary.md`, forcible connection teardown belongs to the gateway, not core. Keeping it `Unimplemented` is honest and fail-closed; a follow-up bead tracks the real mechanism without blocking the cutover.
- Because `wall` is already unbacked, none of this is *required* to make the `4.9` flip safe — but wiring broadcast properly is small and converts dead scaffolding into a working capability, so it is worth doing in-epic rather than deferring.

## Alternatives Considered

- **Broadcast via session-store fan-out (rejected):** iterate `ListActive` and push per session. Wrong shape for an event-sourced system, couples the plugin subsystem to session delivery, and reimplements the bus.
- **Defer broadcast; keep Unimplemented (rejected):** legitimate since the flip is safe regardless, but it leaves `wall` broken and the capability vestigial when the fix is a single dependency edge.
- **Retire the `session.admin` broadcast capability entirely (rejected):** would push `wall` onto a host-command path and remove the plugin-facing broadcast surface; more proto/vocab churn than wiring the existing sink, and forecloses plugin-authored privileged broadcasts.
- **Build a disconnect mechanism now (rejected):** an eviction-flag / disconnect-event observed by the gateway is real future work but out of `eykuh.4`'s scope, with zero current callers to justify it.
- **Remove `DisconnectSession` from the interface + vocab (rejected):** YAGNI-pure, but touches the proto `SessionAdminService` and the `session.admin` vocab for no functional gain; the fail-closed stub + follow-up bead is lower churn.

## Consequences

- **Positive:** `wall` becomes functional (after `4.9` surface reconciliation); `SessionAdminService` gains a real, symmetric, event-sourced broadcast backing; the R5 precondition for the flip is genuinely satisfied (not stubbed); disconnect's gap is tracked rather than faked.
- **Negative:** `PluginSubsystemConfig` gains a new dependency edge (a `core.EventAppender`, or a narrow `SystemBroadcaster` over it); the broadcast subject string `"system"` becomes a shared constant the plugin path and command path must agree on.
- **Neutral:** `session.admin` continues to bundle broadcast + disconnect at current vocab granularity; a `session.admin`-declaring plugin gets a working broadcast and a fail-closed disconnect until the follow-up lands. For `wall` to actually work end-to-end, `4.9` must also (a) add `session.admin` to core-communication's manifest (R7) and (b) reconcile the Lua call sites (`session.list_active`/`session.broadcast`, snake_case) against the brokered surface (`ListActive`/`Broadcast`, PascalCase) — broadcast wiring alone does not make `wall` green.

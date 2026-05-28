---
title: "Substrate Contract"
---

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

Every plugin in HoloMUSH is a consumer of the substrate. This page is the
reference inventory of the surfaces plugins can rely on. For the rules that
govern the plugin–substrate relationship and why they exist, see
[Substrate contract invariants](/extending/explanation/substrate-invariants/);
for the procedure to declare and register emit types, see
[Register plugin emit types](/extending/how-to/register-emit-types/).

Canonical detail lives in the design spec:
[Substrate Contract Design Spec](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md).

## Substrate primitives plugin authors can rely on

These surfaces are stable for the lifetime of the spec. You do not need to
implement them; the substrate provides them turn-key.

| Primitive                              | What it does                                                                                      | Where it lives                                                               |
| -------------------------------------- | ------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| JetStream event bus                    | Durable per-stream delivery, replay, history fallback, ULID identity, JetStream-seq ordering      | `internal/eventbus/` — `Publisher`, `Subscriber`, `HistoryReader` interfaces |
| Crypto envelope                        | Per-event-type sensitivity enforcement, KEK/DEK rotation, AuthGuard fence, INV-50 downgrade fence | `internal/plugin/event_emitter.go`, `internal/plugin/sensitivity_fence.go`   |
| Manifest emit-type validation (INV-S5) | Startup-time fail-fast when manifest declared set does not equal code-registered set              | `internal/plugin/manager.go::loadPlugin`                                     |
| ABAC engine                            | Policy evaluation, attribute resolution wiring, default-deny posture                              | `internal/access/` — `access.Engine` interface                              |
| Focus coordinator                      | Per-connection subscription state, multi-tab visibility, restore-on-reconnect                     | `internal/grpc/focus/`, `pkg/plugin/focus_client.go`                         |
| Plugin host RPCs (Go + Lua parity)     | `Emit`, `QueryHistory`, `JoinFocus`/`LeaveFocus`/`PresentFocus`, `PluginAuditService.AuditEvent`  | `pkg/plugin/` (Go SDK), `internal/plugin/hostfunc/` (Lua hostfuncs)          |
| Per-plugin storage                     | Isolated Postgres schema + role, embedded migration runner, `search_path` scoping                 | `internal/store/`, plugin's `migrations/`                                    |
| Audit projection                       | Host ack-and-skip for plugin-owned subjects; dispatch to plugin's `AuditEvent` RPC                | `internal/eventbus/audit/`                                                   |

For the full substrate inventory with `path:line` citations, see
[§1 of the substrate-contract spec](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md#1-substrate-inventory).

## eventkit and groupkit SDKs (named, not yet built)

`pkg/plugin/eventkit/` and `pkg/plugin/groupkit/` are co-designed in the
substrate-contract spec but their code lands only after N=2 validation (see
[Substrate contract invariants](/extending/explanation/substrate-invariants/#sdk-extraction-policy-inv-s7)).
Today, plugins implement event-replay, group-membership, and focus-wire
patterns inline (scenes-bespoke, channels-bespoke).

| SDK        | Location               | Primitives (planned)                                                                     | Who can use it                                                |
| ---------- | ---------------------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| `eventkit` | `pkg/plugin/eventkit/` | `replay` (ABAC-filtered history fan-out), `cryptoemit` (call-site sensitivity assertion) | Any plugin emitting ABAC-gated sensitive events              |
| `groupkit` | `pkg/plugin/groupkit/` | `membership`, `focuswire`, `groupabac`                                                   | Uses with explicit member-of-entity state (scenes, channels) |

## References

**Specs:**

- [Substrate Contract Design Spec](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md) — canonical detail for §1 (substrate inventory), §2 (boundary invariants), §3 (SDK design), §4 (per-use validation)
- [INV-S5 Mechanism Design Spec](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-05-17-inv-s5-mechanism-design.md) — runtime mechanism for binary and Lua emit-type registration and host-side validation

**ADRs (theme:social-spaces):**

- [holomush-p7w0 — Split Plugin SDK into eventkit and groupkit by Scope](https://github.com/holomush/holomush/blob/main/docs/adr/holomush-p7w0-split-plugin-sdk-eventkit-groupkit.md)
- [holomush-lrt3 — Require N=2 Consumer Validation Before SDK Primitive Extraction](https://github.com/holomush/holomush/blob/main/docs/adr/holomush-lrt3-n2-consumer-validation-sdk-extraction.md)
- [holomush-z1e7 — Strict Plugin-Boundary: Plugins Must Not Modify internal/](https://github.com/holomush/holomush/blob/main/docs/adr/holomush-z1e7-strict-plugin-boundary.md)
- [holomush-3vsb — Startup-Time Set-Equality Validation of crypto.emits Declarations](https://github.com/holomush/holomush/blob/main/docs/adr/holomush-3vsb-manifest-emit-type-startup-validation.md)
- [holomush-c8a9 — Enforce Scene Privacy at Plugin Code, Not ABAC Engine](https://github.com/holomush/holomush/blob/main/docs/adr/holomush-c8a9-scene-privacy-plugin-code-enforcement.md)
- [holomush-vie9 — Use Init-RPC Protocol Extension to Communicate Code-Registered Emit Types](https://github.com/holomush/holomush/blob/main/docs/adr/holomush-vie9-init-rpc-emit-type-communication.md)
- [holomush-7h0c — Scope Lua Load Capture Pass to crypto.emits-Declaring Plugins Only](https://github.com/holomush/holomush/blob/main/docs/adr/holomush-7h0c-lua-load-pass-optin-scope.md)

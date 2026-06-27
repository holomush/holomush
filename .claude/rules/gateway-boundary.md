<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

---
paths:
  - "cmd/holomush/**"
  - "internal/web/**"
  - "internal/grpc/**"
  - "internal/telnet/**"
---

# Gateway Boundary Invariant

The gateway (`cmd/holomush/gateway.go`, `internal/web/`) is a **protocol translation layer only**. It MUST NOT access internal services directly.

| Allowed                                  | Prohibited                                    |
|------------------------------------------|-----------------------------------------------|
| gRPC calls to core server                | Direct access to `WorldService`               |
| Connection management (register/remove)  | Direct access to `SessionStore` for queries   |
| Protocol translation (ConnectRPC ↔ gRPC) | Direct access to repositories or the database |
| Static file serving                      | Business logic or data aggregation            |

All game state queries (location state, presence, characters) MUST flow through core server RPCs. The gateway proxies; it does not compute.

## Why

The gateway runs as a separate process (potentially scaled horizontally) and must not couple itself to internal data shapes. All business logic lives in the core server, exposed via gRPC. Gateway logic that "just queries the DB directly" creates coupling that breaks the multi-process deployment model.

## How to apply

When adding a new gateway endpoint:

1. Identify the gRPC service that owns the data
2. If no RPC exists for what you need, add the RPC to the core server first
3. Have the gateway call the RPC; do **not** add a DB query, repo lookup, or service struct field to the gateway
4. The gateway holds gRPC clients, not service instances

## Structural writes use typed RPCs, not the command path

Web/GUI **structural writes** (create / set / end / invite / kick / transfer —
anything driven by a button or form) MUST go through a **typed RPC on the BFF
facade**, never through `sendCommand` / `HandleCommand`. The command path
(`HandleCommand`) is reserved for **human/CLI conversational verbs** typed into a
terminal — `pose`, `say`, `ooc`, `join`.

Reaching for `sendCommand` to perform a structural mutation from the GUI is the
anti-pattern: it routes a machine-initiated action through the human
text-command parser. If the facade has no typed RPC for the operation, **add the
RPC** (per "How to apply" above) rather than string-building a command.

| Caller                        | Surface                        | Example                       |
| ----------------------------- | ------------------------------ | ----------------------------- |
| GUI button / form (machine)   | typed facade RPC               | `EndScene`, `InviteToScene`   |
| Human / CLI (conversational)  | `HandleCommand` / `sendCommand` | `pose`, `say`, `join`        |

Grounded in ADR `holomush-v4qmu`
(`docs/adr/holomush-v4qmu-typed-rpcs-structural-scene-writes-command-path-human-cli-ve.md`).

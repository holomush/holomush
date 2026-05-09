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

---
title: "Gateway Boundary"
---

The gateway process (`cmd/holomush gateway`) is a **protocol translation and
connection management layer**. It MUST NOT hold domain state, perform
domain logic, or import domain packages.

## What the gateway does

- Accepts incoming connections (telnet, web)
- Translates protocol formats: telnet/ConnectRPC ↔ core gRPC
- Manages connection lifecycle (idle timeouts, TLS handshake, register/deregister)
- Serves the embedded web bundle (static assets)
- Reads `RenderingMetadata` off `EventFrame` and shapes it for the wire
  format the client expects

## What the gateway MUST NOT do

- Maintain a `VerbRegistry`, `EventStore`, or any domain-aware cache
- Translate event payloads using business rules (e.g. "if X then label Y")
- Make ABAC decisions
- Access PostgreSQL directly
- Embed plugin loader code

## Forbidden imports

The CI tripwire test (`cmd/holomush/gateway_imports_test.go`, `INV-GW-1`)
enforces that gateway-side packages MUST NOT import:

- `internal/world`
- `internal/access`
- `internal/store`
- `internal/plugin`
- `internal/eventbus`
- `internal/auth/service`
- `internal/command`

The tripwire covers `internal/web/...`, `internal/telnet/...`, and
gateway files in `cmd/holomush/` (anything not listed in the
`coreOnlyFiles` allowlist).

## Adding a new file to `cmd/holomush/`

1. Decide whether the file is core-only or gateway-side.
2. If core-only and it imports any forbidden package, add it to
   `coreOnlyFiles` in `cmd/holomush/gateway_imports_test.go` with a
   one-line comment explaining why.
3. If gateway-side, ensure no forbidden imports.
4. Run `task test -- ./cmd/holomush/` to verify.

## See also

- Spec: `docs/superpowers/specs/2026-04-26-gateway-verb-registry-sourcing.md`
- Architecture: `site/docs/contributing/architecture.md`
- Event-emit pipeline: `site/docs/contributing/event-emit-pipeline.md`

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Web Client Adapter & SvelteKit Scaffold Design

**Status:** Draft
**Date:** 2026-03-18
**Epic:** holomush-qve (Epic 8: Web Client)
**Task:** holomush-qve.1 (sub-spec 1 of 3)
**Scope:** WebSocket adapter (ConnectRPC), SvelteKit project scaffold, E2E proof

## Overview

This is sub-spec 1 of 3 for Epic 8 (Web Client). It covers the server-side
ConnectRPC adapter in the gateway process and a minimal SvelteKit scaffold that
proves the full browser-to-core pipeline works. Sub-specs 2 (terminal + chat UX)
and 3 (portal pages) build on this foundation.

## Goals

- MUST add a ConnectRPC HTTP handler to the gateway process
- MUST define a web-facing protobuf service (`WebService`) for browser clients
- MUST scaffold a SvelteKit project with generated TypeScript ConnectRPC clients
- MUST embed the SvelteKit build in the gateway binary via `go:embed`
- MUST support a `--web-dir` flag to override embedded static files
- MUST prove end-to-end: browser → ConnectRPC → gateway → gRPC → core
- MUST include a Playwright E2E test

## Non-Goals

- Terminal UI or chat UI (sub-spec 2)
- Portal pages — wiki, characters, admin (sub-spec 3)
- Registered account authentication (future work)
- Offline/PWA support (sub-spec 2)
- Hot module replacement for Go ↔ SvelteKit (use separate dev servers)

## Design Decisions

### CharacterRef in Core Package

The `core` package MUST define a `CharacterRef` struct that groups character
identity fields (ID, Name, LocationID) used by engine methods.

```go
// core/character.go
type CharacterRef struct {
    ID         ulid.ULID
    Name       string
    LocationID ulid.ULID
}
```

Engine methods (`HandleSay`, `HandlePose`, `HandleConnect`, `HandleDisconnect`)
MUST accept `CharacterRef` instead of loose `charID`, `locationID`, `charName`
parameters.

**Rationale:** The engine currently passes character data as loose primitives
(`charID ulid.ULID`, `charName string`, `locationID ulid.ULID`), duplicating
fields that exist in `world.Character`. A direct import of `world.Character`
into `core` is impossible because `world` already imports `core` — creating a
circular dependency. `CharacterRef` is a lightweight value type that lives in
`core`, eliminates primitive obsession in engine signatures, and provides a
single extension point for future character fields in event payloads (e.g.,
description for hover tooltips). `world.Character` can provide a `Ref()` method
to convert to `CharacterRef` at the boundary.

All event payloads that reference a character (say, pose, arrive, leave) MUST
include `character_name` sourced from `CharacterRef.Name`, ensuring protocol
adapters (telnet, web) can display human-readable names without resolving actor
IDs.

### ConnectRPC Instead of Raw WebSocket

The project MUST use [ConnectRPC](https://connectrpc.com) instead of a custom
WebSocket protocol.

**Rationale:** The project already has protobuf service definitions and a gRPC
core server. ConnectRPC exposes protobuf services to browsers over HTTP using the
Connect protocol. This eliminates an entire layer of hand-rolled wire protocol
code. The browser gets type-safe generated TypeScript clients for free. Server
streaming works in browsers (for event feeds). Commands are unary RPCs.

### Web-Facing Proto Separate from Core Proto

The gateway MUST define its own `WebService` proto rather than exposing the
internal `CoreService` directly.

**Rationale:** The core proto is an internal interface designed for gateway↔core
communication. The web proto is a public API designed for browsers — different
field names, simpler types, no internal details leaked. Versioning can evolve
independently.

### Gateway Process Hosts ConnectRPC

The ConnectRPC handler MUST run inside the existing gateway process, not as a
separate process.

**Rationale:** The gateway's purpose is protocol adaptation. Adding ConnectRPC
alongside telnet is natural, reuses the existing gRPC client to core, and keeps
deployment simple.

### pnpm for Package Management

The SvelteKit project MUST use pnpm as its package manager.

**Rationale:** pnpm is fast, has strict dependency resolution, and is fully
compatible with the SvelteKit/Vite/Playwright toolchain. Bun was considered but
has open issues with Vite dev server crashes and Playwright browser launches.

### Embedded Static Files with Override

The gateway MUST embed the SvelteKit build via `go:embed` and MUST support a
`--web-dir` flag for filesystem override.

**Rationale:** Embedding provides a single-binary, zero-config deployment. The
override flag lets operators deploy custom builds or use external static serving
without rebuilding the binary.

### Monorepo Layout

The SvelteKit project MUST live at `web/` in the same repository.

**Rationale:** Single team, shared proto definitions, unified `task` workflows.
The `web/` directory has its own `package.json` and is self-contained.

## Architecture

### System Diagram

```text
Browser (SvelteKit PWA)
  │
  ├─ Unary RPCs (auth, commands) ──→ Gateway HTTP server (:8080)
  └─ Server stream (events)      ──→   ├─ ConnectRPC handler
                                        │     └──→ Core (gRPC)
                                        └─ Static file server
                                              └──→ embedded FS or --web-dir
```

### Gateway Changes

The gateway process gains:

- **HTTP server** on a new port (default `:8080`)
- **ConnectRPC handler** implementing `WebService` (translates to core gRPC calls)
- **Static file server** with embedded FS + filesystem override
- **CORS middleware** for cross-origin dev server requests
- New config: `gateway.web_addr` (`--web-addr`), `gateway.web_dir` (`--web-dir`)

The HTTP mux routes using Go's `http.ServeMux`:

- `/holomush.web.v1.WebService/*` → ConnectRPC handler (default Connect paths)
- `/*` fallback → static file server (SPA-aware, serves `index.html` for
  unknown paths)

ConnectRPC's Go library returns an `http.Handler` via `connect.NewHandler()` that
registers at the service path prefix. This composes cleanly with `http.ServeMux`
alongside the static file handler.

**CORS:** The gateway MUST allow configurable CORS origins via
`gateway.cors_origins` (default: same-origin only). In development, operators set
`cors_origins: ["http://localhost:5173"]` for the Vite dev server. The CORS
middleware MUST allow Connect protocol headers (`Connect-Protocol-Version`,
`Content-Type`).

**Graceful shutdown:** The HTTP server MUST follow the same shutdown pattern as
the existing telnet and control servers — `http.Server.Shutdown(ctx)` with a
5-second timeout context, ordered after telnet listener close.

**Readiness:** The gateway's readiness check MUST include the web HTTP listener.
The observability endpoint SHOULD NOT report ready until all listeners (telnet,
control, web) are bound.

### Web-Facing Protobuf Service

```protobuf
// api/proto/holomush/web/v1/web.proto
syntax = "proto3";
package holomush.web.v1;

option go_package = "github.com/holomush/holomush/pkg/proto/holomush/web/v1;webv1";

service WebService {
  // Authenticate as guest or registered user.
  rpc Login(LoginRequest) returns (LoginResponse);

  // Send a game command (say, pose, quit, etc.)
  rpc SendCommand(SendCommandRequest) returns (SendCommandResponse);

  // Server-streaming event feed. Client receives game events
  // (say, pose, arrive, leave) as they occur.
  rpc StreamEvents(StreamEventsRequest) returns (stream GameEvent);

  // Disconnect ends the session and triggers cleanup (leave events, guest release).
  rpc Disconnect(DisconnectRequest) returns (DisconnectResponse);
}

message LoginRequest {
  string username = 1;
  string password = 2;
}

message LoginResponse {
  bool success = 1;
  string session_id = 2;
  string character_name = 3;
  string error_message = 4;
}

message SendCommandRequest {
  string session_id = 1;
  string text = 2;
}

message SendCommandResponse {
  bool success = 1;
  string output = 2;
  string error_message = 3;
}

message StreamEventsRequest {
  string session_id = 1;
}

message GameEvent {
  string type = 1;
  string character_name = 2;
  string text = 3;
  int64 timestamp = 4;
}

message DisconnectRequest {
  string session_id = 1;
}

message DisconnectResponse {}
```

### ConnectRPC Handler

The handler lives in `internal/web/handler.go` and implements `WebService` by
calling the existing gRPC client to core:

- `Login` → calls `core.Authenticate` RPC, returns session ID
- `SendCommand` → calls `core.HandleCommand` RPC, returns command output
- `StreamEvents` → calls `core.Subscribe` RPC, forwards events as `GameEvent`
- `Disconnect` → calls `core.Disconnect` RPC, triggers leave events and guest
  cleanup. The browser SHOULD call this on page unload (`beforeunload` event).
  As a safety net, the server MUST also detect `StreamEvents` stream closure and
  trigger disconnect if `Disconnect` was not called within 5 seconds.

The handler holds a reference to the same `grpc.Client` the telnet gateway uses.

**Session ID handling:** Auth session IDs are passed via the `session_id` field
in request messages for this sub-spec. This is a known trade-off — it means
every RPC method must validate the session manually rather than using ConnectRPC
interceptors. A future iteration SHOULD migrate to an `Authorization` header
with a ConnectRPC interceptor for centralized auth, matching how the core gRPC
server handles session validation.

**Event payload translation:** The core `Event` message contains a `payload`
field (JSON-encoded bytes). The handler translates this to the flat `GameEvent`
message for each event type:

| Core event type | GameEvent.character_name | GameEvent.text |
| --------------- | ------------------------ | -------------- |
| `say` | from payload `.character_name` | from payload `.text` |
| `pose` | from payload `.character_name` | from payload `.text` |
| `arrive` | from payload `.character_name` | `"has arrived."` |
| `leave` | from payload `.character_name` | `"has left."` |

Unknown event types are silently dropped (forward compatibility).

### Static File Serving

```go
// internal/web/static.go

//go:embed all:dist
var embeddedFS embed.FS

func FileServer(webDir string) http.Handler
```

- If `webDir` is non-empty, serves from the filesystem directory
- Otherwise, serves from the embedded `dist/` directory
- SPA fallback: unknown paths serve `index.html` (SvelteKit client-side routing)

**Bootstrapping:** `go:embed` requires the `dist/` directory to exist at compile
time. A committed placeholder `internal/web/dist/index.html` MUST contain a
minimal page saying "Run `task web:build` to build the web client." This lets
`go build` succeed on a fresh clone without running the frontend build first.
The real SvelteKit build replaces this placeholder.

### SvelteKit Project

```text
web/
  src/
    lib/
      connect/          ← generated TypeScript clients (from buf)
      transport.ts      ← ConnectRPC transport config
    routes/
      +page.svelte      ← minimal page: login + command + event display
  static/
  buf.gen.yaml          ← buf config for TypeScript code generation
  package.json
  pnpm-lock.yaml
  svelte.config.js
  vite.config.ts
  playwright.config.ts
  tests/
    e2e.spec.ts         ← Playwright E2E test
```

For this sub-spec, the SvelteKit app is a **minimal scaffold**:

- One page that connects as guest, sends a command, and displays the event stream
- Proves the full pipeline works end-to-end
- No terminal UI, no chat UI, no styling — those come in sub-spec 2

The `buf` tool generates TypeScript clients from `web.proto` into
`src/lib/connect/`. Generated code is committed.

**Buf configuration:** The existing repo-root `buf.yaml` workspace includes the
proto sources. A new `web/buf.gen.yaml` configures TypeScript-specific code
generation using `@connectrpc/protoc-gen-connect-es` and `@bufbuild/protoc-gen-es`.
The `task web:generate` command runs `buf generate --template web/buf.gen.yaml`
from the repo root, reading protos from `api/proto/` and writing output to
`web/src/lib/connect/`.

### Build Pipeline

The `task` workflow orchestrates the build:

1. `task web:generate` — run `buf generate` for TypeScript clients
1. `task web:build` — run `pnpm build` (SvelteKit static adapter)
1. `task web:embed` — copy `web/build/` to `internal/web/dist/`
1. `task build` — includes step 3, then `go build` (embeds via `go:embed`)

For development:

- `task web:dev` — run `pnpm dev` (Vite dev server on `:5173`)
- Gateway runs on `:8080` with CORS allowing `:5173`
- No `--web-dir` needed in dev — SvelteKit dev server handles static files

## Config

New `gatewayConfig` fields:

```go
type gatewayConfig struct {
    // ... existing fields ...
    WebAddr     string   `koanf:"web_addr"`
    WebDir      string   `koanf:"web_dir"`
    CORSOrigins []string `koanf:"cors_origins"`
}
```

New CLI flags: `--web-addr` (default `:8080`), `--web-dir` (default empty),
`--cors-origins` (default empty, same-origin only).

Config file:

```yaml
gateway:
  web_addr: ":8080"
  web_dir: ""                          # empty = use embedded files
  cors_origins: []                     # e.g., ["http://localhost:5173"] for dev
```

## Testing Strategy

### Unit Tests (Go)

- ConnectRPC handler: mock gRPC client, verify Login/SendCommand/StreamEvents
  translate correctly to core RPCs
- Static file server: embedded vs override, SPA fallback routing
- CORS middleware: verify allowed origins, methods, headers

### Playwright E2E Test

A browser-level test proving the full pipeline:

1. Start gateway (with core) as a test fixture
1. Build SvelteKit and serve via `--web-dir`
1. Open the page in headless Chromium
1. Click "Connect as Guest"
1. Type a say command
1. Assert the event echo appears on screen

This lives at `web/tests/e2e.spec.ts` and runs via `pnpm exec playwright test`.

### Existing Tests

Existing telnet E2E tests and unit tests MUST continue to pass unchanged. The
gateway changes are additive — new HTTP server, new config fields.

## Dependencies

### Go

- `connectrpc.com/connect` — ConnectRPC Go library
- `nhooyr.io/websocket` — NOT needed (ConnectRPC uses HTTP, not raw WebSocket)
- `connectrpc.com/cors` — CORS utilities for ConnectRPC (or standard middleware)

### Node.js / pnpm

- `@sveltejs/kit` — SvelteKit framework
- `@sveltejs/adapter-static` — static site generation
- `@connectrpc/connect` — ConnectRPC client runtime
- `@connectrpc/connect-web` — browser transport
- `@bufbuild/protobuf` — protobuf runtime for TypeScript
- `@playwright/test` — E2E testing
- `buf` CLI — proto code generation

### Build Tools

- `buf` — protobuf toolchain (already used for Go proto generation)
- `pnpm` — Node.js package manager
- `task` — build orchestration (extended with `web:*` tasks)

## Operator Documentation

Update `site/docs/operators/configuration.md` with:

- `gateway.web_addr` and `--web-addr` flag
- `gateway.web_dir` and `--web-dir` flag
- How to serve a custom SvelteKit build
- Development setup instructions

## Future Sub-Specs

This sub-spec establishes the transport layer. The next two sub-specs build on it:

- **Sub-spec 2: Web Client UX** — terminal mode (ANSI-like rendering, command
  input, scrollback) AND chat mode (message bubbles, formatted output, rich
  interactions). Two distinct experiences sharing the same ConnectRPC transport.
- **Sub-spec 3: Portal** — wiki pages, character profiles, admin dashboard.
  Standard web pages using ConnectRPC for data fetching.

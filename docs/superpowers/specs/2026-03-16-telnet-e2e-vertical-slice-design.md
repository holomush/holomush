<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Telnet E2E Vertical Slice — Design Spec

## Goal

Prove the complete telnet vertical slice works end-to-end: multiple users connect via telnet to the gateway process, authenticate, and communicate (say/pose) with each other in the same room — with PostgreSQL, gRPC, and event broadcasting all exercised.

## Scope

Two deliverables:

1. **Gateway telnet handler** — replace the placeholder in `cmd/holomush/gateway.go` with a real handler that forwards commands over gRPC to core and streams events back
2. **E2E BDD integration test** — Ginkgo/Gomega test that spins up all components and verifies multi-user communication

## Non-Goals

- Real authentication (player registration, passwords, character creation)
- Web/WebSocket client support (Epic 8)
- Room navigation during the test (movement works but is not the focus)
- Plugin system verification
- ABAC policy evaluation in the E2E flow (tested separately in Epic 7)

## Architecture

```text
[Telnet Client A] ──TCP──> [Gateway Process] ──gRPC──> [Core Process] ──> [PostgreSQL]
[Telnet Client B] ──TCP──> [Gateway Process] ──gRPC──> [Core Process] ──> [PostgreSQL]
                              |                            |
                              |<── gRPC Subscribe stream ──|
                              |    (location events)       |
```

### Component Responsibilities

| Component | Role |
|-----------|------|
| PostgreSQL | Event persistence, world state |
| Core process | Event engine, session management, broadcaster, gRPC server |
| Gateway process | Telnet listener, gRPC client, protocol translation |
| Telnet clients | Raw TCP connections simulating players |

## Gateway Telnet Handler Design

### Connection Lifecycle

1. Accept TCP connection, assign connection ID (ULID)
2. Send welcome banner
3. Wait for `connect <username> <password>` command
4. Call `grpcClient.Authenticate(username, password)` → get session ID, character info
5. Open `grpcClient.Subscribe(sessionID, ["location:" + locationID])` → event stream
6. Enter main loop:
   - **Player input** → `grpcClient.HandleCommand(sessionID, command)`
   - **Event from stream** → format and send to telnet connection
   - **Error/disconnect** → `grpcClient.Disconnect(sessionID)`, close connection

### Message Formatting

Reuse the formatting patterns from `internal/telnet/handler.go`:

- Say echo: `You say, "message"`
- Say broadcast: `CharName says, "message"`
- Pose echo: `CharName action`
- Pose broadcast: `CharName action`

### File Location

- `internal/telnet/gateway_handler.go` — new file for the gRPC-backed handler
- `internal/telnet/gateway_handler_test.go` — unit tests

This keeps the gateway handler alongside the existing in-process handler. Both implement the same user-facing telnet protocol but differ in backend (direct engine calls vs gRPC).

## Authentication

### Guest Login

The primary auth flow for this slice is **guest login**:

```text
> connect guest
Welcome, Sapphire_Neon! You are in the Town Square.
```

- Command: `connect guest` (no password)
- Server assigns a random two-word name from a themed word list
- Format: `Word1_Word2` (e.g., `Sapphire_Neon`, `Opal_Argon`, `Ruby_Xenon`)
- Guest character is created on the fly, placed in the start location
- Guest characters are ephemeral — cleaned up on disconnect

### Name Generation

Names are drawn from a **themed word pool** with two categories combined:

| Theme | Category A | Category B | Example Names |
|-------|-----------|-----------|---------------|
| Default | Gemstones | Periodic elements | `Sapphire_Neon`, `Opal_Argon` |

The name generator MUST:

- Be pluggable — support additional themes via a `NameTheme` interface
- Avoid collisions — check active sessions before assigning
- Use title case — `Ruby_Xenon` not `ruby_xenon`

```go
type NameTheme interface {
    Name() string
    Generate() (firstName, secondName string)
}
```

Default theme word pools:

- **Gemstones**: Amber, Amethyst, Beryl, Coral, Diamond, Emerald, Garnet, Jade,
  Jasper, Lapis, Moonstone, Obsidian, Onyx, Opal, Pearl, Quartz, Ruby,
  Sapphire, Topaz, Turquoise
- **Elements**: Argon, Boron, Carbon, Cobalt, Copper, Gold, Helium, Iodine,
  Iron, Krypton, Neon, Nickel, Osmium, Radium, Radon, Silver, Titanium,
  Xenon, Zinc, Zircon

20 × 20 = 400 unique combinations — sufficient for the vertical slice.

### Registered Login (Future)

For future use, the full auth flow:

```text
> connect <username> <password>
```

Not implemented in this slice — returns "Registered accounts are not yet
available. Use `connect guest` to play."

### E2E Test Auth

The E2E test uses `connect guest` for both players. Each gets a unique
name from the generator, ensuring distinct character IDs and proper
event filtering (Player A does not see their own broadcast events).

## E2E Test Design

### Test Infrastructure

- **PostgreSQL**: testcontainers (`postgres:18-alpine`) — used as the event store (not memory store) to verify full persistence
- **Core**: started in-process as a goroutine (not OS exec)
- **Gateway**: started in-process as a goroutine
- **Telnet clients**: raw `net.Dial("tcp", addr)` + `bufio.Scanner`
- **TLS certificates**: generated in temp directory per test run

### Process Startup Order

1. Start PostgreSQL container, run migrations
2. Seed test data (two players, two characters, one location)
3. Start core process (gRPC server on random port)
4. Start gateway process (telnet listener on random port, gRPC client to core)
5. Connect telnet clients

### BDD Scenarios (Ginkgo)

```text
Describe("Telnet Vertical Slice E2E")
  BeforeSuite: PostgreSQL + Core + Gateway startup

  Describe("Authentication")
    It("Player A connects and authenticates successfully")
    It("rejects invalid credentials with error message")

  Describe("Say Communication")
    Context("Two players in the same room")
      It("Player A says something and sees their own echo")
      It("Player B receives Player A's say via event stream")
      It("Player A receives Player B's say via event stream")

  Describe("Pose Communication")
    It("Player A poses and sees their own echo")
    It("Player B receives Player A's pose via event stream")

  Describe("Disconnect")
    It("Player A disconnects cleanly via quit")
    It("Player B continues receiving events after A disconnects")

  AfterSuite: shutdown gateway, core, PostgreSQL
```

### Assertion Strategy

- Use `Eventually()` with 5-second timeout for async event delivery
- Read telnet output line-by-line via buffered scanner
- Match expected output patterns (e.g., `ContainSubstring("says,")`)
- Verify events are persisted by checking event store after commands

### Telnet Client Helper

```go
type testTelnetClient struct {
    conn    net.Conn
    scanner *bufio.Scanner
    writer  *bufio.Writer
}

func (c *testTelnetClient) SendLine(line string)
func (c *testTelnetClient) ReadLine() string
func (c *testTelnetClient) ReadUntil(pattern string, timeout time.Duration) string
func (c *testTelnetClient) Close()
```

## Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| testcontainers-go | existing | PostgreSQL container |
| Ginkgo/Gomega | existing | BDD test framework |
| gRPC | existing | Core ↔ Gateway communication |
| net (stdlib) | — | Telnet client simulation |

## Risks

| Risk | Mitigation |
|------|------------|
| gRPC Subscribe stream reliability | Test includes reconnect scenario |
| Port conflicts in CI | Use `:0` for random port assignment |
| Telnet output timing | `Eventually()` with generous timeout |
| TLS cert generation in tests | Reuse `internal/tls` helpers, temp dir per run |
| Hardcoded auth limits concurrency | Only 2 test users needed; guest pool is follow-up |

## Success Criteria

- [ ] Gateway telnet handler forwards commands to core via gRPC
- [ ] Gateway telnet handler streams events back to telnet clients
- [ ] E2E test passes with two simultaneous telnet connections
- [ ] Say and pose messages are delivered between players
- [ ] Event persistence verified in PostgreSQL
- [ ] Test runs in CI without manual intervention
- [ ] Test completes in under 30 seconds

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Telnet E2E Vertical Slice — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove the complete telnet vertical slice works end-to-end — two users connect via telnet to the gateway process, authenticate as guests, and communicate (say/pose) with events flowing through PostgreSQL, gRPC, and the broadcaster.

**Architecture:** The gateway process accepts TCP telnet connections, translates commands to gRPC calls against the core process, and streams events back. A guest authenticator creates ephemeral characters with themed names (Gemstone\_Element). The E2E test uses testcontainers PostgreSQL, starts core and gateway in-process as goroutines, and verifies multi-user communication via raw TCP clients.

**Tech Stack:** Go 1.24, gRPC (existing proto), testcontainers-go, Ginkgo/Gomega, PostgreSQL 18, mTLS (existing `internal/tls`)

**Spec:** `docs/superpowers/specs/2026-03-16-telnet-e2e-vertical-slice-design.md`

**Epic:** `holomush-hbkx`

---

## File Structure

### New Files

| File | Responsibility |
| --- | --- |
| `internal/telnet/guest_auth.go` | `NameTheme` interface, default gemstone/element theme, `GuestAuthenticator` implementing `grpc.Authenticator` |
| `internal/telnet/guest_auth_test.go` | Unit tests for name generation, collision avoidance, authenticator behavior |
| `internal/telnet/gateway_handler.go` | `GatewayHandler` — gRPC-backed telnet connection handler (replaces the gateway stub) |
| `internal/telnet/gateway_handler_test.go` | Unit tests with mock gRPC client interface |
| `test/integration/telnet/telnet_suite_test.go` | Ginkgo suite bootstrap for telnet E2E |
| `test/integration/telnet/e2e_test.go` | Full E2E BDD test: postgres + core + gateway + 2 telnet clients |

### Modified Files

| File | Change |
| --- | --- |
| `internal/grpc/server.go:117` | Add `WithAuthenticator` call — no code change needed (option already exists), but `core.go` wiring changes |
| `cmd/holomush/core.go:341` | Wire `GuestAuthenticator` into `NewCoreServer` via `WithAuthenticator(...)` |
| `cmd/holomush/gateway.go:195-307` | Replace placeholder `handleTelnetConnection` with `GatewayHandler` dispatch; widen `GRPCClient` interface or pass concrete `*grpc.Client` |
| `cmd/holomush/deps.go:112-115` | Widen `GRPCClient` interface to expose the four RPC methods |

### Unchanged (Reference)

| File | Why Referenced |
| --- | --- |
| `internal/grpc/client.go` | `Client` already implements all 4 RPCs — gateway handler calls these |
| `internal/grpc/server.go` | `Authenticator` interface, `CoreServer`, `WithAuthenticator` option — all exist |
| `internal/telnet/handler.go` | Message format patterns (say echo, pose echo) — reuse in gateway handler |
| `internal/core/engine.go` | `SayPayload`, `PosePayload` structs for event deserialization |
| `test/integration/phase1_5_test.go` | Template for testcontainers + TLS + gRPC setup |

---

## Chunk 1: Guest Authenticator

### Task 1: Guest Name Theme + Generator

**Files:**

- Create: `internal/telnet/guest_auth.go`
- Create: `internal/telnet/guest_auth_test.go`

#### Step 1.1: Write the NameTheme interface and default theme test

- [ ] **Write failing test for default theme generation**

In `internal/telnet/guest_auth_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestGemstoneElementTheme_Name(t *testing.T) {
    theme := NewGemstoneElementTheme()
    assert.Equal(t, "gemstone_element", theme.Name())
}

func TestGemstoneElementTheme_Generate(t *testing.T) {
    theme := NewGemstoneElementTheme()
    first, second := theme.Generate()
    assert.NotEmpty(t, first, "first name part should not be empty")
    assert.NotEmpty(t, second, "second name part should not be empty")
}

func TestGemstoneElementTheme_UniqueNames(t *testing.T) {
    theme := NewGemstoneElementTheme()
    seen := make(map[string]bool)
    // Generate 50 names - with 400 combos, collisions are unlikely
    // but we're testing that Generate produces varied output
    for i := 0; i < 50; i++ {
        first, second := theme.Generate()
        name := first + "_" + second
        seen[name] = true
    }
    assert.Greater(t, len(seen), 1, "should generate varied names")
}

func TestGemstoneElementTheme_TitleCase(t *testing.T) {
    theme := NewGemstoneElementTheme()
    for i := 0; i < 20; i++ {
        first, second := theme.Generate()
        assert.Regexp(t, `^[A-Z][a-z]+$`, first, "first part should be title case")
        assert.Regexp(t, `^[A-Z][a-z]+$`, second, "second part should be title case")
    }
}
```

- [ ] **Run test to verify it fails**

```bash
task test -- -run TestGemstoneElementTheme -v ./internal/telnet/...
```

Expected: FAIL — `NewGemstoneElementTheme` undefined.

#### Step 1.2: Implement NameTheme interface and default theme

- [ ] **Write minimal implementation**

In `internal/telnet/guest_auth.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
    "math/rand/v2"
)

// NameTheme generates themed two-part names for guest characters.
type NameTheme interface {
    Name() string
    Generate() (firstName, secondName string)
}

// gemstones is the word pool for the first name part.
var gemstones = []string{
    "Amber", "Amethyst", "Beryl", "Coral", "Diamond",
    "Emerald", "Garnet", "Jade", "Jasper", "Lapis",
    "Moonstone", "Obsidian", "Onyx", "Opal", "Pearl",
    "Quartz", "Ruby", "Sapphire", "Topaz", "Turquoise",
}

// elements is the word pool for the second name part.
var elements = []string{
    "Argon", "Boron", "Carbon", "Cobalt", "Copper",
    "Gold", "Helium", "Iodine", "Iron", "Krypton",
    "Neon", "Nickel", "Osmium", "Radium", "Radon",
    "Silver", "Titanium", "Xenon", "Zinc", "Zircon",
}

// GemstoneElementTheme generates names like "Sapphire_Neon".
type GemstoneElementTheme struct{}

// NewGemstoneElementTheme creates a new gemstone/element theme.
func NewGemstoneElementTheme() *GemstoneElementTheme {
    return &GemstoneElementTheme{}
}

// Name returns the theme identifier.
func (t *GemstoneElementTheme) Name() string {
    return "gemstone_element"
}

// Generate returns a random gemstone and element pair.
func (t *GemstoneElementTheme) Generate() (string, string) {
    return gemstones[rand.IntN(len(gemstones))], elements[rand.IntN(len(elements))]
}
```

- [ ] **Run test to verify it passes**

```bash
task test -- -run TestGemstoneElementTheme -v ./internal/telnet/...
```

Expected: PASS

- [ ] **Commit**

```bash
jj --no-pager new -m "feat(telnet): add NameTheme interface and gemstone/element theme"
```

### Task 2: GuestAuthenticator

**Files:**

- Modify: `internal/telnet/guest_auth.go`
- Modify: `internal/telnet/guest_auth_test.go`

The `GuestAuthenticator` implements `grpc.Authenticator` — when username is `"guest"`, it generates a themed name, creates an ephemeral character ID + location ID, and returns an `AuthResult`. It tracks active guest names to avoid collisions.

#### Step 2.1: Write failing test for GuestAuthenticator

- [ ] **Write test**

Append to `internal/telnet/guest_auth_test.go`:

```go
import (
    "context"

    "github.com/oklog/ulid/v2"

    holoGRPC "github.com/holomush/holomush/internal/grpc"
)

func TestGuestAuthenticator_GuestLogin(t *testing.T) {
    startLocation := ulid.Make()
    auth := NewGuestAuthenticator(NewGemstoneElementTheme(), startLocation)

    result, err := auth.Authenticate(context.Background(), "guest", "")
    require.NoError(t, err)
    require.NotNil(t, result)
    assert.NotEqual(t, ulid.ULID{}, result.CharacterID)
    assert.NotEmpty(t, result.CharacterName)
    assert.Equal(t, startLocation, result.LocationID)
    // Name format: Word_Word
    assert.Contains(t, result.CharacterName, "_")
}

func TestGuestAuthenticator_RegisteredLoginRejected(t *testing.T) {
    startLocation := ulid.Make()
    auth := NewGuestAuthenticator(NewGemstoneElementTheme(), startLocation)

    result, err := auth.Authenticate(context.Background(), "player1", "password")
    assert.Error(t, err)
    assert.Nil(t, result)
    assert.Contains(t, err.Error(), "not yet available")
}

func TestGuestAuthenticator_UniqueNames(t *testing.T) {
    startLocation := ulid.Make()
    auth := NewGuestAuthenticator(NewGemstoneElementTheme(), startLocation)

    names := make(map[string]bool)
    for i := 0; i < 20; i++ {
        result, err := auth.Authenticate(context.Background(), "guest", "")
        require.NoError(t, err)
        assert.False(t, names[result.CharacterName], "duplicate name: %s", result.CharacterName)
        names[result.CharacterName] = true
    }
}

func TestGuestAuthenticator_ImplementsInterface(t *testing.T) {
    startLocation := ulid.Make()
    var _ holoGRPC.Authenticator = NewGuestAuthenticator(NewGemstoneElementTheme(), startLocation)
}
```

- [ ] **Run test to verify it fails**

```bash
task test -- -run "TestGuestAuthenticator" -v ./internal/telnet/...
```

Expected: FAIL — `NewGuestAuthenticator` undefined.

#### Step 2.2: Implement GuestAuthenticator

- [ ] **Write implementation**

Append to `internal/telnet/guest_auth.go`:

```go
import (
    "context"
    "fmt"
    "sync"

    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"

    holoGRPC "github.com/holomush/holomush/internal/grpc"
)

// GuestAuthenticator handles "connect guest" by generating themed names.
// It implements grpc.Authenticator.
type GuestAuthenticator struct {
    theme         NameTheme
    startLocation ulid.ULID
    mu            sync.Mutex
    activeNames   map[string]bool
}

// NewGuestAuthenticator creates a new guest authenticator.
func NewGuestAuthenticator(theme NameTheme, startLocation ulid.ULID) *GuestAuthenticator {
    return &GuestAuthenticator{
        theme:         theme,
        startLocation: startLocation,
        activeNames:   make(map[string]bool),
    }
}

// Authenticate handles guest login (username "guest") and rejects registered logins.
func (a *GuestAuthenticator) Authenticate(_ context.Context, username, _ string) (*holoGRPC.AuthResult, error) {
    if username != "guest" {
        return nil, oops.Code("NOT_AVAILABLE").
            Errorf("registered accounts are not yet available — use `connect guest` to play")
    }

    name, err := a.generateUniqueName()
    if err != nil {
        return nil, oops.Code("NAME_GENERATION_FAILED").Wrap(err)
    }

    return &holoGRPC.AuthResult{
        CharacterID:   ulid.Make(),
        CharacterName: name,
        LocationID:    a.startLocation,
    }, nil
}

// ReleaseGuest removes a guest name from the active set (call on disconnect).
func (a *GuestAuthenticator) ReleaseGuest(name string) {
    a.mu.Lock()
    defer a.mu.Unlock()
    delete(a.activeNames, name)
}

// generateUniqueName tries up to 50 times to find an unused name.
func (a *GuestAuthenticator) generateUniqueName() (string, error) {
    a.mu.Lock()
    defer a.mu.Unlock()

    for range 50 {
        first, second := a.theme.Generate()
        name := first + "_" + second
        if !a.activeNames[name] {
            a.activeNames[name] = true
            return name, nil
        }
    }
    return "", fmt.Errorf("could not generate unique name after 50 attempts")
}
```

Note: The final file will have a single import block combining all imports. The code shown in step 1.2 and this step will be merged into one file. When appending to `guest_auth_test.go`, merge the new imports into the existing `import` block — do not create a second one.

- [ ] **Run test to verify it passes**

```bash
task test -- -run "TestGuestAuthenticator" -v ./internal/telnet/...
```

Expected: PASS

- [ ] **Run all telnet package tests**

```bash
task test -- -v ./internal/telnet/...
```

Expected: All PASS (existing + new tests)

- [ ] **Commit**

```bash
jj --no-pager new -m "feat(telnet): add GuestAuthenticator with themed name generation"
```

---

## Chunk 2: Gateway Handler

### Task 3: Widen GRPCClient Interface in deps.go

**Files:**

- Modify: `cmd/holomush/deps.go:112-115`

The current `GRPCClient` interface only has `Close() error`. The gateway handler needs all four RPCs. The concrete `*grpc.Client` already implements them, so we just widen the interface.

#### Step 3.1: Update the interface

- [ ] **Modify GRPCClient interface**

In `cmd/holomush/deps.go`, replace lines 112–115:

```go
// GRPCClient interface wraps the methods used from holoGRPC.Client.
type GRPCClient interface {
    Close() error
}
```

With:

```go
// GRPCClient interface wraps the methods used from holoGRPC.Client.
type GRPCClient interface {
    Authenticate(ctx context.Context, req *corev1.AuthRequest) (*corev1.AuthResponse, error)
    HandleCommand(ctx context.Context, req *corev1.CommandRequest) (*corev1.CommandResponse, error)
    Subscribe(ctx context.Context, req *corev1.SubscribeRequest) (corev1.Core_SubscribeClient, error)
    Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error)
    Close() error
}
```

This requires adding imports for `context` and `corev1` if not already present. The concrete `*holoGRPC.Client` already satisfies this interface — verify with:

```bash
task build
```

- [ ] **Verify compilation**

```bash
task build
```

Expected: Success (no type assertion errors)

- [ ] **Commit**

```bash
jj --no-pager new -m "refactor(gateway): widen GRPCClient interface to expose all four RPCs"
```

### Task 4: Gateway Telnet Handler

**Files:**

- Create: `internal/telnet/gateway_handler.go`
- Create: `internal/telnet/gateway_handler_test.go`

The `GatewayHandler` is the gRPC-backed telnet connection handler. It replaces the stub in `gateway.go`. It uses the same user-facing protocol as `ConnectionHandler` (connect, say, pose, quit) but calls `grpc.Client` RPCs instead of direct engine methods.

**Note:** `CoreClient` (defined here) and `GRPCClient` (in `deps.go`) have identical method signatures. Both exist because `CoreClient` is the handler's dependency interface (in the `telnet` package) while `GRPCClient` is the gateway's dependency interface (in `cmd/holomush`). The concrete `*grpc.Client` satisfies both — Go structural typing handles this automatically.

**Known limitation:** Broadcast events display a truncated actor ID (first 8 chars of ULID) instead of the character name, because the gRPC `Event` proto carries `actor_id` but not a display name. Resolving this requires either adding `actor_name` to the proto or a character name lookup cache — tracked as follow-up work.

#### Step 4.1: Define the CoreClient interface for testability

- [ ] **Write the interface and failing test**

The handler needs an interface matching what `*grpc.Client` provides, so we can mock it in tests.

In `internal/telnet/gateway_handler.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
    "bufio"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "log/slog"
    "net"
    "strings"

    "github.com/holomush/holomush/internal/core"
    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// CoreClient is the interface for the gRPC client used by GatewayHandler.
type CoreClient interface {
    Authenticate(ctx context.Context, req *corev1.AuthRequest) (*corev1.AuthResponse, error)
    HandleCommand(ctx context.Context, req *corev1.CommandRequest) (*corev1.CommandResponse, error)
    Subscribe(ctx context.Context, req *corev1.SubscribeRequest) (corev1.Core_SubscribeClient, error)
    Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error)
}
```

In `internal/telnet/gateway_handler_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
    "testing"

    holoGRPC "github.com/holomush/holomush/internal/grpc"
)

func TestCoreClient_SatisfiedByGRPCClient(t *testing.T) {
    // Compile-time check that *grpc.Client satisfies CoreClient
    var _ CoreClient = (*holoGRPC.Client)(nil)
}
```

- [ ] **Run test to verify it compiles and passes**

```bash
task test -- -run TestCoreClient_SatisfiedByGRPCClient -v ./internal/telnet/...
```

Expected: PASS

#### Step 4.2: Write GatewayHandler authentication test

- [ ] **Write failing test for guest connect flow**

Append to `internal/telnet/gateway_handler_test.go`:

```go
import (
    "bufio"
    "context"
    "net"
    "strings"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// mockCoreClient is a test double for the gRPC client.
type mockCoreClient struct {
    authResp    *corev1.AuthResponse
    authErr     error
    cmdResp     *corev1.CommandResponse
    cmdErr      error
    subStream   corev1.Core_SubscribeClient
    subErr      error
    disconnResp *corev1.DisconnectResponse
    disconnErr  error
}

func (m *mockCoreClient) Authenticate(_ context.Context, _ *corev1.AuthRequest) (*corev1.AuthResponse, error) {
    return m.authResp, m.authErr
}

func (m *mockCoreClient) HandleCommand(_ context.Context, _ *corev1.CommandRequest) (*corev1.CommandResponse, error) {
    return m.cmdResp, m.cmdErr
}

func (m *mockCoreClient) Subscribe(_ context.Context, _ *corev1.SubscribeRequest) (corev1.Core_SubscribeClient, error) {
    return m.subStream, m.subErr
}

func (m *mockCoreClient) Disconnect(_ context.Context, _ *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
    return m.disconnResp, m.disconnErr
}

func TestGatewayHandler_GuestConnect(t *testing.T) {
    client, server := net.Pipe()
    defer client.Close()
    defer server.Close()

    mock := &mockCoreClient{
        authResp: &corev1.AuthResponse{
            Success:       true,
            SessionId:     "session-123",
            CharacterId:   "01HK153X0006AFVGQT5ZYC0GEK",
            CharacterName: "Sapphire_Neon",
            LocationId:    "01HK153X0006AFVGQT61FPQX3S",
        },
        // Subscribe returns an error so the event goroutine is not launched
        subErr: fmt.Errorf("not subscribed in this test"),
    }

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    handler := NewGatewayHandler(server, mock)
    go handler.Handle(ctx)

    reader := bufio.NewReader(client)

    // Read welcome banner
    line, err := reader.ReadString('\n')
    require.NoError(t, err)
    assert.Contains(t, line, "Welcome to HoloMUSH")

    // Read usage prompt
    line, err = reader.ReadString('\n')
    require.NoError(t, err)
    assert.Contains(t, line, "connect")

    // Send guest connect
    _, err = fmt.Fprintln(client, "connect guest")
    require.NoError(t, err)

    // Read welcome response
    line, err = reader.ReadString('\n')
    require.NoError(t, err)
    assert.Contains(t, line, "Sapphire_Neon")
}
```

- [ ] **Run test to verify it fails**

```bash
task test -- -run TestGatewayHandler_GuestConnect -v ./internal/telnet/...
```

Expected: FAIL — `NewGatewayHandler` undefined.

#### Step 4.3: Implement GatewayHandler

- [ ] **Write the handler**

Complete `internal/telnet/gateway_handler.go` (add to the file created in step 4.1):

```go
// GatewayHandler handles a single telnet connection using gRPC to communicate with core.
type GatewayHandler struct {
    conn      net.Conn
    reader    *bufio.Reader
    client    CoreClient
    sessionID string
    charName  string
    authed    bool
    quitting  bool
}

// NewGatewayHandler creates a new gateway telnet handler.
func NewGatewayHandler(conn net.Conn, client CoreClient) *GatewayHandler {
    return &GatewayHandler{
        conn:   conn,
        reader: bufio.NewReader(conn),
        client: client,
    }
}

// Handle processes the connection until closed or context cancelled.
func (h *GatewayHandler) Handle(ctx context.Context) {
    defer func() {
        if h.authed && h.sessionID != "" {
            _, _ = h.client.Disconnect(ctx, &corev1.DisconnectRequest{
                SessionId: h.sessionID,
            })
        }
        if err := h.conn.Close(); err != nil {
            slog.Debug("error closing gateway connection", "error", err)
        }
    }()

    h.send("Welcome to HoloMUSH!")
    h.send("Use: connect guest")

    lineCh := make(chan string)
    errCh := make(chan error)

    go func() {
        for {
            line, err := h.reader.ReadString('\n')
            if err != nil {
                errCh <- err
                return
            }
            lineCh <- strings.TrimSpace(line)
        }
    }()

    // eventCh is set after authentication + subscription
    var eventCh <-chan *corev1.Event

    for {
        select {
        case <-ctx.Done():
            return

        case err := <-errCh:
            if !errors.Is(err, io.EOF) {
                slog.Debug("gateway connection read error", "error", err)
            }
            return

        case line := <-lineCh:
            newEventCh := h.processLine(ctx, line)
            if newEventCh != nil {
                eventCh = newEventCh
            }
            if h.quitting {
                return
            }

        case event, ok := <-eventCh:
            if !ok {
                slog.Debug("event stream closed")
                h.send("Connection to server lost.")
                return
            }
            h.sendProtoEvent(event)
        }
    }
}

func (h *GatewayHandler) processLine(ctx context.Context, line string) <-chan *corev1.Event {
    cmd, arg := core.ParseCommand(line)

    switch cmd {
    case "connect":
        return h.handleConnect(ctx, arg)
    case "say":
        h.handleSay(ctx, arg)
    case "pose":
        h.handlePose(ctx, arg)
    case "quit":
        h.handleQuit()
    default:
        if cmd != "" {
            h.send("Unknown command: " + cmd)
        }
    }
    return nil
}

func (h *GatewayHandler) handleConnect(ctx context.Context, arg string) <-chan *corev1.Event {
    if h.authed {
        h.send("Already connected.")
        return nil
    }

    // Parse: "guest" or "username password"
    parts := strings.SplitN(arg, " ", 2)
    username := parts[0]
    password := ""
    if len(parts) > 1 {
        password = parts[1]
    }

    resp, err := h.client.Authenticate(ctx, &corev1.AuthRequest{
        Username: username,
        Password: password,
    })
    if err != nil {
        slog.Error("gRPC authenticate failed", "error", err)
        h.send("Connection error. Please try again.")
        return nil
    }
    if !resp.Success {
        h.send(resp.Error)
        return nil
    }

    h.sessionID = resp.SessionId
    h.charName = resp.CharacterName
    h.authed = true

    h.send(fmt.Sprintf("Welcome, %s!", h.charName))

    // Subscribe to location events
    stream, err := h.client.Subscribe(ctx, &corev1.SubscribeRequest{
        SessionId: h.sessionID,
        Streams:   []string{"location:" + resp.LocationId},
    })
    if err != nil {
        slog.Warn("failed to subscribe to events", "error", err)
        return nil
    }

    // Start goroutine to read from gRPC stream into a channel
    eventCh := make(chan *corev1.Event, 100)
    go func() {
        defer close(eventCh)
        for {
            event, err := stream.Recv()
            if err != nil {
                if !errors.Is(err, io.EOF) && ctx.Err() == nil {
                    slog.Debug("event stream recv error", "error", err)
                }
                return
            }
            select {
            case eventCh <- event:
            case <-ctx.Done():
                return
            }
        }
    }()

    return eventCh
}

func (h *GatewayHandler) handleSay(ctx context.Context, message string) {
    if !h.authed {
        h.send("You must connect first.")
        return
    }
    if message == "" {
        h.send("Say what?")
        return
    }

    resp, err := h.client.HandleCommand(ctx, &corev1.CommandRequest{
        SessionId: h.sessionID,
        Command:   "say " + message,
    })
    if err != nil {
        slog.Error("say command failed", "error", err)
        h.send("Error: Your message could not be sent.")
        return
    }
    if !resp.Success {
        h.send("Error: " + resp.Error)
        return
    }
    // Echo to sender
    h.send(fmt.Sprintf("You say, %q", message))
}

func (h *GatewayHandler) handlePose(ctx context.Context, action string) {
    if !h.authed {
        h.send("You must connect first.")
        return
    }
    if action == "" {
        h.send("Pose what?")
        return
    }

    resp, err := h.client.HandleCommand(ctx, &corev1.CommandRequest{
        SessionId: h.sessionID,
        Command:   "pose " + action,
    })
    if err != nil {
        slog.Error("pose command failed", "error", err)
        h.send("Error: Your action could not be sent.")
        return
    }
    if !resp.Success {
        h.send("Error: " + resp.Error)
        return
    }
    h.send(fmt.Sprintf("%s %s", h.charName, action))
}

func (h *GatewayHandler) handleQuit() {
    h.send("Goodbye!")
    h.quitting = true
}

func (h *GatewayHandler) send(msg string) {
    if _, err := fmt.Fprintln(h.conn, msg); err != nil {
        slog.Debug("failed to send to gateway client", "error", err)
    }
}

func (h *GatewayHandler) sendProtoEvent(e *corev1.Event) {
    switch e.Type {
    case "say":
        var p core.SayPayload
        if err := json.Unmarshal(e.Payload, &p); err != nil {
            slog.Error("failed to unmarshal say event", "error", err)
            return
        }
        // Use ActorId prefix as name until character name lookup is wired
        actorName := e.ActorId
        if len(actorName) > 8 {
            actorName = actorName[:8]
        }
        h.send(fmt.Sprintf("%s says, %q", actorName, p.Message))
    case "pose":
        var p core.PosePayload
        if err := json.Unmarshal(e.Payload, &p); err != nil {
            slog.Error("failed to unmarshal pose event", "error", err)
            return
        }
        actorName := e.ActorId
        if len(actorName) > 8 {
            actorName = actorName[:8]
        }
        h.send(fmt.Sprintf("%s %s", actorName, p.Action))
    default:
        slog.Debug("unknown event type in gateway handler", "type", e.Type)
    }
}
```

- [ ] **Run test to verify it passes**

```bash
task test -- -run TestGatewayHandler -v ./internal/telnet/...
```

Expected: PASS

- [ ] **Commit**

```bash
jj --no-pager new -m "feat(telnet): add GatewayHandler — gRPC-backed telnet connection handler"
```

#### Step 4.4: Add more unit tests for GatewayHandler

- [ ] **Write test for say/quit commands**

Append to `internal/telnet/gateway_handler_test.go`:

```go
func TestGatewayHandler_SayCommand(t *testing.T) {
    client, server := net.Pipe()
    defer client.Close()
    defer server.Close()

    mock := &mockCoreClient{
        authResp: &corev1.AuthResponse{
            Success:       true,
            SessionId:     "session-123",
            CharacterId:   "01HK153X0006AFVGQT5ZYC0GEK",
            CharacterName: "Ruby_Xenon",
            LocationId:    "01HK153X0006AFVGQT61FPQX3S",
        },
        cmdResp: &corev1.CommandResponse{Success: true, Output: "You say: hello"},
        // Subscribe returns an error so the event goroutine is not launched
        subErr: fmt.Errorf("not subscribed in this test"),
    }

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    handler := NewGatewayHandler(server, mock)
    go handler.Handle(ctx)

    reader := bufio.NewReader(client)

    // Skip banner (2 lines)
    _, _ = reader.ReadString('\n')
    _, _ = reader.ReadString('\n')

    // Connect
    fmt.Fprintln(client, "connect guest")
    line, _ := reader.ReadString('\n') // Welcome, Ruby_Xenon!
    assert.Contains(t, line, "Ruby_Xenon")

    // Say
    fmt.Fprintln(client, "say hello everyone")
    line, _ = reader.ReadString('\n')
    assert.Contains(t, line, `You say, "hello everyone"`)

    // Quit
    fmt.Fprintln(client, "quit")
    line, _ = reader.ReadString('\n')
    assert.Contains(t, line, "Goodbye")
}

func TestGatewayHandler_RejectsCommandsBeforeAuth(t *testing.T) {
    client, server := net.Pipe()
    defer client.Close()
    defer server.Close()

    mock := &mockCoreClient{}

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    handler := NewGatewayHandler(server, mock)
    go handler.Handle(ctx)

    reader := bufio.NewReader(client)

    // Skip banner
    _, _ = reader.ReadString('\n')
    _, _ = reader.ReadString('\n')

    // Try say before connecting
    fmt.Fprintln(client, "say hello")
    line, _ := reader.ReadString('\n')
    assert.Contains(t, line, "must connect first")
}
```

- [ ] **Run tests**

```bash
task test -- -run TestGatewayHandler -v ./internal/telnet/...
```

Expected: PASS

- [ ] **Commit**

```bash
jj --no-pager new -m "test(telnet): add unit tests for GatewayHandler say/quit/auth-guard"
```

---

## Chunk 3: Wire Authenticator + Gateway Integration

### Task 5: Wire GuestAuthenticator into Core Process

**Files:**

- Modify: `cmd/holomush/core.go:331-342`

The core process creates `NewCoreServer` without an authenticator. We need to create a `GuestAuthenticator` and pass it via `WithAuthenticator`.

**Known limitation:** The start location ULID is hardcoded here to match the existing test fixtures (`01HK153X0006AFVGQT61FPQX3S`). In a real deployment, this should come from world seed data or configuration. The E2E test bypasses `core.go` entirely (wires components in-process) and uses its own `startLocation`, so there is no mismatch in tests. Create a follow-up beads issue to make the start location configurable.

#### Step 5.1: Add GuestAuthenticator to core startup

- [ ] **Modify core.go**

In `cmd/holomush/core.go`, find the block (around line 331–342):

```go
sessions := core.NewSessionManager()
broadcaster := core.NewBroadcaster()
engine := core.NewEngine(realStore, sessions, broadcaster)

// Create gRPC server
creds := credentials.NewTLS(tlsConfig)
grpcServer = grpc.NewServer(grpc.Creds(creds))

// Create and register Core service
coreServer := holoGRPC.NewCoreServer(engine, sessions, broadcaster)
```

Replace the last line with:

```go
// Create guest authenticator with a well-known start location.
// The start location is created during world seeding; for now, use a
// deterministic ID that matches the seed data.
startLocationID, _ := ulid.Parse("01HK153X0006AFVGQT61FPQX3S")
guestAuth := telnet.NewGuestAuthenticator(telnet.NewGemstoneElementTheme(), startLocationID)

// Create and register Core service with guest authentication
coreServer := holoGRPC.NewCoreServer(engine, sessions, broadcaster,
    holoGRPC.WithAuthenticator(guestAuth),
)
```

Add the import for `telnet`:

```go
"github.com/holomush/holomush/internal/telnet"
```

- [ ] **Verify compilation**

```bash
task build
```

Expected: Success

- [ ] **Commit**

```bash
jj --no-pager new -m "feat(core): wire GuestAuthenticator into CoreServer startup"
```

### Task 6: Replace Gateway Placeholder with GatewayHandler

**Files:**

- Modify: `cmd/holomush/gateway.go:195-307`
- Modify: `cmd/holomush/deps.go:112-115` (already done in Task 3)

#### Step 6.1: Replace handleTelnetConnection

- [ ] **Modify gateway.go**

Replace the `handleTelnetConnection` function (line 285–307) and update `runTelnetAcceptLoop` to accept and pass the gRPC client.

The `runTelnetAcceptLoop` function currently calls `handleTelnetConnection(conn)`. Change it to create a `GatewayHandler` and call `Handle`. This requires passing the `GRPCClient` to the accept loop.

Change `runTelnetAcceptLoop` signature from:

```go
func runTelnetAcceptLoop(ctx context.Context, listener net.Listener, cancel func())
```

To:

```go
func runTelnetAcceptLoop(ctx context.Context, listener net.Listener, client GRPCClient, cancel func())
```

Update the call site (line 240) to pass `grpcClient`:

```go
go runTelnetAcceptLoop(ctx, telnetListener, grpcClient, cancel)
```

Inside the accept loop, replace `go handleTelnetConnection(conn)` with:

```go
handler := telnet.NewGatewayHandler(conn, client)
go handler.Handle(ctx)
```

Remove the `handleTelnetConnection` function entirely.

Add the import for `telnet`:

```go
"github.com/holomush/holomush/internal/telnet"
```

- [ ] **Verify compilation**

```bash
task build
```

Expected: Success

- [ ] **Run existing tests**

```bash
task test -- -v ./cmd/holomush/...
```

Expected: PASS (gateway tests use mocks)

- [ ] **Commit**

```bash
jj --no-pager new -m "feat(gateway): replace placeholder telnet handler with GatewayHandler"
```

---

## Chunk 4: E2E Integration Test

### Task 7: E2E Test Infrastructure

**Files:**

- Create: `test/integration/telnet/telnet_suite_test.go`
- Create: `test/integration/telnet/e2e_test.go`

This test mirrors the infrastructure from `test/integration/phase1_5_test.go` but adds a gateway process with a telnet listener.

#### Step 7.1: Create the Ginkgo suite bootstrap

- [ ] **Write suite file**

In `test/integration/telnet/telnet_suite_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package telnet_test

import (
    "testing"

    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

func TestTelnetE2E(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Telnet E2E Suite")
}
```

- [ ] **Verify it compiles**

```bash
go test -tags=integration -c ./test/integration/telnet/ -o /dev/null
```

Expected: Success (compiles, no tests run)

- [ ] **Commit**

```bash
jj --no-pager new -m "test(e2e): add Ginkgo suite bootstrap for telnet E2E tests"
```

#### Step 7.2: Write the test helper — testTelnetClient

- [ ] **Write helper in e2e_test.go**

In `test/integration/telnet/e2e_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package telnet_test

import (
    "bufio"
    "context"
    "fmt"
    "net"
    "os"
    "path/filepath"
    "strings"
    "time"

    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega"    //nolint:revive // gomega convention
    "github.com/oklog/ulid/v2"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials"

    "github.com/holomush/holomush/internal/core"
    grpcpkg "github.com/holomush/holomush/internal/grpc"
    "github.com/holomush/holomush/internal/store"
    "github.com/holomush/holomush/internal/telnet"
    tlscerts "github.com/holomush/holomush/internal/tls"
    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// testTelnetClient wraps a raw TCP connection for test assertions.
type testTelnetClient struct {
    conn    net.Conn
    scanner *bufio.Scanner
    writer  *bufio.Writer
}

func newTestTelnetClient(addr string) (*testTelnetClient, error) {
    conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
    if err != nil {
        return nil, err
    }
    return &testTelnetClient{
        conn:    conn,
        scanner: bufio.NewScanner(conn),
        writer:  bufio.NewWriter(conn),
    }, nil
}

func (c *testTelnetClient) SendLine(line string) {
    _, err := fmt.Fprintln(c.writer, line)
    Expect(err).NotTo(HaveOccurred())
    err = c.writer.Flush()
    Expect(err).NotTo(HaveOccurred())
}

func (c *testTelnetClient) ReadLine() string {
    c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
    if c.scanner.Scan() {
        return c.scanner.Text()
    }
    if err := c.scanner.Err(); err != nil {
        Fail(fmt.Sprintf("ReadLine error: %v", err))
    }
    Fail("ReadLine: no more lines")
    return ""
}

func (c *testTelnetClient) ReadUntil(pattern string, timeout time.Duration) string {
    deadline := time.Now().Add(timeout)
    var lines []string
    for time.Now().Before(deadline) {
        c.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
        if c.scanner.Scan() {
            line := c.scanner.Text()
            lines = append(lines, line)
            if strings.Contains(line, pattern) {
                return line
            }
        }
    }
    Fail(fmt.Sprintf("ReadUntil(%q) timed out after %v. Lines read: %v", pattern, timeout, lines))
    return ""
}

func (c *testTelnetClient) Close() {
    c.conn.Close()
}
```

- [ ] **Verify it compiles**

```bash
go test -tags=integration -c ./test/integration/telnet/ -o /dev/null
```

Expected: Success

- [ ] **Commit**

```bash
jj --no-pager new -m "test(e2e): add testTelnetClient helper for E2E tests"
```

#### Step 7.3: Write the E2E test infrastructure setup

- [ ] **Write BeforeSuite and AfterSuite**

Append to `test/integration/telnet/e2e_test.go`:

```go
var (
    testCtx       context.Context
    testCancel    context.CancelFunc
    pgContainer   testcontainers.Container
    eventStore    *store.PostgresEventStore
    grpcServer    *grpc.Server
    grpcListener  net.Listener
    telnetAddr    string
    startLocation ulid.ULID
)

var _ = BeforeSuite(func() {
    testCtx, testCancel = context.WithTimeout(context.Background(), 2*time.Minute)

    // 1. Start PostgreSQL container
    container, err := postgres.Run(testCtx,
        "postgres:18-alpine",
        postgres.WithDatabase("holomush_e2e"),
        postgres.WithUsername("holomush"),
        postgres.WithPassword("holomush"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2).
                WithStartupTimeout(30*time.Second),
        ),
    )
    Expect(err).NotTo(HaveOccurred())
    pgContainer = container

    connStr, err := container.ConnectionString(testCtx, "sslmode=disable")
    Expect(err).NotTo(HaveOccurred())

    // Run migrations
    migrator, err := store.NewMigrator(connStr)
    Expect(err).NotTo(HaveOccurred())
    Expect(migrator.Up()).To(Succeed())
    migrator.Close()

    // Create event store
    eventStore, err = store.NewPostgresEventStore(testCtx, connStr)
    Expect(err).NotTo(HaveOccurred())

    gameID, err := eventStore.InitGameID(testCtx)
    Expect(err).NotTo(HaveOccurred())

    // 2. Generate TLS certs
    tmpDir, err := os.MkdirTemp("", "holomush-e2e-*")
    Expect(err).NotTo(HaveOccurred())
    certsDir := filepath.Join(tmpDir, "certs")
    Expect(os.MkdirAll(certsDir, 0o700)).To(Succeed())

    ca, err := tlscerts.GenerateCA(gameID)
    Expect(err).NotTo(HaveOccurred())
    serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "localhost")
    Expect(err).NotTo(HaveOccurred())
    clientCert, err := tlscerts.GenerateClientCert(ca, "gateway")
    Expect(err).NotTo(HaveOccurred())
    Expect(tlscerts.SaveCertificates(certsDir, ca, serverCert)).To(Succeed())
    Expect(tlscerts.SaveClientCert(certsDir, clientCert)).To(Succeed())

    serverTLS, err := tlscerts.LoadServerTLS(certsDir, "localhost")
    Expect(err).NotTo(HaveOccurred())
    clientTLS, err := tlscerts.LoadClientTLS(certsDir, "gateway", gameID)
    Expect(err).NotTo(HaveOccurred())

    // 3. Start core (gRPC server) in-process
    sessions := core.NewSessionManager()
    broadcaster := core.NewBroadcaster()
    engine := core.NewEngine(eventStore, sessions, broadcaster)

    // Note: startLocation is a random ULID. The engine does not validate
    // location existence — it just appends events to "location:<id>". This
    // means we don't need to seed the world store for the E2E test to work.
    // A follow-up task should add world seeding for a more realistic test.
    startLocation = ulid.Make()
    guestAuth := telnet.NewGuestAuthenticator(telnet.NewGemstoneElementTheme(), startLocation)

    coreServer := grpcpkg.NewCoreServer(engine, sessions, broadcaster,
        grpcpkg.WithAuthenticator(guestAuth),
    )

    creds := credentials.NewTLS(serverTLS)
    grpcServer = grpc.NewServer(grpc.Creds(creds))
    corev1.RegisterCoreServer(grpcServer, coreServer)

    grpcListener, err = net.Listen("tcp", "localhost:0")
    Expect(err).NotTo(HaveOccurred())

    go func() {
        if err := grpcServer.Serve(grpcListener); err != nil {
            GinkgoWriter.Printf("gRPC server error: %v\n", err)
        }
    }()

    // 4. Start gateway (telnet listener) in-process
    grpcClient, err := grpcpkg.NewClient(testCtx, grpcpkg.ClientConfig{
        Address:   grpcListener.Addr().String(),
        TLSConfig: clientTLS,
    })
    Expect(err).NotTo(HaveOccurred())

    telnetListener, err := net.Listen("tcp", "localhost:0")
    Expect(err).NotTo(HaveOccurred())
    telnetAddr = telnetListener.Addr().String()

    // Accept loop
    go func() {
        for {
            conn, err := telnetListener.Accept()
            if err != nil {
                if testCtx.Err() != nil {
                    return
                }
                continue
            }
            handler := telnet.NewGatewayHandler(conn, grpcClient)
            go handler.Handle(testCtx)
        }
    }()

    GinkgoWriter.Printf("E2E infrastructure ready: telnet=%s grpc=%s\n", telnetAddr, grpcListener.Addr().String())
})

var _ = AfterSuite(func() {
    if grpcServer != nil {
        grpcServer.GracefulStop()
    }
    if eventStore != nil {
        eventStore.Close()
    }
    if pgContainer != nil {
        pgContainer.Terminate(context.Background())
    }
    if testCancel != nil {
        testCancel()
    }
})
```

- [ ] **Verify it compiles**

```bash
go test -tags=integration -c ./test/integration/telnet/ -o /dev/null
```

Expected: Success

- [ ] **Commit**

```bash
jj --no-pager new -m "test(e2e): add BeforeSuite/AfterSuite infrastructure for telnet E2E"
```

#### Step 7.4: Write BDD scenarios

- [ ] **Write the E2E test scenarios**

Append to `test/integration/telnet/e2e_test.go`:

```go
var _ = Describe("Telnet Vertical Slice E2E", func() {

    Describe("Authentication", func() {
        It("Player A connects as guest and receives a themed name", func() {
            client, err := newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())
            defer client.Close()

            // Read banner
            welcome := client.ReadLine()
            Expect(welcome).To(ContainSubstring("Welcome to HoloMUSH"))
            _ = client.ReadLine() // usage line

            // Connect as guest
            client.SendLine("connect guest")

            // Should receive welcome with themed name
            line := client.ReadLine()
            Expect(line).To(MatchRegexp(`Welcome, [A-Z][a-z]+_[A-Z][a-z]+!`))
        })

        It("rejects registered login with helpful message", func() {
            client, err := newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())
            defer client.Close()

            _ = client.ReadLine() // banner
            _ = client.ReadLine() // usage

            client.SendLine("connect player1 password")

            line := client.ReadLine()
            Expect(line).To(ContainSubstring("not yet available"))
        })
    })

    Describe("Say Communication", func() {
        var clientA, clientB *testTelnetClient
        var nameA, nameB string

        BeforeEach(func() {
            var err error
            clientA, err = newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())

            clientB, err = newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())

            // Connect both
            _ = clientA.ReadLine() // banner
            _ = clientA.ReadLine() // usage
            clientA.SendLine("connect guest")
            welcomeA := clientA.ReadLine()
            nameA = extractName(welcomeA)

            _ = clientB.ReadLine()
            _ = clientB.ReadLine()
            clientB.SendLine("connect guest")
            welcomeB := clientB.ReadLine()
            nameB = extractName(welcomeB)

            Expect(nameA).NotTo(Equal(nameB), "guests should get unique names")
        })

        AfterEach(func() {
            if clientA != nil {
                clientA.Close()
            }
            if clientB != nil {
                clientB.Close()
            }
        })

        It("Player A says something and sees their own echo", func() {
            clientA.SendLine("say Hello, world!")
            line := clientA.ReadLine()
            Expect(line).To(ContainSubstring(`You say, "Hello, world!"`))
        })

        It("Player B receives Player A's say via event stream", func() {
            clientA.SendLine("say Can you hear me?")
            _ = clientA.ReadLine() // echo to A

            // B should receive the broadcast
            line := clientB.ReadUntil("says,", 5*time.Second)
            Expect(line).To(ContainSubstring(`"Can you hear me?"`))
        })

        It("Player A receives Player B's say via event stream", func() {
            clientB.SendLine("say Greetings from B!")
            _ = clientB.ReadLine() // echo to B

            line := clientA.ReadUntil("says,", 5*time.Second)
            Expect(line).To(ContainSubstring(`"Greetings from B!"`))
        })
    })

    Describe("Pose Communication", func() {
        var clientA, clientB *testTelnetClient

        BeforeEach(func() {
            var err error
            clientA, err = newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())
            clientB, err = newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())

            // Connect both
            _ = clientA.ReadLine()
            _ = clientA.ReadLine()
            clientA.SendLine("connect guest")
            _ = clientA.ReadLine()

            _ = clientB.ReadLine()
            _ = clientB.ReadLine()
            clientB.SendLine("connect guest")
            _ = clientB.ReadLine()
        })

        AfterEach(func() {
            if clientA != nil {
                clientA.Close()
            }
            if clientB != nil {
                clientB.Close()
            }
        })

        It("Player A poses and sees their own echo", func() {
            clientA.SendLine("pose waves enthusiastically")
            line := clientA.ReadLine()
            Expect(line).To(ContainSubstring("waves enthusiastically"))
        })

        It("Player B receives Player A's pose via event stream", func() {
            clientA.SendLine("pose nods thoughtfully")
            _ = clientA.ReadLine() // echo to A

            line := clientB.ReadUntil("nods thoughtfully", 5*time.Second)
            Expect(line).To(ContainSubstring("nods thoughtfully"))
        })
    })

    Describe("Disconnect", func() {
        It("Player A disconnects cleanly via quit", func() {
            client, err := newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())
            defer client.Close()

            _ = client.ReadLine()
            _ = client.ReadLine()
            client.SendLine("connect guest")
            _ = client.ReadLine()

            client.SendLine("quit")
            line := client.ReadLine()
            Expect(line).To(ContainSubstring("Goodbye"))
        })

        It("Player B continues receiving events after A disconnects", func() {
            cA, err := newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())
            defer cA.Close()
            cB, err := newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())
            defer cB.Close()

            // Connect both
            _ = cA.ReadLine(); _ = cA.ReadLine()
            cA.SendLine("connect guest"); _ = cA.ReadLine()
            _ = cB.ReadLine(); _ = cB.ReadLine()
            cB.SendLine("connect guest"); _ = cB.ReadLine()

            // A says something, B receives it
            cA.SendLine("say before disconnect")
            _ = cA.ReadLine() // echo
            cB.ReadUntil("before disconnect", 5*time.Second)

            // A disconnects
            cA.SendLine("quit")
            _ = cA.ReadLine() // Goodbye
            // Give the disconnect a moment to propagate
            time.Sleep(200 * time.Millisecond)

            // Connect a new player C and verify B still works
            cC, err := newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())
            defer cC.Close()
            _ = cC.ReadLine(); _ = cC.ReadLine()
            cC.SendLine("connect guest"); _ = cC.ReadLine()

            cC.SendLine("say hello from C")
            _ = cC.ReadLine() // echo
            line := cB.ReadUntil("hello from C", 5*time.Second)
            Expect(line).To(ContainSubstring("hello from C"))
        })
    })

    Describe("Event Persistence", func() {
        It("events are persisted to PostgreSQL after say commands", func() {
            client, err := newTestTelnetClient(telnetAddr)
            Expect(err).NotTo(HaveOccurred())
            defer client.Close()

            _ = client.ReadLine()
            _ = client.ReadLine()
            client.SendLine("connect guest")
            _ = client.ReadLine()

            client.SendLine("say persistence check")
            _ = client.ReadLine() // echo

            // Give event time to persist
            time.Sleep(500 * time.Millisecond)

            // Verify event exists in PostgreSQL via event store
            // Replay events from the location stream — should contain our say event
            events, err := eventStore.Replay(testCtx, "location:"+startLocation.String(), ulid.ULID{}, 100)
            Expect(err).NotTo(HaveOccurred())
            Expect(len(events)).To(BeNumerically(">", 0), "expected at least one event persisted")

            // Find our specific event
            found := false
            for _, e := range events {
                if string(e.Type) == "say" {
                    found = true
                    break
                }
            }
            Expect(found).To(BeTrue(), "expected a 'say' event in the store")
        })
    })
})

// extractName pulls "Word_Word" from "Welcome, Word_Word!"
func extractName(welcome string) string {
    // Format: "Welcome, Name!"
    start := strings.Index(welcome, ", ")
    if start == -1 {
        return ""
    }
    end := strings.Index(welcome, "!")
    if end == -1 {
        return welcome[start+2:]
    }
    return welcome[start+2 : end]
}
```

- [ ] **Run the E2E tests**

```bash
go test -race -v -tags=integration -timeout=120s ./test/integration/telnet/...
```

Expected: All PASS. If any fail, debug using the pattern output.

- [ ] **Commit**

```bash
jj --no-pager new -m "test(e2e): add telnet vertical slice BDD scenarios — auth, say, pose, disconnect"
```

---

## Chunk 5: Final Wiring + Quality Gates

### Task 8: Run Full Quality Gates

- [ ] **Run linter**

```bash
task lint
```

Fix any issues.

- [ ] **Run all unit tests**

```bash
task test
```

All PASS.

- [ ] **Run integration tests**

```bash
go test -race -v -tags=integration -timeout=120s ./test/integration/telnet/...
```

All PASS.

- [ ] **Run coverage check**

```bash
task test:coverage
```

Verify `internal/telnet` package is >80%.

- [ ] **Commit any fixes**

```bash
jj --no-pager new -m "chore: fix lint/test issues from telnet E2E vertical slice"
```

### Task 9: Code Review

- [ ] **Run code review**

Invoke `pr-review-toolkit:review-pr` to run comprehensive review.

- [ ] **Address all findings**

Fix any issues identified by the review agents. Re-run `task test` and `task lint` after fixes.

- [ ] **Commit review fixes**

```bash
jj --no-pager new -m "fix: address review findings from telnet E2E vertical slice"
```

### Task 10: Close Epic and Create PR

- [ ] **Close epic**

```bash
bd close holomush-hbkx --reason "Telnet E2E vertical slice complete — guest auth, gateway handler, E2E tests"
```

- [ ] **Create follow-up issues**

Create beads for known limitations:

1. Broadcast events show truncated actor ID instead of character name
2. Hardcoded start location ULID in `core.go` should be configurable
3. E2E test should seed world data for a more realistic test

- [ ] **Create PR**

Use the `finishing-a-development-branch` skill to merge/PR the work.

---

## Post-Implementation Checklist

- [ ] All unit tests pass (`task test`)
- [ ] All integration tests pass (`go test -tags=integration ./test/integration/telnet/...`)
- [ ] Linter clean (`task lint`)
- [ ] Formatter clean (`task fmt`)
- [ ] Coverage >80% for `internal/telnet`
- [ ] License headers on all new files
- [ ] No TODO items left in new code (except intentional follow-up items tracked in beads)
- [ ] Code review passed (`pr-review-toolkit:review-pr`)
- [ ] All review findings addressed
- [ ] Follow-up beads created for known limitations
- [ ] PR created and review requested

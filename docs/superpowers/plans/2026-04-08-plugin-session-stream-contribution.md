# Plugin Session Stream Contribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a general mechanism for plugins to contribute session stream subscriptions at login time and modify them mid-session, enabling channels (and future plugins) to auto-subscribe characters on connect.

**Architecture:** `QuerySessionStreams` RPC on `PluginService` collects streams from opted-in plugins before LISTEN setup (preserving LISTEN-before-replay invariant). `AddSessionStream`/`RemoveSessionStream` on `HostFunctionsService` write to a per-session control channel, handled inline by the Subscribe goroutine.

**Tech Stack:** Go, protobuf/gRPC, gopher-lua, HashiCorp go-plugin, testify, Ginkgo/Gomega

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `api/proto/holomush/plugin/v1/plugin.proto` | Modify | Add `QuerySessionStreams` RPC + messages |
| `api/proto/holomush/plugin/v1/hostfunc.proto` | Modify | Add `AddSessionStream`/`RemoveSessionStream` |
| `internal/plugin/host.go` | Modify | Add `SessionStreamsRequest`, `StreamRegistry`, `QuerySessionStreams` to `Host` |
| `internal/plugin/manifest.go` | Modify | Add `SessionStreams bool` field + validation |
| `internal/plugin/manifest_test.go` | Modify | Validation tests for `session_streams` |
| `internal/plugin/manager.go` | Modify | Add `QuerySessionStreams` fan-out method |
| `internal/plugin/manager_test.go` | Modify | Fan-out unit tests |
| `internal/plugin/mocks/mock_Host.go` | Regenerate | Updated interface mock |
| `internal/plugin/lua/host.go` | Modify | Implement `QuerySessionStreams` via `on_session_subscribe` Lua callback |
| `internal/plugin/lua/host_test.go` | Modify | Lua host QuerySessionStreams tests |
| `internal/plugin/hostfunc/functions.go` | Modify | Add `streamRegistry StreamRegistry` field + `WithStreamRegistry` option |
| `internal/plugin/hostfunc/stdlib_streams.go` | Create | `RegisterStreamFuncs` — `holo.add_session_stream` / `holo.remove_session_stream` |
| `internal/plugin/hostfunc/stdlib_streams_test.go` | Create | Hostfunc stream tests |
| `internal/plugin/goplugin/host.go` | Modify | Implement `QuerySessionStreams` via gRPC |
| `internal/plugin/goplugin/host_test.go` | Modify | Binary host QuerySessionStreams tests |
| `internal/plugin/goplugin/host_service.go` | Modify | Implement `AddSessionStream`/`RemoveSessionStream` for binary plugins |
| `internal/plugin/goplugin/host_service_test.go` | Modify | Binary host service stream tests |
| `internal/grpc/stream_registry.go` | Create | `SessionStreamRegistry` — maps session IDs to control channels |
| `internal/grpc/stream_registry_test.go` | Create | Registry unit tests |
| `internal/grpc/server.go` | Modify | Subscribe: collect plugin streams, ctrlCh, per-stream cancels, afterLISTENHook |
| `internal/grpc/server_test.go` | Modify | Subscribe unit tests for plugin stream integration |
| `test/integration/plugin/` | Create (dir) | New integration test package for plugin E2E |
| `test/integration/plugin/session_streams_suite_test.go` | Create | Ginkgo suite registration |
| `test/integration/plugin/session_streams_integration_test.go` | Create | UC1, UC2, invariant E2E tests |
| `cmd/holomush/` (setup file) | Modify | Wire `SessionStreamRegistry` + `streamContributor` into server + hostfunc |

---

### Task 1: Proto Changes

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto`
- Modify: `api/proto/holomush/plugin/v1/hostfunc.proto`

> Proto changes have no tests. Run `task proto` and `task build` to verify generation and compilation.

- [ ] **Step 1: Add `QuerySessionStreams` to `plugin.proto`**

In `api/proto/holomush/plugin/v1/plugin.proto`, after the `HandleCommand` RPC, add:

```protobuf
  // QuerySessionStreams returns stream names the plugin wants subscribed for a session.
  // Called once at session establishment, before LISTEN setup.
  // Only invoked for plugins that declare session_streams: true in their manifest.
  rpc QuerySessionStreams(QuerySessionStreamsRequest) returns (QuerySessionStreamsResponse);
```

After the existing message definitions, add:

```protobuf
// QuerySessionStreamsRequest provides session context for stream contribution.
message QuerySessionStreamsRequest {
  // Identifier of the character entering the session.
  string character_id = 1;
  // Identifier of the player owning the character.
  string player_id = 2;
  // Session identifier.
  string session_id = 3;
}

// QuerySessionStreamsResponse returns stream names and an optional error.
message QuerySessionStreamsResponse {
  // Stream names the plugin wants added to this session's subscription.
  repeated string streams = 1;
  // Non-empty indicates a plugin-reported error. Host degrades (logs + skips).
  string error = 2;
}
```

- [ ] **Step 2: Add `AddSessionStream` and `RemoveSessionStream` to `hostfunc.proto`**

In `api/proto/holomush/plugin/v1/hostfunc.proto`, after the `GetCommandHelp` RPC, add:

```protobuf
  // AddSessionStream subscribes an active session to an additional stream mid-session.
  // Returns SESSION_NOT_FOUND (codes.NotFound) if session_id is not active.
  rpc AddSessionStream(AddSessionStreamRequest) returns (AddSessionStreamResponse);

  // RemoveSessionStream unsubscribes an active session from a stream.
  // Idempotent: returns success if stream is not subscribed.
  rpc RemoveSessionStream(RemoveSessionStreamRequest) returns (RemoveSessionStreamResponse);
```

After the existing message definitions, add:

```protobuf
// AddSessionStreamRequest specifies which session and stream to add.
message AddSessionStreamRequest {
  // Active session identifier.
  string session_id = 1;
  // Stream name to subscribe to (format: "prefix:id").
  string stream = 2;
}

// AddSessionStreamResponse indicates success or failure.
message AddSessionStreamResponse {
  // Whether the stream was successfully added.
  bool success = 1;
  // Non-empty on error.
  string error = 2;
}

// RemoveSessionStreamRequest specifies which stream to remove.
message RemoveSessionStreamRequest {
  // Active session identifier.
  string session_id = 1;
  // Stream name to unsubscribe from.
  string stream = 2;
}

// RemoveSessionStreamResponse indicates success or failure.
message RemoveSessionStreamResponse {
  // Whether the stream was successfully removed (true even if not subscribed).
  bool success = 1;
  // Non-empty on error.
  string error = 2;
}
```

- [ ] **Step 3: Regenerate proto code**

```bash
task proto
```

Expected: no errors. Generated files updated in `pkg/proto/holomush/plugin/v1/`.

- [ ] **Step 4: Verify compilation**

```bash
task build
```

Expected: successful build. If `UnimplementedPluginServiceServer` or `UnimplementedHostFunctionsServiceServer` fail, add stub implementations in the relevant host files (goplugin/host_service.go, etc.) — the proto generator adds unimplemented stubs to the embedded types automatically.

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(proto): add QuerySessionStreams, AddSessionStream, RemoveSessionStream RPCs"
```

---

### Task 2: Manifest `session_streams` Field

**Files:**

- Modify: `internal/plugin/manifest.go`
- Modify: `internal/plugin/manifest_test.go`

- [ ] **Step 1: Write failing tests**

In `internal/plugin/manifest_test.go`, add a new test group. Find the existing test function structure and add:

```go
func TestManifestSessionStreamsValidationAcceptsLuaPlugin(t *testing.T) {
    data := []byte(`
name: my-plugin
version: 1.0.0
type: lua
session_streams: true
lua-plugin:
  entry: main.lua
`)
    m, err := ParseManifest(data)
    require.NoError(t, err)
    assert.True(t, m.SessionStreams)
}

func TestManifestSessionStreamsValidationAcceptsBinaryPlugin(t *testing.T) {
    data := []byte(`
name: my-plugin
version: 1.0.0
type: binary
session_streams: true
binary-plugin:
  executable: plugin
`)
    m, err := ParseManifest(data)
    require.NoError(t, err)
    assert.True(t, m.SessionStreams)
}

func TestManifestSessionStreamsValidationRejectsCorePlugin(t *testing.T) {
    data := []byte(`
name: my-plugin
version: 1.0.0
type: core
session_streams: true
commands:
  - name: mycmd
    help: "does stuff"
`)
    _, err := ParseManifest(data)
    require.Error(t, err)
    assert.Contains(t, err.Error(), "session_streams")
}

func TestManifestSessionStreamsValidationRejectsSettingPlugin(t *testing.T) {
    data := []byte(`
name: my-plugin
version: 1.0.0
type: setting
session_streams: true
setting:
  display_name: My World
  content_dir: content
  starting_location: start
`)
    _, err := ParseManifest(data)
    require.Error(t, err)
    assert.Contains(t, err.Error(), "session_streams")
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
task test -- -run TestManifestSessionStreams ./internal/plugin/
```

Expected: compile error (field doesn't exist yet) or test failures.

- [ ] **Step 3: Add `SessionStreams` field to `Manifest`**

In `internal/plugin/manifest.go`, add the field after `Priority`:

```go
// SessionStreams indicates the plugin implements QuerySessionStreams and contributes
// streams to session subscriptions. Only valid for lua and binary plugin types.
SessionStreams bool `yaml:"session_streams,omitempty" json:"session_streams,omitempty" jsonschema:"description=Plugin contributes streams to session subscriptions via QuerySessionStreams"`
```

- [ ] **Step 4: Add validation rule**

In `manifest.go`'s `Validate()` method, add after the existing type-specific validations (before the load priority check):

```go
// Validate session_streams: only lua and binary plugins can contribute session streams.
if m.SessionStreams && m.Type != TypeLua && m.Type != TypeBinary {
    return oops.In("manifest").With("name", m.Name).With("type", m.Type).
        New("session_streams is only valid for lua and binary plugin types")
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
task test -- -run TestManifestSessionStreams ./internal/plugin/
```

Expected: all 4 tests PASS.

- [ ] **Step 6: Update JSON schema**

```bash
task generate:schema
```

Expected: `internal/plugin/schema.json` updated with `session_streams` field.

- [ ] **Step 7: Commit**

```bash
jj commit -m "feat(plugin): add session_streams manifest field with lua/binary-only validation"
```

---

### Task 3: `Host` Interface Extension + `StreamRegistry` Interface

**Files:**

- Modify: `internal/plugin/host.go`
- Regenerate: `internal/plugin/mocks/mock_Host.go`

- [ ] **Step 1: Add `SessionStreamsRequest` and `StreamRegistry` to `host.go`**

In `internal/plugin/host.go`, add after the existing content:

```go
// SessionStreamsRequest carries session context for plugin stream contribution queries.
type SessionStreamsRequest struct {
    // CharacterID is the character entering the session.
    CharacterID string
    // PlayerID is the player owning the character.
    PlayerID string
    // SessionID is the active session identifier.
    SessionID string
}

// StreamRegistry allows plugins to modify session stream subscriptions mid-session.
type StreamRegistry interface {
    // AddStream subscribes a session to an additional stream.
    // Returns an error (code SESSION_NOT_FOUND) if the session is not active.
    AddStream(ctx context.Context, sessionID, stream string) error
    // RemoveStream unsubscribes a session from a stream. Idempotent.
    RemoveStream(ctx context.Context, sessionID, stream string) error
}
```

The file currently imports only `context` and `pluginsdk`. Add `"context"` if not already present (it should be).

- [ ] **Step 2: Add `QuerySessionStreams` to `Host` interface**

In `internal/plugin/host.go`, add to the `Host` interface after `DeliverCommand`:

```go
// QuerySessionStreams returns stream names the plugin wants subscribed for a session.
// Only called for plugins with SessionStreams: true in their manifest.
// Returns nil if the plugin has no streams to contribute.
QuerySessionStreams(ctx context.Context, name string, req SessionStreamsRequest) ([]string, error)
```

- [ ] **Step 3: Regenerate mocks**

```bash
task mocks:generate
```

Expected: `internal/plugin/mocks/mock_Host.go` updated with `QuerySessionStreams` method.

- [ ] **Step 4: Verify build**

```bash
task build
```

Expected: compile errors in `internal/plugin/lua/host.go` and `internal/plugin/goplugin/host.go` because they don't implement the new method yet. Note these errors — they'll be fixed in Tasks 6 and 8.

- [ ] **Step 5: Add stub implementations to fix build**

In `internal/plugin/lua/host.go`, add a temporary stub:

```go
// QuerySessionStreams calls the plugin's on_session_subscribe handler if defined.
// Returns nil if the function is not defined in the plugin.
// TODO: implement in Task 6.
func (h *Host) QuerySessionStreams(_ context.Context, name string, _ plugins.SessionStreamsRequest) ([]string, error) {
    return nil, nil
}
```

In `internal/plugin/goplugin/host.go`, add a temporary stub:

```go
// QuerySessionStreams calls the plugin's QuerySessionStreams RPC.
// TODO: implement in Task 8.
func (h *Host) QuerySessionStreams(_ context.Context, name string, _ plugins.SessionStreamsRequest) ([]string, error) {
    return nil, nil
}
```

- [ ] **Step 6: Verify build passes**

```bash
task build
```

Expected: successful build (stubs satisfy interface).

- [ ] **Step 7: Commit**

```bash
jj commit -m "feat(plugin): add SessionStreamsRequest, StreamRegistry, QuerySessionStreams to Host interface"
```

---

### Task 4: `Manager.QuerySessionStreams` Fan-Out

**Files:**

- Modify: `internal/plugin/manager.go`
- Modify: `internal/plugin/manager_test.go`

- [ ] **Step 1: Write failing tests**

In `internal/plugin/manager_test.go`, add (imports: `"sync/atomic"`, `mocks` package):

```go
func TestManagerQuerySessionStreamsReturnsNilWhenNoOptedInPlugins(t *testing.T) {
    m := newTestManager(t)
    result := m.QuerySessionStreams(context.Background(), plugins.SessionStreamsRequest{
        CharacterID: "char-1",
        PlayerID:    "player-1",
        SessionID:   "sess-1",
    })
    assert.Nil(t, result)
}

func TestManagerQuerySessionStreamsMergesContributionsFromMultiplePlugins(t *testing.T) {
    m := newTestManager(t)

    hostA := mocks.NewMockHost(t)
    hostA.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
    hostA.EXPECT().QuerySessionStreams(mock.Anything, "plugin-a", mock.Anything).
        Return([]string{"channel:abc", "channel:shared"}, nil)
    m.RegisterHost(plugins.TypeLua, hostA)

    hostB := mocks.NewMockHost(t)
    hostB.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
    hostB.EXPECT().QuerySessionStreams(mock.Anything, "plugin-b", mock.Anything).
        Return([]string{"channel:shared", "channel:def"}, nil) // channel:shared is a duplicate
    m.RegisterHost(plugins.TypeBinary, hostB)

    loadPlugin(t, m, "plugin-a", plugins.TypeLua, true)  // session_streams: true
    loadPlugin(t, m, "plugin-b", plugins.TypeBinary, true)

    result := m.QuerySessionStreams(context.Background(), plugins.SessionStreamsRequest{
        CharacterID: "char-1", PlayerID: "player-1", SessionID: "sess-1",
    })

    assert.ElementsMatch(t, []string{"channel:abc", "channel:shared", "channel:def"}, result)
}

func TestManagerQuerySessionStreamsDegradeOnSinglePluginError(t *testing.T) {
    m := newTestManager(t)

    hostA := mocks.NewMockHost(t)
    hostA.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
    hostA.EXPECT().QuerySessionStreams(mock.Anything, "plugin-a", mock.Anything).
        Return(nil, errors.New("db unavailable"))
    m.RegisterHost(plugins.TypeLua, hostA)

    hostB := mocks.NewMockHost(t)
    hostB.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
    hostB.EXPECT().QuerySessionStreams(mock.Anything, "plugin-b", mock.Anything).
        Return([]string{"channel:abc"}, nil)
    m.RegisterHost(plugins.TypeBinary, hostB)

    loadPlugin(t, m, "plugin-a", plugins.TypeLua, true)
    loadPlugin(t, m, "plugin-b", plugins.TypeBinary, true)

    result := m.QuerySessionStreams(context.Background(), plugins.SessionStreamsRequest{
        CharacterID: "char-1", PlayerID: "player-1", SessionID: "sess-1",
    })

    assert.Equal(t, []string{"channel:abc"}, result)
}

func TestManagerQuerySessionStreamsSkipsOptedOutPlugins(t *testing.T) {
    m := newTestManager(t)

    host := mocks.NewMockHost(t)
    host.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
    // QuerySessionStreams must NOT be called on opted-out plugin
    m.RegisterHost(plugins.TypeLua, host)
    loadPlugin(t, m, "plugin-a", plugins.TypeLua, false) // session_streams: false

    result := m.QuerySessionStreams(context.Background(), plugins.SessionStreamsRequest{
        CharacterID: "char-1", PlayerID: "player-1", SessionID: "sess-1",
    })
    assert.Nil(t, result)
    // testify/mock will fail the test if QuerySessionStreams was called unexpectedly
}

func TestManagerQuerySessionStreamsDropsInvalidStreamNames(t *testing.T) {
    m := newTestManager(t)
    host := mocks.NewMockHost(t)
    host.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil)
    host.EXPECT().QuerySessionStreams(mock.Anything, "plugin-a", mock.Anything).
        Return([]string{
            "",               // empty — invalid
            "nocolon",        // no colon — invalid
            "has space:abc",  // whitespace — invalid
            "channel:valid",  // valid
        }, nil)
    m.RegisterHost(plugins.TypeLua, host)
    loadPlugin(t, m, "plugin-a", plugins.TypeLua, true)

    result := m.QuerySessionStreams(context.Background(), plugins.SessionStreamsRequest{
        CharacterID: "char-1", PlayerID: "player-1", SessionID: "sess-1",
    })
    assert.Equal(t, []string{"channel:valid"}, result)
}
```

You'll need a helper. Add to the test file (or a `manager_test_helpers_test.go`):

```go
// loadPlugin registers a fake plugin manifest with the manager.
// sessionStreams controls whether Manifest.SessionStreams is true.
func loadPlugin(t *testing.T, m *plugins.Manager, name string, plugType plugins.Type, sessionStreams bool) {
    t.Helper()
    manifest := &plugins.Manifest{
        Name:          name,
        Version:       "1.0.0",
        Type:          plugType,
        SessionStreams: sessionStreams,
    }
    // For lua: set LuaPlugin
    if plugType == plugins.TypeLua {
        manifest.LuaPlugin = &plugins.LuaConfig{Entry: "main.lua"}
    }
    if plugType == plugins.TypeBinary {
        manifest.BinaryPlugin = &plugins.BinaryConfig{Executable: "plugin"}
    }
    // Inject directly into manager's internal map for testing
    // (avoids needing real plugin files)
    // Add to manager.loaded via a test-exported method or use reflect
    // Simplest: add an exported test helper on Manager:
    m.TestLoadPlugin(name, manifest)
}
```

Add `TestLoadPlugin` to `manager.go`:

```go
// TestLoadPlugin injects a plugin directly for unit testing.
// Only available in tests.
func (m *Manager) TestLoadPlugin(name string, manifest *Manifest) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.loaded[name] = &DiscoveredPlugin{Manifest: manifest}
    // pluginHosts entry needed for routing — use whichever host is registered for this type
    if host, ok := m.hosts[manifest.Type]; ok {
        m.pluginHosts[name] = host
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
task test -- -run TestManagerQuerySessionStreams ./internal/plugin/
```

Expected: compile error (`QuerySessionStreams` not defined on Manager) or failures.

- [ ] **Step 3: Implement `Manager.QuerySessionStreams`**

In `internal/plugin/manager.go`, add after existing methods. First check the `DiscoveredPlugin` struct — if it doesn't have a `Manifest` field exposed, check what field name is used (it's likely `manifest` lowercase, in which case use `dp.manifest`):

```go
// isValidStreamName returns true if name is a valid HoloMUSH stream name.
// Stream names must be non-empty, contain exactly one colon, have no whitespace,
// and be at most 256 characters long.
func isValidStreamName(name string) bool {
    if name == "" || len(name) > 256 {
        return false
    }
    for _, r := range name {
        if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
            return false
        }
    }
    colonCount := strings.Count(name, ":")
    return colonCount >= 1
}

// QuerySessionStreams collects plugin-contributed stream names for a session.
// Only plugins with SessionStreams: true in their manifest are queried.
// Plugin errors are logged and skipped (degraded-subscribe policy).
// Invalid stream names are dropped. Duplicate streams are deduplicated.
func (m *Manager) QuerySessionStreams(ctx context.Context, req SessionStreamsRequest) []string {
    m.mu.RLock()
    type pluginEntry struct {
        name string
        host Host
    }
    var opted []pluginEntry
    for name, dp := range m.loaded {
        if dp.Manifest.SessionStreams {
            if host, ok := m.pluginHosts[name]; ok {
                opted = append(opted, pluginEntry{name, host})
            }
        }
    }
    m.mu.RUnlock()

    if len(opted) == 0 {
        return nil
    }

    type result struct {
        name    string
        streams []string
        err     error
    }
    results := make(chan result, len(opted))
    for _, p := range opted {
        p := p
        go func() {
            streams, err := p.host.QuerySessionStreams(ctx, p.name, req)
            results <- result{name: p.name, streams: streams, err: err}
        }()
    }

    seen := make(map[string]bool)
    var merged []string
    for range opted {
        r := <-results
        if r.err != nil {
            slog.WarnContext(ctx, "plugin stream contribution failed — skipping",
                "plugin", r.name,
                "character_id", req.CharacterID,
                "session_id", req.SessionID,
                "error", r.err)
            continue
        }
        for _, s := range r.streams {
            if !isValidStreamName(s) {
                slog.WarnContext(ctx, "plugin returned invalid stream name — dropping",
                    "plugin", r.name,
                    "stream", s)
                continue
            }
            if !seen[s] {
                seen[s] = true
                merged = append(merged, s)
            }
        }
    }
    return merged
}
```

Add `"strings"` to imports if not present.

Check `DiscoveredPlugin` type — look for how loaded plugins store their manifest (in `manager.go` or `bootstrap.go`). The field is likely `Manifest *Manifest`. If it's lowercase `manifest`, update the field access accordingly.

- [ ] **Step 4: Run tests to verify they pass**

```bash
task test -- -run TestManagerQuerySessionStreams ./internal/plugin/
```

Expected: all 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(plugin): Manager.QuerySessionStreams fan-out with degraded-subscribe policy"
```

---

### Task 5: `SessionStreamRegistry`

**Files:**

- Create: `internal/grpc/stream_registry.go`
- Create: `internal/grpc/stream_registry_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/grpc/stream_registry_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestSessionStreamRegistrySendDeliversToRegisteredSession(t *testing.T) {
    r := NewSessionStreamRegistry()
    ch := make(chan sessionStreamUpdate, 1)
    r.Register("sess-1", ch)

    err := r.Send("sess-1", sessionStreamUpdate{stream: "channel:abc", add: true})
    require.NoError(t, err)

    update := <-ch
    assert.Equal(t, "channel:abc", update.stream)
    assert.True(t, update.add)
}

func TestSessionStreamRegistrySendReturnsNotFoundForUnknownSession(t *testing.T) {
    r := NewSessionStreamRegistry()
    err := r.Send("missing", sessionStreamUpdate{stream: "channel:abc", add: true})
    require.Error(t, err)
    assert.Contains(t, err.Error(), "SESSION_NOT_FOUND")
}

func TestSessionStreamRegistrySendReturnsNotFoundAfterDeregister(t *testing.T) {
    r := NewSessionStreamRegistry()
    ch := make(chan sessionStreamUpdate, 1)
    r.Register("sess-1", ch)
    r.Deregister("sess-1")

    err := r.Send("sess-1", sessionStreamUpdate{stream: "channel:abc", add: true})
    require.Error(t, err)
    assert.Contains(t, err.Error(), "SESSION_NOT_FOUND")
}

func TestSessionStreamRegistrySendReturnsChannelFullWhenBufferExhausted(t *testing.T) {
    r := NewSessionStreamRegistry()
    ch := make(chan sessionStreamUpdate, 1) // buffer of 1
    r.Register("sess-1", ch)

    // Fill the buffer
    err := r.Send("sess-1", sessionStreamUpdate{stream: "channel:abc", add: true})
    require.NoError(t, err)

    // Second send to full buffer should return error immediately
    err = r.Send("sess-1", sessionStreamUpdate{stream: "channel:def", add: true})
    require.Error(t, err)
    assert.Contains(t, err.Error(), "CONTROL_CHANNEL_FULL")
}

func TestSessionStreamRegistryAddStreamDelegatesToSend(t *testing.T) {
    r := NewSessionStreamRegistry()
    ch := make(chan sessionStreamUpdate, 1)
    r.Register("sess-1", ch)

    err := r.AddStream(context.Background(), "sess-1", "channel:abc")
    require.NoError(t, err)
    update := <-ch
    assert.Equal(t, "channel:abc", update.stream)
    assert.True(t, update.add)
}

func TestSessionStreamRegistryRemoveStreamDelegatesToSend(t *testing.T) {
    r := NewSessionStreamRegistry()
    ch := make(chan sessionStreamUpdate, 1)
    r.Register("sess-1", ch)

    err := r.RemoveStream(context.Background(), "sess-1", "channel:abc")
    require.NoError(t, err)
    update := <-ch
    assert.Equal(t, "channel:abc", update.stream)
    assert.False(t, update.add)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
task test -- -run TestSessionStreamRegistry ./internal/grpc/
```

Expected: compile error (types not defined yet).

- [ ] **Step 3: Implement `SessionStreamRegistry`**

Create `internal/grpc/stream_registry.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
    "context"
    "sync"

    "github.com/samber/oops"
)

// sessionStreamUpdate is sent on a session's control channel to add or remove a stream.
type sessionStreamUpdate struct {
    stream string
    add    bool // true = subscribe, false = unsubscribe
}

// SessionStreamRegistry maps active session IDs to their Subscribe control channels.
// It implements plugins.StreamRegistry for use by the hostfunc layer.
type SessionStreamRegistry struct {
    mu       sync.Mutex
    channels map[string]chan<- sessionStreamUpdate
}

// NewSessionStreamRegistry creates an empty registry.
func NewSessionStreamRegistry() *SessionStreamRegistry {
    return &SessionStreamRegistry{
        channels: make(map[string]chan<- sessionStreamUpdate),
    }
}

// Register associates a session with its control channel.
// Called by CoreServer.Subscribe at stream setup time.
func (r *SessionStreamRegistry) Register(sessionID string, ch chan<- sessionStreamUpdate) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.channels[sessionID] = ch
}

// Deregister removes a session from the registry.
// Called by CoreServer.Subscribe on exit (via defer).
func (r *SessionStreamRegistry) Deregister(sessionID string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    delete(r.channels, sessionID)
}

// Send writes an update to a session's control channel without blocking.
// Returns SESSION_NOT_FOUND if no active Subscribe exists for the session.
// Returns CONTROL_CHANNEL_FULL if the channel buffer is exhausted.
func (r *SessionStreamRegistry) Send(sessionID string, update sessionStreamUpdate) error {
    r.mu.Lock()
    ch, ok := r.channels[sessionID]
    r.mu.Unlock()
    if !ok {
        return oops.Code("SESSION_NOT_FOUND").Errorf("no active subscribe for session %s", sessionID)
    }
    select {
    case ch <- update:
        return nil
    default:
        return oops.Code("CONTROL_CHANNEL_FULL").Errorf("control channel full for session %s", sessionID)
    }
}

// AddStream implements plugins.StreamRegistry. Subscribes a session to a stream.
func (r *SessionStreamRegistry) AddStream(_ context.Context, sessionID, stream string) error {
    return r.Send(sessionID, sessionStreamUpdate{stream: stream, add: true})
}

// RemoveStream implements plugins.StreamRegistry. Unsubscribes a session from a stream.
func (r *SessionStreamRegistry) RemoveStream(_ context.Context, sessionID, stream string) error {
    return r.Send(sessionID, sessionStreamUpdate{stream: stream, add: false})
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
task test -- -run TestSessionStreamRegistry ./internal/grpc/
```

Expected: all 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(grpc): SessionStreamRegistry for plugin-driven session stream control"
```

---

### Task 6: Lua Host `QuerySessionStreams`

**Files:**

- Modify: `internal/plugin/lua/host.go` (replace stub from Task 3)
- Modify: `internal/plugin/lua/host_test.go`

- [ ] **Step 1: Write failing tests**

In `internal/plugin/lua/host_test.go`, add:

```go
func TestLuaHostQuerySessionStreamsCallsOnSessionSubscribeWhenDefined(t *testing.T) {
    h := NewHost()
    ctx := context.Background()

    // Plugin that returns two streams
    luaCode := `
function on_session_subscribe(character_id, player_id, session_id)
    return {"channel:" .. character_id, "channel:general"}
end
`
    err := h.Load(ctx, &plugins.Manifest{
        Name:          "test-plugin",
        Version:       "1.0.0",
        Type:          plugins.TypeLua,
        SessionStreams: true,
        LuaPlugin:     &plugins.LuaConfig{Entry: "main.lua"},
    }, t.TempDir())
    // Load via code injection — use the test helper pattern from existing host_test.go
    // (check how existing tests inject Lua code without a real file — likely LoadFromCode or similar)
    _ = err // handled below

    // Use the existing test code injection mechanism:
    h.testInjectCode("test-plugin", luaCode)

    req := plugins.SessionStreamsRequest{
        CharacterID: "char-abc",
        PlayerID:    "player-xyz",
        SessionID:   "sess-123",
    }
    streams, err := h.QuerySessionStreams(ctx, "test-plugin", req)
    require.NoError(t, err)
    assert.ElementsMatch(t, []string{"channel:char-abc", "channel:general"}, streams)
}

func TestLuaHostQuerySessionStreamsReturnsNilWhenHandlerNotDefined(t *testing.T) {
    h := NewHost()
    ctx := context.Background()
    h.testInjectCode("test-plugin", `-- no on_session_subscribe defined`)

    streams, err := h.QuerySessionStreams(ctx, "test-plugin", plugins.SessionStreamsRequest{
        CharacterID: "char-abc", PlayerID: "player-xyz", SessionID: "sess-123",
    })
    require.NoError(t, err)
    assert.Nil(t, streams)
}

func TestLuaHostQuerySessionStreamsReturnsErrorWhenHandlerFails(t *testing.T) {
    h := NewHost()
    ctx := context.Background()
    h.testInjectCode("test-plugin", `
function on_session_subscribe(character_id, player_id, session_id)
    error("db connection failed")
end
`)
    _, err := h.QuerySessionStreams(ctx, "test-plugin", plugins.SessionStreamsRequest{
        CharacterID: "char-abc", PlayerID: "player-xyz", SessionID: "sess-123",
    })
    require.Error(t, err)
}
```

Check existing `host_test.go` for the pattern used to inject Lua code without a real file. Look for `testInjectCode` or a similar mechanism. If it doesn't exist, look for how `DeliverEvent` tests work and replicate the pattern. The typical approach in this codebase is to directly assign to `h.plugins[name]` in a test helper.

- [ ] **Step 2: Run tests to verify they fail**

```bash
task test -- -run TestLuaHostQuerySessionStreams ./internal/plugin/lua/
```

Expected: test failures (stub returns nil).

- [ ] **Step 3: Implement `QuerySessionStreams` in Lua host**

Replace the stub in `internal/plugin/lua/host.go`:

```go
// QuerySessionStreams calls the plugin's on_session_subscribe(character_id, player_id, session_id)
// function if defined. Returns the list of stream names the plugin wants added.
// Returns nil without error if the function is not defined.
func (h *Host) QuerySessionStreams(ctx context.Context, name string, req plugins.SessionStreamsRequest) ([]string, error) {
    h.mu.RLock()
    p, ok := h.plugins[name]
    if !ok {
        h.mu.RUnlock()
        return nil, oops.In("lua").With("plugin", name).With("operation", "query_session_streams").New("plugin not loaded")
    }
    code := p.code
    h.mu.RUnlock()

    L, err := h.factory.NewState(ctx)
    if err != nil {
        return nil, oops.In("lua").With("plugin", name).With("operation", "query_session_streams").Wrap(err)
    }
    defer L.Close()
    L.SetContext(ctx)

    if h.hostFuncs != nil {
        h.hostFuncs.Register(L, name)
    }

    if err := L.DoString(code); err != nil {
        return nil, oops.In("lua").With("plugin", name).With("operation", "query_session_streams").Wrap(err)
    }

    fn := L.GetGlobal("on_session_subscribe")
    if fn.Type() == lua.LTNil {
        return nil, nil // handler not defined — not an error
    }

    if err := L.CallByParam(lua.P{
        Fn:      fn,
        NRet:    1,
        Protect: true,
    },
        lua.LString(req.CharacterID),
        lua.LString(req.PlayerID),
        lua.LString(req.SessionID),
    ); err != nil {
        return nil, oops.In("lua").With("plugin", name).With("operation", "on_session_subscribe").Wrap(err)
    }

    ret := L.Get(-1)
    L.Pop(1)

    tbl, ok := ret.(*lua.LTable)
    if !ok {
        if ret.Type() == lua.LTNil {
            return nil, nil
        }
        return nil, oops.In("lua").With("plugin", name).With("operation", "on_session_subscribe").
            Errorf("expected table return, got %s", ret.Type())
    }

    var streams []string
    tbl.ForEach(func(_ lua.LValue, v lua.LValue) {
        if s, ok := v.(lua.LString); ok {
            streams = append(streams, string(s))
        }
    })
    return streams, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
task test -- -run TestLuaHostQuerySessionStreams ./internal/plugin/lua/
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(lua): implement QuerySessionStreams via on_session_subscribe callback"
```

---

### Task 7: Hostfunc Stream Functions (`holo.add_session_stream` / `holo.remove_session_stream`)

**Files:**

- Modify: `internal/plugin/hostfunc/functions.go`
- Create: `internal/plugin/hostfunc/stdlib_streams.go`
- Create: `internal/plugin/hostfunc/stdlib_streams_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/plugin/hostfunc/stdlib_streams_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
    "context"
    "testing"

    lua "github.com/yuin/gopher-lua"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    plugins "github.com/holomush/holomush/internal/plugin"
    "github.com/holomush/holomush/internal/plugin/hostfunc"
)

// mockStreamRegistry records calls for assertion.
type mockStreamRegistry struct {
    addCalls    []struct{ sessionID, stream string }
    removeCalls []struct{ sessionID, stream string }
    addErr      error
    removeErr   error
}

func (m *mockStreamRegistry) AddStream(_ context.Context, sessionID, stream string) error {
    m.addCalls = append(m.addCalls, struct{ sessionID, stream string }{sessionID, stream})
    return m.addErr
}

func (m *mockStreamRegistry) RemoveStream(_ context.Context, sessionID, stream string) error {
    m.removeCalls = append(m.removeCalls, struct{ sessionID, stream string }{sessionID, stream})
    return m.removeErr
}

// Ensure mockStreamRegistry implements plugins.StreamRegistry.
var _ plugins.StreamRegistry = (*mockStreamRegistry)(nil)

func newTestLuaState(t *testing.T, reg plugins.StreamRegistry) *lua.LState {
    t.Helper()
    L := lua.NewState()
    t.Cleanup(L.Close)
    hf := hostfunc.New(nil, hostfunc.WithStreamRegistry(reg))
    hf.Register(L, "test-plugin")
    return L
}

func TestAddSessionStreamCallsRegistryWithCorrectArgs(t *testing.T) {
    reg := &mockStreamRegistry{}
    L := newTestLuaState(t, reg)

    err := L.DoString(`holomush.add_session_stream("sess-1", "channel:abc")`)
    require.NoError(t, err)

    require.Len(t, reg.addCalls, 1)
    assert.Equal(t, "sess-1", reg.addCalls[0].sessionID)
    assert.Equal(t, "channel:abc", reg.addCalls[0].stream)
}

func TestRemoveSessionStreamCallsRegistryWithCorrectArgs(t *testing.T) {
    reg := &mockStreamRegistry{}
    L := newTestLuaState(t, reg)

    err := L.DoString(`holomush.remove_session_stream("sess-1", "channel:abc")`)
    require.NoError(t, err)

    require.Len(t, reg.removeCalls, 1)
    assert.Equal(t, "sess-1", reg.removeCalls[0].sessionID)
    assert.Equal(t, "channel:abc", reg.removeCalls[0].stream)
}

func TestAddSessionStreamWithNilRegistryIsNoOp(t *testing.T) {
    L := lua.NewState()
    defer L.Close()
    // Register without stream registry
    hf := hostfunc.New(nil)
    hf.Register(L, "test-plugin")

    // Should not panic — just be a no-op or return an error message
    err := L.DoString(`holomush.add_session_stream("sess-1", "channel:abc")`)
    require.NoError(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
task test -- -run "TestAddSessionStream|TestRemoveSessionStream" ./internal/plugin/hostfunc/
```

Expected: compile errors (WithStreamRegistry and add/remove_session_stream not defined).

- [ ] **Step 3: Add `WithStreamRegistry` to `functions.go`**

In `internal/plugin/hostfunc/functions.go`:

Add field to `Functions` struct:

```go
streamRegistry plugins.StreamRegistry
```

Add import: `plugins "github.com/holomush/holomush/internal/plugin"`

Add option function:

```go
// WithStreamRegistry sets the stream registry for add/remove session stream host functions.
// When set, plugins can call holomush.add_session_stream and holomush.remove_session_stream.
func WithStreamRegistry(r plugins.StreamRegistry) Option {
    return func(f *Functions) {
        f.streamRegistry = r
    }
}
```

In `Functions.Register`, add after the session functions block:

```go
// Register stream management functions if registry is configured.
if f.streamRegistry != nil {
    RegisterStreamFuncs(ls, mod, f.streamRegistry)
}
```

- [ ] **Step 4: Create `stdlib_streams.go`**

Create `internal/plugin/hostfunc/stdlib_streams.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
    "context"
    "log/slog"

    lua "github.com/yuin/gopher-lua"

    plugins "github.com/holomush/holomush/internal/plugin"
)

const streamRegistryKey = "__holo_stream_registry"

// RegisterStreamFuncs adds holomush.add_session_stream and holomush.remove_session_stream
// to an existing holomush module table.
func RegisterStreamFuncs(ls *lua.LState, holoMushTable *lua.LTable, r plugins.StreamRegistry) {
    ud := ls.NewUserData()
    ud.Value = r
    ls.SetGlobal(streamRegistryKey, ud)

    ls.SetField(holoMushTable, "add_session_stream", ls.NewFunction(addSessionStreamFn))
    ls.SetField(holoMushTable, "remove_session_stream", ls.NewFunction(removeSessionStreamFn))
}

func getStreamRegistry(ls *lua.LState) plugins.StreamRegistry {
    ud := ls.GetGlobal(streamRegistryKey)
    if ud.Type() == lua.LTUserData {
        if userData, ok := ud.(*lua.LUserData); ok {
            if r, ok := userData.Value.(plugins.StreamRegistry); ok {
                return r
            }
        }
    }
    return nil
}

// addSessionStreamFn implements holomush.add_session_stream(session_id, stream).
// Returns nothing on success; logs a warning on error.
func addSessionStreamFn(ls *lua.LState) int {
    sessionID := ls.CheckString(1)
    stream := ls.CheckString(2)

    r := getStreamRegistry(ls)
    if r == nil {
        slog.Warn("holomush.add_session_stream: stream registry not initialized")
        return 0
    }

    ctx := ls.Context()
    if ctx == nil {
        ctx = context.Background()
    }

    if err := r.AddStream(ctx, sessionID, stream); err != nil {
        slog.WarnContext(ctx, "holomush.add_session_stream failed",
            "session_id", sessionID, "stream", stream, "error", err)
    }
    return 0
}

// removeSessionStreamFn implements holomush.remove_session_stream(session_id, stream).
// Returns nothing on success; logs a warning on error.
func removeSessionStreamFn(ls *lua.LState) int {
    sessionID := ls.CheckString(1)
    stream := ls.CheckString(2)

    r := getStreamRegistry(ls)
    if r == nil {
        slog.Warn("holomush.remove_session_stream: stream registry not initialized")
        return 0
    }

    ctx := ls.Context()
    if ctx == nil {
        ctx = context.Background()
    }

    if err := r.RemoveStream(ctx, sessionID, stream); err != nil {
        slog.WarnContext(ctx, "holomush.remove_session_stream failed",
            "session_id", sessionID, "stream", stream, "error", err)
    }
    return 0
}
```

Note: `RegisterStreamFuncs` receives `holoMushTable` (the `holomush` module table, not the `holo` table). Check `functions.go`'s `Register` to confirm — the `mod` variable is set as `holomush` global. Pass `mod` (not a `holo` sub-table) to `RegisterStreamFuncs`.

- [ ] **Step 5: Run tests to verify they pass**

```bash
task test -- -run "TestAddSessionStream|TestRemoveSessionStream" ./internal/plugin/hostfunc/
```

Expected: all 3 tests PASS.

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(hostfunc): add holomush.add_session_stream / remove_session_stream Lua hostfuncs"
```

---

### Task 8: Binary Plugin `QuerySessionStreams`

**Files:**

- Modify: `internal/plugin/goplugin/host.go` (replace stub from Task 3)
- Modify: `internal/plugin/goplugin/host_test.go`

- [ ] **Step 1: Write failing tests**

In `internal/plugin/goplugin/host_test.go`, following the existing `DeliverEvent` test pattern:

```go
func TestGopluginHostQuerySessionStreamsCallsPluginRPC(t *testing.T) {
    // Arrange: create a host with a mock factory
    factory := &mockClientFactory{}
    h := NewHostWithFactory(factory)

    // Load a fake plugin using the test helper pattern from existing tests
    // (look for how existing tests set up a loaded plugin without a real binary)
    // Typically: inject directly into h.plugins map
    mockClient := &mockPluginServiceClient{}
    mockClient.QuerySessionStreamsFunc = func(ctx context.Context, req *pluginv1.QuerySessionStreamsRequest) (*pluginv1.QuerySessionStreamsResponse, error) {
        return &pluginv1.QuerySessionStreamsResponse{
            Streams: []string{"channel:abc", "channel:def"},
        }, nil
    }
    h.testInjectPlugin("test-plugin", mockClient)

    streams, err := h.QuerySessionStreams(context.Background(), "test-plugin", plugins.SessionStreamsRequest{
        CharacterID: "char-1",
        PlayerID:    "player-1",
        SessionID:   "sess-1",
    })
    require.NoError(t, err)
    assert.ElementsMatch(t, []string{"channel:abc", "channel:def"}, streams)
}

func TestGopluginHostQuerySessionStreamsReturnsErrorFromPlugin(t *testing.T) {
    factory := &mockClientFactory{}
    h := NewHostWithFactory(factory)

    mockClient := &mockPluginServiceClient{}
    mockClient.QuerySessionStreamsFunc = func(ctx context.Context, req *pluginv1.QuerySessionStreamsRequest) (*pluginv1.QuerySessionStreamsResponse, error) {
        return &pluginv1.QuerySessionStreamsResponse{Error: "db unavailable"}, nil
    }
    h.testInjectPlugin("test-plugin", mockClient)

    _, err := h.QuerySessionStreams(context.Background(), "test-plugin", plugins.SessionStreamsRequest{
        CharacterID: "char-1", PlayerID: "player-1", SessionID: "sess-1",
    })
    require.Error(t, err)
    assert.Contains(t, err.Error(), "db unavailable")
}
```

Check existing test patterns for `mockPluginServiceClient` — it likely already has fields for `HandleEventFunc`, `HandleCommandFunc`. Add `QuerySessionStreamsFunc` the same way.

- [ ] **Step 2: Run tests to verify they fail**

```bash
task test -- -run TestGopluginHostQuerySessionStreams ./internal/plugin/goplugin/
```

Expected: compile errors or failures.

- [ ] **Step 3: Implement `QuerySessionStreams` in goplugin host**

Replace the stub in `internal/plugin/goplugin/host.go`:

```go
// QuerySessionStreams calls the plugin's QuerySessionStreams RPC.
// Returns nil if the plugin has no streams to contribute.
func (h *Host) QuerySessionStreams(ctx context.Context, name string, req plugins.SessionStreamsRequest) ([]string, error) {
    h.mu.RLock()
    if h.closed {
        h.mu.RUnlock()
        return nil, ErrHostClosed
    }
    p, ok := h.plugins[name]
    h.mu.RUnlock()

    if !ok {
        return nil, oops.In("goplugin").With("plugin", name).With("operation", "query_session_streams").Wrap(ErrPluginNotLoaded)
    }

    resp, err := p.plugin.QuerySessionStreams(ctx, &pluginv1.QuerySessionStreamsRequest{
        CharacterId: req.CharacterID,
        PlayerId:    req.PlayerID,
        SessionId:   req.SessionID,
    })
    if err != nil {
        return nil, oops.In("goplugin").With("plugin", name).With("operation", "query_session_streams").Wrap(err)
    }
    if resp.Error != "" {
        return nil, oops.In("goplugin").With("plugin", name).With("operation", "query_session_streams").New(resp.Error)
    }
    return resp.Streams, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
task test -- -run TestGopluginHostQuerySessionStreams ./internal/plugin/goplugin/
```

Expected: all 2 tests PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "feat(goplugin): implement QuerySessionStreams via PluginService RPC"
```

---

### Task 9: Binary Plugin `HostService` Stream Methods

**Files:**

- Modify: `internal/plugin/goplugin/host_service.go`
- Modify: `internal/plugin/goplugin/host_service_test.go`

- [ ] **Step 1: Write failing tests**

In `internal/plugin/goplugin/host_service_test.go`, following the existing pattern:

```go
func TestPluginHostServiceAddSessionStreamSucceeds(t *testing.T) {
    registry := &mockStreamRegistry{}
    proxy := newMockServiceProxy()
    svc := NewPluginHostService(proxy, slog.Default())
    svc.streamRegistry = registry // inject directly for test; or use constructor option

    resp, err := svc.AddSessionStream(context.Background(), &pluginv1.AddSessionStreamRequest{
        SessionId: "sess-1",
        Stream:    "channel:abc",
    })
    require.NoError(t, err)
    assert.True(t, resp.Success)
    require.Len(t, registry.addCalls, 1)
    assert.Equal(t, "sess-1", registry.addCalls[0].sessionID)
    assert.Equal(t, "channel:abc", registry.addCalls[0].stream)
}

func TestPluginHostServiceAddSessionStreamReturnsNotFoundWhenSessionGone(t *testing.T) {
    registry := &mockStreamRegistry{addErr: oops.Code("SESSION_NOT_FOUND").Errorf("not found")}
    svc := NewPluginHostServiceWithRegistry(newMockServiceProxy(), slog.Default(), registry)

    _, err := svc.AddSessionStream(context.Background(), &pluginv1.AddSessionStreamRequest{
        SessionId: "sess-gone",
        Stream:    "channel:abc",
    })
    require.Error(t, err)
    // Should be gRPC status NotFound
    st, ok := status.FromError(err)
    require.True(t, ok)
    assert.Equal(t, codes.NotFound, st.Code())
}

func TestPluginHostServiceRemoveSessionStreamSucceeds(t *testing.T) {
    registry := &mockStreamRegistry{}
    svc := NewPluginHostServiceWithRegistry(newMockServiceProxy(), slog.Default(), registry)

    resp, err := svc.RemoveSessionStream(context.Background(), &pluginv1.RemoveSessionStreamRequest{
        SessionId: "sess-1",
        Stream:    "channel:abc",
    })
    require.NoError(t, err)
    assert.True(t, resp.Success)
    require.Len(t, registry.removeCalls, 1)
}
```

Add a `mockStreamRegistry` in this test file (same shape as in Task 7, or share via a test helper).

Adjust the constructor: `NewPluginHostService` needs to accept the registry. Use a functional option or a new constructor `NewPluginHostServiceWithRegistry`. Check how `PluginHostService` is currently constructed and follow the same pattern.

- [ ] **Step 2: Run tests to verify they fail**

```bash
task test -- -run "TestPluginHostServiceAddSessionStream|TestPluginHostServiceRemoveSessionStream" ./internal/plugin/goplugin/
```

Expected: compile errors.

- [ ] **Step 3: Add `streamRegistry` to `PluginHostService` and new constructor**

In `internal/plugin/goplugin/host_service.go`:

Add field to `PluginHostService`:

```go
streamRegistry plugins.StreamRegistry
```

Add import: `plugins "github.com/holomush/holomush/internal/plugin"`

Add constructor option:

```go
// NewPluginHostServiceWithRegistry creates a PluginHostService with a stream registry
// for handling AddSessionStream/RemoveSessionStream calls.
func NewPluginHostServiceWithRegistry(proxy plugins.ServiceProxy, logger *slog.Logger, registry plugins.StreamRegistry) *PluginHostService {
    svc := NewPluginHostService(proxy, logger)
    svc.streamRegistry = registry
    return svc
}
```

- [ ] **Step 4: Implement `AddSessionStream` and `RemoveSessionStream`**

In `internal/plugin/goplugin/host_service.go`, add:

```go
// AddSessionStream subscribes an active session to an additional stream.
func (s *PluginHostService) AddSessionStream(ctx context.Context, req *pluginv1.AddSessionStreamRequest) (*pluginv1.AddSessionStreamResponse, error) {
    if s.streamRegistry == nil {
        return nil, status.Error(codes.Unimplemented, "stream registry not configured")
    }
    if err := s.streamRegistry.AddStream(ctx, req.GetSessionId(), req.GetStream()); err != nil {
        e := oops.AsOops(err)
        if e != nil && e.Code() == "SESSION_NOT_FOUND" {
            return nil, status.Errorf(codes.NotFound, "session not found: %s", req.GetSessionId())
        }
        return nil, status.Errorf(codes.Internal, "add stream failed: %v", err)
    }
    return &pluginv1.AddSessionStreamResponse{Success: true}, nil
}

// RemoveSessionStream unsubscribes an active session from a stream. Idempotent.
func (s *PluginHostService) RemoveSessionStream(ctx context.Context, req *pluginv1.RemoveSessionStreamRequest) (*pluginv1.RemoveSessionStreamResponse, error) {
    if s.streamRegistry == nil {
        return nil, status.Error(codes.Unimplemented, "stream registry not configured")
    }
    if err := s.streamRegistry.RemoveStream(ctx, req.GetSessionId(), req.GetStream()); err != nil {
        e := oops.AsOops(err)
        if e != nil && e.Code() == "SESSION_NOT_FOUND" {
            // Session gone — idempotent success (character already disconnected)
            return &pluginv1.RemoveSessionStreamResponse{Success: true}, nil
        }
        return nil, status.Errorf(codes.Internal, "remove stream failed: %v", err)
    }
    return &pluginv1.RemoveSessionStreamResponse{Success: true}, nil
}
```

Check `oops.AsOops` — if it doesn't exist, use `oops.GetCode(err)` or check the oops package for how to extract a code from a wrapped error. The pattern in the codebase is `oops.AsOops(err).Code()` or checking `errors.Is` with a sentinel.

Look at how `errutil.AssertErrorCode` works in `pkg/errutil/` for the right pattern to extract oops codes.

- [ ] **Step 5: Run tests to verify they pass**

```bash
task test -- -run "TestPluginHostServiceAddSessionStream|TestPluginHostServiceRemoveSessionStream" ./internal/plugin/goplugin/
```

Expected: all 3 tests PASS.

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(goplugin): implement AddSessionStream/RemoveSessionStream on PluginHostService"
```

---

### Task 10: `CoreServer.Subscribe` Extension

**Files:**

- Modify: `internal/grpc/server.go`
- Modify: `internal/grpc/server_test.go`

- [ ] **Step 1: Write failing unit tests**

In `internal/grpc/server_test.go`, add a mock for `SessionStreamContributor` (define the interface first):

```go
// mockStreamContributor returns a fixed list of plugin streams.
type mockStreamContributor struct {
    streams []string
}

func (m *mockStreamContributor) QuerySessionStreams(_ context.Context, _ plugins.SessionStreamsRequest) []string {
    return m.streams
}
```

Add tests:

```go
func TestSubscribeIncludesPluginContributedStreams(t *testing.T) {
    // Arrange: CoreServer with a contributor that returns one channel stream
    contrib := &mockStreamContributor{streams: []string{"channel:abc"}}
    s := newTestCoreServer(t, withStreamContributor(contrib))

    session := createTestSession(t, s) // uses existing test helpers

    // Act: subscribe
    msgs := collectSubscribeMessages(t, s, session.ID, 0 /* timeout ms */)

    // Assert: LISTEN was set up for channel:abc (verify via replay or mock event store)
    // The simplest assertion: subscribe doesn't error and the mock event store
    // received a Subscribe call for "channel:abc"
    mockStore := s.eventStore.(*mockEventStore)
    assert.Contains(t, mockStore.subscribedStreams, "channel:abc")
}

func TestSubscribeCtrlChAddStreamSetupLISTENAndReplays(t *testing.T) {
    s := newTestCoreServer(t)
    sess := createTestSession(t, s)

    // Append an event to channel:new before the subscribe
    appendTestEvent(t, s, "channel:new", "test.event", `{}`)

    // Start subscribe in background
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    msgs := startSubscribeAsync(t, ctx, s, sess.ID)

    // Wait for replay complete
    waitForReplayComplete(t, msgs, time.Second)

    // Now send ctrlCh add via registry
    err := s.streamRegistry.AddStream(ctx, sess.ID, "channel:new")
    require.NoError(t, err)

    // Should receive the previously-appended event via tail replay
    msg := waitForMessage(t, msgs, time.Second)
    require.NotNil(t, msg.GetEvent())
    assert.Equal(t, "channel:new", msg.GetEvent().Stream)
}

func TestSubscribeCtrlChRemoveStreamStopsForwarding(t *testing.T) {
    s := newTestCoreServer(t, withStreamContributor(&mockStreamContributor{
        streams: []string{"channel:abc"},
    }))
    sess := createTestSession(t, s)

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    msgs := startSubscribeAsync(t, ctx, s, sess.ID)
    waitForReplayComplete(t, msgs, time.Second)

    // Remove the stream
    err := s.streamRegistry.RemoveStream(ctx, sess.ID, "channel:abc")
    require.NoError(t, err)

    // Append event to channel:abc — should NOT be forwarded
    appendTestEvent(t, s, "channel:abc", "test.event", `{}`)

    // Short wait to confirm no message arrives
    select {
    case msg := <-msgs:
        if msg.GetEvent() != nil && msg.GetEvent().Stream == "channel:abc" {
            t.Fatal("received event on removed stream")
        }
    case <-time.After(200 * time.Millisecond):
        // Expected: no message forwarded
    }
}

func TestSubscribeDeregistersRegistryOnExit(t *testing.T) {
    registry := NewSessionStreamRegistry()
    s := newTestCoreServer(t, withStreamRegistry(registry))
    sess := createTestSession(t, s)

    ctx, cancel := context.WithCancel(context.Background())
    startSubscribeAsync(t, ctx, s, sess.ID)
    time.Sleep(50 * time.Millisecond) // brief settle

    // Verify registered
    err := registry.Send(sess.ID, sessionStreamUpdate{stream: "channel:x", add: true})
    require.NoError(t, err)

    // Cancel to end subscribe
    cancel()
    time.Sleep(50 * time.Millisecond) // let goroutine exit

    // Verify deregistered
    err = registry.Send(sess.ID, sessionStreamUpdate{stream: "channel:x", add: true})
    require.Error(t, err)
    assert.Contains(t, err.Error(), "SESSION_NOT_FOUND")
}
```

These tests use `newTestCoreServer` and supporting helpers — look at how existing `server_test.go` constructs test servers and replicate the pattern. Add `withStreamContributor` and `withStreamRegistry` as `CoreServerOption` helpers for tests.

- [ ] **Step 2: Run tests to verify they fail**

```bash
task test -- -run "TestSubscribeIncludesPlugin|TestSubscribeCtrlCh|TestSubscribeDeregisters" ./internal/grpc/
```

Expected: compile errors (new fields/interface not defined).

- [ ] **Step 3: Add `SessionStreamContributor` interface and new fields to `CoreServer`**

In `internal/grpc/server.go`, add after the existing interface definitions:

```go
// SessionStreamContributor collects plugin-contributed stream names for a session.
type SessionStreamContributor interface {
    QuerySessionStreams(ctx context.Context, req plugins.SessionStreamsRequest) []string
}
```

Add import: `plugins "github.com/holomush/holomush/internal/plugin"`

Add to `CoreServer` struct:

```go
streamContributor SessionStreamContributor
streamRegistry    *SessionStreamRegistry

// afterLISTENHook is called between LISTEN setup and replay in Subscribe.
// Used in tests to inject events into the race window.
afterLISTENHook func()
```

Add option functions:

```go
// WithStreamContributor sets the plugin stream contributor for session subscriptions.
func WithStreamContributor(c SessionStreamContributor) CoreServerOption {
    return func(s *CoreServer) {
        s.streamContributor = c
    }
}

// WithStreamRegistry sets the session stream registry for mid-session stream control.
func WithStreamRegistry(r *SessionStreamRegistry) CoreServerOption {
    return func(s *CoreServer) {
        s.streamRegistry = r
    }
}
```

- [ ] **Step 4: Modify `CoreServer.Subscribe`**

In `server.go`'s `Subscribe` method, make these changes in order:

**After** `info, err := s.sessionStore.Get(...)` and before the `streams := req.Streams` block, add:

```go
// Collect plugin-contributed streams before LISTEN setup.
if s.streamContributor != nil {
    contribCtx, contribCancel := context.WithTimeout(ctx, 2*time.Second)
    pluginStreams := s.streamContributor.QuerySessionStreams(contribCtx, plugins.SessionStreamsRequest{
        CharacterID: info.CharacterID.String(),
        PlayerID:    info.PlayerID.String(),
        SessionID:   info.ID,
    })
    contribCancel()
    if len(req.Streams) == 0 {
        // Will be defaulted below; append after default assignment
    }
    _ = pluginStreams // used after defaults are applied
}
```

Actually restructure the plugin stream collection to happen after the `streams` / `staticStreams` / `locStreamName` computation:

After the existing block that builds `staticStreams` and `locStreamName`, add:

```go
// Merge plugin-contributed streams into staticStreams.
if s.streamContributor != nil {
    contribCtx, contribCancel := context.WithTimeout(ctx, 2*time.Second)
    pluginStreams := s.streamContributor.QuerySessionStreams(contribCtx, plugins.SessionStreamsRequest{
        CharacterID: info.CharacterID.String(),
        PlayerID:    info.PlayerID.String(),
        SessionID:   info.ID,
    })
    contribCancel()
    for _, ps := range pluginStreams {
        // Exclude location streams (managed by locationFollower)
        if !strings.HasPrefix(ps, world.StreamPrefixLocation) {
            // Check for duplicate
            duplicate := false
            for _, existing := range staticStreams {
                if existing == ps {
                    duplicate = true
                    break
                }
            }
            if !duplicate {
                staticStreams = append(staticStreams, ps)
            }
        }
    }
}
```

**Replace** the existing `subscribeStream` calls with a per-stream cancel version. Add a `streamCancels` map before the LISTEN loop:

```go
streamCancels := make(map[string]context.CancelFunc)
subscribeWithCancel := func(sn string) error {
    sctx, cancel := context.WithCancel(ctx)
    streamCancels[sn] = cancel
    return subscribeStream(sctx, s.eventStore, sn, notifyCh, errCh)
}
```

Replace `subscribeStream(ctx, ...)` calls with `subscribeWithCancel(sn)`.

**After** the LISTEN loop and before the synthetic location_state block, add:

```go
// Register this session's control channel for mid-session stream updates.
ctrlCh := make(chan sessionStreamUpdate, 16)
if s.streamRegistry != nil {
    s.streamRegistry.Register(info.ID, ctrlCh)
    defer s.streamRegistry.Deregister(info.ID)
}

// afterLISTENHook fires between LISTEN setup and replay for race-window testing.
if s.afterLISTENHook != nil {
    s.afterLISTENHook()
}
```

**In the live-forward select loop**, add a `ctrlCh` case. Find the `select` statement in the post-replay loop and add:

```go
case ctrl, ok := <-ctrlCh:
    if !ok {
        return nil
    }
    if ctrl.add {
        if _, exists := streamCancels[ctrl.stream]; !exists {
            // Reject attempts to take over location stream management
            if strings.HasPrefix(ctrl.stream, world.StreamPrefixLocation) {
                slog.WarnContext(ctx, "plugin attempted to add location stream — rejected",
                    "session_id", info.ID, "stream", ctrl.stream)
                continue
            }
            if subErr := subscribeWithCancel(ctrl.stream); subErr != nil {
                slog.WarnContext(ctx, "mid-session stream add failed",
                    "session_id", info.ID, "stream", ctrl.stream, "error", subErr)
            } else {
                // Replay tail — use stored cursor if character was subscribed before
                cursor := ulid.ULID{}
                if info.EventCursors != nil {
                    if c, ok := info.EventCursors[ctrl.stream]; ok {
                        cursor = c
                    }
                }
                if _, sendErr := s.replayAndSend(ctx, info, ctrl.stream, cursor, stream, lf); sendErr != nil {
                    return oops.Code("SEND_FAILED").With("session_id", info.ID).Wrap(sendErr)
                }
            }
        }
    } else {
        if cancel, exists := streamCancels[ctrl.stream]; exists {
            cancel()
            delete(streamCancels, ctrl.stream)
        }
    }
```

Also ensure stream cancels are cleaned up on exit. After the defer for `lf.locCancel`, add:

```go
defer func() {
    for _, cancel := range streamCancels {
        cancel()
    }
}()
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
task test -- -run "TestSubscribeIncludesPlugin|TestSubscribeCtrlCh|TestSubscribeDeregisters" ./internal/grpc/
```

Expected: all 4 tests PASS. Run full server tests too:

```bash
task test -- ./internal/grpc/
```

Expected: all existing tests still pass.

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(grpc): Subscribe collects plugin streams, handles mid-session add/remove via ctrlCh"
```

---

### Task 11: Wiring

**Files:**

- Modify: The file in `cmd/holomush/` where `NewCoreServer` and `hostfunc.New` are called. Check `cmd/holomush/` — likely `app.go`, `server.go`, or `setup.go`.

- [ ] **Step 1: Find the wiring file**

```bash
task build
```

Check compiler errors — they'll point to files that need updating. Also:

```bash
ls cmd/holomush/
```

- [ ] **Step 2: Create and wire `SessionStreamRegistry`**

In the wiring file, add:

```go
// Create stream registry — shared by CoreServer (Subscribe) and hostfunc (plugin calls).
streamRegistry := grpcpkg.NewSessionStreamRegistry()
```

- [ ] **Step 3: Pass `streamRegistry` to `hostfunc.New`**

Find the `hostfunc.New(...)` call and add `hostfunc.WithStreamRegistry(streamRegistry)`:

```go
hf := hostfunc.New(kvStore,
    hostfunc.WithWorldService(worldSvc),
    hostfunc.WithSessionAccess(sessionAccess),
    hostfunc.WithStreamRegistry(streamRegistry), // NEW
    // ... other existing options
)
```

- [ ] **Step 4: Pass `streamRegistry` to binary plugin host service**

Find where `goplugin.NewPluginHostService` is called and update to `NewPluginHostServiceWithRegistry`:

```go
hostSvc := goplugin.NewPluginHostServiceWithRegistry(serviceProxy, logger, streamRegistry)
```

- [ ] **Step 5: Pass `manager` as `streamContributor` and `streamRegistry` to `CoreServer`**

Find the `grpcpkg.NewCoreServer(...)` call and add:

```go
grpcpkg.NewCoreServer(engine, sessionStore, dispatcher, cmdServices,
    // ... existing options ...
    grpcpkg.WithStreamContributor(pluginManager), // pluginManager is *plugins.Manager
    grpcpkg.WithStreamRegistry(streamRegistry),   // NEW
)
```

- [ ] **Step 6: Verify build and tests**

```bash
task build && task test
```

Expected: successful build, all unit tests pass.

- [ ] **Step 7: Commit**

```bash
jj commit -m "feat(cmd): wire SessionStreamRegistry and StreamContributor into server and hostfunc"
```

---

### Task 12: Integration Tests

**Files:**

- Create: `test/integration/plugin/session_streams_suite_test.go`
- Create: `test/integration/plugin/session_streams_integration_test.go`

- [ ] **Step 1: Create suite file**

Create `test/integration/plugin/session_streams_suite_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
    "testing"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

func TestPluginSessionStreams(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Plugin Session Streams Suite")
}
```

- [ ] **Step 2: Create integration test file**

Create `test/integration/plugin/session_streams_integration_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
    "context"
    "fmt"
    "net"
    "sync/atomic"
    "time"

    "github.com/oklog/ulid/v2"
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "google.golang.org/grpc"

    "github.com/holomush/holomush/internal/access/policy/policytest"
    "github.com/holomush/holomush/internal/command"
    "github.com/holomush/holomush/internal/core"
    grpcpkg "github.com/holomush/holomush/internal/grpc"
    plugins "github.com/holomush/holomush/internal/plugin"
    "github.com/holomush/holomush/internal/session"
    "github.com/holomush/holomush/internal/store"
    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// staticContributor is a test-only SessionStreamContributor that returns fixed streams.
type staticContributor struct {
    streams []string
    called  atomic.Int32
}

func (c *staticContributor) QuerySessionStreams(_ context.Context, _ plugins.SessionStreamsRequest) []string {
    c.called.Add(1)
    return c.streams
}

var _ = Describe("Plugin Session Stream Contribution", func() {
    var (
        testCtx      context.Context
        testCancel   context.CancelFunc
        container    *postgres.PostgresContainer
        grpcServer   *grpc.Server
        grpcCli      *grpcpkg.Client
        sessionStore *store.PostgresSessionStore
        eventStore   *store.PostgresEventStore
        contributor  *staticContributor
        registry     *grpcpkg.SessionStreamRegistry
    )

    BeforeEach(func() {
        testCtx, testCancel = context.WithTimeout(context.Background(), 60*time.Second)
        // Start Postgres testcontainer — follow the pattern from session_persistence_integration_test.go
        // (copy the container setup and server construction from there)
        // Key difference: pass contributor and registry to CoreServer

        contributor = &staticContributor{streams: []string{}}
        registry = grpcpkg.NewSessionStreamRegistry()

        // Wire contributor and registry into CoreServer via options:
        //   grpcpkg.WithStreamContributor(contributor),
        //   grpcpkg.WithStreamRegistry(registry),
        // See session_persistence_integration_test.go for the full server construction pattern.
    })

    AfterEach(func() {
        testCancel()
        if grpcServer != nil {
            grpcServer.GracefulStop()
        }
        if container != nil {
            _ = container.Terminate(context.Background())
        }
    })

    Describe("UC1: session-start auto-subscribe", func() {
        It("receives messages posted before login via replay", func() {
            // Arrange: configure contributor to return "channel:general"
            contributor.streams = []string{"channel:general"}

            // Append a message to channel:general before the session starts
            msgID := ulid.Make()
            Expect(eventStore.Append(testCtx, core.Event{
                ID:      msgID,
                Stream:  "channel:general",
                Type:    "channel.message",
                Actor:   core.Actor{Kind: core.ActorCharacter, ID: "char-other"},
                Payload: []byte(`{"message":"hello from before"}`),
            })).To(Succeed())

            // Authenticate and subscribe
            resp, err := grpcCli.Authenticate(testCtx, &corev1.AuthenticateRequest{
                Username: "guest", Password: "",
            })
            Expect(err).NotTo(HaveOccurred())
            Expect(resp.Success).To(BeTrue())

            subCtx, subCancel := context.WithTimeout(testCtx, 5*time.Second)
            defer subCancel()
            subStream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
                SessionId: resp.SessionId,
            })
            Expect(err).NotTo(HaveOccurred())

            // Collect messages until REPLAY_COMPLETE
            var received []string
            for {
                msg, err := subStream.Recv()
                Expect(err).NotTo(HaveOccurred())
                if msg.GetControl() != nil &&
                    msg.GetControl().Signal == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE {
                    break
                }
                if msg.GetEvent() != nil {
                    received = append(received, msg.GetEvent().Stream)
                }
            }

            Expect(received).To(ContainElement("channel:general"))
        })

        It("proceeds normally when contributor returns error", func() {
            // Arrange: contributor that panics / returns nothing (simulate failure)
            // by setting contributor.streams to nil and testing session still establishes
            contributor.streams = nil

            resp, err := grpcCli.Authenticate(testCtx, &corev1.AuthenticateRequest{
                Username: "guest", Password: "",
            })
            Expect(err).NotTo(HaveOccurred())
            Expect(resp.Success).To(BeTrue())

            // Subscribe should succeed with core-only streams
            subCtx, subCancel := context.WithTimeout(testCtx, 3*time.Second)
            defer subCancel()
            _, err = grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{SessionId: resp.SessionId})
            Expect(err).NotTo(HaveOccurred())
        })
    })

    Describe("UC2: mid-session subscription changes", func() {
        It("receives messages on a stream added mid-session without reconnecting", func() {
            contributor.streams = nil // no streams at login

            resp, err := grpcCli.Authenticate(testCtx, &corev1.AuthenticateRequest{
                Username: "guest", Password: "",
            })
            Expect(err).NotTo(HaveOccurred())
            Expect(resp.Success).To(BeTrue())
            sessionID := resp.SessionId

            subCtx, subCancel := context.WithTimeout(testCtx, 10*time.Second)
            defer subCancel()
            subStream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{SessionId: sessionID})
            Expect(err).NotTo(HaveOccurred())

            // Wait for REPLAY_COMPLETE
            Eventually(func() bool {
                msg, err := subStream.Recv()
                if err != nil {
                    return false
                }
                return msg.GetControl() != nil &&
                    msg.GetControl().Signal == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE
            }, 3*time.Second, 50*time.Millisecond).Should(BeTrue())

            // Add stream mid-session
            Expect(registry.AddStream(testCtx, sessionID, "channel:new")).To(Succeed())

            // Append event to channel:new
            Expect(eventStore.Append(testCtx, core.Event{
                ID:      ulid.Make(),
                Stream:  "channel:new",
                Type:    "channel.message",
                Actor:   core.Actor{Kind: core.ActorCharacter, ID: "char-other"},
                Payload: []byte(`{"message":"mid-session msg"}`),
            })).To(Succeed())

            // Should receive the event without reconnecting
            Eventually(func() string {
                msg, err := subStream.Recv()
                if err != nil || msg.GetEvent() == nil {
                    return ""
                }
                return msg.GetEvent().Stream
            }, 3*time.Second, 50*time.Millisecond).Should(Equal("channel:new"))
        })

        It("stops forwarding messages after stream removed mid-session", func() {
            contributor.streams = []string{"channel:active"}

            resp, err := grpcCli.Authenticate(testCtx, &corev1.AuthenticateRequest{
                Username: "guest", Password: "",
            })
            Expect(err).NotTo(HaveOccurred())
            sessionID := resp.SessionId

            subCtx, subCancel := context.WithTimeout(testCtx, 10*time.Second)
            defer subCancel()
            subStream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{SessionId: sessionID})
            Expect(err).NotTo(HaveOccurred())

            // Wait for replay complete
            Eventually(func() bool {
                msg, err := subStream.Recv()
                if err != nil {
                    return false
                }
                return msg.GetControl() != nil &&
                    msg.GetControl().Signal == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE
            }, 3*time.Second, 50*time.Millisecond).Should(BeTrue())

            // Remove stream
            Expect(registry.RemoveStream(testCtx, sessionID, "channel:active")).To(Succeed())
            time.Sleep(50 * time.Millisecond) // let ctrlCh be processed

            // Append event — should NOT be forwarded
            Expect(eventStore.Append(testCtx, core.Event{
                ID:      ulid.Make(),
                Stream:  "channel:active",
                Type:    "channel.message",
                Actor:   core.Actor{Kind: core.ActorCharacter, ID: "char-other"},
                Payload: []byte(`{"message":"should not arrive"}`),
            })).To(Succeed())

            // Confirm no message on removed stream
            Consistently(func() bool {
                // Try to receive with a short deadline
                msg, err := subStream.Recv()
                if err != nil {
                    return true // stream ended — acceptable
                }
                return !(msg.GetEvent() != nil && msg.GetEvent().Stream == "channel:active")
            }, 300*time.Millisecond, 50*time.Millisecond).Should(BeTrue())
        })
    })

    Describe("LISTEN-before-replay invariant", func() {
        It("does not lose a message posted in the race window between LISTEN setup and replay", func() {
            contributor.streams = []string{"channel:race"}

            // Install afterLISTENHook to inject event during the race window
            var hookFired atomic.Bool
            // The test CoreServer needs afterLISTENHook — in integration tests this requires
            // the server to be constructed with an afterLISTENHook test option.
            // Add a CoreServerOption for testing:
            //   grpcpkg.WithAfterLISTENHook(func() { ... })
            // This hook fires after LISTEN but before replay.
            raceEventID := ulid.Make()
            // Set up the hook to fire once and append a race-window event
            // (actual hook injection requires the server be constructed in BeforeEach;
            // restructure the test to configure the hook before server construction)

            _ = hookFired
            _ = raceEventID
            // Implementation note: set the hook in BeforeEach, then reconstruct
            // the server with WithAfterLISTENHook. The hook appends to "channel:race"
            // then signals. After subscribe replay, assert the event was received.
            // This test verifies the LISTEN was set up BEFORE the event was appended.
            Pending()
        })
    })
})
```

Note: The Pending() on the race-window test is intentional — it documents the invariant test intent. Implement it fully by adding `WithAfterLISTENHook` to CoreServerOption (a `func()` field on CoreServer, already added in Task 10 as `afterLISTENHook`). Add the exported option:

```go
// WithAfterLISTENHook sets a callback fired between LISTEN setup and replay.
// For testing only.
func WithAfterLISTENHook(hook func()) CoreServerOption {
    return func(s *CoreServer) {
        s.afterLISTENHook = hook
    }
}
```

Then implement the Pending test with a server that uses `WithAfterLISTENHook`.

- [ ] **Step 3: Run integration tests**

```bash
task test:int -- ./test/integration/plugin/
```

Expected: UC1 and UC2 tests pass. The race-window test is Pending (skipped).

- [ ] **Step 4: Implement the race-window invariant test**

In `session_streams_integration_test.go`, replace the `Pending()` body:

```go
It("does not lose a message posted in the race window between LISTEN setup and replay", func() {
    raceEventID := ulid.Make()
    var raceAppended atomic.Bool

    // Rebuild the server (in BeforeEach this would be cleaner;
    // for this test reconstruct inline with the hook)
    //
    // AfterLISTENHook: append event to "channel:race" synchronously
    hookFn := func() {
        if !raceAppended.Swap(true) {
            _ = eventStore.Append(testCtx, core.Event{
                ID:      raceEventID,
                Stream:  "channel:race",
                Type:    "channel.message",
                Actor:   core.Actor{Kind: core.ActorCharacter, ID: "char-other"},
                Payload: []byte(`{"message":"race window event"}`),
            })
        }
    }
    // Reconfigure server with hook (requires server rebuild or in-place hook assignment)
    // If grpcServer was constructed in BeforeEach, add a test-only setter:
    //   grpcServer = rebuildServerWithHook(hookFn)
    // For simplicity, expose a test setter:
    //   coreServer.SetAfterLISTENHook(hookFn)
    _ = hookFn

    contributor.streams = []string{"channel:race"}

    resp, err := grpcCli.Authenticate(testCtx, &corev1.AuthenticateRequest{Username: "guest"})
    Expect(err).NotTo(HaveOccurred())

    subCtx, subCancel := context.WithTimeout(testCtx, 5*time.Second)
    defer subCancel()
    subStream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{SessionId: resp.SessionId})
    Expect(err).NotTo(HaveOccurred())

    var foundRaceEvent bool
    for {
        msg, err := subStream.Recv()
        Expect(err).NotTo(HaveOccurred())
        if msg.GetControl() != nil &&
            msg.GetControl().Signal == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE {
            break
        }
        if msg.GetEvent() != nil && msg.GetEvent().Id == raceEventID.String() {
            foundRaceEvent = true
        }
    }
    Expect(foundRaceEvent).To(BeTrue(), "race window event must be received via replay")
})
```

For the hook to work in integration tests, expose a test-only method on `CoreServer`:

```go
// SetAfterLISTENHook sets the after-LISTEN test hook. For testing only.
func (s *CoreServer) SetAfterLISTENHook(hook func()) {
    s.afterLISTENHook = hook
}
```

- [ ] **Step 5: Run integration tests again**

```bash
task test:int -- ./test/integration/plugin/
```

Expected: all tests pass, including the race-window invariant test.

- [ ] **Step 6: Run full pr-prep**

```bash
task pr-prep
```

Expected: zero failures across lint, format, schema, license, unit, integration, and E2E.

- [ ] **Step 7: Commit**

```bash
jj commit -m "test(integration): plugin session stream contribution — UC1, UC2, LISTEN invariant"
```

---

## Self-Review Notes

- All requirements FR1–FR5, NFR1–NFR3 have corresponding tasks
- Stream name validation is in Task 4 (Manager) and validated before merging
- `afterLISTENHook` / `SetAfterLISTENHook` is needed by both Task 10 and Task 12 — add in Task 10, use in Task 12
- `TestLoadPlugin` helper on Manager: check whether `DiscoveredPlugin.Manifest` is exported — if field is `manifest *Manifest` (unexported), use `Manifest *Manifest` (capitalize) or add a `TestLoadPlugin` method that injects directly. Check `bootstrap.go` to see the struct definition.
- The `oops.AsOops(err).Code()` pattern in Task 9: verify against the oops package API. Alternative: use `strings.Contains(err.Error(), "SESSION_NOT_FOUND")` for the error code check if oops doesn't expose `AsOops`.
- Binary plugin `mockPluginServiceClient` in Task 8: check if it already exists in `host_test.go`. If it wraps `pluginv1.PluginServiceClient`, add `QuerySessionStreams` to it.
- Integration test server construction: closely follow `test/integration/session/session_persistence_integration_test.go` — copy its Postgres testcontainer setup and grpc server wiring, then add `WithStreamContributor(contributor)` and `WithStreamRegistry(registry)`.

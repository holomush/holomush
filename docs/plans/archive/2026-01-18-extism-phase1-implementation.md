# Extism Plugin Framework Phase 1 Implementation Plan

**Status:** Archived (superseded by roadmap)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace wazero with Extism Go SDK and port the echo plugin to Python as proof of concept.

**Architecture:** The Extism SDK wraps wazero internally but provides automatic memory management and multi-language PDK support. We'll replace the manual `alloc`/`handle_event` protocol with Extism's built-in input/output handling. All plugin calls will be wrapped in OpenTelemetry spans for observability.

**Tech Stack:** Go, Extism Go SDK v1.x, OpenTelemetry, Python Extism PDK

---

## Prerequisites

Before starting, capture the current commit hash for the final PR review:

```bash
git rev-parse HEAD
# Save this as COMMIT_AT_START
```

---

## Task 1: Add Extism SDK Dependency

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: Add Extism Go SDK dependency**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go get github.com/extism/go-sdk@v1.7.1
```

Expected: Dependency added to go.mod

**Step 2: Verify dependency installation**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go mod tidy && grep extism go.mod
```

Expected: Line showing `github.com/extism/go-sdk v1.7.1`

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add Extism Go SDK v1.7.1"
```

---

## Task 2: Create ExtismHost Type

**Files:**

- Create: `internal/wasm/extism_host.go`
- Create: `internal/wasm/extism_host_test.go`

**Step 1: Write failing test for ExtismHost creation**

Create `internal/wasm/extism_host_test.go`:

```go
package wasm_test

import (
    "context"
    "testing"

    "github.com/holomush/holomush/internal/wasm"
    "go.opentelemetry.io/otel/trace/noop"
)

func TestExtismHost_New(t *testing.T) {
    tracer := noop.NewTracerProvider().Tracer("test")
    host := wasm.NewExtismHost(tracer)

    if host == nil {
        t.Fatal("NewExtismHost returned nil")
    }
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go test -v ./internal/wasm/... -run TestExtismHost_New
```

Expected: FAIL with undefined: wasm.NewExtismHost

**Step 3: Write minimal implementation**

Create `internal/wasm/extism_host.go`:

```go
// Package wasm provides WebAssembly plugin hosting using Extism.
package wasm

import (
    "context"
    "sync"

    extism "github.com/extism/go-sdk"
    "go.opentelemetry.io/otel/trace"
)

// ExtismHost manages Extism-based WASM plugins with OpenTelemetry tracing.
type ExtismHost struct {
    mu      sync.RWMutex
    plugins map[string]*extism.Plugin
    tracer  trace.Tracer
    closed  bool
}

// NewExtismHost creates a new ExtismHost with the provided tracer.
func NewExtismHost(tracer trace.Tracer) *ExtismHost {
    return &ExtismHost{
        plugins: make(map[string]*extism.Plugin),
        tracer:  tracer,
    }
}

// Close releases all loaded plugins.
func (h *ExtismHost) Close(ctx context.Context) error {
    h.mu.Lock()
    defer h.mu.Unlock()

    if h.closed {
        return nil
    }

    for _, p := range h.plugins {
        p.Close()
    }
    h.plugins = nil
    h.closed = true
    return nil
}
```

**Step 4: Run test to verify it passes**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go test -v ./internal/wasm/... -run TestExtismHost_New
```

Expected: PASS

**Step 5: Add test for Close**

Add to `internal/wasm/extism_host_test.go`:

```go
func TestExtismHost_Close(t *testing.T) {
    tracer := noop.NewTracerProvider().Tracer("test")
    host := wasm.NewExtismHost(tracer)

    err := host.Close(context.Background())
    if err != nil {
        t.Fatalf("Close returned error: %v", err)
    }

    // Double close should not error
    err = host.Close(context.Background())
    if err != nil {
        t.Fatalf("Double Close returned error: %v", err)
    }
}
```

**Step 6: Run test to verify it passes**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go test -v ./internal/wasm/... -run TestExtismHost_Close
```

Expected: PASS

**Step 7: Commit**

```bash
git add internal/wasm/extism_host.go internal/wasm/extism_host_test.go
git commit -m "feat(wasm): add ExtismHost type with tracer support"
```

---

## Task 3: Implement LoadPlugin Method

**Files:**

- Modify: `internal/wasm/extism_host.go`
- Modify: `internal/wasm/extism_host_test.go`

**Step 1: Write failing test for LoadPlugin**

Add to `internal/wasm/extism_host_test.go`:

```go
func TestExtismHost_LoadPlugin(t *testing.T) {
    tracer := noop.NewTracerProvider().Tracer("test")
    host := wasm.NewExtismHost(tracer)
    defer host.Close(context.Background())

    // Minimal valid Extism plugin WASM (exports handle_event)
    // This will be replaced with actual test fixture
    err := host.LoadPlugin(context.Background(), "test-plugin", minimalExtismWASM)
    if err != nil {
        t.Fatalf("LoadPlugin failed: %v", err)
    }

    if !host.HasPlugin("test-plugin") {
        t.Error("HasPlugin returned false for loaded plugin")
    }
}

func TestExtismHost_LoadPlugin_InvalidWASM(t *testing.T) {
    tracer := noop.NewTracerProvider().Tracer("test")
    host := wasm.NewExtismHost(tracer)
    defer host.Close(context.Background())

    err := host.LoadPlugin(context.Background(), "bad", []byte("not wasm"))
    if err == nil {
        t.Error("LoadPlugin should fail for invalid WASM")
    }
}

func TestExtismHost_LoadPlugin_AfterClose(t *testing.T) {
    tracer := noop.NewTracerProvider().Tracer("test")
    host := wasm.NewExtismHost(tracer)
    host.Close(context.Background())

    err := host.LoadPlugin(context.Background(), "test", minimalExtismWASM)
    if err == nil {
        t.Error("LoadPlugin should fail after Close")
    }
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go test -v ./internal/wasm/... -run TestExtismHost_LoadPlugin 2>&1 | head -20
```

Expected: FAIL with undefined: host.LoadPlugin

**Step 3: Write LoadPlugin implementation**

Add to `internal/wasm/extism_host.go`:

```go
import (
    "context"
    "errors"
    "fmt"
    "sync"

    extism "github.com/extism/go-sdk"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/trace"
)

// ErrHostClosed is returned when operations are attempted on a closed host.
var ErrHostClosed = errors.New("plugin host is closed")

// LoadPlugin loads a WASM plugin with the given name and binary.
func (h *ExtismHost) LoadPlugin(ctx context.Context, name string, wasmBytes []byte) error {
    ctx, span := h.tracer.Start(ctx, "ExtismHost.LoadPlugin",
        trace.WithAttributes(attribute.String("plugin.name", name)))
    defer span.End()

    h.mu.Lock()
    defer h.mu.Unlock()

    if h.closed {
        return ErrHostClosed
    }

    manifest := extism.Manifest{
        Wasm: []extism.Wasm{
            extism.WasmData{Data: wasmBytes},
        },
    }

    config := extism.PluginConfig{
        EnableWasi: true,
    }

    plugin, err := extism.NewPlugin(ctx, manifest, config, nil)
    if err != nil {
        return fmt.Errorf("failed to create plugin %s: %w", name, err)
    }

    h.plugins[name] = plugin
    return nil
}

// HasPlugin returns true if a plugin with the given name is loaded.
func (h *ExtismHost) HasPlugin(name string) bool {
    h.mu.RLock()
    defer h.mu.RUnlock()
    _, ok := h.plugins[name]
    return ok
}
```

**Step 4: Create test fixtures**

Create `internal/wasm/testdata/` directory and we'll need a minimal Extism plugin. For now, add a placeholder test variable:

Add at top of `internal/wasm/extism_host_test.go`:

```go
// minimalExtismWASM is a minimal valid Extism plugin that exports handle_event.
// Generated using: extism-js compile -o minimal.wasm minimal.js
// Where minimal.js contains: export function handle_event() { return 0; }
var minimalExtismWASM = []byte{
    0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // WASM header
    0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // Type section: () -> void
    0x03, 0x02, 0x01, 0x00, // Function section
    0x07, 0x10, 0x01, 0x0c, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x5f, 0x65, 0x76, 0x65, 0x6e, 0x74, 0x00, 0x00, // Export "handle_event"
    0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b, // Code section: empty function
}
```

Note: This minimal WASM may need adjustment based on Extism's actual requirements. The test may fail and we'll adjust.

**Step 5: Run tests and adjust**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go test -v ./internal/wasm/... -run TestExtismHost_LoadPlugin
```

Expected: Tests pass or we'll need to adjust the minimal WASM fixture

**Step 6: Commit**

```bash
git add internal/wasm/extism_host.go internal/wasm/extism_host_test.go
git commit -m "feat(wasm): add LoadPlugin method with OTel tracing"
```

---

## Task 4: Implement DeliverEvent Method

**Files:**

- Modify: `internal/wasm/extism_host.go`
- Modify: `internal/wasm/extism_host_test.go`

**Step 1: Write failing test for DeliverEvent**

Add to `internal/wasm/extism_host_test.go`:

```go
import (
    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/pkg/plugin"
    "github.com/oklog/ulid/v2"
    "time"
)

func TestExtismHost_DeliverEvent(t *testing.T) {
    tracer := noop.NewTracerProvider().Tracer("test")
    host := wasm.NewExtismHost(tracer)
    defer host.Close(context.Background())

    err := host.LoadPlugin(context.Background(), "echo", echoPluginWASM)
    if err != nil {
        t.Fatalf("LoadPlugin failed: %v", err)
    }

    event := core.Event{
        ID:        ulid.Make(),
        Stream:    "location:test",
        Type:      core.EventTypeSay,
        Timestamp: time.Now(),
        Actor:     core.Actor{Kind: core.ActorKindCharacter, ID: "char1"},
        Payload:   []byte(`{"message":"hello"}`),
    }

    emitted, err := host.DeliverEvent(context.Background(), "echo", event)
    if err != nil {
        t.Fatalf("DeliverEvent failed: %v", err)
    }

    // Echo plugin should emit one event
    if len(emitted) != 1 {
        t.Fatalf("expected 1 emitted event, got %d", len(emitted))
    }
}

func TestExtismHost_DeliverEvent_PluginNotFound(t *testing.T) {
    tracer := noop.NewTracerProvider().Tracer("test")
    host := wasm.NewExtismHost(tracer)
    defer host.Close(context.Background())

    event := core.Event{
        ID:     ulid.Make(),
        Stream: "location:test",
        Type:   core.EventTypeSay,
    }

    _, err := host.DeliverEvent(context.Background(), "nonexistent", event)
    if err == nil {
        t.Error("DeliverEvent should fail for nonexistent plugin")
    }
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go test -v ./internal/wasm/... -run TestExtismHost_DeliverEvent 2>&1 | head -20
```

Expected: FAIL with undefined: host.DeliverEvent

**Step 3: Write DeliverEvent implementation**

Add to `internal/wasm/extism_host.go`:

```go
import (
    "encoding/json"

    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/pkg/plugin"
)

// ErrPluginNotFound is returned when the requested plugin is not loaded.
var ErrPluginNotFound = errors.New("plugin not found")

// DeliverEvent sends an event to a plugin and returns any emitted events.
func (h *ExtismHost) DeliverEvent(ctx context.Context, pluginName string, event core.Event) ([]plugin.EmitEvent, error) {
    ctx, span := h.tracer.Start(ctx, "ExtismHost.DeliverEvent",
        trace.WithAttributes(
            attribute.String("plugin.name", pluginName),
            attribute.String("event.type", event.Type.String()),
            attribute.String("event.stream", event.Stream),
        ))
    defer span.End()

    h.mu.RLock()
    p, ok := h.plugins[pluginName]
    h.mu.RUnlock()

    if !ok {
        return nil, fmt.Errorf("%w: %s", ErrPluginNotFound, pluginName)
    }

    // Convert core.Event to plugin.Event
    pluginEvent := plugin.Event{
        ID:        event.ID.String(),
        Stream:    event.Stream,
        Type:      plugin.EventType(event.Type.String()),
        Timestamp: event.Timestamp.UnixMilli(),
        ActorKind: plugin.ActorKind(event.Actor.Kind),
        ActorID:   event.Actor.ID,
        Payload:   string(event.Payload),
    }

    eventJSON, err := json.Marshal(pluginEvent)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal event: %w", err)
    }

    // Call plugin's handle_event function
    _, output, err := p.Call("handle_event", eventJSON)
    if err != nil {
        return nil, fmt.Errorf("plugin call failed: %w", err)
    }

    // Empty output means no events to emit
    if len(output) == 0 {
        return nil, nil
    }

    // Parse response
    var response plugin.Response
    if err := json.Unmarshal(output, &response); err != nil {
        return nil, fmt.Errorf("failed to unmarshal response: %w", err)
    }

    return response.Events, nil
}
```

**Step 4: Run tests**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go test -v ./internal/wasm/... -run TestExtismHost_DeliverEvent
```

Expected: Tests pass (may need to create echo plugin fixture first)

**Step 5: Commit**

```bash
git add internal/wasm/extism_host.go internal/wasm/extism_host_test.go
git commit -m "feat(wasm): add DeliverEvent method with OTel tracing"
```

---

## Task 5: Create Python Echo Plugin

**Files:**

- Create: `plugins/echo-python/plugin.py`
- Create: `plugins/echo-python/Makefile`
- Create: `plugins/echo-python/README.md`

**Step 1: Set up Python PDK environment**

Run:

```bash
mkdir -p /Volumes/Code/github.com/holomush/holomush/plugins/echo-python
```

**Step 2: Write the echo plugin in Python**

Create `plugins/echo-python/plugin.py`:

```python
"""Echo plugin - responds to say events with echoed message."""

import json
import extism


def handle_event():
    """Handle incoming events and emit echo responses."""
    # Read input event
    event_json = extism.input_str()
    event = json.loads(event_json)

    # Only respond to "say" events from characters (not plugins)
    if event.get("type") != "say":
        return

    if event.get("actor_kind") == 2:  # ActorKindPlugin
        return

    # Extract message from payload
    payload = json.loads(event.get("payload", "{}"))
    message = payload.get("message", "")

    if not message:
        return

    # Create echo response
    response = {
        "events": [{
            "stream": event.get("stream"),
            "type": "say",
            "payload": json.dumps({"message": f"Echo: {message}"})
        }]
    }

    extism.output_str(json.dumps(response))


extism.plugin_fn(handle_event)
```

**Step 3: Create Makefile for building**

Create `plugins/echo-python/Makefile`:

```makefile
.PHONY: build clean

# Requires: pip install extism-cli
build:
    extism-py plugin.py -o echo.wasm

clean:
    rm -f echo.wasm
```

**Step 4: Create README**

Create `plugins/echo-python/README.md` with content describing:

- Title: "Echo Plugin (Python)"
- Building section with `pip install extism-cli` and `make build`
- Behavior: Listens for `say` events, ignores plugin events, responds with "Echo: <message>"

**Step 5: Build the plugin (if extism-py available)**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush/plugins/echo-python && \
  pip install extism-cli 2>/dev/null || true && \
  extism-py plugin.py -o echo.wasm 2>/dev/null || echo "Build requires extism-cli"
```

Expected: echo.wasm created or message about requiring extism-cli

**Step 6: Commit**

```bash
git add plugins/echo-python/
git commit -m "feat(plugins): add Python echo plugin using Extism PDK"
```

---

## Task 6: Create Test WASM Fixtures

**Files:**

- Create: `internal/wasm/testdata/echo.wasm` (built from Python)
- Modify: `internal/wasm/extism_host_test.go`

**Step 1: Generate minimal test WASM using wat2wasm**

We need a minimal valid Extism plugin for tests. Create WAT source:

Create `internal/wasm/testdata/minimal.wat`:

```wat
(module
  ;; Import Extism host functions
  (import "extism:host/env" "input_length" (func $input_length (result i64)))
  (import "extism:host/env" "input_load_u8" (func $input_load_u8 (param i64) (result i32)))
  (import "extism:host/env" "output_set" (func $output_set (param i64 i64)))
  (import "extism:host/env" "alloc" (func $alloc (param i64) (result i64)))

  ;; Memory
  (memory (export "memory") 1)

  ;; handle_event - returns empty response
  (func (export "handle_event") (result i32)
    i32.const 0
  )
)
```

**Step 2: Convert WAT to WASM**

Run:

```bash
mkdir -p /Volumes/Code/github.com/holomush/holomush/internal/wasm/testdata
# Use wat2wasm if available, or create Go test that embeds pre-built WASM
```

**Step 3: Update test to use fixture**

Update `internal/wasm/extism_host_test.go`:

```go
import (
    _ "embed"
)

//go:embed testdata/minimal.wasm
var minimalExtismWASM []byte

//go:embed testdata/echo.wasm
var echoPluginWASM []byte
```

**Step 4: Run tests**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go test -v ./internal/wasm/... -run TestExtismHost
```

Expected: All tests pass

**Step 5: Commit**

```bash
git add internal/wasm/testdata/ internal/wasm/extism_host_test.go
git commit -m "test(wasm): add Extism test fixtures"
```

---

## Task 7: Add ExtismSubscriber for Event Integration

**Files:**

- Create: `internal/wasm/extism_subscriber.go`
- Create: `internal/wasm/extism_subscriber_test.go`

**Step 1: Write failing test for ExtismSubscriber**

Create `internal/wasm/extism_subscriber_test.go`:

```go
package wasm_test

import (
    "context"
    "testing"
    "time"

    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/internal/wasm"
    "github.com/oklog/ulid/v2"
    "go.opentelemetry.io/otel/trace/noop"
)

type mockEmitter struct {
    emitted []core.Event
}

func (m *mockEmitter) Emit(ctx context.Context, stream string, eventType core.EventType, payload []byte) error {
    m.emitted = append(m.emitted, core.Event{
        Stream:  stream,
        Type:    eventType,
        Payload: payload,
    })
    return nil
}

func TestExtismSubscriber_HandleEvent(t *testing.T) {
    tracer := noop.NewTracerProvider().Tracer("test")
    host := wasm.NewExtismHost(tracer)
    defer host.Close(context.Background())

    err := host.LoadPlugin(context.Background(), "echo", echoPluginWASM)
    if err != nil {
        t.Fatalf("LoadPlugin failed: %v", err)
    }

    emitter := &mockEmitter{}
    sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)

    sub.Subscribe("echo", "location:*")

    event := core.Event{
        ID:        ulid.Make(),
        Stream:    "location:test",
        Type:      core.EventTypeSay,
        Timestamp: time.Now(),
        Actor:     core.Actor{Kind: core.ActorKindCharacter, ID: "char1"},
        Payload:   []byte(`{"message":"hello"}`),
    }

    sub.HandleEvent(context.Background(), event)

    // Wait for async processing
    time.Sleep(100 * time.Millisecond)

    if len(emitter.emitted) != 1 {
        t.Fatalf("expected 1 emitted event, got %d", len(emitter.emitted))
    }
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go test -v ./internal/wasm/... -run TestExtismSubscriber 2>&1 | head -20
```

Expected: FAIL with undefined: wasm.NewExtismSubscriber

**Step 3: Write ExtismSubscriber implementation**

Create `internal/wasm/extism_subscriber.go`:

```go
package wasm

import (
    "context"
    "log/slog"
    "strings"
    "sync"
    "time"

    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/pkg/plugin"
)

// Emitter is the interface for emitting events back to the system.
type Emitter interface {
    Emit(ctx context.Context, stream string, eventType core.EventType, payload []byte) error
}

// ExtismSubscriber routes events to Extism plugins.
type ExtismSubscriber struct {
    host          *ExtismHost
    emitter       Emitter
    mu            sync.RWMutex
    subscriptions map[string][]string // plugin -> stream patterns
    wg            sync.WaitGroup
    ctx           context.Context
    cancel        context.CancelFunc
}

// NewExtismSubscriber creates a subscriber for routing events to plugins.
// The provided context controls the subscriber's lifecycle; when cancelled,
// no new goroutines will be spawned for event handling.
func NewExtismSubscriber(ctx context.Context, host *ExtismHost, emitter Emitter) *ExtismSubscriber {
    ctx, cancel := context.WithCancel(ctx)
    return &ExtismSubscriber{
        host:          host,
        emitter:       emitter,
        subscriptions: make(map[string][]string),
        ctx:           ctx,
        cancel:        cancel,
    }
}

// Subscribe registers a plugin to receive events matching the stream pattern.
func (s *ExtismSubscriber) Subscribe(pluginName, streamPattern string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.subscriptions[pluginName] = append(s.subscriptions[pluginName], streamPattern)
}

// HandleEvent delivers an event to all subscribed plugins.
func (s *ExtismSubscriber) HandleEvent(ctx context.Context, event core.Event) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    for pluginName, patterns := range s.subscriptions {
        if !s.matchesAny(event.Stream, patterns) {
            continue
        }

        go s.deliverWithTimeout(ctx, pluginName, event)
    }
}

func (s *ExtismSubscriber) matchesAny(stream string, patterns []string) bool {
    for _, pattern := range patterns {
        if s.matchPattern(stream, pattern) {
            return true
        }
    }
    return false
}

func (s *ExtismSubscriber) matchPattern(stream, pattern string) bool {
    // Simple glob matching: "location:*" matches "location:anything"
    if strings.HasSuffix(pattern, "*") {
        prefix := strings.TrimSuffix(pattern, "*")
        return strings.HasPrefix(stream, prefix)
    }
    return stream == pattern
}

func (s *ExtismSubscriber) deliverWithTimeout(ctx context.Context, pluginName string, event core.Event) {
    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    emitted, err := s.host.DeliverEvent(ctx, pluginName, event)
    if err != nil {
        slog.Error("plugin event delivery failed",
            "plugin", pluginName,
            "event_type", event.Type,
            "error", err)
        return
    }

    // Emit any events the plugin generated
    for _, emit := range emitted {
        eventType, err := core.ParseEventType(string(emit.Type))
        if err != nil {
            slog.Error("invalid event type from plugin",
                "plugin", pluginName,
                "type", emit.Type,
                "error", err)
            continue
        }

        if err := s.emitter.Emit(ctx, emit.Stream, eventType, []byte(emit.Payload)); err != nil {
            slog.Error("failed to emit plugin event",
                "plugin", pluginName,
                "error", err)
        }
    }
}
```

**Step 4: Run tests**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go test -v ./internal/wasm/... -run TestExtismSubscriber
```

Expected: Tests pass

**Step 5: Commit**

```bash
git add internal/wasm/extism_subscriber.go internal/wasm/extism_subscriber_test.go
git commit -m "feat(wasm): add ExtismSubscriber for event routing"
```

---

## Task 8: Run Lint and Test Coverage

**Files:**

- None (verification only)

**Step 1: Run linter**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && task lint
```

Expected: No lint errors

**Step 2: Run tests with coverage**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && task test
```

Expected: All tests pass, coverage >80%

**Step 3: Fix any issues found**

If lint or test failures, fix them before proceeding.

**Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix: address lint and test issues"
```

---

## Task 9: Update Dependencies File

**Files:**

- Modify: `cmd/holomush/deps.go`

**Step 1: Check if deps.go needs Extism import**

Read the current deps.go and determine if it needs the Extism import for build tags.

**Step 2: Add Extism import if needed**

If deps.go manages explicit imports for build, add:

```go
import (
    _ "github.com/extism/go-sdk"
)
```

**Step 3: Run go mod tidy**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go mod tidy
```

Expected: go.mod and go.sum are clean

**Step 4: Commit**

```bash
git add cmd/holomush/deps.go go.mod go.sum
git commit -m "chore: add Extism to deps imports"
```

---

## Task 10: Final Integration Test

**Files:**

- Create: `internal/wasm/integration_test.go`

**Step 1: Write integration test**

Create `internal/wasm/integration_test.go`:

```go
//go:build integration

package wasm_test

import (
    "context"
    "testing"
    "time"

    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/internal/wasm"
    "github.com/oklog/ulid/v2"
    "go.opentelemetry.io/otel/trace/noop"
)

func TestExtism_Integration(t *testing.T) {
    tracer := noop.NewTracerProvider().Tracer("test")
    host := wasm.NewExtismHost(tracer)
    defer host.Close(context.Background())

    // Load echo plugin
    err := host.LoadPlugin(context.Background(), "echo", echoPluginWASM)
    if err != nil {
        t.Fatalf("LoadPlugin failed: %v", err)
    }

    // Create subscriber with mock emitter
    emitter := &mockEmitter{}
    sub := wasm.NewExtismSubscriber(context.Background(), host, emitter)
    sub.Subscribe("echo", "location:*")

    // Send a say event
    event := core.Event{
        ID:        ulid.Make(),
        Stream:    "location:room1",
        Type:      core.EventTypeSay,
        Timestamp: time.Now(),
        Actor:     core.Actor{Kind: core.ActorKindCharacter, ID: "player1"},
        Payload:   []byte(`{"message":"Hello, world!"}`),
    }

    sub.HandleEvent(context.Background(), event)

    // Wait for async processing
    time.Sleep(200 * time.Millisecond)

    // Verify echo response
    if len(emitter.emitted) != 1 {
        t.Fatalf("expected 1 emitted event, got %d", len(emitter.emitted))
    }

    emittedEvent := emitter.emitted[0]
    if emittedEvent.Type != core.EventTypeSay {
        t.Errorf("expected say event, got %s", emittedEvent.Type)
    }

    // Verify payload contains "Echo:"
    if !strings.Contains(string(emittedEvent.Payload), "Echo:") {
        t.Errorf("expected echo in payload, got %s", emittedEvent.Payload)
    }
}
```

**Step 2: Run integration test**

Run:

```bash
cd /Volumes/Code/github.com/holomush/holomush && go test -v -tags=integration ./internal/wasm/... -run TestExtism_Integration
```

Expected: Integration test passes

**Step 3: Commit**

```bash
git add internal/wasm/integration_test.go
git commit -m "test(wasm): add Extism integration test"
```

---

## Task 11: Update Documentation

**Important:** Follow `docs/CLAUDE.md` for all markdown standards (code fence languages, no hard tabs, blank lines around lists/fences).

**Files:**

- Modify: `docs/plans/2026-01-17-holomush-architecture-design.md`
- Create: `docs/reference/plugin-authoring.md`
- Reference: `docs/CLAUDE.md` (documentation guidelines)

**Step 1: Update architecture documentation**

Update the architecture design doc to reflect Extism instead of raw wazero:

- Change WASM runtime description to "Extism (wraps wazero)"
- Update plugin protocol from "Custom alloc/handle_event" to "Extism PDK exports"
- Update memory management from "Manual ptr/len" to "Automatic"
- Update language support from "TinyGo only" to "Python, JS, Go, Rust"

Review all other system documentation for consistency with this change.

**Step 2: Create plugin authoring guide**

Create `docs/reference/plugin-authoring.md` covering:

1. **Overview**: HoloMUSH plugins are WebAssembly modules using Extism PDK
2. **Quick Start (Python)**: Install CLI, register with `extism.plugin_fn(handle_event)`, build with `extism-py`
3. **Event Structure**: JSON with id, stream, type, timestamp, actor_kind (0=Character, 1=System, 2=Plugin), actor_id, payload
4. **Event Types**: say, pose, arrive, leave, system
5. **Response Structure**: JSON with events array containing stream, type, payload
6. **Avoiding Echo Loops**: Check `actor_kind == 2` to skip plugin events
7. **Testing Locally**: Use `extism call plugin.wasm handle_event --stdin`

**Step 3: Verify documentation accuracy**

Run:

```bash
# Verify markdown lint passes
task lint:markdown

# Verify Python example compiles (if extism-py available)
cd /Volumes/Code/github.com/holomush/holomush/plugins/echo-python && \
  extism-py plugin.py -o /tmp/test.wasm 2>/dev/null || echo "extism-py not installed"
```

Expected: Lint passes, build succeeds or message about extism-py not installed

**Step 4: Commit**

```bash
git add docs/plans/2026-01-17-holomush-architecture-design.md docs/reference/plugin-authoring.md
git commit -m "docs: add plugin authoring guide and update architecture for Extism"
```

---

## Post-Implementation

After completing all tasks:

1. Record the final commit hash as COMMIT_AT_END
2. The PR review will compare COMMIT_AT_START..COMMIT_AT_END
3. Ensure all tests pass: `task test`
4. Ensure lint passes: `task lint`
5. Ensure coverage target met: `task test:coverage`

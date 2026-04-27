# Gateway Verb Registry Sourcing — Phase 1.6 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the gateway-side verb-registry gap surfaced by Phase 1 by making rendering metadata flow on the wire alongside each event, eliminating the gateway's local `VerbRegistry`, and codifying gateway thinness as a CI-enforced invariant.

**Architecture:** Approach B — embed `RenderingMetadata` in `EventFrame` and `eventbusv1.Event`. A new `eventbus.RenderingPublisher` wraps the underlying `eventbus.Publisher` and is the single enrichment site, stamping both the proto envelope field and a `App-Rendering` NATS header (protojson form) at emit time. The audit projection reads the header to populate a new `events_audit.rendering` JSONB column. The gateway becomes domain-stateless and is enforced by an import-graph tripwire test.

**Tech Stack:** Go 1.26, Protobuf (buf), `buf.build/go/protovalidate`, NATS JetStream, PostgreSQL, jj-colocated VCS, `task` task runner, gRPC + ConnectRPC, Ginkgo for integration tests, testify for unit tests.

**Spec:** [`docs/superpowers/specs/2026-04-26-gateway-verb-registry-sourcing.md`](../specs/2026-04-26-gateway-verb-registry-sourcing.md)

**Branch / chain head:** `feat/crypto-phase1`. The spec lives at commit `uoy` and this plan at commit `yks` (descendants of `lxn`, the Phase 1 head). All implementation tasks add commits on top of the plan commit.

**Plan size:** 41 distinct tasks (Task 0 pre-flight survey + Tasks 1–24, 26–41; Task 25 was merged into Task 24 during plan revision and the number is reserved). 13 phases (P0 pre-flight + P1–P12 implementation/verification).

**Phase-execution checkpoints:** at each phase boundary, run `task lint && task test` before continuing. Don't run `task pr-prep` until Phase P12 — earlier phases intentionally leave intermediate failures (e.g. tests that still call the old `RegisterBuiltinTypes`).

---

## File Structure

### New files

| Path | Purpose |
|---|---|
| `internal/eventbus/rendering_publisher.go` | New `RenderingPublisher` wrapper type |
| `internal/eventbus/rendering_publisher_test.go` | Unit tests for `INV-GW-2`, `INV-GW-3`, `INV-GW-4`, `INV-GW-9`, `INV-GW-15` |
| `internal/eventbus/types_proto_sync_test.go` | `INV-GW-14` parity test (Go struct ↔ proto) |
| `internal/store/migrations/000012_events_audit_rendering.up.sql` | Add `rendering JSONB NOT NULL` |
| `internal/store/migrations/000012_events_audit_rendering.down.sql` | Reverse |
| `cmd/holomush/gateway_imports_test.go` | `INV-GW-1` import-graph tripwire |
| `internal/core/exports_test.go` | `INV-GW-11` export check (`RegisterBuiltinTypes` is unexported) |
| `test/integration/eventbus_e2e/rendering_completeness_test.go` | `INV-GW-6` + `INV-GW-13` |
| `test/integration/gateway_invariants/meta_test.go` | `TestAllGatewayRegistryInvariantsHaveTests` meta-test |
| `site/docs/contributing/gateway-boundary.md` | Codifies `INV-GW-1` for contributors |
| `site/docs/contributing/event-emit-pipeline.md` | Publisher-wiring story |
| `site/docs/extending/verb-registration.md` | Plugin-author guide |

### Modified files

| Path | Modification |
|---|---|
| `api/proto/holomush/core/v1/core.proto` | Add `RenderingMetadata`, `EventChannel`, `EventFrame.rendering` |
| `api/proto/holomush/eventbus/v1/eventbus.proto` | Add `Event.rendering` (imports `core/v1`) |
| `internal/eventbus/types.go` | Add `Rendering`, `Headers`, `RenderingMetadata`, `EventChannel` |
| `internal/eventbus/publisher.go` | Copy `Rendering` into proto envelope; merge `Headers` into `nats.Msg.Header`; conversion helpers |
| `internal/eventbus/audit/projection.go` | Read `App-Rendering` header; write `events_audit.rendering` |
| `internal/eventbus/history/cold_postgres.go` | Read `events_audit.rendering` JSONB; populate `Event.Rendering` |
| `internal/core/registry.go` | Add `SourceInfo`, `RegisterWithSource`, `SourceVersion` |
| `internal/core/builtins.go` | `BootstrapVerbRegistry`, unexport `registerBuiltinTypes`, update entries to `corev1.EventChannel` |
| `internal/core/registry_test.go` | Call lowercase `registerBuiltinTypes` |
| `internal/plugin/manager.go` | Required `VerbRegistry` (constructor check); use `RegisterWithSource` |
| `internal/grpc/server.go` | `toProtoSubscribeResponse` copies `Rendering` to `EventFrame` |
| `internal/grpc/query_stream_history.go` | EventFrame builders copy `Rendering` |
| `cmd/holomush/core.go` | Call `BootstrapVerbRegistry(version)` |
| `cmd/holomush/sub_grpc.go` | Construct `RenderingPublisher`; wire into both consumers; AC#10 unit test |
| `cmd/holomush/gateway.go` | Delete `VerbRegistry` construction; delete duplicate Prometheus metric registration |
| `cmd/holomush/gateway_test.go` | Rework EventFrame fixtures; drop registry calls |
| `internal/web/handler.go` | Drop `WithVerbRegistry` option |
| `internal/web/translate.go` | Read `ev.GetRendering()`; nil-drop with metric+log; `corev1→webv1` conversion |
| `internal/web/translate_test.go` | Rework fixtures |
| `internal/telnet/gateway_handler.go` | Read `ev.GetRendering()`; nil-drop |
| `internal/telnet/gateway_handler_test.go` | Rework fixtures |
| `test/integration/plugin/verb_registration_test.go` | Use `BootstrapVerbRegistry("test")` |
| `site/docs/contributing/architecture.md` | Update gateway-boundary section |
| `site/docs/operating/plugin-reloads.md` | Add `source_plugin_version` runbook section |
| (various plugin manager test sites) | Migrate to seeded/empty registry per Section 5 decision table |

---

## PHASE P0 — PRE-FLIGHT SURVEY

### Task 0: Survey plugin manager construction sites

**Files:**

- Read-only: scan `internal/plugin/manager.go`, `internal/plugin/setup/`, `internal/plugin/goplugin/`, `cmd/holomush/sub_grpc.go`, all `*_test.go` under `internal/plugin/` and `test/integration/plugin/`
- Create: `docs/superpowers/plans/2026-04-26-task-0-survey.md` (working notes — committed for traceability, not shipped to site/)

- [ ] **Step 1: Enumerate every `plugins.NewManager(...)` / `plugin.NewManager(...)` call site**

Run:

```bash
rg -n 'plugins?\.NewManager\(' --type go
rg -n 'NewManager\(' internal/plugin/ --type go
```

For each hit, record: file, line, whether `WithVerbRegistry(...)` is among the options, what type of test it is (pure construction / emit / manifest-load / builtin emit).

- [ ] **Step 2: Classify each site per spec Section 5 decision table**

For every site without `WithVerbRegistry`, write the decision: empty `core.NewVerbRegistry()`, seeded `core.BootstrapVerbRegistry("test")` (after Task 5), or seeded-plus-explicit-plugin-verb-registration. **Default to seeded if uncertain.**

Write the survey to `docs/superpowers/plans/2026-04-26-task-0-survey.md` with one line per site. This document is the input to Task 20 (the plugin manager tightening).

- [ ] **Step 3: Commit the survey**

```bash
jj describe -m "plan(crypto-phase1.6): pre-flight survey of plugin manager construction sites"
jj new
```

---

## PHASE P1 — PROTO FOUNDATION

### Task 1: Add `RenderingMetadata` + `EventChannel` to `corev1`

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto`

- [ ] **Step 1: Add `RenderingMetadata` message and `EventChannel` enum**

Edit `api/proto/holomush/core/v1/core.proto`. After the existing `EventFrame` message, add:

```proto
// EventChannel identifies the destination channel for event delivery.
// This is the canonical internal definition; webv1.EventChannel is kept
// in lockstep for the web wire format (INV-GW-16).
enum EventChannel {
  EVENT_CHANNEL_UNSPECIFIED = 0;
  EVENT_CHANNEL_TERMINAL    = 1;
  EVENT_CHANNEL_STATE       = 2;
  EVENT_CHANNEL_BOTH        = 3;
}

// RenderingMetadata carries cleartext rendering instructions for an event.
// Populated by RenderingPublisher.Publish at emit time from the verb
// registry. See docs/superpowers/specs/2026-04-26-gateway-verb-registry-sourcing.md.
message RenderingMetadata {
  option (buf.validate.message).cel = {
    id: "rendering_metadata.label_required_for_speech"
    message: "label must be set when format is 'speech'"
    expression: "this.format != 'speech' || this.label != ''"
  };

  // Category drives client-side renderer routing.
  string category = 1 [(buf.validate.field).string.min_len = 1];

  // Format drives within-category presentation.
  string format = 2 [(buf.validate.field).string.min_len = 1];

  // Label provides type-specific display text. Required when format == "speech".
  string label = 3;

  // DisplayTarget routes the event to TERMINAL, STATE, or BOTH on the client.
  EventChannel display_target = 4 [(buf.validate.field).enum = {
    defined_only: true
    not_in: [0]
  }];

  // SourcePlugin names the plugin that owns this event type, or "builtin"
  // for host-owned types. Recorded for historical/audit fidelity.
  string source_plugin = 5 [(buf.validate.field).string.min_len = 1];

  // SourcePluginVersion is the manifest's version field, or "host-<binary
  // version>" for builtins. Recorded for historical/audit fidelity.
  string source_plugin_version = 6 [(buf.validate.field).string.min_len = 1];
}
```

Ensure `import "buf/validate/validate.proto";` is at the top of the file. If not present, add it.

- [ ] **Step 2: Run `task proto:gen` (or `buf generate`) and verify the new types appear**

```bash
task proto:gen
ls -la pkg/proto/holomush/core/v1/core.pb.go
rg -n 'RenderingMetadata|EventChannel_EVENT' pkg/proto/holomush/core/v1/core.pb.go | head -10
```

Expected: generated Go contains `RenderingMetadata` struct, getters, and `EventChannel_*` constants.

- [ ] **Step 3: Verify protovalidate compiled the message-level CEL rule**

```bash
rg -n 'rendering_metadata.label_required_for_speech' pkg/proto/holomush/core/v1/ 2>/dev/null
```

Expected: at least one match in generated Go (the CEL constraint registration).

If `buf generate` fails on the message-level CEL rule, see spec Section 2 "CEL precedent" — verify `buf.build/go/protovalidate v1.1.3` is up to date in `go.mod` and that `buf/validate/validate.proto` is imported.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(proto): add corev1.RenderingMetadata and EventChannel"
jj new
```

---

#### Task 2: Add `EventFrame.rendering` field

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto`

- [ ] **Step 1: Add `rendering` field to `EventFrame`**

Edit `api/proto/holomush/core/v1/core.proto`. In the `EventFrame` message, after the `bytes cursor = 8;` line, add:

```proto
  // Rendering metadata — cleartext band, populated by RenderingPublisher
  // at emit time. MUST be present on every frame produced by this server
  // (INV-GW-2). Gateway treats absence as a contract violation
  // (drops + metric + log per INV-GW-5).
  RenderingMetadata rendering = 9;
```

- [ ] **Step 2: Run `task proto:gen`**

```bash
task proto:gen
rg -n 'GetRendering\|Rendering\s*\*RenderingMetadata' pkg/proto/holomush/core/v1/core.pb.go | head -5
```

Expected: generated Go has `GetRendering()` accessor and `Rendering *RenderingMetadata` field on `EventFrame`.

- [ ] **Step 3: Verify build still passes**

```bash
task lint
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(proto): add EventFrame.rendering sub-message"
jj new
```

---

#### Task 3: Add `Event.rendering` field to `eventbusv1`

**Files:**

- Modify: `api/proto/holomush/eventbus/v1/eventbus.proto`

- [ ] **Step 1: Import `core/v1` and add the field**

Edit `api/proto/holomush/eventbus/v1/eventbus.proto`. At the top with other imports, add:

```proto
import "holomush/core/v1/core.proto";
```

Then in the `Event` message, after `bytes payload = 6;`, add:

```proto
  // Rendering metadata, populated by RenderingPublisher.Publish before
  // marshaling for JetStream. Mirrors the corev1.RenderingMetadata used
  // on the gRPC Subscribe wire (one schema, two transports).
  holomush.core.v1.RenderingMetadata rendering = 7;
```

- [ ] **Step 2: Run `task proto:gen`**

```bash
task proto:gen
rg -n 'GetRendering' pkg/proto/holomush/eventbus/v1/eventbus.pb.go | head -3
```

Expected: `GetRendering()` accessor on the generated `Event` type.

- [ ] **Step 3: Verify build still passes**

```bash
task lint
```

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(proto): add eventbusv1.Event.rendering field"
jj new
```

---

## PHASE P2 — VERB REGISTRY EXTENSIONS

### Task 4: Add `SourceInfo` + `RegisterWithSource` + `SourceVersion`

**Files:**

- Modify: `internal/core/registry.go`
- Test: `internal/core/registry_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/core/registry_test.go`:

```go
func TestRegisterWithSourceRecordsVersion(t *testing.T) {
    r := NewVerbRegistry()
    err := r.RegisterWithSource(VerbRegistration{
        Type:          "core-communication:say",
        Category:      "communication",
        Format:        "speech",
        Label:         "says",
        DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
        Source:        "core-communication",
    }, "0.1.0")
    require.NoError(t, err)

    assert.Equal(t, "0.1.0", r.SourceVersion("core-communication"))
    assert.Equal(t, "", r.SourceVersion("nonexistent-plugin"))
}

func TestRegisterFallsBackToEmptyVersion(t *testing.T) {
    r := NewVerbRegistry()
    err := r.Register(VerbRegistration{
        Type:          "core-objects:object_create",
        Category:      "state",
        Format:        "delta",
        DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_STATE,
        Source:        "core-objects",
    })
    require.NoError(t, err)
    assert.Equal(t, "", r.SourceVersion("core-objects"))
}
```

- [ ] **Step 2: Run the test to verify failure**

```bash
task test -- -run 'TestRegisterWithSource|TestRegisterFallsBackToEmptyVersion' ./internal/core/
```

Expected: FAIL with `r.RegisterWithSource undefined` and/or `r.SourceVersion undefined`.

- [ ] **Step 3: Implement**

Edit `internal/core/registry.go`. Add (just below the existing `VerbRegistry` struct):

```go
// SourceInfo records origin metadata for a verb registration.
type SourceInfo struct {
    Source  string // "builtin" or plugin manifest name
    Version string // host build version for builtin, manifest version for plugins
}
```

Extend the `VerbRegistry` struct:

```go
type VerbRegistry struct {
    mu      sync.RWMutex
    types   map[string]VerbRegistration
    sources map[string]string // source name -> version
}
```

Update `NewVerbRegistry`:

```go
func NewVerbRegistry() *VerbRegistry {
    return &VerbRegistry{
        types:   make(map[string]VerbRegistration),
        sources: make(map[string]string),
    }
}
```

Add the new methods:

```go
// RegisterWithSource adds a type and records the source's version. Returns
// error if duplicate or invalid.
func (r *VerbRegistry) RegisterWithSource(reg VerbRegistration, version string) error {
    if err := r.Register(reg); err != nil {
        return err
    }
    if reg.Source != "" {
        r.mu.Lock()
        r.sources[reg.Source] = version
        r.mu.Unlock()
    }
    return nil
}

// SourceVersion returns the version recorded for a given source name.
// Returns "" if source is unknown.
func (r *VerbRegistry) SourceVersion(source string) string {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.sources[source]
}
```

- [ ] **Step 4: Run the test to verify pass**

```bash
task test -- -run 'TestRegisterWithSource|TestRegisterFallsBackToEmptyVersion' ./internal/core/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(core): VerbRegistry tracks source/version via RegisterWithSource"
jj new
```

---

#### Task 5: `BootstrapVerbRegistry` + unexport `registerBuiltinTypes`

**Files:**

- Modify: `internal/core/builtins.go`
- Modify: `internal/core/registry_test.go` (call sites)
- Create: `internal/core/exports_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/core/registry_test.go`:

```go
func TestBootstrapVerbRegistryReturnsSeededRegistry(t *testing.T) {
    r, err := BootstrapVerbRegistry("0.4.2-test")
    require.NoError(t, err)

    // Builtins are registered.
    reg, ok := r.Lookup("arrive")
    require.True(t, ok)
    assert.Equal(t, "movement", reg.Category)
    assert.Equal(t, "builtin", reg.Source)

    // Source version uses the host- prefix.
    assert.Equal(t, "host-0.4.2-test", r.SourceVersion("builtin"))
}
```

Create `internal/core/exports_test.go`:

```go
//go:build !integration

package core_test

import (
    "go/ast"
    "go/parser"
    "go/token"
    "strings"
    "testing"

    "github.com/stretchr/testify/require"
)

// TestRegisterBuiltinTypesIsUnexported is INV-GW-11. RegisterBuiltinTypes
// MUST NOT be a public symbol. BootstrapVerbRegistry is the only public
// path that returns a seeded registry.
func TestRegisterBuiltinTypesIsUnexported(t *testing.T) {
    fset := token.NewFileSet()
    f, err := parser.ParseFile(fset, "builtins.go", nil, 0)
    require.NoError(t, err)

    for _, decl := range f.Decls {
        fn, ok := decl.(*ast.FuncDecl)
        if !ok {
            continue
        }
        name := fn.Name.Name
        // RegisterBuiltinTypes (uppercase) must not exist.
        if name == "RegisterBuiltinTypes" {
            t.Errorf("RegisterBuiltinTypes is exported but MUST be unexported (INV-GW-11)")
        }
        // Public seeded constructor must exist.
        if name == "BootstrapVerbRegistry" {
            // Verify it's exported (uppercase).
            require.True(t, strings.ToUpper(name[:1]) == name[:1])
        }
    }
}
```

- [ ] **Step 2: Run the tests to verify failure**

```bash
task test -- -run 'TestBootstrapVerbRegistry|TestRegisterBuiltinTypesIsUnexported' ./internal/core/
```

Expected: FAIL — `BootstrapVerbRegistry` undefined; export-check passes only by accident.

- [ ] **Step 3: Implement**

Replace the contents of `internal/core/builtins.go` with:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"

// BootstrapVerbRegistry returns a VerbRegistry seeded with host-owned event
// types. This is the single public path for obtaining a seeded registry in
// production. Use NewVerbRegistry() for tests that need an empty registry.
//
// hostVersion is the build-time version of the holomush core binary
// (e.g., "0.4.2-rc1" or "dev"). The bootstrapper records each builtin
// registration with source "builtin" and version "host-" + hostVersion
// so plugin-version drift is visible in events_audit replays.
func BootstrapVerbRegistry(hostVersion string) (*VerbRegistry, error) {
    r := NewVerbRegistry()
    if err := registerBuiltinTypes(r, hostVersion); err != nil {
        return nil, err
    }
    return r, nil
}

// registerBuiltinTypes (unexported) registers host-owned event types in the
// given registry. Called only by BootstrapVerbRegistry. Plugin-owned types
// (say/pose/whisper from core-communication, object_* from core-objects)
// are registered by the plugin loader from each plugin's manifest verbs:
// block — see internal/plugin/manager.go — keeping plugin-owned types out
// of internal/core/ per the plugin-boundary discipline.
func registerBuiltinTypes(r *VerbRegistry, hostVersion string) error {
    sourceVersion := "host-" + hostVersion
    builtins := []VerbRegistration{
        // Movement
        {Type: "arrive", Category: "movement", Format: "notification", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_BOTH, Source: "builtin"},
        {Type: "leave", Category: "movement", Format: "notification", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_BOTH, Source: "builtin"},
        {
            Type: "move", Category: "movement", Format: "notification", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_BOTH, Source: "builtin",
            MetadataKeys: []MetadataKey{
                {Key: "from_id", ValueType: "string"},
                {Key: "to_id", ValueType: "string"},
                {Key: "exit_name", ValueType: "string"},
            },
        },

        // State
        {
            Type: "location_state", Category: "state", Format: "snapshot", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_STATE, Source: "builtin",
            MetadataKeys: []MetadataKey{
                {Key: "location", ValueType: "object"},
                {Key: "exits", ValueType: "array"},
                {Key: "present", ValueType: "array"},
            },
        },
        {
            Type: "exit_update", Category: "state", Format: "delta", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_STATE, Source: "builtin",
            MetadataKeys: []MetadataKey{{Key: "exits", ValueType: "array"}},
        },

        // Command
        {Type: "command_response", Category: "command", Format: "narrative", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
        {Type: "command_error", Category: "command", Format: "error", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},

        // System
        {Type: "system", Category: "system", Format: "notification", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
    }
    for _, b := range builtins {
        if err := r.RegisterWithSource(b, sourceVersion); err != nil {
            return err
        }
    }
    return nil
}
```

Note: this still uses `webv1.EventChannel`. Task 22 migrates `VerbRegistration.DisplayTarget` to `corev1.EventChannel` and updates these constants. Splitting keeps each task small.

- [ ] **Step 4: Update `registry_test.go` to call `registerBuiltinTypes` (lowercase)**

Find every call to `RegisterBuiltinTypes` in `internal/core/registry_test.go` and rename to `registerBuiltinTypes(r, "test")`. Sample:

```go
// Before:
err := RegisterBuiltinTypes(r)
// After:
err := registerBuiltinTypes(r, "test")
```

- [ ] **Step 5: Run the tests**

```bash
task test -- ./internal/core/
```

Expected: PASS for the new tests + the existing registry tests (now calling the lowercase function).

- [ ] **Step 6: Commit**

```bash
jj describe -m "feat(core): BootstrapVerbRegistry replaces public RegisterBuiltinTypes"
jj new
```

---

## PHASE P3 — `eventbus.Event` GO-STRUCT EXTENSIONS

### Task 6: Add `Rendering` field + Go-side mirror types

**Files:**

- Modify: `internal/eventbus/types.go`
- Test: `internal/eventbus/types_proto_sync_test.go` (new file, written in Task 8)

- [ ] **Step 1: Add the Go-side types**

Edit `internal/eventbus/types.go`. After the existing `Actor` type, add:

```go
// EventChannel mirrors corev1.EventChannel for ergonomic host-side use
// (avoids forcing test fixtures and emit-site struct literals to import
// the proto package). Kept in lockstep with the proto enum by INV-GW-14.
type EventChannel uint8

const (
    EventChannelUnspecified EventChannel = 0
    EventChannelTerminal    EventChannel = 1
    EventChannelState       EventChannel = 2
    EventChannelBoth        EventChannel = 3
)

// RenderingMetadata is the host-side representation of corev1.RenderingMetadata.
// Populated by RenderingPublisher.Publish before marshaling to the wire.
type RenderingMetadata struct {
    Category            string
    Format              string
    Label               string
    DisplayTarget       EventChannel
    SourcePlugin        string
    SourcePluginVersion string
}
```

In the existing `Event` struct, after the `Payload []byte` line, add:

```go
    // Rendering is populated by RenderingPublisher.Publish before
    // marshaling. Callers MUST NOT populate this field directly; the
    // field is reserved for the publisher chain.
    Rendering *RenderingMetadata
```

- [ ] **Step 2: Verify the package still builds**

```bash
task lint
```

Expected: clean. Existing call sites that construct `eventbus.Event{...}` literals work unchanged (new field is optional).

- [ ] **Step 3: Commit**

```bash
jj describe -m "feat(eventbus): add Event.Rendering and Go-side RenderingMetadata"
jj new
```

---

#### Task 7: Add `Headers` field for pre-publish NATS headers

**Files:**

- Modify: `internal/eventbus/types.go`

- [ ] **Step 1: Add the field**

In `internal/eventbus/types.go`, in the `Event` struct, after the `Rendering` field, add:

```go
    // Headers carries pre-publish NATS headers stamped by the publisher
    // chain (e.g. App-Rendering by RenderingPublisher). JetStreamPublisher
    // merges these into the outgoing nats.Msg headers alongside the
    // system-stamped ones. Callers other than the publisher chain MUST
    // NOT populate this field directly.
    //
    // Reserved-keys rule: caller-written keys MUST start with "App-" and
    // MUST NOT be in the system-reserved set (Nats-Msg-Id, App-Codec,
    // App-Schema-Version, App-Event-Type, App-Actor-Kind, App-Actor-ID,
    // App-Actor-Legacy-ID, traceparent, tracestate). Keys starting with
    // "Nats-" are reserved unconditionally. Violation panics under
    // testing.Testing(); in production logs a warning and the system
    // value wins.
    //
    // Cold-tier reads: this field is publish-path only. The cold-tier
    // history reader leaves Headers nil. Subscribers MUST NOT depend
    // on Headers being populated at read time; they read Event.Rendering.
    Headers map[string]string
```

- [ ] **Step 2: Verify build**

```bash
task lint
```

- [ ] **Step 3: Commit**

```bash
jj describe -m "feat(eventbus): add Event.Headers for pre-publish NATS header transport"
jj new
```

---

#### Task 8: `renderingToProto` + `renderingFromProto` helpers + `INV-GW-14` parity test

**Files:**

- Modify: `internal/eventbus/publisher.go` (or create `internal/eventbus/types_proto.go` if the plan prefers separation)
- Create: `internal/eventbus/types_proto_sync_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/eventbus/types_proto_sync_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package eventbus_test

import (
    "testing"

    "github.com/stretchr/testify/assert"

    "github.com/holomush/holomush/internal/eventbus"
    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// TestRenderingMetadataGoProtoParity is INV-GW-14. The Go struct and
// proto message MUST stay in sync — round-tripping through both
// conversion helpers MUST produce equal values.
func TestRenderingMetadataGoProtoParity(t *testing.T) {
    src := &eventbus.RenderingMetadata{
        Category:            "communication",
        Format:              "speech",
        Label:               "says",
        DisplayTarget:       eventbus.EventChannelTerminal,
        SourcePlugin:        "core-communication",
        SourcePluginVersion: "0.1.0",
    }

    proto := eventbus.RenderingToProto(src)
    require.NotNil(t, proto)
    assert.Equal(t, "communication", proto.GetCategory())
    assert.Equal(t, "speech", proto.GetFormat())
    assert.Equal(t, "says", proto.GetLabel())
    assert.Equal(t, corev1.EventChannel_EVENT_CHANNEL_TERMINAL, proto.GetDisplayTarget())
    assert.Equal(t, "core-communication", proto.GetSourcePlugin())
    assert.Equal(t, "0.1.0", proto.GetSourcePluginVersion())

    roundTrip := eventbus.RenderingFromProto(proto)
    assert.Equal(t, src, roundTrip)
}

func TestRenderingMetadataNilRoundTrip(t *testing.T) {
    assert.Nil(t, eventbus.RenderingToProto(nil))
    assert.Nil(t, eventbus.RenderingFromProto(nil))
}

// TestEventChannelEnumParity asserts the Go-side mirror values match
// the proto enum values. INV-GW-14 (the parity dimension covering
// the EventChannel mirror specifically).
func TestEventChannelEnumParity(t *testing.T) {
    cases := []struct {
        goVal    eventbus.EventChannel
        protoVal corev1.EventChannel
    }{
        {eventbus.EventChannelUnspecified, corev1.EventChannel_EVENT_CHANNEL_UNSPECIFIED},
        {eventbus.EventChannelTerminal, corev1.EventChannel_EVENT_CHANNEL_TERMINAL},
        {eventbus.EventChannelState, corev1.EventChannel_EVENT_CHANNEL_STATE},
        {eventbus.EventChannelBoth, corev1.EventChannel_EVENT_CHANNEL_BOTH},
    }
    for _, c := range cases {
        assert.Equal(t, int32(c.goVal), int32(c.protoVal))
    }
}
```

- [ ] **Step 2: Run to verify failure**

```bash
task test -- ./internal/eventbus/
```

Expected: FAIL with `RenderingToProto undefined` / `RenderingFromProto undefined`.

- [ ] **Step 3: Implement helpers**

Add to `internal/eventbus/publisher.go` (alongside the existing
`ActorToProto` and `actorFromProto` helpers — see `publisher.go:277` and
`subscriber.go:463` for the established pattern):

```go
// RenderingToProto converts the host-side RenderingMetadata to its proto
// form. Returns nil if input is nil. INV-GW-14 ensures parity.
func RenderingToProto(r *RenderingMetadata) *corev1.RenderingMetadata {
    if r == nil {
        return nil
    }
    return &corev1.RenderingMetadata{
        Category:            r.Category,
        Format:              r.Format,
        Label:               r.Label,
        DisplayTarget:       corev1.EventChannel(r.DisplayTarget),
        SourcePlugin:        r.SourcePlugin,
        SourcePluginVersion: r.SourcePluginVersion,
    }
}

// RenderingFromProto converts the proto form to the host-side struct.
// Returns nil if input is nil.
func RenderingFromProto(p *corev1.RenderingMetadata) *RenderingMetadata {
    if p == nil {
        return nil
    }
    return &RenderingMetadata{
        Category:            p.GetCategory(),
        Format:              p.GetFormat(),
        Label:               p.GetLabel(),
        DisplayTarget:       EventChannel(p.GetDisplayTarget()),
        SourcePlugin:        p.GetSourcePlugin(),
        SourcePluginVersion: p.GetSourcePluginVersion(),
    }
}
```

If `corev1` isn't already imported in this file, add:

```go
corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
```

- [ ] **Step 4: Run the test**

```bash
task test -- ./internal/eventbus/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(eventbus): RenderingMetadata Go↔proto conversion helpers (INV-GW-14)"
jj new
```

---

## PHASE P4 — JETSTREAM PUBLISHER UPDATES

### Task 9: Copy `event.Rendering` into proto envelope (`INV-GW-3a`)

**Files:**

- Modify: `internal/eventbus/publisher.go`
- Test: `internal/eventbus/publisher_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/eventbus/publisher_test.go`. The real `eventbustest` API
exposes `Bus *eventbus.Subsystem`; the test pattern follows
`internal/eventbus/subscriber_test.go:70-72`:

```go
// TestPublisherCopiesRenderingIntoEnvelope is INV-GW-3a. JetStreamPublisher
// MUST copy event.Rendering into the proto envelope before Marshal so
// subscribers see the same Rendering on the read side.
func TestPublisherCopiesRenderingIntoEnvelope(t *testing.T) {
    embedded := eventbustest.New(t)
    pub := embedded.Bus.Publisher()
    sub := embedded.Bus.Subscriber()

    rendering := &eventbus.RenderingMetadata{
        Category:            "communication",
        Format:              "speech",
        Label:               "says",
        DisplayTarget:       eventbus.EventChannelTerminal,
        SourcePlugin:        "core-communication",
        SourcePluginVersion: "0.1.0",
    }

    ev := eventbus.Event{
        ID:        core.NewULID(),
        Subject:   eventbus.Subject("events.main.character.01ABC"),
        Type:      eventbus.Type("core-communication.say"),
        Timestamp: time.Now().UTC(),
        Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter, LegacyID: "01ABC"},
        Payload:   []byte(`{"message":"hello"}`),
        Rendering: rendering,
    }
    require.NoError(t, pub.Publish(context.Background(), ev))
    embedded.AwaitStreamLastSeq(t, 1, 0)

    sess, err := sub.OpenSession(
        context.Background(),
        "test-session",
        []eventbus.Subject{eventbus.Subject("events.main.character.01ABC")},
    )
    require.NoError(t, err)
    defer sess.Close()

    select {
    case got := <-sess.Events():
        require.NotNil(t, got.Rendering, "subscriber-side decode must populate Rendering")
        assert.Equal(t, rendering.Category, got.Rendering.Category)
        assert.Equal(t, rendering.Format, got.Rendering.Format)
        assert.Equal(t, rendering.Label, got.Rendering.Label)
        assert.Equal(t, rendering.DisplayTarget, got.Rendering.DisplayTarget)
        assert.Equal(t, rendering.SourcePlugin, got.Rendering.SourcePlugin)
        assert.Equal(t, rendering.SourcePluginVersion, got.Rendering.SourcePluginVersion)
    case <-time.After(2 * time.Second):
        t.Fatal("timeout waiting for event")
    }
}
```

(Verified API: `eventbustest.New(t) *Embedded` with field `Bus *eventbus.Subsystem`. `Bus.Publisher()` and `Bus.Subscriber()` are the canonical accessors. `Subscriber.OpenSession(ctx, name, []Subject) (SessionStream, error)` matches `eventbus.Subscriber` in `bus.go`. The session's `Events()` channel returns `eventbus.Event` values populated by `decodeDelivery` in `subscriber.go`.)

- [ ] **Step 2: Run the test**

```bash
task test -- -run TestPublisherCopiesRenderingIntoEnvelope ./internal/eventbus/
```

Expected: FAIL — Rendering is nil on the read side because Publish doesn't copy it yet.

- [ ] **Step 3: Update `JetStreamPublisher.Publish` to copy `Rendering`**

In `internal/eventbus/publisher.go`, find the existing `eventbusv1.Event` proto construction (around line 156-163 per the spec):

```go
// Before:
protoEv := &eventbusv1.Event{
    Id:        ev.ID[:],
    Subject:   string(ev.Subject),
    Type:      string(ev.Type),
    Timestamp: timestamppb.New(ev.Timestamp),
    Actor:     actorToProto(ev.Actor),
    Payload:   ev.Payload,
}
```

Add the rendering field copy:

```go
// After:
protoEv := &eventbusv1.Event{
    Id:        ev.ID[:],
    Subject:   string(ev.Subject),
    Type:      string(ev.Type),
    Timestamp: timestamppb.New(ev.Timestamp),
    Actor:     actorToProto(ev.Actor),
    Payload:   ev.Payload,
    Rendering: RenderingToProto(ev.Rendering),
}
```

Then update the **two** read-side proto-to-struct decode sites:

**Site 1: subscriber path** at `internal/eventbus/subscriber.go`. Around
line 418 the `decodeDelivery` function declares
`var envelope eventbusv1.Event` and unmarshals into it; the actual
`Event{...}` literal that the function returns is roughly at lines 422–429.
Add `Rendering` to that construction:

```go
// In decodeDelivery, in the Event{...} literal:
return Event{
    ID:        ulid.ULID(envelope.GetId()),  // existing
    Subject:   Subject(envelope.GetSubject()),
    Type:      Type(envelope.GetType()),
    Timestamp: envelope.GetTimestamp().AsTime(),
    Actor:     actorFromProto(envelope.GetActor()),
    Payload:   envelope.GetPayload(),
    Rendering: RenderingFromProto(envelope.GetRendering()),  // NEW
}, nil
```

**Site 2: hot-tier history reader** at `internal/eventbus/history/hot_jetstream.go:388`.
The same envelope-to-Event reconstruction happens for hot-tier replay.
Add the same `Rendering` field copy.

Subscriber: the field name `actorFromProto` (lowercase) is correct — see
`subscriber.go:463`. The publisher-side helper is `ActorToProto` (uppercase)
at `publisher.go:277`. New helpers `RenderingToProto` and `RenderingFromProto`
follow the public-helper convention (uppercase, both in `publisher.go`).

- [ ] **Step 4: Run the test**

```bash
task test -- -run TestPublisherCopiesRenderingIntoEnvelope ./internal/eventbus/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(eventbus): JetStreamPublisher copies Rendering into proto envelope (INV-GW-3a)"
jj new
```

---

#### Task 10: Merge `event.Headers` into `nats.Msg.Header` with collision policy

**Files:**

- Modify: `internal/eventbus/publisher.go`
- Test: `internal/eventbus/publisher_test.go`

- [ ] **Step 1a: Add a `RawMessagesOnSubject` helper to `eventbustest`**

The existing `Embedded` struct exposes `JS jetstream.JetStream` and
`Conn *nats.Conn`; no `RawMessagesOnSubject` helper exists. Add one to
`internal/eventbus/eventbustest/embedded.go`:

```go
// RawMessagesOnSubject returns up to limit messages from the EVENTS stream
// matching the given subject. Useful for asserting NATS-layer details
// (headers, raw bytes) rather than decoded eventbus.Event values.
//
// Uses an ephemeral consumer with DeliverAll + AckExplicit so multiple
// calls are independent. Polls until the consumer drains; no time.Sleep.
func (e *Embedded) RawMessagesOnSubject(t TB, subject string, limit int, timeout time.Duration) []*nats.Msg {
    t.Helper()
    if timeout <= 0 {
        timeout = DefaultAwaitTimeout
    }
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()

    cons, err := e.JS.OrderedConsumer(ctx, eventbus.StreamName, jetstream.OrderedConsumerConfig{
        FilterSubjects: []string{subject},
        DeliverPolicy:  jetstream.DeliverAllPolicy,
    })
    require.NoError(t, err)

    out := make([]*nats.Msg, 0, limit)
    for len(out) < limit {
        msgs, fetchErr := cons.Fetch(limit-len(out), jetstream.FetchMaxWait(200*time.Millisecond))
        if fetchErr != nil && !errors.Is(fetchErr, nats.ErrTimeout) {
            require.NoError(t, fetchErr)
        }
        empty := true
        for msg := range msgs.Messages() {
            empty = false
            // Convert to plain *nats.Msg for caller convenience.
            raw := &nats.Msg{
                Subject: msg.Subject(),
                Reply:   msg.Reply(),
                Header:  msg.Headers(),
                Data:    msg.Data(),
            }
            out = append(out, raw)
            require.NoError(t, msg.Ack())
        }
        if empty {
            break  // no more messages waiting
        }
    }
    return out
}
```

Adjust imports: `"errors"`, `"time"`, `"github.com/nats-io/nats.go"`,
`"github.com/nats-io/nats.go/jetstream"`, and
`"github.com/holomush/holomush/internal/eventbus"` (for `eventbus.StreamName`).
The constant is declared at `internal/eventbus/subsystem.go` as
`const StreamName = "EVENTS"` and is already used by
`eventbustest/embedded.go` line 192 (verify with
`rg -n 'eventbus\.StreamName' internal/eventbus/eventbustest/`).

- [ ] **Step 1b: Write the failing tests**

Add to `internal/eventbus/publisher_test.go`:

```go
func TestPublisherMergesHeadersIntoNatsMsg(t *testing.T) {
    embedded := eventbustest.New(t)
    pub := embedded.Bus.Publisher()

    ev := eventbus.Event{
        ID:        core.NewULID(),
        Subject:   eventbus.Subject("events.main.character.01ABC"),
        Type:      eventbus.Type("core-communication.say"),
        Timestamp: time.Now().UTC(),
        Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
        Payload:   []byte(`{"message":"hi"}`),
        Headers:   map[string]string{"App-Rendering": `{"category":"communication"}`},
    }
    require.NoError(t, pub.Publish(context.Background(), ev))
    embedded.AwaitStreamLastSeq(t, 1, 0)

    msgs := embedded.RawMessagesOnSubject(t, "events.main.character.01ABC", 10, 0)
    require.Len(t, msgs, 1)
    assert.Equal(t, `{"category":"communication"}`, msgs[0].Header.Get("App-Rendering"))
    // System headers still present.
    assert.NotEmpty(t, msgs[0].Header.Get("Nats-Msg-Id"))
}

func TestPublisherCollidingHeaderPanicsInTests(t *testing.T) {
    embedded := eventbustest.New(t)
    pub := embedded.Bus.Publisher()

    ev := eventbus.Event{
        ID:        core.NewULID(),
        Subject:   eventbus.Subject("events.main.character.01ABC"),
        Type:      eventbus.Type("core-communication.say"),
        Timestamp: time.Now().UTC(),
        Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
        Payload:   []byte(`{"message":"hi"}`),
        // Collision: caller writes a system-reserved header.
        Headers: map[string]string{"Nats-Msg-Id": "naughty"},
    }
    assert.Panics(t, func() {
        _ = pub.Publish(context.Background(), ev)
    })
}
```

- [ ] **Step 2: Run the test to verify failure**

```bash
task test -- -run 'TestPublisherMergesHeadersIntoNatsMsg|TestPublisherCollidingHeaderPanicsInTests' ./internal/eventbus/
```

Expected: FAIL — `App-Rendering` header missing; collision-panic doesn't fire.

- [ ] **Step 3: Implement the merge with collision policy**

In `internal/eventbus/publisher.go`:

(a) Add to the existing imports at the top of the file:

```go
import (
    // ... existing imports ...
    "fmt"
    "strings"
    "testing"  // Intentional: testing.Testing() (Go 1.21+) lets the
               // collision policy distinguish test vs production paths.
               // The testing flag set is registered at init in production
               // binaries — known small cost, acceptable for the
               // strict-failure intent (spec Section 2).
)
```

(b) Add as **package-level declarations** (NOT inside `Publish`):

```go
// Reserved system header keys — never overwritten by event.Headers.
// Uses the existing constants from publisher.go:27-46. Per project memory
// rule "no magic values," we MUST NOT duplicate the string literals here.
var reservedHeaderKeys = map[string]struct{}{
    HeaderMsgID:         {},
    HeaderCodec:         {},
    HeaderSchemaVersion: {},
    HeaderEventType:     {},
    HeaderActorKind:     {},
    HeaderActorID:       {},
    HeaderActorLegacyID: {},
    // W3C tracing headers — externally specified, no project constants.
    "traceparent": {},
    "tracestate":  {},
}

// mergeCallerHeaders copies ev.Headers into msgHeader. On collision with a
// reserved key, panics under testing.Testing(); in production logs a
// warning and the system value wins. Caller-supplied keys MUST start
// with "App-" and MUST NOT be in reservedHeaderKeys; "Nats-*" keys are
// reserved unconditionally.
func mergeCallerHeaders(msgHeader nats.Header, ev Event) {
    if len(ev.Headers) == 0 {
        return
    }
    for k, v := range ev.Headers {
        if _, reserved := reservedHeaderKeys[k]; reserved || strings.HasPrefix(k, "Nats-") {
            if testing.Testing() {
                panic(fmt.Sprintf("eventbus: caller wrote reserved header key %q", k))
            }
            slog.Warn("eventbus: caller-written header collides with reserved key; system value wins",
                "header", k, "event_id", ev.ID.String())
            continue
        }
        msgHeader.Set(k, v)
    }
}
```

(c) **Inside `(*JetStreamPublisher).Publish`**, after the existing system
headers are set (i.e. after the last `msg.Header.Set("App-...", ...)`
call around line 213, but BEFORE the `telemetry.InjectHeaders(...)` call
on the following line), add a single line:

```go
mergeCallerHeaders(msg.Header, ev)
```

- [ ] **Step 4: Run the tests**

```bash
task test -- -run 'TestPublisherMergesHeaders|TestPublisherCollidingHeader' ./internal/eventbus/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(eventbus): JetStreamPublisher merges event.Headers with collision policy"
jj new
```

---

## PHASE P5 — RENDERING PUBLISHER

### Task 11: Create `RenderingPublisher` skeleton + lookup-and-stamp (`INV-GW-2`)

**Files:**

- Create: `internal/eventbus/rendering_publisher.go`
- Create: `internal/eventbus/rendering_publisher_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/eventbus/rendering_publisher_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package eventbus_test

import (
    "context"
    "testing"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/internal/eventbus"
    webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)
// Note: subsequent tasks (Task 12, Task 14) add `protojson` and `corev1`
// imports when their test bodies first need them. Adding imports here
// before they're used would fail compile with "imported and not used".

func newSeededTestRegistry(t *testing.T) *core.VerbRegistry {
    t.Helper()
    r := core.NewVerbRegistry()
    require.NoError(t, r.RegisterWithSource(core.VerbRegistration{
        Type:          "core-communication:say",
        Category:      "communication",
        Format:        "speech",
        Label:         "says",
        DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
        Source:        "core-communication",
    }, "0.1.0"))
    return r
}

// TestRenderingPublisherStampsEventRendering is INV-GW-2.
// RenderingPublisher.Publish MUST stamp event.Rendering from the verb
// registry before publishing.
func TestRenderingPublisherStampsEventRendering(t *testing.T) {
    inner := &fakePublisher{}
    rp := eventbus.NewRenderingPublisher(inner, newSeededTestRegistry(t))

    ev := eventbus.Event{
        ID:        ulid.Make(),
        Subject:   eventbus.Subject("events.main.character.01ABC"),
        Type:      eventbus.Type("core-communication:say"),
        Timestamp: time.Now().UTC(),
        Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter},
        Payload:   []byte(`{"message":"hi"}`),
    }
    require.NoError(t, rp.Publish(context.Background(), ev))

    require.Len(t, inner.published, 1)
    got := inner.published[0]
    require.NotNil(t, got.Rendering)
    assert.Equal(t, "communication", got.Rendering.Category)
    assert.Equal(t, "speech", got.Rendering.Format)
    assert.Equal(t, "says", got.Rendering.Label)
    assert.Equal(t, eventbus.EventChannelTerminal, got.Rendering.DisplayTarget)
    assert.Equal(t, "core-communication", got.Rendering.SourcePlugin)
    assert.Equal(t, "0.1.0", got.Rendering.SourcePluginVersion)
}

// fakePublisher captures events for inspection.
type fakePublisher struct {
    published []eventbus.Event
    err       error
}

func (f *fakePublisher) Publish(ctx context.Context, ev eventbus.Event) error {
    if f.err != nil {
        return f.err
    }
    f.published = append(f.published, ev)
    return nil
}
```

- [ ] **Step 2: Run the test**

```bash
task test -- -run TestRenderingPublisherStampsEventRendering ./internal/eventbus/
```

Expected: FAIL — `eventbus.NewRenderingPublisher` undefined.

- [ ] **Step 3: Create `RenderingPublisher`**

Create `internal/eventbus/rendering_publisher.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
    "context"

    "github.com/samber/oops"
    "google.golang.org/protobuf/encoding/protojson"

    "github.com/holomush/holomush/internal/core"
)
// Note: `corev1` is added in Task 12 (header stamp) and `protojson` is
// already used here for renderingJSONOpts. `encoding/json` is not needed
// — the canonical form is protojson, not stdlib json. Imports are kept
// minimal so each task only adds what its body uses.

// renderingJSONOpts is the canonical protojson form for the App-Rendering
// NATS header. UseProtoNames produces snake_case field names matching the
// proto; UseEnumNumbers=false emits enum names like "EVENT_CHANNEL_TERMINAL";
// EmitUnpopulated keeps the shape stable across producers.
var renderingJSONOpts = protojson.MarshalOptions{
    UseProtoNames:   true,
    UseEnumNumbers:  false,
    EmitUnpopulated: true,
}

// RenderingPublisher wraps an underlying eventbus.Publisher and is the
// single enrichment site for rendering metadata. At Publish time it:
//
//  1. Looks up event.Type in the verb registry.
//  2. Stamps event.Rendering from the registration.
//  3. Stamps event.Headers["App-Rendering"] with the protojson form.
//  4. Validates the proto projection against the manifest's protovalidate rules.
//  5. Delegates to the underlying publisher.
//
// All emit-site callers — both PluginEventEmitter and host-direct emit
// sites — receive RenderingPublisher as their eventbus.Publisher dependency.
type RenderingPublisher struct {
    inner    Publisher
    registry *core.VerbRegistry
}

// NewRenderingPublisher constructs a wrapper. inner and registry MUST NOT be nil.
func NewRenderingPublisher(inner Publisher, registry *core.VerbRegistry) *RenderingPublisher {
    if inner == nil {
        panic("eventbus.NewRenderingPublisher: inner publisher is nil")
    }
    if registry == nil {
        panic("eventbus.NewRenderingPublisher: verb registry is nil")
    }
    return &RenderingPublisher{inner: inner, registry: registry}
}

// Publish enriches event with rendering metadata and delegates to the
// underlying publisher.
func (p *RenderingPublisher) Publish(ctx context.Context, event Event) error {
    reg, ok := p.registry.Lookup(string(event.Type))
    if !ok {
        return oops.Code("EMIT_UNKNOWN_VERB").
            With("event_type", string(event.Type)).
            Errorf("verb registry has no entry for event type")
    }

    event.Rendering = &RenderingMetadata{
        Category:            reg.Category,
        Format:              reg.Format,
        Label:               reg.Label,
        DisplayTarget:       EventChannel(reg.DisplayTarget),
        SourcePlugin:        reg.Source,
        SourcePluginVersion: p.registry.SourceVersion(reg.Source),
    }

    // Step in Task 12: stamp App-Rendering header.
    // Step in Task 14: protovalidate.

    return p.inner.Publish(ctx, event)
}
```

Note: `EventChannel(reg.DisplayTarget)` works while `VerbRegistration.DisplayTarget` is still `webv1.EventChannel` (same numeric values). Task 22 migrates the field type.

- [ ] **Step 4: Run the test**

```bash
task test -- -run TestRenderingPublisherStampsEventRendering ./internal/eventbus/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(eventbus): RenderingPublisher stamps event.Rendering from registry (INV-GW-2)"
jj new
```

---

#### Task 12: Stamp `App-Rendering` header + `INV-GW-15` parity test

**Files:**

- Modify: `internal/eventbus/rendering_publisher.go`
- Modify: `internal/eventbus/rendering_publisher_test.go`

- [ ] **Step 1: Write the failing test**

First, add the imports `corev1` and `protojson` to
`internal/eventbus/rendering_publisher_test.go` (Task 11 deliberately
omitted them). The import block becomes:

```go
import (
    "context"
    "testing"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "google.golang.org/protobuf/encoding/protojson"   // NEW: Task 12

    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/internal/eventbus"
    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"   // NEW: Task 12
    webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)
```

Then append the test:

```go
// TestRenderingPublisherStampsAppRenderingHeader is INV-GW-15. The
// header value MUST encode the same RenderingMetadata as event.Rendering,
// using protojson.MarshalOptions{UseProtoNames, UseEnumNumbers=false}.
func TestRenderingPublisherStampsAppRenderingHeader(t *testing.T) {
    inner := &fakePublisher{}
    rp := eventbus.NewRenderingPublisher(inner, newSeededTestRegistry(t))

    ev := eventbus.Event{
        ID:        ulid.Make(),
        Subject:   eventbus.Subject("events.main.character.01ABC"),
        Type:      eventbus.Type("core-communication:say"),
        Timestamp: time.Now().UTC(),
        Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter},
        Payload:   []byte(`{"message":"hi"}`),
    }
    require.NoError(t, rp.Publish(context.Background(), ev))

    require.Len(t, inner.published, 1)
    got := inner.published[0]
    require.NotNil(t, got.Headers)
    headerJSON, ok := got.Headers["App-Rendering"]
    require.True(t, ok, "App-Rendering header missing")

    // Decode header and compare to event.Rendering via the shared canonical form.
    headerMD := &corev1.RenderingMetadata{}
    require.NoError(t, protojson.Unmarshal([]byte(headerJSON), headerMD))

    envelopeMD := eventbus.RenderingToProto(got.Rendering)
    headerCanonical, _ := (protojson.MarshalOptions{UseProtoNames: true, UseEnumNumbers: false, EmitUnpopulated: true}).Marshal(headerMD)
    envelopeCanonical, _ := (protojson.MarshalOptions{UseProtoNames: true, UseEnumNumbers: false, EmitUnpopulated: true}).Marshal(envelopeMD)
    assert.JSONEq(t, string(envelopeCanonical), string(headerCanonical))

    // Sanity: header decodes to expected fields.
    assert.Equal(t, "communication", headerMD.GetCategory())
    assert.Equal(t, "speech", headerMD.GetFormat())
}
```

- [ ] **Step 2: Run the test**

```bash
task test -- -run TestRenderingPublisherStampsAppRenderingHeader ./internal/eventbus/
```

Expected: FAIL — `event.Headers` is nil after Publish.

- [ ] **Step 3: Implement the header stamp**

In `internal/eventbus/rendering_publisher.go`, between the `event.Rendering = ...` block and `return p.inner.Publish(...)`, add:

```go
    // Stamp the App-Rendering NATS header (protojson form) so the audit
    // projection can write events_audit.rendering without proto-decoding
    // the envelope. INV-GW-15 enforces parity with event.Rendering.
    headerBytes, err := renderingJSONOpts.Marshal(RenderingToProto(event.Rendering))
    if err != nil {
        return oops.Code("EMIT_HEADER_MARSHAL_FAILED").
            With("event_type", string(event.Type)).
            Wrap(err)
    }
    if event.Headers == nil {
        event.Headers = make(map[string]string, 1)
    }
    event.Headers["App-Rendering"] = string(headerBytes)
```

- [ ] **Step 4: Run the test**

```bash
task test -- -run TestRenderingPublisherStampsAppRenderingHeader ./internal/eventbus/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(eventbus): RenderingPublisher stamps App-Rendering header (INV-GW-15)"
jj new
```

---

#### Task 13: Strict-mode `EMIT_UNKNOWN_VERB` (`INV-GW-3`)

**Files:**

- Modify: `internal/eventbus/rendering_publisher_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/eventbus/rendering_publisher_test.go`:

```go
// TestRenderingPublisherUnknownVerb is INV-GW-3. Registry-miss returns
// EMIT_UNKNOWN_VERB and does NOT publish.
func TestRenderingPublisherUnknownVerb(t *testing.T) {
    inner := &fakePublisher{}
    rp := eventbus.NewRenderingPublisher(inner, core.NewVerbRegistry())  // empty

    ev := eventbus.Event{
        ID:        ulid.Make(),
        Subject:   eventbus.Subject("events.main.character.01ABC"),
        Type:      eventbus.Type("core-communication:say"),
        Timestamp: time.Now().UTC(),
        Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter},
        Payload:   []byte(`{}`),
    }
    err := rp.Publish(context.Background(), ev)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "EMIT_UNKNOWN_VERB")
    assert.Empty(t, inner.published, "must not publish on unknown verb")
}
```

- [ ] **Step 2: Run the test**

```bash
task test -- -run TestRenderingPublisherUnknownVerb ./internal/eventbus/
```

Expected: PASS — Task 11 already returns `EMIT_UNKNOWN_VERB` on lookup miss. (This test is the explicit invariant check; the implementation was prefigured in Task 11.)

Add `"github.com/holomush/holomush/pkg/errutil"` to the test file's
imports — this Task 13 test is the first place in the file that uses
`errutil.AssertErrorCode`. The helper is documented in CLAUDE.md
(Error Handling section).

- [ ] **Step 3: Commit**

```bash
jj describe -m "test(eventbus): RenderingPublisher returns EMIT_UNKNOWN_VERB (INV-GW-3)"
jj new
```

---

#### Task 14: `protovalidate.Validate` step + `INV-GW-4`

**Files:**

- Modify: `internal/eventbus/rendering_publisher.go`
- Modify: `internal/eventbus/rendering_publisher_test.go`

**Approach:** factor a small private helper `validateRendering(*corev1.RenderingMetadata) error` and unit-test it directly with a malformed proto value. This avoids tampering with the registry (which would require a test-only escape hatch and re-write semantics that don't exist) and tests exactly the same validation path the publisher uses.

- [ ] **Step 1: Add the imports + validator field to `RenderingPublisher`**

In `internal/eventbus/rendering_publisher.go`, add to the imports
(both `protovalidate` and `corev1` — the latter is now first-needed
in this task because `validateRendering` takes a
`*corev1.RenderingMetadata` parameter):

```go
"buf.build/go/protovalidate"

corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
```

Update the type and constructor (extending Task 11's skeleton):

```go
type RenderingPublisher struct {
    inner     Publisher
    registry  *core.VerbRegistry
    validator protovalidate.Validator
}

func NewRenderingPublisher(inner Publisher, registry *core.VerbRegistry) *RenderingPublisher {
    if inner == nil {
        panic("eventbus.NewRenderingPublisher: inner publisher is nil")
    }
    if registry == nil {
        panic("eventbus.NewRenderingPublisher: verb registry is nil")
    }
    v, err := protovalidate.New()
    if err != nil {
        panic("eventbus.NewRenderingPublisher: failed to construct protovalidate.Validator: " + err.Error())
    }
    return &RenderingPublisher{inner: inner, registry: registry, validator: v}
}
```

Add the private helper at the bottom of the file:

```go
// validateRendering runs protovalidate against a RenderingMetadata proto.
// Returns nil on success, or the validator error on failure (caller wraps
// with EMIT_VALIDATION_FAILED).
func (p *RenderingPublisher) validateRendering(md *corev1.RenderingMetadata) error {
    return p.validator.Validate(md)
}
```

Insert the validation step in `Publish` AFTER the header-stamp from Task 12 and BEFORE the `p.inner.Publish` call:

```go
    // Validate the rendering proto against protovalidate rules (INV-GW-4).
    if vErr := p.validateRendering(RenderingToProto(event.Rendering)); vErr != nil {
        return oops.Code("EMIT_VALIDATION_FAILED").
            With("event_type", string(event.Type)).
            Wrap(vErr)
    }
```

- [ ] **Step 2: Write the failing tests in a new internal-test file**

Tasks 11–13 / 15 use `package eventbus_test` (file
`rendering_publisher_test.go`) following the dominant test convention.
Task 14's tests need to call the **private** `validateRendering` method
directly, so they live in a separate file `rendering_publisher_internal_test.go`
declared `package eventbus`. This matches the established
internal-test pattern (`actor_conversion_test.go`,
`decode_delivery_test.go`, `subsystem_exporter_internal_test.go`).

Create `internal/eventbus/rendering_publisher_internal_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package eventbus

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/core"
    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// internalFakePublisher is the in-package version of fakePublisher (defined
// in rendering_publisher_test.go in package eventbus_test). Duplicated
// here because in-package tests cannot import the _test package.
type internalFakePublisher struct{}

func (internalFakePublisher) Publish(_ context.Context, _ Event) error { return nil }

// TestValidateRenderingRejectsSpeechWithoutLabel exercises INV-GW-4 by
// calling the publisher's private validation helper with a malformed
// proto (format=speech, label empty). Tests the protovalidate path
// directly, without bypassing VerbRegistry.Register's belt-and-braces
// check.
func TestValidateRenderingRejectsSpeechWithoutLabel(t *testing.T) {
    rp := NewRenderingPublisher(internalFakePublisher{}, core.NewVerbRegistry())

    bad := &corev1.RenderingMetadata{
        Category:            "communication",
        Format:              "speech",  // requires label
        Label:               "",        // missing — CEL rule fails
        DisplayTarget:       corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
        SourcePlugin:        "core-communication",
        SourcePluginVersion: "0.1.0",
    }

    err := rp.validateRendering(bad)
    require.Error(t, err)
    assert.Contains(t, err.Error(), "label", "validator error should mention the failing constraint")
}

// TestValidateRenderingRejectsUnspecifiedDisplayTarget exercises the
// proto-layer enum.not_in: [0] check (INV-GW-8 mechanism).
func TestValidateRenderingRejectsUnspecifiedDisplayTarget(t *testing.T) {
    rp := NewRenderingPublisher(internalFakePublisher{}, core.NewVerbRegistry())

    bad := &corev1.RenderingMetadata{
        Category:            "communication",
        Format:              "narrative",
        Label:               "",
        DisplayTarget:       corev1.EventChannel_EVENT_CHANNEL_UNSPECIFIED,  // forbidden
        SourcePlugin:        "core-communication",
        SourcePluginVersion: "0.1.0",
    }

    err := rp.validateRendering(bad)
    require.Error(t, err)
}

// TestValidateRenderingAcceptsWellFormed sanity check.
func TestValidateRenderingAcceptsWellFormed(t *testing.T) {
    rp := NewRenderingPublisher(internalFakePublisher{}, core.NewVerbRegistry())

    good := &corev1.RenderingMetadata{
        Category:            "communication",
        Format:              "speech",
        Label:               "says",
        DisplayTarget:       corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
        SourcePlugin:        "core-communication",
        SourcePluginVersion: "0.1.0",
    }

    err := rp.validateRendering(good)
    require.NoError(t, err)
}
```

In `package eventbus`, package-local symbols (`Event`,
`NewRenderingPublisher`, `validateRendering`) are referenced without a
qualifier; only foreign-package types like `corev1.RenderingMetadata`
keep their qualifier. Verify the package convention with:

```bash
rg -l "^package eventbus$" internal/eventbus/*_test.go
```

Expected: lists `actor_conversion_test.go`, `decode_delivery_test.go`,
`subsystem_exporter_internal_test.go` — confirming the convention.

- [ ] **Step 3: Run the tests to verify**

```bash
task test -- -run 'TestValidateRendering' ./internal/eventbus/
```

Expected: PASS for all three (assuming the validator step is added to `Publish` per Step 1).

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(eventbus): RenderingPublisher runs protovalidate (INV-GW-4)"
jj new
```

---

#### Task 15: `INV-GW-9` — `source_plugin` and `source_plugin_version` populated; "builtin" + "host-<version>"

**Files:**

- Modify: `internal/eventbus/rendering_publisher_test.go`

- [ ] **Step 1: Write the failing test**

Add:

```go
// TestRenderingPublisherSourcePluginVersionForBuiltin is INV-GW-9 for builtins.
// host-owned event types (registered via BootstrapVerbRegistry) MUST have
// source_plugin == "builtin" and source_plugin_version == "host-<binary version>".
func TestRenderingPublisherSourcePluginVersionForBuiltin(t *testing.T) {
    r, err := core.BootstrapVerbRegistry("0.4.2-test")
    require.NoError(t, err)

    inner := &fakePublisher{}
    rp := eventbus.NewRenderingPublisher(inner, r)

    ev := eventbus.Event{
        ID:      ulid.Make(),
        Subject: eventbus.Subject("events.main.character.01ABC"),
        Type:    eventbus.Type("arrive"),  // builtin
        Actor:   eventbus.Actor{Kind: eventbus.ActorKindSystem},
        Payload: []byte(`{}`),
    }
    require.NoError(t, rp.Publish(context.Background(), ev))

    require.Len(t, inner.published, 1)
    got := inner.published[0].Rendering
    require.NotNil(t, got)
    assert.Equal(t, "builtin", got.SourcePlugin)
    assert.Equal(t, "host-0.4.2-test", got.SourcePluginVersion)
}

// TestRenderingPublisherSourcePluginVersionForPlugin is INV-GW-9 for plugins.
// Plugin-owned event types MUST have source_plugin = manifest name and
// source_plugin_version = manifest version.
func TestRenderingPublisherSourcePluginVersionForPlugin(t *testing.T) {
    r, err := core.BootstrapVerbRegistry("0.4.2-test")
    require.NoError(t, err)
    require.NoError(t, r.RegisterWithSource(core.VerbRegistration{
        Type:          "core-communication:say",
        Category:      "communication",
        Format:        "speech",
        Label:         "says",
        DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
        Source:        "core-communication",
    }, "0.1.0"))

    inner := &fakePublisher{}
    rp := eventbus.NewRenderingPublisher(inner, r)

    ev := eventbus.Event{
        ID: ulid.Make(), Subject: eventbus.Subject("events.main.character.01ABC"),
        Type: eventbus.Type("core-communication:say"),
        Actor: eventbus.Actor{Kind: eventbus.ActorKindCharacter},
        Payload: []byte(`{}`),
    }
    require.NoError(t, rp.Publish(context.Background(), ev))

    require.Len(t, inner.published, 1)
    got := inner.published[0].Rendering
    require.NotNil(t, got)
    assert.Equal(t, "core-communication", got.SourcePlugin)
    assert.Equal(t, "0.1.0", got.SourcePluginVersion)
}
```

- [ ] **Step 2: Run**

```bash
task test -- -run 'TestRenderingPublisherSourcePluginVersion' ./internal/eventbus/
```

Expected: PASS — implementation from Tasks 11-12 already populates these correctly. (This task is the explicit invariant-test addition.)

- [ ] **Step 3: Commit**

```bash
jj describe -m "test(eventbus): assert source_plugin/version on Rendering (INV-GW-9)"
jj new
```

---

## PHASE P6 — WIRE EVENTBUS THROUGH

### Task 16: `gRPC Subscribe`: copy `Rendering` to `EventFrame`

**Files:**

- Modify: `internal/grpc/server.go`

- [ ] **Step 1: Read the current implementation**

Locate `toProtoSubscribeResponse` at `internal/grpc/server.go:566` (or grep for it). Current shape:

```go
func (s *CoreServer) toProtoSubscribeResponse(ev eventbus.Event) *corev1.SubscribeResponse {
    gameID := s.currentGameID()
    return &corev1.SubscribeResponse{
        Frame: &corev1.SubscribeResponse_Event{
            Event: &corev1.EventFrame{
                Id:        ev.ID.String(),
                Stream:    subjectxlate.ToLegacy(string(ev.Subject), gameID),
                Type:      string(ev.Type),
                Timestamp: timestamppb.New(ev.Timestamp),
                ActorType: ev.Actor.Kind.String(),
                ActorId:   actorIDString(ev.Actor),
                Payload:   ev.Payload,
                Cursor:    encodeEventCursor(ev),
            },
        },
    }
}
```

- [ ] **Step 2: Add `Rendering` field**

Edit the `EventFrame{...}` literal to include:

```go
                Rendering: eventbus.RenderingToProto(ev.Rendering),
```

If `eventbus` isn't imported in `server.go`, it already is (the file uses `eventbus.Event`).

- [ ] **Step 3: Run the existing subscribe tests**

```bash
task test -- ./internal/grpc/
```

Expected: PASS (existing tests construct events without Rendering — `RenderingToProto(nil)` returns nil).

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(grpc): Subscribe response copies Rendering to EventFrame"
jj new
```

---

#### Task 17: `gRPC QueryStreamHistory`: copy `Rendering` to `EventFrame`

**Files:**

- Modify: `internal/grpc/query_stream_history.go`

- [ ] **Step 1: Locate the EventFrame builders**

```bash
rg -n 'EventFrame\{' internal/grpc/query_stream_history.go
```

Spec referenced lines 458 and 486. Both build `EventFrame` values from `eventbus.Event`.

- [ ] **Step 2: Add `Rendering` to each builder**

For each `&corev1.EventFrame{...}` literal in the file, add:

```go
                Rendering: eventbus.RenderingToProto(ev.Rendering),
```

(Use the appropriate variable name — likely `ev` or `event`.)

- [ ] **Step 3: Run tests**

```bash
task test -- ./internal/grpc/
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(grpc): QueryStreamHistory copies Rendering to EventFrame"
jj new
```

---

## PHASE P7 — CORE BOOT WIRING

### Task 18: `cmd/holomush/core.go` — call `BootstrapVerbRegistry(version)` and thread into subsystem configs

**Files:**

- Modify: `cmd/holomush/core.go`
- Modify: `cmd/holomush/sub_grpc.go` (add `VerbRegistry` field to `grpcSubsystemConfig`)
- Modify: `internal/plugin/setup/subsystem.go` (add `VerbRegistry` field if absent)

- [ ] **Step 1: Locate the bootstrap section**

Find where the core-process bootstrap currently happens (around line 162 per the gateway-style call). The package-level `version` variable is in `cmd/holomush/main.go:14`.

- [ ] **Step 2: Add `VerbRegistry` field to subsystem configs**

In `cmd/holomush/sub_grpc.go`, find the `grpcSubsystemConfig` struct (or whatever the gRPC subsystem's config struct is named) and add:

```go
type grpcSubsystemConfig struct {
    // ... existing fields ...
    VerbRegistry *core.VerbRegistry  // populated by runCoreWithDeps from BootstrapVerbRegistry
}
```

Same for `internal/plugin/setup/subsystem.go` — its config struct is `PluginSubsystemConfig` (verified at line 71). Add `VerbRegistry *core.VerbRegistry` to it. Task 19 step 3 wires it via `plugin.WithVerbRegistry(...)` option, but the subsystem needs a config-level slot to pass it through from `runCoreWithDeps`.

- [ ] **Step 3: Add the registry construction in `runCoreWithDeps`**

Near the start of `runCoreWithDeps`, after the dependency factories are
set up but before the gRPC server / plugin manager constructions, add:

```go
verbRegistry, err := core.BootstrapVerbRegistry(version)
if err != nil {
    return oops.Code("VERB_REGISTRY_BOOTSTRAP_FAILED").Wrap(err)
}
```

If `core` isn't imported in `core.go`, add `"github.com/holomush/holomush/internal/core"`.

- [ ] **Step 4: Thread `verbRegistry` into the subsystem configs (inline-literal addition)**

`cmd/holomush/core.go` constructs each subsystem config as an **inline
struct literal** passed directly to a constructor:

- around `core.go:280`: `pluginsetup.NewPluginSubsystem(pluginsetup.PluginSubsystemConfig{...})`
- around `core.go:402`: `newGRPCSubsystem(grpcSubsystemConfig{...})`

There are no `grpcCfg` / `pluginSubsystemCfg` named variables to mutate.
Add `VerbRegistry: verbRegistry,` as a new line inside each existing
inline literal. Worked example for the gRPC subsystem:

```go
// Before (cmd/holomush/core.go:402, approximate):
grpcSys := newGRPCSubsystem(grpcSubsystemConfig{
    DB:        deps.DB,
    ABAC:      deps.ABAC,
    World:     deps.World,
    Auth:      deps.Auth,
    Sessions:  deps.Sessions,
    Bootstrap: deps.Bootstrap,
    Plugins:   deps.Plugins,
    EventBus:  deps.EventBus,
    // ... existing fields ...
})

// After:
grpcSys := newGRPCSubsystem(grpcSubsystemConfig{
    DB:           deps.DB,
    ABAC:         deps.ABAC,
    World:        deps.World,
    Auth:         deps.Auth,
    Sessions:     deps.Sessions,
    Bootstrap:    deps.Bootstrap,
    Plugins:      deps.Plugins,
    EventBus:     deps.EventBus,
    VerbRegistry: verbRegistry,  // NEW: Phase 1.6 wiring
    // ... existing fields ...
})
```

Same pattern for `PluginSubsystemConfig` at `core.go:280`. The exact
list of pre-existing fields varies; preserve them and add the new
field at the end.

- [ ] **Step 5: Verify build**

```bash
task lint
task test -- ./cmd/holomush/
```

Expected: lint PASS, tests PASS. Existing tests don't construct `verbRegistry` so this addition doesn't break them.

- [ ] **Step 6: Commit**

```bash
jj describe -m "feat(cmd): core.go bootstraps VerbRegistry at startup"
jj new
```

---

#### Task 19: `cmd/holomush/sub_grpc.go` — wire `RenderingPublisher` into both consumers + AC#10 unit test

**Files:**

- Modify: `cmd/holomush/sub_grpc.go`
- Test: `cmd/holomush/sub_grpc_test.go`

**Approach:** rather than refactor `(*grpcSubsystem).Start` (a 282-line method whose extraction is nontrivial — see plan-review round 2 finding B2), make a single-purpose helper `(s *grpcSubsystem) wrapPublisher(rawPub eventbus.Publisher) (eventbus.Publisher, error)` that wraps the publisher with `RenderingPublisher`. The helper is callable in isolation by the unit test. Production code in `Start` calls it inline.

- [ ] **Step 1: Add the `wrapPublisher` helper**

In `cmd/holomush/sub_grpc.go`, near other helper methods on `grpcSubsystem`, add:

```go
// wrapPublisher wraps the raw EventBus publisher with RenderingPublisher
// so all emit-site callers (pluginManager and busEventAppender) get
// rendering-metadata enrichment for free. Returns an error if the verb
// registry is not configured.
func (s *grpcSubsystem) wrapPublisher(raw eventbus.Publisher) (eventbus.Publisher, error) {
    if s.cfg.VerbRegistry == nil {
        return nil, oops.Code("GRPC_VERB_REGISTRY_MISSING").
            Errorf("gRPC subsystem requires VerbRegistry for emit-time rendering enrichment")
    }
    return eventbus.NewRenderingPublisher(raw, s.cfg.VerbRegistry), nil
}
```

`s.cfg.VerbRegistry` is a new field on the subsystem config struct (Task 18 + 19 add it together). Update the config struct and the `runCoreWithDeps` wiring to populate it from the bootstrapped registry.

- [ ] **Step 2: Use the helper in `Start`**

Modify `(*grpcSubsystem).Start` lines 144-159. Existing code:

```go
publisher := s.cfg.EventBus.Publisher()
if publisher == nil {
    return oops.Code("GRPC_EVENTBUS_NOT_STARTED").
        Errorf("EventBus publisher is nil; subsystem not started")
}
pluginManager.ConfigureEventEmitter(
    publisher,
    plugins.WithGameID(s.cfg.EventBus.GameID),
)

eventStore := &busEventAppender{
    publisher: publisher,
    bus:       s.cfg.EventBus,
}
```

Replace with:

```go
rawPublisher := s.cfg.EventBus.Publisher()
if rawPublisher == nil {
    return oops.Code("GRPC_EVENTBUS_NOT_STARTED").
        Errorf("EventBus publisher is nil; subsystem not started")
}

publisher, err := s.wrapPublisher(rawPublisher)
if err != nil {
    return err
}

pluginManager.ConfigureEventEmitter(
    publisher,
    plugins.WithGameID(s.cfg.EventBus.GameID),
)

eventStore := &busEventAppender{
    publisher: publisher,
    bus:       s.cfg.EventBus,
}
```

Both consumers now receive `publisher` (the wrapped instance).

- [ ] **Step 3: Wire `VerbRegistry` into the plugin manager construction**

This step lives in a different file than the rest of Task 19 — the
plugin manager is constructed inside `*PluginSubsystem.Start` at
`internal/plugin/setup/subsystem.go` (around line 263, where
`managerOpts := []plugins.ManagerOption{...}` is built).

Edit `internal/plugin/setup/subsystem.go`:

```go
// Inside *PluginSubsystem.Start, in the managerOpts build (line ~263):
managerOpts := []plugins.ManagerOption{
    // ... existing options ...
    plugins.WithVerbRegistry(s.cfg.VerbRegistry),  // NEW: Phase 1.6
}
```

`s.cfg.VerbRegistry` reads from the `PluginSubsystemConfig` field added
in Task 18 step 2. After Task 20 lands the constructor-time nil check
(`INV-GW-10`), this becomes load-bearing — but adding it here in Task 19
is safe because the existing `if m.verbRegistry != nil` guard at
`manager.go:783` is still in effect.

- [ ] **Step 4: Write the AC#10 unit test**

Add to `cmd/holomush/sub_grpc_test.go`:

```go
// TestGrpcSubsystemWrapPublisher is AC#10. Calling wrapPublisher on a
// configured subsystem MUST return a *RenderingPublisher.
func TestGrpcSubsystemWrapPublisher(t *testing.T) {
    registry, err := core.BootstrapVerbRegistry("test")
    require.NoError(t, err)

    s := &grpcSubsystem{
        cfg: grpcSubsystemConfig{
            VerbRegistry: registry,
            // Other cfg fields can be zero values for this test —
            // wrapPublisher only reads VerbRegistry.
        },
    }

    raw := &fakeEventbusPublisher{}  // existing test fake or a tiny stub
    wrapped, err := s.wrapPublisher(raw)
    require.NoError(t, err)

    _, ok := wrapped.(*eventbus.RenderingPublisher)
    assert.True(t, ok, "wrapPublisher must return *eventbus.RenderingPublisher")
}

// TestGrpcSubsystemWrapPublisherWithoutRegistry asserts the error path
// when VerbRegistry is missing.
func TestGrpcSubsystemWrapPublisherWithoutRegistry(t *testing.T) {
    s := &grpcSubsystem{cfg: grpcSubsystemConfig{}}  // no registry
    _, err := s.wrapPublisher(&fakeEventbusPublisher{})
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "GRPC_VERB_REGISTRY_MISSING")
}
```

If `fakeEventbusPublisher` doesn't exist, define it inline (matches the
pattern used in earlier RenderingPublisher tests):

```go
type fakeEventbusPublisher struct{}

func (f *fakeEventbusPublisher) Publish(_ context.Context, _ eventbus.Event) error {
    return nil
}
```

- [ ] **Step 5: Run**

```bash
task test -- -run 'TestGrpcSubsystemWrapPublisher' ./cmd/holomush/
task lint
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
jj describe -m "feat(cmd): wrapPublisher wires RenderingPublisher into pluginManager and busEventAppender (AC#10)"
jj new
```

---

## PHASE P8 — PLUGIN MANAGER TIGHTENING + REMAINING PIECES

### Task 20: Plugin manager — `ErrMissingVerbRegistry`, required check, `INV-GW-10` test

**Files:**

- Modify: `internal/plugin/manager.go`
- Test: `internal/plugin/manager_test.go`
- Migration: per the Task 0 survey

- [ ] **Step 1: Pre-flight migration**

Open `docs/superpowers/plans/2026-04-26-task-0-survey.md` (from Task 0) and apply each row's migration:

- Pure construction tests → `core.NewVerbRegistry()`
- Emit tests → `core.BootstrapVerbRegistry("test")` (+ explicit plugin verb registration if needed)
- Manifest-load tests → `core.NewVerbRegistry()`
- Builtin-emit tests → `core.BootstrapVerbRegistry("test")`

For each test site, edit the file to pass `WithVerbRegistry(...)` with the appropriate registry shape.

- [ ] **Step 2: Verify all tests still pass before tightening**

```bash
task test
```

Expected: PASS. (We migrated test sites but haven't tightened yet — nil-no-op still works for any sites we missed.)

- [ ] **Step 3: Write the failing test**

Add to `internal/plugin/manager_test.go`:

```go
// TestNewManagerRequiresVerbRegistry is INV-GW-10. A nil verbRegistry
// must produce ErrMissingVerbRegistry at construction time.
func TestNewManagerRequiresVerbRegistry(t *testing.T) {
    _, err := plugins.NewManager(/* … usual options without WithVerbRegistry … */)
    require.Error(t, err)
    require.ErrorIs(t, err, plugins.ErrMissingVerbRegistry)
}
```

- [ ] **Step 4: Run to verify failure**

```bash
task test -- -run TestNewManagerRequiresVerbRegistry ./internal/plugin/
```

Expected: FAIL.

- [ ] **Step 5: Implement the tightening**

In `internal/plugin/manager.go`, add at the top:

```go
// ErrMissingVerbRegistry is returned by NewManager when no VerbRegistry
// has been configured via WithVerbRegistry. INV-GW-10.
var ErrMissingVerbRegistry = oops.Code("MISSING_VERB_REGISTRY").
    Errorf("plugin manager requires a VerbRegistry; pass WithVerbRegistry(...)")
```

In `NewManager`, after applying all options, before returning, add:

```go
if m.verbRegistry == nil {
    return nil, ErrMissingVerbRegistry
}
```

- [ ] **Step 6: Run the test + full plugin tests**

```bash
task test -- ./internal/plugin/
```

Expected: PASS (the migration in Step 1 should have eliminated nil-registry sites).

- [ ] **Step 7: Commit**

```bash
jj describe -m "feat(plugin): require non-nil VerbRegistry at construction (INV-GW-10)"
jj new
```

---

#### Task 21: Plugin manager — `RegisterWithSource` call

**Files:**

- Modify: `internal/plugin/manager.go`

- [ ] **Step 1: Update the verb-registration loop**

In `internal/plugin/manager.go:782-805`, replace the existing `Register` call with `RegisterWithSource` and pass the manifest version:

```go
// Before:
if m.verbRegistry != nil && len(dp.Manifest.Verbs) > 0 {
    for _, vs := range dp.Manifest.Verbs {
        regErr := m.verbRegistry.Register(core.VerbRegistration{ ... })
        // ...
    }
}

// After (note: nil guard removed — INV-GW-10 ensures non-nil at construction):
for _, vs := range dp.Manifest.Verbs {
    regErr := m.verbRegistry.RegisterWithSource(core.VerbRegistration{
        Type:          vs.Type,
        Category:      vs.Category,
        Format:        vs.Format,
        Label:         vs.Label,
        DisplayTarget: displayTargetFromString(vs.DisplayTarget),
        Source:        dp.Manifest.Name,
    }, dp.Manifest.Version)
    if regErr != nil {
        // existing rollback path — unchanged
        ...
    }
}
```

The `if m.verbRegistry != nil` guard at line 783 is removed.

- [ ] **Step 2: Run plugin tests**

```bash
task test -- ./internal/plugin/
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
jj describe -m "feat(plugin): use RegisterWithSource so manifest version flows into rendering"
jj new
```

---

#### Task 22: `VerbRegistration.DisplayTarget` → `corev1.EventChannel`

**Files:**

- Modify: `internal/core/registry.go`
- Modify: `internal/core/builtins.go`
- Modify: `internal/plugin/manager.go` (`displayTargetFromString` helper)
- Modify: test fixtures across `internal/web/translate_test.go`, `internal/telnet/gateway_handler_test.go`, `cmd/holomush/gateway_test.go`, `internal/plugin/integration_test.go`, `test/integration/plugin/verb_registration_test.go`

- [ ] **Step 1: Change the field type**

In `internal/core/registry.go`:

```go
// Before:
import webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"

type VerbRegistration struct {
    // ...
    DisplayTarget webv1.EventChannel
    // ...
}

// After:
import corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"

type VerbRegistration struct {
    // ...
    DisplayTarget corev1.EventChannel
    // ...
}
```

If `webv1` is no longer needed in the file, remove the import.

- [ ] **Step 2: Update the builtins**

In `internal/core/builtins.go`, replace every `webv1.EventChannel_*` constant with `corev1.EventChannel_*`. Sample:

```go
// Before:
{Type: "arrive", Category: "movement", Format: "notification", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_BOTH, Source: "builtin"},

// After:
{Type: "arrive", Category: "movement", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_BOTH, Source: "builtin"},
```

Update the import accordingly.

- [ ] **Step 3: Update `displayTargetFromString`**

In `internal/plugin/manager.go`, find `displayTargetFromString` (around line 1242). Change its return type from `webv1.EventChannel` to `corev1.EventChannel` and update the constants it returns.

- [ ] **Step 4: Update test fixtures**

Run a global rename across test files:

```bash
rg -l 'webv1\.EventChannel_EVENT' internal/ cmd/ test/ --type go
```

For each hit (excluding `internal/web/translate.go` which keeps the boundary conversion), replace `webv1.EventChannel_EVENT_CHANNEL_*` with `corev1.EventChannel_EVENT_CHANNEL_*` and update imports.

- [ ] **Step 5: Verify build and tests**

```bash
task lint
task test
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
jj describe -m "refactor(core): VerbRegistration.DisplayTarget uses corev1.EventChannel"
jj new
```

---

#### Task 23: Cold-tier reader populates `Event.Rendering`

**Files:**

- Modify: `internal/eventbus/history/cold_postgres.go`

- [ ] **Step 1: Locate the cold-tier reader**

```bash
rg -n 'func .*Read|SELECT' internal/eventbus/history/cold_postgres.go | head -10
```

The reader queries `events_audit` rows and constructs `eventbus.Event` values. Currently it doesn't read the `rendering` column (it doesn't exist yet — Task 24 adds it).

- [ ] **Step 2: Add the column to the SELECT and parse it**

Modify the `SELECT` clause to include `rendering`:

```sql
SELECT id, subject, type, timestamp, actor_kind, actor_id, payload, schema_ver, codec, js_seq, rendering
FROM events_audit
WHERE ...
```

In the row-scanning code, scan the `rendering` JSONB column into `[]byte` and convert via `RenderingFromProto(protojson.Unmarshal(...))`:

```go
var renderingBytes []byte
if err := rows.Scan(&idBytes, &subject, &typ, &ts, &actorKind, &actorID, &payload, &schemaVer, &codec, &jsSeq, &renderingBytes); err != nil {
    return nil, err
}

var rendering *eventbus.RenderingMetadata
if len(renderingBytes) > 0 {
    var protoMD corev1.RenderingMetadata
    if err := protojson.Unmarshal(renderingBytes, &protoMD); err != nil {
        return nil, oops.Code("HISTORY_BAD_RENDERING").Wrap(err)
    }
    rendering = eventbus.RenderingFromProto(&protoMD)
}

ev := eventbus.Event{
    // ... existing fields ...
    Rendering: rendering,
}
```

NOTE: The `events_audit.rendering` column doesn't exist yet — Task 24 adds it. To unblock testing, this task can be merged with Task 24 OR simply deferred until after Task 24 lands.

**Plan choice:** do Task 24 first (the migration), then this task. Reorder if needed during execution.

- [ ] **Step 3: Build check**

```bash
task lint
```

- [ ] **Step 4: Commit (combined with Task 24)**

Defer commit until Task 24 lands the migration. See Task 24.

---

#### Task 24: Migration `000012_events_audit_rendering` + audit projection writer (single commit)

**Files:**

- Create: `internal/store/migrations/000012_events_audit_rendering.up.sql`
- Create: `internal/store/migrations/000012_events_audit_rendering.down.sql`
- Modify: `internal/eventbus/audit/projection.go`
- Modify: `internal/eventbus/audit/projection_unit_test.go` (or `projection_test.go`)

**Why bundled:** The migration adds `rendering JSONB NOT NULL` (no default). The projection's existing INSERT statement does NOT yet supply the column. Landing the migration alone causes every subsequent audit insert to fail. So the migration MUST land in the same commit as the projection writer update. (See plan-review round 3 finding 3.)

- [ ] **Step 1: Write the up migration**

Create `internal/store/migrations/000012_events_audit_rendering.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE events_audit
  ADD COLUMN rendering JSONB NOT NULL;
```

- [ ] **Step 2: Write the down migration**

Create `internal/store/migrations/000012_events_audit_rendering.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE events_audit DROP COLUMN rendering;
```

- [ ] **Step 3: Update the audit projection to read `App-Rendering` and write the column**

In `internal/eventbus/audit/projection.go`, in the `persist` function
(around line 166-255), add a new header read after the existing header
reads:

```go
const headerRendering = "App-Rendering"

// In persist, after existing header reads:
renderingJSON := h.Get(headerRendering)
if renderingJSON == "" {
    return oops.Code("AUDIT_MISSING_HEADER").
        With("header", headerRendering).
        Errorf("missing header")
}
```

Update the `INSERT` statement to include the `rendering` column:

```go
_, err = p.pool.Exec(ctx, `
    INSERT INTO events_audit (
        id, subject, type, timestamp, actor_kind, actor_id,
        payload, schema_ver, codec, js_seq, rendering
    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
    ON CONFLICT (id) DO NOTHING`,
    idBytes, msg.Subject(), eventType, meta.Timestamp,
    actorKind, actorID, msg.Data(), ver, codec, meta.Sequence.Stream,
    renderingJSON,
)
```

PostgreSQL JSONB validation handles malformed JSON naturally (insert fails with `invalid input syntax for type jsonb`).

- [ ] **Step 4: Add a unit test for header-missing failure**

Add to `internal/eventbus/audit/projection_unit_test.go`. The test uses
the existing `&stubMsg{...}` literal pattern (struct, not function — see
`projection_unit_test.go:23-29` for fields), the existing
`newTestProjection()` helper (`:63`), and `validHeaders(t)` (`:49`)
which builds a minimally-valid header map. `validHeaders` does NOT set
`App-Rendering`, so calling `persist` against it directly exercises
the negative case:

```go
// TestPersistRejectsMissingAppRenderingHeader is INV-GW-13 (negative case):
// a missing App-Rendering header MUST return AUDIT_MISSING_HEADER.
func TestPersistRejectsMissingAppRenderingHeader(t *testing.T) {
    p := newTestProjection()
    h := validHeaders(t)
    // validHeaders does not set App-Rendering — perfect for the negative case.
    msg := &stubMsg{
        headers: h,
        subject: "events.main.character.01ABC",
        meta:    &jetstream.MsgMetadata{Sequence: jetstream.SequencePair{Stream: 1}},
    }
    err := p.persist(msg)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "AUDIT_MISSING_HEADER")
    errutil.AssertErrorContext(t, err, "header", "App-Rendering")
}
```

- [ ] **Step 5: Update `validHeaders` to stamp `App-Rendering`**

Every existing positive test in `projection_unit_test.go` builds its
header map via `validHeaders(t)` at line 49. Centralize the App-Rendering
addition there so all existing positive tests pick it up:

```go
// In internal/eventbus/audit/projection_unit_test.go, function validHeaders:
func validHeaders(t *testing.T) nats.Header {
    t.Helper()
    h := nats.Header{}
    h.Set(headerMsgID, ulid.Make().String())
    h.Set(headerCodec, "identity")
    h.Set(headerEventType, "test.unit")
    h.Set(headerSchemaVersion, "1")
    h.Set(headerActorKind, defaultActorKind)
    // Phase 1.6 addition: App-Rendering is now a required header per
    // INV-GW-13. validHeaders represents a minimally-valid header set,
    // so it must include App-Rendering.
    h.Set("App-Rendering",
        `{"category":"system","format":"narrative",`+
        `"display_target":"EVENT_CHANNEL_TERMINAL","source_plugin":"builtin",`+
        `"source_plugin_version":"host-test","label":""}`)
    return h
}
```

(Note: `"label":""` is fine when format is not `"speech"`.) The header
constant `App-Rendering` is also worth declaring as
`headerRendering = "App-Rendering"` in the projection package alongside
the existing `headerMsgID`/`headerCodec`/etc. constants
(`projection.go` near the top), then used both in `persist` and the
test fixture.

- [ ] **Step 6: Run audit unit tests**

```bash
task test -- ./internal/eventbus/audit/
```

Expected: PASS.

- [ ] **Step 7: Commit (migration + projection writer + test updates together)**

```bash
jj describe -m "feat(audit): events_audit.rendering JSONB NOT NULL + projection writes from App-Rendering header (INV-GW-13)

Bundled to avoid the broken intermediate state where the migration adds
NOT NULL but the projection writer doesn't yet supply the column."
jj new
```

---

#### Task 25: (merged into Task 24)

The original Task 25 (audit projection writer) was merged into Task 24
to avoid the broken intermediate state. Task numbering retains a gap at
25 to preserve numeric stability across plan-reviewer rounds.

---

#### Task 26: Integration test for `INV-GW-6` and `INV-GW-13`

**Files:**

- Create: `test/integration/eventbus_e2e/rendering_completeness_test.go`

The test colocates with the existing eventbus integration suite at
`test/integration/eventbus_e2e/` (verified via
`audit_drift_detector_test.go:1-50` for the harness pattern: `eventbustest.New(t)`,
`freshPool(t)`, `audit.NewSubsystem(...)`, `bus.Bus.Publisher()`).

The project's existing eventbus integration tests use plain testify, not
Ginkgo (verified via `rg "RegisterFailHandler" test/integration/eventbus_e2e/`
returns no hits). Match that style.

- [ ] **Step 1: Write the integration test**

Create `test/integration/eventbus_e2e/rendering_completeness_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/internal/eventbus"
    "github.com/holomush/holomush/internal/eventbus/audit"
    "github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// TestRenderingCompleteness is INV-GW-6 + INV-GW-13. After publishing
// events of mixed builtin types, every events_audit row MUST have a
// non-null `rendering` JSONB column populated from the App-Rendering
// header. The audit projection rejects (returns AUDIT_MISSING_HEADER)
// any message lacking the header, so this test is a positive case:
// publishes flow through RenderingPublisher → JetStream → projection →
// PG with rendering populated.
func TestRenderingCompleteness(t *testing.T) {
    ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
    defer cancel()

    bus := eventbustest.New(t)
    pool := freshPool(t)

    // Stand up the host audit projection so publishes reach events_audit.
    hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
    require.NoError(t, hostSub.Start(ctx))
    t.Cleanup(func() { _ = hostSub.Stop(context.Background()) })

    // Build the wrapped publisher: BootstrapVerbRegistry + RenderingPublisher.
    registry, err := core.BootstrapVerbRegistry("test-0.1")
    require.NoError(t, err)
    rawPub := bus.Bus.Publisher()
    pub := eventbus.NewRenderingPublisher(rawPub, registry)

    // Publish three host-builtin events of different types.
    types := []eventbus.Type{"arrive", "leave", "system"}
    for i, typ := range types {
        ev := eventbus.Event{
            ID:        core.NewULID(),
            Subject:   eventbus.Subject("events.main.test." + typ),
            Type:      typ,
            Timestamp: time.Now().UTC(),
            Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
            Payload:   []byte(`{}`),
        }
        require.NoError(t, pub.Publish(ctx, ev), "publish %d type=%s", i, typ)
    }

    // Wait for the projection to drain: poll events_audit count until
    // it matches the publish count.
    require.Eventually(t, func() bool {
        var count int
        err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM events_audit").Scan(&count)
        return err == nil && count >= len(types)
    }, 10*time.Second, 100*time.Millisecond, "audit projection did not drain")

    // INV-GW-6: every row has a non-null rendering JSONB column.
    var nullCount int
    err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM events_audit WHERE rendering IS NULL").Scan(&nullCount)
    require.NoError(t, err)
    assert.Zero(t, nullCount, "INV-GW-6: every events_audit row MUST have non-null rendering")

    // INV-GW-13 spot-check: rendering column has the expected source_plugin.
    var sourcePlugin string
    err = pool.QueryRow(ctx,
        "SELECT rendering->>'source_plugin' FROM events_audit ORDER BY js_seq LIMIT 1",
    ).Scan(&sourcePlugin)
    require.NoError(t, err)
    assert.Equal(t, "builtin", sourcePlugin)
}
```

The helpers `freshPool(t)`, `fixedJS{}`, and `fixedPool{}` are
pre-existing in `test/integration/eventbus_e2e/` (verified by reading
`audit_drift_detector_test.go:38-48` for the same call shapes). If
`t.Context()` is unavailable on the project's Go version, substitute
`context.Background()`.

- [ ] **Step 2: Run it**

```bash
task test:int -- ./test/integration/eventbus_e2e/
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
jj describe -m "test(integration): events_audit.rendering completeness (INV-GW-6, INV-GW-13)"
jj new
```

---

## PHASE P9 — GATEWAY THINNESS

### Task 27: Delete gateway-side VerbRegistry and duplicate Prometheus metric registration

**Files:**

- Modify: `cmd/holomush/gateway.go`

- [ ] **Step 1: Verify the duplicate is duplicate**

```bash
rg -n 'command\.(CommandExecutions|CommandDuration|AliasExpansions)' cmd/holomush/
```

Expected: matches at `core.go:237` AND `gateway.go:263`. Confirm the registration target is the same Prometheus registry (likely both via `obsServer.MustRegister(...)` against the same `*prometheus.Registry` value or `prometheus.DefaultRegisterer`).

- [ ] **Step 2: Delete the gateway-side block**

In `cmd/holomush/gateway.go`:

1. Delete lines 286-289 (`verbRegistry := core.NewVerbRegistry()` plus the `RegisterBuiltinTypes` block).
2. Delete the `command.CommandExecutions, command.CommandDuration, command.AliasExpansions` registration around line 263.
3. Delete the `WithVerbRegistry(verbRegistry)` argument from `web.NewHandler(...)` and the telnet handler constructor calls (around lines 292 and 338).
4. Remove the `internal/command` and `internal/core` imports if no longer used (check what else uses them in this file).

After the edit, `gateway.go` should have ZERO domain imports per `INV-GW-1`.

- [ ] **Step 3: Verify build**

```bash
task lint
```

Expected: clean (assuming Tasks 28-30 update the Handler/translator to remove their `WithVerbRegistry` options).

If `task lint` flags `web.NewHandler` for an unsupported argument count, proceed to Task 28 (drop the option from the Handler). Tasks 27 and 28 must commit together OR Task 28 runs first.

- [ ] **Step 4: Combine Tasks 27 and 28 into one commit**

See Task 28.

---

#### Task 28: Drop `WithVerbRegistry` options from web Handler and telnet handler

**Files:**

- Modify: `internal/web/handler.go`
- Modify: `internal/telnet/gateway_handler.go`
- Modify: `internal/web/handler_test.go`, `internal/telnet/gateway_handler_test.go` (drop calls to the option)

- [ ] **Step 1: Drop the option in `web/handler.go`**

Find the `WithVerbRegistry` function (~line 89-90) and remove it. Remove the `verbRegistry` field on the `Handler` struct.

- [ ] **Step 2: Drop the option in `telnet/gateway_handler.go`**

Same: remove the option and field.

- [ ] **Step 3: Update test sites**

Find every call to `web.WithVerbRegistry(...)` and `telnet.WithVerbRegistry(...)` in tests; remove. Tests may also need to be updated to construct `EventFrame` values with explicit `Rendering` sub-messages (Task 29 covers this for translate.go).

- [ ] **Step 4: Build check**

```bash
task lint
```

Expected: clean.

- [ ] **Step 5: Commit Tasks 27 + 28 together**

```bash
jj describe -m "feat(gateway): delete VerbRegistry; drop WithVerbRegistry options; remove duplicate metric reg

Verifies via 'rg command\\.(CommandExecutions|CommandDuration|AliasExpansions) cmd/holomush/' that registration only exists in core.go:237."
jj new
```

---

#### Task 29: `internal/web/translate.go` — read `ev.GetRendering()`, nil-drop, conversion

**Files:**

- Modify: `internal/web/translate.go`
- Test: `internal/web/translate_test.go`

- [ ] **Step 1: Write failing tests for the new behavior**

In `internal/web/translate_test.go`, replace the registry-based fixture (`newTestHandler`) with EventFrame-based fixtures:

```go
func TestTranslateEventReadsRenderingOffEventFrame(t *testing.T) {
    h := &Handler{}  // no registry needed
    ev := &corev1.EventFrame{
        Type:      "core-communication:say",
        Timestamp: timestamppb.New(time.Now()),
        Payload:   mustMarshal(t, map[string]string{"character_name": "Alice", "message": "Hello!"}),
        Rendering: &corev1.RenderingMetadata{
            Category:            "communication",
            Format:              "speech",
            Label:               "says",
            DisplayTarget:       corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
            SourcePlugin:        "core-communication",
            SourcePluginVersion: "0.1.0",
        },
    }

    got := h.translateEvent(ev)
    require.NotNil(t, got)
    assert.Equal(t, "communication", got.GetCategory())
    assert.Equal(t, "speech", got.GetFormat())
    assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
    assert.Equal(t, "Alice", got.GetActor())
    assert.Equal(t, "Hello!", got.GetText())
}

// TestTranslateEventDropsNilRendering is INV-GW-5.
func TestTranslateEventDropsNilRendering(t *testing.T) {
    h := &Handler{}
    ev := &corev1.EventFrame{
        Type:    "core-communication:say",
        Payload: mustMarshal(t, map[string]string{"message": "Hello!"}),
        // Rendering is nil — contract violation.
    }

    got := h.translateEvent(ev)
    assert.Nil(t, got, "must drop event with nil Rendering")
}
```

- [ ] **Step 2: Run the test**

```bash
task test -- -run TestTranslateEvent ./internal/web/
```

Expected: FAIL — translate.go currently uses `verbRegistry.Lookup` which is being removed.

- [ ] **Step 3: Rewrite `translate.go`**

In `internal/web/translate.go`, replace the registry-lookup block (lines 44-62) with:

```go
rendering := ev.GetRendering()
if rendering == nil {
    slog.Error("web: dropping event with nil Rendering",
        "event_id", ev.GetId(),
        "event_type", ev.GetType(),
        "stream", ev.GetStream(),
    )
    metrics.GatewayDroppedNilRenderingTotal.WithLabelValues(ev.GetType()).Inc()
    return nil
}

category := rendering.GetCategory()
format := rendering.GetFormat()
label := rendering.GetLabel()
displayTarget := corevToWebV1EventChannel(rendering.GetDisplayTarget())
```

Add the conversion helper at the bottom of the file:

```go
// corevToWebV1EventChannel converts the canonical core/v1 enum to the
// web/v1 enum at the gateway-out boundary. Lockstep enforced by INV-GW-16.
func corevToWebV1EventChannel(c corev1.EventChannel) webv1.EventChannel {
    return webv1.EventChannel(c)
}
```

`metrics.GatewayDroppedNilRenderingTotal` is added in Task 31.

- [ ] **Step 4: Run the test**

```bash
task test -- -run TestTranslateEvent ./internal/web/
```

Expected: PASS, **provided Task 31 (the metric definition) is run first** so `metrics.GatewayDroppedNilRenderingTotal` exists. The phase order is: Task 28 → **Task 31 (metric)** → Task 29 (web translator) → Task 30 (telnet handler) → combined commit. The plan body keeps the original numbering for stability across review rounds; reorder execution accordingly.

- [ ] **Step 5: Commit (combined with Task 31)**

See Task 31.

---

#### Task 30: `internal/telnet/gateway_handler.go` — read `ev.GetRendering()`, nil-drop

**Files:**

- Modify: `internal/telnet/gateway_handler.go`
- Test: `internal/telnet/gateway_handler_test.go`

- [ ] **Step 1: Update `formatEvent` to read off `ev.GetRendering()`**

In `internal/telnet/gateway_handler.go`, the existing `formatEvent`
function (around line 864) does:

```go
// Before (verified at gateway_handler.go:864-893):
func (h *GatewayHandler) formatEvent(ev *corev1.EventFrame) string {
    if h.verbRegistry == nil {
        return h.formatFallback(ev)
    }
    reg, found := h.verbRegistry.Lookup(ev.GetType())
    if !found {
        return h.formatFallback(ev)
    }

    if reg.DisplayTarget != webv1.EventChannel_EVENT_CHANNEL_TERMINAL &&
        reg.DisplayTarget != webv1.EventChannel_EVENT_CHANNEL_BOTH {
        return ""
    }

    switch reg.Category {
    case "communication":
        return h.formatCommunication(ev, reg)
    // ... etc
    }
}
```

Rewrite to drive off `ev.GetRendering()`:

```go
// After:
func (h *GatewayHandler) formatEvent(ev *corev1.EventFrame) string {
    rendering := ev.GetRendering()
    if rendering == nil {
        // INV-GW-5: contract violation — drop with metric + log.
        slog.Error("telnet: dropping event with nil Rendering",
            "event_id", ev.GetId(),
            "event_type", ev.GetType(),
            "stream", ev.GetStream(),
        )
        metrics.GatewayDroppedNilRenderingTotal.WithLabelValues(ev.GetType()).Inc()
        return ""
    }

    // Only format events targeted at TERMINAL or BOTH.
    target := rendering.GetDisplayTarget()
    if target != corev1.EventChannel_EVENT_CHANNEL_TERMINAL &&
        target != corev1.EventChannel_EVENT_CHANNEL_BOTH {
        return ""
    }

    switch rendering.GetCategory() {
    case "communication":
        return h.formatCommunication(ev, rendering)
    case "movement":
        return h.formatMovement(ev, rendering)
    case "command":
        return h.formatCommand(ev, rendering)
    case "system":
        return h.formatSystem(ev)
    case "state":
        return "" // telnet has no sidebar
    default:
        // No registered format — drop silently (gateway is dumb).
        return ""
    }
}
```

- [ ] **Step 2: Update format-helper signatures from `core.VerbRegistration` to `*corev1.RenderingMetadata`**

The helpers `formatCommunication`, `formatMovement`, `formatCommand`
currently accept `reg core.VerbRegistration`. Change their second
parameter to `rendering *corev1.RenderingMetadata`. Inside each helper,
replace `reg.Format`, `reg.Label`, `reg.Category`, `reg.DisplayTarget`
with `rendering.GetFormat()`, `rendering.GetLabel()`,
`rendering.GetCategory()`, `rendering.GetDisplayTarget()`.

`formatFallback` and `formatSystem` keep their existing signatures
(they don't take `reg`).

- [ ] **Step 3: Delete the `verbRegistry` field and `WithVerbRegistry` option**

- Remove `verbRegistry *core.VerbRegistry` field from `GatewayHandler`
  struct (around line 73).
- Remove the `verbRegistry: registry,` line from the constructor (line 99).
- Remove the `WithVerbRegistry` option function (covered by Task 28
  but verify here).
- Remove `core` and `webv1` imports if no longer used.

- [ ] **Step 4: Update tests**

Find every `&GatewayHandler{verbRegistry: registry}` (5+ sites in
`gateway_handler_test.go`). Replace with `&GatewayHandler{}` (no
registry). Update fixture `EventFrame` values to include explicit
`Rendering` sub-messages:

```go
// Before:
ev := &corev1.EventFrame{
    Type:    "say",
    Payload: mustMarshal(map[string]any{"character_name": "Alice", "message": "hi"}),
}

// After:
ev := &corev1.EventFrame{
    Type:    "core-communication:say",
    Payload: mustMarshal(map[string]any{"character_name": "Alice", "message": "hi"}),
    Rendering: &corev1.RenderingMetadata{
        Category:            "communication",
        Format:              "speech",
        Label:               "says",
        DisplayTarget:       corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
        SourcePlugin:        "core-communication",
        SourcePluginVersion: "0.1.0",
    },
}
```

- [ ] **Step 5: Add INV-GW-5 nil-drop test**

```go
// TestFormatEventDropsNilRendering is INV-GW-5 for the telnet path.
func TestFormatEventDropsNilRendering(t *testing.T) {
    h := &GatewayHandler{}
    ev := &corev1.EventFrame{
        Id:   "01ABC",
        Type: "core-communication:say",
        Payload: mustMarshal(map[string]any{"message": "Hello"}),
        // Rendering: nil — contract violation.
    }
    got := h.formatEvent(ev)
    assert.Equal(t, "", got, "nil-rendering events MUST be dropped (no telnet output)")
}
```

- [ ] **Step 6: Run tests**

```bash
task test -- ./internal/telnet/
```

Expected: PASS. (Combined commit with Tasks 29 + 31 — see Task 31.)

---

#### Task 31: Add `holomush_gateway_dropped_nil_rendering_total` metric

**Files:**

- Create or modify: `internal/web/metrics.go` (or wherever gateway-side metrics live)

- [ ] **Step 1: Define the metric**

Find the existing gateway-side metrics file (likely `internal/web/metrics.go` or similar). Add:

```go
var GatewayDroppedNilRenderingTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Namespace: "holomush",
        Subsystem: "gateway",
        Name:      "dropped_nil_rendering_total",
        Help:      "Number of events dropped at the gateway because Rendering was nil (INV-GW-5 violation upstream).",
    },
    []string{"event_type"},
)
```

Register it in the gateway's metric-registration init() or wherever existing gateway metrics get registered.

- [ ] **Step 2: Wire `GatewayDroppedNilRenderingTotal` into translate.go and gateway_handler.go**

Already referenced in Tasks 29 and 30. Just confirm the imports resolve.

- [ ] **Step 3: Run gateway-side tests**

```bash
task test -- ./internal/web/ ./internal/telnet/
```

Expected: PASS.

- [ ] **Step 4: Commit Tasks 29 + 30 + 31 together**

```bash
jj describe -m "feat(gateway): translate.go + gateway_handler.go read EventFrame.Rendering, nil-drop with metric (INV-GW-5)"
jj new
```

---

## PHASE P10 — TRIPWIRE, META-TEST, ENUM PARITY

### Task 32: Import-graph tripwire test (`INV-GW-1`)

**Files:**

- Create: `cmd/holomush/gateway_imports_test.go`

- [ ] **Step 1: Create the test**

Create `cmd/holomush/gateway_imports_test.go` with the exact content from spec Section 4 lines 875-977 (the `TestGatewayImportsAreOnlyProtocolTranslation` function and its helpers).

Make sure to include `import "path/filepath"`.

- [ ] **Step 2: Run the test**

```bash
task test -- -run TestGatewayImportsAreOnlyProtocolTranslation ./cmd/holomush/
```

Expected: PASS — Task 27 already cleaned `gateway.go`; the inverse allowlist matches the file inventory at the spec's commit.

If FAIL with "import X is forbidden in <file>", verify the failing file is correctly classified in `coreOnlyFiles`. If the file is genuinely gateway-side and accidentally has a forbidden import, fix the source code; do not loosen the test.

- [ ] **Step 3: Commit**

```bash
jj describe -m "test(gateway): import-graph tripwire enforces gateway thinness (INV-GW-1)"
jj new
```

---

#### Task 33: Enum parity test (`INV-GW-16`)

**Files:**

- Modify: `internal/web/translate_test.go`

- [ ] **Step 1: Add the test**

```go
// TestEventChannelEnumsInLockstep is INV-GW-16. corev1.EventChannel and
// webv1.EventChannel MUST stay in lockstep — same enum values, same names,
// same numeric assignments.
func TestEventChannelEnumsInLockstep(t *testing.T) {
    cases := []struct {
        name string
        core corev1.EventChannel
        web  webv1.EventChannel
    }{
        {"UNSPECIFIED", corev1.EventChannel_EVENT_CHANNEL_UNSPECIFIED, webv1.EventChannel_EVENT_CHANNEL_UNSPECIFIED},
        {"TERMINAL", corev1.EventChannel_EVENT_CHANNEL_TERMINAL, webv1.EventChannel_EVENT_CHANNEL_TERMINAL},
        {"STATE", corev1.EventChannel_EVENT_CHANNEL_STATE, webv1.EventChannel_EVENT_CHANNEL_STATE},
        {"BOTH", corev1.EventChannel_EVENT_CHANNEL_BOTH, webv1.EventChannel_EVENT_CHANNEL_BOTH},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            assert.Equal(t, int32(c.core), int32(c.web), "numeric mismatch")
            // Names match by suffix (after EVENT_CHANNEL_).
            coreName := corev1.EventChannel_name[int32(c.core)]
            webName := webv1.EventChannel_name[int32(c.web)]
            assert.Equal(t, coreName, webName)
        })
    }
    // Length parity — neither side has extra values.
    assert.Equal(t, len(corev1.EventChannel_name), len(webv1.EventChannel_name))
}
```

- [ ] **Step 2: Run**

```bash
task test -- -run TestEventChannelEnumsInLockstep ./internal/web/
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
jj describe -m "test(web): EventChannel enum parity test (INV-GW-16)"
jj new
```

---

#### Task 34: Meta-test for invariant coverage

**Files:**

- Create: `test/integration/gateway_invariants/meta_test.go`

- [ ] **Step 1: Create the test**

```go
//go:build !integration

package gateway_invariants_test

import (
    "go/ast"
    "go/parser"
    "go/token"
    "path/filepath"
    "strings"
    "testing"

    "github.com/stretchr/testify/require"
)

// TestAllGatewayRegistryInvariantsHaveTests asserts that every numbered
// invariant from the spec has at least one test function whose name
// references it. Spec: docs/superpowers/specs/2026-04-26-gateway-verb-registry-sourcing.md
func TestAllGatewayRegistryInvariantsHaveTests(t *testing.T) {
    invariants := []string{
        "INV-GW-1",
        "INV-GW-2",
        "INV-GW-3",
        "INV-GW-3a",
        "INV-GW-4",
        "INV-GW-5",
        "INV-GW-6",
        "INV-GW-7",
        "INV-GW-8",
        "INV-GW-9",
        "INV-GW-10",
        "INV-GW-11",
        // INV-GW-12 is forward-declared for Phase 3 — excluded.
        "INV-GW-13",
        "INV-GW-14",
        "INV-GW-15",
        "INV-GW-16",
    }

    // Walk the test files we know enforce these invariants.
    testFiles := []string{
        "../../internal/eventbus/rendering_publisher_test.go",
        "../../internal/eventbus/publisher_test.go",
        "../../internal/eventbus/types_proto_sync_test.go",
        "../../internal/eventbus/audit/projection_test.go",
        "../../internal/web/translate_test.go",
        "../../internal/telnet/gateway_handler_test.go",
        "../../internal/core/registry_test.go",
        "../../internal/core/exports_test.go",
        "../../internal/plugin/manager_test.go",
        "../../cmd/holomush/gateway_imports_test.go",
        "../../test/integration/eventbus_e2e/rendering_completeness_test.go",
    }

    invariantToFile := make(map[string][]string)
    fset := token.NewFileSet()
    for _, path := range testFiles {
        absPath, err := filepath.Abs(path)
        require.NoError(t, err)
        f, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
        if err != nil {
            // File may not exist in every plan revision; warn only.
            t.Logf("skipping %s: %v", path, err)
            continue
        }
        // Collect invariant references from comments and function names.
        for _, cg := range f.Comments {
            for _, c := range cg.List {
                for _, inv := range invariants {
                    if strings.Contains(c.Text, inv) {
                        invariantToFile[inv] = append(invariantToFile[inv], path)
                    }
                }
            }
        }
        for _, decl := range f.Decls {
            fn, ok := decl.(*ast.FuncDecl)
            if !ok {
                continue
            }
            for _, inv := range invariants {
                if strings.Contains(fn.Name.Name, strings.ReplaceAll(inv, "-", "")) ||
                   (fn.Doc != nil && strings.Contains(fn.Doc.Text(), inv)) {
                    invariantToFile[inv] = append(invariantToFile[inv], path)
                }
            }
        }
    }

    for _, inv := range invariants {
        files := invariantToFile[inv]
        if len(files) == 0 {
            t.Errorf("invariant %s has no test referencing it", inv)
        }
    }
}
```

- [ ] **Step 2: Run**

```bash
task test -- -run TestAllGatewayRegistryInvariantsHaveTests ./test/integration/gateway_invariants/
```

Expected: PASS for `INV-GW-1` through `INV-GW-16`, with the `INV-GW-12` exclusion noted.

- [ ] **Step 3: Commit**

```bash
jj describe -m "test(meta): assert every gateway-registry invariant has a test"
jj new
```

---

## PHASE P11 — DOCUMENTATION

### Task 35: `site/docs/contributing/gateway-boundary.md`

**Files:**

- Create: `site/docs/contributing/gateway-boundary.md`

- [ ] **Step 1: Write the doc**

Create `site/docs/contributing/gateway-boundary.md`:

```markdown
# Gateway Boundary

The gateway process (`cmd/holomush gateway`) is a **protocol translation

- connection management layer**. It MUST NOT hold domain state, perform
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

The CI tripwire test (`cmd/holomush/gateway_imports_test.go`,
`INV-GW-1`) enforces that gateway-side packages MUST NOT import:

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
```

- [ ] **Step 2: Verify the doc builds**

```bash
task docs:build
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
jj describe -m "docs(contributing): gateway boundary rule and tripwire enforcement"
jj new
```

---

#### Task 36: `site/docs/contributing/event-emit-pipeline.md`

**Files:**

- Create: `site/docs/contributing/event-emit-pipeline.md`

- [ ] **Step 1: Write the doc**

Create with content covering:

- The publisher chain: `EventBus.Publisher()` → `RenderingPublisher` → `JetStreamPublisher`
- The two consumers: `pluginManager.ConfigureEventEmitter` and `busEventAppender`
- Rule: any new publisher consumer MUST receive the wrapped publisher
- The two transports for `RenderingMetadata`: proto envelope + `App-Rendering` NATS header
- Why both transports exist (audit projection avoids codec decode)
- Reference to `INV-GW-15` (parity test)

Keep it under 200 lines.

- [ ] **Step 2: Build + commit**

```bash
task docs:build
jj describe -m "docs(contributing): event-emit pipeline and publisher wiring"
jj new
```

---

#### Task 37: `site/docs/contributing/architecture.md` update

**Files:**

- Modify: `site/docs/contributing/architecture.md`

- [ ] **Step 1: Update the gateway-boundary section**

Find the existing section describing the gateway. Update it to reflect:

- The gateway has no `VerbRegistry`
- All rendering metadata flows on the wire
- `RenderingPublisher` is the single enrichment site

Update the diagram (Mermaid or ASCII) to show the new topology.

- [ ] **Step 2: Build + commit**

```bash
task docs:build
jj describe -m "docs(contributing): update architecture for Phase 1.6 gateway thinness"
jj new
```

---

#### Task 38: `site/docs/extending/verb-registration.md`

**Files:**

- Create or extend: `site/docs/extending/verb-registration.md`

- [ ] **Step 1: Write the doc**

Cover:

- Plugin manifest `verbs:` block schema
- How verb metadata flows from manifest → core registry → `RenderingPublisher` stamps onto event → gateway reads off the wire
- Plugin authors don't need to do anything special at emit time; just declare the verb in `verbs:`
- `crypto.emits` is separate (sensitivity declaration)

Keep it under 250 lines.

- [ ] **Step 2: Build + commit**

```bash
task docs:build
jj describe -m "docs(extending): verb registration flow for plugin authors"
jj new
```

---

#### Task 39: `site/docs/operating/plugin-reloads.md` update

**Files:**

- Modify: `site/docs/operating/plugin-reloads.md`

- [ ] **Step 1: Add the historical-fidelity section**

Append a section explaining:

- Old events keep the rendering they were emitted with
- A plugin reload with a tweaked verb produces label discontinuity in scrollback
- `source_plugin_version` makes drift visible
- SQL recipe (from spec Section 3) for inspecting version distribution in `events_audit`

- [ ] **Step 2: Build + commit**

```bash
task docs:build
jj describe -m "docs(operating): plugin-reload historical fidelity and version drift"
jj new
```

---

## PHASE P12 — FINAL VERIFICATION

### Task 40: Run `task pr-prep`

**Files:** none (verification step)

- [ ] **Step 1: Run the full pre-PR gate**

```bash
task pr-prep
```

Expected: GREEN. Including the seven previously-failing E2E tests in `web/e2e/terminal.spec.ts`.

If any test fails:

1. Identify whether it's a Phase 1.6 invariant test (`INV-GW-*`) or an existing test broken by the changes.
2. If a Phase 1.6 invariant fails: revisit the failing task. **Do not loosen the invariant.**
3. If an existing test broken: investigate whether the existing test was depending on the old gateway-side `VerbRegistry`. If so, update the test fixture to construct `EventFrame` with explicit `Rendering`. If the existing test is testing real behavior that the design broke, **stop and revisit the spec** — don't paper over with fixture changes.
4. Iterate until green.

- [ ] **Step 2: Verify the seven E2E tests specifically**

```bash
task test:e2e -- web/e2e/terminal.spec.ts
```

Expected: PASS for all seven previously-failing tests.

- [ ] **Step 3: Commit any fixture updates**

```bash
jj describe -m "test: update fixtures broken by Rendering migration"
jj new
```

---

#### Task 41: Close `holomush-k18g.5`

**Files:** none (bead operation)

- [ ] **Step 1: Verify all acceptance criteria from spec Section 10**

Walk through Section 10 acceptance criteria 1-19 and check each one off. Note any partial-completion in the bead's notes field.

- [ ] **Step 2: Close the bead**

```bash
bd close holomush-k18g.5 --notes "Phase 1.6 landed on feat/crypto-phase1. RenderingPublisher wraps eventbus.Publisher; rendering metadata flows on every EventFrame and as App-Rendering NATS header; gateway thinness enforced by INV-GW-1 tripwire test; all 16 invariants tested. task pr-prep green."
```

- [ ] **Step 3: Sync beads**

```bash
bd dolt push
```

---

### Self-Review Notes

The plan covers spec Section 10's 19 acceptance criteria as follows:

| AC | Task |
|---|---|
| 1 | Tasks 1, 2 |
| 2 | Task 3 |
| 3 | Task 9 |
| 4 | Task 24 |
| 5 | Tasks 11, 12 |
| 6 | Task 25 |
| 7 | Task 5 |
| 8 | Tasks 6, 8, 11 |
| 9 | Task 18 |
| 10 | Task 19 |
| 11 | Tasks 11-15 |
| 12 | Tasks 27, 28 |
| 13 | Tasks 29, 30, 31 |
| 14 | Tasks 20, 21 |
| 15 | Task 32 |
| 16 | Task 34 |
| 17 | Tasks 35-39 |
| 18 | Task 40 |
| 19 | Task 41 |

All numbered invariants `INV-GW-1` through `INV-GW-16` (excluding the forward-declared `INV-GW-12`) have a named test in a named file. The Plan Task 0 pre-flight survey is the prerequisite for Task 20's plugin manager tightening.

**Known intermediate-state caveats:**

- After Task 24 lands `events_audit.rendering NOT NULL` but before Task 19 wires `RenderingPublisher` (or vice versa), inserts will fail. The plan deliberately orders Phase P7 (boot wiring) before Phase P8 (audit migration) — but in practice both phases land on the same branch and `task pr-prep` is run only at Phase P12. Intermediate `task test:int` runs between phases may fail; this is expected and not a regression.

- After Task 28 drops `WithVerbRegistry` from web/telnet handlers but before Tasks 29-31 update the consumers to read `ev.GetRendering()`, the build is broken. These tasks are sequenced together with a single combined commit at the end of Task 31.

- The `task pr-prep` gate is at Task 40 — earlier intermediate states may not be green.

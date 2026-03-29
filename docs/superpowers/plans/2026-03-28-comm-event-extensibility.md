<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Communication Event Type Extensibility Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace per-type event rendering with a configuration-driven verb registry and category-based rendering, enabling plugin-defined communication verbs without client code changes.

**Architecture:** A VerbRegistry holds rendering metadata (category, format, label) for every event type. The web translation layer and telnet gateway look up types in the registry instead of hardcoding. The web client renders by category (5 renderers) instead of by type (10+ branches). The GameEvent proto is restructured with first-class rendering fields.

**Tech Stack:** Go (verb registry, translation, telnet), Protobuf (GameEvent restructure), SvelteKit/TypeScript (category renderers), Playwright (E2E tests)

**Spec:** `docs/specs/2026-03-28-comm-event-extensibility-design.md`

**Scope:** This plan covers the verb registry, proto changes, translation layer, client rendering migration, and telnet gateway. Channel infrastructure and plugin verb registration are separate plans.

---

## Chunk 1: Verb Registry Foundation

### Task 1: VerbRegistry types and implementation

**Files:**

- Create: `internal/core/registry.go`
- Create: `internal/core/registry_test.go`

**Context:** The VerbRegistry is a thread-safe in-memory map from event type string to rendering metadata. It is the single source of truth for how events render. Both built-in types and plugin types register through the same `Register()` method.

- [ ] **Step 1: Write failing tests for VerbRegistry**

Test cases (table-driven):

- Register a type, look it up → returns VerbRegistration
- Register duplicate type → returns error
- Lookup unregistered type → returns nil/false
- Register with `format: "speech"` and empty label → returns validation error
- Concurrent read/write safety (use goroutines + `sync.WaitGroup`)

```go
// internal/core/registry_test.go
func TestVerbRegistry_Register(t *testing.T) { ... }
func TestVerbRegistry_Register_DuplicateRejected(t *testing.T) { ... }
func TestVerbRegistry_Lookup_NotFound(t *testing.T) { ... }
func TestVerbRegistry_Register_SpeechRequiresLabel(t *testing.T) { ... }
func TestVerbRegistry_ConcurrentAccess(t *testing.T) { ... }
```

- [ ] **Step 2: Run tests — expect FAIL (types don't exist)**

Run: `task test -- -run TestVerbRegistry -v ./internal/core/...`

- [ ] **Step 3: Implement VerbRegistry**

```go
// internal/core/registry.go
package core

import (
    "fmt"
    "sync"
)

// VerbRegistration holds rendering metadata for an event type.
type VerbRegistration struct {
    Type          string
    Category      string        // "communication", "movement", "state", "system", "command"
    Format        string        // "speech", "action", "narrative", "notification", "error", "snapshot", "delta"
    Label         string        // "says", "telepathically sends" — required when Format is "speech"
    DisplayTarget EventChannel  // reuse the existing EventChannel type from proto-generated code
    MetadataKeys  []MetadataKey
}

// MetadataKey declares a well-known metadata field for an event type.
type MetadataKey struct {
    Key         string
    Description string
    ValueType   string // "string", "bool", "object", "array"
}

// VerbRegistry maps event types to their rendering metadata.
type VerbRegistry struct {
    mu    sync.RWMutex
    types map[string]VerbRegistration
}

// NewVerbRegistry creates an empty registry.
func NewVerbRegistry() *VerbRegistry { ... }

// Register adds a type. Returns error if duplicate or invalid.
func (r *VerbRegistry) Register(reg VerbRegistration) error { ... }

// Lookup returns the registration for a type, or false if not found.
func (r *VerbRegistry) Lookup(eventType string) (VerbRegistration, bool) { ... }

// All returns all registrations (for catalog/debug).
func (r *VerbRegistry) All() []VerbRegistration { ... }
```

Validation in Register: reject duplicate types, require label when format is "speech", require non-empty type/category/format.

- [ ] **Step 4: Run tests — expect PASS**

Run: `task test -- -run TestVerbRegistry -v ./internal/core/...`

- [ ] **Step 5: Commit**

`feat(core): add VerbRegistry for event type rendering metadata`

---

### Task 2: Built-in type registrations

**Files:**

- Create: `internal/core/builtins.go`
- Create: `internal/core/builtins_test.go`

**Context:** All existing event types register at startup via a `RegisterBuiltinTypes(registry)` function. This includes types from PR #145 (ooc, pemit). The function is the same path plugins will use — no special cases.

- [ ] **Step 1: Write test that all known EventType constants are registered**

The test calls `RegisterBuiltinTypes()` then verifies every `EventType*` constant in `event.go` has a registration. This prevents drift — adding a new EventType constant without a registration is a test failure.

- [ ] **Step 2: Run test — expect FAIL**

- [ ] **Step 3: Implement RegisterBuiltinTypes**

Register all types per the spec (section "Built-In Registrations"). Include:

- Communication: say (speech, label "says"), pose (action), page (speech, label "pages"), whisper (speech, label "whispers"), whisper\_notice (action — location-broadcast "X whispers to Y"), ooc (action — uses `metadata.style` for say/pose/semipose variants), pemit (narrative — admin emit, no actor)
- Movement: arrive (notification), leave (notification), move (notification)
- State: location\_state (snapshot), exit\_update (delta), object\_create (delta), object\_destroy (delta)
- Command: command\_response (narrative), command\_error (error), object\_use (narrative), object\_examine (narrative), object\_give (narrative)
- System: system (notification)

**OOC rendering note:** OOC has three styles (say, pose, semipose) via a `style` metadata field. Register as `communication/action` — the CommunicationRenderer reads `metadata.style` and `metadata.label` to vary output. When style is "say", `metadata.label` is "OOC" and format is speech-like. When style is "pose" or "semipose", format is action-like. This is handled entirely in the CommunicationRenderer via metadata, not by splitting into separate types.

**Pemit rendering note:** Pemit emits raw text to a target with no actor prefix. Register as `command/narrative` (same as command\_response). The CommandRenderer already handles this format.

**New constant needed:** Add `EventTypeWhisperNotice EventType = "whisper_notice"` to `internal/core/event.go` — the payload type exists but the constant does not.

Note: `DisplayTarget` values use the proto-generated `EventChannel` enum values. Import `webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"` or define local constants that match.

- [ ] **Step 4: Run test — expect PASS**

- [ ] **Step 5: Commit**

`feat(core): register all built-in event types in VerbRegistry`

---

### Task 3: Wire registry into server startup

**Files:**

- Modify: `cmd/holomush/core.go` (add registry creation + RegisterBuiltinTypes call)
- Modify: `internal/grpc/server.go` (accept registry as dependency)
- Modify: `internal/web/handler.go` (accept registry as dependency)

**Context:** The registry must be created early in `runCoreWithDeps()` and passed to components that need it: the web handler (for translation), the telnet gateway (for formatting), and eventually the gRPC server (for plugin validation).

- [ ] **Step 1: Create registry in core.go, call RegisterBuiltinTypes**

Add after EventStore creation (~line 222), before component initialization:

```go
verbRegistry := core.NewVerbRegistry()
core.RegisterBuiltinTypes(verbRegistry)
```

- [ ] **Step 2: Add registry to CoreServer and WebHandler dependencies**

Use the existing options pattern. Add `WithVerbRegistry(*core.VerbRegistry)` option to `CoreServer`. Pass it through to the web handler.

- [ ] **Step 3: Run task test — verify nothing breaks**

Run: `task test`

- [ ] **Step 4: Commit**

`feat(core): wire VerbRegistry into server startup`

---

## Chunk 2: Proto + Translation Layer

> **Atomic deployment note:** Tasks 4-8 (proto, translation, client renderers, eventRouter) form a single breaking change. The proto field renumbering makes old clients incompatible with the new server and vice versa. All these tasks MUST ship in the same PR (squash merged). Individual commits within the branch are fine for review, but the deployment is atomic.

### Task 4: Restructure GameEvent proto

**Files:**

- Modify: `api/proto/holomush/web/v1/web.proto`
- Regenerate: `pkg/proto/holomush/web/v1/*.pb.go` (via `task proto:gen` or equivalent)

**Context:** The GameEvent message is restructured per the spec. This is a breaking wire change — field numbers change meaning. The web client must be updated atomically (handled in Task 7).

Rename `EventChannel` enum field `channel` to `display_target` in the proto. Rename the enum itself if needed to avoid confusion, but the enum values (TERMINAL, STATE, BOTH) stay the same.

- [ ] **Step 1: Update GameEvent message**

```protobuf
message GameEvent {
  string type = 1;
  string category = 2;
  string format = 3;
  EventChannel display_target = 4;
  int64 timestamp = 5;
  string actor = 6;
  string text = 7;
  google.protobuf.Struct metadata = 8;
}
```

- [ ] **Step 2: Regenerate Go proto code**

Run: `task proto:gen` (or `buf generate` — check Taskfile)

- [ ] **Step 3: Fix compilation errors in translate.go and handler.go**

The generated Go types change field names. Update all references from `CharacterName` to `Actor`, from `Channel` to `DisplayTarget`, etc. This is mechanical — fix until `task build` succeeds.

- [ ] **Step 4: Run task build — expect PASS**

- [ ] **Step 5: Commit**

`feat(proto): restructure GameEvent with category/format/display_target`

---

### Task 5: Split command\_response / command\_error at emission

> **Ordering note:** This task MUST run before the translation rewrite (Task 6) so that the new translation layer does not need to handle the legacy IsError synthesis path.

**Files:**

- Modify: `internal/web/translate.go`
- Modify: `internal/web/translate_test.go`

**Context:** Replace the type-switch in `translateEvent()` with registry-based lookup. Currently `translateEvent` is a package-level function with no receiver. It must be converted to a method on `Handler` (which already holds dependencies) so it can access the VerbRegistry. Task 3 adds the registry to Handler's dependencies — this task uses it.

- [ ] **Step 1: Write tests for registry-based translation**

Test cases:

- `say` event → category "communication", format "speech", label "says" in metadata, actor populated
- `pose` event with no\_space → category "communication", format "action", no\_space in metadata
- `command_response` → category "command", format "narrative"
- `location_state` → category "state", format "snapshot", display\_target STATE
- Unknown type → fallback to system/narrative, display\_target TERMINAL
- `page` event → now translated (previously dropped) — category "communication", format "speech"

- [ ] **Step 2: Run tests — expect FAIL**

- [ ] **Step 3: Rewrite translateEvent**

The new function:

1. Look up `ev.GetType()` in registry
2. If found: use registration's category/format/label/display\_target
3. If not found: check payload for category/format fields (plugin fallback), else default to system/narrative/TERMINAL
4. Unmarshal payload bytes
5. Extract `actor` (character\_name from payload or actor\_id resolution)
6. Extract `text` (message field from payload)
7. Build metadata Struct with remaining payload fields (label, channel, no\_space, etc.)
8. Return populated GameEvent

Remove `channelForType()` — replaced by registry lookup.

- [ ] **Step 4: Run tests — expect PASS**

- [ ] **Step 5: Run task test — full suite**

- [ ] **Step 6: Commit**

`refactor(web): replace type-switch translation with VerbRegistry lookup`

---

### Task 6: Rewrite web translation layer

**Files:**

- Modify: `internal/core/event.go` (add EventTypeCommandError constant)
- Modify: `internal/grpc/server.go` (the `emitCommandResponse` method — this is where IsError is set)
- Modify: `internal/web/translate.go` (remove IsError synthesis)
- Modify: `internal/grpc/server_test.go` (references IsError in assertions)
- Modify: `internal/grpc/dispatcher_test.go` (references IsError in assertions)

**Context:** Currently command\_error is synthesized in translate.go when IsError is true. The actual emission site is `emitCommandResponse()` in `internal/grpc/server.go`, not engine.go or command handlers. The spec requires emitting command\_error as a separate type at creation time.

- [ ] **Step 1: Add EventTypeCommandError constant**

```go
EventTypeCommandError EventType = "command_error"
```

- [ ] **Step 2: Find all IsError emission sites**

Run: `grep -rn "IsError" internal/` to find all references. Key sites:

- `internal/grpc/server.go` — `emitCommandResponse()` method sets IsError
- `internal/grpc/server_test.go` — test assertions check IsError
- `internal/grpc/dispatcher_test.go` — test assertions check IsError

- [ ] **Step 3: Split emitCommandResponse into two methods**

In `internal/grpc/server.go`, where `emitCommandResponse` currently takes an `isError` bool parameter, split into:

- Calls with `isError: false` → emit with `EventTypeCommandResponse`
- Calls with `isError: true` → emit with `EventTypeCommandError`

Remove the `IsError` field from `CommandResponsePayload`.

- [ ] **Step 4: Remove IsError synthesis from translate.go**

The translation layer no longer needs to check IsError and swap the type.

- [ ] **Step 5: Update tests in server\_test.go and dispatcher\_test.go**

Change assertions from checking `IsError: true` on `command_response` to checking for `command_error` type directly.

- [ ] **Step 6: Run task test — expect PASS**

- [ ] **Step 7: Commit**

`refactor(core): emit command_error as separate event type`

---

## Chunk 3: Client Rendering Migration

### Task 7: Create category-based Svelte renderers

**Files:**

- Create: `web/src/lib/components/terminal/CommunicationRenderer.svelte`
- Create: `web/src/lib/components/terminal/MovementRenderer.svelte`
- Create: `web/src/lib/components/terminal/CommandRenderer.svelte`
- Create: `web/src/lib/components/terminal/SystemRenderer.svelte`
- Create: `web/src/lib/components/terminal/FallbackRenderer.svelte`
- Modify: `web/src/lib/components/terminal/EventRenderer.svelte` (replace type-switch with category dispatch)

**Context:** The big-bang. Replace the entire `{#if type === 'say'}` chain with 5 category renderers dispatched by `event.category`. Each renderer handles its format variants.

- [ ] **Step 1: Create CommunicationRenderer**

Handles `format: "speech"` and `format: "action"`:

- Speech: `[channel] <actor> <label>, "<text>"` (channel prefix optional)
- Action: `[channel] <actor> <text>` (with no\_space support from metadata)
- Read label from `event.metadata?.label`
- Read channel from `event.metadata?.channel`
- **OOC handling:** OOC events have `metadata.style` ("say", "pose", "semipose") and `metadata.ooc_prefix` ("[OOC]"). When style is "say", render as speech with the OOC prefix. When style is "pose"/"semipose", render as action with OOC prefix. The renderer checks `event.type === 'ooc'` OR `metadata.ooc_prefix` to apply OOC styling — this is a metadata-driven variation within the communication category, not a separate renderer.

- [ ] **Step 2: Create MovementRenderer**

Handles `format: "notification"`:

- `<actor> <text>` with movement-specific styling

- [ ] **Step 3: Create CommandRenderer**

Handles `format: "narrative"` and `format: "error"`:

- Narrative: plain text output
- Error: error-styled text (red/warning color)
- Support ANSI codes in text (use AnsiRenderer when detected)

- [ ] **Step 4: Create SystemRenderer**

Handles `format: "notification"` and `format: "error"`:

- System-styled text with appropriate coloring

- [ ] **Step 5: Create FallbackRenderer**

For unknown categories — display `<actor> <text>` generically, or raw metadata as last resort.

- [ ] **Step 6: Replace EventRenderer.svelte dispatch**

```svelte
{#if event.category === 'communication'}
  <CommunicationRenderer {event} />
{:else if event.category === 'movement'}
  <MovementRenderer {event} />
{:else if event.category === 'command'}
  <CommandRenderer {event} />
{:else if event.category === 'system'}
  <SystemRenderer {event} />
{:else if event.category === 'state'}
  <!-- state events route to sidebar, never rendered in scrollback -->
{:else}
  <FallbackRenderer {event} />
{/if}
```

- [ ] **Step 7: Update theme types if needed**

Check `web/src/lib/theme/types.ts` and `default-dark.json`/`default-light.json` for any type-specific theme tokens that need to become category-specific.

- [ ] **Step 8: Build web client**

Run: `cd web && pnpm build`

- [ ] **Step 9: Commit**

`feat(web): category-based event rendering replaces per-type dispatch`

---

### Task 8: Update eventRouter.ts

**Files:**

- Modify: `web/src/lib/stores/eventRouter.ts`

**Context:** The router currently uses hardcoded channel constants. Update to read `display_target` from the GameEvent instead.

- [ ] **Step 1: Update routeEvent to use display\_target**

Replace the hardcoded `CHANNEL_*` matching with `event.displayTarget` (or whatever the generated TS field name is from the proto).

- [ ] **Step 2: Update routeToSidebar**

The sidebar router currently switches on `event.type` for location\_state, arrive, leave, exit\_update. Change to switch on `event.category === 'state'` for state events, and check `event.category === 'movement'` for arrive/leave presence updates.

- [ ] **Step 3: Build web client**

Run: `cd web && pnpm build`

- [ ] **Step 4: Commit**

`refactor(web): route events by display_target and category`

---

## Chunk 4: Telnet Gateway + Plugin SDK

### Task 9: Category-based telnet formatting

**Files:**

- Modify: `internal/telnet/gateway_handler.go`
- Add/modify tests as needed

**Context:** The telnet gateway's `sendProtoEvent()` has a type-switch for formatting. Replace with category+format dispatch using the VerbRegistry.

- [ ] **Step 1: Accept VerbRegistry in telnet gateway dependencies**

Add registry to the gateway's dependency struct.

- [ ] **Step 2: Replace type-switch with category/format dispatch**

Look up the event type in the registry. Format based on category + format:

- Communication/speech: `<actor> <label>, "<text>"`
- Communication/action: `<actor><space?><text>`
- Movement/notification: `<actor> <text>`
- Command/narrative: `<text>`
- Command/error: `<text>` (error styling)
- System/notification: `<text>`
- State: skip (telnet has no sidebar)
- Unknown: `<text>` as fallback

- [ ] **Step 3: Wire registry into telnet gateway startup in core.go**

- [ ] **Step 4: Run task test**

- [ ] **Step 5: Commit**

`refactor(telnet): category-based event formatting via VerbRegistry`

---

### Task 10: Plugin SDK — remove hardcoded type allowlist

**Files:**

- Modify: `pkg/plugin/event.go`
- Modify: `internal/plugin/subscriber.go`
- Update mock/test files that reference removed constants

**Context:** The plugin SDK currently restricts event types to 5 constants (say, pose, arrive, leave, system). Remove the restriction — plugins can emit any type that is registered in the VerbRegistry. Validation moves from compile-time to runtime.

- [ ] **Step 1: Keep EventType alias, remove constant restrictions**

Keep `type EventType = string` for readability. Remove or deprecate the 5 specific constants. Add a comment that validation is now done by the VerbRegistry at runtime.

- [ ] **Step 2: Update subscriber to validate against registry**

In `internal/plugin/subscriber.go`, when processing EmitEvent responses from plugins, validate that the emitted type exists in the VerbRegistry before forwarding to the event store.

- [ ] **Step 3: Fix compilation across plugin packages**

Update any code referencing the removed constants to use string literals or the core EventType constants.

- [ ] **Step 4: Run task test — full suite**

- [ ] **Step 5: Run task test:int — integration tests**

- [ ] **Step 6: Commit**

`refactor(plugin): remove hardcoded event type allowlist from SDK`

---

## Chunk 5: E2E Verification

### Task 11: E2E tests for category-based rendering

**Files:**

- Modify or create E2E test files in `web/tests/` or `test/integration/`

**Context:** Verify the full stack — Go server emits events, web client renders them using category-based renderers.

- [ ] **Step 1: Write E2E test for say/pose rendering**

Verify the CommunicationRenderer displays `<name> says, "<message>"` for say and `<name> <action>` for pose.

- [ ] **Step 2: Write E2E test for command\_response/command\_error rendering**

Verify CommandRenderer distinguishes narrative from error format.

- [ ] **Step 3: Write E2E test for unknown event type fallback**

If possible, emit an unregistered event type and verify the FallbackRenderer handles it gracefully.

- [ ] **Step 4: Run task test:e2e**

- [ ] **Step 5: Commit**

`test(e2e): verify category-based event rendering`

---

## Post-Implementation Checklist

- [ ] `task test` passes
- [ ] `task lint` passes
- [ ] `task test:int` passes
- [ ] `task test:e2e` passes
- [ ] `task build` succeeds
- [ ] Web client builds: `cd web && pnpm build`
- [ ] Proto regeneration is committed
- [ ] No remaining references to old `character_name` or `channel` (as EventChannel) field names
- [ ] Update `docs/specs/2026-03-28-comm-event-extensibility-design.md` status from "Draft" to "Implemented" for covered sections
- [ ] Create PR using `commit-commands:commit-push-pr`

## Dependency Notes

- **Channel infrastructure** (channels table, channel service, channel commands, dynamic subscription) is a separate plan. Tracked by holomush-8l7d notes.
- **Plugin verb registration** (Lua/Go plugins registering custom types) depends on holomush-cv2k (plugin-first architecture). Deferred.
- **PR #145** added `EventTypeOOC` and `EventTypePemit` — these must be included in built-in registrations (Task 2).

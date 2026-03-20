# Terminal UI — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the terminal mode UI for HoloMUSH's web client — scrollback with ANSI rendering, multiline command input, collapsible info sidebar, dark/light theming, and mobile responsiveness — plus the server-side event channel system that feeds it.

**Architecture:** The server gains an event channel system (TERMINAL/STATE/BOTH) on `GameEvent`, new event types (`room_state`, `exit_update`), stream room-following, and synthetic `room_state` injection on every stream start. The SvelteKit client gets a terminal page with Svelte 5 components: `TerminalView` (scrollback + auto-scroll), `EventRenderer` (semantic HTML per event type), `AnsiRenderer` (ansi\_up for plugin ANSI output), `CommandInput` (multiline textarea + history), collapsible `Sidebar` (room/exits/presence fed by STATE events), `StatusBar`, and a shared theme system (JSON → CSS custom properties).

**Tech Stack:** Go 1.24, protobuf/buf, ConnectRPC, SvelteKit 2, Svelte 5, TypeScript, ansi\_up, Playwright

**Spec:** `docs/specs/2026-03-19-terminal-ui-design.md`

**Epic:** `holomush-qve` (bead: `holomush-qve.5`)

---

## File Structure

### New Files

| File | Responsibility |
| --- | --- |
| `web/src/routes/terminal/+page.svelte` | Terminal page layout: TerminalView + Sidebar + StatusBar |
| `web/src/routes/terminal/+page.ts` | Page options: `ssr=false`, `prerender=false` |
| `web/src/lib/components/terminal/TerminalView.svelte` | Scrollback container, auto-scroll, sentinel, replay separators |
| `web/src/lib/components/terminal/EventRenderer.svelte` | Switch on event type → semantic HTML with CSS classes |
| `web/src/lib/components/terminal/AnsiRenderer.svelte` | ansi\_up wrapper for plugin text with ANSI escapes |
| `web/src/lib/components/terminal/CommandInput.svelte` | Multiline textarea, Enter=send, Shift+Enter=newline, history |
| `web/src/lib/components/terminal/StatusBar.svelte` | Top bar: character, location, connection status, settings |
| `web/src/lib/components/sidebar/Sidebar.svelte` | Collapsible container: icon strip ↔ expanded panel |
| `web/src/lib/components/sidebar/RoomInfo.svelte` | Location name + description |
| `web/src/lib/components/sidebar/ExitList.svelte` | Clickable exit list with lock indicators |
| `web/src/lib/components/sidebar/PresenceList.svelte` | Who's here with idle status |
| `web/src/lib/stores/sidebarStore.ts` | Svelte store: room\_state, exits, presence (from STATE events) |
| `web/src/lib/stores/terminalStore.ts` | Svelte store: scrollback buffer, replay state |
| `web/src/lib/stores/themeStore.ts` | Svelte store: active theme, prefers-color-scheme |
| `web/src/lib/stores/eventRouter.ts` | Route events by channel field to terminal/sidebar stores |
| `web/src/lib/theme/default-dark.json` | Dark theme color definitions |
| `web/src/lib/theme/default-light.json` | Light theme color definitions |
| `web/src/lib/theme/types.ts` | Theme TypeScript types |
| `web/src/lib/util/ansi.ts` | ANSI detection helper (`hasAnsiCodes()`) |
| `web/src/lib/util/urlLinker.ts` | URL auto-linking helper |
| `web/src/tests/stores/sidebarStore.test.ts` | Unit tests for sidebar store |
| `web/src/tests/stores/terminalStore.test.ts` | Unit tests for terminal store |
| `web/src/tests/stores/eventRouter.test.ts` | Unit tests for event routing |
| `web/src/tests/components/EventRenderer.test.ts` | Unit tests for event rendering |
| `web/src/tests/components/CommandInput.test.ts` | Unit tests for input behavior |
| `web/e2e/terminal.spec.ts` | Playwright E2E for terminal flow |

### Modified Files

| File | Change |
| --- | --- |
| `api/proto/holomush/web/v1/web.proto` | Add `EventChannel` enum, `channel` + `metadata` fields to `GameEvent` |
| `internal/core/event.go` | Add `EventTypeRoomState`, `EventTypeExitUpdate` constants + payload structs |
| `internal/web/translate.go` | Handle all event types; populate `channel` and `metadata` on `GameEvent` |
| `internal/web/translate_test.go` | Tests for new translation cases |
| `internal/web/handler.go:107-129` | Wire `AppendCommand` into `SendCommand` |
| `internal/web/handler.go:131-200` | Inject synthetic `room_state` at stream start |
| `internal/grpc/server.go:410+` | Stream room-following: detect move events, resubscribe |
| `web/package.json` | Add `ansi_up` dependency |
| `web/src/routes/+page.svelte` | Reduce to landing/login; terminal moves to `/terminal` |

### Unchanged (Reference)

| File | Why Referenced |
| --- | --- |
| `internal/core/store.go` | `EventStore.Replay` — used for event replay on reconnect |
| `internal/core/broadcaster.go` | `Broadcaster.Subscribe` — live event subscription pattern |
| `internal/session/session.go` | `Store` interface — `AppendCommand`, `GetCommandHistory` methods |
| `internal/world/service.go` | `WorldService` — needed to build `room_state` payloads |
| `web/src/lib/transport.ts` | ConnectRPC transport config — unchanged, reused by terminal page |
| `web/src/lib/connect/` | Generated TypeScript clients — regenerated by `task web:generate` |

---

## Chunk 0: Proto Changes + Codegen

### Task 0.1: Add EventChannel enum and new fields to GameEvent

**Files:**

- Modify: `api/proto/holomush/web/v1/web.proto`

- [ ] **Step 1: Add struct.proto import and EventChannel enum**

Add import and enum before the `GameEvent` message:

```protobuf
import "google/protobuf/struct.proto";

enum EventChannel {
  EVENT_CHANNEL_UNSPECIFIED = 0;
  EVENT_CHANNEL_TERMINAL = 1;
  EVENT_CHANNEL_STATE = 2;
  EVENT_CHANNEL_BOTH = 3;
}
```

- [ ] **Step 2: Add channel and metadata fields to GameEvent**

Extend the existing `GameEvent` message with two new fields:

```protobuf
message GameEvent {
  string type = 1;
  string character_name = 2;
  string text = 3;
  int64 timestamp = 4;
  EventChannel channel = 5;
  google.protobuf.Struct metadata = 6;
}
```

- [ ] **Step 3: Run buf generate + verify**

Run: `task generate`

Expected: Go and TypeScript protobuf code regenerated without errors.

- [ ] **Step 4: Verify Go builds**

Run: `task build`

Expected: Clean build (new fields are zero-valued by default, backward compatible).

- [ ] **Step 5: Verify TypeScript builds**

Run: `cd web && pnpm build`

Expected: Clean build.

- [ ] **Step 6: Commit**

```text
feat(proto): add EventChannel enum and metadata to GameEvent

Adds channel field (TERMINAL/STATE/BOTH) for event routing and
metadata field (Struct) for structured state data. Backward
compatible — existing events default to UNSPECIFIED.
```

---

## Chunk 1: Core Event Types + Translation

### Task 1.1: Add room\_state and exit\_update event types

**Files:**

- Modify: `internal/core/event.go`

- [ ] **Step 1: Write failing test for new event type constants**

Create test in the existing event test file or inline:

```go
func TestEventType_RoomState(t *testing.T) {
	assert.Equal(t, EventType("room_state"), EventTypeRoomState)
	assert.Equal(t, EventType("exit_update"), EventTypeExitUpdate)
}
```

Run: `task test -- -run TestEventType_RoomState ./internal/core/...`

Expected: FAIL — `EventTypeRoomState` undefined.

- [ ] **Step 2: Add constants and payload structs**

In `internal/core/event.go`, add:

```go
EventTypeRoomState  EventType = "room_state"
EventTypeExitUpdate EventType = "exit_update"
```

Add payload structs:

```go
// RoomStatePayload carries a full room snapshot for sidebar updates.
type RoomStatePayload struct {
	Location RoomStateLocation `json:"location"`
	Exits    []RoomStateExit   `json:"exits"`
	Present  []RoomStateChar   `json:"present"`
}

type RoomStateLocation struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type RoomStateExit struct {
	Direction string `json:"direction"`
	Name      string `json:"name"`
	Locked    bool   `json:"locked"`
}

type RoomStateChar struct {
	Name string `json:"name"`
	Idle bool   `json:"idle"`
}

// ExitUpdatePayload carries an updated exit list for the current room.
type ExitUpdatePayload struct {
	Exits []RoomStateExit `json:"exits"`
}
```

- [ ] **Step 3: Run test to verify it passes**

Run: `task test -- -run TestEventType_RoomState ./internal/core/...`

Expected: PASS.

- [ ] **Step 4: Commit**

```text
feat(core): add room_state and exit_update event types

New event types for sidebar state updates in the terminal UI.
Includes RoomStatePayload with location, exits, and present
character data.
```

### Task 1.2: Update translateEvent for channel + metadata

**Files:**

- Modify: `internal/web/translate.go`
- Modify: `internal/web/translate_test.go`

- [ ] **Step 1: Write failing tests for channel assignment**

In `translate_test.go`, build `*corev1.Event` proto messages directly
(the input type for `translateEvent`). The existing test file has helpers
for constructing proto events — follow the same pattern.

```go
func TestTranslateEvent_SayChannel(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{
		"character_name": "Test",
		"message":        "hello",
	})
	ev := &corev1.Event{
		Type:      "say",
		Payload:   payload,
		Timestamp: timestamppb.Now(),
	}
	ge := translateEvent(ev)
	require.NotNil(t, ge)
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, ge.Channel)
}

func TestTranslateEvent_ArriveChannel(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{
		"character_name": "Test",
	})
	ev := &corev1.Event{
		Type:      "arrive",
		Payload:   payload,
		Timestamp: timestamppb.Now(),
	}
	ge := translateEvent(ev)
	require.NotNil(t, ge)
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_BOTH, ge.Channel)
}

func TestTranslateEvent_RoomState(t *testing.T) {
	payload, _ := json.Marshal(core.RoomStatePayload{
		Location: core.RoomStateLocation{ID: "01ABC", Name: "Hall", Description: "A hall."},
		Exits:    []core.RoomStateExit{{Direction: "north", Name: "Gate", Locked: false}},
		Present:  []core.RoomStateChar{{Name: "Gandalf", Idle: false}},
	})
	ev := &corev1.Event{
		Type:    "room_state",
		Payload: payload,
		Timestamp: timestamppb.Now(),
	}
	ge := translateEvent(ev)
	require.NotNil(t, ge)
	assert.Equal(t, "room_state", ge.Type)
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_STATE, ge.Channel)
	require.NotNil(t, ge.Metadata)

	loc := ge.Metadata.Fields["location"].GetStructValue()
	assert.Equal(t, "Hall", loc.Fields["name"].GetStringValue())
}
```

Run: `task test -- -run TestTranslateEvent_SayChannel ./internal/web/...`

Expected: FAIL — `Channel` field not set.

- [ ] **Step 2: Implement channel assignment + metadata population**

Refactor `translateEvent` to assign `channel` for every case and handle
new event types. The function must:

1. Set `Channel` based on the event classification table from the spec.
2. For `room_state` and `exit_update`, build a `structpb.Struct` from the
   JSON payload and set it as `Metadata`.
3. Handle `system` and `move` events (previously dropped as unknown).
4. Default unknown types to `EVENT_CHANNEL_TERMINAL` with best-effort
   text rendering.

Key imports needed:

```go
import "google.golang.org/protobuf/types/known/structpb"
```

For metadata conversion, unmarshal the payload JSON into
`map[string]interface{}` and use `structpb.NewStruct()`.

- [ ] **Step 3: Run tests to verify all pass**

Run: `task test -- -run TestTranslateEvent ./internal/web/...`

Expected: All PASS.

- [ ] **Step 4: Commit**

```text
feat(web): add channel and metadata to event translation

translateEvent now sets EventChannel on all GameEvents and
populates metadata for state events (room_state, exit_update).
Previously unknown types (system, move) are now handled.
```

### Task 1.3: Verify AppendCommand is already wired

**Note:** `AppendCommand` is already called in `internal/grpc/server.go:292`
inside `HandleCommand`. Since the web handler's `SendCommand` calls
`h.client.HandleCommand()` (which is the gRPC server's `HandleCommand`),
command history is already persisted. No additional wiring is needed.

- [ ] **Step 1: Verify with a test**

Write a test confirming that sending a command via the web handler results
in command history being persisted (via the gRPC layer's existing call).

Run: `task test -- -run TestSendCommand ./internal/web/...`

Expected: PASS — existing tests already cover this path.

- [ ] **Step 2: Document in code comment**

Add a comment in `SendCommand` noting that command history persistence
happens in the gRPC layer's `HandleCommand`, not here:

```go
// Command history is persisted by the gRPC HandleCommand handler
// (grpc/server.go) — no additional AppendCommand call needed here.
```

- [ ] **Step 3: Commit**

```text
docs(web): clarify command history persistence path
```

---

## Chunk 2: Stream Room-Following + Synthetic room\_state

### Task 2.1: Inject synthetic room\_state on StreamEvents start

**Files:**

- Modify: `internal/web/handler.go` (StreamEvents method)
- Reference: `internal/world/service.go` (WorldService for room data)

- [ ] **Step 1: Add WorldService to Handler**

The `Handler` struct needs access to `WorldService` to build `room_state`
payloads. Add it as a dependency:

```go
type Handler struct {
	client       CoreClient
	sessionStore session.Store
	tokenRepo    auth.PlayerTokenRepository
	worldService *world.WorldService // NEW
}
```

Update `NewHandler` to accept `*world.WorldService`.

- [ ] **Step 2: Write helper to build room\_state GameEvent**

Create `internal/web/room_state.go`. Key details about WorldService methods:

- `GetLocation(ctx, subjectID string, id ulid.ULID)` — needs a `subjectID`
  for access control. Use a system subject ID (e.g., `"system"`) for
  synthetic events.
- `GetExitsByLocation(ctx, subjectID string, locationID ulid.ULID)` — not
  `ListExits`.
- Characters in location: use WorldService's available query method (check
  actual signature — may be `GetCharactersByLocation` or similar with
  `ListOptions`).
- `LocationID` on session.Info is `ulid.ULID`, not string. Check with
  `!sess.LocationID.IsZero()`, not `sess.LocationID != ""`.
- Timestamp must use Unix seconds (not `UnixMilli`) to match existing
  `translateEvent` behavior.

```go
func (h *Handler) buildRoomState(ctx context.Context, locationID ulid.ULID) (*webv1.GameEvent, error) {
	const systemSubject = "system"

	loc, err := h.worldService.GetLocation(ctx, systemSubject, locationID)
	if err != nil {
		return nil, oops.Wrapf(err, "get location for room_state")
	}

	exits, err := h.worldService.GetExitsByLocation(ctx, systemSubject, locationID)
	if err != nil {
		return nil, oops.Wrapf(err, "list exits for room_state")
	}

	// Build payload, marshal to map, convert to structpb.Struct
	payload := core.RoomStatePayload{
		Location: core.RoomStateLocation{
			ID:          locationID.String(),
			Name:        loc.Name,
			Description: loc.Description,
		},
		Exits:   convertExits(exits),
		Present: []core.RoomStateChar{}, // populated from presence query
	}

	payloadJSON, _ := json.Marshal(payload)
	var m map[string]interface{}
	_ = json.Unmarshal(payloadJSON, &m)

	meta, err := structpb.NewStruct(m)
	if err != nil {
		return nil, oops.Wrap(err)
	}

	return &webv1.GameEvent{
		Type:      "room_state",
		Timestamp: time.Now().Unix(), // Unix seconds, matches translateEvent
		Channel:   webv1.EventChannel_EVENT_CHANNEL_STATE,
		Metadata:  meta,
	}, nil
}
```

Implement `convertExits` to map `world.Exit` → `core.RoomStateExit` based
on the actual `world.Exit` struct fields. Check `internal/world/exit.go`
for the struct definition.

- [ ] **Step 3: Inject room\_state at start of StreamEvents**

In the `StreamEvents` method, after registering the connection but before
entering the event forwarding loop, inject the synthetic room\_state:

```go
// Inject synthetic room_state before replay.
sess, err := h.sessionStore.Get(ctx, sessionID)
if err == nil && !sess.LocationID.IsZero() {
	roomState, err := h.buildRoomState(ctx, sess.LocationID)
	if err != nil {
		slog.Warn("failed to build synthetic room_state", "error", err)
	} else {
		if err := stream.Send(&webv1.StreamEventsResponse{
			Event:    roomState,
			Replayed: true,
		}); err != nil {
			return err
		}
	}
}
```

- [ ] **Step 4: Write test for synthetic room\_state**

Test that StreamEvents sends a room\_state as the first event. Follow
the existing `StreamEvents` test pattern in `handler_test.go`:

```go
func TestStreamEvents_SendsSyntheticRoomState(t *testing.T) {
	locationID := ulid.Make()
	store := mocks.NewMockStore(t)
	store.EXPECT().Get(mock.Anything, "session-1").Return(&session.Info{
		ID:         "session-1",
		LocationID: locationID,
	}, nil)
	store.EXPECT().AddConnection(mock.Anything, mock.Anything).Return(nil)
	store.EXPECT().RemoveConnection(mock.Anything, mock.Anything).Return(nil)

	worldSvc := newMockWorldService(t)
	worldSvc.EXPECT().GetLocation(mock.Anything, "system", locationID).
		Return(&world.Location{Name: "Hall", Description: "A hall."}, nil)
	worldSvc.EXPECT().GetExitsByLocation(mock.Anything, "system", locationID).
		Return([]world.Exit{}, nil)

	// ... setup mock core client Subscribe to return empty stream

	h := NewHandler(mockClient, WithSessionStore(store), WithWorldService(worldSvc))

	// Capture the first event sent on the stream
	// Assert: type == "room_state", channel == STATE, replayed == true
	// Assert: metadata contains location.name == "Hall"
}
```

Run: `task test -- -run TestStreamEvents_SendsSyntheticRoomState ./internal/web/...`

Expected: PASS.

- [ ] **Step 5: Commit**

```text
feat(web): inject synthetic room_state on StreamEvents start

StreamEvents now sends a room_state snapshot as the first event
on every stream start (fresh or reconnect), ensuring the sidebar
is always populated.
```

### Task 2.2: Stream room-following on move events

**Files:**

- Modify: `internal/grpc/server.go` (Subscribe method)

This is the most complex server-side change. When the `Subscribe` handler
detects a `move` event for the subscribed character, it must:

1. Unsubscribe from the old room's stream.
2. Subscribe to the new room's stream.
3. Emit a `room_state` event for the new room.

- [ ] **Step 1: Refactor event forwarding to detect move events**

**Important:** `MovePayload` lives in `internal/world/payloads.go` (not
`core`). Its fields are:

- `EntityType` (world.EntityType) — `"character"` or `"object"`
- `EntityID` (ulid.ULID) — the moving entity
- `ToType` (world.ContainmentType) — destination type
- `ToID` (ulid.ULID) — destination ID

In the `forwardLiveEvents` function (or equivalent), detect moves:

```go
for ev := range merged {
	if err := sendEvent(ev); err != nil {
		return err
	}

	if ev.Type == string(core.EventTypeMove) {
		var payload world.MovePayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			continue
		}
		if payload.EntityType == world.EntityTypeCharacter &&
			payload.EntityID == characterID {
			newLocationID := payload.ToID
			// Cancel old room subscription, subscribe to new room stream
			// ("location:" + newLocationID.String())
			// Build and send room_state for new location
		}
	}
}
```

**Architecture note:** The current `Subscribe` in `grpc/server.go` uses
`mergeChannels` to combine multiple broadcaster subscriptions. Room-following
requires canceling the old location subscription and starting a new one
mid-stream. The implementer should:

1. Read the current `Subscribe` and `forwardLiveEvents` implementation.
2. Use a context-cancelable subscription pattern — wrap each room
   subscription in its own cancelable goroutine.
3. On move: cancel old room goroutine, start new room goroutine, emit
   `room_state`.
4. The `CoreServer` needs `WorldService` as a dependency to build
   `room_state` payloads (add via constructor option).

This is the most complex task in the plan. Budget extra time and write
thorough tests for the subscription transition.

- [ ] **Step 2: Write integration test for room-following**

```go
func TestSubscribe_FollowsCharacterAcrossRooms(t *testing.T) {
	// Setup: character in room A, subscribed to stream
	// Action: emit move event (room A → room B)
	// Assert: stream delivers move event, then room_state for room B,
	//         then events from room B's stream
	// Assert: events from room A's stream are no longer delivered
}
```

- [ ] **Step 3: Implement and verify**

Run: `task test -- -run TestSubscribe_FollowsCharacter ./internal/grpc/...`

Expected: PASS.

- [ ] **Step 4: Run full test suite**

Run: `task test`

Expected: All PASS.

- [ ] **Step 5: Commit**

```text
feat(grpc): stream room-following on character move

Subscribe now detects move events and resubscribes to the new
room's event stream, providing seamless room transitions without
client-side resubscription.
```

### Task 2.3: Update gateway integration

**Files:**

- Modify: `cmd/holomush/core.go` (pass WorldService to web Handler)

- [ ] **Step 1: Pass WorldService to web Handler constructor**

In `core.go` where the web handler is created, add the world service:

```go
webHandler := web.NewHandler(grpcClient,
	web.WithSessionStore(sessionStore),
	web.WithPlayerTokenRepo(tokenRepo),
	web.WithWorldService(worldService),
)
```

Note: `WithWorldService` is a new `HandlerOption` that needs to be added
to `internal/web/handler.go` following the existing options pattern
(`WithSessionStore`, `WithPlayerTokenRepo`).

- [ ] **Step 2: Verify full build and tests**

Run: `task build && task test`

Expected: Clean build and all tests pass.

- [ ] **Step 3: Commit**

```text
feat(gateway): wire WorldService into web handler

Web handler now has access to WorldService for building room_state
payloads used in synthetic event injection and room-following.
```

---

## Chunk 3: Theme System + Svelte Stores

### Task 3.1: Install ansi\_up dependency

**Files:**

- Modify: `web/package.json`

- [ ] **Step 1: Install ansi\_up**

Run: `cd web && pnpm add ansi_up`

- [ ] **Step 2: Verify build**

Run: `cd web && pnpm build`

Expected: Clean build.

- [ ] **Step 3: Commit**

```text
feat(web): add ansi_up dependency for ANSI rendering
```

### Task 3.2: Create theme system

**Files:**

- Create: `web/src/lib/theme/types.ts`
- Create: `web/src/lib/theme/default-dark.json`
- Create: `web/src/lib/theme/default-light.json`

- [ ] **Step 1: Define theme TypeScript types**

```typescript
// web/src/lib/theme/types.ts
export interface ThemeColors {
  'say.speaker': string;
  'say.speech': string;
  'pose.actor': string;
  'pose.action': string;
  system: string;
  arrive: string;
  leave: string;
  background: string;
  'surface': string;
  'border': string;
  'input.prompt': string;
  'input.text': string;
  'input.background': string;
  'status.text': string;
  'status.background': string;
  'sidebar.background': string;
  'scrollback.indicator': string;
}

export interface Theme {
  name: string;
  colors: ThemeColors;
}
```

- [ ] **Step 2: Create dark theme JSON**

```json
{
  "name": "default-dark",
  "colors": {
    "say.speaker": "#4fc3f7",
    "say.speech": "#ffffff",
    "pose.actor": "#81c784",
    "pose.action": "#aaaaaa",
    "system": "#ffb74d",
    "arrive": "#888888",
    "leave": "#888888",
    "background": "#0d0d1a",
    "surface": "#12122a",
    "border": "#1a1a3e",
    "input.prompt": "#4fc3f7",
    "input.text": "#e0e0e0",
    "input.background": "#0a0a16",
    "status.text": "#555555",
    "status.background": "#12122a",
    "sidebar.background": "#12122a",
    "scrollback.indicator": "#ffb74d"
  }
}
```

- [ ] **Step 3: Create light theme JSON**

```json
{
  "name": "default-light",
  "colors": {
    "say.speaker": "#0277bd",
    "say.speech": "#1a1a1a",
    "pose.actor": "#2e7d32",
    "pose.action": "#666666",
    "system": "#e65100",
    "arrive": "#999999",
    "leave": "#999999",
    "background": "#fafafa",
    "surface": "#f0f0f0",
    "border": "#e0e0e0",
    "input.prompt": "#0277bd",
    "input.text": "#1a1a1a",
    "input.background": "#ffffff",
    "status.text": "#999999",
    "status.background": "#f0f0f0",
    "sidebar.background": "#f5f5f5",
    "scrollback.indicator": "#e65100"
  }
}
```

- [ ] **Step 4: Commit**

```text
feat(web): add theme system with dark and light themes

JSON-based theme definitions with typed color keys. CSS custom
properties will be mapped from these at runtime.
```

### Task 3.3: Create Svelte stores

**Files:**

- Create: `web/src/lib/stores/themeStore.ts`
- Create: `web/src/lib/stores/sidebarStore.ts`
- Create: `web/src/lib/stores/terminalStore.ts`
- Create: `web/src/lib/stores/eventRouter.ts`

- [ ] **Step 1: Create themeStore**

```typescript
// web/src/lib/stores/themeStore.ts
import { writable, derived } from 'svelte/store';
import type { Theme, ThemeColors } from '$lib/theme/types';
import darkTheme from '$lib/theme/default-dark.json';
import lightTheme from '$lib/theme/default-light.json';

const themes: Record<string, Theme> = {
  'default-dark': darkTheme as Theme,
  'default-light': lightTheme as Theme,
};

function getInitialTheme(): string {
  if (typeof window === 'undefined') return 'default-dark';
  const saved = localStorage.getItem('holomush-theme');
  if (saved && themes[saved]) return saved;
  return window.matchMedia('(prefers-color-scheme: light)').matches
    ? 'default-light'
    : 'default-dark';
}

export const activeThemeId = writable<string>(getInitialTheme());

export const activeTheme = derived(activeThemeId, ($id) => themes[$id] ?? themes['default-dark']);

export function setTheme(id: string) {
  if (themes[id]) {
    activeThemeId.set(id);
    localStorage.setItem('holomush-theme', id);
  }
}

export function themeToCssVars(colors: ThemeColors): string {
  return Object.entries(colors)
    .map(([key, value]) => `--color-${key.replace(/\./g, '-')}: ${value}`)
    .join('; ');
}
```

- [ ] **Step 2: Create sidebarStore**

```typescript
// web/src/lib/stores/sidebarStore.ts
import { writable } from 'svelte/store';

export interface RoomLocation {
  id: string;
  name: string;
  description: string;
}

export interface RoomExit {
  direction: string;
  name: string;
  locked: boolean;
}

export interface RoomCharacter {
  name: string;
  idle: boolean;
}

export const location = writable<RoomLocation | null>(null);
export const exits = writable<RoomExit[]>([]);
export const presence = writable<RoomCharacter[]>([]);
export const sidebarExpanded = writable<boolean>(false);

export function applyRoomState(metadata: Record<string, unknown>) {
  const loc = metadata.location as RoomLocation | undefined;
  if (loc) location.set(loc);

  const ex = metadata.exits as RoomExit[] | undefined;
  if (ex) exits.set(ex);

  const pr = metadata.present as RoomCharacter[] | undefined;
  if (pr) presence.set(pr);
}

export function addPresence(name: string) {
  presence.update((list) => {
    if (!list.some((c) => c.name === name)) {
      return [...list, { name, idle: false }];
    }
    return list;
  });
}

export function removePresence(name: string) {
  presence.update((list) => list.filter((c) => c.name !== name));
}

export function toggleSidebar() {
  sidebarExpanded.update((v) => !v);
}
```

- [ ] **Step 3: Create terminalStore**

```typescript
// web/src/lib/stores/terminalStore.ts
import { writable, get } from 'svelte/store';
import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';

export interface TerminalLine {
  id: string;
  event: GameEvent;
  replayed: boolean;
}

const DEFAULT_BUFFER_SIZE = 2048;

function getBufferSize(): number {
  if (typeof window === 'undefined') return DEFAULT_BUFFER_SIZE;
  const saved = parseInt(localStorage.getItem('holomush-buffer-size') ?? '', 10);
  return saved >= 512 && saved <= 8192 ? saved : DEFAULT_BUFFER_SIZE;
}

export const lines = writable<TerminalLine[]>([]);
export const replayActive = writable<boolean>(false);
export const newMessageCount = writable<number>(0);
export const isAtBottom = writable<boolean>(true);

let lineCounter = 0;

export function appendLine(event: GameEvent, replayed: boolean) {
  const id = `line-${++lineCounter}`;
  const bufferSize = getBufferSize();

  lines.update((current) => {
    const next = [...current, { id, event, replayed }];
    if (next.length > bufferSize) {
      return next.slice(next.length - bufferSize);
    }
    return next;
  });

  if (!get(isAtBottom)) {
    newMessageCount.update((n) => n + 1);
  }
}

export function clearLines() {
  lines.set([]);
  newMessageCount.set(0);
}

export function markReplayComplete() {
  replayActive.set(false);
}

export function scrolledToBottom() {
  isAtBottom.set(true);
  newMessageCount.set(0);
}

export function scrolledAway() {
  isAtBottom.set(false);
}
```

- [ ] **Step 4: Create eventRouter**

```typescript
// web/src/lib/stores/eventRouter.ts
import type { StreamEventsResponse } from '$lib/connect/holomush/web/v1/web_pb';
import { EventChannel } from '$lib/connect/holomush/web/v1/web_pb';
import { appendLine, replayActive, markReplayComplete } from './terminalStore';
import { applyRoomState, addPresence, removePresence } from './sidebarStore';

export function routeEvent(response: StreamEventsResponse) {
  const event = response.event;
  if (!event) return;

  if (response.replayComplete) {
    markReplayComplete();
    return;
  }

  if (response.replayed) {
    replayActive.set(true);
  }

  const channel = event.channel ?? EventChannel.UNSPECIFIED;

  // Route to terminal (scrollback)
  if (
    channel === EventChannel.TERMINAL ||
    channel === EventChannel.BOTH ||
    channel === EventChannel.UNSPECIFIED
  ) {
    appendLine(event, response.replayed);
  }

  // Route to sidebar stores
  if (channel === EventChannel.STATE || channel === EventChannel.BOTH) {
    routeToSidebar(event);
  }
}

function routeToSidebar(event: { type: string; characterName: string; metadata?: unknown }) {
  const data = metadataToPlain(event.metadata);

  switch (event.type) {
    case 'room_state':
      if (data) applyRoomState(data);
      break;
    case 'exit_update':
      if (data?.exits) {
        exits.set(data.exits as RoomExit[]);
      }
      break;
    case 'arrive':
      if (event.characterName) addPresence(event.characterName);
      break;
    case 'leave':
      if (event.characterName) removePresence(event.characterName);
      break;
  }
}

// Convert protobuf-es Struct to plain JS object.
// protobuf-es v2 Struct has a toJson() method — use it.
// Import: import { toJson, StructSchema } from '@bufbuild/protobuf';
function metadataToPlain(metadata: unknown): Record<string, unknown> | null {
  if (!metadata) return null;
  // protobuf-es v2: if metadata is a Struct message, use toJson()
  if (typeof (metadata as { toJson?: unknown }).toJson === 'function') {
    return (metadata as { toJson: () => Record<string, unknown> }).toJson();
  }
  // Fallback for plain objects (e.g., in tests)
  return metadata as Record<string, unknown>;
}
```

- [ ] **Step 5: Write store unit tests**

Create `web/src/tests/stores/eventRouter.test.ts`:

```typescript
import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { routeEvent } from '$lib/stores/eventRouter';
import { lines, replayActive } from '$lib/stores/terminalStore';
import { presence } from '$lib/stores/sidebarStore';
import { EventChannel } from '$lib/connect/holomush/web/v1/web_pb';

describe('eventRouter', () => {
  beforeEach(() => {
    lines.set([]);
    presence.set([]);
    replayActive.set(false);
  });

  it('routes TERMINAL events to scrollback only', () => {
    routeEvent({
      event: { type: 'say', characterName: 'Test', text: 'hi', channel: EventChannel.TERMINAL },
      replayed: false,
      replayComplete: false,
    });
    expect(get(lines)).toHaveLength(1);
    expect(get(presence)).toHaveLength(0);
  });

  it('routes BOTH events to scrollback and sidebar', () => {
    routeEvent({
      event: { type: 'arrive', characterName: 'Gandalf', text: '', channel: EventChannel.BOTH },
      replayed: false,
      replayComplete: false,
    });
    expect(get(lines)).toHaveLength(1);
    expect(get(presence)).toHaveLength(1);
    expect(get(presence)[0].name).toBe('Gandalf');
  });
});
```

Run: `cd web && pnpm test -- --run`

Expected: PASS (note: need vitest configured — check if it's already in devDeps).

- [ ] **Step 6: Commit**

```text
feat(web): add Svelte stores and event router

Theme store with prefers-color-scheme, sidebar store for room state,
terminal store for scrollback buffer, and event router that dispatches
events by channel field.
```

---

## Chunk 4: Terminal Components

### Task 4.1: EventRenderer component

**Files:**

- Create: `web/src/lib/components/terminal/EventRenderer.svelte`
- Create: `web/src/lib/util/ansi.ts`
- Create: `web/src/lib/util/urlLinker.ts`

- [ ] **Step 1: Create ANSI detection helper**

```typescript
// web/src/lib/util/ansi.ts
const ANSI_REGEX = /\x1b\[/;

export function hasAnsiCodes(text: string): boolean {
  return ANSI_REGEX.test(text);
}
```

- [ ] **Step 2: Create URL auto-linking helper**

```typescript
// web/src/lib/util/urlLinker.ts
const URL_REGEX = /(https?:\/\/[^\s<>"']+)/g;

export function linkUrls(text: string): string {
  return text.replace(URL_REGEX, '<a href="$1" target="_blank" rel="noopener noreferrer">$1</a>');
}
```

- [ ] **Step 3: Create EventRenderer component**

```svelte
<!-- web/src/lib/components/terminal/EventRenderer.svelte -->
<script lang="ts">
  import type { GameEvent } from '$lib/connect/holomush/web/v1/web_pb';
  import { hasAnsiCodes } from '$lib/util/ansi';
  import { linkUrls } from '$lib/util/urlLinker';
  import AnsiRenderer from './AnsiRenderer.svelte';

  interface Props {
    event: GameEvent;
    dimmed?: boolean;
  }

  let { event, dimmed = false }: Props = $props();
</script>

<div class="event event-{event.type}" class:dimmed>
  {#if event.type === 'say'}
    <span class="speaker">{event.characterName}</span> says,
    <span class="speech">"{@html linkUrls(event.text)}"</span>
  {:else if event.type === 'pose'}
    <span class="actor">{event.characterName}</span>
    <span class="action">{@html linkUrls(event.text)}</span>
  {:else if event.type === 'arrive'}
    <span class="arrival">{event.characterName} has arrived.</span>
  {:else if event.type === 'leave'}
    <span class="departure">{event.characterName} has left.</span>
  {:else if event.type === 'system'}
    <span class="system-text">{@html linkUrls(event.text)}</span>
  {:else if event.type === 'move'}
    <span class="move-text">{@html linkUrls(event.text)}</span>
  {:else if hasAnsiCodes(event.text)}
    <AnsiRenderer text={event.text} />
  {:else}
    <span class="generic">{@html linkUrls(event.text)}</span>
  {/if}
</div>

<style>
  .event { line-height: 1.7; }
  .dimmed { opacity: 0.5; }
  .speaker { color: var(--color-say-speaker); }
  .speech { color: var(--color-say-speech); }
  .actor { color: var(--color-pose-actor); }
  .action { color: var(--color-pose-action); }
  .arrival, .departure { color: var(--color-arrive); }
  .system-text { color: var(--color-system); }
  .move-text { color: var(--color-system); }
</style>
```

- [ ] **Step 4: Commit**

```text
feat(web): add EventRenderer with semantic HTML per event type
```

### Task 4.2: AnsiRenderer component

**Files:**

- Create: `web/src/lib/components/terminal/AnsiRenderer.svelte`

- [ ] **Step 1: Create AnsiRenderer**

```svelte
<!-- web/src/lib/components/terminal/AnsiRenderer.svelte -->
<script lang="ts">
  import AnsiUp from 'ansi_up';

  interface Props {
    text: string;
  }

  let { text }: Props = $props();

  const ansiUp = new AnsiUp();
  ansiUp.use_classes = false; // inline styles for color accuracy

  let html = $derived(ansiUp.ansi_to_html(text));
</script>

<span class="ansi-output">{@html html}</span>
```

- [ ] **Step 2: Commit**

```text
feat(web): add AnsiRenderer for plugin ANSI output
```

### Task 4.3: TerminalView component

**Files:**

- Create: `web/src/lib/components/terminal/TerminalView.svelte`

- [ ] **Step 1: Create TerminalView with scrollback and auto-scroll**

```svelte
<!-- web/src/lib/components/terminal/TerminalView.svelte -->
<script lang="ts">
  import { onMount } from 'svelte';
  import { lines, replayActive, newMessageCount, isAtBottom, scrolledToBottom, scrolledAway } from '$lib/stores/terminalStore';
  import EventRenderer from './EventRenderer.svelte';

  let scrollContainer: HTMLDivElement;
  let sentinel: HTMLDivElement;
  let observer: IntersectionObserver;
  let batchedUpdates: unknown[] = [];
  let rafId: number | null = null;

  onMount(() => {
    observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          scrolledToBottom();
        } else {
          scrolledAway();
        }
      },
      { root: scrollContainer, threshold: 0, rootMargin: '50px' }
    );
    observer.observe(sentinel);

    return () => observer.disconnect();
  });

  // Auto-scroll when at bottom and new lines arrive
  $effect(() => {
    if ($isAtBottom && $lines.length > 0) {
      requestAnimationFrame(() => {
        sentinel.scrollIntoView({ behavior: 'instant' });
      });
    }
  });

  function scrollToBottom() {
    sentinel.scrollIntoView({ behavior: 'smooth' });
    scrolledToBottom();
  }
</script>

<div class="terminal-view" bind:this={scrollContainer}>
  <div class="scrollback">
    {#if $replayActive}
      <div class="separator">─── REPLAY ───</div>
    {/if}

    {#each $lines as line, i (line.id)}
      {#if !line.replayed && i > 0 && $lines[i - 1]?.replayed}
        <div class="separator">─── LIVE ───</div>
      {/if}
      <EventRenderer event={line.event} dimmed={line.replayed} />
    {/each}

    <div class="sentinel" bind:this={sentinel}></div>
  </div>

  {#if $newMessageCount > 0}
    <button class="scroll-indicator" onclick={scrollToBottom}>
      ▼ {$newMessageCount} new — click to scroll down
    </button>
  {/if}
</div>

<style>
  .terminal-view {
    flex: 1;
    overflow-y: auto;
    background: var(--color-background);
    font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', monospace;
    font-size: 13px;
    position: relative;
  }
  .scrollback { padding: 8px 12px; }
  .sentinel { height: 1px; }
  .separator {
    color: var(--color-status-text);
    font-size: 10px;
    letter-spacing: 1px;
    margin: 4px 0;
  }
  .scroll-indicator {
    position: sticky;
    bottom: 0;
    width: 100%;
    background: var(--color-border);
    color: var(--color-scrollback-indicator);
    border: none;
    padding: 4px;
    font-size: 11px;
    cursor: pointer;
    text-align: center;
  }
</style>
```

- [ ] **Step 2: Commit**

```text
feat(web): add TerminalView with scrollback and auto-scroll

IntersectionObserver-based scroll detection, replay separators,
new-message indicator, and capped DOM buffer.
```

### Task 4.4: CommandInput component

**Files:**

- Create: `web/src/lib/components/terminal/CommandInput.svelte`

- [ ] **Step 1: Create CommandInput with history and multiline**

```svelte
<!-- web/src/lib/components/terminal/CommandInput.svelte -->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';

  interface Props {
    sessionId: string;
    onSend: (command: string) => void;
  }

  let { sessionId, onSend }: Props = $props();

  let text = $state('');
  let textarea: HTMLTextAreaElement;
  let history: string[] = $state([]);
  let historyIndex = $state(-1);

  // Load command history on mount
  const client = createClient(WebService, transport);

  $effect(() => {
    if (sessionId) {
      client.getCommandHistory({ sessionId }).then((resp) => {
        history = resp.commands ?? [];
      }).catch(() => {
        // Best-effort — history unavailable is not fatal
      });
    }
  });

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      submit();
    } else if (e.key === 'Escape') {
      text = '';
      historyIndex = -1;
    } else if (e.key === 'ArrowUp' && !e.shiftKey) {
      if (historyIndex < history.length - 1) {
        historyIndex++;
        text = history[history.length - 1 - historyIndex];
      }
      e.preventDefault();
    } else if (e.key === 'ArrowDown' && !e.shiftKey) {
      if (historyIndex > 0) {
        historyIndex--;
        text = history[history.length - 1 - historyIndex];
      } else if (historyIndex === 0) {
        historyIndex = -1;
        text = '';
      }
      e.preventDefault();
    }
  }

  function submit() {
    const cmd = text.trim();
    if (!cmd) return;
    history = [...history, cmd];
    historyIndex = -1;
    text = '';
    onSend(cmd);
    // Auto-shrink
    requestAnimationFrame(() => {
      if (textarea) textarea.style.height = 'auto';
    });
  }

  function autoGrow() {
    if (!textarea) return;
    textarea.style.height = 'auto';
    const maxHeight = 6 * 20; // ~6 lines
    textarea.style.height = Math.min(textarea.scrollHeight, maxHeight) + 'px';
  }
</script>

<div class="command-input">
  <span class="prompt">❯</span>
  <textarea
    bind:this={textarea}
    bind:value={text}
    onkeydown={handleKeydown}
    oninput={autoGrow}
    rows="1"
    placeholder="Enter command..."
    spellcheck="false"
    autocomplete="off"
  ></textarea>
</div>

<div class="hints">
  <span>↑↓ history • Shift+Enter newline • Esc clear</span>
</div>

<style>
  .command-input {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    padding: 8px 12px;
    background: var(--color-input-background);
    border-top: 1px solid var(--color-border);
  }
  .prompt {
    color: var(--color-input-prompt);
    line-height: 20px;
    flex-shrink: 0;
  }
  textarea {
    flex: 1;
    background: transparent;
    border: none;
    outline: none;
    color: var(--color-input-text);
    font-family: inherit;
    font-size: inherit;
    resize: none;
    line-height: 20px;
    overflow-y: auto;
  }
  .hints {
    padding: 3px 12px;
    font-size: 9px;
    color: var(--color-status-text);
    background: var(--color-background);
  }
</style>
```

- [ ] **Step 2: Commit**

```text
feat(web): add CommandInput with multiline and history

Enter sends, Shift+Enter for newline, Up/Down cycles history,
Escape clears. Textarea auto-grows to ~6 lines max.
```

### Task 4.5: StatusBar component

**Files:**

- Create: `web/src/lib/components/terminal/StatusBar.svelte`

- [ ] **Step 1: Create StatusBar**

```svelte
<!-- web/src/lib/components/terminal/StatusBar.svelte -->
<script lang="ts">
  import { location } from '$lib/stores/sidebarStore';

  interface Props {
    characterName: string;
    connected: boolean;
    onToggleSidebar: () => void;
    onOpenSettings?: () => void;
    showHamburger?: boolean;
  }

  let { characterName, connected, onToggleSidebar, onOpenSettings, showHamburger = false }: Props = $props();
</script>

<div class="status-bar">
  <div class="left">
    <span class="brand">⬢ HoloMUSH</span>
    <span class="divider">│</span>
    <span class="character">{characterName}</span>
    {#if $location}
      <span class="location">@ {$location.name}</span>
    {/if}
  </div>
  <div class="right">
    <span class="connection" class:connected class:disconnected={!connected}>
      ● {connected ? 'Connected' : 'Disconnected'}
    </span>
    {#if showHamburger}
      <button class="icon-btn" onclick={onToggleSidebar} title="Toggle sidebar">☰</button>
    {/if}
    {#if onOpenSettings}
      <button class="icon-btn" onclick={onOpenSettings} title="Settings">⚙</button>
    {/if}
  </div>
</div>

<style>
  .status-bar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 6px 12px;
    background: var(--color-status-background);
    border-bottom: 1px solid var(--color-border);
    font-size: 11px;
  }
  .left, .right { display: flex; align-items: center; gap: 8px; }
  .brand { color: var(--color-input-prompt); }
  .divider { color: var(--color-border); }
  .character { color: var(--color-pose-actor); }
  .location { color: var(--color-status-text); font-size: 10px; }
  .connected { color: var(--color-status-text); }
  .disconnected { color: var(--color-system); }
  .icon-btn {
    background: none;
    border: none;
    color: var(--color-status-text);
    cursor: pointer;
    font-size: 13px;
  }
</style>
```

- [ ] **Step 2: Commit**

```text
feat(web): add StatusBar component
```

---

## Chunk 5: Sidebar Components

### Task 5.1: RoomInfo, ExitList, PresenceList components

**Files:**

- Create: `web/src/lib/components/sidebar/RoomInfo.svelte`
- Create: `web/src/lib/components/sidebar/ExitList.svelte`
- Create: `web/src/lib/components/sidebar/PresenceList.svelte`

- [ ] **Step 1: Create RoomInfo**

```svelte
<!-- web/src/lib/components/sidebar/RoomInfo.svelte -->
<script lang="ts">
  import { location } from '$lib/stores/sidebarStore';
</script>

{#if $location}
  <div class="room-info">
    <div class="room-name">📍 {$location.name}</div>
    <div class="room-desc">{$location.description}</div>
  </div>
{/if}

<style>
  .room-info { margin-bottom: 12px; }
  .room-name { color: var(--color-system); font-weight: bold; margin-bottom: 4px; }
  .room-desc { color: var(--color-status-text); font-size: 10px; line-height: 1.4; }
</style>
```

- [ ] **Step 2: Create ExitList**

```svelte
<!-- web/src/lib/components/sidebar/ExitList.svelte -->
<script lang="ts">
  import { exits } from '$lib/stores/sidebarStore';

  interface Props {
    onExitClick: (direction: string) => void;
  }

  let { onExitClick }: Props = $props();
</script>

{#if $exits.length > 0}
  <div class="exit-list">
    <div class="section-title">Exits</div>
    {#each $exits as exit}
      <button class="exit" onclick={() => onExitClick(exit.direction)}>
        → <span class="exit-name">{exit.name}</span>
        {#if exit.locked}🔒{/if}
      </button>
    {/each}
  </div>
{/if}

<style>
  .exit-list { margin-bottom: 12px; }
  .section-title { color: var(--color-input-prompt); font-weight: bold; margin-bottom: 4px; font-size: 11px; }
  .exit {
    display: block;
    background: none;
    border: none;
    color: var(--color-pose-actor);
    cursor: pointer;
    font-family: inherit;
    font-size: inherit;
    padding: 1px 0;
    text-align: left;
  }
  .exit:hover { text-decoration: underline; }
</style>
```

- [ ] **Step 3: Create PresenceList**

```svelte
<!-- web/src/lib/components/sidebar/PresenceList.svelte -->
<script lang="ts">
  import { presence } from '$lib/stores/sidebarStore';
</script>

{#if $presence.length > 0}
  <div class="presence-list">
    <div class="section-title">Present</div>
    {#each $presence as char}
      <div class="character" class:idle={char.idle}>
        • {char.name}
        {#if char.idle}<span class="idle-tag">idle</span>{/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .section-title { color: var(--color-input-prompt); font-weight: bold; margin-bottom: 4px; font-size: 11px; }
  .character { padding: 1px 0; }
  .idle { opacity: 0.6; }
  .idle-tag { font-size: 9px; color: var(--color-status-text); }
</style>
```

- [ ] **Step 4: Commit**

```text
feat(web): add sidebar content components

RoomInfo, ExitList (clickable exits), and PresenceList
components driven by sidebarStore.
```

### Task 5.2: Sidebar container (collapsible)

**Files:**

- Create: `web/src/lib/components/sidebar/Sidebar.svelte`

- [ ] **Step 1: Create collapsible Sidebar**

```svelte
<!-- web/src/lib/components/sidebar/Sidebar.svelte -->
<script lang="ts">
  import { sidebarExpanded, toggleSidebar, location, exits, presence } from '$lib/stores/sidebarStore';
  import RoomInfo from './RoomInfo.svelte';
  import ExitList from './ExitList.svelte';
  import PresenceList from './PresenceList.svelte';

  interface Props {
    onExitClick: (direction: string) => void;
    overlay?: boolean;
  }

  let { onExitClick, overlay = false }: Props = $props();
</script>

{#if overlay && $sidebarExpanded}
  <button class="overlay-backdrop" onclick={toggleSidebar}></button>
{/if}

<aside class="sidebar" class:expanded={$sidebarExpanded} class:overlay>
  {#if $sidebarExpanded}
    <div class="sidebar-content">
      <RoomInfo />
      <ExitList {onExitClick} />
      <PresenceList />
    </div>
    <button class="toggle" onclick={toggleSidebar} title="Collapse sidebar">▶</button>
  {:else}
    <div class="icon-strip">
      <div class="icon" title={$location?.name ?? 'Unknown'}>📍</div>
      <div class="badge">{$location?.name?.slice(0, 4) ?? '—'}</div>
      <div class="icon" title="{$exits.length} exits">🚪</div>
      <div class="badge">{$exits.length}</div>
      <div class="icon" title="{$presence.length} present">👥</div>
      <div class="badge">{$presence.length}</div>
      <div class="spacer"></div>
      <button class="toggle" onclick={toggleSidebar} title="Expand sidebar">◀</button>
    </div>
  {/if}
</aside>

<style>
  .sidebar {
    background: var(--color-sidebar-background);
    border-left: 1px solid var(--color-border);
    transition: width 150ms ease;
    display: flex;
    flex-direction: column;
  }
  .sidebar:not(.expanded) { width: 36px; }
  .sidebar.expanded { width: 220px; }
  .sidebar.overlay {
    position: absolute;
    right: 0;
    top: 0;
    bottom: 0;
    z-index: 10;
  }
  .overlay-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.5);
    border: none;
    z-index: 9;
  }
  .sidebar-content {
    flex: 1;
    padding: 12px;
    overflow-y: auto;
    font-size: 11px;
    line-height: 1.6;
  }
  .icon-strip {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 8px 0;
    gap: 2px;
  }
  .icon { font-size: 14px; }
  .badge { font-size: 8px; color: var(--color-status-text); margin-bottom: 4px; }
  .spacer { flex: 1; }
  .toggle {
    background: none;
    border: 1px solid var(--color-border);
    color: var(--color-status-text);
    cursor: pointer;
    border-radius: 4px;
    width: 28px;
    height: 28px;
    font-size: 11px;
    margin: 4px;
  }
</style>
```

- [ ] **Step 2: Commit**

```text
feat(web): add collapsible Sidebar container

Icon strip when collapsed (36px), full panel when expanded (220px).
CSS transition, overlay mode for mobile.
```

---

## Chunk 6: Terminal Page + Event Routing + Mobile + E2E

### Task 6.1: Terminal page route

**Files:**

- Create: `web/src/routes/terminal/+page.ts`
- Create: `web/src/routes/terminal/+page.svelte`
- Modify: `web/src/routes/+page.svelte` (add link to terminal)

- [ ] **Step 1: Create page options**

```typescript
// web/src/routes/terminal/+page.ts
export const ssr = false;
export const prerender = false;
```

- [ ] **Step 2: Create terminal page**

```svelte
<!-- web/src/routes/terminal/+page.svelte -->
<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { routeEvent } from '$lib/stores/eventRouter';
  import { clearLines } from '$lib/stores/terminalStore';
  import { toggleSidebar } from '$lib/stores/sidebarStore';
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import TerminalView from '$lib/components/terminal/TerminalView.svelte';
  import CommandInput from '$lib/components/terminal/CommandInput.svelte';
  import StatusBar from '$lib/components/terminal/StatusBar.svelte';
  import Sidebar from '$lib/components/sidebar/Sidebar.svelte';

  const client = createClient(WebService, transport);

  let sessionId = $state('');
  let characterName = $state('');
  let connected = $state(false);
  let error = $state('');
  let abortController: AbortController | null = null;
  let isMobile = $state(false);

  function onKeydown(e: KeyboardEvent) {
    if (e.ctrlKey && e.key === 'b') {
      e.preventDefault();
      toggleSidebar();
    }
    if (e.ctrlKey && e.key === 'l') {
      e.preventDefault();
      clearLines();
    }
  }

  onMount(() => {
    checkMobile();
    window.addEventListener('resize', checkMobile);
    window.addEventListener('keydown', onKeydown);
  });

  onDestroy(() => {
    window.removeEventListener('resize', checkMobile);
    window.removeEventListener('keydown', onKeydown);
    abortController?.abort();
  });

  function checkMobile() {
    isMobile = window.innerWidth < 768;
  }

  async function login() {
    try {
      const resp = await client.login({ username: 'guest', password: '' });
      sessionId = resp.sessionId;
      characterName = resp.characterName;
      connected = true;
      startStreaming();
    } catch (e) {
      error = e instanceof Error ? e.message : 'Login failed';
    }
  }

  async function startStreaming() {
    abortController?.abort();
    abortController = new AbortController();
    clearLines();

    try {
      for await (const response of client.streamEvents(
        { sessionId, replayFromCursor: true },
        { signal: abortController.signal }
      )) {
        routeEvent(response);
      }
    } catch (e) {
      if (e instanceof Error && e.name !== 'AbortError') {
        connected = false;
      }
    }
  }

  async function sendCommand(command: string) {
    try {
      await client.sendCommand({ sessionId, text: command });
    } catch (e) {
      error = e instanceof Error ? e.message : 'Command failed';
    }
  }

  function handleExitClick(direction: string) {
    sendCommand(direction);
  }

  async function disconnect() {
    abortController?.abort();
    try {
      await client.disconnect({ sessionId });
    } catch { /* best effort */ }
    connected = false;
    sessionId = '';
  }

  // Keyboard shortcuts (Ctrl+B, Ctrl+L) handled via onKeydown listener above
</script>

{#if !connected}
  <div class="login-screen">
    <h1>HoloMUSH</h1>
    {#if error}<p class="error">{error}</p>{/if}
    <button onclick={login}>Connect as Guest</button>
  </div>
{:else}
  <div class="terminal-layout" style={themeToCssVars($activeTheme.colors)}>
    <StatusBar
      {characterName}
      {connected}
      onToggleSidebar={toggleSidebar}
      showHamburger={isMobile}
    />
    <div class="main-area">
      <div class="terminal-column">
        <TerminalView />
        <CommandInput {sessionId} onSend={sendCommand} />
      </div>
      {#if !isMobile}
        <Sidebar onExitClick={handleExitClick} />
      {:else}
        <Sidebar onExitClick={handleExitClick} overlay />
      {/if}
    </div>
  </div>
{/if}

<style>
  .login-screen {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100vh;
    gap: 16px;
    font-family: 'JetBrains Mono', monospace;
    background: #0d0d1a;
    color: #e0e0e0;
  }
  .error { color: #e57373; }
  .terminal-layout {
    display: flex;
    flex-direction: column;
    height: 100vh;
    font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', monospace;
    font-size: 13px;
    background: var(--color-background);
    color: var(--color-input-text);
  }
  .main-area {
    flex: 1;
    display: flex;
    overflow: hidden;
    position: relative;
  }
  .terminal-column {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
</style>
```

- [ ] **Step 3: Update landing page with terminal link**

In `web/src/routes/+page.svelte`, replace the current terminal proof-of-concept
with a simple link to `/terminal`.

- [ ] **Step 4: Verify build**

Run: `cd web && pnpm build`

Expected: Clean build.

- [ ] **Step 5: Commit**

```text
feat(web): add terminal page with event routing and mobile layout

Full terminal layout: StatusBar + TerminalView + CommandInput +
collapsible Sidebar. Event routing by channel field. Mobile
responsive with overlay sidebar below 768px.
```

### Task 6.2: E2E tests

**Files:**

- Create: `web/e2e/terminal.spec.ts`

- [ ] **Step 1: Create Playwright E2E tests**

```typescript
// web/e2e/terminal.spec.ts
import { test, expect } from '@playwright/test';

test.describe('Terminal UI', () => {
  test('connects and displays events', async ({ page }) => {
    await page.goto('/terminal');

    // Login
    await page.click('text=Connect as Guest');
    await expect(page.locator('.terminal-layout')).toBeVisible();

    // Verify status bar shows character name
    await expect(page.locator('.character')).toContainText(/Guest/);
  });

  test('sends commands and receives output', async ({ page }) => {
    await page.goto('/terminal');
    await page.click('text=Connect as Guest');

    // Type and send a command
    const input = page.locator('textarea');
    await input.fill('look');
    await input.press('Enter');

    // Verify command was sent (output appears in scrollback)
    await expect(page.locator('.scrollback .event').first()).toBeVisible();
  });

  test('sidebar toggles with Ctrl+B', async ({ page }) => {
    await page.goto('/terminal');
    await page.click('text=Connect as Guest');

    // Sidebar starts collapsed
    await expect(page.locator('.sidebar:not(.expanded)')).toBeVisible();

    // Toggle with keyboard
    await page.keyboard.press('Control+b');
    await expect(page.locator('.sidebar.expanded')).toBeVisible();

    // Toggle back
    await page.keyboard.press('Control+b');
    await expect(page.locator('.sidebar:not(.expanded)')).toBeVisible();
  });

  test('responsive layout hides sidebar on mobile', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto('/terminal');
    await page.click('text=Connect as Guest');

    // Sidebar should not be visible on mobile
    await expect(page.locator('.sidebar:not(.overlay)')).not.toBeVisible();

    // Hamburger menu should be visible
    await expect(page.locator('button[title="Toggle sidebar"]')).toBeVisible();
  });

  test('command history with up/down arrows', async ({ page }) => {
    await page.goto('/terminal');
    await page.click('text=Connect as Guest');

    const input = page.locator('textarea');

    // Send two commands
    await input.fill('look');
    await input.press('Enter');
    await input.fill('say hello');
    await input.press('Enter');

    // Up arrow should recall last command
    await input.press('ArrowUp');
    await expect(input).toHaveValue('say hello');

    // Up again for previous
    await input.press('ArrowUp');
    await expect(input).toHaveValue('look');
  });
});
```

- [ ] **Step 2: Run E2E tests**

Run: `cd web && pnpm exec playwright test e2e/terminal.spec.ts`

Expected: Tests pass against running dev server (requires `task dev` in
background).

- [ ] **Step 3: Commit**

```text
test(web): add Playwright E2E tests for terminal UI

Tests for connection, command sending, sidebar toggle,
mobile responsive layout, and command history.
```

---

## Post-Implementation Checklist

- [ ] Run `task test` — all Go tests pass
- [ ] Run `task lint` — no lint errors
- [ ] Run `cd web && pnpm build` — TypeScript builds clean
- [ ] Run `cd web && pnpm test -- --run` — Svelte unit tests pass
- [ ] Run `cd web && pnpm exec playwright test` — E2E tests pass
- [ ] Run `task license:check` — SPDX headers on new files
- [ ] Invoke `pr-review-toolkit:review-pr` for comprehensive review
- [ ] Address all review findings
- [ ] Close bead `holomush-qve.5`

# Terminal UI Design Spec (Sub-Spec 2b)

**Status:** Draft
**Epic:** 8 — Web Client
**Bead:** holomush-qve.5
**Depends on:** Sub-spec 2a (Session Persistence, PR #123)
**Date:** 2026-03-19

## Overview

This spec defines the terminal mode UI for HoloMUSH's web client: the primary
gameplay interface where players issue commands and read game output. It covers
the scrollback display, ANSI rendering, command input, collapsible info sidebar,
theming, and mobile responsiveness.

The web infrastructure already exists (ConnectRPC handler, SvelteKit scaffold,
session persistence with replay, event streaming). This spec builds the
interactive terminal layer on top of it.

### New server-side work required

This spec introduces capabilities that do not yet exist in the server:

- **New event types.** `room_state`, `exit_update`, and `system` events MUST be
  added to the core engine (`internal/core/event.go`).
- **Stream room-following.** The `Subscribe` handler MUST track the character's
  current room and switch stream subscriptions when the character moves.
- **Synthetic `room_state` injection.** The `StreamEvents` handler MUST inject
  a current `room_state` snapshot as the first event on every stream start.
- **Event translation updates.** `translateEvent` in `internal/web/translate.go`
  MUST be extended to handle all new event types and populate the new `channel`
  and `metadata` fields on `GameEvent`.
- **Channel assignment.** `translateEvent` MUST assign the `channel` field based
  on event type using the classification table in this spec. The client reads
  the field but does not assign it.
- **Command history wiring.** `SendCommand` in `internal/web/handler.go` MUST
  call `sessionStore.AppendCommand()` after processing each command.
  `AppendCommand` is an internal method on the session store (not a
  client-facing RPC) — it was implemented in sub-spec 2a but is not yet called
  from the web handler.

## Architecture

A single `StreamEvents` connection carries all data the terminal needs. The
server MUST follow the character across rooms, automatically routing events
from the character's current location. The client MUST NOT manage stream
subscriptions.

```text
Server (Go)                          Client (SvelteKit)
┌─────────────────┐                  ┌──────────────────────────┐
│ Core Engine      │                  │ Terminal Page             │
│  └─ Events       │                  │  ├─ TerminalView          │
│     (say, pose,  │  StreamEvents    │  │  ├─ ScrollbackBuffer   │
│      room_state, │ ────────────────►│  │  └─ EventRenderer      │
│      arrive...)  │  (ConnectRPC)    │  ├─ CommandInput          │
│                  │                  │  │  └─ HistoryManager     │
│ Stream follows   │                  │  ├─ Sidebar (collapsible) │
│ character across │                  │  │  ├─ RoomInfo           │
│ rooms            │                  │  │  ├─ ExitList           │
│                  │                  │  │  └─ PresenceList       │
│ Synthetic        │                  │  └─ StatusBar             │
│ room_state on    │                  │                            │
│ every stream     │                  │ Event Router:              │
│ start            │                  │  TERMINAL → ScrollbackBuffer│
└─────────────────┘                  │  STATE    → Sidebar stores  │
                                     │  BOTH     → Both            │
                                     └──────────────────────────┘
```

### Key architectural properties

- **Single event stream.** The server handles room-subscription management
  internally. When the character moves, the stream transitions seamlessly:
  old-room events stop, a `room_state` snapshot for the new room arrives,
  then new-room events flow.
- **Synthetic `room_state` on every stream start.** Whether the client
  connects fresh, reconnects after a disconnect, or reloads the page, the
  server MUST inject a current `room_state` snapshot as the first event
  before replay begins. The sidebar MUST never be empty.
- **Channel-based routing.** Each event carries an explicit `channel` field
  that tells the client where to deliver it (see Event Channel System).

## Event Channel System

### Proto changes

The following changes MUST be made to `api/proto/holomush/web/v1/web.proto`.

Add `import "google/protobuf/struct.proto";` to the imports (not currently
present in `web.proto`).

Add the `EventChannel` enum and extend `GameEvent` with `channel` and
`metadata` fields:

```protobuf
enum EventChannel {
  EVENT_CHANNEL_UNSPECIFIED = 0;
  EVENT_CHANNEL_TERMINAL = 1;
  EVENT_CHANNEL_STATE = 2;
  EVENT_CHANNEL_BOTH = 3;
}

message GameEvent {
  string type = 1;
  string character_name = 2;
  string text = 3;
  int64 timestamp = 4;              // existing field, unchanged
  EventChannel channel = 5;         // NEW
  google.protobuf.Struct metadata = 6; // NEW — structured data for state events
}
```

Note: `timestamp` remains `int64` to match the existing definition. The
`replayed` and `replay_complete` flags live on `StreamEventsResponse` (the
wrapper message), not on `GameEvent`.

### Event classification

The `translateEvent` function in `internal/web/translate.go` MUST assign the
`channel` field based on this table:

| Event          | Channel  | Terminal output           | Sidebar update            |
| -------------- | -------- | ------------------------- | ------------------------- |
| `say`          | TERMINAL | "Name says, ..."          | —                         |
| `pose`         | TERMINAL | "Name does something."    | —                         |
| `system`       | TERMINAL | System message text       | —                         |
| `room_state`   | STATE    | —                         | Full room snapshot        |
| `exit_update`  | STATE    | —                         | Refresh exit list         |
| `arrive`       | BOTH     | "Name has arrived."       | Add to presence list      |
| `leave`        | BOTH     | "Name has left."          | Remove from presence list |
| `move`         | BOTH     | "You head north..."       | Triggers room transition  |

The client MUST route events based on the `channel` field:

- `TERMINAL` → scrollback buffer only
- `STATE` → sidebar stores only
- `BOTH` → scrollback buffer and sidebar stores
- `UNSPECIFIED` → treat as `TERMINAL` (forward-compatible default)

### `room_state` metadata schema

State events carry structured data in the `metadata` field (a proto `Struct`,
represented as JSON). The `room_state` metadata MUST conform to:

```json
{
  "location": {
    "id": "01ABC",
    "name": "The Grand Hall",
    "description": "A vast stone chamber with vaulted ceilings."
  },
  "exits": [
    { "direction": "north", "name": "North Gate", "locked": false },
    { "direction": "down", "name": "Dungeon", "locked": true }
  ],
  "present": [
    { "name": "Gandalf", "idle": false },
    { "name": "Legolas", "idle": true }
  ]
}
```

### Lua plugin events

Plugins MAY set `channel` when emitting events. If unspecified, the server
MUST default to `TERMINAL`. Plugins MAY emit `STATE` events to push custom
sidebar data.

### Room changes

When a character moves (user command or server action), the server MUST:

1. Emit `leave` on the old room's stream.
2. Switch the stream subscription internally.
3. Emit a `room_state` snapshot for the new room.
4. Emit `arrive` on the new room's stream.
5. Continue with live events from the new room.

The client sees a seamless transition. No resubscription logic is needed.

### Session persistence interaction

On reconnect with `replay_from_cursor=true`:

1. Server injects a **synthetic `room_state`** as the first event. The
   wrapping `StreamEventsResponse` marks this with `replayed=true`.
2. Replayed events follow (all wrapped with `replayed=true`).
3. A `StreamEventsResponse` with `replay_complete=true` marks the end of
   replay.
4. Live events follow.

The synthetic `room_state` ensures the sidebar is populated even when the
original `room_state` event sits behind the replay cursor.

## Rendering Pipeline

Two rendering paths handle different event sources.

### Path 1 — Semantic rendering (built-in events)

Built-in event types (`say`, `pose`, `arrive`, etc.) MUST be rendered by a
Svelte component that switches on event type and produces semantic HTML:

```html
<div class="event event-say">
  <span class="speaker">Gandalf</span> says,
  <span class="speech">"Welcome."</span>
</div>
```

Colors come from CSS custom properties defined by the active theme
(`--color-say-speaker`, `--color-pose-action`, etc.). Changing themes changes
the CSS variables — rendering logic stays the same.

### Path 2 — ANSI passthrough (plugin output)

Plugin-generated text that contains ANSI escape codes MUST be rendered by the
`ansi_up` library, which converts escape sequences to styled `<span>` elements.
The client SHOULD detect ANSI codes with a regex check (`/\x1b\[/`) before
invoking the parser. `ansi_up` handles 16-color, 256-color, and truecolor.

### Shared theme schema

Both the web client and the telnet adapter (separate work) SHOULD read from
the same theme schema. Each theme is a JSON object mapping semantic color
names to values:

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
    "input.prompt": "#4fc3f7",
    "input.text": "#e0e0e0"
  }
}
```

The web client maps these to CSS custom properties. The telnet adapter (future,
separate work) maps them to ANSI escape codes. The theme is the single source
of truth; each renderer is native to its medium.

### Replay visual treatment

The client MUST visually distinguish replayed events from live events:

- Replayed events MUST render at 50% opacity.
- A `─── REPLAY ───` separator MUST appear before the replay block.
- A `─── LIVE ───` separator MUST appear after the `replay_complete` marker.
- Once live, all events render at full opacity.

The client determines replay state from the `replayed` and `replay_complete`
fields on `StreamEventsResponse`.

### URL auto-linking

URLs in event text MUST be detected and wrapped in `<a>` tags (clickable, open
in new tab). Detection SHOULD use a standard URL regex.

**Deferred:** Client-side inline image thumbnails for image URLs. No
server-side rich previews — rejected on security grounds (tracking pixels,
content leakage).

### Emoji

Unicode emoji render natively in DOM elements. No special handling is required.
Users access emoji via the OS emoji picker. Emoji are double-width in monospace
fonts, which MAY affect columnar alignment but is acceptable for chat-style
scrollback.

## Terminal Component

### Scrollback buffer

- **Capped DOM buffer.** The client MUST retain the last 2,048 lines in the
  DOM by default. Older lines MUST be pruned from the top. Server-side event
  replay handles deep history.
- **Configurable cap.** The cap SHOULD be a user setting stored in localStorage
  (range: 512–8,192).
- **DOM-based scrolling.** The client MUST use native browser scroll on styled
  `<div>` elements. Virtual scrolling MUST NOT be used — it adds complexity for
  negligible benefit at this scale.
- **Copy/paste.** Works natively — text selection on DOM elements requires no
  special handling.

### Auto-scroll behavior

| State                             | Behavior                                             |
| --------------------------------- | ---------------------------------------------------- |
| At bottom                         | New events MUST auto-scroll into view                |
| Scrolled up                       | Auto-scroll MUST pause (user is reading history)     |
| New events while paused           | MUST show indicator: "▼ N new — click to scroll down"|
| Click indicator or scroll to bottom | MUST resume auto-scroll, dismiss indicator          |

"At bottom" SHOULD mean within 50px of scroll end (forgiving threshold).

**Scroll detection:** The client SHOULD use `IntersectionObserver` on a
sentinel element at the bottom of the scrollback. More efficient than scroll
event listeners.

### Performance guard

If events arrive faster than 60/sec, the client SHOULD batch DOM updates with
`requestAnimationFrame` — append accumulated events once per frame.

## Command Input

Multiline `<textarea>` anchored at the bottom of the terminal.

| Feature             | Behavior                                                      |
| ------------------- | ------------------------------------------------------------- |
| **Submit**          | Enter MUST send command                                       |
| **Newline**         | Shift+Enter MUST insert newline (textarea grows)              |
| **Auto-grow**       | 1 line minimum, ~6 lines maximum before internal scroll       |
| **Auto-shrink**     | MUST collapse to single line after sending                    |
| **Command history** | Up/Down arrows MUST cycle through previous commands           |
| **History source**  | Local buffer (current session) + `GetCommandHistory` on connect |
| **History size**    | SHOULD retain 100 commands locally; server stores per session |
| **Clear**           | Escape MUST clear all input                                   |
| **Line editing**    | Standard browser text input (Home/End, Ctrl+A, word-jump)     |

**History persistence:** On stream start, the client MUST call
`GetCommandHistory` (existing RPC) to seed the local buffer. New commands are
appended locally and persisted server-side via `sessionStore.AppendCommand()`
(an internal session store method, not a client-facing RPC — implemented in
sub-spec 2a).

Multiline commands MUST be stored as single history entries.

## Sidebar Component

### Collapsed state (default)

A 36px icon strip on the right edge:

- Location icon with truncated name
- Exit count badge
- Presence count badge
- Expand button at bottom

### Expanded state

~220px panel showing:

- **Location:** Name and description.
- **Exits:** Direction, name, lock indicator. Clickable — clicking an exit
  MUST send the direction as a command.
- **Presence:** Character names with idle status indicator.

### Toggle

Click the expand/collapse button or press Ctrl+B. SHOULD use CSS transition,
150ms slide.

### Sidebar data flow

| Event                  | Action                              |
| ---------------------- | ----------------------------------- |
| `room_state` (STATE)   | Full sidebar refresh                |
| `arrive` (BOTH)        | Add character to presence list      |
| `leave` (BOTH)         | Remove character from presence list |
| `exit_update` (STATE)  | Refresh exit list                   |

## Status Bar

Minimal top bar MUST show:

- Character name and current location
- Connection status indicator (connected/reconnecting/disconnected)
- Settings icon (gear)

## Theming

### v1 themes

Two built-in themes MUST be provided:

- **HoloMUSH Dark** (default) — dark background, high-contrast colors
- **HoloMUSH Light** — light background, accessible colors

### Theme selection

The client MUST respect `prefers-color-scheme` OS setting by default. The user
MAY override in settings (stored in localStorage).

### Future themes

Adding themes is trivial — each theme is a JSON file. A curated set (Dracula,
Solarized, etc.) and a theme picker MAY be added later without architectural
changes.

## Mobile Responsive

| Breakpoint           | Behavior                                                                                                        |
| -------------------- | --------------------------------------------------------------------------------------------------------------- |
| ≥ 768px (desktop)    | Full layout: terminal + collapsible sidebar                                                                     |
| < 768px (mobile)     | Sidebar MUST be hidden. Hamburger icon in status bar reveals sidebar as overlay. Terminal takes full width. Input above soft keyboard. |

No swipe gestures in v1.

## Keyboard Shortcuts

All shortcuts MUST be implemented:

| Shortcut              | Action                           |
| --------------------- | -------------------------------- |
| Enter                 | Send command                     |
| Shift+Enter           | Insert newline                   |
| Up / Down             | Command history                  |
| Escape                | Clear input                      |
| Page Up / Page Down   | Scroll terminal                  |
| Ctrl+B                | Toggle sidebar                   |
| Ctrl+L                | Clear scrollback (local only)    |

## Svelte Component Structure

```text
web/src/routes/terminal/
  +page.svelte              — Terminal page (layout, routing)
  +page.ts                  — ssr=false, prerender=false

web/src/lib/components/
  terminal/
    TerminalView.svelte     — Scrollback container + auto-scroll logic
    EventRenderer.svelte    — Switches on event type, renders styled output
    AnsiRenderer.svelte     — ansi_up wrapper for plugin text
    CommandInput.svelte     — Multiline textarea + history
    StatusBar.svelte        — Top bar (character, location, connection)
  sidebar/
    Sidebar.svelte          — Collapsible container + toggle
    RoomInfo.svelte         — Location name + description
    ExitList.svelte         — Clickable exit list
    PresenceList.svelte     — Who's here + idle status

web/src/lib/stores/
    sidebarStore.ts         — Svelte store for room_state, exits, presence
    terminalStore.ts        — Scrollback buffer, replay state
    themeStore.ts           — Active theme + prefers-color-scheme

web/src/lib/theme/
    default-dark.json       — Dark theme colors
    default-light.json      — Light theme colors
    types.ts                — Theme schema TypeScript types
```

Note: `+page.ts` sets `ssr = false` and `prerender = false` because the
terminal page requires a live ConnectRPC connection and cannot be statically
rendered.

## Testing Strategy

### Unit tests

- **EventRenderer:** Each event type MUST render correct HTML structure and CSS
  classes.
- **AnsiRenderer:** ANSI escape sequences MUST produce correct styled spans.
- **CommandInput:** Enter sends, Shift+Enter inserts newline, Up/Down cycles
  history, Escape clears.
- **Sidebar stores:** `room_state` events MUST populate stores;
  `arrive`/`leave` events MUST add/remove from presence list.
- **Event routing:** TERMINAL events go to scrollback, STATE events go to
  sidebar stores, BOTH events go to both.
- **Theme application:** CSS custom properties MUST match theme JSON values.

### Integration tests

- **Scrollback auto-scroll:** Verify auto-scroll pauses when scrolled up and
  resumes when scrolled to bottom.
- **Replay treatment:** Replayed events render dimmed with separators.
- **Sidebar collapse/expand:** Toggle works, badges update on arrive/leave.
- **Reconnect flow:** Synthetic room_state populates sidebar on reconnect.

### E2E tests (Playwright)

- Connect, receive events, verify terminal output.
- Send commands, verify they appear in game output.
- Scroll up, verify auto-scroll pauses and indicator appears.
- Toggle sidebar, verify room info displays.
- Resize to mobile breakpoint, verify responsive layout.

## Out of Scope

- Tab completion (requires server-side command introspection)
- Inline image thumbnails (deferred — security design needed)
- Server-side rich link previews (rejected — security concerns)
- Custom theme editor UI
- Telnet adapter formatting (separate work, consumes same theme schema)
- Scene/RP integration in sidebar
- Character stats in sidebar

## Dependencies

| Dependency | Purpose                             | License |
| ---------- | ----------------------------------- | ------- |
| `ansi_up`  | ANSI escape code → HTML conversion  | MIT     |

No other new dependencies required. SvelteKit, ConnectRPC, and protobuf
runtimes are already in the project.

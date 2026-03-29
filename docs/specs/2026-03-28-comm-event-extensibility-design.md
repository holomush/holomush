<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Communication Event Type Extensibility Design

**Status:** Draft
**Date:** 2026-03-28
**Bead:** holomush-8l7d
**Scope:** Extensible event type model, verb registry, channel infrastructure, category-based rendering

## Overview

HoloMUSH's communication events are currently hardcoded constants in
`internal/core/event.go` (say, pose, page, whisper, arrive, leave, system). The
proto uses string for event type, so the wire format is already open, but the Go
constants and web client rendering assume a closed set. This does not scale to
channels, forums, Discord bridging, or plugin-defined communication methods
(telepathy, texting, etc.).

This spec introduces a configuration-driven event extensibility model. Event
types remain discrete strings (good for filtering, ABAC, logging), but a new
**verb registry** provides rendering metadata (category, format, label) that
clients use instead of per-type rendering logic. Built-in types register through
the same mechanism as plugin types — no special cases.

## Goals

- MUST support arbitrary event types without client code changes
- MUST provide a verb registry where built-in and plugin types register identically
- MUST introduce category-based rendering to replace per-type rendering
- MUST add global named channel infrastructure (IRC/MUSH-style)
- MUST expand the plugin SDK to support custom communication verbs
- MUST maintain backward compatibility at the storage layer (events table unchanged)
- MUST define a clear `GameEvent` proto with first-class rendering fields

## Non-Goals

- Scene system (virtual locations — separate feature, not channels)
- OOC communication (location-scoped, not a global channel)
- Forum/async discussion threads (future work)
- Discord bridging implementation (future work, but the model MUST accommodate it)
- Admin UI for channel management (future work)
- Channel moderation tools (future work)

## Design Decisions

### Discrete Types with Category Metadata

Event types MUST remain discrete strings (`say`, `telepathy`, `channel_say`).
A new `category` field MUST be added as a first-class rendering concern.

**Rationale:** Types are the natural filtering, routing, and ABAC key. You can
write `WHERE type = 'telepathy'` or subscribe to specific types. Collapsing
all communication into a single type (e.g., `type: "communication"` with a verb
field) forces consumers to inspect payload for filtering — worse ergonomics for
the common case. Category serves a different consumer (rendering) than type
(routing/filtering). The intentional denormalization is the same pattern as
keeping both a foreign key and a cached display name.

### Built-In Types Use the Same Registration Path as Plugins

Built-in event types (`say`, `pose`, `page`, etc.) MUST register in the verb
registry at server startup using the same `VerbRegistration` structure that
plugins use.

**Rationale:** If built-in types bypass the registry and use hardcoded behavior,
the registry never gets battle-tested against real usage. Bugs in the plugin
registration path only surface when plugin authors hit them. Making `say` go
through the same path as `telepathy` continuously validates the extension
mechanism. No magic, no implicit defaults.

### Category-Based Rendering (Big-Bang Migration)

Clients MUST render by `category` + `format`, never by `type`. The existing
per-type `{#if type === 'say'}` rendering chain MUST be replaced entirely.

**Rationale:** Per-type rendering is the closed-set bottleneck. Every new event
type requires a client code change. Category-based rendering provides a small,
stable set of renderers (5 categories + 1 fallback) that handle any event type
via configuration. This is a breaking change to the web client, but there are no
third-party clients to worry about — now is the time.

### Plugins Resolve Recipients

For plugin-defined communication verbs (telepathy, texting), the plugin MUST
resolve the recipient list. The server provides event delivery to character
streams; the plugin decides which characters receive the message.

**Rationale:** Recipient eligibility is game-logic (ability checks, inventory
queries, ABAC evaluations) that varies wildly per verb. A server-side predicate
DSL powerful enough to cover "has item X in inventory" would become a query
language. Plugins own the domain logic and return character IDs; the server
handles the plumbing.

### Channels as Event Streams

Global named channels MUST be implemented as a new stream type (`channel:<name>`)
using the same event store and LISTEN/NOTIFY infrastructure as location and
character streams.

**Rationale:** Channels are conceptually a message bus with membership — the same
abstraction as location streams but without spatial constraints. Reusing the
existing stream infrastructure means channels get persistence, replay, and
subscription management for free. No new delivery mechanism needed.

## Event Model

### Three Identity Fields

Every event carries three fields that together determine how it is processed and
displayed:

| Field      | Purpose            | Consumer                         | Example values                                         |
| ---------- | ------------------ | -------------------------------- | ------------------------------------------------------ |
| `type`     | What happened      | Filtering, ABAC, plugins, audit  | `say`, `pose`, `telepathy`, `channel_say`, `arrive`    |
| `category` | Rendering strategy | Client renderers (web, telnet)   | `communication`, `movement`, `state`, `system`, `command` |
| `format`   | Template selection  | Category renderer implementation | `speech`, `action`, `narrative`, `notification`, `error`, `snapshot`, `delta` |

### Categories (Closed Set)

Clients MUST handle all categories in this table. New categories require client
code changes and SHOULD be rare.

| Category        | Formats                    | Rendering strategy                            |
| --------------- | -------------------------- | --------------------------------------------- |
| `communication` | `speech`, `action`         | `<actor> <label>, "<text>"` or `<actor> <text>` |
| `movement`      | `notification`             | `<actor> has arrived/left`                    |
| `state`         | `snapshot`, `delta`        | Update sidebar, not shown in scrollback       |
| `system`        | `notification`, `error`    | System-styled text                            |
| `command`       | `narrative`, `error`       | Plain text or error-styled text               |

### Labels

Every event type that uses `format: "speech"` MUST provide an explicit `label`
in its verb registration. There are no implicit or default labels.

| Type           | Label                    |
| -------------- | ------------------------ |
| `say`          | `says`                   |
| `page`         | `pages`                  |
| `whisper`      | `whispers`               |
| `channel_say`  | `says`                   |
| `telepathy`    | `telepathically sends`   |
| `text_message` | `texts`                  |

### Display Targets

The `display_target` field replaces the current hardcoded type-to-channel
mapping. It is deterministically derived from category at event construction
time. While redundant with category, it exists for client convenience — clients
read `display_target` directly without needing a category-to-target lookup
table. The mapping is:

| Category        | Display target |
| --------------- | -------------- |
| `communication` | TERMINAL       |
| `movement`      | BOTH           |
| `state`         | STATE          |
| `system`        | TERMINAL       |
| `command`       | TERMINAL       |

### Concrete Event Examples

```text
# Player says something in a location
{type: "say", category: "communication", format: "speech",
 actor: "Joe", text: "Hello everyone", label: "says"}

# Player poses
{type: "pose", category: "communication", format: "action",
 actor: "Joe", text: "waves hello"}

# Plugin-defined telepathy (private, character-to-character)
{type: "telepathy", category: "communication", format: "speech",
 actor: "Joe", text: "Can you hear me?", label: "telepathically sends"}

# Channel message
{type: "channel_say", category: "communication", format: "speech",
 actor: "Joe", text: "Meeting at 5", label: "says",
 metadata: {channel: "staff"}}

# Channel pose
{type: "channel_pose", category: "communication", format: "action",
 actor: "Joe", text: "waves",
 metadata: {channel: "staff"}}

# Arrival
{type: "arrive", category: "movement", format: "notification",
 actor: "Joe", text: "has arrived."}

# Location state snapshot
{type: "location_state", category: "state", format: "snapshot",
 metadata: {location: {id: "...", name: "Town Square", description: "..."},
            exits: [...], present: [...]}}

# Command output
{type: "command_response", category: "command", format: "narrative",
 text: "You pick up the sword."}

# Command error
{type: "command_error", category: "command", format: "error",
 text: "I don't understand that."}
```

## Proto Changes

### GameEvent (Web Protocol)

The `GameEvent` message MUST be restructured with first-class rendering fields:

```protobuf
message GameEvent {
  // Identity (every event)
  string type = 1;
  string category = 2;
  string format = 3;
  EventChannel display_target = 4;
  int64 timestamp = 5;

  // Common display (most events — empty string when absent)
  string actor = 6;
  string text = 7;

  // Type-specific (everything else)
  google.protobuf.Struct metadata = 8;
}
```

**Field semantics:**

| Field            | Required | Description                                                |
| ---------------- | -------- | ---------------------------------------------------------- |
| `type`           | Yes      | Specific event type string                                 |
| `category`       | Yes      | Rendering category (closed set)                            |
| `format`         | Yes      | Rendering template within category                         |
| `display_target` | Yes      | Where to display: TERMINAL, STATE, or BOTH                 |
| `timestamp`      | Yes      | Unix timestamp                                             |
| `actor`          | No       | Who performed the action (empty for system/state events)   |
| `text`           | No       | Primary display text (empty for state snapshot events)     |
| `metadata`       | No       | Type-specific data: label, channel, no\_space, location, etc. |

**Design rationale for top-level fields:** A field is top-level if it is used by
the rendering dispatch logic (`category`, `format`, `display_target`) or by most
renderers regardless of category (`actor`, `text`, `timestamp`). Everything else
goes in `metadata`. `actor` and `text` are empty on state events, but state
events route to the sidebar — the scrollback renderer that reads `actor`/`text`
never sees them.

**Renamed field:** `character_name` becomes `actor`. Plugins can have
non-character actors (a system, a bot, a bridged Discord user). "Actor" is the
correct abstraction.

**Renamed field:** `channel` (previously `EventChannel`) becomes
`display_target` to avoid collision with the communication channel concept.

### EventFrame (Core Protocol)

The core `EventFrame` proto MUST NOT change. It already uses `string type` and
`bytes payload`. Category, format, and label are stored in the JSONB payload and
promoted to `GameEvent` fields by the web translation layer.

```protobuf
message EventFrame {
  string id = 1;
  string stream = 2;
  string type = 3;
  google.protobuf.Timestamp timestamp = 4;
  string actor_type = 5;
  string actor_id = 6;
  bytes payload = 7;
}
```

### Well-Known Metadata Keys

The `metadata` Struct is the extensibility escape hatch. The following keys are
defined by this spec and MUST use the documented types:

**Why `label` is in metadata, not top-level:** `label` is only used by the
`communication` category's `speech` format renderer. It is empty on every
non-speech event (poses, arrivals, state, commands — the majority of event
types). The top-level field rule is: top-level if used by rendering dispatch
logic or by most renderers regardless of category. `label` fails both tests.
The `CommunicationRenderer` reads `metadata.label` when `format === "speech"` —
a single metadata access in one renderer is acceptable.

| Key         | Type   | Used by                        | Description                        |
| ----------- | ------ | ------------------------------ | ---------------------------------- |
| `label`     | string | `communication` + `speech`     | Verb label ("says", "pages")       |
| `channel`   | string | `channel_*` types              | Channel name ("staff", "lfr")      |
| `no_space`  | bool   | `communication` + `action`     | Suppress space between actor/text  |
| `location`  | object | `location_state`               | Location snapshot data             |
| `exits`     | array  | `location_state`, `exit_update` | Exit list                          |
| `present`   | array  | `location_state`               | Characters present                 |

Plugins MAY add custom metadata keys. They SHOULD declare them in their verb
registration so clients can discover them.

## Verb Registry

### Registration Structure

```go
type VerbRegistration struct {
    Type          string        // "say", "telepathy"
    Category      string        // "communication", "movement", etc.
    Format        string        // "speech", "action", etc.
    Label         string        // "says", "telepathically sends" (required for speech)
    DisplayTarget EventChannel  // TERMINAL, STATE, BOTH
    MetadataKeys  []MetadataKey // declared metadata fields
}

type MetadataKey struct {
    Key         string // "channel", "no_space"
    Description string // human-readable purpose
    ValueType   string // "string", "bool", "object", "array"
}
```

### Concurrency and Conflict Resolution

The `VerbRegistry` MUST be thread-safe. Concurrent reads (type lookups during
translation) and writes (plugin registration during loading) MUST be safe. A
`sync.RWMutex` is the expected implementation: read lock for lookups, write lock
for registration.

**Duplicate registration policy:** Registering a type that already exists MUST
fail with an error. This prevents two plugins from claiming the same type with
conflicting metadata. Built-in types register first at startup; if a plugin
attempts to register `say`, it MUST be rejected. If two plugins attempt to
register `telepathy`, the second MUST be rejected.

### Built-In Registrations

All built-in event types MUST register at server startup:

```go
registry.Register(VerbRegistration{
    Type: "say", Category: "communication", Format: "speech",
    Label: "says", DisplayTarget: TERMINAL,
})
registry.Register(VerbRegistration{
    Type: "pose", Category: "communication", Format: "action",
    DisplayTarget: TERMINAL,
    MetadataKeys: []MetadataKey{
        {Key: "no_space", ValueType: "bool", Description: "Suppress space between actor and text"},
    },
})
registry.Register(VerbRegistration{
    Type: "page", Category: "communication", Format: "speech",
    Label: "pages", DisplayTarget: TERMINAL,
})
registry.Register(VerbRegistration{
    Type: "whisper", Category: "communication", Format: "speech",
    Label: "whispers", DisplayTarget: TERMINAL,
})
registry.Register(VerbRegistration{
    Type: "arrive", Category: "movement", Format: "notification",
    DisplayTarget: BOTH,
})
registry.Register(VerbRegistration{
    Type: "leave", Category: "movement", Format: "notification",
    DisplayTarget: BOTH,
})
registry.Register(VerbRegistration{
    Type: "location_state", Category: "state", Format: "snapshot",
    DisplayTarget: STATE,
    MetadataKeys: []MetadataKey{
        {Key: "location", ValueType: "object"},
        {Key: "exits", ValueType: "array"},
        {Key: "present", ValueType: "array"},
    },
})
registry.Register(VerbRegistration{
    Type: "exit_update", Category: "state", Format: "delta",
    DisplayTarget: STATE,
    MetadataKeys: []MetadataKey{
        {Key: "exits", ValueType: "array"},
    },
})
registry.Register(VerbRegistration{
    Type: "command_response", Category: "command", Format: "narrative",
    DisplayTarget: TERMINAL,
})
registry.Register(VerbRegistration{
    Type: "command_error", Category: "command", Format: "error",
    DisplayTarget: TERMINAL,
})
registry.Register(VerbRegistration{
    Type: "system", Category: "system", Format: "notification",
    DisplayTarget: TERMINAL,
})
registry.Register(VerbRegistration{
    Type: "channel_say", Category: "communication", Format: "speech",
    Label: "says", DisplayTarget: TERMINAL,
    MetadataKeys: []MetadataKey{
        {Key: "channel", ValueType: "string", Description: "Channel name"},
    },
})
registry.Register(VerbRegistration{
    Type: "channel_pose", Category: "communication", Format: "action",
    DisplayTarget: TERMINAL,
    MetadataKeys: []MetadataKey{
        {Key: "channel", ValueType: "string", Description: "Channel name"},
    },
})
registry.Register(VerbRegistration{
    Type: "channel_system", Category: "system", Format: "notification",
    DisplayTarget: TERMINAL,
    MetadataKeys: []MetadataKey{
        {Key: "channel", ValueType: "string", Description: "Channel name"},
    },
})
registry.Register(VerbRegistration{
    Type: "move", Category: "movement", Format: "notification",
    DisplayTarget: BOTH,
    MetadataKeys: []MetadataKey{
        {Key: "from_id", ValueType: "string"},
        {Key: "to_id", ValueType: "string"},
        {Key: "exit_name", ValueType: "string"},
    },
})
registry.Register(VerbRegistration{
    Type: "object_create", Category: "state", Format: "delta",
    DisplayTarget: STATE,
})
registry.Register(VerbRegistration{
    Type: "object_destroy", Category: "state", Format: "delta",
    DisplayTarget: STATE,
})
registry.Register(VerbRegistration{
    Type: "object_use", Category: "command", Format: "narrative",
    DisplayTarget: TERMINAL,
})
registry.Register(VerbRegistration{
    Type: "object_examine", Category: "command", Format: "narrative",
    DisplayTarget: TERMINAL,
})
registry.Register(VerbRegistration{
    Type: "object_give", Category: "command", Format: "narrative",
    DisplayTarget: TERMINAL,
})
registry.Register(VerbRegistration{
    Type: "whisper_notice", Category: "communication", Format: "action",
    DisplayTarget: TERMINAL,
})
```

### Registry Consumers

| Consumer              | How it uses the registry                                                   |
| --------------------- | -------------------------------------------------------------------------- |
| Event construction    | Pulls category/format/label when creating events instead of hardcoding     |
| Web translation layer | Looks up type to populate `GameEvent` fields from `EventFrame` payload     |
| Telnet gateway        | Looks up format/label for ANSI-colored text formatting                     |
| Client verb catalog   | Exposed via RPC so clients can discover types and rendering metadata (deferred — not required for initial implementation; clients use registry data embedded in events) |

### Unknown Types

If an event's `type` is not in the registry (e.g., a plugin was unloaded), the
system MUST NOT crash. The translation layer MUST fall back to:

- `category`: value from payload if present, otherwise `"system"`
- `format`: value from payload if present, otherwise `"narrative"`
- `label`: value from payload if present, otherwise empty string
- `display_target`: `TERMINAL`

## Channel Infrastructure

### Channel Model

Channels are global named message buses. Characters join and leave channels.
Messages broadcast to all members regardless of location. Channels are NOT
locations — they have no presence, exits, or spatial model.

### Channel Streams

Channels MUST use a new stream type: `channel:<name>` (e.g.,
`channel:staff`, `channel:lfr`). This reuses the existing event store and
LISTEN/NOTIFY subscription infrastructure.

When a character connects, the Subscribe handler MUST subscribe them to all
their channels' streams alongside their location and character streams.

### Dynamic Channel Subscription

The current Subscribe handler sets up stream subscriptions once at connection
time and then enters the live event loop. Channels require dynamic subscription
changes while the stream is active (a character joins or leaves a channel
mid-session).

The Subscribe handler MUST support adding and removing LISTEN subscriptions
during an active stream. When the channel service processes a join or leave
command, it MUST notify the Subscribe handler to update the subscription set
for that character's active connections. This uses the same notification
mechanism as location-following (where the handler switches location stream
subscriptions on move events): the handler watches for channel membership
change events on the character stream and updates its LISTEN set accordingly.

**Channel join flow:**

1. Character runs `+join staff`
2. Channel service adds row to `channel_members`, emits a `channel_join` event
   on the character stream
3. Subscribe handler receives the `channel_join` event, adds
   `LISTEN channel:staff` to the active subscription set
4. Character immediately starts receiving events from `channel:staff`

**Channel leave flow:**

1. Character runs `+leave staff`
2. Channel service removes row from `channel_members`, emits a `channel_leave`
   event on the character stream
3. Subscribe handler receives the `channel_leave` event, removes
   `LISTEN channel:staff` from the active subscription set

### Channel Name Validation

Channel names MUST match `[a-z0-9][a-z0-9_-]{0,31}` (lowercase alphanumeric
start, then lowercase alphanumeric, underscore, or hyphen, max 32 characters).
Names are case-insensitive and stored as lowercase. This prevents conflicts
with stream name parsing (the `:` separator) and ensures channel names work
as URL path segments and command arguments.

### Channel Storage

Channel metadata MUST be stored in a `channels` table:

```sql
CREATE TABLE channels (
    name        TEXT PRIMARY KEY,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    owner_id    TEXT NOT NULL REFERENCES characters(id),
    access      TEXT NOT NULL DEFAULT 'public',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE channel_members (
    channel_name TEXT NOT NULL REFERENCES channels(name) ON DELETE CASCADE,
    character_id TEXT NOT NULL,
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_name, character_id)
);
```

The `access` column stores the channel's access mode as a simple string. The
channel service translates this into ABAC evaluations:

| Access value      | ABAC evaluation                                                  |
| ----------------- | ---------------------------------------------------------------- |
| `public`          | Always allowed — no ABAC check for join                          |
| `invite`          | Check `channel:join` action with `invited: true` attribute       |
| `role:<name>`     | Check `channel:join` action with subject role matching `<name>`  |

All channel actions (join, speak, moderate, delete) flow through the ABAC
evaluator. The `access` column determines the *join* policy; speaking and
moderation use standard ABAC permission checks on the `channel:speak` and
`channel:moderate` actions respectively. Channel owners and admins bypass
join restrictions via their role permissions.

### Channel Events

Channel messages use dedicated event types with `metadata.channel`:

```text
{type: "channel_say", ..., metadata: {channel: "staff"}}
{type: "channel_pose", ..., metadata: {channel: "staff"}}
{type: "channel_system", ..., metadata: {channel: "staff"}}
```

These are registered in the verb registry like any other type.

### Channel Commands

```text
+create staff=Staff Channel     # Create channel
+join staff                     # Subscribe
+leave staff                    # Unsubscribe
+staff Hello everyone           # Say on channel
+staff :waves                   # Pose on channel
+who staff                      # List members
+channels                       # List available channels
+delete staff                   # Delete channel (owner/admin)
```

Channel access (join, speak, moderate, delete) MUST be governed by ABAC.

### Channel History

Channel streams persist in the event store. When a character joins a channel,
they MAY replay recent history (configurable per channel or globally). The
replay mechanism is the same as location stream replay on reconnect.

## Plugin Integration

### Plugin Verb Registration

Plugins MUST register custom communication verbs in their manifest:

```lua
plugin.register_verb({
    type = "telepathy",
    category = "communication",
    format = "speech",
    label = "telepathically sends",
    display_target = "terminal",
    metadata_keys = {
        {key = "requires_ability", type = "string",
         description = "Ability needed to receive"},
    },
})
```

The server MUST validate the registration (category is a known value, format is
valid for the category, label is present when format is `speech`) and add it to
the verb registry.

### Verb Registration Lifetime

Verb registrations MUST persist for the server's lifetime, even if the plugin
that registered them is unloaded. This ensures historical events of that type
continue to render correctly via the registry. The unknown-type fallback path
handles the case where events predate the registry (old data), not the case
where a plugin was recently active.

If a plugin is unloaded, its registered types remain in the registry but event
emission for those types is blocked (no plugin to handle the command). If the
plugin is reloaded, it MUST NOT re-register its types (they already exist). If
a *different* plugin attempts to register the same type, it MUST be rejected.

### Plugin Event Emission

The plugin SDK `EmitEvent` MUST be expanded beyond the current 5-type allowlist.
Plugins MUST be able to emit any event type they have registered.

```go
type EmitEvent struct {
    Stream  string // "character:<id>" or "channel:<name>"
    Type    string // Must match a registered verb
    Payload string // JSON string
}
```

The server MUST reject `EmitEvent` calls with unregistered types.

### Plugin Recipient Resolution

For private communication verbs, the plugin receives the full command context
(actor, target, message) and MUST resolve the recipient list. The server
delivers events to the character streams the plugin specifies.

**Example flow (telepathy):**

1. Player types: `tmsg sue=Can you hear me?`
2. Command handler routes to telepathy plugin
3. Plugin receives: `{actor: "Joe", target: "Sue", message: "Can you hear me?"}`
4. Plugin checks: Can Joe send? (ability check, ABAC eval)
5. Plugin checks: Can Sue receive? (ability check, ABAC eval)
6. Plugin returns: `EmitEvent{stream: "character:<sue-id>", type: "telepathy",
   payload: {message: "Can you hear me?"}}`
7. Server looks up `telepathy` in registry, enriches with category/format/label
8. Event appended to Sue's character stream
9. Sue's client receives fully formed `GameEvent`

### Plugin SDK Changes

The `pkg/plugin/event.go` EventType constants MUST be removed. The plugin SDK
MUST accept any string type that matches a registered verb, not a hardcoded
allowlist.

## Client Rendering Changes

### Web Client (SvelteKit)

The `EventRenderer.svelte` component MUST be replaced with category-based
dispatch:

```text
EventRenderer.svelte
  ├── CommunicationRenderer.svelte   (speech, action formats)
  ├── MovementRenderer.svelte        (notification format)
  ├── CommandRenderer.svelte         (narrative, error formats)
  ├── SystemRenderer.svelte          (notification, error formats)
  └── FallbackRenderer.svelte        (unknown categories)
```

State events (`category: "state"`) route to the sidebar via `eventRouter.ts`
and do not render in the scrollback.

**CommunicationRenderer** handles all communication types via format:

- `speech`: `<actor> <label>, "<text>"` — optionally prefixed with
  `[<channel>]` when `metadata.channel` is present
- `action`: `<actor> <text>` — optionally prefixed with `[<channel>]`

**FallbackRenderer** MUST handle unknown categories without crashing. It
displays `<actor> <text>` or raw metadata as a last resort.

**eventRouter.ts** MUST route on `display_target` instead of the current
hardcoded type-to-channel map.

### Telnet Gateway

The telnet gateway MUST switch from type-based to category+format-based
formatting in its `formatEvent` function. The same category/format dispatch
applies, producing ANSI-colored text instead of Svelte components.

## Migration

### Storage Layer

No `events` table migration needed. The `events` table stores `type TEXT` and
`payload JSONB`. New `channels` and `channel_members` tables require a new
migration file (see Channel Storage section).
Existing events remain valid. New fields (category, format, label) are added to
the payload at event construction time. The web translation layer reads them
from payload and promotes them to `GameEvent` fields.

Existing events in the database that lack category/format/label in their payload
MUST be handled by the translation layer using the verb registry lookup. The
registry knows that `type: "say"` maps to `category: "communication"`,
`format: "speech"`, `label: "says"`. This is the same path used for all events —
not a special migration case.

### Proto

The `GameEvent` message changes field numbers and names. This is a breaking
change to the web proto wire encoding — field 2 changes from `string
character_name` to `string category`, etc. Since there are no third-party web
clients, this is acceptable. The web client build MUST be deployed atomically
with the server to avoid wire-format mismatches. The core `EventFrame` proto is
unchanged.

### Command Response / Command Error Split

Currently, `command_error` is a synthetic type manufactured by the web
translation layer when a `command_response` event has `IsError: true`. This
violates the model where type maps 1:1 to registry metadata. The handler MUST
emit `command_response` and `command_error` as separate event types at creation
time. The `IsError` field is removed from `CommandResponsePayload` — the type
itself carries the distinction.

### Page and Whisper Visibility

The current web translation layer silently drops `page` and `whisper` events
(they fall through the default case to `return nil`). With registry-based
translation, these types will be looked up and translated like any other type.
This is an intentional behavioral change — private messages will now appear in
the web client, which is correct behavior that was previously missing.

### Go Constants

Existing `EventType*` constants in `internal/core/event.go` SHOULD be retained
as string constants for convenience but MUST NOT be the source of truth for
rendering metadata. The verb registry is authoritative.

### Plugin SDK

The `pkg/plugin/event.go` `EventType` alias (`type EventType = string`) SHOULD
be retained for readability. The hardcoded constant restrictions MUST be removed.
Validation moves from compile-time constants to runtime registry checks. This is
a mechanical refactor affecting the `Host` interface, mock implementations, and
integration tests.

## Files Affected

| File                                            | Change                                               |
| ----------------------------------------------- | ---------------------------------------------------- |
| `internal/core/event.go`                        | Add VerbRegistry, VerbRegistration types              |
| `internal/core/registry.go` (new)               | VerbRegistry implementation                          |
| `api/proto/holomush/web/v1/web.proto`           | Restructure GameEvent message                        |
| `internal/web/translate.go`                      | Populate category/format/label from registry         |
| `internal/telnet/gateway_handler.go`            | Category+format based ANSI formatting                |
| `web/src/lib/components/terminal/*.svelte`       | Category-based renderer components                   |
| `web/src/lib/stores/eventRouter.ts`             | Route on display\_target field                       |
| `pkg/plugin/event.go`                           | Remove hardcoded type constants, accept any registered type |
| `internal/plugin/subscriber.go`                 | Validate emitted types against registry              |
| `internal/store/migrations/` (new)              | Add channels and channel\_members tables             |
| `internal/world/channel.go` (new)               | Channel CRUD operations                              |
| `internal/command/handlers/channel_*.go` (new)  | Channel command handlers                             |
| `internal/grpc/subscribe.go`                    | Subscribe to channel streams on connect              |

## Testing Strategy

- Unit tests for VerbRegistry (register, lookup, unknown type fallback)
- Unit tests for web translation with registry-populated fields
- Unit tests for category-based telnet formatting
- Integration tests for channel creation, join, message delivery, leave
- Integration tests for plugin verb registration and event emission
- E2E tests for channel commands via telnet and web
- E2E tests for category-based rendering in web client
- E2E test for unknown event type fallback rendering

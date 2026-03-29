# Event Types

Events are the heartbeat of HoloMUSH. Every action in the game world --
speech, movement, object interactions, system notifications -- produces an
event. Events are immutable, ordered, and organized into streams. Clients
subscribe to streams and receive events in real time; the event store supports
replay for catch-up after reconnection.

This page is the comprehensive reference. For the plugin-developer perspective
(subscribing, filtering, emitting), see the
[Extending > Events](../extending/events.md) guide.

## Event Structure

Every event shares a common envelope:

| Field       | Type      | Description                                                    |
| ----------- | --------- | -------------------------------------------------------------- |
| `id`        | ULID      | Globally unique, time-ordered identifier                       |
| `stream`    | string    | Target stream (e.g. `location:01ABC`, `character:01XYZ`)      |
| `type`      | string    | Event type identifier (see below; this is a string, not an enum — plugins can define custom types) |
| `timestamp` | time      | When the event was created                                     |
| `actor`     | Actor     | Who or what caused the event (`character`, `system`, `plugin`) |
| `payload`   | JSON      | Type-specific data                                             |

!!! note "Event types are extensible"

    The types listed here are the built-in set defined in the core server.
    Event type is a plain string on the wire (not a closed enum), so plugins
    can emit events with custom types. Channels, forums, and other
    communication methods will add new types as they're implemented.

## Communication Events

Character-to-character communication within or across locations.
These are the built-in communication types. Plugins and future features
(channels, forums) will add more.

### `say`

Character speech broadcast to the current location.

| Payload Field | Type   | Description       |
| ------------- | ------ | ----------------- |
| `message`     | string | The spoken text   |

### `pose`

Character action or emote broadcast to the current location.

| Payload Field | Type   | Description       |
| ------------- | ------ | ----------------- |
| `message`     | string | The emote text    |

### `page`

Private message sent to another character, delivered via both characters'
`character:<id>` streams.

| Payload Field | Type   | Description                              |
| ------------- | ------ | ---------------------------------------- |
| `sender_id`   | string | ULID of the sending character            |
| `sender_name` | string | Display name of the sender               |
| `message`     | string | The message text                         |
| `is_pose`     | bool   | `true` if the page is a pose-style emote |

### `whisper`

Location-scoped private message. The full content is delivered only to the
target; bystanders receive a whisper notice (see below).

| Payload Field | Type   | Description                                 |
| ------------- | ------ | ------------------------------------------- |
| `sender_id`   | string | ULID of the whispering character            |
| `sender_name` | string | Display name of the sender                  |
| `message`     | string | The whispered text (target only)            |
| `is_pose`     | bool   | `true` if the whisper is a pose-style emote |

Bystanders receive a separate notice event with:

| Payload Field | Type   | Description                                       |
| ------------- | ------ | ------------------------------------------------- |
| `sender_name` | string | Who whispered                                     |
| `target_name` | string | Who was whispered to                              |
| `notice`      | string | Bystander-visible text (e.g. "X whispers to Y.") |

### `arrive`

Character enters a location.

| Payload Field    | Type   | Description                              |
| ---------------- | ------ | ---------------------------------------- |
| `character_name` | string | Name of the arriving character           |
| `from`           | string | Name of the location they came from      |

### `leave`

Character leaves a location.

| Payload Field    | Type   | Description                              |
| ---------------- | ------ | ---------------------------------------- |
| `character_name` | string | Name of the departing character          |
| `to`             | string | Name of the location they're heading to  |

### `system`

Server-generated notification -- login announcements, maintenance warnings,
administrative messages.

| Payload Field | Type   | Description          |
| ------------- | ------ | -------------------- |
| `message`     | string | The notification text |

## World Events

Changes to the spatial model and object state.

### `move`

Character or object moved between locations.

| Payload Field | Type    | Description                                        |
| ------------- | ------- | -------------------------------------------------- |
| `entity_type` | string  | `"character"` or `"object"`                        |
| `entity_id`   | string  | ULID of the entity that moved                      |
| `from_type`   | string  | Source container type (e.g. `"location"`)           |
| `from_id`     | string  | ULID of the source container                       |
| `to_type`     | string  | Destination container type                         |
| `to_id`       | string  | ULID of the destination container                  |
| `exit_id`     | string? | ULID of the exit used (if applicable)              |
| `exit_name`   | string? | Display name of the exit used (if applicable)      |

### `object_create`

New object created in the world.

| Payload Field | Type   | Description                          |
| ------------- | ------ | ------------------------------------ |
| `object_id`   | string | ULID of the new object               |
| `object_name` | string | Display name                         |
| `location_id` | string | ULID of the location it was placed in |

### `object_destroy`

Object permanently removed from the world.

| Payload Field | Type   | Description              |
| ------------- | ------ | ------------------------ |
| `object_id`   | string | ULID of the object       |
| `object_name` | string | Display name             |

### `object_use`

Character uses an object.

| Payload Field  | Type   | Description                    |
| -------------- | ------ | ------------------------------ |
| `object_id`    | string | ULID of the object             |
| `object_name`  | string | Display name                   |
| `character_id` | string | ULID of the character using it |

### `object_examine`

Character examines an object.

| Payload Field  | Type   | Description                        |
| -------------- | ------ | ---------------------------------- |
| `object_id`    | string | ULID of the object                 |
| `object_name`  | string | Display name                       |
| `character_id` | string | ULID of the character examining it |

### `object_give`

Object transferred from one character to another.

| Payload Field       | Type   | Description                  |
| ------------------- | ------ | ---------------------------- |
| `object_id`         | string | ULID of the object           |
| `object_name`       | string | Display name                 |
| `from_character_id` | string | ULID of the giving character |
| `to_character_id`   | string | ULID of the receiving character |

## Location Events

State snapshots and command feedback delivered to location streams.

### `location_state`

Full snapshot of a character's current location, sent on arrival or when the
location changes. Provides everything a client needs to render the location.

| Payload Field        | Type   | Description                                       |
| -------------------- | ------ | ------------------------------------------------- |
| `location.id`        | string | ULID of the location                              |
| `location.name`      | string | Display name                                      |
| `location.description` | string | Full description text                           |
| `exits[]`            | array  | Available exits (see below)                       |
| `present[]`          | array  | Characters currently in the location (see below)  |

Each exit entry:

| Field       | Type   | Description                       |
| ----------- | ------ | --------------------------------- |
| `direction` | string | Cardinal direction or custom name |
| `name`      | string | Display name of the exit          |
| `locked`    | bool   | Whether the exit is locked        |

Each present entry:

| Field  | Type   | Description                     |
| ------ | ------ | ------------------------------- |
| `name` | string | Character name                  |
| `idle` | bool   | Whether the character is idle   |

### `exit_update`

Delta update to the exit list for the current location (e.g. an exit was
locked or unlocked).

| Payload Field | Type  | Description                                |
| ------------- | ----- | ------------------------------------------ |
| `exits[]`     | array | Updated exit entries (same schema as above) |

### `command_response`

Successful command output delivered to the acting character.

| Payload Field | Type   | Description          |
| ------------- | ------ | -------------------- |
| `message`     | string | The response text    |

### `command_error`

Error response when a command fails. Translated from gRPC error codes at the
gateway layer.

| Payload Field | Type   | Description          |
| ------------- | ------ | -------------------- |
| `message`     | string | The error message    |

## Control Signals

Control signals are **not events** -- they are `ControlFrame` messages sent
alongside events on a subscription stream. They carry stream lifecycle
information rather than game state.

| Signal             | Description                                                                  |
| ------------------ | ---------------------------------------------------------------------------- |
| `REPLAY_COMPLETE`  | All historical events have been replayed; subsequent frames are live events  |
| `STREAM_CLOSED`    | The stream has been closed by the server (e.g. character logged out)         |

A `ControlFrame` contains:

| Field     | Type   | Description                           |
| --------- | ------ | ------------------------------------- |
| `signal`  | enum   | One of the signals above              |
| `message` | string | Optional human-readable detail        |

## Stream Types

Events are routed to named streams. Clients subscribe to one or more streams
to receive relevant events.

| Stream Pattern   | Description                                                  |
| ---------------- | ------------------------------------------------------------ |
| `location:<id>`  | All events occurring in a specific location                  |
| `character:<id>` | Private messages, notifications, and per-character state     |
| `channel:<name>` | Named communication channel (e.g. `channel:ooc`)            |

Plugins can subscribe to wildcard patterns:

| Pattern         | Matches                       |
| --------------- | ----------------------------- |
| `location:*`    | All location events           |
| `global:*`      | All global events             |
| `*`             | Everything                    |

Most plugins subscribe to `location:*` or specific event types within
locations. The `character:<id>` stream carries private messages (pages,
whispers) and per-character notifications.

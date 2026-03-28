# Describe, Page & Whisper Commands — Design

Three new commands for HoloMUSH v0.1: `describe` (set descriptions),
`page` (OOC private messaging), and `whisper` (IC private messaging).

## RFC 2119

Keywords MUST, SHOULD, MAY per RFC 2119.

---

## 1. `describe` — Core Go Handler

### Purpose

Set the description of a character, object, or location. Viewed via `look`.

### Syntax

| Form                         | Target                        | Capability       |
| ---------------------------- | ----------------------------- | ---------------- |
| `describe me <text>`         | Caller's character            | `player.describe` |
| `describe here <text>`       | Current location              | `objects.set`    |
| `describe <target>=<text>`   | Named object/location         | `objects.set`    |

`desc` MUST be registered as a system alias for `describe`.

### Parsing Rules

1. If args start with `me`, target is caller's character, remainder is text.
2. If args start with `here`, target is current location, remainder is text.
3. Otherwise, split on first `=`. Left side is target name, right side is text.
4. Empty text MUST return an invalid-args error with usage hint.

### Implementation

- New file: `internal/command/handlers/describe.go`
- **`me` path:** Fetch character via `WorldService.GetCharacter`, set
  `Description` field, persist via `CharacterRepository.Update`. Check
  `player.describe` capability via ABAC engine before mutating.
  `applyProperty()` does not support characters — this path bypasses
  the property system entirely.
- **`here` / named target path:** Resolve via existing `resolveTarget()`,
  look up `description` in the property registry, apply via `applyProperty()`.
  ABAC enforces `objects.set` inside the property system.
- Output on success: `Description set.`

### WorldService Dependency

The `me` path requires access to `CharacterRepository`. Today
`WorldService` exposes `GetCharacter` but not a direct update-description
method. The handler MUST call `WorldService.GetCharacter` + mutate the
struct + `CharacterRepository.Update`. This requires `CharacterRepository`
to be accessible from the handler — either exposed on `Services` or via a
new `WorldService.UpdateCharacterDescription(ctx, subjectID, charID, desc)` method.

The `WorldService` method is preferred — it keeps repository access behind
the service and can enforce ABAC uniformly. This method MUST be added to
the `WorldService` interface in `internal/command/types.go`.

### Registration

Register in `handlers.RegisterAll` with:

- Name: `describe`
- Capability: none (checked inside handler based on target)
- Source: `core`

---

## 2. `page` — Core Go Handler

### Purpose

OOC private messaging between characters. Works across locations.
No one else sees the message.

### Syntax

| Form | Behavior |
|------|----------|
| `page <character>=<message>` | Page character, set as last-paged |
| `page <message>` | Page last-paged character |
| `page <character>=:<action>` | Pose-page to character |

`p` MUST be registered as a system alias for `page`.

### Parsing Rules

1. If args contain `=`, split on first `=`. Left is target name, right is message.
2. If no `=`, entire args is the message; target is last-paged from session.
3. If no `=` and no last-paged target, return error: `Page who? Use: page <name>=<message>`
4. If message starts with `:`, treat as pose. Strip `:`, prepend sender name + space.
5. If message starts with `;`, treat as pose. Strip `;`, prepend sender name (no space).
6. Empty message MUST return an invalid-args error.

### Events

**Target's character stream** — `page` event:

```json
{
  "sender_id": "<ulid>",
  "sender_name": "Sean",
  "message": "Hey there",
  "is_pose": false
}
```

**Sender's character stream** — `command_response`:

| Mode | Sender sees                          | Target sees                |
| ---- | ------------------------------------ | -------------------------- |
| Say  | `You paged Alex: Hey there`          | `Sean pages: Hey there`    |
| Pose | `Long distance to Alex: Sean waves.` | `From afar, Sean waves.`   |

No location stream event. Page is entirely private.

### New Event Type

```go
EventTypePage EventType = "page"
```

### Target Resolution

New method on `session.Access`:

```go
FindByCharacterName(ctx context.Context, name string) (*Info, error)
```

Case-insensitive match against active sessions. Returns
`SESSION_NOT_FOUND` code if no match.

MUST also be added to:

- `session.Store` interface
- `PostgresSessionStore` (SQL: `WHERE LOWER(character_name) = LOWER($1) AND status = 'active'`)
- `session.MemStore` (iterate + case-insensitive compare)
- All test mocks/stubs that implement `session.Access`

### Last-Paged State

New field on `session.Info`:

```go
LastPaged string // character name of last page target
```

Persisted to Postgres via a new `last_paged` column on the `sessions` table.
Updated on every successful page with a target.

Migration: `ALTER TABLE sessions ADD COLUMN last_paged TEXT NOT NULL DEFAULT ''`

The handler reads `LastPaged` from the session info already fetched by the
gRPC `HandleCommand` path. Updates via a new method on `session.Access`:

```go
UpdateLastPaged(ctx context.Context, sessionID string, name string) error
```

This is a dedicated method rather than extending `UpdateActivity`, to
avoid changing the existing interface contract and all its callers/mocks.

### Capability

`comms.page` — granted to all players by default.

---

## 3. `whisper` — Lua Plugin

### Purpose

IC private messaging. Target MUST be in the same location.
Others in the location see that a whisper occurred (but not the content).
Game-customizable via Lua (overhearing, stealth, etc.).

### Syntax

| Form | Behavior |
|------|----------|
| `whisper <character>=<message>` | Whisper to co-located character |
| `whisper <message>` | Whisper to last-whispered character |
| `whisper <character>=:<action>` | Pose-whisper |

`w` MUST be registered as a system alias for `whisper`. This is a
shorthand for the full `whisper` command, not a pose-specific variant.
Note: some test fixtures use `w` → `west` as example alias data. The
production alias `w` → `whisper` takes precedence. Players use `west`
or `move west` directly for navigation — `w` is not needed for movement.

### Parsing Rules

Same as `page` — split on `=`, fall back to last-whispered.

### Events

**Location stream** — `whisper` event (visible to all in location):

```json
{
  "sender_name": "Sean",
  "target_name": "Alex",
  "notice": "Sean whispers to Alex."
}
```

Content is NOT included in the location event.

**Target's character stream** — `whisper` event:

| Mode | Text |
|------|------|
| Say | `Sean whispers, "Hey there"` |
| Pose | `From nearby, Sean waves.` |

**Sender's character stream** — via `holo.emit.character`:

| Mode | Text |
|------|------|
| Say  | `You whisper to Alex: Hey there`         |
| Pose | `You whisper-pose to Alex: waves.`       |

### New Event Type

```go
EventTypeWhisper EventType = "whisper"
```

### Same-Location Check

The Lua handler calls `holo.session.find_by_name(name)` to get the
target's session info including `location_id`. If the target's location
does not match the sender's location, return:
`You don't see anyone named "Alex" here.`

If the target is not online at all:
`No one named "Alex" is connected.`

### New Host Function

```text
holo.session.find_by_name(name) -> table | nil
```

Returns `{character_id, character_name, location_id}` for the named
character's active session. Case-insensitive. Returns nil if not found.

Implemented in `internal/plugin/hostfunc/stdlib.go`. Calls
`session.Access.FindByCharacterName` (same method added for `page`).

The Lua host function pipeline receives dependencies via `RegisterStdlib`.
`session.Access` (or `session.Store`, which embeds `Access`) MUST be
passed to `RegisterStdlib` so the `holo.session` namespace can call
session methods. If `RegisterStdlib` does not currently accept a session
dependency, it MUST be extended.

Requires new capability: `session.query` — granted to plugins that
declare it in their `plugin.yaml`.

### Last-Whispered State

New field on `session.Info`:

```go
LastWhispered string // character name of last whisper target
```

Persisted to Postgres via `last_whispered` column.

Migration: same migration as `last_paged` — add both columns together.

The Lua handler updates this via a new host function:

```text
holo.session.set_last_whispered(name)
```

Scoped to the caller's session (session ID from command context).
Implemented in `internal/plugin/hostfunc/stdlib.go`, calls a new
`UpdateLastWhispered(ctx, sessionID, name)` method on `session.Access`.

### Plugin Registration

Add to `plugins/communication/plugin.yaml`:

- Command `whisper` with capability `comms.whisper`
- Policy for `session.query` capability

### Customization

Operators MAY modify the whisper Lua to add:

- Overhearing chance based on attributes
- Stealth/perception checks
- Distance-based whisper range
- Logging for moderation

---

## 4. Shared Infrastructure

### Migration

One migration adding two columns to `sessions`:

```sql
ALTER TABLE sessions
  ADD COLUMN last_paged TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_whispered TEXT NOT NULL DEFAULT '';
```

### New `session.Access` Methods

Three new methods MUST be added to `session.Access` (and `session.Store`,
which embeds it):

```go
FindByCharacterName(ctx context.Context, name string) (*Info, error)
UpdateLastPaged(ctx context.Context, sessionID string, name string) error
UpdateLastWhispered(ctx context.Context, sessionID string, name string) error
```

Implementations in `PostgresSessionStore` and `MemStore`. All test
mocks/stubs that implement `session.Access` MUST add these methods.

### System Aliases

Seeded on first boot (or added to existing alias seeding if `.3` is done):

- `desc` → `describe`
- `p` → `page`
- `w` → `whisper`

If alias seeding (holomush-a3a7.3) is not yet implemented, these three
aliases SHOULD be added to the `RegisterAll` function as additional
command registrations (same handler, different name) rather than waiting
for the alias system.

---

## 5. Testing

### `describe`

- `describe me <text>` sets caller's character description
- `describe here <text>` sets location description (with `objects.set` capability)
- `describe <target>=<text>` sets named target description
- `describe me` with no text returns usage error
- ABAC: player without `objects.set` cannot describe objects/locations

### `page`

- `page alex=Hey` delivers page event to Alex's character stream
- Sender receives `You paged Alex: Hey` command_response
- `page alex=:waves` delivers pose: `From afar, Sean waves.`
- `page Hey` uses last-paged target
- `page Hey` with no last-paged returns error
- Target not online returns `No one named "Alex" is connected.`
- Last-paged persists across commands in same session

### `whisper`

- `whisper alex=Hey` delivers whisper to Alex + location notice
- Location notice is `Sean whispers to Alex.` (no content)
- Target not in same location returns error
- Target not online returns error
- `whisper alex=:waves` delivers pose-whisper
- `whisper Hey` uses last-whispered target
- Last-whispered persists across commands in same session

### Event Type Registration

Adding `EventTypePage` and `EventTypeWhisper` to `core/event.go` MUST
also update `TestDocumentedEventTypes` in `core/event_test.go` and the
plugin authoring docs, or the test will fail.

### Integration

- Page and whisper E2E tests use the existing telnet E2E infrastructure
  (two concurrent telnet connections to the Docker stack)
- Describe output visible via `look` after setting

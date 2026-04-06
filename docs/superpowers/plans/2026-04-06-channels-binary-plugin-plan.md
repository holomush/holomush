<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Channels Binary Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working channel communication system as a schema-isolated binary plugin — channel creation, membership, messaging, history, and gag control — integrated with the ABAC engine and event system.

**Architecture:** Channels are a standalone binary plugin (`type: binary`) following the core-scenes pattern. The plugin runs as a separate process via hashicorp/go-plugin, communicates over gRPC, and owns its own PostgreSQL schema (`plugin_core_channels`). Messages are dual-written: stored in the plugin's `channel_messages` table for history queries, and emitted as events via `CommandResponse.Events` for real-time subscriber delivery. All command handlers run inside the plugin process.

**Tech Stack:** Go, hashicorp/go-plugin, PostgreSQL (pgx), plugin storage SDK (`pkg/plugin/storage`), protobuf, testify for unit tests.

**Spec:** `docs/specs/2026-04-03-channels-architecture.md`

**Supersedes:** `docs/superpowers/plans/2026-04-04-channels-implementation-plan.md` (built against the old `type: core` plugin architecture removed in PR #192)

**Beads:** Epic `holomush-0sc`, tasks `holomush-0sc.3` through `holomush-0sc.7`

**Scope:** Infrastructure prerequisites, Phase 10.1 (schema), Phase 10.2 (core commands, excluding `=` alias — blocked on `holomush-3ozp`), and Phase 10.5 (history). Phases 10.3 (invite) and 10.4 (moderation) are deferred to separate plans.

**Blocked:** The `=channelname message` shorthand requires plugin manifest alias declarations (`holomush-3ozp`). The `channel say` command works via `channel say name=message` syntax; the `=` shorthand is deferred.

---

## Key Design Decisions

### Schema Isolation

Binary plugins get their own PostgreSQL schema (`plugin_core_channels`). This means:

- **No foreign keys** to core tables (`players`, `characters`). IDs are stored as TEXT and validated at the application level.
- **Plugin-local migrations** in `plugins/core-channels/migrations/`, not `internal/store/migrations/`.
- **Own migration tracker** — `plugin_migrations` table (SDK handles this).

### Dual-Write for Messages

The plugin cannot read from the core event store (schema isolation). Channel messages are:

1. **Stored** in `channel_messages` table — authoritative source for history queries
2. **Emitted** via `CommandResponse.Events` — for real-time delivery to subscribers

This is consistent with how core-scenes stores its own state independently of events.

### Channel Bootstrap

The plugin seeds default channels (e.g., "Public") during `Init()`, not via `internal/bootstrap/`. Seeded channels use `owner_id = "system"` since the plugin cannot look up admin players.

### No gRPC Service (Yet)

Unlike core-scenes which provides `SceneService`, the channel plugin does not expose a gRPC service in this phase. Commands handle all interactions. A `ChannelService` proto will be needed when the Discord bridge (Epic 12) or ABAC `ChannelAttributeProvider` require cross-plugin channel queries.

---

## File Structure

### New Files

| File | Responsibility |
| --- | --- |
| `plugins/core-channels/plugin.yaml` | Binary plugin manifest with command declarations |
| `plugins/core-channels/main.go` | Plugin entry point, Init, HandleCommand routing, seeding |
| `plugins/core-channels/types.go` | Channel, Membership, Gag entities; ChannelType/MemberRole enums; validation; constants |
| `plugins/core-channels/types_test.go` | Entity validation tests |
| `plugins/core-channels/store.go` | ChannelStore — all PostgreSQL operations |
| `plugins/core-channels/store_test.go` | Store unit tests (against test helpers, not real DB) |
| `plugins/core-channels/handlers.go` | All command handler implementations |
| `plugins/core-channels/handlers_test.go` | Handler unit tests with mock store |
| `plugins/core-channels/migrations/000001_channels.up.sql` | Channel tables (schema-isolated, no FK to core) |
| `plugins/core-channels/migrations/000001_channels.down.sql` | Drop channel tables |

### Modified Files

| File | Change |
| --- | --- |
| `internal/core/store.go` | Add `ReplayTail` method to `EventStore` interface |
| `internal/core/store_memory.go` | Add `ReplayTail` to `MemoryEventStore` |
| `internal/store/postgres.go` | Add `ReplayTail` PostgreSQL implementation |
| `internal/core/event.go` | Add channel event type constants and payload structs |
| `internal/core/builtins.go` | Replace `channel_system` with individual notification verb registrations |
| `internal/access/prefix.go` | Add `ResourceChannel` constant, `ChannelResource()` helper, update `knownPrefixes` |
| `internal/command/types.go` | Add `"channel"` to `validResourceTypes`, add channel actions to `validActions` |
| `internal/access/policy/seed.go` | Add 11 channel ABAC seed policies |

---

## Task 1: EventStore.ReplayTail — Interface, Memory, and Postgres

**Bead:** Infrastructure (unblocked). Useful beyond channels (e.g., location arrival context). Channels use their own message table for history, but this is still needed infrastructure per the spec.

**Files:**

- Modify: `internal/core/store.go`
- Modify: `internal/core/store_memory.go`
- Modify: `internal/store/postgres.go`
- Test: `internal/core/store_memory_test.go`

- [ ] **Step 1: Write failing test for MemoryEventStore.ReplayTail**

In `internal/core/store_memory_test.go`, add:

```go
func TestMemoryEventStore_ReplayTail(t *testing.T) {
	store := NewMemoryEventStore()
	ctx := context.Background()
	stream := "channel:test"
	now := time.Now()

	ids := make([]ulid.ULID, 5)
	for i := range 5 {
		ids[i] = NewULID()
		err := store.Append(ctx, Event{
			ID:        ids[i],
			Stream:    stream,
			Type:      EventType("channel_say"),
			Timestamp: now.Add(time.Duration(i) * 10 * time.Millisecond),
			Actor:     Actor{Kind: ActorCharacter, ID: "char-1"},
			Payload:   []byte(`{"message":"msg"}`),
		})
		require.NoError(t, err)
	}

	t.Run("returns last N events in chronological order", func(t *testing.T) {
		events, err := store.ReplayTail(ctx, stream, 3, time.Time{})
		require.NoError(t, err)
		assert.Len(t, events, 3)
		assert.Equal(t, ids[2], events[0].ID)
		assert.Equal(t, ids[3], events[1].ID)
		assert.Equal(t, ids[4], events[2].ID)
	})

	t.Run("respects notBefore filter", func(t *testing.T) {
		cutoff := now.Add(25 * time.Millisecond)
		events, err := store.ReplayTail(ctx, stream, 10, cutoff)
		require.NoError(t, err)
		assert.Len(t, events, 2)
		assert.Equal(t, ids[3], events[0].ID)
		assert.Equal(t, ids[4], events[1].ID)
	})

	t.Run("returns empty slice for unknown stream", func(t *testing.T) {
		events, err := store.ReplayTail(ctx, "channel:nonexistent", 10, time.Time{})
		require.NoError(t, err)
		assert.Empty(t, events)
	})

	t.Run("count larger than available returns all events", func(t *testing.T) {
		events, err := store.ReplayTail(ctx, stream, 100, time.Time{})
		require.NoError(t, err)
		assert.Len(t, events, 5)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestMemoryEventStore_ReplayTail ./internal/core/`

Expected: Compilation error — `ReplayTail` not defined.

- [ ] **Step 3: Add ReplayTail to EventStore interface**

In `internal/core/store.go`, add to the `EventStore` interface:

```go
// ReplayTail returns up to count events from a stream, reading backward
// from the most recent. Events with timestamps at or before notBefore are
// excluded. If notBefore is zero, no time filter is applied. Results are
// returned in chronological (oldest-first) order.
ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time) ([]Event, error)
```

- [ ] **Step 4: Implement ReplayTail on MemoryEventStore**

In `internal/core/store_memory.go`:

```go
// ReplayTail returns the last count events from a stream, optionally
// filtered by notBefore. Results are in chronological order.
func (s *MemoryEventStore) ReplayTail(_ context.Context, stream string, count int, notBefore time.Time) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := s.streams[stream]
	if len(events) == 0 {
		return nil, nil
	}

	var filtered []Event
	hasFilter := !notBefore.IsZero()
	for _, e := range events {
		if hasFilter && !e.Timestamp.After(notBefore) {
			continue
		}
		filtered = append(filtered, e)
	}

	if len(filtered) > count {
		filtered = filtered[len(filtered)-count:]
	}

	return filtered, nil
}
```

- [ ] **Step 5: Add ReplayTail stubs to all test EventStore implementations**

Search for all types implementing `EventStore` in test files and add the `ReplayTail` stub. Check these files:

- `internal/core/engine_test.go` — `failingEventStore`
- `internal/command/dispatcher_test.go` — `stubEventStore` and `mockEventStore`
- `internal/grpc/server_test.go` — `mockEventStore`
- `internal/plugin/service_proxy_impl_test.go` — `mockEventStore` (if it still exists)
- `test/integration/phase1_5_test.go` — `noopEventStore`
- `test/integration/command/ratelimit_integration_test.go` — `stubEventStore`

For each, add:

```go
func (s *<type>) ReplayTail(_ context.Context, _ string, _ int, _ time.Time) ([]core.Event, error) {
	return nil, nil
}
```

Ensure `"time"` is imported in each file that needs it.

- [ ] **Step 6: Implement ReplayTail on PostgresEventStore**

In `internal/store/postgres.go`, add after the existing `Replay` method:

```go
// ReplayTail returns up to count events from a stream, reading backward from
// the most recent. Events with timestamps at or before notBefore are excluded.
// Results are returned in chronological (oldest-first) order.
func (s *PostgresEventStore) ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time) ([]core.Event, error) {
	var rows pgx.Rows
	var err error

	if notBefore.IsZero() {
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM events WHERE stream = $1
			 ORDER BY id DESC LIMIT $2`,
			stream, count)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM events WHERE stream = $1 AND created_at > $2
			 ORDER BY id DESC LIMIT $3`,
			stream, notBefore, count)
	}
	if err != nil {
		return nil, oops.With("operation", "query events (tail)").With("stream", stream).Wrap(err)
	}
	defer rows.Close()

	var events []core.Event
	for rows.Next() {
		var e core.Event
		var idStr string
		var typeStr string
		if scanErr := rows.Scan(&idStr, &e.Stream, &typeStr, &e.Actor.Kind, &e.Actor.ID, &e.Payload, &e.Timestamp); scanErr != nil {
			return nil, oops.With("operation", "scan event row").Wrap(scanErr)
		}
		parsed, parseErr := ulid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.With("operation", "parse event ID").Wrap(parseErr)
		}
		e.ID = parsed
		e.Type = core.EventType(typeStr)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate events").With("stream", stream).Wrap(err)
	}

	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}

	return events, nil
}
```

- [ ] **Step 7: Run all tests**

Run: `task test`

Expected: All tests pass (including the new ReplayTail tests).

- [ ] **Step 8: Commit**

```text
feat(core): add ReplayTail method to EventStore interface

Supports count-based tail reads with optional time filter.
Includes MemoryEventStore and PostgresEventStore implementations
plus stubs for all test EventStore types.
```

---

## Task 2: Access Control, Command Types, and Event Constants

**Bead:** Infrastructure (unblocked). These three changes are small and independent — combined into one task.

**Files:**

- Modify: `internal/access/prefix.go`
- Modify: `internal/access/prefix_test.go`
- Modify: `internal/command/types.go`
- Modify: `internal/command/types_test.go`
- Modify: `internal/core/event.go`
- Modify: `internal/core/builtins.go`

- [ ] **Step 1: Add channel resource prefix**

In `internal/access/prefix.go`:

Add to resource prefix constants:

```go
ResourceChannel  = "channel:"
```

Add to `knownPrefixes` slice:

```go
ResourceChannel,
```

Add helper function:

```go
// ChannelResource returns a channel resource reference string.
func ChannelResource(channelID string) string {
	if channelID == "" {
		panic("access.ChannelResource: empty channelID would create invalid resource reference")
	}
	return ResourceChannel + channelID
}
```

- [ ] **Step 2: Add prefix tests**

In `internal/access/prefix_test.go`:

```go
func TestChannelResourceReturnsChannelPrefixedID(t *testing.T) {
	got := ChannelResource("01HXYZ")
	assert.Equal(t, "channel:01HXYZ", got)
}

func TestChannelResourcePanicsOnEmptyID(t *testing.T) {
	assert.PanicsWithValue(t,
		"access.ChannelResource: empty channelID would create invalid resource reference",
		func() { ChannelResource("") },
	)
}

func TestParseEntityRefParsesChannelResource(t *testing.T) {
	typeName, id, err := ParseEntityRef("channel:01HXYZ")
	require.NoError(t, err)
	assert.Equal(t, "channel", typeName)
	assert.Equal(t, "01HXYZ", id)
}
```

- [ ] **Step 3: Add channel command types**

In `internal/command/types.go`:

Add to `validActions`:

```go
"join": true, "leave": true, "list": true, "create": true,
```

Add to `validResourceTypes`:

```go
"channel": true,
```

- [ ] **Step 4: Add command type tests**

In `internal/command/types_test.go`:

```go
func TestCapabilityAcceptsChannelResourceType(t *testing.T) {
	capability := Capability{Action: "emit", Resource: "channel", Scope: ScopeLocal}
	err := capability.Validate()
	assert.NoError(t, err)
}

func TestCapabilityAcceptsChannelActions(t *testing.T) {
	actions := []string{"join", "leave", "list", "create"}
	for _, action := range actions {
		t.Run("accepts "+action+" action", func(t *testing.T) {
			capability := Capability{Action: action, Resource: "channel", Scope: ScopeLocal}
			err := capability.Validate()
			assert.NoError(t, err)
		})
	}
}
```

- [ ] **Step 5: Add channel event type constants**

In `internal/core/event.go`, add to EventType constants:

```go
// Channel communication event types.
EventTypeChannelSay    EventType = "channel_say"
EventTypeChannelPose   EventType = "channel_pose"
EventTypeChannelJoin   EventType = "channel_join"
EventTypeChannelLeave  EventType = "channel_leave"
EventTypeChannelMute   EventType = "channel_mute"
EventTypeChannelBan    EventType = "channel_ban"
EventTypeChannelKick   EventType = "channel_kick"
EventTypeChannelRename EventType = "channel_rename"
```

Add payload structs (after existing payload structs):

```go
// ChannelMessagePayload is the JSON payload for channel_say and channel_pose events.
type ChannelMessagePayload struct {
	ChannelID     string `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	CharacterID   string `json:"character_id,omitempty"`
	CharacterName string `json:"character_name,omitempty"`
	AuthorName    string `json:"author_name"`
	Message       string `json:"message"`
	Source        string `json:"source"`
}

// ChannelNotificationPayload is the JSON payload for channel join/leave/mute/ban/kick events.
type ChannelNotificationPayload struct {
	ChannelID     string `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	CharacterID   string `json:"character_id,omitempty"`
	CharacterName string `json:"character_name,omitempty"`
	PlayerID      string `json:"player_id"`
	Message       string `json:"message,omitempty"`
}
```

- [ ] **Step 6: Update verb registry**

In `internal/core/builtins.go`, replace the 3 existing channel registrations (`channel_say`, `channel_pose`, `channel_system`) with 8 individual registrations:

```go
// Channel communication types
{
	Type: "channel_say", Category: "communication", Format: "speech", Label: "says",
	DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
	MetadataKeys: []MetadataKey{{Key: "channel", ValueType: "string", Description: "Channel name"}},
},
{
	Type: "channel_pose", Category: "communication", Format: "action",
	DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
	MetadataKeys: []MetadataKey{{Key: "channel", ValueType: "string", Description: "Channel name"}},
},
{
	Type: "channel_join", Category: "communication", Format: "notification",
	DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
	MetadataKeys: []MetadataKey{{Key: "channel", ValueType: "string", Description: "Channel name"}},
},
{
	Type: "channel_leave", Category: "communication", Format: "notification",
	DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
	MetadataKeys: []MetadataKey{{Key: "channel", ValueType: "string", Description: "Channel name"}},
},
{
	Type: "channel_mute", Category: "communication", Format: "notification",
	DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
	MetadataKeys: []MetadataKey{{Key: "channel", ValueType: "string", Description: "Channel name"}},
},
{
	Type: "channel_ban", Category: "communication", Format: "notification",
	DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
	MetadataKeys: []MetadataKey{{Key: "channel", ValueType: "string", Description: "Channel name"}},
},
{
	Type: "channel_kick", Category: "communication", Format: "notification",
	DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
	MetadataKeys: []MetadataKey{{Key: "channel", ValueType: "string", Description: "Channel name"}},
},
{
	Type: "channel_rename", Category: "communication", Format: "notification",
	DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL,
	MetadataKeys: []MetadataKey{{Key: "channel", ValueType: "string", Description: "Channel name"}},
},
```

- [ ] **Step 7: Run all tests**

Run: `task test`

Expected: All tests pass.

- [ ] **Step 8: Commit**

```text
feat(core): add channel infrastructure — resource prefix, command types, event constants

Adds ResourceChannel to ABAC prefix system, channel actions/resource
to command types, 8 channel event type constants with payloads, and
individual verb registrations replacing the single channel_system type.
```

---

## Task 3: ABAC Seed Policies

**Bead:** Infrastructure (unblocked).

**Files:**

- Modify: `internal/access/policy/seed.go`
- Modify: `internal/access/policy/seed_test.go`
- Modify: `internal/access/policy/bootstrap_test.go`

- [ ] **Step 1: Add 11 channel seed policies**

In `internal/access/policy/seed.go`, add to the end of the `SeedPolicies()` return slice (before the closing `}`). Update the count in the function comment accordingly.

```go
// --- Channel seed policies (Epic 10) ---
// See docs/specs/2026-04-03-channels-architecture.md for rationale.
// Note: These policies require a ChannelAttributeProvider to resolve
// resource.channel.* and principal.character.channel_* attributes at
// evaluation time (not yet implemented). Until then, channel operations
// fall back to default-deny; the plugin handles its own authorization.

{
	Name:        "seed:channel-list",
	Description: "All players can list public and admin channels",
	DSLText:     `permit(principal is character, action in ["list"], resource is channel) when { resource.channel.type == "public" || resource.channel.type == "admin" };`,
	SeedVersion: 1,
},
{
	Name:        "seed:channel-join-public",
	Description: "All players can join public channels",
	DSLText:     `permit(principal is character, action in ["join"], resource is channel) when { resource.channel.type == "public" };`,
	SeedVersion: 1,
},
{
	Name:        "seed:channel-member-actions",
	Description: "Members can send, read, and leave their channels",
	DSLText:     `permit(principal is character, action in ["emit", "read", "leave"], resource is channel) when { resource.channel.id in principal.character.channel_memberships };`,
	SeedVersion: 1,
},
{
	Name:        "seed:channel-admin-create",
	Description: "Only admins can create channels by default",
	DSLText:     `permit(principal is character, action in ["create"], resource is channel) when { "admin" in principal.character.roles };`,
	SeedVersion: 1,
},
{
	Name:        "seed:channel-admin-delete",
	Description: "Only admins can delete channels",
	DSLText:     `permit(principal is character, action in ["delete"], resource is channel) when { "admin" in principal.character.roles };`,
	SeedVersion: 1,
},
{
	Name:        "seed:channel-admin-moderate",
	Description: "Only admins can moderate channels",
	DSLText:     `permit(principal is character, action in ["admin"], resource is channel) when { "admin" in principal.character.roles };`,
	SeedVersion: 1,
},
{
	Name:        "seed:channel-forbid-banned",
	Description: "Banned players cannot emit, read, or join channels",
	DSLText:     `forbid(principal is character, action in ["emit", "read", "join"], resource is channel) when { principal.character.channel_banned == true };`,
	SeedVersion: 1,
},
{
	Name:        "seed:channel-forbid-muted",
	Description: "Muted players cannot emit on channels",
	DSLText:     `forbid(principal is character, action in ["emit"], resource is channel) when { principal.character.channel_muted == true };`,
	SeedVersion: 1,
},
{
	Name:        "seed:channel-forbid-archived",
	Description: "No one can emit or join archived channels",
	DSLText:     `forbid(principal is character, action in ["emit", "join"], resource is channel) when { resource.channel.archived == true };`,
	SeedVersion: 1,
},
{
	Name:        "seed:channel-guest-seeded-only",
	Description: "Guests can list and join seeded public channels",
	DSLText:     `permit(principal is character, action in ["list", "join"], resource is channel) when { "guest" in principal.character.roles && resource.channel.type == "public" };`,
	SeedVersion: 1,
},
{
	Name:        "seed:channel-guest-forbid-create",
	Description: "Guests cannot create, delete, or moderate channels",
	DSLText:     `forbid(principal is character, action in ["create", "delete", "admin"], resource is channel) when { "guest" in principal.character.roles };`,
	SeedVersion: 1,
},
```

- [ ] **Step 2: Update seed tests**

Update `internal/access/policy/seed_test.go`:

- Increment the total count assertion
- Update permit/forbid distribution counts
- Add the 11 channel policy names to the expected names list
- Add the 4 channel forbid names to the forbid allowlist

Update `internal/access/policy/bootstrap_test.go`:

- Update forbid count and allowlist if tested there

- [ ] **Step 3: Run tests**

Run: `task test -- ./internal/access/policy/...`

Expected: All tests pass.

- [ ] **Step 4: Commit**

```text
feat(access): add 11 channel ABAC seed policies

Covers listing, joining, member actions, admin operations, ban/mute
enforcement, archived protection, and guest restrictions. Policies
require ChannelAttributeProvider for evaluation (deferred).
```

---

## Task 4: Plugin Manifest and Migrations

**Bead:** `holomush-0sc.3` (Phase 10.1)

**Files:**

- Create: `plugins/core-channels/plugin.yaml`
- Create: `plugins/core-channels/migrations/000001_channels.up.sql`
- Create: `plugins/core-channels/migrations/000001_channels.down.sql`

- [ ] **Step 1: Create plugin manifest**

```yaml
# plugins/core-channels/plugin.yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

name: core-channels
version: 1.0.0
type: binary

storage: postgres

binary-plugin:
  executable: core-channels

commands:
  - name: channel
    capabilities:
      - action: list
        resource: channel
        scope: global
      - action: join
        resource: channel
        scope: global
      - action: leave
        resource: channel
        scope: global
      - action: create
        resource: channel
        scope: global
      - action: emit
        resource: channel
        scope: global
      - action: read
        resource: channel
        scope: global
      - action: delete
        resource: channel
        scope: global
    help: Manage and communicate on channels
    usage: "channel <create|delete|join|leave|list|say|who|history|gag|ungag> [args]"
    helpText: |-
      Channel commands for creating, joining, and communicating on channels.

      Subcommands:
        create <name> [type]  - Create a new channel (type: public, private, admin)
        delete <name>         - Archive a channel
        join <name>           - Join a channel
        leave <name>          - Leave a channel
        list                  - List available channels
        say <name>=<message>  - Send a message to a channel
        who <name>            - Show who is on a channel
        history <name> [N]    - Show last N messages (default 20)
        gag <name>            - Silence a channel for this character
        ungag <name>          - Unsilence a channel for this character
```

- [ ] **Step 2: Create up migration**

Note: No FK references to core tables (schema isolation). IDs stored as TEXT.

```sql
-- plugins/core-channels/migrations/000001_channels.up.sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS channels (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL DEFAULT 'public',
    description TEXT NOT NULL DEFAULT '',
    owner_id    TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ,
    CONSTRAINT channels_type_check CHECK (type IN ('public', 'private', 'admin')),
    CONSTRAINT channels_name_format CHECK (name ~ '^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$')
);

CREATE UNIQUE INDEX IF NOT EXISTS channels_name_unique ON channels (lower(name));

CREATE TABLE IF NOT EXISTS channel_memberships (
    channel_id  TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    player_id   TEXT NOT NULL,
    role        TEXT NOT NULL DEFAULT 'member',
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    muted_until TIMESTAMPTZ,
    banned      BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (channel_id, player_id),
    CONSTRAINT membership_role_check CHECK (role IN ('owner', 'op', 'member'))
);

CREATE TABLE IF NOT EXISTS channel_gags (
    channel_id   TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    character_id TEXT NOT NULL,
    gagged       BOOLEAN NOT NULL DEFAULT true,
    PRIMARY KEY (channel_id, character_id)
);

CREATE TABLE IF NOT EXISTS channel_messages (
    id          TEXT PRIMARY KEY,
    channel_id  TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    author_id   TEXT NOT NULL,
    author_name TEXT NOT NULL,
    message     TEXT NOT NULL,
    event_type  TEXT NOT NULL DEFAULT 'channel_say',
    source      TEXT NOT NULL DEFAULT 'game',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_channel_messages_history
    ON channel_messages (channel_id, created_at DESC);
```

- [ ] **Step 3: Create down migration**

```sql
-- plugins/core-channels/migrations/000001_channels.down.sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP TABLE IF EXISTS channel_messages;
DROP TABLE IF EXISTS channel_gags;
DROP TABLE IF EXISTS channel_memberships;
DROP TABLE IF EXISTS channels;
```

- [ ] **Step 4: Verify build**

Run: `task build`

Expected: Build succeeds (go module sees the new files).

- [ ] **Step 5: Commit**

```text
feat(channels): add binary plugin manifest and migrations

Plugin manifest (type: binary, storage: postgres) with channel
command declarations. Schema-isolated migrations for channels,
memberships, gags, and message history tables. No FK references
to core tables per schema isolation design.
```

---

## Task 5: Entity Types and Validation

**Bead:** `holomush-0sc.3` (Phase 10.1)

**Files:**

- Create: `plugins/core-channels/types.go`
- Create: `plugins/core-channels/types_test.go`

- [ ] **Step 1: Write failing tests**

```go
// plugins/core-channels/types_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChannelCreatesValidPublicChannel(t *testing.T) {
	ch, err := newChannel("General", channelTypePublic, "General discussion", "owner-1")
	require.NoError(t, err)
	assert.Equal(t, "General", ch.Name)
	assert.Equal(t, channelTypePublic, ch.Type)
	assert.Equal(t, "General discussion", ch.Description)
	assert.Equal(t, "owner-1", ch.OwnerID)
	assert.NotEmpty(t, ch.ID)
	assert.NotZero(t, ch.CreatedAt)
	assert.Nil(t, ch.ArchivedAt)
}

func TestValidateChannel(t *testing.T) {
	t.Run("accepts valid channel names", func(t *testing.T) {
		names := []string{"Public", "a", "test-channel", "RP_General", "a1234567890123456789012345678901"}
		for _, name := range names {
			ch, err := newChannel(name, channelTypePublic, "", "owner-1")
			require.NoError(t, err, "name=%q", name)
			assert.Equal(t, name, ch.Name)
		}
	})

	t.Run("rejects invalid channel names", func(t *testing.T) {
		cases := []struct {
			name  string
			input string
		}{
			{"empty name", ""},
			{"starts with hyphen", "-bad"},
			{"starts with underscore", "_bad"},
			{"contains spaces", "has space"},
			{"too long", "a12345678901234567890123456789012"},
			{"special characters", "bad@name"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := newChannel(tc.input, channelTypePublic, "", "owner-1")
				require.Error(t, err)
			})
		}
	})

	t.Run("rejects invalid channel type", func(t *testing.T) {
		_, err := newChannel("test", channelType("bogus"), "", "owner-1")
		require.Error(t, err)
	})

	t.Run("rejects empty owner ID", func(t *testing.T) {
		_, err := newChannel("test", channelTypePublic, "", "")
		require.Error(t, err)
	})
}

func TestChannelIsArchived(t *testing.T) {
	ch, err := newChannel("test", channelTypePublic, "", "owner-1")
	require.NoError(t, err)
	assert.False(t, ch.isArchived())
}

func TestMembershipIsMuted(t *testing.T) {
	m := &membershipRow{Role: roleMember}

	t.Run("returns false when not muted", func(t *testing.T) {
		assert.False(t, m.isMuted())
	})

	t.Run("returns true when muted until future", func(t *testing.T) {
		future := time.Now().Add(time.Hour)
		m.MutedUntil = &future
		assert.True(t, m.isMuted())
	})

	t.Run("returns false when mute has expired", func(t *testing.T) {
		past := time.Now().Add(-time.Hour)
		m.MutedUntil = &past
		assert.False(t, m.isMuted())
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run "TestNewChannel|TestValidateChannel|TestChannel|TestMembership" ./plugins/core-channels/`

Expected: Compilation error — types not defined.

- [ ] **Step 3: Create types.go**

```go
// plugins/core-channels/types.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"time"

	"github.com/oklog/ulid/v2"
)

// channelType represents the access model for a channel.
type channelType string

const (
	channelTypePublic  channelType = "public"
	channelTypePrivate channelType = "private"
	channelTypeAdmin   channelType = "admin"
)

var validChannelTypes = map[channelType]bool{
	channelTypePublic:  true,
	channelTypePrivate: true,
	channelTypeAdmin:   true,
}

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`)

const (
	maxNameLength           = 32
	defaultMaxMessageLength = 4096
	defaultHistoryCount     = 20
	maxHistoryCount         = 500
	maxMemberships          = 50
)

// Member roles.
const (
	roleMember = "member"
	roleOp     = "op"
	roleOwner  = "owner"
)

// channelRow represents a channel record in the database.
type channelRow struct {
	ID          string
	Name        string
	Type        channelType
	Description string
	OwnerID     string
	CreatedAt   time.Time
	ArchivedAt  *time.Time
}

func newChannel(name string, ct channelType, description, ownerID string) (*channelRow, error) {
	ch := &channelRow{
		ID:          ulid.MustNew(ulid.Now(), rand.Reader).String(),
		Name:        name,
		Type:        ct,
		Description: description,
		OwnerID:     ownerID,
		CreatedAt:   time.Now().UTC(),
	}
	if err := ch.validate(); err != nil {
		return nil, err
	}
	return ch, nil
}

func (c *channelRow) validate() error {
	if c.Name == "" {
		return fmt.Errorf("channel name is required")
	}
	if !namePattern.MatchString(c.Name) {
		return fmt.Errorf("channel name must match pattern: starts with alphanumeric, 1-%d chars", maxNameLength)
	}
	if !validChannelTypes[c.Type] {
		return fmt.Errorf("invalid channel type %q", c.Type)
	}
	if c.OwnerID == "" {
		return fmt.Errorf("owner ID is required")
	}
	return nil
}

func (c *channelRow) isArchived() bool {
	return c.ArchivedAt != nil
}

// streamName returns the event stream for this channel.
func (c *channelRow) streamName() string {
	return "channel:" + c.ID
}

// membershipRow represents a player's membership in a channel.
type membershipRow struct {
	ChannelID  string
	PlayerID   string
	Role       string
	JoinedAt   time.Time
	MutedUntil *time.Time
	Banned     bool
}

func (m *membershipRow) isMuted() bool {
	if m.MutedUntil == nil {
		return false
	}
	return m.MutedUntil.After(time.Now())
}

// gagRow represents a per-character channel gag.
type gagRow struct {
	ChannelID   string
	CharacterID string
	Gagged      bool
}

// messageRow represents a stored channel message for history.
type messageRow struct {
	ID         string
	ChannelID  string
	AuthorID   string
	AuthorName string
	Message    string
	EventType  string
	Source     string
	CreatedAt  time.Time
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- -run "TestNewChannel|TestValidateChannel|TestChannel|TestMembership" ./plugins/core-channels/`

Expected: All tests pass.

- [ ] **Step 5: Commit**

```text
feat(channels): add entity types and validation

Channel, membership, gag, and message row types with validation.
Private to the plugin binary (package main, unexported types).
```

---

## Task 6: Channel Store

**Bead:** `holomush-0sc.3` (Phase 10.1)

**Files:**

- Create: `plugins/core-channels/store.go`

- [ ] **Step 1: Create store with all DB operations**

```go
// plugins/core-channels/store.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/plugin/storage"
)

//go:embed migrations/*.up.sql
var migrationsFS embed.FS

// channelStore provides PostgreSQL persistence for the channels plugin.
type channelStore struct {
	pool *pgxpool.Pool
}

// newChannelStore connects to PostgreSQL and runs migrations.
func newChannelStore(ctx context.Context, connString string) (*channelStore, error) {
	pool, err := storage.Connect(ctx, connString)
	if err != nil {
		return nil, oops.Code("CHANNEL_STORE_INIT_FAILED").Wrap(err)
	}

	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		pool.Close()
		return nil, oops.Code("CHANNEL_STORE_INIT_FAILED").Wrap(err)
	}
	if err := storage.RunMigrationsFS(ctx, pool, sub); err != nil {
		pool.Close()
		return nil, err
	}

	return &channelStore{pool: pool}, nil
}

func (s *channelStore) close() { s.pool.Close() }

// --- Channel CRUD ---

func (s *channelStore) createChannel(ctx context.Context, ch *channelRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO channels (id, name, type, description, owner_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		ch.ID, ch.Name, string(ch.Type), ch.Description, ch.OwnerID, ch.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "channels_name_unique") {
			return oops.Code("CHANNEL_DUPLICATE_NAME").With("name", ch.Name).Errorf("channel name %q already in use", ch.Name)
		}
		return oops.Code("CHANNEL_CREATE_FAILED").With("name", ch.Name).Wrap(err)
	}
	return nil
}

func (s *channelStore) getChannel(ctx context.Context, id string) (*channelRow, error) {
	row := &channelRow{}
	var typeStr string
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, type, description, owner_id, created_at, archived_at
		FROM channels WHERE id = $1`, id,
	).Scan(&row.ID, &row.Name, &typeStr, &row.Description, &row.OwnerID, &row.CreatedAt, &row.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("CHANNEL_NOT_FOUND").With("id", id).Errorf("channel not found")
	}
	if err != nil {
		return nil, oops.Code("CHANNEL_GET_FAILED").With("id", id).Wrap(err)
	}
	row.Type = channelType(typeStr)
	return row, nil
}

func (s *channelStore) getChannelByName(ctx context.Context, name string) (*channelRow, error) {
	row := &channelRow{}
	var typeStr string
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, type, description, owner_id, created_at, archived_at
		FROM channels WHERE lower(name) = lower($1)`, name,
	).Scan(&row.ID, &row.Name, &typeStr, &row.Description, &row.OwnerID, &row.CreatedAt, &row.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("CHANNEL_NOT_FOUND").With("name", name).Errorf("channel not found")
	}
	if err != nil {
		return nil, oops.Code("CHANNEL_GET_FAILED").With("name", name).Wrap(err)
	}
	row.Type = channelType(typeStr)
	return row, nil
}

func (s *channelStore) listChannels(ctx context.Context, includeArchived bool) ([]*channelRow, error) {
	query := `SELECT id, name, type, description, owner_id, created_at, archived_at FROM channels`
	if !includeArchived {
		query += ` WHERE archived_at IS NULL`
	}
	query += ` ORDER BY name`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, oops.Code("CHANNEL_LIST_FAILED").Wrap(err)
	}
	defer rows.Close()

	var channels []*channelRow
	for rows.Next() {
		ch := &channelRow{}
		var typeStr string
		if scanErr := rows.Scan(&ch.ID, &ch.Name, &typeStr, &ch.Description, &ch.OwnerID, &ch.CreatedAt, &ch.ArchivedAt); scanErr != nil {
			return nil, oops.Code("CHANNEL_LIST_FAILED").Wrap(scanErr)
		}
		ch.Type = channelType(typeStr)
		channels = append(channels, ch)
	}
	if rows.Err() != nil {
		return nil, oops.Code("CHANNEL_LIST_FAILED").Wrap(rows.Err())
	}
	return channels, nil
}

func (s *channelStore) archiveChannel(ctx context.Context, id string) error {
	now := time.Now().UTC()
	tag, err := s.pool.Exec(ctx, `
		UPDATE channels SET archived_at = $2 WHERE id = $1 AND archived_at IS NULL`,
		id, now)
	if err != nil {
		return oops.Code("CHANNEL_ARCHIVE_FAILED").With("id", id).Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("CHANNEL_NOT_FOUND").With("id", id).Errorf("channel not found")
	}
	return nil
}

// --- Membership ---

func (s *channelStore) addMembership(ctx context.Context, m *membershipRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO channel_memberships (channel_id, player_id, role, joined_at)
		VALUES ($1, $2, $3, $4)`,
		m.ChannelID, m.PlayerID, m.Role, m.JoinedAt)
	if err != nil {
		return oops.Code("MEMBERSHIP_ADD_FAILED").
			With("channel_id", m.ChannelID).With("player_id", m.PlayerID).Wrap(err)
	}
	return nil
}

func (s *channelStore) removeMembership(ctx context.Context, channelID, playerID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM channel_memberships WHERE channel_id = $1 AND player_id = $2`,
		channelID, playerID)
	if err != nil {
		return oops.Code("MEMBERSHIP_REMOVE_FAILED").Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("MEMBERSHIP_NOT_FOUND").Errorf("not a member of this channel")
	}
	return nil
}

func (s *channelStore) getMembership(ctx context.Context, channelID, playerID string) (*membershipRow, error) {
	m := &membershipRow{}
	err := s.pool.QueryRow(ctx, `
		SELECT channel_id, player_id, role, joined_at, muted_until, banned
		FROM channel_memberships WHERE channel_id = $1 AND player_id = $2`,
		channelID, playerID,
	).Scan(&m.ChannelID, &m.PlayerID, &m.Role, &m.JoinedAt, &m.MutedUntil, &m.Banned)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("MEMBERSHIP_NOT_FOUND").Errorf("not a member of this channel")
	}
	if err != nil {
		return nil, oops.Code("MEMBERSHIP_GET_FAILED").Wrap(err)
	}
	return m, nil
}

func (s *channelStore) listMembersByChannel(ctx context.Context, channelID string) ([]*membershipRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT channel_id, player_id, role, joined_at, muted_until, banned
		FROM channel_memberships WHERE channel_id = $1 ORDER BY joined_at`,
		channelID)
	if err != nil {
		return nil, oops.Code("MEMBERSHIP_LIST_FAILED").Wrap(err)
	}
	defer rows.Close()

	var memberships []*membershipRow
	for rows.Next() {
		m := &membershipRow{}
		if scanErr := rows.Scan(&m.ChannelID, &m.PlayerID, &m.Role, &m.JoinedAt, &m.MutedUntil, &m.Banned); scanErr != nil {
			return nil, oops.Code("MEMBERSHIP_LIST_FAILED").Wrap(scanErr)
		}
		memberships = append(memberships, m)
	}
	if rows.Err() != nil {
		return nil, oops.Code("MEMBERSHIP_LIST_FAILED").Wrap(rows.Err())
	}
	return memberships, nil
}

func (s *channelStore) countMembershipsByPlayer(ctx context.Context, playerID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM channel_memberships WHERE player_id = $1`,
		playerID).Scan(&count)
	if err != nil {
		return 0, oops.Code("MEMBERSHIP_COUNT_FAILED").Wrap(err)
	}
	return count, nil
}

// --- Gags ---

func (s *channelStore) setGag(ctx context.Context, channelID, characterID string, gagged bool) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO channel_gags (channel_id, character_id, gagged)
		VALUES ($1, $2, $3)
		ON CONFLICT (channel_id, character_id) DO UPDATE SET gagged = EXCLUDED.gagged`,
		channelID, characterID, gagged)
	if err != nil {
		return oops.Code("GAG_SET_FAILED").Wrap(err)
	}
	return nil
}

func (s *channelStore) getGag(ctx context.Context, channelID, characterID string) (bool, error) {
	var gagged bool
	err := s.pool.QueryRow(ctx, `
		SELECT gagged FROM channel_gags WHERE channel_id = $1 AND character_id = $2`,
		channelID, characterID).Scan(&gagged)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, oops.Code("GAG_GET_FAILED").Wrap(err)
	}
	return gagged, nil
}

// --- Messages ---

func (s *channelStore) insertMessage(ctx context.Context, msg *messageRow) error {
	if msg.ID == "" {
		msg.ID = ulid.MustNew(ulid.Now(), rand.Reader).String()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO channel_messages (id, channel_id, author_id, author_name, message, event_type, source, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		msg.ID, msg.ChannelID, msg.AuthorID, msg.AuthorName, msg.Message, msg.EventType, msg.Source, msg.CreatedAt)
	if err != nil {
		return oops.Code("MESSAGE_INSERT_FAILED").With("channel_id", msg.ChannelID).Wrap(err)
	}
	return nil
}

func (s *channelStore) getHistory(ctx context.Context, channelID string, count int, notBefore time.Time) ([]*messageRow, error) {
	var rows pgx.Rows
	var err error

	if notBefore.IsZero() {
		rows, err = s.pool.Query(ctx, `
			SELECT id, channel_id, author_id, author_name, message, event_type, source, created_at
			FROM channel_messages WHERE channel_id = $1
			ORDER BY created_at DESC LIMIT $2`,
			channelID, count)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT id, channel_id, author_id, author_name, message, event_type, source, created_at
			FROM channel_messages WHERE channel_id = $1 AND created_at > $2
			ORDER BY created_at DESC LIMIT $3`,
			channelID, notBefore, count)
	}
	if err != nil {
		return nil, oops.Code("HISTORY_QUERY_FAILED").With("channel_id", channelID).Wrap(err)
	}
	defer rows.Close()

	var messages []*messageRow
	for rows.Next() {
		m := &messageRow{}
		if scanErr := rows.Scan(&m.ID, &m.ChannelID, &m.AuthorID, &m.AuthorName, &m.Message, &m.EventType, &m.Source, &m.CreatedAt); scanErr != nil {
			return nil, oops.Code("HISTORY_SCAN_FAILED").Wrap(scanErr)
		}
		messages = append(messages, m)
	}
	if rows.Err() != nil {
		return nil, oops.Code("HISTORY_QUERY_FAILED").Wrap(rows.Err())
	}

	// Reverse to chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}
```

- [ ] **Step 2: Run compilation check**

Run: `task test -- -run NONE ./plugins/core-channels/`

Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```text
feat(channels): implement channel store with schema-isolated persistence

All channel, membership, gag, and message history operations using
plugin storage SDK. Embedded migrations run on Init via
storage.RunMigrationsFS. No FK to core tables (schema isolation).
```

---

## Task 7: Plugin Entry Point and Bootstrap Seeding

**Bead:** `holomush-0sc.3` (Phase 10.1)

**Files:**

- Create: `plugins/core-channels/main.go`

- [ ] **Step 1: Create main.go**

```go
// plugins/core-channels/main.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// channelPlugin implements Handler and CommandHandler for the binary plugin.
type channelPlugin struct {
	store *channelStore
}

// HandleEvent is a no-op — the channel plugin does not subscribe to events.
func (p *channelPlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return nil, nil
}

// HandleCommand routes to subcommand handlers.
func (p *channelPlugin) HandleCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return handleCommand(ctx, p.store, req)
}

// Init connects to the schema-isolated database, runs migrations, and seeds
// default channels.
func (p *channelPlugin) Init(ctx context.Context, config *pluginv1.ServiceConfig) error {
	connStr := config.GetConnectionString()
	if connStr == "" {
		return oops.Code("CHANNEL_INIT_FAILED").Errorf("connection_string is required")
	}

	store, err := newChannelStore(ctx, connStr)
	if err != nil {
		return oops.Code("CHANNEL_INIT_FAILED").Wrap(err)
	}
	p.store = store

	seedDefaultChannels(ctx, store)

	slog.Info("core-channels plugin initialised", "storage", "postgres")
	return nil
}

// seedDefaultChannels creates the "Public" channel if it doesn't exist.
func seedDefaultChannels(ctx context.Context, store *channelStore) {
	seeds := []struct {
		name        string
		chanType    channelType
		description string
	}{
		{"Public", channelTypePublic, "General public discussion"},
	}

	for _, seed := range seeds {
		if _, err := store.getChannelByName(ctx, seed.name); err == nil {
			continue // Already exists
		}
		ch, err := newChannel(seed.name, seed.chanType, seed.description, "system")
		if err != nil {
			slog.Warn("failed to create seeded channel", "name", seed.name, "error", err)
			continue
		}
		if err := store.createChannel(ctx, ch); err != nil {
			slog.Warn("failed to seed channel", "name", seed.name, "error", err)
			continue
		}
		slog.Info("seeded channel", "name", ch.Name, "type", ch.Type)
	}
}

func main() {
	plugin := &channelPlugin{}
	pluginsdk.Serve(&pluginsdk.ServeConfig{Handler: plugin})
}
```

- [ ] **Step 2: Run compilation check**

Run: `task test -- -run NONE ./plugins/core-channels/`

Expected: Compiles (handleCommand not yet defined — will be added in next task).

Note: This will fail to compile until Task 8 creates handlers.go. If needed, add a temporary stub:

```go
func handleCommand(_ context.Context, _ *channelStore, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return pluginsdk.Errorf("not yet implemented"), nil
}
```

Remove the stub after Task 8.

- [ ] **Step 3: Commit**

```text
feat(channels): add binary plugin entry point with bootstrap seeding

Plugin Init connects to schema-isolated DB, runs migrations, and
seeds "Public" channel. HandleCommand delegates to handler router.
System-owned seeded channels (owner_id="system").
```

---

## Task 8: Command Handlers

**Bead:** `holomush-0sc.4` (Phase 10.2) + `holomush-0sc.7` (Phase 10.5)

**Files:**

- Create: `plugins/core-channels/handlers.go`

- [ ] **Step 1: Create handlers.go with all command handlers**

```go
// plugins/core-channels/handlers.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/holomush/holomush/internal/core"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func handleCommand(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	subcommand, subargs := splitSubcommand(req.Args)

	switch subcommand {
	case "create":
		return handleCreate(ctx, store, req, subargs)
	case "delete":
		return handleDelete(ctx, store, req, subargs)
	case "join":
		return handleJoin(ctx, store, req, subargs)
	case "leave":
		return handleLeave(ctx, store, req, subargs)
	case "list":
		return handleList(ctx, store, req)
	case "say":
		return handleSay(ctx, store, req, subargs)
	case "who":
		return handleWho(ctx, store, req, subargs)
	case "history":
		return handleHistory(ctx, store, req, subargs)
	case "gag":
		return handleGag(ctx, store, req, subargs)
	case "ungag":
		return handleUngag(ctx, store, req, subargs)
	case "":
		return pluginsdk.Errorf("Usage: channel <create|join|leave|list|say|who|history|gag|ungag>"), nil
	default:
		return pluginsdk.Errorf("Unknown channel subcommand: %s", subcommand), nil
	}
}

func handleCreate(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return pluginsdk.Errorf("Usage: channel create <name> [type]"), nil
	}

	name := parts[0]
	ct := channelTypePublic
	if len(parts) > 1 {
		ct = channelType(strings.ToLower(parts[1]))
	}

	ch, err := newChannel(name, ct, "", req.PlayerID)
	if err != nil {
		return pluginsdk.Errorf("Could not create channel: %s", err), nil
	}

	if err := store.createChannel(ctx, ch); err != nil {
		return pluginsdk.Errorf("Could not create channel: %s", err), nil
	}

	// Add creator as owner
	membership := &membershipRow{
		ChannelID: ch.ID,
		PlayerID:  req.PlayerID,
		Role:      roleOwner,
		JoinedAt:  time.Now().UTC(),
	}
	if err := store.addMembership(ctx, membership); err != nil {
		return pluginsdk.Errorf("Channel created but failed to set ownership: %s", err), nil
	}

	return pluginsdk.OK(fmt.Sprintf("Channel '%s' created (%s).", ch.Name, ch.Type)), nil
}

func handleDelete(ctx context.Context, store *channelStore, _ pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if args == "" {
		return pluginsdk.Errorf("Usage: channel delete <name>"), nil
	}
	ch, err := store.getChannelByName(ctx, args)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}
	if err := store.archiveChannel(ctx, ch.ID); err != nil {
		return pluginsdk.Errorf("Could not delete channel: %s", err), nil
	}
	return pluginsdk.OK(fmt.Sprintf("Channel '%s' archived.", ch.Name)), nil
}

func handleJoin(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if args == "" {
		return pluginsdk.Errorf("Usage: channel join <name>"), nil
	}

	ch, err := store.getChannelByName(ctx, args)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}
	if ch.isArchived() {
		return pluginsdk.Errorf("Channel not found."), nil
	}

	count, err := store.countMembershipsByPlayer(ctx, req.PlayerID)
	if err != nil {
		return pluginsdk.Errorf("Could not join channel: %s", err), nil
	}
	if count >= maxMemberships {
		return pluginsdk.Errorf("You have reached the maximum of %d channel memberships.", maxMemberships), nil
	}

	membership := &membershipRow{
		ChannelID: ch.ID,
		PlayerID:  req.PlayerID,
		Role:      roleMember,
		JoinedAt:  time.Now().UTC(),
	}
	if err := store.addMembership(ctx, membership); err != nil {
		return pluginsdk.Errorf("Could not join channel: %s", err), nil
	}

	payload, _ := json.Marshal(core.ChannelNotificationPayload{
		ChannelID:     ch.ID,
		ChannelName:   ch.Name,
		CharacterID:   req.CharacterID,
		CharacterName: req.CharacterName,
		PlayerID:      req.PlayerID,
	})

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("You have joined channel '%s'.", ch.Name),
		Events: []pluginsdk.EmitEvent{{
			Stream:  ch.streamName(),
			Type:    pluginsdk.EventType(core.EventTypeChannelJoin),
			Payload: string(payload),
		}},
	}, nil
}

func handleLeave(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if args == "" {
		return pluginsdk.Errorf("Usage: channel leave <name>"), nil
	}

	ch, err := store.getChannelByName(ctx, args)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}
	if err := store.removeMembership(ctx, ch.ID, req.PlayerID); err != nil {
		return pluginsdk.Errorf("Could not leave channel: %s", err), nil
	}

	payload, _ := json.Marshal(core.ChannelNotificationPayload{
		ChannelID:     ch.ID,
		ChannelName:   ch.Name,
		CharacterID:   req.CharacterID,
		CharacterName: req.CharacterName,
		PlayerID:      req.PlayerID,
	})

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: fmt.Sprintf("You have left channel '%s'.", ch.Name),
		Events: []pluginsdk.EmitEvent{{
			Stream:  ch.streamName(),
			Type:    pluginsdk.EventType(core.EventTypeChannelLeave),
			Payload: string(payload),
		}},
	}, nil
}

func handleList(ctx context.Context, store *channelStore, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	channels, err := store.listChannels(ctx, false)
	if err != nil {
		return pluginsdk.Errorf("Could not list channels: %s", err), nil
	}
	if len(channels) == 0 {
		return pluginsdk.OK("No channels available."), nil
	}

	var sb strings.Builder
	sb.WriteString("Available channels:\n")
	for _, ch := range channels {
		sb.WriteString(fmt.Sprintf("  %-20s %-8s %s\n", ch.Name, ch.Type, ch.Description))
	}
	return pluginsdk.OK(sb.String()), nil
}

func handleSay(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	channelName, message, err := parseChannelMessage(args)
	if err != nil {
		return pluginsdk.Errorf("Usage: channel say <name>=<message>"), nil
	}

	ch, err := store.getChannelByName(ctx, channelName)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}

	if _, memErr := store.getMembership(ctx, ch.ID, req.PlayerID); memErr != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}

	if len(message) > defaultMaxMessageLength {
		return pluginsdk.Errorf("Message too long (max %d characters).", defaultMaxMessageLength), nil
	}

	eventType := core.EventTypeChannelSay
	displayMessage := message
	if strings.HasPrefix(message, ":") {
		eventType = core.EventTypeChannelPose
		displayMessage = strings.TrimLeft(strings.TrimPrefix(message, ":"), " ")
	} else if strings.HasPrefix(message, ";") {
		eventType = core.EventTypeChannelPose
		displayMessage = strings.TrimPrefix(message, ";")
	}

	// Store message for history (dual-write)
	msg := &messageRow{
		ChannelID:  ch.ID,
		AuthorID:   req.CharacterID,
		AuthorName: req.CharacterName,
		Message:    displayMessage,
		EventType:  string(eventType),
		Source:     "game",
	}
	if insertErr := store.insertMessage(ctx, msg); insertErr != nil {
		return pluginsdk.Errorf("Could not send message: %s", insertErr), nil
	}

	payload, _ := json.Marshal(core.ChannelMessagePayload{
		ChannelID:     ch.ID,
		ChannelName:   ch.Name,
		CharacterID:   req.CharacterID,
		CharacterName: req.CharacterName,
		AuthorName:    req.CharacterName,
		Message:       displayMessage,
		Source:        "game",
	})

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Events: []pluginsdk.EmitEvent{{
			Stream:  ch.streamName(),
			Type:    pluginsdk.EventType(eventType),
			Payload: string(payload),
		}},
	}, nil
}

func handleWho(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if args == "" {
		return pluginsdk.Errorf("Usage: channel who <name>"), nil
	}

	ch, err := store.getChannelByName(ctx, args)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}
	if _, memErr := store.getMembership(ctx, ch.ID, req.PlayerID); memErr != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}

	members, err := store.listMembersByChannel(ctx, ch.ID)
	if err != nil {
		return pluginsdk.Errorf("Could not list members: %s", err), nil
	}
	if len(members) == 0 {
		return pluginsdk.OK(fmt.Sprintf("Channel '%s' has no members.", ch.Name)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Members of '%s' (%d):\n", ch.Name, len(members)))
	for _, m := range members {
		role := ""
		if m.Role == roleOwner {
			role = " (owner)"
		} else if m.Role == roleOp {
			role = " (op)"
		}
		sb.WriteString(fmt.Sprintf("  %s%s\n", m.PlayerID, role))
	}
	return pluginsdk.OK(sb.String()), nil
}

func handleHistory(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return pluginsdk.Errorf("Usage: channel history <name> [count]"), nil
	}

	channelName := parts[0]
	count := defaultHistoryCount
	if len(parts) > 1 {
		parsed, parseErr := strconv.Atoi(parts[1])
		if parseErr != nil || parsed < 1 {
			return pluginsdk.Errorf("Count must be a positive number."), nil
		}
		if parsed > maxHistoryCount {
			count = maxHistoryCount
		} else {
			count = parsed
		}
	}

	ch, err := store.getChannelByName(ctx, channelName)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}

	membership, memErr := store.getMembership(ctx, ch.ID, req.PlayerID)
	if memErr != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}

	messages, err := store.getHistory(ctx, ch.ID, count, membership.JoinedAt)
	if err != nil {
		return pluginsdk.Errorf("Could not retrieve history: %s", err), nil
	}
	if len(messages) == 0 {
		return pluginsdk.OK(fmt.Sprintf("No history for channel '%s'.", ch.Name)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("History for '%s' (last %d):\n", ch.Name, len(messages)))
	for _, m := range messages {
		ts := m.CreatedAt.Format("15:04")
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", ts, m.AuthorName, m.Message))
	}
	return pluginsdk.OK(sb.String()), nil
}

func handleGag(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if args == "" {
		return pluginsdk.Errorf("Usage: channel gag <name>"), nil
	}
	ch, err := store.getChannelByName(ctx, args)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}
	if err := store.setGag(ctx, ch.ID, req.CharacterID, true); err != nil {
		return pluginsdk.Errorf("Could not gag channel: %s", err), nil
	}
	return pluginsdk.OK(fmt.Sprintf("Channel '%s' gagged for this character.", ch.Name)), nil
}

func handleUngag(ctx context.Context, store *channelStore, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
	if args == "" {
		return pluginsdk.Errorf("Usage: channel ungag <name>"), nil
	}
	ch, err := store.getChannelByName(ctx, args)
	if err != nil {
		return pluginsdk.Errorf("Channel not found."), nil
	}
	if err := store.setGag(ctx, ch.ID, req.CharacterID, false); err != nil {
		return pluginsdk.Errorf("Could not ungag channel: %s", err), nil
	}
	return pluginsdk.OK(fmt.Sprintf("Channel '%s' ungagged for this character.", ch.Name)), nil
}

// --- Helpers ---

func splitSubcommand(args string) (string, string) {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return "", ""
	}
	idx := strings.IndexByte(trimmed, ' ')
	if idx == -1 {
		return trimmed, ""
	}
	return trimmed[:idx], strings.TrimSpace(trimmed[idx+1:])
}

func parseChannelMessage(args string) (channelName, message string, err error) {
	if args == "" {
		return "", "", fmt.Errorf("empty input")
	}
	if idx := strings.IndexByte(args, '='); idx > 0 {
		return args[:idx], strings.TrimSpace(args[idx+1:]), nil
	}
	idx := strings.IndexByte(args, ' ')
	if idx == -1 {
		return "", "", fmt.Errorf("no message provided")
	}
	return args[:idx], strings.TrimSpace(args[idx+1:]), nil
}
```

- [ ] **Step 2: Remove handleCommand stub from main.go** (if added in Task 7)

- [ ] **Step 3: Run compilation check**

Run: `task test -- -run NONE ./plugins/core-channels/`

Expected: Compiles successfully.

- [ ] **Step 4: Build the plugin binary**

Run: `task plugin:build -- core-channels`

Expected: Build succeeds, binary at `build/plugins/core-channels/`.

- [ ] **Step 5: Commit**

```text
feat(channels): implement all command handlers

create, delete (archive), join, leave, list, say (with pose
detection), who, history (from plugin message table with join-time
boundary), gag, ungag. Messages dual-written to plugin store
and event stream.
```

---

## Task 9: Build Integration and Full Test Pass

**Bead:** Cross-cutting

**Files:**

- Modify: `internal/store/migrate_integration_test.go` (if migration counts changed)
- Modify: any remaining test files with EventStore stubs

- [ ] **Step 1: Run full lint**

Run: `task lint`

Fix any lint issues in the new plugin code (revive comments, wrapcheck, etc).

- [ ] **Step 2: Run full unit tests**

Run: `task test`

Expected: All tests pass.

- [ ] **Step 3: Run full build including plugins**

Run: `task plugin:build-all`

Expected: core-channels binary built alongside core-scenes.

- [ ] **Step 4: Run pr-prep**

Run: `task pr-prep`

Expected: All checks pass (schema, license, lint, unit, integration, E2E).

- [ ] **Step 5: Commit any remaining fixes**

```text
fix(channels): resolve lint and test compatibility issues
```

---

## Post-Implementation Checklist

- [ ] `task pr-prep` passes with zero failures
- [ ] Update bead status: close `holomush-0sc.3`, `holomush-0sc.4`, `holomush-0sc.7`
- [ ] Close `holomush-0sc.2` (this plan, superseded)
- [ ] Close PR #191 (superseded by new PR)
- [ ] Create new PR from this branch

---

## Deferred

| Item | Bead | Why |
| --- | --- | --- |
| `=channelname` shorthand | `holomush-3ozp` | Requires plugin manifest alias declarations |
| Phase 10.3 — channel types/invite | `holomush-0sc.5` | Separate plan after foundation proven |
| Phase 10.4 — moderation | `holomush-0sc.6` | Depends on 10.3 |
| ChannelAttributeProvider | — | Requires channel plugin to provide a gRPC service |
| Session subscription wiring | `holomush-0sc.9` | Session lifecycle integration |
| Guest auto-join | `holomush-0sc.10` | Depends on session hooks |
| `internal/channel/` package cleanup | — | Delete old package from superseded plan if it was merged |

---

## Risk Register

| Risk | Mitigation |
| --- | --- |
| `channel who` shows player IDs, not names | Plugin can't query players table; future ChannelService proto can resolve names |
| Schema isolation prevents FK validation | Application-level validation via command dispatcher (character session is pre-validated) |
| Dual-write (store + events) not atomic | Acceptable for MUSH workloads; message in store is authoritative, event is best-effort |
| ABAC seed policies not evaluable yet | Plugin handles its own authorization; ABAC provides defense-in-depth when ChannelAttributeProvider lands |
| History `formatHistoryEvent` is basic | Full verb registry rendering is future polish |

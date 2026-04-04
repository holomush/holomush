<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Channels Architecture Design

**Status:** Draft
**Date:** 2026-04-03
**Epic:** 10 — Channels (`holomush-0sc`)
**Bead:** `holomush-0sc.1`

## Overview

Channels provide persistent, named communication streams that operate
independently of the spatial world model. A channel is a lightweight
coordination record — not a world entity — with player-level membership,
per-character visibility overrides, and ABAC-controlled access.

Channels integrate with the existing event store for message delivery and
history, the ABAC engine for authorization, and the verb registry for
rendering. The data model is designed to support future external bridging
(Discord, Slack) without requiring structural changes.

### Design Goals

- Reuse existing event store infrastructure for message delivery and persistence
- Player-level membership with per-character gag overrides
- Fine-grained ABAC policies supporting per-channel, per-player authorization
- Bridge-ready message payloads for future Discord/Slack integration (Epic 12)
- Clean separation from the world model — channels are not spatial entities

### Key Decisions

| Decision           | Choice                        | Rationale                                                              |
| ------------------ | ----------------------------- | ---------------------------------------------------------------------- |
| Entity model       | Lightweight (own repository)  | Channels are not spatial; don't need world model features              |
| Location echo      | No                            | Channel messages do not appear in location streams                     |
| Membership scope   | Player-level                  | Avoids per-alt join overhead; gag overrides handle per-character needs |
| Visibility control | Per-character gag             | Players can silence channels on specific characters                    |
| Creation access    | ABAC-gated (admin default)    | Operators grant `create:channel` via policy as needed                  |
| Channel types      | Public, private, admin        | No unlisted type; private covers invite-only use cases                 |
| History model      | Count-based tail read         | Distinct from cursor-based session replay                              |
| Message format     | Plain text (rich text future) | Rich text tracked separately (`holomush-vrzu`)                         |
| Search             | Future phase                  | Full-text search tracked as `holomush-0sc.8`                           |
| Event types        | `channel_say`/`channel_pose`  | Aligns with verb registry spec (separate speech/action rendering)      |
| Stream naming      | `channel:<channel_id>`        | ULID-based; survives renames. Supersedes comm extensibility sketch     |
| Command prefix     | `channel` (no symbol prefix)  | HoloMUSH does not use symbol prefixes for commands                     |
| Message shorthand  | `=channelname message`        | `=` prefix is unambiguous, one character, no collision                  |
| Guest access       | Auto-join seeded channels     | Guests see community activity; configurable via bootstrap              |

### Related Specs

- **Comm event extensibility:** `docs/specs/2026-03-28-comm-event-extensibility-design.md`
  — Defines verb registry, `channel_say`/`channel_pose` types, and rendering
  categories. This spec supersedes the channel storage sketch in that document
  (section "Channels as Event Streams" uses `channel:<name>` which is replaced
  by `channel:<channel_id>` here for rename safety).
- **ABAC core types:** `docs/specs/abac/01-core-types.md`
- **ABAC seed policies:** `docs/specs/abac/07-migration-seeds.md`

### Faction Channels

Player-level membership means all of a player's characters gain access
when the player joins. For faction-style channels where only specific
characters should have access (e.g., Vampire faction channel hidden from
a player's Werewolf alt), the gag override provides visibility control
but not authorization enforcement.

The solution is ABAC character attributes — not channel model changes.
When character attributes are available to the ABAC engine (via a future
`CharacterAttributeProvider`), operators can write policies like:

```text
permit(principal is character, action in ["emit", "read"], resource is channel)
when { resource.name == "Vampire-IC" && principal.faction == "vampire" };
```

This leverages the existing ABAC investment and requires no new channel
infrastructure. The `CharacterAttributeProvider` is already planned as
follow-up work from the guest auth system (PR #181).

Until character attributes are available, faction channels rely on the
social contract — players voluntarily gag faction channels on the wrong
alt. This is consistent with traditional MUSH behavior.

## Requirements

### Channel Entity

A channel MUST be represented as a record in a dedicated `channels` table, NOT
as a world model entity. Channels do not participate in the spatial graph
(no containment, exits, or entity properties).

```go
type Channel struct {
    ID          ulid.ULID
    Name        string
    Type        ChannelType
    Description string
    OwnerID     ulid.ULID
    CreatedAt   time.Time
    ArchivedAt  *time.Time
}
```

The `Channel` type MUST have a `Validate()` method and `NewChannel()`
constructor, consistent with existing entity patterns in `internal/world/`.

**Channel types:**

| Type      | Discoverable                  | Join Policy         | Default Audience |
| --------- | ----------------------------- | ------------------- | ---------------- |
| `public`  | Yes (listed)                  | Any player          | All members      |
| `private` | No (hidden from non-members)  | Invite or admin add | Invited members  |
| `admin`   | Yes (listed, join restricted) | Admin role required | Staff            |

A channel MUST have a unique, case-insensitive name. Names MUST be 1–32
characters, alphanumeric plus hyphens and underscores. Names MUST NOT start
with a hyphen or underscore. The `Validate()` method MUST enforce the regex
`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`.

A channel MUST have exactly one owner (a player). Ownership MAY be
transferred by an admin (see Phase 10.4 — Moderation).

Archiving (soft delete) MUST set `archived_at` and prevent new messages.
Archived channels MUST NOT appear in listings. Unarchiving MUST be possible
by an admin. The `channel delete` command performs an archive, not a hard
delete. Hard deletion is a future administrative operation.

### Seeded Channels

The bootstrap system MUST support seeding a configurable set of default
channels (e.g., `Public`, `Questions`). Seeded channels are created during
server bootstrap if they do not already exist.

Guest players MUST be auto-joined to all seeded channels on session
creation. Guests MAY send and receive messages on seeded channels. Guests
MUST NOT join other channels, create channels, or use moderation commands.

The set of seeded channels MUST be configurable via the bootstrap config
(same pattern as seeded settings/content). The default set SHOULD include
at least `Public`.

If a seeded channel has been archived, the bootstrap MUST skip it and
log a warning. It MUST NOT automatically unarchive — an admin archived
it intentionally.

**Known limitation:** Player-level membership means all of a player's
characters gain access to a channel when the player joins. For use cases
requiring character-level access control (e.g., faction-only channels), the
gag override provides visibility control but not authorization control. This
limitation is accepted for initial implementation and MAY be revisited if
faction channels become a requirement.

### Channel Repository

The channel repository MUST be a standalone interface, not part of
`WorldService`:

```go
type ChannelRepository interface {
    Create(ctx context.Context, channel *Channel) error
    Get(ctx context.Context, id ulid.ULID) (*Channel, error)
    GetByName(ctx context.Context, name string) (*Channel, error)
    List(ctx context.Context, filter ChannelFilter) ([]*Channel, error)
    Update(ctx context.Context, channel *Channel) error
    Archive(ctx context.Context, id ulid.ULID) error
    Unarchive(ctx context.Context, id ulid.ULID) error
}
```

`ChannelFilter` MUST support filtering by type and excluding archived
channels. It SHOULD support pagination.

### Membership Model

Membership MUST be tracked at the player level. A player joins a channel
once; all their characters receive messages by default.

```go
type ChannelMembership struct {
    ChannelID  ulid.ULID
    PlayerID   ulid.ULID
    Role       MemberRole
    JoinedAt   time.Time
    MutedUntil *time.Time
    Banned     bool
}
```

**Member roles:**

| Role     | Can Send           | Can Moderate                   | Can Delete Channel |
| -------- | ------------------ | ------------------------------ | ------------------ |
| `member` | Yes (unless muted) | No                             | No                 |
| `op`     | Yes                | Yes (mute, ban within channel) | No                 |
| `owner`  | Yes                | Yes                            | Via ABAC policy    |

Only channel owners and admins (via ABAC `admin` action) MAY use
`channel op` and `channel deop`. An operator MUST NOT be able to promote
themselves or others to `owner` role.

A player MUST NOT have more than one membership record per channel. When a
player leaves (or is kicked), the membership row is deleted. A subsequent
join creates a fresh row with a new `joined_at` timestamp. This ensures
the history boundary is always the most recent join time.

The membership repository MUST be a standalone interface:

```go
type MembershipRepository interface {
    Add(ctx context.Context, membership *ChannelMembership) error
    Remove(ctx context.Context, channelID, playerID ulid.ULID) error
    Get(ctx context.Context, channelID, playerID ulid.ULID) (*ChannelMembership, error)
    ListByChannel(ctx context.Context, channelID ulid.ULID) ([]*ChannelMembership, error)
    ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*ChannelMembership, error)
    UpdateRole(ctx context.Context, channelID, playerID ulid.ULID, role MemberRole) error
    SetMute(ctx context.Context, channelID, playerID ulid.ULID, until *time.Time) error
    SetBan(ctx context.Context, channelID, playerID ulid.ULID, banned bool) error
}
```

### Character Visibility Overrides

A character MAY gag (silence) a channel they have access to via their
player's membership. Gagging is a per-character UI preference — it does NOT
affect membership or authorization.

```go
type ChannelGag struct {
    ChannelID   ulid.ULID
    CharacterID ulid.ULID
    Gagged      bool
}
```

The gag repository MUST be a standalone interface:

```go
type GagRepository interface {
    Set(ctx context.Context, channelID, characterID ulid.ULID, gagged bool) error
    Get(ctx context.Context, channelID, characterID ulid.ULID) (bool, error)
    ListGaggedByCharacter(ctx context.Context, characterID ulid.ULID) ([]ulid.ULID, error)
}
```

`ListGaggedByCharacter` returns only the channel IDs that the character has
gagged (not all channels).

When a character enters the game, the system MUST:

1. Subscribe to the character's own stream first (to catch concurrent
   membership changes)
2. Look up the player's channel memberships
3. For each membership, check the character's gag override
4. Subscribe the character to ungagged channel streams

This order ensures that a `channel_join` event arriving during the lookup
window is not missed. Session reconnection MUST follow the same flow.

When a character gags/ungags a channel mid-session, the subscription MUST
be updated immediately.

**Subscription limits:** A player MUST NOT be a member of more than 50
channels simultaneously. The service layer MUST enforce this limit on join.

### Event Integration

Channel messages MUST use the existing event store with stream naming
convention `channel:<channel_id>` (ULID-based, not name-based). Using the
channel ID ensures stream identity survives channel renames.

The channel name for display MUST be carried in the event payload
(`channel_name` field), not derived from the stream name.

Channel messages MUST NOT echo to the speaker's location stream. Channel
communication is entirely separate from spatial communication.

#### Message Event Payload

```go
type ChannelMessagePayload struct {
    ChannelID     string `json:"channel_id"`
    ChannelName   string `json:"channel_name"`
    CharacterID   string `json:"character_id,omitempty"`
    CharacterName string `json:"character_name,omitempty"`
    AuthorName    string `json:"author_name"`
    Message       string `json:"message"`
    Source        string `json:"source"`
}
```

- `AuthorName` MUST always be set. For in-game messages, it equals
  `CharacterName`. For bridged messages (future), it is the external
  user's display name.
- `CharacterID` and `CharacterName` MUST be set for in-game messages.
  They MUST be omitted for externally bridged messages.
- `Source` MUST always be set. For in-game messages, it is `"game"`. For
  bridged messages (future), it is the service name (e.g., `"discord"`,
  `"slack"`). The service layer MUST reject messages where both
  `CharacterID` and `Source != "game"` are missing — every message is
  either from a character (validated by session) or from a bridge
  (validated by bridge configuration).
- `ChannelName` is a snapshot at emission time and MUST NOT be used for
  authorization decisions. For privacy-sensitive rendering (e.g., private
  channels), prefer a live lookup by `ChannelID`.
- `Message` MUST NOT exceed 4096 bytes by default. This limit MUST be
  configurable via server settings. The service layer MUST reject
  oversized messages before event emission.

#### Channel Event Types

The following event types MUST be defined, consistent with the verb registry
spec (`docs/specs/2026-03-28-comm-event-extensibility-design.md`):

| Event Type     | Category        | Format         | Description                          |
| -------------- | --------------- | -------------- | ------------------------------------ |
| `channel_say`  | `communication` | `speech`       | A speech message on a channel        |
| `channel_pose` | `communication` | `action`       | An action/pose on a channel          |
| `channel_join` | `communication` | `notification` | Player joined (system notification)  |
| `channel_leave`| `communication` | `notification` | Player left (system notification)    |
| `channel_mute` | `communication` | `notification` | Player muted by moderator            |
| `channel_ban`  | `communication` | `notification` | Player banned by moderator           |
| `channel_kick`   | `communication` | `notification` | Player removed by moderator          |
| `channel_rename` | `communication` | `notification` | Channel renamed (old + new name)     |

All channel events are emitted to the `channel:<channel_id>` stream so
all subscribers see them.

Join/leave event payloads carry `character_name` referring to the character
who executed the command (not all of the player's characters).

#### Verb Registry Integration

Channel event types MUST register with the verb registry. Channel messages
render in the `speech` or `action` category with a channel prefix:

```text
[Public] Sean says, "hello everyone"
[Public] Sean waves.
[Staff] Admin announces, "server restart in 5 minutes"
```

The channel prefix (e.g., `[Public]`) MUST be derived from
`metadata.channel` in the event payload, consistent with the verb
registry spec. The verb registry registration MUST include `channel`
as a metadata key.

Bridged messages MUST be visually distinguished from in-game messages.
The rendering layer MUST use the `source` field to differentiate:

```text
[Public] Sean (Discord) says, "hello from Discord"
```

### Rate Limiting

The service layer MUST enforce message rate limits:

| Limit                     | Default | Description                            |
| ------------------------- | ------- | -------------------------------------- |
| Per-player, per-channel   | 5/sec   | Messages per second on a single channel|
| Per-player, global        | 10/sec  | Messages per second across all channels|
| Channel creation, per-player | 5/hour | Channel creation rate (when granted)  |

Rate limit violations MUST return an error to the sender. They MUST NOT
result in automatic mute or ban.

### Channel History

Channel history is a **count-based tail read** from the channel's event
stream. It is distinct from cursor-based session replay.

The event store MUST support a tail query:

```go
ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time) ([]Event, error)
```

- `count` — maximum number of events to return, reading backward from the
  most recent. The service layer MUST enforce a maximum of 500.
- `notBefore` — do not return events with timestamps before this value.

This method SHOULD be added to the `EventStore` interface as it is useful
beyond channels (e.g., location arrival context).

#### History Requirements

| Requirement        | Description                                                                                             |
| ------------------ | ------------------------------------------------------------------------------------------------------- |
| Replay on join     | When a player joins a channel, they MUST see the last N messages (configurable per channel, default 20) |
| Join-time boundary | History MUST NOT include messages from before the player's `joined_at` timestamp                        |
| Scrollback command | `channel history <name> [count]` MUST return older messages on demand                                  |
| Count cap          | `count` parameter MUST be capped at 500 at the service layer                                            |
| Retention          | Each channel MUST have a configurable retention duration (default 30 days)                              |
| Admin override     | Admin channels MAY have extended or unlimited retention                                                 |
| Pruning            | A background process MUST prune events older than the retention window                                  |

#### Search (Future — `holomush-0sc.8`)

Full-text search across channel history is a separate phase. The spec is
documented here for context but is NOT in scope for initial implementation.

Requirements for the future search phase:

- Single-channel search: `channel search <name> <query>`
- Cross-channel search across all channels a player is a member of
- Author and date range filters
- PostgreSQL `tsvector`/`tsquery` indexing on message payloads
- Results MUST respect membership (search only channels you belong to)
- ABAC check on search capability
- Pagination for large result sets

### ABAC Integration

#### Resource Prefix

The `channel:` prefix MUST be added to the ABAC resource prefix table:

| Prefix     | DSL Type  | Example         |
| ---------- | --------- | --------------- |
| `channel:` | `channel` | `channel:01ABC` |

#### Code Changes Required

The following files MUST be updated to support channel resources:

| File | Change |
| --- | --- |
| `internal/access/prefix.go` | Add `ResourceChannel = "channel:"`, `ChannelResource()` helper, update `knownPrefixes` |
| `internal/command/types.go` | Add `"channel"` to `validResourceTypes` map |
| `internal/command/types.go` | Add `"join"`, `"leave"`, `"list"`, `"create"` to `validActions` map |

#### Channel Attribute Provider

A `ChannelAttributeProvider` MUST be implemented to resolve channel
attributes during policy evaluation:

| Attribute                       | Type   | Description                                                   |
| ------------------------------- | ------ | ------------------------------------------------------------- |
| `resource.name`                 | string | Channel name                                                  |
| `resource.type`                 | string | Channel type: "public", "private", "admin"                    |
| `resource.owner`                | string | Player ID of channel creator                                  |
| `resource.archived`             | bool   | Whether the channel is archived                               |
| `principal.channel_memberships` | list   | List of channel IDs the principal's player is a member of     |
| `principal.channel_banned`      | bool   | Whether the principal's player is banned from this channel    |
| `principal.channel_muted`       | bool   | Whether the principal's player is muted on this channel       |

The provider MUST follow the existing `AttributeProvider` interface and
register as a core provider (not a plugin provider). The membership, ban,
and mute attributes are resolved for the specific principal-channel pair
in the current `AccessRequest`.

**Caching:** The provider MUST cache player membership data with
invalidation on channel events (`channel_join`, `channel_leave`,
`channel_ban`, `channel_kick`). The invalidation mechanism SHOULD use
the same LISTEN/NOTIFY pattern as the ABAC policy cache. Cache entries
are keyed by player ID and invalidated when any membership change event
is received for that player.

#### Capabilities

| Operation      | Action   | Resource         | Description                        |
| -------------- | -------- | ---------------- | ---------------------------------- |
| Create channel | `create` | `channel:*`      | Create a new channel               |
| Delete channel | `delete` | `channel:<id>`   | Archive a channel                  |
| Join channel   | `join`   | `channel:<id>`   | Join as member                     |
| Leave channel  | `leave`  | `channel:<id>`   | Leave voluntarily                  |
| Send message   | `emit`   | `channel:<id>`   | Send a message                     |
| Read history   | `read`   | `channel:<id>`   | View channel history               |
| List channels  | `list`   | `channel:*`      | View available channels            |
| Moderate       | `admin`  | `channel:<id>`   | Mute, ban, kick, promote/demote    |

#### Seed Policies

```text
/ seed:channel-list (seed_version: 1)
// All players can list public and admin channels
permit(principal is character, action in ["list"], resource is channel)
when { resource.type == "public" || resource.type == "admin" };

/ seed:channel-join-public (seed_version: 1)
// All players can join public channels
permit(principal is character, action in ["join"], resource is channel)
when { resource.type == "public" };

/ seed:channel-member-actions (seed_version: 1)
// Members can send, read, and leave their channels
permit(principal is character, action in ["emit", "read", "leave"], resource is channel)
when { resource.id in principal.channel_memberships };

/ seed:channel-admin-create (seed_version: 1)
// Only admins can create channels by default
permit(principal is character, action in ["create"], resource is channel)
when { principal.role == "admin" };

/ seed:channel-admin-delete (seed_version: 1)
permit(principal is character, action in ["delete"], resource is channel)
when { principal.role == "admin" };

/ seed:channel-admin-moderate (seed_version: 1)
permit(principal is character, action in ["admin"], resource is channel)
when { principal.role == "admin" };

/ seed:channel-forbid-banned (seed_version: 1)
// Banned players cannot emit, read, or join
forbid(principal is character, action in ["emit", "read", "join"], resource is channel)
when { principal.channel_banned == true };

/ seed:channel-forbid-muted (seed_version: 1)
// Muted players cannot emit
forbid(principal is character, action in ["emit"], resource is channel)
when { principal.channel_muted == true };

/ seed:channel-forbid-archived (seed_version: 1)
// No one can emit or join archived channels
forbid(principal is character, action in ["emit", "join"], resource is channel)
when { resource.archived == true };

/ seed:channel-guest-seeded-only (seed_version: 1)
// Guests can list and join seeded public channels if they leave one.
// The membership check (principal.channel_memberships) in member-actions
// handles scoping — guests are only auto-joined to seeded channels.
permit(principal is character, action in ["list", "join"], resource is channel)
when { principal.role == "guest" && resource.type == "public" };

/ seed:channel-guest-forbid-create (seed_version: 1)
// Guests cannot create, delete, or moderate channels
forbid(principal is character, action in ["create", "delete", "admin"], resource is channel)
when { principal.role == "guest" };
```

**Per-channel moderation scoping** is achieved through operator-defined
policies using `resource.name`:

```text
/ example: Grant PlayerX moderation on Public and Newbie
permit(principal is character, action in ["admin"], resource is channel)
when { principal.id == "character:01ABC"
    && resource.name in ["Public", "Newbie"] };

/ example: All builders moderate RP- prefixed channels
permit(principal is character, action in ["admin"], resource is channel)
when { principal.role == "builder" && resource.name like "RP-*" };
```

No new ABAC features are required — the existing DSL, glob matching, and
attribute resolution support all channel authorization patterns.

**Admin full-access note:** The existing `seed:admin-full-access` policy
permits all actions on all resources for admin-role characters. This means
admins can access private channels, bypass bans, and read all history.
This is intentional for a MUSH platform where staff oversight is expected.
Players in private channels should understand that admins have full access.

#### Authorization Layering

Channel operations use a **two-layer authorization model**:

1. **ABAC layer** — Can this principal perform this action on this channel?
   (Policies, roles, attributes, membership, ban/mute status.)
2. **Service layer** — Business logic validation (e.g., channel exists, not
   archived, invite exists for private channels).

The ABAC layer now includes membership awareness via
`principal.channel_memberships`, so it provides defense-in-depth for
unauthorized access. The service layer handles business rules that are not
naturally expressed as ABAC policies (e.g., invite validation for private
channels, rate limiting).

For `join` operations on private channels, the service layer MUST verify
that an invite exists OR the caller has `admin` capability on the channel.

#### Error Response Uniformity

All operations on channels a player cannot see MUST return an identical
error: "channel not found." The service layer MUST NOT distinguish between
"does not exist" and "exists but you cannot see it" in any user-facing
response. This applies to `join`, `who`, `history`, `say`, and `set`.

### Plugin Structure

Channel commands MUST be implemented as a `core-channels` Go compiled
plugin (`type: core`), separate from `core-communication`. Channels have
their own entity model, repositories, and ABAC provider — too much
domain-specific logic for `core-communication`.

The plugin lives at `plugins/core-channels/` and follows the same
patterns as `plugins/core-communication/` (Go handlers, `plugin.yaml`
manifest, compiled into the binary).

### Commands

The `channel` command family uses multi-word command registration (e.g.,
`channel create`, `channel join`) consistent with the existing pattern
for `policy test`/`policy create`. Each subcommand is registered as a
separate entry in the command registry with its own capabilities.

HoloMUSH does not use symbol prefixes for commands. The command name is
`channel`, not `@channel` or `+channel`.

#### Phase 10.2 — Core Commands

| Command           | Syntax                          | Capabilities                 |
| ----------------- | ------------------------------- | ---------------------------- |
| `channel create` | `channel create <name> [type]` | `create:channel:*`           |
| `channel delete` | `channel delete <name>`        | `delete:channel:<id>`        |
| `channel join`   | `channel join <name>`          | `join:channel:<id>`          |
| `channel leave`  | `channel leave <name>`         | `leave:channel:<id>`         |
| `channel list`   | `channel list`                 | `list:channel:*`             |
| `channel say`    | `channel say <name>=<message>` | `emit:channel:<id>`          |
| `channel who`    | `channel who <name>`           | `read:channel:<id>`          |

`channel who` MUST require membership (or admin access) for private
channels.

**Message shorthand:** `=<channelname> <message>` MUST be available for
sending messages to channels. The `=` prefix follows the same say/pose
semantics as pages and other communication commands:

| Input                    | Event Type     | Rendered As                          |
| ------------------------ | -------------- | ------------------------------------ |
| `=public hello`          | `channel_say`  | `[Public] Sean says, "hello"`        |
| `=public :waves`         | `channel_pose` | `[Public] Sean waves`                |
| `=public ;'s hat falls`  | `channel_pose` | `[Public] Sean's hat falls` (no space)|

The default (no prefix) is `say`. The `:` and `;` prefixes trigger
`channel_pose` with the same `no_space` semantics as regular pose.

**Channel aliases:** A channel-specific alias command
(`channel alias <short>=<name>`) MUST be provided so users can create
shorthand for frequently used channels (e.g., `channel alias pub=Public`
allows `=pub hello`). Users MAY also use the general alias system for
channel shortcuts.

**Channel rename:** `channel rename <oldname>=<newname>` MUST be supported
(requires `admin:channel:<id>` capability). Since streams use
`channel:<channel_id>`, renames do not affect event delivery. All
subscribers see the new name on the next message. Channel aliases are
unaffected (they resolve by name at send time). A `channel_rename` event
MUST be emitted to the channel stream as a notification.

#### Phase 10.3 — Channel Types

| Command             | Syntax                                | Capabilities        |
| ------------------- | ------------------------------------- | ------------------- |
| `channel invite`  | `channel invite <name>=<player>`     | `admin:channel:<id>` |
| `channel set`     | `channel set <name>/<setting>=<val>` | `admin:channel:<id>` |
| `channel rename`  | `channel rename <name>=<newname>`    | `admin:channel:<id>` |
| `channel alias`   | `channel alias <short>=<name>`       | (no ABAC — personal) |

#### Phase 10.4 — Moderation

| Command             | Syntax                                      | Capabilities         |
| ------------------- | ------------------------------------------- | -------------------- |
| `channel mute`     | `channel mute <name>=<player>[/<duration>]` | `admin:channel:<id>` |
| `channel unmute`   | `channel unmute <name>=<player>`           | `admin:channel:<id>` |
| `channel ban`      | `channel ban <name>=<player>`              | `admin:channel:<id>` |
| `channel unban`    | `channel unban <name>=<player>`            | `admin:channel:<id>` |
| `channel kick`     | `channel kick <name>=<player>`             | `admin:channel:<id>` |
| `channel op`       | `channel op <name>=<player>`               | `admin:channel:<id>` |
| `channel deop`     | `channel deop <name>=<player>`             | `admin:channel:<id>` |
| `channel transfer` | `channel transfer <name>=<player>`         | `admin:channel:<id>` |

`channel transfer` changes channel ownership. Only admins MAY transfer
ownership.

**Mute duration format:** Durations use `h`, `m`, `s` suffixes
(e.g., `1h`, `30m`, `1h30m`, `24h`). If no duration is specified, the
default is `24h`. Combined forms like `1h30m` MUST be supported.

#### Phase 10.5 — History

| Command             | Syntax                           | Capabilities        |
| ------------------- | -------------------------------- | ------------------- |
| `channel history`  | `channel history <name> [count]`| `read:channel:<id>` |

#### Character Gag Control

| Command           | Syntax                | Capabilities |
| ----------------- | --------------------- | ------------ |
| `channel gag`    | `channel gag <name>` | (no ABAC — client preference) |
| `channel ungag`  | `channel ungag <name>`| (no ABAC — client preference) |

### Database Schema

All ID columns use `TEXT` to store ULIDs, consistent with the existing
schema convention in `internal/store/migrations/`.

#### channels

```sql
CREATE TABLE IF NOT EXISTS channels (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL DEFAULT 'public',
    description TEXT NOT NULL DEFAULT '',
    owner_id    TEXT NOT NULL REFERENCES players(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ,
    CONSTRAINT channels_name_unique UNIQUE (lower(name)),
    CONSTRAINT channels_type_check CHECK (type IN ('public', 'private', 'admin')),
    CONSTRAINT channels_name_format CHECK (name ~ '^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$')
);
```

#### channel\_memberships

```sql
CREATE TABLE IF NOT EXISTS channel_memberships (
    channel_id  TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    player_id   TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    role        TEXT NOT NULL DEFAULT 'member',
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    muted_until TIMESTAMPTZ,
    banned      BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (channel_id, player_id),
    CONSTRAINT membership_role_check CHECK (role IN ('owner', 'op', 'member'))
);
```

#### channel\_gags

```sql
CREATE TABLE IF NOT EXISTS channel_gags (
    channel_id   TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    character_id TEXT NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    gagged       BOOLEAN NOT NULL DEFAULT true,
    PRIMARY KEY (channel_id, character_id)
);
```

### Bridge Readiness (Future — Epic 12)

The channel model MUST NOT preclude external bridging. The following design
constraints ensure bridge compatibility:

1. **Stable identity** — Channel ULIDs provide stable mapping targets for
   external channel IDs.
2. **Bridge-friendly payloads** — `ChannelMessagePayload` carries
   `author_name` and `source` independently of character fields, supporting
   non-game authors with explicit origin tracking.
3. **Nullable character fields** — `character_id` and `character_name` are
   omitted for bridged messages, allowing external authors.
4. **Source authentication** — Messages MUST be either from a character
   (validated by session, `source: "game"`) or from a bridge (validated by
   bridge configuration, `source: "<service>"`). Messages with missing
   character ID AND non-bridge source MUST be rejected as malformed.
5. **Visual distinction** — Bridged messages MUST be rendered differently
   from in-game messages (e.g., `[Public] Sean (Discord) says, "..."`).

A future `channel_bridges` table (NOT implemented in Epic 10) would map
channels to external services:

```sql
-- Future (Epic 12) — NOT part of this spec
CREATE TABLE IF NOT EXISTS channel_bridges (
    channel_id  TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    service     TEXT NOT NULL,
    external_id TEXT NOT NULL,
    direction   TEXT NOT NULL,
    config      JSONB DEFAULT '{}',
    PRIMARY KEY (channel_id, service)
);
```

## Non-Goals

- **World model integration** — Channels are not locations, objects, or
  scenes. They do not participate in the spatial graph.
- **Location echo** — Channel messages do not appear in location streams.
- **Unlisted channel type** — Private channels cover invite-only use cases.
- **Rich text rendering** — Tracked separately as `holomush-vrzu`.
- **Full-text search** — Tracked separately as `holomush-0sc.8`.
- **External bridging implementation** — Epic 12. This spec ensures
  compatibility only.
- **Web client UI for channel management** — Web client changes for
  channels are implementation concerns, not architectural decisions.
- **Moderated mode / voice role** — Not needed for initial implementation.
  The schema supports adding new role values if moderated channels become
  a requirement.

## Phased Delivery

| Phase  | Bead             | Deliverable    | Key Components                          |
| ------ | ---------------- | -------------- | --------------------------------------- |
| 10.1   | `holomush-0sc.3` | Channel schema | Channel entity, repository, migrations  |
| 10.2   | `holomush-0sc.4` | Commands       | join/leave/list/say/who, `=name` alias  |
| 10.3   | `holomush-0sc.5` | Channel types  | Public/private/admin, invite system     |
| 10.4   | `holomush-0sc.6` | Moderation     | Mute, ban, kick, op/deop, transfer      |
| 10.5   | `holomush-0sc.7` | History        | Tail-based replay, retention, pruning   |
| Future | `holomush-0sc.8` | Search         | Full-text search (P3)                   |
| Future | `holomush-vrzu`  | Rich text      | Markdown + emoji for all messages       |
| Future | Epic 12          | Bridging       | Discord/Slack integration               |

## Dependencies

- **Event store** — `EventStore` interface for message delivery and history.
  `ReplayTail` method MUST be added.
- **ABAC engine** — `channel:` resource prefix, `ChannelAttributeProvider`
  with cached membership/ban/mute attributes, LISTEN/NOTIFY invalidation.
- **Verb registry** — `channel_say` and `channel_pose` types registered for
  category-based rendering (per comm event extensibility spec).
- **Command system** — Multi-word `channel` subcommand registration with
  per-subcommand capabilities. `validActions` and `validResourceTypes` maps
  updated. `=` prefix parser support for message shorthand.
- **Access package** — `ResourceChannel` constant and `ChannelResource()`
  helper in `internal/access/prefix.go`.
- **Bootstrap** — Seeded channel configuration for guest auto-join.

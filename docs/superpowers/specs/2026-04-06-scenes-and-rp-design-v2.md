<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Scenes & RP Architecture Design (v2)

**Epic:** 9 (holomush-5rh) | **Status:** Draft | **Date:** 2026-04-06
**Supersedes:** `2026-04-04-scenes-and-rp-design.md` (v1, available via
`jj file show -r scene-system docs/superpowers/specs/2026-04-04-scenes-and-rp-design.md`)

## Overview

Scenes are the primary unit of structured roleplay in HoloMUSH. They provide
isolated, async-capable RP spaces that can exist independently or overlay grid
locations. Scenes support both synchronous (live) and asynchronous (play-by-post)
interaction patterns, allowing characters to participate in multiple scenes
simultaneously while maintaining grid presence.

This v2 spec covers the same Epic 9 product scope as v1 but reflects the
plugin architecture rework that landed in PRs #192 (binary plugins +
GRPCBroker), #194 (manifest alias declarations), and #195 (plugin ABAC trust
boundary). The product design (D1-D10) is unchanged. The architecture by which
that product is built is fundamentally different: scenes are now defined and
served by a binary plugin (`core-scenes`), not by the server core.

## Changes from v1

| Area | v1 (server-centric) | v2 (plugin-owned) |
|------|---------------------|-------------------|
| **Code location** | `internal/world/scene*.go` and `internal/grpc/scene_service.go` | `plugins/core-scenes/` (binary plugin process) |
| **Persistence** | Tables in main database, migration `000003_scenes.up.sql` | Plugin-owned Postgres schema `plugin_core_scenes`, plugin-embedded migrations |
| **gRPC service** | Server-implemented, exposed by main `cmd/holomush` process | Plugin-implemented, served by plugin process, host-proxied |
| **ABAC enforcement** | `AccessPolicyEngine` calls in `SceneService` Go code | Plugin manifest declares Cedar-style policies; host evaluates them; plugin's `AttributeResolverService` resolves scene attributes on demand |
| **Scene resource type** | Server-owned core type, in `ProtectedResourceTypes` | Plugin-owned (`resource_types: [scene]`); removed from `ProtectedResourceTypes` |
| **Service contract** | Direct `SceneService` interface used by adapter | Proto contract `holomush.scene.v1.SceneService` is the only boundary |
| **Migration plan** | Migrate `LocationTypeScene` rows from `locations` table | None — no historical scene data exists in production; plugin starts with empty schema |
| **Cross-component access** | `ServiceProxy` extensions for plugin → scene calls | Removed (PR #192). Cross-plugin communication is via proto contracts declared in `requires`/`provides` |
| **Observability** | Inherited from server-wide tracing/metrics setup | Plugin emits its own spans, metrics, and structured logs at all boundaries; correlated to host via context propagation |

The unchanged sections are noted at the start of each section. Sections 5
(Access Control), 7 (gRPC API), and 9 (replaced with Plugin Architecture) are
substantively rewritten. Section 10 (Observability) is new. Section 8 (Forum
View) is preserved as background but explicitly **out of scope** for this
epic — see "Out of scope" below.

## RFC2119 Keywords

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are used per RFC2119.

## Design Decisions

These are unchanged from v1.

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | Scenes are first-class domain entities, not Location variants | Clean lifecycle, no metaphor leakage for off-grid scenes |
| D2 | Async-first with live support | Distributed timezone player base needs RP that spans hours/days |
| D3 | Characters can be in multiple scenes + grid simultaneously | Async participation requires parallel memberships |
| D4 | Window-per-scene routing (web), focus switching (terminal) | Web client renders threads; telnet multiplexes via commands |
| D5 | Dual IC/OOC event streams per scene | Clean log separation; publish without OOC chatter |
| D6 | Unanimous consent for log publishing | Strong privacy default; no admin override |
| D7 | Pose order tracked but unenforced | Guide, not gate; supports strict and NPR modes |
| D8 | Scene board for discovery, sidebar for active scenes | Separates browsing from participation |
| D9 | Forum view as alternate rendering of scene data | Same APIs, different UX; no new server concepts |
| D10 | Membership and focus are separate concepts | Membership is persistent on character; focus is ephemeral on connection |

## Out of Scope

- **Forum view (D9)** is deferred to a separate cross-cutting epic for "Forum
  rendering" that will cover both scenes and OOC comms. The scene plugin
  exposes the data needed for forum rendering via its standard gRPC contract;
  no scene-side work is required when forum rendering is built.

## 1. Domain Model

*(Unchanged from v1 in product semantics. The Go types live in
`plugins/core-scenes/types.go` rather than `internal/world/`. Field nullability
and naming match v1.)*

### 1.1 Scene Entity

Scene is a first-class entity owned by the `core-scenes` plugin.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| ID | ULID | Yes | Primary identifier |
| Title | string | Yes | Scene name (e.g., "A Decades-Crossed Meeting") |
| Description | string | No | Custom description; if nil and LocationID set, falls back to location description |
| LocationID | *ULID | No | Grid location this scene is attached to; nil = off-grid |
| OwnerID | ULID | Yes | Character who created the scene |
| State | SceneState | Yes | `active`, `paused`, `ended`, `archived` |
| PoseOrderMode | PoseOrderMode | Yes | `strict`, `3pr`, `5pr`, `free` |
| ContentWarnings | []string | No | System-defined warning categories |
| Tags | []string | No | Freeform tags for discovery |
| TemplateID | *ULID | No | Source template if created from one |
| Visibility | SceneVisibility | Yes | `open`, `private` |
| IdleTimeout | *duration | No | Inactivity threshold before auto-pause; nil = game default |
| CreatedAt | time.Time | Yes | |
| EndedAt | *time.Time | No | Set when owner ends scene |
| ArchivedAt | *time.Time | No | Set after publish vote resolves |

### 1.2 Scene States

```text
active --> paused    (configurable inactivity timeout OR owner command)
paused --> active    (any member poses or runs scene resume)
active --> ended     (owner runs scene end)
paused --> ended     (owner runs scene end)
ended  --> archived  (after publish vote resolves or times out)
```

A scene MUST NOT transition backward (e.g., ended → active). State transition
logic lives in `plugins/core-scenes/lifecycle.go` and MUST be enforced both at
the service layer (rejecting invalid RPC requests) and at the store layer
(database constraints or guarded UPDATE statements).

### 1.3 Scene Participant

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| SceneID | ULID | Yes | |
| CharacterID | ULID | Yes | |
| Role | ParticipantRole | Yes | `owner`, `member`, `invited` |
| OriginLocationID | *ULID | No | Grid location when they joined; nil if character was off-grid |
| JoinedAt | time.Time | Yes | |
| PublishVote | *bool | No | nil = not voted, true/false = vote cast |

Scene membership MUST persist until the participant explicitly leaves or the
scene ends. The system MUST NOT idle out participants for inactivity.

### 1.4 Scene Template

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| ID | ULID | Yes | |
| OwnerID | ULID | Yes | Character who saved the template |
| Title | string | Yes | |
| Description | string | No | |
| LocationID | *ULID | No | Default grid location |
| PoseOrderMode | PoseOrderMode | Yes | |
| ContentWarnings | []string | No | |
| Tags | []string | No | |
| CreatedAt | time.Time | Yes | |

Creating a scene from a grid location (shadowing) is semantically equivalent
to creating from an implicit template — the location provides the default
title, description, and LocationID.

### 1.5 Scene Log (Published)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| ID | ULID | Yes | |
| SceneID | ULID | Yes | Source scene |
| Title | string | Yes | Snapshot of scene title |
| Content | string | Yes | Rendered IC log (markdown) |
| Participants | []string | Yes | Character names at time of publication |
| PublishedAt | time.Time | Yes | |

Published logs are immutable snapshots. The original event stream remains
participant-only regardless of publication status.

## 2. Membership vs Focus

*(Unchanged from v1.)*

These are separate concepts at different scopes:

| Concept | Scope | Persistence | Controls |
|---------|-------|-------------|----------|
| Scene membership | Character | Persistent (plugin database) | Read/write access to streams, vote eligibility, log access |
| Connection focus | Connection | Ephemeral (in-memory, server-side) | Input routing, output rendering for this connection |
| Grid presence | Derived | Computed | Character visible at grid location if ANY connection focused on grid |

### 2.1 Rules

- Membership without focus: character is in the scene but not actively
  looking at it. They accumulate notifications.
- Focus without membership: MUST NOT be possible. A connection cannot focus
  on a scene its character is not a member of.
- Grid focus is the default: a connection with no explicit scene focus is
  focused on the character's grid location.
- Joining a scene on a terminal connection SHOULD automatically focus that
  connection on the scene (UX convenience).
- A character with zero active connections retains all scene memberships.

### 2.2 Multi-Connection Visibility

A character can have multiple simultaneous connections (e.g., telnet + web).
Each connection has its own focus. The character is visible in every context
where at least one connection is focused:

- Telnet focused on grid + web focused on scene #42 = visible on grid AND
  a member of scene #42.
- Both connections focused on grid = visible on grid only (still a member
  of all joined scenes, just not focused on any).

Connection focus state lives in the **server**, not the plugin. The plugin
does not know which connection is currently focused on which scene. The
server queries the plugin for membership when needed; the plugin queries the
server for focus when needed (Phase 5 establishes the contract).

## 3. Event Streams & Routing

*(Unchanged from v1 in stream naming and routing logic. Stream publication
moves into the plugin process.)*

### 3.1 Stream Naming

Each scene gets two event streams:

| Stream        | Convention                                    | Content                                              | Archived in log |
| ------------- | --------------------------------------------- | ---------------------------------------------------- | --------------- |
| IC            | `events.<game_id>.scene.<scene_id>.ic`        | Poses, says, arrives, leaves, system                 | Yes             |
| OOC           | `events.<game_id>.scene.<scene_id>.ooc`       | OOC chat, pose order notifications                   | Never           |
| Notifications | `events.<game_id>.notification.<character_id>` | Cross-scene activity markers for non-focused members | Never           |

Grid locations use `events.<game_id>.location.<id>`. Character streams use
`events.<game_id>.character.<id>`.

> **Note (holomush-rops):** All pub/sub stream subjects now use NATS dot-style
> exclusively. Scene IC/OOC migrated in Phase 4 (`holomush-5rh.13`, ADR
> holomush-s9nu); location, character, and notification streams completed by
> `holomush-rops`. The legacy colon form (`scene:<id>:ic`, `location:<id>`,
> `notifications:<character_id>`) is eradicated; `subjectxlate` is deleted.

### 3.2 Command Routing

When a connection sends input, the dispatcher checks that connection's focus:

- Focused on grid: route to `events.<game_id>.location.<character_location_id>`
- Focused on scene: route to `events.<game_id>.scene.<scene_id>.ic`
  (or `events.<game_id>.scene.<scene_id>.ooc` for the `ooc` command)

### 3.3 Output Delivery

A connection receives events from its focused stream(s) only. Events from
other scenes where the character has membership arrive as notifications
through a per-character notification stream: `events.<game_id>.notification.<character_id>`.
The plugin emits notification events as a side effect of writing to a scene
IC stream when the scene has members not currently focused on it.

### 3.4 Subscription Lifecycle

When a connection focuses on a scene:

1. Subscribe to `events.<game_id>.scene.<scene_id>.ic` (primary output)
2. Subscribe to `events.<game_id>.scene.<scene_id>.ooc` (OOC output)
3. Unsubscribe from previous focus stream
4. Replay unseen IC events

When a connection focuses on grid:

1. Subscribe to `events.<game_id>.location.<loc_id>`
2. Unsubscribe from previous focus stream
3. Replay per location's replay policy

The subscription mechanism is server-side. The plugin emits events; the
server's event store and subscription router deliver them to connections.

## 4. Pose Order System

*(Unchanged from v1.)*

Pose order is tracked per-scene, unenforced, and configurable.

### 4.1 Modes

| Mode | Behavior |
|------|----------|
| `strict` | Linear order based on join sequence. Posing moves you to the end. Out-of-turn poses adjust the order. |
| `3pr` | Eligible to pose after 3 other poses since your last. Multiple eligible simultaneously. |
| `5pr` | Eligible to pose after 5 other poses since your last. Multiple eligible simultaneously. |
| `free` | No tracking. Timestamps only. |

### 4.2 Implementation

Pose order is derived from the IC event stream — no separate state table.
The plugin reads recent events from `events.<game_id>.scene.<scene_id>.ic`,
extracts who posed and when, and computes who is next/eligible based on
the mode. Derivation lives in `plugins/core-scenes/poseorder.go`.

### 4.3 Display

- **Telnet/terminal:** `scene order` command displays current pose order.
  No automatic display after each pose.
- **Web chat view:** Pose order bar rendered in UI chrome (always visible).
- **Web forum view:** Pose order shown as thread metadata.

### 4.4 Nudges

Optional idle nudge: if a character in the pose order has not posed within a
configurable threshold, the system MAY send a notification to their
connections. Configurable per-scene by owner. Off by default.

## 5. Access Control & Privacy

This section is **substantively rewritten** for v2. The product semantics
(who can do what) are unchanged. The mechanism is fundamentally different.

### 5.1 ABAC Model

Scene access control is enforced via the host's ABAC policy engine, with
policies declared in the plugin manifest and scene attributes resolved on
demand by the plugin's `AttributeResolverService`.

The plugin owns the `scene` resource type. To enable this, `scene` MUST be
removed from `ProtectedResourceTypes` in `internal/plugin/manifest.go`.
The `scene` type is protected today only because the v1 design placed it
in the server core. With v2, the plugin owns the type and the protection
is incorrect. Removal is part of Phase 1.

### 5.2 Scene Resource Schema

The plugin's `AttributeResolverService.GetSchema` returns:

```go
"scene": {
    Attributes: {
        "id":          STRING,
        "owner":       STRING,
        "state":       STRING,    // "active" | "paused" | "ended" | "archived"
        "visibility":  STRING,    // "open" | "private"
        "location_id": STRING,    // empty if off-grid
        "tags":        STRING_LIST,
        "warnings":    STRING_LIST,
    },
},
```

`AttributeResolverService.ResolveResource` accepts a `resource_id` of the
form `scene:<ulid>` and returns the current values from the plugin's
database. This is called by the host's ABAC engine during policy
evaluation.

### 5.3 Manifest Policies

The plugin manifest declares Cedar-style policies that gate command
execution and resource access. Examples (Phase 1 minimum):

```yaml
policies:
  # Layer 1: command execution gate
  - name: execute-scene-commands
    dsl: >-
      permit(principal is character, action in ["execute"], resource is command)
      when { resource.command.name in ["scene", "scenes"] };

  # Layer 2: read scenes you own (Phase 1 only — Phase 3 will add
  # member-based read for scenes you've joined)
  - name: read-own-scene
    dsl: >-
      permit(principal is character, action in ["read"], resource is scene)
      when { resource.scene.owner == principal.id };
```

Later phases add policies for join (open vs private), invite/kick (owner),
end/pause/resume (owner), and member-based reads. Each policy MUST be
declared in the manifest and MUST reference only attributes returned by
`GetSchema`. Policies referencing unknown attributes generate manifest
warnings (`internal/plugin/manifest_warnings.go`) but do not block load.

### 5.4 ABAC Resource Mapping

| Resource | Action | Who | Phase |
|----------|--------|-----|-------|
| `command:scene` | `execute` | Any character (ABAC default) | 1 |
| `command:scenes` | `execute` | Any character | 1 |
| `scene:<id>` | `read` | Owner | 1 |
| `scene:<id>` | `read` | Members | 3 |
| `scene:<id>` | `write` | Members (active/paused only) | 3 |
| `scene:<id>` | `join` | Open: anyone. Private: invited only | 3 |
| `scene:<id>` | `end` | Owner only | 2 |
| `scene:<id>` | `pause`/`resume` | Owner only in Phase 2 (members have no identity yet); Phase 3 widens `resume` to any member once the participant model exists (D6 async safety) | 2 |
| `scene:<id>` | `invite`/`kick` | Owner only | 3 |
| `scene_log:<id>` | `read` | **NOT enforced via ABAC** — see 5.5 | 6 |
| `scene_log:<id>` | `publish` | System-only (triggered by unanimous vote) | 6 |

### 5.5 Hard Privacy Boundary

Scene log reads MUST be enforced **in plugin code**, not via ABAC policy.
The scene log access check MUST NOT consult the ABAC engine — it is a direct
membership check implemented in the plugin's repository layer
(`plugins/core-scenes/store.go::GetSceneLog`). No role, including admin, can
bypass this check.

This is a deliberate design choice: player RP content is private by default,
and the system architecture makes it structurally impossible for
administrators to read scene logs they were not participants in. The check
lives in the same package as the data, with no policy engine in the path.

The plugin MUST NOT expose any RPC, attribute resolver hook, or side channel
that returns scene log content to a non-participant. This includes:

- `GetSceneLog` RPC: rejects with `permission denied` if caller is not a
  participant
- `AttributeResolverService.ResolveResource`: MUST NOT return log content as
  an attribute
- Postgres role isolation: the plugin's role has no GRANT to other plugins'
  schemas, so even SQL-level cross-plugin reads are impossible

### 5.6 Log Publishing Flow

```text
Owner runs "scene end"
  -> Scene state = ended
  -> Plugin emits notification to all participants: "Publish this scene log? (yes/no)"
  -> Participants vote asynchronously via "scene vote yes/no"
  -> IF all vote yes: snapshot IC stream -> SceneLog with visibility=public
  -> IF any vote no OR vote timeout: log stays participant-only permanently
  -> Scene state = archived
```

Unanimous consent is required. There is no mechanism to re-vote or change
a vote after casting. The vote window has a configurable timeout (game-wide
default, overridable per-scene by owner). When the timeout expires with
outstanding votes, the log stays participant-only.

The roadmap's three-tier visibility model (`public`, `private`, `unlisted`)
was simplified. Scene access uses `open`/`private`. Log visibility is
binary: participant-only (default) or published (unanimous vote). The
`unlisted` concept is unnecessary because the scene board handles
discovery, and private scenes are invisible on the board.

### 5.7 Log Access Summary

| Viewer | Active scene | Ended (unpublished) | Published |
|--------|-------------|---------------------|-----------|
| Participant | Read IC+OOC streams | Read IC+OOC streams | Read IC+OOC streams + published snapshot |
| Non-participant | No access | No access | Published snapshot only |
| Admin | No access | No access | Published snapshot only |

### 5.8 Log Download/Replay

Participants MAY download or replay scene logs at any time regardless of
scene state.

| Interface | Behavior |
|-----------|----------|
| Telnet | `scene log` replays IC events to terminal; `scene log #42` for non-focused scenes |
| Web | In-thread view + download button (markdown/plain text) |
| Published | Public URL, downloadable by anyone |

## 6. Commands

*(Unchanged from v1. Commands are dispatched by the host to the plugin via
`PluginCommandDeliverer`. The plugin claims both `scene` and `scenes` as
top-level commands in its manifest.)*

### 6.1 Scene Lifecycle

| Command | Description |
|---------|-------------|
| `scene create [title]` | Create scene. Flags: `--location`, `--template`, `--visibility`, `--pose-order`, `--tags`, `--warnings` |
| `scene end` | Owner ends scene. Triggers publish vote. Returns terminal-focused participants to grid. |
| `scene pause` | Owner manually pauses scene. |
| `scene resume` | Owner resumes a paused scene. Phase 2 is owner-only because there is no participant model yet; Phase 3 widens this to any member (D6 async safety: async scenes MUST be resumable by any participant, not just the owner, to avoid blocking on an absent owner). |

### 6.2 Membership

| Command | Description |
|---------|-------------|
| `scene join #<id>` | Join open scene (+ focus on terminal). Private requires invite. |
| `scene leave` | Leave focused scene. Drops membership. |
| `scene invite <character>` | Owner invites a character. |
| `scene kick <character>` | Owner removes a participant. |

### 6.3 Focus (Terminal/Telnet)

| Command | Description |
|---------|-------------|
| `scene focus #<id>` | Switch connection focus to scene. |
| `scene grid` | Return focus to grid location. |
| `scene list` | List memberships and focused scene. |

### 6.4 In-Scene

| Command | Description |
|---------|-------------|
| `scene order` | Display current pose order. |
| `scene log` | Replay IC log to terminal. |
| `scene info` | Show scene metadata, participants, state. |
| `scene set <field> <value>` | Owner modifies settings. |

### 6.5 Templates

| Command | Description |
|---------|-------------|
| `scene save-template [name]` | Save current scene config as template. |
| `scene templates` | List saved templates. |
| `scene create --template <name>` | Create from template. |

### 6.6 Publish Vote

| Command | Description |
|---------|-------------|
| `scene vote yes` | Vote to publish log. |
| `scene vote no` | Vote against publishing. |

### 6.7 Scene Board

| Command | Description |
|---------|-------------|
| `scenes` | Browse open/public scenes (scene board). |

## 7. gRPC API

This section is **substantively rewritten** for v2. The RPC list is
unchanged from v1; the implementation location and the host-plugin path
are new.

### 7.1 Service Contract

The proto contract is `holomush.scene.v1.SceneService` defined in
`api/proto/holomush/scene/v1/scene.proto`. Generated bindings live in
`pkg/proto/holomush/scene/v1/`. This contract is the **only** boundary
between the scene plugin and any other component (server core, web client,
other plugins). Direct Go imports of the plugin's internal types from
outside the plugin package are forbidden.

### 7.2 RPCs

| RPC | Description | Phase |
|-----|-------------|-------|
| `CreateScene` | Create from scratch, location, or template | 1 |
| `GetScene` | Scene detail: metadata + participants | 1 |
| `ListScenes` | Scene board: paginated, filterable | 8 |
| `UpdateScene` | Owner modifies settings (`scene set` command) | 2 |
| `EndScene` | Owner ends, triggers vote | 2 |
| `PauseScene` | Owner pauses | 2 |
| `ResumeScene` | Owner resumes (Phase 2); Phase 3 widens to any member (D6 async safety) | 2 |
| `JoinScene` | Join open scene | 3 |
| `LeaveScene` | Drop membership | 3 |
| `InviteToScene` | Owner invites character | 3 |
| `KickFromScene` | Owner removes participant | 3 |
| `CastPublishVote` | Vote yes/no on log publishing | 6 |
| `GetSceneLog` | Participant: stream IC events. Published: public. | 6 |
| `DownloadSceneLog` | Rendered export (markdown/text) | 6 |
| `ListTemplates` | User's saved templates | 7 |
| `CreateTemplate` | Save template | 7 |
| `DeleteTemplate` | Remove template | 7 |
| `GetPoseOrder` | Current pose order state | 4 |

### 7.3 Service Provision

The plugin declares `provides: [holomush.scene.v1.SceneService]` in its
manifest. The plugin's `RegisterServices` method registers the
`SceneServiceServer` implementation on the gRPC registrar passed by the
host. The host proxies inbound requests through the plugin's gRPC server.

The plugin declares `requires: [holomush.world.v1.WorldService]` so the
host injects a connection to the world service via the GRPCBroker. The
plugin uses this connection to query character locations, check that a
declared `LocationID` exists when creating a scene attached to grid, etc.

The web client connects to scene RPCs via ConnectRPC. The route is:
web client → ConnectRPC handler in server → gRPC client to plugin
process. From the web client's perspective, scenes are just another
gRPC service; it does not know they live in a separate process.

## 8. Forum-Style Interface

**Out of scope for this epic.** The forum view is deferred to a separate
cross-cutting epic that will cover both scenes and OOC comms. The scene
plugin's gRPC contract exposes everything a forum renderer needs:
`ListScenes` for thread listings, `GetScene` for thread metadata,
event-stream subscription for posts. No scene-side work is required when
the forum epic is built.

The conceptual mapping (thread = scene, post = pose, etc.) and the UX
differences from chat view are documented in v1 sections 8.1 and 8.2 and
remain valid as background.

## 9. Plugin Architecture

This section is **new in v2** and replaces v1 Section 9 ("Migration from
existing code"), which is obsolete. There is no historical scene data to
migrate; the plugin starts with an empty schema.

### 9.1 Plugin Manifest

`plugins/core-scenes/plugin.yaml`:

```yaml
name: core-scenes
version: 1.0.0
type: binary
resource_types: [scene]

requires:
  - holomush.world.v1.WorldService

provides:
  - holomush.scene.v1.SceneService
# Note: holomush.plugin.v1.AttributeResolverService is NOT listed in
# `provides`. It is a singleton service exposed by the go-plugin subprocess
# itself and discovered by the host via `host.AttributeResolverClient(...)`;
# advertising it here would conflict with the host's implicit registration.
# Keep the real plugin.yaml and this example in sync — see the checked-in
# `plugins/core-scenes/plugin.yaml`.

storage: postgres

binary-plugin:
  executable: core-scenes

commands:
  - name: scene
    capabilities:
      - { action: write, resource: scene, scope: local }
    help: "Manage RP scenes"
    usage: "scene <subcommand> [args]"
  - name: scenes
    help: "Browse open scenes"
    usage: "scenes [--tags tag1,tag2]"

policies:
  # See section 5.3 — phase-by-phase additions
```

### 9.2 Schema Isolation

The host provisions the plugin's Postgres schema before calling `Init`:

| Resource | Value |
|----------|-------|
| Schema name | `plugin_core_scenes` (auto-derived from manifest `name`) |
| Role name | `holomush_plugin_core_scenes` |
| Privileges | `USAGE, CREATE` on own schema; **REVOKE ALL** on `public` |
| Connection string | `postgres://holomush_plugin_core_scenes:<pw>@host:port/db?search_path=plugin_core_scenes` |

The plugin receives the connection string in `ServiceConfig.ConnectionString`
during `Init`. The `search_path` is set automatically by the connection
string parameter; the plugin does not execute `SET search_path`.

### 9.3 Migrations

Migrations live in `plugins/core-scenes/migrations/` and are embedded in the
plugin binary:

```go
//go:embed migrations/*.up.sql
var migrationsFS embed.FS
```

The plugin's `NewSceneStore` calls `storage.RunMigrationsFS(ctx, pool, sub)`
during `Init`. Migration files MUST follow the convention
`NNNNNN_name.up.sql` (six-digit zero-padded version). Only `.up.sql` files
are applied; `.down.sql` files are not supported by the plugin migration
runner. Migrations are tracked in a `plugin_migrations` table in the
plugin's own schema.

Migration scope by phase:

| Phase | Migration | Tables added |
|-------|-----------|--------------|
| 1 | `000001_scenes.up.sql` | `scenes` |
| 3 | `000002_scene_participants.up.sql` | `scene_participants` |
| 6 | `000003_scene_logs.up.sql` | `scene_logs` |
| 7 | `000004_scene_templates.up.sql` | `scene_templates` |

### 9.4 Plugin Lifecycle

The plugin process is launched by the host on server startup. Lifecycle:

1. Host parses the plugin manifest from `plugins/core-scenes/plugin.yaml`
2. Host starts the plugin subprocess via go-plugin
3. During gRPC server setup in the plugin process, the SDK calls
   `RegisterServices` and `RegisterAttributeResolver`; plugin registers
   `SceneServiceServer` and `AttributeResolverServiceServer`
4. Host establishes a gRPC connection to the plugin
5. If the manifest declares `requires`, host opens broker proxies for each
   required service (`holomush.world.v1.WorldService` for scenes)
6. If the manifest declares `storage: postgres`, host provisions the
   plugin's Postgres schema and role via `SchemaProvisioner`
7. Host sends the `Init` RPC to the plugin with `ServiceConfig`
   containing the connection string and any required-service broker addresses
8. Plugin's `Init` opens the connection pool, runs embedded migrations, and
   wires the store into its services
9. After `host.Load` returns, the manager calls `GetSchema` on the plugin's
   `AttributeResolverService` to discover the resource type schema
10. Manager validates that the schema covers the declared `resource_types`
11. Manager validates and installs the manifest policies via
    `policy_installer.go`, with full manifest context for trust boundary checks
12. Manager registers the plugin's commands (`scene`, `scenes`) in the
    command registry
13. Plugin is fully operational and routable

Steps 1-8 are inside `host.Load`. Steps 9-12 are inside `manager.loadPlugin`
after `host.Load` returns. This ordering means the plugin's database is
already provisioned and migrated before any host call into the plugin
beyond `Init` — `GetSchema` and `ResolveResource` can both rely on the
database being available.

Shutdown is host-initiated and not graceful: the host kills the plugin
process via SIGTERM. The plugin SHOULD use `defer pool.Close()` and
`context.Background()` cancellation in long-running operations to limit
exposure to abrupt termination, but MUST NOT depend on receiving a
shutdown signal.

### 9.5 Command Routing

When a user types `scene create foo`:

1. Server's command dispatcher parses input → command name = `"scene"`
2. Registry lookup finds entry registered by `core-scenes` plugin
3. Layer 1 ABAC: host evaluates `permit(principal is character, action in
   ["execute"], resource is command) when { resource.command.name == "scene" }`
   against the active subject
4. Layer 2 capability check: pre-flight against declared `commands.capabilities`
5. Dispatcher calls `PluginCommandDeliverer.DeliverCommand(ctx, "core-scenes", cmd)`
6. Manager routes to `Host.DeliverCommand` for the binary plugin host
7. Host calls the plugin's `HandleCommand` via gRPC
8. Plugin's command handler dispatches the subcommand (`create`) and
   executes business logic via its service layer

### 9.6 Cross-Plugin and Plugin-Host Communication

Cross-plugin and plugin → server communication is exclusively via the
proto contracts declared in `requires`/`provides`. The scene plugin
requires `holomush.world.v1.WorldService` to query character locations
and validate location IDs. The host injects the connection via the
GRPCBroker; the plugin receives it as a gRPC client stub.

The `ServiceProxy` pattern from before PR #192 is **deleted** and MUST
NOT be reintroduced. Any new cross-component data flow goes through a
proto contract.

## 10. Observability

This section is **new in v2**. Observability is a first-class concern from
Phase 1, not a polish task at the end.

### 10.1 Tracing

The plugin emits OpenTelemetry spans at all of these boundaries:

| Span | Operation | Attributes |
|------|-----------|------------|
| `scene.service.<rpc>` | One per gRPC RPC entry point (e.g., `scene.service.create_scene`) | `subject_id`, `scene_id` (if known), `result` |
| `scene.resolver.resolve_resource` | AttributeResolverService.ResolveResource | `resource_type`, `resource_id`, `attributes_returned` |
| `scene.resolver.get_schema` | AttributeResolverService.GetSchema | (no attributes — once per load) |
| `scene.store.<op>` | One per store method (e.g., `scene.store.create`, `scene.store.get`) | `scene_id`, `rows_affected` |
| `scene.lifecycle.transition` | State transition (active→paused, etc.) | `scene_id`, `from_state`, `to_state`, `reason` |
| `scene.command.<sub>` | Each scene subcommand handler | `subject_id`, `subcommand`, `result` |

Spans MUST propagate the parent context received from the host. The host's
plugin gRPC client injects trace context into outgoing requests; the
plugin extracts it and uses it as the parent for all spans within that
request scope.

### 10.2 Metrics

**Architectural gap (Phase 1 implication):** The current plugin infrastructure
does not have a defined mechanism for binary plugins to expose Prometheus
metrics. The server's `internal/observability/server.go` runs a metrics HTTP
endpoint in the server process; binary plugins are separate processes with no
metrics exposure path. Until this is resolved, **Phase 1 emits OTel-style
structured logs and tracing only**; Prometheus metrics for the scene plugin
are deferred until plugin metrics infrastructure exists. This gap is also
recorded in section 11.

When the gap is closed, the metrics below MUST be added.

Counter metrics:

| Metric | Labels | Description |
|--------|--------|-------------|
| `scene_created_total` | `visibility`, `from_template` | Scenes created |
| `scene_state_transitions_total` | `from`, `to`, `reason` | State changes |
| `scene_membership_changes_total` | `op` (`join`/`leave`/`invite`/`kick`/`vote`) | Membership events |
| `scene_publish_votes_total` | `vote` (`yes`/`no`/`timeout`) | Vote outcomes |
| `scene_logs_published_total` | (none) | Logs that reached published state |
| `scene_abac_denials_total` | `action`, `resource_type` | ABAC denials at the resolver layer |

Histogram metrics:

| Metric | Description |
|--------|-------------|
| `scene_rpc_duration_seconds` | Per-RPC latency, labeled by RPC name and result |
| `scene_resolver_duration_seconds` | Attribute resolution latency, labeled by `resource_type` |
| `scene_store_duration_seconds` | Per-operation store latency, labeled by op name |

Gauge metrics:

| Metric | Description |
|--------|-------------|
| `scene_active_scenes` | Current count of scenes in `active` state |
| `scene_total_participants` | Sum of participants across all active scenes |

### 10.3 Structured Logs

The plugin uses `slog` (matching project convention). Log entries at
key boundaries:

- **gRPC entry/exit:** INFO level on success, WARN on validation failure,
  ERROR on infrastructure failure. Always include `subject_id` and `rpc`.
- **State transitions:** INFO with `scene_id`, `from`, `to`, `reason`.
- **ABAC denials:** WARN with `subject_id`, `action`, `resource_id`, `policy`.
- **Hard privacy boundary blocks:** WARN with `subject_id`, `scene_id`,
  `caller_role`. These are security-relevant and MUST be visible to
  operators.
- **Publish vote outcomes:** INFO with `scene_id`, `outcome`, `voters_yes`,
  `voters_no`, `voters_pending`.
- **Store errors:** ERROR with operation, parameters (without secrets),
  and wrapped pgx error code.

### 10.4 Important Business Events

These business events MUST be logged AND emit a metric AND open a span:

| Event | Phase |
|-------|-------|
| Scene created | 1 |
| Scene state transition | 2 |
| Scene join/leave/invite/kick | 3 |
| Pose published to IC stream | 4 |
| Connection focus change | 5 |
| Vote cast | 6 |
| Log published | 6 |
| Hard privacy boundary block (someone tried to read a log they shouldn't) | 6 |
| Scene template created | 7 |

## 11. Areas Needing Deeper Design

These areas are identified but not fully specified. Each SHOULD get its own
focused design pass before implementation. Carried forward from v1.

| Area | Dependency | Notes |
|------|-----------|-------|
| **Binary plugin Prometheus metrics path** | Plugin infra | Binary plugins are separate processes; the server's Prometheus registry in `internal/observability/server.go` is not accessible. Decision needed: (a) plugin runs its own metrics HTTP endpoint that the host scrapes, (b) plugin emits metrics over a gRPC streaming service the host consumes, or (c) plugin pushes to a shared registry via a host-provided service. Phase 1 defers Prometheus metrics for the scene plugin and uses structured logging + OTel tracing only. |
| **Plugin → server event emission contract** | Plugin infra | Phases 4 and 6 require the plugin to emit events to scene IC/OOC streams and the per-character notification stream. The current plugin infra (PRs #192/#194/#195) provides `requires`/`provides` for service consumption but no documented "emit event" path from plugin to host. Either: (a) define a new `EventEmitterService` in plugin infra that the host provides via GRPCBroker, or (b) the scene plugin owns its own event store. Decision needed before Phase 4. |
| Telnet edge cases | None | Multi-character on same connection, connection recovery |
| Content warning taxonomy | None | Which system-defined categories; game-configurable? |
| Idle timeout defaults | None | Game-wide default duration, admin configuration surface |
| Scene board web integration | Epic 8 | How scene board fits in web client navigation |
| Notification preferences | None | Per-scene mute, digest vs real-time, notification channels |
| Published log presentation | Epic 8 | Public URL routing, SEO, share links |
| Forum view detailed UX | Forum epic | Out of scope here; will be designed when forum epic starts |
| **Plugin → server focus model integration** | Phase 5 | Closed by holomush-5rh.14 (Phase 5 design + impl). Connection focus state lives in the server; plugins query (via PluginHostService.IsAnyConnFocused) and influence (via SetConnectionFocus / AutoFocusOnJoin) focus through 3 host RPCs added in Phase 5. |

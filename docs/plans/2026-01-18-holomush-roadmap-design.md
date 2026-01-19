# HoloMUSH Roadmap Design

**Status:** Draft
**Date:** 2026-01-18
**Version:** 2.0.0

## Overview

This document defines the HoloMUSH development roadmap, organized as iterative epics with internal phases managed by phase gates. It supersedes the original architecture design's phase structure and incorporates learnings from the Extism plugin spike.

### Design Goals

- Modern Go core with event-oriented architecture
- Dual protocol support (web and telnet)
- Two-tier plugin system: Lua for simplicity, go-plugin for power
- Tmux-style session persistence and reconnection
- Offline-capable PWA with sync
- Target scale: ~200 concurrent users

### Key Architecture Decisions

| Decision         | Choice                            | Rationale                                                                          |
| ---------------- | --------------------------------- | ---------------------------------------------------------------------------------- |
| Plugin runtime   | Lua (gopher-lua) + go-plugin      | Lua is cheap (~40KB/instance vs ~10MB for WASM), go-plugin enables complex systems |
| Password hashing | argon2id                          | Memory-hard, GPU-resistant, 10 years battle-tested                                 |
| Command prefixes | None                              | Break from MU* tradition (@, +), use plain command names                           |
| Access control   | Phased (static roles → full ABAC) | Avoid over-engineering early while maintaining interface compatibility             |

## Plugin Architecture

### Two-Tier Model

```text
┌─────────────────────────────────────────────────────────────┐
│                       Go Core                               │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────────────┐    ┌─────────────────────────┐     │
│  │   Lua Runtime       │    │   go-plugin Host        │     │
│  │   (gopher-lua)      │    │   (gRPC subprocess)     │     │
│  │   - In-process      │    │   - Process isolated    │     │
│  │   - ~40KB/instance  │    │   - Full language power │     │
│  │   - Sandboxed       │    │   - External API access │     │
│  └──────────┬──────────┘    └───────────┬─────────────┘     │
└─────────────┼───────────────────────────┼───────────────────┘
              │                           │
        *.lua files                 plugin binaries
        (drop in folder)            (per platform)
```

### Tier Comparison

| Aspect            | Lua Scripts                | go-plugin Extensions         |
| ----------------- | -------------------------- | ---------------------------- |
| **Add**           | Drop `.lua` file           | Drop binary + manifest       |
| **Write**         | Text editor                | Compile with Go/Rust/etc     |
| **Capabilities**  | Sandboxed, declared        | Declared in manifest         |
| **Concurrency**   | State pool, cheap          | Full control                 |
| **External APIs** | No                         | Yes (if granted)             |
| **Hot reload**    | Planned                    | Requires restart             |
| **Use cases**     | Commands, simple behaviors | Combat, economy, Discord, AI |

### Lua Script Example

```lua
-- plugins/echo-bot/plugin.lua

plugin = {
    name = "echo-bot",
    version = "1.0.0",
    events = {"say"}
}

function on_event(event)
    if event.type ~= "say" then return end
    if event.actor_kind == "plugin" then return end

    local msg = event.payload.message
    emit_event(event.stream, "say", {
        message = "Echo: " .. msg
    })
end
```

### go-plugin Example

```yaml
# plugins/combat-system/plugin.yaml
name: combat-system
version: 2.1.0
binary: combat-${os}-${arch}
capabilities:
  - events.subscribe.location
  - events.emit.location
  - world.read
  - world.write.characters
  - net.http
```

### Why Not WASM?

The Extism/WASM spike (Phase 1.6) revealed scalability concerns:

| Metric              | WASM (Python/Extism) | Lua (gopher-lua) |
| ------------------- | -------------------- | ---------------- |
| Memory per instance | ~10MB                | ~40KB            |
| Startup time        | ~1.5s                | ~50μs            |
| 100 instances       | ~1GB, 150s           | ~4MB, 5ms        |

For ~200 concurrent users, Lua's lightweight instances eliminate the WASM concurrency concerns entirely. WASM remains a future option if security isolation of untrusted third-party code becomes necessary.

## Epic Roadmap

### Dependency Graph

```text
Core Path:
1 ──▶ 2 ──▶ 3 ──▶ 4 ──▶ 5 ──▶ 6 ──▶ 7 ──▶ 8 ──▶ 9

Extensions (after Epic 6+):
              6 ──▶ 10 (Channels)
                    8 ──▶ 11 (Forums)
              2 + 10 ──▶ 12 (Discord)
                    8 ──▶ 13 (iOS, stretch)
```

### Epic Summary

| Epic | Name                 | Status  | Key Deliverables                       |
| ---- | -------------------- | ------- | -------------------------------------- |
| 1    | Foundation           | Done    | Events, sessions, telnet, gRPC, OTel   |
| 2    | Plugin System        | Next    | Lua runtime, go-plugin, capabilities   |
| 3    | Core Access Control  |         | Static roles, permission interface     |
| 4    | World Model          |         | Locations, exits, objects, characters  |
| 5    | Auth & Identity      |         | Players, characters, argon2id auth     |
| 6    | Commands & Behaviors |         | Parser, aliases, core + Lua commands   |
| 7    | Full ABAC            |         | Dynamic policies, attribute evaluation |
| 8    | Web Client           |         | SvelteKit PWA, terminal, offline       |
| 9    | Scenes & RP          |         | Scene isolation, tags, logging         |
| 10   | Channels             |         | Channel types, membership, moderation  |
| 11   | Forums               |         | Threads, posts, web UI                 |
| 12   | Discord Integration  |         | Bridge, OAuth, notifications           |
| 13   | iOS Client           | Stretch | Swift/SwiftUI native app               |

---

## Epic 1: Foundation (Complete)

**Status:** Done

### Deliverables

- Event system with ULID ordering
- Session management with reconnection
- Telnet adapter
- gRPC control plane
- Observability (OpenTelemetry tracing)
- PostgreSQL event store

### Note on Extism Work

Phase 1.6 (Extism plugin framework) produced working code that validates:

- OTel tracing patterns for plugin calls
- Event delivery and subscription model
- Plugin manifest concepts

This work becomes a spike/reference. Learnings carry forward to Epic 2, but implementation pivots to Lua + go-plugin.

---

## Epic 2: Plugin System

**Goal:** Establish the extension model everything else builds on.

### Phases

| Phase | Deliverable                          | Gate                                                             |
| ----- | ------------------------------------ | ---------------------------------------------------------------- |
| 2.1   | Lua runtime integration (gopher-lua) | Can load and execute a Lua script                                |
| 2.2   | Plugin discovery & lifecycle         | Scripts in `plugins/*/` auto-discovered                          |
| 2.3   | Event subscription & delivery        | Lua scripts receive events, can emit events                      |
| 2.4   | Host functions                       | `query_location()`, `query_character()`, `log()`, `kv_get/set()` |
| 2.5   | Capability model                     | Scripts declare capabilities, host enforces                      |
| 2.6   | go-plugin integration                | Heavy plugins via gRPC subprocess                                |
| 2.7   | Echo bot in Lua                      | Prove the model end-to-end                                       |

### Key Decisions

- Lua sandbox: no file/network access by default
- Capability grants stored in config, not code
- go-plugin uses same event/capability model as Lua
- State pooling for Lua instances (cheap enough to create per-event if needed)

### Host Functions Available to Lua

| Function                            | Description              |
| ----------------------------------- | ------------------------ |
| `emit_event(stream, type, payload)` | Send events              |
| `query_location(id)`                | Read location data       |
| `query_character(id)`               | Read character data      |
| `query_object(id)`                  | Read object data         |
| `log(level, message)`               | Structured logging       |
| `kv_get(key)`                       | Plugin key-value storage |
| `kv_set(key, value)`                | Plugin key-value storage |

---

## Epic 3: Core Access Control

**Goal:** Minimal permission system that everything can build against. Full ABAC comes later.

### Phases

| Phase | Deliverable                   | Gate                                                  |
| ----- | ----------------------------- | ----------------------------------------------------- |
| 3.1   | AccessControl interface       | `Check(subject, action, resource) bool` defined       |
| 3.2   | Static role implementation    | Admin/builder/player roles with hardcoded permissions |
| 3.3   | Plugin capability enforcement | Capabilities checked before host function calls       |
| 3.4   | Integration with event system | Event emission respects permissions                   |

### Roles (MVP)

| Role      | Permissions                               |
| --------- | ----------------------------------------- |
| `admin`   | All actions                               |
| `builder` | World modification + player permissions   |
| `player`  | Basic interaction (say, pose, move, look) |

### Interface

```go
type Subject struct {
    Kind  SubjectKind  // Player, Character, Plugin, System
    ID    string
    Roles []string
}

type AccessControl interface {
    Check(ctx context.Context, subject Subject, action string, resource Resource) (bool, error)
    Grant(ctx context.Context, subject Subject, capability string) error
    Revoke(ctx context.Context, subject Subject, capability string) error
}
```

---

## Epic 4: World Model

**Goal:** Locations, exits, objects, characters with proper schemas and plugin hooks.

### Phases

| Phase | Deliverable                  | Gate                                     |
| ----- | ---------------------------- | ---------------------------------------- |
| 4.1   | Location schema + repository | CRUD operations, location streams        |
| 4.2   | Exit schema + navigation     | Bidirectional exits, `move` works        |
| 4.3   | Object schema + inventory    | Objects exist, can be picked up/dropped  |
| 4.4   | Character-location binding   | Characters have a location               |
| 4.5   | Plugin hooks                 | `on_enter`, `on_leave`, `on_look` events |
| 4.6   | World seeding                | CLI/API to create initial world          |

### Schema Relationships

```text
Location ◄──1:N──► Exit ──► Location
    │
    1:N
    │
Character ◄──1:N──► Object (inventory)
    │
Location ◄──1:N──► Object (in room)
```

### Plugin Events

| Event            | When              |
| ---------------- | ----------------- |
| `location.enter` | Character arrives |
| `location.leave` | Character departs |
| `location.look`  | Character looks   |
| `object.pickup`  | Object taken      |
| `object.drop`    | Object dropped    |

---

## Epic 5: Auth & Identity

**Goal:** Player accounts, character creation, authentication flows.

### Phases

| Phase | Deliverable                  | Gate                                         |
| ----- | ---------------------------- | -------------------------------------------- |
| 5.1   | Player schema                | Accounts with email/password                 |
| 5.2   | Password auth                | argon2id hashing, login flow                 |
| 5.3   | Character creation           | Players create characters, select on connect |
| 5.4   | Session binding              | Login → select character → resume session    |
| 5.5   | OAuth integration (optional) | Discord/Google login linking                 |

### Authentication Flow

```text
Connect → Authenticate (player) → Select Character → Resume/Create Session
```

### Security

- Passwords: argon2id (time=1, memory=64MB, threads=4)
- Sessions: Secure tokens, server-side storage
- Rate limiting on auth endpoints

---

## Epic 6: Commands & Behaviors

**Goal:** Core commands implemented, with some as Lua scripts to prove the plugin model.

### Phases

| Phase | Deliverable             | Gate                                   |
| ----- | ----------------------- | -------------------------------------- |
| 6.1   | Command parser          | Input → command + args                 |
| 6.1b  | Alias system            | System + player-level aliases          |
| 6.2   | Core commands (Go)      | `look`, `move`, `quit`, `who`          |
| 6.3   | Communication (Lua)     | `say`, `pose`, `emit`                  |
| 6.4   | Building commands (Lua) | `dig`, `create`, `describe`, `link`    |
| 6.5   | Admin commands (Go)     | `boot`, `shutdown`, `wall`             |
| 6.6   | Help system (Lua)       | `help` with plugin-contributed entries |

### Command Implementation Types

| Commands                            | Implementation | Rationale                                |
| ----------------------------------- | -------------- | ---------------------------------------- |
| `look`, `move`, `quit`, `who`       | Go             | Core infra, session/world integration    |
| `say`, `pose`, `emit`               | Lua            | Proves plugin model, customizable        |
| `dig`, `create`, `describe`, `link` | Lua            | Moderate complexity, customizable        |
| `boot`, `shutdown`, `wall`          | Go             | Security-critical, direct session access |
| `help`                              | Lua            | Simple, plugin-extendable                |

### Alias System

| Level  | Scope                       | Examples                                   |
| ------ | --------------------------- | ------------------------------------------ |
| System | All players, set by admin   | `l` → `look`, `n/s/e/w` → `move north/...` |
| Player | Per-player, self-configured | `atk` → `attack`, `h` → `home`             |

Resolution order: Exact match → Player alias → System alias

---

## Epic 7: Full ABAC

**Goal:** Evolve Core Access Control into full Attribute-Based Access Control.

### Phases

| Phase | Deliverable                    | Gate                                             |
| ----- | ------------------------------ | ------------------------------------------------ |
| 7.1   | Policy schema                  | Policies stored in DB, versioned                 |
| 7.2   | Attribute evaluation           | Subject/resource/environment attributes resolved |
| 7.3   | Policy engine                  | Evaluate policies against attributes             |
| 7.4   | Runtime policy editing         | Admin UI/commands to manage policies             |
| 7.5   | Audit logging                  | All access decisions logged                      |
| 7.6   | Plugin attribute contributions | Plugins can add attributes                       |

### Policy Example

```yaml
- name: faction-hq-access
  effect: allow
  subjects:
    character.faction: "{{resource.location.faction}}"
  resources:
    type: location
    location.restricted: true
  actions: [enter, look]
```

### Evaluation Order

1. Collect attributes (subject, resource, action, environment)
2. Find matching policies
3. Any `deny` → deny
4. Any `allow` → allow
5. Default → deny

---

## Epic 8: Web Client

**Goal:** SvelteKit PWA with terminal, offline support, and portal features.

### Phases

| Phase | Deliverable         | Gate                                       |
| ----- | ------------------- | ------------------------------------------ |
| 8.1   | Project scaffold    | SvelteKit + PWA setup                      |
| 8.2   | WebSocket transport | Connect to server, receive events          |
| 8.3   | Terminal UI         | Input, scrollback, ANSI rendering          |
| 8.4   | Auth flows          | Login, character select                    |
| 8.5   | Offline support     | Command queue, event cache, reconnect sync |
| 8.6   | Portal: Wiki        | Help pages, lore, documentation            |
| 8.7   | Portal: Characters  | Public profiles, character sheets          |
| 8.8   | Portal: Admin       | Server stats, player management            |

### Offline Behavior

| State     | Behavior                                  |
| --------- | ----------------------------------------- |
| Online    | Real-time WebSocket events                |
| Offline   | Commands queued, cached content available |
| Reconnect | Queue flushed, missed events replayed     |

---

## Epic 9: Scenes & RP

**Goal:** Scene management for narrative play, visibility isolation, logging.

### Phases

| Phase | Deliverable          | Gate                                                     |
| ----- | -------------------- | -------------------------------------------------------- |
| 9.1   | Scene schema         | Create/join/leave scenes                                 |
| 9.1b  | Scene metadata       | Tags, visibility (public/private/unlisted)               |
| 9.2   | Visibility isolation | Scene participants only see each other                   |
| 9.3   | Scene commands       | `scene create`, `scene join`, `scene leave`, `scene end` |
| 9.4   | Scene logging        | Events captured, exportable                              |
| 9.5   | Web portal: Scenes   | Browse active scenes, view logs                          |
| 9.6   | Forum integration    | Scene requests, scheduling                               |

### Visibility Model

```text
Location: "The Tavern"
├── Real-time (no scene) ← new arrivals land here
├── Scene #42 "Secret Meeting" ← isolated
└── Scene #87 "Bar Fight" ← isolated
```

### Scene Metadata

| Field      | Options                                      |
| ---------- | -------------------------------------------- |
| visibility | `public`, `private`, `unlisted`              |
| tags       | `combat`, `mature`, `social`, `plot`, custom |

| Visibility | Who sees in listings | Who can view logs |
| ---------- | -------------------- | ----------------- |
| public     | Everyone             | Everyone          |
| unlisted   | No one (link only)   | Anyone with link  |
| private    | No one               | Participants only |

---

## Epic 10: Channels

**Goal:** In-game communication channels with membership and moderation.

### Phases

| Phase | Deliverable      | Gate                          |
| ----- | ---------------- | ----------------------------- |
| 10.1  | Channel schema   | Name, type, membership        |
| 10.2  | Channel commands | `channel join/leave/list/say` |
| 10.3  | Channel types    | Public, private, admin        |
| 10.4  | Moderation       | Mute, ban, ops                |
| 10.5  | Channel history  | Logging, replay on join       |

---

## Epic 11: Forums

**Goal:** Web-based discussion forums integrated with the game.

### Phases

| Phase | Deliverable         | Gate                     |
| ----- | ------------------- | ------------------------ |
| 11.1  | Forum schema        | Boards, threads, posts   |
| 11.2  | Web UI              | Browsing, posting        |
| 11.3  | Moderation tools    | Edit, delete, lock, move |
| 11.4  | Notifications       | New replies              |
| 11.5  | In-game integration | `forum recent` command   |

---

## Epic 12: Discord Integration

**Goal:** Bridge Discord and HoloMUSH for seamless community interaction.

### Phases

| Phase | Deliverable    | Gate                              |
| ----- | -------------- | --------------------------------- |
| 12.1  | Discord bot    | go-plugin implementation          |
| 12.2  | Channel bridge | Discord ↔ game channel sync       |
| 12.3  | OAuth linking  | Discord account to player account |
| 12.4  | Notifications  | Mentions, DMs forwarded           |
| 12.5  | Status sync    | Online/offline presence           |

---

## Epic 13: iOS Client (Stretch)

**Goal:** Native iOS app for mobile play.

### Phases

| Phase | Deliverable          | Gate              |
| ----- | -------------------- | ----------------- |
| 13.1  | SwiftUI scaffold     | App structure     |
| 13.2  | WebSocket transport  | Connect to server |
| 13.3  | Terminal UI          | Input, scrollback |
| 13.4  | Push notifications   | Event alerts      |
| 13.5  | App Store submission | Published         |

### Note

This is a stretch goal. The PWA (Epic 8) may suffice for mobile needs.

---

## Appendix: RFC2119 Keywords

| Keyword        | Meaning                                    |
| -------------- | ------------------------------------------ |
| **MUST**       | Absolute requirement                       |
| **MUST NOT**   | Absolute prohibition                       |
| **SHOULD**     | Recommended, may ignore with justification |
| **SHOULD NOT** | Not recommended                            |
| **MAY**        | Optional                                   |

---

## Appendix: Extism Spike Reference

The Phase 1.6 Extism work is preserved in:

- `internal/wasm/` - Working Extism host implementation
- `plugins/echo-python/` - Python plugin example
- `docs/plans/2026-01-18-extism-plugin-framework-design.md` - Original design

Key learnings applied to Epic 2:

- OTel tracing patterns for plugin calls
- Event subscription and delivery model
- Capability declaration in manifests
- Host function interface design

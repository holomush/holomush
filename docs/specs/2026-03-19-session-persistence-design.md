<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Server-Side Session Persistence Design

**Status:** Draft
**Date:** 2026-03-19
**Epic:** holomush-qve (Epic 8: Web Client)
**Scope:** Sub-spec 2a — Postgres-backed sessions, event replay on reconnect, command history

## Overview

This is sub-spec 2a of the Epic 8 Web Client. It builds the server-side session
persistence layer that both terminal mode (sub-spec 2b) and chat mode (sub-spec
2c) depend on. The goal is tmux-style session behavior: sessions survive client
disconnects, reconnecting clients see missed events, and command history persists
across connections and protocols (telnet and web).

## Goals

- MUST persist sessions in Postgres (survive server restarts)
- MUST support session detach on disconnect with configurable TTL
- MUST replay missed events on reconnect with gap-free delivery
- MUST store command history server-side per session
- MUST support cross-protocol session continuity (telnet to web and back)
- MUST expose a `SessionStore` interface suitable for cache-aside
- MUST provide proto RPCs for session listing, reattachment, and history retrieval
- MUST support per-role TTL and command history limits via ABAC attributes

## Non-Goals

- Terminal mode UI (sub-spec 2b)
- Chat mode UI, channels, or DMs (sub-spec 2c)
- Offline/PWA support (separate sub-spec)
- Session TTL admin dashboard (sub-spec 3 — portal)
- Redis or external cache implementation (interface only; Postgres first)

## Design Decisions

### Postgres-Backed Sessions with Cache-Aside Interface

Sessions MUST be stored in Postgres behind a `SessionStore` interface.

**Rationale:** The core design goal is tmux-style sessions that survive server
restarts and work across protocols. In-memory sessions cannot survive restarts.
Postgres provides durability without introducing new infrastructure. The
`SessionStore` interface enables a future cache-aside layer (Redis or in-memory)
if the Postgres round-trip becomes a bottleneck. The interface is the seam — no
consumer code changes when caching is added.

### Per-Role TTL and History Limits via ABAC

Session TTL and command history caps MUST be resolved per-role at session
creation time, not hardcoded.

**Rationale:** Different user classes need different limits. Guests should have
short TTLs (5 minutes) to free resources. Regular players get longer (30
minutes). Staff or scene runners may need hours. The ABAC system already provides
role-based attribute resolution. Session limits are attributes like any other.

### Replay-Before-Live Merge Pattern

On reconnect, `Subscribe` MUST replay missed events before switching to live
broadcast, with no gap between replay end and live start.

**Rationale:** The naive approach (replay, then subscribe) can miss events that
arrive between replay and subscription. The correct pattern: subscribe to the
broadcaster first, then concurrently drain the broadcaster channel into a local
slice while replaying from the event store. After replay completes, send any
buffered live events that were not already replayed (dedup by event ID). This
guarantees zero missed events — the same pattern used by event-sourced systems
for catch-up subscriptions.

**Important:** The existing broadcaster channel buffer is 100 events. During a
long replay, live events could overflow this buffer and be silently dropped. The
implementation MUST drain the broadcaster channel concurrently during replay (in
a goroutine), not sequentially after replay completes. This goroutine appends
received events to a thread-safe buffer that is drained after replay finishes.

### One Session Per Character, Multiple Connections

A character MUST have at most one session (active or detached) at any time. The
`sessions` table enforces this with a unique constraint on `(character_id)`
filtered to non-expired statuses.

A session MAY have multiple concurrent connections. Each connection is a separate
client (browser tab, telnet socket) subscribing to a subset of the session's
event streams. Examples:

- Tab 1: terminal mode (room + scene + channels + DMs, all interleaved)
- Tab 2: comms hub (scenes + channels + DMs, no room — grid navigation is
  terminal-only)
- Tab 3: telnet (same streams as terminal mode)

Each connection has its own `connection_id` and its own `StreamEvents` call.
The session tracks connections via a `connections` table (not a field on the
session row). The session transitions to `detached` only when the **last**
connection closes.

**Stream subscriptions per connection type:**

| Stream Type | Terminal / Telnet | Comms Hub |
| ----------- | ----------------- | --------- |
| Room (grid) | Yes               | No        |
| Active scene | Yes (one at a time) | Yes (can view multiple) |
| Channels    | Yes (inline)      | Yes (switchable) |
| DMs         | Yes (inline)      | Yes (switchable) |

**Rationale:** A character is a single entity in the game world — it cannot be
in two places simultaneously. But a player should be able to view their
character's activity through multiple interfaces concurrently. The comms hub
provides a richer messaging experience without replacing the terminal's spatial
gameplay. Multiple connections to one session avoids split-brain state while
enabling multi-view.

### Grid Presence: Phased In / Phased Out

A character's visibility on the grid depends on whether any terminal or telnet
connection is active. When connected only via the comms hub, the character is
"phased out" — still at their grid location but invisible to other players and
not receiving room events.

**States:**

- **Phased in:** At least one terminal or telnet connection exists. Character is
  visible on the grid. Room events (say, pose, arrive, leave) are delivered.
  Other players see the character in the room.
- **Phased out:** Only comms hub connections (or no connections). Character
  remains at their grid location but is invisible. Room events are not delivered
  to this session. Other players do not see the character in the room.

**Transitions:**

- Terminal/telnet connection opens (first one) → phase in: emit arrive event
  (`"<name> materializes."` or similar), start room event delivery.
- Last terminal/telnet connection closes (comms hub still open) → phase out:
  emit leave event (`"<name> fades from view."`), stop room event delivery.
  Session stays `active` (comms hub is still connected).
- All connections close → detached (existing TTL behavior). Character remains
  phased out.
- Reconnect with terminal → phase in again (arrive event, resume room events).

**Rationale:** This avoids the "idle character in the room" problem (option B)
and the "special OOC room" relocation complexity (option A). The character stays
at its location, simplifying reconnect (no need to remember and restore the
previous location). The phase in/out is a lightweight flag on the session, not a
room change. The arrive/leave events make the transition visible to other players
in the room.

### Two-Phase Login with Character Selection

Login MUST be split into player authentication and character selection. A player
authenticates once and receives a player token, then selects which character to
play. This enables character switching without re-entering credentials.

**Flow:**

```text
1. Authenticate(username, password) → player_token + character list
2. If exactly one character: client auto-selects (no picker shown)
   If multiple characters: client shows picker
3. SelectCharacter(player_token, character_id) → session_id
   - Detached session exists → reattach (reattached=true)
   - No session → create new (reattached=false)
4. StreamEvents(session_id, replay_from_cursor=reattached) → event stream
```

**Auto-connect preference:** Players with multiple characters can set a
preference for how character selection works on login:

| Preference | Behavior |
| ---------- | -------- |
| `last_connected` | Auto-select the most recently played character |
| `default` | Auto-select a specific character set as default |
| `ask` | Always show the character picker |

This is stored as a player-level setting (`auth.Player.Preferences`). When
`Authenticate` returns the character list, the response includes a
`auto_connect_character_id` field — set to the auto-selected character if the
preference resolves to one, empty if the player should be prompted. The client
can then call `SelectCharacter` immediately or show the picker.

```protobuf
message AuthenticateResponse {
  // ... existing fields ...
  string auto_connect_character_id = 5; // empty = show picker
}
```

**Single character:** If the player has exactly one character, auto-select
regardless of preference (no picker needed).

**Guest flow:** Guests skip character selection — `Authenticate` with
`username="guest"` creates an ephemeral character and returns the session
directly (backward compatible with sub-spec 1 behavior).

**Character switching (web):** The web UI holds the player token and presents a
character picker in the UI chrome. Switching calls `SelectCharacter` with the new
character ID. The old character's session detaches (if no other connections
remain). The page does not reload — only the session context and stream
subscriptions change. Both terminal and comms hub tabs switch together.

**Character switching (telnet):** The player authenticates and is presented with
a character list. They select one. If they open a second telnet connection and
select the same character, the latest connection wins — the previous connection
receives a "disconnected: logged in from another location" message and is
terminated.

**Player token:** An opaque server-generated token (ULID) stored in a
`player_tokens` table with a TTL (e.g., 24 hours). Not a JWT — the server
validates by lookup. This keeps token revocation simple (delete the row).

```sql
CREATE TABLE player_tokens (
    token       TEXT PRIMARY KEY,
    player_id   TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_player_tokens_player ON player_tokens (player_id);
```

### Concurrent Login Protection

Concurrent login attempts for the same character MUST be serialized. The
reattach operation uses optimistic concurrency: `UPDATE sessions SET status =
'active' ... WHERE character_id = $1 AND status = 'detached'` and checks
rows-affected. If zero rows affected, another client won the race and this
login attempt returns an error ("session already active").

**Rationale:** Without serialization, two clients can both find the same
detached session and both believe they reattached successfully. Optimistic CAS
via the WHERE clause is simpler than pessimistic locking and handles the common
case (no contention) with zero overhead.

### Reconnect Indicator in Stream

Replayed events MUST be distinguishable from live events in the stream response.

**Rationale:** The client needs to render a visual divider showing where the
disconnect happened and where replayed events begin. Without a server-side
signal, the client would have to guess based on timestamp gaps, which is fragile.
A boolean `replayed` field on `StreamEventsResponse` and a `replay_complete`
marker provide explicit signaling.

## Architecture

### Session Lifecycle

```text
                 Login / Reattach
                      │
                      ▼
    ┌─────────────────────────────────┐
    │            ACTIVE               │
    │  1+ connections open, streaming │
    │  events. Multiple tabs/protocols│
    └───────────┬─────────────────────┘
                │ Last connection closes
                │ (all tabs closed, all streams ended,
                │  all telnet sockets dropped)
                ▼
    ┌─────────────────────────────────┐
    │           DETACHED              │
    │  No client. TTL timer starts.   │
    │  Events accumulate in store.    │
    │  Cursors track last-seen event. │
    └───────────┬──────────┬──────────┘
                │          │ TTL expires
                │          ▼
                │  ┌───────────────┐
                │  │   EXPIRED     │
                │  │  Cleanup:     │
                │  │  leave events │
                │  │  guest release│
                │  └───────────────┘
                │
                │ Client reconnects
                │ (Login with existing character
                │  or Reattach with session ID)
                ▼
    ┌─────────────────────────────────┐
    │         ACTIVE (resumed)        │
    │  Replay missed events from      │
    │  cursor. Then switch to live.   │
    └─────────────────────────────────┘
```

**TTL defaults (overridden by ABAC per-role):**

| Role    | Session TTL | Command History Cap |
| ------- | ----------- | ------------------- |
| Guest   | 5 minutes   | 50 commands         |
| Player  | 30 minutes  | 500 commands        |
| Builder | 2 hours     | 1000 commands       |
| Staff   | 8 hours     | 2000 commands       |

### Session State in Postgres

```sql
CREATE TABLE sessions (
    id              TEXT PRIMARY KEY,
    character_id    TEXT NOT NULL,
    character_name  TEXT NOT NULL,
    location_id     TEXT NOT NULL,
    -- connection_id removed: connections tracked in separate table
    is_guest        BOOLEAN NOT NULL DEFAULT false,
    status          TEXT NOT NULL DEFAULT 'active',
    grid_present    BOOLEAN NOT NULL DEFAULT false,
    event_cursors   JSONB NOT NULL DEFAULT '{}',
    command_history TEXT[] NOT NULL DEFAULT '{}',
    ttl_seconds     INTEGER NOT NULL DEFAULT 1800,
    max_history     INTEGER NOT NULL DEFAULT 500,
    detached_at     TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_sessions_active_character
    ON sessions (character_id) WHERE status IN ('active', 'detached');
CREATE INDEX idx_sessions_status ON sessions (status) WHERE status = 'detached';

CREATE TABLE session_connections (
    id          TEXT PRIMARY KEY,       -- connection ULID
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    client_type TEXT NOT NULL,          -- 'terminal', 'comms_hub', 'telnet'
    streams     TEXT[] NOT NULL,        -- streams this connection subscribes to
    connected_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_session_connections_session ON session_connections (session_id);
```

The `ttl_seconds` and `max_history` fields are resolved from ABAC at session
creation and stored per-session. This avoids re-querying ABAC on every operation
and ensures the limits don't change mid-session if policies update.

### SessionInfo Struct

The `SessionInfo` struct moves from `internal/grpc/server.go` to
`internal/session/session.go` (new package). The existing `grpc.SessionInfo`
and `grpc.InMemorySessionStore` are replaced by the new types. The
`CoreServer.sessionStore` field changes type to the new `session.Store`
interface.

```go
// package session

type Status string

const (
    StatusActive   Status = "active"
    StatusDetached Status = "detached"
    StatusExpired  Status = "expired"
)

type Info struct {
    CharacterID   ulid.ULID
    CharacterName string
    LocationID    ulid.ULID
    IsGuest       bool
    Status        Status
    GridPresent   bool
    EventCursors  map[string]ulid.ULID
    TTLSeconds    int
    MaxHistory    int
    DetachedAt    *time.Time
    ExpiresAt     *time.Time
    CreatedAt     time.Time
}

// Connection represents a single client attached to a session.
type Connection struct {
    ID         ulid.ULID
    SessionID  string
    ClientType string   // "terminal", "comms_hub", "telnet"
    Streams    []string // event streams this connection subscribes to
}

```

### Store Interface

```go
// package session

type Store interface {
    Get(ctx context.Context, id string) (*Info, error)
    Set(ctx context.Context, id string, info *Info) error
    Delete(ctx context.Context, id string) error

    FindByCharacter(ctx context.Context, characterID ulid.ULID) (*Info, error)
    ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*Info, error)
    ListExpired(ctx context.Context) ([]*Info, error)

    UpdateStatus(ctx context.Context, id string, status Status,
        detachedAt *time.Time, expiresAt *time.Time) error
    ReattachCAS(ctx context.Context, id string) (bool, error)
    UpdateCursors(ctx context.Context, id string, cursors map[string]ulid.ULID) error
    AppendCommand(ctx context.Context, id string, command string, maxHistory int) error
    GetCommandHistory(ctx context.Context, id string) ([]string, error)

    // Connection management
    AddConnection(ctx context.Context, conn *Connection) error
    RemoveConnection(ctx context.Context, connectionID ulid.ULID) error
    CountConnections(ctx context.Context, sessionID string) (int, error)
}
```

`ReattachCAS` performs the optimistic reattach: `UPDATE sessions SET status =
'active' WHERE id = $1 AND status = 'detached'`. Returns `true` if the row
was updated, `false` if another client already reattached. After a successful
reattach, the caller registers a new connection via `AddConnection`.

`CountConnections` is used during disconnect to determine whether the session
should transition to `detached` — only when the count drops to zero.

`ListByPlayer` replaces the original `ListDetached` — scoped to a specific
player for the `ListSessions` RPC (see authorization section below).

**Implementation notes:**

- `PostgresSessionStore` lives in `internal/store/session_store.go`
- Uses the same `*pgxpool.Pool` as `PostgresEventStore`
- `UpdateCursors` uses Postgres `jsonb_set` for efficient partial updates
- `AppendCommand` enforces the cap using Postgres array slicing:
  `UPDATE sessions SET command_history = command_history[
  greatest(1, array_length(command_history,1) - $2 + 2) :
  array_length(command_history,1)] || ARRAY[$1]`
  (keeps the most recent `max_history` entries)
- `InMemorySessionStore` moves to `internal/session/memstore.go` as a test
  helper implementing `Store`

**Migration from existing code:** The `grpc.SessionInfo` struct and
`grpc.InMemorySessionStore` type in `internal/grpc/server.go` are deleted.
`CoreServer.sessionStore` changes type from the informal in-memory interface
to `session.Store`. All call sites in `server.go` update to the new method
signatures (adding `ctx`, handling `error` returns).

**Cache-aside readiness:** A future `CachedSessionStore` wraps `PostgresSessionStore`:

```go
type CachedSessionStore struct {
    primary SessionStore
    cache   SessionCache
}
```

Reads check cache first, writes update both. The `SessionStore` interface does
not change. No consumer code changes.

### Event Replay on Reconnect

When `Subscribe` is called with `replay_from_cursor = true`:

```text
1. Look up session, get EventCursors from SessionInfo
2. Subscribe to broadcaster channels for live events
3. Start a goroutine that drains the broadcaster channel into a
   thread-safe buffer (slice + mutex). This prevents broadcaster
   channel overflow during replay.
4. For each subscribed stream:
   a. Call EventStore.Replay(stream, afterID=cursor, limit=maxReplay)
   b. Send each replayed event with replayed=true
   c. Track replayed event IDs in a set
5. Stop the draining goroutine, collect the buffered live events
6. For each buffered live event:
   a. Skip if event ID was already replayed (dedup)
   b. Otherwise send with replayed=false
7. Send StreamEventsResponse{replay_complete=true}
8. Switch to normal live forwarding (read directly from broadcaster channel)
```

**Empty cursors:** If `EventCursors` is empty (fresh session that detached before
receiving any events), replay is skipped — the handler sends `replay_complete`
immediately and proceeds to live forwarding.

**Gap-free guarantee:** By subscribing to the broadcaster *before* replaying
from the store, events that arrive during replay are buffered. After replay
completes, the buffer is drained with deduplication (events already sent during
replay are skipped). This ensures zero missed events.

**Replay limit:** Configurable per server (default 1000 events per stream). If
more events accumulated than the limit, only the most recent N are replayed. The
client receives a truncation indicator (a system event or metadata field) so it
can show "... older events omitted ...".

**Deduplication:** Replayed events have ULIDs. Buffered live events also have
ULIDs. During the drain phase, skip any buffered event whose ID was already sent
during replay. After the buffer is drained, dedup is no longer needed (all
subsequent events are guaranteed new).

### Proto Changes

**WebService changes:**

The existing `Login` RPC is replaced by a two-phase flow. `Login` remains as a
convenience for guest access only.

```protobuf
// Phase 1: Authenticate player, get token + character list
rpc Authenticate(AuthenticateRequest) returns (AuthenticateResponse);

// Phase 2: List player's characters (can also use Authenticate response)
rpc ListCharacters(ListCharactersRequest) returns (ListCharactersResponse);

// Phase 3: Select character, create or reattach session
rpc SelectCharacter(SelectCharacterRequest) returns (SelectCharacterResponse);

// Convenience: Guest login (combines authenticate + create character + session)
rpc Login(LoginRequest) returns (LoginResponse);

// Session management
rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);
rpc GetCommandHistory(GetCommandHistoryRequest) returns (GetCommandHistoryResponse);

enum SessionStatus {
  SESSION_STATUS_UNSPECIFIED = 0;
  SESSION_STATUS_ACTIVE = 1;
  SESSION_STATUS_DETACHED = 2;
}

message AuthenticateRequest {
  string username = 1;
  string password = 2;
}

message AuthenticateResponse {
  bool success = 1;
  string player_token = 2;        // opaque token for subsequent RPCs
  string error_message = 3;
  repeated CharacterSummary characters = 4;  // player's characters
  // If exactly one character: client SHOULD auto-select it
  // If zero characters: client shows character creation (future)
}

message CharacterSummary {
  string character_id = 1;
  string character_name = 2;
  SessionStatus session_status = 3; // active/detached/unspecified (no session)
  string location_name = 4;         // last known location
}

message ListCharactersRequest {
  string player_token = 1;
}

message ListCharactersResponse {
  repeated CharacterSummary characters = 1;
}

message SelectCharacterRequest {
  string player_token = 1;
  string character_id = 2;
}

message SelectCharacterResponse {
  bool success = 1;
  string session_id = 2;
  string character_name = 3;
  string error_message = 4;
  bool reattached = 5;  // true if resuming a detached session
}

message ListSessionsRequest {
  // Authenticated via session_id of the caller's active session,
  // or via a future auth header. Returns only sessions belonging
  // to the same player.
  string session_id = 1;
}

message ListSessionsResponse {
  repeated SessionSummary sessions = 1;
}

message SessionSummary {
  string session_id = 1;
  string character_name = 2;
  string location_name = 3;
  SessionStatus status = 4;
  google.protobuf.Timestamp detached_at = 5;
  google.protobuf.Timestamp expires_at = 6;
}

// Reattach is subsumed by SelectCharacter — no separate RPC needed.

message GetCommandHistoryRequest {
  // Only the owning session or staff can retrieve command history.
  string session_id = 1;
}

message GetCommandHistoryResponse {
  repeated string commands = 1;
}
```

**Authorization:** `ListSessions` scopes results to the authenticated player.
The server resolves the caller's player identity from the provided `session_id`
and returns only that player's sessions. `GetCommandHistory` enforces that the
caller owns the requested session (or has staff-level access via ABAC).

**StreamEventsResponse changes:**

```protobuf
message StreamEventsResponse {
  GameEvent event = 1;
  bool replayed = 2;
  bool replay_complete = 3;
}
```

**Core SubscribeRequest changes:**

```protobuf
message SubscribeRequest {
  RequestMeta meta = 1;
  string session_id = 2;
  repeated string streams = 3;
  bool replay_from_cursor = 4;
}
```

### Login Flow Changes

**Current flow:** Login always creates a new session.

**New flow:**

1. Authenticate credentials
2. Check for existing detached session for this character
   (`SessionStore.FindByCharacter`)
3. If detached session found: reattach it (update status to `active`, set
   `connection_id`, clear `detached_at`/`expires_at`). Return existing session ID.
4. If no detached session: create new session as before. Resolve TTL and
   max\_history from ABAC attributes.
5. Client calls `StreamEvents` with `replay_from_cursor=true` if reattaching

The client knows it's a reattach (vs fresh login) because it can compare the
returned session ID against a previously stored one, or check for the presence
of `replayed` events in the stream.

### Disconnect Flow Changes

**Current flow:** `Disconnect` emits leave event and deletes session immediately.

**New flow:**

1. `Disconnect` RPC received (or stream closes without prior Disconnect)
2. Remove this connection from `session_connections`
3. Check `CountConnections` for the session
4. If connections remain (other tabs/protocols still open): done — session stays
   `active`
5. If zero connections: transition session to `detached` status. Set
   `detached_at = now()`, `expires_at = now() + ttl_seconds`
6. Do NOT emit leave event yet — the player may reconnect
7. A background goroutine (session reaper) periodically checks for expired
   sessions:
   - Emit leave events
   - Release guest characters
   - Delete session record (or mark as `expired`)

**Explicit quit:** If the player sends a `quit` command, the session is
terminated immediately (leave event + delete), not detached. This is an explicit
"I'm done" signal, not a disconnect.

### Session Reaper

A background goroutine in the core process periodically scans for expired
detached sessions:

```go
func (r *SessionReaper) Run(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            r.reapExpired(ctx)
        }
    }
}
```

`reapExpired` queries expired sessions using `SELECT ... FOR UPDATE SKIP LOCKED`
to prevent duplicate processing in multi-instance deployments:

```sql
SELECT * FROM sessions
WHERE status = 'detached' AND expires_at < now()
FOR UPDATE SKIP LOCKED
```

For each locked row, the reaper emits leave events, cleans up guest characters,
and deletes the session record — all within the same transaction. `SKIP LOCKED`
ensures that if two reaper instances run concurrently, each processes different
expired sessions with no duplicates.

### Telnet Integration

The telnet gateway handler (`internal/telnet/gateway_handler.go`) MUST use the
same `SessionStore` for session operations. When a telnet client disconnects
(connection drops or quit):

- **Connection drop:** Session transitions to `detached` (same as web)
- **Explicit quit:** Session terminated immediately

When a telnet client connects and authenticates, the same `FindByCharacter`
lookup checks for detached sessions to reattach. Command history from web
sessions carries over to telnet and vice versa.

## Config

New configuration fields:

```yaml
core:
  # Fallback session TTL when ABAC returns no role-specific value.
  # ABAC policies override this per-role (see TTL defaults table above).
  session_ttl: "30m"
  # Fallback max command history when ABAC returns no role-specific value.
  session_max_history: 500
  # Max events replayed on reconnect per stream (server-wide, not per-role)
  session_max_replay: 1000
  # How often the reaper checks for expired sessions
  session_reaper_interval: "30s"
```

## Testing Strategy

### Unit Tests

- `PostgresSessionStore`: CRUD operations, `FindByCharacter`, `ListDetached`,
  cursor updates, command history append with cap enforcement. Uses
  testcontainers PostgreSQL.
- Replay + live merge logic: mock event store returns N historical events, mock
  broadcaster sends M live events during replay. Verify: all events delivered,
  no duplicates, correct `replayed` flags, `replay_complete` marker sent.
- Session reaper: mock store with expired sessions, verify leave events emitted,
  sessions deleted.

### Integration Tests (Ginkgo/Gomega)

- **Full reconnect flow:** Connect as guest → send commands → disconnect →
  reconnect → verify replayed events have `replayed=true` → verify
  `replay_complete` marker → verify live events after replay are `replayed=false`
  → verify command history returned by `GetCommandHistory`.
- **Cross-protocol:** Connect via telnet → send commands → disconnect → reconnect
  via web → verify command history includes telnet commands → verify event
  replay includes telnet-era events.
- **TTL expiration:** Connect → disconnect → wait for TTL → verify leave event
  emitted → verify session deleted → verify reconnect creates new session.
- **Explicit quit:** Send quit command → verify immediate leave event → verify
  session deleted (not detached).
- **Concurrent reattach:** Two clients login as same character simultaneously →
  one succeeds, the other receives "session already active" error.
- **Broadcaster overflow during replay:** Connect, generate >100 events while
  replaying → verify no events lost (concurrent drain goroutine handles it).
- **Empty cursors on reconnect:** Connect → disconnect immediately (before any
  events) → reconnect → verify `replay_complete` sent immediately, no replay
  attempted.
- **Reaper idempotency:** Two reaper instances running concurrently → each
  processes different sessions (`SKIP LOCKED`), no duplicate leave events.

## Dependencies

### Go

- No new dependencies. Uses existing `pgxpool`, `oops`, `ulid`.

### Database

- New `sessions` table migration
- New indexes on `character_id` and `status`

## Future Sub-Specs

This sub-spec establishes the session persistence layer. The next sub-specs
build on it:

- **Sub-spec 2b: Terminal Mode** — ANSI rendering, scrollback (via event
  replay), command input with server-side history, info sidebar. The terminal
  is a thin renderer over the session's event stream.
- **Sub-spec 2c: Chat Mode + Channels/DMs** — Slack-style conversation switcher,
  channels and DM server infrastructure, scene integration. Uses the same
  session persistence for cross-mode continuity.

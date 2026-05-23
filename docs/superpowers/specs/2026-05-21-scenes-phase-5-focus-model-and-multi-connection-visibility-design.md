# Scenes Phase 5: Focus Model + Multi-Connection Visibility

**Bead:** `holomush-5rh.14`
**Predecessors:** `holomush-5rh.13` Phase 4 (event streams + pose order), merged PR #4153 + cleanup PR #4156 (2026-05-21)
**Parent design (v2):** [scenes-and-rp-design-v2.md](2026-04-06-scenes-and-rp-design-v2.md) — Phase 5 binds to §2 (Membership vs Focus), §3.2-3.4 (routing), §6.3 (focus commands), §11 (plugin↔server contract gap — closed by this design)
**Substrate contract:** [substrate-contract](2026-05-16-social-spaces-substrate-contract.md) — INV-S3 (Go+Lua hostfunc parity), INV-S9 (privacy boundary is participant list)
**History-scope privacy:** [history-scope-privacy](2026-05-17-history-scope-privacy-design.md) — iwzt.15 Tier 2 filter-at-delivery merged PR #4155 (2026-05-21)

## 1. Goal

Implement per-connection focus tracking and multi-connection visibility for scenes. Close the v2 §11 plugin↔server focus-contract gap by extending `PluginHostService` with three new RPCs. Add `scene focus`/`scene grid`/`scene list` subcommands. Wire focus changes to JetStream subscription churn so a connection only receives events from its focused stream(s) + always-on character-level streams.

## 2. Non-goals

| Out of scope | Tracked elsewhere |
|---|---|
| Web client focus UI (tabs, toggles) | Phase 9 |
| Channel focus | `holomush-0sc.12` channels rework |
| Location-stream dot-style migration | `holomush-rops` |
| `scene_idle_nudge` background trigger | `holomush-fux3` |
| Forum view focus mapping | Forum epic (`holomush-djj`) |
| Whisper/page per-character subscription mechanics | TBD — not Phase 5 |

## 3. Domain decisions (confirmed in brainstorming)

| # | Decision | Rationale |
|---|---|---|
| D1 | Plugin↔server focus contract = **extend PluginHostService with new RPCs** (substrate-hosted; plugin is gRPC client) | Mirrors existing pattern (JoinFocus/LeaveFocus/PresentFocus already there); INV-S3 Go+Lua parity preserved |
| D2 | Reconnect focus restoration = via `session.Info.PresentingFocus` | Preserves telnet single-pane UX (scene focus survives network blip); leverages existing field; bead's "grid default" still holds when PresentingFocus is nil (genuinely new sessions) |
| D3 | RPC shape = **three separate RPCs**: `SetConnectionFocus`, `AutoFocusOnJoin`, `IsAnyConnFocused` | Semantic clarity per RPC; clean Lua hostfunc bindings (no oneof); AutoFocusOnJoin internally fans out as N×SetConnectionFocus filtered by ClientType |
| D4 | Substrate-side membership validation = **`session.Info.FocusMemberships` lookup** (not a host→plugin RPC) | `IsParticipant` is a core-scenes-internal Go method (`plugins/core-scenes/store.go:1250`), not a substrate-callable RPC. FocusMemberships is server-authoritative — populated by the plugin's existing `JoinFocus` call after `JoinScene` succeeds. Substrate already owns this data; no new transport required. FocusMemberships ⊆ scene_participants (plugin's join flow always pairs JoinScene→JoinFocus at `plugins/core-scenes/commands.go:357-384`). |
| D5 | Per-Connection JetStream filtering = **extend `SessionStreamRegistry` internally** with per-Connection routing (`SendToConnection`); not a plugin-facing API | Today `AddSessionStream`/`RemoveSessionStream` broadcast session-wide via `SessionStreamRegistry` (`internal/grpc/stream_registry.go:79-95`). Phase 5's multi-conn visibility requires per-Connection deltas. Internal extension keeps the plugin surface unchanged (the 3 new RPCs above) and isolates the per-Conn mechanism inside the substrate's subscription_router. `SubscribeRequest` already carries `ConnectionId` + `ClientType` (verified at `internal/grpc/subscribe_server_test.go:213-247` + telnet/web gateways) — no protocol change required. |
| D6 | ULID encoding boundary = **proto uses `bytes`; Lua hostfunc accepts string ULIDs and parses at the boundary** | Matches existing `stdlib_focus.go::parseFocusKey` convention (`internal/plugin/hostfunc/stdlib_focus.go:84-93`). Lua-side ergonomics (strings); proto wire (bytes); substrate-side decoder is the converter. |
| D7 | Concurrency model = **single combined mutator with one Store-side lock acquisition** (no new Coordinator-level lock) | `defaultCoordinator` has no mutex today; locking lives inside the Store. **MemStore** uses a store-wide `sync.RWMutex` (`internal/session/memstore.go:18` field `m.mu`); `UpdateFocusMemberships` and the new `UpdateSessionConnection` (§5.1.1) both acquire it for the full duration of the mutator callback. **Postgres** uses transactional `FOR UPDATE` on the relevant rows (`sessions` and `session_connections`) — the new Store method opens a single transaction that locks both rows in canonical order (D11). Phase 5's operation is a single mutator that receives `(Info, Connection)` snapshots and returns the updated pair atomically — `Connection.FocusKey` and `Info.PresentingFocus` cannot be observed in mismatched states. |
| D8 | AutoFocusOnJoin clobber rule = **skip connections whose `FocusKey` is already non-nil and different** from the target | Preserves the user's most recent explicit focus. A web tab that just ran `scene focus #B` should not be silently re-focused to `scene_id` because someone (or something) auto-focused them onto scene `scene_id`. Implementation: AutoFocusOnJoin's inner per-conn mutator no-ops when `current.FocusKey != nil && *current.FocusKey != target` — caller observes the conn in response `skipped_connection_ids`. |
| D9 | `PresentingFocus` write rule = **only terminal/telnet `scene focus #<id>` explicit set updates it** | `PresentingFocus` is the session-level "where to land on reconnect" pointer, primarily for telnet single-pane UX (`session.go:174-178`). Web tabs (`comms_hub`) are stateless and default to grid on connect; their focus changes should NOT flip the telnet reconnect target. Phase 5 narrows the write trigger to `ClientType ∈ {terminal, telnet}` explicit `scene focus #<id>` commands. `scene grid` does NOT modify `PresentingFocus` (see D10). |
| D10 | `scene grid` does NOT clear `PresentingFocus` | Reviewer worked example: player on telnet in scene A runs `scene grid` to glance at room, then network blip, then reconnect — under "scene grid clears PresentingFocus" they'd land on grid, losing their scene context. Under D10 they land back on scene A. `scene grid` is a per-connection focus switch (current Connection's `FocusKey → nil`), not a session-level reset. Only explicit `scene leave #X` (whose I-10 already at `internal/grpc/focus/leave.go:14-15` clears PresentingFocus when the leave target matched it) ever clears `PresentingFocus` non-explicitly. |
| D11 | Postgres lock-acquisition order = **`sessions` row FIRST, then `session_connections` row(s)** (canonical order to prevent deadlock) | The new `UpdateSessionConnection` impl needs to lock both rows together (read `sessions.focus_memberships`, mutate `session_connections.focus_key` + `sessions.presenting_focus`). Concurrent `LeaveFocus` locks `sessions` only (`session_store.go:680`) — no deadlock with that pair. But two concurrent `UpdateSessionConnection` calls on the same session for different connections could deadlock if each takes the rows in different order; D11 makes the order canonical. |

## 4. Architecture

### 4.1 Components touched

| Where | Change |
|---|---|
| `internal/session/session.go::Connection` | **Add** `FocusKey *FocusKey` field — per-connection focus pointer; nil = grid focus. Mutation via the combined `SessionConnectionMutator` sentinel type (§5.1.1) |
| `internal/session/session.go::Info.PresentingFocus` | **Extend** write semantics per D9 — only terminal/telnet `scene focus #<id>` explicit set updates this field; `scene grid` leaves it alone per D10 |
| `internal/store/migrations/00000N_connection_focus_key.up.sql` (NEW) | Adds `focus_key JSONB NULL` column to `session_connections`. Paired `.down.sql` drops the column. `JSONB` shape matches the `FocusMembership` JSONB schema already used by `sessions.focus_memberships` (`internal/store/migrations/000001_baseline.up.sql:226-234` for existing table; `:NNN` for focus_memberships precedent) |
| `internal/session/session.go::Store` interface | **Add** `UpdateSessionConnection(ctx, sessionID, connectionID, SessionConnectionMutator) error` — single mutator covering BOTH `Info.PresentingFocus` AND `Connection.FocusKey` writes under one lock acquisition (D7). Also add `ListConnectionsBySession(ctx, sessionID) ([]*Connection, error)` (no such enumerator exists today — `internal/session/memstore.go:257-282` has only `Count*`) |
| `internal/session/memstore.go` + `internal/store/session_store.go` | **Implement** the two new Store methods. MemStore acquires the existing store-wide `m.mu` `sync.RWMutex` (`memstore.go:18`) for the mutator callback duration. Postgres opens a single transaction that locks `sessions` row first, then `session_connections` row (D11 canonical order) via `FOR UPDATE` mirroring `UpdateFocusMemberships`'s pattern (`session_store.go:668-755`) |
| `internal/grpc/focus/coordinator.go` | **Extend** Coordinator interface with `SetConnectionFocus(connID, FocusKey)` + `AutoFocusOnJoin(charID, sceneID) → response` + `RestoreFocusOnReconnect(sessionID, connID)`. Each delegates to a single `Store.UpdateSessionConnection` call (one lock acquisition; both fields atomic). **No new Coordinator-level lock.** Preserves I-6. |
| `internal/grpc/focus/subscription_router.go` (NEW) | Translates a focus change → per-Connection stream deltas via `SessionStreamRegistry.SendToConnection` (D5); enqueues IC replay per v2 §3.4 |
| `internal/grpc/stream_registry.go` | **Extend** `SessionStreamRegistry` with `RegisterConnection(sessionID, connectionID, ch)` + `SendToConnection(sessionID, connectionID, update)` — per-Connection targeting alongside the existing session-wide broadcast (D5) |
| `internal/grpc/server.go` (Subscribe handler) | Already receives `SubscribeRequest.ConnectionId` + `ClientType` per existing tests (`internal/grpc/subscribe_server_test.go:213-247`). **Switch** the registration call from `Register(sessionID, ch)` to `RegisterConnection(sessionID, connectionID, ch)`. |
| `api/proto/holomush/plugin/v1/plugin.proto` | **Add** 3 new RPCs to `PluginHostService` (D3) using `bytes` ULID fields per D6 |
| `internal/plugin/hostfunc/stdlib_focus.go` | **Extend** with Lua bindings for the 3 new RPCs (INV-S3 parity, D6 string-ULID convention) |
| `plugins/core-scenes/commands.go` | Replace `:840` placeholder (Phase 5 routing comment); implement `scene focus`, `scene grid`, `scene list` subcommands |

### 4.2 Bridge diagram

```text
                       SUBSTRATE (server-side, in-memory)
                       ┌──────────────────────────────────────┐
                       │ session.Info (per-session)           │
                       │   FocusMemberships []FocusMembership │
                       │     ← validation source (D4)         │
                       │   PresentingFocus *FocusKey          │
                       │                                      │
                       │ session.Connection (per-connection)  │
                       │   FocusKey *FocusKey   ← NEW         │
                       │   ClientType {terminal,telnet,...}   │
                       │   Streams []string                   │
                       │                                      │
                       │ internal/grpc/focus/Coordinator      │
                       │   (I-6: FocusMutator for memberships │
                       │   + SessionConnectionMutator for the │
                       │   combined Connection.FocusKey +     │
                       │   Info.PresentingFocus atomic write) │
                       │                                      │
                       │ internal/grpc/stream_registry        │
                       │   Register(sessionID, ch)             │
                       │   RegisterConnection(sID, cID, ch) ← NEW │
                       │   SendToConnection(sID, cID, upd)  ← NEW │
                       │     used by subscription_router (D5) │
                       └──────────────────────────────────────┘
                              ▲
        SetConnectionFocus    │
        AutoFocusOnJoin       │   (no host→plugin call;
        IsAnyConnFocused      │    membership validated
                              │    against FocusMemberships)
                              │
                       ┌──────┴──────────────────────────────┐
                       │ PluginHostService (existing surface  │
                       │ unchanged; +3 new RPCs per D3)       │
                       │ + SetConnectionFocus                 │
                       │ + AutoFocusOnJoin                    │
                       │ + IsAnyConnFocused                   │
                       └──────┬──────────────────────────────┘
                              │
                              │ gRPC (binary plugin)
                              │ Lua hostfunc (lua plugin) — INV-S3
                              ▼
                       ┌──────────────────────────────────────┐
                       │ core-scenes plugin                   │
                       │  scene focus #<id>: validate locally  │
                       │   + SetConnectionFocus(connID,FK)    │
                       │  scene grid: SetConnectionFocus(nil) │
                       │  scene list: read memberships +      │
                       │   IsAnyConnFocused per scene         │
                       │  scene join: AutoFocusOnJoin(char,X) │
                       │   AFTER JoinFocus has succeeded      │
                       │  notification emit: IsAnyConnFocused │
                       └──────────────────────────────────────┘
```

### 4.3 Per-Connection Subscription Routing (D5 mechanism)

Today `SessionStreamRegistry.Register(sessionID, ch)` keys subscribers by `sessionID`, and `Send(sessionID, update)` broadcasts to **every** channel registered under that session (`internal/grpc/stream_registry.go:79-95`). Phase 5 needs per-Connection deltas (alice's telnet focuses on grid, alice's web focuses on scene #42 — the two `Subscribe` calls must NOT mirror each other's filter updates).

**Mechanism extension** (substrate-internal; not plugin-facing):

```go
// stream_registry.go additions
type SessionStreamRegistry struct {
    // existing: channels map[sessionID] → set[chan ...]
    // NEW: connections map[sessionID] → map[connectionID] → chan ...
    connections map[string]map[ulid.ULID]chan<- sessionStreamUpdate
}

// NEW — register a channel with both session and connection identity.
// Used by CoreServer.Subscribe in Phase 5+ once SubscribeRequest carries
// connection_id. Old session-keyed Register stays for any caller that
// hasn't yet adopted per-connection identity.
func (r *SessionStreamRegistry) RegisterConnection(
    sessionID string, connectionID ulid.ULID, ch chan<- sessionStreamUpdate,
)

// NEW — target a single connection. Used by subscription_router on
// focus changes. Session-wide Send remains for character-level always-on
// streams that should reach every connection.
//
// Error contract (mirrors existing Send at stream_registry.go:84-91):
//   - oops.Code("CONNECTION_NOT_REGISTERED")  → no channel exists for
//     (sessionID, connectionID); caller's Subscribe may have just
//     deregistered (race with disconnect). Substrate-side
//     subscription_router treats this as a non-fatal no-op (the
//     subsequent reconnect's RestoreFocusOnReconnect will re-apply
//     the focus-managed subscription set).
//   - oops.Code("CONTROL_CHANNEL_FULL") → channel buffer exhausted
//     (analog of existing CONTROL_CHANNEL_FULL for Send); caller
//     logs at WARN; the connection's filter set transiently lags.
//   - nil → delivered.
func (r *SessionStreamRegistry) SendToConnection(
    sessionID string, connectionID ulid.ULID, update sessionStreamUpdate,
) error
```

**`subscription_router` algorithm** on a focus change (`SetConnectionFocus(connID, newFocusKey)`):

1. Read current `Connection.FocusKey` (old).
2. Compute desired stream set per §5.3 (focus-managed subset only — always-on streams are not touched).
3. Compute deltas: `streamsToAdd = desired \ current`, `streamsToRemove = current \ desired`.
4. For each `s` in `streamsToRemove`: `registry.SendToConnection(sessionID, connID, {stream: s, add: false})`.
5. For each `s` in `streamsToAdd`: `registry.SendToConnection(sessionID, connID, {stream: s, add: true})`.
6. Update `Connection.Streams` to reflect new focus-managed subset (union with any pre-existing always-on entries).
7. If new focus is a scene, enqueue IC replay for the unseen window (v2 §3.4).

**Subscribe handler update** (`CoreServer.Subscribe` at `internal/grpc/server.go`): `SubscribeRequest.ConnectionId` + `ClientType` are already populated by all current callers (verified at `internal/grpc/subscribe_server_test.go:213-247`; telnet gateway and web handler tests confirm both fields). The Subscribe handler simply switches its registry call from `Register(sessionID, ch)` to `RegisterConnection(sessionID, connectionID, ch)`. The session-keyed `Register` remains in the registry's surface for any non-Subscribe caller, but is no longer the path used here. No new client adoption gate.

**Plugin-facing API unchanged**: plugins call `SetConnectionFocus` / `AutoFocusOnJoin` / `IsAnyConnFocused`. The substrate's subscription_router handles the per-Conn deltas internally — plugins never call `AddSessionStream`/`RemoveSessionStream` for focus-driven subscriptions.

## 5. Data model

### 5.1 `session.Connection.FocusKey *FocusKey`

```go
type Connection struct {
    ID          ulid.ULID
    SessionID   string
    ClientType  string
    Streams     []string
    FocusKey    *FocusKey   // NEW: nil = grid focus; non-nil = focused context
    ConnectedAt time.Time
}
```

**Semantics:**

- `nil` (default for new Connection) = the connection is focused on its character's current grid location.
- Non-nil = the connection is focused on a specific context (`Kind == FocusKindScene` is the only kind in Phase 5; channels add `FocusKindChannel` later).
- **Mutated only via Coordinator** (I-6 carried forward); the field is data, but no other package writes it.

### 5.1.1 `SessionConnectionMutator` — single combined mutator

The existing `FocusMutator` (`internal/session/session.go:91-132`) mutates `FocusMemberships` + `PresentingFocus` only; it cannot atomically also mutate `Connection.FocusKey`. Phase 5 introduces a **single combined mutator** whose callback receives full read-only snapshots of `Info` AND `Connection` and returns updates to both — applied under ONE Store-lock acquisition so the two fields cannot be observed in disagreement (D7):

```go
// SessionConnectionMutator's unexported sentinel blocks construction
// outside internal/grpc/focus, enforcing I-6 (server-authoritative
// mutation) for the combined session-level + connection-level focus
// state that Phase 5 operations mutate atomically.
type sessionConnectionMutatorSentinel struct{}

type SessionConnectionMutator struct {
    _      sessionConnectionMutatorSentinel
    Mutate func(info Info, conn Connection) (nextInfo Info, nextConn Connection, err error)
}

// NewSessionConnectionMutator parallels NewFocusMutator. The sentinel
// pattern makes the constructor callable from any package but enforces
// the "only grpc/focus is the legitimate caller" rule via lint +
// compile-fail documentation test (same enforcement as FocusMutator
// at internal/session/focus_mutator_test.go::TestFocusMutatorRequiresConstructor).
func NewSessionConnectionMutator(
    fn func(info Info, conn Connection) (nextInfo Info, nextConn Connection, err error),
) SessionConnectionMutator
```

The Store interface gains:

```go
type Store interface {
    // existing UpdateFocusMemberships, etc...

    // UpdateSessionConnection runs the mutator under one Store-side
    // lock acquisition. The mutator receives coherent snapshots of
    // both Info and Connection; its returned (nextInfo, nextConn)
    // pair is written atomically. Phase 5 uses this for
    // SetConnectionFocus, AutoFocusOnJoin's inner per-conn mutation,
    // and RestoreFocusOnReconnect — all of which need to write
    // Connection.FocusKey + (conditionally) Info.PresentingFocus
    // together.
    //
    // MemStore impl: acquires the store-wide m.mu sync.RWMutex
    // (memstore.go:18) for the callback duration — same lock that
    // protects UpdateFocusMemberships, so the two operations
    // serialize.
    //
    // Postgres impl: opens a single transaction; locks rows in
    // canonical order (D11): sessions row first via FOR UPDATE,
    // then session_connections row via FOR UPDATE. The mutator's
    // returned values are written via UPDATE statements before
    // COMMIT.
    UpdateSessionConnection(ctx context.Context, sessionID string, connectionID ulid.ULID, m SessionConnectionMutator) error

    // ListConnectionsBySession returns the snapshot of all active
    // Connections for a session. Used by AutoFocusOnJoin's fan-out
    // enumeration. Today MemStore exposes only CountConnections /
    // CountConnectionsByType (memstore.go:257-282); list-by-session
    // is added.
    ListConnectionsBySession(ctx context.Context, sessionID string) ([]*Connection, error)
}
```

This makes the *operation* atomic, not just individual fields. INV-P5-7 (§10) is restated accordingly. The mutator-receives-snapshot pattern eliminates the read-then-write race the round-2 reviewer flagged: validation against `FocusMemberships` happens inside the lock-held callback, not outside. The two-acquisition torn-state risk the round-3 reviewer flagged is eliminated by collapsing both writes into one mutator invocation.

### 5.2 `session.Info.PresentingFocus` write rules

`PresentingFocus` is the session-level "where to land on reconnect" pointer, primarily for telnet single-pane UX (`internal/session/session.go:174-178`). Per D9, only **terminal/telnet** ClientType explicit focus changes update it — web tabs (`comms_hub`) are stateless and default to grid on connect; their focus changes do not flip the telnet reconnect target.

The substrate MUST re-validate `FocusMemberships` contains the target before writing (preserves existing I-2: `PresentingFocus` must either be nil or reference an existing entry in `FocusMemberships`, enforced today at `internal/grpc/focus/present.go:28-32` for `PresentFocus`). Re-validation happens inside the FocusMutator callback under the per-session lock (D7).

| Trigger | PresentingFocus update | I-2 guard |
|---|---|---|
| `scene focus #X` succeeds on a connection with `ClientType ∈ {terminal, telnet}` | Set to `&FocusKey{scene, X}` | mutator re-validates `FocusMemberships` contains `{scene, X}` (D4 + D7) |
| `scene focus #X` succeeds on a `comms_hub` connection | **No write** (D9) | n/a |
| `scene grid` (any ClientType) | **No write** (D10 — `scene grid` is per-connection only; does not touch session-level reconnect target) | n/a |
| `AutoFocusOnJoin(char, X)` succeeds for ≥1 terminal/telnet connection | Set to `&FocusKey{scene, X}` | mutator re-validates `FocusMemberships` contains `{scene, X}` before write |
| `AutoFocusOnJoin` succeeds for only `comms_hub` connections (no terminal/telnet) | **No write** (D9) | n/a |
| Reconnect restoration loads from `PresentingFocus` | No write (read-only path) | (validation performed on read; see §8) |
| Explicit `scene leave #X` (where X was `PresentingFocus`) | Set to nil | I-10 already covers this at `internal/grpc/focus/leave.go:14-15` (the ONLY non-explicit clearing path; see D10) |

### 5.3 Subscription set per Connection

The **focus-managed subset** of `Connection.Streams` is a deterministic function of `(FocusKey, character_id)`. Out-of-scope additions to `Connection.Streams` (channels, whispers, other plugins' subscriptions added via `AddSessionStream`) co-exist additively and are not touched by the subscription_router. Subscription set composition:

| Source | Streams | Owner |
|---|---|---|
| **Focus-managed** (1-2 streams; updated by `subscription_router` on focus change) | `FocusKey == nil` → `location:<character_location_id>` ; `FocusKey.Kind == scene` → `events.<game_id>.scene.<sceneID>.ic` + `.ooc` | Phase 5 substrate routing |
| **Character always-on** (1 stream; written once at connection creation) | `notifications:<character_id>` | Phase 5 (write once); emit trigger is Phase 6 |
| **Plugin-added (out-of-Phase-5)** | channels, whispers, etc. | Other plugins via existing `AddSessionStream` RPCs; not in scope for this routing |

INV-P5-3 (§10) is scoped to the **focus-managed subset only** — the function `(FocusKey, always-on) → focus-managed streams` is deterministic; the resulting wire-level subscription set is `(focus-managed) ∪ (plugin-added)` and is not claimed deterministic across the entire system.

## 6. PluginHostService RPC additions

All three RPCs ship together (single PR) with Lua hostfunc parity (INV-S3).

### 6.1 `SetConnectionFocus`

```proto
message PluginHostServiceSetConnectionFocusRequest {
  bytes connection_id = 1;   // ULID — target connection
  // focus_key is nil for grid focus; non-nil for a specific context.
  // Phase 5 only supports kind = "scene"; channels add "channel" later.
  optional FocusKey focus_key = 2;
}

message FocusKey {
  string kind = 1;         // "scene" in Phase 5
  bytes target_id = 2;     // ULID — scene_id when kind == "scene"
}

message PluginHostServiceSetConnectionFocusResponse {
  // Echo of the applied focus for caller verification.
  optional FocusKey focus_key = 1;
}
```

**Substrate-side flow** (D7 single-mutator pattern — atomicity comes from the Store's single lock acquisition; no new Coordinator-level lock):

1. Look up `Connection` by `connection_id` to fail fast on the obvious mismatch. Error `CONNECTION_NOT_FOUND` if missing.
2. Call `Store.UpdateSessionConnection(sessionID, connection_id, mutator)` (§5.1.1). The mutator runs under the Store's lock; it receives `(info Info, conn Connection)` and atomically:
   - If `focus_key` non-nil and `kind == "scene"`, validates `info.FocusMemberships` contains an entry where `Kind == FocusKindScene` and `TargetID == focus_key.target_id`. Returns `FOCUS_WITHOUT_MEMBERSHIP` if absent (INV-P5-1).
   - Captures `oldFocusKey = conn.FocusKey` for the post-commit subscription delta.
   - Sets `conn.FocusKey = &focus_key`.
   - Per D9: if `conn.ClientType ∈ {terminal, telnet}` AND `focus_key` is a scene-kind `FocusKey` (i.e., this is an explicit `scene focus #<id>`, not `scene grid`), also set `info.PresentingFocus = &focus_key`. Per D10: if this call originated from `scene grid` (passed as a flag through the mutator's closure), leave `info.PresentingFocus` unchanged.
   - Returns `(info, conn, nil)`.
3. After `UpdateSessionConnection` commits successfully, call `subscription_router` (§4.3) to compute focus-managed stream deltas between `oldFocusKey` and `focus_key` and apply them via `SessionStreamRegistry.SendToConnection` (D5).
4. If new focus is a scene, enqueue IC replay for the unseen window (v2 §3.4).

**Race surface:** because both fields are written under one lock acquisition, no external observer can see `Connection.FocusKey` updated while `PresentingFocus` is still stale (or vice versa). A concurrent `LeaveFocus` either runs entirely before step 2 (Phase 5 mutator sees the post-leave state and rejects with `FOCUS_WITHOUT_MEMBERSHIP`) or entirely after (its own mutator sees the post-Phase-5 state and applies cleanly). INV-P5-7 (§10) is restated to claim *operation*-atomicity, not just per-field.

### 6.2 `AutoFocusOnJoin`

```proto
message PluginHostServiceAutoFocusOnJoinRequest {
  bytes character_id = 1;   // ULID
  bytes scene_id = 2;        // ULID
}

message PluginHostServiceAutoFocusOnJoinResponse {
  // ULIDs of connections that received the auto-focus.
  repeated bytes focused_connection_ids = 1;
  // Total active connections for the character at fan-out time
  // (sum of terminal + telnet + comms_hub + ...). Allows the caller
  // to distinguish "no terminal/telnet conns" (focused empty, total>0)
  // from "no active conns at all" (focused empty, total=0).
  uint32 total_connection_count = 2;
  // Connections that were skipped because their FocusKey was already
  // non-nil and different from the target (D8 — preserves user's most
  // recent explicit focus). Empty when no such connections existed.
  repeated bytes skipped_connection_ids = 3;
  // Connections whose mutator returned FOCUS_WITHOUT_MEMBERSHIP during
  // fan-out — either because the caller invoked AutoFocusOnJoin before
  // its JoinFocus committed (caller-misorder bug) OR because a
  // concurrent LeaveFocus removed the membership mid-fanout. Plan-author
  // hazard distinguished from the D8 skip case via a separate field.
  // Each entry carries a reason code from the FocusFailureReason enum.
  repeated FocusFailure failed_connection_ids = 4;
}

message FocusFailure {
  bytes connection_id = 1;
  FocusFailureReason reason = 2;
}

enum FocusFailureReason {
  FOCUS_FAILURE_REASON_UNSPECIFIED = 0;
  FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT = 1;  // FocusMembership for scene_id not present at mutator time
  FOCUS_FAILURE_REASON_CONNECTION_NOT_FOUND = 2;  // connection deregistered between ListConnectionsBySession and UpdateSessionConnection (rare; reconnect/disconnect race)
}
```

**Pre-condition:** caller MUST have completed `JoinFocus(sessionID, {scene, scene_id})` before invoking `AutoFocusOnJoin`. The plugin's `handleJoin` flow at `plugins/core-scenes/commands.go:357-384` already does `JoinScene → JoinFocus` in sequence; Phase 5 adds `AutoFocusOnJoin` as the third step.

**Substrate-side flow** (D7 + D8 + D11):

1. Locate the character's active session.
2. Call `Store.ListConnectionsBySession(sessionID)` (a separate lock acquisition; the snapshot is racy by design, but each per-conn mutator below re-validates atomically). Set `total_connection_count = len(conns)`.
3. Filter the snapshot by `ClientType ∈ {terminal, telnet}` (INV-P5-4).
4. For each filtered `conn`, call `Store.UpdateSessionConnection(sessionID, conn.ID, mutator)` (the §5.1.1 single-mutator path). The mutator, under one lock acquisition:
   - **Skip rule (D8):** if `conn.FocusKey != nil && *conn.FocusKey != {scene, scene_id}`: return `(info, conn, nil)` unchanged; record `conn.ID` in the substrate-side `skipped` set.
   - **Membership validation (INV-P5-1):** else if `info.FocusMemberships` lacks `{scene, scene_id}`: return `FOCUS_WITHOUT_MEMBERSHIP`. Caller records `conn.ID` in `failed_connection_ids` with reason `MEMBERSHIP_ABSENT`.
   - **Apply:** else set `conn.FocusKey = &FocusKey{scene, scene_id}`; if `conn.ClientType ∈ {terminal, telnet}` also set `info.PresentingFocus = &FocusKey{scene, scene_id}` (D9); return `(info, conn, nil)`. Caller records `conn.ID` in `focused`.
   - If `UpdateSessionConnection` returns `CONNECTION_NOT_FOUND` (rare disconnect race): caller records `conn.ID` in `failed_connection_ids` with reason `CONNECTION_NOT_FOUND`.
5. For each connection in the substrate-side `focused` set, run the subscription_router delta (§4.3) + IC replay enqueue.
6. Return response with the three lists + `total_connection_count`.

INV-P5-1 + the §5.2 I-2 guard pin the membership precondition. INV-P5-4 pins the terminal-only filter. INV-P5-11 (§10) pins D8's skip rule. The response shape lets plan authors and observability tools distinguish all four outcomes per connection: focused, skipped, failed-with-reason, or absent (never enumerated — implies the conn is `comms_hub` or non-existent at step 2).

### 6.3 `IsAnyConnFocused`

```proto
message PluginHostServiceIsAnyConnFocusedRequest {
  bytes character_id = 1;   // ULID
  bytes scene_id = 2;        // ULID
}

message PluginHostServiceIsAnyConnFocusedResponse {
  bool focused = 1;
}
```

**Substrate-side flow:**

1. Enumerate the character's connections.
2. Return true iff any has `FocusKey != nil && FocusKey.Kind == "scene" && FocusKey.TargetID == scene_id`.

**Use case:** notification-emission decision. When the plugin emits a `scene_pose` event, it iterates each scene member; for each, calls `IsAnyConnFocused`. If false → emit a notification to `notifications:<member_character_id>` so the member sees a digest entry without being subscribed to scene IC. (Notification trigger logic lands in Phase 6 — Phase 5 ships the RPC and contract.)

### 6.4 ULID encoding boundary (D6, INV-P5-9)

Proto wire format uses `bytes` for all ULID fields (`connection_id`, `character_id`, `scene_id`, `target_id`) — 16-byte fixed length. The Lua hostfunc layer follows the existing `stdlib_focus.go` convention: Lua callers pass ULIDs as 26-character base32 strings; the hostfunc parses via `ulid.Parse(str)` before crossing the gRPC client boundary.

```lua
-- Lua plugin call site (matches existing stdlib_focus.go convention)
holomush.set_connection_focus(connection_id_str, {kind = "scene", target_id = scene_id_str})
holomush.auto_focus_on_join(character_id_str, scene_id_str)
local focused = holomush.is_any_conn_focused(character_id_str, scene_id_str)
```

Hostfunc errors: invalid ULID string → `oops.Code("INVALID_ULID")` returned to Lua as `nil, err_string`. The 16-byte fixed length is enforced at parse time; substrate-side gRPC marshalling receives only well-formed 16-byte values.

INV-P5-9 (§10) pins the boundary contract via parity tests that round-trip a known ULID through both Go SDK + Lua hostfunc.

## 7. Commands

All three Phase 5 commands are owned by `core-scenes`. The substrate provides the RPCs; the plugin handles command parsing + routing.

### 7.1 `scene focus #<id>`

```text
scene focus #42
```

1. Plugin validates: scene #42 exists; caller has membership (`IsParticipant`).
2. Plugin calls `SetConnectionFocus(req.connection_id, &FocusKey{scene, 42})` via PluginHostService.
3. Substrate validates again (defense-in-depth) + applies via Coordinator.
4. Plugin renders confirmation: `> Focused on scene #42.`

**Errors:**

- `SCENE_NOT_FOUND`
- `SCENE_FOCUS_NOT_A_MEMBER` (returned by plugin pre-check; INV-P5-1 second line of defense in substrate)

### 7.2 `scene grid`

```text
scene grid
```

1. Plugin calls `SetConnectionFocus(req.connection_id, nil)` via PluginHostService.
2. Substrate applies via Coordinator; subscription_router switches scene → location stream.
3. Plugin renders confirmation: `> Focused on the grid.`

### 7.3 `scene list`

```text
scene list
```

1. Plugin reads `session.Info.FocusMemberships` (substrate-side; plugin has Phase 4 reads of session state).
2. For each scene-kind membership, calls `IsAnyConnFocused(character_id, scene_id)`.
3. Renders:

   ```text
   Your scenes (3):
     #42 The Tavern             [focused]
     #87 Secret Meeting         [background]
     #91 Plot Discussion        [background]
   ```

   "Focused" shown when `IsAnyConnFocused` returns true; "background" otherwise. Per-connection breakdown (e.g., `[focused — 2 connections]`) is a Phase 6 UX extension; Phase 5 surfaces only the binary indicator.

### 7.4 Auto-focus on `scene join`

The plugin's existing `handleJoin` flow (`plugins/core-scenes/commands.go:357-384`) is `JoinScene → JoinFocus`. Phase 5 extends it to `JoinScene → JoinFocus → AutoFocusOnJoin` — the order is load-bearing: `AutoFocusOnJoin`'s substrate-side precondition (§6.2 step 1) requires the `FocusMembership` written by `JoinFocus` to be in place.

```text
1. plugin: store.JoinScene(scene_id, character_id)  → adds scene_participants row
2. plugin: pluginHost.JoinFocus(session_id, {scene, scene_id})  → adds FocusMembership
3. plugin: pluginHost.AutoFocusOnJoin(character_id, scene_id) → []conn_ids
4. plugin: render
```

Step 4 render (using §6.2 response shape):

- `focused_connection_ids` non-empty: `> Joined scene #42 and focused your terminal connection(s) on it.`
- `focused_connection_ids` empty, `total_connection_count > 0`, `skipped_connection_ids` empty (i.e., only `comms_hub` connections exist): `> Joined scene #42. Use 'scene focus #42' to enter.`
- `focused_connection_ids` empty, `skipped_connection_ids` non-empty (the user is already explicitly focused on a different scene on every terminal-class connection): `> Joined scene #42. Your terminal stays on its current focus; use 'scene focus #42' to switch.`
- `total_connection_count == 0` (the session has somehow no active connections — defensive branch): substrate-internal error path; no render.

## 8. Reconnect focus restoration (D2)

When a new `Connection` is created for an existing session, restoration uses the single-mutator primitive (D7) — validation of `PresentingFocus` against `FocusMemberships` happens inside the `SessionConnectionMutator` callback under one Store-lock acquisition, eliminating the read-validate-write race:

1. The new Connection is registered via `Store.AddConnection` (existing path, unchanged) with `FocusKey = nil` (grid default).
2. The Subscribe handler then calls `Coordinator.RestoreFocusOnReconnect(sessionID, connectionID)`, which:
   - Calls `Store.UpdateSessionConnection(sessionID, connectionID, mutator)`. The mutator receives `(info Info, conn Connection)` snapshots taken under one lock acquisition; it inspects `info.PresentingFocus` AND `info.FocusMemberships` together:
     - If `info.PresentingFocus == nil` → return `(info, conn, nil)` unchanged; connection stays on grid default.
     - If `info.PresentingFocus != nil` and `info.FocusMemberships` contains an entry matching `info.PresentingFocus.Kind` + `info.PresentingFocus.TargetID` → return `(info, conn with FocusKey = &copy(*info.PresentingFocus), nil)`.
     - If `info.PresentingFocus != nil` but no matching membership (revoked while disconnected) → return `(info, conn, nil)` unchanged; the substrate emits a structured warning log carrying `session_id`, `character_id`, prior `PresentingFocus`. (`Info.PresentingFocus` is intentionally NOT cleared here — `LeaveFocus`'s own mutator would have cleared it via I-10 if the leave had actually committed in the substrate; the fact that it's still set means the membership was revoked some other way, and clearing on read would mask the diagnostic signal.)
3. If the mutator wrote a non-nil `Connection.FocusKey`, the post-commit substrate runs the subscription_router delta (§4.3) + IC replay enqueue.

**Reconnect-vs-leave race resolved:** because the mutator reads `info.PresentingFocus` and `info.FocusMemberships` from a single locked snapshot, the two values are always coherent. Possible outcomes vs a concurrent `LeaveFocus`:

- Leave commits first → mutator sees post-leave `FocusMemberships` missing the target → fallback to grid (correct).
- Mutator commits first → reconnect installs the FocusKey + subscription; subsequent leave commits, clears `PresentingFocus`, AND its scene_leave_ic notice reaches this connection's now-subscribed scene IC stream (the connection observes the leave on its wire, exactly as if it had been connected throughout).

INV-P5-5 pins the validation + fallback behavior at the mutator level; INV-P5-12 (§10) pins the reconnect-vs-leave race-serialization property.

**Fallback UX signaling (deferred to `holomush-3d9o`):** when fallback fires (case "absent"), the connection lands on grid without any in-band signal explaining why their previous scene focus was lost. This is a known UX gap — a one-shot "Your focus on scene #42 was lost while disconnected. Use `scene list` to see your current scenes." rendered to the new connection's first frame is desired. Phase 5 emits only the structured warning log; the in-band signal is **deferred to `holomush-3d9o`** so plan authors do not block on UX copy / event-shape decisions.

INV-P5-5 pins the validation + fallback behavior; the follow-up bead pins the eventual UX surface.

## 9. Failure modes

| Failure / edge case | Surface |
|---|---|
| Plugin calls `SetConnectionFocus` with non-existent `connection_id` | gRPC `NotFound` + `oops.Code("CONNECTION_NOT_FOUND")` |
| Plugin calls `SetConnectionFocus` with scene focus but `info.FocusMemberships` lacks `{scene, target_id}` (validated inside `SessionConnectionMutator`, D4 + D7) | gRPC `PermissionDenied` + `oops.Code("FOCUS_WITHOUT_MEMBERSHIP")` — INV-P5-1 |
| Plugin calls `AutoFocusOnJoin` for a character with no active connections at all | `focused_connection_ids` empty, `total_connection_count == 0`; not an error |
| Plugin calls `AutoFocusOnJoin` for a character with only `comms_hub` connections (no terminal/telnet) | `focused_connection_ids` empty, `total_connection_count > 0`, `skipped_connection_ids` empty; not an error — plugin renders the "use 'scene focus #X' to enter" prompt |
| Plugin calls `AutoFocusOnJoin` and one of the terminal/telnet conns is already explicitly focused on a different scene (D8) | That `conn_id` lands in `skipped_connection_ids`; mutator no-ops; user's recent explicit focus is preserved |
| Plugin calls `AutoFocusOnJoin` before `JoinFocus` has written the FocusMembership (caller mis-orders the chain) | Per-conn mutator returns `FOCUS_WITHOUT_MEMBERSHIP`; the outer fan-out skips all conns; outer response carries `focused_connection_ids` empty |
| Concurrent `LeaveFocus` during `AutoFocusOnJoin` fan-out | Per-conn inner `UpdateSessionConnection` mutator sees post-leave `info.FocusMemberships`; returns `FOCUS_WITHOUT_MEMBERSHIP` for the affected conn; outer call surfaces these in `failed_connection_ids` with reason `FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT`; conns that committed before the leave landed are in `focused_connection_ids` |
| Reconnect restoration: `PresentingFocus` references scene character is no longer a member of (revoked while disconnected) | Mutator no-ops (grid fallback) + structured warning log; in-band signal deferred to `holomush-3d9o`; INV-P5-5 |
| Reconnect restoration: concurrent `LeaveFocus` between AddConnection and RestoreFocusOnReconnect | Mutator-side serialization (D7 + INV-P5-12) handles deterministically — either leave commits first (restoration falls back to grid) or restoration commits first (leave's subsequent scene_leave_ic reaches the now-subscribed connection) |
| Any ULID field in any RPC is malformed (Lua-side string parse or proto-side length check) | gRPC `InvalidArgument` + `oops.Code("INVALID_ULID")` — Lua hostfunc returns `nil, err_string` per §6.4 |
| Connection.ClientType is unset or invalid | Treated as non-terminal (AutoFocusOnJoin skips); per `internal/session/memstore.go:231` the field is validated on connection creation already |

## 10. Invariants

| ID | Statement | Pinned by |
|---|---|---|
| **INV-P5-1** | Focus-without-membership MUST NOT be possible: substrate validates against `info.FocusMemberships` (D4) inside the `SessionConnectionMutator` callback (§5.1.1) before applying any non-nil `Connection.FocusKey`. Validation and write are atomic under one Store-lock acquisition. | `internal/grpc/focus/set_connection_focus_test.go::TestSetConnectionFocus_RequiresMembership` + integration test `test/integration/scenes/focus_without_membership_blocked_test.go` |
| **INV-P5-2** | Each Connection has exactly one `FocusKey` at all times (nil = grid; otherwise a single FocusKey). No "multiple focuses per connection." | `internal/session/connection_test.go::TestConnection_FocusKeyNilByDefault` (type-level) |
| **INV-P5-3** | The **focus-managed subset** of `Connection.Streams` is a deterministic function of `(FocusKey, character-level always-on streams)`. Plugin-added streams (channels, whispers) co-exist additively and are not in scope. | `internal/grpc/focus/subscription_router_test.go::TestComputeFocusManagedStreams_Deterministic` |
| **INV-P5-4** | `AutoFocusOnJoin` terminal-only filter: `ClientType ∈ {terminal, telnet}`. Comms_hub connections are NEVER auto-focused. | `internal/grpc/focus/auto_focus_on_join_test.go::TestAutoFocus_FiltersByClientType` |
| **INV-P5-5** | On reconnect, focus restoration validates `info.PresentingFocus` against `info.FocusMemberships` **inside the `SessionConnectionMutator` callback** (§5.1.1) under one Store-lock acquisition; falls back to grid when validation fails. No read-then-mutate race; the two fields are read from a single locked snapshot. | `internal/grpc/focus/restore_connection_focus_test.go::TestReconnect_FallsBackToGridWhenMembershipRevoked` |
| **INV-P5-6** | The 3 new PluginHostService RPCs ship with Go SDK + Lua hostfunc bindings together (INV-S3 substrate-contract parity). | `internal/plugin/hostfunc/stdlib_focus_test.go::TestFocusHostfunc_PhaseFive_LuaParity` |
| **INV-P5-7** | Phase 5 multi-field focus mutations (`Connection.FocusKey` + `Info.PresentingFocus`) MUST be applied via a single `SessionConnectionMutator` (§5.1.1) invocation under one Store-lock acquisition (D7) — both fields atomic at the operation level. No external observer (e.g., `Get(sessionID)`) can see the two in mismatched states. `FocusMutator` retains its existing role for `FocusMemberships`/`PresentingFocus`-only mutations (LeaveFocus, JoinFocus). No Coordinator-level lock; no side-channel writes. | `internal/session/memstore_test.go::TestUpdateSessionConnection_AtomicCommit` (verifies single lock acquisition; observers between subtests cannot see torn state) + `internal/session/session_connection_mutator_test.go::TestSessionConnectionMutator_OnlyConstructibleInGrpcFocus` (compile-fail doc test) |
| **INV-P5-8** | Meta-test: every numbered INV-P5-N declaration MUST cite at least one existing test path. | `internal/test/invariants/inv_p5_coverage_meta_test.go::TestINV_P5_Coverage_Meta` (corpus-walk pattern from Phase 4 T28) |
| **INV-P5-9** | ULID encoding boundary (D6): proto wire = `bytes` (16-byte); Lua hostfunc accepts 26-char base32 strings; malformed input → `INVALID_ULID`. Go SDK + Lua hostfunc round-trip a known ULID identically. | `internal/plugin/hostfunc/stdlib_focus_test.go::TestFocusHostfunc_ULIDRoundTrip` |
| **INV-P5-10** | `SessionStreamRegistry.SendToConnection(sessionID, connectionID, update)` (D5) delivers `update` to EXACTLY the named connection's channel; other connections in the same session do NOT receive the update via this path. Existing session-wide `Send` remains unchanged for non-Phase-5 callers. | `internal/grpc/stream_registry_test.go::TestSendToConnection_TargetsOneConnectionOnly` |
| **INV-P5-11** | `AutoFocusOnJoin` MUST skip a connection whose `FocusKey` is already non-nil and different from the requested target (D8). The skipped `conn_id` appears in `skipped_connection_ids`; the connection's `FocusKey` is unchanged; the user's most recent explicit focus is preserved. | `internal/grpc/focus/auto_focus_on_join_test.go::TestAutoFocus_SkipsAlreadyExplicitlyFocusedConn` |
| **INV-P5-12** | Reconnect restoration vs concurrent `LeaveFocus` serializes via the single Store-lock acquisition: either the leave commits first (restoration's mutator sees no membership → grid fallback) OR the restoration commits first (the leave's subsequent scene_leave_ic reaches the now-subscribed connection on the wire). No corruption; no torn state. | `internal/grpc/focus/restore_connection_focus_test.go::TestReconnect_VsConcurrentLeave_Serializes` |
| **INV-P5-13** | `scene grid` (D10) MUST NOT modify `info.PresentingFocus`. The per-Connection `FocusKey` is cleared to nil; the session-level reconnect target is preserved. The only non-explicit clearing path remains `LeaveFocus` (I-10 at `internal/grpc/focus/leave.go:14-15`). | `internal/grpc/focus/set_connection_focus_test.go::TestSceneGrid_DoesNotClearPresentingFocus` + integration test `test/integration/scenes/reconnect_focus_restoration_test.go::TestReconnect_AfterSceneGrid_RestoresPriorScene` |
| **INV-P5-14** | Postgres `UpdateSessionConnection` MUST lock the `sessions` row FIRST via `FOR UPDATE`, then the `session_connections` row (D11 canonical order). Pinned by a deadlock-detector regression test that races two `UpdateSessionConnection` calls on the same session for different connections. | `internal/store/session_store_test.go::TestUpdateSessionConnection_LockAcquisitionOrder_NoDeadlock` |

## 11. Test plan

### 11.1 Unit tests (per-component)

- `internal/session/connection_test.go` — Connection.FocusKey nil + non-nil paths (INV-P5-2)
- `internal/session/session_connection_mutator_test.go` — SessionConnectionMutator sentinel-construction protection (INV-P5-7 compile-fail doc test)
- `internal/session/memstore_test.go` (extend) — TestUpdateSessionConnection_AtomicCommit (INV-P5-7 op-atomicity verified via interleaved Get + concurrent Set); TestUpdateSessionConnection_VsConcurrentUpdateFocusMemberships (serializes on m.mu); TestListConnectionsBySession enumeration
- `internal/store/session_store_test.go` (extend) — Postgres equivalents via `FOR UPDATE` for the two new Store methods; TestUpdateSessionConnection_LockAcquisitionOrder_NoDeadlock (INV-P5-14)
- `internal/store/migrations/00000N_connection_focus_key_test.go` — up/down idempotency on the new migration (Phase 4 T2 pattern)
- `internal/grpc/focus/set_connection_focus_test.go` — happy path; FocusMemberships rejection inside mutator (INV-P5-1); CONNECTION_NOT_FOUND; comms_hub no-PresentingFocus-write (D9); TestSceneGrid_DoesNotClearPresentingFocus (INV-P5-13)
- `internal/grpc/focus/auto_focus_on_join_test.go` — terminal-only filter (INV-P5-4); empty-result case (no conns / no terminal conns); concurrent-leave failure with FOCUS_FAILURE_REASON_MEMBERSHIP_ABSENT (INV-P5-12 race coverage); TestAutoFocus_SkipsAlreadyExplicitlyFocusedConn (INV-P5-11); TestAutoFocus_ResponseShape (focused/skipped/failed/total_connection_count distinguishable)
- `internal/grpc/focus/is_any_conn_focused_test.go` — true/false branches
- `internal/grpc/focus/subscription_router_test.go` — TestComputeFocusManagedStreams_Deterministic (INV-P5-3)
- `internal/grpc/focus/coordinator_test.go` — TestSetConnectionFocus_RoutesViaCoordinator (INV-P5-7) — verifies Coordinator does NOT take a lock of its own, just delegates to Store.UpdateSessionConnection
- `internal/grpc/stream_registry_test.go` — TestSendToConnection_TargetsOneConnectionOnly (INV-P5-10); TestSendToConnection_ReturnsConnectionNotRegistered (error contract); TestSend_StillBroadcastsForSessionWideCallers (regression on existing behavior)
- `internal/plugin/hostfunc/stdlib_focus_test.go` — TestFocusHostfunc_PhaseFive_LuaParity (INV-P5-6) + TestFocusHostfunc_ULIDRoundTrip (INV-P5-9)
- `plugins/core-scenes/commands_focus_test.go` — `scene focus`/`scene grid`/`scene list` handler unit tests (verified non-existent via `find -maxdepth 1`)

### 11.2 Integration tests

- `test/integration/scenes/focus_without_membership_blocked_test.go` — INV-P5-1 end-to-end
- `test/integration/scenes/auto_focus_on_join_terminal_only_test.go` — INV-P5-4 with mixed-client-type session + skipped-conn D8/INV-P5-11 + failure-reason-coverage (FOCUS_FAILURE_REASON_*)
- `test/integration/scenes/reconnect_focus_restoration_test.go` — INV-P5-5 reconnect with PresentingFocus + revoked-membership fallback + TestReconnect_VsConcurrentLeave_Serializes (INV-P5-12) + TestReconnect_AfterSceneGrid_RestoresPriorScene (INV-P5-13 end-to-end)
- `test/integration/scenes/multi_connection_visibility_test.go` — alice on (telnet=grid, web=scene42); verify per-Connection subscription deltas are disjoint via SendToConnection (INV-P5-10); emit scene event; verify only web receives

### 11.3 Invariant tests

- `internal/test/invariants/inv_p5_coverage_meta_test.go` — INV-P5-8 self-pinning meta-test (Phase 4 T28 corpus-walk pattern; covers all 14 INV-P5-N declarations)

## 12. Out-of-scope reaffirmation

- No location-stream dot-style migration in this phase (`rops` tracks).
- No notification-emission trigger logic (Phase 6 / `fux3`).
- No web client focus UI (Phase 9).
- No channel focus (`0sc.12` rework).
- No in-band signal on reconnect-fallback (deferred to `holomush-3d9o`; structured warning log only in Phase 5).

<!-- adr-capture: sha256=d4fa1522e9865a9a; ts=2026-05-22T01:38:33Z; adrs=holomush-kuf8,holomush-x0ph,holomush-nki4,holomush-8new -->

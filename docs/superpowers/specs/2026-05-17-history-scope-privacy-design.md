<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# History scope privacy — design

**Status:** Draft v6 (2026-05-17) — addressing design-reviewer round 5 findings
**Bead:** `holomush-iwzt` (design)
**Authors:** Sean Brandt with Claude (brainstorming session)
**Triggered by:** New guest "Onyx Radium" observed seeing prior in-character conversation between Emerald and Pearl Radium on 2026-05-17 in the dev environment.

> **Update 2026-05-22 (`holomush-gfo6`):** Floor precision contract upgraded
> from microsecond to nanosecond. See
> [`2026-05-22-nanosecond-timestamps-design.md`](2026-05-22-nanosecond-timestamps-design.md)
> §5 INV-TS-6 / INV-TS-7.

## 1. Problem

Two web-terminal flows leak event history that the requesting character should not be able to see:

- **`QueryStreamHistory`** (`internal/grpc/query_stream_history.go:216-220`) applies a `NotBefore` floor only when the client supplies one. The ABAC step gates "can read this stream at all" but applies no temporal bound, so a fresh guest's empty-cursor backfill returns the location stream's entire retained history.
- **Subscribe replay-from-cursor** — `internal/grpc/list_session_streams.go:91-100` builds a focus plan with `focus.ReplayModeFromCursor`, and Subscribe (`internal/grpc/server.go`) opens a bus session with the stored cursor. For a session created with no prior cursor, the replay window is effectively "from the earliest event the bus still retains."

The bug is privacy-class. It MUST NOT be deferred behind any other improvement.

## 2. Principle

> A session's view of an event stream's history MUST be limited to the time window during which **the requesting session has existed as a live session row** for that stream's scope. The session row is the unit of continuity; transport reattach within the row's TTL is **the same session**.

Per-session floor, no cross-session aggregation. Transport-level detach with later reattach within the session's TTL DOES count as the same session — the floor is **NOT** reset. The session was held open across the disconnect window precisely because the player may reconnect (status=Detached, expires_at=now+TTL per `internal/grpc/server.go::Disconnect`); resetting the floor would discard the player's own pre-disconnect scrollback for no privacy gain (events emitted during the gap arrive on the live durable consumer and are accessible to the original session on reattach).

The floor advances on three transitions only: session-create (the row first exists), character-move (location changed, prior location's events are out of scope), and TTL expiry forcing a brand-new session-create (the prior row is gone — a fresh session row gets a fresh floor naturally).

Per-session-row enforcement. Transport-level disconnects within session TTL preserve `LocationArrivedAt`; multiple concurrent transports for a given character share the session row's single floor (one game session row per character — see schema invariant in §4).

This is the unified principle covering pre-arrival exclusion (session's floor starts at its own arrival), during-absence exclusion when a session genuinely ends (TTL expiry → next SelectCharacter gets a fresh floor), and the orthogonal guest identity overlay (an additional MAX bound on top, scoped to the same session).

**Transport-continuity worked example.** Character X has one game session row. Web client A attaches at T=0 (SelectCharacter creates the session row; `LocationArrivedAt = 0`). Telnet client B attaches at T=10 (SelectCharacter finds the existing row and adds a second transport via `session_connections`; `LocationArrivedAt` unchanged at 0). A's transport drops at T=20: with B still attached the session stays Active; if B had also been gone the session would have transitioned to Detached (`expires_at = T=20+1800s`). Events emitted at T=25 flow into the per-session JetStream durable consumer and are delivered live to B. A reopens its transport at T=30; this triggers `Subscribe.ReattachCAS` if the session had gone Detached (no-op if still Active). After reattach: `LocationArrivedAt = 0` (unchanged); `QueryStreamHistory` for character X returns events with `timestamp >= 0`, including the T=25 events emitted while A's transport was gone. The durable consumer held those events for live delivery to B and replay to A on Subscribe; they are part of the session's continuous existence.

If instead the session had Detached at T=20 (both transports gone) and TTL had expired before any reconnect (e.g., reaper sweeps at T=20+1800s, X's player reconnects at T=2000), `SelectCharacter` would find no session row and create a fresh one with `LocationArrivedAt = 2000`. The fresh session's floor naturally excludes events from the prior lifetime.

Plugin-owned subjects (channels, etc.) MAY adopt different semantics per their own design but MUST declare them explicitly (see I-PRIV-7).

## 3. Scope by stream type

| Stream subject | Gate | Temporal floor | ABAC override |
|---|---|---|---|
| `events.<game_id>.location.<id>` | Hard gate — `session.LocationID.String() == <id>` (default-deny) | `MAX(session.LocationArrivedAt, [if IsGuest] session.GuestCharacterCreatedAt)` | Yes — `"staff"` or `"admin"` in `principal.character.roles` bypasses the hard-gate only (temporal floor still applies — see I-PRIV-6) |
| `events.<game_id>.scene.<scene_id>.ic`, `events.<game_id>.scene.<scene_id>.ooc` | Existing I-17 membership gate (migrated to NATS dot-style by Phase 4 — `holomush-5rh.13`) | `MAX(focusMembership(scene_id).JoinedAt, [if IsGuest] session.GuestCharacterCreatedAt)` | None — scene privacy is absolute |
| `events.<game_id>.character.<id>` | Existing membership gate (holomush-rops) | None (own stream) | N/A |
| Plugin-owned subjects | Plugin's own router (unchanged) | Plugin-defined | Plugin-defined |
| Other public (e.g., `global`, `system`) | ABAC `engine.Evaluate` (unchanged) | None | Through normal ABAC policy |

The location-stream hard gate replaces the current ABAC public-stream policy path for `location:*` only. ABAC remains active for all other public streams.

## 4. Data model

### 4.1 `SessionInfo.LocationArrivedAt`

`internal/session/session.go` — extend the `Info` struct:

```go
type Info struct {
    // ... existing fields ...
    LocationID                   ulid.ULID
    LocationArrivedAt            time.Time   // NEW — §5
    GuestCharacterCreatedAt      time.Time   // NEW — §4.3
    // ... existing fields ...
}
```

The `sessions` PostgreSQL table MUST gain matching columns:

- `location_arrived_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`
- `guest_character_created_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch'`

Paired up/down migrations, logic-free (per `site/docs/contributing/database-migrations.md`).

`LocationArrivedAt` is invariant across idle periods. It SHALL be updated only by the seven transitions enumerated in §5.

### 4.2 `FocusMembership.JoinedAt`

Already present at `internal/session/session.go:88`. No schema change. Scene floor computation reads it directly via `session.FocusMemberships` lookup by scene ID.

### 4.3 `SessionInfo.GuestCharacterCreatedAt` (cached at session creation)

Decision: **cache the guest character's `created_at` value into `SessionInfo` at session-creation time**, not per-query lookup. Rationale:

- Per-query DB hit on every `QueryStreamHistory` call would compound the latency surface that `holomush-87qu` already flags.
- The value is immutable for the lifetime of the session (character row doesn't get recreated mid-session).
- `IsGuest=false` sessions leave the field as `'epoch'` (or zero `time.Time`); the `MAX(.)` floor is then dominated by `LocationArrivedAt`.

Population path: at `internal/grpc/auth_handlers.go` `SelectCharacter` (and the analogous guest-connect path in `internal/auth/guest_service.go`), read `world.Character.CreatedAt` from the character row and stamp it into the new `SessionInfo`. The existing `SessionInfo.IsGuest` field (already at `internal/session/session.go:148`, populated from `world.Player.IsGuest` per `internal/web/auth_handlers.go:335`) controls whether the floor consults this value.

## 5. Session-lifecycle update rules

Seven transitions cover the session lifecycle. Only **two** advance `SessionInfo.LocationArrivedAt`: session-create and character-move. Reattach within TTL is the same session continuing — the floor MUST NOT be reset.

| # | Transition | Code site | `LocationArrivedAt` action |
|---|---|---|---|
| 1 | **Fresh `SelectCharacter`** (no existing session row found — first login, or prior session was hard-ended via quit/logout/boot/reap) | `internal/grpc/auth_handlers.go:331-348` | Set to `now` (equals `CreatedAt`) at session-row creation |
| 2 | **`SelectCharacter` reattach** (existing detached session matched within TTL) | `internal/grpc/auth_handlers.go:295-310` | **Unchanged.** The session row was held open across the disconnect; this is the same session continuing. The user expects to see their own pre-disconnect scrollback. |
| 3 | **`Subscribe.ReattachCAS`** (transport-level reattach, session row already exists) | `internal/grpc/server.go:768-777` | **Unchanged.** Same reasoning as row 2 — page reload, WiFi blip, and tmux-style telnet reattach all MUST preserve the floor. |
| 4 | **Detach** (transport drop, status → `Detached`) | `internal/grpc/server.go:1290-1308` | Unchanged. Preserved for the TTL window (default 1800s); if the session reattaches (#2 or #3) the floor stays where it is. |
| 5 | **Character move** (`world.Service.MoveCharacter`) | `internal/world/service.go:759-790` plus a new session-sync consumer (§5.1) | For every Active/Idle session attached to this character: set `LocationID` to new value AND `LocationArrivedAt` to `now`, atomic update. |
| 6 | **Explicit logout / quit / boot** | `internal/grpc/auth_handlers.go:538+`, `internal/grpc/server.go:427-446`, `:490-508` | N/A — session row deleted. Next login goes through #1 with a fresh floor. |
| 7 | **TTL expiry** (reaper) | `internal/session/reaper.go` | N/A — session row deleted. Next login goes through #1 with a fresh floor. |

The privacy invariant is preserved by the fact that the **session row itself is the unit of continuity**:

- During a transport disconnect, the durable JetStream consumer for the session is held open by the immutability of its `OptStartTime` (§6.2 Tier 1). The consumer SHOULD have an `InactiveThreshold` that meets-or-exceeds the session TTL so events emitted during the disconnect window survive long enough to be delivered to the next Subscribe on the same session ID. Today this holds by configuration accident: `internal/eventbus/subscriber.go::DefaultSessionInactiveThreshold = 24h` is much larger than the default `SessionTTL = 30m` from `cmd/holomush/core.go`. Production wiring (`cmd/holomush/sub_grpc.go`) does not currently invoke `eventbus.WithSessionInactiveThreshold(...)`, so an operator who sets `--session-ttl` greater than 24h would silently lose events emitted in the second half of the disconnect window. Closing that gap (validation in `parseSessionConfig` to reject `sessionTTL > inactiveThreshold`, or auto-deriving `InactiveThreshold = max(default, sessionTTL)`) is tracked separately as a hardening item — non-blocking for this spec because the default-config case is correct. If the InactiveThreshold is exceeded, the durable evicts, the reattached session sees a strict subset of in-flight events: no leak, no extra exposure, only loss.
- A character who genuinely abandons a session (quit, logout, boot, or TTL expiry) loses the row entirely. Re-engagement goes through `SelectCharacter` row 1 — a fresh session with a fresh floor that excludes everything from the prior lifetime.

Idle-status transitions (Active ↔ Idle) DO NOT advance `LocationArrivedAt`. Idle is still "actively attached."

### 5.0 Pre-2026-05-18 rule-3-reset alternative (rejected)

An earlier draft of this spec advanced `LocationArrivedAt` to `now` on every reattach (rows 2 and 3), treating each transport reattach as a fresh arrival. Implementation landed in iwzt.3 + iwzt.8 and broke six `web/e2e/terminal.spec.ts` reload tests — page reload, WiFi blip, and tmux reattach all wiped the user's own pre-disconnect scrollback. The principle is that a session row exists exactly to express continuity across transport disconnects; resetting the floor on transport reattach is incompatible with the session-row-as-continuity model the rest of the codebase already implements. The grace-period workaround (bump only if detach window > N seconds) was considered and rejected as a hack — it adds policy nuance to a binary lifecycle question and tunes a magic threshold. The current rule (#2 and #3 do NOT advance the floor) is simpler, matches the lifecycle, and supports tmux/reload/wifi reattach uniformly.

### 5.1 Character-move session sync (Phase-1 sub-task)

Grounding confirmed via design-reviewer round 1: **no existing path syncs `sessions.location_id` to `characters.location_id` on character move.** `world.Service.MoveCharacter` updates the character row and emits `EventTypeMove`; `locationFollower` at `internal/grpc/location_follow.go:60-130` consumes the event per-Subscribe-stream and switches the bus filter, but is per-stream in-memory state — it never writes the session store.

Phase 1 MUST establish a canonical sync path. The recommended approach:

- Add `session.Store.UpdateLocationOnMove(ctx, characterID, newLocationID, arrivedAt time.Time) error` that updates **all** Active/Idle sessions for the character atomically (single transaction).
- Wire `world.Service.MoveCharacter` to call it immediately after `characterRepo.UpdateLocation` and before the move-event emit. Either:
  - Direct dependency injection (world.Service gains a session.Store dependency — violates layering)
  - Or via a `MovementHook` interface that core-server implements and wires (preserves layering).

The plan stage selects the wiring approach. The semantic requirement is non-negotiable: at the moment a Move event reaches any subscriber, every session in the store MUST already reflect the new `LocationID` and `LocationArrivedAt`.

## 6. Server-side enforcement

### 6.1 `QueryStreamHistory` restructure

`internal/grpc/query_stream_history.go:156-220` Step 5 ("Authorization") becomes:

```text
Step 5: Authorization
  if isPrivateStream(stream):                       # scene:*:*, character:*
      I-17 membership gate (unchanged)
  elif isLocationStream(stream):                    # NEW classification
      if !staffOverride(ctx, info, accessEngine):
          if session.LocationID.String() != extractLocationID(stream):
              return STREAM_ACCESS_DENIED
      # If staff-overridden: skip the location-match check.
  else:                                             # global, system, other public
      ABAC engine.Evaluate (unchanged)

Step 6: Floor
  baseFloor   := req.NotBeforeMs (as time.Time)
  scopeFloor  := streamScopeFloor(info, stream)
  notBefore    = MAX(baseFloor, scopeFloor)
```

`streamScopeFloor(info, stream) time.Time`:

- For `location:*`: `MAX(info.LocationArrivedAt, info.IsGuest ? info.GuestCharacterCreatedAt : time.Time{})`
- For `scene:<id>:*`: `MAX(focusMembership(info, scene_id).JoinedAt, info.IsGuest ? info.GuestCharacterCreatedAt : time.Time{})`
- For `character:<own-id>`: zero (no temporal floor)

`staffOverride` checks the ABAC engine with a fixed request shape (correct field names per `internal/access/policy/types/types.go:132-150`):

```go
accessReq, err := types.NewAccessRequest(
    "character:"+info.CharacterID.String(),
    "read_unrestricted_history",
    "stream:"+stream,
    nil,
)
if err != nil { return false }
decision, evalErr := s.accessEngine.Evaluate(ctx, accessReq)
return evalErr == nil && decision.IsAllowed()
```

The roles attribute (`principal.character.roles`) is resolved by `CharacterProvider` at `internal/access/policy/attribute/character.go:21-107` via the injected `RoleResolver.GetRoles(ctx, subjectID)`. A new seeded policy in `internal/access/policy/seed.go` grants `read_unrestricted_history` to characters with `"staff"` or `"admin"` in `character.roles`:

```go
{
    Name:    "staff_read_unrestricted_history",
    DSLText: `permit(principal is character, action == "read_unrestricted_history", resource is stream) when { "staff" in principal.character.roles || "admin" in principal.character.roles };`,
},
```

**The staff override bypasses the hard-gate location-match check only. The temporal floor STILL applies.** An admin debugging "what did guest X see when they connected?" SHALL get a view that's bounded by their own attachment intervals, NOT retroactive omniscience. This is I-PRIV-6.

### 6.2 Subscribe replay-from-cursor floor — native start-time + filter-at-delivery

Subscribe and `QueryStreamHistory` use **different** event-source paths and must be handled separately.

**Subscribe path** (`internal/eventbus/subscriber.go::JetStreamSubscriber.OpenSession`, line 159-195). Subscribe creates one durable JetStream consumer per session with `DeliverAllPolicy` (line 185). JetStream consumers accept exactly one `DeliverPolicy` per consumer; a multi-subject session cannot have different start positions per filter under a single durable.

**Two-tier enforcement** — perf hint at the consumer (server-side), authoritative filter at the broadcaster (per-event):

#### Tier 1: `OptStartTime` as a perf hint (consumer creation only)

At first call to `OpenSession` for a session, apply the **minimum floor across all subscribed subjects** as the consumer's start time:

```text
minFloor := time.Time{}  // zero — no floor
for each subject in filters:
    f := streamScopeFloor(info, subject)         // §6.1
    if f.After(minFloor): minFloor = f
if !minFloor.IsZero():
    cfg.DeliverPolicy = jetstream.DeliverByStartTimePolicy
    cfg.OptStartTime  = &minFloor
```

**`OptStartTime` is a PERFORMANCE HINT, not the privacy enforcement.** It tells JetStream "don't bother delivering events older than this" so we avoid scanning the entire stream for every new session. The privacy gate is Tier 2.

**JetStream immutability constraint.** `DeliverPolicy`, `OptStartTime`, `OptStartSeq` are server-side immutable on existing consumers (NATS server error 10012: "deliver policy can not be updated"; see https://docs.nats.io/nats-concepts/jetstream/consumers, "Editable" column). Both `OpenSession` (called on every Subscribe, including transport reattach per `internal/grpc/server.go:855`) and `SetFilters` (called on location-following moves) issue `CreateOrUpdateConsumer` — both paths must produce a config compatible with the immutable durable.

**Resolution: NATS is the source of truth.** The implementation MUST resolve the immutability constraint by querying the existing durable's config before issuing `CreateOrUpdateConsumer`, NOT by caching `OptStartTime` in-process or in the session row. The flow:

```text
existing, lookupErr := s.js.Consumer(ctx, StreamName, name)
switch {
case lookupErr == nil && existing != nil:
    info := existing.CachedInfo()
    cfg.DeliverPolicy = info.Config.DeliverPolicy
    cfg.OptStartTime  = info.Config.OptStartTime
    cfg.OptStartSeq   = info.Config.OptStartSeq
    // Filters from the current call still drive the new cfg — subjects are mutable.
case errors.Is(lookupErr, jetstream.ErrConsumerNotFound):
    // First creation for this session — compute fresh minFloor.
    minFloor := computeMinFloor(info, filters)
    if !minFloor.IsZero():
        cfg.DeliverPolicy = jetstream.DeliverByStartTimePolicy
        cfg.OptStartTime  = &minFloor
default:
    // Transient lookup error (connection lost, ctx cancel, JS disabled, etc.):
    // MUST fail closed. Do not proceed to CreateOrUpdateConsumer — sending a
    // freshly-computed minFloor against an existing-but-temporarily-unreachable
    // durable could either trigger NATS error 10012 (immutability) on later
    // contact OR leak via DeliverAllPolicy zero-value if cfg is unset.
    return nil, oops.Code("EVENTBUS_CONSUMER_LOOKUP_FAILED").
        With("session_id", sessionID).
        With("consumer", name).
        Wrap(lookupErr)
}
// Subjects mutate normally (all other ConsumerConfig fields — Durable, Name,
// AckPolicy, MaxAckPending, AckWait, InactiveThreshold — MUST match the
// existing OpenSession/SetFilters defaults; only the three start-policy fields
// are sourced from existing.CachedInfo().Config above):
cfg.FilterSubjects = subjectsToStrings(filters)
cons, err := s.js.CreateOrUpdateConsumer(ctx, StreamName, cfg)
```

This is idempotent and fail-closed: first call creates the consumer with `minFloor` start time; every subsequent call (OpenSession-on-reattach OR SetFilters-on-move) reads the existing config, preserves the immutable fields verbatim, mutates only the subject filter list. Error 10012 is unreachable under the documented call discipline (one in-flight OpenSession per session ID, enforced by the gRPC Subscribe handler being the sole control-plane owner of the session); transient lookup errors return to the caller without attempting `CreateOrUpdateConsumer`.

`OptStartTime` is a **monotonically-decreasing-from-creation** lower bound: at first creation it equals `minFloor`; subsequent floor advances (per-session reattach, moves) make the in-process filter stricter while `OptStartTime` stays put on the server.

**`Subscribe.ReattachCAS` interaction.** On transport reattach, `server.go:855` calls `s.subscriber.OpenSession(...)` again. The idempotent lookup above means the new `jetStreamSessionStream` instance reads the existing consumer's config and sends an identical-policy `CreateOrUpdateConsumer` — effectively a no-op on the durable. The durable's last-acked-seq is preserved. Privacy enforcement on reattach is performed entirely by §6.2 Tier 2 (per-subject filter-at-delivery) using the post-reattach `streamScopeFloor` — `OptStartTime` is a stale lower bound that JetStream uses as a performance hint only.

**`SetFilters` interaction.** On move, the existing consumer is looked up via the same path, its `DeliverByStartTimePolicy` + `OptStartTime` are preserved, only `FilterSubjects` mutates. No error 10012; no consumer recreation; acked-seq cursor preserved.

#### Tier 2: per-subject filter-at-delivery (authoritative)

The Subscribe broadcaster (`internal/grpc/server.go` event dispatch loop) MUST apply, for every event delivered by JetStream:

```text
if event.Timestamp < streamScopeFloor(currentSessionInfo, event.Subject) {
    drop event  // do not forward to client (INV-TS-6: strict-less than floor)
}
// events with event.Timestamp == floor are included (INV-TS-7: >= semantics)
```

`currentSessionInfo` is the **post-reattach, post-move** snapshot — re-read from the session store at delivery time or cached and invalidated on lifecycle transitions (plan-stage choice). This is the load-bearing privacy gate.

**Precision contract.** The comparison MUST be performed at nanosecond granularity (post-`holomush-gfo6`). Event timestamps preserve full nanosecond precision at publish time per INV-TS-4 (`internal/eventbus/publisher.go::Publish` no longer truncates). Floor inputs (`LocationArrivedAt`, `GuestCharacterCreatedAt`, `FocusMembership.JoinedAt`) preserve nanosecond precision per INV-TS-1 (BIGINT epoch-ns columns via the `pgnanos.Time` seam). Comparison uses `>=` semantics so an event at the exact floor ns is INCLUDED (INV-TS-7); strictly-below-floor events are dropped (INV-TS-6). The former µs-granularity contract is superseded by ADRs `holomush-absb` (BIGINT over timestamp9), `holomush-rbw6` (pgnanos.Time named-type seam), and `holomush-f5h0` (AAD byte-equality structural guarantee, supersedes INV-P7-16).

**Cost analysis:**

- **Steady-state (no reattach, no scope change):** `OptStartTime` bounds JetStream delivery from below at `minFloor`. Over-delivery is per-subject: the filter discards events between `minFloor` and the per-subject floor for subjects whose floor exceeds `minFloor`. Cost is bounded by per-subject floor delta × event rate.
- **Reattach:** new `LocationArrivedAt` may exceed `OptStartTime`. JetStream resumes from last-acked-seq; events between last-acked and new floor are delivered by JS but dropped by the filter. Scan-then-discard cost bounded by `(new floor − last-acked timestamp) × event rate` for the duration of the reattach burst.
- **Move:** `SetFilters` swaps filter subjects. The new subject's floor (set to `now` per §5 row 5) means filter rejects any old-timestamp events JetStream might deliver for the new subject. Bounded by per-subject move event rate.

#### `QueryStreamHistory` path (different mechanism)

`internal/grpc/query_stream_history.go` uses `HistoryReader.QueryHistory(HistoryQuery)` — the existing interface already accepts `NotBefore time.Time`. The Phase 2 restructure sets `HistoryQuery.NotBefore = MAX(req.NotBeforeMs as time, streamScopeFloor(info, stream))` before invoking `fetchHistoryFramesFromBus`. No `HistoryReader` API addition; both tier readers (JetStream hot ephemeral consumers and PG audit cold) already honor `NotBefore` per their existing contracts.

The wire protocol is unchanged. The two-tier Subscribe design (immutable `OptStartTime` perf hint + mutable filter-at-delivery enforcement) is documented above as a server-internal implementation choice.

### 6.3 Error opacity

`STREAM_ACCESS_DENIED` is returned for: hard-gate failures, I-17 membership denial, ABAC denial, expired session (already collapsed), missing session (already collapsed). The wire-level error code is identical across all denial reasons — preserving the existing opacity contract (`.claude/rules/grpc-errors.md`).

Server logs SHALL include a structured `denial_reason` slog attribute (values: `wrong_location`, `not_member`, `policy_denied`, `session_expired`, `session_not_found`) for debugging. The slog field never crosses the wire.

## 7. Phasing

Each phase MUST ship as a discrete bead under the parent design epic. Phase 1-3 close the immediate privacy leak; 4-6 complete the model.

| # | Phase | Beads | Closes |
|---|---|---|---|
| 1 | Schema + lifecycle | `sessions.location_arrived_at`+`guest_character_created_at` migration; `SessionInfo` fields; populate on fresh `SelectCharacter`; populate on `SelectCharacter` reattach; populate on `Subscribe.ReattachCAS`; **§5.1 character-move session-sync hook** | No semantic change yet (fields unused at query path) |
| 2 | Location-stream hard-gate + floor | `query_stream_history.go` restructure: location-stream classifier, hard-gate, staff-override stub, scope-floor for location | Primary `iwzt` privacy leak (web `QueryStreamHistory` path) |
| 3 | Subscribe replay floor | (a) `JetStreamSubscriber.OpenSession` first-create path computes `minFloor` across subjects and seeds `DeliverByStartTimePolicy` (§6.2 Tier 1); (b) both `OpenSession`-on-reattach and `SetFilters` query existing durable via `s.js.Consumer(...)` and preserve `DeliverPolicy`/`OptStartTime`/`OptStartSeq` verbatim (I-PRIV-8 — idempotent reseed); (c) Subscribe broadcaster (`server.go` dispatch) applies per-subject filter-at-delivery using current `streamScopeFloor` (§6.2 Tier 2 — load-bearing privacy gate); (d) `Subscribe.ReattachCAS` does NOT recreate the durable — idempotent OpenSession is a no-op on the consumer | Secondary leak path via Subscribe replay |
| 4 | Scene-join floor | `scene:*:*` streams use `FocusMembership.JoinedAt` in `streamScopeFloor` | Scene privacy parity |
| 5 | Staff ABAC override (real) | Seed the staff/admin policy (§6.1); make the `staffOverride` stub from Phase 2 use it; ensure I-PRIV-6 enforcement (floor still applies) | Staff debugging surface without violating I-PRIV-6 |
| 6 | Guest identity overlay (real) | Populate `SessionInfo.GuestCharacterCreatedAt` at guest-session creation; consult in `streamScopeFloor` when `IsGuest` | Closes name-reuse edge case |

Phase 1 is a prerequisite for all subsequent phases. **Phases 2 and 3 MUST ship together** (closing only one leaves a known-leaky path). Phase 4-6 can ship in any order after 2-3 ship.

Phase 1 explicitly ships the schema columns + population code but leaves `streamScopeFloor` returning zero (no enforcement) until Phase 2 — `QueryStreamHistory` is unchanged in Phase 1.

Phase 1 is privacy-leak-neutral but is NOT semantically inert: the §5.1 session-store sync hook on `MoveCharacter` corrects an undetected drift where `sessions.location_id` and `characters.location_id` were never kept in sync (verified via `rg "UpdateLocation"` across `internal/`; no path writes `sessions.location_id` on character move today). Any code that reads `sessions.location_id` to make a routing/policy decision now observes the same value `characters.location_id` already advertised. This is a positive correction. The plan stage MUST audit current readers of `sessions.location_id` and confirm none rely on the previously-stale value.

## 8. Tests

- **Integration (E2E):** `TestPrivacy_NewGuestSeesNoPreArrivalHistory` — connect guest A, A emits events, disconnect A, connect fresh guest B, assert B's `WebQueryStreamHistory` and Subscribe replay return zero events with timestamp < B's `SessionInfo.LocationArrivedAt`. Fails before Phase 2, passes after.
- **Integration:** `TestPrivacy_ReattachWithinTTLPreservesFloor` — character A connects at T0 in location L (LocationArrivedAt=T0), third party emits events at T1, A's transport drops at T2 (status=Detached), third party emits events at T3, A's `Subscribe.ReattachCAS` at T4 (within TTL); A's subsequent history query for `location:L` returns events with timestamps in [T0, T4] — INCLUDING the T1 and T3 events. The session row stayed alive through the disconnect; floor preserved at T0.
- **Integration:** `TestPrivacy_TTLExpiryEndsSessionFreshFloor` — character A connects at T0, A's transport drops at T1, no reattach for TTL+1, reaper deletes the row at T2; A logs in again at T3 → fresh `SelectCharacter` creates a new session with `LocationArrivedAt=T3`; events with timestamps in [T0, T3) are NOT visible to the new session.
- **Integration:** `TestPrivacy_TransportContinuity` — character X has one session row with two transport connections (web + telnet). The session row's `LocationArrivedAt = T0` is set at SelectCharacter time. Web's transport drops at T1 (session stays Active because telnet is still attached, OR transitions to Detached if both drop). Events emitted at T2 flow into the per-session JetStream durable consumer. Web's transport reattaches at T3 (within TTL — `Subscribe.ReattachCAS` is a no-op if Active, flips to Active if Detached). `QueryStreamHistory` for character X at T4 returns events with `timestamp >= T0`, INCLUDING the T2 events emitted during web's transport disconnect (the session row was held open; the per-session durable consumer received those events).
- **Integration:** `TestPrivacy_CharacterMoveResetsFloor` — character X in location A at T1, character X moves to B at T2, X's history query against location:A returns `STREAM_ACCESS_DENIED`; against location:B returns events from T2 only.
- **Integration:** `TestPrivacy_StaffOverrideBypassesGateNotFloor` — staff character with `roles=["staff"]` queries a location they're not in. Result: SUCCESS with events from `MAX(staff_arrival_at_other_location, staff_guest_floor)` only. Asserts I-PRIV-6.
- **Integration:** `TestPrivacy_SceneJoinFloor` — character X joins scene S at T, scene events at T-10..T-1 invisible to X via `QueryStreamHistory` for `events.<game_id>.scene.S.ic`, events at T+1 onward visible.
- **Integration (wire-opacity meta-test, I-PRIV-5):** `TestPrivacy_DenialWireOpacity` — exercise all five denial reasons (wrong location, not-member, policy denied, expired session, session not found) and assert wire-level code is identically `STREAM_ACCESS_DENIED` for each.
- **Unit:** `streamScopeFloor` table-driven cases per stream type (location, scene, character-own, plugin-owned), with and without `IsGuest=true`, with and without scope membership.
- **Unit:** `JetStreamSubscriber.OpenSession` start-time computation — verify `minFloor = max-across-subjects` and that `DeliverByStartTimePolicy` is set with the computed value; boundary cases (zero floor, single subject, multi-subject with mixed floors).
- **Unit (I-PRIV-8):** `JetStreamSubscriber` MUST consult `s.js.Consumer(ctx, StreamName, name)` before issuing `CreateOrUpdateConsumer`. Four test cases:
  - **Fresh OpenSession (no existing durable):** `Consumer(...)` returns `ErrConsumerNotFound`; `CreateOrUpdateConsumer` is called with computed `minFloor` in `OptStartTime`.
  - **OpenSession on reattach (existing durable):** `Consumer(...)` returns existing consumer with `OptStartTime = T0`; `CreateOrUpdateConsumer` is called with `OptStartTime == T0` (NOT a freshly-computed `minFloor`).
  - **SetFilters on move (existing durable):** same as reattach — preserve `OptStartTime`, only mutate `FilterSubjects`.
  - **Transient lookup error (fail closed):** stub `js.Consumer(...)` returns a non-`ErrConsumerNotFound` error (e.g., `nats.ErrConnectionClosed`); `OpenSession` MUST return the wrapped error and MUST NOT call `CreateOrUpdateConsumer`. Asserts the §6.2 third branch.
  All four failing → reseed/reattach would trigger NATS error 10012 OR a brief DeliverAllPolicy leak window.
- **Integration (I-PRIV-3 reattach durability):** open session A on character X at T0 (creates durable with `OptStartTime = T0`); third party emits events at T1; A detaches at T2; A reattaches at T3 (`LocationArrivedAt` UNCHANGED, still T0); A's Subscribe replay MUST deliver events with timestamp ∈ [T0, T3] via JetStream resume from last-acked-seq — durable not recreated, no policy mutation, original `OptStartTime` preserved. The filter-at-delivery floor remains `T0`; no events are dropped solely because of the disconnect window.
- **Unit:** Subscribe broadcaster per-subject filter-at-delivery — verify events with `Timestamp < streamScopeFloor(subject)` are dropped while events at-or-after the floor pass through.
- **Unit (manifest validation, I-PRIV-7):** plugin manifest schema (a) accepts an optional `history_scope:` declaration with a closed enum of values (e.g., `grid`, `scene`, `custom`); (b) rejects unknown values; (c) when a plugin manifest's `emits:` declares a namespace that is neither `location` nor `scene` (per `internal/plugin/manifest.go` validation — entries are bare namespaces, not subject patterns), validation MUST fail unless `history_scope:` is present. Test: manifest with plugin-owned namespace (e.g., `emits: ["custom_subject_ns"]`) and no `history_scope:` → validation error. Same manifest plus `history_scope: custom` → validates. This is what enforces "Silent inheritance of permissive semantics is forbidden."
- **Integration (I-PRIV-7):** an example plugin under `plugins/` declares `history_scope: custom` in its manifest and exercises its divergent semantics — failing this test signals a plugin shipped without honoring I-PRIV-7. Until a real plugin adopts the field, this test is a placeholder `t.Skip("no plugin uses history_scope yet")` paired with a meta-test guard (below) that explicitly counts the skip as I-PRIV-7's coverage tally.
- **Meta-test (`test/meta`):** assert each of I-PRIV-1 through I-PRIV-6 and I-PRIV-8 has at least one corresponding test asserting it by ID. I-PRIV-7 is satisfied either by the integration test above (when non-skipped) OR by the meta-test enumerating zero plugins with `history_scope:` declared (vacuously true). Failing meta-test signals an unverified invariant. This explicitly scopes I-PRIV-7 differently from I-PRIV-1..6, 8 — the invariant is forward-looking, applying when plugins adopt the field.

## 9. Invariants (RFC2119)

- **I-PRIV-1 (MUST):** A session SHALL NOT be able to read any event from any stream that occurred outside the time interval during which the **requesting session row itself** has existed for that stream's scope (in any of `StatusActive`, `StatusIdle`, or `StatusDetached`-within-TTL). The session row's lifetime — from `SelectCharacter` creation through quit/logout/boot/reaper-deletion — is the unit of session-level continuity; transport-level detach within TTL does NOT bound the window because the row persists and the durable consumer continues to receive events. Per-session-row enforcement: the schema enforces one active-or-detached session row per character (`idx_sessions_active_character` at `internal/store/migrations/000001_baseline.up.sql:221-222`), so all transports attached to a given character share that row's single floor. Exception: an explicit ABAC override grants `read_unrestricted_history` subject to the limited bypass defined in §6.1 (hard-gate location-match only; temporal floor still applies).
  Current presence (snapshot) is exempt from this floor — see
  `docs/superpowers/specs/2026-05-19-presence-snapshot-design.md` (I-PRES-2)
  for the carve-out rationale. The snapshot is a current-state fact, not a
  historical event, and is privacy-bounded by minimal-field exposure
  (no `arrived_at_ms`).
- **I-PRIV-2 (MUST):** Guest sessions SHALL have a temporal floor of `MAX(scope_floor, guest_character.CreatedAt)` applied to all stream history reads.
- **I-PRIV-3 (MUST):** `Subscribe.ReattachCAS` (transport reattach) AND `SelectCharacter` reattach SHALL leave the session's `LocationArrivedAt` UNCHANGED. The session row's continuity across the disconnect window (Detached → Active within TTL) is the unit of session-level identity; the floor is set at session-create (§5 row 1) and only advances on character-move (§5 row 5). `Subscribe.ReattachCAS` MUST NOT change `DeliverPolicy`, `OptStartTime`, or `OptStartSeq` on the existing durable consumer; `FilterSubjects` MAY change. Privacy enforcement on reattach is performed by the per-subject filter-at-delivery (§6.2 Tier 2) against the unchanged `LocationArrivedAt`.
- **I-PRIV-4 (MUST NOT):** Idle status change SHALL NOT advance `LocationArrivedAt`. Transport-level reattach (`Subscribe.ReattachCAS`) and `SelectCharacter` reattach SHALL NOT advance `LocationArrivedAt`. (Schema enforces one session row per character; the previous wording's "other concurrent sessions for the same character" clause was imprecise — multiple transport connections attach to the single session row, not to separate sessions.)
- **I-PRIV-5 (MUST):** All denial paths (hard-gate, I-17, ABAC, expired session, missing session) SHALL return the same wire-level error code (`STREAM_ACCESS_DENIED`). Internal `denial_reason` slog attribute is debugging-only and SHALL NOT cross the wire.
- **I-PRIV-6 (MUST):** ABAC staff override SHALL bypass the hard-gate location-match check only; it SHALL NOT bypass the temporal floor.
- **I-PRIV-7 (MUST):** Plugin-owned subjects that adopt history-replay semantics divergent from this spec's grid/scene model SHALL declare their semantics in the plugin's manifest under `history_scope:` and SHALL be exercised by an explicit test. Silent inheritance of permissive semantics is forbidden.
- **I-PRIV-8 (MUST):** Both `JetStreamSubscriber.OpenSession` (including OpenSession-on-reattach) and `JetStreamSubscriber.SetFilters` SHALL query the existing durable via `s.js.Consumer(ctx, StreamName, name)` before issuing `CreateOrUpdateConsumer`. When the durable exists, the new config's `DeliverPolicy`, `OptStartTime`, and `OptStartSeq` MUST be copied verbatim from `existing.CachedInfo().Config`; only `FilterSubjects` may mutate. When the durable does not exist (`jetstream.ErrConsumerNotFound`), the config is built fresh with `minFloor` semantics per §6.2 Tier 1. Sending a different `DeliverPolicy`/`OptStartTime`/`OptStartSeq` against an existing consumer triggers NATS server error 10012 and breaks the session. NATS is the source of truth for these immutable fields; in-process or DB-row caching MUST NOT be used.

## 10. Out of scope

- Channels (epic `0sc`) — channel history semantics will be designed in that epic; I-PRIV-7 applies.
- Connect-time latency (`87qu`) — distinct concern; this spec's §6.2 cursor-by-time choice is favorable to that work but does not commit to a latency budget.
- Connect-time own-event-rendering race (`fujt`) — distinct concern; ships independently.
- Existing event retention policy (JetStream + PG audit) — unchanged.
- Cold-tier crypto AAD changes (`dj95.1-4`) — unrelated.

## 11. Related work

- `internal/access/policy/seed.go:104` — existing `admin` role grant template (mirror for `read_unrestricted_history`)
- `internal/access/policy/attribute/character.go:21-107` — `CharacterProvider` and `RoleResolver` (resolves `principal.character.roles`)
- `internal/access/policy/types/types.go:132-150` — `AccessRequest` shape (`Subject`, `Action`, `Resource`, `Context`)
- `.claude/rules/event-interfaces.md` — `HistoryReader` interface, subject naming
- `.claude/rules/grpc-errors.md` — error opacity and code-translation discipline
- `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` — replay/cursor model
- `docs/repository-audit/2026-05-13/layer-review.md` — adjacent audit findings on the same call surface

## 12. Review history

- **Round 1 (2026-05-17):** design-reviewer NOT READY — 5 blocking, 5 non-blocking. All addressed in v2.
- **Round 2 (2026-05-17):** design-reviewer NOT READY — 2 blocking + 4 non-blocking. Round-1 blockers all resolved; round 2 surfaced a structural error in v2's §6.2 (`LowerBoundByTime` hung on the wrong interface — Subscribe doesn't consume `HistoryReader`) and a missing I-PRIV-7 test. Addressed in this v3:
  - **B-R2.1:** §6.2 rewritten — drop the invented `HistoryReader.LowerBoundByTime`, use JetStream's native `DeliverByStartTimePolicy` (already at `hot_jetstream.go:283`) with `minFloor` across subjects, paired with per-subject filter-at-delivery in the Subscribe broadcaster for over-delivered events. `QueryStreamHistory` uses the existing `HistoryQuery.NotBefore` field — no API addition.
  - **B-R2.2:** I-PRIV-7 now has a unit test (manifest validation), an integration test (initially `t.Skip` until a plugin adopts `history_scope:`), and a meta-test scope adjustment that satisfies I-PRIV-7 vacuously when zero plugins have declared.
  - **N-R2.1, N-R2.2:** §3 staff-override cell rewritten — disjunct phrasing + temporal-floor reminder.
  - **N-R2.3:** §7 leak-neutrality note rewritten to acknowledge the sessions/characters drift correction.
  - **N-R2.4:** §2 gains a multi-session worked example.

- **Round 3 (2026-05-17):** design-reviewer NOT READY — 2 blocking + 3 non-blocking. Round 3 caught a NATS JetStream immutability constraint that v3 §6.2 was implicit about. Addressed in v4:
  - **B-R3.1 (immutability):** §6.2 rewritten into explicit two-tier model: Tier 1 `OptStartTime` as a perf hint set at consumer creation only (immutable per NATS server error 10012); Tier 2 per-subject filter-at-delivery as the load-bearing privacy gate. `SetFilters` MUST replay original `OptStartTime` verbatim. New I-PRIV-8 codifies the constraint.
  - **B-R3.2 (reattach):** §6.2 explicit reattach paragraph + I-PRIV-3 amendment: `Subscribe.ReattachCAS` MUST NOT recreate the durable; enforcement on reattach is the filter, not `OptStartTime`. New integration test asserts this.
  - **N-R3.1:** §6.2 cost analysis softened to acknowledge reattach scan-then-discard cost (bounded by `(new floor − last-acked timestamp) × event rate`).
  - **N-R3.2:** I-PRIV-7 manifest validation strengthened to require closed-enum + rejection of plugin-owned subjects without `history_scope:` declaration. Enforces "silent inheritance forbidden."
  - **N-R3.3:** Phase 3 row expanded to enumerate `SetFilters` + reattach invariants alongside `OpenSession`.
- **Round 4 (2026-05-17):** design-reviewer NOT READY — 2 blocking + 3 non-blocking. Round 4 caught that v4's "store `OptStartTime` on the `jetStreamSessionStream`" rule didn't survive across `OpenSession`-on-reattach (fresh struct constructed per call). Addressed in v5:
  - **B-R4.1:** §6.2 replaces in-process storage with a NATS-source-of-truth pattern — both `OpenSession` (first AND reattach) and `SetFilters` query `s.js.Consumer(...)` first; on hit, copy `DeliverPolicy`/`OptStartTime`/`OptStartSeq` verbatim from existing config; on `ErrConsumerNotFound`, compute fresh `minFloor`. Idempotent by construction; error 10012 impossible.
  - **B-R4.2:** I-PRIV-8 amended to require the NATS lookup pattern (not in-process or DB-row caching). I-PRIV-3 amended with explicit reattach-not-recreate semantics (idempotent OpenSession is a no-op on the durable). §8 I-PRIV-8 unit test expanded to three cases (fresh create, OpenSession-on-reattach, SetFilters).
  - **N-R4.1:** I-PRIV-8 moved to end of §9 for monotonic numbering.
  - **N-R4.2:** §8 I-PRIV-7 manifest test reworded — manifest entries are bare *namespaces* per `internal/plugin/manifest.go` validation (not subject patterns).
  - **N-R4.3:** subsumed into B-R4.1 fix.
- **Round 5 (2026-05-17):** design-reviewer NOT READY — 2 blocking + 3 non-blocking. Round 5 caught two last-mile precision issues. Addressed in this v6:
  - **B-R5.1:** §6.2 pseudocode handled only `nil` and `ErrConsumerNotFound` lookup-error branches; transient errors (NATS connection drop, context cancel) silently fell through with zero `DeliverPolicy` → potential error 10012 or `DeliverAllPolicy` leak window. Added explicit fail-closed third branch returning `EVENTBUS_CONSUMER_LOOKUP_FAILED` without invoking `CreateOrUpdateConsumer`. §8 I-PRIV-8 gains a fourth unit test for the transient-error path. "Impossible by construction" softened to "unreachable under documented call discipline."
  - **B-R5.2:** I-PRIV-3 "MUST NOT recreate or mutate" reworded to explicitly enumerate the immutable fields — "MUST NOT change `DeliverPolicy`, `OptStartTime`, or `OptStartSeq`; `FilterSubjects` MAY change." Resolves the literal-reading tension with §6.2 design.
  - **N-R5.1:** §6.2 pseudocode now notes that `Durable`/`Name`/`AckPolicy`/`MaxAckPending`/`AckWait`/`InactiveThreshold` MUST match existing OpenSession/SetFilters defaults — only the three start-policy fields come from `existing.CachedInfo().Config`.
  - **N-R5.2:** "Impossible by construction" claim softened (paired with B-R5.1 fix).
  - **N-R5.3:** Fourth unit test added to §8 I-PRIV-8 (paired with B-R5.1 fix).

- **Round 6 (2026-05-22):** wording-correction pass during iwzt.17 implementation (`TestPrivacy_TransportContinuity`). The original §2 worked example and §8 test description used "session" loosely to mean either the durable game session row OR a transport connection, which contradicted the schema's `idx_sessions_active_character` unique constraint (one active-or-detached session row per character). Detailed lifecycle rules in §5 + I-PRIV-3 were already schema-faithful; the imprecision was narrative only.
  - §2 worked example rewritten to use "transport client" terminology consistently; the "Other concurrent sessions ... independent floors" sentence replaced with "transports share the session row's single floor".
  - §8 `TestPrivacy_MultiSessionContinuity` renamed to `TestPrivacy_TransportContinuity` and rewritten to describe one session row + two transport connections.
  - I-PRIV-1 reworded — the "other concurrent sessions ... independent floors" clause replaced with a citation of the schema invariant.
  - I-PRIV-4 second sentence replaced — "Other active sessions for the same character" → "Transport-level reattach and `SelectCharacter` reattach SHALL NOT advance `LocationArrivedAt`" with an explicit schema-imprecision footnote.

  No code/schema change: spec language now matches the lifecycle rules in §5 and the I-PRIV-3 reattach semantics already shipped via iwzt.8/.10/.11.

See bead `holomush-iwzt` notes for full round-by-round audit trail.
<!-- adr-capture: sha256=3e4d189e14c7fb62; session=cli; ts=2026-05-21T12:24:10Z; adrs= -->

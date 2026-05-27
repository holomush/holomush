<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Presence snapshot — current-state RPC, not event replay

| Field        | Value                                                                 |
| ------------ | --------------------------------------------------------------------- |
| Status       | Draft                                                                 |
| Bead         | `holomush-5b2j` — Presence list MUST be derived from current-state query, not event replay |
| Blocks       | `holomush-iwzt.15` — Tier 2 filter-at-delivery (I-PRIV-1)             |
| Related spec | `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` (I-PRIV-1..8) |
| Created      | 2026-05-19                                                            |
| Author       | Sean Brandt (with `dev-flow:brainstorming`)                           |

## 1. Problem

The web terminal populates its presence sidebar by listening for `arrive`/`leave`
events on the Subscribe stream
(`web/src/routes/(authed)/terminal/+page.svelte:381-401`). When session B joins
a location after session A is already there, B has no way to learn about A's
presence: A's `arrive` event was emitted before B's `LocationArrivedAt` and is
correctly filtered out by I-PRIV-1 (Tier 2 filter-at-delivery, implemented in
`holomush-iwzt.15`).

The privacy invariant is working as designed; the presence-via-event-replay UX
is the bug. Today the test
`web/e2e/terminal.spec.ts:136` ("presence list shows self and other
connections") passes only because the Tier 2 filter is effectively a no-op in
production (see `holomush-ofpi`). Once iwzt.15 lands, the test fails for the
correct privacy reason — proving the presence model needs to change.

**Decision:** presence MUST be populated by a current-state query at Subscribe
open, with event-stream `arrive`/`leave` deltas applied additively. The
snapshot is the source of truth for who-is-here-now; the event stream is the
source of truth for changes.

## 2. Decisions

### D-1. Semantic of "presence"

Presence at a location is the set of **sessions with `Status=Active` at that
location**, regardless of `GridPresent`. This matches `.claude/rules/terminology.md`
("presence = active sessions, derived from session store") and is
self-consistent with the `LocationArrivedAt` floor used by the I-PRIV-1 Tier 2
filter.

`GridPresent` is **not** the source of truth for presence; it remains a
secondary signal (terminal/telnet attachment indicator) and MAY be surfaced as
a decoration on a `PresenceEntry` in a future revision.

### D-2. Context model — single, server-decided

The RPC returns presence for the session's **current focus context**, not for
an arbitrary location. The server decides the context from the session's state:

- `len(info.FocusMemberships) == 0` → **LOCATION** context. Resolver returns
  Active sessions at `info.LocationID`.
- `len(info.FocusMemberships) > 0` → **UNIMPLEMENTED** (5b2j scope). The
  scene resolver is a follow-up bead. The dispatch rule for multi-membership
  primary-focus selection is also deferred to that bead.

A single-context response keeps the terminal UX simple ("who's in my current
focus?"). A future per-stream RPC (option 2 in brainstorming) can be added
incrementally by promoting the server-side per-stream resolver to a public
surface; the wire shape introduced here is forward-compatible.

### D-3. Bead scope — wire shape now, location resolver only

The proto introduced in 5b2j supports both LOCATION and SCENE contexts and
emits `PresenceState` values `ACTIVE`/`DETACHED`/`INACTIVE`. Only the LOCATION
resolver is implemented; only `PRESENCE_STATE_ACTIVE` is ever emitted in this
bead. The scene resolver, which will emit `DETACHED`/`INACTIVE` for scene
members whose sessions are not currently Active, lands in a follow-up bead
without a wire-breaking change.

### D-4. Snapshot is exempt from the I-PRIV-1 temporal floor

The privacy invariants (I-PRIV-1..8 in the history-scope-privacy spec) gate
which **events** a session may receive. Presence is a current-state fact, not
a historical event: a new arrival walking into a room sees the people in the
room. The snapshot MUST therefore be exempt from `LocationArrivedAt` filtering.

To prevent the snapshot from becoming a side-channel for the duration-of-
presence data the temporal floor protects, `PresenceEntry` MUST carry only
`character_id`, `character_name`, and `state` — **no `arrived_at_ms`, no
first-seen timestamp, no session age**. The set membership and current state
are visible; everything else is not.

### D-5. ABAC: new `list_presence` action, default-deny

A new ABAC action `list_presence` on `resource="location:<id>"` gates the
snapshot RPC. The seed policy is a one-line addition modelled on the existing
`list_characters` rule:

```text
permit(principal is character,
       action in ["list_presence"],
       resource is location)
  when { resource.location.id == principal.character.location };
```

Admin remote-presence is covered by the existing super-rule
(`permit ... when { "admin" in principal.character.roles }` in
`internal/access/policy/seed.go`) — no special-casing required. Same-location
players are allowed; everyone else is denied by default.

### D-6. Admin remote-query plumbing — deferred

The request shape ships with only `session_id` in 5b2j. Admin remote-query
(e.g., querying presence at a location the admin is not currently in) requires
a `target_location_id` field or a separate `AdminListPresence` RPC. Neither is
on a current admin-UX path. Adding the wire field now would commit to one
shape over the other prematurely. The ABAC policy is already correct for
admin access via the super-rule — only the RPC surface is deferred.

### D-7. RPC naming and shape — bare nouns

The RPC is `CoreService.ListFocusPresence(...)`. Enums are bare nouns
(`PresenceContext`, `PresenceState`) matching the established proto style —
see `EventChannel` at `api/proto/holomush/core/v1/core.proto:213` and
`api/proto/holomush/web/v1/web.proto:13`. Field naming on the response is
`context` + `context_id` — the pairing makes the meaning self-documenting
without a `_type` suffix.

### D-8. Client model — keyed by `character_id`, snapshot + additive deltas

The terminal's presence state becomes a `Map<characterID, {name, state}>`. The
snapshot seeds the map; Subscribe `arrive`/`leave` events update it additively
with idempotent semantics (re-adding an existing id is a no-op; removing a
missing id is a no-op). Keying by stable `character_id` (rather than the
string name used today) is what makes the snapshot/event race resolution
trivial: any race between snapshot age and Subscribe buffer order collapses
to set-membership semantics.

The current backfill→presence path is **removed**. Backfill events no longer
update presence; the snapshot is authoritative for initial state and Subscribe
deltas are authoritative for changes. This resolves the TODO at
`+page.svelte:385` (referenced as `holomush-1tvn.15`).

## 3. RPC contract

### 3.1 Proto additions — `api/proto/holomush/core/v1/core.proto`

```proto
enum PresenceContext {
  PRESENCE_CONTEXT_UNSPECIFIED = 0;
  PRESENCE_CONTEXT_LOCATION    = 1;
  PRESENCE_CONTEXT_SCENE       = 2;  // wire-reserved; resolver in follow-up bead
}

enum PresenceState {
  PRESENCE_STATE_UNSPECIFIED = 0;
  PRESENCE_STATE_ACTIVE      = 1;
  PRESENCE_STATE_DETACHED    = 2;  // emitted by future scene resolver
  PRESENCE_STATE_INACTIVE    = 3;  // emitted by future scene resolver
}

message PresenceEntry {
  string        character_id   = 1;
  string        character_name = 2;
  PresenceState state          = 3;
  // No timestamps. See D-4.
}

message ListFocusPresenceRequest {
  RequestMeta meta                 = 1;
  string      player_session_token = 2;
  string      session_id           = 3;
}

message ListFocusPresenceResponse {
  ResponseMeta            meta       = 1;
  PresenceContext         context    = 2;
  string                  context_id = 3;  // LOCATION → location_id; SCENE → scene_id
  repeated PresenceEntry  entries    = 4;
}

service CoreService {
  // ... existing methods ...
  rpc ListFocusPresence(ListFocusPresenceRequest) returns (ListFocusPresenceResponse);
}
```

### 3.2 Gateway proxy — `api/proto/holomush/web/v1/web.proto`

Per `.claude/rules/gateway-boundary.md`, the gateway proxies; it does not
compute. Add a `WebListFocusPresence` RPC that forwards to
`CoreService.ListFocusPresence`. Request/response messages mirror the Core
versions but use the `Web*` prefix per existing gateway convention.

### 3.3 Error codes

| Code                | Trigger                                                                                              |
| ------------------- | ---------------------------------------------------------------------------------------------------- |
| `INVALID_ARGUMENT`  | `session_id` is empty                                                                                |
| `SESSION_NOT_FOUND` | All ownership-validation failures collapse here (enumeration-safe, per `ListSessionStreams` pattern) |
| `SESSION_EXPIRED`   | `info.IsExpired()` returns true                                                                      |
| `PERMISSION_DENIED` | ABAC `list_presence` evaluator returns deny                                                          |
| `UNIMPLEMENTED`     | Session has any `FocusMemberships` (scene resolver is a follow-up bead — see D-2)                    |
| `INTERNAL`          | Underlying store error; full detail logged, wire message generic per `.claude/rules/grpc-errors.md`  |

## 4. Server-side implementation

### 4.1 SessionStore — use existing `ListActiveByLocation`

The handler MUST use the existing method on the `session.Store` interface:

```go
// internal/session/session.go:314
ListActiveByLocation(ctx context.Context, locationID ulid.ULID) ([]*Info, error)
```

Implementations are already in place:

- `internal/session/memstore.go:301` — linear scan over the in-memory map.
- `internal/store/session_store.go:534` (`PostgresSessionStore`) — query:
  `SELECT ... FROM sessions WHERE location_id = $1 AND status = 'active'`.
- `internal/session/mocks/mock_Store.go:692` — already generated.

No additions to the `Store` interface or any implementation are required for
the storage layer. The 5b2j work is purely handler + ABAC + proto + client.

**Index status (grounded):** the `sessions` table has no
`(location_id, status)` composite index as of `internal/store/migrations/000001_baseline.up.sql`.
The relevant indexes today are:

- `(character_id) WHERE status IN ('active', 'detached')` — unique partial,
  enforces "one active-or-detached session per character" (line 222 of the
  baseline migration; relevant to I-PRES-9 below).
- `(status) WHERE status = 'detached'` — partial, for reaper sweeps.

The current `ListActiveByLocation` query falls back to a scan filtered by
`location_id` and an inline status predicate. A `CREATE INDEX idx_sessions_active_location ON sessions (location_id) WHERE status = 'active'`
migration MAY be added at plan stage if expected query volume warrants it.
Not required for correctness; the plan decides based on telemetry expectations.

### 4.2 Handler — `internal/grpc/list_focus_presence.go`

The handler follows the `ListSessionStreams` pattern (`internal/grpc/list_session_streams.go`):

```text
1. requestID = req.Meta.RequestId (for structured logging)
2. if req.SessionId == "" → INVALID_ARGUMENT
3. ValidateSessionOwnership(playerSessionRepo, sessionStore, token, sessionID)
     any failure → collapse to SESSION_NOT_FOUND (enumeration-safe)
4. info := sessionStore.Get(sessionID)
     not-found → SESSION_NOT_FOUND
     err      → INTERNAL
5. if info.IsExpired() → SESSION_EXPIRED
6. if len(info.FocusMemberships) > 0 → UNIMPLEMENTED (D-2)
7. if info.LocationID.IsZero() →
     return {context=LOCATION, context_id="", entries=[]}
     (session has no location yet — not an error)
8. ABAC: engine.Evaluate(
        subject=info.CharacterID,
        action="list_presence",
        resource="location:" + info.LocationID.String())
     deny → PERMISSION_DENIED
9. sessions := sessionStore.ListActiveByLocation(info.LocationID)  // §4.1
10. names := characterNameResolver.Names(sessionCharacterIDs)
11. entries := []
    seenChars := set{}  // dedupe defense-in-depth (I-PRES-9)
    for s in sessions:
      if s.CharacterID in seenChars: continue
      seenChars.add(s.CharacterID)
      name := names[s.CharacterID]
      if name == "": log warning; skip entry (graceful degradation)
      entries.append({s.CharacterID, name, PRESENCE_STATE_ACTIVE})
12. return {meta, context=LOCATION, context_id=info.LocationID, entries}
```

### 4.3 Character-name resolver

A narrow interface, injected at server construction:

```go
type characterNameResolver interface {
    Names(ctx context.Context, ids []ulid.ULID) (map[ulid.ULID]string, error)
}
```

Default implementation queries the world repo / character repo in a single
batched `SELECT id, name FROM characters WHERE id = ANY($1)`. Test
implementations stub the map directly.

This keeps the handler clean of repo plumbing and preserves the gateway-
boundary invariant (the resolver MUST be a server-side dependency, not a
gateway-side call).

### 4.4 ABAC seed policy

Add to `internal/access/policy/seed.go`:

```go
{
    Name:        "list_presence_same_location",
    Description: "Allow characters to query presence at their current location",
    DSLText:     `permit(principal is character, action in ["list_presence"], resource is location) when { resource.location.id == principal.character.location };`,
    SeedVersion: <next available; today the max in seed.go is 5>,
},
```

The `SeedVersion` field MUST be populated per the established convention in
`internal/access/policy/seed.go` (all 35 existing entries set it; today's
range is 1..5). The plan author MUST verify the next-available version
number at implementation time, since other in-flight work may also be
adding seed entries.

Migration: the existing ABAC seed migration mechanism (see
`internal/access/policy/seed.go` callers) MUST replay the new rule
idempotently. No additional migration file is required if the seed
mechanism re-applies seed policies on startup.

## 5. Client integration

**File:** `web/src/routes/(authed)/terminal/+page.svelte`

### 5.1 Data structure change

```ts
// Before: Set<string> keyed by character_name (in addPresence/removePresence)
// After:
const presence = new SvelteMap<
  string /* characterID */,
  { name: string; state: PresenceState }
>();
```

Keying by `character_id` is the load-bearing change — it makes
add/remove idempotent and resolves any snapshot-vs-event-order race by
construction.

### 5.2 Flow in `hydrateAndStream`

```text
T1: client.webListSessionStreams({sessionId})         → streams[]
T2: spawn subscribePromise (Subscribe opens; events → liveBuffer)
T3: parallel ─┬─ backfillStreams(streams[])           → messages
              └─ client.webListFocusPresence({sessionId}) → snapshot
T4: presence.clear(); for e in snapshot.entries: presence.set(e.character_id, {name: e.character_name, state: e.state})
T5: drain liveBuffer:
      • messages:  dedup by eventID, route (unchanged — backfill movement
                                             events do NOT update presence;
                                             see §5.3)
      • movement:  arrive → presence.set(actorCharacterID, {name, state: ACTIVE})
                   leave  → presence.delete(actorCharacterID)
T6: backfillDone = true; live events bypass buffer
```

### 5.3 Removals

- The current backfill→presence delta path (`+page.svelte:380-394`,
  the `if (evRec.category === 'movement')` block inside the seen-event
  dedup) is **removed**. Backfill events no longer touch the presence map.
- The TODO at `+page.svelte:385` referencing `holomush-1tvn.15` is
  resolved by this removal and SHOULD be deleted in the same change.
- `addPresence(actor: string)` / `removePresence(actor: string)` callers
  are updated to take `characterId: string` and look up display name from
  the event payload's actor field. Per `internal/core/engine.go:76`, the
  event's `Actor.ID` is the character ULID — already the right key.

### 5.4 Error handling

| Failure mode                   | Client behaviour                                                       |
| ------------------------------ | ---------------------------------------------------------------------- |
| Snapshot RPC fails (network)   | Render empty presence; log debug; do NOT block terminal usability     |
| Snapshot returns UNIMPLEMENTED | Render empty presence; log "presence unavailable for scene focus"     |
| Stale session                  | Existing `isStaleSession(e)` path at `+page.svelte:361-364`            |
| Subscribe `arrive` for char already in map | No-op (Map.set with same key/value)                        |
| Subscribe `leave` for char not in map      | No-op (Map.delete of absent key)                           |

## 6. Privacy invariants — interaction with I-PRIV-1..8

5b2j's snapshot path carves out a small but explicit exemption from the
history-scope-privacy spec. The relevant cross-references:

- **I-PRIV-1 (Tier 1 / Tier 2 floor):** unchanged. Continues to apply to
  every event flowing through `Subscribe` and `QueryHistory`. The
  snapshot RPC does not produce events.
- **I-PRIV-8 (NATS-as-source-of-truth for OptStartTime):** unchanged.
  The snapshot RPC does not interact with JetStream.
- **New cross-reference required:** `2026-05-17-history-scope-privacy-design.md`
  SHOULD link to this spec at I-PRIV-1 with a one-line note that current
  presence (5b2j, I-PRES-2) is timeless and exempt from the temporal
  floor by design.

## 7. Invariants

| ID         | Invariant                                                                                                                                                          | Severity    |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------- |
| **I-PRES-1** | The snapshot MUST return only sessions with `Status=Active`. Detached and Expired sessions MUST be excluded.                                                       | Correctness |
| **I-PRES-2** | The snapshot MUST be exempt from the I-PRIV-1 temporal floor (`LocationArrivedAt`). Current state is timeless.                                                     | Correctness |
| **I-PRES-3** | All ownership-validation failures MUST collapse to `SESSION_NOT_FOUND` (enumeration-safe per existing `ListSessionStreams` pattern).                               | Security    |
| **I-PRES-4** | The snapshot RPC MUST be ABAC-gated by `action="list_presence"` on `resource="location:<id>"`. Default-deny. Same-location allowed via seed; admin via super-rule. | Security    |
| **I-PRES-5** | If `len(info.FocusMemberships) > 0`, the RPC MUST return `UNIMPLEMENTED`. No silent location fallback for scene-focused sessions.                                  | Correctness |
| **I-PRES-6** | The caller's own session MUST be included in the response when its status and location qualify.                                                                    | UX          |
| **I-PRES-7** | `PresenceEntry` MUST contain exactly three fields: `character_id`, `character_name`, `state`. No timestamps. No duration-of-presence data.                         | Privacy     |
| **I-PRES-8** | The client presence map MUST be keyed by `character_id`. Repeated add for the same id MUST be a no-op; remove for an absent id MUST be a no-op.                    | Correctness |
| **I-PRES-9** | The response MUST deduplicate by `character_id`. Schema enforces "one active-or-detached session per character" via the unique partial index `(character_id) WHERE status IN ('active', 'detached')` at `internal/store/migrations/000001_baseline.up.sql:222`; the wire-level dedup is defense-in-depth against brief reattach-CAS or guest-create races where two rows can transiently satisfy the predicate before the unique constraint fires. | Correctness |

**Meta-test:** a test in the implementation SHOULD assert that every `I-PRES-*`
invariant in this spec is covered by at least one test in §8.1–§8.5. Matches
the meta-test pattern in `2026-05-17-history-scope-privacy-design.md` §8 (the
test/meta package asserts each I-PRIV-* has at least one corresponding test
asserting it by ID).

## 8. Test plan

### 8.1 Server unit tests (`internal/grpc/list_focus_presence_test.go`)

| ID  | Test                                                                                                | Covers                |
| --- | --------------------------------------------------------------------------------------------------- | --------------------- |
| U-1 | happy path — 2 Active sessions at same location → both in response, caller included                | I-PRES-1, I-PRES-6   |
| U-2 | empty location — 0 active sessions → entries=[] (not nil error)                                     | I-PRES-1             |
| U-3 | Detached session at same location → excluded                                                        | I-PRES-1             |
| U-4 | Expired session at same location → excluded                                                         | I-PRES-1             |
| U-5 | Sessions at OTHER locations → excluded (cross-location isolation)                                   | I-PRES-1             |
| U-6 | empty `info.LocationID` → context=LOCATION, context_id="", entries=[]                              | (non-error path)      |
| U-7 | bad player_session_token → SESSION_NOT_FOUND                                                        | I-PRES-3             |
| U-8 | foreign session_id (ownership mismatch) → SESSION_NOT_FOUND                                         | I-PRES-3             |
| U-9 | unknown session_id → SESSION_NOT_FOUND                                                              | I-PRES-3             |
| U-10 | expired session → SESSION_EXPIRED                                                                  | (existing pattern)    |
| U-11 | ABAC deny (no `list_presence` policy seeded) → PERMISSION_DENIED                                   | I-PRES-4             |
| U-12 | ABAC allow via same-location seed policy → success                                                  | I-PRES-4             |
| U-13 | ABAC allow via admin super-rule → success even when at different location (verifies remote ABAC)   | I-PRES-4             |
| U-14 | session with non-empty FocusMemberships → UNIMPLEMENTED                                             | I-PRES-5             |
| U-15 | character-name resolver returns "" for one id → entry skipped, warning logged, others returned     | (graceful degradation) |
| U-16 | underlying store returns 2 rows for same character_id → response deduplicated to 1 entry            | I-PRES-9             |
| U-17 | PresenceEntry response shape — exactly 3 fields, no extra timestamps                                | I-PRES-7             |

### 8.2 SessionStore unit tests (`internal/session/memstore_test.go`, `internal/store/session_store_integration_test.go`)

| ID  | Test                                                                          | Covers              |
| --- | ----------------------------------------------------------------------------- | ------------------- |
| S-1 | ListActiveByLocation returns multiple Active sessions at same location        | I-PRES-1            |
| S-2 | ListActiveByLocation excludes Detached sessions                                | I-PRES-1            |
| S-3 | ListActiveByLocation excludes Expired sessions                                 | I-PRES-1            |
| S-4 | ListActiveByLocation returns empty slice (not nil err) for empty location     | (graceful)          |
| S-5 | ListActiveByLocation filters strictly by location_id (cross-location isolation) | I-PRES-1          |
| S-6 | ListActiveByLocation is consistent across MemStore and PostgresSessionStore   | (parity)            |

### 8.3 Integration tests (Ginkgo, `//go:build integration`)

| ID  | Test                                                                                       | Covers           |
| --- | ------------------------------------------------------------------------------------------ | ---------------- |
| I-1 | Session A connects → session B connects → B's `ListFocusPresence` includes A within 1s    | **AC4**; I-PRES-2 |
| I-2 | Same as I-1, but with iwzt.15 Tier 2 filter ACTIVE — verifies snapshot bypasses the floor | **AC3**; I-PRES-2 |
| I-3 | A and B connect within 50ms — neither ends with duplicate entries in their presence list  | I-PRES-9, race resolution |
| I-4 | Session A connects; B's snapshot before A's `arrive` event reaches B → A is in B's presence anyway | I-PRES-2 |

### 8.4 Client tests (vitest)

| ID  | Test                                                                                       | Covers   |
| --- | ------------------------------------------------------------------------------------------ | -------- |
| C-1 | Snapshot returns 2 entries → presence Map has 2 keys after seed                           | I-PRES-8 |
| C-2 | Snapshot then Subscribe `arrive(C)` for char already in snapshot → Map size unchanged     | I-PRES-8 |
| C-3 | Subscribe `leave(C)` for char NOT in snapshot → no error, no spurious state              | I-PRES-8 |
| C-4 | Snapshot RPC fails → presence Map empty; terminal still functional                        | (graceful) |
| C-5 | Snapshot UNIMPLEMENTED → presence Map empty; debug log emitted                            | I-PRES-5 |

### 8.5 E2E tests (Playwright)

| ID  | Test                                                | Covers     |
| --- | --------------------------------------------------- | ---------- |
| E-1 | `web/e2e/terminal.spec.ts:136` — passes with iwzt.15 Tier 2 active | **AC3** |
| E-2 | (new, optional) — A and B both connected; A disconnects → B's presence updates to remove A within 1s | (leave delta) |

## 9. Documentation impact

| File                                                                  | Change                                                                                                             |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md`   | Add one-line cross-reference at I-PRIV-1: current presence (5b2j, I-PRES-2) is exempt from the floor.              |
| `docs/adr/`                                                           | Two ADRs to capture: (1) snapshot-as-source-of-truth for current state; (2) `list_presence` ABAC action.           |
| `site/docs/contributing/event-store.md`                               | Add a brief note: current-state queries (presence) use dedicated RPCs that bypass `HistoryReader`.                |
| `site/docs/reference/`                                                | Auto-regenerated from proto by `task pr-prep`.                                                                     |
| `.claude/rules/terminology.md`                                        | One-line addition: presence "is queryable via `ListFocusPresence`."                                                |
| `.claude/rules/event-interfaces.md`                                   | One-line addition: current-state queries (e.g., presence) bypass the EventBus; see `ListFocusPresence`.            |
| `holomush-iwzt.15` bead notes                                         | Add note after spec lands: 5b2j unblock pathway confirmed.                                                         |

## 10. Out of scope (5b2j)

- **Scene resolver.** SCENE-context responses return `UNIMPLEMENTED`. Follow-up
  bead will implement the resolver and define multi-membership tiebreaker.
- **Per-stream presence RPC** (option 2 in brainstorming). Designed for, not
  shipped in, this bead.
- **`arrived_at_ms` exposure.** Deliberate omission per D-4; future
  duration-of-presence UX is a separate privacy-reviewed decision.
- **Admin remote-query plumbing** (target_location_id field or AdminListPresence
  RPC). ABAC policy is in; wire surface is deferred per D-6.
- **`(location_id, status)` index migration.** Plan SHOULD verify existing
  indexes during implementation; migration only required if grounding shows
  it absent.
- **Per-character visibility (invisible/hidden roles).** No machinery exists
  today; out of scope until a use case appears.
- **Backfill behaviour changes.** Spec touches only the `arrive`/`leave`-
  feeds-presence path; message backfill is unchanged.

## 11. References

- Blocked bead: `holomush-iwzt.15` (Tier 2 filter-at-delivery)
- Related epic: `holomush-iwzt` (History scope privacy)
- Privacy spec: `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md`
- Privacy ADRs: `docs/adr/holomush-wxty-*.md`, `holomush-rc8b-*.md`, `holomush-kmac-*.md`, `holomush-ghpx-*.md`, `holomush-jhl5-*.md`
- Pattern reference: `internal/grpc/list_session_streams.go` (enumeration-safe handler shape)
- ABAC pattern: `internal/access/policy/seed.go` `list_characters` seed (the model for D-5)
- Terminology: `.claude/rules/terminology.md`
- Gateway boundary: `.claude/rules/gateway-boundary.md`
- Event interfaces: `.claude/rules/event-interfaces.md`
- Event conventions: `.claude/rules/event-conventions.md`
- gRPC errors: `.claude/rules/grpc-errors.md`

<!-- adr-capture: sha256=f1ac3c9f8dd6e49a; ts=2026-05-19T15:16:23Z; adrs=holomush-da2q,holomush-o46k,holomush-lp65 -->

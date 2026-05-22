# History Scope Privacy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `dev-flow:subagent-driven-development` (recommended) or `dev-flow:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the connect-time history leak (`holomush-iwzt`) where new guests and reconnecting characters see prior in-character events from a location.

**Architecture:** Two-tier privacy enforcement. **Tier 1**: per-session `LocationArrivedAt` on `SessionInfo`, populated at session-create / reattach / character-move, used by `QueryStreamHistory` for temporal floor and by Subscribe's `DeliverByStartTimePolicy` as a performance hint. **Tier 2**: per-subject filter-at-delivery in the Subscribe broadcaster as the load-bearing privacy gate. Location streams get a hardcoded current-location-only gate; ABAC remains for staff override only.

**Tech Stack:** Go, PostgreSQL (sqlx + migrations), NATS JetStream (immutable consumer config, query-then-copy pattern), custom ABAC engine (Cedar-inspired DSL), Ginkgo/Gomega integration tests.

**Spec:** `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md`
**Bead:** `holomush-iwzt` (design); ADRs `wxty`, `rc8b`, `kmac`, `ghpx`.

---

## File structure

### New files

| Path | Purpose |
|---|---|
| `internal/store/migrations/000037_add_session_history_floor_columns.up.sql` | Add `location_arrived_at` + `guest_character_created_at` columns to `sessions` table |
| `internal/store/migrations/000037_add_session_history_floor_columns.down.sql` | Drop the two columns |
| `internal/grpc/scope_floor.go` | `streamScopeFloor`, `isLocationStream`, `extractLocationID` helpers shared by `QueryStreamHistory` and Subscribe |
| `internal/grpc/scope_floor_test.go` | Unit tests for the helpers (table-driven per stream type) |
| `internal/world/movement_hook.go` | `MovementHook` interface (defined in world, implemented by core-server) |
| `test/integration/privacy/privacy_test.go` | Ginkgo integration suite — all `TestPrivacy_*` scenarios from spec §8 |
| `internal/eventbus/subscriber_consumer_config.go` | NATS-source-of-truth consumer-config builder (extracted helper for testability) |
| `internal/eventbus/subscriber_consumer_config_test.go` | Unit tests for I-PRIV-8 (4 cases: fresh / reattach-existing / SetFilters-existing / lookup-error) |

### Modified files

| Path | Change |
|---|---|
| `internal/session/session.go` | `Info` struct gains `LocationArrivedAt time.Time` + `GuestCharacterCreatedAt time.Time`; `Store` interface gains `UpdateLocationOnMove` |
| `internal/session/memstore.go` | In-memory implementation of `UpdateLocationOnMove` |
| `internal/store/session_store.go` | Postgres implementation of `UpdateLocationOnMove`; `Set`/`scan*` paths include the new columns |
| `internal/grpc/auth_handlers.go` | Populate `LocationArrivedAt` on fresh `SelectCharacter` (~line 332-352); on `SelectCharacter` reattach (~line 295-310) |
| `internal/grpc/server.go` | Populate `LocationArrivedAt` on `Subscribe.ReattachCAS` (~line 768-777); per-subject filter-at-delivery in the dispatch loop |
| `internal/grpc/query_stream_history.go` | Restructure Step 5 (location hard-gate + staff override); Step 6 floor application |
| `internal/world/service.go` | `MoveCharacter` invokes `MovementHook` after `characterRepo.UpdateLocation`, before event emit |
| `internal/eventbus/subscriber.go` | `OpenSession` + `SetFilters` use the new NATS-source-of-truth consumer-config builder |
| `internal/auth/guest_service.go` | Capture `world.Character.CreatedAt` into `SessionInfo.GuestCharacterCreatedAt` at guest session creation |
| `internal/access/policy/seed.go` | Seed `staff_read_unrestricted_history` policy |
| `internal/plugin/manifest.go` | `history_scope:` field with closed enum; reject plugin-owned namespaces without declaration |
| `internal/plugin/manifest_test.go` | I-PRIV-7 manifest validation tests |
| `test/meta/...` | Meta-test for I-PRIV-1..6, 8 (I-PRIV-7 vacuously satisfied until plugin adoption) |

---

## Phase 1 — Schema + lifecycle (no enforcement yet)

### Task 1: Migration adding session history-floor columns

**Files:**

- Create: `internal/store/migrations/000037_add_session_history_floor_columns.up.sql`
- Create: `internal/store/migrations/000037_add_session_history_floor_columns.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS location_arrived_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS guest_character_created_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch';
```

- [ ] **Step 2: Write the down migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE sessions
    DROP COLUMN IF EXISTS location_arrived_at,
    DROP COLUMN IF EXISTS guest_character_created_at;
```

- [ ] **Step 3: Apply migration to dev DB and verify**

```bash
task migrate:up
psql $DATABASE_URL -c "\d sessions" | grep -E "(location_arrived_at|guest_character_created_at)"
```

Expected: both columns present, `timestamp with time zone, not null`.

- [ ] **Step 4: Verify migration is idempotent (up→down→up cycle)**

```bash
task migrate:down -- 1 && task migrate:up
```

Expected: no errors.

- [ ] **Step 5: Commit**

Commit message: `feat(session): migration 037 — add history-floor columns to sessions`. Per `references/vcs-preamble.md`.

---

### Task 2: SessionInfo fields + store implementations

**Files:**

- Modify: `internal/session/session.go` — add fields to `Info` struct (~line 154-158); add `UpdateLocationOnMove` to `Store` interface (~line 227+)
- Modify: `internal/session/memstore.go` — implement `UpdateLocationOnMove`
- Modify: `internal/store/session_store.go` — implement `UpdateLocationOnMove`; update `Set` and `scan*` helpers
- Test: `internal/session/memstore_test.go`; `internal/store/session_store_test.go`

- [ ] **Step 1: Write the failing memstore test**

Append to `internal/session/memstore_test.go`:

```go
func TestMemStoreUpdateLocationOnMove(t *testing.T) {
    store := NewMemStore()
    ctx := context.Background()
    charID := NewULID()
    origLoc := NewULID()
    newLoc := NewULID()
    origArrival := time.Now().UTC().Add(-time.Hour)
    newArrival := time.Now().UTC()

    info := &Info{
        ID: NewULID().String(), CharacterID: charID,
        LocationID: origLoc, LocationArrivedAt: origArrival,
        Status: StatusActive, CreatedAt: origArrival, UpdatedAt: origArrival,
    }
    require.NoError(t, store.Set(ctx, info.ID, info))

    require.NoError(t, store.UpdateLocationOnMove(ctx, charID, newLoc, newArrival))

    got, err := store.Get(ctx, info.ID)
    require.NoError(t, err)
    assert.Equal(t, newLoc, got.LocationID)
    assert.True(t, got.LocationArrivedAt.Equal(newArrival))
}
```

- [ ] **Step 2: Run test to verify it fails**

`task test -- -run TestMemStoreUpdateLocationOnMove ./internal/session/`
Expected: FAIL — method not defined.

- [ ] **Step 3: Add fields to SessionInfo**

In `internal/session/session.go`, in the `Info` struct (locate `LocationID ulid.ULID` ~line 147):

```go
LocationID              ulid.ULID
LocationArrivedAt       time.Time // §holomush-iwzt §4.1 — per-session attach floor
GuestCharacterCreatedAt time.Time // §holomush-iwzt §4.3 — guest identity overlay floor (zero for non-guest)
IsGuest                 bool
```

- [ ] **Step 4: Add two methods to Store interface**

In `internal/session/session.go` at the `Store` interface (~line 227, after `UpdateGridPresent` / before `ListActiveByLocation`):

```go
// UpdateLocationOnMove atomically updates LocationID and LocationArrivedAt
// for all Active/Idle sessions belonging to characterID. Sessions in other
// statuses (Detached, Expired) are NOT touched — they reattach with their
// own LocationArrivedAt update path via BumpLocationArrivedAt. Per
// holomush-iwzt §5 row 5.
UpdateLocationOnMove(ctx context.Context, characterID ulid.ULID, newLocationID ulid.ULID, arrivedAt time.Time) error

// BumpLocationArrivedAt updates LocationArrivedAt for a single session by ID,
// regardless of status. Used by reattach paths (SelectCharacter-reattach,
// Subscribe.ReattachCAS) where the session may still be in StatusDetached
// when called, OR where transactional ordering between UpdateStatus(active)
// and the floor bump matters. Per holomush-iwzt §5 rows 2, 3 + plan-review
// round 1 finding B6.
BumpLocationArrivedAt(ctx context.Context, sessionID string, arrivedAt time.Time) error
```

- [ ] **Step 5: Implement both methods in memstore**

Append to `internal/session/memstore.go`:

```go
func (m *MemStore) UpdateLocationOnMove(ctx context.Context, characterID ulid.ULID, newLocationID ulid.ULID, arrivedAt time.Time) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    for _, info := range m.sessions {
        if info.CharacterID != characterID {
            continue
        }
        if info.Status != StatusActive && info.Status != StatusIdle {
            continue
        }
        info.LocationID = newLocationID
        info.LocationArrivedAt = arrivedAt
        info.UpdatedAt = arrivedAt
    }
    return nil
}

func (m *MemStore) BumpLocationArrivedAt(ctx context.Context, sessionID string, arrivedAt time.Time) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    info, ok := m.sessions[sessionID]
    if !ok {
        return oops.Code("SESSION_NOT_FOUND").With("session_id", sessionID).Errorf("session not found")
    }
    info.LocationArrivedAt = arrivedAt
    info.UpdatedAt = arrivedAt
    return nil
}
```

- [ ] **Step 6: Implement both methods in postgres session_store**

In `internal/store/session_store.go`, add:

```go
func (s *SessionStore) UpdateLocationOnMove(ctx context.Context, characterID ulid.ULID, newLocationID ulid.ULID, arrivedAt time.Time) error {
    query := `UPDATE sessions
              SET location_id = $1, location_arrived_at = $2, updated_at = $2
              WHERE character_id = $3 AND status IN ('active', 'idle')`
    _, err := s.db.ExecContext(ctx, query, newLocationID, arrivedAt, characterID)
    if err != nil {
        return oops.With("operation", "update_location_on_move").
            With("character_id", characterID.String()).
            Wrap(err)
    }
    return nil
}

func (s *SessionStore) BumpLocationArrivedAt(ctx context.Context, sessionID string, arrivedAt time.Time) error {
    query := `UPDATE sessions
              SET location_arrived_at = $1, updated_at = $1
              WHERE id = $2`
    res, err := s.db.ExecContext(ctx, query, arrivedAt, sessionID)
    if err != nil {
        return oops.With("operation", "bump_location_arrived_at").
            With("session_id", sessionID).Wrap(err)
    }
    n, _ := res.RowsAffected()
    if n == 0 {
        return oops.Code("SESSION_NOT_FOUND").With("session_id", sessionID).Errorf("session not found")
    }
    return nil
}
```

Also: in `Set` and the row-scan helpers (lines around 40-50 and 121-180), add `location_arrived_at` and `guest_character_created_at` to the column lists and scan/value bindings.

- [ ] **Step 7: Run all session tests to verify pass**

`task test -- ./internal/session/ ./internal/store/`
Expected: all PASS.

- [ ] **Step 8: Commit**

Commit message: `feat(session): add LocationArrivedAt + GuestCharacterCreatedAt to SessionInfo + UpdateLocationOnMove`.

---

### Task 3: Populate LocationArrivedAt on all session-create / reattach paths

**Files:**

- Modify: `internal/grpc/auth_handlers.go:295-352` (SelectCharacter — both fresh and reattach)
- Modify: `internal/grpc/server.go:768-777` (Subscribe.ReattachCAS)
- Test: `internal/grpc/auth_handlers_test.go`; `internal/grpc/server_test.go`

- [ ] **Step 1: Write failing test for fresh SelectCharacter populates LocationArrivedAt**

Append to `internal/grpc/auth_handlers_test.go`:

```go
func TestSelectCharacter_FreshSession_SetsLocationArrivedAt(t *testing.T) {
    server, ctx := newTestCoreServer(t)
    playerSession := seedPlayerWithCharacter(t, server, "Alyssa", someLocationID)

    before := time.Now()
    resp, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
        PlayerSessionToken: playerSession.Token,
        CharacterName:      "Alyssa",
    })
    require.NoError(t, err)

    info, _ := server.sessionStore.Get(ctx, resp.SessionId)
    assert.True(t, !info.LocationArrivedAt.Before(before), "LocationArrivedAt should be at-or-after request time")
}
```

- [ ] **Step 2: Run test to verify it fails**

`task test -- -run TestSelectCharacter_FreshSession_SetsLocationArrivedAt ./internal/grpc/`
Expected: FAIL — `LocationArrivedAt` is zero.

- [ ] **Step 3: Update fresh SelectCharacter to populate field**

In `internal/grpc/auth_handlers.go` around line 331-343 (where `sessionInfo := &session.Info{...}` is constructed), add:

```go
sessionInfo := &session.Info{
    // ... existing fields ...
    LocationID:              locationID,
    LocationArrivedAt:       now,   // NEW
    Status:                  session.StatusActive,
    // ... existing fields ...
    CreatedAt:               now,
    UpdatedAt:               now,
}
```

- [ ] **Step 4: Write failing test for SelectCharacter reattach also resets LocationArrivedAt**

```go
func TestSelectCharacter_Reattach_ResetsLocationArrivedAt(t *testing.T) {
    server, ctx := newTestCoreServer(t)
    playerSession := seedPlayerWithCharacter(t, server, "Bertram", someLocationID)
    resp1, _ := server.SelectCharacter(ctx, ... /* fresh */)
    detachSession(t, server, resp1.SessionId)
    time.Sleep(10 * time.Millisecond) // ensure measurable gap

    before := time.Now()
    resp2, err := server.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
        PlayerSessionToken: playerSession.Token, CharacterName: "Bertram",
    })
    require.NoError(t, err)
    require.True(t, resp2.Reattached)

    info, _ := server.sessionStore.Get(ctx, resp2.SessionId)
    assert.True(t, !info.LocationArrivedAt.Before(before), "reattach must advance LocationArrivedAt — I-PRIV-3")
}
```

- [ ] **Step 5: Run test to verify it fails**

Expected: FAIL — reattach branch doesn't touch `LocationArrivedAt`.

- [ ] **Step 6: Add LocationArrivedAt update to SelectCharacter reattach branch**

In `internal/grpc/auth_handlers.go` around line 297-303 (existing reattach branch). Use `BumpLocationArrivedAt` (single-session, status-agnostic) rather than `UpdateLocationOnMove` to avoid the SQL `WHERE status IN ('active', 'idle')` filter race — `UpdateStatus(active)` must commit before any character-status-filtered UPDATE would see the row as Active:

```go
now := time.Now()
if updateErr := s.sessionStore.UpdateStatus(ctx, existingSession.ID,
    session.StatusActive, nil, nil); updateErr != nil { ... }
// NEW: bump LocationArrivedAt by ID (no status filter) to enforce I-PRIV-3
// rule-3 reset semantics. Independent of UpdateStatus commit ordering.
if loErr := s.sessionStore.BumpLocationArrivedAt(ctx, existingSession.ID, now); loErr != nil {
    slog.WarnContext(ctx, "failed to reset LocationArrivedAt on reattach", "error", loErr)
}
existingSession.Status = session.StatusActive
existingSession.LocationArrivedAt = now
existingSession.UpdatedAt = now
```

- [ ] **Step 7: Write failing test for Subscribe.ReattachCAS reset**

In `internal/grpc/server_test.go`:

```go
func TestSubscribeReattachCAS_AdvancesLocationArrivedAt(t *testing.T) {
    server, ctx := newTestCoreServer(t)
    sessionID := seedDetachedSession(t, server, "Cyrus", someLocationID, /*detachedSinceOffset*/ -time.Hour)

    before := time.Now()
    stream := openSubscribe(t, server, sessionID)
    defer stream.CloseSend()

    info, _ := server.sessionStore.Get(ctx, sessionID)
    assert.True(t, !info.LocationArrivedAt.Before(before), "ReattachCAS must advance LocationArrivedAt — I-PRIV-3")
}
```

- [ ] **Step 8: Run test to verify it fails**

Expected: FAIL — `ReattachCAS` doesn't update `LocationArrivedAt`.

- [ ] **Step 9: Add LocationArrivedAt update to Subscribe.ReattachCAS path**

In `internal/grpc/server.go` around line 768-777 (after `sessionStore.ReattachCAS` succeeds). Use `BumpLocationArrivedAt` for the same reason as Step 6 — the CAS sets status to Active but our floor bump should not depend on its commit ordering:

```go
won, casErr := s.sessionStore.ReattachCAS(ctx, sessionID)
if casErr != nil { ... }
if !won {
    return nil, status.Errorf(codes.FailedPrecondition,
        "session reattach CAS lost — another handler won the race")
}
// NEW: bump LocationArrivedAt by ID to enforce I-PRIV-3 reset semantics.
now := time.Now()
if loErr := s.sessionStore.BumpLocationArrivedAt(ctx, sessionID, now); loErr != nil {
    slog.WarnContext(ctx, "failed to reset LocationArrivedAt on ReattachCAS", "error", loErr)
}
```

- [ ] **Step 10: Run all three tests + lint**

`task test -- ./internal/grpc/ && task lint`
Expected: all PASS, lint clean.

- [ ] **Step 11: Commit**

Commit message: `feat(grpc): set LocationArrivedAt on fresh+reattach session paths (iwzt rule-3)`.

---

### Task 4: MovementHook interface + character-move session sync

**Files:**

- Create: `internal/world/movement_hook.go`
- Modify: `internal/world/service.go:759-790` (MoveCharacter)
- Modify: `cmd/holomush/` or wherever core-server wires deps — implement `MovementHook`
- Test: `internal/world/service_test.go`

- [ ] **Step 1: Define MovementHook interface**

Create `internal/world/movement_hook.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
    "context"
    "time"

    "github.com/oklog/ulid/v2"
)

// MovementHook is invoked by Service.MoveCharacter after the character row
// is updated but before the move event is emitted. Implementations propagate
// the new location to dependent stores (e.g., session.Store.UpdateLocationOnMove)
// so consumers reading those stores observe the new location atomically with
// the move event.
//
// Per holomush-iwzt §5.1 / ADR holomush-kmac: this preserves the
// gateway/world layering invariant by letting core-server (which holds both
// world.Service and session.Store deps) implement the hook without world.Service
// gaining a session.Store dependency.
//
// Returning an error from the hook fails the move — caller MUST handle.
type MovementHook interface {
    OnCharacterMoved(ctx context.Context, characterID ulid.ULID, newLocationID ulid.ULID, arrivedAt time.Time) error
}

// NoopMovementHook is the default when no hook is wired (e.g., test contexts).
// Returns nil unconditionally.
type NoopMovementHook struct{}

func (NoopMovementHook) OnCharacterMoved(_ context.Context, _ ulid.ULID, _ ulid.ULID, _ time.Time) error {
    return nil
}
```

- [ ] **Step 2: Add hook field to Service + setter; locate constructor**

First locate the Service constructor pattern: `rg -nP "func New(Service|ServiceWithConfig|World)\b" internal/world/service.go`. Existing pattern is likely `NewService(...)` returning `*Service`. Read those ~10 lines to identify the field-init style (struct literal vs. options struct).

In `internal/world/service.go` near the struct definition:

```go
type Service struct {
    // ... existing fields ...
    movementHook MovementHook
}
```

Update the constructor to default-initialize:

```go
func NewService(/* existing params */) *Service {
    return &Service{
        // ... existing fields ...
        movementHook: NoopMovementHook{},
    }
}
```

Add the setter for explicit installation by core-server at startup:

```go
// SetMovementHook installs a MovementHook that fires after each successful
// character move. Default is NoopMovementHook. Call before MoveCharacter
// invocations begin (typically at server startup).
func (s *Service) SetMovementHook(h MovementHook) {
    if h == nil {
        s.movementHook = NoopMovementHook{}
        return
    }
    s.movementHook = h
}
```

Because the default is non-nil (`NoopMovementHook{}`), the call site in Step 3 can omit the `if s.movementHook != nil` nil-check — the field is always callable. Use that simpler form below.

- [ ] **Step 3: Wire MovementHook into MoveCharacter**

In `internal/world/service.go:759-790`, between `characterRepo.UpdateLocation` (~line 789) and the `MovePayload` build (~line 793):

```go
if err := s.characterRepo.UpdateLocation(ctx, characterID, &toLocationID); err != nil {
    return oops.Code("CHARACTER_MOVE_FAILED").Wrapf(err, "update character %s location", characterID)
}

// Fire MovementHook BEFORE event emit so any consumer observing the move
// event sees the synchronized state. Per holomush-iwzt §5.1 / ADR kmac.
// Constructor default-initializes movementHook to NoopMovementHook{}, so
// no nil-check is required here.
//
// Failure mode (known): if the hook returns an error, characterRepo
// has ALREADY committed the new location (line 789 above). The session
// store will lag the character row; no move event is emitted. The
// caller surfaces CHARACTER_MOVE_FAILED with phase="movement_hook"
// for operator diagnostics. This window is acceptable for v1 — the
// existing pre-hook state already had `sessions.location_id` lagging
// `characters.location_id` permanently (the bug being fixed). Filed as
// follow-up: wrap UpdateLocation + hook + event-emit in a single DB
// transaction. Tracked as a sub-bullet under Step 7 audit.
arrivedAt := time.Now().UTC()
if hookErr := s.movementHook.OnCharacterMoved(ctx, characterID, toLocationID, arrivedAt); hookErr != nil {
    return oops.Code("CHARACTER_MOVE_FAILED").
        With("character_id", characterID.String()).
        With("phase", "movement_hook").
        Wrap(hookErr)
}

// Build move payload (existing code follows)...
```

- [ ] **Step 4: Implement MovementHook in core-server**

Locate where `world.Service` is constructed (likely `cmd/holomush/` or `internal/grpc/`). Add an adapter:

```go
type sessionStoreMovementHook struct {
    sessions session.Store
}

func (h *sessionStoreMovementHook) OnCharacterMoved(ctx context.Context, characterID ulid.ULID, newLocationID ulid.ULID, arrivedAt time.Time) error {
    return h.sessions.UpdateLocationOnMove(ctx, characterID, newLocationID, arrivedAt)
}
```

Then at server startup, after constructing `worldService` and `sessionStore`:

```go
worldService.SetMovementHook(&sessionStoreMovementHook{sessions: sessionStore})
```

- [ ] **Step 5: Write integration test for MoveCharacter syncs session**

Append to `internal/world/service_test.go` (or a new test file if appropriate):

```go
func TestMoveCharacter_FiresMovementHook(t *testing.T) {
    var captured struct {
        charID, locID ulid.ULID
        ts            time.Time
    }
    var hookFired bool
    hook := movementHookFn(func(_ context.Context, charID, locID ulid.ULID, ts time.Time) error {
        hookFired = true
        captured.charID, captured.locID, captured.ts = charID, locID, ts
        return nil
    })
    svc := newTestServiceWithHook(t, hook)
    charID, fromLoc, toLoc := seedCharacterAndTwoLocations(t, svc)

    require.NoError(t, svc.MoveCharacter(ctx, adminSubject, charID, toLoc))

    require.True(t, hookFired)
    assert.Equal(t, charID, captured.charID)
    assert.Equal(t, toLoc, captured.locID)
    assert.WithinDuration(t, time.Now(), captured.ts, 5*time.Second)
}

type movementHookFn func(ctx context.Context, charID, locID ulid.ULID, ts time.Time) error

func (f movementHookFn) OnCharacterMoved(ctx context.Context, charID, locID ulid.ULID, ts time.Time) error {
    return f(ctx, charID, locID, ts)
}
```

- [ ] **Step 6: Run test + lint**

`task test -- ./internal/world/ ./internal/grpc/ ./internal/session/ && task lint`

- [ ] **Step 7: Audit existing consumers of session.LocationID**

Run `rg -nP "\.LocationID" --type go internal/ | grep -v _test.go | grep -v session.LocationID` and review the call sites. For each, confirm it's safe with the previously-stale value now becoming canonical. Document any concerning consumer in a `bd note holomush-iwzt "consumer audit: <path:line> — <verdict>"`. If any consumer requires special handling, file a follow-up bead.

- [ ] **Step 8: Commit**

Commit message: `feat(world): MovementHook interface + sessions/characters location-sync (iwzt kmac)`.

---

### Task 5: GuestCharacterCreatedAt population at guest session creation

**Files:**

- Modify: `internal/auth/guest_service.go:109-200` (CreateGuest)
- Modify: `internal/grpc/auth_handlers.go` (wherever guest sessions are minted from GuestService output)
- Test: `internal/auth/guest_service_test.go`

- [ ] **Step 1: Trace the guest-session creation path**

Run `rg -nP "guestService|CreateGuest|GuestResult" --type go internal/grpc/` to find where `GuestService.CreateGuest`'s `*GuestResult` is consumed and turned into a `SessionInfo`. Likely in `internal/grpc/auth_handlers.go`. Confirm whether the session is built in `auth/guest_service.go` or in the calling handler.

- [ ] **Step 2: Write failing test asserting guest session has GuestCharacterCreatedAt set**

```go
func TestGuestSessionCarriesCharacterCreatedAt(t *testing.T) {
    server, ctx := newTestCoreServer(t)
    resp, err := server.CreateGuestSession(ctx, &corev1.CreateGuestSessionRequest{})
    require.NoError(t, err)
    info, _ := server.sessionStore.Get(ctx, resp.SessionId)
    require.True(t, info.IsGuest)
    assert.False(t, info.GuestCharacterCreatedAt.IsZero(),
        "guest session must capture character.CreatedAt for I-PRIV-2 floor")
}
```

- [ ] **Step 3: Run test to verify it fails**

Expected: FAIL — `GuestCharacterCreatedAt` is zero.

- [ ] **Step 4: Populate GuestCharacterCreatedAt at the session-build site**

Whichever site constructs the guest `SessionInfo`, add (sourced from `world.Character.CreatedAt` on the just-created character):

```go
sessionInfo := &session.Info{
    // ... existing fields ...
    IsGuest:                 true,
    GuestCharacterCreatedAt: char.CreatedAt,  // NEW
    // ... existing fields ...
}
```

- [ ] **Step 5: Run test to verify it passes**

`task test -- -run TestGuestSessionCarriesCharacterCreatedAt ./internal/grpc/`

- [ ] **Step 6: Commit**

Commit message: `feat(auth): capture guest character.CreatedAt into SessionInfo (iwzt I-PRIV-2)`.

---

## Phase 2 — Location-stream hard-gate + floor

### Task 5.5: Build the privacy-test harness in `internal/testsupport/privacytest/`

**Files:**

- Create: `internal/testsupport/privacytest/privacy_harness.go`
- Create: `internal/testsupport/privacytest/privacy_session.go`
- Create: `internal/testsupport/privacytest/matchers.go`

The integration tests in Tasks 9-22 reference helpers like `ts.ConnectGuest`, `sess.MoveTo`, `sess.DetachTransport`, `MatchOopsCode`. None exist today — the only file in `internal/testsupport/privacytest/` is the admin-socket/rekey-scoped `server.go`. This task builds the missing harness as a self-contained dependency of all later integration tests.

- [ ] **Step 1: Survey existing patterns**

Read `test/integration/plugin/binary_plugin_test.go` and `test/integration/auth/core_client_shim_test.go` to see how integration tests currently bootstrap a server + connect ConnectRPC clients. The harness wraps these patterns.

- [ ] **Step 2: Define the Server type with bootstrap**

`internal/testsupport/privacytest/privacy_harness.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package privacytest

import (
    "context"
    "testing"
    "time"

    "github.com/oklog/ulid/v2"
    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// Server is the privacy-test harness wrapping a real holomush stack
// (Postgres + NATS JetStream + core-server + gateway) for integration
// testing of holomush-iwzt history-scope invariants.
type Server struct {
    t              *testing.T
    coreClient     corev1.CoreServiceClient
    sessionStore   sessionStoreAccessor  // exposes UpdateLocationOnMove for test-driven floor manipulation
    // ... bus, db connections, cleanup funcs
}

// Start bootstraps a full stack and returns a Server. Caller MUST defer Stop.
func Start(t *testing.T) *Server { /* spin up testcontainers, run migrations, start core+gateway */ return nil }

// Stop tears down the stack.
func (s *Server) Stop() { /* ... */ }

// NewLocation creates a fresh location in the world. Returns its ULID.
func (s *Server) NewLocation(ctx context.Context) ulid.ULID { /* ... */ return ulid.ULID{} }

// NewSceneWithoutMember creates a scene with no members. Returns its ULID.
func (s *Server) NewSceneWithoutMember(ctx context.Context) ulid.ULID { /* ... */ return ulid.ULID{} }
```

- [ ] **Step 3: Define the Session type**

`internal/testsupport/privacytest/privacy_session.go`:

```go
//go:build integration

package privacytest

import (
    "context"
    "time"

    "github.com/oklog/ulid/v2"
    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// Session wraps an authenticated or guest game session for testing.
type Session struct {
    server            *Server
    SessionID         string
    CharacterID       ulid.ULID
    CharacterName     string
    LocationID        ulid.ULID
    OriginalLocationID ulid.ULID    // set at connect; not mutated by MoveTo
    LocationArrivedAt time.Time
    SessionCreatedAt  time.Time
    Client            corev1.CoreServiceClient
    subscribeCancel   context.CancelFunc
}

// Server-side connect helpers.
func (s *Server) ConnectGuest(ctx context.Context) *Session                          { /* ... */ return nil }
func (s *Server) ConnectAuthed(ctx context.Context, charName string) *Session        { /* ... */ return nil }
func (s *Server) ConnectAuthedWithRoles(ctx context.Context, charName string, roles []string) *Session { /* ... */ return nil }
func (s *Server) AuthedPlayer(ctx context.Context, charName string) *AuthedPlayer    { /* ... */ return nil }

// AuthedPlayer represents a player with potentially multiple sessions for the
// same character. Used for multi-session continuity tests.
type AuthedPlayer struct {
    PlayerID    ulid.ULID
    CharacterID ulid.ULID
    LocationID  ulid.ULID
    server      *Server
}

func (p *AuthedPlayer) OpenWebSession(ctx context.Context) *Session    { /* ... */ return nil }
func (p *AuthedPlayer) OpenTelnetSession(ctx context.Context) *Session { /* ... */ return nil }

// Session command helpers.
func (s *Session) SendCommand(ctx context.Context, cmd string) error                                       { /* ... */ return nil }
func (s *Session) WaitForEvent(ctx context.Context, eventType string) *corev1.GameEvent                    { /* ... */ return nil }
func (s *Session) DrainEvents(ctx context.Context, timeout time.Duration) []*corev1.GameEvent              { /* ... */ return nil }
func (s *Session) Logout(ctx context.Context)                                                              { /* ... */ }
func (s *Session) MoveTo(ctx context.Context, newLocationID ulid.ULID)                                     { /* ... */ }
func (s *Session) DetachTransport(ctx context.Context)                                                     { /* ... */ }
func (s *Session) ReattachTransport(ctx context.Context)                                                   { /* ... */ }
func (s *Session) CreateScene(ctx context.Context) ulid.ULID                                               { /* ... */ return ulid.ULID{} }
func (s *Session) JoinScene(ctx context.Context, sceneID ulid.ULID)                                        { /* ... */ }
func (s *Session) QueryStreamHistory(ctx context.Context, stream string) []*corev1.GameEvent               { /* ... */ return nil }

// Test-driven session-store manipulation.
func (s *Server) ExpireSession(ctx context.Context, sessionID string) { /* directly mark the session row expired */ }
```

- [ ] **Step 4: Define the MatchOopsCode Gomega matcher**

`internal/testsupport/privacytest/matchers.go`:

```go
//go:build integration

package privacytest

import (
    "fmt"

    "github.com/onsi/gomega/types"
    "github.com/samber/oops"
)

// MatchOopsCode returns a Gomega matcher that succeeds when the actual error
// is an oops error whose Code() matches expected. Use:
//   Expect(err).To(MatchOopsCode("STREAM_ACCESS_DENIED"))
func MatchOopsCode(expected string) types.GomegaMatcher {
    return &oopsCodeMatcher{expected: expected}
}

type oopsCodeMatcher struct {
    expected string
    gotCode  string
}

func (m *oopsCodeMatcher) Match(actual interface{}) (bool, error) {
    err, ok := actual.(error)
    if !ok || err == nil {
        return false, fmt.Errorf("MatchOopsCode expects an error, got %T", actual)
    }
    oopsErr, ok := oops.AsOops(err)
    if !ok {
        return false, nil
    }
    m.gotCode = oopsErr.Code()
    return m.gotCode == m.expected, nil
}

func (m *oopsCodeMatcher) FailureMessage(actual interface{}) string {
    return fmt.Sprintf("expected oops code %q, got %q (full err: %v)", m.expected, m.gotCode, actual)
}

func (m *oopsCodeMatcher) NegatedFailureMessage(actual interface{}) string {
    return fmt.Sprintf("expected oops code NOT to be %q, but it was", m.expected)
}
```

- [ ] **Step 5: Implement the Start bootstrap**

Use testcontainers (per existing integration tests) to spin up Postgres + NATS. Run all migrations via the same path `task migrate:up` uses. Start a core-server bound to those backends. Construct gRPC clients via in-process pipe (per `test/integration/auth/core_client_shim_test.go` pattern). Wire down to teardown all of it in `Stop()`.

Reference the existing `internal/testsupport/privacytest/server.go` for the testcontainers + bus patterns.

- [ ] **Step 6: Implement Session helpers minimally**

For each helper, implement against the real `CoreServiceClient`:

- `SendCommand` → `client.SendCommand(...)`
- `MoveTo` → invoke `MoveCharacter` via the client (find the appropriate RPC; may need `WorldService` client)
- `DetachTransport` → close the Subscribe stream and clear `subscribeCancel`
- `ReattachTransport` → open a new Subscribe stream (per I-PRIV-3 Round 3 amendment, no `LocationArrivedAt`-derived floor advances on reattach; durable consumer's `OptStartTime` is immutable; filter-at-delivery enforces the original floor)
- `QueryStreamHistory` → `client.QueryStreamHistory(...)` returns events
- `Logout` → `client.Logout(...)` or equivalent
- `JoinScene` → `client.JoinScene(...)` or `FocusCoordinator.JoinScene(...)`
- `WaitForEvent` → `select`-loop with timeout reading from a per-session events channel populated by the Subscribe goroutine

- [ ] **Step 7: Verify harness compiles**

```bash
task test -- -tags=integration -run=^$ ./internal/testsupport/privacytest/...
```

Expected: build succeeds, no tests run (the `-run=^$` matches nothing).

- [ ] **Step 8: Write a smoke test exercising the harness**

`internal/testsupport/privacytest/privacy_harness_smoke_test.go`:

```go
//go:build integration

package privacytest_test

import (
    "context"
    "testing"

    . "github.com/onsi/gomega"
    "github.com/holomush/holomush/internal/testsupport/privacytest"
)

func TestPrivacyHarnessSmoke(t *testing.T) {
    g := NewGomegaWithT(t)
    ts := privacytest.Start(t)
    defer ts.Stop()

    ctx := context.Background()
    guest := ts.ConnectGuest(ctx)
    g.Expect(guest.SessionID).NotTo(BeEmpty())
    g.Expect(guest.CharacterName).NotTo(BeEmpty())
    g.Expect(guest.LocationID.IsZero()).To(BeFalse())

    g.Expect(guest.SendCommand(ctx, "say hello-from-smoke-test")).To(Succeed())
    ev := guest.WaitForEvent(ctx, "core-communication:say")
    g.Expect(ev).NotTo(BeNil())

    guest.Logout(ctx)
}
```

- [ ] **Step 9: Run smoke test**

```bash
task test:int -- -run TestPrivacyHarnessSmoke ./internal/testsupport/privacytest/
```

Expected: PASS.

- [ ] **Step 10: Commit**

Commit message: `feat(testsupport): privacy-test harness (privacytest.Server + Session + matchers)`.

---

### Task 6: `streamScopeFloor` + `isLocationStream` helpers

**Files:**

- Create: `internal/grpc/scope_floor.go`
- Create: `internal/grpc/scope_floor_test.go`

- [ ] **Step 1: Write failing unit tests (table-driven)**

`internal/grpc/scope_floor_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
    "testing"
    "time"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"

    "github.com/holomush/holomush/internal/session"
)

func TestStreamScopeFloor(t *testing.T) {
    locID := ulid.MustParse("01H000000000000000000000A1")
    charID := ulid.MustParse("01H000000000000000000000C1")
    sceneID := ulid.MustParse("01H000000000000000000000S1")
    arrival := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
    sceneJoin := time.Date(2026, 5, 17, 11, 0, 0, 0, time.UTC)
    guestCreated := time.Date(2026, 5, 17, 9, 30, 0, 0, time.UTC)

    cases := []struct {
        name   string
        info   *session.Info
        stream string
        want   time.Time
    }{
        {"location current — non-guest", &session.Info{CharacterID: charID, LocationID: locID, LocationArrivedAt: arrival},
            "location:" + locID.String(), arrival},
        {"location current — guest, arrival later than guest_created",
            &session.Info{CharacterID: charID, LocationID: locID, LocationArrivedAt: arrival, IsGuest: true, GuestCharacterCreatedAt: guestCreated},
            "location:" + locID.String(), arrival},
        {"location current — guest, guest_created later than arrival (shouldn't happen but MAX wins)",
            &session.Info{CharacterID: charID, LocationID: locID, LocationArrivedAt: time.Time{}, IsGuest: true, GuestCharacterCreatedAt: guestCreated},
            "location:" + locID.String(), guestCreated},
        {"scene member — non-guest, uses JoinedAt",
            &session.Info{CharacterID: charID, FocusMemberships: []session.FocusMembership{
                {Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: sceneJoin},
            }}, "scene:" + sceneID.String() + ":ic", sceneJoin},
        {"character own stream — no floor", &session.Info{CharacterID: charID}, "character:" + charID.String(), time.Time{}},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := streamScopeFloor(tc.info, tc.stream)
            assert.True(t, got.Equal(tc.want), "got %v want %v", got, tc.want)
        })
    }
}

func TestIsLocationStream(t *testing.T) {
    assert.True(t, isLocationStream("location:01H000000000000000000000A1"))
    assert.False(t, isLocationStream("scene:01H000000000000000000000S1:ic"))
    assert.False(t, isLocationStream("character:01H000000000000000000000C1"))
    assert.False(t, isLocationStream("global"))
}
```

- [ ] **Step 2: Run test — verify it fails**

`task test -- ./internal/grpc/ -run TestStreamScopeFloor`
Expected: FAIL — symbols don't exist.

- [ ] **Step 3: Implement the helpers**

Create `internal/grpc/scope_floor.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
    "strings"
    "time"

    "github.com/holomush/holomush/internal/session"
)

// streamScopeFloor returns the temporal floor for a session's view of the
// given stream. Per holomush-iwzt §6.1.
func streamScopeFloor(info *session.Info, stream string) time.Time {
    var base time.Time
    switch {
    case isLocationStream(stream):
        base = info.LocationArrivedAt
    case strings.HasPrefix(stream, "scene:"):
        sceneID, ok := extractSceneID(stream)
        if !ok {
            return time.Time{}
        }
        for _, m := range info.FocusMemberships {
            if m.Kind == session.FocusKindScene && m.TargetID.String() == sceneID {
                base = m.JoinedAt
                break
            }
        }
    case strings.HasPrefix(stream, "character:"):
        return time.Time{}
    default:
        return time.Time{}
    }
    if info.IsGuest && info.GuestCharacterCreatedAt.After(base) {
        return info.GuestCharacterCreatedAt
    }
    return base
}

// isLocationStream reports whether a stream subject is a grid location stream.
// Per holomush-iwzt §3.
func isLocationStream(stream string) bool {
    if !strings.HasPrefix(stream, "location:") {
        return false
    }
    rest := strings.TrimPrefix(stream, "location:")
    return rest != "" && !strings.Contains(rest, ":")
}

// extractLocationID returns the ULID portion of a location stream.
// Caller MUST check isLocationStream first; otherwise behavior is undefined.
func extractLocationID(stream string) string {
    return strings.TrimPrefix(stream, "location:")
}

// extractSceneID returns the scene ULID from a scene:<id>:ic or scene:<id>:ooc subject.
func extractSceneID(stream string) (string, bool) {
    rest := strings.TrimPrefix(stream, "scene:")
    parts := strings.SplitN(rest, ":", 2)
    if len(parts) != 2 {
        return "", false
    }
    return parts[0], true
}
```

- [ ] **Step 4: Run tests — verify pass**

`task test -- ./internal/grpc/ -run TestStreamScopeFloor && task test -- ./internal/grpc/ -run TestIsLocationStream`
Expected: PASS.

- [ ] **Step 5: Commit**

Commit message: `feat(grpc): scope_floor helpers (streamScopeFloor, isLocationStream)`.

---

### Task 7: QueryStreamHistory restructure — hard-gate + floor application

**Files:**

- Modify: `internal/grpc/query_stream_history.go:156-220`

- [ ] **Step 1: Write failing test for hard-gate denial when not in location**

Append to `internal/grpc/query_stream_history_test.go`:

```go
func TestQueryStreamHistory_LocationHardGate_DeniedWhenNotInLocation(t *testing.T) {
    server, ctx := newTestCoreServerWithBus(t)
    sess := seedActiveSessionInLocation(t, server, "Diana", locA)

    resp, err := server.QueryStreamHistory(ctx, &corev1.QueryStreamHistoryRequest{
        SessionId: sess.ID,
        Stream:    "location:" + locB.String(),  // different location
    })
    require.Nil(t, resp)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "STREAM_ACCESS_DENIED")
}
```

- [ ] **Step 2: Run test to verify it fails**

Expected: FAIL — current code calls ABAC which permits read by default seed policy.

- [ ] **Step 3: Add staffOverride stub**

In `internal/grpc/scope_floor.go` (or a sibling file), add a stub returning false:

```go
// staffOverride is a stub for Phase 5 (staff ABAC override). Phase 2 ships
// with this returning false unconditionally so the hard-gate is exercised.
// Phase 5 replaces with a real ABAC engine.Evaluate call against
// "read_unrestricted_history".
func staffOverride(_ context.Context, _ *session.Info, _ accessTypes.AccessPolicyEngine) bool {
    return false
}

// staffOverride takes the existing accessTypes.AccessPolicyEngine
// (`internal/access/policy/types/types.go:474`) rather than declaring a
// parallel local interface. The existing s.accessEngine field at
// `internal/grpc/server.go:163` is already typed accessTypes.AccessPolicyEngine,
// so the stub and real impl below both consume that type directly.
```

(Adjust imports/type names to match the actual `internal/access` types — verify via `rg "type AccessRequest" internal/access/policy/types/`.)

- [ ] **Step 4: Restructure QueryStreamHistory Step 5**

In `internal/grpc/query_stream_history.go` between lines 156 and 214, replace the public-stream ABAC branch:

```go
// Step 5: Authorization — three-way classifier.
if isPrivateStream(req.Stream) {
    // (existing I-17 membership gate — unchanged)
    if strings.HasPrefix(req.Stream, "scene:") {
        if _, keyErr := streamToFocusKey(req.Stream); keyErr != nil {
            return nil, keyErr
        }
    }
    if !sessionHasMembership(info, req.Stream) {
        slog.InfoContext(ctx, "stream access denied by I-17 membership gate",
            "session_id", req.SessionId, "denial_reason", "not_member",
            "character_id", info.CharacterID.String(), "stream", req.Stream)
        return nil, oops.Code("STREAM_ACCESS_DENIED").
            With("session_id", req.SessionId).With("stream", req.Stream).
            Errorf("not authorized to read stream")
    }
} else if isLocationStream(req.Stream) {
    // NEW: hard-gate for location streams.
    if !staffOverride(ctx, info, s.accessEngine) {
        if info.LocationID.String() != extractLocationID(req.Stream) {
            slog.InfoContext(ctx, "stream access denied by location hard-gate",
                "session_id", req.SessionId, "denial_reason", "wrong_location",
                "character_id", info.CharacterID.String(),
                "session_location", info.LocationID.String(),
                "requested_stream", req.Stream)
            return nil, oops.Code("STREAM_ACCESS_DENIED").
                With("session_id", req.SessionId).With("stream", req.Stream).
                Errorf("not authorized to read stream")
        }
    }
} else {
    // Other public streams (global, system) — existing ABAC path unchanged.
    if s.accessEngine == nil {
        return nil, oops.Code("STREAM_ACCESS_DENIED").With("stream", req.Stream).
            Errorf("access engine not configured")
    }
    accessReq, reqErr := accessTypes.NewAccessRequest(
        "character:"+info.CharacterID.String(),
        accessTypes.ActionRead, "stream:"+req.Stream, nil,
    )
    if reqErr != nil { return nil, oops.Code("INTERNAL").Wrap(reqErr) }
    decision, evalErr := s.accessEngine.Evaluate(ctx, accessReq)
    if evalErr != nil {
        return nil, oops.Code("INTERNAL").With("stream", req.Stream).Wrap(evalErr)
    }
    if !decision.IsAllowed() {
        slog.InfoContext(ctx, "stream access denied by ABAC",
            "session_id", req.SessionId, "denial_reason", "policy_denied",
            "character_id", info.CharacterID.String(),
            "stream", req.Stream, "policy_id", decision.PolicyID())
        return nil, oops.Code("STREAM_ACCESS_DENIED").
            With("session_id", req.SessionId).With("stream", req.Stream).
            Errorf("not authorized to read stream")
    }
}
```

- [ ] **Step 5: Apply scope floor in Step 6**

In `internal/grpc/query_stream_history.go` at the existing `// Step 6: Parse not_before.` (~line 216):

```go
// Step 6: Compute effective NotBefore = MAX(client-supplied, server-side scope floor).
var notBefore time.Time
if req.NotBeforeMs > 0 {
    notBefore = time.UnixMilli(req.NotBeforeMs).UTC()
}
scopeFloor := streamScopeFloor(info, req.Stream)
if scopeFloor.After(notBefore) {
    notBefore = scopeFloor
}
```

- [ ] **Step 6: Run hard-gate test — verify PASS now**

`task test -- ./internal/grpc/ -run TestQueryStreamHistory_LocationHardGate_DeniedWhenNotInLocation`

- [ ] **Step 7: Run all internal/grpc tests + lint**

`task test -- ./internal/grpc/ && task lint`
Expected: all PASS. Note: existing tests that queried location streams while not-present will now fail — update them to seed the session into the location they query, or to assert the new deny behavior.

- [ ] **Step 8: Commit**

Commit message: `feat(grpc): QueryStreamHistory location hard-gate + scope floor (iwzt Phase 2)`.

---

### Task 8: Integration test — guest sees no pre-arrival history

**Files:**

- Create: `test/integration/privacy/privacy_test.go`
- Create: `test/integration/privacy/suite_test.go` (Ginkgo bootstrap)

- [ ] **Step 1: Create Ginkgo suite bootstrap**

`test/integration/privacy/suite_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package privacy_test

import (
    "testing"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
)

func TestPrivacy(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Privacy Suite (holomush-iwzt)")
}
```

- [ ] **Step 2: Write the failing privacy test**

`test/integration/privacy/privacy_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package privacy_test

import (
    "context"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
    "github.com/holomush/holomush/internal/testsupport/privacytest"
)

var _ = Describe("I-PRIV-1: new guest sees no pre-arrival location history", func() {
    var ts *privacytest.Server
    BeforeEach(func() { ts = privacytest.Start(GinkgoT()) })
    AfterEach(func() { ts.Stop() })

    It("returns empty history for events emitted before the guest's session created_at", func() {
        ctx := context.Background()
        // Guest A connects, emits a pose, disconnects.
        guestA := ts.ConnectGuest(ctx)
        guestA.SendCommand(ctx, "pose hello")
        guestA.WaitForEvent(ctx, "core-communication:pose")
        guestA.Logout(ctx)

        // Brief gap so timestamps don't tie.
        time.Sleep(50 * time.Millisecond)

        // Guest B connects (fresh) into the same location.
        guestB := ts.ConnectGuest(ctx)
        Expect(guestB.LocationID).To(Equal(guestA.LocationID))

        resp, err := guestB.Client.QueryStreamHistory(ctx, &corev1.QueryStreamHistoryRequest{
            SessionId: guestB.SessionID,
            Stream:    "location:" + guestB.LocationID.String(),
        })
        Expect(err).NotTo(HaveOccurred())
        for _, ev := range resp.GetEvents() {
            Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", guestB.SessionCreatedAt),
                "event %q at %s leaked before guest B session created_at %s",
                ev.GetType(), ev.GetTimestamp().AsTime(), guestB.SessionCreatedAt)
        }
    })
})
```

- [ ] **Step 3: Run integration test to verify it fails before Phase 2 fix**

(Should pass now since Phase 2 already merged; this is the regression guard.)

`task test:int -- ./test/integration/privacy/`
Expected: PASS.

- [ ] **Step 4: Commit**

Commit message: `test(privacy): I-PRIV-1 guest sees no pre-arrival history (integration)`.

---

### Task 9: Integration test — character move resets floor

**Files:**

- Modify: `test/integration/privacy/privacy_test.go`

- [ ] **Step 1: Add the move-resets-floor scenario**

```go
var _ = Describe("I-PRIV-1: character move resets location floor", func() {
    var ts *privacytest.Server
    BeforeEach(func() { ts = privacytest.Start(GinkgoT()) })
    AfterEach(func() { ts.Stop() })

    It("denies query against the previous location and floors the new", func() {
        ctx := context.Background()
        char := ts.ConnectAuthed(ctx, "Diana")  // arrives in locA
        otherChar := ts.ConnectAuthed(ctx, "Theia")  // in locB
        otherChar.SendCommand(ctx, "say hello from locB before Diana arrives")
        otherChar.WaitForEvent(ctx, "core-communication:say")

        // Move Diana to locB.
        char.MoveTo(ctx, otherChar.LocationID)
        beforeArrive := time.Now()

        // Query against locA: STREAM_ACCESS_DENIED (Diana is no longer there).
        _, errA := char.Client.QueryStreamHistory(ctx, &corev1.QueryStreamHistoryRequest{
            SessionId: char.SessionID,
            Stream:    "location:" + char.OriginalLocationID.String(),
        })
        Expect(errA).To(MatchOopsCode("STREAM_ACCESS_DENIED"))

        // Query against locB: returns events only at-or-after Diana's arrival.
        respB, errB := char.Client.QueryStreamHistory(ctx, &corev1.QueryStreamHistoryRequest{
            SessionId: char.SessionID,
            Stream:    "location:" + otherChar.LocationID.String(),
        })
        Expect(errB).NotTo(HaveOccurred())
        for _, ev := range respB.GetEvents() {
            Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", beforeArrive))
        }
    })
})
```

- [ ] **Step 2: Run + Commit**

`task test:int -- ./test/integration/privacy/` → PASS.

Commit message: `test(privacy): I-PRIV-1 character move resets location floor (integration)`.

---

### Task 10: Integration test — denial wire-opacity (I-PRIV-5)

**Files:**

- Modify: `test/integration/privacy/privacy_test.go`

- [ ] **Step 1: Add the wire-opacity meta-test**

```go
var _ = Describe("I-PRIV-5: denial wire opacity", func() {
    var ts *privacytest.Server
    BeforeEach(func() { ts = privacytest.Start(GinkgoT()) })
    AfterEach(func() { ts.Stop() })

    DescribeTable("returns STREAM_ACCESS_DENIED for every denial reason",
        func(setup func(ctx context.Context) (*privacytest.Session, *corev1.QueryStreamHistoryRequest)) {
            ctx := context.Background()
            sess, req := setup(ctx)
            _, err := sess.Client.QueryStreamHistory(ctx, req)
            Expect(err).To(MatchOopsCode("STREAM_ACCESS_DENIED"),
                "denial path must collapse to STREAM_ACCESS_DENIED — I-PRIV-5")
        },
        Entry("wrong location (hard-gate)", func(ctx context.Context) (*privacytest.Session, *corev1.QueryStreamHistoryRequest) {
            sess := ts.ConnectAuthed(ctx, "Alpha")
            other := ts.NewLocation(ctx)
            return sess, &corev1.QueryStreamHistoryRequest{SessionId: sess.SessionID, Stream: "location:" + other.String()}
        }),
        Entry("not member (I-17 scene)", func(ctx context.Context) (*privacytest.Session, *corev1.QueryStreamHistoryRequest) {
            sess := ts.ConnectAuthed(ctx, "Beta")
            scene := ts.NewSceneWithoutMember(ctx)
            return sess, &corev1.QueryStreamHistoryRequest{SessionId: sess.SessionID, Stream: "scene:" + scene.String() + ":ic"}
        }),
        Entry("ABAC policy denied (other public)", func(ctx context.Context) (*privacytest.Session, *corev1.QueryStreamHistoryRequest) {
            sess := ts.ConnectAuthed(ctx, "Gamma")  // no special roles
            return sess, &corev1.QueryStreamHistoryRequest{SessionId: sess.SessionID, Stream: "admin:audit"}
        }),
        Entry("expired session", func(ctx context.Context) (*privacytest.Session, *corev1.QueryStreamHistoryRequest) {
            sess := ts.ConnectAuthed(ctx, "Delta")
            ts.ExpireSession(ctx, sess.SessionID)
            return sess, &corev1.QueryStreamHistoryRequest{SessionId: sess.SessionID, Stream: "location:" + sess.LocationID.String()}
        }),
        Entry("session not found (deleted)", func(ctx context.Context) (*privacytest.Session, *corev1.QueryStreamHistoryRequest) {
            sess := ts.ConnectAuthed(ctx, "Epsilon")
            ts.DeleteSession(ctx, sess.SessionID)
            return sess, &corev1.QueryStreamHistoryRequest{SessionId: sess.SessionID, Stream: "location:" + sess.LocationID.String()}
        }),
        // Per I-PRIV-5 (spec §6.3 + §9): missing-session denial collapses to
        // STREAM_ACCESS_DENIED at the QueryStreamHistory layer. The
        // implementation MUST translate the inner SESSION_NOT_FOUND from
        // sessionStore.Get into STREAM_ACCESS_DENIED before returning to the
        // gRPC wire (denial_reason=session_not_found goes to slog only).
        // Task 7 Step 4 adds this translation in the existing Get error
        // branch (query_stream_history.go ~line 65-74), preserving wire
        // opacity with all other denial paths.
    )
})
```

- [ ] **Step 2: Run + Commit**

Commit message: `test(privacy): I-PRIV-5 denial wire-opacity meta-test`.

---

## Phase 3 — Subscribe replay floor (NATS-source-of-truth + filter-at-delivery)

### Task 11: Extract consumer-config builder helper

**Files:**

- Create: `internal/eventbus/subscriber_consumer_config.go`
- Modify: `internal/eventbus/subscriber.go:159-195` (OpenSession) — delegate to builder

- [ ] **Step 1: Write failing unit test for the builder**

Create `internal/eventbus/subscriber_consumer_config_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/nats-io/nats.go/jetstream"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// stubJS satisfies the narrow `consumerLookup` interface.
// LookupConsumer returns a consumerInfo (NOT jetstream.Consumer) so the
// test stub does NOT need to implement the full jetstream.Consumer surface.
type stubJS struct {
    info           consumerInfo  // nil → not found
    lookupErr      error
    lookupCalled   bool
}

func (s *stubJS) LookupConsumer(_ context.Context, _, _ string) (consumerInfo, error) {
    s.lookupCalled = true
    if s.lookupErr != nil {
        return nil, s.lookupErr
    }
    return s.info, nil
}

// stubConsumer satisfies the narrow consumerInfo interface — just CachedInfo().
type stubConsumer struct {
    cfg jetstream.ConsumerConfig
}

func (s *stubConsumer) CachedInfo() *jetstream.ConsumerInfo {
    return &jetstream.ConsumerInfo{Config: s.cfg}
}

func TestBuildConsumerConfig_FreshOpenSession_ComputesMinFloor(t *testing.T) {
    minFloor := time.Now().Add(-time.Hour)
    js := &stubJS{info: nil, lookupErr: jetstream.ErrConsumerNotFound}
    cfg, err := buildConsumerConfig(context.Background(), js, "STREAM", "consumer-name",
        []string{"events.gid.location.X"}, minFloor)
    require.NoError(t, err)
    assert.Equal(t, jetstream.DeliverByStartTimePolicy, cfg.DeliverPolicy)
    require.NotNil(t, cfg.OptStartTime)
    assert.True(t, cfg.OptStartTime.Equal(minFloor))
}

func TestBuildConsumerConfig_ExistingConsumer_PreservesStartPolicy(t *testing.T) {
    origStart := time.Now().Add(-2 * time.Hour)
    js := &stubJS{info: &stubConsumer{cfg: jetstream.ConsumerConfig{
        DeliverPolicy: jetstream.DeliverByStartTimePolicy, OptStartTime: &origStart,
    }}}
    cfg, err := buildConsumerConfig(context.Background(), js, "STREAM", "consumer-name",
        []string{"events.gid.location.X", "events.gid.scene.Y.ic"}, time.Now())
    require.NoError(t, err)
    assert.Equal(t, jetstream.DeliverByStartTimePolicy, cfg.DeliverPolicy)
    require.NotNil(t, cfg.OptStartTime)
    assert.True(t, cfg.OptStartTime.Equal(origStart), "MUST copy existing OptStartTime verbatim — I-PRIV-8")
    assert.ElementsMatch(t, []string{"events.gid.location.X", "events.gid.scene.Y.ic"}, cfg.FilterSubjects)
}

func TestBuildConsumerConfig_TransientLookupError_FailsClosed(t *testing.T) {
    js := &stubJS{lookupErr: errors.New("nats: connection closed")}
    _, err := buildConsumerConfig(context.Background(), js, "STREAM", "consumer-name",
        []string{"events.gid.location.X"}, time.Now())
    require.Error(t, err)
    var oopsErr interface{ Code() string }
    require.True(t, errors.As(err, &oopsErr))
    assert.Equal(t, "EVENTBUS_CONSUMER_LOOKUP_FAILED", oopsErr.Code())
}

// (stubConsumer with CachedInfo() helper omitted for brevity — see existing
// test patterns in internal/eventbus/*_test.go for stub examples.)
```

- [ ] **Step 2: Run tests — verify they fail**

`task test -- ./internal/eventbus/ -run TestBuildConsumerConfig`
Expected: FAIL — `buildConsumerConfig` not defined.

- [ ] **Step 3: Implement the builder**

Create `internal/eventbus/subscriber_consumer_config.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
    "context"
    "errors"
    "time"

    "github.com/nats-io/nats.go/jetstream"
    "github.com/samber/oops"
)

// consumerInfo is the narrow surface buildConsumerConfig needs from an
// existing JetStream consumer — just enough to read its config. Real
// jetstream.Consumer satisfies this trivially. Tests provide a stub that
// implements ONLY CachedInfo without touching the full ~7-method
// jetstream.Consumer surface.
type consumerInfo interface {
    CachedInfo() *jetstream.ConsumerInfo
}

// consumerLookup is the narrow surface buildConsumerConfig needs from a
// JetStream context. Returns the local consumerInfo (NOT jetstream.Consumer)
// so test stubs do not need to satisfy the full jetstream.Consumer interface.
// Production callers pass a thin adapter that wraps s.js.Consumer(...) and
// returns its result as a consumerInfo.
type consumerLookup interface {
    LookupConsumer(ctx context.Context, stream, consumer string) (consumerInfo, error)
}

// buildConsumerConfig produces a jetstream.ConsumerConfig that is safe to pass
// to CreateOrUpdateConsumer regardless of whether the durable already exists.
// Implements the NATS-source-of-truth pattern per holomush-iwzt §6.2 / I-PRIV-8.
//
// On existing-durable hit: copies DeliverPolicy/OptStartTime/OptStartSeq verbatim
// from the existing consumer's config; only FilterSubjects is set fresh.
//
// On ErrConsumerNotFound: builds a fresh config with DeliverByStartTimePolicy +
// minFloor (or DeliverAllPolicy if minFloor is zero).
//
// On any other lookup error: fails closed by returning EVENTBUS_CONSUMER_LOOKUP_FAILED.
// CreateOrUpdateConsumer MUST NOT be invoked.
func buildConsumerConfig(
    ctx context.Context, js consumerLookup,
    streamName, consumerName string,
    filterSubjects []string, minFloor time.Time,
) (jetstream.ConsumerConfig, error) {
    existing, lookupErr := js.LookupConsumer(ctx, streamName, consumerName)
    switch {
    case lookupErr == nil && existing != nil:
        info := existing.CachedInfo()
        if info == nil {
            return jetstream.ConsumerConfig{}, oops.Code("EVENTBUS_CONSUMER_LOOKUP_FAILED").
                With("stream", streamName).With("consumer", consumerName).
                Errorf("existing consumer returned nil CachedInfo")
        }
        cur := info.Config
        cfg := jetstream.ConsumerConfig{
            Durable:        consumerName,
            Name:           consumerName,
            FilterSubjects: filterSubjects,
            DeliverPolicy:  cur.DeliverPolicy,
            OptStartSeq:    cur.OptStartSeq,
            // Other immutable fields (AckPolicy, AckWait, MaxAckPending,
            // InactiveThreshold) propagated by the caller via the default
            // cfg shape established at OpenSession-time.
        }
        // OptStartTime is *time.Time — preserve nil vs. non-nil verbatim.
        if cur.OptStartTime != nil {
            t := *cur.OptStartTime
            cfg.OptStartTime = &t
        }
        return cfg, nil
    case errors.Is(lookupErr, jetstream.ErrConsumerNotFound):
        cfg := jetstream.ConsumerConfig{
            Durable:        consumerName,
            Name:           consumerName,
            FilterSubjects: filterSubjects,
        }
        if !minFloor.IsZero() {
            cfg.DeliverPolicy = jetstream.DeliverByStartTimePolicy
            cfg.OptStartTime = &minFloor
        } else {
            cfg.DeliverPolicy = jetstream.DeliverAllPolicy
        }
        return cfg, nil
    default:
        return jetstream.ConsumerConfig{}, oops.Code("EVENTBUS_CONSUMER_LOOKUP_FAILED").
            With("stream", streamName).With("consumer", consumerName).
            Wrap(lookupErr)
    }
}
```

- [ ] **Step 4: Add the production adapter**

The narrow `consumerLookup` interface returns `consumerInfo`, but the real `jetstream.JetStream.Consumer(...)` returns `jetstream.Consumer`. Add a one-line adapter in `subscriber_consumer_config.go`:

```go
// jsConsumerLookupAdapter wraps a jetstream.JetStream so it satisfies the
// narrow consumerLookup interface buildConsumerConfig consumes.
type jsConsumerLookupAdapter struct{ js jetstream.JetStream }

func (a jsConsumerLookupAdapter) LookupConsumer(ctx context.Context, stream, name string) (consumerInfo, error) {
    c, err := a.js.Consumer(ctx, stream, name)
    if err != nil { return nil, err }
    return c, nil  // jetstream.Consumer satisfies consumerInfo (has CachedInfo)
}
```

OpenSession and SetFilters in Task 12 then call `buildConsumerConfig(ctx, jsConsumerLookupAdapter{js: s.js}, ...)`.

- [ ] **Step 5: Run tests — verify PASS**

`task test -- ./internal/eventbus/ -run TestBuildConsumerConfig`

- [ ] **Step 6: Commit**

Commit message: `feat(eventbus): NATS-source-of-truth consumer-config builder (iwzt ghpx)`.

---

### Task 12: Wire builder into OpenSession + SetFilters

**Files:**

- Modify: `internal/eventbus/subscriber.go:159-195` (OpenSession)
- Modify: `internal/eventbus/subscriber.go:336-366` (SetFilters)

- [ ] **Step 1: Refactor OpenSession to use the builder**

In `internal/eventbus/subscriber.go:177-187`, replace the inline cfg construction:

```go
name := sessionConsumerName(sessionID)
subjects := subjectsToStrings(filters)
if len(subjects) == 0 {
    return nil, oops.Code("EVENTBUS_SESSION_FILTERS_REQUIRED").
        With("session_id", sessionID).Errorf("at least one subject filter required")
}

// Compute minFloor across subjects — Phase 3 wires this from session info.
// For now (this task only), pass time.Time{} so first-create gets DeliverAllPolicy,
// preserving existing behavior. Phase 3's downstream task replaces the zero value
// with the real minFloor computation.
minFloor := time.Time{}

cfg, err := buildConsumerConfig(ctx, jsConsumerLookupAdapter{js: s.js}, StreamName, name, subjects, minFloor)
if err != nil {
    return nil, err
}
// Augment cfg with the non-start-policy defaults that OpenSession owns:
cfg.AckPolicy = jetstream.AckExplicitPolicy
cfg.AckWait = s.ackWait
cfg.MaxAckPending = s.maxAckPending
cfg.InactiveThreshold = s.inactiveThreshold

cons, err := s.js.CreateOrUpdateConsumer(ctx, StreamName, cfg)
if err != nil {
    return nil, oops.Code("EVENTBUS_SESSION_CONSUMER_FAILED").
        With("session_id", sessionID).With("consumer", name).Wrap(err)
}
```

- [ ] **Step 2: Refactor SetFilters identically**

In `internal/eventbus/subscriber.go:336-366`, apply the same `buildConsumerConfig` pattern. Confirm via existing tests that SetFilters callers still pass.

- [ ] **Step 3: Run all subscriber tests**

`task test -- ./internal/eventbus/`
Expected: PASS — existing behavior preserved (minFloor=zero → DeliverAllPolicy, same as before).

- [ ] **Step 4: Commit**

Commit message: `refactor(eventbus): OpenSession + SetFilters use NATS-source-of-truth builder`.

---

### Task 13: Compute minFloor at OpenSession entry from session info

**Files:**

- Modify: `internal/eventbus/subscriber.go` — extend `OpenSession` signature to accept the session info needed to compute minFloor, OR add a `MinFloor time.Time` to the call site
- Modify: caller in `internal/grpc/server.go:855`

Decision (locked here to prevent ambiguity): add a new optional parameter `minFloor time.Time` to `OpenSession`. Caller computes it.

- [ ] **Step 1: Update OpenSession signature**

```go
func (s *JetStreamSubscriber) OpenSession(
    ctx context.Context, sessionID string, identity SessionIdentity,
    filters []Subject, minFloor time.Time,
) (SessionStream, error) {
    // ... pass minFloor through to buildConsumerConfig
}
```

- [ ] **Step 2: Update Subscriber interface in `bus.go`**

Add `minFloor time.Time` to the `Subscriber.OpenSession` signature.

- [ ] **Step 3: Update server.go caller to compute minFloor**

In `internal/grpc/server.go:855` (before `subscriber.OpenSession`):

```go
// Compute minFloor across all subscribed subjects per holomush-iwzt §6.2 Tier 1.
// MAX across subjects (not MIN) — JetStream delivers events >= start time, so
// MAX gives the smallest set that includes events visible to at least one subject.
// (Per-subject filter-at-delivery in Tier 2 drops events below per-subject floor.)
var minFloor time.Time
for _, subj := range subscribedSubjects {  // adjust var name to match
    f := streamScopeFloor(info, string(subj))
    if f.After(minFloor) {
        minFloor = f
    }
}
sessionStream, err := s.subscriber.OpenSession(ctx, sessionID, identity, subscribedSubjects, minFloor)
```

- [ ] **Step 4: Update SetFilters signature similarly**

Current signature (verify at `internal/eventbus/subscriber.go:336`): `SetFilters(ctx context.Context, filters []Subject) error`. New signature: `SetFilters(ctx context.Context, filters []Subject, minFloor time.Time) error`. Recompute `minFloor` at each filter change (per holomush-iwzt §6.2 — although immutability rules mean the builder will preserve the original `OptStartTime` regardless; passing the new minFloor still matters for the `ErrConsumerNotFound` branch and future invariants).

- [ ] **Step 5: Find and update ALL OpenSession callsites**

The interface change breaks every caller. Enumerate them first:

```bash
rg -nP "OpenSession\(" --type go internal/
```

Expected hits: `internal/eventbus/subscriber.go` (implementation), `internal/eventbus/bus.go` (interface), `internal/grpc/server.go` (the production caller updated above), plus 10-15 test callsites in `internal/eventbus/subscriber*_test.go`, `internal/eventbus/eventbustest/embedded_test.go`, `internal/eventbus/bus_test.go` (fakeBus.OpenSession). Pass `time.Time{}` (zero minFloor) at every test callsite — tests don't exercise floor semantics; zero preserves the existing DeliverAllPolicy behavior in those paths.

Same exercise for `SetFilters(`:

```bash
rg -nP "SetFilters\(" --type go internal/
```

- [ ] **Step 6: Run tests**

`task test -- ./internal/eventbus/ ./internal/grpc/`
Expected: PASS.

- [ ] **Step 7: Commit**

Commit message: `feat(eventbus): plumb minFloor through OpenSession + SetFilters (iwzt Tier 1)`.

---

### Task 14: Per-subject filter-at-delivery in Subscribe broadcaster

**Files:**

- Modify: `internal/grpc/server.go` — locate the Subscribe dispatch loop (look for `stream.Send(...)` calls in the Subscribe handler around line 1000-1100)

- [ ] **Step 1: Write failing test — events below floor are dropped**

In `internal/grpc/server_test.go`:

```go
func TestSubscribeBroadcaster_DropsBelowScopeFloor(t *testing.T) {
    server, ctx := newTestCoreServerWithBus(t)
    sess := seedActiveSessionInLocation(t, server, "Eve", locA)
    // Manually set LocationArrivedAt to T_now to force a strict floor.
    require.NoError(t, server.sessionStore.UpdateLocationOnMove(
        ctx, sess.CharacterID, locA, time.Now()))

    // Emit an event with timestamp BEFORE LocationArrivedAt by injecting at the bus.
    pastEvent := makeEventAt(time.Now().Add(-time.Hour), "location:"+locA.String())
    require.NoError(t, server.injectRawBusEvent(pastEvent))

    // Open subscribe; assert pastEvent does NOT arrive.
    stream := openSubscribe(t, server, sess.ID)
    select {
    case ev := <-stream.Events():
        t.Fatalf("unexpected event delivered below floor: %v", ev)
    case <-time.After(300 * time.Millisecond):
        // Pass — event was filtered.
    }
}
```

- [ ] **Step 2: Run test — verify FAIL**

`task test -- -run TestSubscribeBroadcaster_DropsBelowScopeFloor ./internal/grpc/`
Expected: FAIL — broadcaster does not filter.

- [ ] **Step 3: Add filter at the dispatch site**

The Subscribe event delivery lives in `func (s *CoreServer) dispatchDelivery(...)` at `internal/grpc/server.go:1006-1069` (call site is `internal/grpc/server.go:981`). This is **a single-event function called once per delivery, NOT a for-loop** — so `continue` is invalid. The filter MUST ack the delivery before returning so JetStream does NOT redeliver the same event forever.

Insert at the top of `dispatchDelivery`, BEFORE the existing `stream.Send` and ack logic:

```go
// Per-subject filter-at-delivery — load-bearing privacy gate per holomush-iwzt §6.2 Tier 2.
//
// Read session info per event to pick up post-reattach / post-move floor
// changes. This is a per-event sessionStore.Get — acceptable for the
// initial implementation; a follow-up bead (Step 5 below) covers caching
// this in a per-Subscribe goroutine local when latency profiling shows
// it matters.
//
// Drop path MUST ack the delivery — otherwise JetStream redelivers the
// same event indefinitely on every consumer reconnect.
currentInfo, getErr := s.sessionStore.Get(ctx, info.ID)
if getErr != nil {
    // Defensive: if we cannot read session info we fail closed and drop
    // the event. Ack-and-return so JS does not redeliver.
    slog.WarnContext(ctx, "filter-at-delivery dropped event — session lookup failed",
        "session_id", info.ID, "error", getErr)
    if ackErr := delivery.Ack(); ackErr != nil {
        slog.WarnContext(ctx, "ack failed on filter-drop path", "error", ackErr)
    }
    return nil
}
floor := streamScopeFloor(currentInfo, string(event.Subject))
if !floor.IsZero() && event.Timestamp.Before(floor) {
    slog.DebugContext(ctx, "filter-at-delivery dropped event below scope floor",
        "session_id", info.ID, "subject", event.Subject,
        "event_ts", event.Timestamp, "floor", floor)
    if ackErr := delivery.Ack(); ackErr != nil {
        slog.WarnContext(ctx, "ack failed on filter-drop path", "error", ackErr)
    }
    return nil
}
// ... fall through to existing stream.Send + ack ...
```

- [ ] **Step 4: Run test — verify PASS**

`task test -- -run TestSubscribeBroadcaster_DropsBelowScopeFloor ./internal/grpc/`

- [ ] **Step 5: File optimization follow-up bead**

```bash
bd create "Optimize filter-at-delivery: cache session floor in per-Subscribe goroutine" \
  --type task --priority 3 --labels web,perf,follow-up \
  --description "Initial Subscribe dispatch (iwzt Task 14) does a per-event sessionStore.Get for the filter-at-delivery check. Latency-acceptable for v1 but expected to dominate hot-path for busy locations. Optimization: snapshot relevant SessionInfo fields (LocationID, LocationArrivedAt, IsGuest, GuestCharacterCreatedAt, FocusMemberships) in a per-Subscribe goroutine local, refreshed by lifecycle hooks (BumpLocationArrivedAt, MovementHook fires, Subscribe.ReattachCAS). Requires extending the locationFollower ctrl channel or adding a sibling channel."
```

- [ ] **Step 6: Commit**

Commit message: `feat(grpc): per-subject filter-at-delivery in Subscribe broadcaster (iwzt Tier 2)`.

---

### Task 15: Integration test — reconnect-after-detach durable replay (I-PRIV-3)

> **Round 3 spec amendment (2026-05-17):** the original "filter drop" framing
> for this task was rejected by the design-reviewer when NATS JetStream
> consumer-immutability constraints (error 10012) made the "advance the
> consumer's `OptStartTime` on reattach" mechanism impossible. Per the §I-PRIV-3
> amendment, `Subscribe.ReattachCAS` MUST NOT change the durable consumer's
> `DeliverPolicy`/`OptStartTime`/`OptStartSeq`, and `LocationArrivedAt` is
> UNCHANGED across reattach. Events emitted during the detach window have
> timestamp ≥ `LocationArrivedAt` and therefore PASS the per-event
> filter-at-delivery — they MUST be delivered to the reattached session via
> JetStream resume from last-acked-seq. There is no `LastReattachAt` floor
> in production; the pre-Round-3 wording referring to one was removed from
> the harness in PR iwzt-16-plan-fix.

**Files:**

- Modify: `test/integration/privacy/privacy_test.go`
- Dependencies: requires the harness's live Subscribe stream wiring
  (`Session.DetachTransport`, `Session.ReattachTransport`, `Session.WaitForEvent`)
  — tracked as precursor bead `holomush-m5nj` so the harness work can land
  independently of this test.

- [ ] **Step 1: Add the reattach-durability scenario**

```go
var _ = Describe("I-PRIV-3: ReattachCAS preserves durable; Subscribe replay delivers detach-window events", func() {
    var ts *integrationtest.Server
    BeforeEach(func() { ts = integrationtest.Start(GinkgoT()) })
    AfterEach(func() { ts.Stop() })

    It("delivers events emitted during transport detach when transport reattaches", func() {
        ctx := context.Background()
        sessA := ts.ConnectAuthed(ctx, "Felix")
        other := ts.ConnectAuthed(ctx, "Gemma")  // same location
        Expect(sessA.LocationID).To(Equal(other.LocationID))

        sessA.DetachTransport(ctx)
        // While Felix's transport is detached, Gemma emits.
        const detachWindowType = "iwzt16-test:during-detach-marker"
        Expect(other.EmitDirectEvent(ctx,
            "location:"+other.LocationID.String(),
            detachWindowType,
            []byte(`{"character_name":"Gemma","action":"speaks while Felix is detached."}`))).
            To(Succeed())

        sessA.ReattachTransport(ctx)
        // Subscribe replay MUST deliver the detach-window event via durable
        // resume — per spec §I-PRIV-3 (Round 3) the filter-at-delivery floor
        // remains LocationArrivedAt, which is unchanged across reattach;
        // the detach-window event is at-or-after that floor so it passes.
        ev := sessA.WaitForEvent(ctx, detachWindowType)
        Expect(ev).NotTo(BeNil(),
            "reattach Subscribe replay MUST deliver the detach-window event (I-PRIV-3)")
        Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", sessA.LocationArrivedAt),
            "delivered event must be at-or-after the unchanged LocationArrivedAt")
    })
})
```

- [ ] **Step 2: Run + Commit**

`task test:int -- ./test/integration/privacy/`
Commit message: `test(privacy): I-PRIV-3 reattach durable replay (events delivered across detach window)`.

---

### Task 16: Integration test — multi-session continuity

**Files:**

- Modify: `test/integration/privacy/privacy_test.go`

- [ ] **Step 1: Add multi-session continuity test**

```go
var _ = Describe("I-PRIV-4 / rc8b: per-session floors are independent", func() {
    var ts *privacytest.Server
    BeforeEach(func() { ts = privacytest.Start(GinkgoT()) })
    AfterEach(func() { ts.Stop() })

    It("preserves telnet visibility when web reattaches", func() {
        ctx := context.Background()
        char := ts.AuthedPlayer(ctx, "Hugo")
        webSess := char.OpenWebSession(ctx)       // arrived T0
        time.Sleep(50 * time.Millisecond)
        telnetSess := char.OpenTelnetSession(ctx) // arrived T1
        time.Sleep(50 * time.Millisecond)

        webSess.DetachTransport(ctx)
        other := ts.ConnectAuthed(ctx, "Iris")  // same location
        other.SendCommand(ctx, "say t2-event")
        other.WaitForEvent(ctx, "core-communication:say")
        webSess.ReattachTransport(ctx)
        webArrival := time.Now()

        // Telnet's QueryStreamHistory still includes the t2 event.
        telnetHistory := telnetSess.QueryStreamHistory(ctx, "location:"+char.LocationID.String())
        Expect(eventTypesAt(telnetHistory)).To(ContainElement("core-communication:say"))

        // Web's QueryStreamHistory does NOT include the t2 event (below web's
        // post-reattach floor).
        webHistory := webSess.QueryStreamHistory(ctx, "location:"+char.LocationID.String())
        for _, ev := range webHistory {
            Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", webArrival))
        }
    })
})
```

- [ ] **Step 2: Run + Commit**

Commit message: `test(privacy): I-PRIV-4 multi-session continuity (web reattach preserves telnet visibility)`.

---

## Phase 4 — Scene-join floor

### Task 17: Extend streamScopeFloor for scene streams (already present in §6.1)

Phase 2 Task 6 already implemented the scene branch in `streamScopeFloor`. Phase 4's enforcement work happens because Task 7 already wires `streamScopeFloor` into Step 6 — scenes pick up the floor automatically. This task verifies via test.

- [ ] **Step 1: Write integration test — scene-join floor**

```go
var _ = Describe("I-PRIV-2 / scene floor: scene events before join are invisible", func() {
    var ts *privacytest.Server
    BeforeEach(func() { ts = privacytest.Start(GinkgoT()) })
    AfterEach(func() { ts.Stop() })

    It("denies pre-join events and floors post-join history", func() {
        ctx := context.Background()
        owner := ts.ConnectAuthed(ctx, "Jamie")
        scene := owner.CreateScene(ctx)
        owner.SendCommand(ctx, "say in-scene-before-Kai")  // emits to scene:S:ic

        time.Sleep(50 * time.Millisecond)
        joiner := ts.ConnectAuthed(ctx, "Kai")
        joiner.JoinScene(ctx, scene)
        joinedAt := time.Now()

        history := joiner.QueryStreamHistory(ctx, "scene:"+scene.String()+":ic")
        for _, ev := range history {
            Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", joinedAt))
        }
    })
})
```

- [ ] **Step 2: Run + Commit**

If the scene floor wasn't already implemented end-to-end in Task 6+7, this test surfaces the gap; iterate on `streamScopeFloor` + Subscribe filter to cover scenes.

Commit message: `test(privacy): I-PRIV-2 scene-join floor enforcement`.

---

## Phase 5 — Staff ABAC override

### Task 18: Seed staff/admin policy

**Files:**

- Modify: `internal/access/policy/seed.go`
- Test: `internal/access/policy/seed_test.go`

- [ ] **Step 1: Verify the policy DSL idiom against existing seeds**

Before authoring, read `internal/access/policy/seed.go` for the existing resource-type-guard idioms. The DSL accepts `resource is <type>` where `<type>` is one of the registered ResourceProvider namespace types. Confirm whether `stream` is registered as a resource namespace via `rg -nP "ResourceProvider|RegisterResource|namespace.*stream" internal/access/`. If `stream` is NOT a registered resource type, either (a) register it (separate task, out of scope), or (b) use a more permissive seed without the type guard.

For staff override the cleanest seed (no resource-type dependency) is action-only:

```go
{
    Name:    "staff_read_unrestricted_history",
    DSLText: `permit(principal is character, action == "read_unrestricted_history", resource) when { "staff" in principal.character.roles || "admin" in principal.character.roles };`,
},
```

(Removing the `resource is stream` type guard so the policy matches any resource passed in — appropriate because the gate is about the action, not the resource type.)

- [ ] **Step 2: Add seed-coverage test asserting policy is loaded**

```go
func TestSeed_IncludesStaffUnrestrictedHistoryPolicy(t *testing.T) {
    seeds := AllSeeds()  // or whatever the export is
    var found bool
    for _, s := range seeds {
        if s.Name == "staff_read_unrestricted_history" {
            found = true
            break
        }
    }
    require.True(t, found, "Phase 5 must seed staff_read_unrestricted_history policy")
}
```

- [ ] **Step 3: Run + Commit**

`task test -- ./internal/access/policy/`
Commit message: `feat(access): seed staff_read_unrestricted_history policy (iwzt Phase 5)`.

---

### Task 19: Replace staffOverride stub with real ABAC call

**Files:**

- Modify: `internal/grpc/scope_floor.go` (the stub from Task 7)

- [ ] **Step 1: Write failing test — staff override bypasses hard-gate**

In `internal/grpc/query_stream_history_test.go`:

```go
func TestQueryStreamHistory_StaffOverride_BypassesHardGate(t *testing.T) {
    server, ctx := newTestCoreServerWithBus(t)
    staff := seedActiveSessionWithRole(t, server, "AdminAce", locA, []string{"staff"})

    // Staff queries a location they are NOT in — should succeed (bypass).
    _, err := server.QueryStreamHistory(ctx, &corev1.QueryStreamHistoryRequest{
        SessionId: staff.ID,
        Stream:    "location:" + locB.String(),
    })
    require.NoError(t, err, "staff role must bypass location hard-gate")
}
```

- [ ] **Step 2: Run test — verify FAIL**

Expected: FAIL (stub returns false).

- [ ] **Step 3: Replace stub with real implementation**

In `internal/grpc/scope_floor.go` (the stub's `accessEngine` interface already takes `accessTypes.AccessRequest` by value from Task 7 Step 3, matching `NewAccessRequest`'s return type):

```go
func staffOverride(ctx context.Context, info *session.Info, engine accessTypes.AccessPolicyEngine) bool {
    if engine == nil {
        return false
    }
    accessReq, err := accessTypes.NewAccessRequest(
        "character:"+info.CharacterID.String(),
        "read_unrestricted_history",
        "stream:*",  // resource-agnostic — policy is action-scoped
        nil,
    )
    if err != nil { return false }
    decision, evalErr := engine.Evaluate(ctx, accessReq)
    return evalErr == nil && decision.IsAllowed()
}
```

- [ ] **Step 4: Run tests — verify PASS**

- [ ] **Step 5: Add integration test — staff override does NOT bypass floor (I-PRIV-6)**

```go
var _ = Describe("I-PRIV-6: staff override bypasses gate, not floor", func() {
    var ts *privacytest.Server
    BeforeEach(func() { ts = privacytest.Start(GinkgoT()) })
    AfterEach(func() { ts.Stop() })

    It("staff in locA querying locB sees events only at-or-after their own arrival floor", func() {
        ctx := context.Background()
        staff := ts.ConnectAuthedWithRoles(ctx, "Mira", []string{"staff"})  // in locA
        other := ts.ConnectAuthed(ctx, "Nadia")  // in locB
        other.SendCommand(ctx, "say long-before-staff-asks")
        other.WaitForEvent(ctx, "core-communication:say")
        time.Sleep(2 * time.Second)
        staffQueryAt := time.Now()

        // staff arrival to its own location was AT staff connect time, well before staffQueryAt.
        // But the test focuses on what queries return: staff's floor is its own
        // LocationArrivedAt in locA. For locB query under override, the floor still applies.
        resp, err := staff.Client.QueryStreamHistory(ctx, &corev1.QueryStreamHistoryRequest{
            SessionId: staff.SessionID,
            Stream:    "location:" + other.LocationID.String(),
        })
        Expect(err).NotTo(HaveOccurred())
        for _, ev := range resp.GetEvents() {
            // Staff was NOT in locB when the say-event fired; their floor for that
            // location (computed naturally via the streamScopeFloor MAX) excludes it.
            Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", staff.LocationArrivedAt))
        }
        Expect(resp.GetEvents()).NotTo(ContainElementMatching(func(e *corev1.GameEvent) bool {
            return e.GetType() == "core-communication:say"
        }), "the pre-arrival 'long-before-staff-asks' MUST NOT be returned — I-PRIV-6")
    })
})
```

(Spec §6.1 commits to: the temporal floor for staff cross-location reads IS `staff.LocationArrivedAt` (the requesting session's own arrival timestamp at its own location). For a staff member in locA querying locB, the floor is their locA arrival time. This is I-PRIV-6's "bypass hard-gate not floor" applied literally — staff can read across locations, but not retroactively past their own session's lifetime. The test above asserts exactly this semantic. Do NOT change the floor computation for staff queries.)

- [ ] **Step 6: Commit**

Commit message: `feat(grpc): real staffOverride + I-PRIV-6 enforcement (iwzt Phase 5)`.

---

## Phase 6 — Guest identity overlay

### Task 20: Wire GuestCharacterCreatedAt into streamScopeFloor MAX

The `streamScopeFloor` helper from Task 6 already includes the `info.IsGuest && info.GuestCharacterCreatedAt.After(base)` overlay. Task 5 already populated the field at guest session creation. Phase 6 verifies end-to-end.

- [ ] **Step 1: Write integration test — guest sees no events from before its character was created**

```go
var _ = Describe("I-PRIV-2: guest character.CreatedAt floor (name-reuse safety)", func() {
    var ts *privacytest.Server
    BeforeEach(func() { ts = privacytest.Start(GinkgoT()) })
    AfterEach(func() { ts.Stop() })

    It("brand-new guest character with same name as a logged-out prior guest sees nothing of prior history", func() {
        ctx := context.Background()
        firstGuest := ts.ConnectGuest(ctx)
        firstName := firstGuest.CharacterName
        firstGuest.SendCommand(ctx, "say first-guest-utterance")
        firstGuest.WaitForEvent(ctx, "core-communication:say")
        firstGuest.Logout(ctx)
        // Wait long enough that the namer pool may recycle the name.
        time.Sleep(100 * time.Millisecond)

        // Spin up many guests until we get one with the same name (test infra trick).
        var reusedGuest *privacytest.Session
        for i := 0; i < 50; i++ {
            g := ts.ConnectGuest(ctx)
            if g.CharacterName == firstName {
                reusedGuest = g
                break
            }
            g.Logout(ctx)
        }
        if reusedGuest == nil {
            Skip("namer pool did not produce a name collision within 50 attempts — pool likely too large for this test environment")
        }

        // The reused-name guest sees NONE of the first guest's events.
        history := reusedGuest.QueryStreamHistory(ctx, "location:"+reusedGuest.LocationID.String())
        for _, ev := range history {
            Expect(ev.GetTimestamp().AsTime()).To(BeTemporally(">=", reusedGuest.SessionCreatedAt),
                "name-reuse leaked event from prior identity — I-PRIV-2 broken")
        }
    })
})
```

- [ ] **Step 2: Run + Commit**

Commit message: `test(privacy): I-PRIV-2 guest name-reuse identity floor`.

---

## I-PRIV-7 — Plugin manifest history_scope validation

### Task 21: Add `history_scope:` field to manifest schema

**Files:**

- Modify: `internal/plugin/manifest.go`
- Modify: `schemas/plugin.schema.json` (run `go generate` to update)
- Test: `internal/plugin/manifest_test.go`

- [ ] **Step 1: Write failing unit tests (4 cases per spec §8)**

```go
func TestManifest_HistoryScopeValidation(t *testing.T) {
    cases := []struct {
        name     string
        yaml     string
        wantErr  bool
        errMatch string
    }{
        {
            name: "plugin with no emits (OK — history_scope not required)",
            yaml: `name: example`,
            wantErr: false,
        },
        {
            name: "plugin emitting events, history_scope=grid (OK)",
            yaml: `name: example
emits: ["scene"]
history_scope: grid`,
            wantErr: false,
        },
        {
            name: "plugin emitting events, history_scope=scene (OK)",
            yaml: `name: example
emits: ["scene"]
history_scope: scene`,
            wantErr: false,
        },
        {
            name: "plugin emitting events, history_scope=custom (OK)",
            yaml: `name: example
emits: ["custom_ns"]
history_scope: custom`,
            wantErr: false,
        },
        {
            name: "plugin emitting events without history_scope (REJECT — I-PRIV-7)",
            yaml: `name: example
emits: ["scene"]`,
            wantErr:  true,
            errMatch: "history_scope",
        },
        {
            name: "plugin with unknown history_scope value (REJECT)",
            yaml: `name: example
emits: ["custom_ns"]
history_scope: nonexistent`,
            wantErr:  true,
            errMatch: "history_scope",
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            _, err := ParseManifest([]byte(tc.yaml))
            if tc.wantErr {
                require.Error(t, err)
                assert.Contains(t, err.Error(), tc.errMatch)
            } else {
                require.NoError(t, err)
            }
        })
    }
}
```

- [ ] **Step 2: Run tests — verify FAIL**

`task test -- ./internal/plugin/ -run TestManifest_HistoryScopeValidation`
Expected: FAIL.

- [ ] **Step 3: Add `HistoryScope` field to the Manifest struct**

In `internal/plugin/manifest.go`:

```go
type Manifest struct {
    // ... existing fields ...
    HistoryScope string `yaml:"history_scope,omitempty" json:"history_scope,omitempty"`
}

// Closed enum for valid history_scope values per holomush-iwzt I-PRIV-7.
var validHistoryScopes = map[string]bool{
    "grid":   true,
    "scene":  true,
    "custom": true,
}
```

- [ ] **Step 4: Add a manifest-level validator for I-PRIV-7**

The existing `validateEmits(names []string)` (around line 274-294) takes a string slice with no manifest receiver — it cannot access `m.HistoryScope`. Add the I-PRIV-7 check at the manifest level instead, in `(*Manifest).Validate` (the call site of `validateEmits(m.Emits)` at `internal/plugin/manifest.go:441`).

After the call to `validateEmits(m.Emits)` succeeds inside `Validate`, add:

```go
// I-PRIV-7: history_scope validation. Two requirements:
//
//  1. If declared, the value MUST be one of the closed-enum values.
//  2. Plugins emitting under any namespace are required to declare
//     history_scope EXPLICITLY. The default is opt-in, not exempt-list —
//     this avoids the "stream prefix vs manifest namespace" confusion
//     identified during plan-review (the spec's §3 grid/scene rows are
//     stream PREFIXES like `location:X` or `scene:Y:ic`, not manifest
//     namespace identifiers).
//
// Per holomush-iwzt I-PRIV-7.
if m.HistoryScope != "" && !validHistoryScopes[m.HistoryScope] {
    return oops.With("plugin", m.Name).
        Errorf("history_scope %q is invalid; valid: grid, scene, custom", m.HistoryScope)
}
if len(m.Emits) > 0 && m.HistoryScope == "" {
    return oops.With("plugin", m.Name).
        With("emits", m.Emits).
        Errorf("plugin emits events but manifest does not declare history_scope (holomush-iwzt I-PRIV-7)")
}
```

This opt-in default (every emitting plugin must declare `history_scope`) is more restrictive than the spec's narrow §3-only exemption attempt — but cleanly closes the "silent inheritance" gap I-PRIV-7 forbids. Existing plugin manifests in the repo will need their `manifest.yaml` updated to declare `history_scope: grid` (core-communication) or `history_scope: scene` (core-scenes) — call out as a migration step.

- [ ] **Step 4.5: Update existing plugin manifests**

Manifest filename in this repo is `plugin.yaml` (NOT `manifest.yaml`). Enumerate:

```bash
rg -nP "^emits:" plugins/*/plugin.yaml
```

Known-existing manifests requiring migration (verify via the grep above; update each):

- `plugins/core-communication/plugin.yaml` — emits to `location:*` streams (say/pose/whisper/page). Add `history_scope: grid`.
- `plugins/core-scenes/plugin.yaml` — emits to `scene:*:*` streams. Add `history_scope: scene`.
- Any other plugin under `plugins/` whose `emits:` field is non-empty MUST get a declaration too. Use `history_scope: grid` for plugins targeting location streams, `history_scope: scene` for scene streams, `history_scope: custom` only for plugins with truly novel semantics.

Without this migration step, `task test` will fail on plugin loading because every existing emitting manifest now fails I-PRIV-7 validation.

- [ ] **Step 5: Run tests — verify PASS**

`task test -- ./internal/plugin/`

- [ ] **Step 6: Run `go generate ./...` to regenerate JSON schema**

```bash
go generate ./...
git diff schemas/plugin.schema.json  # verify history_scope is in the schema
```

- [ ] **Step 7: Commit**

Commit message: `feat(plugin): manifest history_scope field + I-PRIV-7 validation`.

---

### Task 22: Placeholder integration test for plugin-history-scope (I-PRIV-7)

**Files:**

- Modify: `test/integration/privacy/privacy_test.go`

- [ ] **Step 1: Add the skipping placeholder**

```go
var _ = Describe("I-PRIV-7: plugin-owned history_scope semantics", func() {
    It("exercises a plugin that declared custom history_scope (placeholder)", func() {
        Skip("no plugin currently declares history_scope: custom — re-enable when a plugin adopts this field")
        // When a plugin adopts history_scope: custom, replace Skip with the
        // real test exercising its divergent semantics.
    })
})
```

- [ ] **Step 2: Commit**

Commit message: `test(privacy): I-PRIV-7 placeholder skip (no plugin uses history_scope yet)`.

---

## Meta-test

### Task 23: Meta-test for I-PRIV-* invariant coverage

**Files:**

- Create or modify: `test/meta/iwzt_invariants_test.go`

- [ ] **Step 1: Write the meta-test**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
    "os"
    "path/filepath"
    "regexp"
    "strings"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// TestMeta_IPrivInvariantCoverage asserts that every I-PRIV-N invariant
// (1..6, 8) declared in the history-scope privacy spec has at least one
// corresponding test that mentions it by ID. I-PRIV-7 is satisfied
// vacuously when no plugin declares history_scope: (see spec §8).
func TestMeta_IPrivInvariantCoverage(t *testing.T) {
    required := []string{"I-PRIV-1", "I-PRIV-2", "I-PRIV-3", "I-PRIV-4", "I-PRIV-5", "I-PRIV-6", "I-PRIV-8"}
    found := make(map[string]bool, len(required))

    pat := regexp.MustCompile(`I-PRIV-\d+`)
    err := filepath.WalkDir(".", func(path string, d os.DirEntry, walkErr error) error {
        if walkErr != nil { return walkErr }
        if d.IsDir() || !strings.HasSuffix(path, "_test.go") { return nil }
        data, _ := os.ReadFile(path)
        for _, m := range pat.FindAll(data, -1) {
            found[string(m)] = true
        }
        return nil
    })
    require.NoError(t, err)

    for _, inv := range required {
        assert.True(t, found[inv], "invariant %s has no corresponding test mentioning it by ID", inv)
    }
    // I-PRIV-7: satisfied vacuously when no plugin manifest declares history_scope.
    // The manifest-validation unit test in Task 21 partially covers it; full
    // satisfaction is when a plugin adopts the field and the placeholder test
    // becomes real.
}
```

(Adjust the walk root to encompass `internal/`, `test/integration/`, `plugins/` as appropriate for the repo's `test/meta` directory.)

- [ ] **Step 2: Run + Commit**

`task test:int -- ./test/meta/`
Commit message: `test(meta): I-PRIV-* invariant coverage assertion`.

---

## Final integration

### Task 24: Run full pr-prep + push

- [ ] **Step 1: Run full pr-prep**

`task pr-prep`
Expected: green (lint + format + schema + license + unit + integration + E2E).

- [ ] **Step 2: Code-reviewer + abac-reviewer adversarial pass**

Invoke `code-reviewer` and `abac-reviewer` per CLAUDE.md pre-push gates. ABAC reviewer fires because `internal/access/policy/seed.go` was modified.

- [ ] **Step 3: Push branch + open PR**

Per `references/vcs-preamble.md`. Targeted rebase: `jj rebase -r <change> -d main@origin`; set bookmark; push.

- [ ] **Step 4: bd close the relevant beads on merge**

After merge, close: `holomush-iwzt` (design), the four ADR beads remain open (they're decision records, not work items).

---

## Spec coverage check

| Spec section / invariant | Plan task |
|---|---|
| §2 principle + multi-session example | Task 16 (integration) |
| §3 scope grid | Tasks 6, 7, 17 |
| §4.1 SessionInfo.LocationArrivedAt | Tasks 1, 2 |
| §4.2 FocusMembership.JoinedAt | Task 6 (read), Task 17 (test) |
| §4.3 GuestCharacterCreatedAt | Tasks 1, 2, 5, 20 |
| §5 lifecycle rules (7 transitions) | Tasks 3, 4 |
| §5.1 character-move sync hook | Task 4 |
| §6.1 QueryStreamHistory restructure | Task 7 |
| §6.2 Subscribe replay floor (NATS SoT + filter) | Tasks 11, 12, 13, 14 |
| §6.3 error opacity | Task 10 |
| §7 phasing | Tasks 1-22 |
| §8 tests | Tasks 8, 9, 10, 15, 16, 17, 19 (Step 5), 20, 21 (Step 1), 22, 23 |
| I-PRIV-1 | Tasks 8, 9 |
| I-PRIV-2 | Tasks 17, 20 |
| I-PRIV-3 | Tasks 3, 15 |
| I-PRIV-4 | Task 16 |
| I-PRIV-5 | Task 10 |
| I-PRIV-6 | Task 19 (Step 5) |
| I-PRIV-7 | Tasks 21, 22 |
| I-PRIV-8 | Task 11 |

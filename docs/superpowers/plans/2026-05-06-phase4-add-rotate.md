# Phase 4: Add + Rotate Lifecycle Ops — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `dek.Manager.Add` (append participant, no key rotation) and `dek.Manager.Rotate` (mint new DEK version, mark old rotated) with synchronous N-of-N cache invalidation.

**Architecture:** Two new types (`Invalidator` func, `BindingResolver` interface) are injected into `dek.NewManager`. `Add` does a pre-select → idempotent JSONB append → invalidation publish. `Rotate` does INSERT new row → cache seed → invalidation → mark old rotated; on invalidation failure, rolls back by evicting caches + marking the new row destroyed. Store gains 4 methods: `updateParticipants`, `markRotated`, `markDestroyed`, `selectByBindingID`.

**Tech Stack:** Go 1.26, PostgreSQL JSONB, NATS request-reply (via `invalidation.Coordinator`), testify, pgx

**Spec reference:** [docs/superpowers/specs/2026-05-06-phase4-add-rotate-design.md](../specs/2026-05-06-phase4-add-rotate-design.md)

**Grounding spec:** [docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md](../specs/2026-04-25-event-payload-crypto-design.md)

---

## Files touched

| File | Responsibility |
|---|---|
| `internal/eventbus/crypto/dek/manager.go` | `Invalidator` + `BindingResolver` types, updated `NewManager`, `manager` struct, `Add`/`Rotate` impl |
| `internal/eventbus/crypto/dek/store.go` | `updateParticipants`, `markRotated`, `markDestroyed`, `selectByBindingID` |
| `internal/eventbus/crypto/dek/store_add_rotate_test.go` | Tests for new Store methods |
| `internal/eventbus/crypto/dek/store_integrity_test.go` | INV-37 startup integrity test |
| `internal/eventbus/crypto/dek/manager_test.go` | Nil-guard tests for new params |
| `internal/eventbus/crypto/dek/manager_integration_test.go` | Call-site updates, Add/Rotate unit + integration tests, INV tests |
| `internal/eventbus/crypto/dek/store_integration_test.go` | Call-site updates (2 calls) |
| `test/integration/crypto/e2e_test.go` | Call-site update |
| `test/integration/crypto/emit_test.go` | Call-site update |
| `test/integration/crypto/metadata_only_test.go` | Call-site update |
| `test/integration/crypto/plugin_decrypt_test.go` | Call-site update |

---

## Decomposition & sequencing

```
T1: Store methods (no Manager dependency, can land first)
├── T2: Invalidator + BindingResolver types + NewManager sig change + call-site migration
├── T3: Manager.Add implementation + unit tests
├── T4: Manager.Rotate implementation + unit tests
├── T5: INV-12 + INV-13 + INV-29 integration tests
└── T6: INV-37 — startup integrity check for crashed Rotate recovery
```

---

### Task 1: Store methods — updateParticipants, markRotated, markDestroyed, selectByBindingID

**Files:**
- Create: `internal/eventbus/crypto/dek/store_add_rotate_test.go`
- Modify: `internal/eventbus/crypto/dek/store.go`

- [ ] **Step 1: Write failing tests for Store methods**

Create `internal/eventbus/crypto/dek/store_add_rotate_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
    "context"
    "encoding/json"
    "testing"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/codec"
    "github.com/holomush/holomush/internal/store"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// testPool creates a testcontainer-backed *pgxpool.Pool with migrations
// applied. Teardown is registered on t.Cleanup.
func testPool(t *testing.T) *pgxpool.Pool {
    t.Helper()
    ctx := context.Background()
    pgContainer, err := postgres.Run(ctx,
        "postgres:18-alpine",
        postgres.WithDatabase("test"),
        postgres.WithUsername("test"),
        postgres.WithPassword("test"),
        postgres.BasicWaitStrategies(),
    )
    require.NoError(t, err)
    t.Cleanup(func() { _ = pgContainer.Terminate(ctx) })
    connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
    require.NoError(t, err)
    migrator, err := store.NewMigrator(connStr)
    require.NoError(t, err)
    require.NoError(t, migrator.Up())
    migrator.Close()
    pool, err := pgxpool.New(ctx, connStr)
    require.NoError(t, err)
    t.Cleanup(pool.Close)
    return pool
}

// storeAddRotateTestEnv holds the test dependencies.
type storeAddRotateTestEnv struct {
    pool  *pgxpool.Pool
    store *Store
}

// setupStoreAddRotateTest creates the store and a test DEK row.
func setupStoreAddRotateTest(t *testing.T) (*storeAddRotateTestEnv, ContextID) {
    t.Helper()
    pool := testPool(t)
    store := NewStore(pool)
    ctxID := ContextID{Type: "scene", ID: "test-scene-1"}

    // Insert a DEK row so updateParticipants/markRotated have a target.
    initial := []Participant{
        {PlayerID: "p1", CharacterID: "c1", BindingID: "b1", JoinedAt: time.Now().UTC()},
    }
    row := row{
        ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1,
        WrappedDEK:   []byte("fake-dek-bytes-for-testing-only"),
        WrapProvider: "test", WrapKeyID: "k1",
        Participants: initial,
    }
    _, err := store.insert(context.Background(), row)
    require.NoError(t, err)

    return &storeAddRotateTestEnv{pool: pool, store: store}, ctxID
}

// TestStore_UpdateParticipants_AppendsNewParticipant verifies updateParticipants
// adds a new participant to the JSONB array.
func TestStore_UpdateParticipants_AppendsNewParticipant(t *testing.T) {
    env, ctxID := setupStoreAddRotateTest(t)

    p := Participant{
        PlayerID: "p2", CharacterID: "c2", BindingID: "b2",
        JoinedAt: time.Now().UTC(), AddedVia: "test",
    }
    result, err := env.store.updateParticipants(context.Background(), ctxID, p)
    require.NoError(t, err)

    assert.Equal(t, uint32(1), result.Version)
    require.Len(t, result.Participants, 2)
    assert.Equal(t, "p2", result.Participants[1].PlayerID)
    assert.Equal(t, "b2", result.Participants[1].BindingID)
}

// TestStore_UpdateParticipants_NoopOnDuplicate verifies idempotency:
// adding the same (player_id, binding_id) returns the active row unchanged.
func TestStore_UpdateParticipants_NoopOnDuplicate(t *testing.T) {
    env, ctxID := setupStoreAddRotateTest(t)

    p := Participant{
        PlayerID: "p1", CharacterID: "c1", BindingID: "b1",
        JoinedAt: time.Now().UTC(), AddedVia: "test",
    }
    result, err := env.store.updateParticipants(context.Background(), ctxID, p)
    require.NoError(t, err)

    // Should return the active row with exactly 1 participant (no duplication).
    assert.Equal(t, uint32(1), result.Version)
    require.Len(t, result.Participants, 1)
}

// TestStore_UpdateParticipants_NoActiveDEK verifies DEK_NOT_FOUND when
// no active DEK exists for the context.
func TestStore_UpdateParticipants_NoActiveDEK(t *testing.T) {
    pool := testPool(t)
    store := NewStore(pool)
    ctxID := ContextID{Type: "scene", ID: "nonexistent"}

    _, err := store.updateParticipants(context.Background(), ctxID, Participant{
        PlayerID: "p1", CharacterID: "c1", BindingID: "b1",
        JoinedAt: time.Now().UTC(),
    })
    require.Error(t, err)
    assert.ErrorIs(t, err, pgx.ErrNoRows)
}

// TestStore_MarkRotated_SetsRotatedAtAndSupersededBy verifies markRotated
// sets both columns on the target row.
func TestStore_MarkRotated_SetsRotatedAtAndSupersededBy(t *testing.T) {
    env, ctxID := setupStoreAddRotateTest(t)

    // First insert a second row as the successor.
    newRow := row{
        ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 2,
        WrappedDEK:   []byte("fake-dek-v2"),
        WrapProvider: "test", WrapKeyID: "k1",
        Participants: []Participant{
            {PlayerID: "p1", CharacterID: "c1", BindingID: "b1", JoinedAt: time.Now().UTC()},
        },
    }
    newID, err := env.store.insert(context.Background(), newRow)
    require.NoError(t, err)

    // Mark v1 as rotated.
    err = env.store.markRotated(context.Background(), 1, 1, newID)
    require.NoError(t, err)

    // Verify the row is now rotated.
    _, err = env.store.selectActive(context.Background(), ctxID)
    require.NoError(t, err) // should find v2 as active

    // selectByID on v1 should show rotated_at set.
    r, err := env.store.selectByID(context.Background(), codec.KeyID(1), 1)
    require.NoError(t, err)
    assert.NotNil(t, r.RotatedAt)
}

// TestStore_MarkDestroyed_SetsDestroyedAt verifies the rollback helper.
func TestStore_MarkDestroyed_SetsDestroyedAt(t *testing.T) {
    env, ctxID := setupStoreAddRotateTest(t)

    // Insert a second row to destroy.
    newRow := row{
        ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 2,
        WrappedDEK:   []byte("fake-dek-v2"),
        WrapProvider: "test", WrapKeyID: "k1",
        Participants: []Participant{
            {PlayerID: "p1", CharacterID: "c1", BindingID: "b1", JoinedAt: time.Now().UTC()},
        },
    }
    newID, err := env.store.insert(context.Background(), newRow)
    require.NoError(t, err)

    err = env.store.markDestroyed(context.Background(), codec.KeyID(newID), 2)
    require.NoError(t, err)

    // selectByID should now fail with pgx.ErrNoRows (destroyed rows filtered).
    _, err = env.store.selectByID(context.Background(), codec.KeyID(newID), 2)
    require.Error(t, err)
}

// TestStore_SelectByBindingID_FindsMatchingRows verifies the JSONB @>
// containment query finds DEKs that include the target binding.
func TestStore_SelectByBindingID_FindsMatchingRows(t *testing.T) {
    env, _ := setupStoreAddRotateTest(t)

    rows, err := env.store.selectByBindingID(context.Background(), "b1")
    require.NoError(t, err)
    require.Len(t, rows, 1)
    assert.Equal(t, "scene", rows[0].ContextType)

    // b2 should not exist.
    rows, err = env.store.selectByBindingID(context.Background(), "b2")
    require.NoError(t, err)
    assert.Len(t, rows, 0)
}
```

Run: `task test:int`
Expected: FAIL — methods not defined

- [ ] **Step 2: Implement updateParticipants**

In `internal/eventbus/crypto/dek/store.go`, append after `insert`:

```go
// updateParticipants appends p to the active DEK's participant set.
// Idempotent on (player_id, binding_id) — duplicate returns the active
// row unchanged. Returns the active row wrapped in a pgx.ErrNoRows
// sentinel when no active DEK exists.
func (s *Store) updateParticipants(ctx context.Context, ctxID ContextID, p Participant) (row, error) {
    active, err := s.selectActive(ctx, ctxID)
    if err != nil {
        return row{}, err // pgx.ErrNoRows → DEK_NOT_FOUND for caller
    }

    participantJSON, err := json.Marshal(p)
    if err != nil {
        return row{}, oops.Code("DEK_PARTICIPANTS_MARSHAL_FAILED").Wrap(err)
    }

    tag, err := s.pool.Exec(ctx, `
        UPDATE crypto_keys
           SET participants = participants || $3::jsonb
         WHERE context_type = $1 AND context_id = $2
           AND rotated_at IS NULL AND destroyed_at IS NULL
           AND NOT EXISTS (
             SELECT 1 FROM jsonb_array_elements(participants) AS part
             WHERE part->>'player_id' = $4 AND part->>'binding_id' = $5
           )`,
        ctxID.Type, ctxID.ID, participantJSON, p.PlayerID, p.BindingID,
    )
    if err != nil {
        return row{}, oops.Code("DEK_PARTICIPANTS_UPDATE_FAILED").
            With("context_type", ctxID.Type).
            With("context_id", ctxID.ID).Wrap(err)
    }
    if tag.RowsAffected() == 0 {
        return active, nil // duplicate — idempotent no-op
    }

    return s.selectActive(ctx, ctxID)
}
```

- [ ] **Step 3: Implement markRotated**

Append after `updateParticipants` in `internal/eventbus/crypto/dek/store.go`:

```go
// markRotated sets rotated_at and superseded_by on the target row.
func (s *Store) markRotated(ctx context.Context, keyID codec.KeyID, version uint32, supersededBy int64) error {
    tag, err := s.pool.Exec(ctx, `
        UPDATE crypto_keys
           SET rotated_at = NOW(), superseded_by = $3
         WHERE id = $1 AND version = $2 AND rotated_at IS NULL AND destroyed_at IS NULL`,
        int64(keyID), version, supersededBy,
    )
    if err != nil {
        return oops.Code("DEK_MARK_ROTATED_FAILED").
            With("key_id", uint64(keyID)).
            With("version", version).Wrap(err)
    }
    if tag.RowsAffected() == 0 {
        return oops.Code("DEK_MARK_ROTATED_NOT_FOUND").
            With("key_id", uint64(keyID)).
            With("version", version).
            Errorf("no unrotated row for key %d v%d", keyID, version)
    }
    return nil
}
```

- [ ] **Step 4: Implement markDestroyed**

Append after `markRotated` in `internal/eventbus/crypto/dek/store.go`:

```go
// markDestroyed sets destroyed_at on the target row. Best-effort
// rollback helper — used when Rotate's invalidation fails.
func (s *Store) markDestroyed(ctx context.Context, keyID codec.KeyID, version uint32) error {
    _, err := s.pool.Exec(ctx, `
        UPDATE crypto_keys
           SET destroyed_at = NOW()
         WHERE id = $1 AND version = $2 AND destroyed_at IS NULL`,
        int64(keyID), version,
    )
    if err != nil {
        return oops.Code("DEK_MARK_DESTROYED_FAILED").
            With("key_id", uint64(keyID)).
            With("version", version).Wrap(err)
    }
    return nil
}
```

- [ ] **Step 5: Implement selectByBindingID**

Append after `markDestroyed` in `internal/eventbus/crypto/dek/store.go`:

```go
// selectByBindingID returns all active DEK rows whose participants
// array contains an element with the given binding_id. Used by the
// wizard-transfer rebind handler to find affected DEKs.
func (s *Store) selectByBindingID(ctx context.Context, bindingID string) ([]row, error) {
    probe := []Participant{{BindingID: bindingID}}
    probeJSON, err := json.Marshal(probe)
    if err != nil {
        return nil, oops.Code("DEK_BINDING_PROBE_MARSHAL_FAILED").Wrap(err)
    }

    rows, err := s.pool.Query(ctx, `
        SELECT id, context_type, context_id, version, wrapped_dek,
               wrap_provider, wrap_key_id, participants, created_at, rotated_at
          FROM crypto_keys
         WHERE participants @> $1::jsonb
           AND rotated_at IS NULL AND destroyed_at IS NULL
         ORDER BY id`,
        probeJSON,
    )
    if err != nil {
        return nil, oops.Code("DEK_SELECT_BY_BINDING_FAILED").Wrap(err)
    }
    defer rows.Close()

    var out []row
    for rows.Next() {
        var r row
        var participantsJSON []byte
        if err := rows.Scan(
            &r.ID, &r.ContextType, &r.ContextID, &r.Version, &r.WrappedDEK,
            &r.WrapProvider, &r.WrapKeyID, &participantsJSON, &r.CreatedAt, &r.RotatedAt,
        ); err != nil {
            return nil, oops.Code("DEK_SELECT_BY_BINDING_SCAN_FAILED").Wrap(err)
        }
        if err := json.Unmarshal(participantsJSON, &r.Participants); err != nil {
            return nil, oops.Code("DEK_PARTICIPANTS_UNMARSHAL_FAILED").Wrap(err)
        }
        out = append(out, r)
    }
    return out, rows.Err()
}
```

- [ ] **Step 6: Run tests**

Run: `task test:int`
Expected: PASS

- [ ] **Step 7: Commit**

---

### Task 2: Invalidator + BindingResolver types, NewManager signature change, call-site migration

**Files:**
- Modify: `internal/eventbus/crypto/dek/manager.go`
- Modify: `internal/eventbus/crypto/dek/manager_test.go`
- Modify: `internal/eventbus/crypto/dek/manager_integration_test.go`
- Modify: `internal/eventbus/crypto/dek/store_integration_test.go`
- Modify: `test/integration/crypto/e2e_test.go`
- Modify: `test/integration/crypto/emit_test.go`
- Modify: `test/integration/crypto/metadata_only_test.go`
- Modify: `test/integration/crypto/plugin_decrypt_test.go`

- [ ] **Step 1: Add Invalidator and BindingResolver types**

In `internal/eventbus/crypto/dek/manager.go`, add before `NewManager`:

```go
// Invalidator publishes a cache-invalidation request to all replicas.
// action is one of "rotate", "participants_changed", or "rekey".
type Invalidator func(ctx context.Context, ctxID ContextID, action string, version, successorVersion uint32) error

// BindingResolver resolves a character's current binding_id.
type BindingResolver interface {
    Current(ctx context.Context, characterID string) (string, error)
}
```

- [ ] **Step 2: Update manager struct and NewManager signature**

In `internal/eventbus/crypto/dek/manager.go`, update the `manager` struct to add `invalidate` and `bindings` fields:

```go
type manager struct {
    provider   kek.Provider
    store      *Store
    cache      *Cache
    partCache  *ParticipantsCache
    invalidate Invalidator
    bindings   BindingResolver
}
```

Update `NewManager` to take the two new params:

```go
func NewManager(
    provider kek.Provider,
    store *Store,
    cache *Cache,
    partCache *ParticipantsCache,
    invalidate Invalidator,
    bindings BindingResolver,
) (Manager, error) {
    switch {
    case provider == nil:
        return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "provider").
            Errorf("dek.NewManager requires a non-nil kek.Provider")
    case store == nil:
        return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "store").
            Errorf("dek.NewManager requires a non-nil *Store")
    case cache == nil:
        return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "cache").
            Errorf("dek.NewManager requires a non-nil *Cache")
    case partCache == nil:
        return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "partCache").
            Errorf("dek.NewManager requires a non-nil *ParticipantsCache")
    case invalidate == nil:
        return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "invalidate").
            Errorf("dek.NewManager requires a non-nil Invalidator")
    case bindings == nil:
        return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "bindings").
            Errorf("dek.NewManager requires a non-nil BindingResolver")
    }
    return &manager{
        provider: provider, store: store, cache: cache,
        partCache: partCache, invalidate: invalidate, bindings: bindings,
    }, nil
}
```

- [ ] **Step 3: Update NewManagerForUnitTest**

`NewManagerForUnitTest` returns a manager with nil fields — update `configured()` accordingly (no change needed, it already checks the 4 original fields; Add/Rotate have their own nil guards).

- [ ] **Step 4: Write tests for new nil guards**

In `internal/eventbus/crypto/dek/manager_test.go`, append:

```go
// TestNewManager_RejectsNilInvalidator verifies the nil guard on the
// new Invalidator parameter.
func TestNewManager_RejectsNilInvalidator(t *testing.T) {
    _, err := dek.NewManager(
        kek.NewNoneProviderForUnitTest(),
        &dek.Store{},
        dek.NewCache(dek.CacheConfig{}),
        dek.NewParticipantsCache(dek.CacheConfig{}),
        nil,
        &stubBindingResolver{},
    )
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "DEK_MANAGER_DEPENDENCY_NIL")
}

// TestNewManager_RejectsNilBindingResolver verifies the nil guard on the
// new BindingResolver parameter.
func TestNewManager_RejectsNilBindingResolver(t *testing.T) {
    _, err := dek.NewManager(
        kek.NewNoneProviderForUnitTest(),
        &dek.Store{},
        dek.NewCache(dek.CacheConfig{}),
        dek.NewParticipantsCache(dek.CacheConfig{}),
        func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
        nil,
    )
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "DEK_MANAGER_DEPENDENCY_NIL")
}
```

Add the stub helper at the top of `manager_test.go`:

```go
// stubBindingResolver implements dek.BindingResolver for tests.
type stubBindingResolver struct {
    bindingID string
    err       error
}

func (s *stubBindingResolver) Current(_ context.Context, _ string) (string, error) {
    return s.bindingID, s.err
}
```

- [ ] **Step 5: Update all existing test call sites**

Every call site that passes 4 args must now pass `nil, nil` for the two new params. Update the following files:

`internal/eventbus/crypto/dek/manager_test.go` (4 call sites at lines 22, 30, 38, 48):
```go
// Line 22: add nil, nil
_, err := dek.NewManager(nil, &dek.Store{}, dek.NewCache(dek.CacheConfig{}), dek.NewParticipantsCache(dek.CacheConfig{}), nil, nil)
// Line 30:
_, err := dek.NewManager(kek.NewNoneProviderForUnitTest(), nil, dek.NewCache(dek.CacheConfig{}), dek.NewParticipantsCache(dek.CacheConfig{}), nil, nil)
// Line 38:
_, err := dek.NewManager(kek.NewNoneProviderForUnitTest(), &dek.Store{}, nil, dek.NewParticipantsCache(dek.CacheConfig{}), nil, nil)
// Line 48:
_, err := dek.NewManager(kek.NewNoneProviderForUnitTest(), &dek.Store{}, dek.NewCache(dek.CacheConfig{}), nil, nil, nil)
```

`internal/eventbus/crypto/dek/store_integration_test.go` (2 call sites at lines 46, 103):
```go
mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache, nil, nil)
mgr, err := dek.NewManager(provider, store, cache, partCache, nil, nil)
```

`internal/eventbus/crypto/dek/manager_integration_test.go` (8 call sites at lines 126, 161, 185, 206, 235, 282, 284, 344):
```go
// Each adds nil, nil as the last two args.
mgr, err := dek.NewManager(provider, dek.NewStore(pool), cache, partCache, nil, nil)
// etc. for all 8 call sites
```

`test/integration/crypto/e2e_test.go` (line 106):
```go
dekMgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache, nil, nil)
```

`test/integration/crypto/emit_test.go` (line 84):
```go
dekMgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache, nil, nil)
```

`test/integration/crypto/metadata_only_test.go` (line 78):
```go
dekMgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache, nil, nil)
```

`test/integration/crypto/plugin_decrypt_test.go` (line 173):
```go
dekMgr, err := dek.NewManager(provider, dekStore, dekCache, dekPartCache, nil, nil)
```

- [ ] **Step 6: Run tests to verify compilation**

Run: `task test -- ./internal/eventbus/crypto/dek/`
Expected: PASS (compiles and passes)

Run: `task test:int`
Expected: PASS (integration tests compile and pass with nil, nil)

- [ ] **Step 7: Commit**

---

### Task 3: Manager.Add implementation

**Files:**
- Modify: `internal/eventbus/crypto/dek/manager.go`
- Modify: `internal/eventbus/crypto/dek/manager_integration_test.go`

- [ ] **Step 1: Add testIntegrationPool helper**

In `internal/eventbus/crypto/dek/manager_integration_test.go`, add before the first test function:

```go
// testIntegrationPool creates a testcontainer-backed *pgxpool.Pool with
// migrations applied. Used by Add, Rotate, and INV integration tests.
func testIntegrationPool(t *testing.T) *pgxpool.Pool {
    t.Helper()
    connStr, cleanup := newTestPGPool(t)
    t.Cleanup(cleanup)
    pool, err := pgxpool.New(context.Background(), connStr)
    require.NoError(t, err)
    t.Cleanup(pool.Close)
    return pool
}
```

- [ ] **Step 2: Write failing tests for Manager.Add**

Append to `internal/eventbus/crypto/dek/manager_integration_test.go`:

```go
// stubInvalidator records invalidation calls for assertions.
type stubInvalidator struct {
    calls []invalidationCall
}

type invalidationCall struct {
    ctxID            dek.ContextID
    action           string
    version          uint32
    successorVersion uint32
}

func (s *stubInvalidator) call() dek.Invalidator {
    return func(_ context.Context, ctxID dek.ContextID, action string, v, sv uint32) error {
        s.calls = append(s.calls, invalidationCall{
            ctxID: ctxID, action: action, version: v, successorVersion: sv,
        })
        return nil
    }
}

// TestManager_Add_AppendsParticipantAndPublishesInvalidation is INV-12.
func TestManager_Add_AppendsParticipantAndPublishesInvalidation(t *testing.T) {
    pool := testIntegrationPool(t)
    store := dek.NewStore(pool)
    cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

    ctxID := dek.ContextID{Type: "scene", ID: "add-test"}

    // Create a DEK via GetOrCreate first.
    mgr, err := dek.NewManager(
        newTestProvider(t), store, cache, partCache,
        nil, // invalidator — nil here during setup
        &stubBindingResolver{bindingID: "bind-1"},
    )
    require.NoError(t, err)

    initial := []dek.Participant{
        {PlayerID: "p1", CharacterID: "c1", BindingID: "bind-1", JoinedAt: time.Now().UTC()},
    }
    _, err = mgr.GetOrCreate(context.Background(), ctxID, initial)
    require.NoError(t, err)

    // Now build a real stub invalidator and inject it into a new manager.
    invStub := &stubInvalidator{}
    mgr, err = dek.NewManager(
        newTestProvider(t), store, cache, partCache,
        invStub.call(),
        &stubBindingResolver{bindingID: "bind-2"},
    )
    require.NoError(t, err)

    err = mgr.Add(context.Background(), ctxID, dek.Participant{
        PlayerID: "p2", CharacterID: "c2", JoinedAt: time.Now().UTC(),
    })
    require.NoError(t, err)

    // Verify invalidation was published.
    require.Len(t, invStub.calls, 1)
    assert.Equal(t, "participants_changed", invStub.calls[0].action)
    assert.Equal(t, uint32(1), invStub.calls[0].version)
    assert.Equal(t, uint32(0), invStub.calls[0].successorVersion)

    // Verify the participant set was updated.
    parts, err := mgr.Participants(context.Background(), codec.KeyID(1), 1)
    require.NoError(t, err)
    require.Len(t, parts, 2)
    assert.Equal(t, "p2", parts[1].PlayerID)
    assert.Equal(t, "bind-2", parts[1].BindingID)
}

// TestManager_Add_IdempotentOnBindingID verifies second Add is a no-op.
func TestManager_Add_IdempotentOnBindingID(t *testing.T) {
    pool := testIntegrationPool(t)
    store := dek.NewStore(pool)
    cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

    ctxID := dek.ContextID{Type: "scene", ID: "idempotent-test"}

    mgr, err := dek.NewManager(
        newTestProvider(t), store, cache, partCache,
        nil,
        &stubBindingResolver{bindingID: "bind-1"},
    )
    require.NoError(t, err)

    initial := []dek.Participant{
        {PlayerID: "p1", CharacterID: "c1", BindingID: "bind-1", JoinedAt: time.Now().UTC()},
    }
    _, err = mgr.GetOrCreate(context.Background(), ctxID, initial)
    require.NoError(t, err)

    invStub := &stubInvalidator{}
    mgr, err = dek.NewManager(
        newTestProvider(t), store, cache, partCache,
        invStub.call(),
        &stubBindingResolver{bindingID: "bind-1"},
    )
    require.NoError(t, err)

    // First Add should succeed.
    err = mgr.Add(context.Background(), ctxID, dek.Participant{
        PlayerID: "p1", CharacterID: "c1",
    })
    require.NoError(t, err)
    require.Len(t, invStub.calls, 1)

    // Second Add with same (player_id, binding_id) should be no-op.
    err = mgr.Add(context.Background(), ctxID, dek.Participant{
        PlayerID: "p1", CharacterID: "c1",
    })
    require.NoError(t, err)
    // Only one invalidation call total — second was a no-op.
    require.Len(t, invStub.calls, 1)

    // Participants should still have exactly 1 entry.
    parts, err := mgr.Participants(context.Background(), codec.KeyID(1), 1)
    require.NoError(t, err)
    require.Len(t, parts, 1)
}

// TestManager_Add_BindingMissingFails verifies BINDING_NOT_FOUND when
// no active binding exists.
func TestManager_Add_BindingMissingFails(t *testing.T) {
    pool := testIntegrationPool(t)
    store := dek.NewStore(pool)
    cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

    ctxID := dek.ContextID{Type: "scene", ID: "binding-missing-test"}

    mgr, err := dek.NewManager(
        newTestProvider(t), store, cache, partCache,
        nil,
        &stubBindingResolver{bindingID: "bind-1"},
    )
    require.NoError(t, err)

    initial := []dek.Participant{
        {PlayerID: "p1", CharacterID: "c1", BindingID: "bind-1", JoinedAt: time.Now().UTC()},
    }
    _, err = mgr.GetOrCreate(context.Background(), ctxID, initial)
    require.NoError(t, err)

    invStub := &stubInvalidator{}
    mgr, err = dek.NewManager(
        newTestProvider(t), store, cache, partCache,
        invStub.call(),
        &stubBindingResolver{err: oops.Code("BINDING_NOT_FOUND").
            Errorf("no active binding for character c3")},
    )
    require.NoError(t, err)

    err = mgr.Add(context.Background(), ctxID, dek.Participant{
        PlayerID: "p2", CharacterID: "c3",
    })
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "BINDING_NOT_FOUND")
    // No invalidation should have been published.
    assert.Len(t, invStub.calls, 0)
}
```

Run: `task test:int`
Expected: FAIL — Manager.Add is a stub returning DEK_ADD_NOT_IMPLEMENTED

- [ ] **Step 3: Implement Manager.Add**

Replace the stub body at `internal/eventbus/crypto/dek/manager.go`:

```go
// Add appends a participant to the active DEK's set without rotating.
func (m *manager) Add(ctx context.Context, ctxID ContextID, p Participant) error {
    if err := m.configured(); err != nil {
        return err
    }
    if m.invalidate == nil {
        return oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "Invalidator").
            Errorf("Add requires a non-nil Invalidator — pass invalidation.Coordinator adapter")
    }
    if m.bindings == nil {
        return oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "BindingResolver").
            Errorf("Add requires a non-nil BindingResolver")
    }

    if p.BindingID == "" {
        bindingID, err := m.bindings.Current(ctx, p.CharacterID)
        if err != nil {
            return err // propagates BINDING_NOT_FOUND from binding repo
        }
        p.BindingID = bindingID
    }

    row, err := m.store.updateParticipants(ctx, ctxID, p)
    if err != nil {
        return err
    }

    return m.invalidate(ctx, ctxID, "participants_changed", row.Version, 0)
}
```

- [ ] **Step 4: Run tests**

Run: `task test:int`
Expected: PASS

- [ ] **Step 5: Run unit test suite**

Run: `task test -- ./internal/eventbus/crypto/dek/`
Expected: PASS

- [ ] **Step 6: Commit**

---

### Task 4: Manager.Rotate implementation

**Files:**
- Modify: `internal/eventbus/crypto/dek/manager.go`
- Modify: `internal/eventbus/crypto/dek/manager_integration_test.go`

- [ ] **Step 1: Write failing tests for Manager.Rotate**

Append to `internal/eventbus/crypto/dek/manager_integration_test.go`:

```go
// TestManager_Rotate_MintsFreshDEKAndMarksOldRotated is INV-13.
func TestManager_Rotate_MintsFreshDEKAndMarksOldRotated(t *testing.T) {
    pool := testIntegrationPool(t)
    store := dek.NewStore(pool)
    cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

    ctxID := dek.ContextID{Type: "scene", ID: "rotate-test"}

    mgr, err := dek.NewManager(
        newTestProvider(t), store, cache, partCache,
        nil,
        &stubBindingResolver{bindingID: "bind-1"},
    )
    require.NoError(t, err)

    initial := []dek.Participant{
        {PlayerID: "p1", CharacterID: "c1", BindingID: "bind-1", JoinedAt: time.Now().UTC()},
    }
    _, err = mgr.GetOrCreate(context.Background(), ctxID, initial)
    require.NoError(t, err)

    invStub := &stubInvalidator{}
    mgr, err = dek.NewManager(
        newTestProvider(t), store, cache, partCache,
        invStub.call(),
        &stubBindingResolver{bindingID: "bind-1"},
    )
    require.NoError(t, err)

    newParticipants := []dek.Participant{
        {PlayerID: "p2", CharacterID: "c2", BindingID: "bind-2", JoinedAt: time.Now().UTC()},
    }
    err = mgr.Rotate(context.Background(), ctxID, newParticipants, "test departure")
    require.NoError(t, err)

    // Verify invalidation was published.
    require.Len(t, invStub.calls, 1)
    assert.Equal(t, "rotate", invStub.calls[0].action)
    assert.Equal(t, uint32(1), invStub.calls[0].version)
    assert.Equal(t, uint32(2), invStub.calls[0].successorVersion)

    // Old DEK (v1) should still be unwrappable (INV-13).
    _, err = mgr.Resolve(context.Background(), codec.KeyID(1), 1)
    require.NoError(t, err)

    // New DEK (v2) should be active.
    _, err = mgr.Resolve(context.Background(), codec.KeyID(2), 2)
    require.NoError(t, err)

    // New participants are on v2.
    parts, err := mgr.Participants(context.Background(), codec.KeyID(2), 2)
    require.NoError(t, err)
    require.Len(t, parts, 1)
    assert.Equal(t, "p2", parts[0].PlayerID)
}

// TestManager_Rotate_RollsBackOnInvalidationFailure is INV-29.
func TestManager_Rotate_RollsBackOnInvalidationFailure(t *testing.T) {
    pool := testIntegrationPool(t)
    store := dek.NewStore(pool)
    cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})

    ctxID := dek.ContextID{Type: "scene", ID: "rotate-fail-test"}

    mgr, err := dek.NewManager(
        newTestProvider(t), store, cache, partCache,
        nil,
        &stubBindingResolver{bindingID: "bind-1"},
    )
    require.NoError(t, err)

    initial := []dek.Participant{
        {PlayerID: "p1", CharacterID: "c1", BindingID: "bind-1", JoinedAt: time.Now().UTC()},
    }
    key, err := mgr.GetOrCreate(context.Background(), ctxID, initial)
    require.NoError(t, err)

    // Build a stub that fails on invalidation.
    mgr, err = dek.NewManager(
        newTestProvider(t), store, cache, partCache,
        func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error {
            return oops.Code("INVALIDATION_PARTIAL_FAILURE").Errorf("simulated failure")
        },
        &stubBindingResolver{bindingID: "bind-1"},
    )
    require.NoError(t, err)

    newParticipants := []dek.Participant{
        {PlayerID: "p2", CharacterID: "c2", BindingID: "bind-2", JoinedAt: time.Now().UTC()},
    }
    err = mgr.Rotate(context.Background(), ctxID, newParticipants, "test")
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "INVALIDATION_PARTIAL_FAILURE")

    // The original DEK should still be the active one.
    parts, err := mgr.Participants(context.Background(), key.KeyID, key.Version)
    require.NoError(t, err)
    assert.Len(t, parts, 1)
    assert.Equal(t, "p1", parts[0].PlayerID)

    // No new DEK version should exist.
    _, err = mgr.Resolve(context.Background(), codec.KeyID(uint64(key.KeyID)+1), key.Version+1)
    require.Error(t, err)
}
```

Run: `task test:int`
Expected: FAIL — Manager.Rotate is a stub returning DEK_ROTATE_NOT_IMPLEMENTED

- [ ] **Step 2: Implement Manager.Rotate**

Replace the stub body at `internal/eventbus/crypto/dek/manager.go`:

```go
// Rotate mints a new DEK version and marks the old one rotated.
func (m *manager) Rotate(ctx context.Context, ctxID ContextID,
    newParticipants []Participant, reason string) error {

    if err := m.configured(); err != nil {
        return err
    }
    if m.invalidate == nil {
        return oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "Invalidator").
            Errorf("Rotate requires a non-nil Invalidator")
    }

    activeRow, err := m.store.selectActive(ctx, ctxID)
    if err != nil {
        return err
    }

    dekBytes := make([]byte, DEKByteLength)
    if _, err := io.ReadFull(rand.Reader, dekBytes); err != nil {
        return oops.Code("DEK_RNG_FAILED").Wrap(err)
    }
    wrapped, kekKeyID, err := m.provider.Wrap(ctx, dekBytes)
    if err != nil {
        return oops.Code("DEK_WRAP_FAILED").Wrap(err)
    }
    if err := validateProviderWrapOutput(wrapped, kekKeyID); err != nil {
        return err
    }

    newRow := row{
        ContextType: ctxID.Type, ContextID: ctxID.ID,
        Version:      activeRow.Version + 1,
        WrappedDEK:   wrapped,
        WrapProvider: m.provider.Name(),
        WrapKeyID:    kekKeyID,
        Participants: newParticipants,
    }
    newID, err := m.store.insert(ctx, newRow)
    if err != nil {
        return oops.Code("DEK_STORE_INSERT_FAILED").Wrap(err)
    }

    //nolint:gosec // G115: newID is a DB BIGSERIAL value
    newKeyID := codec.KeyID(newID)
    newVersion := newRow.Version

    material := NewMaterial(dekBytes)
    m.cache.Put(CacheKey{KeyID: newKeyID, Version: newVersion}, ctxID, material)
    m.partCache.Put(ParticipantsCacheKey{
        ContextType: ctxID.Type, ContextID: ctxID.ID, Version: newVersion,
    }, newParticipants)

    //nolint:gosec // G115: activeRow.ID is a DB BIGSERIAL value
    if err := m.invalidate(ctx, ctxID, "rotate",
        activeRow.Version, newVersion); err != nil {
        // Rollback: evict caches + mark new row destroyed.
        m.cache.Invalidate(CacheKey{KeyID: newKeyID, Version: newVersion})
        m.partCache.Invalidate(ParticipantsCacheKey{
            ContextType: ctxID.Type, ContextID: ctxID.ID, Version: newVersion,
        })
        _ = m.store.markDestroyed(ctx, newKeyID, newVersion)
        return err
    }

    return m.store.markRotated(ctx,
        codec.KeyID(activeRow.ID), activeRow.Version, newID)
}
```

- [ ] **Step 3: Run tests**

Run: `task test:int`
Expected: PASS

- [ ] **Step 4: Run unit test suite**

Run: `task test -- ./internal/eventbus/crypto/dek/`
Expected: PASS

- [ ] **Step 5: Commit**

---

### Task 5: Integration tests — INV-12, INV-13, INV-29 with real PG + NATS

**Files:**
- Modify: `internal/eventbus/crypto/dek/manager_integration_test.go`

These tests use the embedded NATS `eventbustest` harness and a real PG testcontainer. They exercise the full Add/Rotate lifecycle with a real `invalidation.Coordinator`.

- [ ] **Step 1: Add the integration tests**

Append to `internal/eventbus/crypto/dek/manager_integration_test.go`:

```go
// TestIntegration_Add_GrantsImmediateReadAccess is INV-12:
// After Add(participant), the new participant can decrypt events
// that were emitted before they joined.
func TestIntegration_Add_GrantsImmediateReadAccess(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test requires NATS + PG")
    }
    pool := testIntegrationPool(t)
    store := dek.NewStore(pool)
    cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: 5 * time.Minute})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: 5 * time.Minute})
    provider := newTestProvider(t)
    ctxID := dek.ContextID{Type: "scene", ID: "inv12-" + randomSuffix()}

    embedded := eventbustest.New(t)
    coordCfg := invalidation.Config{ClusterID: "test-cluster", InvalidateTimeout: 5 * time.Second}.Defaults()
    coord, err := invalidation.New(coordCfg, invalidation.Deps{
        Conn: embedded.Conn, Registry: &singleMemberRegistry{},
        DEKCache: cache, PartCache: partCache, Logger: slog.Default(),
    })
    require.NoError(t, err)
    require.NoError(t, coord.Start(context.Background()))
    defer func() { _ = coord.Stop(context.Background()) }()

    invalidator := func(ctx context.Context, cid dek.ContextID, action string, v, sv uint32) error {
        return coord.RequestInvalidation(ctx, cid, invalidation.Action(action), v, sv)
    }

    mgr, err := dek.NewManager(provider, store, cache, partCache,
        invalidator,
        &stubBindingResolver{bindingID: "bind-carol"},
    )
    require.NoError(t, err)

    // Create the DEK with initial participants (Alice only).
    initial := []dek.Participant{
        {PlayerID: "alice", CharacterID: "c-alice", BindingID: "bind-alice", JoinedAt: time.Now().UTC()},
    }
    key, err := mgr.GetOrCreate(context.Background(), ctxID, initial)
    require.NoError(t, err)

    // Add Carol after DEK creation.
    err = mgr.Add(context.Background(), ctxID, dek.Participant{
        PlayerID: "carol", CharacterID: "c-carol", BindingID: "bind-carol", JoinedAt: time.Now().UTC(),
    })
    require.NoError(t, err)

    // Carol should be in the participant set now.
    parts, err := mgr.Participants(context.Background(), key.KeyID, key.Version)
    require.NoError(t, err)
    require.Len(t, parts, 2)
    assert.Equal(t, "carol", parts[1].PlayerID)

    // Verify the DEK is still v1 (no rotation).
    assert.Equal(t, uint32(1), key.Version)
}

// TestIntegration_Rotate_PreservesOldDEK is INV-13:
// After Rotate, old events encrypted under the old DEK are still
// decryptable.
func TestIntegration_Rotate_PreservesOldDEK(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test requires NATS + PG")
    }
    pool := testIntegrationPool(t)
    store := dek.NewStore(pool)
    cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: 5 * time.Minute})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: 5 * time.Minute})
    provider := newTestProvider(t)
    ctxID := dek.ContextID{Type: "scene", ID: "inv13-" + randomSuffix()}

    embedded := eventbustest.New(t)
    coordCfg := invalidation.Config{ClusterID: "test-cluster"}.Defaults()
    coord, err := invalidation.New(coordCfg, invalidation.Deps{
        Conn: embedded.Conn, Registry: &singleMemberRegistry{},
        DEKCache: cache, PartCache: partCache, Logger: slog.Default(),
    })
    require.NoError(t, err)
    require.NoError(t, coord.Start(context.Background()))
    defer func() { _ = coord.Stop(context.Background()) }()

    invalidator := func(ctx context.Context, cid dek.ContextID, action string, v, sv uint32) error {
        return coord.RequestInvalidation(ctx, cid, invalidation.Action(action), v, sv)
    }

    mgr, err := dek.NewManager(provider, store, cache, partCache,
        invalidator,
        &stubBindingResolver{bindingID: "bind-alice"},
    )
    require.NoError(t, err)

    initial := []dek.Participant{
        {PlayerID: "alice", CharacterID: "c-alice", BindingID: "bind-alice", JoinedAt: time.Now().UTC()},
    }
    key, err := mgr.GetOrCreate(context.Background(), ctxID, initial)
    require.NoError(t, err)

    // Rotate to new participants.
    err = mgr.Rotate(context.Background(), ctxID, []dek.Participant{
        {PlayerID: "bob", CharacterID: "c-bob", BindingID: "bind-bob", JoinedAt: time.Now().UTC()},
    }, "alice left")
    require.NoError(t, err)

    // Old DEK (v1) should still be resolvable.
    oldKey, err := mgr.Resolve(context.Background(), key.KeyID, key.Version)
    require.NoError(t, err)
    assert.Equal(t, key.KeyID, oldKey.KeyID)
    assert.Equal(t, uint32(1), oldKey.Version)

    // New DEK (v2) should also be resolvable.
    _, err = mgr.Resolve(context.Background(),
        codec.KeyID(uint64(key.KeyID)+1), key.Version+1)
    require.NoError(t, err)
}

// TestIntegration_Rotate_DeletesNewRowOnInvalidationFailure is INV-29:
// When invalidation fails, Rotate rolls back: the new row is destroyed
// and the old DEK remains the sole active row.
func TestIntegration_Rotate_DeletesNewRowOnInvalidationFailure(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test requires NATS + PG")
    }
    pool := testIntegrationPool(t)
    store := dek.NewStore(pool)
    cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: 5 * time.Minute})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: 5 * time.Minute})
    provider := newTestProvider(t)
    ctxID := dek.ContextID{Type: "scene", ID: "inv29-" + randomSuffix()}

    mgr, err := dek.NewManager(provider, store, cache, partCache,
        nil,
        &stubBindingResolver{bindingID: "bind-alice"},
    )
    require.NoError(t, err)

    initial := []dek.Participant{
        {PlayerID: "alice", CharacterID: "c-alice", BindingID: "bind-alice", JoinedAt: time.Now().UTC()},
    }
    key, err := mgr.GetOrCreate(context.Background(), ctxID, initial)
    require.NoError(t, err)

    // Build a manager whose invalidator always fails.
    mgr, err = dek.NewManager(provider, store, cache, partCache,
        func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error {
            return oops.Code("INVALIDATION_PARTIAL_FAILURE").
                Errorf("simulated NATS failure")
        },
        &stubBindingResolver{bindingID: "bind-alice"},
    )
    require.NoError(t, err)

    err = mgr.Rotate(context.Background(), ctxID, []dek.Participant{
        {PlayerID: "bob", CharacterID: "c-bob", BindingID: "bind-bob", JoinedAt: time.Now().UTC()},
    }, "alice left")
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "INVALIDATION_PARTIAL_FAILURE")

    // Old DEK should still be active.
    _, err = mgr.Resolve(context.Background(), key.KeyID, key.Version)
    require.NoError(t, err)

    // The inserted-but-destroyed row should not appear.
    _, err = mgr.Resolve(context.Background(),
        codec.KeyID(uint64(key.KeyID)+1), key.Version+1)
    require.Error(t, err)
}
```

Run: `task test:int`
Expected: FAIL — types/helpers not yet defined (randomSuffix, singleMemberRegistry, etc.)

- [ ] **Step 2: Add test helpers**

Append to `internal/eventbus/crypto/dek/manager_integration_test.go`:

```go
// randomSuffix returns a short random string for unique test IDs.
func randomSuffix() string {
    b := make([]byte, 4)
    _, _ = rand.Read(b)
    return fmt.Sprintf("%x", b)
}

// singleMemberRegistry implements cluster.Registry for single-replica tests.
type singleMemberRegistry struct{}

func (s *singleMemberRegistry) ID() lifecycle.SubsystemID                { return "single-member-registry" }
func (s *singleMemberRegistry) DependsOn() []lifecycle.SubsystemID        { return nil }
func (s *singleMemberRegistry) Start(_ context.Context) error             { return nil }
func (s *singleMemberRegistry) Stop(_ context.Context) error              { return nil }
func (s *singleMemberRegistry) Self() cluster.MemberID                    { return "self-1" }
func (s *singleMemberRegistry) LiveMembers() []cluster.Member             { return []cluster.Member{{ID: "self-1"}} }
func (s *singleMemberRegistry) LiveCount() int                            { return 1 }
func (s *singleMemberRegistry) Member(_ cluster.MemberID) (cluster.Member, bool) { return cluster.Member{}, false }
func (s *singleMemberRegistry) ProbeAndPill(_ context.Context, _ cluster.MemberID, _ cluster.PillReason) error { return nil }
func (s *singleMemberRegistry) Subscribe(_ cluster.MemberObserver) (cancel func()) { return func() {} }
```

Add imports:
```go
import (
    "crypto/rand"
    "fmt"
    "log/slog"

    "github.com/holomush/holomush/internal/cluster"
    "github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
    "github.com/holomush/holomush/internal/eventbus/eventbustest"
    "github.com/holomush/holomush/internal/lifecycle"
)
```

- [ ] **Step 3: Run integration tests**

Run: `task test:int`
Expected: PASS

- [ ] **Step 4: Run full test suite**

Run: `task test -- ./internal/eventbus/crypto/dek/`
Expected: PASS

Run: `task test:int`
Expected: PASS

- [ ] **Step 5: Commit**

---

### Task 6: INV-37 — Startup integrity check for crashed Rotate recovery

**Files:**
- Modify: `internal/eventbus/crypto/dek/store.go`
- Create: `internal/eventbus/crypto/dek/store_integrity_test.go`

INV-37 requires that a crashed Rotate (two unrotated rows for the same context) be resolvable by a startup integrity check without manual intervention.

- [ ] **Step 1: Write failing integration test**

Create `internal/eventbus/crypto/dek/store_integrity_test.go` in package `dek` (internal test — accesses unexported `row` and `insert` directly, avoiding both the `Row` name collision with the existing exported `Row` type and the INV-27 `[]byte` field guard):

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
    "context"
    "testing"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// TestStore_ResolveIntegrity_ResolvesCrashedRotate is INV-37.
// Simulates a crashed Rotate by directly inserting two unrotated rows
// (bypassing Manager.Rotate via package-internal access), then verifies
// ResolveIntegrity resolves them.
func TestStore_ResolveIntegrity_ResolvesCrashedRotate(t *testing.T) {
    pool := testPool(t)
    store := NewStore(pool)

    ctxID := ContextID{Type: "scene", ID: "inv37-test"}

    // Insert v1 — unexported insert + row accessible from package dek.
    r1 := row{
        ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1,
        WrappedDEK:   []byte("fake-dek-v1"),
        WrapProvider: "test", WrapKeyID: "k1",
        Participants: []Participant{
            {PlayerID: "alice", CharacterID: "c-alice", BindingID: "bind-alice", JoinedAt: time.Now().UTC()},
        },
    }
    _, err := store.insert(context.Background(), r1)
    require.NoError(t, err)

    // Simulate crashed Rotate: insert v2 without marking v1 rotated.
    r2 := row{
        ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 2,
        WrappedDEK:   []byte("fake-dek-v2"),
        WrapProvider: "test", WrapKeyID: "k1",
        Participants: []Participant{
            {PlayerID: "bob", CharacterID: "c-bob", BindingID: "bind-bob", JoinedAt: time.Now().UTC()},
        },
    }
    _, err = store.insert(context.Background(), r2)
    require.NoError(t, err)

    // Both rows exist with rotated_at IS NULL. Verify pre-condition.
    var count int
    err = pool.QueryRow(context.Background(), `
        SELECT COUNT(*) FROM crypto_keys
         WHERE context_type = $1 AND context_id = $2
           AND rotated_at IS NULL AND destroyed_at IS NULL`,
        ctxID.Type, ctxID.ID).Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 2, count, "pre-condition: two unrotated rows simulate crashed Rotate")

    // Run the integrity check.
    err = store.ResolveIntegrity(context.Background())
    require.NoError(t, err)

    // After resolution, only v2 should remain unrotated.
    err = pool.QueryRow(context.Background(), `
        SELECT COUNT(*) FROM crypto_keys
         WHERE context_type = $1 AND context_id = $2
           AND rotated_at IS NULL AND destroyed_at IS NULL`,
        ctxID.Type, ctxID.ID).Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 1, count, "post-resolution: exactly one unrotated row")

    // v1 should be marked rotated.
    var rotatedAt *time.Time
    err = pool.QueryRow(context.Background(), `
        SELECT rotated_at FROM crypto_keys
         WHERE context_type = $1 AND context_id = $2 AND version = 1`,
        ctxID.Type, ctxID.ID).Scan(&rotatedAt)
    require.NoError(t, err)
    assert.NotNil(t, rotatedAt, "v1 should be marked rotated by integrity check")
}
```

Run: `task test:int`
Expected: FAIL — ResolveIntegrity not defined

- [ ] **Step 2: Add ResolveIntegrity to Store**

Append after `markDestroyed` in `internal/eventbus/crypto/dek/store.go`:

```go
// ResolveIntegrity finds contexts with multiple unrotated, undestroyed
// rows (crashed Rotate) and marks the earlier versions rotated, keeping
// only the max version active. Idempotent — safe to run at every startup.
func (s *Store) ResolveIntegrity(ctx context.Context) error {
    rows, err := s.pool.Query(ctx, `
        SELECT context_type, context_id
          FROM crypto_keys
         WHERE rotated_at IS NULL AND destroyed_at IS NULL
         GROUP BY context_type, context_id
        HAVING COUNT(*) > 1`)
    if err != nil {
        return oops.Code("DEK_INTEGRITY_QUERY_FAILED").Wrap(err)
    }
    defer rows.Close()

    type ctxKey struct {
        ctxType, ctxID string
    }
    var conflicted []ctxKey
    for rows.Next() {
        var ck ctxKey
        if err := rows.Scan(&ck.ctxType, &ck.ctxID); err != nil {
            return oops.Code("DEK_INTEGRITY_SCAN_FAILED").Wrap(err)
        }
        conflicted = append(conflicted, ck)
    }
    if err := rows.Err(); err != nil {
        return oops.Code("DEK_INTEGRITY_ROWS_ERR").Wrap(err)
    }

    for _, ck := range conflicted {
        // Mark all but the max-version row as rotated.
        _, err := s.pool.Exec(ctx, `
            UPDATE crypto_keys
               SET rotated_at = NOW()
             WHERE context_type = $1 AND context_id = $2
               AND rotated_at IS NULL AND destroyed_at IS NULL
               AND version < (
                   SELECT MAX(version) FROM crypto_keys
                    WHERE context_type = $1 AND context_id = $2
                      AND rotated_at IS NULL AND destroyed_at IS NULL
               )`,
            ck.ctxType, ck.ctxID,
        )
        if err != nil {
            return oops.Code("DEK_INTEGRITY_RESOLVE_FAILED").
                With("context_type", ck.ctxType).
                With("context_id", ck.ctxID).Wrap(err)
        }
    }
    return nil
}
```

- [ ] **Step 3: Run integration tests**

Run: `task test:int`
Expected: PASS

- [ ] **Step 4: Commit**

---

## Post-implementation checklist

- [ ] `task lint` green
- [ ] `task fmt` green
- [ ] `task test -- ./internal/eventbus/crypto/dek/` green
- [ ] `task test:int` green (integration tests compile and pass)
- [ ] `task pr-prep` green
- [ ] `bd close` for task beads
- [ ] PR opened, reviewed, squash-merged

## Bead chain structure

```text
holomush-fi0n                    (existing epic — Phase 4 Add + Rotate)
├── holomush-fi0n.1              (done — FileSource.Persist)
├── holomush-fi0n.2              (existing — composite index, deferred to Phase 5)
├── holomush-fi0n.3              (NEW — Store methods)
├── holomush-fi0n.4              (NEW — Invalidator/BindingResolver + NewManager sig + call-site migration)
├── holomush-fi0n.5              (NEW — Manager.Add implementation)
├── holomush-fi0n.6              (NEW — Manager.Rotate implementation)
├── holomush-fi0n.7              (NEW — INV-12/INV-13/INV-29 integration tests)
└── holomush-fi0n.8              (NEW — INV-37 startup integrity for crashed Rotate recovery)
```

### holomush-fi0n.3: Store methods

```bash
bd create \
  --title "Phase 4: Store — updateParticipants, markRotated, markDestroyed, selectByBindingID" \
  --type task \
  --priority 2 \
  --parent holomush-fi0n \
  --description "$(cat <<'EOF'
**Goal:** Add four Store methods needed by Add/Rotate: updateParticipants (idempotent JSONB append), markRotated, markDestroyed (rollback helper), selectByBindingID (JSONB @> containment query).

**Design reference:** docs/superpowers/specs/2026-05-06-phase4-add-rotate-design.md § New Store methods
**Plan reference:** docs/superpowers/plans/2026-05-06-phase4-add-rotate.md § Task 1

**TDD acceptance criteria:**
- TestStore_UpdateParticipants_AppendsNewParticipant — appends to JSONB array
- TestStore_UpdateParticipants_NoopOnDuplicate — idempotent on (player_id, binding_id)
- TestStore_UpdateParticipants_NoActiveDEK — returns pgx.ErrNoRows
- TestStore_MarkRotated_SetsRotatedAtAndSupersededBy — sets both columns
- TestStore_MarkDestroyed_SetsDestroyedAt — sets destroyed_at so row is filtered out
- TestStore_SelectByBindingID_FindsMatchingRows — JSONB @> containment works

**Verification steps:**
- task lint
- task test:int (integration-tagged Store tests)

**Files touched:**
- internal/eventbus/crypto/dek/store_add_rotate_test.go — tests for updateParticipants, markRotated, markDestroyed, selectByBindingID
- internal/eventbus/crypto/dek/store.go — add updateParticipants, markRotated, markDestroyed, selectByBindingID

**Dependencies:** none (Store is self-contained; no Manager changes needed)

**Out of scope:** Rekey-specific Store methods; composite index (fi0n.2 — deferred to Phase 5)
EOF
)"
```

### holomush-fi0n.4: Invalidator + BindingResolver types, NewManager signature change, call-site migration

```bash
bd create \
  --title "Phase 4: Invalidator/BindingResolver types + NewManager(6 params) + call-site migration" \
  --type task \
  --priority 2 \
  --parent holomush-fi0n \
  --description "$(cat <<'EOF'
**Goal:** Define Invalidator func type and BindingResolver interface in dek, expand NewManager from 4 to 6 params, update all call sites (8 files, ~15 calls).

**Design reference:** docs/superpowers/specs/2026-05-06-phase4-add-rotate-design.md § Dependency injection
**Plan reference:** docs/superpowers/plans/2026-05-06-phase4-add-rotate.md § Task 2

**TDD acceptance criteria:**
- TestNewManager_RejectsNilInvalidator — nil guard returns DEK_MANAGER_DEPENDENCY_NIL
- TestNewManager_RejectsNilBindingResolver — nil guard returns DEK_MANAGER_DEPENDENCY_NIL
- All existing tests compile and pass with nil, nil appended
- task test:int compiles and passes

**Verification steps:**
- task lint
- task test -- ./internal/eventbus/crypto/dek/
- task test:int

**Files touched:**
- internal/eventbus/crypto/dek/manager.go — add Invalidator type, BindingResolver interface; update NewManager signature and manager struct
- internal/eventbus/crypto/dek/manager_test.go — add nil-guard tests, stubBindingResolver helper, update 4 call sites
- internal/eventbus/crypto/dek/manager_integration_test.go — update 8 call sites
- internal/eventbus/crypto/dek/store_integration_test.go — update 2 call sites
- test/integration/crypto/e2e_test.go — update 1 call site
- test/integration/crypto/emit_test.go — update 1 call site
- test/integration/crypto/metadata_only_test.go — update 1 call site
- test/integration/crypto/plugin_decrypt_test.go — update 1 call site

**Dependencies:** holomush-fi0n.3 (Store methods must exist first)

**Out of scope:** Actual Add/Rotate implementation (Tasks 3/4); production wiring of real Invalidator (rebind handler — future task)
EOF
)"
```

### holomush-fi0n.5: Manager.Add

```bash
bd create \
  --title "Phase 4: Manager.Add — append participant + publish invalidation" \
  --type task \
  --priority 2 \
  --parent holomush-fi0n \
  --description "$(cat <<'EOF'
**Goal:** Implement Manager.Add: resolve binding_id → idempotent JSONB append → publish participants_changed invalidation.

**Design reference:** docs/superpowers/specs/2026-05-06-phase4-add-rotate-design.md § Manager.Add — §6.1
**Plan reference:** docs/superpowers/plans/2026-05-06-phase4-add-rotate.md § Task 3

**TDD acceptance criteria:**
- TestManager_Add_AppendsParticipantAndPublishesInvalidation — participant appended, invalidation published
- TestManager_Add_IdempotentOnBindingID — second Add with same (player_id, binding_id) is no-op
- TestManager_Add_BindingMissingFails — BINDING_NOT_FOUND when no active binding

**Verification steps:**
- task lint
- task test:int (Add integration tests)
- task test -- ./internal/eventbus/crypto/dek/

**Files touched:**
- internal/eventbus/crypto/dek/manager.go — replace Add stub with implementation
- internal/eventbus/crypto/dek/manager_integration_test.go — testIntegrationPool, stubInvalidator type, Add tests

**Dependencies:** holomush-fi0n.4 (Invalidator/BindingResolver types + NewManager sig must exist)

**Out of scope:** Integration tests with real NATS (Task 5); production call site wiring (future phases)
EOF
)"
```

### holomush-fi0n.6: Manager.Rotate

```bash
bd create \
  --title "Phase 4: Manager.Rotate — mint new DEK version + mark old rotated + invalidation" \
  --type task \
  --priority 2 \
  --parent holomush-fi0n \
  --description "$(cat <<'EOF'
**Goal:** Implement Manager.Rotate: mint fresh DEK → INSERT new row → seed caches → publish rotate invalidation → mark old rotated. On invalidation failure, roll back (evict caches + markDestroyed).

**Design reference:** docs/superpowers/specs/2026-05-06-phase4-add-rotate-design.md § Manager.Rotate — §6.2
**Plan reference:** docs/superpowers/plans/2026-05-06-phase4-add-rotate.md § Task 4

**TDD acceptance criteria:**
- TestManager_Rotate_MintsFreshDEKAndMarksOldRotated — new row inserted, old row preserved (INV-13), invalidation published (INV-29)
- TestManager_Rotate_RollsBackOnInvalidationFailure — old DEK remains sole active row, inserted row destroyed

**Verification steps:**
- task lint
- task test:int (Rotate integration tests)
- task test -- ./internal/eventbus/crypto/dek/

**Files touched:**
- internal/eventbus/crypto/dek/manager.go — replace Rotate stub with implementation
- internal/eventbus/crypto/dek/manager_integration_test.go — Rotate tests

**Dependencies:** holomush-fi0n.5 (Manager.Add — Rotate reuses Invalidator pattern)

**Out of scope:** Startup integrity check (INV-37 — deferred to production wiring task); Rekey (Phase 5); scheduled/explicit rotate triggers
EOF
)"
```

### holomush-fi0n.7: INV-12/INV-13/INV-29 integration tests

```bash
bd create \
  --title "Phase 4: Integration tests — Add+Rotate with real NATS invalidation" \
  --type task \
  --priority 2 \
  --parent holomush-fi0n \
  --description "$(cat <<'EOF'
**Goal:** Write integration tests exercising Add + Rotate with real embedded NATS + invalidation.Coordinator.

**Design reference:** docs/superpowers/specs/2026-05-06-phase4-add-rotate-design.md § Test plan
**Plan reference:** docs/superpowers/plans/2026-05-06-phase4-add-rotate.md § Task 5

**TDD acceptance criteria:**
- TestIntegration_Add_GrantsImmediateReadAccess (INV-12) — Carol added to participant set after DEK creation
- TestIntegration_Rotate_PreservesOldDEK (INV-13) — old DEK still resolvable after Rotate
- TestIntegration_Rotate_DeletesNewRowOnInvalidationFailure (INV-29) — old DEK remains sole active after invalidation failure

**Verification steps:**
- task lint
- task test:int
- task test -- ./internal/eventbus/crypto/dek/

**Files touched:**
- internal/eventbus/crypto/dek/manager_integration_test.go — 3 integration tests + helpers (randomSuffix, singleMemberRegistry)

**Dependencies:** holomush-fi0n.6 (Manager.Rotate must exist)

**Out of scope:** Full E2E with testcontainers (crypto/e2e_test.go covers that surface); multi-replica N>1 tests (cluster harness exists but orchestration is Phase 5)
EOF
)"
```

### holomush-fi0n.8: INV-37 startup integrity for crashed Rotate recovery

```bash
bd create \
  --title "Phase 4: INV-37 — startup integrity check resolves crashed Rotate" \
  --type task \
  --priority 2 \
  --parent holomush-fi0n \
  --description "$(cat <<'EOF'
**Goal:** Implement Store.ResolveIntegrity to detect and auto-resolve contexts with multiple unrotated rows (crashed Rotate), marking earlier versions rotated and keeping max-version active.

**Design reference:** docs/superpowers/specs/2026-05-06-phase4-add-rotate-design.md § Crash recovery (INV-37)
**Plan reference:** docs/superpowers/plans/2026-05-06-phase4-add-rotate.md § Task 6

**TDD acceptance criteria:**
- TestStore_ResolveIntegrity_ResolvesCrashedRotate — two unrotated rows → integrity check resolves, v1 marked rotated, v2 sole active

**Verification steps:**
- task lint
- task test:int
- task test -- ./internal/eventbus/crypto/dek/

**Files touched:**
- internal/eventbus/crypto/dek/store.go — add ResolveIntegrity method
- internal/eventbus/crypto/dek/store_integrity_test.go — INV-37 integration test (package dek, accesses unexported row/insert)

**Dependencies:** holomush-fi0n.3 (testPool helper)

**Out of scope:** Production wiring of ResolveIntegrity at server startup (separate task — call site in cmd/holomush/); multi-row crash scenarios beyond the basic two-row case
EOF
)"
```

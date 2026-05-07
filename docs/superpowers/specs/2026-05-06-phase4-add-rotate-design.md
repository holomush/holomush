# Phase 4: Add + Rotate Lifecycle Ops — Design

## Status

Design approved 2026-05-06.

Builds on: [docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md](2026-04-25-event-payload-crypto-design.md)
Phase 3 complete. `dek.Manager.Add` / `Rotate` are stubs; invalidation substrate
(`invalidation.Coordinator.RequestInvalidation`) shipped in Phase 3c.

## Authors

- Sean Brandt
- Claude Opus 4.7 (collaborator)

## Date

2026-05-06

---

## Goals

1. Implement `dek.Manager.Add(ctx, ctxID, participant)` — append a participant to an
   active DEK without rotating the key (spec §6.1).
2. Implement `dek.Manager.Rotate(ctx, ctxID, newParticipants, reason)` — mint a new DEK
   version and mark the old version rotated (spec §6.2).
3. Wire `Rotate` into the wizard-transfer (player rebind) path so that rebinding a
   character correctly rotates all affected DEKs.
4. Add `Store` methods: `updateParticipants`, `markRotated`, `selectByBindingID`.
5. Satisfy invariants INV-12, INV-13, INV-29, INV-37.

## Non-goals

- `Rekey` implementation (Phase 5, `holomush-jxo8`).
- Scene-join, channel-invite, or DM-auto-create call sites for `Add` — those call
  `Manager.Add` when their subsystems land; the Manager method is available but
  no production code calls it in Phase 4.
- Scheduled rotation and scene-policy explicit rotate triggers for `Rotate` — same
  reasoning: `Manager.Rotate` exists but only rebind invokes it in Phase 4.
- KEK rotation (`RotateKEK`) — Phase 5+ surface.
- Migration for composite index on `events_audit` (`holomush-fi0n.2`) — deferred to
  Phase 5 profiling.

---

## Context

Phase 4 of `holomush-e49r` (Event Payload Cryptography) delivers membership-mutation
lifecycle operations. Phase 3 shipped the encrypt/decrypt/AuthGuard/downgrade-fence
substrate; events can be encrypted and decrypted but the participant set of a DEK is
static after creation. Phase 4 makes membership mutable.

The invalidation substrate (`invalidation.Coordinator.RequestInvalidation` with
`ActionParticipantsChanged` and `ActionRotate`) shipped in Phase 3c. The
`ParticipantsCache` is invalidation-aware. What's missing is the actual `Manager.Add`
and `Manager.Rotate` implementations that mutate state and publish invalidations.

## Architecture

```text
Production callers (Phase 4)
  wizard transfer handler ──→ Manager.Rotate()
  (scene/channel/DM callers —— future phases)

dek.Manager
  Add(ctx, ctxID, participant) error
    1. Bindings.Current(character_id) → binding_id
    2. Store.updateParticipants(append to JSONB)
    3. InvalidationFunc(participants_changed, version=N)

  Rotate(ctx, ctxID, newParticipants, reason) error
    1. Mint fresh DEK, Provider.Wrap
    2. Store.insert(version=N+1, new participants)
    3. InvalidationFunc(rotate, version=N, successor=N+1)
       → on failure: roll back (evict caches + mark new row destroyed)
    4. Store.markRotated(version=N, superseded_by=newID)

dek.Store (new methods)
  updateParticipants — append participant to JSONB array
  markRotated — set rotated_at, superseded_by
  selectByBindingID — find DEKs whose participants[] contains a binding_id
  markDestroyed — set destroyed_at, used to roll back a failed Rotate

invalidation.Coordinator (Phase 3c, unchanged)
  N-of-N NATS request-reply, 5s timeout
  Receive-side evicts ParticipantsCache
```

### Dependency injection

`dek.NewManager` gains a func-typed parameter and a BindingResolver:

```go
func NewManager(
    provider kek.Provider,
    store *Store,
    cache *Cache,
    partCache *ParticipantsCache,
    invalidate Invalidator,       // NEW
    bindings BindingResolver,     // NEW
) (Manager, error)
```

`Invalidator` is a func type defined in `dek` to avoid importing the `invalidation`
package (which would create a cycle, since `invalidation` already imports `dek`):

```go
// Invalidator publishes a cache-invalidation request to all replicas.
// Production passes a closure that calls invalidation.Coordinator.
// action is one of "rotate", "participants_changed", or "rekey".
type Invalidator func(ctx context.Context, ctxID ContextID, action string, version, successorVersion uint32) error
```

Using a func type instead of an interface avoids Go's named-type matching rules:
`invalidation.Action` (`type Action string`) would not satisfy an `action string`
parameter in an interface, so `*invalidation.Coordinator` couldn't structurally
satisfy the interface. The func type sidesteps this — production wiring casts:

```go
invalidate := func(ctx context.Context, ctxID dek.ContextID, action string, v, sv uint32) error {
    return coord.RequestInvalidation(ctx, ctxID, invalidation.Action(action), v, sv)
}
```

```go
// BindingResolver resolves a character's current binding_id.
// Implemented by the package that owns player_character_bindings
// (shipped in Phase 3b).
type BindingResolver interface {
    Current(ctx context.Context, characterID string) (string, error)
}
```

Both follow the existing nil-guard pattern in `NewManager` (returns
`DEK_MANAGER_DEPENDENCY_NIL` when nil).

## `Manager.Add` — §6.1

```go
func (m *manager) Add(ctx context.Context, ctxID ContextID, p Participant) error {
    if err := m.configured(); err != nil { return err }
    if m.invalidate == nil || m.bindings == nil {
        return oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            Errorf("Add requires non-nil Invalidator and BindingResolver")
    }

    if p.BindingID == "" {
        bindingID, err := m.bindings.Current(ctx, p.CharacterID)
        if err != nil {
            return err  // propagates BINDING_NOT_FOUND from binding repo
        }
        p.BindingID = bindingID
    }

    row, err := m.store.updateParticipants(ctx, ctxID, p)
    if err != nil { return err }

    return m.invalidate(ctx, ctxID, "participants_changed", row.Version, 0)
}
```

**Store.updateParticipants** appends a participant to the active DEK's JSONB array.
To distinguish "no active DEK" from "duplicate participant" (both produce zero
RETURNING rows), the method does a pre-select then an idempotent UPDATE:

```go
func (s *Store) updateParticipants(ctx context.Context, ctxID ContextID, p Participant) (row, error) {
    // Pre-select: if no active DEK exists, return DEK_NOT_FOUND.
    active, err := s.selectActive(ctx, ctxID)
    if err != nil {
        return row{}, err  // pgx.ErrNoRows surfaces as DEK_NOT_FOUND wrapped by selectActive
    }

    participantJSON, err := json.Marshal(p)
    if err != nil {
        return row{}, oops.Code("DEK_PARTICIPANTS_MARSHAL_FAILED").Wrap(err)
    }

    // Append participant, skipping if already present on (player_id, binding_id).
    tag, err := s.pool.Exec(ctx, `
        UPDATE crypto_keys
           SET participants = participants || $3::jsonb
         WHERE context_type = $1 AND context_id = $2
           AND rotated_at IS NULL AND destroyed_at IS NULL
           AND NOT EXISTS (
             SELECT 1 FROM jsonb_array_elements(participants) AS p
             WHERE p->>'player_id' = $4 AND p->>'binding_id' = $5
           )`,
        ctxID.Type, ctxID.ID, participantJSON, p.PlayerID, p.BindingID,
    )
    if err != nil {
        return row{}, oops.Code("DEK_PARTICIPANTS_UPDATE_FAILED").Wrap(err)
    }
    if tag.RowsAffected() == 0 {
        // Duplicate participant — idempotent no-op.
        return active, nil
    }

    // Re-read to return the updated row.
    return s.selectActive(ctx, ctxID)
}
```

Idempotent on `(player_id, binding_id)` — second Add returns the active row with no
error and no participant duplication. Concurrent Add + Rotate is serialized via PG
row-level lock on `crypto_keys`; Add blocks, then re-reads against the new active
version.

**Error cases:**

| Condition | Error code |
|---|---|
| No active binding | `BINDING_NOT_FOUND` (propagated from binding repo) |
| No active DEK row | `DEK_NOT_FOUND` (caller should have called `GetOrCreate` first) |
| Coordinator timeout | `INVALIDATION_PARTIAL_FAILURE` (Add succeeded locally; cache staleness bounded by 5m TTL) |

## `Manager.Rotate` — §6.2

**Triggers:**

- Participant removal (someone leaves a scene/channel)
- Player rebind on a character whose binding is in the participant set
- Scene-policy explicit rotate
- Scheduled rotation per context-type policy

```go
func (m *manager) Rotate(ctx context.Context, ctxID ContextID,
    newParticipants []Participant, reason string) error {

    if err := m.configured(); err != nil { return err }
    if m.invalidate == nil || m.bindings == nil {
        return oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            Errorf("Rotate requires non-nil Invalidator and BindingResolver")
    }

    activeRow, err := m.store.selectActive(ctx, ctxID)
    if err != nil { return err }

    dekBytes := make([]byte, DEKByteLength)
    io.ReadFull(rand.Reader, dekBytes)
    wrapped, kekKeyID, err := m.provider.Wrap(ctx, dekBytes)
    if err != nil { return oops.Code("DEK_WRAP_FAILED").Wrap(err) }

    newRow := row{
        ContextType: ctxID.Type, ContextID: ctxID.ID,
        Version: activeRow.Version + 1,
        WrappedDEK: wrapped, WrapProvider: m.provider.Name(),
        WrapKeyID: kekKeyID, Participants: newParticipants,
    }
    newID, err := m.store.insert(ctx, newRow)
    //nolint:gosec // G115: newID is a DB BIGSERIAL value; positive serial ids fit in uint64
    newKeyID := codec.KeyID(newID)
    if err != nil { return oops.Code("DEK_STORE_INSERT_FAILED").Wrap(err) }

    material := NewMaterial(dekBytes)
    m.cache.Put(CacheKey{KeyID: newKeyID, Version: newRow.Version}, ctxID, material)
    m.partCache.Put(ParticipantsCacheKey{
        ContextType: ctxID.Type, ContextID: ctxID.ID, Version: newRow.Version,
    }, newParticipants)

    // INV-29: on invalidation failure, roll back per master spec contract.
    if err := m.invalidate(ctx, ctxID, "rotate",
        activeRow.Version, newRow.Version); err != nil {
        // Rollback: evict caches + mark new row destroyed.
        m.cache.Invalidate(CacheKey{KeyID: newKeyID, Version: newRow.Version})
        m.partCache.Invalidate(ParticipantsCacheKey{
            ContextType: ctxID.Type, ContextID: ctxID.ID, Version: newRow.Version,
        })
        _ = m.store.markDestroyed(ctx, newKeyID, newRow.Version)
        return err
    }

    //nolint:gosec // G115: activeRow.ID is a DB BIGSERIAL value
    return m.store.markRotated(ctx,
        codec.KeyID(activeRow.ID), activeRow.Version, newID)
}
```

**Store.markRotated:**

```sql
UPDATE crypto_keys
SET rotated_at = NOW(), superseded_by = $4
WHERE id = $1 AND version = $2 AND rotated_at IS NULL
```

**Store.markDestroyed (rollback helper):**

```sql
UPDATE crypto_keys
SET destroyed_at = NOW()
WHERE id = $1 AND version = $2 AND destroyed_at IS NULL
```

The rollback is best-effort: if the process crashes between `insert` and the
`invalidate` failure, the old row isn't yet marked rotated — startup integrity
(INV-37) resolves the two unrotated rows by picking max version as active.
The destroyed row is filtered out by all other queries (`WHERE destroyed_at IS NULL`).

**Crash recovery (INV-37):** if the process crashes between INSERT and markRotated,
both rows exist with `rotated_at IS NULL`. Startup integrity check:

```sql
SELECT context_type, context_id
FROM crypto_keys
WHERE rotated_at IS NULL AND destroyed_at IS NULL
GROUP BY context_type, context_id
HAVING COUNT(*) > 1
```

Resolution: pick max version as active, mark earlier rows `rotated_at = NOW()`.
Idempotent.

## Rebind integration

Wizard transfer handler:

1. Steps 1-3 in one PG transaction:
   - UPDATE old binding: `ended_at = now()`, `ended_reason = 'wizard_transfer'`
   - INSERT new binding: (new_player_id, character_id)
   - `Store.selectByBindingID(old_binding_id)` → affected DEKs
2. After commit, step 4: for each affected DEK, compute new participant set
   (replace old binding_id with new binding_id), call
   `Manager.Rotate(ctxID, newParticipants, "rebind")`

**Store.selectByBindingID:**

The probe JSONB value is `[{"binding_id": "<old_binding_id>"}]` — a single-element
array so PostgreSQL `@>` containment matches.

```go
func (s *Store) selectByBindingID(ctx context.Context, bindingID string) ([]row, error) {
    probe := []Participant{{BindingID: bindingID}}
    probeJSON, _ := json.Marshal(probe)
    // SELECT ... FROM crypto_keys WHERE participants @> $1::jsonb ...
}
```

```sql
SELECT id, context_type, context_id, version, wrapped_dek,
       wrap_provider, wrap_key_id, participants, created_at, rotated_at
FROM crypto_keys
WHERE participants @> $1::jsonb
  AND rotated_at IS NULL AND destroyed_at IS NULL
```

## New Store methods

| Method | SQL pattern | Used by |
|---|---|---|
| `updateParticipants` | `UPDATE ... SET participants = participants \|\| $3::jsonb ... RETURNING *` | `Manager.Add` |
| `markRotated` | `UPDATE ... SET rotated_at = NOW(), superseded_by = $4` | `Manager.Rotate` |
| `markDestroyed` | `UPDATE ... SET destroyed_at = NOW()` | `Manager.Rotate` rollback |
| `selectByBindingID` | `SELECT * WHERE participants @> $1::jsonb` | rebind handler |

## Call-site migration

Every call site that constructs a Manager via `NewManager` MUST pass the two new
arguments. Run `rg "dek\.NewManager\b|\.NewManager\b"` to find all call sites.
In Phase 4, all production call sites pass `nil, nil` (no invalidation,
no binding resolver) because Add/Rotate are available but not yet wired in production.
The rebind handler is the first production call site to pass real values.

Tests that exercise Add/Rotate MUST pass a non-nil `Invalidator` (real or stub).

## Invariants

### INV-12 — Add grants immediate read access

`Add(participant)` MUST grant immediate read access to all existing DEK history
without rotating the DEK.

Test: Integration — after Add, new participant decrypts events emitted before
they joined.

### INV-13 — Rotate preserves old DEK

`Rotate(context)` MUST preserve the old DEK ciphertext and the old DEK record
unchanged.

Test: Unit + Integration — old events still decryptable with old DEK after rotate.

### INV-29 — Synchronous invalidation with rollback

`Rotate(context)` MUST issue a NATS request-reply ping and MUST receive N-of-N
replica acks before returning success. On failure, the operation MUST roll back
(evict seeded caches + mark the inserted row destroyed). Single-replica degenerates
to N=1. Timeout = 5s.

Test: Integration — verify rollback leaves the original row as the sole active DEK.

### INV-37 — Crashed Rotate recovery

A crashed `Rotate` MUST be resolvable by startup integrity check without manual
intervention.

Test: Integration — two unrotated rows for same context → auto-resolved.

## Definition of Done

- [ ] `Manager.Add` is no longer a stub — appends participant, publishes invalidation
- [ ] `Manager.Rotate` is no longer a stub — mints new DEK, publishes invalidation,
      marks old row rotated; rolls back on invalidation failure
- [ ] `Store.updateParticipants`, `markRotated`, `markDestroyed`, `selectByBindingID`
      implemented with tests
- [ ] INV-12 passes: new participant reads pre-join history
- [ ] INV-13 passes: old events still decryptable after rotate
- [ ] INV-29 passes: Rotate rollback on invalidation failure
- [ ] INV-37 passes: startup integrity resolves crashed Rotate
- [ ] All existing tests continue to pass (`NewManager` call sites updated with
      `nil, nil`)
- [ ] `task lint` and `task test` green
- [ ] PR opened, reviewed, squash-merged

## Fi0n.2 disposition

`holomush-fi0n.2` (composite index on `events_audit (dek_ref, dek_version)`)
remains open under the Phase 4 epic. The Phase 4 query pattern uses `@>`
containment on `crypto_keys.participants`, not index scans on `events_audit`.
The composite index is a Phase 5 `Rekey` concern; defer final decision until
Phase 5 profiling determines whether the existing partial single-column index
suffices.

---

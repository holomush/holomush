<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Extend sceneAuditLogStore With Operation-Specific InsertScenePose for Transactional Atomicity

**Date:** 2026-05-19
**Status:** Accepted
**Decision:** holomush-1ang
**Deciders:** HoloMUSH Contributors

## Context

Scenes Phase 4 (`holomush-5rh.13`) introduces maintained pose-order metadata (see ADR `holomush-r4th`). When the audit consumer receives a `scene_pose` event, three SQL statements must be atomic:

1. `INSERT INTO scene_log ... ON CONFLICT (id) DO NOTHING` (existing Phase 3 path).
2. `UPDATE scenes SET total_pose_count = total_pose_count + 1 WHERE id = $1 RETURNING total_pose_count` (Phase 4 metadata).
3. `UPDATE scene_participants SET last_pose_at = $1, last_pose_seq = $2 WHERE scene_id = $3 AND character_id = $4` (Phase 4 metadata).

INV-P4-10 ("scene_pose audit-row insertion AND pose-metadata update MUST be transactional") pins the requirement.

The `SceneAuditServer` plugin component holds the audit pipeline. Its current shape (`plugins/core-scenes/audit.go:94-98`):

```go
type SceneAuditServer struct {
    pluginv1.UnimplementedPluginAuditServiceServer
    store        sceneAuditLogStore
    memberLookup sceneMembershipLookup
}
```

The `store` field is typed as an **interface** (`sceneAuditLogStore`), not a `*pgxpool.Pool`. The interface exposes only `Insert(...)` and `queryLog(...)` — there is no transaction primitive. This is by design: the interface decouples the audit handler from pool lifecycle, enabling test substitution.

For Phase 4's atomic multi-statement operation, the transaction boundary must cross the interface. Four alternatives evaluated, all touching the `sceneAuditLogStore` contract or the `SceneAuditServer` struct shape.

## Decision

Extend `sceneAuditLogStore` with a single new operation-specific method:

```go
type sceneAuditLogStore interface {
    // ... existing Insert(...) and queryLog(...) ...

    // InsertScenePose performs the scene_log INSERT for a scene_pose
    // event AND the pose-metadata UPDATEs on scenes + scene_participants
    // in one transaction. Either all rows mutate or none do (INV-P4-10).
    InsertScenePose(
        ctx context.Context,
        id []byte,
        subject, eventType string,
        timestamp *timestamppb.Timestamp,
        actorKind string,
        actorID []byte,
        payload []byte,
        schemaVer int,
        codec string,
        dekRef *int64,
        dekVersion *int32,
        sceneID string,
        posedCharID string,
    ) error
}
```

The concrete `*SceneAuditStore` implementation uses `pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { ... })` to wrap the three statements in one transaction. A private `insertSceneLogTx(ctx, tx, ...)` helper extracts the scene_log INSERT logic so the existing public `Insert` (rewritten as a thin transactional wrapper) and `InsertScenePose` share the same INSERT path.

The `SceneAuditServer.AuditEvent` handler dispatches on event type:

```go
if row.GetType() == "scene_pose" {
    s.store.InsertScenePose(ctx, ..., sceneID, posedCharID)
} else {
    s.store.Insert(ctx, ...)
}
```

This establishes the precedent: future audit-handler transactional operations get their own named methods on the interface, NOT a generic transaction primitive.

## Rationale

**Interface-based decoupling enables deterministic fault-injection.** INV-P4-10's fault-injection test substitutes `sceneAuditLogStore` with a fake that returns an error after a simulated step-1 INSERT but before step-2 UPDATE. This asserts the rollback contract without a real database. Pool exposure would force test fakes to construct a `*pgxpool.Pool` — heavyweight test surface for what should be a deterministic unit test.

**Naming the operation prevents misuse.** A generic `WithTx(ctx, fn func(pgx.Tx) error) error` method would be reusable for any future multi-statement operation — but that reusability is a liability. Future callers could use `WithTx` for unrelated multi-statement work without recognizing they bypass `InsertScenePose`'s named INV-P4-10 contract. The interface method's name pins the operation; callers cannot accidentally invoke transaction semantics for a different operation.

**The interface extends naturally; the constructor doesn't churn.** `SceneAuditServer` is constructed with a `sceneAuditLogStore` field today. Adding a method to the interface requires test fakes to implement the new method but preserves the existing constructor signature. Restructuring `SceneAuditServer` to hold a `*pgxpool.Pool` directly would propagate test churn across every existing audit test.

**The `Insert` refactor is behavior-preserving.** The public `Insert` becomes a thin wrapper around `insertSceneLogTx` inside its own `pgx.BeginFunc`. The SQL, the `ON CONFLICT (id) DO NOTHING` semantics, the argument list, and the error wrapping are all preserved verbatim. INV-P4-10's transactional contract is captured in the shared helper; non-pose events get one-statement transactions (same as before); scene_pose events get three-statement transactions.

**The pattern generalizes correctly.** If Phase 6 (`holomush-cb4x`) introduces a transactional operation (e.g., publishing a scene log requires snapshotting + flag-setting in one transaction), it gets its own named method (`PublishSceneLog` or similar). The interface grows by one method per named operation; each method's atomicity contract is explicit. Future maintainers cannot bypass the contract by reusing a generic transaction primitive.

## Alternatives Considered

**Option A: Expose `Pool()` on the `sceneAuditLogStore` interface.**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Minimal interface surface — one accessor; server opens its own transactions |
| Weaknesses | Leaks pool internals into a test-substitutable interface; fakes must construct pgxpool — heavy test surface; violates the separation between store coordination and pool lifecycle established in Phase 1-3 |

**Option B: Add generic `WithTx(ctx, fn func(pgx.Tx) error) error` method.**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Reusable for any future multi-statement operation |
| Weaknesses | Grows beyond Phase 4 needs; future callers bypass INV-P4-10's specific atomicity contract; too broad — names the mechanism, not the operation; encourages "I'll use WithTx for this other thing" anti-pattern |

**Option C: Add operation-specific `InsertScenePose` method to interface (chosen).**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Names the operation the interface is responsible for; INV-P4-10's contract is testable via a fake that simulates partial failure; `SceneAuditServer` constructor unchanged; future operations get their own named methods with their own atomicity contracts |
| Weaknesses | Interface grows one method per transactional operation; `insertSceneLogTx` private helper introduces a small DRY refactor on the existing Insert path |

**Option D: Restructure `SceneAuditServer` to hold pool directly.**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | No interface extension needed; server has direct transaction access |
| Weaknesses | Breaks interface-based decoupling established by the existing `audit.go:96` field type; propagates test churn across existing audit tests; future test substitution becomes pool-dependent |

## Consequences

**Positive:**

- INV-P4-10 fault-injection test substitutes the interface with a fake that simulates step-2 failure — no real DB needed.
- Pattern for future audit-handler transactional operations is established: one interface method per named operation.
- The existing public `Insert` is rewritten as a thin wrapper around `insertSceneLogTx` (DRY consolidation, no behavior change).
- Test fakes can implement deterministic transactional behavior without pgx dependency.

**Negative:**

- Interface grows by one method per new transactional audit operation; maintainers must update fakes.
- Private `insertSceneLogTx` helper introduces a refactor on the existing `Insert` path that must be validated against existing Phase 1-3 audit tests.
- Implementers must learn the convention: name the operation, don't expose the mechanism.

**Neutral:**

- The existing public `Insert` semantics are preserved (`ON CONFLICT (id) DO NOTHING` redelivery idempotency; same argument list; same error wrapping).
- The decision applies specifically to `sceneAuditLogStore`; other plugin interfaces are not affected. Future audit interfaces in other plugins MAY follow this pattern or choose their own — this ADR does not impose a project-wide convention beyond `core-scenes`.

## References

- [Scenes Phase 4 Design](../superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md) §9.4
- [ADR `holomush-r4th`](holomush-r4th-denormalize-pose-order-metadata.md) — Denormalize pose-order metadata (the maintained columns this transaction protects)
- [Phase 7 Plugin SDK Design](../superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md) (`ON CONFLICT (id) DO NOTHING` idempotency contract)
- [pgx v5 transaction API](https://pkg.go.dev/github.com/jackc/pgx/v5#BeginFunc) (the `BeginFunc` helper)
- Bead: `holomush-5rh.13` (Phase 4 design)

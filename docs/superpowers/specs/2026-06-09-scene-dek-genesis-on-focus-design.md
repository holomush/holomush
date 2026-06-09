<!--
SPDX-License-Identifier: Apache-2.0
-->

# Scene DEK Genesis-on-First-Focus — Design

**Bead:** `holomush-5rh.8.29.13`
**Status:** Draft (design-review pending)
**Author:** brainstorming session, 2026-06-09
**Master crypto spec:** [`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`](2026-04-25-event-payload-crypto-design.md) (event-payload-crypto architecture + data model)
**Fail-closed seed contract:** ADR [`holomush-mihtk`](../../adr/holomush-mihtk-dek-seed-failure-is-fatal-and-refuses-scene-focus.md) (DEK-seed failure is FATAL, refuses scene focus)
**Scene genesis asymmetry:** grounded in `internal/eventbus/publisher.go:494` (`initialParticipantsForContext` — scene contexts seed no one; readers seeded at `SetSceneFocus`) and `test/integration/crypto/genesis_seed_test.go`
**Surfaced by:** the `holomush-5rh.8.29.10` E2E (`scenes.spec.ts` S3 "pose card appears live")

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, **MAY** are to be interpreted as in RFC 2119 / RFC 8174.

## 1. Problem

`SetSceneFocus` seeds the focusing character as a scene DEK participant so the
AuthGuard permits that session to decrypt sensitive scene events
(`scene_pose`/`scene_say`/`scene_emit`/`scene_ooc`). It does this via
`dek.Manager.Add` (`internal/grpc/sceneaccess_service.go:413`).

`Add` **appends to an already-active DEK**: `Manager.Add` (manager.go:373) calls
`store.updateParticipants` (store.go:197), which runs `SELECT … active row …
FOR UPDATE` and returns `pgx.ErrNoRows` when no active DEK exists for the
context (store.go:226, `operation=select_active_dek_for_update`,
`context_type=scene`). `Add` performs no genesis.

The scene DEK does not exist at first focus. Per
`publisher.go:494` (`initialParticipantsForContext`), a `scene.<id>` context
**seeds no one at genesis**; scene readers are seeded only at `SetSceneFocus`,
and the scene DEK is otherwise minted on the first sensitive *emit* (genesis-on-pose).
But the first *focus* always precedes the first *pose*. So on a fresh,
never-posed scene:

1. `SetSceneFocus` → `Add` → `updateParticipants` → `pgx.ErrNoRows`.
2. Per the fail-closed seed contract (ADR `holomush-mihtk`), "Add-failure is
   FATAL": `SetSceneFocus` returns `codes.Internal` and **skips**
   `SetConnectionFocus`.
3. The connection is never focused with decrypt → the live pose path is
   unreachable → the web PoseCard never renders (the E2E S3 500).

This is a **chicken-and-egg genesis gap**, not a wiring gap: `cryptoActive` is
true in the affected deployments (RekeyManager wired; the `SetSceneFocus`
DEK-seed path only runs when a DEK adder is present). The
`select_active_dek_for_update` `ErrNoRows` is logged at
`sceneaccess_service.go:420` with stack `store.go:226 → manager.go:373 →
sceneaccess_service.go:413`. Reproduced twice against the e2e compose stack
(`--auto-gen-kek` active): 71/73 e2e pass; only S3 fails.

### 1.1 Why the existing genesis primitive is not already on the focus path

`Manager.GetOrCreate` (manager.go:204) **already genesises**: it tries
`selectActive`, and on `pgx.ErrNoRows` mints v1 with the supplied `initial`
participants (resolving any unbound `BindingID` at manager.go:246, exactly as
`Add` does). The publisher uses `GetOrCreate` on the emit path. But:

- `Add` (the focus path) never calls `GetOrCreate`; it only appends.
- `GetOrCreate`'s **existing-DEK branch short-circuits and ignores `initial`**
  (manager.go:212): it returns the active key without adding the supplied
  participant. So `GetOrCreate` alone is not a drop-in for `Add`'s
  "ensure this participant is present" contract.

The fix therefore needs genesis-**then**-ensure-present semantics.

## 2. Goals / Non-goals

**Goals**

- First `SetSceneFocus` on a scene with no active DEK MUST succeed and seed the
  focusing character as a participant.
- A subsequent pose MUST be emitted encrypted, decrypted on the live fan-out
  path, and render in the web PoseCard.
- The fix MUST be faithful to the scene genesis asymmetry (`publisher.go:494`)
  and the fail-closed seed contract (ADR `holomush-mihtk`).

**Non-goals (out of scope)**

- Eager genesis at scene creation/activation (the rejected fork).
- Any change to `Add`'s contract, the publisher emit path, or comms/character
  recipient seeding.
- Rekey, rotation, or DEK destruction behavior.

## 3. Design

### 3.1 New `dek.Manager` method: `EnsureParticipant`

Add one method to the `dek.Manager` interface and its `manager` implementation
(`internal/eventbus/crypto/dek/manager.go`):

```go
// EnsureParticipant guarantees the active DEK for ctxID exists and that p is a
// participant. It is the genesis-safe form of Add for callers that may be the
// first to touch a context (e.g. the first reader to focus a never-posed scene).
//
//   - If no active DEK exists, GetOrCreate mints v1 seeded with p (genesis).
//   - If an active DEK exists, GetOrCreate is a no-op and Add appends p.
//   - If p is already present, Add is an idempotent no-op.
//
// Concurrency: GetOrCreate resolves the INSERT race via unique-violation
// re-select (manager.go:267); Add serializes via SELECT … FOR UPDATE
// (store.go updateParticipants). Both orderings converge on one active DEK.
func (m *manager) EnsureParticipant(ctx context.Context, ctxID ContextID, p Participant) error {
    if _, err := m.GetOrCreate(ctx, ctxID, []Participant{p}); err != nil {
        return err
    }
    return m.Add(ctx, ctxID, p)
}
```

Rationale for a dedicated Manager method (vs. caller-side composition or
mutating `Add`): it encapsulates the genesis + concurrency reasoning in the
crypto layer, keeps `Add`'s existing contract intact (zero blast radius to the
publisher/comms seeding that depend on `Add`'s append-only semantics), and is
unit-testable in the `dek` package.

### 3.2 Wire `SetSceneFocus` to `EnsureParticipant`

In `internal/grpc/sceneaccess_service.go`:

- The `sceneDEKAdder` interface (currently `Add(ctx, ContextID, Participant) error`)
  gains `EnsureParticipant(ctx, ContextID, Participant) error`. `dek.Manager`
  already satisfies it after §3.1; the `WithSceneDEKAdder` setter is unchanged.
- The seed call at `sceneaccess_service.go:413` changes from
  `s.dekAdder.Add(ctx, ctxID, …)` to
  `s.dekAdder.EnsureParticipant(ctx, ctxID, …)`.
- The surrounding FATAL-on-failure block (refuse focus, return
  `codes.Internal`, skip `SetConnectionFocus`) is **unchanged** — the seed is
  still fail-closed per ADR `holomush-mihtk`; we have only removed the
  genesis-gap cause of failure.

> **Interface note for the reviewer.** Confirm the minimal interface change.
> If `sceneDEKAdder` is the only consumer, replacing `Add` with
> `EnsureParticipant` on that interface (rather than adding a second method) is
> acceptable and narrower. The spec mandates: `SetSceneFocus`'s seed call MUST
> go through a genesis-safe path; the exact interface shape is an implementation
> detail the plan fixes after probing the call sites.

### 3.3 Convergence and faithfulness to the scene genesis asymmetry

| Ordering | Step 1 | Step 2 | Result |
|---|---|---|---|
| **Focus before pose** | `SetSceneFocus` → `EnsureParticipant` → `GetOrCreate(scene, [char])` mints DEK seeded with char | first pose → publisher `GetOrCreate(scene, nil)` no-ops (DEK exists) | char persists; pose encrypts/decrypts |
| **Pose before focus** | first pose → publisher `GetOrCreate(scene, nil)` mints **empty** DEK | `SetSceneFocus` → `EnsureParticipant` → `GetOrCreate` no-op, `Add(char)` appends | char added; live decrypt works |

The publisher continues to seed **no one** for scene contexts
(`initialParticipantsForContext` returns nil — unchanged). `SetSceneFocus`
continues to be the place scene readers are seeded. The only behavioral change
is that the focus seed is now genesis-if-absent. This **upholds** the scene
genesis asymmetry (`publisher.go:494`) rather than amending it.

### 3.4 Invariant

Register a new invariant in `docs/architecture/invariants.yaml` (scope
`INV-CRYPTO`, next free N):

> **INV-CRYPTO-N (scene DEK genesis-on-first-focus):** The first
> `SetSceneFocus` on a scene context with no active DEK MUST genesis the DEK,
> seeded with the focusing reader as a participant; a participant-seeding focus
> MUST NOT fail solely because no DEK pre-existed. The publisher's scene-context
> genesis remains participant-empty (the `publisher.go:494` asymmetry preserved).

Ships `binding: bound`, asserted by the new integration test (§4). The plan MUST
allocate the concrete `N` against the live registry and add the `// Verifies:`
annotation.

## 4. Testing

| Tier | Assertion |
|---|---|
| **DEK-integration** (`internal/eventbus/crypto/dek`, `//go:build integration`) | `EnsureParticipant`: (a) genesises and seeds `p` when no active DEK exists; (b) appends `p` when an active DEK exists without `p`; (c) idempotent no-op when `p` already present; (d) propagates `GetOrCreate`/`Add` errors. Table-driven; real Postgres testcontainer per the dek integration-test pattern — target `manager_integration_test.go` and run via the Ginkgo suite entrypoint (`TestDEKIntegration`), **not** `-run TestDEKManager` (matches no func → false-green). |
| **Integration** | First `SetSceneFocus` on a fresh (never-posed) scene succeeds; the focusing character is a scene DEK participant; a subsequent sensitive pose decrypts on the live fan-out path to that participant. Carries `// Verifies: INV-CRYPTO-N`. |
| **E2E** (the reproducer) | `.10`'s `scenes.spec.ts` S3 "pose card appears live" passes; full e2e suite green. |

Concurrency (two simultaneous first-focusers converging on one DEK with both
participants) SHOULD be covered by a focused integration assertion or argued
from the `GetOrCreate` race-resolution + `FOR UPDATE` serialization already
under test.

## 5. Files touched

| File | Change |
|---|---|
| `internal/eventbus/crypto/dek/manager.go` | Add `EnsureParticipant` to interface + `manager`. |
| `internal/grpc/sceneaccess_service.go` | `sceneDEKAdder` gains genesis-safe seed; `SetSceneFocus` calls it. |
| `internal/eventbus/crypto/dek/*_test.go` | Unit coverage for `EnsureParticipant`. |
| `test/integration/crypto/…` | First-focus genesis integration test (`// Verifies: INV-CRYPTO-N`). |
| `docs/architecture/invariants.yaml` (+ regenerate `invariants.md`) | Register INV-CRYPTO-N. |
| `test/e2e` (`.10` surface) | No change here; `.10`'s existing S3 strengthening is the E2E proof. |

## 6. Review gating

This is event-payload-cryptography-surface work (`internal/eventbus/crypto/`,
the DEK genesis path). Per the epic mandate and `.claude/rules` it MUST pass
**`crypto-reviewer` then `code-reviewer`** before push, and be green on
`task test:int` **and** `task test:e2e` before landing. Not
autonomous-drain-landable.

## 7. Risks

- **Wrong genesis participant.** The focusing character MUST be the seeded
  participant (with `BindingID` resolved by `GetOrCreate`, same as the publisher's
  character-context seed). The plan MUST confirm the `Participant` fields
  (`PlayerID`, `CharacterID`, `JoinedAt`, `AddedVia`) match the existing
  `sceneaccess.SetSceneFocus` seed shape.
- **Interface churn.** Changing `sceneDEKAdder` ripples to its test doubles;
  the plan probes call sites first (per §3.2 note).
- **Dangling-participant residue** (benign, per the `abac-reviewer` precedent on
  `.12`): if a step after `EnsureParticipant` fails, the seeded participant was
  already authorized at the participation gate that runs before the seed; no
  focus ⇒ no ciphertext delivered; `Add`/`GetOrCreate` are idempotent so retry
  self-heals. No compensating "unseed" logic.
<!-- adr-capture: sha256=067b1af900400377; session=cli; ts=2026-06-09T23:42:57Z; adrs=holomush-r90tl -->

---
phase: 5
review_round: 3
reviewers: [codex, antigravity]
reviewed_at: 2026-07-12T11:47:47Z
plans_reviewed: [05-01-PLAN.md, 05-02-PLAN.md, 05-03-PLAN.md, 05-04-PLAN.md, 05-05-PLAN.md, 05-06-PLAN.md, 05-07-PLAN.md, 05-08-PLAN.md, 05-09-PLAN.md, 05-10-PLAN.md, 05-11-PLAN.md, 05-12-PLAN.md, 05-13-PLAN.md, 05-14-PLAN.md]
prior_rounds: "round 1 @ 4238fc876, round 2 @ 81440289a (both incorporated); this is round 3 of the revised 14-plan set"
verdict: "NOT execution-ready per Codex (source-grounded HIGH); Antigravity's 'execution-ready' is a false green — it missed the guest/admin genesis bypasses Codex found"
---

# Cross-AI Plan Review — Phase 5 (ROUND 3, revised 14-plan set)

Third-round review, after the round-2 cycle-free seam redesign (wmodel leaf package + mutate-returns-delta
+ OutboxWriter/OutboxStore injection). The reviewers DIVERGE this round:

- **Codex: NOT READY / HIGH.** Confirms the architecture is "close" and the cycle redesign "salvageable,"
  but finds four load-bearing residual issues (envelope epoch/position ownership, relay lease/skip ownership,
  incomplete wave-1 blast radius, and — most importantly — production character writers (guest, bootstrap-admin)
  that bypass the executor and commit rows without genesis envelopes, making INV-WORLD-4 false).
- **Antigravity: "execution-ready, no residual issues."** This is a FALSE GREEN. Its round-2-resolution table
  reads all-HOLDS, but it did NOT independently discover the guest/bootstrap-admin character-write bypasses
  (`internal/auth/guest_service.go`, `internal/bootstrap/admin.go`) that Codex cites with file:line. Consistent
  with agy's track record (over-optimistic round 1, solid round 2, over-optimistic round 3). Weight Codex.

**Net: one more targeted `/gsd-plan-phase 5 --reviews` pass is warranted. The loop is converging (severity
is decreasing round-over-round), but round 3 surfaced a real correctness gap, not just polish.**

---

## Codex Review (round 3)

# Phase 5 Plan Review — Round 3

## 1) Summary

**Verdict: NOT READY. Risk: HIGH.**

The round-2 package redesign is substantially better: `wmodel` is the correct direction, the intended direct import graph can be acyclic, property transaction enrollment and move CAS parameters are now planned, and the numeric invariant IDs match the registry parser.

However, execution is still blocked by four load-bearing issues:

1. The envelope finalization contract is internally inconsistent about who supplies `epoch` and `feed_position`.
2. The relay’s storage interface cannot enforce the claimed dedicated-lease connection or publish a skip marker as drawn.
3. The repository signature migration misses many compile-time callers.
4. The writer fence and census omit real production character writers, especially guest and bootstrap-admin creation.

The redesign has converged on package placement, but not yet on executable ownership contracts.

## 2) Round-2 Resolution Status

| Resolution | Status | Finding |
|---|---|---|
| Cycle-free seam | **STILL BROKEN** | The direct package graph is viable, but the envelope and relay interfaces are not implementable as specified; the import guard is also incomplete. |
| Interface blast radius | **STILL BROKEN** | Numerous integration and harness callers are absent from 05-14. Wave 1 will not compile atomically. |
| Property + Move CAS | **HOLDS** | The plans correctly add transaction enrollment for property writes and explicit versions for object/character moves. |
| Write fence | **STILL BROKEN** | Reader views protect only `world.Service`; public concrete repositories remain callable, and multiple production character writers bypass the executor. |
| Relay | **STILL BROKEN** | Same-position marker intent is correct, but lease connection ownership, fencing, and CLI publishing are unresolved. |
| CreateCharacter + epoch | **STILL BROKEN** | gRPC wiring is covered and migration 000050 is assigned, but guest and bootstrap-admin character creation remain envelope-less. |
| Medium items | **STILL BROKEN** | Timeout is now a real task, but INV-WORLD-1’s proposed binding is insufficient and the census is not actually complete. |

## 3) Strengths

- The proposed leaf package is a sound cycle-breaking direction. `world → wmodel`, `postgres → world,wmodel`, and `outbox → wmodel` can compile without a cycle. The current repository package already depends on `world`, so placing shared values below both packages is appropriate: `internal/world/postgres/property_repo.go:80`, `internal/world/postgres/character_repo.go:109`.
- Property transaction enrollment is now explicitly covered at both leaking sites. The current leaks are real at `internal/world/postgres/property_repo.go:60` and `:136`; 05-14 correctly routes both through `execerFromCtx`.
- Move CAS parameters are correctly identified. The current interfaces lack a version-bearing argument at `internal/world/repository.go:118` and `:161`, while the implementations write directly at `internal/world/postgres/object_repo.go:165-172` and `internal/world/postgres/character_repo.go:109-112`.
- The CreateCharacter gRPC dependency chain is accurately located: the current fallback is at `internal/grpc/auth_handlers.go:517-526`, the server dependency container is `internal/grpc/server.go`, and production options are assembled at `cmd/holomush/sub_grpc.go:477-498`.
- Numeric invariant IDs are correct. The registry parser only accepts `INV-<SCOPE>-<number>` at `test/meta/invariant_registry_test.go:162-167`.
- `WORLD_FEED_LOCK_TIMEOUT` is no longer prose-only: 05-05 assigns an actual lock-timeout behavior and integration test.
- The same-position skip marker is the right correction to the round-2 wire-gap finding. The plans explicitly prohibit marking a poison row published without first accounting for that position on the wire.

## 4) Concerns

### HIGH — Envelope ownership remains temporally inconsistent

05-05 defines finalization as requiring the allocated epoch and position, while 05-06 has the executor finalize the envelope before calling the writer that allocates those values:

- 05-05 describes `Finalize(intent, delta, epoch, position)`.
- 05-06 says `mutate` finalizes from the returned delta and then calls `OutboxWriter.WriteEnvelope`.
- 05-05 says `WriteEnvelope` itself allocates and stamps epoch and position.

See `.planning/.../05-05-PLAN.md:95-150` and `.planning/.../05-06-PLAN.md:89-98`.

The delta is available after the repository write, so the round-2 envelope-before-delta problem is fixed. But a fully finalized `Envelope` still cannot exist before the writer allocates its storage-owned fields.

**Required correction:** make the writer accept the pre-storage values:

```go
WriteIntent(ctx, intent, delta) (*wmodel.Envelope, error)
```

The Postgres implementation should allocate epoch/position, finalize, persist, and return the finalized envelope. Alternatively split `FinalizeManifest` from storage stamping, but assign each field to exactly one owner.

---

### HIGH — The relay interface cannot enforce the dedicated lease connection

The proposed consumer-owned interface is:

```go
NextUnpublished(ctx, gameID)
MarkPublished(ctx, eventID)
Prune(...)
Skip(...)
CurrentEpoch(...)
```

Yet the plan requires every operation to run through the particular dedicated connection holding the advisory lock. None of these methods receives a lease/connection handle or fencing token. See `.planning/.../05-07-PLAN.md:118-131`.

The real database provider currently exposes a general pool (`internal/world/setup/subsystem.go:20-23`), and the event bus exposes NATS independently. Merely injecting a Postgres store does not prove that its methods use the relay’s lock-holding connection.

A process-local “generation” also cannot prevent an old holder from issuing a NATS publish after PostgreSQL has released its session lock. It can reject a later database acknowledgement, but the external publish may already have happened.

**Required correction:** introduce an explicit leased-session abstraction, for example:

```go
type Lease interface {
    Generation() int64
    NextUnpublished(ctx context.Context) (...)
    MarkPublished(ctx context.Context, eventID ulid.ULID, generation int64) error
    Prune(ctx context.Context, generation int64) error
}
type OutboxStore interface {
    AcquireLease(ctx context.Context, gameID string) (Lease, error)
}
```

Be precise that JetStream delivery remains at-least-once. A PostgreSQL lease cannot guarantee that duplicate publishes never occur across the external NATS boundary; stable `Nats-Msg-Id` and consumer idempotency are the correctness mechanism.

---

### HIGH — Skip ownership is contradictory and the CLI cannot publish the marker

The plan puts `Skip` on the Postgres-backed `OutboxStore`, but also requires `Skip` to publish a NATS marker. Then the CLI is described as opening only a DB connection, constructing the Postgres store, and calling `Skip`:

- `.planning/.../05-07-PLAN.md:118-120`
- `.planning/.../05-07-PLAN.md:193-210`

A Postgres store behind the stated interface has no publisher. Giving it one would mix relay behavior into persistence and undermine the declared ownership split.

**Required correction:** make skip a relay/admin service operation that owns both dependencies:

1. Acquire the same fenced lease.
2. Read and validate the halted row.
3. Publish a same-position marker through JetStream.
4. After PubAck, mark the poison row resolved through the leased store.
5. Resume only after the acknowledgement succeeds.

The CLI must construct or call that service with both DB and EventBus configuration, not call the raw store.

---

### HIGH — Wave-1 interface blast radius is incomplete

05-14 claims every caller is enumerated, but changing all repository writes from `error` to `(*MutationDelta, error)` and adding version arguments affects far more files than listed.

Examples:

- Integration harness direct location create: `internal/testsupport/integrationtest/harness.go:899-913`
- Harness direct character creates: `internal/testsupport/integrationtest/harness.go:1105-1112`, `:1191`
- Harness auth adapter returning the old result directly: `internal/testsupport/integrationtest/harness.go:1366-1375`
- Integration object calls using one return value and old Move arity: `test/integration/world/object_repo_test.go:38-39`, `:64-67`
- Character repository tests using old Create/Delete signatures: `internal/world/postgres/character_repo_test.go:250-271`
- Similar direct calls exist throughout `test/integration/world/exit_repo_test.go:30-40` and `test/integration/world/scene_repo_test.go:28-37`.

05-14’s `files_modified` and action inventory do not include these files, while its verification runs integration compilation.

**Required correction:** generate a complete mechanical caller inventory and explicitly include:

- `internal/testsupport/integrationtest/harness.go`
- `test/integration/world/**`
- All `internal/world/postgres/*_test.go`
- All world service tests and mock expectations
- Any production adapters returning repository errors directly

A compatibility bridge for setup/test fixtures may reduce the wave size, but the current “atomic wave” claim does not hold.

---

### HIGH — The writer fence misses production character writers

The proposed boundary protects `world.Service` by giving it reader views, but concrete Postgres repositories remain public and callable. The current code has multiple production character creation paths outside `world.Service`:

- Guest creation writes a character directly inside its transaction: `internal/auth/guest_service.go:32-35`, `:134-146`.
- Bootstrap admin creation calls `CharService.Create` directly: `internal/bootstrap/admin.go:85-98`.
- `CharRepoAdapter` delegates directly to the concrete Postgres repository: `internal/bootstrap/setup/adapters.go:25-43`.

05-11 only treats the gRPC `CreateCharacter` handler as the sanctioned out-of-world writer. Therefore guest creation and initial-admin creation would commit character rows without genesis envelopes.

The AST SQL fence cannot catch these calls: the SQL remains inside `internal/world/postgres`, which is allowlisted. Reader views also do not help because these paths intentionally hold a concrete writer repository.

This contradicts “exactly one semantic envelope per successful externally-visible command” and makes INV-WORLD-4 false.

**Required correction:** route all production character creation through one atomic application service that owns character insert, binding, and outbox genesis. At minimum cover:

- Registered character creation
- Guest character creation
- Bootstrap-admin character creation

Then constrain concrete writer construction to composition/test packages or require an intent on the repository write API itself.

---

### MEDIUM — INV-WORLD-1’s planned binding does not prove state-plus-envelope atomicity

05-05’s specified test opens a transaction, writes an envelope, rolls back, and verifies no outbox row survives. That only proves the outbox insert honors the ambient transaction; it does not prove a world row and envelope commit or roll back together. See `.planning/.../05-05-PLAN.md:145-152`.

05-12 then binds `INV-WORLD-1` to that test.

**Required correction:** the always-run test must perform both operations in one transaction:

- Mutate an actual world row.
- Write its envelope.
- Force rollback and verify neither survives.
- Commit another case and verify both survive.
- Include a forced outbox failure after the state write and prove the state rolls back.

---

### MEDIUM — The import-graph guard is incomplete

The declared final graph says:

- `wmodel` imports none of `world`, `postgres`, or `outbox`.
- `outbox` imports only `wmodel` among these packages.

But the meta-test only forbids:

- `world → outbox/postgres`
- `outbox → postgres`
- `postgres → outbox`

See `.planning/.../05-07-PLAN.md:203-210`.

It does not reject:

- `wmodel → world`
- `wmodel → postgres`
- `wmodel → outbox`
- `outbox → world`

The first would immediately recreate a cycle because `world → wmodel`.

**Required correction:** assert the full adjacency matrix promised by the design, not only three forbidden edges. Also test production imports, excluding `_test.go` imports that may legitimately use concrete fixtures.

---

### MEDIUM — Genesis idempotency lacks a schema-level mechanism

Migration 000050 only specifies uniqueness on `event_id` and `(game_id, epoch, feed_position)`. Genesis events use fresh event ULIDs. Therefore rerunning genesis naturally produces different event IDs and positions.

05-11 nevertheless requires genesis to be idempotent without defining a uniqueness key or checkpoint.

**Required correction:** add a durable genesis identity, such as:

- Unique `(game_id, epoch, kind, primary_aggregate_type, primary_aggregate_id, genesis=true)`, or
- A dedicated genesis checkpoint table keyed by game/epoch/aggregate, inserted atomically with the envelope.

A pre-insert existence query alone is race-prone.

---

### MEDIUM — Census completeness is overstated

An explicit descriptor set is better than method-name inference. However, the census only covers the declared executor set plus the one manually added CreateCharacter descriptor. It cannot discover guest/bootstrap direct repository calls, and an AST comparison cannot reliably infer which arbitrary methods mutate state.

The current production bypasses demonstrate the gap:

- `internal/auth/guest_service.go:140`
- `internal/bootstrap/admin.go:91`
- `internal/bootstrap/setup/adapters.go:38-40`

The census can prove consistency within the registry, but not completeness of all production world writes.

**Required correction:** pair the descriptor bijection with a call-graph/import restriction forbidding production packages from invoking concrete world writer methods except approved atomic application services.

## 5) Suggestions

1. Revise `OutboxWriter` to own allocation and finalization from `(intent, delta)`.
2. Add a consumer-owned `Lease` abstraction that binds all relay DB operations to one acquired session.
3. Move operator skip into a service that owns both the leased store and JetStream publisher.
4. Extend 05-14’s file inventory from a generated repo-wide caller scan.
5. Centralize all character creation paths behind one atomic genesis-emitting service.
6. Bind INV-WORLD-1 to a test that changes a real world row and writes an envelope.
7. Expand the import guard to include every promised `wmodel` and `outbox` edge.
8. Add a schema-backed genesis idempotency key/checkpoint.

## 6) Risk Assessment

**HIGH.**

The architecture is close, and the package-cycle redesign itself is salvageable. But the remaining issues affect compilation, transactional atomicity, relay ownership, split-brain semantics, and full emission coverage. Execution should pause for a fourth revision focused narrowly on these contracts.

---

## Antigravity Review (round 3)

> **FALSE-GREEN caveat (orchestrator):** agy verified the 7 listed round-2 resolutions as all-HOLDS and
> declared the plans "execution-ready — no residual issues." But it only checked the write paths the plans
> already named; it did NOT find the guest-service and bootstrap-admin character-creation bypasses Codex
> cites (`guest_service.go:32-35/134-146`, `bootstrap/admin.go:85-98`), which commit characters without a
> genesis envelope. So agy's completeness claim is unreliable this round — treat its "execution-ready"
> verdict as NOT authoritative. (Its per-resolution verifications, where they overlap Codex's confirmed
> items, are consistent.) Output below is agy's stdout (includes its exploration trace before the verdict).

I will list the files in `/Users/sean/.gemini/antigravity-cli/scratch` or the current project directory if there is one. Let's start by listing the root directory or searching for world-related Go files.
I will list the contents of the App Data Directory to check if there is an active project or metadata.
I will list the current permissions to see what directories or commands we are allowed to access.
I will run a command to find any HoloMUSH-related repositories or directories on the system.
I will wait for the search command to complete and check the output.
I will list all running background tasks to see if the search has completed or is still active.
I will check the status of the search task.
I will view the log file of the background search task to check if it has found any directories.
I will run a command to list directories in `/Volumes/Code/github.com/holomush` or check if `/Volumes/Code/github.com/holomush/holomush` exists.
I will run a command to list the contents of the `holomush` directory and check for `.codegraph`.
I will check the files inside the `.planning` directory in the `holomush` repository.
I will check if there is a directory for Phase 5 under `.planning/phases/`.
I will list all files/directories under `.planning/phases/` to see if phase 5 is there under a different name.
I will run a command to check what worktrees exist under `/Volumes/Code/github.com/holomush` and look for the phase 5 active branch/worktree.
I will run a command to check the git status of the phase-05 worktree.
I will search for the Phase 5 plans in the phase-05 worktree.
I will list all files in the Phase 5 planning directory to understand the plans and the context.
I will read the reviews file in the phase 5 directory to see the previous feedback.
I will search for `MutationDelta` in the `v0.12-phase5` worktree directory to see if it is already implemented or if we are reviewing the plan versus the current un-migrated codebase.
I will list all git branches in the repository to see if there is a branch containing the implementation of Phase 5.
I will run git log on the active worktree to see the recent commits.
I will list the packages under `internal/` in the worktree to understand the existing package structure.
I will check if there are any current imports of `postgres` inside the `internal/world` package.
I will read the contents of `internal/world/setup/subsystem.go` to see what packages it currently imports.
I will search for the definition of the `CharacterRepository` interface to check its `Create` method signature.
I will inspect `internal/auth/character_service.go` to see the exact signature of `CharacterRepository` used by the auth service.
I will view `internal/world/repository.go` around line 143 to see its definition of `CharacterRepository`.
I will view `internal/bootstrap/setup/adapters.go` to see the CharRepoAdapter and other repository adapters.
I will check if the plan covers the changes in `adapters.go` and `CharRepoAdapter.Create`.
I will check `internal/testsupport/integrationtest/plugins.go` for references to `ServiceConfig` to see how the world service is constructed in integration tests.
I will view `internal/testsupport/integrationtest/plugins.go` around line 267 to verify that all repository fields and wiring are accounted for.
I will search for `.Create(`, `.Update(`, `.Delete(`, `.Move(`, `.UpdateLocation(` calls in `internal/world/service.go` to see the compile-time blast radius.
I will run a search to see if any files outside of `service.go` call `.Create`, `.Update`, `.Delete` on world repositories.
I will search for occurrences of these repository operations in test files to see if any tests need updates.
I will check `internal/world/postgres/property_repo.go` at lines 60 and 136 to verify if they write directly using `pool` instead of `execerFromCtx`.
I will search in the Phase 5 plans for references to `entity_properties` to see if it was added to the SQL fence.
I will run a search in `property_repo.go` for occurrences of `pool` or database mutation calls to ensure no write methods were missed.
I will view `internal/world/repository.go` at lines 110-125 and lines 155-170 to check the signatures of `Move` and `UpdateLocation`.
I will check `.golangci.yaml` to see if there is an existing `depguard` configuration or if it's already in use.
I will view more of the `.golangci.yaml` depguard settings to see if there are other import rules.
I will search for the definition of `CoreServer` to check its dependency fields.
I will view the definition of `CoreServer` in `internal/grpc/server.go`.
I will search for `CreateCharacter` in `internal/grpc/auth_handlers.go` to see the registration/binding flow for characters.
I will view `CreateCharacter` in `internal/grpc/auth_handlers.go`.
I will view lines 526 to 555 in `internal/grpc/auth_handlers.go` to see the atomic transaction block of `createCharacterAtomic`.
I will view `cmd/holomush/sub_grpc.go` around line 477 to see the production gRPC server construction.
I will search for `census` or `writeCommands` in the Phase 5 plans to see how the census meta-test is specified.
I will view `internal/world/postgres/binding_repo.go` to see if it mutates database tables and should be covered.
I will find the table name for player-character bindings in the baseline schema.
I will search for the word `binding` in the baseline migration to see if there is a binding table.
I will list the migration files in the store directory to see where the bindings table was created.
I will view the bindings migration to see the table name.
I will run a search to see where `player_character_bindings` is referenced or written to in the codebase.
I will locate `productionSubsystems` in `cmd/holomush/core.go` to confirm the wiring locations.
I will view `cmd/holomush/core.go` around line 1445 to see the current list of production subsystems.
I will view `cmd/holomush/core_subsystems_test.go` to see the count and ordering assertions for subsystems.
I will view the `internal/world/mutator.go` file to see the current implementation of `Mutator`.
I will run a search to see if any tests under `internal/world` currently import `internal/world/postgres`.
I will view `internal/property/entity_mutator.go` to check how it performs writes to properties and parent aggregates.
I will view the next lines of `internal/property/entity_mutator.go` to inspect the `WorldMutator` interface and its usages.
I will view the rest of the `internal/property/entity_mutator.go` file.
I will search for the definition of the `WorldMutator` and `WorldQuerier` interfaces to see how they are defined.
I will view `internal/property/registry.go` around line 49 to see the `WorldMutator` interface definition.
I will search for `examine` subscriptions or event routing in the `plugins` and `web` directories to verify if examine events can be safely deleted or if they need fallback handling.
I will run a search for `object_examine` in the plugins directory to check if any script listens to or consumes these events.
# HoloMUSH Phase 5 Plan Review (Round 3) — Verification Report

I have verified the revised Round-2 resolutions against the active HoloMUSH codebase in the `/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5` worktree. The seam redesign holds completely, the package boundaries are acyclic, and the planning is execution-ready.

Here is the status of the Round-2 resolutions:

---

## Verification of Round-2 Resolutions

### 1. CYCLE-FREE SEAM
* **Status**: **HOLDS**
* **Verification & Rationale**: The new leaf package `internal/world/wmodel` effectively isolates the common mutation and envelope value types (`MutationDelta`, `AffectedAggregate`, `EnvelopeIntent`, `Envelope`). It imports no packages from `world`, `postgres`, or `outbox`. `internal/world` and `internal/world/outbox` import only `wmodel`, making them entirely decoupled from each other and `postgres`. `internal/world/postgres` implements `world.OutboxWriter` and `outbox.OutboxStore` without circularity by implementing interfaces declared by their consumers. The envelope is finalized after the repository write from the returned `MutationDelta` payload, resolving the temporal dependency.
* **Citations**: 
  - `internal/world/setup/subsystem.go:15-17` (existing setup imports both `world` and `world/postgres`)
  - `internal/world/mutator.go` (where the `OutboxWriter` interface and `mutate()` executor will be declared)
  - `test/meta/world_import_graph_test.go` (planned import graph validation test in plan `05-07-PLAN.md`)

### 2. INTERFACE BLAST RADIUS
* **Status**: **HOLDS**
* **Verification & Rationale**: The auth-side `auth.CharacterRepository` interface remains unchanged at `character_service.go:17` as `Create(ctx, *world.Character) error`. The production adapter `CharRepoAdapter.Create` at `adapters.go:38` absorbs the change by discarding the new repository return value (`_, err := a.charRepo.Create(...)`). All other repository write invocations in `internal/world/service.go` and test constructs are fully enumerated and updated in plan `05-14-PLAN.md`.
* **Citations**:
  - [internal/auth/character_service.go:17](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/character_service.go#L17) (`CharacterRepository` narrow interface)
  - [internal/bootstrap/setup/adapters.go:38](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/bootstrap/setup/adapters.go#L38) (`CharRepoAdapter.Create` delta discard)
  - [internal/testsupport/integrationtest/plugins.go:286](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/testsupport/integrationtest/plugins.go#L286) (wiring adapter)
  - `internal/world/service.go:222, 244, 276` (all Service-side repository write calls to update return signatures)

### 3. PROPERTY + MOVE CAS
* **Status**: **HOLDS**
* **Verification & Rationale**: The property creation and update transaction leaks in `PropertyRepository` are resolved by replacing raw connection pool writes with transaction-enrolled `execerFromCtx(ctx, r.pool)` queries in plan `05-14-PLAN.md`. The `entity_properties` table has been successfully added to the SQL fence in `05-09-PLAN.md`. The `ObjectRepository.Move` and `CharacterRepository.UpdateLocation` signatures are updated in `repository.go:118` and `161` to accept `expectedVersion`, which is threaded directly from the initial entity read in `service.go`.
* **Citations**:
  - [internal/world/postgres/property_repo.go:60](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/property_repo.go#L60) (`PropertyRepository.Create` transaction leak to fix)
  - [internal/world/postgres/property_repo.go:136](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/property_repo.go#L136) (`PropertyRepository.Update` transaction leak to fix)
  - [internal/world/repository.go:118](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/repository.go#L118) (`ObjectRepository.Move` signature)
  - [internal/world/repository.go:161](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/repository.go#L161) (`CharacterRepository.UpdateLocation` signature)

### 4. WRITE FENCE
* **Status**: **HOLDS**
* **Verification & Rationale**: Swapping the `world.Service` repository fields to read-only views prevents direct writes inside the service layer. The AST token fence in `test/meta/world_sql_fence_test.go` acts as the mechanical backstop by verifying that raw SQL writes are confined strictly to `internal/world/postgres`. This creates a structurally sound and compile-time/static-checked write fence boundary (`INV-WORLD-4`).
* **Citations**:
  - `internal/world/service.go:45` (Service fields)
  - `test/meta/world_sql_fence_test.go` (planned AST fence)

### 5. RELAY
* **Status**: **HOLDS**
* **Verification & Rationale**: The relay skip recovery publishes an operator-authorized marker message to JetStream at the skipped sequence position, ensuring the feed remains sequential and gap-free (`INV-WORLD-3`). Database queries for the relay run entirely over the dedicated connection holding the session-level advisory lock, and a fencing generation is verified during JetStream PubAck to prevent network partition split-brain publishes. The skip CLI subcommand is wired in `cmd/holomush/outbox_admin.go`.
* **Citations**:
  - `internal/world/outbox/relay.go` (leased relay loop)
  - `cmd/holomush/outbox_admin.go` (planned skip CLI command)

### 6. CREATECHARACTER + EPOCH
* **Status**: **HOLDS**
* **Verification & Rationale**: `OutboxWriter` is threaded through `CoreServer` at `server.go:152` and instantiated through `WithOutboxWriter` in `sub_grpc.go:477`, enabling the registration-flow `createCharacterAtomic` wrapper to write its genesis snapshot in the same transaction. The `epoch` column is correctly added to `outbox` and `world_feed_counter` in migration `000050`, with a corresponding store implementation in postgres.
* **Citations**:
  - [internal/grpc/server.go:152](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/grpc/server.go#L152) (`CoreServer` struct)
  - [internal/grpc/auth_handlers.go:521](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/grpc/auth_handlers.go#L521) (`createCharacterAtomic` fallback)
  - [cmd/holomush/sub_grpc.go:477](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/cmd/holomush/sub_grpc.go#L477) (wiring option)

### 7. MEDIUM
* **Status**: **HOLDS**
* **Verification & Rationale**: The counter timeout task in `05-05-PLAN.md` implements a concrete `lock_timeout` fallback returning `WORLD_FEED_LOCK_TIMEOUT`. `INV-WORLD-1` (ATOMIC-FEED) is bound to the always-run transaction-rollback integration test in `postgres/outbox_store_test.go`. The structural census meta-test uses AST parsing to assert a bijection over the explicit `writeCommands` descriptor set on the write executor.
* **Citations**:
  - `internal/world/postgres/outbox_store_test.go` (planned INV-WORLD-1 rollback test)
  - `test/meta/world_envelope_census_test.go` (planned census bijection test)

---

## Summary & Residual Issues

No new or residual issues were found during this round-3 verification. Deleting the `EmitExamineEvent` post-commit path was verified to be completely safe, as there are no active subscribers to `object_examine` in the plugins or PWA frontend (only registration/declarations exist, which can be safely pruned). 

The package graph and interface layout converge completely with the requirements. The plans are **execution-ready**.

---

## Consensus Summary (round 3)

**Net verdict: NOT execution-ready — HIGH (Codex, source-grounded). Antigravity's "execution-ready" is a
false green (missed the guest/admin genesis bypasses).** The round-2 cycle-free seam redesign is confirmed
the right direction ("close," "salvageable"), but four load-bearing residual issues remain, plus four MEDIUM
refinements. One more targeted `/gsd-plan-phase 5 --reviews` pass is warranted. **The loop is converging** —
severity has fallen each round (round 1 = mechanism unimplementable → round 2 = import cycles → round 3 =
contract-ownership precision + two under-modeled write paths). After this incorporation, execution is the
likely next step.

### Round-2 fixes CONFIRMED holding (both reviewers)

Property transaction enrollment (`property_repo.go:60/136` → `execerFromCtx`); move CAS `expectedVersion` on
`repository.go:118/161`; numeric `INV-WORLD-1..4` bind under the parser (`invariant_registry_test.go:162`);
`WORLD_FEED_LOCK_TIMEOUT` is now a real task; the same-position skip *marker* is the right correction to the
round-2 wire-gap; the `wmodel` leaf package is a sound cycle-breaking direction (`world→wmodel`,
`postgres→world,wmodel`, `outbox→wmodel` can compile acyclic).

### Round-3 blockers (Codex, HIGH — all source-grounded)

1. **Envelope epoch/position ownership is temporally inconsistent.** 05-05 has `Finalize(intent, delta, epoch,
   position)` and says `WriteEnvelope` allocates epoch/position, but 05-06 has the executor finalize the
   envelope *before* calling the writer that allocates those storage-owned fields — a fully-finalized
   `Envelope` can't exist pre-write. Fix: `WriteIntent(ctx, intent, delta) (*wmodel.Envelope, error)` — the
   postgres impl allocates epoch/position, finalizes, persists, returns; assign each field exactly one owner.
2. **Relay `OutboxStore` can't enforce the dedicated lease connection.** `NextUnpublished`/`MarkPublished`/…
   take no lease/connection handle or fencing token, and the DB provider exposes only a general pool
   (`subsystem.go:20-23`); injecting a store doesn't prove its methods run on the lock-holding connection. A
   process-local generation can't stop an old holder publishing to NATS after PG released the session lock.
   Fix: an explicit `Lease` abstraction (`AcquireLease(ctx, gameID) → Lease` with `Generation()`), and state
   plainly that JetStream is at-least-once — `Nats-Msg-Id` + consumer idempotency is the real dedup mechanism,
   not the PG lease.
3. **Skip ownership is contradictory; the CLI can't publish the marker.** `Skip` is on the postgres
   `OutboxStore` yet must publish a NATS marker, and the CLI opens only a DB connection (05-07:118-120,
   193-210) — a store has no publisher. Fix: skip = a relay/admin *service* owning both the leased store and
   the JetStream publisher (acquire lease → validate halted row → publish same-position marker → mark resolved
   → resume); the CLI constructs that service with DB + EventBus config.
4. **Wave-1 blast radius still incomplete** (round-2 P2 partially missed). Changing repo writes to
   `(*MutationDelta, error)` + version args affects far more than 05-14 lists: `integrationtest/harness.go`
   (899-913, 1105-1112, 1191, 1366-1375), `test/integration/world/{object,exit,scene}_repo_test.go`,
   `internal/world/postgres/character_repo_test.go:250-271`, and world service tests/mocks. 05-14's
   verification runs integration compilation, so the "atomic wave" claim fails as written. Fix: a repo-wide
   mechanical caller scan into `files_modified`, or a compatibility bridge for fixtures.
5. **The writer fence misses production character writers (the most substantive catch).** Reader views only
   fence `world.Service`; concrete postgres repos stay public and callable. **Guest creation**
   (`guest_service.go:32-35/134-146`) and **bootstrap-admin creation** (`bootstrap/admin.go:85-98`) write
   characters directly, and `CharRepoAdapter` (`adapters.go:25-43`) delegates to the concrete repo — so those
   characters commit **without genesis envelopes**, contradicting "exactly one envelope per externally-visible
   command" and making **INV-WORLD-4 false**. The AST SQL fence can't catch these (SQL stays in allowlisted
   postgres). Fix: route ALL character creation (registered + guest + bootstrap-admin) through one atomic
   genesis-emitting application service; constrain concrete writer construction to composition/test packages.

### Round-3 MEDIUM (Codex)

- **INV-WORLD-1 binding is insufficient** — the rollback test writes only an envelope + rolls back; it doesn't
  prove a world *row* and envelope commit/roll back together. Bind it to a test that mutates a real world row,
  writes its envelope, forces rollback (neither survives), commits another case (both survive), and forces an
  outbox failure after the state write (state rolls back).
- **Import-graph guard is incomplete** — it forbids only 3 edges; it doesn't reject `wmodel→world/postgres/outbox`
  or `outbox→world`. `wmodel→world` would immediately recreate a cycle. Assert the full adjacency matrix
  (production imports only).
- **Genesis idempotency has no schema mechanism** — 000050 uniques on `event_id` + `(game_id, epoch,
  feed_position)`; genesis uses fresh ULIDs, so rerun differs. Add a durable genesis identity key (e.g.
  `(game_id, epoch, kind, aggregate_type, aggregate_id, genesis=true)`) or a genesis-checkpoint table inserted
  atomically with the envelope. A pre-insert existence query alone is race-prone.
- **Census completeness overstated** — the descriptor bijection proves registry consistency but can't discover
  the guest/bootstrap direct repo calls (`guest_service.go:140`, `admin.go:91`, `adapters.go:38-40`). Pair it
  with a call-graph/import restriction forbidding production packages from calling concrete world writer
  methods except approved atomic services. (Same root as blocker #5.)

### Divergent Views

Codex NOT-READY/HIGH vs Antigravity "execution-ready." **Resolution: Codex.** Antigravity's all-HOLDS table
overlaps Codex's confirmed items where checkable, but it did not surface the guest/admin genesis bypasses or
the envelope/relay ownership contradictions — its completeness claim is unsupported. This mirrors round 1
(agy over-optimistic) rather than round 2 (agy solid).

### Recommended next step

`/gsd-plan-phase 5 --reviews` (round-3 incorporation), targeted at the four blockers + four MEDIUMs. Priority:
1. **One atomic character-creation service** behind which registered/guest/bootstrap-admin creation all emit a
   genesis envelope (blocker #5 + census #4) — this is the real correctness gap.
2. **Envelope ownership**: `WriteIntent(intent, delta)` allocates epoch/position + finalizes + returns
   (blocker #1).
3. **Relay Lease abstraction** owning connection + fencing generation; skip as a service owning DB + publisher;
   at-least-once documented (blockers #2, #3).
4. **Complete the wave-1 caller inventory** via a repo-wide scan (blocker #4).
5. MEDIUMs: real INV-WORLD-1 atomicity test; full import-graph adjacency assertion; schema-backed genesis
   idempotency key.

After this pass the findings should be mechanical/verifiable enough to execute — the architecture is settled;
what remains is contract precision and write-path coverage.

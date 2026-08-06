# Phase 3: World Character Commands — Research

**Researched:** 2026-08-06
**Domain:** Go domain-layer write commands (version-guarded CAS + transactional outbox), event-driven host subsystems, NATS JetStream KV, ABAC seeds, invariant-registry amendment
**Confidence:** HIGH for in-repo seams (every claim below opened with `Read`/`rg` this session); HIGH for the nats.go KV API (read from the pinned module source in `GOMODCACHE`)

> **Provenance convention in this file.** `[VERIFIED: path:line]` means the source-of-truth file was
> opened this session and the quoted text is verbatim. `[REFUTED]` marks a CONTEXT.md code claim that
> the tree contradicts. `[NOT FOUND]` means the symbol does not exist — do not invent it.

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-31:** Two task-based commands — **`RetireCharacter`** and **`UnretireCharacter`**, neither taking a status argument. Rejected: a single `SetCharacterLifecycle(status)`.
- **D-32:** **Two new taxonomy kinds and two new census rows** — `character_retired`, `character_unretired`.
- **D-33:** Retire is a **narrow write**: `status`, version bump, and one envelope in one transaction. Session teardown and grid removal are NOT in the command's transaction — they are the reactor's job (D-36).
- **D-34:** When the retiring character is the player's `players.default_character_id`, retire **clears that pointer in the same transaction**.
- **D-35:** ~~Rename is status-agnostic.~~ **SUPERSEDED by D-44.**
- **D-36:** The fanout is an **event-driven host subsystem** consuming `character_retired` off `events.*.character.>`, NOT a synchronous call from an out-of-Service application service. Accepted consequences: `SubsystemID` cascade; at-least-once JetStream redelivery, so the handler MUST be idempotent; retirement becomes eventually consistent.
- **D-37:** A retired character lands at the **configured starting location** — the already-resolved `starting_location_id`. No new manifest key, no new schema.
- **D-38:** The move runs **through `world.Service.MoveCharacter` as a system subject** (precedent: `"system:bootstrap"`), not a direct location write. Consequence: retirement emits two envelopes (`character_retired`, then `character_moved`), and the system subject must satisfy the character-write ABAC gate.
- **Reactor v1 scope fence:** sessions only — `EmitLeave` to the OLD location, `EmitSessionEnded`, session teardown, then the move. Scenes, channels, and any future pages/stories consumers are explicitly NOT in v1.
- **D-39:** IDENT-04's "their **own** character" is enforced in **ABAC policy, not in the command**. The commands call `checkAccess` and assert no ownership predicate of their own.
- **D-40:** **Distinct ABAC actions** — `retire` and `unretire` against `CharacterResource` — not a reuse of the existing `write` action.
- **D-41:** ~~Admins may rename.~~ **DEFERRED with D-44.**
- **D-43:** Phase 3 writes **no** `idle` transition. Settled by INV-WORLD-5.
- **D-44:** **`RenameCharacter` is removed from Phase 3 and from the v0.13 milestone**, and moves to the backlog linked to Phase 999.6.
- **D-42:** `last_active_at` is written through a NATS JetStream KV buffer with a periodic flush, in its OWN general-purpose subsystem. The KV bucket MUST set `Storage: jetstream.FileStorage` explicitly. The listener covers session start/end AND character-generated activity via the `Actor.Kind == character` signal; it MUST NOT hook `internal/session/session.go:485` `RefreshConnection`. The flusher is a fourth out-of-world writer under INV-WORLD-4 and the enumeration is amended in the same change.

### Claude's Discretion

- Reactor fanout ordering: `EmitLeave` at the OLD location BEFORE the move, so the notification names the place they left.
- A new `SessionEndedCause` constant for retirement, alongside `quit | logout | guest_end | kicked | reaped | evicted`.
- Reactor idempotency by checking current state, so a JetStream redelivery no-ops rather than double-emitting `leave`.
- `UnretireCharacter` on an already-active character returns a typed error rather than silently succeeding.
- Payload and error-code shapes follow the existing `BuildCharacterUpdatePayload` / `CodeConcurrentEdit` precedent in `internal/world/service.go:799-836`.
- **Left to the planner (D-42, values not design):** the throttle window (5 min default), the flush interval, and the bucket name.

### Deferred Ideas (OUT OF SCOPE)

- **Rename + the approval dimension** (D-44) — backlog, linked to Phase 999.6.
- **Immutable core name + situational display aliases** — milestone-scale identity model.
- **Former-names reservation table.**
- **INV-WORLD-6's rename half is already false in production** — file it; not Phase 3 work.
- **Reactor consumers beyond sessions** — scenes, channels, pages/stories.
- **A `character_renamed` taxonomy kind** — withdrawn with D-44.
</user_constraints>

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| **IDENT-04** | A player can **soft-retire** their own character; the character leaves active play, its record and name are preserved, and the operation is reversible. | §2 (narrow-write CAS shape + the `s.mutator` routing that the census AST cross-check forces), §5 (the reactor that makes "leaves active play" observable), §8 (the ABAC seeds that make "their own" true) |
| **IDENT-10** | Every new character mutation carries **`expected_version`** (migration `000049`) and emits through the **transactional outbox in-transaction**. | §2 (`classifyCASZeroRow` → `world.CodeConcurrentEdit`, `mutate()` same-tx envelope), §7 (the two-replica harness that proves it) |
</phase_requirements>

---

## Summary

Phase 3 is two independent subsystems plus two domain commands, and the single highest-risk finding is
**not** the one the ROADMAP flagged. The `writeCommands` census is *two* assertions, not one: a
kind↔command bijection **and** a `go/ast` cross-check that the registered command set equals the set of
`world.Service` methods whose body references the `s.mutator` selector. That second assertion
**forces `RetireCharacter`/`UnretireCharacter` to route through `worldMutator.mutate()`** — the exact
opposite of the `CharacterRepository.Rename` precedent CONTEXT points at. Following Rename's
repo-writes-its-own-envelope shape and *also* registering a census row is a guaranteed red test.

The second highest-risk finding is the subsystem cascade. It is **not 5 sites — it is 13**, spread over
5 files, and one of them pins an **exact ordered 18-element start sequence** that two new IDs must be
inserted into at topologically-correct positions. The CONTEXT count of 18→20 is correct; the
"5-site" framing is a serious undercount that will burn a wave if a plan trusts it.

Third: D-42's stated KV failure mode is backwards. `jetstream.KeyValueConfig.Storage` zero value **is**
`FileStorage`, so an unset bucket is file-backed *everywhere*, including in the memory-configured test
harness. The decision (set it explicitly) still stands; the reasoning must be inverted, and the flusher
subsystem needs a `NewSubsystemWithStorage`-style seam so tests can force `MemoryStorage`.

**Primary recommendation:** structure the phase as three independent tracks — (A) the two domain
commands routed through `s.mutator` with a new `CharacterRepository.SetStatus`, (B) the retirement
reactor subsystem, (C) the `last_active_at` KV subsystem — and land the 13-site subsystem cascade
**once**, for both new IDs together, as a single dedicated task before (B) and (C) branch.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| `RetireCharacter` / `UnretireCharacter` authorization | ABAC engine (`internal/access/policy`) | — | D-39 puts ownership in policy; `Service.checkAccess` is the only call site |
| Status write + version CAS | Writer boundary (`internal/world/postgres`) | — | INV-WORLD-4 confines raw `characters` mutation SQL here |
| Envelope emission | Write executor (`internal/world.worldMutator.mutate`) | — | census AST cross-check forces this tier (§1) |
| `players.default_character_id` clearing | Writer boundary (`internal/world/postgres`), same tx | — | `players` is NOT a fenced world table; the reaping-guard read is the layering precedent (§2.4) |
| Retirement fanout (sessions, notify, move) | Host subsystem (`cmd/holomush` composition + a new package) | `internal/presence`, `internal/session` | D-36; must not live in `internal/world` (import-graph + gateway-boundary rules) |
| `last_active_at` buffering | JetStream KV (NATS) | — | D-42: the emit path never touches Postgres |
| `last_active_at` flush | Host subsystem → writer boundary | — | 4th out-of-world writer under INV-WORLD-4 |

---

## 1. `writeCommands` census bijection semantics — the highest-value finding

### 1.1 What the census actually is

There are **two** assertions, in `test/meta/world_envelope_census_test.go`, and they constrain
different things.

**Assertion A — the kind↔command bijection** (`TestWorldEnvelopeCensusBijection`, lines 64-109).
It is a bijection between:
- the **explicit closed descriptor set** `world.WriteCommands()` (each descriptor is a
  `{Command string; Kind string}` pair), and
- the **declared taxonomy kinds** `outbox.Kinds()`.

Three sub-checks, verbatim:

```go
require.Falsef(t, dup, "command %q is registered once (no duplicate descriptor)", c.Command)          // :77
require.Truef(t, outbox.IsDeclared(c.Kind), "command %q maps to kind %q, which MUST be declared …")   // :80-82
if other, clash := kindToCommand[c.Kind]; clash {
    t.Fatalf("kind %q has two in-Service producers (%q and %q); the in-Service bijection is one-producer-of-record",
        c.Kind, other, c.Command)                                                                      // :84-87
}
```
`[VERIFIED: test/meta/world_envelope_census_test.go:64-109]`

Plus **coverage in the other direction** (`:96-108`): every declared kind must have exactly one
registered in-Service producer, unless it is in `outOfServiceOnlyKinds()` — which today contains only
`outbox.KindCharacterGenesis` `[VERIFIED: test/meta/world_envelope_census_test.go:56-60]`.

**Assertion B — the `go/ast` cross-check** (`TestWorldEnvelopeCensusMatchesServiceMutatingMethods`,
lines 187-207). `serviceMutatingMethods` parses `internal/world/service.go` and collects every
`*Service` method whose body contains the selector expression `<recv>.mutator`
`[VERIFIED: test/meta/world_envelope_census_test.go:115-141, 162-180]`. It then asserts **set equality**
against `world.WriteCommands()`, in both directions:

```go
assert.Truef(t, ok, "world.Service.%s routes through the write executor but has no census descriptor — register it (D-01: no un-migrated command)", name)  // :197-199
assert.Truef(t, ok, "census descriptor %q is not a world.Service method routing through the executor (stale descriptor)", name)                            // :202-205
```

### 1.2 How to make the meta-test fail — reproduced in words

Five distinct failure modes, all reachable from this phase:

1. **Register a descriptor whose `Kind` is not in `outbox` registry** → `IsDeclared` false → `require.Truef` at :80.
2. **Register two descriptors sharing one `Kind`** → `t.Fatalf` at :85 ("two in-Service producers").
3. **Declare a taxonomy kind with no registered in-Service producer and no `outOfServiceOnlyKinds` entry** → `assert.Truef` at :105 ("declared kind %q has no registered in-Service write command").
4. **Add a `*Service` method that touches `s.mutator` without a descriptor** → :197.
5. **Register a descriptor for a `*Service` method whose body does NOT reference `s.mutator`** → :202 ("stale descriptor"). ← *this is the trap*

### 1.3 D-32's "forced, not chosen" claim — **VERIFIED**

CONTEXT D-32 asserts two commands cannot share a kind, and that reusing `character_updated` collides
with `UpdateCharacterDescription`.

- **Two commands cannot share a kind:** VERIFIED. Failure mode 2 above, `t.Fatalf` at line 85, message
  verbatim: `"kind %q has two in-Service producers (%q and %q); the in-Service bijection is one-producer-of-record"`.
- **`character_updated` is taken by `UpdateCharacterDescription`:** VERIFIED —
  `{Command: "UpdateCharacterDescription", Kind: kindCharacterUpdated}` `[VERIFIED: internal/world/mutator.go:97]`.
- Therefore `character_retired` + `character_unretired` are the only shape that satisfies both
  assertions with two commands. **D-32 is correct.**

### 1.4 The trap D-32 does **not** cover — routing

CONTEXT's `<code_context>` quotes `CharacterRepository.Rename`'s doc comment: *"Rename MUST NOT be
routed through `worldMutator.mutate()`"*. **VERIFIED** verbatim at
`internal/world/postgres/character_repo.go:201-205`:

> The consequence is a rule callers must honour: Rename MUST NOT be routed through
> `worldMutator.mutate()`. mutate() writes an envelope of its own from the returned delta, so routing
> Rename through it would emit TWO envelopes for one rename and break the
> exactly-one-envelope-per-command property. Phase 3's RenameCharacter calls this method directly and
> supplies the intent.

**But that rule is specific to `Rename`, and it exists only because `Rename` writes its own envelope**
— `r.outbox.WriteIntent(txCtx, intent, delta)` is inside `Rename`'s own `withTx`
`[VERIFIED: internal/world/postgres/character_repo.go:257-259]`. The reason it does so is stated at
`:191-199`: the 02-12 operator CLI `holomush character name set` calls the repo method directly from
`cmd/holomush`, so the envelope has to be written at the repo layer to cover that caller.

**Retire has no such out-of-Service caller.** And copying Rename's shape would trip census failure
mode 5: a `Service.RetireCharacter` that calls `s.characterRepo.Retire(...)` never references
`s.mutator`, so the registered `{Command: "RetireCharacter"}` descriptor fails
`TestWorldEnvelopeCensusMatchesServiceMutatingMethods` with *"census descriptor %q is not a
world.Service method routing through the executor (stale descriptor)"*.

There is a second, harder blocker: `Service.characterRepo` is typed `CharacterReader`, not
`CharacterRepository` `[VERIFIED: internal/world/service.go:101]` — the 05-11 reader-view compile
fence. A `s.characterRepo.Retire(...)` call is a **type error**, full stop. The only writer handle in
package `world` is `worldMutator.characterWriter` `[VERIFIED: internal/world/mutator.go:146]`.

> **CORRECTED GUIDANCE (supersedes the framing in the research brief):** `RetireCharacter` **MUST**
> route through `s.mutator`. The correct precedent is `UpdateCharacterDescription`
> (`internal/world/service.go:799-836`) / `DeleteCharacter` (`:745-777`), **not** `Rename`.
> The "MUST NOT route through mutate()" rule applies to `Rename` alone.

### 1.5 Exact shapes

**A `writeCommands` row** — `internal/world/mutator.go:68-76`:

```go
type WriteCommandDescriptor struct {
	Command string  // stable world.Service write-command method name
	Kind    string  // the single taxonomy kind the command's envelope declares
}
```
The set lives at `internal/world/mutator.go:85-100` and is copied out by `WriteCommands()` at
`:107-111`. `Kind` values are **local unexported string consts** in package `world`
(`internal/world/service.go:37-56`), duplicated rather than imported because
**`internal/world` MUST NOT import `internal/world/outbox`** — a forbidden import edge
`[VERIFIED: test/meta/world_import_graph_test.go:47-48, {world, outbox}]`.

**A taxonomy entry** — two pieces, both in `internal/world/outbox/taxonomy.go`:
1. the exported kind const (`:28-57`), e.g. `KindCharacterUpdated = "character_updated"`;
2. a `KindSchema` entry in the `registry` literal (`:93-120`):
   `{Kind: KindCharacterUpdated, Aggregate: wmodel.AggregateCharacter, SchemaVersion: 1, Payload: characterUpdatePayload}`.

**What Phase 3 must add, per kind (×2):**

| Site | File:line | Edit |
|---|---|---|
| Exported kind const | `internal/world/outbox/taxonomy.go:52-57` | `KindCharacterRetired = "character_retired"` (+ unretired) |
| `registry` entry | `internal/world/outbox/taxonomy.go:108-113` | `{Kind: …, Aggregate: wmodel.AggregateCharacter, SchemaVersion: 1, Payload: <schema>}` |
| Payload schema var | `internal/world/outbox/taxonomy.go:125-164` | new `[]PayloadField` (or reuse `characterUpdatePayload`'s shape) |
| Local mirror const | `internal/world/service.go:51-55` | `kindCharacterRetired = "character_retired"` (+ unretired) |
| Census descriptor | `internal/world/mutator.go:85-100` | `{Command: "RetireCharacter", Kind: kindCharacterRetired}` (+ unretire) |
| `AppSchemaVersion` | `internal/world/outbox/taxonomy.go:19` | **bump** — the doc says *"Bump it whenever the set of declared kinds … changes"* `[VERIFIED: internal/world/outbox/taxonomy.go:13-19]` |

> The `AppSchemaVersion` bump is easy to miss and is a documented obligation, not a nicety.

---

## 2. Version-guarded CAS + transactional outbox for a narrow write

### 2.1 The `mutate()` seam

```go
func (m *worldMutator) mutate(ctx, intent wmodel.EnvelopeIntent,
    write func(ctx context.Context) (*wmodel.MutationDelta, error)) (*wmodel.MutationDelta, error)
```
`[VERIFIED: internal/world/mutator.go:197-217]`. In ONE `Transactor.InTransaction` it runs the write
closure, then `m.outbox.WriteIntent(txCtx, intent, d)`. Both parameters are non-optional — an
intent-less or closure-less call does not type-check (`:180-181`).

### 2.2 What a new narrow write must replicate

Three layers. Copy `UpdateCharacterDescription` / `updateCharacter` verbatim in shape:

**(a) `Service.RetireCharacter`** — mirror `internal/world/service.go:799-836`:
1. nil-guard `s.characterRepo`;
2. `s.checkAccess(ctx, subjectID, "retire", access.CharacterResource(id.String()), prefixCharacter)`
   (D-40's new action; `prefixCharacter` is `internal/world/service.go:177`);
3. `char, err := s.characterRepo.Get(ctx, id)` — supplies the **CAS guard version** and the current status;
4. nil-guard `s.mutator`;
5. build payload (§2.5);
6. `intent := s.buildIntent(kindCharacterRetired, wmodel.AggregateCharacter, id, subjectID, payload)`;
7. `s.mutator.retireCharacter(ctx, intent, id, world.StatusRetired, char.Version)`;
8. map errors: `errors.Is(err, ErrConcurrentEdit)` → `oops.Code(CodeConcurrentEdit)`;
   `errors.Is(err, ErrNotFound)` → `CHARACTER_NOT_FOUND`.

**(b) `worldMutator.retireCharacter`** — mirror `internal/world/mutator.go:259-263`:
```go
func (m *worldMutator) retireCharacter(ctx context.Context, intent wmodel.EnvelopeIntent,
    id ulid.ULID, status Status, expectedVersion int) (*wmodel.MutationDelta, error) {
    return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
        return m.characterWriter.SetStatus(txCtx, id, status, expectedVersion)
    })
}
```

**(c) `CharacterRepository.SetStatus`** — **`[NOT FOUND]`**, must be created. There is no status
writer today: `CharacterRepository` exposes exactly `Create`, `Update`, `Rename`, `Delete`,
`UpdateLocation`, `UpdatePreferences` `[VERIFIED: internal/world/repository.go:197-244]`, and `Update`'s
doc says it "does NOT write characters.name" — it also does not touch `status`
(`internal/world/postgres/character_repo.go:150-152`: `UPDATE characters SET description = $2, location_id = $3, version = version + 1`).

Model the new method on `UpdateLocation`/`Rename`'s statement shape, **minus** the envelope write:

```go
query := `UPDATE characters SET status = $2, version = version + 1 WHERE id = $1`
args  := []any{id.String(), string(status)}
if expectedVersion > 0 { query += ` AND version = $3`; args = append(args, expectedVersion) }
query += ` RETURNING version`
// inside withTx:
//   err := tx.QueryRow(...).Scan(&newVersion)
//   errors.Is(err, pgx.ErrNoRows) -> classifyCASZeroRow(txCtx, tx,
//       `SELECT version FROM characters WHERE id = $1 FOR UPDATE`, id, CHARACTER_NOT_FOUND-wrap)
//   delta = primaryDeltaVersioned(wmodel.AggregateCharacter, id, false, newVersion-1, newVersion)
```
Pattern lifted verbatim from `[VERIFIED: internal/world/postgres/character_repo.go:225-256]`.
It must also update the mockery mock `internal/world/worldtest/mock_CharacterRepository.go`
(regenerate with `mockery`).

### 2.3 Where `WORLD_CONCURRENT_EDIT` comes from

- The typed sentinel + code live in package `world`: `world.CodeConcurrentEdit` and
  `world.ErrConcurrentEdit`, referenced from the writer boundary as
  `oops.Code(world.CodeConcurrentEdit).…Wrap(world.ErrConcurrentEdit)`
  `[VERIFIED: internal/world/postgres/character_repo.go:291-295]`.
- The zero-row classifier is `classifyCASZeroRow(txCtx, tx, lockQuery, id, notFoundErr)` — it
  distinguishes **absent row → NOT_FOUND** from **row present at a different version →
  WORLD_CONCURRENT_EDIT** `[VERIFIED: internal/world/postgres/character_repo.go:165-170, 246-251]`.
- **Convention:** `expectedVersion == 0` means an *unversioned* write (the guard clause is simply not
  appended). IDENT-10 / INV-WORLD-7 require the web mutation boundary to reject 0, but that rejection
  is the RPC layer's job (Phase 4+), not the repo's.

### 2.4 D-34 — clearing `players.default_character_id`

**FK verified:**
```sql
default_character_id TEXT,                                     -- 000001_baseline.sql:64
ALTER TABLE players ADD CONSTRAINT ...
    FOREIGN KEY (default_character_id) REFERENCES characters(id) ON DELETE SET NULL;  -- :82-84
```
`[VERIFIED: internal/store/migrations/000001_baseline.sql:64, 82-84]` — CONTEXT's claim is **VERIFIED**:
the FK self-heals on hard delete only, so a soft retire leaves a dangling pointer.

**Is a same-transaction clear feasible?** Yes, with two caveats a plan must state:

1. **Fence:** `players` is **not** in `fencedWorldTables`, which is exactly
   `locations, exits, characters, objects, entity_properties, outbox, world_feed_counter`
   `[VERIFIED: test/meta/world_sql_fence_test.go:62-65]`. An `UPDATE players …` string literal inside
   `internal/world/postgres` therefore does **not** trip the SQL fence.
2. **Layering:** there is an explicit, documented precedent for touching the auth `players` table on
   the world tx connection — `internal/world/postgres/reaping_guard.go`'s
   `SELECT reaping_at … FOR UPDATE`, described as *"an intentional, durable
   auth-table-read-on-the-world-conn exception a future layering pass MUST NOT 'fix away'"*
   `[VERIFIED: test/meta/world_sql_fence_test.go:49-56]`. **That precedent is a READ.** D-34 extends it
   to a WRITE. This is a genuine widening and should be documented in the same change (a comment at the
   new site + a note in the fence test's doc block), or it will read as an accidental layering
   violation to the next reviewer.

**Suggested statement** (idempotent, no read-modify-write, safe under any caller):
```sql
UPDATE players SET default_character_id = NULL WHERE default_character_id = $1
```

> **Two-pool note.** The auth `players` repo and the world repos hold **different `*pgxpool.Pool`
> handles to the same database** — `internal/auth/character_reaping.go:108-116` calls this "the
> two-pool boundary" and explicitly does NOT claim atomicity across it. The reaping guard proves a
> world-tx connection can reach `players`, so a *single-connection* same-tx write is possible; the
> plan must do it on the **world tx connection** (`txFromContext(txCtx)`), never through the auth pool.

### 2.5 Payload

Follow `BuildCharacterUpdatePayload` (`internal/world/service.go:818`). The declared
`characterUpdatePayload` is `{character_id: ulid, description: string}`
`[VERIFIED: internal/world/outbox/taxonomy.go:156-159]` — not reusable. Declare a new
`characterLifecyclePayload`, e.g. `{character_id: ulid, status: string}`, new-values-only and
erasure-safe (the registry's stated rule, `internal/world/outbox/taxonomy.go:122-124`).

---

## 3. The subsystem cascade — **13 sites, not 5**

### 3.1 Current count: 18

`SubsystemID` const block `[VERIFIED: internal/lifecycle/subsystem.go:16-50]`, in order:
`Database, TLS, ABAC, Auth, World, Plugins, Sessions, Bootstrap, GRPC, EventBus, AuditProjection,
Cluster, AdminSocket, CryptoChainVerifier, CryptoPolicy, RekeyCheckpointSweep, OutboxRelay,
CharacterNameBlockList` = **18** (indices 0-17). CONTEXT's "18→20" is **VERIFIED**.

The block carries its own instruction verbatim: *"New ids go at the END of this block: the stringer is
linecomment-driven, so an insertion mid-block silently renumbers every later id."*
`[VERIFIED: internal/lifecycle/subsystem.go:47-48]`

### 3.2 The complete site list — **REFUTES the "5-site" framing**

| # | File:line | What |
|---|---|---|
| 1 | `internal/lifecycle/subsystem.go:49` | Add the 2 new IDs at the **END** of the const block |
| 2 | `internal/lifecycle/subsystemid_string.go:31,34,36` | **Regenerate** — `task generate`. Contains `_ = x[SubsystemCharacterNameBlockList-17]`, the packed `_SubsystemID_name` string, and a 19-element `_SubsystemID_index` array |
| 3 | `cmd/holomush/core.go:1188-1210` | `productionSubsystemSet` struct — add 2 **named** fields |
| 4 | `cmd/holomush/core.go:1218-1260` | `productionSubsystems()` return slice — add `s.X` entries |
| 5 | `cmd/holomush/core.go:856` | The real `productionSubsystems(productionSubsystemSet{…})` composite literal in the wiring |
| 6 | `cmd/holomush/core_subsystems_test.go:63` | `func allStubs() [18]stubSubsystem` — **fixed-size array in the signature** |
| 7 | `cmd/holomush/core_subsystems_test.go:64` | `return [18]stubSubsystem{…}` — the literal, + 2 new entries |
| 8 | `cmd/holomush/core_subsystems_test.go:88` | `func setFromStubs(s [18]stubSubsystem)` — signature + the `:89-108` index→field mapping |
| 9 | `cmd/holomush/core_subsystems_test.go:126-127` | `if len(subs) != 18 { … want 18 … }` |
| 10 | `cmd/holomush/core_subsystems_test.go:292-293` | second `if len(subs) != 18 { … }` |
| 11 | `cmd/holomush/core_subsystems_test.go:134-152` | `TestSubsystemAdminSocketConstantExists`'s distinctness `ids := []lifecycle.SubsystemID{…}` list |
| 12 | `cmd/holomush/core_subsystems_test.go:390-427` | `realProductionSubsystemGraphForPropertyTest` — constructs **real** subsystem types + `require.Len(t, graph, 18, …)` |
| 13 | `cmd/holomush/core_topo_order_test.go:126-165` | `realProductionSubsystemGraph` — real constructors + `require.Len(t, graph, 18, …)` |
| 13b | `cmd/holomush/core_topo_order_test.go:194-213` | **`want []lifecycle.SubsystemID`** — an **exact ordered 18-element pinned start sequence** |

`[VERIFIED: all of the above opened this session]`

**The comment at `core_subsystems_test.go:60-62` states the hazard verbatim:** *"NOTE: the array type
is FIXED-SIZE. Adding a subsystem without widening all three occurrences below is a COMPILE error in
this package, not a failing length assertion."*

**Site 13b is the subtlest.** `TestProductionSubsystemsTopologicalStartOrderIsPinned` asserts the exact
ordered output of `Orchestrator.StartAll` over the real dep graph. `topoSort`'s queue tie-break is a
deterministic `sort.Slice` by `SubsystemID` `[VERIFIED: cmd/holomush/core_topo_order_test.go:180-182]`,
so two new IDs appended at the end of the iota block will sort **last within their ready-tier**. The
plan must compute where they land, not guess.

**Sites 12 and 13 also require real constructors**: both build the subsystem list from real
`New…Subsystem(…Config{})` calls that must allocate nothing and touch no live resources
`[VERIFIED: cmd/holomush/core_topo_order_test.go:113-121]` — *"None of these constructors allocate or
touch live resources (07-09 D-12 Wave A made every constructor allocate nothing precisely so this is
possible)"*. Both new subsystem constructors MUST honour that.

**Model to copy:** `internal/world/setup/relay_subsystem.go` (`worldsetup.NewOutboxRelaySubsystem`) —
the most recent registered subsystem with a `DependsOn(Database, EventBus)` shape.

### 3.3 Prepare/Activate

The `Subsystem` interface is `ID / DependsOn / Prepare / Activate / Stop`
`[VERIFIED: internal/lifecycle/subsystem.go:65-143]`. Disposition for the two new ones:

| Subsystem | Prepare | Activate |
|---|---|---|
| Retirement reactor | create the durable JetStream consumer (acquisition, no domain traffic) | start `Consume` (a work loop carrying domain traffic) |
| `last_active_at` KV | create/attach the KV bucket + the durable listener consumer | start the listener + the periodic flush ticker |

This mirrors `audit.Subsystem` exactly and satisfies the Prepare/Activate contract's own carve-out for
process-internal substrate (`internal/lifecycle/subsystem.go:78-87`).

---

## 4. JetStream KV — the repo's first use

### 4.1 Pinned version & absence of precedent

- `github.com/nats-io/nats.go v1.52.0` `[VERIFIED: go.mod:22]`
- `github.com/nats-io/nats-server/v2 v2.14.3` `[VERIFIED: go.mod:21]`
- **Zero** existing `KeyValue` / `CreateKeyValue` / `KeyValueConfig` usage in the tree — the only
  `KeyValue` hits are `attribute.KeyValue` (OTel) and `ast.KeyValueExpr`
  `[VERIFIED: rg over `**/*.go`]`. **D-42's "first use of JetStream KV in the codebase" is VERIFIED.**

### 4.2 The API, read from the pinned module source

Constructors on `jetstream.JetStream` `[VERIFIED: $GOMODCACHE/github.com/nats-io/nats.go@v1.52.0/jetstream/kv.go:40-65]`:

```go
KeyValue(ctx context.Context, bucket string) (KeyValue, error)             // :40 — open existing; ErrBucketNotFound
CreateKeyValue(ctx context.Context, cfg KeyValueConfig) (KeyValue, error)  // :47 — ErrBucketExists if present
UpdateKeyValue(ctx context.Context, cfg KeyValueConfig) (KeyValue, error)  // :54
CreateOrUpdateKeyValue(ctx context.Context, cfg KeyValueConfig) (KeyValue, error) // :59  ← use this (idempotent Prepare)
DeleteKeyValue(ctx context.Context, bucket string) error                   // :65
```

`KeyValueConfig` fields relevant here `[VERIFIED: .../jetstream/kv.go:210-275]`:

| Field | Type | Default | Note |
|---|---|---|---|
| `Bucket` | `string` | — | `^[a-zA-Z0-9_-]+$` only (`validBucketRe`, kv.go:501) |
| `Description` | `string` | — | |
| `History` | `uint8` | 1 | max 64 |
| `TTL` | `time.Duration` | 0 (no expiry) | per-bucket key expiry |
| `MaxBytes` | `int64` | -1 | |
| `MaxValueSize` | `int32` | -1 | |
| **`Storage`** | `StorageType` | **`FileStorage`** | see §4.3 |
| `Replicas` | `int` | 1 | |
| `LimitMarkerTTL` | `time.Duration` | 0 | required for per-key TTL; needs server API level ≥1 |

`KeyValue` operations `[VERIFIED: .../jetstream/kv.go:92-207]`:
```go
Get(ctx, key) (KeyValueEntry, error)                       // ErrKeyNotFound if absent
Put(ctx, key string, value []byte) (uint64, error)         // upsert, returns revision
Create(ctx, key, value, opts...) (uint64, error)           // ErrKeyExists if present
Update(ctx, key, value []byte, revision uint64) (uint64, error)  // CAS on revision
Delete(ctx, key, opts...) error                            // tombstone, keeps revisions
Purge(ctx, key, opts...) error                             // destructive
Keys(ctx, opts...) ([]string, error)                       // loads ALL keys into memory
ListKeys(ctx, opts...) (KeyLister, error)                  // streaming — PREFER for the flusher
Watch(ctx, keys string, opts...) (KeyWatcher, error)
Status(ctx) (KeyValueStatus, error)
```

> `ListKeys`' own doc warns: *"On buckets with a large number of keys and frequent writes, duplicate
> keys may be reported during listing."* `[VERIFIED: .../jetstream/kv.go:185-188]` — the flusher must be
> duplicate-tolerant, which the idempotent `UPDATE` predicate in §4.4 already gives it for free.

### 4.3 D-42's storage claim — **half VERIFIED, half REFUTED**

**VERIFIED:** a KV bucket carries its own storage config and does not inherit the stream's.
`prepareKeyValueConfig` builds a brand-new `StreamConfig` with `Storage: cfg.Storage`
`[VERIFIED: .../jetstream/kv.go:668-691, field `Storage: cfg.Storage` at :674]`.

**REFUTED — the failure mode is inverted.** `Storage`'s zero value **is** `FileStorage`:
```go
// FileStorage specifies on disk storage. It's the default.
FileStorage StorageType = iota   // stream_config.go:610-611
MemoryStorage                     // :613
```
`[VERIFIED: $GOMODCACHE/…/jetstream/stream_config.go:610-613]`, and the `KeyValueConfig.Storage`
doc says *"If not specified, the default is FileStorage"* `[VERIFIED: .../jetstream/kv.go:234-236]`.

So CONTEXT's sentence *"A bucket left at the default in a memory-configured test harness will silently
lose unflushed writes"* is **false** — a defaulted bucket is **file-backed everywhere**. The real
hazard is the mirror image:

> In a `NewSubsystemWithStorage(cfg, jetstream.MemoryStorage)` test harness, a KV bucket left at the
> default silently becomes **file-backed** and writes into the embedded server's `StoreDir` — leaking
> on-disk state across test runs and re-opening a stale bucket whose config no longer matches (which is
> what `CreateKeyValue`'s `ErrStreamNameAlreadyInUse` reconciliation path at kv.go:539-561 exists for).

**The decision stands, the reasoning inverts.** Concrete guidance for the plan:
- production wires `Storage: jetstream.FileStorage` **explicitly** (self-documenting, immune to an
  upstream default flip);
- the new subsystem exposes a `New…WithStorage(cfg, storage jetstream.StorageType)` seam mirroring
  `eventbus.NewSubsystemWithStorage` `[VERIFIED: internal/eventbus/subsystem.go:68-70]`, so integration
  tests can force `MemoryStorage` and leave no `StoreDir` residue;
- use `CreateOrUpdateKeyValue`, not `CreateKeyValue`, so `Prepare` stays idempotent (the interface
  requires it — `internal/lifecycle/subsystem.go:105-109`).

**Two CONTEXT line-citations are off:**
- `internal/eventbus/subsystem.go:63-65` — **VERIFIED**, verbatim:
  `// NewSubsystem constructs the subsystem from a Config. / // FileStorage is the default; tests override via NewSubsystemWithStorage. / func NewSubsystem(cfg Config) *Subsystem {`
- `":214-222 resolveStoreDir"` — **partially wrong.** Line 214 is the *call* (`storeDir, err := s.resolveStoreDir()`) inside `connectEmbedded`; the function itself is at
  `[VERIFIED: internal/eventbus/subsystem.go:490-501]`. `StoreDir` is set on `server.Options` at `:222`.

### 4.4 The flush shape — **VERIFIED as correct**

Column: `last_active_at BIGINT NOT NULL DEFAULT 0`
`[VERIFIED: internal/store/migrations/000054_character_identity_and_lifecycle.sql:32-33]`, with the
migration's own comment: *"Last-active time as BIGINT epoch NANOSECONDS, never a time-zone-carrying SQL
type (INV-STORE-1). 0 is the Unix epoch and is the never-active sentinel"*. Mirrored in Go as
`world.NeverActive int64 = 0` `[VERIFIED: internal/world/lifecycle.go:39]`.

The monotonic-guard predicate is correct and idempotent under redelivery:
```sql
UPDATE characters SET last_active_at = $2 WHERE id = $1 AND last_active_at < $2
```
No read-modify-write, no version bump (this is an **operational** column, not world state — do **not**
route it through `mutate()`, or the census AST cross-check fires and the world feed gains a spurious
envelope per flush).

> **Fence collision — plan-blocking.** `characters` **is** a fenced world table
> `[VERIFIED: test/meta/world_sql_fence_test.go:62-65]`, so this `UPDATE characters …` string literal
> **must live inside `internal/world/postgres`** — that is the only allowlisted directory
> (`writerBoundaryDir`, `test/meta/world_sql_fence_test.go:132-141`). The flusher subsystem calls a new
> exported writer-boundary function (precedent: `worldpostgres.BackfillCharacterIdentity`, the free
> function migration 000055 calls for exactly this reason —
> `[VERIFIED: test/meta/world_import_graph_test.go:92-102]`). The flusher's own package is **not** on
> the composition allowlist, so either (a) it takes the writer as an injected interface wired from
> `cmd/holomush` (already allowlisted), or (b) `internal/world/setup` (allowlisted) hosts it.
> Option (a) matches the repo's dependency-injection habit; option (b) is fewer moving parts.

**Bucket key/value shape (planner's call, but constrained):** key must match
`^[-/_=\.a-zA-Z0-9]+$` (`validKeyRe`, kv.go:502) — a bare ULID qualifies. Value: the epoch-nanos
int64, either raw or a tiny JSON. `History: 1` (only the latest matters). Consider `TTL` ≈ a small
multiple of the flush interval so a crashed flusher does not accumulate keys forever, but note
per-key TTL needs `LimitMarkerTTL` + server API level ≥1 (`kv.go:657-667`).

### 4.5 The listener

D-42 says the listener keys off `Actor.Kind == character` on a broad subscription, "same subscription
shape the audit projector uses". Verified substrate: the audit projector filters
`eventbus.SubjectFilter` = `"events.>"` over `eventbus.StreamName` = `"EVENTS"`
`[VERIFIED: internal/eventbus/subsystem.go:25-28; internal/eventbus/audit/projection.go:110-113]`.
The actor kinds are `eventbus.ActorKindUnknown/Character/Player/System/Plugin` (mapped from
`core.ActorCharacter/ActorSystem/ActorPlugin` by `internal/presence/emitter.go:110-121`).

---

## 5. The retirement reactor

### 5.1 `createConsumerWithRetry` — **VERIFIED shared**

```go
func createConsumerWithRetry(ctx context.Context,
    create func(context.Context) (jetstream.Consumer, error)) (jetstream.Consumer, error)
```
`[VERIFIED: internal/eventbus/audit/projection.go:167-188]`. Its doc says verbatim: *"Shared by
newProjection and PluginConsumerManager.Add."* `[VERIFIED: internal/eventbus/audit/projection.go:162-163]`

> **It is UNEXPORTED (lowercase `c`).** The reactor, living in a different package, **cannot call it**.
> CONTEXT's *"the reactor's JetStream consumer is a third user"* is therefore **not achievable as
> written** without one of: exporting it, moving it to a shared helper package, or re-implementing the
> ~20-line bounded retry. Budget a task for this. The retry schedule is
> `consumerCreateBackoffs = {100ms, 250ms}` `[VERIFIED: projection.go:93-96]`, declared `var` (not
> `const`) so tests can shorten it.

The consumer config to copy `[VERIFIED: internal/eventbus/audit/projection.go:110-126]`:
```go
js.CreateOrUpdateConsumer(ctx, eventbus.StreamName, jetstream.ConsumerConfig{
    Durable: cfg.ConsumerName, Name: cfg.ConsumerName,
    FilterSubject: <"events.*.character.>">,       // narrower than the projector's "events.>"
    AckPolicy: jetstream.AckExplicitPolicy,
    AckWait: …, MaxAckPending: …, MaxDeliver: …,
})
```

### 5.2 Subject + type — **VERIFIED**

`EnvelopeToEvent` builds the subject as `eventbus.Qualify(env.GameID, "<aggregateType>.<aggregateID>")`
and sets `Event.Type = env.Kind`
`[VERIFIED: internal/world/outbox/wire.go:154-170]`. `Qualify` prepends `events.<gameID>.`
`[VERIFIED: internal/eventbus/qualify.go:23]`. So a retire envelope lands on
`events.<game_id>.character.<charULID>` with `Type == "character_retired"` — the D-36 filter
`events.*.character.>` and the "switch on `Type`" shape are both correct.

**Timing consequence:** the envelope reaches JetStream via the **outbox relay** (`SubsystemOutboxRelay`),
not the command. The reactor is therefore downstream of the relay — genuinely eventually consistent, as
D-36 accepts.

### 5.3 Idempotency, per effect

At-least-once redelivery means each effect needs an idempotent formulation:

| Effect | Idempotent formulation | Substrate |
|---|---|---|
| **End sessions** | `Store.DeleteByCharacter(ctx, characterID) (*Info, error)` — returns the deleted `*Info`, or nothing to do on redelivery | `[VERIFIED: internal/session/session.go:306, 428]` |
| **Notify the old location** | Gate on the `*Info` returned above: no session ⇒ no `EmitLeave`. The `LocationID` for the leave comes from that `Info`, so a redelivery that finds nothing cannot double-emit | `internal/presence/emitter.go:148-168` |
| **Move to start** | `MoveCharacter` is inherently idempotent-ish: after the first move `char.LocationID == startLoc`, so re-running is a no-op write that still emits a second `character_moved` envelope. **Guard explicitly**: read the character first and skip when already at `starting_location_id` | `internal/world/service.go:985-1048` |

Also add a **status guard**: read `characters.status`; if it is no longer `retired` (someone
un-retired between delivery and processing), the reactor should ack-and-skip rather than evict a
now-active character. `world.ParseStatus` / `world.Selectable`
`[VERIFIED: internal/world/lifecycle.go:43-62]` are the exhaustive-read helpers INV-WORLD-5 requires.

### 5.4 The existing leave fanouts — **CONTEXT UNDERCOUNTS; "four" is REFUTED**

There are **seven** production `EmitLeave` call sites, not four
`[VERIFIED: rg over `**/*.go` excluding `_test.go`]`:

| # | Site | Reason string | Paired `EmitSessionEnded`? |
|---|---|---|---|
| 1 | `internal/auth/auth_service.go:248` | `"evicted"` | yes, `:256` (`SessionEndedCauseEvicted`) |
| 2 | `internal/grpc/lifecycle_handler.go:199` | `"quit"` | yes, `:207` (`SessionEndedCauseGuestEnd`) |
| 3 | `internal/grpc/lifecycle_handler.go:252` | `"phased out"` | no |
| 4 | `cmd/holomush/sub_grpc.go:844` (`OnExpired`) | `"session expired"` | yes, `:852` (`SessionEndedCauseReaped`) |
| 5 | `cmd/holomush/sub_grpc.go:870` (`OnGridPhaseOut`) | `"phased out"` | no |
| 6 | `internal/grpc/command_handler.go:268` | `"quit"` | yes, `:271` |
| 7 | `internal/grpc/command_handler.go:330` | `"booted"` | yes, `:335` |
| 8 | `internal/grpc/auth_handlers.go:716` | `"logout"` | yes, `:720` (`SessionEndedCauseLogout`) |

(That is eight `EmitLeave` sites across seven distinct flows; the reactor is the **ninth**.)

**Reuse vs duplicate:**
- **Reuse:** `presence.Emitter` itself. Its two methods are exactly the narrow interface
  `internal/auth/auth_service.go:26-29` already declares consumer-side:
  ```go
  EmitLeave(ctx context.Context, char core.CharacterRef, reason string) error
  EmitSessionEnded(ctx context.Context, char core.CharacterRef, sessionID, cause, reason string) error
  ```
  `[VERIFIED: internal/auth/auth_service.go:26-29]` — copy that consumer-defined-interface shape rather
  than importing `internal/presence` directly (it is the in-repo pattern for breaking the cycle).
- **Duplicate (unavoidably):** the `core.CharacterRef{ID, Name, LocationID}` construction and the
  log-warn-on-error handling — every one of the eight sites hand-rolls it. There is **no** shared
  "end this session and announce it" helper `[NOT FOUND]`. The reactor makes nine hand-rolls; a plan
  *may* extract one, but that is a refactor beyond the phase fence.
- **New:** a `SessionEndedCauseRetired = "retired"` const alongside the six existing
  `[VERIFIED: internal/core/session_ended_payload.go:24-31]`. Note the struct's field comment
  (`:20`) lists the causes inline — update it in the same edit.

### 5.5 D-37 / D-38 — starting location + system subject

**Starting location.** `starting_location_id` is resolved at bootstrap and stored in the metadata
store:
```go
loc, findErr := b.worldService.FindLocationByName(ctx, "system:bootstrap", manifest.Setting.StartingLocation)
…
b.metadataStore.Set(ctx, "starting_location_id", loc.ID.String())
```
`[VERIFIED: internal/bootstrap/setting.go:133-141]` — CONTEXT's `:133-144` citation is accurate.
Exposed by `func (s *BootstrapSubsystem) StartLocationID() ulid.ULID`, which **panics if called before
`Prepare()`** `[VERIFIED: internal/bootstrap/setup/subsystem.go:299-303]`. The reactor must therefore
declare `DependsOn(SubsystemBootstrap)` or resolve lazily.

**System-subject precedent.** `"system:bootstrap"` is a raw string literal at
`internal/bootstrap/setting.go:134`. There is **no `access.SystemSubject(...)` helper** `[NOT FOUND]`.
Two distinct system paths exist and the plan must pick one deliberately:

| Path | Mechanism | Verified at |
|---|---|---|
| **Prefixed** `"system:<x>"` | `parseEntityType` splits on `:` → `"system"` → matches `principal is system` in the DSL | `internal/access/policy/engine.go:542-548, 559-562` |
| **Bare** `"system"` | **hard bypass**, `EffectSystemBypass`, but only if `access.IsSystemContext(ctx)`; otherwise a hard `SYSTEM_SUBJECT_REJECTED` error | `internal/access/policy/engine.go:91-104`; `internal/access/context.go:16-18` |

> `access.ParseEntityRef` (`internal/access/prefix.go:290-320`) matches `system` only on **exact
> equality** and would reject `"system:bootstrap"` — but it has **zero production callers**
> `[VERIFIED: rg]`, so it is not on this path. Do not reason from it.

**The ABAC gap D-38 flags is REAL and unpatched.** Existing system seeds cover **location and exit
only**:
```
seed:system-bootstrap-world  — permit(principal is system, action in ["read","write"], resource is location);
seed:system-bootstrap-exits  — permit(principal is system, action in ["read","write"], resource is exit);
```
`[VERIFIED: internal/access/policy/seed.go:206-217]`. There is **no** system→character permit.
`MoveCharacter` checks `"write"` on `access.CharacterResource(id)`
`[VERIFIED: internal/world/service.go:989-992]`, so a `"system:retirement"` subject is **denied today**.
Phase 3 must add a seed — recommended minimal grant:

```
seed:system-retirement-character
permit(principal is system, action in ["read", "write"], resource is character);
```

**Movement pipeline.** `MoveCharacter` reads the character (CAS version + from-location), verifies the
destination exists, routes `moveCharacter` through `mutate()`, then fires
`s.movementHook.OnCharacterMoved(...)` **post-commit**, treating a hook failure as operational
degradation (log + metric, command success) `[VERIFIED: internal/world/service.go:1035-1045]`. This is
the `MoveCharacter → MovementHook → UpdateLocationOnMove` pipeline that issue #4788 records as
covered only by simulated moves — the reactor genuinely exercises it. **VERIFIED.**

---

## 6. INV-WORLD-4 amendment + INV-WORLD-6's defect

### 6.1 INV-WORLD-4 today

`[VERIFIED: docs/architecture/invariants.yaml:5057-5083]`. `binding: bound`, `asserted_by`:
```
internal/world/outbox/writer_boundary_test.go
test/meta/world_sql_fence_test.go
test/meta/world_import_graph_test.go
test/integration/auth/guest_reaper_tombstone_test.go
cmd/holomush/cmd_character_name_integration_test.go
```
Its summary enumerates *"exactly THREE sanctioned out-of-world writers, each emitting its envelope
atomically"*: (1) the character-genesis application service (05-15, CREATION), (2) the
character-reaping application service (05-16, DELETION), (3) the operator name-resolution command
`holomush character name set` (02-12, RENAME).

**The 02-12 amendment precedent is stated in the entry itself** — the sentence to copy the shape of:

> *"The count was TWO until 02-12 shipped the third writer; it is amended deliberately rather than the
> atomicity clause being weakened, because what was false was the enumeration and not the guarantee."*

The amendment landed in commit `2d9bdab52` (`feat(v0.13): phase 2 — ABAC & schema vocabulary,
character-name admission, identity schema (#4941)`) `[VERIFIED: git log -S]`.

### 6.2 The Phase 3 amendment

Two things must change in `docs/architecture/invariants.yaml`, plus a regeneration:

1. **THREE → FOUR**, adding the `last_active_at` flusher, with a per-writer clause naming why it is
   sanctioned and *how it is different*: it is the first sanctioned writer that emits **no envelope**,
   because `last_active_at` is an operational column and not world state. That is a genuine widening of
   the invariant's shape, not just its count — the existing text says *"each emitting its envelope
   atomically"*, which the flusher does **not** do.

   > **Planner decision required.** Either (a) amend the clause to "each world-STATE writer emits its
   > envelope atomically; the operational-column writer is exempt and named", or (b) argue the flusher
   > is out of INV-WORLD-4's scope entirely. CONTEXT explicitly defers the deeper
   > "should the fence distinguish world-state from operational writers" question to a separate todo —
   > so option (a), the minimal honest amendment, is the phase-scoped answer.

2. **Trailing count sentence**, mirroring 02-12's: *"The count was THREE until 03-xx shipped the
   fourth writer; …"*.

3. `go run ./cmd/inv-render` to regenerate `docs/architecture/invariants.md`, then
   `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`
   `[per .claude/rules/invariants.md]`.

4. **`asserted_by` addition.** The flusher's write is asserted by whichever new integration test proves
   the flush lands. Do NOT add a `// Verifies:` to a test that only touches the code — the registry's
   own rule (`.claude/rules/invariants.md`) forbids a fabricated binding.

**Which test binds INV-WORLD-4 for the new writer:** the SQL fence (`test/meta/world_sql_fence_test.go`)
already binds the mechanical half and needs no change **provided** the flusher's `UPDATE characters`
lives in `internal/world/postgres` (§4.4). The *enumeration* half needs a new integration test in the
style of `test/integration/auth/guest_reaper_tombstone_test.go`.

### 6.3 INV-WORLD-6 — the defect is **CONFIRMED**

Current registry state `[VERIFIED: docs/architecture/invariants.yaml:5095-5107]`:

- `summary`: *"RETIRE-PRESERVES-NAME: retiring a character leaves its row and its name reservation
  intact; a character name becomes claimable again **only through a sanctioned tombstone-emitting hard
  delete, and there are exactly TWO such paths** — `world.Service.DeleteCharacter` … and
  `auth.CharacterReapingService` …"*
- `binding: bound`
- `asserted_by: [test/integration/world/character_lifecycle_test.go]`

**The rename half is false.** `CharacterRepository.Rename` writes `name`, `normalized_name`,
`name_skeleton`, `name_skeleton_unicode_version` in one statement
`[VERIFIED: internal/world/postgres/character_repo.go:225-235]`, freeing the old normalized name with
no tombstone — and the shipped operator CLI `cmd/holomush/cmd_character_name.go:405` calls it. That CLI
is itself enumerated in INV-WORLD-4 as the third sanctioned out-of-world writer (RENAME), so the
registry contradicts itself across two entries.

**The binding test does not exercise rename.** `rg -c "Rename|rename"
test/integration/world/character_lifecycle_test.go` returns **no matches**
`[VERIFIED: rg]`; the `// Verifies: INV-WORLD-6` block at `:213-219` asserts only retire-then-reclaim
FAILS plus the two delete paths releasing the name. So nothing goes red today.

**Disposition:** file a GitHub issue (`gh issue create -R holomush/holomush --label bug`). **The retire
half of INV-WORLD-6 is unaffected and stands** — Phase 3's soft retire writes only `status`, never the
name columns. Phase 3 should NOT attempt the fix; that is rename's territory (999.20).

---

## 7. The two-replica resilience harness

| Property | Value |
|---|---|
| Location | `test/integration/resilience/` (7 files) |
| Package | `package resilience_test` |
| Build tag | `//go:build integration` `[VERIFIED: test/integration/resilience/resilience_suite_test.go:4]` |
| Entry point | `func TestWorldModelResilience(t *testing.T)` (Ginkgo `RunSpecs`) `[VERIFIED: :47-56]` |
| Gate | **`quarantinetest.Enabled()`** — skips unless `HOLOMUSH_RUN_QUARANTINED=1` `[VERIFIED: :48-50]` |
| Substrate | N in-process `CoreServer` replicas over ONE real NATS JetStream container (`internal/testsupport/natstest`) + ONE shared Postgres `[VERIFIED: :6-16]` |
| Selection | `task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` `[VERIFIED: :44-45]` |

**Is it in `task test:int`'s package list?** — **the premise is stale.** `test:int` has **no package
enumeration**: it runs `{{.CLI_ARGS | default "./..."}}`
`[VERIFIED: Taskfile.yaml:277-289]`, with the comment *"No package enumeration: … `./...` compiles
cleanly under `-tags=integration`."* So the suite **compiles** on every `task test:int` and **skips**
unless the env var is set. It is deliberately NOT part of the required Integration Test PR gate; it
runs on the nightly Quarantine Health lane.

**What "pointing it at the new commands" concretely requires:**

1. A new `Describe` in `test/integration/resilience/`, modelled on
   `m12_lastwritewins_test.go`'s spec 1 ("deterministic interleave (location, service level) — both
   replicas read the same version, one commits, the stale writer is REJECTED with
   `WORLD_CONCURRENT_EDIT`") `[VERIFIED: m12_lastwritewins_test.go:41-56]`, retargeted from
   `UpdateLocation` to `RetireCharacter`.
2. **No quarantine-marker patterns** may appear in any file in that package — the suite doc forbids it
   explicitly (`resilience_suite_test.go:20-27`), because it gates on `quarantinetest.Enabled()` alone
   and a marker would trip the bijection meta-test at `test/meta/quarantine_registry_test.go:31`.
3. Read-backs go **straight to the shared pgxpool**, never through sessions or subscriber frames
   (`m12_lastwritewins_test.go:55-56`, "RESEARCH Pitfall 6").
4. `const m12SpecTimeout = 3 * time.Minute` is the in-file budget precedent.

---

## 8. ABAC + command surface

### 8.1 Action vocabulary is open

`types.NewAccessRequest` validates only non-emptiness of subject/action/resource plus reserved
attribute keys `[VERIFIED: internal/access/policy/types/types.go:143-173]`. The
`ActionRead/Write/Delete/Enter/Execute/Emit/Use` consts (`:91-99`) are conveniences, **not** a closed
set. **`retire` / `unretire` are legal action strings with no vocabulary change required.**
The engine's `Evaluate` path likewise does not enumerate actions.

### 8.2 What today's seeds already do

`[VERIFIED: internal/access/policy/seed.go]`

| Seed | DSL | Effect on retire/unretire |
|---|---|---|
| `seed:admin-full-access` (`:105-109`) | `permit(principal is character, action, resource) when { "admin" in principal.character.roles };` | **Bare `action`** ⇒ admins already get `retire` and `unretire` for free. **No new admin seed needed.** |
| `seed:player-self-access` (`:39-43`) | `permit(principal is character, action in ["read","write"], resource is character) when { resource.character.id == principal.character.id };` | Does **not** cover `retire`/`unretire` — a player self-retire needs this widened or a new seed. |
| `seed:system-bootstrap-world` / `-exits` (`:206-217`) | `principal is system` on `location` / `exit` | **No system→character permit exists.** §5.5. |

### 8.3 Exactly what this phase adds

| Seed | DSL | Why |
|---|---|---|
| `seed:player-self-retire` (**conditional — see the open question**) | `permit(principal is character, action in ["retire"], resource is character) when { resource.character.id == principal.character.id };` | IDENT-04's "their own character", D-39/D-40. **Deliberately omits `unretire`** so D-40's admin-only-unretire future is expressible. |
| `seed:system-retirement-character` | `permit(principal is system, action in ["read","write"], resource is character);` | D-38's reactor move. **Required** — the reactor is denied without it. |

`SeedPolicy` shape: `{Name, Description, DSLText, SeedVersion}` `[VERIFIED: internal/access/policy/seed.go:39-43]`.
Seed-provider coverage is validated at construction by `validateSeedProviderCoverage`
`[VERIFIED: internal/access/setup/seed_coverage.go:43-60]` — it scans for
`(principal|resource).<ns>.<attr>` refs and fails loudly on an unregistered namespace. Both proposed
seeds use only `resource.character.id` / `principal.character.id`, namespaces already registered.

### 8.4 Open question — player self-retire is not settled by the code

The research brief flags it and the tree confirms it: **every sketched retire path is
`AdminRetireCharacter`** (CONTEXT D-31 quotes sketch 004 as pinning *"sends `AdminRetireCharacter` /
`AdminUnretireCharacter`"*), yet IDENT-04's text is *"A player can soft-retire **their own**
character."* The code cannot settle which is true — no RPC exists for either yet.

**Recommendation:** ship **both** seeds (`seed:player-self-retire` + admin via the existing
`seed:admin-full-access`) and let the RPC surface decide in Phase 6. Shipping the player seed costs
nothing and discharges IDENT-04's literal wording; withholding it means IDENT-04 closes on an
admin-only path, which contradicts the requirement text. **Flag for user confirmation.** `[ASSUMED]`

---

## 9. Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---|---|---|---|
| Zero-row CAS classification | A custom "did the row exist or move?" query | `classifyCASZeroRow` (`internal/world/postgres/character_repo.go:166`) | Locks `FOR UPDATE` and splits NOT_FOUND from WORLD_CONCURRENT_EDIT correctly |
| Mutation delta construction | A hand-built `wmodel.MutationDelta` | `primaryDeltaVersioned(aggregate, id, tombstone, fromV, toV)` (`character_repo.go:256`) | INV-WORLD-2 delta parity |
| Same-tx envelope | Calling `OutboxWriter.WriteIntent` yourself | `worldMutator.mutate(ctx, intent, write)` | Compile-time write-requires-envelope seam; the census AST check requires it |
| Consumer creation retry | A fresh backoff loop | `createConsumerWithRetry` (export or relocate it) | Absorbs the documented JetStream warmup race (flake holomush-l015) |
| Session teardown by character | Iterating `ListActive` and filtering | `Store.DeleteByCharacter(ctx, characterID) (*Info, error)` | One round-trip, returns the `Info` the leave event needs, idempotent |
| Leave/session-ended events | Publishing raw `eventbus.Event`s | `presence.Emitter.EmitLeave` / `.EmitSessionEnded` via a consumer-defined 2-method interface | Handles payload marshalling, actor mapping, the `location.<id>` subject, and the fresh-context discipline in `session_ended.go:26-38` |
| Character location write | A direct `UPDATE characters SET location_id` | `world.Service.MoveCharacter` | D-38; keeps INV-WORLD-4 at four writers instead of five, and exercises MovementHook |
| KV bucket creation | `CreateKeyValue` + `ErrBucketExists` handling | `CreateOrUpdateKeyValue` | `Prepare` MUST be idempotent (`internal/lifecycle/subsystem.go:105-109`) |
| Enumerating all KV keys | `Keys(ctx)` | `ListKeys(ctx)` | `Keys` loads everything into memory; `ListKeys` streams |
| Status parsing | `strings.ToLower` / `==` comparisons | `world.ParseStatus` + `world.Selectable` | INV-WORLD-5's exhaustive-switch-with-denying-default is load-bearing (`internal/world/lifecycle.go:64-80`) |
| Event construction | `eventbus.Event{}` literals | `eventbus.NewEvent(...)` | `.claude/rules/event-conventions.md` |

---

## 10. Common Pitfalls

### Pitfall 1: Copying `Rename`'s repo-writes-envelope shape
**What goes wrong:** `TestWorldEnvelopeCensusMatchesServiceMutatingMethods` fails with *"census
descriptor %q is not a world.Service method routing through the executor (stale descriptor)"*; and
before that, `s.characterRepo.Retire(...)` does not compile because `Service.characterRepo` is a
`CharacterReader`.
**Why:** the "MUST NOT route through mutate()" rule is Rename-specific and exists only for the
operator CLI's direct caller.
**Avoid:** route through `s.mutator`; the repo method returns a delta and writes **no** envelope.
**Warning sign:** the new repo method takes an `intent wmodel.EnvelopeIntent` parameter.

### Pitfall 2: Trusting "5-site cascade"
**What goes wrong:** the package does not compile (fixed-size `[18]stubSubsystem` arrays) or the
pinned topological order test fails.
**Avoid:** work the 13-site table in §3.2 mechanically; run `task generate` for the stringer.
**Warning sign:** a plan task that touches fewer than 5 files for the cascade.

### Pitfall 3: Assuming an unset KV `Storage` means Memory
**What goes wrong:** test buckets silently become file-backed, leaving `StoreDir` residue that a later
run re-opens with a mismatched config.
**Avoid:** set `Storage` explicitly in production; expose a `…WithStorage` seam for tests.

### Pitfall 4: Putting `UPDATE characters SET last_active_at` outside `internal/world/postgres`
**What goes wrong:** `TestWorldSQLFence` (a `go/ast` string-literal scanner) fails —
`characters` is a fenced table and `internal/world/postgres` is the only allowlisted directory.
**Avoid:** an exported free function in the writer boundary, called through an injected interface.
**Warning sign:** the flusher package importing `pgx` directly.

### Pitfall 5: Routing the `last_active_at` flush through `mutate()`
**What goes wrong:** every flush emits a world-change envelope, and the census demands a taxonomy kind
for a non-world-state column.
**Avoid:** the flusher is an out-of-world writer under INV-WORLD-4, envelope-exempt, and the invariant
text must say so.

### Pitfall 6: A reactor that double-emits `leave` on redelivery
**What goes wrong:** JetStream `AckExplicitPolicy` + `MaxDeliver` means at-least-once; a slow handler
gets the message twice and the location sees two departure notices.
**Avoid:** gate every effect on observed state (§5.3) — `DeleteByCharacter` returning nil ⇒ skip the
leave; character already at `starting_location_id` ⇒ skip the move; status no longer `retired` ⇒
ack-and-skip the whole message.

### Pitfall 7: Forgetting `AppSchemaVersion`
**What goes wrong:** consumers cannot tell which taxonomy revision produced a row.
**Avoid:** bump `internal/world/outbox/taxonomy.go:19` in the same change as the two new kinds — it is
a documented obligation (`:13-19`).

### Pitfall 8: `StartLocationID()` panics
**What goes wrong:** `panic("bootstrap/setup: StartLocationID() called before Prepare()")`
(`internal/bootstrap/setup/subsystem.go:303`).
**Avoid:** declare `DependsOn(SubsystemBootstrap)` on the reactor, or resolve the value lazily inside
the handler rather than at construction.

### Pitfall 9: Assuming `createConsumerWithRetry` is callable
**What goes wrong:** it is unexported; the reactor's package cannot reach it.
**Avoid:** budget a task to export/relocate it, or accept a small duplication with a comment pointing
at the original.

---

## 11. Code Examples

### The narrow-write Service command (shape derived from `UpdateCharacterDescription`)
```go
// Source: internal/world/service.go:799-836 (verbatim shape), adapted
func (s *Service) RetireCharacter(ctx context.Context, subjectID string, characterID ulid.ULID, expectedVersion int) error {
    if s.characterRepo == nil {
        return oops.Code("CHARACTER_RETIRE_FAILED").Errorf("character repository not configured")
    }
    resource := access.CharacterResource(characterID.String())
    if err := s.checkAccess(ctx, subjectID, "retire", resource, prefixCharacter); err != nil {
        return err
    }
    char, err := s.characterRepo.Get(ctx, characterID)
    if err != nil { /* ErrNotFound -> CHARACTER_NOT_FOUND; else CHARACTER_GET_FAILED */ }
    if s.mutator == nil {
        return oops.Code("CHARACTER_RETIRE_FAILED").Errorf("world write executor not configured (OutboxWriter + Transactor required)")
    }
    payload, err := BuildCharacterLifecyclePayload(characterID, StatusRetired)
    if err != nil { /* wrap */ }
    intent := s.buildIntent(kindCharacterRetired, wmodel.AggregateCharacter, characterID, subjectID, payload)
    if _, err := s.mutator.retireCharacter(ctx, intent, characterID, StatusRetired, char.Version); err != nil {
        if errors.Is(err, ErrConcurrentEdit) {
            return oops.Code(CodeConcurrentEdit).With("character_id", characterID.String()).Wrap(err)
        }
        if errors.Is(err, ErrNotFound) {
            return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "retire character %s", characterID)
        }
        return oops.Code("CHARACTER_RETIRE_FAILED").Wrapf(err, "retire character %s", characterID)
    }
    return nil
}
```

### The version-predicated status write (shape from `Rename`, minus the envelope)
```go
// Source: internal/world/postgres/character_repo.go:225-256 (verbatim shape), adapted
query := `UPDATE characters SET status = $2, version = version + 1 WHERE id = $1`
args := []any{id.String(), string(status)}
if expectedVersion > 0 {
    query += ` AND version = $3`
    args = append(args, expectedVersion)
}
query += ` RETURNING version`

var delta *wmodel.MutationDelta
txErr := withTx(ctx, r.pool, func(txCtx context.Context) error {
    tx := txFromContext(txCtx)
    var newVersion int
    err := tx.QueryRow(txCtx, query, args...).Scan(&newVersion)
    if errors.Is(err, pgx.ErrNoRows) {
        return classifyCASZeroRow(txCtx, tx,
            `SELECT version FROM characters WHERE id = $1 FOR UPDATE`, id,
            oops.Code("CHARACTER_NOT_FOUND").With("id", id.String()).Wrap(world.ErrNotFound))
    }
    if err != nil {
        return oops.Code("CHARACTER_STATUS_UPDATE_FAILED").With("id", id.String()).Wrap(err)
    }
    // D-34: clear a dangling default-character pointer in the SAME transaction.
    if _, uErr := tx.Exec(txCtx,
        `UPDATE players SET default_character_id = NULL WHERE default_character_id = $1`, id.String()); uErr != nil {
        return oops.Code("CHARACTER_RETIRE_DEFAULT_CLEAR_FAILED").With("id", id.String()).Wrap(uErr)
    }
    delta = primaryDeltaVersioned(wmodel.AggregateCharacter, id, false, newVersion-1, newVersion)
    return nil
})
```

### The KV bucket (production wiring)
```go
// Source: $GOMODCACHE/github.com/nats-io/nats.go@v1.52.0/jetstream/kv.go:59, 210-275
kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
    Bucket:      "character_activity",              // ^[a-zA-Z0-9_-]+$
    Description: "buffered characters.last_active_at epoch-nanos, flushed periodically",
    History:     1,                                  // only the latest write matters
    Storage:     jetstream.FileStorage,              // EXPLICIT — buckets do not inherit the stream's
    Replicas:    1,
})
```

### The durable consumer (shape from the audit projector)
```go
// Source: internal/eventbus/audit/projection.go:109-127
cons, err := createConsumerWithRetry(ctx, func(ctx context.Context) (jetstream.Consumer, error) {
    return js.CreateOrUpdateConsumer(ctx, eventbus.StreamName, jetstream.ConsumerConfig{
        Durable:       "character_retirement_reactor",
        Name:          "character_retirement_reactor",
        FilterSubject: "events.*.character.>",
        AckPolicy:     jetstream.AckExplicitPolicy,
        AckWait:       cfg.AckWait,
        MaxAckPending: cfg.MaxAckPending,
        MaxDeliver:    cfg.MaxDeliver,
    })
})
```

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `testify` (unit); Ginkgo v2 + Gomega (integration) |
| Config file | `Taskfile.yaml` (`test`, `test:int`, `test:cover`); `.golangci.yaml` |
| Quick run command | `task test -- ./internal/world/... ./test/meta/` |
| Full suite command | `task test && task test:int` |

**Never run bare `go test`.** `task lint`/`task fmt` before every commit; `task fmt` mutates files
(SPDX headers, aligned Go blocks) and those edits must be committed.

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|--------------|
| IDENT-04 | `RetireCharacter` sets `status='retired'`, bumps `version`, leaves `name`/`normalized_name` untouched | unit (repo, mocked) + audit-integration (real row) | `task test -- -run TestRetire ./internal/world/...` | ❌ Wave 0 |
| IDENT-04 | Retired character's name is still refused by the creation path | audit-integration | `task test:int -- -run TestCharacterLifecycle ./test/integration/world/` | ✅ (extend `character_lifecycle_test.go`) |
| IDENT-04 | `UnretireCharacter` restores `status='active'`; on an already-active character returns a typed error | unit | `task test -- -run TestUnretire ./internal/world/...` | ❌ Wave 0 |
| IDENT-04 | Retire clears `players.default_character_id` **in the same transaction** (rollback ⇒ neither changes) | audit-integration | `task test:int -- -run TestRetireClearsDefaultCharacter ./test/integration/world/` | ❌ Wave 0 |
| IDENT-04 | Reactor: `character_retired` ⇒ sessions ended, `leave` at the OLD location, character at `starting_location_id` | full-stack integration | `task test:int -- -run TestRetirementReactor ./test/integration/world/` | ❌ Wave 0 |
| IDENT-04 | Reactor is idempotent: a redelivered message emits exactly one `leave` and one move | full-stack integration | same suite, second `It` | ❌ Wave 0 |
| IDENT-04 | Retire / idle-out / purge stay three distinct operations; `DeleteCharacter`'s cascade+tombstone path is untouched | unit (census + lifecycle) | `task test -- ./test/meta/ ./internal/world/` | ✅ (INV-WORLD-5 binding test) |
| IDENT-10 | Stale `expected_version` on retire ⇒ `WORLD_CONCURRENT_EDIT`, no silent overwrite | unit + **two-replica integration** | `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` | ✅ suite exists, new `Describe` needed |
| IDENT-10 | State change + its ONE envelope commit or roll back together | audit-integration | `task test:int -- ./internal/world/outbox/` | ✅ (INV-WORLD-1 pattern) |
| IDENT-10 | Census bijection holds for the two new kinds; AST cross-check green | unit (meta) | `task test -- ./test/meta/` | ✅ exists — **must go RED first** |
| — | `last_active_at` is written without a per-event DB write | **external-NATS integration** | `task test:int -- -run TestCharacterActivityFlush ./test/integration/world/` | ❌ Wave 0 |
| — | Flusher's `UPDATE` is monotonic + idempotent (`WHERE last_active_at < $2`) | unit (repo, mocked) + audit-integration | `task test -- ./internal/world/postgres/` | ❌ Wave 0 |
| — | INV-WORLD-4 enumeration amended; registry regenerated and green | unit (meta) | `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` | ✅ exists |
| — | 20 production subsystems register; topological order pinned | unit | `task test -- ./cmd/holomush/ ./internal/lifecycle/` | ✅ exists — **will go RED on the cascade** |

### Tier assignment — the load-bearing distinction

Per `.claude/rules/testing.md`, embedded NATS (`eventbustest`) is correct at every tier **except**
external-mode-specific behavior. Applying that rule:

| Behavior | Tier | Harness | Rationale |
|---|---|---|---|
| Status CAS, payload/intent building, error mapping | **unit** | mockery `worldtest` mocks | no I/O |
| Row + envelope commit/rollback together; `default_character_id` clear | **audit-integration** | Postgres testcontainer + embedded NATS | needs a real transaction |
| Retirement reactor end-to-end (relay → consumer → sessions → move) | **full-stack integration** | `integrationtest.Start(t)` (Postgres + embedded NATS + real `CoreServer`) | reactor is a registered subsystem in the real graph |
| **`last_active_at` KV buffer + flush** | **full-stack integration with EMBEDDED NATS — sufficient** | `integrationtest.Start(t)`; force the bucket to `jetstream.MemoryStorage` via the `…WithStorage` seam | **KV is a plain JetStream stream** (`prepareKeyValueConfig` returns a `StreamConfig`, kv.go:668). Embedded NATS is a full JetStream server (`JetStream: true`, `internal/eventbus/subsystem.go:220`). Nothing about KV requires the external-mode shape — that carve-out is for external dial / fail-closed boot / single-principal scoping / multi-node per-replica invalidation / DLQ. **`natstest` is NOT required.** |
| Two-replica stale-version rejection | **external-NATS integration** | `test/integration/resilience/` + `natstest` | N replicas each with an independent connection to one broker — the documented `natstest` case |

> **Wave-structure consequence:** the KV work does **not** need a real NATS container, so track (C) can
> run in the same wave as track (B) on the shared `integrationtest` harness. Only the
> resilience `Describe` (track A's IDENT-10 proof) needs `natstest`, and that suite already exists.

### Sampling Rate

- **Per task commit:** `task test -- ./internal/world/... ./test/meta/ ./cmd/holomush/` — fast, and
  covers the three meta-tests this phase can break (census ×2, subsystem counts, invariant registry).
- **Per wave merge:** `task test && task test:int`
- **Phase gate:** `task pr-prep` green inline in the parent, plus
  `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/`
  run once by hand (it is not on the PR gate).

### Wave 0 Gaps

- [ ] `internal/world/service_retire_test.go` — covers IDENT-04 command-level behavior (unit)
- [ ] `internal/world/postgres/character_repo_status_test.go` — covers the CAS + `default_character_id` clear
- [ ] Regenerate `internal/world/worldtest/mock_CharacterRepository.go` (`mockery`) after `SetStatus` lands
- [ ] `test/integration/world/retirement_reactor_test.go` — covers the D-36/37/38 fanout + idempotency
- [ ] `test/integration/world/character_activity_flush_test.go` — covers the KV buffer + flush
- [ ] New `Describe` in `test/integration/resilience/` — covers IDENT-10's two-replica proof
- [ ] Extend `test/integration/world/character_lifecycle_test.go` for the retire-path assertions
      (do NOT touch its `// Verifies: INV-WORLD-6` block's claims)
- [ ] Framework install: **none** — Ginkgo/Gomega/testify/mockery/testcontainers all present

### PORTAL-10 verification-integrity rules (binding on every phase plan)

1. **Census with set equality** — already structurally satisfied by
   `TestWorldEnvelopeCensusBijection` + `…MatchesServiceMutatingMethods`. Prove the new rows go
   **RED first** (add the descriptor before the kind, observe the failure, then add the kind).
2. **Paired positive control on every denial test** — the retire-denial test must show the same
   subject succeeding under a granted seed.
3. **Assertions against marshaled response bytes** — applies at the RPC layer (Phase 4+); at this
   layer, assert against the **actual DB row** and the **actual outbox row**, never a populated Go
   struct.
4. **Gates demonstrated RED against the pre-fix state** — the census, the subsystem-count tests, and
   the invariant meta-tests must each be observed failing before the fix.
5. **Wire-level assertion of opacity/authorization** — `errutil.AssertErrorCode` resolves the
   **deepest** code under the pinned `samber/oops v1.22.0` (issue #4902). For `WORLD_CONCURRENT_EDIT`
   this is fine (it is the deepest code), but do not use it to prove a *top-level* code.
6. **Invariant-scope discipline** — allocate in `INV-WORLD` (the scope exists and is registered). Do
   NOT mint `INV-RETIRE-*`. Ship `binding: pending` rather than fabricating a `// Verifies:`.

---

## Security Domain

**`security_enforcement: true`, `security_asvs_level: 1`** `[VERIFIED: .planning/config.json]`.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|------------------|
| V2 Authentication | no | no auth surface changes; the reactor is a host subsystem |
| V3 Session Management | **yes** | `Store.DeleteByCharacter` is the single teardown path; a retired character's sessions MUST NOT survive. Session status vocabulary (`StatusActive/Detached/Expired`, `internal/session/session.go:21`) is distinct from character lifecycle and must not be conflated |
| V4 Access Control | **yes** | ABAC default-deny; `Service.checkAccess` at every command entry (`internal/world/service.go:209-254`). D-39 puts ownership in policy — so the **seed is the control**, and a permissive seed is a privilege escalation with no compile-time backstop |
| V5 Input Validation | **yes** | `world.ParseStatus` rejects anything outside the closed vocabulary; the DB `CHECK (status IN ('active','retired','idle'))` is the second line |
| V6 Cryptography | no | no crypto surface; `crypto.emits` untouched |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Over-broad system-subject seed (`permit(principal is system, action, resource)`) | Elevation of Privilege | Scope the new seed to `resource is character` and `action in ["read","write"]` only — never a bare `action` |
| Bare `"system"` subject reaching a request context that is not a system context | Elevation of Privilege | Already fail-closed: `SYSTEM_SUBJECT_REJECTED` unless `access.IsSystemContext(ctx)` (`internal/access/policy/engine.go:92-101`). Prefer the prefixed `"system:<x>"` + explicit seed over the bypass |
| Retire used to evict another player's character | Elevation of Privilege | The `resource.character.id == principal.character.id` predicate in the self-retire seed; paired positive control in the test |
| SQL injection via status | Tampering | Parameterized `$2`; `Status` is a typed const, never caller-supplied text |
| Reactor evicting a character that was un-retired between emit and delivery | Tampering / DoS | Read-current-state guard before each effect (§5.3) |
| `last_active_at` as a presence oracle | Information Disclosure | The column lags by up to one flush interval **by construction** (D-42); readers needing real-time accuracy consult KV. Any surface exposing it is a Phase 5 privacy decision, not Phase 3's |
| KV bucket unbounded growth on flusher failure | DoS | `MaxBytes` and/or `TTL` on the bucket; `History: 1` |

---

## Project Constraints (from CLAUDE.md)

| Directive | Where it binds this phase |
|---|---|
| **TDD** — tests before implementation, must pass before a task is complete; `tdd_mode: true` enforces RED/GREEN/REFACTOR | Every task. The census and subsystem-count tests give free RED phases |
| **MUST use `task`** for build/test/lint/fmt; never bare `go test`/`go build`/`golangci-lint` | Every verification step |
| **MUST delegate verbose task runs** to `local-check`; the FINAL `task pr-prep` runs inline in the parent | Execution-agent instructions |
| **MUST run `task test:int` on refactors** — `task test` does not compile `//go:build integration` files | The subsystem cascade touches shared types |
| **`oops`** structured errors: `oops.Code("X").With(k,v).Wrap(err)`; test with `errutil.AssertErrorCode` | All new error paths |
| **`slog.*Context`** variants whenever a `ctx` is in scope; `errutil.LogErrorContext` for errors | Reactor + flusher (both are ctx-carrying loops) |
| **Migrations**: one `NNNNNN_name.sql`, both directions, idempotent, no triggers/functions, `BIGINT` epoch-nanos never `TIMESTAMPTZ` (INV-STORE-1) | **Phase 3 needs NO migration** — `status` and `last_active_at` both landed in 000054 |
| **SPDX headers** on new `.go` files; applied by `task fmt` (which mutates files — commit the edits) | Every new file |
| **Invariant registry** — consult before designing; amend in the same change; never fabricate a `// Verifies:` | §6 |
| **Terminology** — `location` never `room`; `character` ≠ `player`; `presence` is session-derived, never read off `characters.location_id` | Reactor docs and payload field names |
| **Event conventions** — `eventbus.NewEvent(...)`, never `eventbus.Event{}`; domain-relative dot subjects; `core.NewULID()` for event IDs, `idgen.New()` for entity PKs | Reactor + flusher |
| **Gateway boundary** — the gateway is protocol translation only | Reactor lives in core, not `internal/web` |
| **Worktree isolation** — already satisfied (`v013-phase-03`); never edit the primary worktree | — |
| **Protected `main`** — feature branch + PR + squash merge; never `[ci skip]` | Ship |
| **Session-start skill** — `dev-flow:grepping` must be loaded before the first response | Every agent |

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | everything | ✓ | per `go.mod` | — |
| `task` (go-task) | all build/test/lint | ✓ | — | — |
| Docker | `task test:int` (Postgres testcontainers, `natstest`) | assumed ✓ | — | none — integration tests cannot run without it |
| `github.com/nats-io/nats.go` | JetStream KV | ✓ | v1.52.0 | — |
| `github.com/nats-io/nats-server/v2` | embedded NATS | ✓ | v2.14.3 | — |
| `mockery` | regenerate `worldtest` mocks after the repo-interface change | ✓ (`.mockery.yaml` present) | — | hand-write the mock (discouraged) |
| `stringer` | `subsystemid_string.go` regeneration via `task generate` | ✓ | — | hand-edit (discouraged — the packed index array is error-prone) |
| `go run ./cmd/inv-render` | regenerate `docs/architecture/invariants.md` | ✓ (in-tree) | — | — |
| `gh` CLI | file the INV-WORLD-6 issue | ✓ | — | — |

**Missing dependencies with no fallback:** none identified.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|---|---|---|
| A1 | Player self-retire is in scope for v0.13 (IDENT-04's literal wording) despite every sketch showing only `AdminRetireCharacter` | §8.4 | Ships a policy grant nobody uses (cheap), or omits one IDENT-04 requires (requirement not closed). **Needs user confirmation.** |
| A2 | The `last_active_at` flusher is inside INV-WORLD-4's scope and the enumeration goes THREE→FOUR with an envelope-exemption clause | §6.2 | Either the invariant text overclaims (says every writer emits an envelope when one does not) or the flusher is silently unfenced. CONTEXT asserts FOUR; the exemption wording is my inference. **Needs planner ratification.** |
| A3 | A 5-minute throttle window and a comparable flush interval are reasonable defaults | §4 | Purely a tuning value; CONTEXT explicitly leaves it to the planner |
| A4 | `character_retired` / `character_unretired` payloads should be `{character_id, status}` | §2.5 | Wire-contract shape; a different field set is equally valid but is one-way once emitted |
| A5 | The reactor and the flusher are two separate Go packages (not one) | §3 | CONTEXT mandates two *subsystems*; package layout is a plan decision |
| A6 | `Store.DeleteByCharacter` returns `(nil, nil)` when no session exists (rather than an error) — the basis for the reactor's idempotency gate | §5.3 | If it errors on absence, the redelivery guard must be written differently. **Verify the implementation body before writing the handler.** |
| A7 | Docker is available on the execution host | Environment | `task test:int` cannot run |

---

## Open Questions

1. **Does v0.13 expose player self-retire, or admin-only?**
   - What we know: IDENT-04 says "A player can soft-retire **their own** character." Every sketch cited
     in CONTEXT shows `AdminRetireCharacter` / `AdminUnretireCharacter`. `seed:admin-full-access` covers
     admins for free.
   - What's unclear: whether a player-facing RPC ships in v0.13 at all (Phase 5/6 territory).
   - Recommendation: ship **both** seeds; the domain command is identical either way. Confirm with the
     user during `/gsd-plan-phase`.

2. **Does INV-WORLD-4's "each emitting its envelope atomically" clause survive the flusher?**
   - What we know: the flusher writes `characters.last_active_at` and emits nothing.
   - What's unclear: whether the clean amendment is an exemption clause or a scope narrowing.
   - Recommendation: minimal honest amendment (exemption clause, explicitly naming the writer and why),
     with the deeper "world-state vs operational writer" question left in CONTEXT's separate todo.

3. **Where does `createConsumerWithRetry` live after this phase?**
   - What we know: unexported in `internal/eventbus/audit`, shared by two callers in that package.
   - Recommendation: export it as `audit.CreateConsumerWithRetry` (smallest diff), or relocate to a
     neutral `internal/eventbus` helper. Decide in the plan; do not leave it to the executor.

4. **Which package hosts the flusher's writer-boundary call?**
   - What we know: `UPDATE characters` must live in `internal/world/postgres`; the flusher's package
     will not be on the composition allowlist.
   - Recommendation: exported free function in `internal/world/postgres` (precedent:
     `BackfillCharacterIdentity`), injected as an interface from `cmd/holomush`.

5. **Where in the pinned topological order do the two new subsystems land?**
   - What we know: `topoSort` tie-breaks by `SubsystemID` value; new IDs sort last within their tier.
   - Recommendation: derive it by running the test once and reading the failure diff, not by hand.

---

## Sources

### Primary (HIGH confidence — opened this session)
- `internal/world/mutator.go`, `internal/world/service.go`, `internal/world/repository.go`, `internal/world/lifecycle.go`
- `internal/world/postgres/character_repo.go`, `internal/world/outbox/taxonomy.go`, `internal/world/outbox/wire.go`
- `test/meta/world_envelope_census_test.go`, `test/meta/world_sql_fence_test.go`, `test/meta/world_import_graph_test.go`
- `internal/lifecycle/subsystem.go`, `internal/lifecycle/subsystemid_string.go`
- `cmd/holomush/core.go`, `cmd/holomush/core_subsystems_test.go`, `cmd/holomush/core_topo_order_test.go`
- `internal/eventbus/subsystem.go`, `internal/eventbus/audit/projection.go`, `internal/eventbus/qualify.go`
- `internal/presence/emitter.go`, `internal/core/session_ended_payload.go`, `internal/session/session.go`
- `internal/auth/auth_service.go`, `internal/auth/character_reaping.go`, `internal/grpc/lifecycle_handler.go`, `internal/grpc/command_handler.go`, `internal/grpc/auth_handlers.go`, `cmd/holomush/sub_grpc.go`
- `internal/access/prefix.go`, `internal/access/policy/engine.go`, `internal/access/policy/seed.go`, `internal/access/policy/types/types.go`, `internal/access/setup/seed_coverage.go`
- `internal/bootstrap/setting.go`, `internal/bootstrap/setup/subsystem.go`
- `internal/store/migrations/000001_baseline.sql`, `internal/store/migrations/000054_character_identity_and_lifecycle.sql`
- `docs/architecture/invariants.yaml` (INV-WORLD-4/5/6/7)
- `test/integration/resilience/resilience_suite_test.go`, `test/integration/resilience/m12_lastwritewins_test.go`
- `test/integration/world/character_lifecycle_test.go`
- `Taskfile.yaml`, `go.mod`, `.planning/config.json`
- **Pinned module source:** `$GOMODCACHE/github.com/nats-io/nats.go@v1.52.0/jetstream/kv.go`, `.../stream_config.go`

### Secondary (MEDIUM confidence)
- `.claude/rules/*` (testing, invariants, event-conventions, database-migrations, logging, search-tools, gateway-boundary, subagent-briefing)
- `.planning/phases/03-world-character-commands/03-CONTEXT.md`, `.planning/REQUIREMENTS.md`, `.planning/STATE.md`

### Tertiary (LOW confidence)
- None. No web search was performed; the nats.go API was read from the pinned module source rather than from documentation.

---

## Metadata

**Confidence breakdown:**
- Census semantics: **HIGH** — the meta-test was read line-by-line and both assertions quoted verbatim.
- Subsystem cascade: **HIGH** — every one of the 13 sites opened; counts derived from the tree, not from memory.
- JetStream KV API: **HIGH** — read from the pinned `v1.52.0` source; the `FileStorage = iota` default is a direct quote.
- Reactor substrate: **HIGH** for the consumer/subject/session/presence seams; **MEDIUM** for the idempotency design (A6 needs one more read).
- ABAC: **HIGH** for the absence of a system→character permit and for the open action vocabulary; **MEDIUM** for the recommended seed text (untested DSL).
- Invariant amendment: **HIGH** for current state; **MEDIUM** for the recommended wording (A2).

**Research date:** 2026-08-06
**Valid until:** 2026-09-05 (30 days — in-repo seams are stable; `main` moves, so re-verify `file:line` citations if the branch rebases past a world/lifecycle change)

**REFUTED CONTEXT claims (summary):**
1. The "5-site compile cascade" — it is **13 sites across 5 files**, one of which pins an exact ordered 18-element sequence.
2. "A bucket left at the default in a memory-configured test harness will silently lose unflushed writes" — the default **is** `FileStorage`; the hazard is the inverse (file-backed residue in a memory harness).
3. "the four existing leave+session-ended fanouts" — there are **eight** `EmitLeave` sites across seven flows.
4. `createConsumerWithRetry` as "a third user" — it is **unexported** and unreachable from another package as written.
5. `internal/eventbus/subsystem.go:214-222 resolveStoreDir` — `:214` is the call site; the function is at `:490-501`.
6. Implied by the brief: that Retire should follow `Rename`'s repo-writes-envelope shape — the census AST cross-check and the reader-view compile fence both forbid it.

**NOT FOUND symbols (do not invent):**
- `CharacterRepository.SetStatus` / `.Retire` — no status writer exists; must be created.
- `world.Service.RenameCharacter` — never existed (only the repo method + the operator CLI).
- `access.SystemSubject(...)` — no helper; `"system:bootstrap"` is a raw literal.
- A shared "end session + announce departure" helper — all eight fanouts hand-roll it.
- Any existing JetStream KV usage — zero in the tree.

# Phase 3: World Character Commands - Pattern Map

**Mapped:** 2026-08-07
**Files analyzed:** 14 new/modified files across 3 tracks (A: domain commands, B: retirement reactor, C: last_active_at KV subsystem)
**Analogs found:** 12 / 14 (2 no-analog: JetStream KV usage, job-caller authorization — see "No Analog Found")

> **Sourcing note.** 03-RESEARCH.md already verified most analogs with `[VERIFIED: path:line]`
> citations; this map re-opened the load-bearing analogs this session and quotes them verbatim.
> All `world.Service` signatures below show the **current tree shape** (`subjectID string`); per
> RESEARCH §0.2 / Pitfall 10, plans MUST write them **caller-shaped** (`world.HumanCaller` /
> `world.JobCaller`, `[ASSUMED]` names) and reconcile against Phase 02.1's landed code.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/world/service.go` (add `RetireCharacter`/`UnretireCharacter`) | service (domain command) | CRUD (guarded write) | `Service.UpdateCharacterDescription` (`service.go:799-836`) + `UpdateCharacterPreferences` (`:856-891`) | exact |
| `internal/world/mutator.go` (2 census rows + 2 per-op executor methods) | service (write executor) | CRUD | `updateCharacterPreferences` (`mutator.go:250-254`), `writeCommands` set (`:85-100`) | exact |
| `internal/world/repository.go` (add `SetStatus` to `CharacterRepository`) | model (interface) | CRUD | `UpdatePreferences`/`Rename` entries (`repository.go:197-244`) | exact |
| `internal/world/postgres/character_repo.go` (add `SetStatus` impl + D-34 players clear) | model (repo) | CRUD (CAS) | `Rename` (`character_repo.go:225-256`) **minus the envelope write** | exact (with a documented deviation) |
| `internal/world/outbox/taxonomy.go` (2 kinds, registry entries, payload, `AppSchemaVersion` bump) | config (taxonomy registry) | — | existing `KindCharacterPreferencesUpdate` rows (`taxonomy.go:52-57, 108-114`) | exact |
| `internal/world/worldtest/mock_CharacterRepository.go` | test (mock) | — | regenerate with `mockery` | exact |
| `internal/eventbus/consumer/` (NEW pkg — D-46 relocation) | utility (shared retry) | event-driven | `createConsumerWithRetry` + `consumerCreateBackoffs` (`internal/eventbus/audit/projection.go:93-96, 167-188`) | exact (move, not rewrite) |
| retirement reactor subsystem (NEW pkg + `internal/*/setup` wiring) | service (host subsystem) | event-driven | `internal/world/setup/relay_subsystem.go` (lifecycle shape) + `internal/eventbus/audit/projection.go:108-139` (durable consumer) | role-match |
| `internal/core/session_ended_payload.go` (add `SessionEndedCauseRetired`) | config (const) | — | existing cause consts (`session_ended_payload.go:24-31`) | exact |
| `last_active_at` KV subsystem (NEW pkg) | service (host subsystem) | event-driven + batch flush | subsystem shape: `relay_subsystem.go`; storage seam: `eventbus.NewSubsystemWithStorage` (`internal/eventbus/subsystem.go:62-71`); KV API: **no in-repo analog** | partial |
| `internal/world/postgres/` flush writer (NEW exported free function) | model (repo, writer boundary) | batch | `worldpostgres.BackfillCharacterIdentity` free-function precedent (per `test/meta/world_import_graph_test.go:92-102`) | role-match |
| `internal/lifecycle/subsystem.go` + `cmd/holomush/core.go` + 2 test files (SubsystemID cascade, 13 sites, 18→20) | config (wiring) | — | the 02-12 `SubsystemCharacterNameBlockList` addition; site list in RESEARCH §3.2 | exact |
| `docs/architecture/invariants.yaml` (INV-WORLD-4 THREE→FOUR) | config (registry) | — | the 02-12 TWO→THREE amendment sentence quoted in the entry itself (`invariants.yaml:5057-5083`) | exact |
| `internal/access/policy/seed.go` (`seed:player-self-retire`; job seed conditional on §0.4-2) | config (ABAC seed) | — | `seed:player-self-access` (`seed.go:39-43`) | exact |

## Pattern Assignments

### `internal/world/service.go` — `RetireCharacter` / `UnretireCharacter` (service, guarded CRUD)

**Analog:** `Service.UpdateCharacterDescription` (`internal/world/service.go:799-836`). The census
AST cross-check (`test/meta/world_envelope_census_test.go:187-207`) **forces** the method body to
reference `s.mutator` — do NOT copy `CharacterRepository.Rename`'s repo-writes-envelope shape
(Rename-only rule; also `Service.characterRepo` is a read-only `CharacterReader`, `service.go:101`).

**Core pattern** (`service.go:799-836`, verbatim):
```go
func (s *Service) UpdateCharacterDescription(ctx context.Context, subjectID string, characterID ulid.ULID, description string) error {
	if s.characterRepo == nil {
		return oops.Code("CHARACTER_UPDATE_FAILED").Errorf("character repository not configured")
	}
	resource := access.CharacterResource(characterID.String())
	if err := s.checkAccess(ctx, subjectID, "write", resource, prefixCharacter); err != nil {
		return err
	}
	char, err := s.characterRepo.Get(ctx, characterID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "get character %s", characterID)
		}
		return oops.Code("CHARACTER_GET_FAILED").Wrapf(err, "get character %s", characterID)
	}
	if s.mutator == nil {
		return oops.Code("CHARACTER_UPDATE_FAILED").Errorf("world write executor not configured (OutboxWriter + Transactor required)")
	}
	char.Description = description
	payload, err := BuildCharacterUpdatePayload(characterID, description)
	if err != nil {
		return oops.Code("CHARACTER_UPDATE_FAILED").Wrapf(err, "build character update payload %s", characterID)
	}
	intent := s.buildIntent(kindCharacterUpdated, wmodel.AggregateCharacter, characterID, subjectID, payload)
	if _, err := s.mutator.updateCharacter(ctx, intent, char); err != nil {
		if errors.Is(err, ErrConcurrentEdit) {
			return oops.Code(CodeConcurrentEdit).With("character_id", characterID.String()).Wrap(err)
		}
		if errors.Is(err, ErrNotFound) {
			return oops.Code("CHARACTER_NOT_FOUND").Wrapf(err, "update character %s", characterID)
		}
		return oops.Code("CHARACTER_UPDATE_FAILED").Wrapf(err, "update character %s", characterID)
	}
	return nil
}
```

**Adaptations for retire/unretire:**
- action string: `"retire"` / `"unretire"` (D-40; open vocabulary — `types.NewAccessRequest` validates non-emptiness only, `internal/access/policy/types/types.go:143-173`)
- kind consts: `kindCharacterRetired` / `kindCharacterUnretired` (local mirror consts in `service.go:37-56` — `internal/world` MUST NOT import `internal/world/outbox`, `test/meta/world_import_graph_test.go:47-48`)
- CAS guard: pass `char.Version` (the `UpdateCharacterPreferences` pattern at `service.go:864-878`)
- new payload builder mirroring `BuildCharacterUpdatePayload` (`service.go:818`); error codes `CHARACTER_RETIRE_FAILED` / `CHARACTER_UNRETIRE_FAILED`
- `UnretireCharacter` on an already-active character: typed error (fail-loudly), read `char.Status` after `Get`
- Ready-adapted full body: RESEARCH §11 first code block

### `internal/world/mutator.go` — census rows + per-op executors

**Census row pattern** (`mutator.go:85-100`):
```go
var writeCommands = []WriteCommandDescriptor{
	// ...
	{Command: "UpdateCharacterDescription", Kind: kindCharacterUpdated},
	{Command: "UpdateCharacterPreferences", Kind: kindCharacterPreferencesUpdate},
}
```
Add `{Command: "RetireCharacter", Kind: kindCharacterRetired}` and
`{Command: "UnretireCharacter", Kind: kindCharacterUnretired}`.

**Per-op executor pattern** (`mutator.go:250-254`, verbatim — `updateCharacterPreferences`'s closure builder):
```go
) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		return m.characterWriter.UpdatePreferences(txCtx, characterID, prefs, expectedVersion)
	})
}
```
New method: `retireCharacter(ctx, intent, id, status Status, expectedVersion int)` calling
`m.characterWriter.SetStatus(txCtx, id, status, expectedVersion)` (one shared executor per command,
or one per command — the census keys on Service methods, not executor methods). `mutate()` itself
(`mutator.go:197-217`) is untouched: one `InTransaction` = write closure + `outbox.WriteIntent`.

### `internal/world/postgres/character_repo.go` — `SetStatus` (repo, CAS write)

**Analog:** `Rename` (`character_repo.go:225-256`) **minus** its `WriteIntent` call (that envelope
write is Rename-specific; `SetStatus` returns the delta and lets `mutate()` write the envelope).

**Core pattern** (RESEARCH §11, shape lifted verbatim from `character_repo.go:225-256`):
```go
query := `UPDATE characters SET status = $2, version = version + 1 WHERE id = $1`
args := []any{id.String(), string(status)}
if expectedVersion > 0 {
	query += ` AND version = $3`
	args = append(args, expectedVersion)
}
query += ` RETURNING version`

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
**Error/helper patterns to reuse (Don't Hand-Roll):** `classifyCASZeroRow` (`character_repo.go:166`)
splits NOT_FOUND from `WORLD_CONCURRENT_EDIT`; `primaryDeltaVersioned` (`:256`);
`oops.Code(world.CodeConcurrentEdit).…Wrap(world.ErrConcurrentEdit)` (`:291-295`).
**Layering note for D-34:** `players` is not a fenced table (`test/meta/world_sql_fence_test.go:62-65`);
the reaping-guard READ precedent (`reaping_guard.go`, fence test `:49-56`) is being widened to a
WRITE — document at the site + in the fence test's doc block.

### `internal/world/outbox/taxonomy.go` — 2 kinds + registry entries + payload

**Analog:** the `KindCharacterPreferencesUpdate` rows. Const block (`taxonomy.go:52-57`):
```go
	KindCharacterGenesis           = "character_genesis"
	KindCharacterUpdated           = "character_updated"
	// ... add:
	// KindCharacterRetired   = "character_retired"
	// KindCharacterUnretired = "character_unretired"
```
Registry entry pattern (`taxonomy.go:108-114`):
```go
{Kind: KindCharacterPreferencesUpdate, Aggregate: wmodel.AggregateCharacter, SchemaVersion: 1, Payload: characterPreferencesPayload},
```
New `characterLifecyclePayload []PayloadField` = `{character_id: ulid, status: string}`
(new-values-only, erasure-safe — registry rule at `taxonomy.go:122-124`; the existing
`characterUpdatePayload` is `{character_id, description}` and NOT reusable).
**MANDATORY:** bump `AppSchemaVersion` (`taxonomy.go:19` — "Bump it whenever the set of declared kinds … changes").

### `internal/eventbus/consumer/` (NEW) — D-46 relocation

**Source to move verbatim:** `internal/eventbus/audit/projection.go:93-96` + `:167-188`:
```go
var consumerCreateBackoffs = []time.Duration{
	100 * time.Millisecond,
	250 * time.Millisecond,
}

func createConsumerWithRetry(ctx context.Context, create func(context.Context) (jetstream.Consumer, error)) (jetstream.Consumer, error) {
	var lastErr error
	for attempt := 0; attempt <= len(consumerCreateBackoffs); attempt++ {
		cons, err := create(ctx)
		if err == nil {
			return cons, nil
		}
		lastErr = err
		if attempt == len(consumerCreateBackoffs) {
			break
		}
		if ctx.Err() != nil {
			return nil, lastErr
		}
		select {
		case <-time.After(consumerCreateBackoffs[attempt]):
		case <-ctx.Done():
			return nil, lastErr
		}
	}
	return nil, lastErr
}
```
Export both (backoffs stay `var` for test shortening — see the `withShortBackoffs(t)` note at
`projection.go:88-92`). Update BOTH audit callers (`projection.go:109`, `plugin_consumer.go:194`)
**preserving each caller's error wrap**: `wrapConsumerCreateError` → `AUDIT_CONSUMER_CREATE_FAILED`
(`projection.go:146-152`) and `wrapPluginConsumerCreateError` → `AUDIT_PLUGIN_CONSUMER_CREATE_FAILED`.
**Interlock:** 02.2's D-55 names this relocated wrapper as its provenance stamp site — check whether
02.2 already landed it before budgeting (RESEARCH §0.4-1).

### Retirement reactor subsystem (NEW pkg, host subsystem, event-driven)

**Lifecycle-shape analog:** `internal/world/setup/relay_subsystem.go` — the most recent registered
subsystem. Copy its skeleton exactly:
```go
// relay_subsystem.go:64-80
func NewOutboxRelaySubsystem(cfg OutboxRelaySubsystemConfig) *OutboxRelaySubsystem {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &OutboxRelaySubsystem{cfg: cfg}       // allocates NOTHING (topo-order test requires it)
}
func (s *OutboxRelaySubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemOutboxRelay }
func (s *OutboxRelaySubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemDatabase, lifecycle.SubsystemEventBus}
}
```
- `Prepare` = idempotent, guarded on a field (`relay_subsystem.go:86-89` `if s.relay != nil { return nil }`) — creates the durable consumer, no domain traffic.
- `Activate` = idempotent, guarded on `s.done` (`:119-127`); launches the consume loop on a `context.Background()`-derived cancel ctx.
- `Stop` = resets both guards to nil so retry works (`:145-167`, WR-01 comment).
- Constructors MUST allocate nothing and touch no live resources (`cmd/holomush/core_topo_order_test.go:113-121`).
- Reactor `DependsOn` must include `SubsystemBootstrap` (or resolve `StartLocationID()` lazily — it panics before `Prepare()`, `internal/bootstrap/setup/subsystem.go:299-303`) plus Database/EventBus/World deps as wiring requires.

**Durable-consumer analog:** `internal/eventbus/audit/projection.go:108-127`:
```go
cons, err := createConsumerWithRetry(ctx, func(ctx context.Context) (jetstream.Consumer, error) {
	return js.CreateOrUpdateConsumer(ctx, eventbus.StreamName, jetstream.ConsumerConfig{
		Durable:       cfg.ConsumerName,
		Name:          cfg.ConsumerName,
		FilterSubject: eventbus.SubjectFilter,   // reactor uses "events.*.character.>" instead
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       cfg.AckWait,
		MaxAckPending: cfg.MaxAckPending,
		MaxDeliver:    cfg.MaxDeliver,
	})
})
```
Switch on `Event.Type == "character_retired"` — subject/type are stamped by
`outbox/wire.go:154-170` (`eventbus.Qualify(gameID, "<aggregate>.<id>")`, `Type = env.Kind`).

**Consumer-defined presence interface analog** (`internal/auth/auth_service.go:26-29`, verbatim —
copy this shape rather than importing `internal/presence` directly, to avoid the documented import
cycle):
```go
type PresenceEmitter interface {
	EmitLeave(ctx context.Context, char core.CharacterRef, reason string) error
	EmitSessionEnded(ctx context.Context, char core.CharacterRef, sessionID, cause, reason string) error
}
```
Idempotency-per-effect table: RESEARCH §5.3 (`Store.DeleteByCharacter` returns `(nil, nil)` on
absence ⇒ skip leave; skip move if already at `starting_location_id`; ack-and-skip if status no
longer `retired` via `world.ParseStatus`, `internal/world/lifecycle.go:43-62`).

### `internal/core/session_ended_payload.go` — new cause const

**Analog:** the existing const block (`session_ended_payload.go:24-31`,
`quit | logout | guest_end | kicked | reaped | evicted`). Add `SessionEndedCauseRetired = "retired"`
AND update the struct field comment at `:20` that lists the causes inline (RESEARCH §5.4).

### `last_active_at` KV subsystem (NEW pkg, host subsystem, event-driven + batch)

**Subsystem shape:** same `relay_subsystem.go` skeleton as the reactor.
**Storage seam analog** (`internal/eventbus/subsystem.go:62-71`, verbatim):
```go
// NewSubsystem constructs the subsystem from a Config.
// FileStorage is the default; tests override via NewSubsystemWithStorage.
func NewSubsystem(cfg Config) *Subsystem {
	return NewSubsystemWithStorage(cfg, jetstream.FileStorage)
}

// NewSubsystemWithStorage allows tests to use MemoryStorage for speed.
func NewSubsystemWithStorage(cfg Config, storage jetstream.StorageType) *Subsystem {
	return &Subsystem{cfg: cfg.Defaults(), storage: storage}
}
```
Mirror this pair on the new subsystem so tests force `MemoryStorage` (the corrected D-42 hazard:
`FileStorage` is the ZERO VALUE — an unset bucket is file-backed everywhere, leaking `StoreDir`
residue into memory-configured test harnesses).

**KV wiring (no in-repo analog — pinned-module pattern, RESEARCH §11):**
```go
kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{   // NOT CreateKeyValue — Prepare must be idempotent
	Bucket:      "character_activity",
	Description: "buffered characters.last_active_at epoch-nanos, flushed periodically",
	History:     1,
	Storage:     jetstream.FileStorage,   // EXPLICIT — buckets do not inherit the stream's storage
	Replicas:    1,
})
```
Flusher iterates via `ListKeys` (streaming; `Keys` loads all into memory) and must be
duplicate-tolerant (the monotonic UPDATE below already is).

### Writer-boundary flush function (NEW exported free function in `internal/world/postgres`)

**Analog:** `worldpostgres.BackfillCharacterIdentity` — the exported free-function precedent for an
out-of-`mutate()` writer-boundary entry point (`test/meta/world_import_graph_test.go:92-102`).
**Core SQL** (monotonic + idempotent; NO version bump, NO envelope — do NOT route through `mutate()`
or the census fires):
```sql
UPDATE characters SET last_active_at = $2 WHERE id = $1 AND last_active_at < $2
```
The `UPDATE characters` string literal MUST live in `internal/world/postgres` (`characters` is
fenced; `writerBoundaryDir` is the only allowlist — `test/meta/world_sql_fence_test.go:62-65,
132-141`). The flusher subsystem takes the writer as an injected interface wired from `cmd/holomush`
(the repo's DI habit) or lives in `internal/world/setup` (also allowlisted).

### Subsystem cascade — `internal/lifecycle/subsystem.go` + `cmd/holomush/*`

**Analog:** the 02-12 `SubsystemCharacterNameBlockList` addition. **13 sites, not 5** — work
RESEARCH §3.2's table mechanically (const-block END insertion per `subsystem.go:47-48`'s own
warning; `task generate` for the stringer; `productionSubsystemSet` named fields + return slice +
composite literal in `core.go`; three fixed-size `[18]stubSubsystem` occurrences and two `len == 18`
assertions in `core_subsystems_test.go`; the distinctness ID list; the two real-constructor graphs
with `require.Len(..., 18, ...)`; and the **exact ordered pinned start sequence** at
`core_topo_order_test.go:194-213` — compute insertion position from the `SubsystemID` tie-break at
`:180-182`, don't guess). Do the 18→20 widening ONCE for both new IDs.

### `docs/architecture/invariants.yaml` — INV-WORLD-4 amendment

**Analog:** the entry's own 02-12 amendment sentence (`invariants.yaml:5057-5083`):
> "The count was TWO until 02-12 shipped the third writer; it is amended deliberately rather than
> the atomicity clause being weakened, because what was false was the enumeration and not the guarantee."

Copy the shape: THREE→FOUR + a trailing "The count was THREE until 03-xx…" sentence + amend the
"each emitting its envelope atomically" clause (the flusher emits NO envelope — the minimal honest
amendment per RESEARCH §6.2). Then `go run ./cmd/inv-render` + the three meta-tests. No fabricated
`// Verifies:` — bind only via a test that genuinely asserts the flush.

### `internal/access/policy/seed.go` — self-retire seed (job seed conditional)

**Analog** (`seed.go:39-43`, `seed:player-self-access`):
```go
// SeedPolicy shape: {Name, Description, DSLText, SeedVersion}
// DSL: permit(principal is character, action in ["read","write"], resource is character)
//        when { resource.character.id == principal.character.id };
```
New `seed:player-self-retire`: same shape with `action in ["retire"]` only (deliberately omits
`unretire`, D-40). Admin path needs NO new seed — `seed:admin-full-access` (`seed.go:105-109`) uses
bare `action`. **Do NOT re-derive D-45's `system:` seed** (struck; Pitfall 11). The `job:retirement`
seed is interlock §0.4-2 — carry both branches or plan after 02.2. Any `seed.go` touch fires the
`abac-reviewer` gate before push.

## Shared Patterns

### Guarded-mutation chain (all Track-A code)
`checkAccess` → `characterRepo.Get` (version = CAS guard) → build payload → `s.buildIntent(kind,
aggregate, id, subjectID, payload)` → per-op `s.mutator.<op>` → map `ErrConcurrentEdit` →
`oops.Code(CodeConcurrentEdit)`, `ErrNotFound` → `CHARACTER_NOT_FOUND`. Source:
`internal/world/service.go:799-836`.

### Error handling (all files)
`oops.Code("CODE").With(k, v).Wrap/Wrapf(err, …)` at every boundary; typed sentinels
(`world.ErrConcurrentEdit`, `world.ErrNotFound`) checked with `errors.Is`; tests use
`errutil.AssertErrorCode` (deepest code — fine for `WORLD_CONCURRENT_EDIT`, not for top-level
opacity claims).

### Logging (both new subsystems)
`slog.InfoContext(ctx, "…", "key", val)` — context-carrying variants only (relay_subsystem.go:111,
134 are the in-shape examples); `errutil.LogErrorContext` for errors.

### Event construction (reactor + any emit)
`eventbus.NewEvent(...)`, never `eventbus.Event{}` literals; producers emit domain-relative
subjects, `eventbus.Qualify` prepends `events.<game_id>.` (`.claude/rules/event-conventions.md`).

### Test patterns
- Unit: table-driven testify, ACE names; mockery mocks from `internal/world/worldtest` (regenerate after `SetStatus`).
- Integration: Ginkgo/Gomega, `//go:build integration`; full-stack via `integrationtest.Start(t)`; KV tests use EMBEDDED NATS + the `…WithStorage(MemoryStorage)` seam (`natstest` NOT required — RESEARCH Validation table); two-replica IDENT-10 proof extends `test/integration/resilience/` modelled on `m12_lastwritewins_test.go:41-56`.
- Meta-tests must be observed RED first (census, subsystem counts, invariant registry) — PORTAL-10 rule 4.

## No Analog Found

| File | Role | Data Flow | Reason / What to use instead |
|------|------|-----------|------------------------------|
| KV bucket usage (bucket create, Put/ListKeys, flush loop) | service | batch | First JetStream KV use in the codebase (verified: zero `KeyValueConfig` hits). Use the pinned-module API shapes in RESEARCH §4.2/§11 (`nats.go v1.52.0 jetstream/kv.go`), not memory. |
| Reactor authorization (`JobCaller`, `job:` seed, provenance triple) | middleware/policy | request-response | Phase 02.1/02.2 substrate has NO artifacts yet. Write `[ASSUMED]`-marked caller shapes per RESEARCH §0.2-0.3; do not invent constructor names as fact; the three §0.4 interlocks must be surfaced to the planner, not resolved. |

## Metadata

**Analog search scope:** `internal/world/`, `internal/world/postgres/`, `internal/world/outbox/`, `internal/world/setup/`, `internal/eventbus/audit/`, `internal/eventbus/`, `internal/auth/`, `internal/presence/`, `internal/access/policy/`, `internal/lifecycle/`, `cmd/holomush/`, `test/meta/`, `test/integration/resilience/`
**Files read this session:** 8 (service.go ×2 ranges, mutator.go ×2 ranges, taxonomy.go, projection.go, relay_subsystem.go, auth_service.go excerpt, eventbus/subsystem.go excerpt) + CONTEXT.md + RESEARCH.md (all 03-RESEARCH `[VERIFIED]` citations treated as pre-verified this session, 2026-08-07 re-grounding pass)
**Pattern extraction date:** 2026-08-07

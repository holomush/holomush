---
phase: 03-world-character-commands
plan: 05
subsystem: character-activity
tags: [jetstream-kv, lifecycle, subsystem, world-boundary, invariants, ident-10]
status: complete

requires:
  - "internal/charactivity skeleton + NewSubsystemWithStorage seam (03-02)"
  - "internal/eventbus/consumer.CreateWithRetry (03-02, D-46)"
  - "characters.last_active_at BIGINT epoch-nanos column (migration 000054, Phase 2)"
  - "02.2 D-68 option A+D — the timer-driven-job answer the §0.4-3 interlock defers to"
provides:
  - "internal/world/postgres: UpdateCharacterLastActive (exported writer-boundary free function) + ActivityExecutor"
  - "internal/charactivity: listener.go / flusher.go / ActivityWriter / activityKV / BucketName / DefaultConsumerName / JobName"
  - "internal/testsupport/integrationtest: WithCharacterActivity StartOption"
  - "INV-WORLD-4 amended THREE -> FOUR with a named envelope-exemption clause"
affects:
  - "any future sanctioned out-of-world writer (the enumeration is now FOUR, and one of them is envelope-exempt)"
  - "any future operational-column writer (a second one turns the exemption into a class — the deferred question)"

tech-stack:
  added:
    - "JetStream KV (nats.go v1.52.0) — the repo's FIRST KV bucket"
  patterns:
    - "named function type as an injected writer, so a fenced free function can cross a package boundary no interface could"
    - "revision-conditional delete as the whole concurrency argument for a read/write/delete triple that is deliberately not atomic"
    - "a narrowed KV seam that takes the revision EXPLICITLY, because jetstream's delete options are opaque to a test fake"

key-files:
  created:
    - internal/world/postgres/activity.go
    - internal/world/postgres/activity_test.go
    - internal/world/postgres/activity_integration_test.go
    - internal/charactivity/listener.go
    - internal/charactivity/flusher.go
    - internal/charactivity/charactivity_test.go
    - test/integration/charactivity/charactivity_suite_test.go
    - test/integration/charactivity/character_activity_flush_test.go
  modified:
    - internal/charactivity/subsystem.go
    - internal/charactivity/subsystem_test.go
    - cmd/holomush/core.go
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md
    - internal/testsupport/integrationtest/harness.go
    - internal/testsupport/integrationtest/options.go

decisions:
  - "Interlock §0.4-3 fired on the ANSWERED branch: 02.2's 02.2-05-SUMMARY records D-68 as option A+D and names this flusher explicitly, so the subsystem registers job identity `character_activity` with declared writes [character] at Activate and unregisters at Stop, stamping NO per-execution attributes. Nothing was invented."
  - "FlushInterval default is 5 minutes — the planner-owned D-42 value. It IS the column's worst-case lag, by construction."
  - "The KV seam declares DeleteRevision(ctx, key, revision) rather than jetstream's variadic KVDeleteOpt: deleteOpts is unexported, so a fake could not observe the guarded revision — the one property R1 exists to protect."
  - "A malformed VALUE is dropped revision-conditionally, not unconditionally as the plan's prose said. Only a malformed KEY NAME is purged unconditionally, because a key name is fixed for the key's life and can never become flushable."
  - "Prepare now REFUSES an unwired config and Activate REFUSES an unprepared subsystem, replacing the skeleton's permissive no-ops (retirement's precedent)."

metrics:
  duration: "~70 min"
  completed: 2026-08-09

actuals:
  tokens: 22600
  tasks: 3
  commits: 5
---

# Phase 03 Plan 05: Character Activity Buffer and Flush Summary

`characters.last_active_at` is now actually written — by a durable bus listener that buffers one JetStream KV `Put` per character-actor event and a flush ticker that drains the bucket through a new writer-boundary function, with `INV-WORLD-4`'s enumeration amended THREE → FOUR in the same change and its atomicity clause narrowed rather than weakened.

## What was built

### The writer (`internal/world/postgres/activity.go`)

`UpdateCharacterLastActive(ctx, db ActivityExecutor, characterID, lastActiveNanos)` is a single statement:

```sql
UPDATE characters SET last_active_at = $2 WHERE id = $1 AND last_active_at < $2
```

No transaction, no read-modify-write, no version arithmetic, no envelope. The monotonic predicate carries **all four** idempotency properties the caller needs, and `UPDATE 0` is success in every one of them:

| Case | Why zero rows | Why that is correct |
|---|---|---|
| Stale value | `last_active_at < $2` false | A key left behind by a refused delete is re-flushed with its OLD value next tick and must not regress the column |
| Same value twice | same | `ListKeys` may report a duplicate key under churn |
| Unknown id | `id = $1` false | The character may have been hard-deleted between buffer and flush |
| Never-active (0) | `0 < $2` **true** | The strict `<` is what lets the first positive value overwrite the sentinel |

It lives in `internal/world/postgres` and nowhere else, for the reason `identity_backfill.go` does: `characters` is a fenced table and this is the only allowlisted directory. There is **no** marker escape hatch for `.go` files — the migration scan's `world-sql-fence:allow` is `.sql`-only — so the placement is forced, not preferred.

### The subsystem (`internal/charactivity`)

The 03-02 skeleton's bodies are filled; its `ID()`, `DependsOn()` and the `NewSubsystemWithStorage` pair are untouched, exactly as that plan intended.

| Stage | What it does |
|---|---|
| `Prepare` | `CreateOrUpdateKeyValue` for bucket `character_activity` (`History: 1`, `Storage` **explicit**, `Replicas: 1`) + the durable `character_activity_listener` consumer on `events.>` via `consumer.CreateWithRetry`. Idempotent behind the `s.kv` guard. |
| `Activate` | Registers the job identity, then starts the consume loop and the flush ticker. `done` closes only when **both** have exited. |
| `Stop` | Cancels, **joins both producers**, then runs one final drain, then releases. |

`CreateOrUpdateKeyValue` rather than `CreateKeyValue` is not a style choice: the lifecycle contract requires `Prepare` to be re-runnable, and `CreateKeyValue` answers `ErrBucketExists` on the second boot.

**The emit path never touches Postgres.** The listener decodes the wire event, and on a character actor performs exactly one KV `Put` keyed by the bare actor ULID with the event timestamp as decimal epoch-nanoseconds. It always acks: a buffered-activity loss is bounded by the next event the character causes, whereas an unacked message on a consumer subscribed to *every* subject would eventually stall the whole listener. The trade is deliberately asymmetric.

Actor and timestamp are read from the message **body**, not the `App-Actor-*` headers. The headers carry no event timestamp, and JetStream's metadata timestamp is *store* time; the body is the only place the event's own instant survives. Payload sensitivity is irrelevant — only cleartext envelope metadata is read.

### R1 — why the flush is safe without being atomic

The flusher's `Get` → write → `Delete` triple is not atomic and does not need to be, because every delete is conditioned on the revision the read observed:

```go
func (j jsKV) DeleteRevision(ctx context.Context, key string, revision uint64) error {
	return j.kv.Delete(ctx, key, jetstream.LastRevision(revision))
}
```

`LastRevision` stamps `Nats-Expected-Last-Subject-Sequence`, so the server refuses the delete marker when the latest revision has moved. A refusal is **not a failure**: it means the listener buffered newer activity mid-flush, the key stays carrying the newer timestamp, and the next tick flushes it. The already-written older value is harmless — the writer is monotonic. A deterministic unit test drives exactly that interleave and asserts the survivor still holds `2000`.

**The seam had to be widened to make that testable.** `jetstream`'s `deleteOpts` is unexported, so a fake `KeyValue` receiving `...KVDeleteOpt` can observe *that* an option was passed but never *which revision* it guards — the single property the guard exists to protect. `activityKV` therefore declares `DeleteRevision(ctx, key, revision)`, and the production adapter is the only place the option is constructed.

### The registry amendment

`INV-WORLD-4` now enumerates FOUR writers. The atomicity clause was **narrowed, not weakened**:

> …each **WORLD-STATE** writer among them emits its envelope atomically — writers (1) through (3) — and exactly one, writer (4), is an **OPERATIONAL-COLUMN** writer that emits NO envelope and is named here as the sole exemption…

with the reason at the exemption (`last_active_at` is not world state; one envelope per flushed character per tick would flood the feed), the 02-12-shaped trailing count sentence, and the deferred question recorded inline: *whether the fence should distinguish world-state from operational writers as a CLASS is deferred; a second operational-column writer would turn this exemption into a class.* That is the plan's assumption-delta decision, written where the next reader of the invariant will find it.

`docs/architecture/invariants.md` was regenerated with `go run ./cmd/inv-render`; nothing inside a generated region was hand-edited, and `task lint:invariants` (`inv-render -check`) is green.

Two files were added to `asserted_by`, each because it genuinely asserts a distinct clause:

| File | Clause it proves |
|---|---|
| `internal/world/postgres/activity_integration_test.go` | the writer bumps no `version` and creates no outbox row (the exemption itself) |
| `test/integration/charactivity/character_activity_flush_test.go` | writer (4) exists and reaches the column end to end |

### The full-stack proof

`test/integration/charactivity/` is its **own** suite package with exactly one `RunSpecs` call, so `-run TestCharacterActivityFlush` names a real entry function rather than passing vacuously (SG-1). The spec boots the real subsystem through the new `WithCharacterActivity(jetstream.MemoryStorage, 250ms)` harness option — the same `Prepare`/`Activate`/`Stop` contract production runs, with the *real* writer-boundary function as the injected writer, not a harness stand-in — publishes one character-actor event, and asserts, in order:

1. the KV key appears carrying the event's nanos, **while the column is still `world.NeverActive`** — the emit path performed no database write;
2. one tick later the column equals the buffered value;
3. `characters.version` is unchanged and the outbox row count is unchanged;
4. the flushed key is gone (`ErrKeyNotFound`), so the bucket stays bounded.

## Interlock §0.4-3 — which branch fired

**The ANSWERED branch.** `02.2-05-SUMMARY.md` (§"D-68's answer for `03-05-PLAN.md:100`'s reserved timer-job decision") settles it verbatim: D-68 is option **A + D**, so *every* background consumer registers a `job:` identity and a declared capability class, and *only event-driven* ones additionally carry per-execution instance scoping. It names this flusher explicitly.

So the subsystem registers `JobName = "character_activity"` with `jobWrites = ["character"]` at `Activate` and unregisters at `Stop`, and stamps **no** per-execution attributes because it is timer-driven. Nothing was invented, and the second half of 02.2's answer is carried in the constant's doc comment rather than silently dropped: this write lands at the `INV-WORLD-4` boundary and crosses **no ABAC chokepoint**, so nothing consumes the identity at an `Evaluate` call today — which is a fact about the current write path, not a reason to skip the identity. The moment anything in this subsystem does cross a chokepoint, the identity and class are already there and already correct.

`cmd/holomush` passes **the same** `jobRegistry` instance `abacSub` reads, for the reason the retirement reactor's wiring states: a second registry would report the job as not running.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Unconditional delete of a malformed VALUE would destroy a concurrent Put**

- **Found during:** Task 2
- **Issue:** The plan says *"Malformed keys/values: log and Delete unconditionally (poison-pill hygiene — no revision guard needed on garbage)."* For a malformed **key name** that is right — a key name is fixed for the key's life, so it can never become flushable and no future `Put` is being protected. For a malformed **value** it is wrong: the key is a valid character ULID, so a listener `Put` can (and eventually will) replace the garbage with a good timestamp, and an unconditional delete at that moment destroys it. That is precisely the R1 failure mode the plan adopted revision-conditional deletes to prevent.
- **Fix:** Split the two. A key name that does not parse as a ULID is purged unconditionally; a key whose *value* does not parse is dropped at the revision the flush read. Both bound the bucket; only the second can ever race a `Put`, and it now loses that race safely.
- **Files modified:** `internal/charactivity/flusher.go`
- **Commit:** 2fb0d1f48

**2. [Rule 2 - Missing critical functionality] The skeleton's permissive lifecycle no-ops became fail-open**

- **Found during:** Task 2
- **Issue:** 03-02's skeleton had `Activate` succeed without `Prepare` (documented as "must not panic") and `Prepare` succeed with an empty config. Once the bodies are real, both silently produce a subsystem that never writes `last_active_at`, with nothing to point at.
- **Fix:** `Prepare` returns `CHARACTER_ACTIVITY_UNWIRED` for a nil writer / nil JetStream provider / nil handle; `Activate` returns `CHARACTER_ACTIVITY_NOT_PREPARED`. This is the retirement reactor's precedent (`RETIREMENT_REACTOR_UNWIRED` / `RETIREMENT_REACTOR_NOT_PREPARED`) and the orchestrator always runs the whole `Prepare` sweep first, so neither fires in production.
- **Files modified:** `internal/charactivity/subsystem.go`, `internal/charactivity/subsystem_test.go`
- **Commit:** 2fb0d1f48

**3. [Rule 3 - Blocking] The `RefreshConnection` acceptance grep is defeated by prose**

- **Found during:** Task 2
- **Issue:** The listener's doc comment named `session.RefreshConnection` as the deliberately-unhooked seam. The plan's acceptance gate is `! rg -q 'RefreshConnection' internal/charactivity/`, which cannot tell a comment from a call — so documenting the prohibition broke the check that enforces it.
- **Fix:** The comment now names `internal/session/session.go`'s connection lease-refresh path by file and concept rather than by symbol, and says why the symbol is not spelled. The prohibition stays documented and the gate stays mechanical.
- **Files modified:** `internal/charactivity/listener.go`
- **Commit:** 2fb0d1f48

### Two acceptance criteria needing a note (both PASS on substance)

- `! rg -q '\.Keys\(' internal/charactivity/flusher.go` reports a hit on `lister.Keys()`. The criterion's intent is to forbid `kv.Keys(ctx)`, the variant that loads every key into memory; `KeyLister.Keys()` is the *channel accessor of the streaming API the criterion requires*. `rg -o 'kv\.Keys\(' ` over the package returns nothing — the substantive property holds and the regex is over-broad.
- `rg -c 'RunSpecs' test/integration/charactivity/` returns 3, not 1, because the doc comments explain the one-`RunSpecs`-per-package rule. Call sites: `rg -o 'RunSpecs\(' ` returns exactly **1**.

## Threat mitigations applied

| Threat | Disposition | Where |
|---|---|---|
| T-03-16 (Info disclosure, presence oracle) | accepted as planned | the column lags by up to one flush interval by construction; no new surface exposes it |
| T-03-17 (DoS, unbounded KV growth) | mitigated | delete-after-flush + `History: 1`; failed keys retry next tick; malformed key names purged, malformed values dropped at their read revision |
| T-03-18 (Tampering, fenced SQL escaping) | mitigated | the `UPDATE characters` literal is in `internal/world/postgres` only (SQL fence green); `! rg 'pgx\|pgxpool' internal/charactivity/` holds — the flusher package imports no driver |
| T-03-19 (Tampering, spurious envelopes) | mitigated | bare `UPDATE`, no executor; both the unit test (`NotContains "version"`) and two integration specs pin version + outbox-count unchanged |
| T-03-20 (EoP, invented flusher provenance) | mitigated | the interlock's ANSWERED branch fired; 02.2's D-68 was followed verbatim, and no `trigger_kind` or tick-provenance shape was minted |

## Verification

| Command | Result |
|---|---|
| `task test` | 11357 tests, 4 skipped (quarantined), **exit 0** |
| `task test -- ./internal/charactivity/ ./internal/world/postgres/ ./cmd/holomush/ ./test/meta/` | 791 tests, **exit 0** |
| `task test:int -- ./internal/world/postgres/` | 333 tests, **exit 0** |
| `task test:int -- -run TestCharacterActivityFlush ./test/integration/charactivity/` | 1 test, **exit 0** |
| `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` | 7 tests, **exit 0** |
| `task test -- -run 'TestWorldSQLFence' ./test/meta/` | 8 tests, **exit 0** |
| `go run ./cmd/inv-render` | clean regeneration; `lint:invariants` (`-check`) green |
| `task lint` | **exit 0** before every commit; `task fmt` output committed |
| `task test:int -- -run TestZZZNoSuchTest ./test/integration/... ./internal/testsupport/...` | **exit 0** — the harness change compiles against every integration suite |

All verdicts read from the **exit code**, never from matched output.

## Known Stubs

None. Every symbol this plan created is fully implemented; no `TODO` / `FIXME` / `not implemented` exists in the three touched packages.

## Success Criteria

| Criterion | Status |
|---|---|
| ROADMAP criterion 5 — `last_active_at` is written, with no per-event database write | met |
| The emit path provably never touches Postgres | met — asserted mid-spec, before the flush tick |
| The flush is monotonic, idempotent, version-neutral, envelope-free | met |
| `INV-WORLD-4` amended THREE → FOUR with the exemption clause, regenerated, meta-tests green | met |
| Tests run on embedded NATS with `MemoryStorage` through the seam | met — `WithCharacterActivity` takes the storage type explicitly |
| The flusher-provenance question consumed-or-recorded, never invented | met — consumed (02.2 D-68) |

## Requirement status

**IDENT-10 is met** by this plan: the column now has its writer, and the whole path is proven end to end. **IDENT-04** is not this plan's to close — it belongs to 03-04's reactor and 03-06's full-stack proof — so it is left as those plans set it.

## Note on an unrelated dirty file

`.claude/agent-memory/abac-reviewer/MEMORY.md` shows as modified in the working tree. It is a curation of the abac-reviewer's memory from the 02.2 phase review and is unrelated to this plan's files; per the scope boundary it was neither fixed nor committed here.

## Self-Check: PASSED

All eight created files exist on disk; all five commit hashes resolve in `git log`.

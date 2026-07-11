<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# F1 resilience verdict — world-model concurrency and dual-write under chaos

This note is the OPS-05 evidence document (#4791). It records what the gated
two-replica resilience harness (`test/integration/resilience/`) empirically
demonstrates about the world model under concurrency and broker chaos, quoting
the verbatim verdict lines from a single canonical run. It is one of the two
grounding inputs the MODEL-01 ADR (#4784) consumes; the sibling
[`f1-eventsourcing-why.md`](f1-eventsourcing-why.md) records *why* the
architecture is CRUD-not-event-sourced. This note is deliberately **neutral**
between the ADR's options — the weighing belongs to the ADR/decision, not here.

## Verdict summary

- **M12 — last-write-wins, no version guard: REPRODUCED deterministically.** The
  world model is direct-write CRUD in Postgres with no optimistic-concurrency
  control. Two replicas racing a write to the same location row produce a silent
  lost update: one replica's stale full-row `UPDATE` reverts a field the other
  already committed, and **both writers return `nil`** — no conflict is ever
  surfaced. Proven by an explicit-interleave mechanism spec (always reproduces),
  reinforced by a command-fidelity race (N=50, every round loses one write with
  zero conflict signals). A hybrid cross-field natural-window race observed k=0
  of N=100 this run — which per design does **not** refute M12; it only bounds
  the *uncontrolled* window as narrow. The deterministic proof is the verdict.

- **M2 — dual-write non-atomicity: window CHARACTERIZED.** `MoveCharacter`
  commits the character row first and emits the move notification *post-commit*;
  the two are not atomic. With a wired emitter and a broker frozen mid-move, the
  DB commit persists, the caller receives an emit-failure error carrying
  `move_succeeded=true`, and the notification's actual delivery is **decoupled**
  from that error: this run it was delivered *late and out-of-band* after the
  caller had already seen failure (the frozen broker buffered the publish in the
  client TCP send buffer and flushed it on unpause); on other timings it can be
  lost outright. Either way the caller cannot know whether the notification
  landed — that ambiguity **is** the non-atomicity window (D-07 asks only that
  the window be shown to exist, not that a deterministic loss be forced). A
  further first-class finding: **production wires no emitter at all**
  (`internal/world/setup/subsystem.go` omits `EventEmitter`), so today the entire
  move-notification leg is dead code — every move reports `EVENT_EMITTER_MISSING`
  with `move_succeeded=true` while the DB commits unconditionally.

- **Restart / reconnect: state recovers from the database, no replay.** A
  restarted replica boots cleanly against the already-existing EVENTS stream and
  serves pre-restart state straight from the shared database — recovery is a **DB
  read, not an event-sourced rebuild**; no replay runs at boot. A detached client
  reattaches (blocking until `REPLAY_COMPLETE`) and resumes live delivery. After
  a broker flap, publishing recovers once the broker is unpaused.

## Reproduction

Single command (Docker must be running — the harness starts a NATS JetStream
testcontainer):

```bash
HOLOMUSH_RUN_QUARANTINED=1 task test:int -- \
  -run TestWorldModelResilience -timeout 30m ./test/integration/resilience/
```

- **Suite location:** `test/integration/resilience/` (Ginkgo, build tag
  `//go:build integration`). Specs: `boot_smoke_test.go`,
  `m12_lastwritewins_test.go`, `m2_dualwrite_test.go`, `restart_reconnect_test.go`.
- **Typical runtime:** ~44s of Ginkgo execution (12 specs) plus a one-time
  `plugin:build:host` step; well under the 30m budget.
- **Gate:** the suite is nightly/opt-in (D-05) — it self-skips on the required
  `Integration Test` PR lane and runs only under `HOLOMUSH_RUN_QUARANTINED=1`, so
  it stays off the blocking lane while serving as the **standing regression check
  for Phase 5's MODEL-03 guard**. With the env var unset the suite skips (0 specs
  run, exit 0).

## Evidence

Verbatim verdict lines from the canonical `HOLOMUSH_RUN_QUARANTINED=1` run
(exit 0; captured via the suite's `RESILIENCE_VERDICT_LOG` sink), each mapped to
its spec and file.

M2 dual-write window — `test/integration/resilience/m2_dualwrite_test.go`:

```text
M2-VERDICT: control: healthy broker — move committed (row at 01KX9BCP7JX5XEBRE6BV3M4DP1) AND the move notification reached the stream subject events.main.location.01KX9BCP7JX5XEBRE6BV3M4DP1 (event type "move")
M2-VERDICT: flap-window: DB commit survived (row at 01KX9BCP7P39TN2DB1NFWPGWX0) + caller error EVENT_EMIT_FAILED (outer CHARACTER_MOVE_EVENT_FAILED) with move_succeeded=true, while the notification delivery was DECOUPLED from that result: delivered LATE and out-of-band after the caller already saw emit failure (frozen-broker TCP buffer flushed on unpause) — the D-07 non-atomicity window is real (the caller cannot know whether the notification landed)
M2-VERDICT: production-shape: with NO emitter wired (production construction), the move committed (row at 01KX9BDSEE0BV9X7GK8Q3NXH71) but reported EVENT_EMITTER_MISSING (outer CHARACTER_MOVE_EVENT_FAILED) with move_succeeded=true — the M2 notification leg is dead code in production today
```

M12 last-write-wins — `test/integration/resilience/m12_lastwritewins_test.go`:

```text
M12-VERDICT: setup: two replicas booted over one broker + one shared DB with in-tree plugins; A4 dual-plugin fallback NOT needed (locID=01KX9BDW9KEKEJ7KTVQ46BHAYV)
M12-VERDICT: deterministic-interleave: reproduced deterministically (A's rename "TestLoc_01KX9BDW"->"name-A-committed" silently reverted to "TestLoc_01KX9BDW" by B's stale full-row UPDATE; both UpdateLocation calls returned nil)
M12-VERDICT: concurrent-describe: both-succeed-no-conflict N=50 (every round: both commands returned success, one write silently superseded, zero conflicts surfaced; 50 writes lost)
M12-VERDICT: cross-field-race: k=0 of N=100 natural-window races lost (window never interleaved; NOT a refutation — the deterministic proof in spec 1 stands)
```

Restart / reconnect / broker flap — `test/integration/resilience/restart_reconnect_test.go`:

```text
CHAOS-VERDICT: replica-restart: B' booted cleanly against the existing EVENTS stream and served pre-restart state "written-before-restart" from the shared DB (recovery is DB-read, not event replay)
CHAOS-VERDICT: client-reconnect: session detached then reattached (REPLAY_COMPLETE observed) and resumed live delivery — EVENTS LastSeq advanced past 0
CHAOS-VERDICT: broker-flap: publishing recovered after a docker-pause flap (paused-attempt=returned-error; LastSeq advanced past 1 after unpause)
```

## Mechanism

**M12 (lost update) citation chain:**

- The write-lost scenario is driven by a read-modify-write: a caller reads a
  location, mutates one field in its in-memory copy, and writes the whole row
  back — `internal/property/entity_mutator.go` (the `describe`/property path from
  `plugins/core-objects` via the `SetProperty` host capability).
- `internal/world/postgres/location_repo.go` `Update` issues an **unguarded
  full-row** `UPDATE locations SET type, shadows_id, name, description, owner_id,
  replay_policy, archived_at WHERE id = $1` — every field is overwritten from the
  caller's copy, with **no version predicate** in the `WHERE`.
- The `locations` table in `internal/store/migrations/000001_baseline.up.sql`
  carries **no version column**, so no optimistic-concurrency check is even
  possible today.
- Grounded correction: the `plugins/core-objects` `set` command is a write-less
  stub, so cross-field corruption is demonstrated via a `describe` command racing
  the identical `world.Service` rename path (both funnel into the same full-row
  `UPDATE`), not via `set`.

**M2 (dual-write non-atomicity) citation chain:**

- `internal/world/service.go` `MoveCharacter` commits the character row
  (`UpdateLocation`) and only *then* calls `EmitMoveEvent` post-commit; on emit
  failure it returns `CHARACTER_MOVE_EVENT_FAILED` with `move_succeeded=true` (the
  row already persisted).
- `internal/world/events.go` `emitWithRetry` retries the publish 3 times with
  exponential backoff from 50ms (~350ms window) before surfacing
  `EVENT_EMIT_FAILED`; with a nil emitter `EmitMoveEvent` returns
  `EVENT_EMITTER_MISSING`. Per `samber/oops` the caller-visible `Code()` is the
  deepest chain code (`EVENT_EMIT_FAILED` / `EVENT_EMITTER_MISSING`), wrapped by
  the outer `CHARACTER_MOVE_EVENT_FAILED` categorization; `move_succeeded=true`
  merges up through the chain context.
- `internal/world/setup/subsystem.go` constructs the production `world.Service`
  with **no `EventEmitter`** — so the notification leg is inert in production, and
  the harness must deliberately wire an emitter to make the window observable.

## Implications for MODEL-01

The evidence is presented neutrally; it constrains **both** ADR options equally
and expresses no preference.

- **For option A (build event sourcing):** M12 and M2 are both consequences of
  state being the source of truth with events as a post-commit side effect. Under
  an event-first model the append *is* the write, so the dual-write gap (M2)
  cannot arise and the append sequence supplies ordering (M12). The evidence shows
  the class of defect an event-sourced foundation would structurally remove — at
  the cost of building the projection/rebuild path that does not exist today
  (restart recovery is currently a DB read, not a replay).

- **For option B (formally adopt CRUD-canonical):** the same evidence scopes the
  compensating controls precisely — M12 needs optimistic concurrency (a version
  column + version-predicated `UPDATE`, neither of which exists today), and M2
  needs a transactional outbox so the DB write and the event publish become
  durable together. The restart/reconnect evidence confirms the CRUD recovery
  model already works (state is DB-derived), and the production finding (no
  emitter wired) means the M2 notification leg would have to be *built and wired*,
  not merely fixed, before an outbox is meaningful.

Neither reading is endorsed here. The decision — including whether world-state
event sourcing was ever intended — is the ADR's to make.

## Cross-references

- **#4791** — OPS-05 (world-model resilience harness; this document is its
  evidence output).
- **#4784** — MODEL-01 (the world-model architecture ADR that consumes this doc).
- **#4798** — M12 last-write-wins finding.
- [`f1-eventsourcing-why.md`](f1-eventsourcing-why.md) — the archaeology of *why*
  the world model is CRUD-not-event-sourced (the ADR's other grounding input).
- Suite: `test/integration/resilience/` — the standing regression check, opt-in
  via `HOLOMUSH_RUN_QUARANTINED=1`.

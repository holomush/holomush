---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 08
subsystem: world-model / transactional-outbox resilience gate
tags: [outbox, relay, lease, consumer, fault-injection, resilience, M2, MODEL-04, D-05, chaos]
requires: [05-06, 05-07]
provides:
  - "outbox relay fault-injection matrix (relay-crash-around-PubAck / dual-relay lease fencing / duplicate-delivery / broker-downtime) over the two-replica resilience harness"
  - "M2 end-to-end closure: relay redelivers a move envelope committed during a broker blip (notification not lost)"
  - "per-aggregate concurrent-writer race coverage for all four world aggregates (location/exit/character/object) under two replicas + one broker + one shared DB"
  - "chaos_helpers relay/consumer construction seams: setup.NewOutboxStore adapter reuse, test-local checkpoint adapter, same-tx outbox row seeder, per-subject stream-count + published_at inspectors"
affects: [05-10, 05-11, 05-12]
tech-stack:
  added: []
  patterns: [production-adapter-reuse-in-test, per-spec-unique-game-id-isolation, per-subject-stream-count-assertion, durable-generation-fence-on-live-conn, lease-release-via-defercleanup, crash-before-mark-wrapper]
key-files:
  created:
    - test/integration/resilience/outbox_faultinjection_test.go
  modified:
    - test/integration/resilience/chaos_helpers_test.go
    - test/integration/resilience/m2_dualwrite_test.go
  deleted: []
decisions:
  - "The integrationtest harness does NOT run the OutboxRelaySubsystem (it is production-wired at the composition root), so the specs construct the REAL relay/lease/reference-consumer directly over the shared stack via the production setup.NewOutboxStore adapter — the relay under test is the real 05-07 relay with a real generation-fenced advisory-lock lease and a real external-NATS publisher."
  - "Specs drive Drain/Apply EXPLICITLY (sweep-only relay, nil Waker) so every fault window is deterministic — no background wakeup races an assertion."
  - "Per-spec UNIQUE game ids isolate each fault-injection spec's feed counter / lease / positions (start at 1) and its wire subjects, so per-subject stream counts are immune to other broker traffic and cross-spec row accumulation."
  - "The durable generation fence is proven on a LIVE connection: the NEW holder marking under the OLD generation re-reads the durable lease_generation column and fails closed (round-4 A2) — stronger than a released-flag check."
  - "The at-least-once wire / exactly-once effect framing (round-3) is asserted, NOT a 'duplicates impossible' claim: a replayed envelope across the handoff is absorbed by the durable receipt (Nats-Msg-Id / event-ULID dedup)."
  - "Character has no full-object service update (UpdateCharacterDescription does its own read-modify-write), so its per-aggregate race exercises the guarded character repo directly — the same CAS + zero-row classifier the service delegates to."
metrics:
  duration: ~110min
  tasks: 2
  files: 3
  completed: 2026-07-12
status: complete
---

# Phase 5 Plan 08: Outbox Fault-Injection Matrix + M2 End-to-End Closure Summary

The empirical enforcement layer the one-pager names: the permanent D-05
two-replica resilience suite gains the outbox fault-injection matrix, the M2
end-to-end redelivery assertion, and per-aggregate race coverage. 05-06 proved
the outbox write is DB-atomic; 05-07 built the single leased relay; this plan
proves the feed survives REAL broker chaos end-to-end and that the guard holds
per-aggregate under two replicas.

## What was built

**Task 1 — outbox fault-injection matrix (`outbox_faultinjection_test.go` + chaos_helpers seams).**
A new `Describe("Outbox relay fault-injection matrix")` drives the REAL relay /
lease / reference consumer over two in-process replicas, one external NATS
broker, and one shared Postgres:

- **relay crash around PubAck** — a `crashBeforeMarkStore` wrapper fails the
  FIRST `MarkPublished` after a successful PubAck (the durability gap). Drain #1
  publishes (wire subject count 1) then "crashes" (transient, no halt); the row
  stays unpublished. Drain #2 (restart) redelivers the still-unpublished row with
  the SAME Nats-Msg-Id — JetStream dedup keeps the stored count at 1 (no double
  effect on the wire) — and marks it published. The reference consumer then
  dedups the same envelope on the event ULID (durable receipt): applied once.
- **dual relay / lease fencing** — replica A acquires the per-game advisory-lock
  lease (generation g1); replica B's bounded `AcquireLease` BLOCKS and expires
  (only the holder makes DB-side progress). A publishes+marks row1 under g1, then
  releases (a dropped holder connection); B acquires with a durably bumped g2.
  B marking under A's STALE g1 re-reads the durable `lease_generation` column and
  is REJECTED on a LIVE connection (round-4 A2); the released holder's ack is also
  stale. B drains row2 under g2. Across the handoff a replayed row1 is a
  receipt-deduped no-op — the consumer-visible effect is exactly-once and the
  watermark advances to position 2 with no skip or duplicate (at-least-once wire,
  exactly-once effect — round-3 framing; no "duplicates impossible" claim).
- **duplicate delivery** — the same envelope delivered twice is applied once
  (durable receipt dedup): first Apply true, second Apply false, one durable
  effect committed.
- **broker downtime** — `pauseBroker` freezes the broker; a bounded Drain stalls
  to the ceiling and surfaces as a transient outage (nothing published, the relay
  does NOT halt — it backs off). On `unpauseBroker` the relay resumes and drains
  all three rows in feed-position order (published_at non-decreasing with
  feed_position; each subject stored exactly once).

`chaos_helpers_test.go` gained the reusable seams: `outboxStoreFor` (the
production `setup.NewOutboxStore` adapter over the postgres store), `busPublisher`
(the replica's external-mode publisher), `newOutboxRelay`, `seedOutboxRow` (a
same-tx outbox writer), `envelopeSubject`, a test-local `checkpointStoreAdapter`
(mirroring the unexported setup adapter) + `newReferenceConsumer`,
`streamSubjectCount` (per-subject stream count over an independent connection),
and `outboxPublishedAt`.

**Task 2 — M2 end-to-end redelivery + per-aggregate races (`m2_dualwrite_test.go`).**
The M2 Describe gains the assertion the 05-06 rewrite deferred: a `MoveCharacter`
under a frozen broker commits state + the move envelope in one transaction, so the
envelope is committed-but-UNPUBLISHED during the blip (the notification is
PENDING, not lost). After `unpauseBroker` the single leased relay publishes the
move envelope — `published_at` is set only after PubAck, and the char subject
count advances — so a NATS blip after commit cannot lose the notification (M2
closed end-to-end). The Describe doc + a new `M2-VERDICT: relay-redelivery` line
record the closure.

A new `Describe("Per-aggregate concurrent-writer races")` (two replicas, one
broker, one shared DB) proves the version guard surfaces `WORLD_CONCURRENT_EDIT`
on ALL FOUR aggregates: location / exit / object race deterministically through
the service (both replicas read the same version, one commits, the other's stale
write is rejected); character races the guarded character repo directly (no
full-object service update exists — `UpdateCharacterDescription` does its own
read-modify-write), exercising the same CAS + zero-row classifier the service
delegates to.

## Verification

- `task lint` — green (exit 0). (golangci-lint does not process `//go:build
  integration` files by default, matching every existing integration test file;
  style follows the existing suite conventions.)
- Integration compile of `./test/integration/resilience/` under
  `-tags=integration` — green (`-run zzzNoMatchCompileOnly`, 0 tests, clean
  build).
- **D-05 resilience suite RUN (opt-in gate):**
  `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience
  ./test/integration/resilience/` — **PASS (24.9s)**. Captured verdict lines:
  - `CHAOS-VERDICT: relay-crash-around-PubAck: envelope redelivered on restart; Nats-Msg-Id dedup kept the wire at 1 message; reference consumer applied the event ULID exactly once`
  - `CHAOS-VERDICT: dual-relay: single-lease DB progress (non-holder blocked); handoff bumped generation 1->2; stale-generation ack REJECTED against the durable column; consumer effect exactly-once across the handoff`
  - `CHAOS-VERDICT: duplicate-delivery: the same envelope delivered twice applied exactly once (durable receipt dedup)`
  - `CHAOS-VERDICT: broker-downtime: relay published nothing while frozen (no halt), then resumed and drained all 3 rows in feed-position order on recovery`
  - `M2-VERDICT: relay-redelivery: a move committed while the broker was frozen was PUBLISHED by the relay after recovery — a NATS blip after commit cannot lose the notification (M2 closed end-to-end)`
  - `M2-VERDICT: per-aggregate-{location,exit,character,object}: two-replica stale write rejected with WORLD_CONCURRENT_EDIT`
- With `HOLOMUSH_RUN_QUARANTINED` unset the suite self-skips (off the blocking PR lane).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Relay leases pinned pool connections that blocked harness teardown**
- **Found during:** first full D-05 suite run (Task 1 + Task 2 verification).
- **Issue:** each test-constructed relay (and the manual dual-relay leases)
  acquires an advisory-lock lease that PINS a pool connection. The specs never
  released them, so `pgxpool.Close()` during the harness LIFO teardown BLOCKED on
  the leaked connection. This stalled the whole cleanup chain — an m12 replica's
  `core-scenes` plugin scheduler never got stopped and spammed SASL-auth failures
  for ~9 minutes until the go-test 10m timeout fired (2 failures, exit 1). All
  spec BODIES had already passed (every verdict printed); the failure was purely
  the teardown hang.
- **Fix:** release every lease at spec end via `DeferCleanup` — `relay.Stop(...)`
  for each relay (crash / broker-downtime / M2-redelivery), and idempotent
  `lease.Release(...)` for the manual dual-relay leases. Re-run: PASS in 24.9s
  (down from a 10m timeout).
- **Files modified:** outbox_faultinjection_test.go, m2_dualwrite_test.go.
- **Commit:** 801da167a (Task 1), c207a1c8a (Task 2).

### Scope / process notes

- Per the identical per-task verify command (the ~24s D-05 quarantined run
  covers BOTH tasks), the empirical suite was run ONCE after both tasks landed
  rather than twice; each task additionally compile-checked under
  `-tags=integration` before commit. The one run exercises every new spec.
- No `test/quarantine.yaml` row was added: the suite is gated by
  `quarantinetest.Enabled()` (the D-05 opt-in idiom), not per-spec quarantine
  markers — the new files carry none of the bijection-meta-test marker patterns
  (per resilience_suite_test.go's package doc).

## Known Stubs

None. The reference consumer's LIVE durable-JetStream consume loop + genesis
snapshot remain 05-11's forward boundary (unchanged by this plan); the specs drive
the relay's Drain and the consumer's Apply directly, which is the correct
seam-level exercise for a fault-injection gate.

## Threat Flags

None. No new network endpoint, auth path, or trust-boundary schema change beyond
the plan's `<threat_model>` (T-05-24/25/26 all mitigated by the broker-downtime /
dual-relay / duplicate-delivery specs respectively). The tests import no
`internal/access`/crypto surface; the relay publishes already-authorized,
already-committed facts.

## Self-Check: PASSED

- FOUND: test/integration/resilience/outbox_faultinjection_test.go
- FOUND: test/integration/resilience/chaos_helpers_test.go
- FOUND: test/integration/resilience/m2_dualwrite_test.go
- FOUND commit: 801da167a (Task 1 — fault-injection matrix + helpers)
- FOUND commit: c207a1c8a (Task 2 — M2 redelivery + per-aggregate races)
- D-05 resilience suite: PASS (24.9s) with all CHAOS-VERDICT + M2-VERDICT lines emitted; `task lint` green; integration compile green.

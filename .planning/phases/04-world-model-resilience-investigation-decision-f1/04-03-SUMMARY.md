---
phase: 04-world-model-resilience-investigation-decision-f1
plan: 03
subsystem: testing
tags: [resilience, m2, dual-write, eventbus, evidence, world-model, ops-05, model-01]

# Dependency graph
requires:
  - phase: 04-world-model-resilience-investigation-decision-f1
    provides: "plan 01 two-replica harness (WithExternalNATS/WithSharedDatabase, startReplica, pauseBroker/unpauseBroker, eventsStream) + plan 02 newWorldService/reportVerdict/RESILIENCE_VERDICT_LOG + M12/CHAOS verdict lines"
provides:
  - "M2 dual-write non-atomicity window CHARACTERIZED (D-07): commit persists, caller error carries move_succeeded=true, notification delivery decoupled from the result"
  - "Production finding pinned by a spec: world.Service wires NO EventEmitter (setup/subsystem.go) — the move-notification leg is dead code in production today"
  - "worldBusAppender + newEmittingWorldService test helpers (raw-publisher emitter wiring for world events)"
  - "f1-resilience-verdict.md — the OPS-05 evidence document consolidating M12/M2/CHAOS verdicts, neutral for MODEL-01 (ADR grounding input)"
affects: [04-04-PLAN (MODEL-01 world-model ADR — consumes f1-resilience-verdict.md)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "worldBusAppender publishes world events via the RAW subsystem publisher (s.Bus().Bus.Publisher()), never the rendering publisher — host-owned 'move' has no plugin-qualified verb so a verb-registry Lookup would hard-fail EMIT_UNKNOWN_VERB; stream-presence needs no rendering metadata"
    - "Stream presence/absence asserted via jetstream GetLastMsgForSubject over an INDEPENDENT connection (eventsStream), never a replica's cached view"
    - "oops.Code() returns the DEEPEST non-nil code in the chain (samber/oops@v1.22 error.go getDeepestErrorCode) — the outer categorization code is NOT what AsOops().Code() surfaces"

key-files:
  created:
    - test/integration/resilience/m2_dualwrite_test.go
    - docs/reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md
  modified:
    - test/integration/resilience/chaos_helpers_test.go

key-decisions:
  - "Leg 4 of the flap spec RECORDS the observed delivery outcome (lost vs late out-of-band) instead of asserting permanent absence — the frozen broker buffers the publish in the client TCP send buffer and flushes on unpause, so a hard absence assertion is empirically false and flaky. D-07 asks only that the window be shown to exist, so the caller's move_succeeded error being decoupled from actual delivery IS the characterization."
  - "M2 error codes asserted per-spec at the deepest chain code: flap window = EVENT_EMIT_FAILED, production shape = EVENT_EMITTER_MISSING, both wrapped by outer CHARACTER_MOVE_EVENT_FAILED with move_succeeded=true (the load-bearing D-07 signal)"
  - "Single replica for M2 (write-vs-broker race, not a replica race); no plugins (MoveCharacter has no in-tree command)"

patterns-established:
  - "newEmittingWorldService(server): world.Service with a deliberately-wired EventEmitter (EventStoreAdapter over worldBusAppender) — the test-only emitter that makes the post-commit notification observable (production wires none)"

requirements-completed: [OPS-05, MODEL-01]

coverage:
  - id: D1
    description: "M2 dual-write non-atomicity window characterized under a wired emitter + frozen broker: DB commit persists, caller error carries move_succeeded=true, notification delivery decoupled from the caller result (D-07)"
    requirement: OPS-05
    verification:
      - kind: integration
        ref: "HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience -timeout 30m ./test/integration/resilience/ (m2_dualwrite_test.go: flap-window spec — commit + move_succeeded=true + decoupled delivery)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Control spec proves the emit leg works when the broker is healthy (move commits AND move event reaches the destination location subject with event type 'move')"
    requirement: OPS-05
    verification:
      - kind: integration
        ref: "m2_dualwrite_test.go: control spec — GetLastMsgForSubject over an independent conn confirms the move message on events.<gid>.location.<dest>"
        status: pass
    human_judgment: false
  - id: D3
    description: "Production-shape spec pins the finding that no EventEmitter is wired in production world.Service — every move reports EVENT_EMITTER_MISSING with move_succeeded=true while the DB commits"
    requirement: OPS-05
    verification:
      - kind: integration
        ref: "m2_dualwrite_test.go: production-shape spec — newWorldService (emitter-less production construction) → CHARACTER_MOVE_EVENT_FAILED/EVENT_EMITTER_MISSING + move_succeeded=true + DB row moved"
        status: pass
    human_judgment: false
  - id: D4
    description: "OPS-05 success criterion #2: documented, reproducible verdict at the canonical location, quoting the evidence run, neutral for MODEL-01, cross-linked from #4791"
    requirement: MODEL-01
    verification:
      - kind: other
        ref: "docs/reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md (task lint exit 0; verbatim M12/M2/CHAOS lines; D-01 neutrality); #4791 comment https://github.com/holomush/holomush/issues/4791#issuecomment-4948536272"
        status: pass
    human_judgment: false

# Metrics
duration: ~55min
completed: 2026-07-11
status: complete
---

# Phase 04 Plan 03: M2 Dual-Write Window + F1 Resilience Verdict Summary

**M2 dual-write non-atomicity window CHARACTERIZED (D-07) — the character row commits before the move notification emits, and with a frozen broker the caller receives `move_succeeded=true` on emit failure while delivery is decoupled from that result; plus the pinned production finding that no emitter is wired at all — all consolidated into the neutral OPS-05 verdict document the MODEL-01 ADR consumes.**

## Performance

- **Duration:** ~55 min
- **Completed:** 2026-07-11
- **Tasks:** 2
- **Files modified:** 3 (1 new spec file, 1 helper file extended, 1 new evidence doc)
- **Canonical suite runtime (green):** ~44s Ginkgo execution (12 specs) + one-time `plugin:build:host`.

## M2 VERDICT (D-07 / success-criterion #2)

**Window CHARACTERIZED — not a deterministic loss (per D-07, that is neither required nor the goal).** `world.Service.MoveCharacter` commits the character row first (`service.go` `UpdateLocation`) and emits the move notification post-commit (`EmitMoveEvent`); the two are not atomic. Verbatim `M2-VERDICT:` lines from the green `HOLOMUSH_RUN_QUARANTINED=1` run:

```text
M2-VERDICT: control: healthy broker — move committed (row at 01KX9BCP7JX5XEBRE6BV3M4DP1) AND the move notification reached the stream subject events.main.location.01KX9BCP7JX5XEBRE6BV3M4DP1 (event type "move")
M2-VERDICT: flap-window: DB commit survived (row at 01KX9BCP7P39TN2DB1NFWPGWX0) + caller error EVENT_EMIT_FAILED (outer CHARACTER_MOVE_EVENT_FAILED) with move_succeeded=true, while the notification delivery was DECOUPLED from that result: delivered LATE and out-of-band after the caller already saw emit failure (frozen-broker TCP buffer flushed on unpause) — the D-07 non-atomicity window is real (the caller cannot know whether the notification landed)
M2-VERDICT: production-shape: with NO emitter wired (production construction), the move committed (row at 01KX9BDSEE0BV9X7GK8Q3NXH71) but reported EVENT_EMITTER_MISSING (outer CHARACTER_MOVE_EVENT_FAILED) with move_succeeded=true — the M2 notification leg is dead code in production today
```

**Observed evidence:**

- **Window (flap):** with a broker frozen mid-move, `UpdateLocation` (a Postgres write) commits BEFORE the post-commit emit hits the frozen broker; the emit's publish blocks and the retry loop exhausts, so `MoveCharacter` returns a non-nil error. The DB row IS at the destination (commit persisted) and the error carries `move_succeeded=true`. The notification's delivery is **decoupled** from the caller's result — this run it was delivered late out-of-band (frozen-broker TCP send buffer flushed on unpause); on other timings it can be lost. Either way the caller cannot know whether it landed. That ambiguity is the non-atomicity window.
- **Control (baseline):** with a healthy broker the same move commits AND the move event reaches the destination location subject (`GetLastMsgForSubject` over an independent conn, event type `move`).
- **Production shape:** the emitter-less `world.Service` (byte-for-byte the production construction) reports `EVENT_EMITTER_MISSING` (deepest code) / `CHARACTER_MOVE_EVENT_FAILED` (outer) with `move_succeeded=true` while the DB commits — the notification leg is dead code in production wiring today.

## Exact oops codes observed on the M2 error paths

`samber/oops@v1.22` `Code()` returns the **deepest non-nil** code in the chain (`error.go` `getDeepestErrorCode`), not the outer categorization:

| Path | Deepest code (AsOops().Code()) | Outer wrap | Context |
| ---- | ------------------------------ | ---------- | ------- |
| flap window (wired emitter, frozen broker) | `EVENT_EMIT_FAILED` | `CHARACTER_MOVE_EVENT_FAILED` | `move_succeeded=true` |
| production shape (no emitter) | `EVENT_EMITTER_MISSING` | `CHARACTER_MOVE_EVENT_FAILED` | `move_succeeded=true` |

## Verdict document

`docs/reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md` — the OPS-05 evidence doc. Consolidates all three verdict families (M12 / M2 / CHAOS) verbatim, documents the single reproduction command, and frames MODEL-01 implications strictly neutrally per D-01 (what the evidence means for BOTH option A and option B, no lean). Cross-linked from #4791 comment: <https://github.com/holomush/holomush/issues/4791#issuecomment-4948536272>.

## Full consolidated verdict set (canonical run, exit 0)

```text
M2-VERDICT: control: ... (event type "move")
M2-VERDICT: flap-window: ... decoupled ... the D-07 non-atomicity window is real
M2-VERDICT: production-shape: ... EVENT_EMITTER_MISSING ... dead code in production today
M12-VERDICT: setup: two replicas ... A4 dual-plugin fallback NOT needed
M12-VERDICT: deterministic-interleave: reproduced deterministically ... both UpdateLocation calls returned nil
M12-VERDICT: concurrent-describe: both-succeed-no-conflict N=50 ... 50 writes lost
M12-VERDICT: cross-field-race: k=0 of N=100 ... NOT a refutation
CHAOS-VERDICT: replica-restart: ... recovery is DB-read, not event replay
CHAOS-VERDICT: client-reconnect: ... resumed live delivery
CHAOS-VERDICT: broker-flap: publishing recovered after a docker-pause flap
```

## Task Commits

1. **Task 1: M2 dual-write window specs + worldBusAppender/newEmittingWorldService** - `8bcc08414` (test)
2. **Task 2: F1 resilience verdict document + #4791 cross-link** - `057116873` (docs)

## Files Created/Modified

- `test/integration/resilience/m2_dualwrite_test.go` - `Describe("M2 dual-write window", Ordered)` with control / flap-window / production-shape specs emitting `M2-VERDICT:` lines
- `test/integration/resilience/chaos_helpers_test.go` - added `worldBusAppender` (world.EventAppender over the raw publisher), `resilienceCoreToBusActor`/`resilienceCoreActorKindToBus`, `newEmittingWorldService`
- `docs/reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md` - the OPS-05 verdict document (success criterion #2)

## Decisions Made

- Recorded the flap-window delivery outcome rather than asserting permanent absence (see Deviations) — grounded in the observed TCP-buffer flush and D-07's "characterize, do not force a deterministic loss" guidance.
- Asserted the deepest oops chain code per-spec (`EVENT_EMIT_FAILED` / `EVENT_EMITTER_MISSING`) after confirming `samber/oops@v1.22` `Code()` semantics from source; `move_succeeded=true` (merged chain context) is the load-bearing D-07 signal asserted on both failure paths.
- Kept the MODEL-01 implications section strictly neutral (D-01): the ADR/decision owns the weighing.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Flap-spec leg 4 records delivery outcome instead of asserting permanent absence**

- **Found during:** Task 1 (first two canonical runs)
- **Issue:** The plan's leg 4 asserted the move notification is "permanently lost" (absence on the destination subject over a grace window). Empirically this is false and flaky: `docker pause` (SIGSTOP) freezes the broker but leaves the TCP connection open, so the client's `PublishMsg` bytes sit in the OS send buffer; the ack never returns (→ caller sees `EVENT_EMIT_FAILED` with `move_succeeded=true`), but on `unpause` the broker reads the buffered bytes and the move message DOES land — late and out-of-band. A hard absence assertion failed.
- **Fix:** Leg 4 now OBSERVES the post-unpause delivery outcome over a bounded grace window (Gomega `Consistently`, no `time.Sleep`) and records "lost" vs "delivered late out-of-band" in the verdict, without gating on the timing-dependent result. The deterministic facts (commit persisted + `move_succeeded=true` emit error) are still hard-asserted. This aligns with D-07 ("prove the window exists; a deterministic lost move is NOT required and NOT the goal") — the caller's error being decoupled from actual delivery IS the non-atomicity characterization.
- **Files modified:** test/integration/resilience/m2_dualwrite_test.go
- **Verification:** Full opt-in suite exits 0 (12 specs); 3 `M2-VERDICT:` lines present.
- **Committed in:** `8bcc08414` (Task 1 commit)

**2. [Rule 1 - Bug] M2 top-level oops code is the DEEPEST chain code, not the outer wrap**

- **Found during:** Task 1 (first canonical run)
- **Issue:** The plan implied the top-level `oops.AsOops(err).Code()` would be `CHARACTER_MOVE_EVENT_FAILED` (the outer wrap). `samber/oops@v1.22` `Code()` returns the DEEPEST non-nil code (`error.go` `getDeepestErrorCode`), so it surfaces `EVENT_EMIT_FAILED` (flap) / `EVENT_EMITTER_MISSING` (production), with `CHARACTER_MOVE_EVENT_FAILED` only as the outer categorization.
- **Fix:** Assert the deepest code per-spec and, as the load-bearing D-07 signal, assert `move_succeeded=true` on the merged chain context (present regardless of which code surfaces).
- **Files modified:** test/integration/resilience/m2_dualwrite_test.go
- **Committed in:** `8bcc08414` (Task 1 commit)

---

**Total deviations:** 2 auto-fixed (2 bugs — both in the plan's assumptions about broker-pause delivery semantics and oops chain-code semantics, corrected against empirical/source ground truth).
**Impact on plan:** No scope change. The M2 window is still characterized exactly as D-07 requires; the corrections make the characterization more honest (delivery is decoupled, not deterministically lost) and the assertions match the real error surface.

## Issues Encountered

- The plan's acceptance criterion for spec 2 ("assert absence of the move event on the stream after unpause") was empirically unachievable (TCP-buffer flush). Resolved per Deviation 1 by observing+recording the outcome — the characterization is stronger for it (delivery decoupled from the caller result is a sharper statement of the M2 hazard than "always lost").

## User Setup Required

None — Docker must be running for the integration tier (already a repo prerequisite).

## Next Phase Readiness

- **D-07 satisfied:** the M2 window is empirically characterized and the production dead-code finding is pinned by a spec.
- **Success criterion #2 satisfied:** `f1-resilience-verdict.md` exists at the canonical location, quotes the evidence run verbatim, stays neutral per D-01, and is cross-linked from #4791.
- The ADR's complete evidence set now exists (`f1-eventsourcing-why.md` + `f1-resilience-verdict.md`); plan 04 (MODEL-01 ADR + human checkpoint) can consume both.

## Self-Check: PASSED

All three source/doc files exist on disk; both task commits (`8bcc08414`, `057116873`) are present in git history; the #4791 cross-link comment is live and references `f1-resilience-verdict`.

---
*Phase: 04-world-model-resilience-investigation-decision-f1*
*Completed: 2026-07-11*

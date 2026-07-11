---
phase: 04-world-model-resilience-investigation-decision-f1
plan: 02
subsystem: testing
tags: [resilience, m12, last-write-wins, world-model, integration-test, nats, jetstream, chaos]

# Dependency graph
requires:
  - phase: 04-world-model-resilience-investigation-decision-f1
    provides: "plan 01 two-replica harness — WithExternalNATS/WithSharedDatabase seams, startReplica, pauseBroker/unpauseBroker, gated resilience suite"
provides:
  - "M12 last-write-wins verdict: corruption REPRODUCED deterministically (D-06) — a stale full-row UPDATE silently reverts a committed rename, both writers returning nil"
  - "newWorldService(server) helper — production-fidelity world.Service over a replica's shared pool"
  - "All four chaos dimensions of success criterion #1 exercised: concurrent commands, broker flap, replica restart, client reconnect"
  - "M12-VERDICT: / CHAOS-VERDICT: quotable report lines + RESILIENCE_VERDICT_LOG capture channel"
affects: [04-03-PLAN (M2 dual-write window), 04-04-PLAN (world-model ADR — this is decision input D-06 / success-criterion #2)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Deterministic M12 interleave: two newWorldService paths over one shared pool replicate entity_mutator's Get→mutate-one-field→UpdateLocation(full row) with explicit ordering — the corruption is proven, not sampled"
    - "Start-gun goroutine race (channel + sync.WaitGroup, zero sleeps) for concurrent-command fidelity specs (RESEARCH Pattern 3)"
    - "Read-backs go straight to the shared pgxpool via SELECT, never through sessions or subscriber frames (RESEARCH Pitfall 6)"
    - "Broker/transport chaos assertions are outcome-based: EVENTS LastSeq advance over an independent connection, never connection-state callbacks (RESEARCH Pitfall 5)"
    - "ReportAfterSuite + RESILIENCE_VERDICT_LOG env sink so verdict lines are quotable even though gotestsum --format pkgname suppresses a passing test's stdout"

key-files:
  created:
    - test/integration/resilience/m12_lastwritewins_test.go
    - test/integration/resilience/restart_reconnect_test.go
  modified:
    - test/integration/resilience/chaos_helpers_test.go

key-decisions:
  - "Both M12 replicas boot WITH in-tree plugins (shared-DB double-plugin boot is clean per plan 01); the assumption-A4 fallback (boot B without plugins) was NOT needed"
  - "The deterministic spec is the D-06 verdict; the cross-field natural-window k/N only bounds the uncontrolled window width — 0/N is explicitly non-refuting"
  - "Restart/reconnect specs run WITHOUT plugins — the command path was covered in Task 1; direct world.Service + EmitDirectEvent writes suffice for the transport/broker dimensions"

patterns-established:
  - "newWorldService(server): construct a world.Service over s.Pool() + s.AccessEngine(), no EventEmitter (mirrors production world/setup and integrationtest/plugins.go:267)"
  - "reportVerdict(line): stable-prefix evidence line on three channels (GinkgoWriter, os.Stdout, AddReportEntry), captured to a file by a ReportAfterSuite when RESILIENCE_VERDICT_LOG is set"

requirements-completed: [OPS-05]

coverage:
  - id: D1
    description: "M12 last-write-wins corruption reproduced deterministically under two replicas — a stale full-row UPDATE silently reverts a committed rename, both UpdateLocation calls returning nil (D-06 mechanism proof, success-criterion #2)"
    requirement: OPS-05
    verification:
      - kind: integration
        ref: "HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/ (m12_lastwritewins_test.go: deterministic-interleave spec)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Concurrent `describe here` commands from two replicas both report success while N writes are silently superseded with zero conflict signals (D-06 command-fidelity reachability, N=50)"
    requirement: OPS-05
    verification:
      - kind: integration
        ref: "m12_lastwritewins_test.go: concurrent-describe spec — both SendCommand return nil, read-back is exactly one of the two texts"
        status: pass
    human_judgment: false
  - id: D3
    description: "Cross-field natural-window race bounds the uncontrolled lost-update window (k/N; 0/N is non-refuting — the deterministic proof stands)"
    requirement: OPS-05
    verification:
      - kind: integration
        ref: "m12_lastwritewins_test.go: cross-field-race spec — k=0 of N=100 observed this run"
        status: pass
    human_judgment: false
  - id: D4
    description: "All four chaos dimensions of success criterion #1 exercised green: concurrent commands (D1/D2), replica restart (DB-derived recovery, no replay), client detach/reattach live-delivery resume, broker-flap publish recovery"
    requirement: OPS-05
    verification:
      - kind: integration
        ref: "restart_reconnect_test.go: replica-restart + client-reconnect + broker-flap specs (3 CHAOS-VERDICT lines)"
        status: pass
    human_judgment: false

# Metrics
duration: ~35min
completed: 2026-07-11
status: complete
---

# Phase 04 Plan 02: M12 Last-Write-Wins Verdict + Chaos Specs Summary

**M12 world-state corruption REPRODUCED deterministically under two replicas — a stale full-row UPDATE silently reverts a committed rename with both writers returning nil — plus green restart/reconnect/broker-flap specs completing all four chaos dimensions of success criterion #1.**

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-07-11
- **Tasks:** 2
- **Files modified:** 3 (2 new spec files, 1 helper file extended + bug-fixed)
- **Suite runtime (green):** ~6s Ginkgo spec execution (9 specs: 3 boot-smoke + 3 M12 + 3 chaos); ~14s including the `plugin:build:host` step `task test:int` runs first.

## M12 VERDICT (D-06 / success-criterion #2)

**REPRODUCED — deterministically.** The world model is direct-write CRUD with no version guard, and two replicas racing a write to the same row produce a silent lost update. Verbatim verdict lines from the green `HOLOMUSH_RUN_QUARANTINED=1` run:

```
M12-VERDICT: setup: two replicas booted over one broker + one shared DB with in-tree plugins; A4 dual-plugin fallback NOT needed (locID=01KX9A4P0HSH37465NEQN26XSR)
M12-VERDICT: deterministic-interleave: reproduced deterministically (A's rename "TestLoc_01KX9A4P"->"name-A-committed" silently reverted to "TestLoc_01KX9A4P" by B's stale full-row UPDATE; both UpdateLocation calls returned nil)
M12-VERDICT: concurrent-describe: both-succeed-no-conflict N=50 (every round: both commands returned success, one write silently superseded, zero conflicts surfaced; 50 writes lost)
M12-VERDICT: cross-field-race: k=0 of N=100 natural-window races lost (window never interleaved; NOT a refutation — the deterministic proof in spec 1 stands)
```

**Observed evidence:**
- **Mechanism (deterministic):** Replica A commits a rename (`TestLoc_01KX9A4P` → `name-A-committed`). Replica B, holding a copy read *before* A's rename, commits only a description change; its unguarded full-row `UPDATE ... SET name=$4, description=$5 ...` rewrites `name` back to the stale `TestLoc_01KX9A4P`. Read-back via the shared pool: `description` = B's value, `name` = the ORIGINAL. **A's committed rename is destroyed, and both `UpdateLocation` calls returned nil — no conflict is ever surfaced.**
- **Command fidelity (N=50):** Two real `describe here` commands (one per replica) race every round; both report success and the surviving description is always exactly one of the two — the other write is silently superseded. 50/50 rounds lost a write, zero conflicts raised.
- **Cross-field natural window:** k=0 of N=100. The uncontrolled command-vs-service timing window did not naturally interleave at the read-modify-write boundary this run (the command dispatch path is much heavier than the direct UPDATE). Per plan design, **0/N does NOT refute M12** — it only bounds the natural window as narrow; the deterministic spec is the verdict.

## CHAOS VERDICT (success-criterion #1)

All four chaos dimensions green. Verbatim `CHAOS-VERDICT:` lines:

```
CHAOS-VERDICT: replica-restart: B' booted cleanly against the existing EVENTS stream and served pre-restart state "written-before-restart" from the shared DB (recovery is DB-read, not event replay)
CHAOS-VERDICT: client-reconnect: session detached then reattached (REPLAY_COMPLETE observed) and resumed live delivery — EVENTS LastSeq advanced past 0
CHAOS-VERDICT: broker-flap: publishing recovered after a docker-pause flap (paused-attempt=returned-error; LastSeq advanced past 1 after unpause)
```

- **Replica restart:** a fresh replica B' boots cleanly against the already-existing EVENTS stream (CreateOrUpdateStream idempotence) and reads pre-restart state straight from the shared DB. **ADR observation: canonical world state is DB-derived — recovery is a DB read, NOT an event-sourced rebuild; no replay runs at boot.**
- **Client reconnect:** detach → reattach blocks until REPLAY_COMPLETE, then a post-reattach publish advances EVENTS LastSeq (outcome-based).
- **Broker flap:** while `docker pause`d the publish attempt **returned an error** (bounded 8s deadline); after unpause a fresh publish landed and LastSeq advanced.

## Task Commits

1. **Task 1: M12 lost-update specs + newWorldService helper** - `507ee9ea7` (test)
2. **Task 2: replica-restart / client-reconnect / broker-flap specs + pauseBroker fix** - `9d281b85b` (test)

## Files Created/Modified
- `test/integration/resilience/m12_lastwritewins_test.go` - 3 graduated M12 specs (deterministic mechanism, command-fidelity N=50, cross-field k/N) emitting M12-VERDICT lines
- `test/integration/resilience/restart_reconnect_test.go` - 3 chaos specs (restart, reconnect, broker flap) emitting CHAOS-VERDICT lines
- `test/integration/resilience/chaos_helpers_test.go` - added `newWorldService`, `reportVerdict`, the `RESILIENCE_VERDICT_LOG` ReportAfterSuite; fixed the wave-1 `pauseBroker`/`unpauseBroker` two-return signature bug

## Decisions Made
- Booted both M12 replicas with in-tree plugins; the A4 dual-plugin fallback was not needed (shared-DB double-plugin boot is clean — plugin migrations idempotent, guest seeding uses fresh ULIDs).
- Treated the deterministic-interleave spec as the D-06 verdict and the cross-field k/N as a window-width bound only (0/N explicitly non-refuting, stated in the spec message + verdict line).
- Restart/reconnect specs run without plugins (direct service + EmitDirectEvent writes) since the command path was already covered in Task 1.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed latent wave-1 `pauseBroker`/`unpauseBroker` two-return signature bug**
- **Found during:** Task 2 (broker-flap spec — the first-ever caller of `pauseBroker`)
- **Issue:** Plan 01's helpers wrote `Expect(cli.ContainerPause(ctx, id, opts)).To(Succeed())`, but the pinned `github.com/moby/moby/client` `ContainerPause`/`ContainerUnpause` return `(ContainerPauseResult, error)` — a two-value signature. Gomega received the `ContainerPauseResult{}` as the actual and failed: "Expected an error-type. Got: <client.ContainerPauseResult>: {}". Plan 01's boot smoke never called these helpers, so the bug lay dormant until this flap spec.
- **Fix:** Assign both returns (`_, err := cli.ContainerPause(...)`) and assert `Expect(err).NotTo(HaveOccurred())`.
- **Files modified:** test/integration/resilience/chaos_helpers_test.go
- **Verification:** Broker-flap spec now green; full suite exits 0.
- **Committed in:** `9d281b85b` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug).
**Impact on plan:** Necessary correctness fix to a wave-1 substrate helper; no scope change. No new external package.

## Issues Encountered
- **gotestsum `--format pkgname` suppresses passing-test stdout** (the `task test:int` default), so M12-VERDICT / CHAOS-VERDICT lines do not appear on the console for a green run. Resolved by adding a `ReportAfterSuite` that writes every verdict report entry to the path named by `RESILIENCE_VERDICT_LOG` — the run's evidence is quotable regardless of console format. The exit code remains the true acceptance gate; the verdict lines quoted above were captured verbatim via that sink from the green `HOLOMUSH_RUN_QUARANTINED=1` run.
- A benign teardown log ("external NATS subsystem Stop: nats: connection reconnecting") appears after the broker flap — the NATS client is mid-reconnect when the replica's t.Cleanup Stop closes it. Not a failure; the suite exits 0.

## User Setup Required
None - Docker must be running for the integration tier (already a repo prerequisite).

## Next Phase Readiness
- **D-06 satisfied:** M12 corruption is empirically reproduced with a deterministic mechanism proof + command-fidelity evidence. This is decision input #2 for the plan 04 world-model ADR.
- The verdict lines carry the stable `M12-VERDICT:` / `CHAOS-VERDICT:` prefixes so plan 03's evidence doc can quote them verbatim.
- Plan 03 (M2 dual-write window) can reuse `newWorldService`, `pauseBroker`/`unpauseBroker` (now fixed), and the `reportVerdict` / `RESILIENCE_VERDICT_LOG` channel.

## Self-Check: PASSED

All three source files and this SUMMARY exist on disk; both task commits (`507ee9ea7`, `9d281b85b`) are present in git history.

---
*Phase: 04-world-model-resilience-investigation-decision-f1*
*Completed: 2026-07-11*

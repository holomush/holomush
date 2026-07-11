---
phase: 04-world-model-resilience-investigation-decision-f1
plan: 01
subsystem: testing
tags: [resilience, integration-test, harness, eventbus, nats, jetstream, testcontainers, postgres]

# Dependency graph
requires:
  - phase: 03
    provides: eventbus external-mode subsystem + natstest single-node JetStream container harness
provides:
  - "integrationtest.Start seams: WithExternalNATS(url) + WithSharedDatabase(connStr)"
  - "integrationtest.Server accessors: ConnStr() and Bus()"
  - "test/integration/resilience package: gated Ginkgo suite + chaos helpers + two-replica boot smoke"
  - "Proof that two in-process CoreServer replicas share one broker + one database (D-03)"
affects: [04-02-PLAN (M12 last-write-wins), 04-03-PLAN (M2 dual-write window), 04-04-PLAN (model ADR)]

# Tech tracking
tech-stack:
  added: ["github.com/moby/moby/client (promoted already-pinned indirect dep to direct — docker pause/unpause primitive)"]
  patterns:
    - "Two-replica resilience harness: one natstest broker + one shared Postgres, N in-process CoreServer replicas each dialing the external-mode eventbus.Subsystem"
    - "D-05 opt-in gate via quarantinetest.Enabled() with NO quarantine-registry marker (heavyweight-but-not-flaky suite reuses the nightly env toggle)"

key-files:
  created:
    - test/integration/resilience/resilience_suite_test.go
    - test/integration/resilience/chaos_helpers_test.go
    - test/integration/resilience/boot_smoke_test.go
  modified:
    - internal/testsupport/integrationtest/options.go
    - internal/testsupport/integrationtest/harness.go
    - go.mod
    - go.sum

key-decisions:
  - "External subsystem built from Config{Mode,URL}.Defaults() only (no explicit StreamMaxAge/DupeWindow) so every replica presents an identical desiredStreamConfig and CreateOrUpdateStream is a no-op on replica 2's boot"
  - "Option resolution moved to the very top of Start so the DB and bus seams can branch on it (otherwise dead code)"
  - "Docker pause/unpause client is github.com/moby/moby/client (what testcontainers-go v0.43.0 embeds), NOT github.com/docker/docker/client — the two have divergent ContainerPause signatures"

patterns-established:
  - "startReplica(t, url, connStr, extra...): replica 1 passes connStr=\"\" (creates DB), replica 2 passes replica 1's ConnStr() (joins DB)"
  - "Broker-state assertions observe an INDEPENDENT connection (env.Conn) + stream.Info LastSeq, never a replica's cached view"

requirements-completed: [OPS-05]

coverage:
  - id: D1
    description: "integrationtest.Start gains WithExternalNATS + WithSharedDatabase seams and ConnStr()/Bus() accessors; default (option-less) Start behavior is byte-for-byte unchanged"
    requirement: OPS-05
    verification:
      - kind: integration
        ref: "task test:int -- -run TestNoSuchTestZZZ ./internal/testsupport/integrationtest/ ./test/integration/privacy/ (compile of harness + existing consumer under -tags=integration)"
        status: pass
      - kind: other
        ref: "task lint (0 issues) + task test (10221 unit tests, 3 skipped)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Gated resilience suite proves two replicas share one natstest broker + one shared Postgres database and agree on GameID; suite self-skips on the required Integration Test lane and runs under HOLOMUSH_RUN_QUARANTINED=1"
    requirement: OPS-05
    verification:
      - kind: integration
        ref: "HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/ (3 boot-smoke specs green, ~2.8s)"
        status: pass
      - kind: integration
        ref: "task test:int -- -run TestWorldModelResilience ./test/integration/resilience/ (env unset → 1 skipped, 0 specs run, exit 0 — D-05 gate)"
        status: pass
      - kind: integration
        ref: "test/meta/quarantine_registry_test.go#TestQuarantineRegistryBijection (registry↔marker bijection undisturbed — suite registers no marker)"
        status: pass
    human_judgment: false

# Metrics
duration: ~40min
completed: 2026-07-11
status: complete
---

# Phase 04 Plan 01: World-Model Resilience Harness Substrate Summary

**Two-replica resilience harness — external-NATS + shared-database seams on `integrationtest.Start`, plus a gated `test/integration/resilience` suite whose boot smoke proves two in-process CoreServer replicas genuinely share one JetStream broker and one Postgres database.**

## Performance

- **Duration:** ~40 min
- **Completed:** 2026-07-11
- **Tasks:** 2
- **Files modified:** 7 (2 harness source, 3 new test files, go.mod, go.sum)

## Accomplishments
- `WithExternalNATS(url)` swaps the embedded eventbustest bus for a production external-mode `eventbus.Subsystem` dialing a shared broker; `WithSharedDatabase(connStr)` joins an existing per-test DB. Both default empty → zero blast radius for every existing suite.
- Option resolution now runs at the top of `Start`, so the DB and bus seams are live rather than dead code; `Server.ConnStr()` and `Server.Bus()` accessors added.
- New gated `test/integration/resilience` package: opt-in via `quarantinetest.Enabled()` with NO quarantine-registry marker (D-05), so it stays off the required Integration Test PR lane while running nightly / on demand.
- Boot smoke (3 specs) proves, over one natstest broker + one shared Postgres: replicas share the database (cross-pool read of replica A's location), share the broker (EVENTS LastSeq advances over an independent connection after replica A publishes), and agree on GameID.

## Task Commits

1. **Task 1: WithExternalNATS/WithSharedDatabase seams + accessors** - `d389033ee` (feat)
2. **Task 2: gated resilience suite + two-replica boot smoke** - `badc79b77` (test)

## Files Created/Modified
- `internal/testsupport/integrationtest/options.go` - `WithExternalNATS`, `WithSharedDatabase` StartOptions
- `internal/testsupport/integrationtest/harness.go` - moved option resolution to top of `Start`; DB + bus seams; `connStr` field; `ConnStr()` + `Bus()` accessors
- `test/integration/resilience/resilience_suite_test.go` - `TestWorldModelResilience` entry + D-05 gate + `suiteT`
- `test/integration/resilience/chaos_helpers_test.go` - `startExternalNATS`, `startReplica`, `pauseBroker`/`unpauseBroker`
- `test/integration/resilience/boot_smoke_test.go` - two-replica boot smoke `Describe(Ordered)`
- `go.mod` / `go.sum` - promote `github.com/moby/moby/client` to a direct dependency

## Decisions Made
- Built the external subsystem from `Config{Mode,URL}.Defaults()` only (per plan), guaranteeing identical `desiredStreamConfig` across replicas so replica 2's `CreateOrUpdateStream` is idempotent.
- No Pitfall-3 / assumption-A4 re-seeding fix was needed: replica 2 booted cleanly on the shared database. Guest start-location seeding uses fresh `idgen.New()` ULIDs (no unique-key collision) and the versioned plugin migrations are a no-op on the already-migrated schema — exactly as the `WithSharedDatabase` doc-comment predicts.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Correct docker-client package + options-arg for pause/unpause**
- **Found during:** Task 2 (chaos helpers)
- **Issue:** The plan/RESEARCH sketch used a 2-arg `cli.ContainerPause(ctx, id)` and implied `github.com/docker/docker/client`. The compiler rejected it: testcontainers-go v0.43.0's `DockerClient` embeds `github.com/moby/moby/client`, whose `ContainerPause`/`ContainerUnpause` take a third `ContainerPauseOptions{}` / `ContainerUnpauseOptions{}` argument.
- **Fix:** Imported `dockerclient "github.com/moby/moby/client"` and passed the empty options structs. Ran `task deps` (go mod tidy) to promote the already-pinned indirect dep to direct.
- **Files modified:** test/integration/resilience/chaos_helpers_test.go, go.mod, go.sum
- **Verification:** Package compiles under `-tags=integration`; boot smoke runs green.
- **Committed in:** `badc79b77` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking).
**Impact on plan:** Mechanical API-signature correction discovered at compile time; no scope change. No new external package introduced — `moby/moby/client` was already a pinned transitive dep of testcontainers.

## Issues Encountered
None beyond the deviation above.

## Observed smoke-suite runtime (feeds plan 02/03 timeout budgets)
- Full `HOLOMUSH_RUN_QUARANTINED=1` run of the 3 boot-smoke specs: **~2.8–2.9s** (single NATS container start + two light replica boots + one publish/observe round-trip).
- Env-unset skip path: <1s (skips before booting anything).
- Budget guidance: plan 02/03 add real chaos (broker pause, concurrent writers, replica restart) on top of this ~3s substrate; the plan's 15m `-timeout` is comfortably generous. Expect per-scenario setup dominated by the single NATS `StartNATS` container start (seconds), amortized across an `Ordered` suite via `BeforeAll`.

## User Setup Required
None - no external service configuration required (Docker must be running for the integration tier, already a repo prerequisite).

## Next Phase Readiness
- Substrate is real and proven: plan 02 (M12 last-write-wins) and plan 03 (M2 dual-write window) can build directly on `startReplica` + `pauseBroker`/`unpauseBroker`.
- The D-05 gate is verified in both directions, so adding heavier resilience specs will not touch the required Integration Test PR check.

## Self-Check: PASSED

All 5 source/test files and the SUMMARY exist on disk; both task commits (`d389033ee`, `badc79b77`) are present in git history.

---
*Phase: 04-world-model-resilience-investigation-decision-f1*
*Completed: 2026-07-11*

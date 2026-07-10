---
phase: 03-platform-hardening-deployment-scaling
plan: 03
subsystem: eventbus-external-connect
tags: [nats, external-mode, fail-closed, provision, jetstream, CLUSTER-01]
status: complete
requires:
  - "03-01: eventbus.ModeExternal / Config.URL / Config.Credentials / TLSConfig / Config.IsProvision()"
  - "03-02: internal/testsupport/natstest.StartNATS / NATSEnv.URL"
provides:
  - "eventbus.dialExternal(cfg) — external NATS dial with creds/TLS, fail-closed EVENTBUS_EXTERNAL_CONNECT_FAILED"
  - "eventbus.Subsystem.connect() mode switch (embedded/external) + exporterEnabled() OQ-7 guard"
  - "EnsureStream provision:false verify-or-fail seam (EVENTBUS_STREAM_CONFIG_MISMATCH)"
  - "test/integration/eventbus_external — external boot proofs (INV-EVENTBUS-29 asserted_by)"
affects:
  - "downstream: CLUSTER-02 boot self-check, CLUSTER-03 multi-node, CLUSTER-04 DLQ (all run on external transport)"
  - "Plan 05 registry capstone mints INV-EVENTBUS-29 (annotation already placed here)"
tech-stack:
  added: []
  patterns:
    - "connect() mode switch inside Start (OQ-1) — no second Subsystem impl behind an interface"
    - "shared rollbackStart(conn) helper replaces per-branch inline rollback"
    - "desiredStreamConfig() single source of truth for provision create-path and verify-path (no drift)"
    - "fail-closed boot mirrors mandatory-KEK precedent (D-02): coded oops error, no fallback"
key-files:
  created:
    - internal/eventbus/natsdial.go
    - internal/eventbus/subsystem_external_test.go
    - test/integration/eventbus_external/external_boot_test.go
  modified:
    - internal/eventbus/subsystem.go
decisions:
  - "connect() takes no ctx (unused by embedded/external) — deviates from plan's connect(ctx) to satisfy revive unused-parameter; ctx stays on EnsureStream where it is used"
  - "exporter guard extracted to exporterEnabled() predicate for a genuine unit assertion of OQ-7 without a broker"
metrics:
  duration: "~40m"
  completed: "2026-07-10"
  tasks: 2
  files: 4
---

# Phase 3 Plan 03: External-mode connect branch + provision opt-out + fail-closed boot Summary

Implemented CLUSTER-01's core code delta: `eventbus.Subsystem.Start` now selects
its NATS transport by `Config.Mode`. External mode dials a configured cluster
with `.creds`/TLS (D-04), fails closed at boot if unreachable (D-02, no embedded
fallback), skips the embedded-only Prometheus exporter (OQ-7), and honors a
`provision:false` verify-or-fail seam for locked-down clusters (D-03). All the
mode-independent downstream surface (`Conn()`/`JS()`/`EnsureStream`/drain) is
unchanged, so cluster/invalidation/audit consumers keep working over the
transport-agnostic `natsconn.Conn` seam.

## What shipped

- **`natsdial.go` — `dialExternal(cfg) (*nats.Conn, error)`:** builds `nats.Connect`
  options — `nats.Name("holomush-server")`, `nats.UserCredentials` when
  `cfg.Credentials` is set, `nats.RootCAs`/`nats.ClientCert` when the TLS block is
  populated — and wraps any dial error as
  `oops.Code("EVENTBUS_EXTERNAL_CONNECT_FAILED").With("url", …).Wrap(err)`. No
  `RetryOnFailedConnect`: at boot we want an immediate coded failure; nats.go's
  reconnect handles transient drops of an established connection (D-02).
- **`subsystem.go` — `connect()` mode switch (OQ-1):** extracted the transport head
  of `Start` into `connect() → {connectEmbedded | connectExternal}`. The embedded
  path is byte-preserved (server bring-up, in-process `nats.Connect`, `jetstream.New`,
  full rollback). The external path dials + `jetstream.New`. `Start` now guards
  "already started" on `s.conn` (mode-agnostic) and shares a `rollbackStart(conn)`
  helper that closes the conn, nils the seams, and shuts the embedded server down
  only when `s.server != nil`.
- **Exporter guard (OQ-7):** `exporterEnabled()` returns
  `Mode == ModeEmbedded && PrometheusExporter`, so external mode never reaches
  `s.server.MonitorAddr()` (nil-deref averted). The external cluster exposes its own
  `/varz`; the runbook (D-16) points there.
- **`EnsureStream` provision opt-out (D-03):** branches on `Config.IsProvision()`.
  Default keeps `CreateOrUpdateStream` (idempotent, embedded+external parity).
  `provision:false` calls `s.js.Stream(ctx, EVENTS)` + compares the owned config
  fields (Subjects, Retention, MaxAge, Duplicates) against `desiredStreamConfig()`;
  an absent stream or any drift fails closed with `EVENTBUS_STREAM_CONFIG_MISMATCH`
  and the server creates nothing.

## Tests

- **Unit (`subsystem_external_test.go`, `package eventbus`):** `dialExternal` against
  `nats://127.0.0.1:1` → nil conn + `EVENTBUS_EXTERNAL_CONNECT_FAILED`;
  `exporterEnabled()` truth table (embedded+flag→true; external+flag→false; …).
  `task test -- ./internal/eventbus/` green — 202 tests (was 196 pre-plan; +6),
  all existing embedded/exporter/rollback tests still pass (embedded path preserved).
- **Integration (`test/integration/eventbus_external/external_boot_test.go`,
  `//go:build integration`, Ginkgo, natstest container):** external connect happy
  path (JS live, EVENTS declared); unreachable-URL fail-closed (conn/js nil);
  provision:false verify-success against a matching pre-existing stream;
  provision:false config-mismatch fail-closed; provision:false absent-stream
  fail-closed asserting the stream was NOT created. `Ran 5 of 5 Specs … SUCCESS!`
  via `task test:int -- ./test/integration/eventbus_external/`. Fail-closed specs
  carry `// Verifies: INV-EVENTBUS-29` (entry minted in Plan 05).
- `task build`, `task fmt`, `task lint` all green; fmt output committed.

## Deviations from Plan

### Auto-fixed / adjustments

**1. [Rule 3 - blocking lint] `connect()` takes no `ctx`**
- **Found during:** Task 1.
- **Issue:** The plan specified `connect(ctx) (*nats.Conn, jetstream.JetStream, error)`,
  but neither the embedded nor external transport head uses `ctx` (`resolveStoreDir`
  and `jetstream.New` take none; `EnsureStream(ctx)` runs after `connect` in `Start`).
  An unused `ctx` parameter trips revive's `unused-parameter`.
- **Fix:** `connect()` has no parameter; `ctx` stays on `EnsureStream` where it is
  genuinely used. Net behavior identical.

**2. [Rule 2 - testability] Extracted `exporterEnabled()` predicate**
- **Found during:** Task 1.
- **Issue:** The plan's Task 1 lists a unit test proving the exporter block is not
  entered in external mode, but the guard was inline in `Start` and unreachable
  without a live broker (external `Start` fails at dial first), so the inline guard
  could not be unit-asserted in isolation.
- **Fix:** Extracted the guard to `exporterEnabled()` — a self-documenting predicate
  that directly asserts OQ-7 (embedded-only) via a table test, with no broker needed.
  `Start` calls it in place of the inline `if s.cfg.PrometheusExporter`.

### TDD Gate Compliance

`type: tdd` plan. Following the documented precedent of 03-01/03-02 on this shared
milestone branch, RED/GREEN were exercised locally per task (wrote the test, observed
failure — new symbols `dialExternal`/`exporterEnabled` don't compile, integration
specs fail without the provision branch — then implemented and observed pass) but
committed atomically per task so every commit is bisect-clean. `dialExternal` is a
new symbol, so a separate compiling test-only RED commit is not achievable; no
separate `test(...)` commit precedes the `feat(...)` commits. Intentional, documented.

## Known Stubs

None. The exporter runbook pointer for external mode is a doc concern owned by the
CLUSTER runbook plan (D-16), not a code stub here.

## Threat Flags

None beyond the plan's threat register (T-03-05..08 all mitigated: `.creds`/TLS auth,
fail-closed boot, provision:false verify-or-fail). No new trust-boundary surface
introduced beyond the external NATS connection the plan already models.

## Commits

- 99405818c `feat(03-03): add external-mode connect() switch + dial helper + exporter guard`
- 44b3374f3 `feat(03-03): add provision:false verify-or-fail seam + external-boot integration proof`

## Self-Check: PASSED

- Files exist: internal/eventbus/natsdial.go, internal/eventbus/subsystem.go,
  internal/eventbus/subsystem_external_test.go,
  test/integration/eventbus_external/external_boot_test.go — all FOUND.
- Commits exist: 99405818c, 44b3374f3 — both FOUND.
- `task test -- ./internal/eventbus/` green (202); external integration suite 5/5 green.
- `task build` / `task lint` / `task fmt` green.

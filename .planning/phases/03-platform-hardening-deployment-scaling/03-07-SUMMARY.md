---
phase: 03-platform-hardening-deployment-scaling
plan: 07
subsystem: eventbus-audit-dlq-replay
tags: [dlq, replay, audit, cobra, cli, cluster-04, recovery]
status: complete
requires:
  - "internal/eventbus/audit/dlq.go — EVENTS_AUDIT_DLQ stream + Capture (03-04)"
  - "internal/eventbus/audit/projection.go persist — the idempotent events_audit write (03-04)"
  - "internal/eventbus.Config external dial (natsdial.go)"
provides:
  - "internal/eventbus/audit/replay.go — ReplayDLQ + ReplayOptions/ReplayResult (idempotent DLQ→events_audit re-drive)"
  - "internal/eventbus/audit.writeAuditRow(ctx, pool, subject, msg) — shared persist body (live + replay)"
  - "internal/eventbus.Dial(cfg) — exported external-NATS dial for operator CLIs"
  - "cmd/holomush audit dlq {list,show,replay} operator command group"
affects:
  - "cmd/holomush/root.go (NewRootCmd registers NewAuditCmd)"
  - "cmd/holomush/gateway_imports_test.go (cmd_audit.go added to core-only allowlist)"
  - "internal/eventbus/audit/projection.go (persist refactored to call writeAuditRow with an explicit subject)"
tech-stack:
  added: []
  patterns:
    - "Shared writeAuditRow body drives both the live projection and DLQ replay so header parsing + ON CONFLICT DO NOTHING idempotency are byte-identical"
    - "Ephemeral OrderedConsumer over a LimitsPolicy stream = read-only replay: acking never deletes, so re-runs re-read every dead letter and idempotency absorbs the duplicate writes"
    - "Original event subject recovered from the DLQ subject suffix (originalSubject) so a restored audit row matches the live path"
    - "Operator CLI dials NATS + Postgres directly (no admin UDS), mirroring migrate.go / cmd_admin.go host-shell tools"
key-files:
  created:
    - internal/eventbus/audit/replay.go
    - internal/eventbus/audit/replay_test.go
    - internal/eventbus/audit/dlq_replay_integration_test.go
    - cmd/holomush/cmd_audit.go
    - cmd/holomush/cmd_audit_test.go
  modified:
    - internal/eventbus/audit/projection.go
    - internal/eventbus/natsdial.go
    - cmd/holomush/root.go
    - cmd/holomush/gateway_imports_test.go
decisions:
  - "DLQ replay integration spec co-located at internal/eventbus/audit/dlq_replay_integration_test.go (package audit_test), NOT the plan's test/integration/audit/ — that dir is package audit_test for the access-control logger and lacks the eventbustest+Postgres harness. Follows Plan 04's documented deviation; reuses openPool/countAuditRows/eventbustest with zero duplication."
  - "writeAuditRow gained an explicit subject param: on replay msg.Subject() is the DLQ-wrapped subject, so the original event subject is recovered from the DLQ suffix and stored — the restored row matches what the live path would have written (timestamp/js_seq remain the DLQ message's, since the original stream metadata is not preserved by capture)."
  - "Recovery invariant deliberately NOT bound: the replay spec asserts the CLUSTER-04 recovery half; INV-EVENTBUS-30 covers the capture half (Term/Nak). A recovery invariant is a candidate for the phase's registry consolidation — no false-green binding added."
metrics:
  duration: "~50m"
  completed: "2026-07-10"
  tasks: 3
  files: 9
---

# Phase 3 Plan 07: Audit dead-letter replay CLI (CLUSTER-04) Summary

Delivered CLUSTER-04's recovery half. Audit dead letters captured to
`EVENTS_AUDIT_DLQ` (Plan 04) can now be inspected and re-driven back into
`events_audit` once the underlying outage is fixed — dead letters are
recoverable, not "nicer-looking data loss" (D-11). Replay reuses the live
projection's write body, so it is idempotent (`ON CONFLICT (id) DO NOTHING`
on the preserved `Nats-Msg-Id`) and header-parsing-identical to the live path.

## What shipped

- **Idempotent replay entry (Task 1)** — `internal/eventbus/audit/replay.go`:
  `ReplayDLQ(ctx, js, pool, cfg, opts) (ReplayResult, error)` reads the
  `EVENTS_AUDIT_DLQ` stream via an ephemeral `OrderedConsumer` (read-only over a
  `LimitsPolicy` stream) and writes each message through the shared
  `writeAuditRow`. `ReplayOptions` bounds a pass (`MsgID` filter / `Limit`
  fence); `ReplayResult` reports scanned/replayed/skipped/failed. A
  genuinely-poison message (e.g. missing `Nats-Msg-Id`) is counted `Failed` and
  retained in the DLQ, never consumed. The projection's `persist` was refactored
  to call `writeAuditRow` so the live and replay paths share one write body.
- **Operator CLI (Task 2)** — `cmd/holomush/cmd_audit.go`: `NewAuditCmd()` with a
  `dlq` subgroup — `list` (stream summary: count/bytes/oldest/newest), `show
  <nats-msg-id>` (a single dead letter's subject/headers/metadata), and `replay`
  (`--all` / `--msg-id` / `--limit` → `audit.ReplayDLQ`). Registered with one
  `cmd.AddCommand(NewAuditCmd())` line in `NewRootCmd`. Dials external NATS via
  the new exported `eventbus.Dial` and Postgres via `DATABASE_URL` — **no admin
  UDS** (OQ-5). The replay command builds the game-scoped DLQ subject
  (`internal.<game_id>.audit.dlq`) from `event_bus.game_id` so subject recovery
  is correct for any game.
- **Real-broker proof (Task 3)** —
  `internal/eventbus/audit/dlq_replay_integration_test.go`: seed
  `EVENTS_AUDIT_DLQ` with a well-formed captured message → `ReplayDLQ` → assert
  the `events_audit` row is present with the **original** event subject
  (recovered from the DLQ suffix); a second replay leaves the row count at one
  (idempotent).

## Tests

- **Unit** (`task test -- ./internal/eventbus/audit/ ./cmd/holomush/`, green):
  `replayOne` skip/failed accounting + ack behavior with a nil pool (poison and
  MsgID-mismatch paths return before DB access); `originalSubject`
  strip/mismatch/empty-prefix; replay flag validation (`--all`/`--msg-id`
  mutual-exclusion, selection required); `dlqConfigForGame` subject scoping;
  `audit dlq {list,show,replay}` command-tree resolution.
- **Integration** (`task test:int -- ./internal/eventbus/audit/`, green — 120
  tests, 2 pre-existing quarantine skips): the seed→replay→row-present +
  double-replay-idempotent proof, plus all existing projection/DLQ-capture specs
  still green after the `persist`/`writeAuditRow` refactor.
- `task build`, `task lint`, `task fmt` clean.

## Deviations from Plan

### Auto-fixed / Adjusted

**1. [Rule 3 - blocking location] Integration spec co-located at `internal/eventbus/audit/` instead of `test/integration/audit/`**
- **Found during:** Task 3.
- **Issue:** The plan's `test/integration/audit/` is package `audit_test` for the
  access-control audit logger (`internal/audit`) — a different `audit` package —
  and has no `eventbustest`/Postgres harness.
- **Fix:** Co-located at `internal/eventbus/audit/dlq_replay_integration_test.go`,
  reusing `openPool`/`countAuditRows`/`eventbustest`. Mirrors Plan 04's
  documented decision; `task test:int` runs `./...` so the runner is unaffected.

**2. [Rule 1 - correctness] Replay recovers the original event subject**
- **Found during:** Task 3.
- **Issue:** `writeAuditRow` stored `msg.Subject()`; on replay that is the
  DLQ-wrapped subject (`internal.<game>.audit.dlq.<orig>`), which would corrupt
  the restored row's `subject` column vs. the live path.
- **Fix:** `writeAuditRow` takes an explicit `subject`; replay recovers the
  original from the DLQ suffix (`originalSubject`), and the CLI builds the
  game-scoped prefix from `event_bus.game_id`. `timestamp`/`js_seq` remain the
  DLQ message's — the original stream metadata is not preserved by capture and
  is genuinely unrecoverable; the dedup key (`id` from `Nats-Msg-Id`) is
  preserved, so idempotency holds.
- **Files modified:** internal/eventbus/audit/projection.go,
  internal/eventbus/audit/replay.go, cmd/holomush/cmd_audit.go.
- **Commit:** 7c57962f5.

**3. [Rule 3 - blocking gate] `cmd_audit.go` added to the gateway-boundary core-only allowlist**
- **Found during:** Task 2.
- **Issue:** `TestGatewayImportsAreOnlyProtocolTranslation` (INV-EVENTBUS-1) flags
  any `cmd/holomush` file importing `internal/eventbus*` as gateway-side.
- **Fix:** Added `cmd_audit.go`/`cmd_audit_test.go` to `coreOnlyFiles` — the DLQ
  CLI is a host-shell operator tool (matches `cmd_admin.go`/`migrate.go`
  precedent), not the gateway.

## Threat Flags

None. No new network endpoint, auth path, or trust-boundary schema change beyond
the plan's `<threat_model>` (T-03-19/20). Replay reuses the projection persist
path (crypto/codec/DEK handling untouched — the change only adds an explicit
subject arg and a new read-then-write caller); captured payloads are already the
encrypted-at-rest envelope, so no new plaintext surface. The DLQ subject stays
inside the CLUSTER-02 `internal.>` granted prefix. Crypto-reviewer gate expected
pre-push (projection.go is a crypto-gated path): `msg.Data()`→envelope
byte-equality (INV-21) and all codec/DEK handling are unchanged.

## Commits

- 1de0737cc `feat(03-07): idempotent audit DLQ replay reusing the projection persist path`
- ee90446e2 `feat(03-07): holomush audit dlq {list,show,replay} operator CLI`
- 7c57962f5 `test(03-07): real-broker DLQ replay proof + faithful original-subject recovery`

## Self-Check: PASSED

- Files exist: replay.go, replay_test.go, dlq_replay_integration_test.go,
  cmd_audit.go, cmd_audit_test.go — all FOUND.
- Commits exist: 1de0737cc, ee90446e2, 7c57962f5 — all FOUND.
- `task test`, `task test:int` (audit + cmd), `task build`, `task lint`,
  `task fmt` — all green.
- No stubs introduced; entry + CLI + tests only.

---
phase: 03-platform-hardening-deployment-scaling
plan: 04
subsystem: eventbus-audit-dlq
tags: [dlq, jetstream, audit, never-drop, prometheus, cluster-04]
status: complete
requires:
  - "eventbus.DLQConfig{MaxAge,MaxBytes} (03-01 config surface)"
provides:
  - "internal/eventbus/audit/dlq.go — dlqPublisher{js,cfg,counter} + EnsureStream + Capture (D-10 reusable helper)"
  - "audit.Config.DLQ field + Defaults; DLQMessagesTotal counter (holomush_audit_dlq_messages_total)"
  - "projection.handle final-attempt DLQ capture: Term on success / Nak on DLQ-publish failure (D-09 never-drop)"
  - "EVENTS_AUDIT_DLQ bounded stream (subject internal.<game_id>.audit.dlq.>)"
affects:
  - "cmd/holomush/core.go (audit.NewSubsystem now plumbs event_bus.dlq retention + subject)"
  - "internal/eventbus/audit/projection.go (newProjection provisions DLQ stream; handle Term/Nak seam)"
tech-stack:
  added: []
  patterns:
    - "Narrow dlqJetStream interface (CreateOrUpdateStream+PublishMsg) makes the helper broker-free unit-testable"
    - "dlqCapturer interface on the projection makes the Term/Nak decision deterministic without a broker"
    - "White-box //go:build integration test in package audit overrides p.dlq to simulate a DLQ outage against a real consumer"
key-files:
  created:
    - internal/eventbus/audit/dlq.go
    - internal/eventbus/audit/dlq_test.go
    - internal/eventbus/audit/projection_dlq_unit_test.go
    - internal/eventbus/audit/dlq_capture_integration_test.go
    - internal/eventbus/audit/dlq_neverdrop_integration_test.go
  modified:
    - internal/eventbus/audit/subsystem.go
    - internal/eventbus/audit/projection.go
    - internal/eventbus/audit/lag_metric.go
    - cmd/holomush/core.go
decisions:
  - "DLQ integration tests co-located at internal/eventbus/audit/ (RESEARCH Wave-0 path) rather than the plan's test/integration/audit/ — that dir is a different `audit` package (access logger) lacking the eventbustest+Postgres harness; co-location reuses openPool/publishTestMessage/eventbustest with zero duplication"
  - "Original event subject preserved via the DLQ subject suffix (internal.<game>.audit.dlq.<orig-subject>), NOT a new header — keeps captured headers byte-identical to the original for replay dedup"
  - "DLQConfig.Storage zero-value = jetstream.FileStorage (durable prod default); tests override to MemoryStorage"
metrics:
  duration: "~55m"
  completed: "2026-07-10"
  tasks: 3
  files: 9
---

# Phase 3 Plan 04: Audit dead-letter capture (CLUSTER-04) Summary

Closed the `TODO (Phase B): wire a DLQ` at the audit projection. Audit messages
that exhaust `MaxDeliver` are now captured to a bounded `EVENTS_AUDIT_DLQ`
stream (header-preserving) and Term'd; if the DLQ publish itself fails the
message is Nak'd instead — nothing is ever silently dropped (D-09). A
`holomush_audit_dlq_messages_total` counter and size/age-capped retention
(D-11/D-12) give operators a bounded, alertable dead-letter surface.

## What shipped

- **Reusable helper (D-10)** — `internal/eventbus/audit/dlq.go`: `dlqPublisher`
  holding a `dlqJetStream` (narrow `CreateOrUpdateStream`+`PublishMsg` seam), a
  `DLQConfig`, and the `DLQMessagesTotal` counter. `EnsureStream` idempotently
  provisions `EVENTS_AUDIT_DLQ` (subject `internal.<game_id>.audit.dlq.>`,
  `LimitsPolicy`, `MaxAge`/`MaxBytes` from config, `0`→`-1` unbounded map).
  `Capture` publishes `&nats.Msg{Subject: <prefix>.<orig-subject>, Header:
  msg.Headers(), Data: msg.Data()}` and increments the counter on success only.
- **Never-drop seam (D-09)** — `projection.handle` persist-error path now reads
  `msg.Metadata().NumDelivered` (the new jetstream API — no `msg.Info()`); at
  `>= MaxDeliver` it `Capture`s then `Term`s, or `Nak`s if the capture fails.
  Sub-cap behavior is byte-identical to the prior deliberate no-ack backoff
  (Pitfall 3). `newProjection` provisions the DLQ stream once at construction.
- **Config + metric** — `audit.Config.DLQ` (+ `DLQConfig.Defaults()`);
  `DLQMessagesTotal` registered alongside the existing audit collectors. `core.go`
  plumbs `event_bus.dlq` retention + the game-scoped subject into `audit.Config`
  without touching `productionSubsystems` arity (OQ-6).
- **`// Verifies: INV-EVENTBUS-30`** — annotated on both real-broker specs
  (entry to be minted in Plan 05 per the phase's registry consolidation; no
  fabricated `invariants.yaml` binding added here).

## Tests

- **Unit** (`task test -- ./internal/eventbus/audit/`, 105→ green): DLQ helper
  EnsureStream (bounded config, idempotent, `0`→`-1` maxbytes, declare-error),
  Capture (subject/header/data preservation + counter, error-without-count), and
  all three `handle` branches (Term on capture success, Nak on DLQ-publish
  failure, sub-cap no-op).
- **Integration** (`task test:int -- ./internal/eventbus/audit/`, green):
  `TestProjectionCapturesPoisonToDLQAfterMaxDeliver` (poison → EVENTS_AUDIT_DLQ +
  counter, not persisted) and white-box `TestProjectionNeverDropsWhenDLQPublishFails`
  (failing DLQ → DLQ stream stays empty, message unpersisted, retained for
  redelivery). Existing projection integration tests still green (now provision
  the DLQ stream). `task build`, `task lint`, `task fmt` clean; `rg "msg\.Info\("
  internal/eventbus/audit/` empty.

## Deviations from Plan

### Auto-fixed / Adjusted

**1. [Rule 3 - blocking location] DLQ integration tests co-located at `internal/eventbus/audit/` instead of `test/integration/audit/`**
- **Found during:** Task 3.
- **Issue:** The plan's `test/integration/audit/` is package `audit_test` importing
  `internal/audit` (the access-control audit logger) — a different `audit` package —
  and has no eventbustest/Postgres harness. Placing eventbus-audit DLQ specs there
  would require re-scaffolding the whole harness under an aliased import.
- **Fix:** Co-located at `internal/eventbus/audit/dlq_capture_integration_test.go`
  (package `audit_test`) + `dlq_neverdrop_integration_test.go` (package `audit`,
  white-box), reusing `openPool`/`publishTestMessage`/`eventbustest`/`fixedJS`/
  `fixedPool` with zero duplication. RESEARCH.md § Wave-0 Gaps names exactly this
  path (`internal/eventbus/audit/dlq_integration_test.go`). `task test:int` runs
  `./...`, so the runner is unaffected.

**2. [Rule 2 - correctness] `MaxDeliver == 0` guard added to the DLQ gate**
- **Found during:** Task 2.
- **Issue:** `MaxDeliver: 0` means "unlimited redelivery" (per `Config.MaxDeliver`
  doc); `NumDelivered >= 0` would fire the DLQ branch on the first attempt.
- **Fix:** Gate is `mErr == nil && p.cfg.MaxDeliver > 0 && meta.NumDelivered >= …`.

## Follow-up filed

- **#4776** — "Wire audit DLQ into the plugin audit consumer" (`enhancement`):
  the per-plugin consumer has the same poison exposure and reuses `dlq.go` (D-10).

## Threat Flags

None — no new network endpoint, auth path, or trust-boundary schema change beyond
the plan's `<threat_model>` (T-03-09..12). The DLQ subject stays inside the
CLUSTER-02 `internal.>` granted prefix; captured payloads are already the
encrypted-at-rest envelope (no new plaintext surface). Crypto-reviewer gate
expected pre-push (projection.go is a crypto-gated path): the audit row
`msg.Data()`→envelope byte-equality (INV-21) and all codec/DEK handling are
untouched — the change only adds the final-attempt DLQ branch.

## Commits

- c906edd86 `feat(03-04): add reusable audit DLQ capture helper + bounded-stream config`
- ec9e94c88 `feat(03-04): capture audit poison to DLQ on final attempt (Term/Nak)`
- f8459ff66 `test(03-04): real-broker DLQ capture + never-drop proof (INV-EVENTBUS-30)`
- de4f6d146 `style(03-04): drop redundant nats.Header conversion (unconvert)`

## Self-Check: PASSED

- Files exist: dlq.go, dlq_test.go, projection_dlq_unit_test.go,
  dlq_capture_integration_test.go, dlq_neverdrop_integration_test.go — all FOUND.
- Commits exist: c906edd86, ec9e94c88, f8459ff66, de4f6d146 — all FOUND.
- `rg "msg\.Info\(" internal/eventbus/audit/` — CLEAN.
- No stubs introduced; helper + hook + tests only.

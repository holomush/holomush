---
phase: 06-operational-hardening-assurance-gates
plan: 05
subsystem: audit-dlq-cli
tags: [ops-04, audit, dlq, replay, game-id, external-nats, cli, natstest]

# Dependency graph
requires:
  - phase: 06-01
    provides: "composite-PK / deterministic-timestamp writeAuditRow regime (events_audit dedup keys on (id, event_ms)); the DLQ replay path shares writeAuditRow"
provides:
  - "--game-id flag on `holomush audit dlq replay`"
  - "resolveGameID(ctx, lookup, override, coreGameID) — override → core.game_id → persisted DB, mirroring core.go:300-303"
  - "F3 fix: the CLI's DLQ subject prefix (internal.<game_id>.audit.dlq) now matches the server's persisted subject for external-NATS deployments"
  - "divergent-game natstest integration test that drives the real resolver seam end-to-end (failure guard + recovery)"
affects: [operators, audit-recovery, ship, gsd-verifier]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "CLI game_id resolution MIRRORS the server's own order exactly (override → configured core.game_id via config.Load(..., \"core\") → persisted DB) so CLI and server never diverge on the DLQ subject"
    - "openAuditStore returns a *store.PostgresEventStore so ONE pool backs both the ReplayDLQ write (Pool()) and the persisted-game_id read leg (GetSystemInfo)"
    - "package-main integration test reaches the unexported resolver seam; external-mode scoping uses a real natstest container, never embedded eventbustest"

key-files:
  created:
    - cmd/holomush/cmd_audit_dlq_replay_integration_test.go
    - .planning/phases/06-operational-hardening-assurance-gates/06-05-SUMMARY.md
  modified:
    - cmd/holomush/cmd_audit.go
    - cmd/holomush/cmd_audit_test.go
  deleted:
    - internal/eventbus/audit/dlq_replay_integration_test.go

key-decisions:
  - "resolveGameID precedence is override → core.game_id → DB (NOT the round-1 inverted override → DB → config); this mirrors the server and prefers an explicit core.game_id over a possibly-stale DB value"
  - "The resolver loads the `core` config SECTION via config.Load(configFile, cmd, &coreCfg, \"core\") — the exact call core.go:125 uses — NOT event_bus.game_id, NOT the YAML root, and never applies event_bus.Config.Defaults() (which would force \"main\" and reintroduce the F3 mismatch)"
  - "openAuditPool refactored to openAuditStore (*store.PostgresEventStore) so GetSystemInfo is reused for the DB read leg with a single pool, rather than reimplementing the game_id query"
  - "The tautological same-\"main\" embedded-NATS F3 test is REPLACED (per D-06), not augmented: a package-in-internal/eventbus/audit cannot reach the unexported cmd/holomush resolver, so the genuine seam test lives in cmd/holomush"

requirements-completed: [OPS-04]

coverage:
  - id: T1
    description: "resolveGameID mirrors the server order (override → core.game_id → persisted DB); four precedence legs + DB-error branch"
    requirement: OPS-04
    verification:
      - kind: automated
        ref: "task test -- ./cmd/holomush/... — TestResolveGameIDPrefersOverride / PrefersCoreGameIDOverDB / FallsBackToDB / ReturnsEmptyWhenAllUnset / PropagatesDBError (green)"
        status: pass
  - id: T1b
    description: "config.Load(..., \"core\") selects core.game_id, not event_bus.game_id or the YAML root — the command-side section selection is regression-proof (round-4 LOW)"
    requirement: OPS-04
    verification:
      - kind: automated
        ref: "task test -- ./cmd/holomush/... — TestConfigLoadCoreSectionSelectsCoreGameID (temp YAML with divergent core.game_id vs event_bus.game_id → coregame)"
        status: pass
  - id: T2
    description: "divergent-game natstest test drives the REAL resolver seam end-to-end: failure guard (wrong override → Failed>0, 0 rows) + recovery (resolver reads seeded DB ULID → Replayed==1, subject==original)"
    requirement: OPS-04
    verification:
      - kind: automated
        ref: "task test:int -- -run TestAuditDLQReplayResolvesGameIDForExternalNATS ./cmd/holomush/... (real NATS container + composite-PK events_audit; green)"
        status: pass
  - id: T3
    description: "--game-id flag exists; runAuditDLQReplay passes the RESOLVED id (not raw event_bus cfg.GameID) to dlqConfigForGame; no Defaults() on the game_id path"
    requirement: OPS-04
    verification:
      - kind: automated
        ref: "task build green; rg confirms no event_bus.Defaults() on the game_id resolution path in cmd_audit.go"
        status: pass

metrics:
  duration: ~35min
  completed: 2026-07-15
  tasks: 2
  files: 4
  commits: 2

status: complete
---

# Phase 6 Plan 5: OPS-04 Audit-DLQ Replay game_id Resolution Summary

Fixed the F3 external-NATS bug in `holomush audit dlq replay` — the CLI resolved
the DLQ subject prefix from the empty `event_bus.game_id`, defaulting to
`internal.main.audit.dlq` while the server publishes to `internal.<ULID>.audit.dlq`,
so every dead letter was counted Failed and nothing recovered. The CLI now resolves
the effective game_id mirroring the server (`--game-id` override → configured
`core.game_id` → persisted DB value), and the tautological same-game coverage test
is replaced with a divergent-game natstest test that drives the real resolver seam.

## What was built

**Task 1 — `--game-id` flag + `resolveGameID` (TDD).** Added the `--game-id`
override flag and `resolveGameID(ctx, lookup sysInfoReader, override, coreGameID)`,
whose precedence exactly mirrors the server at `core.go:300-303`: non-empty override
wins; else the configured `core.game_id` (loaded via `config.Load(configFile, cmd,
&coreCfg, "core")` — the same section+call `core.go:125` uses, NOT `event_bus.game_id`,
NOT the YAML root, and without `event_bus.Config.Defaults()`); else the persisted
`holomush_system_info` game_id via `GetSystemInfo(ctx, "game_id")`
(`ErrSystemInfoNotFound` → empty, the resolver invents nothing). The system-info read
is an injected function seam so the resolver is unit-testable without a live pool.
`openAuditPool` became `openAuditStore` returning a `*store.PostgresEventStore` so one
pool backs both the `ReplayDLQ` write path (`Pool()`) and the DB read leg
(`GetSystemInfo`). Unit tests cover all four precedence legs, the DB-error branch, and
a focused config-section test proving `config.Load(..., "core")` selects `core.game_id`
over a divergent `event_bus.game_id`.

**Task 2 — divergent-game natstest integration test.** New
`cmd/holomush/cmd_audit_dlq_replay_integration_test.go` (package `main`,
`//go:build integration`) reaches the unexported resolver seam
(`resolveGameID → dlqConfigForGame → audit.ReplayDLQ`) end-to-end against a REAL
single-node NATS JetStream container (`natstest.StartNATS`) and a migrated
composite-PK `events_audit` Postgres. It seeds a server-style ULID game_id into
`holomush_system_info`, seeds a dead letter under `internal.<ULID>.audit.dlq.<orig>`,
then asserts BOTH the failure guard (wrong `--game-id` override → prefix mismatch →
`Failed > 0`, zero rows — no cross-tenant mis-write) and recovery (override + core
empty → the resolver's DB leg recovers the seeded ULID → `Replayed == 1`,
`events_audit.subject == origSubject`). The tautological same-`"main"` embedded-NATS
F3 test in `internal/eventbus/audit/dlq_replay_integration_test.go` was removed (D-06).

## Verification

- `task test -- ./cmd/holomush/...` — green (resolver precedence + config-section unit tests).
- `task test:int -- -run TestAuditDLQReplayResolvesGameIDForExternalNATS ./cmd/holomush/...` — green (real NATS container, composite-PK events_audit; failure guard + recovery).
- `task build` — green.
- `task lint` — green (govet shadow on the `core` config load fixed via a distinct `loadErr`).

## Deviations from Plan

None affecting behavior. One structural refinement within Rule 2/3 scope:
`openAuditPool` (returning a bare `*pgxpool.Pool`) was refactored to `openAuditStore`
(returning `*store.PostgresEventStore`) so the plan's "reuse GetSystemInfo, do NOT
reimplement the key" directive is honored with a SINGLE pool (Pool() for the write,
GetSystemInfo for the read) — `NewPostgresEventStore` takes a DSN and owns its pool,
and it is the only caller. The `--game-id` flag maps to the `core` section's
`game_id` koanf key during the `core` config overlay, but this is harmless: only
explicitly-set flags overlay, and `resolveGameID` checks the override first, so the
result is identical whether the override arrives via the flag or the overlaid field.

## Self-Check: PASSED

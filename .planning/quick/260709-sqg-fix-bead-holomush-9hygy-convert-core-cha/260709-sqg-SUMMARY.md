---
phase: quick-260709-sqg
plan: 01
status: complete
subsystem: plugins/core-channels
tags: [storage, pgnanos, migrations, timestamps, INV-STORE-1]
requires: []
provides: [core-channels-bigint-timestamps]
affects: [plugins/core-channels]
tech-stack:
  patterns: [pgnanos scan/insert seam, BIGINT epoch-nanosecond columns]
key-files:
  modified:
    - plugins/core-channels/migrations/000001_channels.up.sql
    - plugins/core-channels/migrations/000002_create_channel_log.up.sql
    - plugins/core-channels/store.go
    - plugins/core-channels/audit.go
    - plugins/core-channels/service.go
    - plugins/core-channels/service_rpcs.go
    - plugins/core-channels/service_test.go
    - plugins/core-channels/commands_test.go
    - plugins/core-channels/audit_test.go
decisions:
  - "Edited both up-migrations in place (never shipped) — no conversion migration, no .gfo6-cutoff exemption."
  - "MembershipForHistory keeps its exported time.Time signature; scans into a local pgnanos.Time and returns .Time()."
metrics:
  duration: ~15m
  completed: 2026-07-09
  tasks: 1
  files: 9
requirements: [holomush-9hygy, CHAN-01, CHAN-02, CHAN-03]
---

# Phase quick-260709-sqg Plan 01: core-channels BIGINT timestamps Summary

Converted the five core-channels TIMESTAMPTZ columns to BIGINT epoch-nanoseconds via the pgnanos seam, clearing the `task lint:no-timestamptz` milestone-ship blocker (fixes holomush-9hygy).

## What changed

- **Migrations (edited in place):**
  - `000001_channels.up.sql` — `channels.created_at`, `channel_memberships.joined_at`, `channel_ops_events.occurred_at` → `BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT`.
  - `000002_create_channel_log.up.sql` — `channel_log.timestamp` → `BIGINT NOT NULL` (no default; Insert always supplies it); `channel_log.inserted_at` → `BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT`.
  - Added core-scenes-style INV-STORE-1 header notes; no `TIMESTAMPTZ`/`TIMESTAMP` token remains. Down-migrations untouched; no `.gfo6-cutoff` file created.
- **store.go** — imported `internal/pgnanos`; `channelRow.CreatedAt` and `channelMemberRow.JoinedAt` are now `pgnanos.Time` (scanned directly via `sql.Scanner`). `MembershipForHistory` scans a local `pgnanos.Time` and returns `.Time()` (signature unchanged). `DeleteChannelLogOlderThan` passes `pgnanos.From(cutoff)` as the SQL arg.
- **audit.go** — imported pgnanos; `channelLogRow.timestamp` → `pgnanos.Time`; `Insert` and `queryLog` bind `pgnanos.From(...AsTime())`; `QueryHistory` streams `r.timestamp.Time()`. Updated the stale nil-timestamp guard comment (BIGINT epoch-ns, not TIMESTAMPTZ).
- **service.go / service_rpcs.go** — `rowToChannelInfo` uses `row.CreatedAt.Time()`; `ListChannels` passes `rows[i].CreatedAt.Time()`; proto conversions use `r.JoinedAt.Time()` / `r.timestamp.Time()`.
- **Tests** — wrapped `channelMemberRow.JoinedAt` and `channelLogRow.timestamp` struct-literal fixtures with `pgnanos.From(...)` in service_test.go, commands_test.go, audit_test.go (added pgnanos imports). Fake `MembershipForHistory` member maps stay `time.Time` (signature unchanged).

## Verification (exact outputs)

- `task lint:no-timestamptz` → **exit 0** (milestone-ship gate green).
- `task test -- ./plugins/core-channels/...` → **DONE 189 tests in 2.228s**, all pass.
- `task test:int -- ./plugins/core-channels/...` → **DONE 190 tests in 2.877s** (real Postgres BIGINT round-trip through Insert/queryLog/DeleteChannelLogOlderThan/MembershipForHistory).
- Full `task test:int` → **DONE 10471 tests, 5 skipped in 124.399s** (all suites green).
- Full `task lint` → **exit 0**.

## Deviations from Plan

None — plan executed exactly as written.

## Commit

- `1284ba341` — fix(core-channels): convert timestamp columns to BIGINT epoch-ns

## Self-Check: PASSED

- Migrations, store.go, audit.go verified present with BIGINT columns and pgnanos seam.
- Commit `1284ba341` present in git log.
- No `.gfo6-cutoff` file under `plugins/core-channels/migrations/`.

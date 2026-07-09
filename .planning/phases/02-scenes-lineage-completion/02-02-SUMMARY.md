---
phase: 02-scenes-lineage-completion
plan: 02
subsystem: core-scenes-plugin-store
tags: [scenes, notifications, mute, migrations, postgres, plugin-store]
requires:
  - "plugin_core_scenes schema + SceneStore CRUD conventions (plugins/core-scenes/store.go)"
  - "BIGINT epoch-nanos timestamp domain (migration 000007)"
provides:
  - "scene_notify_prefs table (migration 000011) — per-character global notify pref + per-scene mute + D-05 mode seam"
  - "SceneStore.SetSceneMute / SetSceneNotifyPref / GetSceneNotifyPref / ListMutedScenes"
affects:
  - plugins/core-scenes/store.go
  - plugins/core-scenes/migrations/
tech-stack:
  added: []
  patterns:
    - "single table, two row shapes disambiguated by NULL vs non-NULL scene_id (RESEARCH Open Q2)"
    - "partial unique indexes: one global row per character (WHERE scene_id IS NULL), one per (char,scene) (WHERE scene_id IS NOT NULL)"
    - "ON CONFLICT ... WHERE <partial-index-predicate> upsert inference for idempotent writes"
    - "notify-OFF persisted as muted=NOT enabled on the NULL-scene_id global row (no separate enabled column)"
key-files:
  created:
    - plugins/core-scenes/migrations/000011_scene_notify_prefs.up.sql
    - plugins/core-scenes/migrations/000011_scene_notify_prefs.down.sql
    - plugins/core-scenes/notify_prefs_test.go
  modified:
    - plugins/core-scenes/store.go
decisions:
  - "Global notify pref stores as muted=NOT enabled on the NULL-scene_id row rather than adding an `enabled` column — matches the table shape pinned in must_haves (character_id, scene_id NULL, muted, mode) and keeps a single semantic column."
  - "mode column ships NOT NULL DEFAULT 'realtime' (D-05 digest seam); GetSceneNotifyPref returns mode from the row or 'realtime' when absent, so digest lands later with no migration."
  - "Timestamps are BIGINT epoch-nanos (created_at/updated_at) to match the plugin schema's post-000007 convention, not TIMESTAMPTZ."
  - "No FK from scene_notify_prefs to scenes — the table is plugin-owned mute/pref state; keeping it FK-free avoids ordering constraints against scene lifecycle and matches the threat model (state never trusts an ambient character; identity enforced at ABAC layer in later plans)."
  - "Task 2 committed as a single atomic feat (test + impl together) instead of separate RED/GREEN commits: the integration-tagged test references the not-yet-existing methods, so a test-only commit would leave the -tags=integration build broken at an intermediate commit (breaks bisect/parallel agents). RED was established by the guaranteed compile failure before implementation."
metrics:
  duration: ~15m
  completed: 2026-07-09
status: complete
---

# Phase 2 Plan 02: Scene Notify-Prefs Plugin Store Summary

Persisted per-character global scene-notify preferences and per-scene mute flags in the plugin-owned `plugin_core_scenes` schema via new migration `000011_scene_notify_prefs` and four `SceneStore` CRUD methods, with a `mode` column shipping as the D-05 digest seam (default `realtime`). This is the state layer that Plan 03 (telnet mute command + RPCs), Plan 04 (core mute-suppression), and Plan 05 (web mute slice) read/write.

## What was built

- **Task 1 — migration 000011** (`plugins/core-scenes/migrations/000011_scene_notify_prefs.{up,down}.sql`): creates `scene_notify_prefs(character_id text NOT NULL, scene_id text NULL, muted bool NOT NULL DEFAULT false, mode text NOT NULL DEFAULT 'realtime', created_at/updated_at bigint epoch-nanos)`. A `scene_id IS NULL` row is the per-character global notify pref; a non-NULL row is a per-scene mute (RESEARCH Open Q2 / A5). Two partial unique indexes enforce the two row shapes: `scene_notify_prefs_global (character_id) WHERE scene_id IS NULL` and `scene_notify_prefs_scene (character_id, scene_id) WHERE scene_id IS NOT NULL`. Idempotent up (`IF NOT EXISTS`), reversible down (`IF EXISTS`, indexes then table in reverse order). No triggers/functions; SPDX headers on both files.
- **Task 2 — store CRUD** (`plugins/core-scenes/store.go`): `SetSceneMute(ctx, char, scene, muted)` (idempotent upsert of the per-scene row via `ON CONFLICT (character_id, scene_id) WHERE scene_id IS NOT NULL`), `SetSceneNotifyPref(ctx, char, enabled)` (upsert the NULL-scene_id global row, persisting `muted = NOT enabled`; `mode` keeps its `realtime` default), `GetSceneNotifyPref(ctx, char) (enabled, mode, err)` (defaults `(true, "realtime")` on `pgx.ErrNoRows`), and `ListMutedScenes(ctx, char)` (non-NULL rows with `muted=true`, ordered, scoped strictly to the queried character). All follow the store's `startSpan` / `recordError` / `oops.Code("SCENE_...")` conventions. Integration round-trip tests (`notify_prefs_test.go`, `//go:build integration`) written first (RED — compile failure, methods absent) then implementation (GREEN).

## Verification

- `task test:int -- ./plugins/core-scenes/...` — PASS (665 tests, 21.2s), including the new `SceneStore notify prefs` suite covering: mute round-trip (include after mute, exclude after unmute), mute idempotency, cross-character isolation, notify default `(enabled=true, mode="realtime")`, disabled-pref round-trip, re-enable idempotency, and global-pref independence from per-scene mutes.
- `task lint:go` — PASS (0 issues). `task fmt` clean for changed Go/SQL files.
- Migration idempotency/reversibility exercised transitively: `NewSceneStore` runs the embedded up migration on a fresh testcontainer DB for every integration test; acceptance greps confirm `IF NOT EXISTS` (up) / `IF EXISTS` (down) and zero `TRIGGER|CREATE FUNCTION|PROCEDURE`.

## Deviations from Plan

- **[Commit cadence] Task 2 committed as one atomic `feat` (test + impl)** rather than the strict RED-commit-then-GREEN-commit TDD cadence. Rationale: the `//go:build integration` test references the four not-yet-existing methods, so a test-only intermediate commit would leave the `-tags=integration` build broken (a repo learning explicitly flags intermediate broken builds — breaks `git bisect` and parallel agents). RED was still honored: the methods provably did not exist before implementation, and the tests were authored before the store code. No functional deviation from the plan's behavior spec.
- **[Rule interpretation — table shape]** The global notify pref is stored as `muted = NOT enabled` on the NULL-scene_id row rather than via a dedicated `enabled` column. The plan permitted either ("persist enabled as the inverse of a muted-global or an explicit enabled column"); the muted-inverse form was chosen to match the `must_haves` table shape `(character_id, scene_id NULL, muted, mode)` exactly and avoid an extra column.

No auto-fixed bugs (Rule 1), no missing-critical additions (Rule 2), no blocking-issue fixes (Rule 3), no architectural changes (Rule 4), no authentication gates.

## Known Stubs

None. The `mode` column is an intentional, documented forward seam (D-05), not a stub: it persists and round-trips today defaulting `realtime`; digest delivery is a later plan and needs no schema change.

## Commits

- `a2335b1e6` — feat(02-02): migration 000011_scene_notify_prefs (up/down)
- `82892bd7d` — feat(02-02): scene notify-prefs store CRUD with mode digest seam

## Self-Check: PASSED

- Files exist: `000011_scene_notify_prefs.up.sql`, `000011_scene_notify_prefs.down.sql`, `notify_prefs_test.go`, `store.go` (modified) — all confirmed on disk.
- Commits `a2335b1e6` and `82892bd7d` present in git history.

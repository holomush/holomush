---
phase: 02-scenes-lineage-completion
plan: 06
subsystem: core-scenes-plugin + telnet-gateway
tags: [scenes, idle-timeout, lifecycle, scheduler, notifications, invariants]
requires:
  - "publish_scheduler ticker-sweep pattern (plugins/core-scenes/publish_scheduler.go)"
  - "idle_timeout_secs column + scene_idle_nudge event type (already declared, dormant)"
  - "gamenotice.Idle leader primitive (02-01) + telnet formatEvent render dispatch"
provides:
  - "SceneStore.ListScenesIdlePastThreshold — active scenes past effective idle threshold (explicit game default param)"
  - "idleScheduler — active→paused sweep + OFF-by-default scene_idle_nudge emitter"
  - "telnet render of core-scenes:scene_idle_nudge via gamenotice.Idle (review Finding 4)"
  - "INV-SCENE-71 — idle active→paused transition, no re-transition once paused"
affects:
  - plugins/core-scenes/idle_scheduler.go
  - plugins/core-scenes/store.go
  - plugins/core-scenes/main.go
  - plugins/core-scenes/plugin.yaml
  - internal/telnet/gateway_handler.go
tech-stack:
  added: []
  patterns:
    - "deterministic ticker sweep (injected now func) mirroring publish_scheduler"
    - "narrow store interface (sceneIdleStore) for independent sweep mockability"
    - "explicit game-default parameter into a pool-only store (store never reads config — review Finding 1)"
    - "game-agnostic scene_log suffix match (%scene.<id>.ic) avoids an IC-subject-prefix param"
    - "event-type render branch in formatEvent (before category dispatch) routing to a shared leader"
key-files:
  created:
    - plugins/core-scenes/idle_scheduler.go
    - plugins/core-scenes/idle_scheduler_test.go
    - plugins/core-scenes/idle_scheduler_integration_test.go
  modified:
    - plugins/core-scenes/store.go
    - plugins/core-scenes/main.go
    - plugins/core-scenes/plugin.yaml
    - plugins/core-scenes/main_test.go
    - internal/telnet/gateway_handler.go
    - internal/telnet/gateway_handler_test.go
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md
decisions:
  - "Idle basis is the newest IC scene_log timestamp, falling back to created_at when the scene has no IC log yet (a never-touched active scene idles out from creation time)."
  - "Store idle query matches scene_log by suffix (LIKE '%scene.<id>.ic') so SceneStore stays game-id-agnostic and the pinned signature (ctx, nowNs, defaultIdleTimeoutSecs) needs no IC-subject-prefix param."
  - "All idle arithmetic is epoch-nanoseconds (last_activity_ns + idle_timeout_secs*1e9 <= nowNs); the plan's '*1000 (ms)' shorthand was a unit mismatch against the nowNs nanosecond signature and was corrected to nanos."
  - "idle_timeout_default is a duration config knob (matching vote_window/cooloff_window/scheduler_interval), converted to seconds at scheduler construction; a non-positive value fails loud at Init like scheduler_interval."
  - "SQL boundary is inclusive (<=): a scene at exactly last_activity + idle_timeout is idle."
  - "scene_idle_nudge payload is ad-hoc JSON {scene_id} (no proto message exists); telnet reads scene_id from the payload only (no DB lookup)."
metrics:
  duration: ~40m
  completed: 2026-07-09
status: complete
---

# Phase 2 Plan 06: Idle-Timeout Lifecycle + Optional Idle Nudge Summary

Wired the dormant idle-timeout infra: a `publish_scheduler`-style ticker sweep now transitions active scenes idle past their effective threshold to `paused` (game-wide default supplied explicitly, per-scene `idle_timeout_secs` override via COALESCE), emits an OFF-by-default `scene_idle_nudge`, and renders that nudge on telnet through the shared `gamenotice.Idle` leader. Registered and bound INV-SCENE-71 for the idle transition.

## What was built

- **Task 1 — store query + sweep** (`idle_scheduler.go`, `store.go`): `SceneStore.ListScenesIdlePastThreshold(ctx, nowNs int64, defaultIdleTimeoutSecs int)` returns active scenes whose newest IC activity (or `created_at` fallback) `+ COALESCE(idle_timeout_secs, $default)*1e9 <= nowNs`; the default is an EXPLICIT parameter (the pool-only store never reads config — review Finding 1); paused scenes are excluded (no re-transition). `idleScheduler.Run`/`sweep` mirror `publish_scheduler` with an injected `now`, defensive `IsValidTransition(active,paused)`, per-row WARN-and-continue fault tolerance, and `oops`-wrapped scan errors. Unit tests (fake store) cover transition-per-row, per-row failure tolerance, non-active skip, scan-error wrap. Integration tests cover the inclusive boundary, the per-scene override beating the injected default, paused-exclusion, and the active→paused sweep + no re-transition.
- **Task 2 — wire + flag-gated emit + config** (`main.go`, `plugin.yaml`, `idle_scheduler.go`): `applyConfig` decodes `idle_timeout_default` (duration, default 30m, fail-loud on non-positive) and `idle_nudge_enabled` (bool, default OFF); `Init` constructs `idleScheduler` from the decoded default + interval + nudge flag + the service's event sink/gameID and runs it alongside the publish scheduler on the same daemon context. After a successful active→paused transition, the sweep emits `scene_idle_nudge` via `EventSink.Emit(pluginsdk.EmitIntent{...})` (the binary emit path, NOT `core.NewEvent()`) carrying `{scene_id}` — ONLY when the flag is ON. No `phase4EmitTypes`/`crypto.emits` change (INV-PLUGIN-32 set-equality preserved). The stale `scene_idle_nudge` manifest description (and the `holomush-fux3` deferred-trigger note) was reworded to the implemented semantics.
- **Task 3 — INV-SCENE-71** (`invariants.yaml` → regenerated `invariants.md`): scope SCENE, `binding: bound` → `idle_scheduler_integration_test.go` (`// Verifies: INV-SCENE-71` above the sweep transition + no-re-transition spec).
- **Task 4 — telnet render** (`gateway_handler.go`): a render branch in `formatEvent` (before the category dispatch) routes `core-scenes:scene_idle_nudge` EventFrames to `gamenotice.Idle(sceneID)`, reading `scene_id` from the frame payload only (gateway-boundary, no DB lookup). Closes review Finding 4 — the generic `SCENE_ACTIVITY` "has new activity" leader / empty `formatSystem` render is bypassed for the dedicated `[>GAME: … is now idle]` phrasing. TDD RED (empty render) → GREEN.

## Verification

- `task test -- ./plugins/core-scenes/...` — 694 tests pass (includes 6 unit idle-sweep/emit tests + config fail-loud test + INV-PLUGIN-32 manifest-registry equality).
- Integration (Docker): `TestCoreScenesIntegration -ginkgo.focus='ListScenesIdlePastThreshold|idleScheduler'` — 6 idle specs pass (inclusive boundary, per-scene override, paused-exclusion, active→paused + no re-transition).
- `task test -- ./internal/telnet/...` — 115 tests pass (new idle-render test proves `[>GAME: Scene #<id> is now idle]`, distinct from the activity leader).
- `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` — pass; `go run ./cmd/inv-render && git diff --exit-code docs/architecture/invariants.md` — clean.
- `task lint:go` — 0 issues. `task lint:plugin-manifests` — pass. `task plugin:build:host` — core-scenes builds (no EVENT_TYPE_REGISTRY_MISMATCH). `task fmt` applied.

## must_haves check

- Idle active scene auto-transitions active→paused via ticker sweep; game default decoded from config and passed EXPLICITLY into `ListScenesIdlePastThreshold`; per-scene `idle_timeout_secs` overrides via COALESCE — YES.
- Idle nudge emitted ONLY when its flag is ON; OFF by default — YES (unit test: flag OFF → transition but no emit; flag ON → one emit with scene_id).
- Idle nudge renders on telnet as `[>GAME: Scene #<id> is now idle]` via `gamenotice.Idle` (NOT the generic SCENE_ACTIVITY leader) — YES (Finding 4 closed).
- Paused scene does not re-transition; sweep tolerates per-row failures without aborting — YES (query excludes paused; per-row WARN-and-continue).

## Deviations from Plan

- **[Rule 1 — unit correctness] Idle arithmetic corrected to nanoseconds.** The plan/PATTERNS shorthand `last_activity_ms + idle_timeout_secs*1000 ≤ now` mixes epoch-milliseconds with the pinned `nowNs int64` nanosecond signature (that comparison is always true — a bug). Implemented as epoch-nanoseconds throughout: `COALESCE(MAX(scene_log.timestamp), created_at) + COALESCE(idle_timeout_secs,$default)::bigint*1000000000 <= nowNs`. Behavior matches the intent; the acceptance-grepped signature `(ctx, nowNs int64, defaultIdleTimeoutSecs int)` is unchanged.
- **[Design choice — signature fidelity] scene_log matched by suffix, no IC-prefix param.** The pinned store signature has no IC-subject-prefix argument (unlike the board query, which takes one). To keep the store game-id-agnostic under that fixed signature, the idle query matches `scene_log.subject LIKE '%scene.<id>.ic'` rather than `= '<prefix>' || id || '.ic'`. Same `.ic` activity semantics as the board.
- **[Test tiering] Boundary / COALESCE-override / paused-exclusion proven in the integration suite, not a pure unit test.** The plan's Task 1 acceptance lists these under "unit test", but they are SQL-level properties that a fake store cannot genuinely assert (a fake that re-implements COALESCE would be tautological — a flagged repo anti-pattern). They live in `idle_scheduler_integration_test.go` (real DB), mirroring the `publish_scheduler` integration-test precedent; the unit test (`idle_scheduler_test.go`) covers the sweep control flow. INV-SCENE-71 binds to the integration test that genuinely asserts the transition + no-re-transition.
- **[Commit cadence] TDD RED honored, committed atomically per task.** For package-`main` unit tests referencing new symbols, a RED-only commit breaks `task test` compile; per the 02-02 precedent (avoid broken intermediate builds), test+impl were committed together. Task 4 (telnet) did a genuine RED→GREEN split within the working tree (confirmed empty render before impl) but committed atomically.

No architectural changes (Rule 4), no auth gates. Threat register T-02-17..20 mitigations all in place (payload-only scene_id render, IsValidTransition + paused-exclusion, per-row tolerance, no emit-registry change).

## Known Stubs

None. The idle nudge is intentionally OFF by default (spec §4.4), not a stub: the emit path is fully wired and exercised (flag-ON unit test); enabling it is a config toggle.

## Commits

- `0f6660bfe` — feat(02-06): idle store query + idleScheduler sweep (active→paused)
- `603afcf04` — feat(02-06): wire idle scheduler + flag-gated idle-nudge emitter + game default
- `455943e78` — docs(02-06): register INV-SCENE-71 (idle active→paused transition)
- `ca813f3bd` — feat(02-06): render scene_idle_nudge on telnet via gamenotice.Idle

## Self-Check: PASSED

- Files exist: idle_scheduler.go, idle_scheduler_test.go, idle_scheduler_integration_test.go, store.go, main.go, plugin.yaml, gateway_handler.go, invariants.yaml/.md — all confirmed on disk.
- Commits 0f6660bfe, 603afcf04, 455943e78, ca813f3bd present in git history.
- INV-SCENE-71 registered (`binding: bound`) and invariants.md regenerated in-sync; registry meta-tests green.

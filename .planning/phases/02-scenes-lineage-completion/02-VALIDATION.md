---
phase: 2
slug: scenes-lineage-completion
status: reconciled
nyquist_compliant: true
wave_0_complete: true
created: 2026-07-08
reconciled: 2026-07-09
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `02-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go — testify (unit) + Ginkgo/Gomega (integration, `//go:build integration`) |
| **Config file** | `Taskfile.yaml` task targets; no separate test config |
| **Quick run command** | `task test -- ./plugins/core-scenes/...` |
| **Full suite command** | `task test` then `task test:int` (needs Docker) |
| **Estimated runtime** | quick ~30–90s; full integration several min (Docker) |

---

## Sampling Rate

- **After every task commit:** Run `task test -- ./plugins/core-scenes/...` (scoped quick run)
- **After every plan wave:** Run `task test` (full unit); add `task test:int` on any wave touching the telnet gateway, reconnect/focus-restore, or ABAC policies
- **Before `/gsd-verify-work`:** `task test` and `task test:int` both green
- **Max feedback latency:** ~90s (unit); integration on wave boundaries

---

## Per-Task Verification Map

> Reconciled 2026-07-09 from the 7 planned PLAN.md files (22 tasks; +1 from the
> 2026-07-09 reviews pass — Plan 06 Task 4 telnet idle render; +1 from the
> 2026-07-09 round-3 reviews pass — Plan 03 Task 4 self-scoped mute/notify
> read-back, Concern 1). Every task carries an `<automated>` verify command;
> TDD-first tasks create their test file on the RED step; no watch-mode flags.
> `File Exists` = when the test artifact is created (in-task on TDD RED, or via
> existing meta/integration infra).

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 2-01-01 | 01 | 1 | SCENEFWD-02 | T-02-02 | content-free `[>GAME:…]` leader (scene_id only) | unit (TDD) | `task test -- ./internal/telnet/gamenotice/...` | ▶ in-task | ⬜ pending |
| 2-01-02 | 01 | 1 | SCENEFWD-02 | T-02-01, T-02-03 | throttled render, consumes only scene_id | unit (TDD) | `task test -- ./internal/telnet/...` | ▶ in-task | ⬜ pending |
| 2-01-03 | 01 | 1 | SCENEFWD-02 | T-02-01 | INV-SCENE-70 telnet-privacy bind | meta | `go run ./cmd/inv-render && git diff --exit-code docs/architecture/invariants.md; task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` | ✅ infra | ⬜ pending |
| 2-02-01 | 02 | 1 | SCENEFWD-02 | T-02-04 | idempotent + reversible migration 000011 | integration | `task test:int -- -run TestSceneStoreMigrations ./plugins/core-scenes/ 2>/dev/null \|\| task test:int` | ▶ in-task | ⬜ pending |
| 2-02-02 | 02 | 1 | SCENEFWD-02 | T-02-04, T-02-05 | per-character mute round-trip isolation | integration (TDD) | `task test:int -- -run TestSceneNotifyPrefs ./plugins/core-scenes/ 2>/dev/null \|\| task test:int` | ▶ in-task | ⬜ pending |
| 2-03-01 | 03 | 2 | SCENEFWD-02 | T-02-07 | ABAC-gated mute RPCs, fail-closed | unit (TDD) | `task lint:proto && task test -- ./plugins/core-scenes/...` | ▶ in-task | ⬜ pending |
| 2-03-02 | 03 | 2 | SCENEFWD-02 | T-02-08 | gated telnet mute/unmute (no validActions change) | unit | `task test -- ./plugins/core-scenes/...` | ▶ in-task | ⬜ pending |
| 2-03-03 | 03 | 2 | SCENEFWD-02 | T-02-07 | participant-gated DSL policy, default-deny | unit + lint | `task lint && task test -- ./plugins/core-scenes/...` | ▶ in-task | ⬜ pending |
| 2-03-04 | 03 | 2 | SCENEFWD-02 | T-02-09b | self-scoped mute/notify read-back on ListCharacterScenes, fail-OPEN (round-3 Concern 1) | unit (TDD) | `task lint:proto && task test -- ./plugins/core-scenes/... -run 'ListCharacterScenes\|Mute'` | ▶ in-task | ⬜ pending |
| 2-04-01 | 04 | 3 | SCENEFWD-02 | T-02-11, T-02-12 | per-character TTL cache holds {globalEnabled, mutedSet}, cross-character isolation (Finding 2) | unit (TDD) | `task test -- ./internal/grpc/... -run SceneMute` | ▶ in-task | ⬜ pending |
| 2-04-02 | 04 | 3 | SCENEFWD-02 | T-02-10, T-02-13, T-02-13b | global-off OR muted → suppress at downgrade, fail-open (Finding 2) | unit + integration (TDD) | `task test -- ./internal/grpc/... && task test:int -- -run 'SceneActivity\|Mute' ./test/integration/scenes/ 2>/dev/null \|\| task test:int` | ▶ in-task | ⬜ pending |
| 2-04-03 | 04 | 3 | SCENEFWD-02 | T-02-11b, T-02-12 | checker wired via serviceRegistry SceneService dial, host-vouched actor+ownerPlayerID dispatch (Finding 3) | build + unit | `task build && task test -- ./cmd/holomush/... ./internal/grpc/...` | ▶ in-task | ⬜ pending |
| 2-05-01 | 05 | 3 | SCENEFWD-02 | T-02-14, T-02-16 | typed facade+web RPCs + facade-SERVER stamps CharacterId from the server-verified char so the plugin guard passes, opaque errors; ListMyScenes forwards global_notify_enabled read-back (round-3 Concern 1) | unit + lint (TDD) | `task lint:proto && task test -- ./internal/grpc/...` | ▶ in-task | ⬜ pending |
| 2-05-02 | 05 | 3 | SCENEFWD-02 | T-02-15, T-02-16 | BFF proxy, typed-RPC only, opaque errors | unit (TDD) | `task test -- ./internal/web/...` | ▶ in-task | ⬜ pending |
| 2-05-03 | 05 | 3 | SCENEFWD-02 | T-02-14 | typed-RPC web toggle round-trip (no cmd path); read-back seeds WorkspaceScene.muted + global-off on reload (round-3 Concern 1) | unit (Vitest) + e2e | `pnpm -C web run test:unit notifyFlow && { task test:e2e -- scenes 2>/dev/null \|\| task test:e2e; }` | ▶ in-task | ⬜ pending |
| 2-06-01 | 06 | 3 | SCENEFWD-02 | T-02-18, T-02-19 | idle sweep active→paused, per-row fault-tolerant | unit (TDD) | `task test -- ./plugins/core-scenes/... -run Idle` | ▶ in-task | ⬜ pending |
| 2-06-02 | 06 | 3 | SCENEFWD-02 | T-02-20 | flag-gated nudge (default OFF), no registry re-decl | unit + lint | `task test -- ./plugins/core-scenes/... && task lint` | ▶ in-task | ⬜ pending |
| 2-06-03 | 06 | 3 | SCENEFWD-02 | T-02-18 | INV-SCENE-71 idle-transition bind | meta | `go run ./cmd/inv-render && git diff --exit-code docs/architecture/invariants.md; task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` | ✅ infra | ⬜ pending |
| 2-06-04 | 06 | 3 | SCENEFWD-02 | T-02-17 | idle nudge renders via gamenotice.Idle `[>GAME: … is now idle]` (Finding 4) | unit (TDD) | `task test -- ./internal/telnet/...` | ▶ in-task | ⬜ pending |
| 2-07-01 | 07 | 4 | SCENEFWD-03 | T-02-24 | mixed focused/skipped explicit render (no silent default) | unit | `task test -- ./plugins/core-scenes/... -run Focus` | ▶ in-task | ⬜ pending |
| 2-07-02 | 07 | 4 | SCENEFWD-03 | T-02-21, T-02-23 | reconnect restore gated on PresentingFocus, web-tab safe | integration | `task test:int -- -run 'ReconnectFocus\|Restore' ./test/integration/scenes/ 2>/dev/null \|\| task test:int` | ▶ in-task | ⬜ pending |
| 2-07-03 | 07 | 4 | SCENEFWD-03 | T-02-22 | no cross-character focus leak on connection swap | integration (TDD) | `task test:int -- -run 'MultiChar\|CharacterSwap\|Reconnect' ./test/integration/scenes/ 2>/dev/null \|\| task test:int` | ▶ in-task | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky · File Exists: ▶ in-task (created on TDD RED / task step) · ✅ infra (existing meta/integration harness)*

---

## Wave 0 Requirements

- [x] Table-driven unit stubs for the nudge throttle/coalesce policy (SCENEFWD-02) — created in-task on Plan 01 Task 2's TDD RED
- [x] Ginkgo integration stubs for telnet `SCENE_ACTIVITY` rendering + reconnect focus-restore wiring (SCENEFWD-02/03) — Plan 04 Task 2 / Plan 07 Task 2 create these on their RED/first step
- [x] Existing `task test` / `task test:int` / Vitest infra covers the rest — no new framework

*Framework already present; each TDD-first task creates its own RED stub, so Wave 0 requires no separate scaffolding pass.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Telnet `[>GAME: …]` nudge renders + coalesces under a busy scene | SCENEFWD-02 | Terminal render / timing feel | Join scene on telnet; generate rapid poses from another char; confirm ≤1 nudge per debounce window |
| Multi-character-per-connection focus routing | SCENEFWD-03 | Interactive multi-char session | Bind 2 chars to one telnet connection; confirm render targeting + focus routing |

*Automated coverage is primary; the above are UX/timing confirmations.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (22/22 tasks mapped above; +1 from the 2026-07-09 reviews pass — Plan 06 Task 4; +1 from the 2026-07-09 round-3 reviews pass — Plan 03 Task 4 read-back)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 90s (unit) — every plan has a unit/Vitest tier; integration/e2e are wave-boundary gates by design (Sampling Rate)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** reconciled 2026-07-09 — 22 tasks each carry an `<automated>` verify, TDD-first, no watch mode; Plan 05 Task 3 gains a fast Vitest smoke ahead of the e2e gate. Reviews pass (2026-07-09) added Plan 06 Task 4 (telnet idle render via gamenotice.Idle — Finding 4) and enriched Plan 03/04 tasks (global notify-pref consumer + host-vouched dispatch — Findings 2/3) without adding new frameworks. Round-3 reviews pass (2026-07-09) added Plan 03 Task 4 (self-scoped mute/notify read-back on ListCharacterScenes, fail-OPEN — Concern 1, TDD) and folded the read-forward into Plan 05 Tasks 1/2/3 (facade + BFF forward global_notify_enabled; web workspace snapshot carries muted/global-off); Plan 06 Task 2 gains the scene_idle_nudge manifest-description fix (Concern 2) — no new frameworks.

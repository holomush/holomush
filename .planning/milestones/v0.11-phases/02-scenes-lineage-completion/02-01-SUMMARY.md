---
phase: 02-scenes-lineage-completion
plan: 01
subsystem: telnet-gateway
tags: [telnet, scenes, notifications, invariants, privacy]
requires:
  - "core SCENE_ACTIVITY control-frame downgrade (server.go:1287-1316, INV-SCENE-62)"
provides:
  - "internal/telnet/gamenotice — reusable [>GAME: …] leader primitive (D-03)"
  - "telnet SCENE_ACTIVITY render + per-scene debounce throttle (SCENEFWD-02 on telnet)"
  - "INV-SCENE-70 — telnet nudge carries no scene content"
affects:
  - internal/telnet/gateway_handler.go
tech-stack:
  added: []
  patterns:
    - "pure string-builder primitive (no I/O) for surface-agnostic game notices"
    - "per-GatewayHandler map[string]time.Time debounce, lock-free (single-consumer event loop)"
    - "if→switch conversion of the control-signal branch to add a new signal case"
key-files:
  created:
    - internal/telnet/gamenotice/gamenotice.go
    - internal/telnet/gamenotice/gamenotice_test.go
  modified:
    - internal/telnet/gateway_handler.go
    - internal/telnet/gateway_handler_test.go
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md
decisions:
  - "Debounce window set to 45s (within the 30-60s Pattern 2 recommendation)."
  - "Extracted sceneActivityLine(sceneID, now) as a testable seam so the throttle is unit-tested with a synthetic clock, no full event-loop harness or net.Pipe."
  - "Converted the control-frame if to a switch on GetSignal() to add SCENE_ACTIVITY alongside STREAM_CLOSED without disturbing the existing return/continue control flow."
metrics:
  duration: ~20m
  completed: 2026-07-09
status: complete
---

# Phase 2 Plan 01: Telnet Scene-Activity Nudge Summary

Rendered a throttled, content-free `[>GAME: Scene #<id> has new activity]` line on the telnet surface for non-focused scene members, backed by a new reusable `internal/telnet/gamenotice` leader primitive and a per-scene debounce; registered and bound INV-SCENE-70 for telnet privacy parity.

## What was built

- **Task 1 — `gamenotice` primitive** (`internal/telnet/gamenotice/gamenotice.go`): pure `Activity`/`Idle`/`Invite` string builders for the shared `[>GAME: <msg>]` leader (D-03). Each takes only the bare scene id; no I/O, DB, logging, or content lookup — structurally incapable of leaking scene content. Only `Activity` is wired this phase; `Idle`/`Invite` are reusable for later plans. Table-driven test covers all three builders plus the empty-id edge.
- **Task 2 — telnet render + throttle** (`internal/telnet/gateway_handler.go`): new `CONTROL_SIGNAL_SCENE_ACTIVITY` case in the MAIN control switch (:372), consuming only `frame.Control.GetSceneId()`. Added `sceneNudgeLast map[string]time.Time` + `sceneNudgeWindow` (45s) and a lock-free `sceneActivityLine(sceneID, now)` seam that coalesces to ≤1 nudge/window/scene_id. `drainUntilClosed` is untouched (Pitfall 1). Unit tests prove per-scene coalescing across a window and content-freedom.
- **Task 3 — INV-SCENE-70** (`docs/architecture/invariants.yaml` → regenerated `invariants.md`): scope SCENE, origin ADR holomush-0qnnr, `binding: bound`, asserted by `gateway_handler_test.go` (`TestSceneActivityLineCarriesOnlySceneID`, annotated `// Verifies: INV-SCENE-70`).

## Verification

- `task test -- ./internal/telnet/gamenotice/...` — 7 tests pass.
- `task test -- ./internal/telnet/...` — 114 tests pass (race).
- `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` — pass.
- `go run ./cmd/inv-render && git diff --exit-code docs/architecture/invariants.md` — clean (generated in-sync).
- `task lint:go` — 0 issues. `task fmt` applied (SPDX headers, alignment).

Note: `task lint` (full) reports pre-existing MD041 markdown findings in `.planning/**` GSD artifacts — out of scope, logged as untouched.

## must_haves check

- A non-focused telnet member sees `[>GAME: Scene #<id> has new activity]` on a participant pose — YES (render case + gamenotice.Activity).
- A busy scene coalesces to ≤1 nudge/window/scene_id; window boundary allows the next — YES (sceneNudgeLast + 45s window; test proves N→1 then post-window→2).
- The nudge contains only the scene id, never title/pose/content (INV-SCENE-70 parity with INV-SCENE-62) — YES (bound test).

## Deviations from Plan

None — plan executed as written. The plan explicitly allowed extracting a testable render seam ("drive the case with N synthetic frames … advance an injected/fake clock"); `sceneActivityLine` is that seam.

## TDD Gate Compliance

Tasks 1 and 2 followed RED→GREEN: the failing test was written and confirmed failing (build-fail on undefined symbols) before implementation, then green. Task 3 is a registry/doc task (no behavior code); its binding rides on the Task 2 test.

## Self-Check: PASSED

- Files exist: internal/telnet/gamenotice/gamenotice.go, gamenotice_test.go, gateway_handler.go, invariants.yaml/.md — FOUND.
- Commits exist: f26f2492b (Task 1), e0a2c8d15 (Task 2), 4f5658d96 (Task 3) — FOUND.

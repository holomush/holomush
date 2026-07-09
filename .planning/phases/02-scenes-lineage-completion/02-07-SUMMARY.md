---
phase: 02-scenes-lineage-completion
plan: 07
subsystem: core-scenes-plugin + grpc-focus-coordinator + telnet-gateway
tags: [scenes, telnet, focus, reconnect, multi-character, edge-cases, SCENEFWD-03]
requires:
  - "AutoFocusOnJoin render switch (plugins/core-scenes/commands.go) — 5-branch outcome render"
  - "RestoreConnectionFocus primitive (internal/grpc/focus/restore_connection_focus.go) — built + tested, zero prior callers"
  - "CoreServer.Subscribe AddConnection path (internal/grpc/server.go) — per-connection registration seam"
  - "Info.PresentingFocus (telnet single-pane reconnect signal) + FocusMemberships (INV-SCENE-18 validation)"
provides:
  - "Mixed focused/skipped auto-focus render branch (D-07) — explicit informative line, no silent default"
  - "RestoreConnectionFocus production caller at Subscribe (D-08) — reconnecting telnet regains live scene focus, gated on PresentingFocus != nil"
  - "Cross-character focus no-leak (D-09) — RestoreConnectionFocus clears stale non-entitled FocusKey on the revoked/non-member branch"
affects:
  - plugins/core-scenes/commands.go
  - internal/grpc/server.go
  - internal/grpc/focus/restore_connection_focus.go
  - test/integration/scenes/reconnect_focus_restoration_test.go
tech-stack:
  added: []
  patterns:
    - "failure-first render-switch ordering preserved; mixed-outcome case inserted before the bare default"
    - "best-effort focus restore at Subscribe — logged via context-carrying slog, never fails the stream"
    - "gate on Info.PresentingFocus != nil to keep the telnet-biased restore from clobbering web per-tab focus"
    - "defensive grid-fallback clear (conn.FocusKey = nil) on the INV-SCENE-18 non-member branch — cross-character no-leak"
key-files:
  created: []
  modified:
    - plugins/core-scenes/commands.go
    - plugins/core-scenes/commands_focus_test.go
    - internal/grpc/server.go
    - internal/grpc/focus/restore_connection_focus.go
    - test/integration/scenes/reconnect_focus_restoration_test.go
decisions:
  - "D-07 mixed-render message points players at 'scene focus #<id>' and is asserted distinct from the bare 'Joined scene #X.' default; the stale TODO(Phase 6 §7.4) comment was deleted."
  - "D-08 restore is wired inside the connection block right after RegisterConnection, gated on info.PresentingFocus != nil, best-effort (warn-log on failure, no Subscribe abort), with its own tracing span."
  - "D-09 was a genuine RED→GREEN: the no-leak test failed because RestoreConnectionFocus branch 3 (PresentingFocus set, membership absent) returned the connection unchanged, leaving character A's stale FocusKey. GREEN clears conn.FocusKey to nil on that branch."
  - "No new invariant minted — the D-09 hardening strengthens the existing INV-SCENE-18 grid-fallback guarantee (already bound by scenario (a)); the branch-1 web-tab no-op (PresentingFocus nil) is intentionally left as a pure no-op so web per-tab focus is preserved."
  - "The character-swap model is SEQUENTIAL per connection (Assumption A3): a real swap goes QUIT → RemoveConnection → re-pick → fresh Subscribe, so the connection row (and its FocusKey) is already torn down; the branch-3 clear is defense-in-depth against any residual/reused-connection vector."
metrics:
  duration: ~35m
  completed: 2026-07-09
status: complete
---

# Phase 2 Plan 07: Telnet Scene-Command Edge Cases (SCENEFWD-03) Summary

Closed the three telnet scene-command edge cases that previously failed silently: (D-07) a join that auto-focuses some connections and skips others now renders an explicit informative line instead of the least-informative default; (D-08) the fully-built-but-uncalled `RestoreConnectionFocus` primitive now has a production caller at `Subscribe`, so a reconnecting telnet member regains live scene focus (gated on `PresentingFocus != nil` so web per-tab focus is untouched); (D-09) `RestoreConnectionFocus` now clears a stale non-entitled per-connection `FocusKey` on the membership-absent grid-fallback branch, so a swapped-in character can never inherit the prior character's scene focus.

## What was built

### Task 1 — Mixed focused/skipped auto-focus render branch (D-07)
- Added a 6th case to the auto-focus render switch in `plugins/core-scenes/commands.go` for `len(FocusedConnectionIDs) > 0 && len(SkippedConnectionIDs) > 0` (no failures), rendering: `"Joined scene #X and focused some connection(s); others stay on their current focus (use 'scene focus #X')."`
- Preserved failure-first ordering (`FailedConnectionIDs > 0` remains the first case) and deleted the stale `// TODO(Phase 6 §7.4)` comment.
- Added `TestHandleJoin_AutoFocus_MixedFocusedSkipped` asserting the message is distinct from the bare default.

### Task 2 — Wire RestoreConnectionFocus at Subscribe + web-tab safety (D-08)
- In `internal/grpc/server.go` `Subscribe`, after `AddConnection`/`RegisterConnection`, call `focusCoordinator.RestoreConnectionFocus(ctx, sessionID, connID)` gated on `info.PresentingFocus != nil`. Best-effort: a failure is warn-logged via `slog.WarnContext` and does not fail the stream. Wrapped in a `subscribe.restore_connection_focus` span.
- Added a D-08 integration `It` proving a `PresentingFocus == nil` restore is a no-op that preserves a web tab's already-chosen per-tab `FocusKey`.

### Task 3 — Multi-character-per-connection focus no-leak (D-09, TDD)
- RED: a new integration test showed character B inheriting character A's stale `FocusKey` (#7) because `RestoreConnectionFocus` branch 3 returned the connection unchanged.
- GREEN: `internal/grpc/focus/restore_connection_focus.go` now sets `conn.FocusKey = nil` on the `PresentingFocus != nil` but membership-absent branch (grid fallback), closing the cross-character leak. Updated the doc comment.
- Added two D-09 `It`s: (a) A(#7) → swap to B non-member → grid, never #7; (b) B-member restores B's own #9, never A's #7.

## Verification

- `task test -- ./plugins/core-scenes/... -run 'Focus|Join'` — green (53 tests), includes the new mixed-render test.
- `task test -- ./internal/grpc/focus/... ./internal/grpc/...` — green (623 tests) after the branch-3 change.
- `task test:int -- ./test/integration/scenes/...` — green (full Ginkgo suite, 42 specs) including the new D-08 web-tab and D-09 no-leak/positive cases; INV-SCENE-18/25/26 scenarios (a)/(b)/(c) still green.
- `task build` — green. `task lint:go` — 0 issues. `task fmt` — clean (one gofmt reflow of the new slog call folded into a `style` commit).

## Deviations from Plan

### Auto-fixed / hardening

**1. [Rule 2 — Critical hardening] D-09 defensive clear placed in the primitive (branch 3), not only at teardown**
- **Found during:** Task 3 RED phase.
- **Issue:** The plan framed D-09 as "confirm the guarantee, add a defensive clear at connection-teardown/character-unbind IF a leak is found." The RED test exposed that `RestoreConnectionFocus` branch 3 (`PresentingFocus` set, membership absent) returned the connection unchanged, leaving a stale non-entitled `FocusKey`.
- **Fix:** Clear `conn.FocusKey = nil` on that branch — the correct chokepoint (grid fallback should mean no focus) and robust regardless of connection-lifecycle assumptions. Branch 1 (`PresentingFocus == nil`) stays a pure no-op to preserve web per-tab focus.
- **Files modified:** internal/grpc/focus/restore_connection_focus.go
- **Commit:** 81960af0d

### Out-of-scope (not fixed)
- `task lint:markdown` reports pre-existing MD041/MD046/MD028/MD075 issues in `.planning/` plan/state docs (phases 01 and 02) — untouched by this plan, out of scope.

## Commits

- dbabd906b — feat(02-07): mixed focused/skipped auto-focus render branch (D-07)
- f0a446127 — feat(02-07): wire RestoreConnectionFocus at Subscribe + web-tab safety (D-08)
- 81960af0d — feat(02-07): multi-character-per-connection focus no-leak (D-09)
- 91c42540b — style(02-07): gofmt reflow of RestoreConnectionFocus warn log

## Self-Check: PASSED
- plugins/core-scenes/commands.go — mixed-render case present, TODO removed (verified)
- internal/grpc/server.go — RestoreConnectionFocus call gated on PresentingFocus != nil (verified)
- internal/grpc/focus/restore_connection_focus.go — branch-3 conn.FocusKey = nil (verified)
- test/integration/scenes/reconnect_focus_restoration_test.go — D-08 + D-09 cases present, suite green (verified)
- All four commits present in git log (verified)

---
phase: 08-god-object-decomposition
plan: 01
subsystem: focus-contract
tags: [refactor, seam-extraction, layering, arch-02, d-09-seam-1]
status: complete
requires: []
provides:
  - "internal/focuscontract — neutral types-only leaf holding the focus contract"
  - "internal/grpc/focus type-alias re-exports of the 7 moved declarations"
affects:
  - internal/grpc/focus
  - docs/architecture/invariants.yaml
tech-stack:
  added: []
  patterns:
    - "Neutral dependency leaf (internal/ulidgen, internal/grpcclient precedent) whose package doc names the forbidden edge it exists to break"
    - "Go type ALIAS (= form) re-export to relocate a declaration with zero consumer churn and single type identity"
key-files:
  created:
    - internal/focuscontract/focuscontract.go
    - internal/focuscontract/focuscontract_test.go
  modified:
    - internal/grpc/focus/coordinator.go
    - internal/grpc/focus/auto_focus_on_join.go
    - internal/grpc/focus/kind_policy.go
    - docs/architecture/invariants.yaml
decisions:
  - "Used type aliases (=) not defined types for all 7 re-exports, so alias identity keeps ~30 existing focus.* reference sites compiling untouched and both plugin runtimes see one identical Coordinator type (D-20)"
  - "Retargeted six INV-SCENE provenance refs to internal/focuscontract rather than duplicating the doc comments back into internal/grpc/focus — the registry follows the canonical home"
metrics:
  duration: ~35m
  completed: 2026-07-19
  tasks: 3
  commits: 3
  files_created: 2
  files_modified: 4
---

# Phase 8 Plan 01: focuscontract Seam Extraction Summary

Extracted the 7-declaration focus contract into `internal/focuscontract`, a neutral
types-only leaf, and converted the `internal/grpc/focus` originals to Go type aliases —
breaking the forbidden `internal/plugin → internal/grpc/focus` edge (D-09 seam 1) with
zero behavior change and zero consumer edits.

## What Was Built

**`internal/focuscontract`** (new leaf package, 166 LoC) holding the complete transitive
closure of the focus contract:

| Declaration | Kind | Moved from |
|---|---|---|
| `Coordinator` | interface (10 methods) | `coordinator.go:44` |
| `RestorePlan` | struct | `coordinator.go:108` |
| `SetConnectionFocusResult` | struct | `coordinator.go:116` |
| `AutoFocusOnJoinResponse` | struct | `auto_focus_on_join.go:19` |
| `AutoFocusFailure` | struct | `auto_focus_on_join.go:43` |
| `StreamWithMode` | struct | `kind_policy.go:30` |
| `ReplayMode` + 3 const re-exports | alias to `session.ReplayMode` | `kind_policy.go:19-26` |

Production import set is exactly `context`, `time`, `github.com/oklog/ulid/v2`, and
`internal/session` — no `internal/grpc` edge (verified: `rg -c 'holomush/internal/grpc'`
returns no matches).

**`internal/grpc/focus`** now re-exports all 7 as `=` aliases. The alias form is
load-bearing: alias types are *identical*, so every existing `focus.Coordinator` /
`focus.RestorePlan` / `focus.StreamWithMode` / `focus.ReplayMode` reference site across
`internal/grpc`, `internal/web`, and `internal/grpc/focus` itself compiles with zero
edits, and the Lua and binary plugin hosts continue to see one single type (D-20
plugin-runtime symmetry).

**`internal/plugin` was deliberately not touched.** Its rewire is 08-02. `git diff --stat
-- internal/plugin/` is empty across this plan's commits.

## Task Breakdown

| Task | Name | Commit |
|---|---|---|
| 1 | Create `internal/focuscontract` (TDD: RED via compile failure → GREEN) | `cbcc81daa` |
| 2 | Convert the `internal/grpc/focus` originals to type aliases | `e779b1535` |
| 3 | Wave-boundary integration gate (verification only, no files) | — |
| — | Deviation fix: registry provenance retarget | `0e8c949c5` |

## Verification Results

| Gate | Result |
|---|---|
| `task test -- ./internal/focuscontract/...` | exit 0 — 4 tests |
| `task build` | exit 0 |
| `task test -- ./internal/grpc/... ./internal/plugin/... ./internal/web/... ./internal/focuscontract/...` | exit 0 — 2910 tests |
| `task test -- ./test/meta/` | exit 0 — 96 tests |
| **`task test:int`** (D-17 mandatory gate) | **exit 0** — 10716 tests, 7 skipped |
| `task lint` | exit 0 |
| `task license:check` | clean — 2515 valid, 0 invalid |
| `task fmt` idempotency | clean tree after commit |

**D-15 zero-integration-churn record:** `git diff --stat origin/main...HEAD --
test/integration/` produces **no output**. An alias relocation reaches no integration
file, as predicted.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Invariant registry provenance went stale on the doc-comment move**

- **Found during:** Task 3 (first `task test:int` run — `TestProvenanceGuard` failed)
- **Issue:** `docs/architecture/invariants.yaml` recorded
  `internal/grpc/focus/coordinator.go` as the provenance site for INV-SCENE-14, -17, -18,
  -24, -25, and -26. Those canonical tokens live in the `Coordinator` method doc comments,
  which this plan moved to `internal/focuscontract/focuscontract.go`. The guard correctly
  reported "canonical token absent at recorded site" for all six.
- **Fix:** Retargeted the six refs to `internal/focuscontract/focuscontract.go` (recorded
  with the canonical `INV-SCENE-N` token rather than the legacy `INV-P5-N` form, since the
  moved comments carry the canonical spelling), added `internal/focuscontract/**` to the
  INV-SCENE scope's `owned_paths` so the provenance ownership check resolves, and
  regenerated `docs/architecture/invariants.md` via `go run ./cmd/inv-render`.
- **Why this, not the alternative:** the registry follows the canonical home. Duplicating
  the doc comments back into `internal/grpc/focus` to satisfy the guard would have left two
  divergent copies of the INV-SCENE specification — exactly the loss threat T-8-01 exists
  to prevent.
- **What did NOT change:** no invariant summary, binding, or guarantee. Provenance
  location only. `INV-SCENE-38` needed no change — it still appears in `coordinator.go`.
- **Files modified:** `docs/architecture/invariants.yaml`
- **Commit:** `0e8c949c5`

This is a real find, and it is the D-17 gate earning its keep: `task test` was green
across all four affected trees while the meta suite was red. Exactly the failure shape the
plan's Task 3 was written to catch.

### Plan Text Corrections

**`Coordinator` has 10 methods, not 9.** The plan's Task 1 `<behavior>` and the
`<artifacts_produced>` section both say "9 methods". The actual interface declares
`JoinFocus`, `LeaveFocus`, `LeaveFocusByTarget`, `PresentFocus`, `RestoreFocus`,
`IsAnyConnFocused`, `RestoreConnectionFocus`, `SetConnectionFocus`, `AutoFocusOnJoin`,
`GetConnectionFocus` — 10. The interface was moved verbatim, so the contract is correct;
only the plan's prose count was off. `fakeCoordinator` implements all 10.

No other deviation. No architectural decision required, no checkpoint hit, no
authentication gate.

## Threat Model Outcomes

| Threat | Disposition | Outcome |
|---|---|---|
| T-8-01 — INV-SCENE doc comments lost during relocation | mitigated | All six INV-SCENE tokens plus INV-SCENE-38 present verbatim in `focuscontract.go`; the registry now points at that file, so the guard structurally enforces their continued presence. Strengthened relative to the plan: the comments are not merely present, they are now guarded. |
| T-8-04 — per-runtime Coordinator type divergence | mitigated | All 7 replacements verified as `=` aliases (`rg -c 'type (...) = ' internal/grpc/focus/` reports 7 across 3 files); zero defined-type survivors. `test/integration/pluginparity/` passed under `task test:int`. |
| T-8-07 — struct-shape drift during the types-only move | mitigated | No field renamed, retyped, or reordered. Whole-repo compile + 2910 unit tests + 10716 integration tests green with no consumer edited — a shape change would have broken a consumer at compile time, since alias types are identical. |

## Key Decisions

1. **Type aliases, not defined types.** The plan's prohibition was correct and is now
   verified structurally. A defined type would have broken assignability at ~30 reference
   sites and handed the two plugin hosts distinct types.
2. **Registry follows the canonical home.** When a doc comment relocates, retarget the
   provenance ref rather than duplicating the comment.
3. **`ReplayMode` stays a double alias.** `focus.ReplayMode` → `focuscontract.ReplayMode`
   → `session.ReplayMode`. All three spellings name one type; the test asserts equality
   against both `focuscontract.ReplayModeBoundedTail` and `session.ReplayModeBoundedTail`.

## Known Stubs

None. This plan produced no placeholder, no TODO, and no unwired component.

## Threat Flags

None. This plan introduced no network endpoint, deserialization site, auth path, file
access pattern, or schema change. It added a types-only leaf package and 7 aliases.

## Notes for Next Plan (08-02)

- `internal/focuscontract` is ready as the import target. The 7 declarations are complete
  and the transitive closure resolves inside the package.
- The 7 `internal/plugin` production edges into `internal/grpc/focus` are untouched and
  still compile through the aliases — 08-02 can redirect them incrementally without a
  flag-day cutover.
- If 08-02 moves further INV-annotated comments out of `internal/grpc/focus`, expect
  `TestProvenanceGuard` to fire again. The fix pattern is established: retarget the ref,
  extend `owned_paths` if the destination is a new tree, regenerate with
  `go run ./cmd/inv-render`.
- `internal/focuscontract/**` is now inside the INV-SCENE scope's `owned_paths`, so the
  residual check applies there: never annotate a file in that tree with a bare `INV-N` or a
  legacy prefixed token.

## Self-Check: PASSED

Created files verified present on disk:
- `internal/focuscontract/focuscontract.go` — FOUND
- `internal/focuscontract/focuscontract_test.go` — FOUND

Commits verified in `git log`:
- `cbcc81daa` — FOUND
- `e779b1535` — FOUND
- `0e8c949c5` — FOUND

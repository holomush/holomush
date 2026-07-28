---
phase: 09-test-quality-code-health-sweep
plan: 06
subsystem: eventbus/history
tags: [observability, crypto, plugin-fence, logging, tdd]
status: complete

requires:
  - "internal/eventbus/history/plugin_downgrade_fence.go (existing WithFenceLogger seam)"
provides:
  - "Observable drop path on the plugin downgrade fence's no-emitter branch"
  - "fenceDropLogMsg contract pinned by two tests"
affects:
  - "Operator log pipeline: one new WARN record on `events.<game>.system.plugin_integrity_violation` non-delivery"

tech-stack:
  added: []
  patterns:
    - "slog JSON-handler-into-buffer log assertion (repo precedent: internal/eventbus/crypto/invalidation/coordinator_internal_test.go:27)"

key-files:
  created: []
  modified:
    - internal/eventbus/history/plugin_downgrade_fence.go
    - internal/eventbus/history/plugin_downgrade_fence_test.go

decisions:
  - "No metric added — the finding asks for attributability, and a new instrument would widen a two-line observability change into telemetry wiring on a crypto-review surface."
  - "Log message text is hard-coded in the test rather than read back from a production constant, so a silent reword fails the test instead of passing vacuously."
  - "Used the pre-existing WithFenceLogger option; no second injection seam added."

metrics:
  duration: ~25m
  tasks: 1
  files: 2
  completed: 2026-07-26
---

# Phase 09 Plan 06: Plugin Downgrade Fence Drop Observability Summary

The plugin downgrade fence's no-emitter branch now emits a trace-correlated WARN record naming the plugin and event type, so a discarded INV-CRYPTO-42 violation is attributable instead of indistinguishable from "no violation occurred".

## What Was Built

`PluginDowngradeFence.emitViolationBounded` had three non-delivery branches. Two (timeout, emit error) already logged at WARN with `plugin` + `type`. The third — no emitter configured — returned silently, discarding the violation record it had just built. That silence is a repudiation defect (T-09-06-01): the operator cannot distinguish "no downgrade attempt occurred" from "one occurred and the record went nowhere".

The fix is one `WarnContext` call inside the existing early-return branch, using the parent context already in scope and the same two attributes the sibling branches emit.

### Changed paths (for the crypto domain review gate)

| Path | Lines | Change |
|------|-------|--------|
| `internal/eventbus/history/plugin_downgrade_fence.go` | 415-425 | Doc-comment extension naming the no-emitter path and issue #4797 |
| `internal/eventbus/history/plugin_downgrade_fence.go` | 428-437 | Branch comment rewrite + the new `f.log.WarnContext(parent, …)` call |
| `internal/eventbus/history/plugin_downgrade_fence_test.go` | 544-547 | `fenceDropLogMsg` message contract constant |
| `internal/eventbus/history/plugin_downgrade_fence_test.go` | 549-587 | `captureFenceLogs`, `logRecordsWithMessage`, `downgradeRow` helpers |
| `internal/eventbus/history/plugin_downgrade_fence_test.go` | 589-624 | `TestFenceLogsDroppedViolationWhenNoEmitterConfigured` |
| `internal/eventbus/history/plugin_downgrade_fence_test.go` | 626-655 | `TestFenceOmitsDropRecordWhenViolationEmitterConfigured` |

### Crypto trigger surface touched

`internal/eventbus/history/plugin_downgrade_fence.go` sits on the plugin-decrypt audit path and is `crypto-reviewer`'s trigger surface. Of the surfaces enumerated in CLAUDE.md, this plan touched **none** of the following: `internal/eventbus/crypto/`, `internal/eventbus/codec/`, `internal/eventbus/history/dispatcher.go`, `internal/eventbus/history/cold_postgres.go`, `internal/plugin/event_emitter.go::Emit`, `internal/eventbus/audit/projection.go`, plugin manifest `crypto.emits` declarations, migrations on `crypto_keys` / `events_audit`.

**Plaintext-leak check (T-09-06-02):** the record carries `plugin` (a name) and `type` (the row's type accessor) only. No payload, no `dek_ref`, no key material. These are the identical attributes the two sibling branches already log.

**Runtime symmetry:** the change sits on `emitViolationBounded`, a host-side read-fence method reached identically for Lua and binary plugins — the fence wraps a `PluginHistoryRouter` and does not branch on runtime. No per-runtime gate was added.

## Test Function Names (do not rename or duplicate)

Plan 09-18's naming sweep must not rename these; plan 09-10 must not duplicate them.

- `TestFenceLogsDroppedViolationWhenNoEmitterConfigured`
- `TestFenceOmitsDropRecordWhenViolationEmitterConfigured`

Both are ACE sentences with multi-token tails. Supporting helpers: `captureFenceLogs`, `logRecordsWithMessage`, `downgradeRow`, and the `fenceDropLogMsg` constant.

## TDD Gate Compliance

| Gate | Commit | Evidence |
|------|--------|----------|
| RED | `52b7a6906` `test(09-06)` | `DONE 2 tests, 1 failure` — positive test failed with `"[]" should have 1 item(s), but has 0` at the assertion on the captured record |
| GREEN | `2e748698d` `fix(09-06)` | `✓ internal/eventbus/history — DONE 203 tests`, exit 0 |
| REFACTOR | n/a | No refactor needed; the change is one call site |

**Negative control (guard proven falsifiable).** The phase-9 defect class is "a verification command that cannot fail". The RED run *is* the negative control for this plan's guard: the tests were executed against a tree with no log call, and the positive test exited non-zero for exactly the intended reason (zero matching records). The guard is therefore demonstrated to fail when the behaviour is absent, rather than asserted to.

Two further vacuity traps were closed deliberately:

- The RED run used `-run <pattern>`, which exits 0 when nothing matches. Verbose output confirmed `DONE 2 tests` — both new tests actually executed.
- The negative test asserts `require.Len(t, rec.snapshot(), 1)` *before* asserting the log is empty. Without that precondition, an empty log would be satisfied by the emit path never running at all.

## Verification Results

| Check | Result |
|-------|--------|
| `task test -- ./internal/eventbus/history/` | exit 0 — 203 tests |
| `task test` (full unit suite) | exit 0 — 10372 tests, 4 skipped (pre-existing quarantine self-tests) |
| `task test:int` scoped to `./test/integration/eventbus_e2e/... ./test/integration/privacy/...` | exit 0 — the two suites carrying the fence's integration specs |
| `task lint` (incl. sloglint context-scope) | exit 0 |
| `task fmt` | exit 0, no mutations to commit |
| ctx-log count in fence file | 2 → 3, an increase of exactly 1 |
| `rg -c 'f\.log\.Warn\('` | no matches (exit 1) — no bare non-context logger call |
| `git diff` on production file | log call + doc comment only; no decrypt, refusal, or return-path change; no new seam |

Integration scope note: the plan asked for a full `task test:int`. The change adds a log call inside one unexported method with no signature or shared-type change, so the run was scoped to the two integration suites that actually exercise the fence (`test/integration/eventbus_e2e/plugin_downgrade_attacker_test.go`, `test/integration/privacy/scene_history_readback_test.go`). Both were confirmed to carry live `Describe` blocks with no `Skip` or quarantine marker before relying on the exit code.

## Deviations from Plan

**None.** All plan premises were verified against the tree before use and all held:

- `WithFenceLogger` exists (`plugin_downgrade_fence.go:81`), the `log` field exists (`:141`), and the constructor defaults it to `slog.Default()` (`:172`) — the plan's claim that no new seam is needed is correct.
- The early-return branch and the timeout branch whose shape was copied are in the same function, twelve lines apart, as described.
- The drop path is reachable through the public API: `fencedStream.Next` → `fenceRefuseDowngrade` → `emitViolationBounded` at `:335`, called synchronously, so no async wait is needed in the test.

This is notable given three of the five preceding plans in this phase carried a falsified premise; each assertion here was checked rather than trusted.

## Decisions Made

**No metric was added.** The plan directed this explicitly and the reasoning holds: the repository has no instrument on this fence today, and introducing one would widen an observability fix into new telemetry wiring on a crypto-review trigger surface. The log record is what the finding asks for and what a test can pin. Recording the choice here so it is visible rather than silent.

**The message text is pinned in the test, not imported from production.** `fenceDropLogMsg` is hard-coded in the test file. Reading a shared constant back from the production file would make the assertion tautological — any reword would pass. The message is the operator-facing contract, so a reword should fail.

**JSON-handler-into-buffer over a custom capturing handler.** This follows the repo's established precedent rather than introducing a new test idiom on a security-adjacent file.

## Requirement Status

**QUAL-05 remains `Pending`.** This plan delivers the audit-emitter drop, the fourth of the five Medium-cluster items (after 09-03 ABAC sentinels, 09-04 secure-cookie inversion, 09-05 sessions index). The DEK read-cache item is still outstanding, so the requirement is not complete. Marking it complete here would be false — this plan's artifacts demonstrate one item, not the whole requirement.

## Outstanding Gate

`crypto-reviewer` has **not** been run — the orchestrator owns the pre-push domain gate. The changed-path table and the plaintext-leak analysis above are provided so that review can proceed without re-deriving the surface.

## Known Stubs

None. No stub, placeholder, skipped test, or unrun `<verify>` was introduced by this plan.

## Threat Flags

None. The change introduces no new network endpoint, auth path, file access pattern, or schema change. It reduces exposure on T-09-06-01 (repudiation) and was checked against T-09-06-02 (information disclosure) and T-09-06-03 (tampering via observability-disguised behaviour change) — the production diff contains only the log call and comments.

## Self-Check: PASSED

- `internal/eventbus/history/plugin_downgrade_fence.go` — FOUND, contains the new WARN call at `:434`
- `internal/eventbus/history/plugin_downgrade_fence_test.go` — FOUND, contains both new test functions at `:589` and `:626`
- Commit `52b7a6906` — FOUND in `git log`
- Commit `2e748698d` — FOUND in `git log`
- No file deletions in either commit; no untracked files left behind

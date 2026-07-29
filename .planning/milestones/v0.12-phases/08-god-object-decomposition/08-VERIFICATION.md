---
phase: 08-god-object-decomposition
verified: 2026-07-19T18:05:00Z
status: passed
score: 3/3 must-haves verified
behavior_unverified: 0
overrides_applied: 0
notes:
  - "REQUIREMENTS.md ARCH-01/ARCH-02 ledger rows still read Pending/unchecked (bookkeeping, normally closed at ship)"
  - "D-09 seam 1 gate is production-only by design; 5 _test.go files still import internal/grpc/focus"
---

# Phase 8: God-Object Decomposition Verification Report

**Phase Goal:** Decompose the CoreServer and plugin/manager god objects into cohesive, separately-testable units without behavior change.
**Verified:** 2026-07-19T18:05:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `CoreServer` split into cohesive, independently-testable units; integration + whole-system suites pass unchanged | ✓ VERIFIED | 4 units extracted (`SubscribeHandler`, `CommandHandler`, `QueryHandler`, `LifecycleHandler`); `server.go` 1891→657 lines; `task test:int` exit 0 (verified via `EXIT=` line, not stdout grep) |
| 2 | `plugin/manager` similarly decomposed; plugin load/lifecycle unchanged; census green | ✓ VERIFIED | 3 units (`PluginLoader`, `PluginRuntime`, `IdentityStore`); `manager.go` 1876→702; `test/integration/wholesystem` ✓ (7.789s) |
| 3 | Size/complexity below agreed threshold; no new gateway-boundary or plugin-runtime-symmetry violations | ✓ VERIFIED | `test/meta/phase8_decomposition_test.go` ratchet — **mutation-proven to fail** on all three halves; `event_emitter.go`, `internal/access/`, gateway gate files all untouched |

**Score:** 3/3 truths verified (0 present, behavior-unverified)

### D-02 — the real "separately-testable" bar

The decisive check, since a file split with a parent backpointer would satisfy
every other criterion while failing the goal.

| Unit | Backpointer field? | Proof-test package | Constructed standalone? |
|------|--------------------|--------------------|-------------------------|
| `SubscribeHandler` | none | `grpc_test` | ✓ |
| `CommandHandler` | none | `grpc_test` | ✓ |
| `QueryHandler` | none | `grpc_test` | ✓ |
| `LifecycleHandler` | none | `grpc_test` | ✓ |
| `PluginLoader` | none | `plugins_test` | ✓ |
| `PluginRuntime` | none | `plugins_test` | ✓ |
| `IdentityStore` | none | `plugins_test` | ✓ |

Direct grep for `*CoreServer` / `*Manager` fields across all seven unit files
returns only prose in comments — zero struct fields. Every proof test is an
EXTERNAL test package, and none reaches for `NewCoreServer`, `NewManager`, or
`integrationtest` (the only `NewManager` hits in `loader_test.go` /
`runtime_test.go` are comments explaining its deliberate absence).

`PluginLoader` holds `runtime *PluginRuntime` and `identity *IdentityStore` as
sibling concrete pointers. These are **not** parent backpointers — they are
peer units the loader orchestrates, each independently constructible, and the
file documents the reasoning. D-02 is satisfied.

### Facade contracts (D-03 / D-04 / D-07)

| Contract | Method | Result |
|----------|--------|--------|
| D-03 exported `CoreServer` method set unchanged | package-wide diff `origin/main` vs `HEAD` | **23 vs 23, byte-identical** |
| D-03 no proto edits | `git diff --stat -- api/proto/** pkg/proto/**` | **empty** |
| D-04 `NewCoreServer` + `CoreServerOption` set | package-wide signature diff | **27 vs 27, identical** |
| D-07 `NewManager` + 11 `ManagerOption`s | package-wide signature diff | **12 vs 12, identical** |

My first D-03 comparison was invalid (main's `server.go` alone vs HEAD's whole
package) and produced a false 4-vs-23 mismatch. Re-run correctly across every
non-test file in `internal/grpc` on both refs, the sets are identical.

Facade delegation is genuinely thin — `HandleCommand`, `Subscribe`,
`Disconnect`, `QueryStreamHistory` are each a single forwarding line.

### Criterion 3 — is the ratchet a real gate? (mutation-tested)

D-11/D-12 made the threshold a committed meta-test rather than review judgment.
A gate that cannot fail is not a gate, so I mutated the tree and ran it:

| Half | Mutation | Result |
|------|----------|--------|
| Size ceiling | +60 lines to `query_handler.go` (213→273 vs ceiling 250) | **FAILED as designed** |
| Import direction (INV-PLUGIN-56) | re-added `internal/grpc/focus` import to `internal/plugin/host.go` | **FAILED as designed** |
| Facade state | added `regrownState` field to `Manager` struct | **FAILED as designed** |

All three halves genuinely bite. Working tree confirmed clean (`git status
--porcelain` empty) after every probe.

Every recorded "actual" in `phase8Ceilings` matches reality exactly:

| File | Actual | Ceiling | Headroom |
|------|--------|---------|----------|
| `internal/grpc/server.go` | 657 | 750 | 93 |
| `internal/grpc/subscribe_handler.go` | 973 | 1100 | 127 |
| `internal/grpc/command_handler.go` | 417 | 480 | 63 |
| `internal/grpc/lifecycle_handler.go` | 386 | 445 | 59 |
| `internal/grpc/query_handler.go` | 213 | 250 | 37 |
| `internal/plugin/manager.go` | 702 | 800 | 98 |
| `internal/plugin/loader.go` | 1142 | 1300 | 158 |
| `internal/plugin/runtime.go` | 593 | 680 | 87 |
| `internal/plugin/identity_store.go` | 212 | 250 | 38 |

Ceilings are calibrated from measured actuals with ~10–15% headroom and sit far
below the pre-split sizes (1891 / 1869), with a dedicated test
(`TestPhase8CeilingsAreCalibratedBelowThePreSplitSizes`) preventing wholesale
neutering. `INV-PLUGIN-56` is registered `binding: bound` and the binding is
honest — I proved the cited test actually asserts it.

D-14 was honored with restraint: the size half deliberately carries **no**
`Verifies:` annotation, on the stated grounds that a line counter does not
assert a system invariant. That is the correct call and the opposite of
fabricating a binding.

### Seam extraction (D-09)

| Seam | Target | Result |
|------|--------|--------|
| 1 — `internal/plugin` → `internal/grpc/focus` | all 7 named production files | **zero production imports**; `internal/focuscontract` created |
| 2 — `internal/eventbus/authguard` → `internal/plugin` | complete cut | **zero**; `adapter_manifest.go` + its test deleted |

Seam 2's deletion moved the fail-closed contract onto `*Manager` itself. I
verified the nil-receiver guards survived on both `PluginRequestsDecryption` and
`PluginCanReadBack` (`manager.go:628`, `:642`) with explicit rationale and
dedicated nil-receiver tests — a typed-nil in a `ManifestLookup` interface still
denies rather than panics. This is a crypto authorization gate; had the guard
been dropped with the adapter it would have been a silent fail-open. It was not.

### Behavior preservation (D-15 / D-16)

| Check | Result |
|-------|--------|
| `git diff --stat origin/main...HEAD -- test/integration/` | **completely empty — zero edits** |
| Assertion changes in modified unit tests | only receiver renames (`s.` → `h.`); zero assertion semantics changed |
| `task test` | **exit 0** |
| `task test:int` | **exit 0** |
| `task lint` | **exit 0** |

D-15's contract — zero assertion edits anywhere under the integration tree — is
met literally, not approximately. The one changed assertion line in
`server_helpers_test.go` is `s.emitCommandResponse` → `h.emitCommandResponse`:
same call, same expectation, different receiver.

### D-19 / D-20 — review-gate surfaces

| Constraint | Result |
|-----------|--------|
| D-20 plugin-runtime symmetry chokepoint `event_emitter.go::Emit` | **untouched** — no gate moved, so no runtime asymmetry could be introduced |
| D-19 `internal/access/` (abac-reviewer trigger) | **untouched** |
| Gateway-boundary gate files (`gateway_imports_test.go`, `gateway_closure_test.go`) | **untouched**, and passing |

`cmd/holomush/sub_grpc.go` is the only caller change (4 insertions): it deletes
the authguard adapter and passes `pluginManager` directly. That is a
*simplification* of the caller, not a new wiring concept — D-04's facade-leak
test is satisfied.

### D-08 — `TestLoadPlugin`

Relocated to `internal/plugin/export_test.go:19`. Zero occurrences in any
production file, so it no longer ships in the production binary. The
`export_test.go` route was viable exactly as D-08 predicted once seam 2 removed
the sole out-of-package caller.

### Anti-Patterns Found

| Check | Result |
|-------|--------|
| New `TBD`/`FIXME`/`XXX` in phase diff | **zero** |
| New `TODO`/`HACK`/`PLACEHOLDER` in phase diff | **zero** (`git diff \| rg '^\+.*TODO...'` empty) |
| Stub/unimplemented returns in new units | **none** |

The two `TODO`s visible in touched files are both pre-existing on `origin/main`
(one carries issue ref `holomush-l60y`). No debt-marker blocker.

### Requirements Coverage

| Requirement | Description | Status | Evidence |
|-------------|-------------|--------|----------|
| ARCH-01 | `CoreServer` decomposed, no behavior change | ✓ SATISFIED | 4 units + thin facade; suites green; zero integration edits |
| ARCH-02 | `plugin/manager` decomposed, no behavior change | ✓ SATISFIED | 3 units + facade holding only `loader/runtime/identity/cfg` |

### Notes / Honest Caveats

These do not block the goal but should not be silently absorbed into a clean pass:

1. **Requirements ledger not updated.** `.planning/REQUIREMENTS.md:35-36` still
   shows ARCH-01/ARCH-02 as `[ ]`, and the tracking table (lines 95-96) reads
   `Pending`. The work is done; the ledger says otherwise. Normally closed at
   ship — worth doing before the PR lands so the roadmap does not misreport.

2. **The import-direction gate is production-only by design.** Five `_test.go`
   files still import `internal/grpc/focus`. This is deliberate and documented
   (`go/build`'s `Package.Imports` excludes test files, matching the
   `world_import_graph_test.go` precedent), and test fixtures crossing the
   boundary are legitimate. Naming it so a future reader does not mistake the
   gate for stronger than it is: it guards production edges only.

3. **"Behavior-preserving" is bounded by the existing suite's coverage.** D-16
   consciously declined up-front characterization tests on the grounds that the
   integration + whole-system suites already are the characterization layer.
   That was a ratified decision and the suites do pass unchanged with zero
   assertion edits — but the proof is exactly as strong as that suite, no
   stronger. This is an accepted cost, not a defect.

4. **Deferred, honestly recorded.** `deferred-items.md` names four free
   functions (`replayCompleteFrame`, `streamClosedFrame`,
   `subscribeSessionNotFound`, `filterSetToSlice`) still in `server.go` though
   used only by `subscribe_handler.go`, and a **pre-existing** data race in
   `internal/world/setup/relay_subsystem.go:294` reproduced on the pre-phase
   baseline and filed as **#4828**. Neither was caused by this phase; both are
   disclosed rather than buried.

### Gaps Summary

None. All three roadmap success criteria are met against the live codebase, and
the criterion-3 gate was mutation-tested rather than taken on the SUMMARY's
word. The phase's own contract (D-01 through D-20) holds on every checkable
point: no backpointers, byte-identical facades, no proto drift, both seams cut,
crypto fail-closed guards preserved through the adapter deletion, symmetry
chokepoint untouched, and zero integration-tree assertion edits.

The strongest evidence that this is a genuine decomposition rather than a file
split: the ratchet fails when I reintroduce each failure mode it claims to
prevent, and the extracted units construct and exercise from external test
packages with only their own collaborators.

---

_Verified: 2026-07-19T18:05:00Z_
_Verifier: Claude (gsd-verifier)_

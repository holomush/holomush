---
phase: 03-world-character-commands
fixed_at: 2026-08-09T00:00:00Z
review_path: .planning/phases/03-world-character-commands/03-REVIEW.md
iteration: 2
findings_in_scope: 24
fixed: 22
skipped: 2
status: partial
---

# Phase 03: Code Review Fix Report (cumulative, iterations 1–2)

**Fixed at:** 2026-08-09
**Source review:** `.planning/phases/03-world-character-commands/03-REVIEW.md` (iteration 2;
iteration 1's review was the same path at commit `0a580f797`)
**Iteration:** 2

**Cumulative summary (both passes):**

| Pass | In scope | Fixed | Skipped |
|---|---|---|---|
| Iteration 1 | 15 (0 critical, 9 warning, 6 info) | 13 | 2 |
| Iteration 2 | 9 (0 critical, 5 warning, 4 info) | 9 | 0 |
| **Total** | **24** | **22** | **2** |

Status is `partial` solely because the same two scope decisions remain deliberately unfixed. No
finding was skipped in iteration 2.

> **Finding IDs are per-iteration and DO NOT carry across passes.** Iteration 2's `WR-01`…`WR-05`
> and `IN-01`…`IN-04` are *different findings* from iteration 1's identically-numbered ones. Where
> an iteration-2 finding is a follow-up on an iteration-1 fix, the lineage is stated explicitly.

**Gates — all green, no hangs (shutdown-barrier code was edited in both passes):**

| Gate | Baseline (pre-iteration-1) | After iteration 1 | After iteration 2 |
|---|---|---|---|
| `task test` | 11357 ok | 11373 ok | **11378 ok, exit 0** (+5) |
| `task test:int` | 11827 ok | 11843 ok | **11848 ok, exit 0** (+5) |
| `task lint` | exit 0 | exit 0 | **exit 0** |

**Where verification ran:** the **main checkout of this worktree**
(`/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03`, branch `v013-phase-03`). The
orchestrator explicitly forbade `git worktree add`, so no isolated throwaway worktree was created
and no `node_modules` link was touched. These numbers are therefore reproducible from the tree
exactly as it stands.

**`internal/access/` was not touched in either pass.** The `abac-reviewer` READY verdict stands and
needs no re-run.

## What iteration 2 was about

Iteration 2 was not a fresh crop of unrelated defects. Five of its nine findings were the re-review
catching **iteration 1's own fixes not finishing the job** — two of them fixes that had *moved* a
defect rather than removed it, and two that had left the new guarantee **unfalsifiable**:

| Iteration 2 finding | Lineage |
|---|---|
| WR-01 (buffer write drops a newer timestamp) | iteration 1's WR-03 relocated the loss |
| WR-02 (revision-guard test never reaches the guard) | iteration 1's WR-03 regression test was a false green |
| WR-03 (retirement's fake models `Stop()` as a barrier) | iteration 1's WR-02 upgraded only one of two fakes |
| WR-04 (join and barrier share one budget) | introduced by iteration 1's WR-02 |
| IN-02 (`CreateBackoffs` still exported and mutable) | iteration 1's IN-05 removed the mutation, not the mutability |

Every behavioural fix in this pass was **verified RED** by reverting the production change and
confirming the test fails on the exact assertion, then restored and re-confirmed green. No temporary
markers remain in the tree.

## Fixed Issues — iteration 2

### WR-01: The revision-conditional buffer write drops a newer timestamp when the flusher deletes the key mid-write

**Files modified:** `internal/charactivity/listener.go`, `internal/charactivity/charactivity_test.go`
**Commit:** `1c1ec77b6`
**Applied fix:** Took the review's remedy — the compare-and-set is now a **bounded retry**
(`bufferAttempts = 3`) rather than a single pass. `buffer` was split into a loop plus a new
`tryBuffer` returning "settled", where *settled* means stored, deliberately skipped as not-newer, or
failed in a way no re-read can cure. A refused `Update` and an `ErrKeyExists` `Create` both return
**not settled** and re-read; a genuinely newer concurrent value settles on the next pass at the
monotonic early exit, so the loop converges rather than spinning.

The reviewer's diagnosis held on inspection: the losing interleaving is the flusher's
`DeleteRevision(key, N)` **succeeding** at the same revision `N` the listener read, after flushing
the *older* value — so the refusal meant "the key is gone", not "someone stored something fresher".
Re-reading lands on the `ErrKeyNotFound` branch where `Create` restores the newer value.

New regression test `TestListenerRebuffersItsValueWhenTheFlusherDeletedTheKeyItRead` drives exactly
that interleaving. **Verified RED** with the loop bound reduced to a single pass: the key is absent
afterwards and the newer timestamp is gone (`Should be true` on the survivor assertion).

### WR-02: The test named for the buffer's revision guard never reached it

**Files modified:** `internal/charactivity/charactivity_test.go`
**Commit:** `d04978963`
**Applied fix:** Replaced the fake's `beforeGet` seam with `afterGet`, which fires **after** the
lookup has captured its entry (and after the mutex is released, since the hook re-enters the fake).
The reviewer was right that `beforeGet` landed the interposed write *ahead* of the read, so the
listener saw the newer value and returned at the monotonic early exit — `Update` was never called
and the guard had zero coverage.

`fakeKV.Get` now captures the entry under the lock before invoking the hook, which also models a
real `Get` more faithfully: it returns a **snapshot** at its own revision, which is precisely what
the caller's later `Update` is conditioned on.

**Verified RED** by swapping `l.kv.Update(ctx, key, val, entry.Revision())` for an unconditional
`l.kv.Put(ctx, key, val)` — the test now fails on the interposed-value assertion, which is exactly
the falsification the review reported as missing.

### WR-03: The retirement package's `ConsumeContext` fake modelled `Stop()` as a barrier

**Files modified:** `internal/retirement/subsystem_test.go`
**Commit:** `ecc71cddc`
**Applied fix:** Ported charactivity's fake. `Stop()` no longer closes `closed` synchronously; it
spawns a goroutine that runs an optional `inFlight` hook and only then closes the channel — the
contract nats.go actually provides, and the one `Activate`'s join relies on.

`fakeJobs` gained a mutex (its `Unregister` is now reached from `Stop` while the in-flight hook may
be writing the same log) plus `mark`/`recorded`/`declared` accessors; existing direct field reads
were routed through them so the package stays `-race` clean.

New test `TestStopJoinsAnInFlightHandlerBeforeRetractingTheJobsLiveness` asserts the ordering
`register → handler-finished → unregister` — the guarantee `subsystem.go`'s doc block claims and
that nothing in the package could previously falsify. **Verified RED** by deleting the
`select { case <-cc.Closed(): … }` block from `Activate`: the log becomes
`[register, unregister]`, i.e. liveness is retracted while the fanout is still running.

### WR-04: The join budget and the barrier budget were one constant, started together

**Files modified:** `internal/charactivity/subsystem.go`, `internal/charactivity/charactivity_test.go`,
`internal/retirement/subsystem.go`, `internal/retirement/subsystem_test.go`
**Commit:** `61e2b276d`
**Applied fix:** Split the constant in **both** subsystems: `barrierTimeout = 5s` (the inner
`<-cc.Closed()` wait) and `stopTimeout = barrierTimeout + 1s` (the outer `<-s.done` join). The
reviewer's mechanism is right — `Stop` cancels `runCtx` and the goroutine's `<-runCtx.Done()`
returns immediately, so equal budgets expire together and the join can never observe the barrier
resolving.

Both timeout arms are now **observable**, replacing comment-only arms:

- charactivity inner: warns that a handler may still be in flight; outer: warns that the final
  drain is being skipped.
- retirement inner: warns that a fanout may still be in flight; outer: warns that job liveness is
  being retracted under a possible in-flight fanout. `Stop`'s signature changed from
  `Stop(_ context.Context)` to `Stop(ctx context.Context)` so the warn carries trace context per
  the repo's `*Context` slog rule.

`unregisterJob()` still runs on the retirement timeout path — a `Stop` that skipped it would leak
the declaration — but the state is no longer silent, and the outer wait now genuinely outlasts the
barrier, so reaching it is a real and rare condition rather than a coin flip.

Added `TestTheJoinBudgetStrictlyOutlastsTheBarrierBudget` to **both** packages as the mechanical
guard against re-collapsing the constants. **Verified RED** by setting
`stopTimeout = barrierTimeout` — both fail with `"5s" is not greater than "5s"`.

*Accepted cost, deliberately:* a wedged handler now adds up to 6 s (was 5 s) to shutdown per
subsystem.

### WR-05: `session_ended_payload.go` documented the subject in the eradicated colon form

**Files modified:** `internal/core/session_ended_payload.go`
**Commit:** `9bff6869f`
**Applied fix:** Doc-only. Replaced "the character's own stream (character:{ID})" with the dot form
`events.<game_id>.character.<id>`, naming the actual publish site
(`internal/presence/session_ended.go`, which emits the domain-relative `character.<id>` that
`eventbus.Qualify` prefixes), why the shape is load-bearing this phase (the retirement reactor's
consumer filter and its positional aggregate parse), and that the sole surviving colon usage is the
ABAC policy DSL's type prefix (`holomush-rops`).

### IN-01: The flush spec's synchronous no-write assertion is narrower but still racy

**Files modified:** `test/integration/charactivity/character_activity_flush_test.go`
**Commit:** `d43c4217d`
**Applied fix:** Took the review's first option — state the residual window honestly — rather than
deleting the assertion. The check is still worth keeping: a synchronous emit-path write would fail
it **deterministically**, which is the regression it exists to catch. The comment now says plainly
that it is not race-free, that the same column is written by the asynchronous pipeline
(publish-ack → listener delivery → KV write → an imminent tick), that the window is
microseconds-to-milliseconds rather than unbounded, and that the timing-independent structural proof
is the version/outbox pair asserted further down.

### IN-02: `consumer.CreateBackoffs` was still an exported mutable slice guarded by a length check

**Files modified:** `internal/eventbus/consumer/consumer.go`,
`internal/eventbus/consumer/consumer_test.go`, `internal/eventbus/audit/projection.go`,
`internal/eventbus/audit/projection_unit_test.go`
**Commit:** `ad25c74d3`
**Applied fix:** Took the stronger of the two suggested remedies. The slice is now unexported
(`defaultBackoffs`) with `DefaultBackoffs() []time.Duration` returning `slices.Clone(...)`, so no
package can reach the shared value at all — the previous doc line "nothing mutates it" was a
convention, not a constraint. Verified first that nothing outside the package assigned to or read it
(only two prose references in `audit/`, updated to the new name).

The guard was also strengthened from `require.Len(..., 2)` to a full contents assertion, since a
length check passes even when both durations are swapped — the exact mutation it was supposed to
catch. Added `TestDefaultBackoffsHandsBackACopySoACallerCannotMutateTheSharedSchedule`;
**verified RED** by returning the slice directly instead of a clone (mutating the returned slice
reaches the shared default: `99h0m0s` where `100ms` is required).

### IN-03: The abandonment alarm cannot distinguish "MaxDeliver reached" from "reached and then re-tuned"

**Files modified:** `internal/retirement/reactor.go`
**Commit:** `2f32c3f2f`
**Applied fix:** Took the review's second option — record the assumption — rather than plumbing the
effective value. Reading `cons.CachedInfo().Config.MaxDeliver` means widening `consumeStarter`
beyond the single `Consume` method it exists to keep narrow (and threading it through `Prepare`,
which builds the reactor *before* the consumer). Disproportionate for a diagnostic that is not a
control-flow gate and is only imprecise across a config **change**.

`isFinalDelivery`'s doc block now states the assumption, both failure modes (false alarm on a
message JetStream will redeliver; missed alarm on one it will not), the exact remedy, and why it was
deferred. It also records that the `maxDeliver <= 0` early return is what makes the `int → uint64`
conversion safe — which the reviewer independently confirmed.

### IN-04: `slog.SetDefault` is swapped globally inside a package unit test

**Files modified:** `internal/retirement/reactor_test.go`
**Commit:** `4cdae019d`
**Applied fix:** Took the review's first option. Injecting the logger would need a new
logger-**and**-context variant in `pkg/errutil` — the existing `errutil.LogError` takes a logger but
no `ctx`, which the repo's logging rule forbids wherever a `ctx` is in scope — so that is a
shared-package change out of proportion to an Info finding.

The swap now carries an explicit, greppable constraint marker naming what makes it safe
(sequential execution only), stating `no test in package retirement may call t.Parallel() while this
test exists`, and naming what would remove the constraint. Confirmed the constraint currently holds:
`rg -n 't\.Parallel\(\)' internal/retirement/` matches only this comment.

## Skipped Issues — unchanged from iteration 1

Both remain open scope decisions, skipped by explicit instruction in both passes. Neither was
re-raised as a finding in iteration 2's review.

### (iteration 1) IN-01: `RetireCharacter` and `UnretireCharacter` have no production caller

**File:** `internal/world/service.go:929, 1029`
**Reason:** Scope decision, not a defect. The admin surface lands in Phase 6 by design, and whether
shipping the commands unwired is acceptable is an **open question awaiting a human ruling in
`03-UAT.md` test 1**. No gRPC RPC, web handler, CLI command, or other caller was added in either
pass.

### (iteration 1) IN-02: No seed grants `retire`/`unretire` to a character's owner

**File:** `internal/access/policy/seed.go`
**Reason:** retire/unretire are **admin-only for v0.13** by an explicit user decision dated
2026-08-07 that superseded D-40's player-retires half. Adding an owner grant would violate a locked
decision, and would additionally force a re-run of the `abac-reviewer` gate.

## Fixed Issues — iteration 1 (carried forward)

Full detail is preserved in the commits; summarised here so this report covers both passes.

| ID | Title | Commit | Note |
|---|---|---|---|
| WR-01 | Reactor logged an ERROR "poison" line for every non-world event | `daa3a47c5` | header gate moved ahead of decode; verified RED |
| WR-02 | `Stop` did not join in-flight handlers in either new subsystem | `cb4d6262a` | real `Closed()` barrier; **iteration 2 WR-03/WR-04 finish this** |
| WR-03 | Listener could destroy a newer buffered timestamp with an older one | `961e13423` | monotonic + revision guard; **iteration 2 WR-01/WR-02 finish this** |
| WR-04 | `SetStatus`'s D-34 players write inverts the reaping guard's lock order | `8b1a7c2df` | **documented, not reordered** — downgrade; iteration 2's reviewer independently agreed |
| WR-05 | `outbox.AppSchemaVersion` doc asserted a non-existent wire property | `d8975d839` | doc-only |
| WR-06 | Flush spec raced the flush ticker it asserted against | `137bd1744` | **iteration 2 IN-01 refines the comment** |
| WR-07 | `aggregateFromSubject` read the last token, not the aggregate token | `0e9d32370` | positional parse; iteration 2 confirmed it denies rather than fails open |
| WR-08 | Exhausting `MaxDeliver` silently abandoned a fanout | `91144fdec` | in-handler alarm; **iteration 2 IN-03 records its one imprecision** |
| WR-09 | `charactivity.Stop` drained the DB even when `Activate` never ran | `24b2de17a` | activated/joined split; verified RED |
| IN-03 | Leave announcement used the session's location, not the character's | `11c5364bd` | |
| IN-04 | Tautological assertion in the retire-concurrency spec | `f56f1b7bb` | |
| IN-05 | Cross-package mutation of `consumer.CreateBackoffs` | `711fb9ee1` | **iteration 2 IN-02 removes the mutability too** |
| IN-06 | Unsynchronised field access in the KV test fake | `a1fcaa51d` | |

Iteration 1 also produced `0b3efacc2` (`style(03): apply task fmt`).

## Notes for the phase record

- **`internal/access/` untouched in both passes.** No `abac-reviewer` re-run required.
- **No `// Verifies:` annotation was added or changed**, and `docs/architecture/invariants.yaml` /
  `invariants.md` were not edited. None of these fixes proves a registry invariant; `inv-render`
  was not run because nothing it generates changed (`task lint:invariants` passes).
- **`RetireCharacter`/`UnretireCharacter` bodies in `internal/world/service.go` were not
  deduplicated** — their parallelism is load-bearing for the two AST census meta-tests.
- **`.planning/phases/03-world-character-commands/03-REVIEW.md` was reflowed by `task fmt` during
  this pass and immediately restored** with `git checkout --`. It is a tool-owned planning artifact
  and carries no fix-pass edits.
- **Iteration 2 added 5 unit tests** (11373 → 11378). Four are falsification guards for fixes whose
  guarantees were previously unprovable; the fifth is the WR-01 regression.
- **Behavioural changes to production code in this pass:** the listener's bounded retry
  (`internal/charactivity/listener.go`), the split timeout constants and new warn lines in both
  subsystems, and `retirement.Subsystem.Stop`'s parameter name. Everything else was test-fidelity
  or documentation.

---

_Fixed: 2026-08-09_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 2_

---
phase: 03-world-character-commands
fixed_at: 2026-08-09T00:00:00Z
review_path: .planning/phases/03-world-character-commands/03-REVIEW.md
iteration: 3
findings_in_scope: 28
fixed: 26
skipped: 2
status: partial
---

# Phase 03: Code Review Fix Report (cumulative, iterations 1–3)

**Fixed at:** 2026-08-09
**Source review:** `.planning/phases/03-world-character-commands/03-REVIEW.md` (iteration 3;
earlier passes' reviews are preserved at `03-REVIEW.iter2.md` / `03-REVIEW.iter3.md`)
**Iteration:** 3 — **final pass; the loop ends here.**

**Cumulative summary (all three passes):**

| Pass | In scope | Fixed | Skipped |
|---|---|---|---|
| Iteration 1 | 15 (0 critical, 9 warning, 6 info) | 13 | 2 |
| Iteration 2 | 9 (0 critical, 5 warning, 4 info) | 9 | 0 |
| Iteration 3 | 4 (0 critical, 3 warning, 1 info) | 4 | 0 |
| **Total** | **28** | **26** | **2** |

Status is `partial` solely because the same two scope decisions remain deliberately unfixed, exactly
as in iterations 1 and 2. **No finding was skipped in iteration 3**, and nothing is being handed to
the user as "could not fix".

> **Finding IDs are per-iteration and DO NOT carry across passes.** Iteration 3's `WR-01`…`WR-03`
> and `IN-01` are *different findings* from the identically-numbered ones in earlier passes. Lineage
> is stated explicitly wherever a finding follows up on an earlier fix.

**Gates — all green, no hangs (shutdown-barrier code was edited in all three passes):**

| Gate | Baseline | After iter 1 | After iter 2 | After iter 3 |
|---|---|---|---|---|
| `task test` | 11357 ok | 11373 ok | 11378 ok | **11384 ok, exit 0** (+6) |
| `task test:int` | 11827 ok | 11843 ok | 11848 ok | **11854 ok, exit 0** (+6) |
| `task lint` | exit 0 | exit 0 | exit 0 | **exit 0** |

Integration ran in 171 s with no hang, which matters this pass specifically: the barrier code was
restructured in both subsystems.

**Where verification ran:** the **main checkout of this worktree**
(`/Volumes/Code/github.com/holomush/.worktrees/v013-phase-03`, branch `v013-phase-03`). The
orchestrator forbade `git worktree add`, so no isolated throwaway worktree was created and no
`node_modules` link was touched. These numbers are reproducible from the tree exactly as it stands.

**`internal/access/` was not touched in any pass.** The `abac-reviewer` READY verdict stands.
`docs/architecture/invariants.yaml` *was* edited this pass, which by the orchestrator's own note
does not invalidate that verdict.

---

## The decision this pass was convened to make

The orchestrator's instruction was explicit: iteration 2's `barrierTimeout` (5 s) / `stopTimeout`
(6 s) split made the outer join deterministic **by making it dead**, and I was to fix it *not* by
re-tuning the two constants against each other, but by making the code and its documented guarantee
agree — "either the arm is genuinely reachable and says what happened, or the doc stops claiming a
barrier it does not provide."

**I chose the first option in both subsystems, with one correction to how the choice is framed.**

The honest reading is that these are not two options but a false dichotomy applied to the wrong
variable. Re-tuning constants cannot make the outer arm reachable, because the arm's unreachability
is **structural, not numerical**: the shutdown goroutine's only blocking wait *is* the barrier, so
`done` closes within `barrierTimeout + ε` on every path no matter what the two numbers are. Making
the arm "reachable" by shrinking `stopTimeout` below `barrierTimeout` would only recreate the
coin-flip that iteration 2 was fixing. The constants are innocent.

What was actually wrong is that **both subsystems inferred the barrier's outcome from a signal that
does not carry it.** `s.done` records *goroutine exit*, and the goroutine returns on **both** of its
arms. So "the goroutine exited" was being read as "the handler was joined" — two different facts.

So the fix is to **propagate the barrier's outcome instead of inferring it**: a `barrierOK` channel,
closed only on the arm where `cc.Closed()` actually resolved. Every consumer of the barrier's result
now reads the fact it needs:

- **retirement** reports the liveness retraction off `barrierOK` — a genuinely reachable place that
  says exactly what happened;
- **charactivity** gates the final drain on `barrierOK`, so the doc's R2b claim becomes true;
- the outer `stopTimeout` arm survives in both, **demoted in its own comment** to the residual case
  it can still catch (a goroutine that never scheduled), and no longer carrying the report for the
  common case.

That satisfies both halves of the instruction at once: the reporting arm is reachable *and* says
what happened, and the docs now state guarantees the code holds. It is also the fix the review
itself proposed, which I adopted after confirming its mechanism against the source rather than on
the review's authority.

This is the third pass in a row to find that the *previous* pass moved a defect rather than removing
it. The pattern in all three cases was the same: **a guarantee asserted about a code path no test
reached.** Accordingly, every behavioural fix in this pass was proved by reaching the path — see the
RED evidence under each finding, and the `barrier` test seam that makes a 5 s production budget
falsifiable in a millisecond-scale unit test.

---

## Fixed Issues — iteration 3

### WR-01: Retirement's outer-timeout arm was unreachable and carried the only liveness-retraction log line

**Files modified:** `internal/retirement/subsystem.go`, `internal/retirement/subsystem_test.go`
**Commit:** `93e27ff62`

**Applied fix:** Added `Subsystem.barrierOK`, closed by the shutdown goroutine **only** on the
`<-cc.Closed()` arm. `Stop` now reports the retraction from a non-blocking read of that channel
after the join, rather than from the outer timeout arm. The outer arm keeps a warn, but its message
and comment were rewritten to the residual condition it can actually catch ("shutdown goroutine
never exited within its budget"), and `stopTimeout`'s doc block now states plainly that the arm is
nearly unreachable by construction and MUST NOT carry the wedged-fanout report.

The review's diagnosis held exactly on inspection, and the RED run captured it verbatim: with the
`barrierOK` check removed, the only output under a wedged fanout is

```text
WARN retirement reactor barrier timed out; a fanout may still be in flight
```

— the join succeeds, and `unregisterJob()` retracts the job's ABAC liveness one line later in total
silence. That is the half-applied-fanout precondition the barrier exists to make visible.

**Verified RED** by deleting the `barrierOK` select from `Stop`:
`TestStopReportsTheLivenessRetractionWhenTheBarrierWasAbandoned` fails on its exact assertion
(`does not contain "retirement reactor retracting job liveness under a possible in-flight fanout"`).
Restored, re-confirmed green (61 tests).

A second test, `TestStopStaysSilentAboutLivenessWhenTheBarrierResolved`, pins the other side so the
new warn cannot degrade into noise on the ordinary path.

**On the test seam:** the abandoned-barrier path was previously unreachable in a unit test at any
acceptable runtime, which is *why* it went unverified for two passes. An unexported
`Subsystem.barrier` field (zero ⇒ the 5 s production constant) lets a package test shorten it to
10 ms. This follows the file's existing `createConsumer` seam convention and leaves `Config` — the
public surface — untouched.

### WR-02: `charactivity.Stop` treated a barrier-abandoned shutdown as "joined" and drained anyway

**Files modified:** `internal/charactivity/subsystem.go`, `internal/charactivity/charactivity_test.go`
**Commit:** `2ab1c8710`

**Applied fix:** Same `barrierOK` mechanism, wired to the **drain gate** rather than to a log line.
`Stop` now distinguishes three facts instead of two — `activated`, `exited` (the old `joined`,
renamed to what it actually records), and `joined` (from `barrierOK`) — and the drain requires all
three. The reviewer's point that this was iteration 1's WR-09 `activated`-vs-`joined` conflation
*reappearing one level down* is correct, and the rename makes the distinction legible at the call
site rather than only in a comment.

The doc block was rewritten to state the guarantee that actually holds: the final drain runs only
when both producers were observably joined, and a barrier timeout **skips** it. The conditional is
now presented as the guarantee itself rather than as a caveat — `Stop` cannot promise an
unconditional barrier, because the barrier is bounded by design, and the honest claim is that the
drain never runs while a handler may still be live.

This also resolves the two arms' disagreement the review flagged: the outer arm's "skipping the
final drain is the safe answer" is now what the reachable inner path does too.

**Verified RED** by reverting the gate to `activated && exited && kv != nil`:
`TestStopSkipsTheFinalDrainWhenTheBarrierWasAbandoned` fails on both assertions (a write is
recorded, and the buffer is emptied under a live handler) while
`TestStopJoinsBothProducersBeforeTheFinalDrain` — the resolving-arm test — stays green. That split
is the precise evidence the old test could not reach this path.

**Also fixed (the review's separate note):** `stopTimeout` was being reused as the *drain* budget.
It now has its own `drainTimeout`, so the join's deadline and the drain's are no longer coupled by
accident.

### WR-03: `INV-ACCESS-14`'s registry rationale was falsified by this phase's own diff

**Files modified:** `docs/architecture/invariants.yaml`, `docs/architecture/invariants.md`
(regenerated), `internal/retirement/reactor_test.go`
**Commit:** `d3c02b70c`

**This is the finding the orchestrator flagged for care, so the reasoning is given in full.**

I verified the review's claim before acting: the wrapper landed
(`internal/eventbus/consumer/consumer.go`), the handler boundary landed (`reactor.go::newDelivery`),
and `rg "Verifies: INV-ACCESS-14"` returned nothing. The rationale claiming "no message, no consumer
wrapper and no handler boundary exists in-tree" was false in every clause, and named Phase 3 as the
owner of the fix.

**But the tests the review pointed at did not, on their own, justify a binding — and I did not bind
them as-is.** The invariant's third clause is "*the handler cannot alter it*". The two candidate
tests each prove a half:

- `TestNewDeliveryDerives…` proves the stamp is read off the delivered message;
- `TestProcessAuthorizes…` proves `process` passes provenance through unaltered.

They **do not compose**. `process`'s delivery in that test is *hand-built* (`f.delivery()`), never
produced by `newDelivery`. So a `handleDecoded` that re-derived or overwrote the provenance between
the stamp and its use would leave both green — which is exactly the clause in question, undetected.

I confirmed this empirically rather than by argument: injecting `d.Aggregate = d.Character.String()
+ "X"` into `handleDecoded` **kept both existing tests passing**. Annotating either with
`// Verifies: INV-ACCESS-14` would therefore have been precisely the partial-binding false-green the
registry meta-tests exist to catch — the INV-RB-3 / INV-PRIVACY-7 failure mode named in
`.claude/rules/invariants.md`, and one the meta-test explicitly *cannot* detect (it catches
Skip-only placeholders, not partial bindings).

So I took the third path, which the review allowed for and which is better than either of its two:
**close the gap, then bind honestly.** The new
`TestHandleDecodedAuthorizesUnderProvenanceStampedFromTheDeliveredMessage` spans the whole boundary
— wire bytes in, the `world.Caller` the world calls authorize under out — and goes **RED** on that
same injected mutation while both pre-existing tests stay green. That is a real falsification of
"the handler cannot alter it", not an argument from construction.

Only then did I set `binding: bound` + `asserted_by: internal/retirement/reactor_test.go`, rewrite
the rationale to describe the tree as it now is (including *why* spanning the boundary is what makes
the third clause falsifiable), and regenerate with `go run ./cmd/inv-render` — never hand-editing
`invariants.md`. I also corrected the two now-stale cross-references: the section comment above
`INV-ACCESS-13`, and the sentence inside `INV-ACCESS-13`'s own summary that still said INV-ACCESS-14
was "pending, for Phase 3 to bind".

**Confirmed** with the three registry meta-tests named in the instruction
(`TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted`,
7 tests green), the full `./test/meta/` suite (171 tests green), and `inv-render -check`.

### IN-01: A non-conflict failure on the buffer's `Update` path was never logged above Debug

**Files modified:** `internal/charactivity/listener.go`, `internal/charactivity/charactivity_test.go`
**Commit:** `f4df8a5ed` (+ `b553c8e16` for the wrapcheck follow-up)

**Applied fix:** `tryBuffer` now returns `(settled bool, err error)`; `buffer` carries the last
error out of the bounded loop and escalates to `logBufferFailure` when it is non-nil, falling
through to the Debug contention line only when it is not. This is the review's suggested shape.

The review's `(false, nil)` for a revision refusal required a discriminator it did not specify, so I
established one from the source: nats.go implements `Update` as a publish with
`WithExpectLastSequencePerSubject` (`jetstream/kv.go:1093`), so a revision conflict returns an
`*APIError` carrying code `10071` (wrong last sequence), and `APIError.Is` compares on `ErrorCode`
(`jetstream/errors.go:498-509`) — making `errors.Is(err, jetstream.ErrKeyExists)` the correct and
stable test.

**This exposed a test-fidelity defect that would have inverted the fix.** `fakeKV.Update` returned a
bare `errors.New("wrong last sequence")` for a revision conflict — an error the new discriminator
classifies as an *outage*. Left alone, every ordinary conflict would have escalated to ERROR, and
the escalation test would have passed against a listener that blamed infrastructure for everything.
Corrected the fake to return `jetstream.ErrKeyExists` (what nats.go actually returns) and added an
`alwaysConflict` seam so both exits are independently provable — the same class of fake-fidelity
correction iteration 2 made for `fakeConsumeContext`.

**Verified RED** by restoring the swallow (`return false, nil` on every `Update` error):
`TestListenerReportsAnInfrastructureFailureOnTheUpdatePathRatherThanBlamingContention` fails with
the outage reported as `DEBUG … gave up buffering under contention`, which is precisely the finding.
`TestListenerStillTreatsAnExhaustedRevisionRaceAsContention` guards the opposite regression.

**Follow-up in `b553c8e16`:** `task lint` flagged the carried error under `wrapcheck`. Fixed
properly with `oops.Wrap` at the return boundary — **not** a `//nolint`, per the repo rule — wrapped
bare because `logBufferFailure` applies `CHARACTER_ACTIVITY_BUFFER_FAILED` at the reporting site and
coding it twice would bury the diagnostic. The same commit carries yamlfmt's reflow of the registry
block; `inv-render -check` confirms `invariants.md` stayed in sync.

---

## Skipped Issues — unchanged across all three passes

Both remain open scope decisions, skipped by explicit instruction in every pass. Neither was
re-raised as a finding in iteration 3's review.

### (iteration 1) IN-01: `RetireCharacter` and `UnretireCharacter` have no production caller

**File:** `internal/world/service.go:929, 1029`
**Reason:** Scope decision, not a defect. The admin surface lands in Phase 6 by design, and whether
shipping the commands unwired is acceptable is an **open question awaiting a human ruling in
`03-UAT.md` test 1**. No gRPC RPC, web handler, CLI command, or other caller was added in any pass.

### (iteration 1) IN-02: No seed grants `retire`/`unretire` to a character's owner

**File:** `internal/access/policy/seed.go`
**Reason:** retire/unretire are **admin-only for v0.13** by an explicit user decision dated
2026-08-07 that superseded D-40's player-retires half. Adding an owner grant would violate a locked
decision and would force a re-run of the `abac-reviewer` gate.

---

## Fixed Issues — iterations 1 and 2 (carried forward)

Full detail is in the commits and in the iteration-2 report preserved at `03-REVIEW-FIX.iter3.md`.

### Iteration 2

| ID | Title | Commit | Note |
|---|---|---|---|
| WR-01 | Buffer write drops a newer timestamp when the flusher deletes the key | `1c1ec77b6` | bounded retry; iteration 3 confirmed it converges |
| WR-02 | Revision-guard test never reached the guard | `d04978963` | `afterGet` seam; iteration 3 confirmed it interposes |
| WR-03 | Retirement's fake modelled `Stop()` as a barrier | `ecc71cddc` | iteration 3 confirmed it turns RED without the barrier |
| WR-04 | Join and barrier shared one budget | `61e2b276d` | **iteration 3 WR-01/WR-02 finish this** |
| WR-05 | `session_ended` subject documented in the eradicated colon form | `9bff6869f` | doc-only |
| IN-01 | Flush spec's no-write assertion still racy | `d43c4217d` | comment states the residual window |
| IN-02 | `CreateBackoffs` exported and mutable | `ad25c74d3` | unexported + copying accessor |
| IN-03 | Abandonment alarm's configured-vs-effective `MaxDeliver` | `2f32c3f2f` | assumption recorded |
| IN-04 | `slog.SetDefault` swapped globally in a unit test | `4cdae019d` | constraint marked |

### Iteration 1

| ID | Title | Commit | Note |
|---|---|---|---|
| WR-01 | ERROR "poison" line for every non-world event | `daa3a47c5` | header gate before decode |
| WR-02 | `Stop` did not join in-flight handlers | `cb4d6262a` | **iter 2 WR-03/WR-04 + iter 3 WR-01/WR-02 finish this** |
| WR-03 | Listener could destroy a newer buffered timestamp | `961e13423` | **iter 2 WR-01/WR-02 finish this** |
| WR-04 | `SetStatus`'s D-34 players write inverts the lock order | `8b1a7c2df` | documented, not reordered |
| WR-05 | `outbox.AppSchemaVersion` doc asserted a non-existent property | `d8975d839` | doc-only |
| WR-06 | Flush spec raced the ticker it asserted against | `137bd1744` | **iter 2 IN-01 refines** |
| WR-07 | `aggregateFromSubject` read the last token | `0e9d32370` | positional parse |
| WR-08 | Exhausting `MaxDeliver` silently abandoned a fanout | `91144fdec` | **iter 2 IN-03 records its imprecision** |
| WR-09 | `charactivity.Stop` drained even when `Activate` never ran | `24b2de17a` | **iter 3 WR-02 finishes this one level down** |
| IN-03 | Leave used the session's location, not the character's | `11c5364bd` | |
| IN-04 | Tautological assertion in the retire-concurrency spec | `f56f1b7bb` | |
| IN-05 | Cross-package mutation of `consumer.CreateBackoffs` | `711fb9ee1` | **iter 2 IN-02 removes mutability** |
| IN-06 | Unsynchronised field access in the KV test fake | `a1fcaa51d` | |

Iteration 1 also produced `0b3efacc2` (`style(03): apply task fmt`).

---

## Notes for the phase record

- **`internal/access/` untouched in all three passes.** No `abac-reviewer` re-run required.
  `docs/architecture/invariants.yaml` was edited this pass, which does not invalidate that verdict.
- **One `// Verifies:` annotation was added** — `INV-ACCESS-14`, on a test written specifically to
  make its third clause falsifiable, and only after empirically confirming the pre-existing tests
  did **not** cover it. `invariants.md` was regenerated with `cmd/inv-render`, never hand-edited.
- **`RetireCharacter`/`UnretireCharacter` bodies were not deduplicated** — their parallelism is
  load-bearing for the two AST census meta-tests.
- **`03-REVIEW.md` was reflowed by `task lint` again this pass and immediately restored** with
  `git checkout --`, exactly as in iteration 2. It is a tool-owned artifact and carries no fix-pass
  edits. The untracked `.gsd/` directory and the `*.iter2.md` / `*.iter3.md` backups were left alone.
- **Iteration 3 added 6 unit tests** (11378 → 11384). Five reach paths no test previously reached;
  the sixth is the counter-regression guard for the new escalation.
- **New test seam:** an unexported `barrier` field on both subsystems (zero ⇒ the production
  constant). This is what makes a 5 s barrier budget falsifiable in a unit test, and is the reason
  these two arms went unverified for two passes. `Config` — the public surface — is unchanged.
- **Behavioural changes to production code in this pass:** the `barrierOK` signal and its consumers
  in both subsystems (`internal/retirement/subsystem.go`,
  `internal/charactivity/subsystem.go`), the separate `drainTimeout`, and the listener's error
  escalation (`internal/charactivity/listener.go`). Everything else was tests, docs, or the
  registry.
- **No temporary markers remain.** Every RED proof was performed by mutating production code, then
  restoring from a scratchpad copy and re-confirming green; `rg 'TEMP RED PROOF'` is clean and
  `git diff` against the restored files was empty each time.

---

_Fixed: 2026-08-09_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 3 (final)_

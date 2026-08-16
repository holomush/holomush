---
phase: 03-world-character-commands
fixed_at: 2026-08-09T00:00:00Z
review_path: .planning/phases/03-world-character-commands/03-REVIEW.md
iteration: 1
findings_in_scope: 15
fixed: 13
skipped: 2
status: partial
---

# Phase 03: Code Review Fix Report

**Fixed at:** 2026-08-09
**Source review:** `.planning/phases/03-world-character-commands/03-REVIEW.md`
**Iteration:** 1

**Summary:**
- Findings in scope: 15 (0 critical, 9 warning, 6 info — `fix_scope: all`)
- Fixed: 13
- Skipped: 2 (both scope decisions, skipped by instruction)

**Gates (all green, run in the main checkout of this worktree — not a throwaway env, so these numbers are reproducible from the tree as it stands):**

| Gate | Baseline | After |
|---|---|---|
| `task test` | 11357 ok | **11373 ok, exit 0** (+16 new tests) |
| `task test:int` | 11827 ok | **11843 ok, exit 0** |
| `task lint` | exit 0 | **exit 0** |

`task fmt` mutated `internal/retirement/reactor.go` (one call reflowed); committed as `style(03)`.

**No file under `internal/access/` was touched.** The `abac-reviewer` gate's READY verdict on the
current state stands and does not need re-running for these fixes.

## Two findings that need a human ruling

1. **WR-04 was downgraded, not implemented as written.** The review calls the retire/genesis lock
   order "a textbook deadlock pair". I could not establish that the cycle closes. See the WR-04
   entry below for the analysis; if you disagree, the fix to apply is in the code comment.
2. **WR-02 changed production shutdown ordering** in both new subsystems. It is the one fix here
   that alters runtime behaviour on a path integration tests exercise only incidentally.

## Fixed Issues

### WR-01: Retirement reactor logged an ERROR "poison" line for every non-world event

**Files modified:** `internal/retirement/reactor.go`, `internal/retirement/reactor_test.go`
**Commit:** `daa3a47c5`
**Applied fix:** Moved the event-type gate ahead of the body decode in `handleDecoded`. Confirmed
the reviewer's premise first: `internal/presence/session_ended.go:67` publishes on
`character.<id>`, which `eventbus.Qualify` turns into `events.<game>.character.<id>` — squarely
inside the `events.*.character.>` filter. `process`'s own type check is retained as defence in
depth for direct callers. Regression test asserts **zero ERROR records** for a `session_ended`
delivery whose body could never decode; **verified RED** against the old ordering (1 error logged).

### WR-02: `Stop` did not join in-flight handlers in either new subsystem

**Files modified:** `internal/charactivity/subsystem.go`, `internal/charactivity/charactivity_test.go`,
`internal/retirement/subsystem.go`, `internal/retirement/reactor.go`
**Commit:** `cb4d6262a`
**Applied fix:** Took remedy **(a)** — a real barrier — rather than (b) rewording the claim, after
verifying in nats.go v1.52.0 that one exists. `Stop()` only does
`closed.CompareAndSwap(0,1)` + `close(s.done)` (`jetstream/pull.go:768`), but `Closed()` is
genuine: `closedCh` is closed from the subscription's closed handler, which nats.go invokes at the
**end** of `waitForMsgs` (`nats.go:3656`), after the delivery loop has exited — so once it fires no
handler is running. Both Activate goroutines now wait on it, bounded by `stopTimeout`, before
closing `done`. This makes charactivity's R2b comment true as written and makes retirement's
`unregisterJob()` genuinely follow the last handler.

Also applied the suggested `classifyWorldError` guard as belt-and-braces: a deny observed while the
lifecycle ctx is already cancelled is redelivered rather than acked, since during shutdown a deny
may mean "the job lost its attributes" rather than "the seed refuses this write".

The charactivity fake returned a **nil** `Closed()` channel and could not falsify any of this. It
now models the real contract (Stop returns while a handler is still running; `Closed()` signals only
once it returns), and `TestStopJoinsBothProducersBeforeTheFinalDrain` interposes an in-flight `Put`
whose value the final drain must observe. **Verified RED** without the barrier — the drain flushes
the stale `1000` instead of `2000`.

### WR-03: The activity listener could destroy a newer buffered timestamp with an older one

**Files modified:** `internal/charactivity/listener.go`, `internal/charactivity/flusher.go`,
`internal/charactivity/charactivity_test.go`
**Commit:** `961e13423`
**Applied fix:** `record` now does a guarded read-compare-write: `Create` when absent, return when
the buffered value is already `>=` ours, else `Update` at the revision read — mirroring the
flusher's `deleteAtRevision`. `Create`/`Update` added to `activityKV` and the `jsKV` adapter; the
fake models both (`ErrKeyExists`, wrong-last-sequence) and gains a `beforeGet` seam. An unparsable
buffered value falls through and is cured, matching the flusher's treatment of the same value.
Four regression tests; **the monotonic guard and the revision guard were each verified RED
independently.**

### WR-04: `SetStatus`'s D-34 players write inverts the reaping guard's lock order

**Files modified:** `internal/world/postgres/character_repo.go`, `test/meta/world_sql_fence_test.go`
**Commit:** `8b1a7c2df`
**Applied fix:** **Documented, not reordered — this is a deliberate downgrade of the finding, and
the one item here most in need of your ruling.**

The inversion is real and was undocumented: retire takes `characters` → `players`;
`PlayerReapingGuard.EnsureNotReaping` takes `players` → `characters`. But I could not establish the
"textbook deadlock pair". A deadlock needs each transaction waiting on a lock the other holds, and
the genesis side never waits on anything the retire holds:

- Genesis **INSERTs a new** `characters` row (`internal/auth/character_genesis.go:189-192`) rather
  than locking an existing one, so the retire's row lock on the retiring character is never
  contended.
- The `characters.player_id` FK check takes only `FOR KEY SHARE` on the `players` row genesis
  already holds `FOR UPDATE` — compatible with itself.

So a concurrent retire merely **waits** on genesis's `players` lock and proceeds when genesis
commits. I also declined the suggested pre-emptive `SELECT 1 FROM players WHERE
default_character_id = $1 FOR UPDATE`: there is no index on `default_character_id`
(`migrations/000001_baseline.sql:64`), so it would seq-scan `players` on **every** retire to defend
against a cycle that cannot currently form. Reordering the players clear *ahead* of the CAS was also
declined — it would contradict the "the clear is behind the CAS" property the same review verified
as correct.

The `SetStatus` doc block now states the inversion, why it is latent, and the **exact** change that
would make it a live 40P01 (genesis acquiring a lock on an *existing* `characters` row), with the
fix to apply at that point. The fence commentary cross-references it.

**If you judge the deadlock real, the code comment names the fix.** No retry-on-40P01 was added,
since it would be speculative machinery for a cycle I could not construct.

### WR-05: `outbox.AppSchemaVersion`'s doc asserted a wire property that does not exist

**Files modified:** `internal/world/outbox/taxonomy.go`
**Commit:** `d8975d839`
**Applied fix:** Doc-only, as instructed — no wire behaviour and no relay-stamped header changed.
Verified the premise: `rg -n AppSchemaVersion --type go` returns only its own declaration, its own
test, and one prose mention; the `App-Schema-Version` header is stamped from
`eventbus.SchemaVersion` (`publisher.go:62,304`). The comment now states it is a build-time marker,
that it is on no envelope/row/header, and where the header actually comes from.

### WR-06: The activity flush spec raced the flush ticker it was asserting against

**Files modified:** `test/integration/charactivity/character_activity_flush_test.go`
**Commit:** `137bd1744`
**Applied fix:** Took the review's second option (no production seam added). The no-database-write
claim is now a **synchronous** check immediately after `emit()` returns — publish has returned, so
anything the emit path would write is already written. The buffered-value assertion reads through to
the column when the key has already been drained (`bufferedOrFlushed`), keeping the assertion about
the *value* rather than about when the tick lands: the old form failed **permanently** once a tick
drained the key, because the key never comes back and every subsequent poll returned
`ErrKeyNotFound`. The `flushInterval` comment's false claim about not racing the first tick is
replaced with the reason no assertion may depend on tick phase.

### WR-07: `aggregateFromSubject` read the last token, not the aggregate token

**Files modified:** `internal/retirement/reactor.go`, `internal/retirement/reactor_test.go`
**Commit:** `0e9d32370`
**Applied fix:** Positional parse; any shape other than the four-token
`events.<game>.character.<ulid>` returns `""`, which denies (correctly) instead of authorizing
against a facet name. Table test covers the canonical subject plus five rejected shapes.

### WR-08: Exhausting `MaxDeliver` silently abandoned a retirement fanout

**Files modified:** `internal/retirement/reactor.go`, `internal/retirement/reactor_test.go`
**Commit:** `91144fdec`
**Applied fix:** Chose the in-handler alarm over an advisory subscription.
`jetstream.ConsumeErrHandler` is **not** a seam for this — it reports consume-loop errors, not
delivery exhaustion — and the handler is the one place that knows both the delivery count and
*which* character was abandoned. On the final unacked delivery it now logs at ERROR with code
`RETIREMENT_FANOUT_ABANDONED`, carrying subject, event id, and the delivery counts. The trigger is a
pure `isFinalDelivery(numDelivered, maxDeliver)` with a table test, including the unlimited
(`maxDeliver <= 0`) case.

### WR-09: `charactivity.Stop` performed a database-writing drain even when `Activate` never ran

**Files modified:** `internal/charactivity/subsystem.go`, `internal/charactivity/charactivity_test.go`
**Commit:** `24b2de17a`
**Applied fix:** Split `activated` from `joined` exactly as suggested. Regression test drives the
orchestrator's rollback shape (Prepare, then Stop with no Activate); **verified RED** against the
old `joined := s.done == nil` — it wrote to `characters` and emptied the durable buffer.

### IN-03: The leave announcement used the session's location, not the character's

**Files modified:** `internal/retirement/reactor.go`, `internal/retirement/reactor_test.go`
**Commit:** `11c5364bd`
**Applied fix:** Prefers `char.LocationID`, falling back to `info.LocationID` for a character with
no location set. The error log for a failed emit now reports the same location the ref carries.
Renamed `TestProcessEmitsTheLeaveAtTheDeletedSessionsLocation`, whose *name* asserted the behaviour
being corrected, and added divergence and fallback tests.

### IN-04: Tautological assertion in the retire-concurrency spec

**Files modified:** `test/integration/resilience/retire_concurrency_test.go`
**Commit:** `f56f1b7bb`
**Applied fix:** Removed the `NotTo(Equal(codeAlreadyRetired))` check and relocated the guard-order
signal to where it actually lives — the mid-state read establishing the row is **already retired**
when B calls, so the lifecycle guard is live and would have rejected B differently had it run first.

Note: this suite is quarantined nightly/opt-in (#4791) and is skipped by the default `task test:int`.
Run under `HOLOMUSH_RUN_QUARANTINED=1` the retire-concurrency spec **passes** (its `M12-VERDICT` line
is emitted). The 4 failures in that opt-in run are broker-blip / outbox-relay fault-injection specs
in other files (`nats: connection reconnecting`), unrelated to this change.

### IN-05: Cross-package mutation of `consumer.CreateBackoffs`

**Files modified:** `internal/eventbus/consumer/consumer.go`,
`internal/eventbus/consumer/consumer_test.go`, `internal/eventbus/audit/projection_unit_test.go`
**Commit:** `711fb9ee1`
**Applied fix:** Took the functional-option remedy rather than restating the constraint, since both
test packages call `CreateWithRetry` directly. `CreateWithRetry` now takes variadic `CreateOption`s
with `WithBackoffs`; both packages pass a per-call schedule and **nothing mutates shared state**, so
the "MUST NOT call `t.Parallel()`" constraint is gone rather than duplicated. Existing two-arg
production call sites are unchanged. The give-up test additionally asserts the shared default is
untouched by an override.

### IN-06: Unsynchronised field access in the KV test fake

**Files modified:** `internal/charactivity/charactivity_test.go`
**Commit:** `a1fcaa51d`
**Applied fix:** Both hooks (`beforeDelete` and the new `beforeGet`) are captured under `k.mu` and
invoked **after** releasing it — the hook re-enters the fake, so holding the mutex across the call
would deadlock.

## Skipped Issues

### IN-01: `RetireCharacter` and `UnretireCharacter` have no production caller

**File:** `internal/world/service.go:929, 1029`
**Reason:** Skipped by instruction — scope decision, not a defect. The admin surface lands in Phase 6
by design, and whether shipping the commands unwired is acceptable is an **open question awaiting a
human ruling in `03-UAT.md` test 1**. No gRPC RPC, web handler, CLI command, or other caller was
added.
**Original issue:** Neither command is reachable from any RPC, command dispatcher, or CLI in this
phase, so INV-WORLD-7's "every v0.13 character mutation RPC carries expected_version" has no boundary
to be enforced at yet, and the retire path is exercised only by tests.

### IN-02: No seed grants `retire`/`unretire` to a character's owner

**File:** `internal/access/policy/seed.go`
**Reason:** Skipped by instruction — retire/unretire are **admin-only for v0.13** by an explicit user
decision dated 2026-08-07 that superseded D-40's player-retires half. Adding an owner grant would
violate a locked decision. Also avoids touching `internal/access/`, which would force a re-run of the
`abac-reviewer` gate.
**Original issue:** `Service.RetireCharacter` checks action `"retire"`; the only seed that can match
is the blanket admin permit, so D-39's stated rationale has no policy expressing it.

## Notes for the phase record

- **`internal/access/` was not touched.** No `abac-reviewer` re-run required.
- **No `// Verifies:` annotation was added or changed.** None of these fixes proves a registry
  invariant, and `docs/architecture/invariants.md` was not edited.
- **`RetireCharacter`/`UnretireCharacter` bodies in `internal/world/service.go` were not
  deduplicated** — their parallelism is load-bearing for the two AST census meta-tests.
- **16 new unit tests.** Five behavioural fixes (WR-01, WR-02, WR-03 ×2, WR-09) were each verified
  RED against the pre-fix code before being accepted.

---

_Fixed: 2026-08-09_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_

---
phase: 03-world-character-commands
reviewed: 2026-08-09T00:00:00Z
depth: standard
files_reviewed: 55
files_reviewed_list:
  - cmd/holomush/core.go
  - cmd/holomush/core_subsystems_test.go
  - cmd/holomush/core_topo_order_test.go
  - cmd/holomush/sub_grpc.go
  - docs/architecture/invariants.md
  - docs/architecture/invariants.yaml
  - internal/access/policy/seed.go
  - internal/access/policy/seed_test.go
  - internal/access/policy/seed_profile_visibility_test.go
  - internal/access/policy/attribute/character_test.go
  - internal/charactivity/subsystem.go
  - internal/charactivity/subsystem_test.go
  - internal/charactivity/listener.go
  - internal/charactivity/flusher.go
  - internal/charactivity/charactivity_test.go
  - internal/core/session_ended_payload.go
  - internal/eventbus/audit/plugin_consumer.go
  - internal/eventbus/audit/projection.go
  - internal/eventbus/audit/projection_unit_test.go
  - internal/eventbus/consumer/consumer.go
  - internal/eventbus/consumer/consumer_test.go
  - internal/lifecycle/subsystem.go
  - internal/lifecycle/subsystemid_string.go
  - internal/retirement/reactor.go
  - internal/retirement/reactor_test.go
  - internal/retirement/subsystem.go
  - internal/retirement/subsystem_test.go
  - internal/testsupport/integrationtest/harness.go
  - internal/testsupport/integrationtest/options.go
  - internal/testsupport/integrationtest/plugins.go
  - internal/testsupport/integrationtest/real_abac.go
  - internal/testsupport/integrationtest/real_abac_test.go
  - internal/world/caller_test.go
  - internal/world/mutator.go
  - internal/world/outbox/taxonomy.go
  - internal/world/payloads.go
  - internal/world/repository.go
  - internal/world/service.go
  - internal/world/service_retire_test.go
  - internal/world/postgres/activity.go
  - internal/world/postgres/activity_test.go
  - internal/world/postgres/activity_integration_test.go
  - internal/world/postgres/character_repo.go
  - internal/world/postgres/character_repo_status_test.go
  - internal/world/postgres/character_repo_status_integration_test.go
  - internal/world/worldtest/mock_CharacterRepository.go
  - test/integration/access/evaluation_test.go
  - test/integration/charactivity/charactivity_suite_test.go
  - test/integration/charactivity/character_activity_flush_test.go
  - test/integration/resilience/retire_concurrency_test.go
  - test/integration/retirement/retirement_suite_test.go
  - test/integration/retirement/retirement_reactor_test.go
  - test/integration/world/character_lifecycle_test.go
  - test/integration/world/character_retire_atomicity_test.go
  - test/meta/world_sql_fence_test.go
findings:
  critical: 0
  warning: 9
  info: 6
  total: 15
status: issues_found
---

# Phase 03: Code Review Report

**Reviewed:** 2026-08-09
**Depth:** standard
**Files Reviewed:** 55
**Status:** issues_found

## Summary

Reviewed the 29-commit `adbfe0e2f..HEAD` range covering the character retire/unretire
commands, the two new lifecycle subsystems (`internal/retirement`, `internal/charactivity`),
the shared consumer-retry relocation, the SubsystemID 18→20 cascade, the ABAC job seed, and
the INV-WORLD-4 registry amendment.

The CAS path in `CharacterRepository.SetStatus` and the guard chain in
`Service.RetireCharacter`/`UnretireCharacter` are correct as written: the caller-supplied
`expectedVersion` (not the freshly-read one) reaches the SQL predicate, the version precheck
genuinely precedes the lifecycle guard, the lifecycle switches have denying default arms, and
the `players.default_character_id` clear is inside the same transaction and behind the CAS.
`UpdateCharacterLastActive`'s monotonic predicate is sound against a `BIGINT NOT NULL DEFAULT 0`
column. `docs/architecture/invariants.md` and `.yaml` are consistent with each other — no
hand-edit inside the generated region. Every `// Verifies:` annotation in the diff
(`activity_integration_test.go:110`, `character_activity_flush_test.go:122`) genuinely asserts
the INV-WORLD-4 clause it claims (version unchanged + outbox row count unchanged), and the two
places that deliberately *decline* to annotate document why.

The defects cluster in the two new event-driven subsystems, where several guarantees asserted
in comments (and "proven" by unit tests over fakes) do not hold against the real
`jetstream.ConsumeContext` and the real `events.*.character.>` traffic mix. There is also one
documentation claim about `outbox.AppSchemaVersion` that is factually false about the wire.

No blocker-class defect (incorrect committed state, security hole, or unrecoverable data loss)
was provable in this range.

## Warnings

### WR-01: Retirement reactor logs an ERROR "poison" line for every non-world event on its filter — including the one it emits itself

**File:** `internal/retirement/reactor.go:229-237` (with `181-198`, `254-260`)
**Issue:** `handleDecoded` fully decodes the message body *before* the event-type gate in
`process` runs. `newDelivery` calls `outbox.UnmarshalEnvelope(wire.GetPayload())`, which
requires a world envelope with a parseable `event_id`
(`internal/world/outbox/wire.go:100-104`). The consumer's filter is
`events.*.character.>` (`reactor.go:39`), and `internal/presence/session_ended.go:67`
publishes `session_ended` on exactly `character.<id>`. Every session end — every logout,
guest reap, eviction — therefore produces:

```
retirement reactor could not decode a delivered event; acking as poison  WORLD_OUTBOX_WIRE_BAD_EVENT_ID
```

at ERROR level. It is self-inflicting: the reactor's own step-(3) `EmitSessionEnded`
(`reactor.go:312`) lands on the same subject and comes straight back to its own consumer, so
*every successful retirement* logs at least one spurious "poison" error. The behavioural
outcome (ack) is correct, but the diagnostic signal the "poison" classification exists to
provide is destroyed — a genuinely undecodable message is indistinguishable from routine
traffic — and the error rate is proportional to session churn.
**Fix:** gate on the header before touching the body:

```go
func (r *reactor) handleDecoded(ctx context.Context, subject string, hdr nats.Header, data []byte) disposition {
	// Cheap header gate FIRST: the filter is the whole character aggregate, so
	// presence session_ended and other non-world events arrive here and are not
	// world envelopes at all. Decoding them would misreport routine traffic as poison.
	if hdr.Get(eventbus.HeaderEventType) != eventTypeCharacterRetired {
		return ack
	}
	d, err := newDelivery(subject, hdr, data)
	...
}
```

`process`'s own `d.EventType` check (`reactor.go:255`) then becomes a defence-in-depth no-op
rather than the only gate.

### WR-02: `Subsystem.Stop` does not actually join in-flight handlers in either new subsystem, and the test that claims it does cannot fail

**File:** `internal/charactivity/subsystem.go:377-391, 402-434`; `internal/retirement/subsystem.go:287-293, 302-319`
**Issue:** Both `Stop` bodies rely on `jetstream.ConsumeContext.Stop()` as a barrier.
`Stop()` in nats.go v1.52.0 (`jetstream/pull.go:768-780`) only does
`s.closed.CompareAndSwap(0,1)` + `close(s.done)` and returns; the handler is invoked from the
NATS subscription's dispatch goroutine (`pull.go:287`), which is never joined. So:

- `charactivity/subsystem.go:400-401`'s claim — *"Stop halts both producers, JOINS them, and
  only then runs one final drain, so no listener Put can race the shutdown flush (R2b)"* — is
  false. A listener `Put` can land concurrently with, or after, the final `drain`.
- `retirement/subsystem.go` tears down `s.reactor`, `s.cons`, `s.cc` and calls
  `unregisterJob()` while a `process()` may still be mid-fanout. Once the job's liveness is
  retracted, a still-running `MoveCharacter` loses its ABAC attributes; `classifyWorldError`
  (`reactor.go:343-350`) treats `ErrPermissionDenied` as **terminal and acks**, which would
  permanently abandon a half-applied fanout (session already deleted, character never moved).
  In practice `runCtx` is cancelled one line earlier, so the engine most likely surfaces an
  evaluation failure (→ `retry`) rather than a deny — but that is an accident of ordering, not
  a guarantee the code establishes.

`charactivity/charactivity_test.go:408-424`
(`TestStopJoinsBothProducersBeforeTheFinalDrain`) cannot falsify any of this: `fakeConsumer`
never delivers a message and `fakeConsumeContext.Stop()` is a synchronous flag set
(`charactivity_test.go:448-470`).
**Fix:** either (a) use `cc.Drain()` plus `<-cc.Closed()` to obtain a real barrier before the
final drain / job retraction, or (b) keep `Stop()` and correct the comments and the test name
to state the actual guarantee ("no *new* deliveries are dispatched; an in-flight handler may
outlive Stop"). If (b), move `unregisterJob()` to *after* a bounded grace period, or make
`classifyWorldError` treat a deny observed while `ctx.Err() != nil` as `retry`:

```go
func (r *reactor) classifyWorldError(ctx context.Context, err error, msg string) disposition {
	if errors.Is(err, world.ErrPermissionDenied) && ctx.Err() == nil {
		... return ack
	}
	... return retry
}
```

### WR-03: The activity listener can silently destroy a newer buffered timestamp with an older one

**File:** `internal/charactivity/listener.go:84-88`
**Issue:** `record` does an unconditional `kv.Put(ctx, id.String(), <nanos>)`. The buffer has
no monotonic guard of its own — only the *database* writer does
(`internal/world/postgres/activity.go:66`). Under at-least-once redelivery (a message whose
`AckWait` expired after a slower message was already handled), an older event's `Put` overwrites
a newer, not-yet-flushed value at a fresh revision. The flusher then writes the older value,
the DB predicate `last_active_at < $2` absorbs it as a no-op, and the newer timestamp is gone
— `last_active_at` is left behind by the delta until the character acts again. The package doc
claims lag is bounded "by up to one flush interval, BY CONSTRUCTION"; this path exceeds that
bound.
**Fix:** make the buffer monotonic too — read-compare-put with the read revision as the guard,
mirroring the flusher's `deleteAtRevision`:

```go
entry, err := l.kv.Get(ctx, key)
switch {
case errors.Is(err, jetstream.ErrKeyNotFound):
	_, err = l.kv.Create(ctx, key, val)   // loses a race harmlessly; next event re-buffers
case err == nil:
	if prev, perr := strconv.ParseInt(string(entry.Value()), 10, 64); perr == nil && prev >= nanos {
		return // an older redelivery MUST NOT clobber newer buffered activity
	}
	_, err = l.kv.Update(ctx, key, val, entry.Revision())
}
```

(`activityKV` in `flusher.go:28-38` needs `Create`/`Update` added; the existing fake already
models revision semantics, so the unit tests extend cleanly.)

### WR-04: `SetStatus`'s D-34 players write inverts the lock order the reaping guard establishes

**File:** `internal/world/postgres/character_repo.go:511-519`
**Issue:** The retire transaction acquires locks in the order `characters` (the CAS `UPDATE`
at line 496) → `players` (`UPDATE players SET default_character_id = NULL ...` at line 512-514).
`PlayerReapingGuard.EnsureNotReaping` (`internal/world/postgres/reaping_guard.go:64-70`)
establishes the *opposite* order on the same two tables and the same connection: `SELECT ...
FROM players ... FOR UPDATE` first, then the character-genesis `INSERT INTO characters`. A
concurrent guest-genesis for player P and a retire of P's default character are therefore a
textbook deadlock pair; Postgres aborts one with SQLSTATE 40P01, surfacing as an opaque
`CHARACTER_RETIRE_DEFAULT_CLEAR_FAILED` or `CHARACTER_CREATE_FAILED`. The long doc block at
`character_repo.go:466-475` and the fence commentary at
`test/meta/world_sql_fence_test.go:57-68` both discuss the layering exception but neither
mentions lock ordering.
**Fix:** take the `players` row lock first inside `SetStatus` on the retire branch, matching
the guard's order, before the character CAS:

```go
if status == world.StatusRetired {
	// Lock players BEFORE characters, matching PlayerReapingGuard's order
	// (reaping_guard.go:64) — the reverse order deadlocks against genesis.
	if _, err := tx.Exec(txCtx,
		`SELECT 1 FROM players WHERE default_character_id = $1 FOR UPDATE`, characterID.String()); err != nil { ... }
}
```

At minimum, document the hazard and add a retry-on-40P01 at the command boundary.

### WR-05: `outbox.AppSchemaVersion`'s doc asserts a wire property that does not exist; the 1→2 bump is inert

**File:** `internal/world/outbox/taxonomy.go:11-21`
**Issue:** The comment states the constant *"stamps the taxonomy REVISION that produced a
world-change feed row; a consumer reads it to know which taxonomy shape a payload was encoded
against."* Nothing reads it. `rg -n AppSchemaVersion --type go` returns only its own
declaration, its own test, and a prose mention in `internal/auth/character_genesis.go:27`. The
`App-Schema-Version` NATS header is stamped from a *different* constant,
`eventbus.SchemaVersion = "1"` (`internal/eventbus/publisher.go:62`); `wmodel.Envelope`
carries only the per-kind `SchemaVersion`; the `outbox` table has no such column. Bumping to 2
therefore changes no byte on the wire or on disk while creating the impression that consumers
can now distinguish taxonomy revisions — the exact false confidence a version constant is
supposed to eliminate.
**Fix:** either wire it (stamp it into `wmodel.Envelope` / the outbox row and have
`UnmarshalEnvelope` surface it), or rewrite the doc to say what is true:

```go
// AppSchemaVersion is a BUILD-TIME marker of the taxonomy registry revision.
// It is NOT stamped on any envelope, outbox row, or NATS header today — the
// App-Schema-Version header comes from eventbus.SchemaVersion. Bumping it
// documents a vocabulary change; it does not signal one to any consumer.
```

### WR-06: The activity flush spec races the flush ticker it is asserting against

**File:** `test/integration/charactivity/character_activity_flush_test.go:106-120`
**Issue:** `flushInterval` is 250 ms and the ticker starts at `Activate`, i.e. inside
`integrationtest.Start`, an unbounded time before `emit()` runs (an `AuthedPlayer` provisioning
and three DB round trips intervene). The tick phase is therefore uncorrelated with the emit,
which makes both pre-flush assertions racy:

- `Eventually(bufferedValue, ...).Should(Equal(...))` (line 111) requires the key to still be
  in the bucket when Gomega polls. If a tick drains it first, `bufferedValue` returns
  `ErrKeyNotFound` on every subsequent poll and the `Eventually` times out after 5 s.
- `Expect(lastActiveAt()).To(Equal(world.NeverActive))` (line 114) fails outright if a tick
  landed between the successful poll and this line.

The comment at lines 24-26 claims the interval is "long enough that the pre-flush assertions
below are not racing the very first tick", which only holds if the emit happens within one
interval of `Activate` — it does not. Expect intermittent CI failures under load.
**Fix:** remove the timing dependence. Either add a start-delay/manual-trigger seam to the
flusher for tests, or restructure so the pre-flush claim is asserted without needing the key
to survive: assert `Eventually(lastActiveAt).Should(Equal(at))` for the flush, and prove
"the emit path performed no database write" with a *synchronous* check immediately after
`emit()` returns (before any await), plus the version/outbox assertions that already exist.

### WR-07: `aggregateFromSubject` reads the last token, not the aggregate token

**File:** `internal/retirement/reactor.go:208-213`
**Issue:** The consumer filter is `events.*.character.>` — the `>` wildcard admits any depth.
`aggregateFromSubject` returns `subject[strings.LastIndex(subject, ".")+1:]`, so a subject with
a facet (`events.g1.character.<ulid>.something`) yields `"something"` as the provenance
`trigger_subject`. That value is compared byte-for-byte against `bags.Resource["id"]` by
`seed:job-retirement-instance-scoped`, so the write silently default-denies with a
`JOB_CHARACTER_ACCESS_DENIED` that `classifyWorldError` acks as terminal — the retirement
fanout is dropped with no retry and a misleading "policy denied" log. Today's world-envelope
subjects are exactly four tokens (`internal/world/outbox/wire.go:159-160`), so this is latent,
not live; but the function's own doc calls the value "the BARE aggregate ULID … (`events.<game>.character.<ulid>`)",
which is the shape it does not enforce.
**Fix:** parse positionally and refuse anything else:

```go
func aggregateFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) != 4 { // events.<game>.character.<ulid>
		return ""       // an unexpected shape MUST NOT masquerade as an aggregate id
	}
	return parts[3]
}
```

An empty result then denies (as it should) instead of authorizing against a facet name.

### WR-08: Exhausting `MaxDeliver` silently abandons a retirement fanout

**File:** `internal/retirement/subsystem.go:105-112` (with `reactor.go:343-350`)
**Issue:** `defaultMaxDeliver = 10` with no `ErrHandler`, no advisory subscription, and no DLQ.
The comment acknowledges *"reaching this cap means a genuinely stuck effect surface"* — but
nothing observes it. After ten failed deliveries JetStream advances past the message and the
character stays retired-but-not-evicted: session live, still at the old location, no leave
emitted. The only trace is ten scattered `redelivering` log lines and a
`MAX_DELIVERIES` advisory nobody consumes.
**Fix:** subscribe to `$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.EVENTS.<consumer>` and log
at ERROR with the character id, or set `jetstream.ConsumeErrHandler` and emit a metric. At
minimum, record a counter so an operator dashboard can alarm on abandoned fanouts.

### WR-09: `charactivity.Stop` performs a database-writing drain even when `Activate` never ran

**File:** `internal/charactivity/subsystem.go:409, 423-427`
**Issue:** `joined := s.done == nil` makes the never-activated case indistinguishable from the
successfully-joined case, so a subsystem that only reached `Prepare` still runs a full
`drain` — listing every key in the bucket and issuing one `UPDATE characters` per key. That is
exactly the state the orchestrator's rollback path produces
(`internal/lifecycle/orchestrator.go:77-83` calls `rollback` → `Stop` on everything already
prepared) when *some other* subsystem fails to prepare or activate. A failed boot should not
be mutating `characters`.
**Fix:** make the two states distinct:

```go
activated := s.done != nil
joined := false
if activated { /* select on s.done / timeout, set joined */ }
if activated && joined && kv != nil { ...drain... }
```

## Info

### IN-01: `RetireCharacter` and `UnretireCharacter` have no production caller

**File:** `internal/world/service.go:929, 1029`
**Issue:** `rg -n "RetireCharacter|UnretireCharacter" --type go` outside tests returns only
`service.go`, `mutator.go`, and `reactor.go`'s respelled event-type constant. Neither command
is reachable from any RPC, command dispatcher, or CLI in this phase, so INV-WORLD-7's
"every v0.13 character mutation RPC carries expected_version" has no boundary to be enforced
at yet, and the whole retire path is exercised only by tests.
**Fix:** expected phasing — confirm the RPC/command surface is scheduled and tracked, and note
in the phase summary that the commands ship unwired.

### IN-02: No seed grants `retire`/`unretire` to a character's owner

**File:** `internal/access/policy/seed.go` (new seed at the job block; no human-facing grant)
**Issue:** `Service.RetireCharacter` checks action `"retire"` (`service.go:942`). The only seed
that can match it is the blanket admin permit at `seed.go:107`
(`permit(principal is character, action, resource) when { "admin" in principal.character.roles }`).
D-39's stated rationale — *"their own character is an ABAC policy decision, not a Go
conditional"* — currently has no policy expressing it, so a player cannot retire their own
character at all.
**Fix:** if owner-initiated retirement is in scope for v0.13, the seed is missing; if not,
say so in the phase notes so the D-39 rationale is not read as already satisfied.

### IN-03: The leave announcement uses the session's location, not the character's

**File:** `internal/retirement/reactor.go:307`
**Issue:** `core.CharacterRef{... LocationID: info.LocationID}` takes the location from the
deleted session row, while the status guard already read `char.LocationID` from the character
row two steps earlier. If the two disagree (a move that landed after the session row was last
written), the leave is announced at the wrong location.
**Fix:** prefer `char.LocationID` when non-nil and fall back to `info.LocationID`, or document
why the session row is authoritative here.

### IN-04: Tautological assertion in the retire-concurrency spec

**File:** `test/integration/resilience/retire_concurrency_test.go:154-159`
**Issue:** Line 154 asserts `oopsErr.Code()` equals `world.CodeConcurrentEdit`; line 158 then
asserts the same value is not `"CHARACTER_ALREADY_RETIRED"`. The second assertion cannot fail
unless the first already has, so the "negative half of the guard-order proof" it is documented
as providing is decorative.
**Fix:** delete it, or replace it with the assertion that actually carries the guard-order
signal — e.g. assert the stale call is rejected while the row's status is already `retired`
(which the surrounding fixture establishes), and keep only the positive code check.

### IN-05: Cross-package mutation of `consumer.CreateBackoffs` from the audit test package

**File:** `internal/eventbus/audit/projection_unit_test.go:184-196`
**Issue:** `withShortBackoffs` now writes an exported package-level var owned by
`internal/eventbus/consumer`, and `internal/eventbus/consumer/consumer_test.go:19-25` has an
identical helper doing the same. The "MUST NOT call t.Parallel()" warning at
`internal/eventbus/consumer/consumer.go:32-37` now has to hold across two packages, and it is
stated in only one of them.
**Fix:** replace the global with a functional option (`CreateWithRetry(ctx, create,
WithBackoffs(...))`) so no test mutates shared package state, or restate the constraint in
the audit package's helper doc.

### IN-06: Unsynchronised field access in the KV test fake

**File:** `internal/charactivity/charactivity_test.go:107-113`
**Issue:** `fakeKV.DeleteRevision` reads and writes `k.beforeDelete` before taking `k.mu`,
while every other method guards its state. Safe today because only one flusher goroutine
exists per test, but it is the one field a concurrency test would touch, and `-race` would
flag it the moment a spec drives two drains.
**Fix:** move the hook read inside the mutex (capture under lock, invoke after unlocking) or
document that the fake is single-goroutine only.

---

_Reviewed: 2026-08-09_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_

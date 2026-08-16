---
phase: 03-world-character-commands
plan: 06
subsystem: retirement-fullstack-proof
tags: [integration-harness, outbox-relay, jetstream, ginkgo, abac, idempotency, ident-04, ident-10]
status: complete

requires:
  - "internal/retirement reactor + durable consumer + lifecycle contract (03-04)"
  - "internal/testsupport/integrationtest StartOption shape + WithCharacterActivity precedent (03-05)"
  - "world.Service.RetireCharacter with caller-supplied expected_version (03-01)"
  - "seed:job-retirement-instance-scoped + world.JobCaller/Provenance (03-04 / 02.2)"
  - "world/setup.OutboxRelaySubsystem (05-07)"
provides:
  - "internal/testsupport/integrationtest: WithOutboxRelay + WithRetirementReactor StartOptions"
  - "internal/testsupport/integrationtest: Server.World(), Server.RetirementStartLocation()"
  - "internal/testsupport/integrationtest: one shared world.Service and one shared jobs.Registry"
  - "test/integration/retirement: TestRetirementReactor suite (5 specs)"
affects:
  - "every future event-driven-consumer suite (the relay is now bootable, so a committed world write can reach a consumer)"
  - "every future job-authorization suite (the ABAC engine now sees job liveness in the harness)"

tech-stack:
  added: []
  patterns:
    - "a core NATS subscription as the observation instrument for a JetStream feed, so a spec watches the same bytes the durable consumer sees without opening a competing consumer"
    - "consumer AckFloor as the execution proof for a redelivery assertion — otherwise 'no second effect' holds equally well when the message never arrived"
    - "temporary RED probes in every It, run once, to prove a -run filter is not vacuous"

key-files:
  created:
    - test/integration/retirement/retirement_suite_test.go
    - test/integration/retirement/retirement_reactor_test.go
  modified:
    - internal/testsupport/integrationtest/harness.go
    - internal/testsupport/integrationtest/options.go
    - internal/testsupport/integrationtest/plugins.go
    - internal/testsupport/integrationtest/real_abac.go
    - internal/testsupport/integrationtest/real_abac_test.go
    - .planning/REQUIREMENTS.md

decisions:
  - "WithRetirementReactor CONSTRUCTS nothing new for the world surface: the harness now builds exactly ONE world.Service in Start and shares it with both the plugin subsystem and the reactor, so the service a spec retires through and the service the reactor moves through are the same object by construction rather than by matching two ServiceConfig literals."
  - "The harness now always builds one jobs.Registry and passes it to startRealABAC, mirroring cmd/holomush. Without it the job attribute provider sees no live job, and BOTH halves of the instance-fence spec deny — the denial passing for the wrong reason. The paired positive control is what makes that failure mode detectable."
  - "The redelivery proof republishes the captured wire bytes with a REFRESHED Nats-Msg-Id rather than re-invoking an unexported handler. Direct re-invocation was the plan's suggestion but would have required exporting a handler entry that production does not need; a byte-identical republish would have been swallowed by JetStream dedup and tested nothing."
  - "IDENT-04 and IDENT-10 are both flipped here, with the last_active_at operational-column writer named explicitly as the one argued exemption rather than left unmentioned."

metrics:
  duration: "~55 min"
  completed: 2026-08-09

actuals:
  tokens: 12400
  tasks: 2
  commits: 3
---

# Phase 03 Plan 06: Full-Stack Retirement Proof Summary

Retirement is no longer asserted to work — it is demonstrated, through the real
outbox relay, the real durable consumer and the real handler, by a suite whose
`-run` filter names a test function that actually exists.

## The two false greens this plan closed

### SG-1 — a filter that matched nothing

The phase's acceptance gate was `-run TestRetirementReactor
./test/integration/world/`. That package's only entry function is `TestWorld`,
so the filter matched no test at all. Measured on this branch, before the new
package was written into the command:

```
task test:int -- -run TestRetirementReactor ./test/integration/world/
∅  test/integration/world (474ms) [no tests to run]
DONE 0 tests   →   exit 0
```

Zero tests, exit 0, green gate. `test/integration/retirement/` now carries
`func TestRetirementReactor` as its single `RunSpecs` entry point, so the same
filter has a real target.

### SG-2 — the relay nobody started

`plugins.go` documented, accurately, that the outbox relay "is a separate
subsystem not started by this harness." Nothing owned a fix. The consequence was
structural: a world write committed its outbox row and stopped there, so **no
event-driven consumer of world state could be observed in this harness at all**.
`WithOutboxRelay()` boots the real `setup.NewOutboxRelaySubsystem` through its
production `Prepare`/`Activate`/`Stop` contract, and the stale half of that
comment now names its exception.

## What was built

### Two owned StartOptions

| Option | Boots | Notes |
|---|---|---|
| `WithOutboxRelay()` | `worldsetup.NewOutboxRelaySubsystem` over the harness pool + embedded bus | real lease, real LISTEN/NOTIFY waker, real drain loop |
| `WithRetirementReactor()` | `retirement.NewSubsystem` with harness-resolved production-shaped deps | registers the `retirement` job into the shared registry at Activate |

They are orthogonal — neither implies the other — and the retirement suite
passes both. Two of the reactor's dependencies are deliberately **not** the
harness's own, and each for a reason a spec depends on:

- **A real `presence.NewEmitter` over the bus publisher** (rendering-wrapped, as
  production's is). The harness's own presence emitter publishes into
  `&noopPublisher{}`, so the reactor's `leave` and `session_ended` would have
  been invisible — the exact opposite of what the option exists for. A useful
  side effect: it is the *only* presence emitter reaching the bus, so a leave
  count of 1 cannot be diluted by unrelated harness traffic.
- **A move destination distinct from the guest start location.** Characters are
  seeded where guests start; a destination equal to it would hit the reactor's
  already-there skip gate, and the move would correctly emit nothing. Read back
  via `Server.RetirementStartLocation()`, which `t.Fatalf`s rather than
  returning a zero ULID when the option was not passed.

### Two structural corrections the options forced

Both are answers to the plan's open question, and both replace a
matching-two-literals arrangement with a single object.

**The world service is now built once.** The plan allowed either reusing the
plugin path's `world.Service` or constructing a second one "with the identical
ServiceConfig wiring". Neither was satisfying: the plugin path's instance was a
local inside `startPlugins`, and a second literal is identical only until
someone edits one of them. `Start` now builds the single instance via
`newWorldService(pool, engine)` and threads it into `pluginDeps`; `Server.World()`
exposes it. **The service a spec retires through and the service the reactor
moves through are the same object**, not two objects that happen to agree.

**The job registry is now threaded.** `startRealABAC` built its ABAC subsystem
with no `JobRegistry`, so `attribute.JobProvider` saw a nil registry and answered
"no job is running" for everything. Under that wiring the instance-fence spec's
denial would have passed while proving nothing — every job write denies when no
job is live. `Start` now builds one `jobs.Registry` unconditionally and passes it
to `startRealABAC`, exactly as `cmd/holomush/core.go` passes its single instance.
An empty registry still fails closed, so every existing suite is unchanged.

This is precisely the failure the **paired positive control** exists to catch:
with the registry unthreaded, the deny spec is green and its control is red.

### The suite — five specs, all proven to execute

`test/integration/retirement/`, `//go:build integration`, one `RunSpecs`.

| Spec | Asserts |
|---|---|
| fanout | session row gone; `leave` on `events.main.location.<OLD>` with reason `retired`; `session_ended` on the character subject with `cause=retired` and the right session id; `characters.location_id` = the starting location |
| feed order | `character_retired`'s `feed_position` < `character_moved`'s, read from the real outbox rows (IDENT-10 ordering edge) |
| redelivery | after a second delivery of the same event: exactly one leave, exactly one `character_moved` |
| instance fence | a job caller carrying provenance for aggregate X is DENIED against aggregate Y, with code `JOB_CHARACTER_ACCESS_DENIED` |
| positive control | the same caller against aggregate X is PERMITTED, and the row actually moved |

**No synthetic event is published to shortcut the relay.** The fanout spec calls
`RetireCharacter` and then waits; everything asserted is produced by the relay
publishing the committed envelope and the durable consumer delivering it.

Two details are load-bearing and worth naming:

- The capture subscription is opened and **flushed** before the retire, because
  `leave` and `session_ended` are emitted during the fanout and a subscription
  opened afterwards would miss them.
- The redelivery spec records the consumer's `AckFloor.Consumer`, republishes,
  and waits for the floor to advance **before** asserting that no second effect
  occurred. Without that step, "exactly one leave" holds just as well when the
  redelivered message never arrived — a third false green, in a plan whose
  purpose is closing two.

## Proving the specs actually run

`gotestsum --format pkgname` suppresses a passing package's output, so a green
run alone cannot distinguish "the specs passed" from "the specs never ran" —
which is how SG-1 survived. Following 03-03's technique, a temporary failing
assertion was inserted into **each** of the five `It` bodies, the suite was run
once, and the runner reported:

```
Ran 5 of 5 Specs in 3.055 seconds
FAIL! -- 0 Passed | 5 Failed | 0 Pending | 0 Skipped
```

with all five distinct probe strings present in the output. The probes were then
removed and the suite re-run green. `rg -c 'RED-PROBE'` over the tree returns
nothing; the probes exist only in this paragraph.

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 3 — Blocking] The real-ABAC path had no job registry, so the instance-fence spec could not be written honestly**

- **Found during:** Task 1
- **Issue:** `startRealABAC` omitted `ABACSubsystemConfig.JobRegistry`. The plan
  said to "verify the real-ABAC path seeds it" — the *seed* is indeed installed
  from the production corpus, but seeding is only half the gate. Liveness is the
  other half (D-49), and with a nil registry `attribute.JobProvider` stamps no
  attributes at all, so every job-gating seed default-denies. The denial spec
  would have been green for a reason unrelated to the instance fence.
- **Fix:** `Start` builds one `jobs.Registry` unconditionally and passes it to
  `startRealABAC`, mirroring `cmd/holomush/core.go`'s single instance. The
  reactor registers into that same instance at `Activate`.
- **Files modified:** `internal/testsupport/integrationtest/harness.go`,
  `internal/testsupport/integrationtest/real_abac.go`,
  `internal/testsupport/integrationtest/real_abac_test.go`
- **Commit:** add8cda14

**2. [Rule 2 — Missing critical structure] Two world.Service literals would have to be kept in agreement by hand**

- **Found during:** Task 1
- **Issue:** The plan's fallback ("a service constructed with the identical
  ServiceConfig wiring including OutboxWriter") produces a second literal whose
  fidelity depends on nobody editing one of them. In a suite whose whole claim is
  "the reactor writes through the real world service", that is the wrong kind of
  identical.
- **Fix:** One `newWorldService` helper, called once in `Start`, threaded into
  `pluginDeps.worldSvc` and into the reactor. Exposed as `Server.World()`.
- **Files modified:** `internal/testsupport/integrationtest/harness.go`,
  `internal/testsupport/integrationtest/plugins.go`
- **Commit:** add8cda14

### Deviation from the plan's prescribed redelivery mechanism

The plan specified "invoke the handler entry DIRECTLY a second time with the
recorded event". The reactor's handler is unexported and reachable only through
its durable consumer; direct invocation from an external test package would have
required **exporting a production seam that exists solely for a test**. Instead
the spec republishes the captured wire bytes — same subject, same body, same
`App-Event-Type` — with a refreshed `Nats-Msg-Id`, because JetStream would dedup
a byte-identical republish and the reactor would never see a second delivery.
The reactor reads the id only as provenance, never as a gate, so the refresh
changes nothing the handler decides on. This still avoids the coupling the plan
was steering away from: it does not depend on `AckWait` timing.

## Requirement closure — the honest read

Every prior plan in this phase deliberately left both requirements unchecked,
recording that the last plan would flip them honestly. Both are flipped.

**IDENT-04 — earned.** Each clause has a landed, tested answer:

| Clause | Where it is proven |
|---|---|
| soft-retired | `RetireCharacter` status write + one envelope (03-01, `service_retire_test.go`) |
| leaves active play | `SelectCharacter` excludes retired (`character_lifecycle_test.go`) **and** the reactor evicts the session and moves the character — demonstrated end to end here |
| record and name preserved | INV-WORLD-6 spec, including the production-`RetireCharacter` path, not only a direct SQL status flip |
| reversible | `UnretireCharacter` with its own nine-case spec set (03-01) |
| admin-driven | no player-facing seed; six paired evaluation specs (03-04) |

**IDENT-10 — earned, with one exemption named rather than hidden.** The character
mutations this phase added — `RetireCharacter` and `UnretireCharacter` — each
take a caller-supplied `expected_version`, refuse a zero or negative one before
any read, prefer the version conflict over the lifecycle guard on a stale caller,
and commit exactly one envelope in the same transaction as the status write. The
feed-order spec above reads those envelopes back from the real `outbox` table,
which until this plan was never actually drained in an integration harness.

The exemption: 03-05's `last_active_at` writer touches the `characters` table
with no `expected_version` and emits no envelope. That is **not** an unnoticed
gap in IDENT-10 — it is an operational column rather than world state, and
`INV-WORLD-4` was amended in the same change to enumerate it as the sole
envelope-exempt writer with the reasoning recorded at the exemption. IDENT-10
governs character *mutations* (registered world commands, which the envelope and
caller censuses enumerate); the operational-column writer is outside that set by
an argued, registered decision. Flipping IDENT-10 without saying so would have
been the tidier answer and the less honest one.

## A tooling observation (not fixed here)

`gsd-tools query requirements.mark-complete IDENT-04 IDENT-10` updated the
checkboxes but reported both as `table_unmatched` — the traceability table's
phase column reads `Phase 3` while the verb matches on the phase directory key.
The two rows were set to `Complete` by hand, which is filling a value into a
shape the tool already writes, not new structure. Worth an upstream note; no
local workaround was invented.

## Threat mitigations applied

| Threat | Disposition | Where |
|---|---|---|
| T-03-21 (Repudiation, criterion 2 asserted not demonstrated) | mitigated | this plan IS the mitigation — 03-04's T-03-10/11/13 move from unit-asserted to observed through the real chain |
| T-03-22 (Tampering, harness options masking production gaps) | mitigated | real relay, real subsystem lifecycles, real presence emitter on the bus, one shared real `world.Service`; the no-synthetic-event rule keeps the relay in the loop, and the AckFloor check keeps the redelivery honest |

## Known Stubs

None. The two new test files and the five modified harness files contain no
`TODO`, `FIXME`, `STUB` or not-implemented body, and no temporary probe survived.

## Verification

| Gate | Result |
|---|---|
| `task test:int -- ./internal/testsupport/integrationtest/` | 33 tests, **exit 0** |
| `task test:int -- -run TestRetirementReactor ./test/integration/retirement/` | **exit 0**; RED probe run proved `Ran 5 of 5 Specs` |
| `task test:int -- -run TestRetirementReactor ./test/integration/world/` | **exit 0 with 0 tests** — the SG-1 pathology, measured |
| `task test` (whole repo) | 11357 tests, 4 skipped, **exit 0** |
| `task test:int` (whole repo) | 11827 tests, 7 skipped, **exit 0** |
| `task lint` | **exit 0** before every commit; `task fmt` output committed |

Every verdict read from the **exit code**, never from matched output.

## Observed values recorded for the phase record

- **World service branch:** neither of the plan's two options — a third,
  `Start`-owned single instance shared by both consumers (see Deviations).
- **Feed positions (spec 2):** the assertion is relational
  (`character_retired` < `character_moved`) rather than pinned to literals,
  because the harness seeds a guest start location and a retirement hall before
  the spec runs, so absolute positions are boot-order artifacts. The relation is
  the property; a literal would be a fixture.
- **Deny-code shape consumed (spec 4):** `JOB_CHARACTER_ACCESS_DENIED` — 02.2's
  D-58 principal-kind prefix on the entity code, asserted on the resolved oops
  code alongside `errors.Is(err, world.ErrPermissionDenied)`.

## Note on an unrelated dirty file

`.claude/agent-memory/abac-reviewer/MEMORY.md` and the untracked `.gsd/`
directory were both dirty before this plan started and are unrelated to its
files. Per the scope boundary, neither was touched.

## Self-Check: PASSED

Both created files exist on disk; all three commit hashes resolve in `git log`.

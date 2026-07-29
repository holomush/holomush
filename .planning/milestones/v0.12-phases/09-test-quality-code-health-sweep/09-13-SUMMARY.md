---
phase: 09-test-quality-code-health-sweep
plan: 13
subsystem: test-quality
tags: [QUAL-04, session-lifecycle, idle-timeout, reaper, privacy-floor]
status: complete
requires:
  - 09-12 (registry contract, marker form, lifecycleHarness)
  - 09-20 (DetachAndExpireSession seam, telnet client-type seam)
  - 09-07 (EmitDirectEventAt timestamped emit)
provides:
  - "test/integration/session/lifecycle_ttl_test.go — 8 specs for the idle-timeout transition family"
  - "reapSessionAndExpectRowGone — drives the real session.Reaper, guarded by a ListExpired precondition"
  - "detachAndExpectDetached — shared detach-to-detached step"
  - "Matrix rows detach-all.{web-guest,web-char,telnet}, reaper-sweep.{web-guest,web-char,telnet}, post-ttl-relogin.{web-char,telnet}"
affects:
  - 09-14
  - 09-16
tech-stack:
  added: []
  patterns:
    - "A precondition that asserts the row matches the production predicate BEFORE driving it, so a mis-seeded fixture fails loudly instead of timing out on an unreachable assertion"
    - "An absence assertion is only trusted when a positive control rides in the SAME query — an empty read satisfies a bare absence check"
    - "Bracketing relationships between injected instants are asserted, not assumed, so wall-clock collapse fails the spec rather than making it pass for the wrong reason"
key-files:
  created:
    - test/integration/session/lifecycle_ttl_test.go
  modified:
    - test/session-matrix.yaml
decisions:
  - "post-ttl-relogin.web-guest left planned with blocked_on rather than covered by a second-guest stand-in that could not fail"
  - "The plan's ExpireSession guard was replaced with an anchored, positive-controlled pattern: the literal needle matches DetachAndExpireSession, the seam the plan REQUIRES"
  - "A second event above the new floor is read back in the same query, after instrumentation showed the fresh read returning zero rows"
metrics:
  duration: 71m
  tasks: 2
  files: 2
  completed: 2026-07-26
---

# Phase 09 Plan 13: Idle-Timeout Matrix Rows Summary

Eight of the nine owed cells now have specs that drive the production detach → expiry → reap →
re-login chain and assert its history-floor consequences; the ninth is left `planned` with the
exact missing seam named, because the only way to "cover" it was a spec that could not fail.

## Cells landed — 8 of 9

| Matrix row | Container | Landed |
|---|---|---|
| `detach-all.web-guest` | Dropping every connection detaches the session and starts its time-to-live | yes |
| `detach-all.web-char` | " | yes |
| `detach-all.telnet` | " | yes |
| `reaper-sweep.web-guest` | The reaper sweep at time-to-live expiry removes the session row | yes |
| `reaper-sweep.web-char` | " | yes |
| `reaper-sweep.telnet` | " | yes |
| `post-ttl-relogin.web-char` | Logging in again after expiry produces a genuinely fresh session | yes |
| `post-ttl-relogin.telnet` | " | yes |
| `post-ttl-relogin.web-guest` | — | **NO — `planned`, `blocked_on` recorded** |

Registry after this plan: **16 spec** (was 8), **17 planned** (was 25), 10 not-applicable,
3 not-implementable, 2 covered-elsewhere. 48 rows, unchanged.

## The cell NOT claimed, and why

**`post-ttl-relogin.web-guest`** stays `planned`, now `owed_by: "09-14"` with a `blocked_on`
naming the identical missing seam that already blocks `reattach-select.web-guest`:

- `Server.ConnectGuest` mints a **new** guest player and character on every call — `CreateGuest`
  then `SelectCharacter` in one body (`harness.go:1152-1190`).
- `Session.playerSessionToken` is unexported and has no accessor (verified: no exported
  `Session`/`AuthedPlayer` method matching token/select/reselect).
- `Server` exposes no `coreServer` handle, so the test package cannot call `SelectCharacter`
  itself (verified: no exported `Server` method matching CoreServer/Core/SelectCharacter).
- `Server.AuthedPlayer` — the handle that *does* allow repeated opens — creates a **registered**
  player.

The available stand-in was "connect a second guest". That would satisfy *different session id*
and *later arrival timestamp* trivially, with **no expiry involved and nothing for the reaper to
have done** — a spec that passes whether or not the transition under test works. That is the
false-green class this registry exists to prevent, so the row was left `planned`.

**Visible consequence for 09-16:** three guest/multi-session cells are blocked on two missing
harness seams (`reattach-select.web-guest`, `post-ttl-relogin.web-guest` on guest re-select;
`reattach-cas.multi-session` on per-connection detach). If 09-14 does not build them, these
remain `planned` at phase end. That is a real gap, and it is meant to be visible rather than
papered over.

## The defect this plan caught in its own first draft

The post-expiry specs originally ended with a bare *"the fresh read does not contain the planted
event"*. All 24 specs passed. Instrumenting the spec with phase timings showed why that was not
trustworthy:

```
TIMING first-read  n=1   5.010s
TIMING second-read n=0  10.085s
```

The fresh read returned **zero rows**. A bare absence assertion is satisfied by an empty result,
so the spec could not distinguish *"the floor excluded the event"* from *"this read returns
nothing at all"* — it would have gone green either way. That is defect class (i)/(n) from the
phase list, and it would have shipped as a green cell asserting a privacy property it never
tested.

**Fix:** a second event is emitted **above** the new session's floor and read back in the **same**
query. The single read now asserts both directions — the new event present, the planted event
absent — so the floor is demonstrably what separates them. (Also observed: each
`QueryStreamHistoryBounded` call costs a fixed ~5s in this suite, which is why these two specs
take ~10s each. Pre-existing characteristic of the history read path, not introduced here; the
one-read design keeps it at two reads per spec rather than three.)

## Load-bearing demonstrations — every guard observed failing, attributed by name

No guard was accepted without seeing it fail. Each break was reverted and the suite re-run green.

| Guard | Break applied | Observed failure |
|---|---|---|
| Reaper seam is load-bearing | seeded via `ExpireSession` instead of `DetachAndExpireSession` | **exactly the 3 reaper specs** failed on the `ListExpired` precondition (`[] to contain element`); the other 19 stayed green |
| Detach preserves the arrival floor | bumped `location_arrived_at` inside the shared detach helper | **exactly the 3 detach specs** failed on the INV-PRIVACY-1 / I-PRIV-3 message; the reaper specs, which call the same mutated helper but do not assert arrival, stayed green |
| Re-login gets a fresh floor | forced the new session to inherit the expired session's floor | **exactly the 2 post-relogin specs** failed on the INV-PRIVACY-1 exclusion, with their positive controls still passing (so the read was live); the other 22 stayed green |
| Marker uniqueness check | seeded a duplicate marker into the check's input | `uniq -d` emitted `detach-all.telnet` — the empty real result is absence, not a broken command |

The first three are attributed **by construction**: only the specs asserting the broken property
failed, so a non-zero exit is not being mistaken for "my check fired" (the 09-09 trap).

## Deviations from Plan

**1. [Rule 1 — bug] Task 1's `ExpireSession` acceptance guard matches the seam the plan REQUIRES.**

- **Found during:** Task 1 acceptance checks.
- **Issue:** the criterion is `rg -c 'ExpireSession' test/integration/session/` returns **no
  matches**. But `DetachAndExpireSession` — the seam the same task *mandates* — contains
  `ExpireSession` as a substring. The literal guard returns **9** on a correct implementation,
  i.e. it is unfalsifiable in the wrong direction and fails the very code it is meant to bless.
  This is the phase's (c)/(l) superstring class.
- **Fix:** anchored to the call form `\.ExpireSession\(`, which cannot match
  `...AndExpireSession(` (the preceding character is `d`, not `.`). Returns **0**. Positive-
  controlled against a two-line probe containing both spellings: it matched the forbidden call
  and not the required seam, so the empty result is evidence of absence rather than of a pattern
  that never matches.

**2. [Rule 3 — blocking] `test/session-matrix.yaml` is modified, which Task 1's file-scope
criterion excludes.**

- **Found during:** Task 1 commit.
- **Issue:** the criterion says `git diff` touches no file outside `test/integration/session/`.
  But the plan's own objective is to move nine registry rows off `planned`, and those rows live
  in `test/session-matrix.yaml`. A spec carrying a marker whose row still says `planned` fails
  09-16's bijection. The two instructions cannot both be satisfied.
- **Fix:** modified the registry, which is the plan's stated deliverable. The criterion's actual
  intent — *no production time-to-live constant was altered* — is satisfied and verified: the
  only files touched are the new spec file and the registry.

**3. [Rule 2 — missing critical functionality] Added a positive control to the fresh history
read.**

- **Found during:** Task 2, after instrumenting the spec.
- **Issue:** the plan's acceptance criterion asks only for *"the absence of the pre-session event
  from the bounded history read"*. The read returns zero rows, so that assertion passes
  vacuously. See the section above.
- **Fix:** a second event above the new floor, asserted present in the same query.

**4. Plan wording adjusted: the planted event sits BETWEEN the two floors, not before both.**

- **Found during:** Task 2 design.
- **Issue:** the plan says to assert the fresh read excludes *"an event emitted before the first
  session existed"*. An event below **both** floors is excluded by the old floor and the new one
  alike, so the assertion cannot distinguish an inherited floor from a fresh one — it would pass
  even on the bug it is meant to catch.
- **Fix:** the event is planted strictly between the two arrival timestamps, which is the only
  placement that discriminates. Both bracketing relationships are asserted explicitly, so a
  wall-clock collapse fails the spec rather than making it pass for the wrong reason. Negative
  control 3 confirms the assertion fails under an inherited floor.

**5. `post-ttl-relogin.web-guest` reassigned `owed_by: "09-13"` → `"09-14"`.**

Leaving it owed by this plan would be false once this plan closes. It is grouped with
`reattach-select.web-guest`, which 09-12 already assigned to 09-14 blocked on the same seam.

## Verification

| Gate | Result |
|---|---|
| `task test:int -- ./test/integration/session/...` | exit 0 |
| Specs actually registered | **24 It specs, all passed** (Ginkgo JSON report; pre-plan count was 16, so +8) |
| `task test` | exit 0 — 10412 tests, 4 skipped |
| `task lint` | exit 0 |
| `task fmt` | exit 0; mutations committed |
| `task test -- ./test/meta/` (registry shape) | exit 0 — 103 tests |
| Registry shape | 48 rows; 16 spec / 17 planned / 10 n/a / 3 not-implementable / 2 covered-elsewhere |
| Marker ↔ registry bijection | 16 spec rows ↔ 16 markers, **no orphan in either direction**, no duplicates |
| Forbidden helper (`\.ExpireSession\(`) | 0 matches, pattern positive-controlled |
| Real reaper driven (`session.NewReaper`) | present |
| `time.Sleep` in the session suite | 0, unchanged from the pre-plan baseline of 0 |
| Files touched | `test/integration/session/lifecycle_ttl_test.go`, `test/session-matrix.yaml` — no production constant altered |
| Commit deletions | none (`git diff --diff-filter=D` empty for both commits) |

Spec counts were read from `-ginkgo.json-report`, not parsed from `gotestsum --format pkgname`,
which prints no spec counts and so cannot detect a spec silently vanishing (09-11 finding). The
scoped `task test:int -- ./test/integration/<domain>/...` form worked again, a fifth confirmation
that the claim in `plan-review-learnings.md` that it ignores `--` args is false.

## Self-Check: PASSED

- `test/integration/session/lifecycle_ttl_test.go` — FOUND
- `test/session-matrix.yaml` — FOUND (modified)
- Commit `eb8c78084` — FOUND
- Commit `cabd91e7c` — FOUND

## Known Stubs

None. No stub, placeholder, skipped test, or unrun `<verify>` was introduced. The one uncovered
cell is recorded as `planned` with `blocked_on` naming the missing seam — a first-class registry
state, visible to the meta-test, not a silent gap.

## Note on QUAL-04 status

QUAL-04 remains **Pending**, following 09-12 and 09-20. This plan delivers 3 of the matrix's 12
transition rows; 17 rows are still `planned` across 09-14 and 09-15, and 09-16 locks the
bijection. The last of 09-14/15/16 to land should mark it.

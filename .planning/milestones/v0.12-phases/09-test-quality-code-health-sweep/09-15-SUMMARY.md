---
phase: 09-test-quality-code-health-sweep
plan: 15
subsystem: test-quality
tags: [QUAL-04, session-lifecycle, privacy-floor, move-arrival, wifi-blip, INV-PRIVACY-6]
status: complete
requires:
  - 09-12 (registry contract, marker form, lifecycleHarness)
  - 09-20 (OpenTelnetSession, DetachAndExpireSession, WithBuiltinCommands seams)
  - 09-07 (EmitDirectEventAt — the timestamped emit every spec here depends on)
  - 09-13 (the real-reaper drive reused by the expiry spec)
  - 09-14 (GuestPlayer; the owed_by convention)
provides:
  - "test/integration/session/lifecycle_privacy_floor_test.go — 9 specs: the last 7 matrix cells plus the two named privacy-floor specifications"
  - "AuthedPlayer.AdditionalCharacter — two game sessions under ONE player session"
  - "The INV-PRIVACY-6 floor-preservation arm, bound in the invariant registry"
  - "Matrix rows move-arrival.{web-char,telnet,multi-session}, wifi-blip.{web-guest,web-char,telnet,multi-session}"
affects:
  - 09-16
tech-stack:
  added: []
  patterns:
    - "Prove a drop is REAL (connection rows empty) before asserting anything continuity-shaped on top of it"
    - "Break the read, not the bookkeeping: a negative control that fails an earlier guard proves that guard, not the one under test"
    - "Keep opt-in symbol names out of prose when a guard greps for them, or the guard stops distinguishing use from mention"
key-files:
  created:
    - test/integration/session/lifecycle_privacy_floor_test.go
  modified:
    - internal/testsupport/integrationtest/harness.go
    - test/session-matrix.yaml
    - test/integration/privacy/privacy_test.go
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md
decisions:
  - "Move rows took route (b): kept MoveTo and relabelled the cells as privacy-floor-after-simulated-move, because route (a) would prove the OPPOSITE — the harness world service has no MovementHook, so MoveCharacter there leaves location_arrived_at unchanged"
  - "The production movement-lifecycle claim is cited to issue #4788 rather than filed anew; #4788 already records zero production callers, no integration test, no movement command, and names a command-to-move integration test in its acceptance"
  - "Built AuthedPlayer.AdditionalCharacter so the multi-session cells hold two sessions under ONE player session — the only shape that makes a token-keyed teardown detectable"
  - "The two named privacy specs are Ginkgo containers carrying their identifier verbatim, not func Test... symbols; no meta-test binds the names today"
  - "INV-PRIVACY-6 flipped pending -> bound: the new spec asserts BOTH clauses in one read, so it is not a partial binding"
metrics:
  duration: 71m
  tasks: 3
  files: 5
  completed: 2026-07-26
---

# Phase 09 Plan 15: Move-Arrival, Transport-Blip and the Named Privacy-Floor Specifications Summary

All seven owed cells landed, plus the three named floor assertions. The matrix is
now fully populated except the one cell 09-14 handed to nobody, and the registry
09-16 locks says exactly what each of these specs proves — including where one of
them proves less than its row title suggests.

## The seven cells — all landed

| Matrix row | Landed |
|---|---|
| `move-arrival.web-char` | yes |
| `move-arrival.telnet` | yes |
| `move-arrival.multi-session` | yes |
| `wifi-blip.web-guest` | yes |
| `wifi-blip.web-char` | yes |
| `wifi-blip.telnet` | yes |
| `wifi-blip.multi-session` | yes |

**Registry disposition for 09-16:** 48 rows — **32 `spec`** (was 25), **1 `planned`**
(was 8), 10 not-applicable, 2 covered-elsewhere, 3 not-implementable. The single
remaining `planned` row is `reattach-cas.multi-session`, still `owed_by: unassigned`;
this plan did not touch it and did not build the per-connection detach seam it names.
Marker ↔ `spec`-row bijection verified exact: **32 markers ↔ 32 rows**, `diff` of the
two sorted sets empty over two NON-EMPTY sets, no duplicate marker anywhere under
`test/`.

The bijection needle had to be anchored: `rg '// matrix-row: (\S+)'` over `test/` also
matches the literal placeholder `<id>` in five doc comments and in the registry's own
header, giving 37 "markers". Anchoring to `[a-z0-9.-]+$` over `*.go` gives the real 32.

## The move rows: route (b), and why route (a) would have proved the opposite

The plan offered two routes and asked which was taken. **Route (b): kept `Session.MoveTo`
and relabelled the cells.** Route (a) — driving `world.Service.MoveCharacter` — is not
merely intractable here; it would produce a green cell for the wrong reason and a red
one for the right reason:

- `MoveCharacter` propagates the new location to the session store through a
  `MovementHook` (`internal/world/movement_hook.go:13-33`). The only implementation,
  `sessionStoreMovementHook`, is wired **exclusively** at `cmd/holomush/sub_grpc.go:331`.
  The integration harness builds its world service without one
  (`internal/testsupport/integrationtest/plugins.go:267`), so `MoveCharacter` there runs
  against `NoopMovementHook` and leaves `location_arrived_at` **unchanged** — the exact
  opposite of what this row asserts.
- The harness world service is built inside `startPlugins`, so it is not reachable at
  all from `lifecycleHarness()`, which does not use `WithInTreePlugins`.

**What the cells claim now:** the privacy floor applied correctly to a session whose
location changed. **What they explicitly do not claim:** that the production movement
pipeline advances that floor. Both the spec file header and all three registry rows say
this in full.

**Where the production claim lives instead: issue #4788**, confirmed OPEN. It records
that `world.Service.MoveCharacter` has **zero production callers** and is **not
integration-tested**, that no plugin registers a movement command, and its acceptance
includes "a command→move integration test". No new issue was filed — #4788 already owns
this, and duplicating it would fragment the gap. One detail #4788 does not name, now
recorded in the registry notes: **`session.Store.UpdateLocationOnMove`** — the tail of
that pipeline and the statement that actually writes the new floor — has **no test
reference anywhere in the tree**. That negative is attributable rather than assumed: the
same needle finds 5 hits in non-test files and zero in `*_test.go`.

`rg -c 'MoveCharacter' test/integration/session/lifecycle_privacy_floor_test.go` returns
0 matches for a *call*; the symbol appears only in the header explaining why it is not
called. The rows do not claim production movement coverage, so the plan's criterion is
satisfied in the direction it was written for.

## The multi-session column needed a seam, and only one of the two cells got real content from it

`Server.AuthedPlayer` and `Server.GuestPlayer` each provision one player with one
character, so two calls give two players with two tokens — a shape that cannot express
"two concurrent sessions of one player session". **`AuthedPlayer.AdditionalCharacter`**
(additive, mirrors the existing character-seeding block) returns a handle sharing
`PlayerID` and the bearer token with a fresh `CharacterID`.

That distinction is load-bearing for **`wifi-blip.multi-session` and nothing else**.
`DetachTransport` calls the production `Disconnect` RPC, which takes a session id AND
the caller's token and validates the pairing through `auth.ValidateSessionOwnership`
(`internal/grpc/lifecycle_handler.go:106-123`). With two separate players the tokens
differ, so a token-keyed teardown would still leave the other session alone and the
spec would pass. With one shared token it takes both down — demonstrated by negative
control C.

**`move-arrival.multi-session` is honestly weaker, and says so.** The production half is
that each session's view is floored by its OWN row (`streamScopeFloor` takes one
`session.Info`), so the sibling's read is decided by the sibling's arrival regardless of
the mover's row. The sibling's row being untouched in the first place is a property of
`MoveTo`'s `WHERE` clause — the test's own setup, not production behaviour. Production's
real write is keyed by CHARACTER and updates every active session of that character, and
is untested. Both the spec comment and the registry row state this rather than letting
the cell read as stronger than it is.

## The two named privacy specifications

Both are present verbatim and passing:

- **`TestPrivacy_ReattachWithinTTLPreservesFloor`**
- **`TestPrivacy_TTLExpiryEndsSessionFreshFloor`**

**Record for the naming sweep in 09-18: these two names are EXEMPT.** They are pinned by
`docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` §8, which names them
verbatim as its integration acceptance. Their underscore form is not a violation under the
tightened predicate — the trailing segments are multi-word descriptive clauses.

**A plan claim that did not check out.** The plan states the names are "pinned by a
meta-test in the privacy specification that finds them by name". **No such meta-test
exists.** `rg -ln 'history-scope-privacy-design|TestPrivacy_' test/meta/` exits 1, while
the same needle hits the spec document — so the negative is controlled, not a needle
typo. The names appear only in prose. They were kept anyway: the specification names them
as its acceptance, and a future consumer looking them up by name is exactly who they are
for.

**They are Ginkgo containers carrying the identifier verbatim, not `func TestPrivacy_...`
symbols.** The package is a Ginkgo suite whose Gomega fail handler is registered inside
`RunSpecs`; a plain `Test` function in the same package would run outside that
registration and, if it ran first, would panic rather than fail. The container form keeps
the repo's Ginkgo requirement for full-stack integration tests, keeps the shared suite
harness, and leaves the token greppable — `rg -c` returns 3 for each.

The reattach spec bounds its read at **T4 itself**: the attach moment the production
Subscribe handler stamps on `REPLAY_COMPLETE`, which is what a real client passes as
`not_after_ms` on reconnect backfill. That makes the read genuinely the specification's
`[T0, T4]` window rather than an arbitrary future bound. The attach moment is asserted
non-zero first, because the legacy zero sentinel would silently make the bound unbounded
and the window claim false.

The expiry spec reaches the expired state the way production does — detach through the
`Disconnect` RPC, backdate with `DetachAndExpireSession`, drive the real `session.Reaper`
— because the specification's own wording is that the reaper deleted the session.

## The floor-preservation arm: issue #4682, and the annotation earned rather than inherited

**Issue #4682** ("iwzt: I-PRIV-6 floor-preservation arm") was confirmed **OPEN** before
the work started, and is closed by this plan's PR.

The invariant reads: *"ABAC staff override bypasses the hard-gate location-match only,
NOT the temporal floor."* The new spec asserts **both clauses in one read**: staff query
a location they are not in, the query **succeeds** (the location match was bypassed), and
it returns the event above their own arrival while omitting the one below it.

**`// Verifies: INV-PRIVACY-6` was added, and the binding flipped `pending` → `bound`**
with `asserted_by` naming the file. That decision was made against the invariant's own
summary in `docs/architecture/invariants.yaml`, not against the sibling spec's
annotation. It is explicitly **not** a partial binding on a multi-clause invariant — the
concern `.claude/rules/invariants.md` raises, and the one INV-PRIVACY-6's own stale
comment recorded — because one spec covers both clauses. `docs/architecture/invariants.md`
was regenerated with `go run ./cmd/inv-render`; the three binding meta-tests pass (7
tests, non-vacuous).

The access-engine question resolved to the **harness default**, and that is not
incidental: the default is what grants `read_unrestricted_history`, so it is what supplies
the override under test. Demonstrated rather than asserted — negative control G ran the
block against a deny-all engine and it failed the gate-bypass clause with "not authorized
to read stream".

## Load-bearing demonstrations — seven controls, every one observed failing

No guard was accepted without seeing it fail, and every failure is attributed **by
construction**: only the specs asserting the broken property failed. Each break was
reverted and the suite re-run green.

| # | Guard | Break applied | Observed failure |
|---|---|---|---|
| A | the move advances the floor *for the read* | reset the row's arrival to the original AFTER the bookkeeping was read | **exactly the 3 move specs**, on the exclusion assertion, with the read returning **2 frames** — so the positive control passed and the query was demonstrably live, not empty |
| B1 | the transport drop is REAL | no-op'd `DetachTransport` in the blip helper | **exactly the 3 helper-based blip specs**, on "the transport MUST really be gone during the gap" |
| B2 | the blip preserves the floor | reset the arrival AFTER the unchanged-floor assertion read it | **exactly the 3 helper-based blip specs**, on the gap-event assertion |
| C | the sibling session survives | detached the steady session too | **exactly 1 spec**, on "the concurrent session MUST stay ACTIVE" |
| D | reattach preserves the floor (named spec) | reset the arrival after T4's assertion | **exactly 1 spec**, on the T1 inclusion |
| E | expiry hands over no floor (named spec) | gave the fresh session the expired one's floor | **exactly 1 spec**, on the `[T0, T3)` exclusion |
| F | INV-PRIVACY-6 floor clause | lowered the staff arrival below the planted event | **exactly 1 spec**, on the floor-preservation assertion |
| G | INV-PRIVACY-6 override is genuinely exercised | ran the block against a deny-all engine | **exactly 1 spec**, on the gate-bypass clause: "not authorized to read stream" |

Controls A, B2, D and E share a deliberate shape, taken from 09-14's control 5: the break
is applied **after** the bookkeeping assertion has read the row, so the bookkeeping still
looks right and the failure lands on the assertion under test. Breaking the row before the
read would fail an earlier guard and prove that guard instead.

## Deviations from Plan

**1. [Rule 3 — blocking] Task 2's `ExpireSession` guard could never pass.**

- **Found during:** Task 2 acceptance checks.
- **Issue:** the criterion requires `rg -c 'ExpireSession' test/integration/session/` to
  return **no matches**. It returns **10** — and returned 10 before this plan, because
  `DetachAndExpireSession`, the helper the same task MANDATES, contains that substring
  (8 hits) and the file's prose names the trap helper twice. The guard as written fails on
  a correct implementation. Same class as 09-20's deviation 1.
- **Fix:** replaced with a falsifiable equivalent that separates use from substring:
  `rg -c '\bts\.ExpireSession\('` must return **0** (it does; exit 1) while
  `rg -c 'DetachAndExpireSession'` returns 8. The plan's actual intent — the trap helper is
  not used — is satisfied and verified, and the new needle would fire on a real use.

**2. [Rule 3 — blocking] Task 3's `WithRealABAC` guard was tripped by my own doc comment.**

- **Found during:** Task 3 acceptance checks.
- **Issue:** the criterion requires `rg -c 'WithRealABAC' test/integration/privacy/` not to
  increase. Baseline was 0; my explanatory comment ("`WithRealABAC` / `WithPolicyEngine`
  are the opt-ins the denial specs use") took it to 1 without any use. A needle that counts
  mentions cannot distinguish a future real use from prose — the guard would have been dead
  from this commit onward.
- **Fix:** reworded the comment to name the opt-ins descriptively rather than literally, and
  said in the comment why. Count is back to 0, and the guard stays falsifiable.

**3. Two multi-session specs use a combined cleanup rather than two `DeferCleanup(sess.Logout)`.**

- **Found during:** the first Task 1 run — the multi-session move spec panicked in teardown.
- **Issue:** `Session.Logout` calls the production Logout RPC with the player-session token,
  which invalidates it and cascades away EVERY game session it owns. Registering the suite's
  usual per-session cleanup for both sessions therefore fails the second with
  `SESSION_NOT_FOUND` (`internal/auth/auth_service.go:274-285`). Same root cause as 09-14's
  deviation 5, different shape.
- **Fix:** `logoutSharedPlayerSession` drops the sibling's transport through the production
  Disconnect RPC — so no Subscribe goroutine outlives the spec — then logs out once. Both
  calls are idempotent against a spec that failed partway through.

**4. [Rule 2] Built a harness seam the plan's file list does not carry.**

- **Found during:** designing the multi-session cells, before Task 1.
- **Issue:** the plan's `files_modified` lists only the two test files, but neither
  multi-session cell can express "two concurrent sessions of one player session" without a
  route to a second character on one player. The available shape — two separate players —
  makes the blip cell's central risk undetectable (see above).
- **Fix:** added `AuthedPlayer.AdditionalCharacter` to
  `internal/testsupport/integrationtest/harness.go` (additive; no existing symbol changed).
  Proven load-bearing by negative control C. Precedent: 09-14 deviation 1.

**5. `test/session-matrix.yaml`, `docs/architecture/invariants.yaml` and `.md` are modified, which the plan's file list omits.**

Identical to 09-13's and 09-14's deviation on the registry: the plan's stated deliverable is
to move rows off `planned`, and those rows live in the registry — a spec carrying a marker
whose row still says `planned` fails 09-16's bijection. The invariant files follow from
Task 3's own instruction to add `asserted_by` and are regenerated, not hand-edited.

**6. A plan factual claim corrected in the artifacts.**

The plan asserts a meta-test pins the two privacy test names. None exists; see above. The
names were kept regardless, and the spec file records both the finding and the reason.

## Verification

| Gate | Result |
|---|---|
| `task test:int -- ./test/integration/session/...` | exit 0 |
| `task test:int -- ./test/integration/privacy/...` | exit 0 |
| Session specs actually registered | **42 It specs, all passed** (Ginkgo JSON report; pre-plan count was 33, so +9) |
| `task test:int` (full lane) | exit 0 — 10837 tests, 7 skipped |
| `task test` | exit 0 |
| `task lint` | exit 0 |
| `task fmt` | exit 0; mutations committed |
| `task test -- ./test/meta/` | exit 0 — 103 tests |
| Invariant binding meta-tests | exit 0 — 7 tests (non-vacuous count, so the `-run` filter matched) |
| Registry shape | 48 rows; 32 spec / 1 planned / 10 n/a / 2 covered-elsewhere / 3 not-implementable |
| Marker ↔ registry bijection | 32 ↔ 32; `diff` empty over two NON-EMPTY sets; no duplicates |
| `\bts\.ExpireSession\(` in the session suite | 0 (exit 1) — the trap helper is unused |
| `time.Sleep` in the session suite | 0, unchanged from the pre-plan baseline of 0 |
| `WithRealABAC` in the privacy suite | 0, unchanged from the pre-plan baseline of 0 |
| Commit deletions | none (`git diff --diff-filter=D` empty for all three commits) |
| Working tree | clean |

Spec counts were read from `-ginkgo.json-report`, not parsed from `gotestsum --format
pkgname`, which prints no spec counts and so cannot detect a spec silently vanishing
(09-11 finding). The scoped `task test:int -- ./test/integration/<domain>/...` form worked
again — a seventh confirmation that the claim in `plan-review-learnings.md` that
`test:int` ignores `--` args is false.

Neither of the two audit-projection `Eventually` flakes recorded by 09-20 reproduced on
this plan's full-lane runs.

## Self-Check: PASSED

- `test/integration/session/lifecycle_privacy_floor_test.go` — FOUND
- `internal/testsupport/integrationtest/harness.go` — FOUND (modified)
- `test/session-matrix.yaml` — FOUND (modified)
- `test/integration/privacy/privacy_test.go` — FOUND (modified)
- `docs/architecture/invariants.yaml` — FOUND (modified)
- `docs/architecture/invariants.md` — FOUND (regenerated)
- Commit `2143da48c` — FOUND
- Commit `7d79f08d1` — FOUND
- Commit `3f6ad74b0` — FOUND
- Issue #4682 — confirmed OPEN before work started
- Issue #4788 — confirmed OPEN; cited, not duplicated

## Known Stubs

None. No stub, placeholder, skipped test or unrun `<verify>` was introduced. Every negative
control was reverted and its absence verified (`rg -n 'NEGCTRL'` exits 1 across `test/` and
`docs/`).

The one uncovered matrix cell, `reattach-cas.multi-session`, is untouched by this plan and
remains `planned` with `blocked_on` naming the missing per-connection-detach seam and
`owed_by: unassigned` naming the absence of an owner — first-class registry states, visible
to the meta-test, not silent gaps.

The production movement-lifecycle gap is recorded in three registry rows and cited to issue
#4788. It is a gap in the SYSTEM's coverage, not a stub introduced here.

## Note on QUAL-04 status

QUAL-04 is marked **Complete** by this plan. It is the last of 09-12, 09-13, 09-14, 09-15
and 09-20 to land, and 09-20's note asks the last of them to mark it. The requirement's
subject — "a session-lifecycle test matrix covers the connect / reconnect / multi-character
/ idle-timeout paths" — is delivered: 47 of the 48 matrix positions now carry a spec, a
verified covered-elsewhere pointer, or a committed non-applicability, and the 48th carries
a named blocker rather than silence. The source work item's own acceptance bar of ≥15 specs
in `./test/integration/session/...` is exceeded at 42.

09-16 still has to land the bijection meta-test that makes the registry's claims
build-enforced rather than merely checked by hand here.

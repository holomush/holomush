---
phase: 09-test-quality-code-health-sweep
plan: 14
subsystem: test-quality
tags: [QUAL-04, session-lifecycle, termination, telnet, guest-reauth]
status: complete
requires:
  - 09-12 (registry contract, marker form, lifecycleHarness)
  - 09-20 (WithBuiltinCommands, OpenTelnetSession, DetachAndExpireSession seams)
  - 09-13 (the guest re-authentication gap this plan inherited)
provides:
  - "test/integration/session/lifecycle_termination_test.go — 7 specs for quit, explicit logout and tmux-style telnet reattach"
  - "Server.GuestPlayer — the guest counterpart of Server.AuthedPlayer, unblocking guest re-authentication"
  - "Matrix rows quit-command.{web-guest,web-char,telnet}, explicit-logout.{web-guest,web-char,telnet}, telnet-tmux-reattach.telnet, reattach-select.web-guest, post-ttl-relogin.web-guest"
affects:
  - 09-16
tech-stack:
  added: []
  patterns:
    - "A rejected stand-in is excluded BY CONSTRUCTION — an assertion the stand-in cannot satisfy — rather than by convention in a comment"
    - "Read the transport identity back from Postgres BEFORE a termination, because connection rows CASCADE away with the session"
    - "A guard that a completed plan owes a cell is a false-assurance artifact; owed_by: unassigned is the honest state"
key-files:
  created:
    - test/integration/session/lifecycle_termination_test.go
  modified:
    - internal/testsupport/integrationtest/harness.go
    - test/integration/session/lifecycle_attach_test.go
    - test/integration/session/lifecycle_ttl_test.go
    - test/session-matrix.yaml
decisions:
  - "Built the guest re-authentication seam (Server.GuestPlayer) rather than leaving both reassigned guest cells planned"
  - "Did NOT build per-connection detach; reattach-cas.multi-session stays planned with owed_by: unassigned"
  - "No administrator-boot spec: the registry rows already carried the resetpassword --kick citation and both gaps, reconciled against the source directly"
  - "The one-session-per-player-session count is taken across every status, and its limited independent falsifiability is stated rather than dressed up"
metrics:
  duration: 96m
  tasks: 3
  files: 5
  completed: 2026-07-26
---

# Phase 09 Plan 14: Deliberate-Termination and Telnet-Reattach Matrix Rows Summary

Nine of the ten cells owed to this plan now have specs, including both guest cells that
09-12 and 09-13 left blocked — this plan built the missing seam rather than accepting the
stand-in they rejected. The tenth stays `planned` and is now honestly recorded as owed by
nobody.

## Cells landed — 9 of 10

| Matrix row | Container | Landed |
|---|---|---|
| `quit-command.web-guest` | The quit command ends the session rather than detaching it | yes |
| `quit-command.web-char` | " | yes |
| `quit-command.telnet` | " | yes |
| `explicit-logout.web-guest` | Explicit logout deletes the game sessions the player session owns | yes |
| `explicit-logout.web-char` | " | yes |
| `explicit-logout.telnet` | " | yes |
| `telnet-tmux-reattach.telnet` | Tmux-style telnet reattach under one player session | yes |
| `reattach-select.web-guest` | Reattach within TTL through the character-selection path | **yes — was blocked** |
| `post-ttl-relogin.web-guest` | Logging in again after expiry produces a genuinely fresh session | **yes — was blocked** |
| `reattach-cas.multi-session` | — | **NO — `planned`, `owed_by: unassigned`** |

Registry after this plan: **25 spec** (was 16), **8 planned** (was 17), 10 not-applicable,
2 covered-elsewhere, 3 not-implementable. 48 rows, unchanged. Marker ↔ spec-row bijection
verified exact: 25 markers, 25 `spec` rows, `diff` of the two sorted sets empty, no
duplicate marker anywhere in `test/`.

## The two reassigned guest cells: the seam was built

09-13 handed these over with a precise diagnosis, and it still held on re-verification:
`Server.ConnectGuest` mints a **new** guest player and character on every call
(`harness.go`, CreateGuest then SelectCharacter in one body), `Session.playerSessionToken`
is unexported with no accessor, and `Server` exposes no `coreServer` handle the test
package could call `SelectCharacter` through. `Server.AuthedPlayer` — the handle that does
allow repeated opens — creates a **registered** player.

09-13 rejected the only available stand-in ("connect a second guest") because it satisfies
*different session id* and *later arrival timestamp* trivially, with nothing for the
transition under test to have done. That judgement was right, so this plan did not revisit
it; it removed the constraint instead.

**`Server.GuestPlayer(ctx) *AuthedPlayer`** (`internal/testsupport/integrationtest/harness.go`)
provisions a guest player + starter character via the production `CreateGuest` RPC and
returns an `AuthedPlayer` handle **without** opening a session — the guest counterpart of
`Server.AuthedPlayer`. The guest's own bearer token then drives `OpenWebSession` /
`OpenTelnetSession` repeatedly, re-entering production `SelectCharacter` for the **same**
guest character. The guest player id is read from the `characters` row, because it is not
on the `CreateGuest` response and no session row exists yet to take it from.

**The rejected stand-in is now excluded by construction, not by convention.** Both specs
assert `second.CharacterID == guest.CharacterID`. A second guest fails that assertion, and
negative control 4 below demonstrates it failing — so the stand-in cannot be reintroduced
by a later edit without the suite going red.

`post-ttl-relogin.web-guest` also carries guest-specific content the web-char arm cannot:
the INV-PRIVACY-2 guest identity floor is the **character's** creation time, so it is
asserted **unchanged** across the re-login while the INV-PRIVACY-1 arrival floor **moved**.
The guest floor is additionally asserted to sit *below* the arrival floor, because
`streamScopeFloor` applies the guest overlay only when it is the later of the two
(`internal/grpc/scope_floor.go:61-64`) — so the spec establishes that the arrival floor is
demonstrably what governs its history reads rather than assuming it.

## The cell NOT claimed, and why `owed_by` changed

**`reattach-cas.multi-session`** stays `planned`. Its blocker is a third, larger seam:
`integrationtest.Session` models exactly one transport, and `DetachTransport` calls the
production `Disconnect` RPC, which detaches the **whole session** rather than one
connection. Exercising a reattach while a sibling connection stays live needs two
concurrent Subscribe streams on one session with only one dropped. Neither this plan's
tasks nor 09-20's carry that, and 09-15 covers the move-arrival and wifi-blip rows — so no
remaining plan in the phase owns it.

`owed_by` was therefore changed from `"09-14"` to `unassigned`. Leaving a completed plan
named as the ower would be false the moment this plan closed, which is the exact
false-assurance artifact this registry exists to prevent. The row's `notes` say all of
this in full, so 09-16 locks a registry that is honest about the gap rather than one that
points at a closed plan.

## The administrator-boot row: no spec, and the reason re-verified at source

No spec was written for `admin-boot.{web-char,telnet,multi-session}`, and **nothing in this
plan's specs, comments, registry edits or this SUMMARY asserts that an administrator
session-boot entry point does not exist.** One does.

`internal/command/handlers/resetpassword.go` was read directly rather than trusted through
09-20's paraphrase, and the paraphrase checks out:

- `resetpassword.go:27` — usage string `resetpassword <username> [password] [--kick]`.
- `resetpassword.go:35` — `capSessionKick = {Action: "admin", Resource: "session", Scope: ScopeGlobal}`, checked at `:124`.
- `resetpassword.go:197-218` — on `--kick`, lists the target player's characters and calls
  `exec.Services().Session().DeleteByCharacter` per character.

Both gaps confirmed and unchanged:

1. **Semantic.** `DeleteByCharacter` is a raw `DELETE ... RETURNING`
   (`internal/store/session_store.go:813-827`) that emits nothing, so `--kick` delivers the
   row-deleted half and **not** the `session_ended` half this row asserts: no
   `STREAM_CLOSED` reaches subscribers and no disconnect hooks fire. Tracked by **issue
   #4862**.
2. **Wiring.** `RegisterAdmin` panics on any nil dependency
   (`internal/command/handlers/register.go:14-23`) and needs five the harness does not wire.

**Registry check for the same defect: clean.** The three `admin-boot` rows already carried
the `resetpassword --kick` citation, both gaps, and issue 4862 — placed there by 09-12 from
09-20's disposition. No row asserts the capability is absent, and none needed correcting.
Nothing was propagated; there was nothing to propagate.

## Covered-elsewhere citation on this plan's rows: opened and confirmed

`explicit-logout.multi-session` is the one covered-elsewhere cell on this plan's rows. It
was opened, not trusted: `test/integration/auth/multi_tab_test.go:384-506`, "each call site
from spec §4.4.5 returns SESSION_NOT_FOUND after logout". It logs out in one tab and
retries four call sites from a second, each of which must reject the now-stale token —
which is this cell's transition. It asserts the session is gone from every *consumer
surface* rather than reading the deleted row back directly; the registry's `notes` already
record that limitation. The companion at `:508` ("Subscribe rejects a stale token with
SESSION_NOT_FOUND before sending any frame") pins the Subscribe call site. **Confirmed; not
re-implemented.**

## No implementation divergence from the matrix

Both deliberate-termination transitions behave as the matrix claims. `quit` and `logout`
each remove the session row **immediately** — asserted by a keyed `SESSION_NOT_FOUND`
lookup taken straight after the transition, with no expiry seam applied and no reaper
driven. Nothing had to be weakened to match the code, and no new issue was filed.

## Correction to this plan's own framing of the telnet row

Task 2's wording ("a telnet connection attached … a second telnet connection attached",
"a telnet client reconnecting resumes its session") reads as though it covers telnet
transport behaviour. It does not, and per 09-20's seam boundary it cannot: **no telnet
gateway is in the loop and `internal/telnet` is never entered.** The session is opened
through the same `SelectCharacter` RPC as a web session; the only production-observable
difference is `session_connections.client_type`, stamped by the Subscribe handler
(`internal/grpc/subscribe_handler.go:358-364`).

That is still load-bearing — the grid-presence roster counts connections by `client_type`,
so it decides who is visible to whom — but it is SESSION STATE, not protocol. The spec's
leading comment and the registry row both say so explicitly, in the same terms the registry
header already uses for every telnet cell. The plan's phrase "every telnet cell is
genuinely new coverage" is accurate as a statement about coverage novelty and is left
standing; what is corrected is only the protocol implication.

## Load-bearing demonstrations — five controls, every one observed failing

No guard was accepted without seeing it fail, and each failure is attributed **by
construction**: only the specs asserting the broken property failed, so a non-zero exit is
not being mistaken for "my check fired" (the 09-09 trap). Each break was reverted and the
suite re-run green.

| # | Guard | Break applied | Observed failure |
|---|---|---|---|
| 1 | quit really dispatches | dropped `WithBuiltinCommands()` from the shared harness | **exactly the 3 quit specs** failed on the surviving session row; the other 27 stayed green — **and `SendCommand` still returned nil**, confirming the `Succeed()` line is vacuous on its own |
| 2 | logout is not detach | replaced `sess.Logout(ctx)` with `sess.DetachTransport(ctx)` | **exactly the 3 logout specs** failed on `SESSION_NOT_FOUND`; a detach-based logout cannot pass them |
| 3 | telnet-ness is read, not asserted from the opener | ran the tmux spec against `OpenWebSession` | **exactly 1 spec** failed, on the client-type assertion: `["terminal"] to contain element telnet` |
| 4 | the rejected guest stand-in is excluded | replaced `guest.OpenWebSession` with `ts.ConnectGuest` in both guest specs | **exactly the 2 new guest specs** failed — the reattach spec on `Reattached: false to be true`, the re-login spec on the `CharacterID` mismatch |
| 5 | the guest history-floor exclusion is live | forced the fresh guest session's persisted floor back to the expired session's, leaving the harness struct un-refreshed so the ordering assertions still pass | **exactly 1 spec** failed, on the INV-PRIVACY-1 exclusion, with the read returning **2 frames** — so the positive control passed and the read was demonstrably live, not empty |

Control 5's shape is deliberate. Refreshing the harness struct after the break made the
spec fail one assertion *earlier* (`MUST carry a LATER arrival floor`), which proves that
guard live but says nothing about the exclusion. Leaving the struct un-refreshed reproduces
how a real production bug would present — bookkeeping looks right, the read is wrong — and
pushes the failure onto the assertion under test. That is also the 09-13 lesson applied:
the exclusion is only trustworthy because a positive-control event rides in the same query,
and the failure output shows it did.

## An honesty note on the one-session-per-player-session count

The tmux-reattach spec asserts the owning player session holds exactly one `sessions` row.
That assertion is weaker than it looks and the spec says so rather than dressing it up: the
partial unique index `idx_sessions_active_character`
(`internal/store/migrations/000001_baseline.up.sql:221`) already forbids two rows for one
character while either is active or detached, so a count restricted to those statuses could
not rise above one and would be decorative. The count is therefore taken across **every**
status, which leaves the "old row parked elsewhere, new row minted alongside it" shape
detectable. The load-bearing assertions on that row remain the same session identifier, the
unchanged arrival timestamp, the new connection id (with the dropped connection's row
asserted gone first), and the `client_type` read back from Postgres.

## Deviations from Plan

**1. [Rule 2 — missing critical functionality] Built a harness seam the plan's file list does not carry.**

- **Found during:** the reassigned-cells assessment, before Task 1.
- **Issue:** `reattach-select.web-guest` and `post-ttl-relogin.web-guest` were reassigned to
  this plan, but the plan's `files_modified` lists only
  `test/integration/session/lifecycle_termination_test.go`. Covering them from a test file
  alone is impossible — the blocker is the absence of a harness route that re-authenticates
  the same guest. The two honest options were to build the seam or to leave both cells
  `planned`; leaving them planned was explicitly acceptable, but the seam turned out to be
  small and purely additive.
- **Fix:** added `Server.GuestPlayer` to `internal/testsupport/integrationtest/harness.go`
  (additive; no existing symbol changed) and wrote the two specs into the containers that
  already own their rows, in `lifecycle_attach_test.go` and `lifecycle_ttl_test.go`. Proven
  by negative controls 4 and 5.

**2. [Rule 3 — blocking] `test/session-matrix.yaml` is modified, which the plan's file list omits.**

- **Found during:** Task 1 commit.
- **Issue:** identical to 09-13's deviation 2. The plan's stated deliverable is to move
  registry rows off `planned`, and those rows live in the registry. A spec carrying a marker
  whose row still says `planned` fails 09-16's bijection.
- **Fix:** modified the registry, which is the plan's deliverable in substance.

**3. `reattach-cas.multi-session` reassigned `owed_by: "09-14"` → `unassigned`.**

Covered in full above. This is a deliberate, documented widening of the `owed_by` value
space; the shape test's only requirement is that the payload key is present, which it is.

**4. Plan wording on the telnet row corrected in the artifacts.**

Covered in full above under "Correction to this plan's own framing of the telnet row".

**5. Logout specs use a guarded cleanup rather than the suite's usual `DeferCleanup(sess.Logout)`.**

- **Found during:** Task 1 design.
- **Issue:** `auth.Service.Logout` resolves the token before deleting it and returns
  `SESSION_NOT_FOUND` when it is already gone (`internal/auth/auth_service.go:274-285`),
  which `Session.Logout` turns into a `require.NoError` failure. A spec that both logs out
  and registers the standard cleanup would fail in teardown.
- **Fix:** a local `logoutOnce` helper registers the cleanup and suppresses it once the
  spec's own logout has run — so the cleanup still fires if the spec fails **before**
  reaching its logout, which is when it is actually needed. The quit specs keep the plain
  cleanup: quit ends the game session only, so the player-session credential survives and
  `Logout` still has work to do.

## Verification

| Gate | Result |
|---|---|
| `task test:int -- ./test/integration/session/...` | exit 0 |
| Specs actually registered | **33 It specs, all passed** (Ginkgo JSON report; pre-plan count was 24, so +9) |
| `task test:int` (full lane) | exit 0 — 10837 tests, 7 skipped |
| `task test` | exit 0 — 10412 tests, 4 skipped |
| `task lint` | exit 0 |
| `task fmt` | exit 0; mutations committed |
| `task test -- ./test/meta/` (registry shape) | exit 0 — 103 tests |
| Registry shape | 48 rows; 25 spec / 8 planned / 10 n/a / 2 covered-elsewhere / 3 not-implementable |
| Marker ↔ registry bijection | 25 markers ↔ 25 `spec` rows; `diff` of the sorted sets empty; both sets non-empty, so the empty diff is evidence rather than a query that matched nothing |
| Duplicate markers | none — every marker occurs exactly once across `test/` |
| `time.Sleep` in the session suite | 0, unchanged from the pre-plan baseline of 0 |
| Commit deletions | none (`git diff --diff-filter=D` empty for all three commits) |

Spec counts were read from `-ginkgo.json-report`, not parsed from `gotestsum --format
pkgname`, which prints no spec counts and so cannot detect a spec silently vanishing (09-11
finding). The scoped `task test:int -- ./test/integration/<domain>/...` form worked again —
a sixth confirmation that the claim in `plan-review-learnings.md` that `test:int` ignores
`--` args is false.

Neither of the two audit-projection `Eventually` flakes recorded by 09-20 reproduced on this
plan's full-lane run.

## Self-Check: PASSED

- `test/integration/session/lifecycle_termination_test.go` — FOUND
- `internal/testsupport/integrationtest/harness.go` — FOUND (modified)
- `test/integration/session/lifecycle_attach_test.go` — FOUND (modified)
- `test/integration/session/lifecycle_ttl_test.go` — FOUND (modified)
- `test/session-matrix.yaml` — FOUND (modified)
- Commit `34e97fca1` — FOUND
- Commit `1115660b7` — FOUND
- Commit `1044ce870` — FOUND

## Known Stubs

None. No stub, placeholder, skipped test or unrun `<verify>` was introduced. The one
uncovered cell is recorded as `planned` with `blocked_on` naming the missing seam and
`owed_by: unassigned` naming the absence of an owner — first-class registry states, visible
to the meta-test, not silent gaps.

## Note on QUAL-04 status

QUAL-04 remains **Pending**, following 09-12, 09-13 and 09-20. This plan delivers 3 of the
matrix's 12 transition rows plus the two reassigned guest cells; 8 rows are still `planned`
across 09-15 (7) and unassigned (1), and 09-16 locks the bijection. The last of 09-15/09-16
to land should mark it.

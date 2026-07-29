---
phase: 09-test-quality-code-health-sweep
plan: 12
subsystem: test-quality
tags: [QUAL-04, session-lifecycle, registry, integration-harness]
status: complete
requires:
  - 09-07 (EmitDirectEventAt seam)
  - 09-20 (telnet client-type seam, reaper-selectable row, WithBuiltinCommands)
provides:
  - "test/session-matrix.yaml — the 48-row session-lifecycle matrix registry"
  - "Marker convention `// matrix-row: <id>`, bijection key field `id`"
  - "TestSessionMatrixRegistryShape — the registry's shape guard"
  - "lifecycleHarness() — the suite-scoped in-process stack for session specs"
  - "Specs for matrix rows 1-3 (fresh selection, both reattach paths)"
affects:
  - 09-13
  - 09-14
  - 09-15
  - 09-16
tech-stack:
  added: []
  patterns:
    - "A registry row never claims coverage that does not exist: `planned` is a first-class disposition, not an absence"
    - "Each disposition owns exactly one payload key, so 'exactly one disposition' is checkable rather than promised"
    - "Every guard is observed failing before it is accepted, attributed to its own assertion by name"
key-files:
  created:
    - test/session-matrix.yaml
    - test/meta/session_matrix_registry_test.go
    - test/integration/session/lifecycle_harness_test.go
    - test/integration/session/lifecycle_attach_test.go
  modified:
    - test/integration/session/session_persistence_suite_test.go
decisions:
  - "Added a fifth disposition, `planned`, so no row claims a spec before that spec is written"
  - "The registry follows the source TABLE, not 09-RESEARCH's derived column totals, which disagree with the table they annotate"
  - "multi_tab_test.go:217/242 is NOT cited: it creates no game session, so it cannot cover a reattach cell"
  - "reattach-cas.web-guest gets its own spec rather than citing the presence suite, which covers the cell only partially"
metrics:
  duration: 26m
  tasks: 3
  files: 5
  completed: 2026-07-26
---

# Phase 09 Plan 12: Session-Lifecycle Matrix Registry Summary

The session-lifecycle matrix is now a committed, machine-read registry with a shape test that
lands with it, a suite-scoped in-process harness the lifecycle specs share, and passing specs
for the connect and both reconnect transitions.

## What plans 09-13, 09-14, 09-15 and 09-16 need from this

**Bijection key field: `id`.** Formed as `<transition-slug>.<column-slug>`, e.g.
`reattach-cas.telnet`. Unique across all 48 rows.

**Marker form — exactly this, on its own line immediately above the `It(`:**

```go
// matrix-row: reattach-cas.telnet
It("returns a detached telnet session to active with a fresh telnet-typed connection row", func() {
```

**Harness accessor: `lifecycleHarness()`** in `test/integration/session/lifecycle_harness_test.go`.
It returns the one `*integrationtest.Server` shared by the whole suite, started in the suite's
existing `BeforeSuite` (`startLifecycleHarness`) and stopped in its `AfterSuite`. Call it from a
`BeforeEach` or from inside a spec; never start your own.

**Per-session teardown is YOUR obligation.** `Server.Stop` stops only the plugin subsystem — it
does not log sessions out or tear down their transports. Every spec that opens a session
registers its own cleanup from inside that spec:

```go
sess := lifecycleHarness().ConnectGuest(ctx)
DeferCleanup(func() { sess.Logout(ctx) })
```

`DeferCleanup` is correct *there* (a session belongs to one spec) and catastrophic for the
harness itself — a `DeferCleanup` registered from an `It` or `BeforeEach` fires at that spec's
end, which is why the harness is started from `BeforeSuite` instead.

**Harness options in force:** `WithBuiltinCommands()` only. Not `WithInTreePlugins` (needs
binary plugin artifacts these specs have no use for), not `WithRealABAC` (the permissive default
is what session-state specs need). If your plan needs real policy, do not silently add it —
these specs depend on the permissive default.

## The registry's shape, and what each state means

`test/session-matrix.yaml` — 48 rows, one per position in the 12×4 matrix. Five dispositions,
each owning exactly one payload key so that "exactly one disposition" is a checkable property:

| disposition | payload key | meaning | count |
|---|---|---|---:|
| `spec` | `spec` | A spec exists in this repo AND carries the marker | 8 |
| `covered-elsewhere` | `covered_by` | A pre-existing spec genuinely exercises it | 2 |
| `planned` | `owed_by` | No spec yet; a named later plan owes it | 25 |
| `not-applicable` | `reason` | The question does not arise | 10 |
| `not-implementable-from-harness-defaults` | `gap_notes` | The system answers, but not with these semantics and not by any reachable route | 3 |

`notes` and `issue` are free-form and permitted on any row; neither is a payload key, so neither
can imply a disposition.

### Rows owed to later plans

| Plan | Rows owed | Cells |
|---|---|---:|
| 09-13 | `detach-all`, `reaper-sweep`, `post-ttl-relogin` | 9 |
| 09-14 | `quit-command`, `explicit-logout`, `telnet-tmux-reattach`, plus 2 blocked cells below | 10 |
| 09-15 | `move-arrival`, `wifi-blip` | 7 |

### Two cells are blocked on a missing harness seam

Both are `planned` with a `blocked_on` naming the exact gap. Neither is a coverage decision you
should quietly reverse:

- **`reattach-select.web-guest`** — no seam re-runs `SelectCharacter` for a guest.
  `ConnectGuest` does CreateGuest + SelectCharacter in one call and keeps the guest's
  player-session token unexported; `AuthedPlayer`, the handle that *does* allow repeated
  `OpenWebSession`, creates a **registered** player. Needs a guest equivalent of `AuthedPlayer`,
  or an exported re-select on `Session`.
- **`reattach-cas.multi-session`** — `integrationtest.Session` models exactly one transport, and
  `DetachTransport` calls the production `Disconnect` RPC, which detaches the whole session
  rather than one connection. Needs per-connection detach.

## The administrator-boot row

Three cells (`admin-boot.web-char`, `.telnet`, `.multi-session`) carry
`not-implementable-from-harness-defaults`, derived from **09-20's SUMMARY §"Administrator-boot
row disposition"**, and reconciled against `internal/command/handlers/resetpassword.go` directly.
Each cites `resetpassword --kick` (`resetpassword.go:197-218`) as a real, capability-gated
administrator session-boot path that **does exist**, plus both of its gaps:

1. **Semantic** — `--kick` calls `DeleteByCharacter`, a raw `DELETE ... RETURNING`
   (`session_store.go:813-827`) that emits nothing, so it delivers the row-deleted half and not
   the `RecordBootedSession` / `session_ended` half the row asserts. **Issue #4862** tracks this
   `session_ended` semantic gap — not a missing capability.
2. **Wiring** — `RegisterAdmin` panics on any nil dependency (`register.go:14-23`) and needs five
   the harness does not wire.

The two gaps have different remedies and the notes say so: closing the wiring gap alone would
make the command reachable while still delivering the wrong semantics, so a spec written against
it would go green on a transition the system does not perform.

**No row asserts the capability is absent.** Verified two ways, because a line-wise `rg` cannot
see a phrase split across a YAML fold boundary: the line-wise scan returned no matches, and a
second scan over the file flattened to a single line also returned none. That second pattern was
positive-controlled — it fires on the probe string *"there is no administrator boot entry point
in this tree"* — so the empty result is evidence of absence rather than of a query that did not
match. The registry positively asserts the path **DOES exist** (`resetpassword` appears 5 times).

## Covered-elsewhere pointers: every one opened and confirmed

| Cell | Cited spec | Verdict |
|---|---|---|
| `reattach-select.multi-session` | `multi_tab_test.go:81/106` "both tabs reattach to one session…" | **Confirmed.** Asserts `GetReattached()==true` and both tabs return the same session id. Does *not* assert arrival-timestamp preservation — recorded in the row's notes. |
| `explicit-logout.multi-session` | `multi_tab_test.go:386/411` "each call site … returns SESSION_NOT_FOUND after logout" | **Confirmed.** Logout in one tab, four call sites retried from a second. Asserts the session is gone from every consumer surface rather than reading the deleted row directly — recorded. Companion at `:508` pins the Subscribe call site. |

### One candidate REJECTED — a false claim in the research doc

09-RESEARCH.md's D-16 table lists `multi_tab_test.go:217/242` ("browser cookie + concurrent
telnet auth") as covering *"Reattach × telnet + multi-session"*. **It does not.** Reading the
body (lines 242-279): it authenticates twice and asserts `len(player_sessions) >= 2`. It never
calls `SelectCharacter`, so **no game session is ever created** and no reattach occurs — the
`"reattach holds"` in its name is not backed by any assertion in it. The research doc hedged
this whole table with *"plausibly covers 4–5 of them"*; the plausible reading was wrong. Citing
it would have produced exactly the overstated-coverage artifact this registry exists to prevent.
That cell is `planned`, not covered.

Similarly, `presence/reattach_presence_test.go:42/65` drives the same detach/reattach sequence
for a guest but its subject is `grid_present` (I-LIVE-3); it asserts neither session identity nor
arrival-timestamp preservation. That is a **partial** cover, so `reattach-cas.web-guest` gets its
own spec and the presence suite is cross-referenced in the row's `notes` rather than claimed as
its disposition. (Precedent for refusing partial bindings: INV-PRIVACY-6.)

## Load-bearing demonstrations — every guard observed failing

No guard in this plan was accepted without seeing it fail. Each break was reverted and the suite
re-run green.

| Guard | Break applied | Observed failure |
|---|---|---|
| Row count | deleted `wifi-blip.multi-session` | "MUST carry one row per position…" — 47 ≠ 48 |
| Id uniqueness | duplicated `wifi-blip.web-char` | `duplicate row id "wifi-blip.web-char" at rows[46]; already used at rows[45]` |
| Disposition exclusivity | added `reason:` to a `spec` row | `row "fresh-select.telnet" declares disposition "spec" but also carries "reason", the payload of "not-applicable"` |
| Not-applicable ratchet | reclassified a `planned` cell as `not-applicable` | "MUST mark exactly the 10 positions the source matrix marks n/a" |
| `WithBuiltinCommands` wiring | dropped the option | throwaway quit spec timed out by name — **and `SendCommand` still returned nil**, confirming a no-error assertion would have been vacuous |
| Telnet client-type assertions | `OpenTelnetSession` → `clientTypeTerminal` | **exactly the 3 telnet specs** failed on `["terminal"]`; the other 13 stayed green |
| I-PRIV-3 arrival-floor assertions | made `ReattachTransport` bump the floor | **exactly the 3 reattach-CAS specs** failed on the I-PRIV-3 message; the reattach-select specs stayed green |

The last two are attributed by construction: only the specs asserting the broken property failed,
so a non-zero exit is not being mistaken for "my check fired" (the 09-09 trap).

## Deviations from Plan

**1. [Rule 2 — missing critical functionality] Added a fifth disposition, `planned`.**

- **Found during:** Task 1, writing rows for transitions 4-12.
- **Issue:** the plan says both *"Rows referencing specs that do not exist yet are correct at this
  point"* and *"Never mark a matrix cell as covered by a spec … without having confirmed that the
  named spec actually exists and actually exercises the transition."* These contradict: 25 rows
  belong to unwritten plans, so marking them `spec` would have the registry overstate coverage
  from the moment it is committed — for the several days until 09-16 lands.
- **Fix:** `planned` carries `owed_by` (and optional `blocked_on`), so the row still gives
  09-13/14/15 their division of labour without claiming coverage. When each spec is written its
  row flips `planned` → `spec` and gains a marker. 09-16 can additionally assert that no
  `planned` rows remain at phase end — a stronger completion gate than the bijection alone.

**2. [Rule 1 — bug] The registry follows the source TABLE, not 09-RESEARCH's column totals.**

- **Found during:** Task 1, counting n/a cells.
- **Issue:** 09-RESEARCH.md states column totals "web-guest 9, web-char 10, telnet 12,
  multi-session 7". Reading the verbatim table it annotates gives **9 / 11 / 12 / 6**. Both sum
  to 38, and both agree there are 10 n/a, but the per-column distribution differs. The prose also
  says n/a covers *"web-guest/web-char on rows 9/10"* (4 cells) whereas the table marks only
  web-guest there (2 cells) — and taking the prose literally yields 11 n/a, not 10.
- **Fix:** followed the table, which is the verbatim archive record and internally consistent.
  The 10 n/a positions are: multi-session on `fresh-select`, `detach-all`, `reaper-sweep`,
  `post-ttl-relogin`, `quit-command`; web-guest on `admin-boot`, `move-arrival`; and web-guest,
  web-char, multi-session on the telnet-only `telnet-tmux-reattach`. The count is pinned by a
  constant in the shape test, so a future reclassification fails the build.

**3. [Rule 3 — blocking] Modified `session_persistence_suite_test.go`, which the plan's file list omits.**

- **Found during:** Task 2.
- **Issue:** the plan directs the harness to start in *the suite's existing* `BeforeSuite` and
  stop in its `AfterSuite`, but lists only `lifecycle_harness_test.go` under `files_modified`.
  Ginkgo permits one `BeforeSuite` per suite, so the instruction is unsatisfiable without editing
  the bootstrap file.
- **Fix:** added two calls (`startLifecycleHarness()` / `stopLifecycleHarness()`) with a comment
  explaining the scope requirement. No existing behaviour changed.

**4. [Rule 1 — bug] Two acceptance criteria were unfalsifiable as written; replaced with
falsifiable equivalents.**

- `rg -c 'DeferCleanup' lifecycle_harness_test.go` returning **no matches** conflicts with the
  same task's instruction to state and demonstrate the per-session `DeferCleanup` obligation in
  that very file. Replaced with the criterion's stated intent, checked precisely: no
  `DeferCleanup` appears inside `startLifecycleHarness`, `stopLifecycleHarness` or
  `lifecycleHarness`. The scan strips comments first (so the doc-comment *example* is not
  miscounted) and asserts each function was actually located, so it cannot pass vacuously; it was
  positive-controlled by injecting a `DeferCleanup` and confirming detection.
- `rg -c 'WithRealABAC' test/integration/session/` returning **no matches** conflicts with the
  requirement that the doc comment *name that option with reasons*. This is the doc-comment-needle
  defect class. Replaced with a comment-stripped scan for **call sites**, positive-controlled on a
  real call and negative-controlled on the comment form actually present. Result: no call sites of
  `WithRealABAC` or `WithInTreePlugins`; the sole `Start` call passes `WithBuiltinCommands()` only.

**5. [Rule 1 — bug, in my own spec] Asserted `Info.IsGuest`, which production deliberately leaves false.**

- **Found during:** Task 3, first suite run — the guest spec failed.
- **Issue:** `SelectCharacter` intentionally does **not** set `session.Info.IsGuest`
  (`internal/grpc/auth_handlers.go:291-296`): `Disconnect` reads that flag to delete the session
  immediately, which would break page-reload reattach. The guest signal is the non-zero
  `GuestCharacterCreatedAt` (INV-PRIVACY-2).
- **Fix:** assert the field production actually uses. And a second trap surfaced underneath: the
  column round-trips as the **Unix epoch** when unset, not Go's zero `time.Time`, so
  `NotTo(BeZero())` would have passed on an unset floor and proved nothing. Observed the real
  values (guest `2026-07-27T00:01:30Z`, registered `1970-01-01T00:00:00Z`) before choosing the
  assertions: the guest floor is compared to the session's own `CreatedAt`, and the registered
  session's floor is asserted as `.Unix() == 0`. Both are now falsifiable in both directions.

## Verification

| Gate | Result |
|---|---|
| `task test:int -- ./test/integration/session/...` | exit 0 |
| Specs actually registered | **16 It specs, all passed** (Ginkgo JSON report; pre-phase count was 7, so +9) |
| `task test` | exit 0 — 10412 tests, 4 skipped |
| `task lint` | exit 0 |
| `task fmt` | exit 0; mutations committed |
| Full Task 1 gate (both `test -f` + `--- PASS:` pin + lint) | exit 0 |
| Registry shape | 48 rows, 10 not-applicable, no duplicate ids |
| Marker ↔ registry cross-check | 8 spec rows ↔ 8 markers, bijective; container/name text matches exactly; no duplicate marker anywhere in the repo |
| `RunSpecs` in the session package | exactly 1 — no second spec runner |
| `time.Sleep` in the session suite | none added (still 0) |

Spec counts were read from `-ginkgo.json-report`, not parsed from `gotestsum --format pkgname`,
which prints no spec counts and so cannot detect a spec silently vanishing (09-11 finding).
`task test:int -- ./test/integration/<domain>` scoping worked again, confirming a fourth time
that the claim in `plan-review-learnings.md` that it ignores `--` args is false.

## Self-Check: PASSED

- `test/session-matrix.yaml` — FOUND
- `test/meta/session_matrix_registry_test.go` — FOUND
- `test/integration/session/lifecycle_harness_test.go` — FOUND
- `test/integration/session/lifecycle_attach_test.go` — FOUND
- `test/integration/session/session_persistence_suite_test.go` — FOUND (modified)
- Commit `24c57a569` — FOUND
- Commit `91d5aa371` — FOUND
- Commit `28b537b81` — FOUND

## Known Stubs

None. No stub, placeholder, skipped test, or unrun `<verify>` was introduced. The two uncovered
cells are recorded as `planned` with `blocked_on` naming the missing seam — a first-class
registry state, not a silent gap, and visible to the meta-test.

## Note on QUAL-04 status

QUAL-04 remains **Pending**, following 09-20's reasoning. This plan delivers the registry, the
harness seam, and 3 of the matrix's 12 transition rows; 09-13, 09-14 and 09-15 carry the rest and
09-16 locks the bijection. 25 rows are still `planned`. Marking the requirement complete now
would be exactly the overstated-assurance artifact this phase exists to eliminate. The last of
09-13/14/15/16 to land should mark it.

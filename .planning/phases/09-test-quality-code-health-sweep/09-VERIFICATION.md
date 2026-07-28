---
phase: 09-test-quality-code-health-sweep
verified: 2026-07-27T20:15:00Z
status: gaps_found
score: 3/4 must-haves verified
behavior_unverified: 2
overrides_applied: 0
requirements:
  - id: QUAL-02
    ledger: Pending
    verdict: deliberate-documented-shortfall
    plans: [09-01, 09-08, 09-10, 09-17, 09-19, 09-21]
    evidence: "09-19-SUMMARY.md:223-233 rules it Pending; shortfalls measured, not smoothed. internal/tls 88.12% (floor met). cmd/holomush 70.09% (9.91 short, #4861 OPEN). Project 79.11% (0.89 short of D-02's 80%). Both halves of the D-04 gate deferred (#4875 project never posts; #4876 patch would deadlock docs-only PRs)."
  - id: QUAL-03
    ledger: Pending
    verdict: deliberate-documented-shortfall
    plans: [09-02, 09-09, 09-11, 09-18]
    evidence: "09-02-SUMMARY.md:166-174 sets the not-flipping-early protocol; 09-09-SUMMARY.md:213-224 states the honest scope (the zero-assertion predicate says nothing about misleading-but-passing tests); 09-19-SUMMARY.md:235 carries it forward. Residual tracked as #4860."
  - id: QUAL-04
    ledger: Complete
    verdict: verified-by-execution
    plans: [09-07, 09-12, 09-13, 09-14, 09-15, 09-16, 09-20]
    evidence: "42/42 specs passed, 0 skipped, exit 0 (run by this verifier). 48-row registry + bijection meta-test green inside a 109-test test/meta run, exit 0."
  - id: QUAL-05
    ledger: Pending
    verdict: deliberate-documented-shortfall
    plans: [09-02, 09-03, 09-04, 09-05, 09-06]
    evidence: "4 of the 5 arch-review Medium items landed in-tree and are read-verified. DEK read-cache deferred on #4792 (OPEN, carrying a rationale comment). The de-slop/humanization half of the requirement text was deferred with no placeholder issue, by explicit decision (09-02-SUMMARY.md:22,181)."
gaps:
  - truth: "Packages under the reconciled coverage bar are backfilled with genuine behavioral tests; the coverage gate passes repo-wide"
    status: partial
    reason: "The backfill half is real and measurable. The gate half does not exist: no coverage context is a required check on the protected branch, and codecov/project posts on no ref. cmd/holomush also remains 9.91 points below its named floor."
    artifacts:
      - path: ".codecov.yml"
        issue: "project status tightened to threshold 0% but posts nowhere (#4875); patch status posts but is not required (#4876)"
      - path: "cmd/holomush/"
        issue: "70.09% against a named 80% floor (#4861); residual concentrated in cmd_audit.go 26.83%, world_genesis.go 47.69%, kek_provision.go 50.66%, admin_client.go 50.81%"
    missing:
      - "cmd/holomush raised to 80%, or the floor renegotiated in ROADMAP/REQUIREMENTS rather than only in an issue"
      - "At least one coverage context enforced on ruleset 11923801, or SC1's 'gate passes repo-wide' clause amended to match the deferral"
behavior_unverified_items:
  - truth: "The ACE naming ratchet fails the build if a topic-style test name or vague subtest label regresses"
    test: "Rename a test function to a bare-topic final segment (e.g. TestFooBar -> TestFoo_Bar) and run task test -- ./test/meta/"
    expected: "TestACENamingRatchetFindsNoTopicStyleTestNames fails naming the offending declaration"
    why_human: "Verifying it requires mutating the tree, which this verification was scoped out of. Read-verified only: test/meta/ace_naming_registry_test.go:253-322 carries real require assertions plus non-vacuity floors (checked>1000, labels>500, seen>500), so an empty walk cannot pass as clean — but the failing-on-regression path was not exercised."
  - truth: "The session-matrix bijection meta-test fails on a seeded breakage (a spec row without a marker, or a marker on a non-spec row)"
    test: "Delete one `// matrix-row:` marker from test/integration/session/, or flip one row's disposition, then run task test -- ./test/meta/"
    expected: "TestSessionMatrixSpecRowsAndInCodeMarkersAreBijective or TestSessionMatrixRegistryShape fails"
    why_human: "Same mutation constraint. Read-verified only: test/meta/session_matrix_registry_test.go:359-451 pins specIDs == markerIDs with NotEmpty guards on both sides, and :302 pins all five disposition populations. The registry header claims each guard was demonstrated failing on a seeded breakage; that claim is a SUMMARY-class claim this verification did not reproduce."
notes:
  - "09-21-SUMMARY.md genuinely does not exist. Plan 09-21 did execute — PR #4874 (head 97e692dc6, merged 2026-07-28 as 4ab2b9868). Its must-haves were re-derived from the live API by this verifier rather than inferred. 09-17-SUMMARY.md:377 records the absence as blocking under its own Rule 3 and resolved it the same way."
  - "STATE.md:8 and :373 say 'all 21 plans executed'; ROADMAP.md:270 says '20/21' with 09-21's checkbox unticked. The artifact-level truth is between them: 09-21 executed, produced no SUMMARY."
  - "Only 4 of 20 SUMMARYs carry a requirements-completed frontmatter field (09-01, 09-08 = [QUAL-02]; 09-09, 09-10 = []). None lists QUAL-04, yet REQUIREMENTS.md marks QUAL-04 Complete. The ledger is right and the frontmatter is under-populated."
  - "#4793, #4796, #4797 are still OPEN although their fixes are in-tree and read-verified. Bookkeeping, not a code gap. #4794 (secure cookie) is correctly CLOSED."
  - "deferred-items.md records two load-dependent integration flakes (dlq_capture_integration_test.go:105, admin_read_stream_e2e_test.go:889) with no GitHub issue and no test/quarantine.yaml row. They are documented in the phase directory only."
---

# Phase 9: Test-Quality & Code-Health Sweep Verification Report

**Phase Goal:** Backfill coverage and remediate test/code-health debt to the reconciled bar.
**Verified:** 2026-07-27T20:15:00Z
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Packages under the reconciled coverage bar are backfilled with genuine behavioral tests; the coverage gate passes repo-wide | ✗ FAILED | Backfill real: `internal/tls` **88.12%** (codecov file report, merge SHA `4ab2b9868`). But `cmd/holomush` **70.09%** vs its named 80% floor, project **79.11%** vs 80%, and **no coverage context is a required check** — `gh api rulesets/11923801` returns [Build, Lint, Test, CodeRabbit, Integration Test, E2E Test, Conventional Commit, Vuln]. The gate does not pass repo-wide because it does not exist. |
| 2 | Skeleton/tautological tests are replaced with real assertions; ACE naming violations are gone (naming audit clean) | ✓ VERIFIED | `task test -- ./test/meta/` **exit 0**, 109 tests — includes both ACE ratchet tests. Assertion count across the 55 touched files 3154 → 3154 (09-18-SUMMARY.md:111); repo-wide `git diff 4ab2b9868^..4ab2b9868 -- '*_test.go'` = **+539 / −47** assertion lines. `internal/store/alias.go:52` carries the compile-time `var _ AliasRepository = (*PostgresAliasRepository)(nil)`. Four eventbus scaffolding files trimmed to 28–34 lines each, every `Skip()` citing an OPEN issue (#4853–#4856, confirmed live). |
| 3 | A session-lifecycle test matrix covers the connect / reconnect / multi-character / idle-timeout paths | ✓ VERIFIED | **Ran it**: `./test/integration/session/...` under `-tags=integration` → **Ran 42 of 42 Specs … SUCCESS! 42 Passed \| 0 Failed \| 0 Pending \| 0 Skipped**, exit 0. `test/session-matrix.yaml` = 48 rows (32 spec / 2 covered-elsewhere / 1 planned / 10 n/a / 3 not-implementable), all four paths populated (table below). |
| 4 | The arch-review Medium cluster is addressed or explicitly deferred with rationale | ✓ VERIFIED | 4 of 5 landed in-tree and read-verified; the 5th (DEK read-cache) deferred on #4792, confirmed OPEN with a rationale comment. Per-item table below. |

**Score:** 3/4 truths verified (0 present, behavior-unverified at the criterion level; 2 sub-properties listed under `behavior_unverified_items`)

### Criterion 1 — the coverage chain, checked against the authoritative service

The phase's headline claim is that an E2E coverage upload that had been landing empty for
~4 months now lands real. I did not re-run `task test:e2e:cover`; I checked the **outcome**
against codecov, which is stronger than re-running the producer.

| Query | Result |
|-------|--------|
| `commits/4ab2b9868…` (phase merge) | `coverage 79.11 · sessions 3 · state complete` |
| `commits/eee76d23e…` (a PR head 09-19 cited) | `coverage 79.11 · sessions 3 · state complete` |
| `commits/97e692dc6…` (PR #4874 head) | `coverage 79.11 · sessions 3 · state complete` |
| `commits/4ed921eed` (main tip today) | `coverage 79.10 · sessions 3 · state complete` |

Three sessions, not two — the de-duplication model `.codecov.yml` used to assert is gone and
the file now says so. The decisive per-file evidence that E2E coverage genuinely flows:
`cmd/holomush/core.go` **85.39%** and `cmd/holomush/sub_grpc.go` **76.08%**. Those are the two
files that were ignored precisely because their coverage "never flowed back"; they are now
un-ignored in `.codecov.yml` and carry real counts. A configuration change alone could not
produce those numbers.

**Where it falls short, measured not asserted:**

| Floor | Target | Actual (codecov, merge SHA) | Gap |
|-------|--------|------------------------------|-----|
| `internal/tls` | 80% | 88.12% (303 lines / 267 hits) | met |
| `cmd/holomush` | 80% | **70.09%** (3838 lines / 2690 hits) | −9.91 (#4861) |
| project | 80% (D-02) | **79.11%** | −0.89 |

`cmd/holomush`'s residual is exactly where #4861 says it is: `cmd_audit.go` 26.83,
`cmd_admin_totp_deps.go` 37.28, `world_genesis.go` 47.69, `kek_provision.go` 50.66,
`admin_client.go` 50.81, `crypto_rekey_wiring.go` 51.61, `outbox_admin.go` 51.78. The SUMMARY's
figures and mine agree to two decimal places; nothing was smoothed.

**The gate half.** SC1 says "the coverage gate passes repo-wide". Independently confirmed on the
merge commit via **both** GitHub endpoints:

- `commits/4ab2b9868/status` → statuses: `codecov/patch success` **only**
- `commits/4ab2b9868/check-runs` → Build, Test, Lint, Integration Test, E2E Test, Vuln, CodeQL — **no codecov context**

`codecov/project` appears in neither, corroborating #4875's 64-observation finding from a
different sample. And the ruleset carries no coverage context, so `codecov/patch` — which does
post — gates nothing. The `.codecov.yml` project status was still tightened to `threshold: 0%`;
the file itself calls this "correct-in-waiting, not an active gate", which is honest, but it does
not satisfy the criterion as worded.

### Criterion 3 — the session-lifecycle matrix, executed

| ROADMAP path | Matrix transitions | Cells | Status |
|--------------|--------------------|-------|--------|
| connect | fresh-select | 3 spec + 1 n/a | ✓ |
| reconnect | reattach-select, reattach-cas, telnet-tmux-reattach, wifi-blip | 11 spec + 1 covered-elsewhere + 1 **planned** + 3 n/a | ✓ (one uncovered cell) |
| multi-character | move-arrival.multi-session, wifi-blip.multi-session | 2 spec | ✓ |
| idle-timeout | detach-all, reaper-sweep, post-ttl-relogin | 9 spec + 3 n/a | ✓ |

**"multi-character" is genuinely covered, not conflated with multi-tab.** The registry's
`multi-session` column mostly means one player, two tabs — but `move-arrival.multi-session` and
`wifi-blip.multi-session` build *two characters of one player* via
`AuthedPlayer.AdditionalCharacter`, asserted at
`lifecycle_privacy_floor_test.go:316` and `:518` ("two characters of one player MUST hold two
distinct game sessions"), and `test/integration/auth/multi_tab_test.go:282` is a
`Describe("Multi-tab session isolation — two characters of one player")`. I checked this
specifically because the column name invited the conflation.

**The declared-but-uncoverable cell is honestly declared.** `reattach-cas.multi-session` is
`disposition: planned`, `owed_by: unassigned`, `issue: 4863` (confirmed OPEN), with a
`blocked_on` naming the missing seam (per-connection detach). Its notes say in plain terms
"OWED BY NOBODY, DELIBERATELY … This is the matrix's ONE genuinely uncovered cell at phase end."
This is the opposite of the phase's signature defect — it is a row that exists and asserts
nothing, and it *says* it asserts nothing rather than borrowing a `spec` label.

Three further rows (`admin-boot.web-char/.telnet/.multi-session`) are
`not-implementable-from-harness-defaults` with both gaps stated separately (semantics:
`resetpassword --kick` deletes the row but emits no `session_ended`, #4862; drivability:
`RegisterAdmin`'s heavy deps are unwired). The registry explicitly refuses to claim no admin-boot
path exists — a false-absence claim the plan's own prohibitions forbid.

**Anti-substitution seams verified in code, not from test intent:**

| Seam | How it could have been faked | What the code actually does |
|------|------------------------------|-----------------------------|
| telnet client type | relabel a terminal session | `lifecycle_attach_test.go:56` `SELECT client_type FROM session_connections` — read back from Postgres |
| reaper sweep | drive the `ExpireSession` helper, which writes a row the reaper never selects | `lifecycle_ttl_test.go:127` constructs the production `session.NewReaper` and runs it; every eligibility setup uses `DetachAndExpireSession`, never `ExpireSession` |
| ordering | `time.Sleep` | zero sleeps; timestamped emit helper + injected reaper clock (`reaperTick = 25ms`) |

### Criterion 4 — the arch-review Medium cluster

| Item | Disposition | Evidence |
|------|-------------|----------|
| secure-cookie default | ✓ inverted | `cmd/holomush/gateway.go:92` `defaultSecureCookies = true`; `:130` flag registration; `:324` `Secure: cfg.SecureCookies` |
| ABAC empty-string sentinels | ✓ removed | `rg '\] = ""' internal/access/policy/attribute/*.go` (non-test) → **exit 1, zero hits**; `has_owner`/`has_value`/`has_parent_location`/`has_location` witnesses emitted on every branch; ADR `holomush-ti1b` cited at each site |
| silent audit-emitter drop | ✓ observable | `internal/eventbus/history/plugin_downgrade_fence.go:429` nil-emitter branch → `:434` `WarnContext(parent, …)` — context-carrying, per the logging rule |
| DEK read-cache | ⏸ deferred | #4792 **OPEN**, carrying a "Why it is out of scope here" comment. Not closed, not claimed. |
| `sessions.location_id` index | ✓ landed | migration `000053_*.up.sql`/`.down.sql`; round-trip test asserts absence by **keyed catalog lookup** (`SELECT indexdef FROM pg_indexes WHERE tablename='sessions' AND indexname=$1`), not by counting indexes — satisfying the plan's own prohibition |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Repo-wide unit suite green after ~138 test renames | `task test` | exit 0 — 10418 tests, 4 skipped | ✓ PASS |
| Meta-tests (ACE ratchet + matrix registry) green | `task test -- ./test/meta/` | exit 0 — 109 tests | ✓ PASS |
| Session-lifecycle matrix specs green | `task test:int -- ./test/integration/session/...` | exit 0 — Ran 42 of 42, 0 skipped | ✓ PASS |
| E2E coverage lands non-empty | codecov API, merge SHA | 3 sessions, `core.go` 85.39%, `sub_grpc.go` 76.08% | ✓ PASS |
| A coverage gate is enforced | `gh api rulesets/11923801` | 8 contexts, none from codecov | ✗ FAIL |
| PR #4874 exists, merged, CI complete on its head | `gh pr view 4874` + codecov | head `97e692dc6`, merged, 3 sessions complete | ✓ PASS |

Exit codes were read directly (`echo "EXIT=$?"` on an unpiped command), never inferred from
matching a string in stdout.

### Requirements Coverage

| Requirement | Ledger | Verdict | Evidence |
|-------------|--------|---------|----------|
| QUAL-02 | Pending | **deliberate, documented** | 09-19-SUMMARY.md:223-233 issues the ruling and says `requirements.mark-complete` was **not** run. Three floors measured and short; #4861/#4875/#4876 all OPEN. |
| QUAL-03 | Pending | **deliberate, documented** | 09-02-SUMMARY.md:166-174 (protocol), 09-09-SUMMARY.md:213-224 ("a statement about one predicate, not a clean bill of health"), 09-19-SUMMARY.md:235. Residual #4860 OPEN. |
| QUAL-04 | Complete | **verified by execution** | 42/42 specs, exit 0; bijection meta-test green. |
| QUAL-05 | Pending | **deliberate, documented** | 4/5 cluster items in-tree; #4792 deferred with rationale; the de-slop half deliberately not started and deliberately given no placeholder issue (09-02-SUMMARY.md:22 — "an issue with no acceptance criterion is the same overstatement in a different place"). |

**No orphaned requirements.** Every ID in every PLAN frontmatter (QUAL-02/03/04/05) maps to a
REQUIREMENTS.md row, and REQUIREMENTS.md assigns no further ID to Phase 9.

**All three Pending statuses are deliberate.** Each has a named SUMMARY passage, a stated reason,
and — where a shortfall is quantitative — an open issue carrying the number. This is the single
most important finding of this verification: the ledger's Pending rows are honest bookkeeping,
not silent shortfall.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `.planning/STATE.md` | 8, 373 | "all 21 plans executed" vs ROADMAP's "20/21" and an unticked 09-21 | ⚠️ Warning | Bookkeeping drift; 09-21 did execute but produced no SUMMARY, so neither statement is exactly right |
| phase dir | — | `09-21-SUMMARY.md` absent | ⚠️ Warning | Its must-haves were unrecorded; this verification re-derived all five from the GitHub/codecov APIs instead. Evidence quality is *higher* than a SUMMARY would have given (live, unstaleable) but the phase record has a hole. |
| `deferred-items.md` | 09-20 section | Two load-dependent integration flakes with no issue and no `test/quarantine.yaml` row | ⚠️ Warning | Documented in the phase directory only; they will not survive into the tracker |
| GitHub | #4793, #4796, #4797 | Fixes landed in-tree, issues still OPEN | ℹ️ Info | Ledger hygiene, not a code gap |

No TBD/FIXME/XXX debt markers were introduced by this phase's changes.

### Where I only read, and did not run

Stated plainly, per the phase's own standard:

- **Criterion 4's five items are read-verified, not executed.** I confirmed the code shape at
  `path:line` for each; I did not stand up a gateway to observe a `Secure` cookie on the wire,
  nor run the migration against Postgres (its round-trip test is inside the integration lane I
  did not run in full).
- **The two ratchets' fail-closed behaviour is read-verified only.** Both carry real `require`
  assertions and explicit non-vacuity floors, so they cannot pass on an empty walk — but I could
  not mutate the tree to prove they fail on a regression. Recorded as
  `behavior_unverified_items`, not scored.
- **I ran only `./test/integration/session/...` of the integration lane**, not the whole thing.
  The milestone integration audit covered the rest; the two flakes in `deferred-items.md` were
  not reproduced.

### Gaps Summary

One criterion of four is unmet, and it is unmet in a way the phase itself measured and published.
SC1 is a conjunction: *backfill with genuine behavioral tests* **and** *the coverage gate passes
repo-wide*. The first conjunct holds — the E2E measurement chain is genuinely repaired (three
codecov sessions where there were two, and two previously-unmeasurable wiring files now carrying
85% and 76%), and the tests added are behavioural rather than percentage-moving. The second
conjunct fails twice over: `cmd/holomush` sits 9.91 points below its named floor, and no coverage
context gates any merge in this repository — `codecov/project` posts on no ref at all, and
`codecov/patch`, which does post, is not in the protected-branch ruleset.

What makes this `gaps_found` rather than a phase failure is that every shortfall carries a
measurement, an attribution, and an open issue, and the phase explicitly declined to mark QUAL-02
Complete. The gap is between the ROADMAP's success criterion and reality — not between the
SUMMARYs and the codebase. On the ~40 claims I cross-checked, the SUMMARYs and the tree agreed
every time, including on figures that made the phase look worse (70.09%, 79.11%, the uncovered
matrix cell, the unwritten de-slop batch).

Closing SC1 needs either the two coverage shortfalls closed, or SC1's gate clause amended in
ROADMAP.md to match the deferral already recorded in `.codecov.yml` and #4875/#4876.

---

_Verified: 2026-07-27T20:15:00Z_
_Verifier: Claude (gsd-verifier)_

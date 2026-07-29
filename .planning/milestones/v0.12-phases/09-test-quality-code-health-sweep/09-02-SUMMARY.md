---
phase: 09-test-quality-code-health-sweep
plan: 02
subsystem: tracking
tags: [issues, deferrals, test-scaffolding, security-polish]
status: complete
requires: []
provides:
  - "issue numbers for the four eventbus_e2e skip files (read by plan 09-11)"
  - "issue numbers for the ec22.9 security-polish residue"
  - "written deferral rationale for #4792 and de-slop"
affects: []
tech-stack:
  added: []
  patterns: []
key-files:
  created: []
  modified: []
decisions:
  - "D-11 satisfied by filing new issues that cite their closed predecessors, because all four predecessors exist and were closed NOT_PLANNED — the plan's premise that none existed was falsified"
  - "The three ec22.9 residue items were re-verified individually against current code; two had drifted, so each issue was re-scoped to what is actually true rather than filed at the plan's stale framing"
  - "No placeholder issue opened for de-slop (D-20) — an issue with no acceptance criterion is the same overstatement in a different place"
metrics:
  duration: ~35min
  tasks: 3
  files: 0
  completed: 2026-07-26
---

# Phase 09 Plan 02: Issue Filing & Deferral Recording Summary

Filed seven open GitHub issues and one deferral comment so that nothing this phase trims or
defers can be lost — and corrected two falsified premises in the plan along the way.

## What was done

This plan modifies **no repository files**. Its artifacts live entirely in GitHub Issues.

### Four skip-file tracking issues (Task 1)

Plan 09-11 reads this mapping. Each file's current `Skip(...)` cites a retired beads token;
09-11 repoints it at the issue number in the right-hand column.

| File (`test/integration/eventbus_e2e/`) | Current skip token | New issue |
|---|---|---|
| `audit_drift_detector_test.go` | `TODO(holomush-ecbg)` | **#4853** |
| `backfill_rebuild_test.go` | `TODO(holomush-l4kx)` | **#4854** |
| `js_storage_corruption_test.go` | `TODO(holomush-6nds)` | **#4855** |
| `multi_protocol_fanout_test.go` | `TODO(holomush-nko7)` | **#4856** |

All four are `enhancement` + `priority::low`. None is labelled `bug` — these are unwritten
tests, not defects. Each body states the behaviour the unwritten test would prove (not merely
"implement this test"), the file path, why it is blocked, and its tracker history.

### Three security-polish residue issues (Task 2)

| Issue | Item |
|---|---|
| **#4857** | argon2 decoy hash on the auth-miss path uses an all-zero salt/key — `internal/auth/auth_service.go:144` |
| **#4858** | the deliberate absence of `http.Server.WriteTimeout` on the gateway is undocumented, so a future contributor can add one and truncate streaming — `internal/web/server.go:144-156` |
| **#4859** | `pr-prep.md:173` still names `addlicense`; the repo uses pinned `license-eye` v0.8.0 |

Each cites closed predecessor **#2382** and states that the cookie/TLS quarter of #2382 is
delivered this phase via **#4794** while these were deferred. Filed separately, never bundled —
bundling is what let them be lost when #2382's fourth item was addressed.

### Deferrals recorded (Task 3)

**#4792 (DEK read-cache) — commented, left OPEN.** The comment records that landing it needs
(a) a benchmark demonstrating the per-pose `crypto_keys` read amplification actually drops and
stays dropped across an invalidation, and (b) a `crypto-reviewer` READY verdict, since it
touches `internal/eventbus/crypto/` and interacts with INV-CRYPTO-16 and the rekey
invalidation coordinator. Neither is test-quality or code-health work. The comment states
plainly that the issue is **not** addressed by this phase.

**De-slop / humanization (D-20) — deferred here, with no tracker item, deliberately.** The
parent epic `holomush-89o9` survives only as **#3918, which is CLOSED**, with zero surviving
member issues; TRIAGE.md records the epic as consolidated into the GSD backlog. There is no
scoped, verifiable work item behind it and an unbounded prose sweep has no acceptance
criterion an implementer could be held to. No placeholder issue was opened: an issue with no
acceptance criterion is the same overstatement in a different place. This paragraph is the
deferral record.

## Deviations from Plan

### 1. [Rule 1 — Falsified premise] All four skip-file predecessors exist and are closed

**Found during:** Task 1.

**Issue:** The plan asserted *"All four retired tracker ids that those files currently cite
were confirmed to have no corresponding GitHub issue"* and that a prior search *"returned only
one closed, substring-false-matching result"*. Both are false. Every one of the four has a
GitHub issue, and all four were closed **NOT_PLANNED on 2026-05-17** in one bulk triage sweep:

| File | Bead | Predecessor | Post-beads disposition (TRIAGE.md) |
|---|---|---|---|
| `audit_drift_detector_test.go` | `holomush-ecbg` | **#2881** | Consolidated → ROADMAP Phase 999.14 |
| `backfill_rebuild_test.go` | `holomush-l4kx` | **#2880** | Consolidated → ROADMAP Phase 999.14 |
| `js_storage_corruption_test.go` | `holomush-6nds` | **#2387** | **Archive only — not migrated** |
| `multi_protocol_fanout_test.go` | `holomush-nko7` | **#2386** | **Archive only — not migrated** |

**Root cause of the plan's error:** searching by *tracker id* returns nothing, because GitHub
issues never carried the beads ids. Searching by *behaviour phrase* returns exact title
matches. This is the phase-9 defect class exactly — a search whose zero result was read as
"absent" when the thing exists under a different token.

**Fix:** the deliverable is unchanged and still necessary (a closed issue cannot be cited by
09-11), but each new issue now cites its predecessor and its post-beads disposition, so the
tracker does not silently duplicate declined work.

**Carry-forward for plan 09-11 — read before trimming:** #4855 and #4856 cover work that was
declined **twice** (closed NOT_PLANNED, then classified "Archive only — not migrated" rather
than consolidated). Their issue bodies say so, and say that **deleting the test file outright
is a legitimate resolution**. 09-11 should make that call consciously rather than
reflexively retaining a skip for work nobody plans to do.

### 2. [Rule 1 — Stale finding list] Two of three residue items had drifted; filing them as written would have fabricated defects

**Found during:** Task 2. The plan named the three items verbatim from a 3-month-old finding.
Each was re-verified against current code first.

| Item | Plan's framing | Verified reality |
|---|---|---|
| argon2 dummy-hash entropy | live | **Live, unchanged.** Filed as written (#4857). |
| `http.Server` write timeout | "a missing write timeout" | **Premise changed.** #2382 itself called the absence "acceptable" and asked only to *document the rationale inline*. GH-4785 since added `ReadTimeout`/`ReadHeaderTimeout`/`IdleTimeout`, and `http2.Server.WriteByteTimeout: 10s` already provides streaming-safe write-side liveness. Adding `WriteTimeout` would **break** streaming. Re-scoped to the surviving residue: the missing inline note (#4858). |
| unpinned `addlicense` | live | **Resolved.** `addlicense` is gone from all tooling, replaced by `license-eye` pinned at `LICENSE_EYE_VERSION: v0.8.0`; `lefthook.yaml` (the second cited site) no longer exists. Only a stale doc line survives, filed with its premise corrected (#4859). |

**Why deviate:** filing "fix the unpinned addlicense" when addlicense does not exist, or "add
the missing write timeout" when adding it would break streaming, would inject two fabricated
defects into the tracker — the precise falsification this phase exists to eliminate, and a
violation of the repo's "MUST produce grounded findings" rule. Each issue was re-scoped to
what is true and states explicitly how it differs from #2382's original wording.

**Net effect on the plan's gate:** unchanged — three separately-filed, open, truthfully-scoped
issues covering every part of #2382's residue that survives in any form, with the resolved
parts documented as resolved.

## Verification

All three task gates passed by exit code (never by matching output text).

The Task 1 gate was additionally **negative-controlled**, per the phase-9 requirement that a
verification must be able to fail:

| Scenario | Exit |
|---|---|
| the four real issues (positive control) | **0** |
| a closed issue substituted (#2881) | 1 |
| only 3 issues | 1 |
| 5 issues (one extra) | 1 |
| nonexistent issue number | 1 |
| empty handoff file | 1 |

The handoff files were confirmed via `od -c` to end with a trailing newline, so `while read`
consumes every line — the failure mode the plan's own gate note called out.

Final state check, all live queries:

- #4853, #4854, #4855, #4856, #4857, #4858, #4859 — all **OPEN**
- #4792 — **OPEN**, carrying the deferral comment
- every issue labelled `enhancement` + `priority::low`; none labelled `bug`
- `git status --short` empty — no repository files modified, as the plan specified

## Requirements status — both left Pending, deliberately

This plan's frontmatter carries `[QUAL-03, QUAL-05]`, but **neither is marked complete**,
following the precedent 09-01 set for QUAL-02.

QUAL-03 is *"skeleton/weak tests are remediated to assert real behavior, and ACE naming
violations corrected"*; QUAL-05 is *"a code-health & security-polish batch is applied"*. This
plan remediated no test and applied no batch — it filed the tracking issues that later plans
depend on. QUAL-03 is additionally carried by 09-09, 09-11 and 09-18; QUAL-05 by 09-03, 09-04,
09-05 and 09-06. Flipping either here would assert a property no artifact in this plan
demonstrates.

## Self-Check: PASSED

- No files claimed created or modified; `git status --short` confirms an unmodified tree.
- All seven issue numbers resolved to `OPEN` via `gh issue view` at time of writing.
- #4792 confirmed `OPEN` with a comment matching the benchmark prerequisite.
- No de-slop placeholder issue was opened (Task 3 acceptance criterion 4).

## Known Stubs

None. This plan produces no code.

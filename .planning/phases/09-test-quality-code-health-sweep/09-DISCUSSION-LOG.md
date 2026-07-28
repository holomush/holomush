# Phase 9: Test-Quality & Code-Health Sweep - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-25
**Phase:** 9-test-quality-code-health-sweep
**Areas discussed:** Coverage target definition, Test-quality sweep vs. gate, Session matrix depth, Medium-cluster triage, Phase shape & sizing gates

---

## Area selection

| Option | Description | Selected |
|--------|-------------|----------|
| Coverage target definition | QUAL-02's "packages under the reconciled bar" has no operational meaning after Phase 6 removed per-package measurement | ✓ |
| Test-quality: sweep vs. gate | Arch review found the codebase clean; bead lists are concrete but drifted | ✓ |
| Session matrix depth | izk0's 12×4 matrix and its harness prerequisites | ✓ |
| Medium-cluster triage | Which of the 5 arch-review Mediums land vs. defer; de-slop scope | ✓ |

**User's choice:** All four.

---

## Coverage target definition (QUAL-02)

### Q1 — What does "packages under the reconciled bar" mean operationally?

| Option | Description | Selected |
|--------|-------------|----------|
| Named allowlist (0yo6 four) | Backfill `cmd/holomush`, `internal/tls`, `internal/xdg` @≥80%, `internal/core` @≥90%. Bounded, provenance-faithful. *(Claude's recommendation)* | |
| Risk-ranked repo audit | Fresh whole-repo audit; rank by uncovered × blast radius; backfill top N | ✓ |
| Both — allowlist then risk-ranked | Clear the four first, then spend remaining budget risk-ranked | |

**User's choice:** Risk-ranked repo audit.
**Notes:** Chosen over Claude's bounded recommendation. Captured as D-01, with the 0yo6 four retained as a floor rather than the definition.

### Q2 — Commit to a project-coverage number, or just not regress?

| Option | Description | Selected |
|--------|-------------|----------|
| Modest lift to ~60% | ~5 points from the 54.6% baseline; achievable from the allowlist packages alone. *(Claude's recommendation)* | |
| No numeric target | Ratchet only; project% moves as a side effect | |
| Aggressive — 70%+ | Large lift; risk of coverage theatre | ✓ |
| You decide | Pick from the audit numbers | |

**User's choice:** Aggressive — 70%+.
**Notes:** Claude flagged that ~15 points on a large Go codebase is the single largest sizing risk in the phase. Recorded as D-02 with a mandatory research-sizing planner flag rather than softening the target.

### Q3 — Touch the half-finished codecov gate config?

| Option | Description | Selected |
|--------|-------------|----------|
| Make required + tighten later | Add to ruleset `11923801` at `threshold: 1%`. *(Claude's recommendation)* | |
| Tighten to 0% AND make required | True no-drop plus enforcement | ✓ |
| Leave gate config alone | Backfill only; enforcement stays a separate operator task | |

**User's choice:** Tighten to 0% AND make required.
**Notes:** Triggered the follow-up sequencing question below, since enforcing no-drop *during* a 15-point climb would gate the phase's own PRs.

### Q4 — Packages where unit coverage is not meaningful (e.g. `runCore()`)?

| Option | Description | Selected |
|--------|-------------|----------|
| `codecov:ignore` + rationale comment | Explicit, reviewable exclusion. *(Claude's recommendation)* | |
| Count integration-flag coverage | Let the integration upload count toward the package | ✓ |
| You decide | Judge per package during the audit | |

**User's choice:** Count integration-flag coverage.
**Notes:** Recorded as D-03, with an explicit "do not `codecov:ignore` to move a number" constraint.

---

## Test-quality: sweep vs. gate (QUAL-03)

### Q1 — Gate sequencing (follow-on from coverage Q3)

| Option | Description | Selected |
|--------|-------------|----------|
| Last plan, after the lift | Backfill to 70%, then tighten + require. *(Claude's recommendation)* | |
| First — set the floor immediately | Enforce no-drop from day one | |
| Split — require first, tighten last | Add to ruleset early at `threshold: 1%`; drop to 0% as the final plan | ✓ |

**User's choice:** Split — require first, tighten last.
**Notes:** Closes Phase 6's unfinished ruleset gap immediately while deferring the no-drop bar until after the climb. Recorded as D-04.

### Q2 — QUAL-03 scope given the evidence conflict

| Option | Description | Selected |
|--------|-------------|----------|
| Re-derive lists against HEAD | Verify each ec22.15/16 site, fix survivors. *(Claude's recommendation)* | |
| Fresh repo-wide audit | Define a predicate; run over all 6538 test functions | |
| Both | Union of the re-derived lists and a fresh predicate sweep | ✓ |

**User's choice:** Both.
**Notes:** Recorded as D-06. Neither source alone is sufficient — the arch review's clean sweep and the stale bead lists disagree, and the union resolves that rather than picking a side.

### Q3 — What defines an ACE violation, and do we gate it?

| Option | Description | Selected |
|--------|-------------|----------|
| Predicate + shrink-only allowlist | Seed with today's survivors; meta-test forbids growth. *(Claude's recommendation)* | |
| Predicate, no allowlist — fix all | Remediate every hit; clean end state, unbounded until run | ✓ |
| Curated list only, no gate | Fix ~15 named sites by hand | |

**User's choice:** Predicate, no allowlist — fix all.
**Notes:** Recorded as D-07 (predicate definition) and D-08 (fix-all posture). Claude noted the scope is unsized until the predicate runs, which produced the sizing-gate question in the final area.

### Q4 — The four `eventbus_e2e` skip-with-unreachable-setup files

| Option | Description | Selected |
|--------|-------------|----------|
| Move to quarantine registry | `test/quarantine.yaml` entries + open issue each. *(Claude's recommendation)* | |
| Trim body to `t.Skip` + issue ref | Delete the unreachable setup, keep a one-line skip | ✓ |
| Implement them | Write the four eventbus specs | |

**User's choice:** Trim body to `t.Skip` + issue ref.
**Notes:** Recorded as D-11. Planner flag added: the files cite retired bead IDs, so each needs a live GitHub issue mapped or filed before the trim, or the reference dangles.

---

## Session matrix depth (QUAL-04)

*Claude surfaced before the questions that izk0's stated blocker no longer exists — the `privacytest` harness was consolidated into `integrationtest` and all named TODO-panic helpers are implemented. This materially reduced the area's cost and reshaped the options.*

### Q1 — How much of the 12×4 matrix lands?

| Option | Description | Selected |
|--------|-------------|----------|
| Full 12×4 as specified | Every cell a spec or an explicit pointer; telnet column is zero-coverage today. *(Claude's recommendation)* | ✓ |
| Full matrix, telnet column deferred | Web + multi-session only | |
| Core transitions only | High-risk rows across all transports | |

**User's choice:** Full 12×4 as specified.
**Notes:** Recorded as D-12.

### Q2 — How is "every cell covered" tracked?

| Option | Description | Selected |
|--------|-------------|----------|
| Committed matrix + meta-test | Table plus a bijection meta-test asserting each named spec exists. *(Claude's recommendation)* | ✓ |
| Matrix doc, no meta-test | Doc comment only, zero enforcement | |
| Specs only, no matrix artifact | Skip the bookkeeping | |

**User's choice:** Committed matrix + meta-test.
**Notes:** Recorded as D-13. Consistent with the ratchet posture chosen for QUAL-02 and QUAL-03.

### Q3 — Fold in the adjacent privacy items?

| Option | Description | Selected |
|--------|-------------|----------|
| Both | `holomush-dqd1`'s two tests plus #4682's I-PRIV-6 floor arm. *(Claude's recommendation)* | ✓ |
| dqd1 only | Fold only what izk0 explicitly names | |
| Neither — matrix rows only | Keep both as separate issues | |

**User's choice:** Both.
**Notes:** Recorded as D-14.

### Q4 — Controlled timestamps for the floor-preservation tests

| Option | Description | Selected |
|--------|-------------|----------|
| Add a timestamped variant | `EmitDirectEventAt(..., at time.Time)`; deterministic, no sleeps. *(Claude's recommendation)* | ✓ |
| Advance the session clock instead | Manipulate `LocationArrivedAt` via a helper | |
| You decide | Let research pick | |

**User's choice:** Add a timestamped variant.
**Notes:** Recorded as D-15. Also avoids adding to the `~16 time.Sleep` pattern flagged by `holomush-ec22.13`.

---

## Medium-cluster triage (QUAL-05)

### Q1 — Which of the 5 Mediums land?

| Option | Description | Selected |
|--------|-------------|----------|
| All five | Closes the milestone's own named cluster. *(Claude's recommendation)* | |
| All except #4792 (DEK cache) | Defer the perf change needing benchmarks + crypto-reviewer | ✓ |
| Security-first subset | #4793/#4794/#4797 only | |

**User's choice:** All except #4792 (DEK cache).
**Notes:** Recorded as D-17. #4792 stays open with a deferral comment; this uses the ROADMAP success criterion's explicit "or deferred with rationale" clause.

### Q2 — #4794 secure-cookie mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| Invert the default | Secure/HSTS/CSP on unless explicitly disabled; needs a release note. *(Claude's recommendation)* | ✓ |
| Fail-closed at boot | Refuse to start if insecure and not localhost | |
| Auto-detect from proxy headers | Derive from TLS state / `X-Forwarded-Proto` | |

**User's choice:** Invert the default.
**Notes:** Recorded as D-18, rated `costly` reversibility — undo re-introduces the vulnerability and needs a second release note.

### Q3 — De-slop / humanization (`holomush-89o9`)

| Option | Description | Selected |
|--------|-------------|----------|
| Out — deferred with rationale | Epic closed, zero surviving member issues, no acceptance criterion. *(Claude's recommendation)* | ✓ |
| In, narrowly scoped | Humanizer pass on files this phase touches | |
| In, repo-wide sweep | Full pass across comments and docs | |

**User's choice:** Out — deferred with rationale.
**Notes:** Recorded as D-20. Honest half-closure of QUAL-05 rather than a faked sweep.

### Q4 — `holomush-ec22.9` security polish

| Option | Description | Selected |
|--------|-------------|----------|
| Fold cookie/TLS only | It *is* #4794; defer the other three. *(Claude's recommendation)* | ✓ |
| Fold all four | Include dummy-hash entropy, write timeout, addlicense pin | |
| Out entirely | Not named in QUAL-05's text | |

**User's choice:** Fold cookie/TLS only.
**Notes:** Recorded as D-21.

---

## Phase shape & sizing gates

### Q1 — Sizing the unbounded ACE sweep

| Option | Description | Selected |
|--------|-------------|----------|
| Research reports count, re-scope above threshold | Hit count visible before planning commits; ~150 triggers re-scope. *(Claude's recommendation)* | ✓ |
| Scope the predicate to first-party packages | Bound by construction; exclude `test/integration/`, `cmd/` | |
| No gate — fix whatever it returns | Commit sight-unseen | |

**User's choice:** Research reports count, re-scope above threshold.
**Notes:** Recorded as D-09 — the release valve for D-08's "fix all".

### Q2 — Ordering of the ACE rename vs. test-adding work

| Option | Description | Selected |
|--------|-------------|----------|
| ACE sweep last, single pass | Avoids rename-vs-add conflicts; sweep verifies the phase's own new tests. *(Claude's recommendation)* | ✓ |
| ACE sweep first | Clean baseline before writing anything new | |
| Predicate-as-lint first, sweep last | Gate lands early but is red mid-phase | |

**User's choice:** ACE sweep last, single pass.
**Notes:** Recorded as D-10.

### Q3 — PR shape under milestone branching

| Option | Description | Selected |
|--------|-------------|----------|
| One PR on the milestone branch | Matches `branching_strategy: milestone` from #4852. *(Claude's recommendation)* | ✓ |
| Split — security PR separate | Two branches under a one-branch config | |
| You decide | Planner picks once waves are known | |

**User's choice:** One PR on the milestone branch.
**Notes:** Recorded as D-22. Worktree created at `.worktrees/v0.12-foundation-hardening` on branch `gsd/v0.12-milestone`. (Corrected during the reviews replan: the worktree directory carries the `v0.12-foundation-hardening` name but the branch is `gsd/v0.12-milestone`, per `git branch --show-current`. The two are not required to match and here they do not.)

### Q4 — #4796 index migration shape

| Option | Description | Selected |
|--------|-------------|----------|
| Plain `CREATE INDEX IF NOT EXISTS` | Matches all 52 existing migrations. *(Claude's recommendation)* | ✓ |
| `CONCURRENTLY` — needs runner support | Non-blocking but non-transactional; new capability | |
| You decide | Research confirms runner behaviour first | |

**User's choice:** Plain `CREATE INDEX IF NOT EXISTS`.
**Notes:** Recorded as D-19. No `CREATE INDEX CONCURRENTLY` precedent exists in the repo.

---

## Claude's Discretion

- Exact risk weighting in the D-01 coverage ranking.
- The precise D-09 re-scope threshold (~150 is a starting number).
- Whether D-15's timestamped emit is a new method or a variadic option.
- Wave/plan decomposition inside the D-04 → backfill → D-10 ordering constraint.
- Whether the D-13 matrix meta-test lives in `test/meta/` or beside the suite.

## Deferred Ideas

- **#4792** DEK read-cache — perf change needing benchmarks + crypto-reviewer.
- **`holomush-89o9`** de-slop/humanization — closed epic, no scoped work.
- **`holomush-ec22.9` residue** — argon2 dummy-hash entropy, write timeout, addlicense pin.
- **`holomush-ec22.13`** — ~16 `time.Sleep` async-sync sites.
- **`holomush-ec22.14`** — ~20 string-match error assertions.
- **`holomush-ec22.22`** — archive stale plans; belongs with the docs program (999.15).
- **Implementing the four `eventbus_e2e` TODO tests** — substantial eventbus work.
- **Phase-8 merge follow-ups** — #4830, #4831, #4850, #4829, #4828.
- **`loader.go` (1142 LoC)** — next god-object split candidate; architecture work (999.9).

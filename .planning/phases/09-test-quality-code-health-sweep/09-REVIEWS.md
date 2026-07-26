---
phase: 9
reviewers: [codex, pi]
reviewed_at: 2026-07-26T00:58:00Z
reviewer_models:
  codex: cli-default
  pi: moonshotai/kimi-k3 (openrouter, --thinking high)
plans_reviewed: [09-01, 09-02, 09-03, 09-04, 09-05, 09-06, 09-07, 09-08, 09-09, 09-10, 09-11, 09-12, 09-13, 09-14, 09-15, 09-16, 09-17, 09-18, 09-19]
verdicts:
  codex: HIGH — not ready to execute
  pi: LOW-to-MEDIUM — execute after narrow fixes
orchestrator_verdict: NOT READY — replan required
---

# Cross-AI Plan Review — Phase 9

Both lanes were prompt-fed the full plan set (19 plans + CONTEXT + RESEARCH + REQUIREMENTS + PROJECT context, ~96K tokens) with repo access and an explicit instruction to verify claims against source rather than review plan text in isolation. Claude was skipped for independence (it is the executing runtime).

---

## Codex Review

# Summary

The plans are unusually evidence-conscious and contain several strong fail-closed checks, but they are **not ready to execute**. Two QUAL-04 assumptions are contradicted by the live harness: telnet sessions are still TODO-fatal, and `ExpireSession` bypasses rather than drives the reaper. Coverage governance also has a push/PR lifecycle gap, while plan 09-17’s automated verification does not verify status contexts at all. These are execution blockers, not editorial refinements.

# Strengths

- The secure-default analysis is grounded correctly. The current CLI default is false at [`gateway.go:120`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/cmd/holomush/gateway.go:120), while the value directly controls the web server at [`gateway.go:309`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/cmd/holomush/gateway.go:309). Cookie construction already starts secure and explicitly downgrades for local HTTP at [`cookie.go:45`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/web/cookie.go:45), and HSTS/CSP are conditional at [`security_headers.go:74`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/web/security_headers.go:74). Plan 09-04 targets the actual defect.

- The ABAC remediation follows the right invariant. Current providers still emit empty sentinels, while the plan preserves the boolean witnesses and replaces sentinel values with omission. Its TDD structure and mandatory ABAC review are appropriate.

- Plan 09-07’s timestamped emit design fits the existing implementation. `EmitDirectEvent` already uses `eventbus.NewEvent` and the production publisher at [`session.go:759`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/session.go:759). Adding a sibling that overrides the constructed event’s timestamp and returns its ID is narrow and testable.

- The matrix ratchet has a good precedent. The quarantine guard already implements registry/marker equality and reports both sets at [`quarantine_registry_test.go:35`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/test/meta/quarantine_registry_test.go:35), with filesystem errors propagated during the walk at [`quarantine_registry_test.go:76`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/test/meta/quarantine_registry_test.go:76). Plan 09-16 adds the important full-grid shape check, preventing a trivially satisfied bijection after row deletion.

- Plan 09-19’s Codecov query is genuinely fail-closed: each `awk` stage begins in the failing state, and missing API values cannot pass accidentally. Its anchored `.codecov.yml` checks also avoid matching explanatory comments.

# Concerns

- **HIGH — The telnet matrix cannot be implemented with the planned files.** `OpenTelnetSession` is explicitly TODO-fatal at [`session.go:866`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/session.go:866). The common attach path hardcodes `ClientType: "terminal"` at [`session.go:431`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/session.go:431) and [`session.go:455`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/session.go:455). Plan 09-14 nevertheless promises a genuine telnet reconnect while modifying only a test file. The actual telnet suite remains bootstrap-only at [`telnet_suite_test.go:15`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/test/integration/telnet/telnet_suite_test.go:15). A `"terminal"` test relabelled as telnet would create assurance theatre.

- **HIGH — The expiry helper cannot exercise the reaper behavior claimed by 09-13/09-15.** `ExpireSession` directly changes the row to `expired` at [`harness.go:993`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/harness.go:993). The real reaper only selects `detached` rows whose expiry is past at [`session_store.go:444`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/store/session_store.go:444), then transitions and deletes them at [`reaper.go:116`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/session/reaper.go:116). Therefore “detach → `ExpireSession` → assert row deleted” cannot drive production reaping. The harness already exposes the pool and session store for proper reaper tests at [`harness.go:1286`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/harness.go:1286), but the plans do not use them.

- **HIGH — The default lifecycle harness cannot dispatch `quit`.** Plan 09-12 explicitly starts with harness defaults, but the default command registry is empty at [`harness.go:468`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/harness.go:468). `quit` only becomes available when compiled-in handlers are registered; its registration is at [`register.go:86`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/command/handlers/register.go:86). Yet 09-14 expects to drive quit through `SendCommand`, which calls the production command handler at [`session.go:89`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/testsupport/integrationtest/session.go:89). The plan needs either `WithInTreePlugins`, explicit compiled-in registration, or a dedicated termination driver.

- **HIGH — Coverage verification depends on a PR that the wave graph never creates.** Plan 09-17 requires current-branch CI rollups and potentially a push/re-run at [`09-17-PLAN.md:81`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-17-PLAN.md:81) and [`09-17-PLAN.md:132`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-17-PLAN.md:132). Plan 09-19 similarly requires branch-side Codecov reports before tightening at [`09-19-PLAN.md:73`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-19-PLAN.md:73). The current local branch has no upstream, and the plan graph contains no push/open-draft-PR checkpoint before either operation. Under the normal execute-then-ship loop, these tasks cannot obtain their required evidence.

- **HIGH — Plan 09-17’s automated gates do not prove its deliverable.** Task 1’s command merely lists three PR numbers at [`09-17-PLAN.md:95`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-17-PLAN.md:95). It neither checks that those PRs changed Go nor queries their status rollups. Because the pipeline has no `pipefail`, a failed `gh` can also be masked by successful `head`. Task 2 only confirms two configuration literals at [`09-17-PLAN.md:138`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-17-PLAN.md:138); it passes without observing either Codecov context. This is the most consequential remaining false-green shell gate because the next action can block all PRs.

- **MEDIUM — The E2E coverage repair needs stale-output protection.** The current task neither removes `coverage-e2e.out` before running nor propagates failure from `go tool covdata textfmt`; it ultimately exits only with `E2E_EXIT` at [`Taskfile.yaml:242`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/Taskfile.yaml:242) and [`Taskfile.yaml:250`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/Taskfile.yaml:250). A failed conversion can therefore leave a prior non-empty profile available to the new body check. Also, overriding the container to the host UID should be treated cautiously: the image deliberately runs as the `holomush` user with a prepared home at [`Dockerfile:10`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/Dockerfile:10).

- **MEDIUM — Migration rollback remains manual.** Plan 09-05’s automated verification is only `task test:int` at [`09-05-PLAN.md:89`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-05-PLAN.md:89). That proves the up migration applies, not that the down migration removes the index. The repository already exposes targeted migration movement through `Migrator.Steps` at [`migrate.go:106`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/store/migrate.go:106) and has a rollback-testing precedent at [`migrations_audit_shape_integration_test.go:109`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/internal/store/migrations_audit_shape_integration_test.go:109).

- **MEDIUM — Several plan-local gates are weaker than their acceptance criteria.** Plan 09-12 verifies only file existence plus lint at [`09-12-PLAN.md:113`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-12-PLAN.md:113), not 48 rows, uniqueness, ten `n/a` rows, or exclusive dispositions. Downstream plans consume this registry before 09-16 eventually validates it. Plan 09-11’s `rg -c | wc -l` check at [`09-11-PLAN.md:95`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-11-PLAN.md:95) also converts an `rg` read error into a zero count.

- **LOW — Plan metadata and acceptance wording drift from actual mutations.** Plan 09-07 lists only `session.go` in `files_modified` at [`09-07-PLAN.md:7`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-07-PLAN.md:7), although Task 2 changes the privacy suite. Plan 09-18 lists only its meta-test at [`09-18-PLAN.md:7`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-18-PLAN.md:7), despite changing roughly 114 test files. Its requirement that every old name disappear “anywhere in the repository” at [`09-18-PLAN.md:175`](/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening/.planning/phases/09-test-quality-code-health-sweep/09-18-PLAN.md:175) also conflicts with recording every old name in the committed summary.

# Suggestions

1. Insert a QUAL-04 harness prerequisite before 09-12:

   - parameterize attach by client type and implement `OpenTelnetSession`;
   - add a deterministic detached-expiry/reaper-sweep seam;
   - decide how quit, logout, and admin boot are driven through production entry points;
   - verify each seam independently before populating the matrix.

2. Make 09-12’s shared harness option explicit. If quit is tested, register compiled-in handlers or start with the precise plugin option required. Avoid a vague “defaults” contract.

3. Split coverage closure into pre-PR and post-push stages. Open or push a draft PR before 09-17, wait for both uploads, observe the exact contexts, then run the operator checkpoint after the updated config has completed another CI run.

4. Replace 09-17’s automated checks with a fail-closed script that:

   - selects three merged Go-changing PRs;
   - extracts status rollups;
   - requires both exact Codecov contexts;
   - treats API errors and missing contexts as failures.

5. In `test:e2e:cover`, remove the output profile before the run, capture the `covdata textfmt` exit status, and require at least one non-header record from the newly created file.

6. Add a migration integration test that migrates to 52, steps to 53, asserts `idx_sessions_location_id`, steps back to 52, asserts absence, and reapplies 53.

7. Land the matrix schema/shape test with 09-12, then extend it to the full marker bijection in 09-16. Downstream plans should not consume a registry whose basic shape is manually asserted.

8. Correct `files_modified` metadata and scope the ACE old-name search to executable references, excluding the summary that intentionally records the old names.

# Risk Assessment

**HIGH.** The security-polish plans are generally strong, and the coverage end-state is designed carefully. However, the central QUAL-04 work currently depends on three unavailable harness behaviors—telnet transport differentiation, real reaper execution, and registered termination commands. The Codecov operator sequence also lacks the PR lifecycle needed to produce its evidence, while its precondition gate can pass without querying a single status. Executing the plans unchanged is likely to stall in Waves 3–4 or, worse, substitute weaker tests for the promised transport and lifecycle coverage.

---

## pi Review (moonshotai/kimi-k3)

# Cross-AI Plan Review: Phase 9 (Test-Quality & Code-Health Sweep)

## Summary

This is an unusually well-grounded set of 19 plans. I independently re-verified the vast majority of load-bearing claims against the live repository and the live codecov/GitHub APIs — file:line citations for ABAC sentinels, the plugin-downgrade-fence drop path, migration numbering, coverage figures (78.28% project / 64.25% `cmd/holomush` / 76.23% `internal/tls`, all essentially matching the review document), the four Ginkgo skip files and their dead retired-bead references, the harness helpers, the `TestINV_*` count (32 declarations / 19 files), and the ACE-predicate false-positive examples. Nearly everything checked out exactly as claimed, including several unusually precise details (drifted subtest line numbers, `after_n_builds: 2` appearing 3× unanchored vs 1× anchored, the `assert.Equal`-whole-map absence idiom). This is a plan set written by someone who actually ran the tools, not one that took the research document on faith.

The scope is large (19 plans, 7 waves) but is justified: the phase closes with a genuine bijection-style meta-test for the session matrix and a genuine AST-based naming ratchet, both modeled tightly on an existing in-repo precedent (`test/meta/quarantine_registry_test.go`). The plan set correctly identifies and corrects two premise-falsifying findings from research (the coverage target is already met; the ACE predicate needed tightening) rather than blindly executing stale CONTEXT.md numbers. I found one real gap (a depguard claim that doesn't hold), one unverifiable environmental hypothesis (the E2E coverage-flush root cause), a handful of scale/complexity concerns worth flagging, and a couple of small internal-consistency nits. None of these are severe enough to block the phase, but several are worth fixing before execution.

## Strengths

- **Coverage math is independently re-verified and correct.** Live codecov API queries against `main` today return project=78.28% (sessions:2), `cmd/holomush`=64.25%, `internal/tls`=76.23%, `internal/xdg`=97.91%, `internal/core`=91.55%, `e2e` flag=0.0 — all matching the review document's Gate 1 table within noise (`git`/query drift). Plan 09-19's Task 1 automated gate, run against `main` as-is, genuinely exits 1 at the very first sub-check (`e2e` flag > 0), and the plan's own note says so — this is a live, honest, self-aware verify block, not a decorative one.
- **The seven ABAC sentinel sites (09-03) are exactly where claimed.** `internal/access/policy/attribute/location.go:72,80`, `object.go:117,125,133`, `property.go:93,102` — all confirmed via `grep`. The reference form at `character.go:131-148` and the omission idiom (`assert.Equal(t, tt.expectAttrs, attrs)` at `character_test.go:254`, whole-map equality, not `NotContains`) is real and the plan correctly identifies it as the pattern to mirror, including correctly noting there's no `NotContains` convention in this package.
- **The plugin-downgrade-fence citation is exact.** `f.emitter == nil` at `internal/eventbus/history/plugin_downgrade_fence.go:423`, and the sibling `WarnContext` branch with the exact shape to copy is 12-15 lines below, confirmed.
- **Migration 000053 is genuinely free and the precedent is genuine.** `internal/store/migrations/` tops out at `000052_events_audit_partition`; `idx_sessions_active_character`/`idx_sessions_status` exist but nothing indexes `location_id`; `000008_session_player_fk.{up,down}.sql` is a real, exact `CREATE INDEX IF NOT EXISTS` / `DROP INDEX IF EXISTS` pair to copy.
- **The four Ginkgo skip files and their dead bead references are exactly as claimed.** All four (`audit_drift_detector_test.go:36`, `js_storage_corruption_test.go:38`, `multi_protocol_fanout_test.go:36`, `backfill_rebuild_test.go:28`) use bare Ginkgo `Skip(...)` inside `It(...)`, not `t.Skip`, and `gh issue list --search` for all four bead ids (`holomush-nko7`, `-6nds`, `-l4kx`, `-ecbg`) returns nothing live (only one false-positive substring match on a closed, unrelated issue). Plan 09-11's correction of a prior misreading ("not literally the file's first line") is itself independently verified against `backfill_rebuild_test.go:26-28`.
- **The harness helper inventory for QUAL-04 is fully accurate.** All 8 cited helpers (`WaitForEvent:171`, `MoveTo:303`, `DetachTransport:366`, `ReattachTransport:419`, `QueryStreamHistory:705`, `QueryStreamHistoryBounded:722`, `EmitDirectEvent:770`, `ExpireSession:995`) exist at their exact cited lines, `rg 'panic\("TODO'` over the package returns zero hits, and `allowAllPolicyEngine` really is the harness default (`harness.go:314`), confirming the CONTEXT.md open question about which ABAC mode plan 09-15's floor-preservation arm needs is correctly resolved.
- **Plan 09-07's design to add a sibling method rather than mutate `EmitDirectEvent`'s signature is well justified** — `EmitDirectEvent` has 18+ call sites across `test/integration/{privacy,streams,resilience,cursor_bounded_backfill}` and `internal/testsupport/integrationtest/harness_smoke_test.go`; changing its signature would have had a large, unnecessary blast radius.
- **The tightened ACE predicate (~114 vs the literal 1,106) is soundly reasoned**, and the `TestINV_*` carve-out count correction (32 declarations across 19 files, not "~25") is independently confirmed exactly (`grep -c` by declaration vs by file gives 32 vs 19). Plan 09-18's Task 1 verify block deliberately counts declarations not files for this reason, and is correct to do so.
- **Genuinely no-BS "no analog found" honesty** in `09-PATTERNS.md` for the E2E coverage-flush repair — the plan doesn't manufacture a false precedent; it names the two testable hypotheses (bind-mount uid, `stop_grace_period`) and requires the executor to gather live evidence before choosing a fix, which is the right posture for an environment-dependent bug with no in-repo analog.
- **The 38-populated-cell correction (not 48) is exact** — I independently recomputed the izk0 matrix from the archived bead JSONL and got 10 n/a / 38 populated, matching the plan precisely, correcting an error present in CONTEXT.md itself.
- **Real regression-locking discipline**: nearly every meta-test-landing task requires the executor to seed a violation, observe a non-zero exit, then restore — this is the right way to prove a "ratchet" is load-bearing rather than merely present.

## Concerns

- **HIGH — Plan 09-07's depguard claim is unsupported by the actual config.** Plan 09-07's acceptance criteria and threat model both assert "`task lint` exits 0, including the depguard rule forbidding production imports of test-support packages" (`09-07-PLAN.md:102`, `:158`, `:164`). I checked `.golangci.yaml:135-155`: the `no-test-only-constructs-in-production` depguard rule denies exactly four packages — `eventbustest`, `coretest`, `quarantinetest`, `natstest` — and `internal/testsupport/integrationtest` (the package this plan modifies) is **not** among them. `test/meta/depguard_config_test.go` also only pins those same three of the four. The actual protection for `EmitDirectEventAt` never reaching production is the `//go:build integration` tag (confirmed by a live `go build` reproduction: a package built only under `//go:build integration` fails to compile from a non-tagged importer), not a lint rule. This is a real gap between what the plan claims lint enforces and what lint actually enforces — the acceptance criterion should either be dropped/reworded to point at the build-tag mechanism, or (better) `internal/testsupport/integrationtest` should be added to the depguard deny list as part of this plan, closing the gap rather than asserting it's already closed.
- **MEDIUM — The E2E coverage-flush root cause (plan 09-01) is genuinely unverified, and the plan is honest about that but the risk deserves a callout.** I could not conclusively test either hypothesis (bind-mount uid mismatch vs missing `stop_grace_period`) in this sandbox — Linux-container bind-mount uid semantics don't reproduce faithfully on macOS Docker Desktop, and I have no access to the actual CI runner. Both hypotheses are architecturally plausible and correctly identified as untested (`adduser -D` on Alpine does create uid 1000, and `stop_grace_period` is genuinely absent from all compose files, both confirmed). This is fine as a *tracer* plan design (it's explicitly typed `tracer` and instructed to diagnose from live evidence before fixing) — but if both hypotheses turn out wrong, the entire QUAL-02 09-10/09-17/09-19 chain stalls, since they all transitively depend on 09-01. The plan has no documented fallback if the uid/grace-period fixes don't resolve it (e.g., a "known workaround" or escalation path). Given this is the tracer for the whole 7-wave phase, a documented fallback (or at minimum a stated re-scope trigger) would reduce single-point-of-failure risk.
- **MEDIUM — 19 plans for 4 requirements is real scope, and the review's own instructions flagged this for scrutiny.** Breaking it down: QUAL-02 gets 5 plans, QUAL-03 gets ~3.5 (shares 09-02), QUAL-04 gets 6, QUAL-05 gets ~4.5 (shares 09-02). QUAL-04 (the session matrix) is the largest single consumer at 6 plans (09-07, 12, 13, 14, 15, 16) — but given the matrix is genuinely 38 cells plus a from-scratch harness seam plus a bijection meta-test plus 3 named privacy floor tests, splitting it by transition-family across waves 3-5 is defensible rather than padding. The real complexity driver is that this phase does five structurally distinct things (repair a broken measurement chain, invert a security default, backfill two coverage floors, author 38 lifecycle specs with a meta-test, sweep ~114 test renames with a second meta-test) that don't decompose into fewer plans without conflating unrelated review surfaces (ABAC, crypto, migration, security-default, ACE-naming). I don't think this is over-engineered so much as an inherently five-shaped requirement set being executed faithfully. Still, an executing team should be aware that "4 requirements" undersells the actual work; ~19 plans is closer to the honest size.
- **MEDIUM — Plan 09-19's checkpoint (Task 3) creates a live deadlock risk that the plan itself names but cannot fully de-risk from inside the phase.** Adding `codecov/patch`/`codecov/project` to ruleset `11923801` as required checks, on the SAME PR that is currently open and whose HEAD commit's statuses were the ones just verified in Task 1, has a subtle timing hazard: GitHub evaluates required-status-checks against the **current head SHA's** existing status entries. If the ruleset edit happens after the final CI run but the PR is later force-pushed/rebased for any reason (e.g., resolving a merge conflict against `main` per the "pre-push rebase" requirement in `landing-the-plane.md`), the two coverage statuses have to re-post on the new SHA before merge is possible — and if e2e coverage flakes even once post-rebase, the PR is stuck. The plan's step 4 instructs "remove it again immediately" if stuck pending, which is the right mitigation, but this is a real operational risk on the single, large PR this phase produces (D-22), not a hypothetical.
- **LOW — Branch-name inconsistency across planning artifacts.** `09-CONTEXT.md:263` and `09-DISCUSSION-LOG.md:253` say the milestone branch is `gsd/v0.12-foundation-hardening`; `09-RESEARCH.md` and the actual repo (`git rev-parse --abbrev-ref HEAD`) say `gsd/v0.12-milestone`. The 19 plan bodies themselves never name the branch directly, so this doesn't affect execution, but it's worth cleaning up before this artifact set is archived, since a future reader following CONTEXT.md's citation would look for the wrong branch.
- **LOW — Plan 09-01's emptiness-guard specification is slightly underspecified relative to the two candidate root causes.** The plan asks the executor to add a guard that fails when `go tool covdata textfmt` produces a header-only profile — but if the actual defect is the bind-mount uid issue, `covdata textfmt` over an *empty coverdata directory* (not a missing/malformed one) behaves differently than over a genuinely corrupt directory; I did not verify this edge case (no Linux Docker environment available here). Worth a note in the plan that the guard's behavior should be verified against whichever root cause is actually confirmed, not assumed to behave identically for both.
- **LOW — Plan 09-17's "operator action moved to the final plan" reasoning is sound, but the plan set never states what happens to the milestone if the operator declines or is unavailable to run Task 3 of 09-19.** Given `autonomous: false` is correctly set on 09-19 and the checkpoint is correctly typed `blocking`, this is handled at the workflow level rather than being a plan defect — flagging only because it's the one plan in 19 that cannot complete autonomously, and it's also the very last plan in the phase, meaning the entire phase's "done" state hinges on a human action with no stated timeout/escalation.

## Suggestions

1. **Fix or reframe the 09-07 depguard claim.** Either (a) add `internal/testsupport/integrationtest` to the `no-test-only-constructs-in-production` depguard deny list in `.golangci.yaml` as part of this plan's Task 1 (closing the actual gap, and arguably overdue given the package has existed and grown for a while), or (b) reword the acceptance criteria and threat model to correctly attribute the protection to the `//go:build integration` tag rather than to depguard. Option (a) is stronger and cheap.
2. **Give plan 09-01 an explicit re-scope trigger.** State a concrete signal (e.g., "if neither hypothesis produces a non-empty profile after two fix attempts, stop and escalate as a phase-blocking finding" ) so a failed tracer doesn't silently retry indefinitely or, worse, get "fixed" by loosening the emptiness guard itself.
3. **Consider whether 09-19 Task 3's ruleset edit should be deferred to a moment strictly after the final rebase-before-push**, per `landing-the-plane.md`'s pre-push rebase step, to minimize the window in which a force-push could invalidate the just-verified statuses. The plan's own verification step re-checks after the edit, but doesn't explicitly sequence itself after the mandatory pre-push rebase.
4. **Reconcile the branch-name drift in CONTEXT.md/DISCUSSION-LOG.md against RESEARCH.md/actual `git`** before this planning-artifact set is archived.

## Risk Assessment

**LOW-to-MEDIUM.** The plan set is unusually well-grounded — nearly every load-bearing factual claim I checked against the live repository and live external APIs held up exactly, including several claims precise enough that a casual reader would assume they were approximated (drifted line numbers, exact false-positive counts, exact percentage figures). The two sizing-gate overrides (coverage target already met; ACE predicate tightened to ~114) are correctly justified and independently reproducible from the live codecov API and a local AST count, not merely asserted. The one real defect found (the 09-07 depguard mis-attribution) is narrow and easy to fix. The genuine residual risk is environmental and outside the plans' control — whether the E2E coverage-flush tracer (09-01) actually resolves with one of its two stated hypotheses, since the entire back half of the QUAL-02 chain (09-10, 09-17, 09-19) is transitively blocked on it succeeding. If it does resolve, the rest of the phase's mechanics (ABAC fix, cookie-default inversion, migration, ACE sweep, session matrix) are all soundly designed and independently low-risk.

---

## Consensus Summary

The two lanes reached **opposite verdicts** — codex HIGH / not-ready, kimi-k3 LOW-to-MEDIUM. The divergence is not noise, and resolving it is the main output of this review. The orchestrator independently verified the load-bearing findings from both lanes against source; all citations below were re-checked, not relayed.

### Why the lanes disagree — a wrong grep pattern produced a confident false negative

kimi-k3's Strengths list asserts: *"The harness helper inventory for QUAL-04 is fully accurate… `rg 'panic("TODO'` over the package returns zero hits."*

That command does return zero hits. But the blocker codex found is:

```
internal/testsupport/integrationtest/session.go:876:
    p.server.t.Fatalf("integrationtest.AuthedPlayer.OpenTelnetSession: TODO iwzt-16 — telnet transport differentiation requires Subscribe goroutine wiring")
```

`t.Fatalf(...)` is not `panic("TODO`. The query was wrong for the codebase's idiom, and the zero-hit result was read as proof of absence. **This is the same error 09-RESEARCH.md made** — it verified the eight helpers `holomush-izk0` happened to name, found no TODO panics among them, and concluded the harness was ready. Neither asked *"what does the telnet column actually require?"* and then checked that specific thing.

kimi-k3's review is otherwise excellent and independently re-verified a great deal (coverage figures against the live codecov API, the seven ABAC sentinel sites, migration numbering, the 38-cell matrix recomputed from the archived JSONL, the 32-vs-25 `TestINV_*` count). Its verdict is wrong only because one query silently under-reported.

### Verified blockers — all confirmed by the orchestrator against source

| # | Finding | Source | Impact |
|---|---|---|---|
| B1 | `OpenTelnetSession` is TODO-fatal | `internal/testsupport/integrationtest/session.go:876` | The telnet column — which D-12 calls "the highest-value column" — has **no harness**. The sole `ClientType:` (`session.go:459`) is hardcoded `"terminal"`. Plan 09-14 promises a genuine telnet reconnect while modifying only a test file. |
| B2 | `ExpireSession` cannot drive the reaper | `harness.go:995` sets `status='expired'`; `ListExpired` (`internal/store/session_store.go:446`) selects `WHERE status = 'detached' AND expires_at < now()` | An `ExpireSession`'d row is **never selected by the reaper**. "detach → ExpireSession → assert row deleted" would pass while proving nothing about production reaping. |
| B3 | `quit` is not dispatchable by default | `harness.go:468-470`: *"otherwise it gets an empty registry (no commands registered)"* | 09-12 starts from harness defaults; 09-14 drives `quit` through `SendCommand`. Needs `WithInTreePlugins`, explicit compiled-in registration, or a dedicated termination driver. |
| B4 | 09-17 / 09-19 need a PR the wave graph never creates | `git rev-parse @{u}` → *no upstream configured*; `gh pr list --head gsd/v0.12-milestone` → `[]` | Both plans require branch-side codecov reports and CI status rollups. No push/draft-PR checkpoint exists anywhere in waves 1–7. |
| B5 | 09-17's own gates do not prove its deliverable | `09-17-PLAN.md:95`, `:138` | Task 1 lists three PR numbers without checking they changed Go or querying status rollups; no `pipefail`, so a failed `gh` is masked by a successful `head`. Task 2 confirms two config literals without observing either codecov context. Same defect class the internal checker already fixed twice. |
| B6 | 09-07 mis-attributes its threat mitigation to depguard | `.golangci.yaml` deny list = `eventbustest`, `coretest`, `quarantinetest`, `natstest` — **not** `internal/testsupport/integrationtest` | 09-07 asserts depguard enforcement at `:102`, `:158`, `:164`, `:173`, including as a **threat-model mitigation** (T-09-07-01). The real protection is the `//go:build integration` tag. A threat model citing a control that does not exist is worse than one citing none. |

B1–B5 are codex's; B6 is kimi-k3's. Neither lane found the other's.

### Where both lanes agree
- The security-polish plans (09-03 ABAC, 09-04 cookie default, 09-05 migration, 09-06 audit drop) are soundly designed and correctly grounded.
- The two sizing-gate overrides (coverage target already met; ACE predicate tightened to ~114) are correctly justified and independently reproducible.
- 09-19 Task 1's codecov gate is genuinely fail-closed — kimi-k3 confirmed it exits 1 against `main` today, at the first sub-check.
- The meta-test ratchets (09-16 bijection, 09-18 AST naming) follow the `test/meta/quarantine_registry_test.go` precedent correctly, and the seed-a-violation-then-restore discipline is the right way to prove a ratchet is load-bearing.
- 19 plans is large but proportionate — kimi-k3's analysis is that the phase does five structurally distinct things that do not decompose further without conflating review surfaces (ABAC / crypto / migration / security-default / ACE-naming).

### Non-blocking items worth folding in
- **MEDIUM (codex):** `test:e2e:cover` neither removes `coverage-e2e.out` before running nor propagates `covdata textfmt` failure (`Taskfile.yaml:242,250`) — a failed conversion can leave a stale non-empty profile that satisfies the new emptiness check.
- **MEDIUM (codex):** 09-05 verifies only `task test:int`, proving the up migration applies but not that the down migration removes the index. `Migrator.Steps` (`internal/store/migrate.go:106`) and the precedent at `migrations_audit_shape_integration_test.go:109` support a real round-trip test.
- **MEDIUM (codex):** 09-12 verifies file existence + lint (`:113`), not 38 populated rows / 10 `n/a` / uniqueness — and downstream plans consume that registry before 09-16 validates it. 09-11 reintroduces `rg -c | wc -l` (`:95`).
- **MEDIUM (kimi-k3):** 09-01 is the tracer for the whole phase and has no documented fallback or re-scope trigger if neither root-cause hypothesis holds. The entire QUAL-02 chain (09-10, 09-17, 09-19) is transitively blocked on it.
- **MEDIUM (kimi-k3):** 09-19 Task 3's ruleset edit races the mandatory pre-push rebase in `landing-the-plane.md` — a force-push after the edit requires both coverage statuses to re-post on the new SHA before merge is possible.
- **LOW (codex):** `files_modified` drift — 09-07 omits the privacy suite (`:7`); 09-18 lists only its meta-test despite renaming ~114 files (`:7`). 09-18's "every old name disappears anywhere in the repository" (`:175`) conflicts with recording old names in its own committed summary.
- **LOW (kimi-k3):** CONTEXT.md `:263` and DISCUSSION-LOG `:253` name the branch `gsd/v0.12-foundation-hardening`; the actual branch is `gsd/v0.12-milestone` (the worktree directory carries the old name).

### Orchestrator verdict: NOT READY — replan required

B1–B3 are the substantive ones: **QUAL-04's telnet, reaper/TTL, and termination rows have no working harness seam**, and three plans (09-13, 09-14, 09-15) are written as though they do. Executing unchanged would either stall in waves 3–4 or — worse — substitute a `"terminal"`-labelled session for a telnet one and an `ExpireSession` row-poke for a reaper sweep, producing green specs that prove nothing. That is precisely the assurance theatre this phase exists to eliminate.

The replan should:
1. Insert a **QUAL-04 harness-prerequisite plan** before 09-12: parameterize attach by client type and implement `OpenTelnetSession`; add a detached-past-expiry seam that drives the real reaper (or reframe those rows to use `Server.Pool()`/session store directly per `harness.go:1286`); decide how quit/logout/admin-boot are driven through production entry points. Verify each seam independently before populating the matrix.
2. Add a **push / draft-PR checkpoint** before 09-17, and sequence 09-19's operator action strictly after the final pre-push rebase.
3. Rewrite **09-17's gates** to require both exact codecov contexts on real Go-changing PRs, treating API errors and missing contexts as failures.
4. Fix **09-07's threat model** — either add `internal/testsupport/integrationtest` to the depguard deny list (stronger, cheap) or re-attribute the mitigation to the build tag.
5. Fold in the MEDIUMs above; correct `files_modified` and the branch-name drift.

**Method note for the next reviewer.** Every miss in this round — research's, kimi-k3's, and the internal plan-checker's — has the same shape: a query that resolves cleanly while answering a narrower question than the one that mattered. Citations resolved (18/18). No `panic("TODO` existed. Both were true and neither established that the harness could do what the plans assumed. When verifying a capability, name the capability first and check *that*; do not verify a list someone else drew up and infer coverage from it.

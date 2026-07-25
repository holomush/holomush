# Phase 9: Test-Quality & Code-Health Sweep - Context

**Gathered:** 2026-07-25
**Status:** Ready for planning

<domain>
## Phase Boundary

Backfill coverage and remediate test/code-health debt to the reconciled bar.
This is the **closing phase of milestone v0.12 Foundation Hardening**. Four
fixed requirements (from `.planning/ROADMAP.md` §Phase 9 and
`.planning/REQUIREMENTS.md`):

- **QUAL-02:** Packages under the reconciled bar are backfilled with genuine
  behavioral tests.
- **QUAL-03:** Skeleton/weak tests are remediated to assert real behavior, and
  ACE test-naming violations are corrected to the sentence convention.
- **QUAL-04:** A session-lifecycle test matrix covers connect / reconnect /
  multi-character / idle-timeout paths.
- **QUAL-05:** A code-health & security-polish batch is applied — the
  arch-review Medium cluster, triaged at phase time.

Discussion clarified **how** to implement each. The requirement set is fixed and
NOT open for planning to re-scope. No new player-facing features.
</domain>

<decisions>
## Implementation Decisions

### QUAL-02 — Coverage backfill

- **D-01 (what "under the bar" means):** **Risk-ranked whole-repo audit.**
  Phase 6 (D-07) removed per-package measurement — codecov measures *patch*
  (changed lines) and *project* (whole repo), never per-package — so the
  requirement's literal wording has no operational meaning as written. Resolve
  it by running a fresh whole-repo coverage audit and ranking packages by
  **(uncovered statements × blast radius)**, weighting `internal/eventbus`,
  `internal/access`, `internal/crypto`, `internal/session`, and `internal/world`
  upward. The `holomush-0yo6` named set (`cmd/holomush`, `internal/tls`,
  `internal/xdg` @≥80%; `internal/core` @≥90%) is a **floor, not the
  definition** — those four are expected to surface in the audit and MUST be
  cleared regardless of where they rank.
- **D-02 (project target):** **70%+**, up from the ~54.6% baseline. This is a
  deliberately aggressive ~15-point lift — see the planner flag below.
- **D-03 (uncoverable code):** **Integration-flag coverage counts.** Packages
  like `cmd/holomush` whose real exercise is integration-only (`runCore()`,
  `cmd/holomush/core.go:166`) have their integration-upload coverage counted
  toward the package rather than being `codecov:ignore`d. Do NOT mark such code
  ignored to make a number move.
- **D-04 (gate sequencing):** **Require first at the current threshold, tighten
  last.** Add `codecov/patch` and `codecov/project` to the protect-main ruleset
  **early** (closes the gap Phase 6 explicitly flagged and never closed) while
  leaving `threshold: 1%`; drop to `threshold: 0%` (true no-drop ratchet) as the
  **final plan of the phase**, after the lift has landed.
  — **Reversibility:** costly — the ruleset change (`11923801`) is a GitHub
  repo-settings action performed by an operator, not a change any PR can make or
  revert; backing it out needs the same operator round-trip.
- **D-05 (no coverage theatre):** Backfill MUST be **genuine behavioral tests**
  (positive + negative paths, real assertions). A test written only to move a
  percentage is a QUAL-03 violation being created by QUAL-02 work.

> **⚠ Planner flags for QUAL-02:**
> - **54.6% → 70% is ~15 points on a large Go codebase.** This is the single
>   largest sizing risk in the phase. Research MUST report the audit's actual
>   per-package numbers and the estimated statement count needed to reach 70%
>   **before** the plan commits to the target. If the number is implausible for
>   one phase, surface it as an explicit re-scope decision rather than silently
>   planning around it.
> - **Ordering is load-bearing.** D-04's "require early" means every PR merged
>   during the phase — including unrelated ones and Phase 9's own — must clear
>   the codecov statuses. Plans that *delete* covered code or do
>   behaviour-preserving refactors can fail a project status even at
>   `threshold: 1%`. Sequence gate-enabling plans so they do not strand the
>   phase's own remaining plans.
> - **codecov config is `.codecov.yml`** (single authoritative file; Phase 6
>   D-08 deleted the duplicate). `notify.after_n_builds: 2` and
>   `wait_for_ci: true` are load-bearing — coverage is the merge of unit +
>   (integration/e2e) uploads. Do not raise `after_n_builds` to 3; the file's
>   own comment explains why that blocks notification entirely.
> - Multi-line `slog.*Context` error branches count as many uncovered lines —
>   budget error-branch tests explicitly or patch% tanks even with happy paths
>   covered (carried forward from Phase 6).

### QUAL-03 — Weak tests + ACE naming

- **D-06 (scope = union):** Take **both** sources. (a) Re-derive the
  `holomush-ec22.15` (ACE) and `holomush-ec22.16` (weak-test) site lists against
  current `HEAD`, fixing survivors and recording what drifted away; **and**
  (b) run a fresh repo-wide mechanical predicate sweep. Neither source alone is
  sufficient — see the evidence conflict below.
- **D-07 (ACE predicate):** A violation is **`TestX_Y` (underscore form) whose
  function declares NO subtests**, plus vague subtest strings
  (`"success"`, `"error case"`, `"happy path"`, `"test N"`, …). The sanctioned
  exception in `.claude/rules/testing.md` is `TestType_Method` *paired with
  subtests* — the predicate MUST honour it or it produces ~1653 false positives.
- **D-08 (remediation posture):** **Fix all hits — no allowlist.** Do not seed a
  shrink-only allowlist; remediate every violation the predicate returns so the
  end state is clean.
  — **Reversibility:** costly — a repo-wide test rename touches many files
  across many packages; undoing it is a mechanical revert but any concurrent
  branch rebasing over it pays the conflict cost.
- **D-09 (sizing gate):** Research **runs the predicate and reports the hit
  count BEFORE planning commits to D-08.** Above roughly **150 renames**, that
  triggers an explicit re-scope conversation rather than a silently expanded
  phase. "Fix all" is the intent; the count must be visible before it is locked.
- **D-10 (ordering):** **ACE sweep runs LAST, as a single pass.** Coverage
  backfill (QUAL-02) and the session matrix (QUAL-04) both *add* test files;
  the sweep *renames* existing ones. Running the sweep last avoids
  rename-vs-add conflicts across plans **and** makes the sweep verify the
  phase's own newly-written tests comply.
- **D-11 (skip-with-unreachable-setup):** The four
  `test/integration/eventbus_e2e/` files whose first line is `t.Skip("TODO…")`
  with unreachable setup below — **trim the body to `t.Skip` + a live GitHub
  issue reference**, deleting the maintained dead code.

> **⚠ Planner flags for QUAL-03:**
> - **Evidence conflict — resolve, do not assume.** The 2026-07-11 arch review
>   ran a scripted zero-assertion sweep
>   (`docs/reviews/arch-review/2026-07-11/findings/d9a-testing-ci.md:57`):
>   ~130 candidates → ~40 read by hand → **every one resolved to a genuine
>   assertion** (shared helpers `requireOpaqueInternal` / `requireWorldNotFound`
>   / `requireOopsCode`, strict-mockery "nothing unexpected was called" tests,
>   `analysistest.Run` fixtures, the `emit_intent_parity_test.go` reflection
>   guard). A predicate that flags those is wrong. Calibrate the predicate
>   against that list before trusting its output.
> - **The bead lists are 3 months stale (2026-04-25) and have drifted.**
>   `internal/session/memstore_test.go` — cited **8 times** across ec22.15 and
>   ec22.16 — **no longer exists** (Phase 7/8 refactors). Verified still
>   present: `TestStatus_String` at `internal/session/session_test.go:32`; six
>   `"happy path"` subtests in `internal/store/player_session_store_test.go`;
>   all four `eventbus_e2e` skip files; `internal/store/alias_test.go`,
>   `internal/world/object_test.go`, `internal/access/resolver_test.go`,
>   `internal/plugin/manifest_test.go`. Re-derive every cited site — do not
>   apply the list blind.
> - **Zero-assertion "compile canaries" have a better home.** ec22.16 flags
>   `TestLocationResolverSatisfiesInterface` / `TestAliasRepositoryInterface`
>   style tests: the same guarantee is expressed as
>   `var _ Interface = Type{}` at package scope, outside a test function.
> - The four skip files cite **retired bead IDs**. Each needs an open GitHub
>   issue (map or file one) before D-11's trim, or the reference dangles.
> - Only **one** vague subtest string exists repo-wide today — the subtest half
>   of the predicate is near-clean; the top-level naming half carries the work.

### QUAL-04 — Session-lifecycle test matrix

- **D-12 (scope):** **Full 12×4 matrix as `holomush-izk0` specifies** — every
  transition × {web guest, web regular char, telnet, multi-session}. Each of the
  48 cells gets either a passing spec or an explicit "covered elsewhere"
  pointer. Acceptance: `task test:int -- ./test/integration/session/...` runs
  **≥15 specs**. The telnet column is included deliberately —
  `test/integration/telnet/` contains **only** a suite bootstrap today, so it is
  the highest-value column.
- **D-13 (cell proof):** **Committed matrix + meta-test.** Check in the matrix
  table and add a meta-test asserting every named spec actually exists —
  modelled on the quarantine-registry bijection
  (`test/meta/quarantine_registry_test.go`). Coverage claims must not be able to
  silently rot. This is the same ratchet posture as D-04/D-08.
- **D-14 (fold-ins):** Fold in **both** adjacent privacy items:
  `holomush-dqd1`'s two named tests
  (`TestPrivacy_ReattachWithinTTLPreservesFloor`,
  `TestPrivacy_TTLExpiryEndsSessionFreshFloor` — izk0 explicitly says they
  "live here") **and** issue **#4682** (`holomush-hdnx`, the I-PRIV-6
  floor-preservation arm). Both are session-lifecycle × history-floor
  assertions; splitting them duplicates setup.
- **D-15 (controlled timestamps):** Add a **timestamped emit variant** —
  `EmitDirectEventAt(..., at time.Time)` or an option argument alongside the
  existing `Session.EmitDirectEvent`. Deterministic ordering with no sleeps.
  This also avoids adding a seventeenth site to the `~16 time.Sleep`
  async-synchronisation pattern that `holomush-ec22.13` already flags.
- **D-16 (anti-redundancy):** `test/integration/auth/multi_tab_test.go` already
  covers much of the multi-session column (two-tab guest, same-character
  two-tab, two-characters-one-player, logout cascade, Subscribe post-logout).
  The matrix MUST cite it for those cells rather than duplicating.

> **⚠ Planner flags for QUAL-04 — izk0's stated blocker is GONE:**
> - izk0 says the matrix is blocked on TODO-panic helpers in a `privacytest`
>   harness. **That package no longer exists** — it was consolidated into
>   `internal/testsupport/integrationtest/`, and **every named helper is
>   implemented**: `WaitForEvent` (`session.go:171`), `MoveTo` (`:303`),
>   `DetachTransport` (`:366`), `ReattachTransport` (`:419`),
>   `QueryStreamHistory` (`:705`), `QueryStreamHistoryBounded` (`:722`),
>   `ExpireSession` (`harness.go:995`). **Zero TODO panics remain.** QUAL-04 is
>   spec authoring, not harness construction — plan it accordingly.
> - `Session.EmitDirectEvent` (`session.go:770`) already **is** option (B) that
>   `holomush-hdnx` recommended building. Its signature is
>   `(ctx, stream, evType, payload)` — **no timestamp parameter**, which is the
>   only remaining gap D-15 closes.
> - `test/integration/session/session_persistence_suite_test.go` is **108 lines
>   of bootstrap with zero `Describe`/`It` blocks** — this is the empty suite
>   izk0 targets. Existing sibling coverage: `session_lease_test.go` (4 blocks),
>   `session_list_active_by_location_test.go` (5 blocks).
> - #4682's floor-arm test needs an `allowAllPolicyEngine` override in addition
>   to controlled timestamps; `integrationtest` exposes `WithRealABAC()` —
>   confirm which shape the test needs.

### QUAL-05 — Code-health & security-polish batch

- **D-17 (Medium-cluster triage):** **Four of five land; #4792 is deferred.**
  - **In:** #4793 (ABAC empty-string sentinels — latent fail-open),
    #4794 (secure-cookie/HSTS/CSP default), #4796 (`sessions.location_id`
    index), #4797 (plugin-decrypt audit emitter silently drops records).
  - **Deferred with rationale:** **#4792** (DEK read-cache bypassed, ~P+1
    `crypto_keys` reads/event). It is a **performance** change on the encrypted
    read path: it needs benchmarks proving the read amplification actually drops
    plus a `crypto-reviewer` pass, and neither is test-quality or code-health
    work. This satisfies the ROADMAP success criterion's "or explicitly deferred
    with rationale" clause. **Leave #4792 open and comment the deferral on it.**
- **D-18 (#4794 mechanism):** **Invert the default.** Secure cookies + HSTS +
  CSP are ON unless explicitly disabled for local dev, rather than gated on a
  default-false flag that leaves a TLS-terminating proxy serving non-`Secure`
  cookies. Fail-safe by default, consistent with the repo's default-deny posture
  everywhere else. **Requires a release note** — it changes behaviour for anyone
  running plain HTTP.
  — **Reversibility:** costly — reverting re-introduces the vulnerability and
  requires a second release note walking back a documented security default;
  the flag's meaning is now part of the operator-facing contract.
- **D-19 (#4796 migration shape):** **Plain `CREATE INDEX IF NOT EXISTS`.**
  Matches every existing index migration in the repo (e.g.
  `idx_sessions_player_session_id` in `000008_session_player_fk.up.sql`) —
  idempotent, transactional, with a paired `DROP INDEX IF EXISTS` down. There is
  **no `CREATE INDEX CONCURRENTLY` precedent** in the 52 existing migrations,
  and `CONCURRENTLY` cannot run inside a transaction, so it would require
  verifying/adding non-transactional runner support — a new capability, not a
  new migration.
  — **Reversibility:** costly — undo is a paired down migration, not a code
  revert; the migration number is consumed once merged.
- **D-20 (de-slop / humanization):** **Out of scope — explicitly deferred with
  rationale.** `holomush-89o9` is a **CLOSED** epic with **zero surviving member
  issues**; there is no scoped, verifiable work item behind it, and an unbounded
  prose sweep has no acceptance criterion. Record the deferral honestly rather
  than faking closure of QUAL-05's second half.
- **D-21 (ec22.9 security polish):** **Fold the cookie/TLS-coupling item only** —
  it *is* #4794, so handle it there and drop the duplicate. **Defer** the other
  three (argon2 dummy-hash entropy, `http.Server` write timeout, `addlicense`
  pin) as separate small issues; file them if no live issue exists.

> **⚠ Planner flags for QUAL-05 — all four in-scope items verified live:**
> - **#4793** — 7 sentinel sites remain: `internal/access/policy/attribute/`
>   `location.go:72,80`, `object.go:117,125,133`, `property.go:93,102`.
>   `character.go:139` is **already fixed** and carries the explanatory comment —
>   use it and `stream.go:40-48` as the in-repo reference form.
>   `.claude/rules/abac-providers.md` auto-loads on this path and specifies the
>   required shape (omit the key; always emit the `has_X` witness on every code
>   path). **`abac-reviewer` MUST run** before push.
> - **#4797** touches the plugin-decrypt audit path
>   (`internal/eventbus/history/plugin_downgrade_fence.go:423` is the
>   `f.emitter == nil` site). **`crypto-reviewer` MUST run** before push — this
>   surface is in its trigger list even though #4792 is deferred.
> - **#4796** verified: `sessions.location_id` has **no index** in any migration
>   (`000001_baseline.up.sql` has `idx_sessions_active_character` and
>   `idx_sessions_status`; `000008` adds `idx_sessions_player_session_id`).
>   Next free migration number is **000053**.
> - **#4794** — the flag is a `secure bool` threaded into
>   `internal/web/cookie.go:45` (`sessionCookie`) and
>   `internal/web/security_headers.go:66-83`. Note `cookie.go:41-55` already
>   constructs with `Secure: true` and *downgrades* when `secure=false`, so the
>   inversion is about the flag's **default and plumbing**, not the cookie
>   construction.

### Phase shape

- **D-22 (PR shape):** **One PR on the milestone branch.** All plans land on
  `gsd/v0.12-foundation-hardening` and ship as a single PR — matching the
  `git.branching_strategy: "milestone"` config that landed in #4852. Phase 9 is
  the only phase remaining in v0.12, so the milestone branch is effectively this
  phase's branch. **Worktree already created** at
  `/Volumes/Code/github.com/holomush/.worktrees/v0.12-foundation-hardening`.

### Claude's Discretion

- Exact risk weighting in the D-01 coverage ranking (which packages count as
  high blast radius, and by how much).
- The precise D-09 re-scope threshold (~150 is a starting number, not a
  requirement).
- Whether the D-15 timestamped emit is a new method or a variadic option on the
  existing `EmitDirectEvent`.
- Wave/plan decomposition within the D-04 (gate-early) → backfill → D-10
  (ACE-sweep-last) ordering constraint.
- Whether the D-13 matrix meta-test lives in `test/meta/` (alongside the
  quarantine and invariant registry guards) or beside the suite.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements / roadmap
- `.planning/ROADMAP.md:258-268` — Phase 9 goal, dependency on Phase 6, and the
  four success criteria.
- `.planning/REQUIREMENTS.md:46-49` — QUAL-02, QUAL-03, QUAL-04, QUAL-05 text.
- `.planning/ROADMAP.md:410-420` — backlog cluster 999.10 (code health &
  test-quality program), the strategic parent of these requirements.
- `.planning/phases/06-operational-hardening-assurance-gates/06-CONTEXT.md`
  §QUAL-01 (D-07/D-08) — the coverage reconciliation this phase builds on, and
  the ruleset step it left unfinished.

### Source-of-truth for QUAL-02/03/04 detail (⚠ NOT in GitHub Issues)
> The 2026-07-09 beads→Issues migration **closed** the mirror issues for these
> items (#3859 QUAL-02, #2381 ACE, #2380 weak tests, #3918 de-slop). Their full
> descriptions — including the izk0 matrix table and the ec22.15/16 site lists —
> survive **only** in the archive JSONL below. Read it; do not assume GitHub has
> the detail.
- `.planning/archive/beads/2026-07-09-beads-live.jsonl` — records
  `holomush-izk0` (12×4 matrix + anti-redundancy + acceptance),
  `holomush-0yo6` (coverage targets per package),
  `holomush-ec22.15` (ACE site list), `holomush-ec22.16` (weak-test site list),
  `holomush-dqd1` (the two named privacy tests),
  `holomush-hdnx` (I-PRIV-6 floor arm + the emit-mechanism options A/B).
- `.planning/archive/beads/TRIAGE.md:238-247` — the "Code health &
  test-quality program" cluster membership.

### Arch-review findings (2026-07-11, PR #4807)
- `docs/reviews/arch-review/2026-07-11/findings/d9a-testing-ci.md` — HIGH-1
  coverage-not-enforced; **§Strengths:57 is the clean zero-assertion sweep** that
  the QUAL-03 predicate must be calibrated against.
- `docs/reviews/arch-review/2026-07-11/issue-plan.md:58-63` — the Medium cluster
  rows I12/I13/I14/I16/I17 with their source file:line citations;
  issue-number map at `:93-102`.
- `docs/reviews/arch-review/2026-07-11/REPORT.md:276` — the Medium cluster as a
  named group.
- `docs/reviews/arch-review/2026-07-11/findings/d2-abac.md` — M1, the
  empty-string sentinel finding (#4793).
- `docs/reviews/arch-review/2026-07-11/findings/d4-perimeter.md` — M1, the
  secure-cookie/HSTS/CSP finding (#4794).
- `docs/reviews/arch-review/2026-07-11/findings/d6-reliability.md` — M, the
  silent audit-emitter drop (#4797).
- `docs/reviews/arch-review/2026-07-11/findings/d7-data.md` — the
  `sessions.location_id` index finding (#4796).
- `docs/reviews/arch-review/2026-07-11/findings/d5-performance.md` — M1, the
  DEK read-cache finding (#4792, **deferred** — read only to write the deferral
  rationale).

### Live GitHub issues (verified OPEN 2026-07-25)
- **#4793** ABAC empty-string sentinels · **#4794** secure-cookie default ·
  **#4796** `sessions.location_id` unindexed · **#4797** silent audit-emitter
  drop · **#4682** I-PRIV-6 floor arm — all **in scope**.
- **#4792** DEK read-cache — **deferred**; comment the rationale on it.
- Query with `gh issue view <n> -R holomush/holomush`.

### ADRs
- `docs/adr/holomush-ti1b-providers-omit-optional-attrs.md` — the omit-don't-
  sentinel rule #4793 restores.
- `docs/adr/holomush-iv43-cedar-aligned-fail-safe-type-semantics.md` — the
  missing-attribute fail-safe semantics that empty-string sentinels defeat.

### Repo rules (auto-load on their paths; apply during implementation)
- `.claude/rules/testing.md` — ACE naming, coverage semantics, test tiers,
  quarantine, `// Verifies:` invariant bindings.
- `.claude/rules/references/testing-detail.md` — ACE examples, table-driven
  shape, quarantine policy, test-tier detail.
- `.claude/rules/abac-providers.md` — the required omit-plus-`has_X`-witness
  form for #4793.
- `.claude/rules/database-migrations.md` — #4796's migration (idempotent, paired
  up/down, no triggers/functions).
- `.claude/rules/invariants.md` — if any new guarantee here rises to a registry
  invariant (the D-13 matrix meta-test may).
- `.claude/rules/logging.md` — #4797's fix adds a log/metric on the drop path;
  `*Context` variants are mandatory where a `ctx` is in scope.

### Config / gate surfaces
- `.codecov.yml` — project ratchet (`target: auto`, `threshold: 1%`) and patch
  (`target: 80%`, `threshold: 5%`); `notify.after_n_builds: 2` +
  `wait_for_ci: true`. Single authoritative file.
- `.github/workflows/ci.yaml` — the jobs codecov consumes.
- protect-main ruleset **`11923801`** — where D-04's "make required" lands
  (operator action, not an in-repo change).
- `test/meta/quarantine_registry_test.go` — the bijection meta-test D-13 models.
- `site/src/content/docs/contributing/how-to/integration-tests.md` — harness
  tiers and `WithInTreePlugins()` semantics.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`internal/testsupport/integrationtest/`** — the canonical in-process stack
  and the whole of QUAL-04's substrate. Already implements every helper izk0
  listed as a blocker: `Session.WaitForEvent` (`session.go:171`),
  `Session.MoveTo` (`:303`), `Session.DetachTransport` (`:366`),
  `Session.ReattachTransport` (`:419`), `Session.QueryStreamHistory` (`:705`),
  `Session.QueryStreamHistoryBounded` (`:722`),
  `Session.EmitDirectEvent` (`:770`), `Server.ExpireSession`
  (`harness.go:995`). Options include `WithInTreePlugins()`, `WithRealABAC()`,
  `WithPluginCrypto()`.
- **`test/integration/auth/multi_tab_test.go`** — pre-existing multi-session
  coverage the matrix cites rather than duplicates (D-16).
- **`test/integration/privacy/privacy_test.go`** — already drives
  `integrationtest.Start(suiteT)`; the I-PRIV-6 gate-bypass arm lives here and
  #4682 completes it.
- **`internal/access/policy/attribute/character.go:139`** and
  **`stream.go:40-48`** — the two providers already in the correct
  omit-the-key form; the reference implementations for fixing the other three.
- **`test/meta/quarantine_registry_test.go`** — the registry↔marker bijection
  pattern D-13's matrix meta-test copies.
- **`internal/store/migrations/000008_session_player_fk.up.sql`** — the exact
  index-migration shape #4796 follows (`CREATE INDEX IF NOT EXISTS` + paired
  `DROP INDEX IF EXISTS`).

### Established Patterns
- **Ratchet-not-review-judgment** (Phase 8, `INV-PLUGIN-56`): a committed
  mechanical gate, not reviewer discipline, is what stops regrowth. D-04, D-08,
  and D-13 all follow it.
- **Test tiers**: unit (`task test`) never compiles `//go:build integration`
  files — any shared-type change MUST also run `task test:int`.
- **ACE naming**: sentence-form names; `TestType_Method` allowed *only* with
  subtests; subtests lowercase sentences.
- **Quarantine three-marker idiom** + `test/quarantine.yaml` bijection — the
  auditable-debt pattern, relevant background for D-11's disposition of the four
  skip files.
- **Error assertions**: `errutil.AssertErrorCode` / `assert.ErrorIs` over string
  matching (`holomush-ec22.14` flags ~20 string-match sites — adjacent to but
  not inside QUAL-03's scope).

### Integration Points
- Coverage gates → `.codecov.yml` (in-repo) **plus** ruleset `11923801`
  (operator action). Both halves are needed for D-04.
- ACE predicate → likely a `gorules/analyzers/` module-plugin or a
  `test/meta/` walker. Note the golangci-lint v2 constraint: **one
  `register.Plugin` call = one enableable linter id**.
- Session matrix → `test/integration/session/session_persistence_suite_test.go`
  (currently bootstrap-only, 108 lines, zero `Describe`/`It`).
- #4794 → `internal/web/cookie.go:45`, `internal/web/security_headers.go:66-83`,
  and the gateway flag plumbing that feeds `secure bool`.
- #4796 → new migration `000053_*` on the `sessions` table.
- #4797 → `internal/eventbus/history/plugin_downgrade_fence.go:423`.

</code_context>

<specifics>
## Specific Ideas

- **The aggressive end was chosen deliberately on every coverage axis** —
  risk-ranked audit over the safe named allowlist, 70%+ over a modest lift, and
  no-drop + required over leaving the gate half-wired. This mirrors Phase 6's
  posture (take the harder, durable path in a foundation-hardening milestone),
  and it means the sizing evidence in the QUAL-02 planner flags is the check
  that keeps it honest rather than the target being softened preemptively.
- **"Fix all, no allowlist" was chosen over a shrink-only allowlist** —
  a clean end state rather than a managed-debt register. D-09's sizing gate is
  the release valve; use it rather than quietly reintroducing an allowlist.
- **Every assurance claim in this phase gets a mechanical prover.** The arch
  review named its own root theme as *"assurance artifacts (docs, tests, UI
  copy) overstate what the code delivers."* A phase about test quality that
  shipped an unverified matrix table would reproduce exactly that defect — hence
  D-13.
- **`claude_orchestration` beta (from #4852) has never activated.** Phase 9 is
  the first phase since it was enabled. Its five-condition gate is fail-closed
  and returns `inline(<reason>)` on any miss, so it cannot regress execution —
  but it may also silently never fire. Observe whether wave parallelism returns
  during `/gsd-execute-phase`; this is an observation, not a phase requirement.

</specifics>

<deferred>
## Deferred Ideas

- **#4792 — DEK read-cache bypassed on the encrypted read path.** Deferred with
  rationale (D-17): a performance change needing benchmarks + `crypto-reviewer`,
  not test-quality or code-health work. Leave open; comment the deferral.
- **De-slop / humanization (`holomush-89o9`).** Deferred with rationale (D-20):
  the epic is closed with zero surviving member issues, so there is no scoped,
  verifiable work item and no acceptance criterion for an unbounded prose sweep.
- **`holomush-ec22.9` residue** — argon2 dummy-hash entropy, `http.Server` write
  timeout, `addlicense` pin. Small and security-adjacent, but not named in
  QUAL-05's text (D-21). File as issues if none exist.
- **`holomush-ec22.13`** — ~16 `time.Sleep` async-synchronisation sites.
  Adjacent to QUAL-03; D-15 avoids *adding* to the pattern but does not retire
  the existing sites.
- **`holomush-ec22.14`** — ~20 string-match error assertions that should use
  `errutil.AssertErrorCode` / `errors.Is`. Test-rigour work, but outside
  QUAL-03's weak-test/ACE-naming boundary as written.
- **`holomush-ec22.22`** — archive ~30 stale plans + consolidate phase-7.x ABAC
  plan files. Documentation hygiene; belongs with the docs program (999.15).
- **Implementing the four `eventbus_e2e` TODO tests** (audit drift detector, JS
  storage corruption, multi-protocol fanout, backfill rebuild). D-11 only trims
  their dead scaffolding; writing them is substantial eventbus work of its own.
- **Phase-8 merge follow-ups** — #4830 (invariant-summary overclaim, merged
  unfixed), #4831 (`ConfigureEventEmitter` lost update, needs a design call),
  #4850, #4829, #4828 (pre-existing outbox race). Not Phase 9 scope; noted so
  they are not lost at milestone close.
- **`loader.go` (1142 LoC)** — the largest remaining unit after Phase 8 and the
  next split candidate. Architecture work (999.9), not code health.

</deferred>

---

*Phase: 9-test-quality-code-health-sweep*
*Context gathered: 2026-07-25*

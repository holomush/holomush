---
quick_id: 260730-wh1
phase: quick-260730-wh1
verified: 2026-07-31T04:00:14Z
status: passed
score: 7/7 must-haves verified
behavior_unverified: 0
overrides_applied: 0
---

# Quick Task 260730-wh1: Fix GH #4892 dependency-only exempt paths — Verification Report

**Task goal:** the "Dependency-only" PR-exemption path list must describe what Renovate
actually touches, live in one authoritative machine-readable home, and shed the two false
claims it shipped with (`web/bun.lock`, which does not exist; "Renovate PRs are exempt by
definition", false for three of four open Renovate PRs).

**Verified:** 2026-07-31T04:00:14Z
**Worktree:** `/Volumes/Code/github.com/holomush/.worktrees/fix-4892-dep-exempt-paths` @ `b8fdbc486`
**Re-verification:** No — initial verification
**Method:** every gate below was re-executed by the verifier in its own process. The exit
codes recorded are the ones the verifier OBSERVED, not the ones SUMMARY.md claimed. No
observed value contradicted a SUMMARY claim.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence (observed) |
| --- | --- | --- | --- |
| 1 | Every entry in the dependency-only path set corresponds to ≥1 real tracked path — no fabricated file (D-01) | ✓ VERIFIED | `git ls-files -- ':(glob)<g>'` per glob: `**/go.mod`→2, `**/go.sum`→2, `**/package.json`→2, `**/pnpm-lock.yaml`→1, `**/pnpm-workspace.yaml`→1, `**/bun.lock`→1, `**/uv.lock`→2, `compose*.yaml`→5, `Dockerfile`→1, `**/Dockerfile`→2. Gate **exit 0**. Fabricated-path grep `web/bun\.lock` (excl. `.git/`, `.planning/`) inverted → **exit 0** (zero hits) |
| 2 | A lockfile-only diff under `scripts/` reads as EXEMPT at ALL FIVE `scripts/**` claim sites, with no site simultaneously calling it non-exempt (D-02) | ✓ VERIFIED | Set-pinned, line-scoped 3-line-window gate → **exit 0**, printing `OK` for all five: `CONTRIBUTING.md:114`, `CONTRIBUTING.md:172`, `.github/PULL_REQUEST_TEMPLATE.md:64`, `.github/PULL_REQUEST_TEMPLATE/chore.md:26`, `.github/ISSUE_TEMPLATE/chore.yml:26`. Discovered file set equalled the four expected policy files exactly (drift in either direction fails) |
| 3 | A dependency PR also carrying regenerated `.pb.go` / `_pb.ts` reads as NOT exempt in both full-restatement sites (D-03) | ✓ VERIFIED | `rg -q "_pb\.ts"` in both → **exit 0**. Read verbatim: `CONTRIBUTING.md:162-166` and `.github/PULL_REQUEST_TEMPLATE.md:54-58` each state it is **not** exempt and cite the `automerge: false` rationale |
| 4 | The categorical "Renovate PRs are exempt by definition" claim appears nowhere; exemption is stated as path-derived (D-03) | ✓ VERIFIED | `rg "exempt +by +definition"` repo-wide (excl. `.git/`, `.planning/`) inverted → **exit 0** (zero hits). Replacement at `CONTRIBUTING.md:161-162`: "a Renovate PR is exempt if and only if its diff stays inside those shapes — being a Renovate PR is not itself an exemption" |
| 5 | `Taskfile.yaml` carries `vars.DEPENDENCY_ONLY_PATHS` as single authoritative list; both full-restatement sites name it authoritative (D-01) | ✓ VERIFIED | `yq -e '.vars.DEPENDENCY_ONLY_PATHS'` set-equality against the ten expected globs (no `sort -u` either side, so a dup fails) → **exit 0**. Key present outside comment text → **exit 0**. `rg -q "DEPENDENCY_ONLY_PATHS"` in both prose files → **exit 0**. Glob-coverage loop driven FROM the authoritative var, asserting each glob appears literally in both prose files → **exit 0** |
| 6 | An implementer of #4890 learns, before writing code, that `scripts/docs-paths-regex.sh` cannot compile these shapes — hard-errors on `**/foo`, silently miscompiles `compose*.yaml` | ✓ VERIFIED | Comment present at `Taskfile.yaml:41-66` (all five mandated points). **The verifier independently proved the warning is TRUE, not just present** — see "Matcher-limitation validation" below. Both documented failure modes reproduced |
| 7 | `task lint:markdown`, `task lint:yaml`, `task fmt:check`, `task lint:docs-paths-sync` all exit 0 | ✓ VERIFIED | Re-run inline by the verifier: **all four exit 0** (see table below) |

**Score:** 7/7 truths verified (0 present, behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
| --- | --- | --- | --- |
| `Taskfile.yaml` | `vars.DEPENDENCY_ONLY_PATHS` block scalar + matcher-limitation comment | ✓ VERIFIED | +37/-0. Literal-block-scalar `KEY: |` form matching `DOCS_ONLY_PATHS`, ten globs at lines 67-77, 26-line comment block at 41-66. `DOCS_ONLY_PATHS` itself untouched (only referenced in the new comment) |
| `CONTRIBUTING.md` | amended `### Exempt by file path` (heading UNCHANGED) + chore-intake paragraph | ✓ VERIFIED | +25/-12. Heading byte-identical: `main:CONTRIBUTING.md:147` and `HEAD:CONTRIBUTING.md:148` both `### Exempt by file path` (line number shifted by the +1 chore-intake line only) |
| `.github/PULL_REQUEST_TEMPLATE.md` | amended `## Exemptions` | ✓ VERIFIED | +19/-4. Full restatement w/ plain-text authoritative pointer (no link — matches that file's style) |
| `.github/PULL_REQUEST_TEMPLATE/chore.md` | lockfile qualifier only | ✓ VERIFIED | +3/-1, qualifier clause inside the existing `>` blockquote |
| `.github/ISSUE_TEMPLATE/chore.yml` | lockfile qualifier only, indentation preserved | ✓ VERIFIED | +3/-1. `task lint:yaml` exit 0 proves the `markdown.value:` block scalar still parses |

### Key Link Verification

| From | To | Via | Status | Details |
| --- | --- | --- | --- | --- |
| Both full-restatement prose sites | `Taskfile.yaml` `DEPENDENCY_ONLY_PATHS` | named-as-authoritative | ✓ WIRED | `CONTRIBUTING.md:159-161` markdown link + "which is the authoritative version" (mirrors the `DOCS_ONLY_PATHS` precedent at 154-155); `PULL_REQUEST_TEMPLATE.md:53-54` plain-text pointer |
| chore.md:28, chore.yml:29, CONTRIBUTING.md:111 | `#exempt-by-file-path` | anchor link | ✓ WIRED | All three inbound targets confirmed present; heading unchanged, so the anchor still resolves. rumdl MD057 is disabled, so this was checked by hand as the plan required |
| `**/uv.lock` glob | `scripts/**` lockfile carve-out | all five claim sites | ✓ WIRED | Line-scoped gate exit 0 at all five; the contradiction is closed, not relocated |
| `DEPENDENCY_ONLY_PATHS` | #4890 (first consumer) | matcher-limitation comment | ✓ WIRED | Comment names #4890 explicitly and forbids pointing the existing helper at the var |

### Matcher-limitation validation (Truth 6 — warning proved TRUE, not merely present)

A warning that is present but wrong would be its own defect. The verifier ran
`scripts/docs-paths-regex.sh` against synthetic inputs via `REPO_ROOT` (the repo's own
Taskfile untouched):

| Documented claim | Verifier's observation | Verdict |
| --- | --- | --- |
| hard-errors `unsupported '**' position`, exit 1, on leading `**/` globs | stderr `ERROR: glob '**/go.mod' has unsupported '**' position`, **exit 1** | ✓ accurate |
| affects eight of the ten entries | 8 entries carry a leading `**/` and none is the `**/*.md` special case | ✓ accurate |
| `compose*.yaml` falls to the literal branch, yielding ERE `compose*\.yaml` | emitted `^(compose*\.yaml)$`, **exit 0** — no error | ✓ accurate |
| that ERE matches `compose.yaml` but NOT `compose.prod.yaml` | `grep -E`: `compose.yaml` MATCHES, `compose.prod.yaml` NO MATCH | ✓ accurate — silent miscompile confirmed |
| `scripts/docs-paths-regex.sh` left unmodified | `git diff main...HEAD -- scripts/docs-paths-regex.sh` → 0 lines | ✓ accurate |

### Gate Execution (verifier-observed exit codes)

| Gate | Command | Observed exit |
| --- | --- | --- |
| `DEPENDENCY_ONLY_PATHS` set-equality (ten globs, dup-sensitive) | `yq -e` + sorted string compare | 0 |
| every glob matches ≥1 tracked file | `git ls-files -- ':(glob)$g'` loop | 0 |
| key present outside comment text | `rg -v '^\s*#' \| rg -q` | 0 |
| fabricated path absent repo-wide | inverted `rg "web/bun\.lock"` | 0 |
| categorical Renovate claim absent repo-wide | inverted `rg "exempt +by +definition"` | 0 |
| `DEPENDENCY_ONLY_PATHS` named in both prose files | `rg -q` ×2 | 0 |
| `_pb.ts` codegen carve-out in both prose files | `rg -q` ×2 | 0 |
| claim-site qualifier (set-pinned + 3-line window per line) | set compare + per-line `sed`/`rg -qi lockfile` | 0 (5/5 `OK`) |
| `### Exempt by file path` heading present & byte-identical | `rg -qF` + `git show main:` compare | 0 |
| glob coverage in both prose files, driven from the var | `yq -e` + `rg -qF` loop | 0 |
| `task lint:markdown` | inline | 0 |
| `task lint:yaml` | inline | 0 |
| `task fmt:check` | inline | 0 |
| `task lint:docs-paths-sync` | inline | 0 (`docs-paths in sync across Taskfile + ci.yaml + ci-docs-skip.yaml.`) |
| clean tree vs BOTH index and HEAD | `git diff --quiet && git diff --cached --quiet` | 0 |
| #4892 carries `confirmed-bug` | `gh issue view … \| rg -q '^confirmed-bug$'` | 0 |

Both claimed commits exist: `git cat-file -t 2255f14f7` → `commit`, `b8fdbc486` → `commit`.
Working tree carries only the untracked `.planning/quick/260730-wh1-…/` directory, which the
orchestrator owns.

### Real-PR Walkthrough (goal-backward: does a reader get the right answer today?)

File lists fetched live via `gh pr view <n> -R holomush/holomush --json files` — they match
CONTEXT.md exactly, so the scenarios are grounded in reality, not in planning notes.

| PR | State | Files (live) | Matching shapes | Verdict under current text | Correct? |
| --- | --- | --- | --- | --- | --- |
| #4848 | OPEN | `web/package.json`, `web/pnpm-lock.yaml` | `**/package.json`, `**/pnpm-lock.yaml` | **EXEMPT** | ✓ |
| #4550 | OPEN | `compose.prod.yaml`, `go.mod`, `go.sum`, `site/bun.lock`, `site/package.json` | `compose*.yaml`, `**/go.mod`, `**/go.sum`, `**/bun.lock`, `**/package.json` | **EXEMPT** | ✓ |
| #4847 | MERGED | `scripts/uv.lock`, `.claude/skills/holomush-dev/scripts/uv.lock`, `site/bun.lock`, `web/pnpm-lock.yaml` | `**/uv.lock` ×2, `**/bun.lock`, `**/pnpm-lock.yaml` | **EXEMPT — and NOT contradicted by the `scripts/**` rule** | ✓ |
| #4851 | OPEN | `site/bun.lock`, `site/package.json` | `**/bun.lock`, `**/package.json` | **EXEMPT** | ✓ |

Notes on the two scenarios that could have gone wrong:

- **#4847 is the contradiction test.** With `**/uv.lock` exempt, an unqualified `scripts/**`
  non-exempt claim would make `scripts/uv.lock` simultaneously exempt and gated — the exact
  defect #4892 reports, merely relocated. All five claim sites now carry the carve-out
  ("except a **lockfile** under `scripts/` matching the dependency-only shapes, which is
  exempt"), and the canonical statement at `CONTRIBUTING.md:172-177` scopes it precisely:
  "Any non-lockfile change under `scripts/` stays gated." No reading of the current text
  yields both verdicts. `.claude/skills/holomush-dev/scripts/uv.lock` is covered by
  `**/uv.lock` on its own (note: `.claude/skills/**` is NOT in `DOCS_ONLY_PATHS`, so the
  dependency glob is doing real work there).
- **#4550 is the `compose.prod.yaml` test.** Read as a glob — which is how the prose presents
  it — `compose*.yaml` matches. The ERE miscompile that would MISS it is a machine hazard
  only, and it is exactly what the `Taskfile.yaml` comment warns #4890 about.

### Scope Boundaries (confirmed held)

| Boundary | Observed |
| --- | --- |
| chore.md / chore.yml gained qualifier ONLY | `rg 'go\.mod\|package\.json\|bun\.lock\|uv\.lock\|DEPENDENCY_ONLY_PATHS\|_pb\.ts\|pnpm'` in both → exit 1 (zero hits). They still defer to CONTRIBUTING.md by anchor link |
| no `lint:dependency-paths-sync` gate added | `rg 'dependency-paths-sync'` across `Taskfile.yaml`, `scripts/`, `.github/` → exit 1 |
| `scripts/docs-paths-regex.sh` unmodified | 0 diff lines |
| `DOCS_ONLY_PATHS` unmodified | Taskfile diff is +37/-0; the only `DOCS_ONLY_PATHS` occurrence in the diff is the new comment referencing it |
| `pr-guide.md` unaffected | Carries no `scripts/**` claim and no path enumeration; its CONTRIBUTING.md link at :85 has no anchor fragment. Untouched, correctly |

### Anti-Patterns Found

None. `rg 'TBD|FIXME|XXX|TODO|HACK|PLACEHOLDER|coming soon|not yet implemented'` over the
added lines of `git diff main...HEAD` → exit 1 (zero hits). No stubs, no placeholders, no
unreferenced debt markers.

### Observations (informational — not gaps)

1. **`Dockerfile` gets belt-and-braces; `go.mod` / `go.sum` / `package.json` do not.** The
   `Taskfile.yaml` comment justifies listing both `Dockerfile` and `**/Dockerfile` because
   "not every glob dialect" treats `**/Dockerfile` as matching a root-level file. That same
   dialect hazard applies to root `go.mod`, `go.sum`, and root-adjacent manifests, which are
   listed only in `**/` form. Under the two dialects the repo actually uses — git `:(glob)`
   pathspec (verified: `**/go.mod` → 2 matches, including root) and Go doublestar — this is
   correct and PR #4550 resolves as exempt. Recorded so #4890 picks a matcher that preserves
   it rather than rediscovering the asymmetry. Not a gap: the ten globs are a LOCKED D-01
   decision and every one matches ≥1 tracked file.
2. **`task pr-prep` was not run.** It is named in the plan's `<success_criteria>` but it is
   NOT one of the seven `must_haves.truths` (which name only the four lint/format gates, all
   observed exit 0). The plan assigns it explicitly to the parent session as the final
   pre-push gate, and CLAUDE.md forbids accepting a sub-agent's claim that it passed — so it
   correctly remains the orchestrator's ship-time step. Flagged as a pre-push reminder, not a
   verification gap.

### Gaps Summary

None. All seven must-have truths verified against the codebase with verifier-observed exit
codes. All five artifacts exist, are substantive, and are wired to each other and to the
authoritative variable. The four real-PR scenarios that motivated #4892 now resolve to the
correct verdict, and #4847 — the case that would have exposed a merely-relocated
contradiction — resolves unambiguously.

Every SUMMARY.md gate claim was independently re-executed. No claim was contradicted by
observation.

---

_Verified: 2026-07-31T04:00:14Z_
_Verifier: Claude (gsd-verifier)_

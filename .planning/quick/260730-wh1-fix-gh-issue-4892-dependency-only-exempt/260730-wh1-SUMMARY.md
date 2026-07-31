---
quick_id: 260730-wh1
phase: quick-260730-wh1
plan: 01
title: "Fix GH #4892 — dependency-only exempt path list does not match reality"
issue: 4892
status: complete
date: 2026-07-30
subsystem: repo-governance
tags: [contributing, exemptions, renovate, taskfile, policy-docs]
requires: []
provides:
  - "vars.DEPENDENCY_ONLY_PATHS in Taskfile.yaml (authoritative dependency-only glob set)"
affects:
  - "GH #4890 — the enforcement workflow that will be DEPENDENCY_ONLY_PATHS's first consumer"
tech-stack:
  added: []
  patterns:
    - "Authoritative machine-readable list in Taskfile.yaml vars + prose mirrors that name it as authoritative (mirrors the DOCS_ONLY_PATHS precedent)"
key-files:
  created: []
  modified:
    - Taskfile.yaml
    - CONTRIBUTING.md
    - .github/PULL_REQUEST_TEMPLATE.md
    - .github/PULL_REQUEST_TEMPLATE/chore.md
    - .github/ISSUE_TEMPLATE/chore.yml
decisions:
  - "D-01: exemption defined by manifest/lockfile SHAPE globs, authoritative in Taskfile.yaml"
  - "D-02: a lockfile under scripts/ is exempt; any non-lockfile scripts/ change stays gated — stated explicitly at all five claim sites"
  - "D-03: categorical 'Renovate PRs are exempt by definition' deleted; replaced with a path-derived rule plus an explicit codegen-carve-out"
metrics:
  tasks: 3
  files: 5
  commits: 3
  completed: 2026-07-30
---

# Quick Task 260730-wh1: Fix GH #4892 dependency-only exempt paths — Summary

The dependency-only PR exemption now describes what Renovate actually touches, has one
authoritative machine-readable home in `Taskfile.yaml`, and no longer carries the two false
claims (`web/bun.lock`, which does not exist; and "Renovate PRs are exempt by definition",
false for three of the four Renovate PRs open on 2026-07-30).

## What changed, per file

| File | Change |
| --- | --- |
| `Taskfile.yaml` | New `vars.DEPENDENCY_ONLY_PATHS` literal-block scalar, placed immediately after `DOCS_ONLY_PATHS`, plus the mandated comment block (authoritative-mirror note; no machine consumer / no sync gate yet, #4890 will be first; `docs-paths-regex.sh` hard-errors on leading `**/`; it silently miscompiles a `foo*.bar` shape to an ERE that misses `foo.baz.bar`; why `Dockerfile` and `**/Dockerfile` are both listed). **Superseded by the post-review revision below — the set is now 15 entries, not 10.** |
| `CONTRIBUTING.md` | Dependency-only bullet rewritten (shape-based globs, `DEPENDENCY_ONLY_PATHS` named authoritative via markdown link, path-derived Renovate rule, `.pb.go`/`_pb.ts` codegen carve-out). Lockfile carve-out added to BOTH `scripts/**` claims — the chore-intake paragraph (:114) and the canonical statement below the exempt list (:172). |
| `.github/PULL_REQUEST_TEMPLATE.md` | Same full restatement (plain-text pointer to `DEPENDENCY_ONLY_PATHS`, no link — matching that file's existing style) plus the lockfile carve-out on its `scripts/**` claim. |
| `.github/PULL_REQUEST_TEMPLATE/chore.md` | Lockfile qualifier clause only, inside the existing `>` blockquote. No globs, no pointer, no codegen carve-out — it still defers to CONTRIBUTING.md by anchor link. |
| `.github/ISSUE_TEMPLATE/chore.yml` | Lockfile qualifier clause only, inside the `>` blockquote nested in the markdown `value:` block scalar. Indentation preserved. |

## Verification

Every gate below was observed RED against the pre-fix tree before any edit, so a PASS is
evidence rather than a gate that cannot fire.

### Pre-fix RED confirmation (run before Task 1)

| Gate | Observed exit |
| --- | --- |
| `confirmed-bug` label on #4892 (precondition) | 0 — label present |
| fabricated-path grep (`web/bun\.lock`, inverted) | 1 (RED, as predicted) |
| categorical-claim grep (`exempt +by +definition`, inverted) | 1 (RED) |
| `yq -e '.vars.DEPENDENCY_ONLY_PATHS'` | 1 (RED — key absent) |
| claim-site qualifier gate | 1 (RED) — named exactly the five predicted lines: `CONTRIBUTING.md:114`, `CONTRIBUTING.md:162`, `.github/PULL_REQUEST_TEMPLATE.md:56`, `.github/PULL_REQUEST_TEMPLATE/chore.md:26`, `.github/ISSUE_TEMPLATE/chore.yml:26` |

### Task 1 gates

| Gate | Exit |
| --- | --- |
| `DEPENDENCY_ONLY_PATHS` set-equality against the ten expected globs | 0 |
| every glob matches ≥1 tracked file (`git ls-files -- ':(glob)…'`) | 0 |
| key present outside comment text | 0 |
| `task lint:yaml` | 0 |
| `task lint:docs-paths-sync` | 0 |

### Task 2 gates

| Gate | Exit |
| --- | --- |
| fabricated path absent repo-wide (excl. `.git/`, `.planning/`) | 0 |
| categorical Renovate claim absent repo-wide | 0 |
| `DEPENDENCY_ONLY_PATHS` named in both full-restatement files | 0 |
| `_pb.ts` codegen carve-out in both full-restatement files | 0 |
| claim-site qualifier gate (set-pinned + 3-line window per claim line) | 0 |
| `### Exempt by file path` heading byte-identical | 0 |
| `task lint:markdown` | 0 |
| `task lint:yaml` | 0 |

### Task 3 gates

| Gate | Exit |
| --- | --- |
| `task fmt` | 0 — **mutated nothing**, so no fmt-output commit was needed |
| `task fmt:check` | 0 |
| `task lint:markdown` | 0 |
| `task lint:yaml` | 0 |
| `task lint:docs-paths-sync` | 0 |
| glob-coverage in both prose files, driven from the authoritative var | 0 |
| claim-site qualifier gate re-run AFTER `task fmt` | 0 |
| clean tree vs BOTH index and HEAD (`git diff --quiet && git diff --cached --quiet`) | 0 |
| `confirmed-bug` on #4892 | 0 |
| negative + positive controls re-run post-fmt | 0 (all three) |

Anchor integrity confirmed by hand (rumdl MD057 is disabled): `### Exempt by file path` is
unchanged and all three inbound links still target `#exempt-by-file-path` —
`CONTRIBUTING.md:111`, `.github/PULL_REQUEST_TEMPLATE/chore.md:28`,
`.github/ISSUE_TEMPLATE/chore.yml:29`.

Scope boundaries confirmed held: `scripts/docs-paths-regex.sh` is unmodified, no
`lint:dependency-paths-sync` gate was added, and `chore.md`/`chore.yml` still enumerate zero
dependency paths (grep for `go.mod|package.json|bun.lock|uv.lock|DEPENDENCY_ONLY_PATHS|_pb.ts`
in those two files returns no hits).

## Commits

| SHA | Message |
| --- | --- |
| `2255f14f7` | `chore(taskfile): add authoritative DEPENDENCY_ONLY_PATHS var` |
| `b8fdbc486` | `docs(contributing): correct the dependency-only exemption at all five claim sites` |
| `9cb8d8f53` | `fix(contributing): exclude E2E compose files and define the lockfile subset` (post-review — see below) |

Task 3 produced no commit: it is a verification-and-reconciliation sweep, `task fmt` mutated
nothing, and the sweep found no drift between the var and its two prose mirrors.

## Deviations from Plan

None — the plan executed exactly as written. Notes on judgment calls made inside the plan's
latitude:

- **Carve-out sentence placement.** At `CONTRIBUTING.md:172` and
  `.github/PULL_REQUEST_TEMPLATE.md:64` the phrase "with one lockfile carve-out" was placed on
  the same line as the `scripts/**` assertion rather than in a trailing sentence. The
  claim-site gate uses a 3-line window, so a trailing placement would have passed — but it
  would sit at window edge `n+2` and a future reflow could push it out. Same-line placement is
  reflow-robust. This is wording latitude explicitly granted by CONTEXT.md.
- **Task 3 committed nothing.** The plan says to commit whatever `task fmt` mutates; it
  mutated nothing, so there was nothing to commit. No empty commit was created.

## Deferred Issues

None discovered. The two items the plan deferred to #4890 remain deferred by design:
generalizing `scripts/docs-paths-regex.sh` (it cannot compile eight of the ten new globs and
silently miscompiles a ninth), and narrowing `**/package.json` so a `scripts.postinstall`
edit cannot ride in as "dependency-only" (threat `T-4892-01`, accepted). Both are recorded in
the new `Taskfile.yaml` comment block and the plan's threat register respectively, so #4890
inherits them rather than rediscovering them.

## Known Stubs

None. No placeholder values, no TODO/FIXME markers, and no unwired data paths were
introduced — this change is one YAML variable and four prose mirrors.

## Self-Check: PASSED

- `Taskfile.yaml`, `CONTRIBUTING.md`, `.github/PULL_REQUEST_TEMPLATE.md`,
  `.github/PULL_REQUEST_TEMPLATE/chore.md`, `.github/ISSUE_TEMPLATE/chore.yml` — all present
  and modified as claimed.
- Commits `2255f14f7`, `b8fdbc486` and `9cb8d8f53` (the post-review revision) all verified
  present in `git log`.
- No `.planning/` artifact was committed; the orchestrator owns that commit.
- `task pr-prep` **was** subsequently run inline by the parent session against the
  post-review tree: `status=pass`, `lane=fast`, `exit=0`, tree clean against both index and
  HEAD afterwards. This resolves the pre-push reminder recorded in `VERIFICATION.md`.

## Post-review revision (commit `9cb8d8f53`)

`gsd-code-reviewer` returned 0 CRITICAL / 2 HIGH / 3 MEDIUM / 3 LOW against the first two
commits. All four factual claims in the new prose were confirmed TRUE, and the transcription
was clean — the defects were in **policy shape**. Both HIGH findings were independently
re-verified against the repo before acting, and the resulting glob-set changes were taken
back to the user because the ten globs were a locked decision.

| Finding | Verdict | Resolution |
| --- | --- | --- |
| **H-1** — `scripts/` carve-out contradicted the authoritative var; "lockfile" undefined, so `go.sum` was undecidable | CONFIRMED (latent — no `scripts/go.mod` exists today) | Var now enumerates the lockfile subset explicitly (`**/go.sum`, `go.tool*.sum`, `**/pnpm-lock.yaml`, `**/bun.lock`, `**/uv.lock`); every other entry is a manifest and stays gated under `scripts/` |
| **H-2** — `compose*.yaml` exempted `compose.e2e.yaml` / `compose.e2e.cover.yaml`, which define the required `E2E Test` check | CONFIRMED (`test:e2e:cover` → `ci.yaml:286`) | Compose files listed individually (`compose.yaml`, `compose.prod.yaml`, `compose.cluster.yaml`); the E2E pair is excluded, with a gate asserting they stay excluded |
| **M** — `**/pyproject.toml` missing, leaving `**/uv.lock` nearly unreachable (pep621 bumps both together) | CONFIRMED (both `uv.lock` files sit beside a `pyproject.toml`) | Added |
| **L** — `go.tool*.{mod,sum}` matched no shape | CONFIRMED (4 tracked files) | Added, with a recorded rationale for why they are exempt while `compose.e2e*.yaml` is not |

**The `go.tool*` tension, recorded rather than papered over.** Bumping a pinned tool can
change what `task lint` enforces, which is the same concern that excludes the E2E compose
files. The distinction, now written into the Taskfile comment: a `go.tool*` bump is judged
by the full lint suite running on that same PR, so a regression surfaces immediately as a
red check. `compose.e2e*.yaml` configures the harness that does the judging — a weakened
harness cannot catch itself.

**Final set: 15 entries** (5 lockfiles + 10 manifests). Every entry verified to match ≥1
tracked file; both prose mirrors verified in set-equality with the var; `compose.e2e.yaml`
and `compose.e2e.cover.yaml` verified NOT matched by any entry.

One self-inflicted defect caught and fixed during this revision: the comment initially cited
`Taskfile.yaml:305-313` for the E2E task, but the edit had already shifted those lines to
`328-336`. Replaced with a stable task-name reference instead of a same-file line range.

# Design

## Context

See proposal.md for the motivation. Today these facts shape the approach:

- `CLAUDE.md` names GSD 18 times. Ten more instruction files name `/gsd-*` commands or `.planning/`. They are listed under Decision 6.
- Five Go comments cite files in `.planning/phases/02.2-…/02.2-ACTION-AUDIT.md` and `.planning/phases/06.1-…/06.1-VERIFICATION.md`. About 30 `origin_spec` entries in `docs/architecture/invariants.yaml` point into `.planning/phases/`. GSD's v0.13 milestone close moved those phases to `.planning/milestones/v0.13-phases/`, so these citations were already broken on `main`. This change repoints them (task 3.2).
- The orphan check in `test/meta/invariant_registry_test.go` walks only `docs/superpowers/specs/`. `.claude/rules/invariants.md` documents that limit and lists `.planning/phases/**/*-SPEC.md` as a hand-registered escape hatch.
- `DOCS_ONLY_PATHS` in `Taskfile.yaml` has byte-identical copies in both triggers of `ci.yaml` and `ci-docs-skip.yaml`. `scripts/lint-docs-paths-sync.sh` enforces the match. The PR template repeats the list in prose.
- `openspec/specs/` is empty. The only OpenSpec content is `openspec/config.yaml` and this change.
- The repository pins no Claude Code plugins in `.claude/settings.json`. Generic review skills such as `/suite-review` and `dev-flow:review-pr` live on each contributor's machine.

## Goals / Non-Goals

**Goals:**

- One workflow in the instructions, from idea to merged PR: issue, OpenSpec change, implementation, review, PR, archive.
- No dangling citation: every path that a comment, `origin_spec` or doc cites still exists after the change.
- A test, not prose, for the new orphan-check coverage.

**Non-Goals:**

- Rewriting historical documents. The plans and reviews under `docs/plans/`, `docs/superpowers/` and `docs/reviews/` keep their GSD mentions, because they record what happened.
- Changing the `.claude/rules/references/*-review-learnings.md` content beyond the header that names its reader.

## Decisions

### 1. Keep `.planning/` in place as a frozen archive

`.planning/` stays where it is. A new `.planning/README.md` marks the tree as a read-only archive of the GSD era. It points new work to `openspec/changes/` and release planning to GitHub milestones and project #1.

Alternative: move the cited artifacts into `docs/archive/planning/` and delete the rest. This rewrites five Go comments and about 30 `origin_spec` values, and it breaks links in closed issues and old commits. It gives no benefit to a reader, so this design rejects it.

GSD wrote `.planning/`, but GSD no longer runs here, so adding a README does not edit a file that a tool still parses.

### 2. Map each GSD gate to an OpenSpec or repository gate

| GSD gate | Replacement |
| --- | --- |
| `/gsd-discuss-phase` | `/opsx:explore` |
| `/gsd-plan-phase` and `gsd-plan-checker` | `/opsx:propose`, then `openspec validate <change> --strict` |
| `/gsd-execute-phase` | `/opsx:apply` |
| `gsd-verifier` | The change's acceptance task in `tasks.md`, run on the branch before push and on `main` after merge |
| `/gsd-code-review` | A pre-push code review of the branch diff (Decision 3) |
| `/gsd-ship` | `task pr-prep`, then `gh pr create` |
| (none) | `/opsx:archive` after merge |
| `/gsd-quick`, `/gsd-fast` | The direct path: issue, fix, review, PR, with no OpenSpec change |

`crypto-reviewer` and `abac-reviewer` do not change. Only the sentences that order them against `/gsd-code-review` change. They now run before the generic code review.

### 3. Name the generic code review by its property, with a default command

The instruction says: before push, run an adversarial code review over the branch diff, and address every finding. It names `/suite-review` as the default and `dev-flow:review-pr` as the alternative. The Octopus bot review on the PR stays, and every thread still gets its own reply.

Alternative: hard-require one command. The repository does not install either skill, so a hard requirement fails for a contributor who has only the other one.

### 4. Extend the orphan walk with a list of roots

The walk becomes a loop over three roots: `docs/superpowers/specs`, `openspec/specs` and `openspec/changes`. A root that does not exist is skipped. The per-file logic does not change. A table-driven unit test feeds a temporary directory tree to the extracted walker. The test proves that an orphan under each root fails and that `.planning/` is not read. The rule file and the `invariants.yaml` comments that describe the old walk root change in the same commit.

### 5. Add `openspec/**/*.yaml` to the docs-only set, and not `.agents/**`

`openspec/` holds Markdown proposals and specs plus YAML metadata. None of them affect a build or a test, so the full CI run gives no signal for them. The Markdown already matches `**/*.md`, so only the YAML needs an entry. The entry names the file type, so a Go file under `openspec/` still runs full CI. The issue gate reads the same Taskfile variable, so OpenSpec-only PRs are also exempt from the issue-first gate. That is intended: they carry no code, like any other docs-only PR, and the change's proposal names its issue.

The orphan guard scans docs-only trees, so `task pr-prep:docs` runs it. Without that step, an OpenSpec-only PR can merge an unregistered id and break the next code PR.

The previous text of this decision listed `openspec/**`. The suite review found that it matched every file type, so this design narrowed it. `.agents/**` holds generated skills. Its Markdown files already match `**/*.md`, and a future skill can carry a script, so this design does not list the directory.

### 6. Files to rewrite

| File | Change |
| --- | --- |
| `CLAUDE.md` | Rewrite eight sections: Documentation Structure, Spec-Driven Development, Workflow, Code review, Issue tracking, Strategic Themes, Pre-Push Review Gates, Landing the Plane. Fix the TDD row in Testing. Replace "MUST NOT use GitHub Projects" with the milestone and project rule. |
| `docs/CLAUDE.md` | Replace "Strategic planning (GSD-owned)". |
| `.claude/rules/landing-the-plane.md` | Replace "Skipping the chain" and the GSD sentence in the CI-skip rule, and name the #5050 template incident there, because the rule now applies to templates as well as tools. |
| `.claude/rules/invariants.md` | Add the OpenSpec paths to `paths:`, update the walk-root text, and keep `.planning/` as a hand-registered archive note. Point the backlog sentence at GitHub issues. |
| `.claude/rules/references/plan-review-learnings.md`, `design-review-learnings.md` | Header only: the reader is a plan or spec reviewer, not `gsd-plan-checker`. |
| `.claude/agents/crypto-reviewer.md`, `abac-reviewer.md` | Replace `/gsd-code-review` with "the generic code review". |
| `.claude/agents/branch-readiness-check.md` | Check for a pre-push code review instead of `/gsd-code-review`. |
| `.claude/skills/holomush-dev/skills/review-crypto/SKILL.md`, `review-abac/SKILL.md` | Same wording as the agents. |
| `.claude/skills/holomush-dev/skills/release-notes/SKILL.md`, `scripts/release_notes_collect.py` | Point at GitHub milestones and ADRs instead of `.planning/ROADMAP.md`. |
| `.claude/skills/bug-triage/SKILL.md` | Strategic planning lives in milestones and the project. |
| `.claude/hooks/remind-pre-action-review.sh` | Same gate wording as `CLAUDE.md`. |
| `site/src/content/docs/contributing/how-to/pr-prep.md` | Describe `.planning/**` as an archive and add `openspec/**`. |
| `.github/PULL_REQUEST_TEMPLATE.md`, `CONTRIBUTING.md` | Add `openspec/**/*.yaml` to the docs-only list. |
| `.claude/rules/references/invariants-detail.md` | Name the three walk roots, like `.claude/rules/invariants.md`. |
| `.claude/skills/sketch-findings-holomush/` | Repoint the moved `.planning/phases/01-portal-spec/` citations. |

### 7. Retire the engram rules last, with consent

Rules `2f2r70zqwz` and `a32nfcekfc` name GSD. After the instructions merge, the session fetches each rule, proposes a superseding text or an archive, and acts only on the user's yes.

## Risks / Trade-offs

- A contributor with GSD installed can still run `/gsd-*` and write into `.planning/`. The README and `CLAUDE.md` say not to, but nothing enforces it. A lint that fails on new files under `.planning/` is possible later. It is not in this change.
- `AGENTS.md` is a symlink to `CLAUDE.md`, so Codex sessions get the same text. `task lint:docs-symmetry` keeps that true.
- An acceptance command is shell in a Markdown file, and agents run it, also on `main` after merge. A maintainer reviews it in the PR like any other file, and the agent shows the command before it runs it. This is the same trust as a Taskfile change.
- `openspec/config.yaml` changes no longer run full CI. If it ever gains a field that affects a build, this decision must change.
- This change is itself docs-only except for the meta-test, so its PR runs full CI because of the Go test file.

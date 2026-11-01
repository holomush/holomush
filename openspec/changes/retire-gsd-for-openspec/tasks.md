# Tasks

## 1. Orphan check covers OpenSpec (TDD)

- [x] 1.1 In `test/meta/invariant_registry_test.go`, add the table-driven test `TestOrphanWalkScansEveryLiveSpecTree`. Each case builds a temporary tree. The orphan cases put an orphan under `docs/superpowers/specs/`, `openspec/specs/`, `openspec/changes/` or `openspec/changes/archive/`. The pass cases cover a registered id, an unmigrated-scope id, a missing `openspec/specs/`, and an orphan under `.planning/`. Verify: `task test -- -run TestOrphanWalkScansEveryLiveSpecTree ./test/meta/` fails to compile or fails, because the walker is not yet extracted.
- [x] 1.2 Extract the orphan walk into a helper that takes a repository root and loops over the three spec roots. The helper skips a root that does not exist. Call it from `TestEveryRegistryInvariantHasBinding`, which first requires every root to exist in the repository. Verify: the test from 1.1 passes, and `task test -- ./test/meta/` passes.
- [x] 1.3 Update the walk-root text in `.claude/rules/invariants.md`, `.claude/rules/references/invariants-detail.md` and in the `invariants.yaml` comments near lines 2193, 2406, 2426 and 2476. In the rule, add `openspec/specs/**/*.md` and `openspec/changes/**/*.md` to `paths:`, and keep `.planning/` as a hand-registered archive note. Verify: `go run ./cmd/inv-render` leaves `docs/architecture/invariants.md` unchanged, and `rg -n 'walks only' .claude/rules docs/architecture/invariants.yaml` names all three roots wherever it matches.

## 2. Docs-only routing

- [x] 2.1 Add `openspec/**/*.yaml` after `.planning/**` in `Taskfile.yaml` `DOCS_ONLY_PATHS`, both triggers of `.github/workflows/ci.yaml`, and both triggers of `.github/workflows/ci-docs-skip.yaml`. Verify: `task lint:docs-paths-sync` passes. If the entry is removed from one file only, it fails.
- [x] 2.2 Add `openspec/**/*.yaml` to the docs-only list in `.github/PULL_REQUEST_TEMPLATE.md`, `CONTRIBUTING.md` and `site/src/content/docs/contributing/how-to/pr-prep.md`, and describe `.planning/**` as the GSD archive there. Verify: both lists name the same paths as `DOCS_ONLY_PATHS`.

- [x] 2.3 Run the orphan guard in `task pr-prep:docs`. Verify: `task pr-prep:docs` runs `TestEveryRegistryInvariantHasBinding` and `TestOrphanWalkScansEveryLiveSpecTree`.

## 3. Archive marker

- [x] 3.1 Add `.planning/README.md`. It marks the tree as a read-only archive of the GSD era. It points new work to `openspec/changes/` and release planning to GitHub milestones and project #1. It says that the files stay because code and `invariants.yaml` cite them. Verify: every path in `rg -o '\.planning/[^ `"),]+' --glob '*.go' --glob 'invariants.yaml'` exists.

- [x] 3.2 Repoint the citations that GSD's v0.13 milestone close broke. It moved the completed phases from `.planning/phases/` to `.planning/milestones/<version>-phases/`, so the Go comments, `invariants.yaml` `origin_spec` values, `test/session-matrix.yaml`, `scripts/pr-gate-paths.sh`, the `sketch-findings-holomush` skill and `plan-review-learnings.md` pointed at missing files. Verify: every `.planning/` path cited outside `.planning/`, `openspec/` and the historical `docs/` trees exists.

## 4. Instruction rewrite

- [x] 4.1 Rewrite `CLAUDE.md` per design Decision 6. Use the loop table from Decision 2 and the review gate from Decision 3. New specs go to `openspec/changes/<name>/`. Release planning uses GitHub milestones and project #1, with the Priority field kept in sync with the `priority::*` labels. Keep the direct path for small fixes. Verify: `task lint:docs-symmetry` passes, and `rg -n '/gsd-|gsd-plan-checker|gsd-verifier|MUST NOT\*\* use GitHub Projects' CLAUDE.md` finds nothing.
- [x] 4.2 Rewrite `docs/CLAUDE.md` "Strategic planning", `.claude/rules/landing-the-plane.md` ("Skipping the chain" and the CI-skip sentence), and the headers of the two `.claude/rules/references/*-review-learnings.md` files. Verify: `rg -n '/gsd-|gsd-plan-checker' docs/CLAUDE.md .claude/rules --glob '!.claude/rules/references/**'` finds nothing.
- [x] 4.3 Update the three agents `crypto-reviewer.md`, `abac-reviewer.md` and `branch-readiness-check.md`, and the `review-crypto` and `review-abac` skills. The domain gates run before the generic code review. Verify: `rg -n 'gsd' .claude/agents .claude/skills/holomush-dev/skills/review-*` finds nothing.
- [x] 4.4 Update `remind-pre-action-review.sh` reminders to the Decision 3 wording. Verify: `bash -n .claude/hooks/remind-pre-action-review.sh` passes, and `rg -n 'gsd' .claude/hooks` finds nothing.
- [x] 4.5 Point `release-notes/SKILL.md`, `release_notes_collect.py` and `bug-triage/SKILL.md` at GitHub milestones and ADRs. Verify: `rg -n 'GSD|ROADMAP' .claude/skills/holomush-dev .claude/skills/bug-triage` finds nothing, and `python3 -m py_compile .claude/skills/holomush-dev/scripts/release_notes_collect.py` passes.

## 5. Branch verification

- [x] 5.1 Run the acceptance command from proposal.md in the worktree, then `task pr-prep`. Verify: both exit 0.

## 6. After merge

- [ ] 6.1 Run the acceptance command from proposal.md on `main` after the merge. Verify: it exits 0.

## Workflow follow-up

- Archive the change with `/opsx:archive retire-gsd-for-openspec`.
- Fetch engram rules `2f2r70zqwz` and `a32nfcekfc`, propose a superseding text or an archive for each, and act only on the user's yes.
- After the archive PR merges, close #5069.

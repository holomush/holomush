# Proposal

## Why

HoloMUSH adopted OpenSpec in #5066, but every agent instruction still sends sessions through GSD: the `/gsd-*` commands, the `.planning/` phase loop, and the GSD review gates. Two workflows now compete, and `CLAUDE.md` still says "MUST NOT use GitHub Projects" although the project (holomush org project #1) now carries milestones and priority. Issue #5069 asks to retire GSD gradually, so that a feature can go from idea to merged PR with OpenSpec and GitHub Issues alone.

## What Changes

- Rewrite the agent instructions that name GSD. The files are `CLAUDE.md`, `docs/CLAUDE.md`, two rules files, two review-learnings references, three agents, four skills, one script, one hook and the contributor page `pr-prep.md`. The design lists each one.
- **BREAKING** (process): new spec work goes to `openspec/changes/<name>/`, not `.planning/phases/`. No `/gsd-*` command is part of the workflow.
- Replace the GSD gates. `openspec validate --strict` replaces `gsd-plan-checker`, the change's acceptance tasks replace `gsd-verifier`, and a named pre-push code review replaces `/gsd-code-review`. `crypto-reviewer` and `abac-reviewer` stay as they are.
- Replace the "MUST NOT use GitHub Projects" rule. GitHub milestones and project #1 (with its Priority field kept in sync with the `priority::*` labels) become the release-planning surface that `.planning/ROADMAP.md` was.
- Freeze `.planning/` as a read-only archive with a short README marker. Nothing in it moves, so the code comments and `invariants.yaml` `origin_spec` entries that cite it stay valid.
- Extend the invariant orphan check to walk `openspec/specs/` and `openspec/changes/` as well as `docs/superpowers/specs/`.
- Treat `openspec/**/*.yaml` as docs-only in the CI path filters (`ci.yaml`, `ci-docs-skip.yaml`, `Taskfile.yaml` `DOCS_ONLY_PATHS`, the PR template and `CONTRIBUTING.md`). OpenSpec Markdown already matches `**/*.md`. Run the orphan guard in the docs-only lane, so an OpenSpec-only PR still runs it.
- Retire the two engram rules that mention GSD (`2f2r70zqwz`, `a32nfcekfc`), with the user's consent at that step.

Out of scope: migrating old `.planning/` or `docs/superpowers/specs/` content into `openspec/specs/`, uninstalling GSD from contributor machines, and changing what `crypto-reviewer` or `abac-reviewer` check.

## Capabilities

### New Capabilities

- `invariant-registry`: the registry's guard against spec-referenced invariant ids that are missing from `invariants.yaml`, including which spec trees it scans.
- `ci-docs-only-routing`: the set of docs-only paths, and how its copies stay identical. A diff that touches only these paths runs the stand-in workflow, not full CI.

### Modified Capabilities

None. `openspec/specs/` is empty today.

## Impact

- Instructions and docs: the files listed under What Changes. No product code changes.
- Tests: `test/meta/invariant_registry_test.go` gains the OpenSpec walk roots.
- CI: three path lists and the PR template change together. `task lint:docs-paths-sync` keeps them identical.
- Engram: two repository rules are superseded or archived.
- Contributors: anyone who still runs `/gsd-*` commands must switch to `/opsx:*`.

## Acceptance

Run from the repository root. Every command must exit 0.

```bash
! rg -n '/gsd-|gsd-plan-checker|gsd-verifier' CLAUDE.md docs/CLAUDE.md .claude/rules .claude/agents .claude/skills .claude/hooks site/src/content/docs .github --glob '!.claude/rules/references/**'
task test -- -run 'TestOrphanWalkScansEveryLiveSpecTree|TestEveryRegistryInvariantHasBinding' ./test/meta/
task lint:docs-paths-sync
task lint:docs-symmetry
test -f .planning/README.md
```

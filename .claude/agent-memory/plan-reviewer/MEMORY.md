# plan-reviewer agent memory

This file accumulates HoloMUSH-specific patterns of good and bad plans
discovered during adversarial plan review. Entries are added by the agent
itself after completing a review.

Keep under 200 lines. Curate — don't hoard.

## Common plan gaps in this codebase

- [Verify helper existence](feedback_verify_helper_existence.md) — plans frequently invent methods/helpers on existing types ("extend test file X" with calls that don't exist on X)
- [No signature placeholders](feedback_no_signature_placeholders.md) — interface signatures and parameter lists must be inlined, never deferred via `/* COPY FROM existing.go */` comments
- [Verify imports against code blocks](feedback_verify_imports_in_code_blocks.md) — every package qualifier (`slog.`, `timestamppb.`, `status.`) used in a plan's example code must appear in the file's existing imports OR in the plan's "imports needed" directive
- **Missing `jj new` between phase-commit boundaries.** When a plan decomposes a PR into N
  per-phase commits, every phase-end "commit task" MUST conclude with `jj new -m "phase
  N+1 (in progress)"` so the next phase's edits land in a fresh `@`. Plans that end the
  phase task with bare `jj describe` cause subsequent file edits to fold into the named
  phase commit; a later "describe" then clobbers the earlier description. Always grep
  the plan for `jj describe` not followed by `jj new` at task boundaries.
- **`@-` vs `@` bookmark targets.** When a plan creates a push bookmark, verify which
  commit `@-` actually points to by tracing the commit/new sequence forward from the
  last `jj new`. Plans that say "@- is the Phase N commit, since @ is currently empty
  after the commit-then-new pattern" must actually contain that `jj new` somewhere —
  authors sometimes assume the pattern without writing the step.
- **False-starts left in executable text.** Watch for "Wait — that command is wrong, use
  this instead:" patterns inside step bodies. Subagents skim and may run the first
  block they see. Flag any plan that emits two contradictory commands in the same step.
- **Long-running steps without a budget annotation.** `task pr-prep` runs 5–15 min; a
  per-task subagent dispatch with default timeout will starve. Plans should annotate
  long-running verifications and recommend `run_in_background` + Monitor.
- **"Manual-ish" steps in subagent-driven plans.** Multi-terminal manual checks cannot
  be executed by an automated agent. Either script them inline or label them
  `(MANUAL — pre-merge only)`.
- **Stale prose around revised code blocks.** When a previous review forces the author to
  simplify a code block, the surrounding "Notes" / explanation prose often gets left
  describing the OLD construct. On revision passes, diff the prose against the current
  code, not just the code-against-itself. (Round-2 example: Task 7 step 2's source line
  was simplified from a two-fallback construct to one line, but the post-code Notes
  block still described "two fallbacks because...".)
- **Cross-task instruction drift after partial-mirror fixes.** When ONE task adopts a
  pattern (e.g., `describe + jj new`) and a later task deliberately does NOT adopt it
  (last-phase exception), prose like "Mirror this pattern at Tasks N, M, K" must
  enumerate exceptions explicitly. Round-2 example: Task 6 step 4 said "Mirror at Tasks
  9, 13, and 16" but Task 16 step 3 explicitly forbids the mirror. Subagent execution
  per-task hides this contradiction; an inline-execution agent might propagate the
  wrong pattern to Task 16.

## Decomposition patterns that work here

- **Per-phase commit pattern that DOES work**: Task N edits files; Task N+1 step 1 runs
  `jj describe -m "phase N: ..."` then step 2 runs `jj new -m "phase N+1 (in progress)"`.
  Plan 2026-04-25-session-workspace-isolation.md gets this RIGHT for Phase 3→4 (Task 13)
  but WRONG for Phase 1→2 (Task 6 missing the `jj new`).
- **Helper extraction for shell-script reuse**: when two consumers (Taskfile cmd + hook)
  need the same path-discovery logic, ship one `scripts/<helper>.sh` sourced by both.
  Spec called this out as an "Implementation note"; plan implemented it. Pattern works
  cleanly in the HoloMUSH layout.
- **Repo-reality verification of cited line numbers**: plans that cite `file:line-line`
  ranges should be grep-verified before review approval. The 2026-04-25 plan cited
  `Taskfile.yaml:515-551`, `CLAUDE.md` 552/570/849 — all four matched current
  main@origin (`642c93e39baf`). When citations are accurate, reviewers can move fast.

## Review reflexes

- For every `path:line` citation in the plan, run a quick `Read` or `rg` to verify the line range still covers what the plan claims. Drift across PRs is real — the spec under review used `:96-102` for a block, the plan used `:95-102` for the same block. Both can be off after the next merge.
- Plans that put a `task pr-prep` gate at the END but never run `task lint` per-task accumulate lint debt across N commits. Per project rule (`MUST run task lint before committing`), each commit checkpoint should be lint-clean. Flag this as non-blocking but real.
- For Ginkgo vs `testing.T`: never assume a `_test.go` file is BDD-style. Verify with `rg "var _ = Describe" path/to/file`. Plain `func TestX(t *testing.T)` is the default in this repo's `eventbus_e2e` integration tree.
- For the `task test:int` invocation: integration tests in HoloMUSH live in mixed locations. Plugin store integration tests are at `./plugins/<name>/` with `//go:build integration`, NOT at `./test/integration/plugin/...`. Always cross-reference `Taskfile.yaml:test:int` for the canonical package list before approving a path in a plan.
- `//nolint:unparam` does NOT suppress revive's `unused-parameter` rule. Both `unparam` (linter, `.golangci.yaml:31`) and `revive` rule `unused-parameter` (`.golangci.yaml:130`) flag unused function parameters. golangci-lint nolint directives suppress by linter name (`unparam`, `revive`), not by individual revive rule. Plans that introduce a temporary unused-parameter state for staged refactors MUST suppress both: `//nolint:unparam,revive // ...`.
- For jj-colocated repos with @ on a non-empty docs commit, the safe cadence is "new-first, then edit, then describe" — never "edit, then describe, then new", which silently merges code into the docs commit AND clobbers its message. Always verify with `jj log -r 'main@origin..@'` BEFORE approving any plan whose Task 1 starts with file edits.

# plan-reviewer agent memory

This file accumulates HoloMUSH-specific patterns of good and bad plans
discovered during adversarial plan review. Entries are added by the agent
itself after completing a review.

Keep under 200 lines. Curate — don't hoard.

## Common plan gaps in this codebase

- [Verify helper existence](feedback_verify_helper_existence.md) — plans frequently invent methods/helpers on existing types ("extend test file X" with calls that don't exist on X)
- [No signature placeholders](feedback_no_signature_placeholders.md) — interface signatures and parameter lists must be inlined, never deferred via `/* COPY FROM existing.go */` comments
- [Verify imports against code blocks](feedback_verify_imports_in_code_blocks.md) — every package qualifier (`slog.`, `timestamppb.`, `status.`) used in a plan's example code must appear in the file's existing imports OR in the plan's "imports needed" directive

## Decomposition patterns that work here

<!-- Populated by the agent over time. -->

## Review reflexes

- For every `path:line` citation in the plan, run a quick `Read` or `rg` to verify the line range still covers what the plan claims. Drift across PRs is real — the spec under review used `:96-102` for a block, the plan used `:95-102` for the same block. Both can be off after the next merge.
- Plans that put a `task pr-prep` gate at the END but never run `task lint` per-task accumulate lint debt across N commits. Per project rule (`MUST run task lint before committing`), each commit checkpoint should be lint-clean. Flag this as non-blocking but real.
- For Ginkgo vs `testing.T`: never assume a `_test.go` file is BDD-style. Verify with `rg "var _ = Describe" path/to/file`. Plain `func TestX(t *testing.T)` is the default in this repo's `eventbus_e2e` integration tree.
- For the `task test:int` invocation: integration tests in HoloMUSH live in mixed locations. Plugin store integration tests are at `./plugins/<name>/` with `//go:build integration`, NOT at `./test/integration/plugin/...`. Always cross-reference `Taskfile.yaml:test:int` for the canonical package list before approving a path in a plan.

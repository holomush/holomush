<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Sub-Agent Briefing Checklist

When dispatching a sub-agent (via the `Agent` tool or a worktree-isolated
`fix-worker`), include these in the prompt — sub-agents do NOT inherit
context, skills, or tool habits from the parent session.

## Always include

1. **Goal** — what specifically you want done. Not "look at this" — "find X, fix Y in `path:line`".
2. **Working directory** — agents start in the parent's cwd; tell them if they need to `cd` somewhere else.
3. **Tool precedence** — brief them on the search ladder per `.claude/rules/search-tools.md` (probe → rg → ast-grep; never bare grep). Sub-agents default to rg/full-file reads without it.
4. **VCS** — VCS is native git; agents use `git` directly (no VCS skill needed). If an agent sets up isolation, brief it on `git worktree add`/`remove` (see `.claude/rules/landing-the-plane.md`). Sub-agents do NOT inherit skills from the parent session.
5. **Output expectations** — word count, structure (verdict / report / file path), what NOT to include.
6. **Verbose task runs** — sub-agents run `task test`/`lint`/`build` inline in their own context (they are exempt from the offload deny); the PARENT session must not.

## Per-task additions

| Task type | Brief them on |
|-----------|--------------|
| Code search | probe MCP for Go symbol/AST; `rg` for text (never bare `grep`); `ast-grep` for structural/codemods (`.claude/rules/search-tools.md`) |
| Implementation | line-scoped `//nolint:<rule>` only — never widen `.golangci.yaml`. Repo precedent: `internal/web/handler.go:381,418,460,484` use `//nolint:wrapcheck // gRPC status errors pass through as-is`. 27+ such directives in the codebase. |
| TDD | run `task test -- ./<package>` per change; use Ginkgo for integration tests (build tag `//go:build integration`) |
| Sub-agent dispatch | Default model floor `sonnet`. `haiku` ONLY for agents whose output is schema-constrained AND independently verified downstream (e.g. a mechanical distiller in a fan-out that a sonnet+ verifier checks); NEVER `haiku` for judgment the caller acts on unverified (test triage, review, flake-vs-real). Prefer `effort: low` on a sonnet agent over haiku — `effort` errors on haiku 4.5, and haiku's $ win on a short agent is tiny. Repo-owned reviewer agents (code/crypto/abac-reviewer) stay `opus` — never downgrade for cost (design/plan-reviewer: see the tiering note below) |
| Git worktree work | sub-agents share the parent's worktree and branch — verify `git status` before they commit; NEVER let parallel agents commit or run `git worktree add` concurrently (they collide on the shared index/working tree). `git reflog` recovers commits after a bad reset/rebase |
| Closing beads | grounded evidence required; in-bead "Closed:"/"Fixed:" comments are NOT proof — verify the cited fix in current code (the `bead-auditor` agent caught false-fix cases on `wfza.21`, `wfza.62`) |

> **Repo-agent model tiers (verified 2026-07-03):** reviewers (`code`/`crypto`/`abac`-reviewer) = `opus`; investigators/runners (`bead-auditor`, `branch-readiness-check`, `adr-extractor`, `local-check`, `local-pr-prep`) = `sonnet`. Plugin agents (design/plan-reviewer, fix-worker, Explore, …) are plugin-owned — the repo cannot set their model here.

## Anti-patterns

- DO NOT dispatch parallel `Agent` calls that edit the same files — they share the parent's working copy and will collide
- DO NOT dispatch parallel `bd create` — there's an ID-allocation race; parallel calls all report the same ID with their respective titles but only ONE actually commits
- DO NOT trust a sub-agent's claim that `task pr-prep` passed — always run it yourself in the parent before pushing. Sub-agents can't catch schema-regeneration side-effects (e.g., `go generate` updating `schemas/plugin.schema.json`) that must be committed before the PR is current
- DO NOT delegate UNDERSTANDING to the sub-agent ("based on your findings, fix the bug"). Synthesize first; give them concrete actions.
- DO NOT dispatch a `local-*` offload agent (`local-check`/`local-pr-prep`) in the same parallel tool batch as another maybe-failing call — a `local-*` failure alongside a sibling failure risks a cancel-storm (ADR holomush-cr3gq)

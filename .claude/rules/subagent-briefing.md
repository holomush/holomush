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
3. **Tool precedence** — point them at the project's search ladder (`.claude/rules/search-tools.md`): `mcp__probe__search_code`/`extract_code` for Go symbol/AST queries, `rg` for text (never bare `grep`), `ast-grep` for structural matches/codemods, `Read` with offset/limit for known paths. Sub-agents default to `rg` and full-file `Read` without explicit reminder.
4. **VCS skill** — if the agent may run `jj`/`git` commands or set up worktrees, ensure its agent definition has `jj:jujutsu` in `skills:` frontmatter. Sub-agents do NOT inherit skills from the parent session.
5. **Output expectations** — word count, structure (verdict / report / file path), what NOT to include.

## Per-task additions

| Task type | Brief them on |
|-----------|--------------|
| Code search | probe MCP for Go symbol/AST; `rg` for text (never bare `grep`); `ast-grep` for structural/codemods (`.claude/rules/search-tools.md`) |
| Implementation | line-scoped `//nolint:<rule>` only — never widen `.golangci.yaml`. Repo precedent: `internal/web/handler.go:381,418,460,484` use `//nolint:wrapcheck // gRPC status errors pass through as-is`. 27+ such directives in the codebase. |
| TDD | run `task test -- ./<package>` per change; use Ginkgo for integration tests (build tag `//go:build integration`) |
| Sub-agent dispatch | Default model floor `sonnet`. `haiku` ONLY for agents whose output is schema-constrained AND independently verified downstream (e.g. a mechanical distiller in a fan-out a sonnet+ verifier checks); NEVER `haiku` for judgment the caller acts on unverified (test triage, review, flake-vs-real). Prefer `effort: low` on a sonnet agent over haiku — `effort` errors on haiku 4.5, and haiku's $ win on a short agent is tiny. Reviewer agents (code/crypto/abac/design/plan) stay `opus`/`fable` — never downgrade for cost |
| jj workspace work | verify `jj st` is clean BEFORE they run `jj describe` — otherwise their describe clobbers the in-flight change's description |
| Op-log mutations | `jj op restore` / `jj op abandon` are gated by the `jj:jujutsu` plugin's `guard-jj-mutating` hook (bypass: `# jj-op-approved`); ensure the sub-agent's frontmatter lists `jj:jujutsu` in `skills:` so they read the recovery ladder before reaching for either command |
| Closing beads | grounded evidence required; in-bead "Closed:"/"Fixed:" comments are NOT proof — verify the cited fix in current code (the `bead-auditor` agent caught false-fix cases on `wfza.21`, `wfza.62`) |

## Anti-patterns

- DO NOT dispatch parallel `Agent` calls that edit the same files — they share the parent's working copy and will collide
- DO NOT dispatch parallel `bd create` — there's an ID-allocation race; parallel calls all report the same ID with their respective titles but only ONE actually commits
- DO NOT trust a sub-agent's claim that `task pr-prep` passed — always run it yourself in the parent before pushing. Sub-agents can't catch schema-regeneration side-effects (e.g., `go generate` updating `schemas/plugin.schema.json`) that must be committed before the PR is current
- DO NOT delegate UNDERSTANDING to the sub-agent ("based on your findings, fix the bug"). Synthesize first; give them concrete actions.

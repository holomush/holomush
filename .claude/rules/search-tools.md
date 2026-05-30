<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Code & Text Search Tooling

This rule defines the search-tool precedence for HoloMUSH. It pairs with the
`enforce-task-runner.sh` PreToolUse hook, which **nudges** (no longer blocks)
the `grep`/`cat`/`head`/`tail`/`find` file utilities toward this ladder — it
prints a reminder but lets the command run. It still **hard-blocks**
`go test`/`go build`/`golangci-lint`/`gofmt` (use the `task` runner instead).
The file utilities were softened from hard blocks because an `exit 2` deny now
cascades badly: recent harnesses dispatch tool calls in parallel batches, so
denying one segment (e.g. the `tail /tmp/log` in a `task test … ; tail` run)
cancels every sibling call in the batch. Honor the nudges, but they won't
dead-end you.

## The ladder — match the tool to the question

| Question shape | Tool | Notes |
| --- | --- | --- |
| String literal, comment, log line, config key, error text | **`rg`** (ripgrep) | Always available in Bash. Faster than `grep`, `.gitignore`-aware, skips binaries. Never invoke bare `grep`/`egrep`/`fgrep`. |
| Find files by name / type | **`fd`** (or the Glob tool) | `fd -e go`, `fd -t f manifest`. Faster than `find`; use `find` only for predicates `fd`/Glob can't express (`-mtime`, `-size`, `-exec`). |
| "Where is X defined / how does Y work" — want the whole function/type | **`mcp__probe__search_code`** / `extract_code` | AST-aware; returns the enclosing function/type as one block. Use ElasticSearch-style boolean queries (`(plugin AND fence) OR …`). Zero-index, stateless. |
| "Every `await` in a loop", "all `slog.Info(` without ctx", **and rewrite them** | **`ast-grep`** (`sg`) | Matches by code *structure*, not text — invariant-shaped patterns regex can't express, plus codemods. |
| "Last N lines", count, capping command stdout | **`rg`/`wc`/`\| head`** in Bash | The Read tool reads files forward only; piping `cmd \| head`/`\| tail` to cap output is fine and not blocked. |

| Requirement | Rule |
| --- | --- |
| **MUST NOT** invoke bare `grep`/`egrep`/`fgrep` | Use `rg`. The hook nudges; honor it. |
| **MUST** prefer `mcp__probe__search_code` over `rg` | For Go symbol/AST "where is X / how does Y work" questions (per CLAUDE.md tool precedence). |
| **SHOULD** reach for `ast-grep` | For structural matches and codemods — see patterns below. |
| **MUST** brief sub-agents on this ladder | Sub-agents default to `rg`/full-file `Read` without an explicit reminder. |

## Searching files ≠ deciding whether a command succeeded

The ladder above is for finding text *in files*. Deciding whether a **command
run** passed, failed, or should be retried is a different question — and
`grep`/`rg` over its output is the wrong tool for it.

| Requirement | Rule |
| --- | --- |
| **MUST** use the exit code | To decide pass/fail of a command, check `$?` — never grep its stdout/stderr for a "success"/"error" string. |
| **MUST NOT** branch on a matched output string | A command's output can echo test fixtures, expected-value assertions, or your own input, so a status substring may appear on a **passing** run. Real example: `task pr-prep`'s `pr-prep-lock.bats` self-test surfaces `another pr-prep is running` on healthy runs — an agent's retry loop grepped for that string, mistook every pass for lock contention, and re-ran the full lane forever (May 2026). |
| **MUST** read the exit code, not the log tail, for background runs | With `run_in_background`, get pass/fail from the run's reported exit code (or a result artifact the command writes), not by grepping the captured log. Piping through `tee`/`tail`/a trailing `echo` masks the real exit code unless `set -o pipefail` is on. |
| **MAY** grep output as a *secondary* confirmation | After the exit code says pass, matching a known terminal line (e.g. `✓ All PR checks passed.`) is fine as a sanity check — never as the primary signal. |

## ast-grep for HoloMUSH (Go)

`ast-grep` (`sg`) matches parsed syntax (whitespace/var-name independent) and can
rewrite. Always pass `-l go`. It shines for **composite-literal / struct / type
shapes and codemods** — these are verified to work:

```bash
# core.Event{} struct literals — MUST be core.NewEvent() (CLAUDE.md convention).
sg -p 'core.Event{$$$FIELDS}' -l go                  # find (75 hits today)

# codemod shape (preview only; add -U / --update-all to apply):
sg -p 'Foo{A: $V}' -r 'Foo{A: $V, B: true}' -l go
```

**Known Go limitation (ast-grep ≤0.42 / current 0.42.x):** package-qualified
*call* patterns — `slog.Info($$$A)`, `oops.Errorf($$$A)`, `$RECV.Info($$$A)` —
misparse (`type_conversion_expression`/`ERROR`) and match **nothing**, even in
code full of those calls ([ast-grep#646](https://github.com/ast-grep/ast-grep/issues/646)).
A zero result there means the pattern is broken, **not** that the invariant holds.
So for call-site and import audits — bare `slog.Info(`/`slog.Warn(` where a ctx
is in scope (see [logging.md](logging.md)), `math/rand` usage (use `crypto/rand`)
— reach for **`rg`**, which is textual and reliable. Non-qualified/builtin calls
(`panic($$$A)`) do match in ast-grep.

> `ast-grep` is structural search/rewrite *within* files. It does **not** build a
> call graph or find references across files — there is no symbol-navigation tool
> wired into this repo (LSP was rejected: multi-workspace index confusion in the
> jj setup). For "who calls X", use `mcp__probe__search_code` with the symbol name.

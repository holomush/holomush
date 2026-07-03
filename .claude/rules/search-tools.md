<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Code & Text Search Tooling (pointer)

Full ladder + rg/ast-grep gotchas: the **`dev-flow:grepping` skill** (force-loaded
each session). This file keeps only the repo-specific MUSTs the skill does not carry.

| Requirement | Rule |
| --- | --- |
| **MUST NOT** bare `grep`/`egrep`/`fgrep` | Use `rg`. A `PreToolUse` hook nudges; honor it. |
| **MUST** prefer `mcp__probe__search_code` over `rg` | For Go symbol/AST "where is X / how does Y work". |
| **SHOULD** use `ast-grep` (`sg -l go`) | Structural matches + codemods; NOT for pkg-qualified call patterns (misparse — use `rg`). |
| **MUST** brief sub-agents on the ladder | They default to `rg` / full-file `Read` without it. |

## Searching files ≠ judging whether a command succeeded

| Requirement | Rule |
| --- | --- |
| **MUST** decide pass/fail by EXIT CODE | Never grep a command's stdout/stderr for a "success"/"error" string — output can echo fixtures or your own input. |
| **MUST NOT** branch on a matched output string | Real incident (May 2026): an agent grepped `pr-prep` output for `another pr-prep is running` — a string the `pr-prep-lock.bats` self-test prints on HEALTHY runs — and re-ran the full lane forever. |
| **MUST** read the exit code (not the log tail) for background runs | Piping through `tee`/`tail`/trailing `echo` masks `$?` unless `set -o pipefail`. |
| **MAY** grep output as a SECONDARY confirmation | Only after the exit code says pass (e.g. matching `✓ All PR checks passed.`). |

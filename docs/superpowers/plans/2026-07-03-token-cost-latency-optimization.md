<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Token / Cost / Latency Optimization — Implementation Plan (Phase 1 + 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cut main-thread context growth and per-subagent cost for the holomush Claude Code dev workflow by adding four run-and-distill offload agents, tightening one always-on rule, and formalizing the subagent model-tier convention — the low-risk, fully-specifiable half of design `holomush-wagqb`.

**Architecture:** All changes live under `.claude/**` (AI-tooling config, not product code). Four new agent definitions (`local-test`, `local-pr-prep`, `local-build`, `local-lint`) run verbose `task` commands in their own context and return a compact text verdict. Three edits tighten/clarify existing rules. No Go code, CI, or product behavior changes.

**Tech Stack:** Claude Code agent definitions (Markdown + YAML frontmatter), `.claude/rules/*.md`, `.claude/settings.json`, `task` runner, `bd`, `jj`.

**Spec:** `docs/superpowers/specs/2026-07-03-token-cost-latency-optimization-design.md`
**Design bead:** `holomush-wagqb`

---

## Scope

This plan implements **Phase 1 + Phase 2** of the spec (§6):

| In this plan | Spec ref | Why now |
| --- | --- | --- |
| `local-test` offload agent | T2c | Biggest per-invocation main-context win; fully specifiable |
| `local-pr-prep` offload agent | T2c | Same; encodes the CLAUDE.md pr-prep reading protocol |
| `local-build` offload agent | T2c | Same contract; net win on opus/fable main sessions (RD3) |
| `local-lint` offload agent | T2c | Same; `task lint` output is often long (many linters × files) |
| Subagent model-tier carve-out | §4.3 | One-line rule edit; legitimizes future haiku-at-fan-out |
| Verify + document repo-agent tiering | T2d | Fleet already tiered — this locks it in + covers the 4 new agents |
| `search-tools.md` → pointer | T1b | Trims a 5.5 KB always-on rule that duplicates the grepping skill |
| Document `bd prime` operator-side de-dup | T1a | Repo is already single-source (RD4); no repo edit — document the operator's `~/.claude` cleanup |

**Deferred to follow-on plans** (do NOT attempt here):

| Deferred | Spec ref | Blocker |
| --- | --- | --- |
| `landing-the-plane` → CLAUDE.md pointer; `subagent-briefing` slim | T1c | Mechanism decided (RD1); deferred to a follow-on plan for scope, not blocked |
| Grepping cheat-sheet + on-demand load | T2a (repo half) | Mechanism decided (RD1); follow-on plan |
| Split `testing.md` / `invariants.md` via index-pointer | T2b | Mechanism decided (RD1: index-pointer); follow-on plan |
| Trim CLAUDE.md 36 KB → ≤ 24 KB | T3a | Target decided (RD2: conservative ≤24 KB); follow-on plan + mandatory human line-review |

Phase 1 + 2 deliver working, independently-verifiable improvements on their own.

## File Structure

| File | Action | Responsibility |
| --- | --- | --- |
| `.claude/agents/local-test.md` | Create | Run `task test [args]`, return pass/fail + failing tests only |
| `.claude/agents/local-pr-prep.md` | Create | Run `task pr-prep`, return the exit-code-first verdict + result-file status |
| `.claude/agents/local-build.md` | Create | Run `task build`, return pass/fail + first compile errors |
| `.claude/agents/local-lint.md` | Create | Run `task lint`, return pass/fail + one line per finding |
| `.claude/rules/subagent-briefing.md` | Modify (line 27) | Replace blanket sonnet floor with the narrow haiku carve-out |
| `.claude/rules/search-tools.md` | Modify (shrink) | Reduce to a pointer preserving only the repo-specific MUSTs |
| `~/.claude/settings.json` | Operator-side (non-repo) | RD4: remove the duplicate PreCompact `bd prime`; **not part of the repo PR** — documented only (the repo `.claude/settings.json` is already the correct single source) |

Guardrail (spec N2, §4.1): all four offload agents get tools `Bash, Read, Grep, Glob` and **no** `Edit`/`Write`/`NotebookEdit` — read-only **by briefing** (Edit/Write withheld so the agent cannot author files). Note: `Bash` is NOT a hard sandbox — a `task` command's own file regeneration, `rm`, or redirects can still mutate the shared working copy; the parent reviewing the diff / re-running `pr-prep` is the real safety net (spec §4.1(a) says "read-only-**briefed**").

Note on `effort`: all four offload agents are `sonnet` (not haiku), so `effort: low` is valid — `effort` errors only on Haiku 4.5 (spec §4.2). Since no agent in this plan is haiku and no MUST-level decision rests on the claim, spec §5's conditional MUST ("independently confirm before a MUST relies on it") is satisfied for this plan.

---

### Task 1: Subagent model-tier carve-out (§4.3)

Do this first — it establishes the convention the new agents cite.

**Files:**

- Modify: `.claude/rules/subagent-briefing.md:27`

- [ ] **Step 1: Confirm the current line**

Run: `rg -n 'model floor' .claude/rules/subagent-briefing.md`
Expected: `27:| Sub-agent dispatch | model floor is \`sonnet\` (never \`haiku\` for sub-agents) |`

- [ ] **Step 2: Replace the table row with the carve-out**

Replace line 27's cell text so the row reads:

```markdown
| Sub-agent dispatch | Default model floor `sonnet`. `haiku` ONLY for agents whose output is schema-constrained AND independently verified downstream (e.g. a mechanical distiller in a fan-out a sonnet+ verifier checks); NEVER `haiku` for judgment the caller acts on unverified (test triage, review, flake-vs-real). Prefer `effort: low` on a sonnet agent over haiku — `effort` errors on haiku 4.5, and haiku's $ win on a short agent is tiny. Reviewer agents (code/crypto/abac/design/plan) stay `opus`/`fable` — never downgrade for cost |
```

- [ ] **Step 3: Verify the row still renders as one table row**

Run: `rg -n 'Default model floor' .claude/rules/subagent-briefing.md`
Expected: exactly one match on a single line; `rg -c '^\| Sub-agent dispatch' .claude/rules/subagent-briefing.md` returns `1`.

- [ ] **Step 4: Verify no other rule contradicts it**

Run: `rg -rn 'never .*haiku|floor is .*sonnet' .claude/rules/`
Expected: only the edited line matches; no stale "never haiku" blanket statement remains elsewhere.

- [ ] **Step 5: Commit**

```text
jj describe -m "docs(rules): narrow subagent model-floor to a haiku carve-out (holomush-wagqb §4.3)"
```

(Working copy is already the change; `jj describe` updates the message. If subsequent tasks share the change, use `jj commit -m ...` to seal it and start a new change per your commit cadence.)

---

### Task 2: `local-test` offload agent (T2c)

**Files:**

- Create: `.claude/agents/local-test.md`
- Reference template: `.claude/agents/bead-auditor.md` (frontmatter shape)

- [ ] **Step 1: Create the agent definition**

Create `.claude/agents/local-test.md` with exactly this content:

```markdown
---
name: local-test
description: |
  Run the holomush unit-test suite (`task test`, optionally scoped to a package
  or `-run` filter) in an isolated context and return ONLY a compact verdict —
  never the raw test output. Use when the caller wants "run the tests", "does
  this package pass", "check the tests for X" and does NOT need the full log in
  the main thread. Read-only: runs the command, reports failures. Does NOT edit
  code or fix failures.
model: sonnet
effort: low
color: green
tools:
  - Bash
  - Read
  - Grep
  - Glob
---

You are a test-runner that offloads verbose `task test` output from the caller's
context. Your ENTIRE value is that your final message is SHORT: the caller reads
your ~10-line verdict instead of thousands of lines of test output.

## What to run

The caller gives you a package path and/or `-run` filter, or nothing.

- No args:            `task test`
- Package given: `task test -- ./internal/command/`
- Filter given: `task test -- -run TestName ./internal/command/`

Run the command as the SOLE command in one Bash call (no `| tee`/`| tail`/
trailing `echo`, which mask the exit code). Use `task` — never raw `go test`.

## How to decide pass/fail

Use the EXIT CODE, not string-matching the output (a passing run can echo the
word "FAIL" from a test fixture). `$?` == 0 → pass. Non-zero → fail.

## What to return (STRICT — this is your whole output)

On pass:
```

PASS — `<command you ran>`
<N> packages ok (or: <N> tests ok)

```text

On fail:
```

FAIL — `<command you ran>` (exit <code>)
Failing tests:

- <TestName> (<pkg>): <first line of the failure/assert message>
- ...
<if truncated:> (+<M> more — re-run scoped to see them)

```text

Rules:
- List at most 15 failing tests; if more, say how many were omitted.
- Give ONE line per failure — the test name, its package, and the first error
  line. Do NOT paste stack traces, diffs, or `--- FAIL` blocks verbatim.
- If the command could not run (compile error, missing Docker for
  session-store tests), say so in one line and quote only the first error line.
- NEVER include the full command output. NEVER edit files.
```

- [ ] **Step 2: Verify the frontmatter parses (agent is discoverable)**

New agent files are picked up on session start. After a `/reload-plugins` or a
fresh session, run: `rg -n '^name: local-test$' .claude/agents/local-test.md`
Expected: one match. (Discoverability in the Agent tool is confirmed at dispatch
time in Step 3.)

- [ ] **Step 3: Positive verification — dispatch on a known-passing package**

Dispatch the `local-test` agent (via the Agent tool, `subagent_type: local-test`)
with prompt: "Run tests for ./internal/core/ and report."
Expected: a `PASS — ...` verdict, no raw test output, under ~10 lines.

- [ ] **Step 4: Negative verification — introduce a temporary failing test**

Add a deliberately-failing test to a scratch file:
Run: create `internal/core/zzz_offload_probe_test.go` with:

```go
package core

import "testing"

func TestOffloadProbeDeliberateFail(t *testing.T) { t.Fatal("intentional probe failure") }
```

Dispatch `local-test` with: "Run tests for ./internal/core/ -run TestOffloadProbeDeliberateFail and report."
Expected: a `FAIL — ... (exit 1)` verdict naming `TestOffloadProbeDeliberateFail` with the `intentional probe failure` message, and NO raw output.

- [ ] **Step 5: Remove the probe file and confirm green**

Run: `rm internal/core/zzz_offload_probe_test.go`
Run: `jj st` — expected: `zzz_offload_probe_test.go` no longer present; working copy back to the intended change set.

- [ ] **Step 6: Commit**

```text
jj commit -m "feat(agents): local-test offload agent — distilled task test verdict (holomush-wagqb T2c)"
```

---

### Task 3: `local-pr-prep` offload agent (T2c)

**Files:**

- Create: `.claude/agents/local-pr-prep.md`

- [ ] **Step 1: Create the agent definition**

Create `.claude/agents/local-pr-prep.md` with exactly this content:

```markdown
---
name: local-pr-prep
description: |
  Run `task pr-prep` (the fast lane) in an isolated context and return ONLY the
  exit-code-first verdict plus the result-file status — never the multi-minute
  raw output. Use for ITERATION/TRIAGE while fixing a branch ("is pr-prep green
  yet?"). NOT a substitute for the final pre-push gate: the caller MUST run
  `task pr-prep` themselves before the real push, because a subagent cannot
  surface schema-regeneration side-effects. Read-only: runs the command, reports.
model: sonnet
effort: low
color: yellow
tools:
  - Bash
  - Read
  - Grep
  - Glob
---

You offload `task pr-prep`'s long, verbose output from the caller's context and
return a short verdict. Follow the holomush pr-prep reading protocol EXACTLY
(from CLAUDE.md), because go-task collapses every failure to exit 201.

## What to run

Run `task pr-prep` as the SOLE command in one Bash call — no `| tee`/`| tail`/
trailing `echo` (they mask `$?`). Do not add flags.

## How to read the result (exit-code first, then disambiguate)

1. Capture the exit code. `0` → PASS. Non-zero → something stopped it.
2. On non-zero, DISTINGUISH by behavior, never by grepping the output for a
   status string:
   - **Contention:** returned in ~2s, ran no lane steps, printed
     `ERROR: another pr-prep is running` to stderr. This is RETRY-able — report
     it as contention, do NOT treat it as a gate failure.
   - **Real failure:** ran lane steps (minutes), failed a named check
     (`fmt:check`, `lint:*`, a test, `license:check`, `build`). Report the
     failed check. Do NOT retry.
   - MUST NOT drive any retry off the string `another pr-prep is running` — the
     `pr-prep-lock.bats` self-test prints that exact string on HEALTHY runs.
3. Read the authoritative verdict file: pr-prep prints a line matching the
   prefix `▸ pr-prep result:` naming a file; read that file's `status=` line
   (`pass`/`fail`/`contention`). Match the prefix — do not assume a line number.

## What to return (STRICT)

On pass:
```

PASS — task pr-prep (status=pass)
✓ All PR checks passed.

```text

On real failure:
```

FAIL — task pr-prep (exit <code>, status=fail)
Failed check: <named gate, e.g. lint:go / fmt:check / build / a test pkg>
<one line of the first actionable error>

```text

On contention:
```

CONTENTION — another pr-prep is running (status=contention). Retry-able; not a gate failure.

```text

Rules:
- One line per failed check; do NOT paste the full lane output or stack traces.
- ALWAYS append this reminder to a PASS verdict: "Advisory only — the parent
  MUST run `task pr-prep` itself before the actual push (schema-regen side-effects)."
- NEVER edit files. NEVER push.
```

- [ ] **Step 2: Verify frontmatter parses**

After `/reload-plugins` or a fresh session: `rg -n '^name: local-pr-prep$' .claude/agents/local-pr-prep.md`
Expected: one match.

- [ ] **Step 3: Verification — dispatch and confirm the shape**

Dispatch `local-pr-prep` with prompt: "Run pr-prep and report."
Expected: one of PASS / FAIL / CONTENTION in the strict shape above; on PASS the
"Advisory only — parent MUST re-run" reminder is present; NO raw lane output in
the message. (Whichever verdict the branch actually yields is fine — you are
verifying the OUTPUT SHAPE and the advisory reminder, not forcing a pass.)

- [ ] **Step 4: Commit**

```text
jj commit -m "feat(agents): local-pr-prep offload agent — exit-code-first triage verdict (holomush-wagqb T2c)"
```

---

### Task 4: Verify + document repo-agent tiering (T2d)

The 6 repo-local agents are already correctly tiered; this task locks that in and
records the convention so drift is visible. Plugin agents are out of scope.

**Files:**

- Read-only: `.claude/agents/*.md`
- Modify: `.claude/rules/subagent-briefing.md` (append a short "model tier" note under the carve-out from Task 1)

- [ ] **Step 1: Snapshot the current tiering**

Run: `rg -n '^model:' .claude/agents/*.md`
Expected exactly (order may vary):

```text
abac-reviewer.md:model: opus
crypto-reviewer.md:model: opus
code-reviewer.md:model: opus
branch-readiness-check.md:model: sonnet
bead-auditor.md:model: sonnet
adr-extractor.md:model: sonnet
local-test.md:model: sonnet
local-pr-prep.md:model: sonnet
local-build.md:model: sonnet
local-lint.md:model: sonnet
```

(the last four exist after the offload-agent tasks — Tasks 2, 3, 7, 8; this task depends on all four). Confirm reviewers=opus, investigators/runners=sonnet — matches spec §4.2. If any disagree, that is the only place a change is warranted; otherwise no `model:` edits.

- [ ] **Step 2: Document the tier convention**

Append to `.claude/rules/subagent-briefing.md` (after the Task-1 carve-out row), a 3-line note:

```markdown
> **Repo-agent model tiers (verified 2026-07-03):** reviewers (`code`/`crypto`/`abac`-reviewer) = `opus`; investigators/runners (`bead-auditor`, `branch-readiness-check`, `adr-extractor`, `local-test`, `local-pr-prep`, `local-build`, `local-lint`) = `sonnet`. Plugin agents (design/plan-reviewer, fix-worker, Explore, …) are plugin-owned — the repo cannot set their model here.
```

- [ ] **Step 3: Verify the note is well-formed and the file still lints**

Run: `rg -n 'Repo-agent model tiers' .claude/rules/subagent-briefing.md`
Expected: one match.

- [ ] **Step 4: Commit**

```text
jj commit -m "docs(rules): document verified repo-agent model tiers (holomush-wagqb T2d)"
```

---

### Task 5: `search-tools.md` → pointer (T1b)

Reduce the 5.5 KB always-on rule (no `paths:` — loads every session) to a compact
pointer that preserves ONLY the repo-specific MUSTs the grepping skill does not
itself carry.

**Files:**

- Modify: `.claude/rules/search-tools.md` (shrink from ~76 lines to ~18)

- [ ] **Step 1: Extract the MUSTs to preserve**

Run: `rg -n 'MUST|exit code|never .*grep|probe' .claude/rules/search-tools.md`
The pointer MUST retain these repo-specific rules (verbatim intent):

1. Never bare `grep`/`egrep`/`fgrep` → use `rg` (hook nudges, honor it).
2. Prefer `mcp__probe__search_code` over `rg` for Go symbol/AST "where is X" questions.
3. `ast-grep` for structural matches / codemods.
4. MUST brief sub-agents on this ladder (they default to `rg`/full-file reads).
5. **The command-success rule:** use the EXIT CODE to decide pass/fail — never branch on a matched output string (the May 2026 `pr-prep`/`another pr-prep is running` cancel-loop incident); for background runs read the reported exit code, not the log tail.

- [ ] **Step 2: Replace the file body with the pointer**

Keep the file's existing top-of-file SPDX/license comment block AND its
`paths:`-less form (this rule stays unscoped/always-on intentionally so the
exit-code rule is always present). Replace only the body **below the header**
with:

```markdown
# Code & Text Search Tooling (pointer)

Full ladder + rg/ast-grep gotchas: the **`dev-flow:grepping` skill** (force-loaded
each session). This file keeps only the repo-specific MUSTs the skill does not carry.

| Requirement | Rule |
| --- | --- |
| MUST NOT bare `grep`/`egrep`/`fgrep` | Use `rg`. A `PreToolUse` hook nudges; honor it. |
| MUST prefer `mcp__probe__search_code` over `rg` | For Go symbol/AST "where is X / how does Y work". |
| SHOULD use `ast-grep` (`sg -l go`) | Structural matches + codemods; NOT for pkg-qualified call patterns (misparse — use `rg`). |
| MUST brief sub-agents on the ladder | They default to `rg` / full-file `Read` without it. |

## Searching files ≠ judging whether a command succeeded

| Requirement | Rule |
| --- | --- |
| MUST decide pass/fail by EXIT CODE | Never grep a command's stdout/stderr for a "success"/"error" string — output can echo fixtures or your own input. |
| MUST NOT branch on a matched output string | Real incident (May 2026): an agent grepped `pr-prep` output for `another pr-prep is running` — a string the `pr-prep-lock.bats` self-test prints on HEALTHY runs — and re-ran the full lane forever. |
| MUST read the exit code (not the log tail) for background runs | Piping through `tee`/`tail`/trailing `echo` masks `$?` unless `set -o pipefail`. |
| MAY grep output as a SECONDARY confirmation | Only after the exit code says pass (e.g. matching `✓ All PR checks passed.`). |
```

- [ ] **Step 3: Verify the shrink and MUST-preservation**

Run: `wc -l .claude/rules/search-tools.md` — expected: roughly 18–24 lines (down from ~76).
Run: `rg -c 'exit code|probe|bare .grep|another pr-prep is running' .claude/rules/search-tools.md` — expected: ≥ 4 (all five MUSTs present).

- [ ] **Step 4: Commit**

```text
jj commit -m "docs(rules): reduce search-tools.md to a pointer over the grepping skill (holomush-wagqb T1b)"
```

---

### Task 6: Document the `bd prime` operator-side de-dup (T1a — RD4)

**Decided (RD4): no repo change.** The repo `.claude/settings.json` already
registers `bd prime` exactly once per event (SessionStart `:69`, PreCompact `:35`)
— it is the correct, portable single source. The observed doubling originates
OUTSIDE the repo: the operator's `~/.claude/settings.json` PreCompact registration
(`:176`) plus `bd`'s plugin-level SessionStart prime injector. So this task is
**documentation only** — it records the operator-side cleanup and edits no
repo-tracked file.

> **Bead materialization note:** this is a doc/operator task with a fresh-session
> manual verification; its acceptance is "operator cleanup documented + verified
> once." It makes **no repo change** and MUST NOT edit `.claude/settings.json`.

**Files:**

- Read-only: `.claude/settings.json` (confirm already single-source), `~/.claude/settings.json`
- Operator-side only (NOT repo-tracked, NOT in the PR): `~/.claude/settings.json`

- [ ] **Step 1: Confirm the repo is already single-source**

Run: `rg -n 'bd prime' .claude/settings.json`
Expected: exactly two matches — one under SessionStart, one under PreCompact (one
registration per event; NOT a within-repo duplicate). No repo edit is warranted.

- [ ] **Step 2: Confirm the external duplicates**

Run: `rg -n 'bd prime' ~/.claude/settings.json`
Expected: a PreCompact `bd prime` in the operator's global — the PreCompact
duplicate. For SessionStart, the second copy comes from `bd`'s own plugin-level
prime injector (enabled `beads@beads-marketplace`), not a repo hook.

- [ ] **Step 3: Document the operator-side cleanup (no repo edit)**

Record on `holomush-wagqb` (via `bd note`) the exact operator-side action to
reclaim the ~1.2K tok/session, WITHOUT editing any repo file:

- Remove the duplicate PreCompact `bd prime` from `~/.claude/settings.json` (the
  repo already provides PreCompact prime portably; the operator's global also
  covers their non-holomush projects, so this is their discretion).
- If a fresh session still shows two SessionStart "Beads Workflow Context" blocks,
  the second is the beads-plugin injector — the operator disables whichever of the
  repo SessionStart `bd prime` OR the plugin injector is redundant *on their
  machine* (do NOT drop the repo one in the PR — it is the portable guarantee).

The repo `.claude/settings.json` is **left unchanged**.

- [ ] **Step 4: Verify a single Beads Workflow Context block (operator, fresh session)**

After the operator applies their `~/.claude` cleanup, a fresh Claude Code session's
SessionStart output shows the "Beads Workflow Context" block exactly ONCE. This is
an operator / fresh-session check; record the outcome on `holomush-wagqb`.

- [ ] **Step 5: No repo commit**

This task makes **no repo-tracked change** — nothing to commit. The operator's
`~/.claude/settings.json` edit is local-machine only; note completion on
`holomush-wagqb`.

---

### Task 7: `local-build` offload agent (T2c)

Sibling of `local-test`; identical run-and-distill contract. Model `sonnet`,
`effort: low` (RD3: net win on an opus/fable main session even though build
output is short).

**Files:**

- Create: `.claude/agents/local-build.md`

- [ ] **Step 1: Create the agent definition**

Create `.claude/agents/local-build.md` with exactly this content:

```markdown
---
name: local-build
description: |
  Run `task build` in an isolated context and return ONLY a compact pass/fail
  verdict with the first compile errors — never the raw build output. Use when
  the caller wants "does it build", "check the build". Read-only: runs the
  command, reports. Does NOT edit code.
model: sonnet
effort: low
color: blue
tools:
  - Bash
  - Read
  - Grep
  - Glob
---

You offload `task build` output from the caller's context. Your final message is
SHORT: the caller reads your verdict, not the build log.

## Run

`task build` as the SOLE command in one Bash call (no `| tee`/`| tail`/trailing
`echo`, which mask the exit code). Use `task` — never raw `go build`.

## Decide pass/fail

Use the EXIT CODE, not string-matching output. `$?` == 0 → pass.

## Return (STRICT — your whole output)

On pass: `PASS — task build`

On fail:
FAIL — task build (exit <code>)
Compile errors:

- <file>:<line>: <message>
- ... (at most 15; append "(+<M> more)" if truncated)

Rules: one line per error (`file:line: message`); NEVER paste the full build
output or stack traces; NEVER edit files.
```

- [ ] **Step 2: Verify the frontmatter parses**

After `/reload-plugins` or a fresh session: `rg -n '^name: local-build$' .claude/agents/local-build.md`
Expected: one match.

- [ ] **Step 3: Positive verification**

Dispatch `local-build` (Agent tool, `subagent_type: local-build`) with: "Run the build and report."
Expected: `PASS — task build`, no raw build output, ~1 line.

- [ ] **Step 4: Negative verification**

Create `internal/core/zzz_build_probe.go`:

```go
package core

var _ = thisSymbolIntentionallyDoesNotExist
```

Dispatch `local-build` with: "Run the build and report."
Expected: `FAIL — task build (exit <code>)` with a `zzz_build_probe.go` compile
error line (undefined: `thisSymbolIntentionallyDoesNotExist`), NO raw output.

- [ ] **Step 5: Remove the probe, confirm green, commit**

Run: `rm internal/core/zzz_build_probe.go` then `jj st` (probe gone).

```text
jj commit -m "feat(agents): local-build offload agent — distilled task build verdict (holomush-wagqb T2c)"
```

---

### Task 8: `local-lint` offload agent (T2c)

Sibling of `local-test`; identical contract. Model `sonnet`, `effort: low`.
`task lint` output is often long (many linters × files), so its offload value is
high.

**Files:**

- Create: `.claude/agents/local-lint.md`

- [ ] **Step 1: Create the agent definition**

Create `.claude/agents/local-lint.md` with exactly this content:

```markdown
---
name: local-lint
description: |
  Run `task lint` in an isolated context and return ONLY a compact pass/fail
  verdict with one line per finding — never the raw linter output (which fans
  out across many linters × files and is often long). Use when the caller wants
  "run the linters", "check lint". Read-only: runs the command, reports. Does
  NOT edit code or auto-fix.
model: sonnet
effort: low
color: magenta
tools:
  - Bash
  - Read
  - Grep
  - Glob
---

You offload `task lint`'s long output from the caller's context. Your final
message is SHORT.

## Run

`task lint` as the SOLE command in one Bash call (no `| tee`/`| tail`/trailing
`echo`). Use `task` — never `golangci-lint`/`gofmt` directly.

## Decide pass/fail

Use the EXIT CODE, not string-matching output. `$?` == 0 → pass.

## Return (STRICT)

On pass: `PASS — task lint`

On fail:
FAIL — task lint (exit <code>)
Findings:

- <file>:<line>: <linter>: <message>
- ... (at most 20; append "(+<M> more of <total>)" if truncated)

Rules: one line per finding; NEVER paste the full linter output; NEVER edit
files or run an auto-fixer.
```

- [ ] **Step 2: Verify the frontmatter parses**

After `/reload-plugins` or a fresh session: `rg -n '^name: local-lint$' .claude/agents/local-lint.md`
Expected: one match.

- [ ] **Step 3: Positive verification**

Dispatch `local-lint` (Agent tool, `subagent_type: local-lint`) with: "Run the linters and report."
Expected: `PASS — task lint` (assuming the tree is clean), no raw output.

- [ ] **Step 4: Negative verification**

Create `internal/core/zzz_lint_probe.go` with a violation the active sloglint
`context: scope` rule flags (a bare `slog.Info` with a `ctx` in scope — per
`.claude/rules/logging.md`, this check is active):

```go
package core

import (
	"context"
	"log/slog"
)

func zzzLintProbe(ctx context.Context) { slog.Info("probe") }
```

Dispatch `local-lint` with: "Run the linters and report."
Expected: `FAIL — task lint (exit <code>)` naming `zzz_lint_probe.go` in a
finding line, NO raw output. (Any lint finding on the probe file satisfies the
FAIL-shape check — the goal is to verify the distilled fail format.)

- [ ] **Step 5: Remove the probe, confirm green, commit**

Run: `rm internal/core/zzz_lint_probe.go` then `jj st` (probe gone).

```text
jj commit -m "feat(agents): local-lint offload agent — distilled task lint verdict (holomush-wagqb T2c)"
```

---

## Verification (whole-plan)

Per spec §7:

- **Context tax:** after the repo tasks land and a fresh session starts, confirm
  `search-tools.md` no longer contributes its full 5.5 KB (it is now ~1 KB).
  Record the before/after SessionStart size delta on `holomush-wagqb`. (The
  single-`bd prime` check is operator-side per RD4 / Task 6.)
- **Offload agents:** confirm a `local-test` / `local-build` / `local-lint` run
  keeps the raw command output out of the main thread (final message is the
  compact verdict only).
- **Guardrail preservation:** confirm every MUST listed in Task 5 Step 1 appears
  in the new `search-tools.md`, and the `subagent-briefing` carve-out preserves
  the reviewer-tier fence.
- **No product-code change:** `jj diff --stat` shows only `.claude/**` and
  `docs/**` paths touched.

## Out of scope

Product code, CI workflows, the beads/jj toolchain, and Phase 3/4 initiatives
(T1c, T2a, T2b, T3a — see Scope table; mechanisms/targets decided per RD1/RD2,
execution is a follow-on plan; T3a additionally needs a human line-review).
<!-- adr-capture: sha256=8ed595dc155d2956; session=cli; ts=2026-07-03T16:05:18Z; adrs=holomush-w1a26,holomush-etnfd,holomush-cr3gq -->

<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Offload-Agent Adoption + Token Optimization Round 2 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make offload the enforced default for verbose `task` runs in the main session, and ship the wagqb Phase 3/4 always-on token reductions.

**Architecture:** All changes live under `.claude/**`, `CLAUDE.md`, `scripts/tests/`, and `site/src/content/docs/contributing/` (AI-tooling config + doc relocation — no product code). One new merged agent (`local-check`) replaces three; a PreToolUse deny (main-session-only, gated on an empirical probe) enforces delegation; rule/CLAUDE.md trims follow wagqb's index-pointer mechanism. Spec: `docs/superpowers/specs/2026-07-06-offload-adoption-token-optimization-design.md` (holomush-drf7b).

**Tech Stack:** Claude Code agent/hook config (YAML-frontmatter markdown, bash hooks, jq), bats (`scripts/tests/`), jj.

**Ground rules for every task (from the spec + repo memories):**

- Work in the `agent-token-optimization` jj workspace. Commit per task with the message given in the task. `jj --no-pager` on every jj command; `--git` on diffs.
- A newly created/edited `.claude/agents/*.md` CANNOT be dispatched in the session that created it (agent registry is fixed at session start). Hook **script** edits take effect immediately (scripts are executed per call; only `settings.json` registration is session-start-fixed). Verify agents by file inspection + a fresh-session note, hooks by direct stdin invocation.
- `task fmt` mutates files (SPDX headers) — run it before each commit and include its output in the commit.
- Never run `bd github sync`. Never `jj rebase -r @ -o main@origin` (use the `-s` recipe).

---

## File structure (what exists / what this plan touches)

| Path | Action | Task |
| --- | --- | --- |
| `.claude/agents/local-check.md` | Create (merge of 3) | 1 |
| `.claude/agents/local-test.md`, `local-lint.md`, `local-build.md` | Delete | 1 |
| `.claude/agents/local-pr-prep.md` | Modify (description only) | 1 |
| `.claude/hooks/enforce-task-runner.sh` | Modify (probe: temp; offload rules: permanent) | 2, 3 |
| `.claude/hooks/enforce-task-runner.test.sh` | Create (colocated harness) | 3 |
| `scripts/tests/claude-hooks.bats` | Create (wires harness into `task test:bats`) | 3 |
| `.claude/hooks/remind-pre-action-review.sh` | Modify (intent verbs) | 4 |
| `CLAUDE.md` | Modify (T1c §Landing de-dup: task 5; T3a trim + delegation rows + link fixes: task 10) | 5, 10 |
| `.claude/rules/subagent-briefing.md` | Modify (slim + local-check refs) | 5 |
| `.claude/hooks/require-grepping-skill.sh` | Modify (cheat-sheet) | 6 |
| `.claude/rules/testing.md` | Modify (split) | 7 |
| `.claude/rules/references/testing-detail.md` | Create | 7 |
| `.claude/rules/invariants.md` | Modify (split) | 8 |
| `.claude/rules/references/invariants-detail.md` | Create | 8 |
| `.claude/agents/{crypto-reviewer,bead-auditor,code-reviewer}.md` | Modify (descriptions only) | 9 |
| `site/src/content/docs/contributing/how-to/{pr-prep,pr-guide,sessions,integration-tests}.md` | Modify (receive relocated prose) | 10 |
| `docs/roadmap.md` | Modify (receives Strategic Themes query detail) | 10 |

`.claude/rules/references/` does not exist yet — Task 7 creates it. Everything else exists on disk (verified 2026-07-06).

---

### Task 1: `local-check` agent (merge) + `local-pr-prep` description

**Files:**

- Create: `.claude/agents/local-check.md`
- Delete: `.claude/agents/local-test.md`, `.claude/agents/local-lint.md`, `.claude/agents/local-build.md`
- Modify: `.claude/agents/local-pr-prep.md` (frontmatter `description:` block only)

- [ ] **Step 1: Create `.claude/agents/local-check.md`** with exactly this content:

````markdown
---
name: local-check
description: |
  MUST be dispatched instead of running `task test`, `task lint`, `task build`,
  `task test:int`, or `task test:cover` inline in the main session — inline runs
  flood the main context with raw output (hook-enforced; `# offload-exempt`
  overrides only when raw output is genuinely needed in-thread). Give it a check
  kind — test | lint | build | int | cover — plus optional package / `-run`
  args; it returns a compact pass/fail verdict, never the raw log. Read-only:
  runs the command, reports. Does NOT edit code or fix failures.
model: sonnet
effort: low
permissionMode: plan
color: green
tools:
  - Bash
  - Read
  - Grep
  - Glob
---

You are a check-runner that offloads verbose `task` output from the caller's
context. Your ENTIRE value is that your final message is SHORT: the caller
reads your ~10-line verdict instead of thousands of lines of raw output.

## What to run

The caller names a kind and optional args. Kind → command:

| Kind    | Command                                      |
| ------- | -------------------------------------------- |
| `test`  | `task test [-- <pkg> / -run='…' <pkg>]`      |
| `lint`  | `task lint`                                  |
| `build` | `task build`                                 |
| `int`   | `task test:int [-- <pkg>]` (requires Docker) |
| `cover` | `task test:cover`                            |

No kind given → ask nothing; infer from the caller's wording (e.g. "does it
build" → build); if genuinely ambiguous, run `test`.

Before building a `test`/`int` command, validate any caller-supplied value:

- Package path MUST match `^\./[A-Za-z0-9_./-]+$` — refuse and report an
  error if it contains anything else (the pattern already covers `./...`).
- `-run` filter: refuse and report an error if the value contains a
  single-quote character (`'`) — there is no safe way to embed one in the
  single-quoted argument below. Otherwise standard Go regexp syntax is
  expected and allowed, INCLUDING `|` for multi-test alternation (e.g.
  `TestA|TestB`, the pattern `.claude/rules/invariants.md` itself prescribes).
- ALWAYS bind the `-run` value with `=` and single-quote it as ONE token —
  `-run='<value>'`, never `-run <value>` as two separate tokens. `=`-binding
  means the value can never be misparsed as a separate flag (even if it
  starts with `-`); single-quoting means no character inside it is special to
  the shell except the single quote already excluded above.

Run the command as the SOLE command in one Bash call (no `| tee`/`| tail`/
trailing `echo`, which mask the exit code). Use `task` — never raw `go test`,
`go build`, `golangci-lint`, or `gofmt`.

## How to decide pass/fail

Use the EXIT CODE, not string-matching the output (a passing run can echo the
word "FAIL" from a test fixture). `$?` == 0 → pass. Non-zero → fail.

## What to return (STRICT — this is your whole output)

On pass:

```text
PASS — `<command you ran>`
<one line: N packages ok / N tests ok / clean lint / built OK / coverage summary>
```

For `cover` on pass, the summary line lists overall coverage plus any package
below the 80% floor (one `pkg: NN.N%` item each); if none are below, say so.

On fail:

```text
FAIL — `<command you ran>` (exit <code>)
<per-kind failure lines — see caps below>
```

- `test`/`int`/`cover`: one line per failing test — `<TestName> (<pkg>): <first
  line of the failure/assert message>` — at most 15; if more, append
  `(+<M> more — re-run scoped to see them)`.
- `lint`: one line per finding — `<file>:<line>: <linter>: <message>` — at
  most 20; append `(+<M> more of <total>)` if truncated.
- `build`: one line per compile error — `<file>:<line>: <message>` — at most
  15; append `(+<M> more)` if truncated.

Rules:

- If the command could not run at all (compile error before tests, missing
  Docker for `int` or session-store tests), say so in ONE line and quote only
  the first error line.
- NEVER include the full command output. NEVER paste stack traces, diffs, or
  `--- FAIL` blocks verbatim. NEVER edit files or run an auto-fixer.
````

- [ ] **Step 2: Delete the three superseded agents**

```bash
rm .claude/agents/local-test.md .claude/agents/local-lint.md .claude/agents/local-build.md
```

- [ ] **Step 3: Rewrite `local-pr-prep`'s description** — in `.claude/agents/local-pr-prep.md`, replace the entire frontmatter `description: |` block (keep every other frontmatter key and the whole body untouched) with:

```yaml
description: |
  MUST be used for `task pr-prep` ITERATION/TRIAGE ("is pr-prep green yet?")
  instead of running it inline — returns the exit-code-first verdict plus the
  result-file status, never the multi-minute raw output. NOT the final gate:
  the parent MUST still run `task pr-prep` itself before the actual push
  (schema-regeneration side-effects a subagent cannot surface). Read-only.
```

- [ ] **Step 4: Verify** — registry is session-start-fixed, so no live dispatch; verify statically:

```bash
rg -c '^name: local-check$' .claude/agents/local-check.md   # expect 1
ls .claude/agents/ | rg 'local-'                            # expect exactly: local-check.md, local-pr-prep.md
rg -n 'offload-exempt' .claude/agents/local-check.md        # expect a hit in the description
```

Note in the task hand-off: live-dispatch verification of `local-check` needs a fresh session (recorded gotcha).

- [ ] **Step 5: Commit**

```bash
task fmt
jj --no-pager commit -m "feat(agents): merge local-test/lint/build into local-check; directive descriptions (holomush-drf7b §3.1-3.2)"
```

---

### Task 2: Empirical probe — `agent_id` presence + deny-schema (gates Task 3's default)

**Files:**

- Modify (temporarily, reverted in-task): `.claude/hooks/enforce-task-runner.sh`

- [ ] **Step 1: Add the probe line.** In `enforce-task-runner.sh`, immediately after the line `[[ -z "$COMMAND" ]] && exit 0`, insert:

```bash
# TEMP PROBE (holomush-drf7b §7) — remove before commit
printf '%s\n' "$INPUT" >> /tmp/holomush-hook-probe.jsonl
```

- [ ] **Step 2: Generate a main-session sample** — run any Bash command in the main session (the hook fires on it):

```bash
echo probe-main
```

- [ ] **Step 3: Generate a subagent sample** — dispatch the `Explore` agent (plugin-provided, registered at session start) with the prompt: *"Run exactly one Bash command: `echo probe-sub`. Then reply DONE. Nothing else."*

- [ ] **Step 4: Read the probe log and compare**

```bash
jq -c '{agent_id: (.agent_id // "ABSENT"), agent_type: (.agent_type // "ABSENT"), cmd: .tool_input.command}' /tmp/holomush-hook-probe.jsonl
```

Expected: the `probe-main` entry shows `agent_id: "ABSENT"`; the `probe-sub` entry shows a non-empty `agent_id`. **Decision rule (RD5):** both as expected → Task 3 ships `OFFLOAD_ENFORCE` defaulting to `deny`. `agent_id` absent in the subagent entry (or present in the main entry) → Task 3 ships defaulting to `nudge`, and you MUST file a follow-up bead: `bd create --type=task --title="Flip OFFLOAD_ENFORCE to deny once agent_id lands in PreToolUse hook input" --priority=2 --description="holomush-drf7b RD5 fallback: the empirical probe found agent_id unusable for main-vs-subagent discrimination on this CC version; enforce-task-runner.sh shipped OFFLOAD_ENFORCE=nudge. Re-probe on CC upgrades and flip the default to deny."`

- [ ] **Step 5: Remove the probe line and the log; record the finding**

```bash
rm -f /tmp/holomush-hook-probe.jsonl
jj --no-pager diff --git   # expect ONLY the probe line as a leftover; remove it via Edit, then expect empty diff
bd note holomush-drf7b "probe (§7): agent_id main=<ABSENT|value> sub=<value|ABSENT> → OFFLOAD_ENFORCE default=<deny|nudge>"
```

No commit (net-zero diff). The deny-JSON *schema* half of the probe is verified live in Task 3 Step 7.

---

### Task 3: Hook enforcement — offload deny/nudge in `enforce-task-runner.sh` (TDD)

**Files:**

- Create: `.claude/hooks/enforce-task-runner.test.sh` (colocated harness, `nudge-adr-capture.test.sh` pattern)
- Modify: `.claude/hooks/enforce-task-runner.sh`
- Create: `scripts/tests/claude-hooks.bats` (wires the harness into `task test:bats`, which pr-prep runs)

- [ ] **Step 1: Write the failing harness.** Create `.claude/hooks/enforce-task-runner.test.sh` (make it executable) with exactly:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Test harness for enforce-task-runner.sh offload rules (holomush-drf7b §3.3).
# Feeds synthetic PreToolUse JSON on stdin; asserts exit code, stdout
# (deny JSON), and stderr (nudges). Pattern: nudge-adr-capture.test.sh.

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK="$SCRIPT_DIR/enforce-task-runner.sh"

pass=0
fail=0

# mkinput <command> [agent_id]
mkinput() {
  if [ -n "${2:-}" ]; then
    jq -cn --arg cmd "$1" --arg aid "$2" '{tool_input:{command:$cmd}, agent_id:$aid}'
  else
    jq -cn --arg cmd "$1" '{tool_input:{command:$cmd}}'
  fi
}

# expect_case <name> <mode> <stdin-json> <want_exit> <stdout-pat-or-empty> <stderr-pat-or-empty>
expect_case() {
  local name="$1" mode="$2" input="$3" want_exit="$4" want_out="$5" want_err="$6"
  local got_out got_err got_exit errf
  errf="$(mktemp)"
  got_out="$(printf '%s' "$input" | OFFLOAD_ENFORCE="$mode" "$HOOK" 2>"$errf")" && got_exit=0 || got_exit=$?
  got_err="$(cat "$errf")"; rm -f "$errf"
  if [ "$got_exit" -ne "$want_exit" ]; then
    echo "FAIL $name: exit $got_exit, want $want_exit" >&2; fail=$((fail+1)); return
  fi
  if [ -z "$want_out" ]; then
    [ -n "$got_out" ] && { echo "FAIL $name: stdout non-empty: $got_out" >&2; fail=$((fail+1)); return; }
  else
    printf '%s' "$got_out" | grep -qE "$want_out" || { echo "FAIL $name: stdout '$got_out' !~ /$want_out/" >&2; fail=$((fail+1)); return; }
  fi
  if [ -n "$want_err" ]; then
    printf '%s' "$got_err" | grep -qE "$want_err" || { echo "FAIL $name: stderr '$got_err' !~ /$want_err/" >&2; fail=$((fail+1)); return; }
  fi
  pass=$((pass+1))
}

DENY_PAT='"permissionDecision": *"deny"'

# --- deny mode: each matched task name in the MAIN session is denied ---
for name in test test:int test:cover lint build; do
  expect_case "deny-$name" deny "$(mkinput "task $name")" 0 "$DENY_PAT" ""
done
expect_case "deny-test-with-args" deny "$(mkinput 'task test -- ./internal/command/')" 0 "$DENY_PAT" ""
expect_case "deny-chained" deny "$(mkinput 'cd foo && task test')" 0 "$DENY_PAT" ""
expect_case "deny-names-local-check" deny "$(mkinput 'task lint')" 0 'local-check' ""

# --- NOT matched: excluded names run untouched ---
for name in test:verbose test:e2e lint:go lint:proto docs:build fmt; do
  expect_case "allow-$name" deny "$(mkinput "task $name")" 0 "" ""
done

# --- subagent (agent_id present): all offload rules skipped ---
expect_case "subagent-skip" deny "$(mkinput 'task test' 'agent-abc123')" 0 "" ""

# --- escape hatch ---
expect_case "exempt" deny "$(mkinput 'task test # offload-exempt')" 0 "" ""

# --- pr-prep family: never denied, soft stderr nudge only ---
for name in pr-prep pr-prep:full pr-prep:docs; do
  expect_case "prprep-$name" deny "$(mkinput "task $name")" 0 "" 'local-pr-prep'
done

# --- nudge mode: matched names nudge on stderr, no deny JSON ---
expect_case "nudge-mode" nudge "$(mkinput 'task test')" 0 "" 'local-check'

# --- pre-existing rules unaffected: raw go test still exit-2 blocked ---
expect_case "go-test-still-blocked" deny "$(mkinput 'go test ./...')" 2 "" "task test"

echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
```

```bash
chmod +x .claude/hooks/enforce-task-runner.test.sh
```

- [ ] **Step 2: Run the harness — verify it fails**

```bash
.claude/hooks/enforce-task-runner.test.sh
```

Expected: FAIL lines for every `deny-*`, `prprep-*`, `nudge-mode` case (the offload rules don't exist yet); `allow-*`, `subagent-skip`, `exempt`, `go-test-still-blocked` may already pass. Non-zero exit.

- [ ] **Step 3: Implement.** Three edits to `.claude/hooks/enforce-task-runner.sh`:

**(a)** After the line `[[ -z "$COMMAND" ]] && exit 0`, insert:

```bash
# --- Offload enforcement config (holomush-drf7b §3.3) ---
# Main-session inline `task test|test:int|test:cover|lint|build` is redirected
# to the local-check agent. deny = PreToolUse JSON permission denial (cancels
# the call — NOTE: a deny also cancels sibling calls in a parallel tool batch;
# accepted, since the replacement is an Agent dispatch and verbose runs are
# usually solo). nudge = stderr advisory only. Env-overridable for tests and
# emergencies. Subagent calls (agent_id present) are exempt: offload agents
# and implementer subagents run task freely in their own cheap contexts.
# Escape hatch: append `# offload-exempt` to the command (cf. # jj-exempt).
OFFLOAD_ENFORCE="${OFFLOAD_ENFORCE:-deny}"   # ← 'deny' only if the Task-2 probe passed; else 'nudge'
AGENT_ID=$(echo "$INPUT" | jq -r '.agent_id // empty' 2>/dev/null) || AGENT_ID=""
```

(If the Task 2 probe decided `nudge`, write `nudge` as the default here instead.)

**(b)** In the `case "$word" in` block inside the segment loop, add this branch immediately before the `go)` branch:

```bash
    task)
      # Offload redirect — main session only, exempt token honored.
      if [[ -z "$AGENT_ID" ]] && [[ "$COMMAND" != *"# offload-exempt"* ]]; then
        offload_kind=""
        case "$second_word" in
          test)       offload_kind="test" ;;
          test:int)   offload_kind="int" ;;
          test:cover) offload_kind="cover" ;;
          lint)       offload_kind="lint" ;;
          build)      offload_kind="build" ;;
          pr-prep|pr-prep:full|pr-prep:docs)
            echo "Nudge: iterating on pr-prep? Dispatch the local-pr-prep agent for a compact verdict. Final pre-push gate? Run inline — the parent MUST run it itself. (task $second_word still runs.)" >&2
            ;;
        esac
        if [[ -n "$offload_kind" ]]; then
          offload_args="${rest#"$second_word"}"
          offload_args="$(strip_leading_ws "$offload_args")"
          suggested="$offload_kind${offload_args:+ $offload_args}"
          if [[ "$OFFLOAD_ENFORCE" == "deny" ]]; then
            jq -cn --arg reason "Inline \`task $second_word\` floods the main context. Dispatch the local-check agent (Agent tool, subagent_type: local-check, prompt: '$suggested') and read its compact verdict. If raw output is genuinely needed in-thread, re-run with \`# offload-exempt\` appended." \
              '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:$reason}}'
            exit 0
          else
            echo "Nudge: dispatch the local-check agent (subagent_type: local-check, prompt: '$suggested') instead of inline task $second_word — keeps raw output out of the main context. Append # offload-exempt if raw output is needed. (task $second_word still runs.)" >&2
          fi
        fi
      fi
      ;;
```

**(c)** Update the header comment block (lines 5–19 region): after the sentence about hard-blocking direct go/lint commands, add one sentence: `Additionally REDIRECTS main-session inline 'task test|test:int|test:cover|lint|build' to the local-check offload agent (deny or nudge per OFFLOAD_ENFORCE; '# offload-exempt' escape hatch; subagent calls exempt via agent_id) — holomush-drf7b §3.3.`

Implementation notes (already reflected above — do not deviate): no bash-4 associative arrays (macOS `/usr/bin/env bash` may be 3.2); the deny JSON is built with `jq -cn --arg` so the reason is safely escaped; the exempt check runs against the RAW `$COMMAND` (before quote-stripping), so the comment token survives; first matched segment wins (`exit 0` after the deny).

- [ ] **Step 4: Run the harness — verify all cases pass**

```bash
.claude/hooks/enforce-task-runner.test.sh
```

Expected: `pass=<N> fail=0`, exit 0.

- [ ] **Step 5: Wire into an executed target.** First check the precedent harness is green, then create the shim:

```bash
.claude/hooks/nudge-adr-capture.test.sh; echo "exit=$?"
```

Create `scripts/tests/claude-hooks.bats` with exactly (if the precedent harness FAILED above, omit its `@test` block and file a bug bead citing the failure output instead):

```bash
#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Executes the colocated .claude/hooks/*.test.sh harnesses (which previously
# had no wired runner) as part of `task test:bats` — which pr-prep runs.

setup() {
  REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
}

@test "enforce-task-runner hook harness passes" {
  run "$REPO_ROOT/.claude/hooks/enforce-task-runner.test.sh"
  [ "$status" -eq 0 ]
}

@test "nudge-adr-capture hook harness passes" {
  run "$REPO_ROOT/.claude/hooks/nudge-adr-capture.test.sh"
  [ "$status" -eq 0 ]
}
```

- [ ] **Step 6: Run the bats suite**

```bash
task test:bats
```

Expected: all bats files pass, including the two new `claude-hooks` tests.

- [ ] **Step 7: Live schema verification (second half of the §7 probe).** In the MAIN session, run:

```bash
task build
```

Expected (deny default): the call is DENIED and the model-visible reason names `local-check` — this confirms the CC version accepts the deny-JSON schema end-to-end. If instead the command runs (schema silently ignored): flip `OFFLOAD_ENFORCE` default to `nudge`, file the RD5 follow-up bead (Task 2 Step 4 wording), and note the schema mismatch on holomush-drf7b. Then confirm the escape hatch:

```bash
task build # offload-exempt
```

Expected: runs normally. Record: `bd note holomush-drf7b "schema probe: deny <accepted|ignored> live; escape hatch OK"`

- [ ] **Step 8: Commit**

```bash
task fmt
jj --no-pager commit -m "feat(hooks): main-session offload deny/nudge for task test|lint|build + harness + bats wiring (holomush-drf7b §3.3)"
```

---

### Task 4: Intent-verb reminder in `remind-pre-action-review.sh`

**Files:**

- Modify: `.claude/hooks/remind-pre-action-review.sh`

- [ ] **Step 1: Add the branch.** Immediately before the `# plan-reviewer triggers:` comment block, insert:

```bash
# local-check offload triggers: prompt asks to run tests/lint/build. Remind to
# dispatch the offload agent rather than running the task inline (holomush-drf7b §3.4).
if printf '%s' "$lower" | grep -qE '(run[[:space:]]+(the[[:space:]]+)?tests?\b|does[[:space:]]+it[[:space:]]+(build|compile)|check[[:space:]]+(the[[:space:]]+)?lint|run[[:space:]]+lint|is[[:space:]]+(it|the[[:space:]]+build)[[:space:]]+green)'; then
  reminders+=("**Offload reminder:** dispatch the \`local-check\` agent (\`subagent_type: local-check\`, prompt: \`<test|lint|build|int|cover> [args]\`) instead of running \`task test\`/\`task lint\`/\`task build\` inline — inline runs are hook-denied in the main session (\`# offload-exempt\` to override).")
fi
```

- [ ] **Step 2: Verify by direct invocation**

```bash
printf '{"prompt":"run the tests for internal/naming"}' | .claude/hooks/remind-pre-action-review.sh
printf '{"prompt":"refactor the naming package"}' | .claude/hooks/remind-pre-action-review.sh; echo "exit=$?"
```

Expected: first prints the `Offload reminder` block; second prints nothing, exit 0.

- [ ] **Step 3: Commit**

```bash
task fmt
jj --no-pager commit -m "feat(hooks): offload intent-verb reminder in remind-pre-action-review (holomush-drf7b §3.4)"
```

---

### Task 5: T1c — landing-the-plane de-dup + subagent-briefing slim

**Files:**

- Modify: `CLAUDE.md` (§"Landing the Plane" only)
- Modify: `.claude/rules/subagent-briefing.md`

`landing-the-plane.md` is CANONICAL and is NOT touched (revised T1c; the wagqb pointer-ization is void). Only the CLAUDE.md-side duplicate collapses.

- [ ] **Step 1: Collapse CLAUDE.md's duplicated rebase recipe.** In `CLAUDE.md` § "Landing the Plane (Session Completion)", replace the entire "**Pre-push rebase** — defer to the `jj:jujutsu` skill's…" paragraph (the one containing the full `jj rebase -s "$(jj log …)"` command and the PR #4049 reference) with this single line:

```markdown
**Pre-push rebase:** use the chain-safe `-s` recipe — canonical copy in `.claude/rules/landing-the-plane.md` (Mandatory checklist, "Push" item) and the `jj:jujutsu` skill ("Pre-Push Rebase"); the `guard-jj-rebase-chain` hook blocks the truncation-prone `-r @` shape.
```

- [ ] **Step 2: Slim `subagent-briefing.md` in place** (every MUST retained):
  1. The `local-test`/`local-lint`/`local-build` mentions live in exactly two places — the "Repo-agent model tiers" blockquote and the `local-*` anti-pattern bullet (the "Sub-agent dispatch" table row names no local-* agents; leave it). In the blockquote, the investigator list becomes `bead-auditor`, `branch-readiness-check`, `adr-extractor`, `local-check`, `local-pr-prep`.
  2. Replace the anti-pattern bullet "DO NOT dispatch a `local-*` offload agent … cancel-storm (ADR holomush-cr3gq)" text's agent list the same way (keep the rule and the ADR cite verbatim).
  3. Replace item 3 of "Always include" (the tool-precedence paragraph) with the one-liner: `3. **Tool precedence** — brief them on the search ladder per \`.claude/rules/search-tools.md\` (probe → rg → ast-grep; never bare grep). Sub-agents default to rg/full-file reads without it.`
  4. Add one bullet at the end of "Always include": `6. **Verbose task runs** — sub-agents run \`task test\`/\`lint\`/\`build\` inline in their own context (they are exempt from the offload deny); the PARENT session must not.`

- [ ] **Step 3: Verify**

```bash
rg -n 'local-(test|lint|build)\b' CLAUDE.md .claude/rules/ && echo "STALE REFS REMAIN" || echo "clean"
rg -c 'MUST' .claude/rules/subagent-briefing.md   # compare before/after: count must not DECREASE except via the item-3 pointer swap
```

- [ ] **Step 4: Commit**

```bash
task fmt
jj --no-pager commit -m "docs(rules): T1c revised — collapse CLAUDE.md rebase duplicate; slim subagent-briefing, local-check refs (holomush-drf7b §4.1)"
```

---

### Task 6: T2a — grepping force-load → cheat-sheet

**Files:**

- Modify: `.claude/hooks/require-grepping-skill.sh`

- [ ] **Step 1: Replace the heredoc.** Replace everything from `cat <<'EOF'` through `EOF` in `require-grepping-skill.sh` with:

```bash
cat <<'EOF'
## Search-tool ladder (grepping cheat-sheet)

Repo search ladder (full skill: `dev-flow:grepping` — load on demand; the
dev-flow plugin's nudge-rg-failure hook will point you at it after any rg
failure):

- **Go symbol / "where is X defined, how does Y work"** → `mcp__probe__search_code`
  first (whole AST blocks; beats grep→Read).
- **Raw text** → `rg`. NEVER bare `grep`/`egrep`/`fgrep` (PreToolUse hook nudges).
- **Structural code shapes / codemods** → `ast-grep` (`sg -l go`); NOT for
  pkg-qualified call patterns (misparses — use `rg`).

rg silent-failure traps (these produce WRONG results, not errors):
- `rg 'A\|B'` — `\|` matches a LITERAL pipe; alternation is bare `|`.
- `rg -rn 'pat'` — rg's `-r` is --replace and EATS `n` as replacement text;
  rg is already recursive: use `rg -n 'pat'`.

Judging command success: decide pass/fail by EXIT CODE, never by grepping
stdout/stderr for success/error strings (fixtures echo those; May 2026
pr-prep incident). Brief sub-agents on this ladder — they default to bare
grep / full-file reads without it.
EOF
```

Also update the file's own header comment (lines 5–14): it now *injects a cheat-sheet* instead of requiring the full skill; note the on-demand reload path via the plugin's `nudge-rg-failure` hook.

- [ ] **Step 2: Verify the verbatim-retain list (spec §4.1 T2a).** All four items MUST appear in the new output:

```bash
out=$(.claude/hooks/require-grepping-skill.sh </dev/null)
for pat in 'NEVER bare' 'probe' '\\\|' '\-rn' 'EXIT CODE'; do
  printf '%s' "$out" | grep -qE -- "$pat" || echo "MISSING: $pat"
done
```

Expected: no `MISSING:` lines.

- [ ] **Step 3: Commit**

```bash
task fmt
jj --no-pager commit -m "feat(hooks): T2a — grepping force-load becomes search-ladder cheat-sheet (holomush-drf7b §4.1)"
```

---

### Task 7: T2b — split `testing.md`

**Files:**

- Modify: `.claude/rules/testing.md`
- Create: `.claude/rules/references/testing-detail.md`

- [ ] **Step 1: Snapshot, create the references dir, move content.** FIRST: `cp .claude/rules/testing.md /tmp/testing-before.md` (the Step 4 MUST-diff is meaningless without it). Then `mkdir -p .claude/rules/references`. Move these sections VERBATIM (cut from `testing.md`, paste into `references/testing-detail.md` under a `# Testing — deep reference` H1 with the same section headings; do not rewrite prose):
  - "Table-Driven Tests" (the full Go example block)
  - "Assertions" (the testify example block)
  - "Mockery" (config note + example)
  - "Test Engine Helpers (ABAC)" (keep a 1-line index in the core: `Test engines: policytest.GrantEngine / AllowAll / DenyAll / ErrorEngine — examples in the deep reference.`)
  - "Plugin Tests (`internal/plugin`)" (whole section)
  - Under "Integration Tests (Ginkgo/Gomega BDD)": the "Structure" example block and "Async operations" block (keep the requirement table + the run command in the core)
  - Under "Test Tiers": the "Diagnosing CI-only integration failures" paragraph and the whole-system-tier prose paragraph (keep the tier table, the quarantine MUSTs, and the three marker idioms in the core — those are load-bearing)
  - The ACE "Functions with subtests" example block AND the good/bad example table under "Functions without subtests" (keep the ACE requirement table in the core)
  - Under "Quarantine": the "Quarantine is for genuinely intermittent…" rationale paragraph and the "To un-quarantine…" paragraph. The fix-known-causes rule lives ONLY inside the cut paragraph and carries no RFC2119 keyword (the MUST-diff cannot catch its loss) — keep in the core this one-line restatement verbatim: `Quarantine is for flakiness with an open bead and no reproducible cause; if the root cause is known, fix it — do NOT quarantine it.` Also keep the three marker idioms and the bijection/`quarantine:audit` requirements in the core.
  - The `eventbustest` explanation paragraph under "Test Tiers" (keep the one-line MUST: production code MUST NOT import `eventbustest`/`coretest` — depguard-enforced)
- [ ] **Step 2: Add the pointer + keep every MUST.** At the end of the slimmed `testing.md` core add: `Deep reference: .claude/rules/references/testing-detail.md (read on demand).` The core MUST retain unchanged: the `paths:` frontmatter, Coverage table, integration-refactor MUST, test-file layout, ACE naming MUST table, Test Quality table, Invariant-Bindings section, tier table + quarantine requirements + marker idioms, depguard notes, Ginkgo requirement table.
- [ ] **Step 3: Fix the stale link while here** — `site/docs/contributing/quarantine.md` → `site/src/content/docs/contributing/how-to/quarantine.md` (verify with `ls`).
- [ ] **Step 4: Verify size + MUST preservation**

```bash
wc -c .claude/rules/testing.md           # target ≤ 5120; acceptance ≤ 6144
diff <(rg -o 'MUST( NOT)?' /tmp/testing-before.md | sort | uniq -c) \
     <(cat .claude/rules/testing.md .claude/rules/references/testing-detail.md | rg -o 'MUST( NOT)?' | sort | uniq -c)
```

The MUST-diff must be empty — no MUST lost across the two files. Size gate: the 5 KB figure is a SHOULD-level target (wagqb §3.4); if the core is still above **6144 bytes** after the full cut-list, STOP — do NOT relocate MUST-bearing content to hit a number. Record the achieved size + one-line justification: `bd note holomush-drf7b "T2b testing.md core: <N> bytes (target 5120, SHOULD-level) — remaining content is MUST-bearing"`.

- [ ] **Step 5: Commit**

```bash
task fmt
jj --no-pager commit -m "docs(rules): T2b — split testing.md into ≤5KB core + on-demand deep reference (holomush-drf7b §4.1)"
```

---

### Task 8: T2b — split `invariants.md`

**Files:**

- Modify: `.claude/rules/invariants.md`
- Create: `.claude/rules/references/invariants-detail.md`

- [ ] **Step 1: Snapshot, then move content.** `cp .claude/rules/invariants.md /tmp/invariants-before.md`. Move VERBATIM into `references/invariants-detail.md` (H1 `# Invariant registry — deep reference`):
  - The "In scope vs. out of scope" two-column table + the borderline-test-infra paragraph (keep in the core the one-sentence rule of thumb: *"if violating it is a regression in a guarantee rather than a missing feature, it is an invariant"* and the scope list)
  - The narrative paragraphs inside "Known escape hatches" (keep in the core the 2-line MUST: the orphan check walks only `docs/superpowers/specs/`; invariants introduced in `docs/specs/` or code MUST be registered by hand)
  - The historical rationale sentences in the design-time table (e.g. the epic-holomush-hz0v4 renumbering story — keep each MUST row, cut the war-story prose into the reference)
- [ ] **Step 2: Pointer + verify.** Add `Deep reference: .claude/rules/references/invariants-detail.md (read on demand).` at the end of the core. The core keeps: the artifact table, both requirement tables (design-time + binding ratchet, all rows), the 5-step binding workflow, the `paths:` frontmatter.

```bash
wc -c .claude/rules/invariants.md        # target ≤ 5120; acceptance ≤ 6144
diff <(rg -o 'MUST( NOT)?' /tmp/invariants-before.md | sort | uniq -c) \
     <(cat .claude/rules/invariants.md .claude/rules/references/invariants-detail.md | rg -o 'MUST( NOT)?' | sort | uniq -c)
```

Expected: empty MUST-diff. Same size-gate rule as Task 7: 5120 is the SHOULD-level target; above 6144 after the full cut-list → STOP, keep the MUSTs, `bd note` the achieved size + justification.

- [ ] **Step 3: Commit**

```bash
task fmt
jj --no-pager commit -m "docs(rules): T2b — split invariants.md into ≤5KB core + on-demand deep reference (holomush-drf7b §4.1)"
```

---

### Task 9: §4.3 — reviewer/aux description trims

**Files:**

- Modify: `.claude/agents/code-reviewer.md`, `.claude/agents/crypto-reviewer.md`, `.claude/agents/bead-auditor.md` (frontmatter `description: |` blocks only; bodies untouched)

- [ ] **Step 1: `code-reviewer.md`** — replace the description block content with:

```yaml
description: |
  MUST run before any of: `jj git push`, `git push`, `gh pr create`, `bd close`,
  or before responding to user text containing "push", "open a PR", "create a
  PR", "merge", "ship", "land", "ready to push/merge/ship", "close the bead",
  "mark done", "mark complete", "wrap up", "finalize". Adversarial independent
  reviewer — not the implementer's ally; findings grounded at `path:line`.
  Read-only. Skipping requires explicit user override (e.g. "skip review",
  "no review needed").
```

- [ ] **Step 2: `crypto-reviewer.md`** — replace with:

```yaml
description: |
  MUST run BEFORE `code-reviewer` for any change touching:
  `internal/eventbus/crypto/`, `internal/eventbus/codec/`,
  `internal/eventbus/history/dispatcher.go`,
  `internal/eventbus/history/cold_postgres.go`,
  `internal/plugin/event_emitter.go::Emit`,
  `internal/eventbus/audit/projection.go`, plugin manifest `crypto.emits`
  declarations, or migrations on `crypto_keys`/`events_audit`. Also fires on
  user text containing "ship the crypto", "crypto is ready", "rekey",
  "AuthGuard", "DEK", "AAD", or any phrasing that suggests pushing
  crypto-domain code without a domain-specialist review pass first.
  Adversarial reviewer for the event-payload-crypto invariants (master spec:
  docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md); findings
  at `path:line`; verdict READY / NOT READY. Read-only. Skipping requires
  explicit user override (e.g. "skip crypto review", "no crypto review
  needed").
```

- [ ] **Step 3: `bead-auditor.md`** — replace with:

```yaml
description: |
  Read-only investigator that audits open `bd` issues for closure candidates,
  grounded in current code. Use when asked to "audit beads", "clean up beads",
  "find stale beads", "review open beads", or to triage an issue cluster.
  Produces CLOSE/KEEP verdicts with evidence at `path:line`; does NOT execute
  closures (the orchestrator/user runs `bd close`). Can be dispatched in
  parallel, partitioned by epic.
```

- [ ] **Step 4: Verify no trigger lost** — every quoted trigger phrase and path from the OLD descriptions must appear in the new ones (the trims cut connective prose only):

```bash
jj --no-pager diff --git .claude/agents/code-reviewer.md .claude/agents/crypto-reviewer.md .claude/agents/bead-auditor.md
```

Read the diff: confirm removed lines contain no path, no quoted trigger phrase, and no MUST clause absent from the added lines.

- [ ] **Step 5: Commit**

```bash
task fmt
jj --no-pager commit -m "docs(agents): trim reviewer/auditor descriptions ~50%, all triggers retained (holomush-drf7b §4.3)"
```

---

### Task 10: T3a — CLAUDE.md trim (≤ 24 KB) + delegation rows + link fixes — HUMAN-REVIEWED

**Files:**

- Modify: `CLAUDE.md`
- Modify (receive relocated prose): `site/src/content/docs/contributing/how-to/pr-prep.md`, `how-to/sessions.md`, `how-to/pr-guide.md`, `how-to/integration-tests.md`, `site/src/content/docs/contributing/explanation/` (architecture overview), `docs/roadmap.md`

Rule for every cut: the prose moves VERBATIM to the named target (merged under a fitting heading); every MUST it contained stays in CLAUDE.md as a ≤ 1-line index entry linking the target. Work through the cut-list in order, re-measuring after each; stop relocating when `wc -c CLAUDE.md` ≤ 24576.

- [ ] **Step 1: Snapshot + baseline**

```bash
cp CLAUDE.md /tmp/claude-before.md && wc -c CLAUDE.md   # expect ~36084
```

- [ ] **Step 2: Add the delegation rows (§3.4)** to the Commands-section requirement table:

```markdown
| **MUST** delegate verbose task runs | Dispatch `local-check` for `task test\|lint\|build\|test:int\|test:cover` (and `local-pr-prep` for pr-prep iteration) instead of inline Bash — hook-enforced; `# offload-exempt` overrides when raw output is genuinely needed |
| **MUST** run final gate inline      | A `local-check` PASS satisfies "run `task test` before claiming complete"; the FINAL `task pr-prep` before a push still runs inline in the parent |
```

- [ ] **Step 3: Execute the cut-list (in order):**
  1. "Reading the pr-prep result" paragraph → `how-to/pr-prep.md` (index line: `**Reading the result:** exit code first — go-task collapses failures to 201; contention vs failure by BEHAVIOR, never the lock string; authoritative verdict in the \`▸ pr-prep result:\` file. Full guide: [pr-prep how-to].`)
  2. Commands-section fast/full-lane prose paragraphs → `how-to/pr-prep.md` (keep the lane one-liners + `HOLOMUSH_PR_PREP_FORCE_FULL` mention as index entries)
  3. "Session isolation" explanation prose + `gh`-in-workspace note → `how-to/sessions.md` (keep the 3 MUST table rows + a 1-line `gh -R holomush/holomush` index entry)
  4. "Session-store testing" subsection → `how-to/integration-tests.md` (index: `Session-store tests need Docker even under \`task test\` — MUST use \`sessiontest.NewStore(t)\`; the deliberate SharedPostgres exception. Details: [integration-tests how-to].`)
  5. "Integration test harness" subsection prose + when/when-not table → `how-to/integration-tests.md` (keep the 2-line summary + DenyAllEngine pointer)
  6. "Pre-Push Review Gates" intro/footer prose → `how-to/pr-guide.md` (keep BOTH tables — they are the MUSTs)
  7. "Strategic Themes" query snippet + explanation prose → `docs/roadmap.md` (keep the requirement table)
  8. "Core Systems" (World Model / Plugin System detail / Access Control / Command Authorization) + "Patterns" sections → merge under fitting headings in the EXISTING `site/src/content/docs/contributing/explanation/architecture.md` (do not create a new file; skip any paragraph the target already covers — check before pasting); keep in CLAUDE.md a 5-line index of the MUST-bearing items (plugin-runtime-symmetry pointer, two-layer command authz one-liner, `http.Flusher`/`Unwrap()` middleware MUST, event-sourcing one-liner)
- [ ] **Step 4: Fix ALL stale contributing links** — every `site/src/content/docs/contributing/<name>.md` reference in CLAUDE.md whose file now lives under `how-to/` gets the segment added (verify each target with `ls`):

```bash
rg -n 'site/src/content/docs/contributing/[a-z-]+\.md' CLAUDE.md
```

- [ ] **Step 5: Gates**

```bash
wc -c CLAUDE.md          # MUST be ≤ 24576
diff <(rg -o 'MUST( NOT)?' /tmp/claude-before.md | sort | uniq -c) <(rg -o 'MUST( NOT)?' CLAUDE.md | sort | uniq -c)
```

The MUST-count diff will NOT be empty (prose MUSTs became index entries) — review it line by line: every removed MUST must be re-findable either as a CLAUDE.md index entry or verbatim in a relocation target (`rg` the exact phrase in the target file). Also: `task lint:docs-symmetry` (AGENTS.md symlink) must stay green; `ls -la AGENTS.md` unchanged.

- [ ] **Step 6: HUMAN REVIEW GATE** — present `jj --no-pager diff --git` to the user and get explicit approval before committing (spec §4.2: the T3a diff MUST be human-reviewed). Do NOT commit without it.

- [ ] **Step 7: Commit (after approval)**

```bash
task fmt
jj --no-pager commit -m "docs: T3a — CLAUDE.md 36KB→≤24KB MUST-preserving trim + offload delegation rows + how-to link fixes (holomush-drf7b §4.2, §3.4)"
```

---

### Task 11: Verification wrap-up + documentary notes

**Files:** none created (measurements + notes + gates)

- [ ] **Step 1: Guardrail-preservation checklist (spec §7).** For each file changed in Tasks 5–10, diff against `main` and confirm every removed `MUST`/`MUST NOT`/`NEVER` line is reachable at its new home (index entry, reference file, or site doc). Record: `bd note holomush-drf7b "guardrail checklist: <N> relocated MUSTs verified reachable; 0 lost"`.
- [ ] **Step 2: Token measurements (char/4 proxy).** Record before/after on the bead:

```bash
wc -c CLAUDE.md .claude/rules/testing.md .claude/rules/invariants.md .claude/rules/subagent-briefing.md
b=$(git show main:CLAUDE.md | wc -c); a=$(wc -c < CLAUDE.md); echo "CLAUDE.md delta: $((b-a)) bytes (~$(( (b-a)/4 )) tok)"
```

`bd note holomush-drf7b "token deltas: CLAUDE.md -<N>B, testing.md core -<N>B, invariants.md core -<N>B, grepping injection -<N>B, agent descriptions -<N>w"`

- [ ] **Step 3: §4.4 documentary note (operator-side, no repo change):** `bd note holomush-drf7b "OPERATOR NOTE (spec §4.4): per-session agent listing is dominated by marketplace plugin agents (~40+ descriptions); disabling unused plugins for this project in ~/.claude config saves more per session than all repo trims combined. Operator's call — their config spans other projects."`
- [ ] **Step 4: Full gate + push prep.** Run the final gate INLINE (the pr-prep nudge is advisory; N4 requires the parent run):

```bash
task pr-prep
```

Expected: exit 0, `✓ All PR checks passed.` — the hook does not deny pr-prep (nudge only). If `fmt`/`license` mutated files, commit them. Then follow `.claude/rules/landing-the-plane.md`: `jj git fetch`, the `-s` rebase recipe, `jj bookmark set <branch> -r @-`, `jj git push --branch <branch>`, include any pending `.beads/interactions.jsonl` change.

- [ ] **Step 5: Commit any residue + done**

```bash
jj --no-pager st   # expect clean
```

---

## Verification summary (maps to spec §7)

| Spec §7 item | Where |
| --- | --- |
| `agent_id` + schema probe | Task 2 (input fields), Task 3 Step 7 (live deny schema) |
| Hook test matrix | Task 3 Steps 1–6 (harness + bats wiring, both `OFFLOAD_ENFORCE` modes) |
| Offload behavior check (fresh session) | Task 1 Step 4 note + Task 3 Step 7; full fresh-session dispatch check happens post-merge |
| Context tax before/after | Task 11 Step 2 |
| Guardrail preservation | Task 7/8 MUST-diffs, Task 10 Step 5, Task 11 Step 1 |
| Existing gates | Task 3 Step 6 (`task test:bats`), Task 11 Step 4 (`task pr-prep`) |

<!-- adr-capture: sha256=ca99f8250a6f560f; session=cli; ts=2026-07-06T22:20:25Z; adrs= -->

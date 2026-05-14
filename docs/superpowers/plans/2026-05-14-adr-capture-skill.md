<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# ADR Capture Skill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the four-artifact ADR-capture system (skill + hook + agent + Python migration) and run the 17-ADR migration to the bd-id-canonical filename convention, in one PR.

**Architecture:** A `PostToolUse` hook nudges Claude when a watched spec/plan path lacks a current capture marker. The user (or Claude) invokes `/capture-adrs <path>` to dispatch the read-only `adr-extractor` agent, review candidates one at a time, file `bd` decision records, and write ADR files under `docs/adr/`. A one-shot Python script converts the existing 17 ADRs to the new convention; a durable `adr-doctor.sh` enforces directory invariants on `task lint`.

**Tech Stack:** Bash 3.2+ (POSIX shell + `jq` for the hook and doctor), Python 3 stdlib (migration script), Claude Code skill/agent/hook markdown, `bd` CLI (decision-type issue tracking with validation), `jj` (rename preserves history).

**Spec:** [`docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md`](../specs/2026-05-13-adr-capture-skill-design.md). All invariant references (INV-A<n>) point at the spec.

**Working directory:** `/Volumes/Code/github.com/holomush/.worktrees/adr-capture-b2qy` (isolated jj workspace, anchored on `main@origin`).

---

## Task ordering rationale

| Phase | Tasks | Why this order |
|-------|-------|----------------|
| 1. Hook | 1–5 | Hook tests are the easiest to write first; shell + `jq` only; no bd state changes. |
| 2. Agent + skill | 6–8 | Static markdown; depend on nothing besides being saved. |
| 3. Migration script | 9–13 | Builds on rendered format; needs bd `--validate` working (it's been verified empirically). |
| 4. Doctor script | 14–16 | Needs migrated files to check; runs after migration produces them. |
| 5. Run + dogfood + pr-prep | 17–20 | End-to-end verification using the artifacts the prior phases shipped. |

---

## Task 1: Hook test harness scaffold + non-spec path bail

**Files:**

- Create: `.claude/hooks/nudge-adr-capture.test.sh`
- Create: `.claude/hooks/nudge-adr-capture.sh` (minimal stub)

- [ ] **Step 1: Write the failing test harness**

Create `.claude/hooks/nudge-adr-capture.test.sh` with this content:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Test harness for nudge-adr-capture.sh hook.
# Feeds synthetic PostToolUse JSON on stdin; asserts exit code + stdout.

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK="$SCRIPT_DIR/nudge-adr-capture.sh"

pass=0
fail=0
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

# expect_case <case-name> <stdin-json> <expected-exit> <expected-stdout-pattern-or-empty>
expect_case() {
  local name="$1" input="$2" want_exit="$3" want_stdout_pat="$4"
  local got_stdout got_exit
  got_stdout="$(printf '%s' "$input" | "$HOOK" 2>/dev/null)" && got_exit=0 || got_exit=$?
  if [ "$got_exit" -ne "$want_exit" ]; then
    echo "FAIL $name: exit $got_exit, want $want_exit" >&2
    fail=$((fail+1))
    return
  fi
  if [ -z "$want_stdout_pat" ]; then
    if [ -n "$got_stdout" ]; then
      echo "FAIL $name: stdout non-empty: $got_stdout" >&2
      fail=$((fail+1))
      return
    fi
  else
    if ! printf '%s' "$got_stdout" | grep -qE "$want_stdout_pat"; then
      echo "FAIL $name: stdout '$got_stdout' did not match /$want_stdout_pat/" >&2
      fail=$((fail+1))
      return
    fi
  fi
  pass=$((pass+1))
}

# --- Case 1: non-spec path (internal code edit) → silent ---
expect_case "non-spec-path" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/internal/foo.go"}}' \
  0 ""

echo "passed=$pass failed=$fail"
[ "$fail" -eq 0 ]
```

- [ ] **Step 2: Make the test executable**

Run:

```bash
chmod +x .claude/hooks/nudge-adr-capture.test.sh
```

- [ ] **Step 3: Run the test (must fail because hook doesn't exist yet)**

Run:

```bash
.claude/hooks/nudge-adr-capture.test.sh
```

Expected: error like `"$HOOK": No such file or directory`, non-zero exit.

- [ ] **Step 4: Write the minimal hook stub**

Create `.claude/hooks/nudge-adr-capture.sh` with this content:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PostToolUse hook: nudge Claude when a spec/plan is edited without a
# current ADR-capture marker. See
# docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md §2.

set -uo pipefail

# Read PostToolUse JSON from stdin; bail silently on parse failure.
input="$(cat)"
tool_name="$(printf '%s' "$input" | jq -r '.tool_name // empty' 2>/dev/null)"
file_path="$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty' 2>/dev/null)"

# Bail unless an Edit/Write touches an absolute file_path.
case "$tool_name" in
  Edit|Write) ;;
  *) exit 0 ;;
esac
[ -n "$file_path" ] || exit 0

# Watched-path regex (single source of truth per spec §Watched-path pattern).
if ! [[ "$file_path" =~ ^(.*/)?docs/(superpowers/)?(specs|plans)/.+\.md$ ]]; then
  exit 0
fi

# Path is in scope. Subsequent steps will check the marker and emit
# additionalContext JSON when stale or missing. Until then: stub.
exit 0
```

- [ ] **Step 5: Make the hook executable**

Run:

```bash
chmod +x .claude/hooks/nudge-adr-capture.sh
```

- [ ] **Step 6: Run the test (must pass — non-spec path bail)**

Run:

```bash
.claude/hooks/nudge-adr-capture.test.sh
```

Expected: `passed=1 failed=0`, exit 0.

- [ ] **Step 7: Run shellcheck on both scripts**

Run:

```bash
shellcheck .claude/hooks/nudge-adr-capture.sh .claude/hooks/nudge-adr-capture.test.sh
```

Expected: no output, exit 0.

- [ ] **Step 8: Commit**

```bash
jj describe -m "feat(hooks): scaffold nudge-adr-capture hook + test harness

Phase 1 task 1 of holomush-b2qy: empty PostToolUse hook that recognizes
watched spec/plan paths and bails on non-spec edits, plus shell test
harness that asserts the bail behavior.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 1"
```

(`jj` automatically snapshots the working copy; no `jj add` needed.)

---

## Task 2: Hook watched-path regex coverage

Goal: lock down the regex behavior for all four families, nested subdirs,
and out-of-scope paths.

**Files:**

- Modify: `.claude/hooks/nudge-adr-capture.test.sh` (add cases)

- [ ] **Step 1: Add 7 path-routing test cases**

Edit `.claude/hooks/nudge-adr-capture.test.sh`. Find the comment
`# --- Case 1: non-spec path (internal code edit) → silent ---`. Just
above the `echo "passed=$pass failed=$fail"` line, insert these cases:

```bash
# --- Case 2: docs/specs flat → reaches marker logic (currently still silent stub) ---
expect_case "docs-specs-flat" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/docs/specs/foo.md"}}' \
  0 ""

# --- Case 3: docs/plans flat ---
expect_case "docs-plans-flat" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/docs/plans/bar.md"}}' \
  0 ""

# --- Case 4: docs/superpowers/specs nested ---
expect_case "docs-superpowers-specs-nested" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/docs/superpowers/specs/2026/baz.md"}}' \
  0 ""

# --- Case 5: docs/superpowers/plans flat ---
expect_case "docs-superpowers-plans-flat" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/docs/superpowers/plans/qux.md"}}' \
  0 ""

# --- Case 6: docs/adr/ — out of scope ---
expect_case "docs-adr-not-watched" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/docs/adr/0001-foo.md"}}' \
  0 ""

# --- Case 7: workspace path (.worktrees/foo/docs/specs/...) — should match ---
expect_case "worktree-path-matches" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/repo/.worktrees/foo/docs/specs/bar.md"}}' \
  0 ""

# --- Case 8: non-Edit/Write tool → bail ---
expect_case "non-edit-tool-bail" \
  '{"tool_name":"Read","tool_input":{"file_path":"/repo/docs/specs/foo.md"}}' \
  0 ""
```

- [ ] **Step 2: Run the test**

Run:

```bash
.claude/hooks/nudge-adr-capture.test.sh
```

Expected: `passed=8 failed=0`, exit 0. (Hook still emits no stdout for all
cases, which is what the current stub does; we'll exercise marker logic
next.)

- [ ] **Step 3: Run shellcheck**

```bash
shellcheck .claude/hooks/nudge-adr-capture.sh .claude/hooks/nudge-adr-capture.test.sh
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
jj describe -m "test(hooks): cover all watched-path families + out-of-scope cases

8 path-routing fixtures: docs/specs, docs/plans, docs/superpowers/specs
(nested), docs/superpowers/plans, docs/adr/ (rejection), worktree path,
non-Edit tool. Hook still bails silently (no marker logic yet) — task 3
adds marker handling.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 2"
```

---

## Task 3: Marker convention — strip + SHA + kind disambiguation

Goal: implement the §"Marker convention" parse rule and the three-kind
disambiguation table (`sha` / `optout` / `malformed`).

**Files:**

- Modify: `.claude/hooks/nudge-adr-capture.sh` (add marker handling)
- Modify: `.claude/hooks/nudge-adr-capture.test.sh` (add 5 marker cases)

- [ ] **Step 1: Add 5 failing marker test fixtures**

Edit `.claude/hooks/nudge-adr-capture.test.sh`. Just above the
`echo "passed=$pass failed=$fail"` line, insert these cases (which need
the test harness's `tmpdir` helper to materialize files):

```bash
# Helper: write content + marker to a file under tmpdir; return path.
make_spec() {
  local name="$1" content="$2"
  local p="$tmpdir/docs/specs/$name.md"
  mkdir -p "$(dirname "$p")"
  printf '%s' "$content" > "$p"
  printf '%s' "$p"
}

# Compute SHA the way the hook will (after stripping any trailing marker line).
spec_sha() {
  local file="$1"
  # Drop trailing marker line if last line matches; sha256sum the rest.
  awk 'BEGIN{prev=""} {if(NR>1)print prev; prev=$0} END{
    if(prev ~ /^<!-- adr-capture: .*-->$/) {}
    else { if(NR>0) print prev }
  }' "$file" | sha256sum | cut -c1-16
}

# --- Case 9: spec with no marker → nudge (currently still stub; xfail until hook impl) ---
no_marker_path="$(make_spec "no-marker" 'spec body without marker\n')"
expect_case "no-marker-nudges" \
  "$(printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' "$no_marker_path")" \
  0 'hookSpecificOutput.*PostToolUse.*no marker'

# --- Case 10: spec with fresh (matching) marker → silent ---
fresh_body="fresh content\n"
fresh_path="$(make_spec "fresh-marker" "$fresh_body")"
fresh_sha="$(spec_sha "$fresh_path")"
printf '\n<!-- adr-capture: sha256=%s; session=test; ts=2026-05-14T00:00:00Z; adrs= -->\n' "$fresh_sha" >> "$fresh_path"
expect_case "fresh-marker-silent" \
  "$(printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' "$fresh_path")" \
  0 ""

# --- Case 11: spec with stale (mismatched) marker → nudge ---
stale_path="$(make_spec "stale-marker" "original content")"
printf '\n<!-- adr-capture: sha256=deadbeefdeadbeef; session=test; ts=2026-05-14T00:00:00Z; adrs= -->\n' >> "$stale_path"
expect_case "stale-marker-nudges" \
  "$(printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' "$stale_path")" \
  0 'hookSpecificOutput.*PostToolUse.*content changed'

# --- Case 12: opt-out marker → silent (no nudge) ---
optout_path="$(make_spec "optout" "doc body")"
printf '\n<!-- adr-capture: optout=true; reason="external doc" -->\n' >> "$optout_path"
expect_case "optout-silent" \
  "$(printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' "$optout_path")" \
  0 ""

# --- Case 13: malformed marker (prefix but no sha256= and no optout=) → nudge ---
mal_path="$(make_spec "malformed" "doc body")"
printf '\n<!-- adr-capture: foo=bar -->\n' >> "$mal_path"
expect_case "malformed-nudges" \
  "$(printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' "$mal_path")" \
  0 'hookSpecificOutput.*PostToolUse'
```

- [ ] **Step 2: Run the test (must fail — hook doesn't implement markers yet)**

Run:

```bash
.claude/hooks/nudge-adr-capture.test.sh
```

Expected: `passed=9 failed=4` (cases 9, 11, 13 expect stdout pattern but
hook emits nothing; case 10 passes because fresh marker → silent and stub
is already silent; case 12 passes for the same reason).

- [ ] **Step 3: Implement marker handling in the hook**

Edit `.claude/hooks/nudge-adr-capture.sh`. Replace the trailing
`# Path is in scope...` comment block (the stub exit) with:

```bash
# Path is in scope. Inspect the file's last line for an adr-capture marker.
[ -r "$file_path" ] || exit 0

# Read the file's final line. If it matches the marker prefix, treat it
# as the marker and the rest of the file as "stripped content".
last_line="$(tail -n 1 "$file_path")"
case "$last_line" in
  '<!-- adr-capture: '*' -->')
    marker_line="$last_line"
    has_marker=1
    ;;
  *)
    marker_line=""
    has_marker=0
    ;;
esac

# Classify the marker kind: optout | sha | malformed.
# Disambiguator: optout=true + reason="..." → optout; sha256=<16hex> → sha; else malformed.
marker_kind="none"
optout_reason=""
marker_sha=""
if [ -n "$marker_line" ]; then
  if printf '%s' "$marker_line" | grep -qE 'optout=true'; then
    if printf '%s' "$marker_line" | grep -qE 'reason="[^"]+"'; then
      marker_kind="optout"
      optout_reason="$(printf '%s' "$marker_line" | sed -nE 's/.*reason="([^"]+)".*/\1/p')"
    else
      marker_kind="malformed"
    fi
  elif printf '%s' "$marker_line" | grep -qE 'sha256=[0-9a-f]{16}([^0-9a-f]|$)'; then
    marker_kind="sha"
    marker_sha="$(printf '%s' "$marker_line" | sed -nE 's/.*sha256=([0-9a-f]{16}).*/\1/p')"
  else
    marker_kind="malformed"
  fi
fi

# Compute current SHA over stripped content (first 16 hex chars).
# CRITICAL: bash command substitution "$(...)" strips trailing newlines from
# the captured value. The skill stamps a SHA over content that ends with `\n`
# (per spec §Marker convention "Stamping rule" step 2). If we capture into a
# variable, the trailing `\n` is lost and the hook SHA disagrees with the
# skill SHA on every spec. Stream awk's output directly into sha256sum
# instead.
if [ "$has_marker" = "1" ]; then
  current_sha="$(awk -v n="$(wc -l <"$file_path")" 'NR<n' "$file_path" \
    | sha256sum | cut -c1-16)"
else
  current_sha="$(sha256sum <"$file_path" | cut -c1-16)"
fi

# Decide outcome.
case "$marker_kind" in
  optout)
    exit 0  # opt-out wins; never nudge
    ;;
  sha)
    if [ "$marker_sha" = "$current_sha" ]; then
      exit 0  # fresh marker; silent
    fi
    reason="content changed since capture"
    ;;
  malformed)
    reason="malformed marker"
    ;;
  none)
    reason="no marker"
    ;;
esac

# Nudge path. Emit PostToolUse additionalContext JSON; bare stdout does
# NOT reach Claude for PostToolUse — only additionalContext does.
jq -nc \
  --arg path "$file_path" \
  --arg reason "$reason" \
  '{
    hookSpecificOutput: {
      hookEventName: "PostToolUse",
      additionalContext: ("adr-capture: " + $path + " was modified (" + $reason + "). Run /capture-adrs " + $path + " to extract any ADR-worthy decisions, or --dry-run to preview.")
    }
  }'
exit 0
```

- [ ] **Step 4: Run the test (all must pass)**

Run:

```bash
.claude/hooks/nudge-adr-capture.test.sh
```

Expected: `passed=13 failed=0`, exit 0.

- [ ] **Step 5: Verify JSON shape on the nudge path manually**

Run:

```bash
printf '{"tool_name":"Edit","tool_input":{"file_path":"/tmp/no-such-spec.md"}}' \
  | .claude/hooks/nudge-adr-capture.sh
```

Expected: silent (file doesn't exist; `[ -r "$file_path" ]` bails).

Now create a temp spec and re-test:

```bash
mkdir -p /tmp/adr-test/docs/specs && \
echo "test" > /tmp/adr-test/docs/specs/foo.md && \
printf '{"tool_name":"Edit","tool_input":{"file_path":"/tmp/adr-test/docs/specs/foo.md"}}' \
  | .claude/hooks/nudge-adr-capture.sh \
  | jq -e '.hookSpecificOutput.hookEventName == "PostToolUse"'
rm -rf /tmp/adr-test
```

Expected: `true` (jq returns true), exit 0.

- [ ] **Step 6: Run shellcheck**

Run:

```bash
shellcheck .claude/hooks/nudge-adr-capture.sh .claude/hooks/nudge-adr-capture.test.sh
```

Expected: clean (or only `SC2155` "declare and assign separately" which is acceptable in our style).

- [ ] **Step 7: Commit**

```bash
jj describe -m "feat(hooks): nudge-adr-capture marker handling + JSON output

Implements §Marker convention and §Watched-path pattern from the spec:
- Last-line marker detection (case match on '<!-- adr-capture: ...-->')
- Three-kind disambiguation: sha / optout / malformed
- Stripped-content SHA-256 (first 16 hex)
- PostToolUse additionalContext JSON (bare stdout doesn't reach Claude)

INV-A6, INV-A7, INV-A8, INV-A10 (hook side), INV-A16 covered by 13
shell-harness fixtures.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 3"
```

---

## Task 4: Hook bash-3.2 compatibility check + missing-file path

Goal: harden the hook against bash 3.2 and unreadable files.

**Files:**

- Modify: `.claude/hooks/nudge-adr-capture.test.sh` (add 2 cases)

- [ ] **Step 1: Add 2 failing-path test cases**

Edit `.claude/hooks/nudge-adr-capture.test.sh`. Just above the
`echo "passed=$pass failed=$fail"` line, add:

```bash
# --- Case 14: file_path missing entirely → silent ---
expect_case "missing-file-path" \
  '{"tool_name":"Edit","tool_input":{}}' \
  0 ""

# --- Case 15: file_path points at non-existent file → silent ---
expect_case "nonexistent-file" \
  '{"tool_name":"Edit","tool_input":{"file_path":"/tmp/this/does/not/exist/docs/specs/x.md"}}' \
  0 ""
```

- [ ] **Step 2: Run the test (cases 14-15 should already pass under the existing hook)**

Run:

```bash
.claude/hooks/nudge-adr-capture.test.sh
```

Expected: `passed=15 failed=0` (case 14 bails on empty `file_path`; case
15 bails on `[ -r "$file_path" ]`).

- [ ] **Step 3: Verify bash-3.2 compatibility**

Run:

```bash
/bin/bash --version | head -1
/bin/bash .claude/hooks/nudge-adr-capture.test.sh
```

Expected: bash version is 3.2.57(1)-release (or similar 3.2.x); test
passes 15/15.

- [ ] **Step 4: Commit**

```bash
jj describe -m "test(hooks): cover missing/unreadable file paths

Two more fixtures: missing file_path key, non-existent target file.
Hook bails silently on both. Verified compatible with /bin/bash 3.2
(macOS default).

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 4"
```

---

## Task 5: Wire hook into `.claude/settings.json`

**Files:**

- Modify: `.claude/settings.json` (append hook to PostToolUse Edit|Write block)

- [ ] **Step 1: Read current settings.json**

Run:

```bash
cat .claude/settings.json | jq '.hooks.PostToolUse[] | select(.matcher == "Edit|Write")'
```

Expected: an object with `hooks: [auto-format.sh, go-vet.sh]` and matcher
`Edit|Write`.

- [ ] **Step 2: Add the nudge hook to that block**

Use Edit to modify `.claude/settings.json`. Find:

```jsonc
{
  "hooks": [
    {
      "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/auto-format.sh",
      "type": "command"
    },
    {
      "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/go-vet.sh",
      "type": "command"
    }
  ],
  "matcher": "Edit|Write"
},
```

Replace with:

```jsonc
{
  "hooks": [
    {
      "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/auto-format.sh",
      "type": "command"
    },
    {
      "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/go-vet.sh",
      "type": "command"
    },
    {
      "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/nudge-adr-capture.sh",
      "type": "command"
    }
  ],
  "matcher": "Edit|Write"
},
```

- [ ] **Step 3: Validate JSON**

Run:

```bash
jq . .claude/settings.json > /dev/null
```

Expected: silent, exit 0 (valid JSON).

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(hooks): wire nudge-adr-capture into PostToolUse Edit|Write

Hook now fires alongside auto-format.sh and go-vet.sh.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 5"
```

---

## Task 6: Create `adr-extractor` agent

**Files:**

- Create: `.claude/agents/adr-extractor.md`

- [ ] **Step 1: Write the agent definition**

Create `.claude/agents/adr-extractor.md` with this content:

````markdown
---
name: adr-extractor
description: |
  Read-only agent that scans a finalized spec/plan and (optionally) its
  brainstorming session transcript to identify ADR-worthy decisions.
  Returns strict JSON. Used by /capture-adrs but reusable for batch
  retrospective extraction and audit workflows.
model: sonnet
tools:
  - Read
  - Grep
  - Glob
  - mcp__probe__search_code
  - mcp__probe__extract_code
  - Bash
skills:
  - jj:jujutsu
---

# adr-extractor

You scan a finalized spec or plan (and optionally the brainstorming
session transcript that produced it) to identify Architecture Decision
Record (ADR) candidates.

## Worthiness criteria

A candidate is ADR-worthy iff ALL of the following are true:

1. **Architectural** — not implementation detail (e.g., not "use
   kebab-case for slugs").
2. **Has rejected alternatives** with a real trade-off — the presence
   of a credible alternative that was considered AND rejected is the
   signal.
3. **Load-bearing** for future decisions or contributors — six months
   from now someone asking "why is X this way" should be able to find
   the answer here.
4. **Not already captured** in `docs/adr/` — you MUST grep / probe the
   directory and run `bd list --type decision` before proposing a new
   candidate. If a related ADR exists, propose `supersedes` rather than
   "new."

Score each candidate 0–4 by how many criteria it passes. Score < 4 is
borderline — surface it anyway, but flag in your output.

## Transcript scan strategies (priority order)

1. **Windowed (default).** Locate the spec's `Write`/`Edit` tool calls
   in the transcript; read 100 turns before each (cap configurable via
   the caller's `TRANSCRIPT_WINDOW` parameter).
2. **Brainstorm marker.** If the transcript contains a `Skill:
   superpowers:brainstorming` invocation line, scan from that turn
   forward.
3. **Full fallback.** Grep the entire transcript for decision-shaped
   phrases (`reject`, `chose`, `alternative`, `trade-off`, `instead
   of`, `in favor of`, `settled on`, `landed on`) and read matching
   regions.

If no transcript is available, use spec-text-only mode and note this in
your `dropped` array under a `transcript-unavailable` reason.

## Output contract

Return STRICT JSON. No prose preamble. No commentary outside the JSON.
On internal failure, return `{"error": "<short reason>"}`.

The schema:

```jsonc
{
  "candidates": [
    {
      "title": "string",                       // imperative; capped 60 chars
      "context": "2–4 sentences",
      "options_considered": [
        {
          "name": "string",
          "strengths": "string",
          "weaknesses": "string",
          "chosen": true | false
        }
      ],
      "decision": "1–2 sentences",
      "rationale": ["bullet", "bullet"],
      "consequences": {
        "positive": ["..."],
        "negative": ["..."],
        "neutral": ["..."]
      },
      "spec_section": "§3.5",
      "transcript_quotes": ["..."],
      "worthiness_score": 0..4,
      "supersedes": null | "<bd-id>"
    }
  ],
  "dropped": [
    { "region": "spec §4.2 or transcript-unavailable", "reason": "implementation detail (slug casing)" }
  ]
}
```

## Read-only contract

You MUST NOT write files. You MUST NOT modify state. The skill that
invokes you is responsible for any disk writes. Your tools list does
not include `Write`, `Edit`, or `NotebookEdit`; if you find yourself
needing one, return `{"error": "..."}` instead.

## Output cap

Total response length (all candidates + dropped) MUST fit within the
caller's `OUTPUT_LIMIT` parameter (default 800 words). If you cannot
fit everything, prioritize candidates by `worthiness_score` descending
and include a `"truncated": true` field at the top level.
````

- [ ] **Step 2: Validate frontmatter parses**

Run:

```bash
head -20 .claude/agents/adr-extractor.md | head -1
```

Expected: `---` (frontmatter start).

Run:

```bash
awk '/^---$/{c++; next} c==1' .claude/agents/adr-extractor.md \
  | head -20
```

Expected: shows YAML between the `---` markers (name, description,
model, tools, skills).

- [ ] **Step 3: Run task fmt to normalize whitespace**

Run:

```bash
task fmt
```

Expected: format runs; possibly minor whitespace normalization on the
new file.

- [ ] **Step 4: Verify no unrelated drift**

Run:

```bash
jj st
```

Expected: only `.claude/agents/adr-extractor.md` and possibly
`.claude/hooks/*` from prior tasks. If unrelated Go file drift appears:

```bash
jj restore --from @- cmd/holomush/admin_read_stream_e2e_test.go cmd/holomush/cmd_admin_read_stream.go cmd/holomush/cmd_admin_read_stream_test.go internal/admin/socket/read_stream_handler_test.go
```

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(agents): add read-only adr-extractor agent

Sonnet agent for ADR-candidate detection. Tools allowlist excludes
Write/Edit/NotebookEdit per INV-A15. System prompt encodes the
four-criterion worthiness test, transcript scan strategies, strict
JSON output contract.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 6"
```

---

## Task 7: Create `adr-extractor` fixture set

**Files:**

- Create: `.claude/agents/adr-extractor.testdata/01-clean-current-format.md`
- Create: `.claude/agents/adr-extractor.testdata/02-original-format.md`
- Create: `.claude/agents/adr-extractor.testdata/03-no-decisions.md`
- Create: `.claude/agents/adr-extractor.testdata/04-already-captured.md`
- Create: `.claude/agents/adr-extractor.testdata/05-transcript-only.md`
- Create: `.claude/agents/adr-extractor.testdata/README.md`

- [ ] **Step 1: Write fixture 1 — clean current-format spec**

Create `.claude/agents/adr-extractor.testdata/01-clean-current-format.md`:

```markdown
# Fixture: Clean current-format spec

## Decision

Use bd-decision IDs as ADR filename identifiers.

## Alternatives Considered

- **Sequential NNNN with auto-renumber:** simple but parallel branches
  collide on the next integer. — rejected
- **Reserve numbers via placeholder PRs:** complex; adds round-trip. — rejected
- **bd-decision ID as filename:** atomically allocated; no collision. — selected

## Rationale

Atomic allocation by bd eliminates the parallel-branch collision class
that the NNNN convention has.

Expected agent output: 1 candidate, worthiness_score=4.
```

- [ ] **Step 2: Write fixture 2 — original-format spec**

Create `.claude/agents/adr-extractor.testdata/02-original-format.md`:

```markdown
# Fixture: Original-format spec

## Context

Session tokens stored client-side carry forgery risk.

## Decision

Use opaque session tokens validated server-side, not signed JWTs.

## Rationale

JWT validation logic adds complexity; revocation is hard. Opaque tokens
trade a lookup per request for trivial revocation.

## Alternatives Considered

JWTs were considered and rejected for the revocation issue.

Expected agent output: 1 candidate, worthiness_score=4.
```

- [ ] **Step 3: Write fixture 3 — no decisions**

Create `.claude/agents/adr-extractor.testdata/03-no-decisions.md`:

```markdown
# Fixture: Spec with no ADR-worthy decisions

This document describes the directory structure of `internal/world/`
and the naming convention for entity files. It contains no
architectural decisions — just documentation of existing layout.

The naming convention: snake_case for file names, PascalCase for
exported Go types.

Expected agent output: zero candidates; dropped list explains the
content is documentation, not architecture.
```

- [ ] **Step 4: Write fixture 4 — already captured**

Create `.claude/agents/adr-extractor.testdata/04-already-captured.md`:

```markdown
# Fixture: Spec referencing an already-captured decision

## Background

This proposal extends the existing ABAC engine described in ADR 0009
(custom Go-native ABAC). We're adding eager attribute resolution per
ADR 0012.

## Decision

Use eager attribute resolution with per-request caching, as already
captured in ADR 0012.

Expected agent output: zero new candidates; dropped list cites the
existing ADRs.
```

- [ ] **Step 5: Write fixture 5 — transcript-only decision**

Create `.claude/agents/adr-extractor.testdata/05-transcript-only.md`:

```markdown
# Fixture: Spec where the rationale lives in transcript

## Decision

Use a Python script for the migration.

(No "Alternatives Considered" section; the rationale for Python over Go
lived in the brainstorming chat — not in the spec.)

Expected agent output: 1 candidate, worthiness_score < 4 (criterion 2
fails at spec-text only); flagged as borderline. With transcript
available, agent should pull the rejected-Go discussion and bump to 4.
```

- [ ] **Step 6: Write the README**

Create `.claude/agents/adr-extractor.testdata/README.md`:

```markdown
<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# adr-extractor fixture set

Manual regression set for the `adr-extractor` agent. Used when changing
the agent's system prompt to catch prompt drift.

## Running

```bash
task test:agents
```
```

```text

(Wired in Task 19 of the implementation plan.)

Each fixture's expected behavior is documented inline at the bottom of
the fixture file.

```text

- [ ] **Step 7: Run task fmt**

Run:

```bash
task fmt
```

Expected: completes; possibly minor whitespace normalization.

- [ ] **Step 8: Commit**

```bash
jj describe -m "test(agents): adr-extractor fixture set (5 cases + README)

Fixtures cover: clean current-format, original-format, no decisions,
already-captured, transcript-only. Used by 'task test:agents' (wired
in a later task) for prompt-drift detection.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 7"
```

---

## Task 8: Create `/capture-adrs` skill

**Files:**

- Create: `.claude/skills/capture-adrs/SKILL.md`

- [ ] **Step 1: Write the skill**

Create `.claude/skills/capture-adrs/SKILL.md`:

````markdown
---
name: capture-adrs
description: |
  Use when the user has finalized a spec or plan and wants to extract
  ADR-worthy decisions into both `docs/adr/<bd-id>-<slug>.md` files
  AND `bd create -t decision` records. Triggered by `/capture-adrs
  <path>` or by the nudge-adr-capture hook's reminder. NOT for general
  ADR audit — use the adr-extractor agent directly for that.
---

# /capture-adrs

Extract ADR-worthy decisions from a finalized spec or plan, get
per-candidate user approval, file `bd` decision records, and write
ADR files under `docs/adr/`. Stamp the spec with a content-hash marker
when done.

Full design: `docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md`.

## When to invoke

- User says `/capture-adrs <path>` (explicit path)
- User says `/capture-adrs` (interactive; prompt for the spec)
- Hook nudged with a `system-reminder` (PostToolUse) — pass the file
  path mentioned in the nudge
- User says `/capture-adrs <path> --dry-run` (no writes) or `--re-run`
  (force re-scan even on matching SHA)

## Tool availability check

You MUST run this skill in the user's main session context, where
`AskUserQuestion` is available. If `AskUserQuestion` is NOT in scope,
fail fast: print "capture-adrs requires AskUserQuestion (main session
context); aborting" and exit. (Per INV-A19.)

## Step-by-step

### 1. Resolve spec path

If the user provided a path, validate it against the regex:

```text
^(.*/)?docs/(superpowers/)?(specs|plans)/.+\.md$
```

Reject paths outside (including any under `docs/adr/`). If no path
given, list recent edits in the four watched roots via:

```bash
git log -n 20 --pretty=format: --name-only -- docs/specs/ docs/plans/ docs/superpowers/specs/ docs/superpowers/plans/ 2>/dev/null | sort -u | head -20
```

Use `AskUserQuestion` with one option per recent file to pick.

### 2. Idempotency check

Read the file. Strip the trailing marker line if present (last line
matching `^<!-- adr-capture: .*-->$`). Compute SHA-256 of the
remainder; take first 16 hex chars.

Decide in this order (first match wins):

1. **Opt-out:** marker has `optout=true` AND `reason="..."` → abort
   with message naming the reason. `--re-run` does NOT override
   opt-out. Exit.
2. **Fresh:** marker has `sha256=<hex>` matching current AND no
   `--re-run` → print "Already captured." + listed bd-ids from
   `adrs=...`. Exit.
3. **Proceed:** marker missing, malformed, or SHA mismatched.

### 3. Heuristic pre-scan

Walk the spec; collect regions matching any of:

- Header `^#+\s+(Options Considered|Alternatives Considered|Decision|Rationale|Trade-?offs|Why not)`
- Inline `(rejected because|chose|chosen|instead of|in favor of|decided against|ruled out|settled on|landed on)`
- `Alternative [A-Z]:` blocks

Output: list of `{start_line, end_line, header}` tuples.

### 4. Resolve transcript path

Look for `$CLAUDE_TRANSCRIPT_PATH` env var; if absent, scan
`~/.claude/projects/$(pwd encoded)/<session-uuid>/` for the JSONL
whose session ID matches `$CLAUDE_SESSION_ID`. If nothing resolves,
use the literal string `"none"`; the agent will run in spec-text-only
mode.

### 5. Dispatch the adr-extractor agent

Use the `Agent` tool with `subagent_type: adr-extractor`. Prompt:

```text
SPEC: <absolute-path>
SPEC_HEURISTIC_REGIONS: [{start_line, end_line, header}, ...]
TRANSCRIPT: <absolute-path-or-"none">
TRANSCRIPT_WINDOW: "100-turns-before-spec-writes"
EXISTING_ADRS_DIR: docs/adr/
OUTPUT_LIMIT: 800

Return JSON per the schema in your system prompt. Do not prepend prose.
```

Parse the JSON. On parse failure, retry once with a stricter prompt
("Your previous response was not parseable JSON. Return ONLY the JSON
object."). On second failure, fall back to surfacing the heuristic
regions as one-line candidates (warn the user).

### 6. Per-candidate review loop

For each candidate in `result.candidates`:

Present `AskUserQuestion` with:

- `question`: `"ADR candidate <i>/<n>: <title>"`
- `header`: `"ADR <i>/<n>"` (truncate to 12 chars)
- `options`:
  - **Accept** — "Write this ADR + file bd decision record"
  - **Skip** — "Drop this candidate (logged in report)"
  - **Edit** — "Refine fields before accepting"
  - **Show full context** — "Display spec excerpt + transcript quotes"

On **Edit**: ask which field (Title / Context / Decision / Rationale /
Alternatives / Consequences / supersedes), then free-text refinement,
then re-present.

On **Show full context**: print spec lines `start_line..end_line` and
`transcript_quotes`, then re-present.

Collect ALL accept/skip decisions BEFORE writing anything. (Per
INV-A1: skill MUST NOT write files until all triage is complete.)

### 7. Write phase

For each accepted candidate, in order:

1. Render the ADR markdown body following §"ADR format (unified)" in
   the spec — Context, Decision, Rationale, Alternatives Considered,
   Consequences, References — with the `**Date:**`, `**Status:**`,
   `**Deciders:**` header.
2. Run, piping the body via stdin (avoids shell-quoting fragility for
   titles containing apostrophes, double-quotes, or other characters):

   ```bash
   printf '%s' "$body" | bd create -t decision --validate --title "<title>" --stdin
   ```

   Capture the `holomush-XXXX` ID from stdout. On validate failure,
   abort this candidate (other candidates continue); report at end.
3. Compute slug: kebab-case of title, drop stop-words (a, an, the,
   for, of, to, in, on, with), cap 60 chars.
4. Prepend `**Decision:** <bd-id>` line below the `**Status:**` line
   in the body.
5. Write `docs/adr/<bd-id>-<slug>.md` with the full body.
6. If candidate has `supersedes: <existing-bd-id>`:

   ```bash
   bd dep add <new-bd-id> <existing-bd-id> --type supersedes
   bd close <existing-bd-id> --reason "Superseded by <new-bd-id>"
   ```

   Rewrite the superseded file's `**Status:**` to `Superseded by <new-bd-id>`.

### 8. Regenerate `docs/adr/README.md`

Walk `docs/adr/`. For each non-stub file (`<bd-id>-<slug>.md`), parse
`**Date:**`, title, `**Status:**`. Sort by date desc. Rewrite the
index table between `## Index` and the next `##` header. Preserve the
migration map between `<!-- BEGIN MIGRATION MAP -->` and `<!-- END
MIGRATION MAP -->` sentinels.

### 9. Stamp the marker

Normalize the spec to end with `\n` (append if absent). Recompute SHA-
256 of normalized content (first 16 hex). Append:

```text
<!-- adr-capture: sha256=<hex>; session=<short>; ts=<RFC3339>; adrs=<id1>,<id2>,... -->
```

(`<short>` = first 8 chars of `$CLAUDE_SESSION_ID` or `cli`; `<ts>` =
`date -u +%Y-%m-%dT%H:%M:%SZ`.)

For zero accepted candidates, `adrs=` is empty but the marker is still
written.

### 10. Final report

Print:

```text
Captured <N> ADRs:
  - <bd-id> <title> → docs/adr/<bd-id>-<slug>.md
  ...

Spec marker written. Run `task fmt` then commit when ready.
```

Skill MUST NOT commit. User does that.

## Failure modes

- bd write fails: roll back any file writes for that candidate;
  continue; report partial.
- Sub-agent JSON malformed twice: fall back to heuristic-only
  candidates with a warning.
- Spec modified mid-flow (e.g., user edits during review): abort
  before stamping; suggest re-running.

## Anti-patterns

- DO NOT commit. (INV-A2)
- DO NOT write files before all triage decisions are collected.
  (INV-A1)
- DO NOT overwrite opt-out markers. (INV-A10)
- DO NOT call `bd create` without `--validate`. (INV-A3 + INV-A4)
- DO NOT use a model floor below sonnet for the adr-extractor dispatch.
````

- [ ] **Step 2: Verify the skill has no committing language**

Run:

```bash
grep -E '(jj commit|jj describe|git commit|git add)' .claude/skills/capture-adrs/SKILL.md
```

Expected: only matches inside the "Anti-patterns" `DO NOT commit`
sentence, NOT as command instructions. (This is the predicate
`forbid_skill_commits` in adr-doctor.sh will assert; we're checking
it manually here.)

If matches appear as instructions (e.g., a step says "run `jj
commit`"), refactor to remove them.

- [ ] **Step 3: Run task fmt**

Run:

```bash
task fmt
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(skills): /capture-adrs orchestration skill

Skill body covers all 10 procedural steps from §1 of the spec: path
resolution, idempotency check (opt-out first, then SHA), heuristic
pre-scan, transcript resolution, adr-extractor dispatch, per-candidate
review loop (AskUserQuestion), write phase (bd create + file write +
supersession edges), README regeneration, marker stamping, final
report. INV-A1, A2, A3, A10 (skill side), A19 enforced.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 8"
```

---

## Task 9: Migration script — parser

**Files:**

- Create: `scripts/adr-migrate.py`

- [ ] **Step 1: Write the parser skeleton**

Create `scripts/adr-migrate.py`:

```python
#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
"""
One-shot migration: convert docs/adr/NNNN-<slug>.md to
docs/adr/<bd-id>-<slug>.md, file bd decision records, write stubs at
legacy paths, regenerate README.

Usage:
    python3 scripts/adr-migrate.py            # dry-run (default)
    python3 scripts/adr-migrate.py --apply    # actually mutate state

See docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md §4.
"""
import argparse
import re
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
ADR_DIR = REPO_ROOT / "docs" / "adr"
LEGACY_PATTERN = re.compile(r"^(\d{4})-([a-z0-9-]+)\.md$")


@dataclass
class LegacyADR:
    path: Path
    number: int
    slug: str
    title: str = ""
    date: str = ""
    status: str = "Accepted"
    superseded_by_number: int | None = None
    context: str = ""
    decision: str = ""
    rationale: str = ""
    alternatives: str = ""
    consequences: str = ""
    references: str = ""
    # Filled in after bd create:
    bd_id: str = ""

    @property
    def new_filename(self) -> str:
        assert self.bd_id, "bd_id not yet allocated"
        return f"{self.bd_id}-{self.slug}.md"


def discover_legacy_adrs() -> list[LegacyADR]:
    """Find all NNNN-<slug>.md files in docs/adr/, sorted by number."""
    out = []
    for p in sorted(ADR_DIR.iterdir()):
        m = LEGACY_PATTERN.match(p.name)
        if not m:
            continue
        out.append(LegacyADR(path=p, number=int(m.group(1)), slug=m.group(2)))
    return out


def parse_legacy(adr: LegacyADR) -> None:
    """Populate fields by parsing the markdown file."""
    text = adr.path.read_text(encoding="utf-8")

    # Title: first H1; strip the "ADR NNNN: " prefix.
    m = re.search(r"^#\s+(.+?)\s*$", text, re.MULTILINE)
    assert m, f"{adr.path}: no H1 found"
    title = m.group(1)
    title = re.sub(r"^ADR\s+\d+:\s*", "", title).strip()
    adr.title = title

    # Date.
    m = re.search(r"^\*\*Date:\*\*\s+(\S+)\s*$", text, re.MULTILINE)
    assert m, f"{adr.path}: no Date header"
    adr.date = m.group(1)

    # Status (may include "Superseded by ADR-XXXX", "[ADR 0014](...)", or "0014").
    # Real-world forms seen in docs/adr/:
    #   **Status:** Superseded by [ADR 0014](0014-...md)   (file body)
    #   **Status:** Superseded by 0014                     (README)
    #   **Status:** Superseded by ADR-0014                 (some hand-written)
    # The regex accepts all three: optional `[`, optional `ADR` token with
    # optional `-` or space separator, leading zeros allowed, then digits.
    m = re.search(r"^\*\*Status:\*\*\s+(.+?)\s*$", text, re.MULTILINE)
    assert m, f"{adr.path}: no Status header"
    status_raw = m.group(1).strip()
    adr.status = status_raw
    sup = re.search(r"Superseded by\s+(?:\[)?(?:ADR[\s-]?)?0*(\d+)", status_raw)
    if sup:
        adr.superseded_by_number = int(sup.group(1))

    # Sections: split on H2 headers and capture content per section name.
    sections = split_h2_sections(text)
    adr.context = sections.get("Context", "")
    adr.decision = sections.get("Decision", "")
    adr.rationale = sections.get("Rationale", "")
    adr.alternatives = sections.get("Alternatives Considered", "")
    adr.consequences = sections.get("Consequences", "")
    adr.references = sections.get("References", "")

    # If current format (no Alternatives Considered H2 but Options Considered
    # nested under Context), lift it out.
    if not adr.alternatives:
        m = re.search(r"###\s+Options Considered\s*\n(.+?)(?=^##\s|\Z)",
                      adr.context, re.DOTALL | re.MULTILINE)
        if m:
            adr.alternatives = m.group(1).strip()
            adr.context = adr.context[:m.start()].rstrip()

    # All required sections must be non-empty after the lift.
    for fld in ("title", "date", "context", "decision", "rationale", "alternatives", "consequences"):
        if not getattr(adr, fld):
            sys.stderr.write(
                f"WARN: {adr.path.name}: empty field {fld!r} after parse\n"
            )


def split_h2_sections(text: str) -> dict[str, str]:
    """Return {section_name: body} for each `## Section` block."""
    out: dict[str, str] = {}
    # Find each `## Name` header and the text up to the next `## ` or EOF.
    pattern = re.compile(r"^##\s+(.+?)\s*\n(.*?)(?=^##\s|\Z)",
                         re.DOTALL | re.MULTILINE)
    for m in pattern.finditer(text):
        out[m.group(1).strip()] = m.group(2).strip()
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--apply", action="store_true",
                    help="Actually mutate state (default: dry-run)")
    args = ap.parse_args()

    adrs = discover_legacy_adrs()
    print(f"Found {len(adrs)} legacy ADR files in {ADR_DIR}.")
    for a in adrs:
        parse_legacy(a)
        print(f"  {a.path.name}: title={a.title!r} date={a.date} "
              f"status={a.status!r}"
              + (f" supersededBy=ADR-{a.superseded_by_number:04d}"
                 if a.superseded_by_number else ""))

    if not args.apply:
        print("\nDry-run complete. Re-run with --apply to mutate state.")
        return 0

    # Apply phase added in subsequent tasks.
    sys.stderr.write("ERROR: --apply not yet implemented (task 10+)\n")
    return 1


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 2: Make it executable**

```bash
chmod +x scripts/adr-migrate.py
```

- [ ] **Step 3: Self-test the supersession regex against the real ADR 0007 format**

Run:

```bash
python3 -c "
import re
pat = re.compile(r'Superseded by\s+(?:\[)?(?:ADR[\s-]?)?0*(\d+)')
for label, s in [
    ('file', '**Status:** Superseded by [ADR 0014](0014-direct.md)'),
    ('readme', '**Status:** Superseded by 0014'),
    ('hand', '**Status:** Superseded by ADR-0014'),
    ('hand2', 'Superseded by ADR 0014'),
]:
    m = pat.search(s)
    assert m and m.group(1) == '14', f'FAIL {label}: {m}'
    print(f'OK {label} → ADR-{int(m.group(1)):04d}')
"
```

Expected: 4 `OK` lines, exit 0. (Catches Finding #2 from plan-reviewer
r1 — the original regex `Superseded by ADR-?(\d+)` did not match the
real file's `[ADR 0014]` format and would have silently lost the
supersession edge.)

- [ ] **Step 4: Run the parser in dry-run**

Run:

```bash
python3 scripts/adr-migrate.py
```

Expected: lists all 17 ADRs with parsed title, date, status. ADR 0007
should report `supersededBy=ADR-0014`. No `WARN: empty field` messages
for any of the 17.

If WARN messages appear: the parser needs adjustment for that ADR's
format. Inspect the warned file and either expand the parser or add a
test annotation. (For the existing 17, the parser should be sufficient
on first pass — both formats are handled.)

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scripts): adr-migrate.py — parser phase

Walk docs/adr/NNNN-<slug>.md, parse both format families (original +
current with embedded Options Considered subsection), report fields.
Dry-run only; --apply not implemented yet.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 9"
```

---

## Task 10: Migration script — renderer + slug + dry-run mapping

**Files:**

- Modify: `scripts/adr-migrate.py`

- [ ] **Step 1: Add the renderer**

Edit `scripts/adr-migrate.py`. Add this function after
`split_h2_sections`:

```python
def slugify(title: str) -> str:
    """Kebab-case the title, drop stop-words, cap 60 chars."""
    s = title.lower()
    s = re.sub(r"[^\w\s-]", "", s)
    s = re.sub(r"\s+", "-", s).strip("-")
    stop = {"a", "an", "the", "for", "of", "to", "in", "on", "with"}
    parts = [p for p in s.split("-") if p and p not in stop]
    return "-".join(parts)[:60].rstrip("-")


def render_adr(adr: LegacyADR, bd_id: str) -> str:
    """Render the unified-format markdown body for the new ADR file."""
    return f"""<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# {adr.title}

**Date:** {adr.date}
**Status:** {adr.status}
**Decision:** {bd_id}
**Originally:** ADR {adr.number:04d}
**Deciders:** HoloMUSH Contributors

## Context

{adr.context}

## Decision

{adr.decision}

## Rationale

{adr.rationale}

## Alternatives Considered

{adr.alternatives}

## Consequences

{adr.consequences}

## References

{adr.references}
"""


def render_bd_description(adr: LegacyADR) -> str:
    """Render the body to feed `bd create -t decision --description`.

    Identical to the file body BUT omits the `**Decision:**` header line
    (the bd record IS the decision; cross-linking is one-way: file → bd).
    """
    return f"""## Context

{adr.context}

## Decision

{adr.decision}

## Rationale

{adr.rationale}

## Alternatives Considered

{adr.alternatives}

## Consequences

{adr.consequences}

## References

{adr.references}

Legacy ADR number: {adr.number:04d}
"""
```

- [ ] **Step 2: Use slug in the dry-run mapping**

In `main()`, replace the inner-loop print with:

```python
    for a in adrs:
        parse_legacy(a)
        a.slug = slugify(a.title) or a.slug  # prefer title-derived slug
        print(f"  ADR-{a.number:04d}  →  <bd-id>-{a.slug}.md  "
              f"({a.date}, {a.status})"
              + (f"  [supersededBy=ADR-{a.superseded_by_number:04d}]"
                 if a.superseded_by_number else ""))
```

- [ ] **Step 3: Run the dry-run**

```bash
python3 scripts/adr-migrate.py
```

Expected: lists 17 ADRs with planned `<bd-id>-<slug>.md` filenames; bd-id
shows literal `<bd-id>` because they're not allocated yet in dry-run.
The slug should be derived from the parsed title for each ADR (e.g.,
`opaque-session-tokens-instead-signed-jwts`).

- [ ] **Step 4: Validate that rendered ADRs would satisfy bd --validate**

Run this Python snippet (one-liner, no temp file needed):

```bash
python3 -c "
import sys
sys.path.insert(0, 'scripts')
from adr_migrate import discover_legacy_adrs, parse_legacy, render_adr
for a in discover_legacy_adrs():
    parse_legacy(a)
    body = render_adr(a, 'holomush-test')
    for req in ('## Decision', '## Rationale', '## Alternatives Considered'):
        assert req in body, f'{a.path.name}: missing {req}'
print('All 17 render with required sections.')
"
```

Expected: `All 17 render with required sections.` (exit 0).

If the import fails because `adr_migrate.py` has a hyphen: rename the
local copy to `adr_migrate` in the import OR use `runpy`:

```bash
python3 -c "
import runpy, sys
m = runpy.run_path('scripts/adr-migrate.py')
for a in m['discover_legacy_adrs']():
    m['parse_legacy'](a)
    body = m['render_adr'](a, 'holomush-test')
    for req in ('## Decision', '## Rationale', '## Alternatives Considered'):
        assert req in body, f'{a.path.name}: missing {req}'
print('All 17 render with required sections.')
"
```

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scripts): adr-migrate renderer + slug

slugify() generates filename slugs from titles. render_adr() produces
the unified-format markdown body that the migration writes to the new
file path; render_bd_description() produces the same content minus the
**Decision:** header (cross-linking is file → bd, not symmetric).
All 17 existing ADRs render with the three validator-required headers.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 10"
```

---

## Task 11: Migration script — bd integration (create + dep + close)

**Files:**

- Modify: `scripts/adr-migrate.py`

- [ ] **Step 1: Add bd-write helpers**

Edit `scripts/adr-migrate.py`. Add after `render_bd_description`:

```python
def run(cmd: list[str], stdin: str | None = None, check: bool = True) -> str:
    """Run a subprocess; return stdout. Raise on non-zero unless check=False."""
    p = subprocess.run(
        cmd,
        input=stdin,
        capture_output=True,
        text=True,
        check=False,
    )
    if check and p.returncode != 0:
        sys.stderr.write(f"ERROR: {cmd!r} exited {p.returncode}\n")
        sys.stderr.write(p.stderr)
        raise SystemExit(p.returncode)
    return p.stdout


BD_ID_PATTERN = re.compile(r"Created issue: (\S+)")
# Self-test on module load: catches a future bd CLI stdout change at import
# time rather than mid-migration (when 17 records have already been written).
assert BD_ID_PATTERN.search(
    "✓ Created issue: holomush-xxxx — title"
), "BD_ID_PATTERN no longer matches bd create stdout; update the regex."


def bd_create_decision(title: str, description: str) -> str:
    """`bd create -t decision --validate`; return the new bd-id."""
    out = run([
        "bd", "create",
        "-t", "decision",
        "--validate",
        "--title", title,
        "--description", description,
    ])
    m = BD_ID_PATTERN.search(out)
    if not m:
        sys.stderr.write(f"ERROR: could not parse bd-id from:\n{out}\n")
        raise SystemExit(1)
    return m.group(1)


def bd_dep_supersedes(new_id: str, old_id: str) -> None:
    """Record a supersedes dep edge: new_id supersedes old_id."""
    run(["bd", "dep", "add", new_id, old_id, "--type", "supersedes"])


def bd_close_superseded(old_id: str, new_id: str) -> None:
    """Close the superseded record with a reason."""
    run(["bd", "close", old_id, "--reason", f"Superseded by {new_id}"])


def bd_dolt_commit(message: str) -> None:
    run(["bd", "dolt", "commit", "-m", message])
```

- [ ] **Step 2: Add the apply-phase function**

Add after the bd helpers:

```python
def apply_migration(adrs: list[LegacyADR]) -> None:
    """Mutate state: create bd records, rename files, write stubs, regen README."""
    # Pass 1: create bd records for every ADR; populate bd_id.
    for a in adrs:
        a.slug = slugify(a.title) or a.slug
        body = render_bd_description(a)
        a.bd_id = bd_create_decision(a.title, body)
        print(f"  bd: {a.bd_id} ← ADR-{a.number:04d} ({a.title!r})")

    # Pass 2: build number→bd-id index for supersession edges.
    by_number = {a.number: a for a in adrs}

    # Pass 3: rename files, write stubs (next task).
    # Pass 4: supersession edges + close (next task).
```

Replace the `--apply` placeholder in `main()`:

```python
    apply_migration(adrs)
    print("\nMigration apply phase 1 (bd create) complete.")
```

- [ ] **Step 3: Run a contained smoke test**

Do NOT run the full migration yet (it would create 17 real bd records).
Smoke-test the bd helpers against a single fixture:

```bash
python3 -c "
import runpy
m = runpy.run_path('scripts/adr-migrate.py')
bd_id = m['bd_create_decision'](
    'TEST migration smoke test',
    '''## Decision
Test only.
## Rationale
Smoke test for adr-migrate.
## Alternatives Considered
None — this is a probe.
'''
)
print(f'created {bd_id}')
import subprocess
subprocess.run(['bd', 'delete', bd_id, '-f'], check=True)
print(f'deleted {bd_id}')
"
```

Expected: `created holomush-XXXX` then `deleted holomush-XXXX`, exit 0.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(scripts): adr-migrate bd integration helpers

bd_create_decision(): bd create -t decision --validate, parses bd-id
from stdout. bd_dep_supersedes(): bd dep add <new> <old> --type
supersedes (positional form per CLI spec). bd_close_superseded():
bd close <old> --reason 'Superseded by <new>'. apply_migration() pass
1 creates bd records for all 17 ADRs.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 11"
```

---

## Task 12: Migration script — rename + stub + README + supersession + asserts

**Files:**

- Modify: `scripts/adr-migrate.py`

- [ ] **Step 1: Add file rename + stub + README + supersession + inline asserts**

Edit `scripts/adr-migrate.py`. Replace the placeholder Pass 3/Pass 4
comments at the end of `apply_migration` with:

```python
    # Pass 3: rename files + write stubs.
    # jj has no `mv` subcommand; the snapshotter auto-detects the rename
    # from a filesystem move, preserving `jj log --follow` continuity
    # (INV-A17).
    for a in adrs:
        new_path = ADR_DIR / a.new_filename
        body = render_adr(a, a.bd_id)
        # Filesystem rename — jj detects this when it next snapshots.
        a.path.rename(new_path)
        # Overwrite the renamed file with the unified-format body.
        new_path.write_text(body, encoding="utf-8")
        # Write stub at the legacy path (fresh file at the OLD name).
        a.path.write_text(stub_body(a), encoding="utf-8")
        print(f"  renamed: {a.path.name} → {a.new_filename}; stub written")

    # Pass 4: supersession edges + close.
    for a in adrs:
        if a.superseded_by_number is None:
            continue
        superseder = by_number.get(a.superseded_by_number)
        if superseder is None:
            sys.stderr.write(
                f"WARN: ADR-{a.number:04d} says superseded by "
                f"ADR-{a.superseded_by_number:04d} but that ADR is "
                f"not present.\n"
            )
            continue
        bd_dep_supersedes(superseder.bd_id, a.bd_id)
        bd_close_superseded(a.bd_id, superseder.bd_id)
        # Rewrite the superseded file's Status header to use bd-id.
        new_path = ADR_DIR / a.new_filename
        text = new_path.read_text(encoding="utf-8")
        text = re.sub(
            r"^\*\*Status:\*\*\s+.+$",
            f"**Status:** Superseded by {superseder.bd_id}",
            text,
            count=1,
            flags=re.MULTILINE,
        )
        new_path.write_text(text, encoding="utf-8")
        print(f"  supersession: {a.bd_id} ← superseded by {superseder.bd_id}")

    # Pass 5: regenerate README.
    regenerate_readme(adrs)

    # Pass 6: inline assertions (INV-A12, A13).
    # Run BEFORE bd_dolt_commit so a verify failure leaves the bd database
    # un-committed and the working copy abandonable via `jj abandon`.
    verify_post_migration(adrs)

    # Pass 7: bd dolt commit. Only reached if verification passed.
    bd_dolt_commit("migration: import 17 legacy ADRs as decision records")
```

Add the supporting functions (above `apply_migration`):

```python
def stub_body(a: LegacyADR) -> str:
    return f"""<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Moved

This ADR has moved to **[`{a.new_filename}`]({a.new_filename})**.

- **bd decision:** `{a.bd_id}` (run `bd show {a.bd_id}` for live status)
- **Legacy number:** ADR {a.number:04d}
- **Date migrated:** 2026-05-14
"""


def regenerate_readme(adrs: list[LegacyADR]) -> None:
    """Rewrite docs/adr/README.md end-to-end."""
    by_date_desc = sorted(adrs, key=lambda a: a.date, reverse=True)
    index_rows = "\n".join(
        f"| [{a.title}]({a.new_filename}) | {a.date} | {a.status} | `{a.bd_id}` |"
        for a in by_date_desc
    )
    migration_rows = "\n".join(
        f"| ADR {a.number:04d} | `{a.bd_id}` | [{a.new_filename}]({a.new_filename}) |"
        for a in sorted(adrs, key=lambda a: a.number)
    )
    content = f"""<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Architecture Decision Records (ADRs)

This directory contains Architecture Decision Records (ADRs) documenting
significant design decisions made during HoloMUSH development. Each ADR
captures the context, options considered, decision made, and consequences
of architectural choices.

ADRs are immutable once accepted. If a decision is reversed, a new ADR
supersedes the old one; the bd decision record gains a `--type supersedes`
edge and the file's `**Status:**` reflects the supersession.

## Index

| Title | Date | Status | bd decision |
|-------|------|--------|-------------|
{index_rows}

<!-- BEGIN MIGRATION MAP -->

## Migration map (2026-05-14)

The legacy `NNNN-<slug>.md` numbering was retired in favor of
bd-decision IDs. Stubs at the old paths preserve external references.

| Legacy | bd decision | Current file |
|--------|-------------|--------------|
{migration_rows}

<!-- END MIGRATION MAP -->

## Format

See `docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md`
§"ADR format (unified)" for the canonical template. All ADRs use one
format: Context, Decision, Rationale, Alternatives Considered,
Consequences, References.

## Template

New ADRs are written by the `/capture-adrs` skill, which renders from
the spec's format definition. To write one manually, follow the same
shape and use `bd create -t decision --validate` to file the record.

## Writing guidelines

| Guideline                 | Description                                                                                              |
| ------------------------- | -------------------------------------------------------------------------------------------------------- |
| **Immutability**          | ADRs are permanent records — do not edit accepted ADRs to change decisions                               |
| **Supersession**          | To reverse a decision, create a new ADR and mark the old one as "Superseded by `<bd-id>`"                |
| **RFC2119 keywords**      | Use MUST/SHOULD/MAY in consequences when describing implementation requirements                          |
| **Comprehensive options** | Document ALL options considered, not just the chosen one                                                 |
| **Trade-off clarity**     | Consequences should honestly capture both benefits and costs                                             |
| **Future-proof**          | Assume readers in 5 years won't have context — explain everything                                        |

## References

- [Michael Nygard's ADR template](https://github.com/joelparkerhenderson/architecture-decision-record)
- [ADR Tools GitHub](https://github.com/npryce/adr-tools)
- [RFC 2119: Key words for RFCs](https://www.ietf.org/rfc/rfc2119.txt)
"""
    (ADR_DIR / "README.md").write_text(content, encoding="utf-8")


def verify_post_migration(adrs: list[LegacyADR]) -> None:
    """Inline asserts enforcing INV-A12, A13."""
    real_files = sorted(
        p.name for p in ADR_DIR.iterdir()
        if re.match(r"^[a-z0-9]+-[a-z0-9-]+\.md$", p.name)
        and not re.match(r"^\d{4}-", p.name)
    )
    stubs = sorted(
        p.name for p in ADR_DIR.iterdir()
        if re.match(r"^\d{4}-.+\.md$", p.name)
    )
    assert len(real_files) == 17, f"expected 17 real files, got {len(real_files)}: {real_files}"
    assert len(stubs) == 17, f"expected 17 stubs, got {len(stubs)}: {stubs}"
    assert (ADR_DIR / "README.md").exists(), "README.md missing"
    assert not (ADR_DIR / "legacy").exists(), "legacy/ subdirectory MUST NOT exist (INV-A12 flat-stub rule)"

    # Each stub links to an existing real file.
    for stub_name in stubs:
        stub = ADR_DIR / stub_name
        text = stub.read_text(encoding="utf-8")
        m = re.search(r"\[`([a-z0-9-]+\.md)`\]", text)
        assert m, f"{stub_name}: no link in stub"
        target = m.group(1)
        assert (ADR_DIR / target).exists(), f"{stub_name} links to missing {target}"

    # Supersession edge present (ADR 0007 → 0014).
    sup = [a for a in adrs if a.superseded_by_number is not None]
    if sup:
        # Should be exactly 1 in the current backlog.
        assert len(sup) == 1, f"expected 1 supersession, got {len(sup)}"
        a = sup[0]
        superseder = next(s for s in adrs if s.number == a.superseded_by_number)
        # Cross-check the dep edge exists.
        deps = run(["bd", "dep", "list", superseder.bd_id])
        assert "supersedes" in deps and a.bd_id in deps, (
            f"supersession edge missing: {superseder.bd_id} should supersede {a.bd_id}\n{deps}"
        )

    print(f"\n✓ Post-migration verification passed: "
          f"{len(real_files)} real + {len(stubs)} stubs + README + "
          f"{len(sup)} supersession edges.")
```

- [ ] **Step 2: Dry-run to verify the new code parses**

```bash
python3 scripts/adr-migrate.py
```

Expected: still works as dry-run; no `--apply` ran; the new code is only
loaded, not executed.

- [ ] **Step 3: Run a syntax-only lint pass**

```bash
python3 -m py_compile scripts/adr-migrate.py
```

Expected: silent, exit 0.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(scripts): adr-migrate file rename + stub + README + asserts

Pass 3: filesystem rename each NNNN-<slug>.md to <bd-id>-<slug>.md
(jj snapshotter auto-detects the rename) and rewrite contents in
unified format; write stub at the legacy path.
Pass 4: supersession edges (bd dep add --type supersedes; bd close
--reason) and rewrite superseded file's Status header.
Pass 5: regenerate docs/adr/README.md (index sorted by date; migration
map between sentinels).
Pass 6: bd dolt commit.
Pass 7: inline asserts for INV-A12 (35 files, no legacy/), stub links,
INV-A13 (supersession edge).

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 12"
```

---

## Task 13: Migration script — dry-run no-mutation check (INV-A18)

**Files:**

- Modify: `scripts/adr-migrate.py`

- [ ] **Step 1: Add explicit dry-run guard**

Edit `scripts/adr-migrate.py`. In `main()`, replace:

```python
    if not args.apply:
        print("\nDry-run complete. Re-run with --apply to mutate state.")
        return 0
```

with:

```python
    if not args.apply:
        # INV-A18: dry-run MUST NOT mutate any state.
        # Defensive: explicitly do NOT call bd_create / file rename / file writes.
        # The print loop above only reads; we exit before apply_migration.
        print("\nDry-run complete. Re-run with --apply to mutate state.")
        return 0
```

- [ ] **Step 2: Verify dry-run leaves the working copy untouched**

```bash
python3 scripts/adr-migrate.py >/dev/null
jj st
```

Expected: working copy lists ONLY the in-flight `scripts/adr-migrate.py`
and any prior task changes. No new files in `docs/adr/`, no
modifications.

- [ ] **Step 3: Verify bd state unchanged by dry-run**

```bash
before=$(bd list --type decision --json | jq 'length')
python3 scripts/adr-migrate.py >/dev/null
after=$(bd list --type decision --json | jq 'length')
[ "$before" = "$after" ] && echo "INV-A18: bd count stable ($before)"
```

Expected: `INV-A18: bd count stable (0)` (assuming no decision records
exist yet).

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(scripts): adr-migrate explicit dry-run guard (INV-A18)

Annotate the dry-run exit branch with the invariant it upholds; manual
verification of bd-count stability + jj st cleanliness.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 13"
```

---

## Task 14: `adr-doctor.sh` — file-count checks

**Files:**

- Create: `scripts/adr-doctor.sh`

- [ ] **Step 1: Write the skeleton + 5 file-count checks**

Create `scripts/adr-doctor.sh`:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Durable health check for docs/adr/. Wired into `task lint` via the
# lint:adr target. Exits 0 if clean; 1 on any check failure; 2 on
# missing prerequisites.
#
# See docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md
# §"`adr-doctor.sh` health check".

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ADR_DIR="$REPO_ROOT/docs/adr"
SPEC="$REPO_ROOT/docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md"

explain=0
if [ "${1:-}" = "--explain" ]; then
  explain=1
fi

fail_count=0

note() {
  [ "$explain" = "1" ] && echo "→ $*" >&2
}

check_fail() {
  echo "FAIL: $*" >&2
  fail_count=$((fail_count + 1))
}

# Prerequisites.
command -v bd  >/dev/null || { echo "missing prerequisite: bd"  >&2; exit 2; }
command -v jq  >/dev/null || { echo "missing prerequisite: jq"  >&2; exit 2; }
[ -d "$ADR_DIR" ] || { echo "missing $ADR_DIR" >&2; exit 2; }

# --- count_real_files (INV-A12) ---
note "count_real_files (INV-A12)"
real=$(find "$ADR_DIR" -maxdepth 1 -type f -name '*.md' \
  | grep -E '/[a-z][a-z0-9-]+-[a-z][a-z0-9-]+\.md$' \
  | grep -vE '/[0-9]{4}-' \
  | wc -l | tr -d ' ')
if [ "$real" != "17" ]; then
  check_fail "expected 17 <bd-id>-<slug>.md files; got $real"
fi

# --- count_stubs (INV-A12) ---
note "count_stubs (INV-A12)"
stubs=$(find "$ADR_DIR" -maxdepth 1 -type f -name '*.md' \
  | grep -E '/[0-9]{4}-' \
  | wc -l | tr -d ' ')
if [ "$stubs" != "17" ]; then
  check_fail "expected 17 [0-9]{4}-*.md stubs; got $stubs"
fi

# --- count_readme (INV-A12) ---
note "count_readme (INV-A12)"
if [ ! -f "$ADR_DIR/README.md" ]; then
  check_fail "missing $ADR_DIR/README.md"
fi
total=$(find "$ADR_DIR" -maxdepth 1 -type f -name '*.md' | wc -l | tr -d ' ')
if [ "$total" != "35" ]; then
  check_fail "expected 35 files total in $ADR_DIR; got $total"
fi

# --- no_legacy_subdir (INV-A12) ---
note "no_legacy_subdir (INV-A12)"
if [ -d "$ADR_DIR/legacy" ]; then
  check_fail "$ADR_DIR/legacy must not exist (stubs MUST stay flat)"
fi

# --- stub_links_real (INV-A12) ---
note "stub_links_real (INV-A12)"
for stub in "$ADR_DIR"/[0-9][0-9][0-9][0-9]-*.md; do
  [ -f "$stub" ] || continue
  target=$(grep -oE '\[`[a-z0-9-]+\.md`\]' "$stub" | head -1 | tr -d '[]`' )
  if [ -z "$target" ]; then
    check_fail "$stub: no link found in stub body"
    continue
  fi
  if [ ! -f "$ADR_DIR/$target" ]; then
    check_fail "$stub: links to missing $target"
  fi
done

if [ "$fail_count" -gt 0 ]; then
  echo "$fail_count check(s) failed." >&2
  exit 1
fi
echo "adr-doctor: all checks passed."
exit 0
```

- [ ] **Step 2: Make executable**

```bash
chmod +x scripts/adr-doctor.sh
```

- [ ] **Step 3: Run (expect failure — no migration has happened yet)**

```bash
scripts/adr-doctor.sh
```

Expected: failures reporting "expected 17 <bd-id>-<slug>.md files; got 0"
and similar. Exit 1.

This is correct — the migration hasn't run yet. The checks will pass
after Task 18.

- [ ] **Step 4: Run shellcheck**

```bash
shellcheck scripts/adr-doctor.sh
```

Expected: clean (or only `SC2155`-style style notes acceptable).

- [ ] **Step 5: Commit**

```bash
jj describe -m "feat(scripts): adr-doctor.sh file-count checks

Skeleton + 5 INV-A12 checks (real-file count, stub count, README
presence, total=35, no legacy/ subdir, stubs link to real files).
Will fail until the migration runs; that's expected for now.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 14"
```

---

## Task 15: `adr-doctor.sh` — content + frontmatter + supersession checks

**Files:**

- Modify: `scripts/adr-doctor.sh`

- [ ] **Step 1: Add 6 more checks**

Edit `scripts/adr-doctor.sh`. Before the final `if [ "$fail_count" -gt 0 ];`
block, insert:

```bash
# --- file_has_decision_header (INV-A4, INV-A5) ---
note "file_has_decision_header (INV-A4, INV-A5)"
for f in "$ADR_DIR"/*-*.md; do
  [ -f "$f" ] || continue
  case "$(basename "$f")" in
    [0-9][0-9][0-9][0-9]-*) continue ;;  # stub
    README.md) continue ;;
  esac
  bn=$(basename "$f")
  bd_id_from_filename="${bn%-*.md}"  # naive: take everything before the last '-<slug>.md'
  # Tighten: bd-id format is 'holomush-XXXX'; pull the first 'holomush-XXXX' substring.
  bd_id_from_filename=$(echo "$bn" | grep -oE '^holomush-[a-z0-9]+' || true)
  decision_line=$(grep -E '^\*\*Decision:\*\*\s+holomush-' "$f" | head -1)
  if [ -z "$decision_line" ]; then
    check_fail "$f: missing **Decision:** holomush-<id> header"
    continue
  fi
  decision_id=$(echo "$decision_line" | grep -oE 'holomush-[a-z0-9]+')
  if [ "$decision_id" != "$bd_id_from_filename" ]; then
    check_fail "$f: **Decision:** $decision_id does not match filename bd-id $bd_id_from_filename"
    continue
  fi
  if ! bd show "$decision_id" >/dev/null 2>&1; then
    check_fail "$f: bd show $decision_id failed (record missing)"
  fi
done

# --- file_has_validator_sections (INV-A4) ---
note "file_has_validator_sections (INV-A4)"
for f in "$ADR_DIR"/holomush-*-*.md; do
  [ -f "$f" ] || continue
  for hdr in '## Decision' '## Rationale' '## Alternatives Considered'; do
    if ! grep -qF "$hdr" "$f"; then
      check_fail "$f: missing $hdr header"
    fi
  done
done

# --- agent_frontmatter (INV-A14, INV-A15) ---
note "agent_frontmatter (INV-A14, INV-A15)"
AGENT="$REPO_ROOT/.claude/agents/adr-extractor.md"
if [ ! -f "$AGENT" ]; then
  check_fail "agent file missing: $AGENT"
else
  # Extract YAML between the first pair of '---' lines.
  fm=$(awk '/^---$/{c++; next} c==1' "$AGENT")
  if ! printf '%s\n' "$fm" | grep -qE '^model:\s+sonnet\s*$'; then
    check_fail "$AGENT: model must be sonnet"
  fi
  if printf '%s\n' "$fm" | grep -qE '^\s+-\s+(Write|Edit|NotebookEdit)\s*$'; then
    check_fail "$AGENT: tools list MUST NOT include Write/Edit/NotebookEdit"
  fi
fi

# --- hook_executable ---
note "hook_executable"
HOOK="$REPO_ROOT/.claude/hooks/nudge-adr-capture.sh"
if [ ! -x "$HOOK" ]; then
  check_fail "$HOOK: not executable"
fi
if ! shellcheck "$HOOK" >/dev/null 2>&1; then
  check_fail "$HOOK: shellcheck failed"
fi

# --- forbid_skill_commits (INV-A2) ---
note "forbid_skill_commits (INV-A2)"
SKILL="$REPO_ROOT/.claude/skills/capture-adrs/SKILL.md"
if [ -f "$SKILL" ]; then
  # Look for command-shaped forbidden strings (in code blocks or inline-command form).
  # The Anti-patterns section may mention them in 'DO NOT commit' prose; allow that.
  if grep -qE '(^\s*\$\s*(jj commit|jj describe|git commit|git add)|^\s*`(jj commit|jj describe|git commit|git add)`)' "$SKILL"; then
    check_fail "$SKILL: contains a commit/describe command — skill MUST NOT commit"
  fi
fi

# --- supersession_edges (INV-A13) ---
note "supersession_edges (INV-A13)"
for f in "$ADR_DIR"/holomush-*-*.md; do
  [ -f "$f" ] || continue
  status=$(grep -E '^\*\*Status:\*\*\s+Superseded by\s+holomush-' "$f" | head -1 || true)
  [ -n "$status" ] || continue
  this_id=$(grep -oE '^\*\*Decision:\*\*\s+holomush-[a-z0-9]+' "$f" | grep -oE 'holomush-[a-z0-9]+')
  superseder=$(echo "$status" | grep -oE 'holomush-[a-z0-9]+')
  # Confirm bd dep list shows the edge.
  if ! bd dep list "$superseder" 2>/dev/null | grep -q "supersedes.*$this_id"; then
    check_fail "$f: Status says superseded by $superseder, but bd dep edge missing"
  fi
done
```

- [ ] **Step 2: Run the doctor (still expected to fail — no migration yet)**

```bash
scripts/adr-doctor.sh
```

Expected: a longer list of failures. The agent_frontmatter and
hook_executable checks should now pass (those artifacts exist from
earlier tasks). The ADR-content checks still fail.

- [ ] **Step 3: Run shellcheck**

```bash
shellcheck scripts/adr-doctor.sh
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
jj describe -m "feat(scripts): adr-doctor.sh content + frontmatter + supersession checks

Adds 6 checks: file_has_decision_header (INV-A4/A5),
file_has_validator_sections (INV-A4), agent_frontmatter (INV-A14/A15),
hook_executable, forbid_skill_commits (INV-A2), supersession_edges
(INV-A13). Still fails until migration runs, except the agent and hook
checks which pass on current state.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 15"
```

---

## Task 16: `adr-doctor.sh` — `invariant_coverage` meta-test + Taskfile wiring

**Files:**

- Modify: `scripts/adr-doctor.sh`
- Modify: `Taskfile.yaml`

- [ ] **Step 1: Add the meta-test check**

Edit `scripts/adr-doctor.sh`. Before the final `if [ "$fail_count" ...]`
block, insert:

```bash
# --- invariant_coverage (meta-test) ---
note "invariant_coverage"
SURFACE_ENUM='doctor|hook-test|skill-test|migration-assert|manual'
# Extract every '| INV-A<n> | <surface> |' row from the spec.
while IFS='|' read -r _ id surface _; do
  id="$(echo "$id" | tr -d ' ')"
  surface="$(echo "$surface" | sed 's/^ *//;s/ *$//')"
  case "$id" in INV-A[0-9]*) ;; *) continue ;; esac
  if [ -z "$surface" ]; then
    check_fail "meta-test: $id has empty Surface column"
    continue
  fi
  # Validate every '+' -joined token is in the enum.
  IFS='+' read -ra toks <<< "$(echo "$surface" | tr -d ' ')"
  for tok in "${toks[@]}"; do
    case "$tok" in
      doctor|hook-test|skill-test|migration-assert|manual) ;;
      *) check_fail "meta-test: $id has Surface token '$tok' not in {$SURFACE_ENUM}" ;;
    esac
  done
  # If doctor-tagged, this script must mention '# INV-A<n>' in a comment.
  if echo "$surface" | grep -q 'doctor'; then
    if ! grep -qE "#.*$id\b" "$0"; then
      check_fail "meta-test: $id is doctor-tagged but no '# $id' comment exists in adr-doctor.sh"
    fi
  fi
done < "$SPEC"
```

- [ ] **Step 2: Audit the script for `# INV-A<n>` comments**

The meta-test requires a `# INV-A<n>` comment for every doctor-tagged
invariant. Doctor-tagged (per the spec table) are: A2, A4, A5, A12,
A13, A14, A15. Verify each appears as a comment:

```bash
for i in A2 A4 A5 A12 A13 A14 A15; do
  if grep -qE "#.*INV-$i\b" scripts/adr-doctor.sh; then
    echo "OK INV-$i"
  else
    echo "MISSING INV-$i"
  fi
done
```

Expected: all 7 print `OK`. If any prints `MISSING`, add a `# INV-A<n>`
comment to the relevant `note ...` line or check body. (The existing
`note "<name> (INV-A<n>)"` lines should satisfy this for most; the
forbid_skill_commits line uses `(INV-A2)` which already matches.)

- [ ] **Step 3: Add the Taskfile target**

Edit `Taskfile.yaml`. Find the existing `lint:` task and append a new
target:

```yaml
  lint:adr:
    desc: "Run docs/adr/ health check (adr-doctor.sh)"
    cmds:
      - scripts/adr-doctor.sh

  adr:migrate:
    desc: "One-shot migration of legacy ADRs to bd-id filename convention. Use -- --apply to mutate."
    cmds:
      - python3 scripts/adr-migrate.py {{.CLI_ARGS}}
```

Locate the existing `lint:` task — it uses `cmds:` with nested
`- task: lint:xxx` entries (NOT `deps:`). Append `- task: lint:adr` to
the cmds list. Find this block in `Taskfile.yaml`:

```yaml
  lint:
    desc: Run all linters
    cmds:
      - task: lint:go
      - task: lint:proto
      - task: lint:markdown
      - task: lint:yaml
      - task: lint:actions
      - task: lint:access-migration
      - task: lint:test-helpers
      - task: lint:plugin-manifests
      - task: lint:docs-symmetry
```

Replace with:

```yaml
  lint:
    desc: Run all linters
    cmds:
      - task: lint:go
      - task: lint:proto
      - task: lint:markdown
      - task: lint:yaml
      - task: lint:actions
      - task: lint:access-migration
      - task: lint:test-helpers
      - task: lint:plugin-manifests
      - task: lint:docs-symmetry
      - task: lint:adr
```

Verify with:

```bash
task --list 2>&1 | grep -E 'lint:adr|adr:migrate'
```

Expected: both `lint:adr` and `adr:migrate` appear in the task list.

- [ ] **Step 4: Run the doctor with --explain**

```bash
scripts/adr-doctor.sh --explain 2>&1 | tail -40
```

Expected: each `→ <check-name>` line, then a list of remaining failures
(the ADR content checks still fail until migration). The
`invariant_coverage` line should NOT fail (all 7 doctor invariants
have comments; the SPEC table parses).

- [ ] **Step 5: Confirm `task lint:adr` exits non-zero (expected pre-migration)**

```bash
task lint:adr || echo "lint:adr failed as expected (no migration yet)"
```

Expected: `lint:adr failed as expected (no migration yet)`.

- [ ] **Step 6: Commit**

```bash
jj describe -m "feat(scripts,taskfile): adr-doctor meta-test + Taskfile targets

invariant_coverage check walks the spec's Invariants table, asserts
every row has a Surface value from the enum, and requires a
'# INV-A<n>' comment in this script for every doctor-tagged invariant.
Taskfile gains adr:migrate (proxies to python3) and lint:adr (proxies
to adr-doctor.sh); lint depends on lint:adr.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 16"
```

---

## Task 17: Run migration dry-run; eyeball the planned mapping

**No file changes** — review-only step.

- [ ] **Step 1: Run the dry-run**

```bash
task adr:migrate 2>&1 | tee /tmp/adr-migrate-dryrun.log
```

Expected: ~17 lines like:

```text
ADR-0001  →  <bd-id>-opaque-session-tokens.md  (2026-02-02, Accepted)
...
ADR-0007  →  <bd-id>-command-security-model.md  (2026-02-04, Superseded by ADR-0014)  [supersededBy=ADR-0014]
...
```

Concluding with `Dry-run complete. Re-run with --apply to mutate state.`

- [ ] **Step 2: Verify dry-run made no changes**

```bash
jj st | head -20
```

Expected: only the in-flight working-copy files from the plan tasks
(scripts, hooks, agent, skill). No new files in `docs/adr/`. No
`.beads/` deltas.

```bash
ls docs/adr/ | wc -l
```

Expected: `18` (17 legacy NNNN files + README.md).

- [ ] **Step 3: Sanity check the 17 slugs in the dry-run output**

Scan `/tmp/adr-migrate-dryrun.log` for:

- All 17 ADRs present (0001-0017)
- Each slug looks reasonable (kebab-case, < 60 chars)
- ADR-0007 carries the `supersededBy=ADR-0014` annotation
- No `WARN: empty field` lines

If any slug looks wrong, adjust `slugify()` in `scripts/adr-migrate.py`
or override per-ADR (not expected for the current 17).

- [ ] **Step 4: No commit yet** — dry-run is informational; nothing to commit.

---

## Task 18: Apply the migration

**No file changes by hand** — `task adr:migrate -- --apply` does all
file changes. This task is about running it and verifying.

- [ ] **Step 1: Verify pre-state is clean**

```bash
jj st | grep -E '(admin_read_stream|cmd_admin)' && \
  echo "WARN: unrelated drift present; restore before running migration" || \
  echo "pre-state clean"
```

If WARN appears, restore unrelated drift:

```bash
jj restore --from @- cmd/holomush/admin_read_stream_e2e_test.go cmd/holomush/cmd_admin_read_stream.go cmd/holomush/cmd_admin_read_stream_test.go internal/admin/socket/read_stream_handler_test.go
```

- [ ] **Step 2: Apply the migration**

```bash
task adr:migrate -- --apply 2>&1 | tee /tmp/adr-migrate-apply.log
```

Expected: ~17 `bd: holomush-XXXX ← ADR-NNNN` lines, then 17 `renamed`
lines, 1 `supersession` line, then `✓ Post-migration verification
passed: 17 real + 17 stubs + README + 1 supersession edges.`

- [ ] **Step 3: Run the doctor — all checks must pass**

```bash
task lint:adr
```

Expected: `adr-doctor: all checks passed.`, exit 0.

- [ ] **Step 4: Verify file counts**

```bash
find docs/adr -maxdepth 1 -type f -name '*.md' | wc -l
```

Expected: `35`.

```bash
find docs/adr -maxdepth 1 -type f -name 'holomush-*-*.md' | wc -l
```

Expected: `17`.

```bash
find docs/adr -maxdepth 1 -type f -name '[0-9][0-9][0-9][0-9]-*.md' | wc -l
```

Expected: `17`.

- [ ] **Step 5: Verify bd state**

```bash
bd list --type decision --json | jq 'length'
```

Expected: `17`.

```bash
bd dep list | jq '[.[] | select(.dep_type=="supersedes")] | length' 2>/dev/null \
  || bd dep list | grep -c supersedes
```

Expected: `1` (the 0007 → 0014 edge).

- [ ] **Step 6: Spot-check one migrated ADR**

```bash
ls docs/adr/holomush-*-opaque-session-tokens*.md
```

Expected: one file matching the pattern.

```bash
new_path=$(ls docs/adr/holomush-*-opaque-session-tokens*.md)
head -15 "$new_path"
```

Expected: `# Use Opaque Session Tokens...` (no `ADR 0001:` prefix),
`**Date:** 2026-02-02`, `**Decision:** holomush-XXXX`, `**Originally:**
ADR 0001`.

```bash
cat docs/adr/0001-opaque-session-tokens.md
```

Expected: stub body with `Moved` heading, link to the new file,
`Legacy number: ADR 0001`.

- [ ] **Step 7: Run task fmt to normalize migration output**

```bash
task fmt 2>&1 | tail -5
```

Expected: clean (formatters may normalize whitespace in the 34 new
files; minor).

- [ ] **Step 8: Restore any unrelated drift task fmt introduced**

```bash
jj restore --from @- cmd/holomush/admin_read_stream_e2e_test.go cmd/holomush/cmd_admin_read_stream.go cmd/holomush/cmd_admin_read_stream_test.go internal/admin/socket/read_stream_handler_test.go 2>/dev/null
jj st | head -50
```

- [ ] **Step 9: Commit the migration result**

```bash
jj describe -m "migration: import 17 legacy ADRs as bd-decision records

One-shot conversion from docs/adr/NNNN-<slug>.md to
docs/adr/<bd-id>-<slug>.md. Each ADR file is renamed via filesystem
move (jj snapshotter auto-detects); a stub is written at the legacy
path linking to the new file. README rebuilt with the new Index +
Migration map. Supersession edge ADR 0007 → 0014 recorded as bd dep
--type supersedes; 0007's record closed with reason.

Total: 17 renamed real files, 17 stubs at legacy paths, 1 regenerated
README, 17 new bd decision records, 1 supersession edge.

INV-A11: single jj change. INV-A12: 35-file directory. INV-A13:
supersession edge present. INV-A17: filesystem rename preserves
jj log --follow continuity.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 18"
```

---

## Task 19: Dogfood `/capture-adrs` on this very spec

Goal: prove the skill works end-to-end by running it against the
ADR-capture spec itself. Whatever ADRs surface, ship them in the same
PR.

- [ ] **Step 1: Verify the skill is installed**

```bash
ls .claude/skills/capture-adrs/SKILL.md
```

Expected: file exists.

- [ ] **Step 2: Invoke the skill on the spec**

In a Claude Code interactive prompt, run:

```text
/capture-adrs docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md
```

Expected: skill walks through the candidate review loop. Likely
candidates from this spec include:

- "Use bd-decision ID as ADR filename identifier"
- "PostToolUse hook emits additionalContext JSON instead of bare
  stdout"
- "ADR file body and bd description follow a single unified format
  rendered by independent renderers"
- "Migration is a one-shot Python script, not durable Go tooling"
- "Stubs preserved flat (no legacy/ subdirectory) for external link
  resolution"

Accept the ones you judge ADR-worthy; skip the rest.

- [ ] **Step 3: Verify the spec now has a marker**

```bash
tail -1 docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md
```

Expected: `<!-- adr-capture: sha256=<hex>; session=...; ts=...; adrs=holomush-...,... -->`

- [ ] **Step 4: Verify the captured ADRs landed**

```bash
ls docs/adr/holomush-*-*.md | wc -l
```

Expected: `17 + <captured count>`. If you captured 3 ADRs, the count is
20.

- [ ] **Step 5: Verify the hook no longer nudges on the spec**

```bash
printf '{"tool_name":"Edit","tool_input":{"file_path":"%s"}}' \
  "$(pwd)/docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md" \
  | .claude/hooks/nudge-adr-capture.sh
```

Expected: no stdout (marker is fresh).

- [ ] **Step 6: Run the doctor**

```bash
task lint:adr
```

Expected: `adr-doctor: all checks passed.`

- [ ] **Step 7: Format + commit**

```bash
task fmt 2>&1 | tail -3
jj restore --from @- cmd/holomush/admin_read_stream_e2e_test.go cmd/holomush/cmd_admin_read_stream.go cmd/holomush/cmd_admin_read_stream_test.go internal/admin/socket/read_stream_handler_test.go 2>/dev/null
jj describe -m "feat(adr): dogfood /capture-adrs on its own design spec

Ran the skill against the spec that produced it. Surfaced ADRs are
checked into docs/adr/ alongside the migration result; the spec
carries a fresh adr-capture marker. End-to-end proof.

Bead: holomush-b2qy
Plan: docs/superpowers/plans/2026-05-14-adr-capture-skill.md task 19"
```

(If zero candidates were accepted in step 2, the commit message is the
same — INV-A9 guarantees the marker is still written, and the proof is
that the skill ran cleanly without writes.)

---

## Task 20: Final pr-prep + push

- [ ] **Step 1: Verify working copy is clean (relative to plan tasks)**

```bash
jj st | head -50
```

Expected: only files touched by this plan: `.claude/{skills,agents,hooks}/`,
`scripts/adr-*`, `Taskfile.yaml`, `.claude/settings.json`, `docs/adr/*`,
`docs/superpowers/{specs,plans}/2026-05-1*-*.md`, `.beads/dolt/*`.

- [ ] **Step 2: Run task pr-prep**

```bash
task pr-prep 2>&1 | tee /tmp/pr-prep.log | tail -50
```

Expected: green (all CI mirror jobs pass). If anything fails, fix it
before continuing — do NOT push a red branch.

- [ ] **Step 3: Verify the bead is still open and ready to close**

```bash
bd show holomush-b2qy
```

Expected: status `open` (we close it after the PR lands).

- [ ] **Step 4: Push the branch**

```bash
jj git fetch
jj rebase -r @ -d main@origin   # targeted, scoped to this change
jj bookmark set adr-capture-b2qy -r @
jj git push --branch adr-capture-b2qy
```

Expected: pushed; PR URL printed or branch ready to PR.

- [ ] **Step 5: Open the PR**

```bash
gh pr create \
  --title "feat: ADR capture skill + hook + agent + bd-id migration" \
  --body "$(cat <<'EOF'
## Summary

Ships the four-artifact ADR-capture system described in
docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md, plus
the one-shot migration of 17 legacy ADRs to the bd-decision-id
canonical filename convention.

## What's new

- `/capture-adrs` skill: extracts ADR-worthy decisions from finalized
  specs/plans into both docs/adr/ files AND bd decision records.
- `adr-extractor` agent: read-only sonnet agent, owns the worthiness
  judgment + transcript scan logic.
- `nudge-adr-capture.sh` PostToolUse hook: emits additionalContext
  JSON when a watched spec/plan is edited without a current marker.
- `adr-migrate.py`: one-shot Python script that ran here once.
- `adr-doctor.sh`: durable health check on `task lint`.
- 17 legacy ADRs renamed to `holomush-XXXX-<slug>.md`; 17 stubs at
  legacy paths preserve external links; READMEs regenerated;
  supersession edge (0007 → 0014) recorded as a bd dep.

## Invariants

19 numbered RFC2119 invariants (INV-A1..A19) defined in the spec,
each with an explicit Surface enum value. adr-doctor.sh asserts the
doctor-tagged ones on `task lint`.

## Test plan

- [x] Hook test harness: 15 cases (passed=15 failed=0)
- [x] Doctor passes post-migration: `task lint:adr` green
- [x] Migration dry-run no-mutation: `jj st` empty after dry-run; bd
  decision count unchanged
- [x] Dogfood: ran /capture-adrs on this spec; surfaced ADRs in the
  PR diff
- [x] task pr-prep green

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Expected: PR URL printed.

- [ ] **Step 6: Run `/autofix <PR#>` for CodeRabbit feedback (if applicable)**

Per the project workflow: CodeRabbit will review the PR. Address its
findings via `/autofix <PR#>` or manually.

- [ ] **Step 7: Close the bead after merge**

After the PR merges:

```bash
bd close holomush-b2qy --reason "Shipped in PR #<N>; skill, hook, agent, migration all live; doctor wired into lint."
```

---

## Self-review checklist

After writing this plan, the author confirmed:

- **Spec coverage:** every component in §"Files modified / created" of
  the spec has at least one task. Hook → tasks 1-5. Agent → 6-7. Skill
  → 8. Migration → 9-13. Doctor → 14-16. Migration run → 17-18.
  Dogfood → 19. Push → 20.
- **Invariant coverage:** every Surface-tagged invariant is enforced
  by an artifact this plan ships:
  - `doctor`-tagged (A2, A4, A5, A12, A13, A14, A15) → adr-doctor.sh
    checks added in tasks 14-15.
  - `hook-test`-tagged (A6, A7, A8, A10-hook, A16) → test harness
    fixtures in tasks 1-4.
  - `skill-test`-tagged (A1, A3, A9, A10-skill) → covered by task 19
    dogfood (skill ran end-to-end).
  - `migration-assert`-tagged (A12, A13, A18) → inline asserts in
    tasks 12-13.
  - `manual`-tagged (A11, A17, A19) → covered by PR review in task 20.
- **Placeholder scan:** no TBD/TODO/"implement later" left in any
  step.
- **Type consistency:** skill, agent, migration use the same slug
  convention (kebab-case, drop stop-words, cap 60 chars); same marker
  format; same `**Decision:** holomush-XXXX` file header.

Plan complete.

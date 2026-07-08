#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# PreToolUse hook: enforce task runner, nudge toward dedicated tools
# HARD-BLOCKS direct go/lint commands (go test/build, golangci-lint, gofmt):
# these have a correct `task` alternative and raw use bypasses task-runner
# setup, so running them is a genuine mistake worth denying.
# SOFT-NUDGES the cat/head/tail/find/grep file utilities (echo to stderr, no
# exit 2). They are frequently the right tool (head -c, tail -f, piping into
# jq, find -exec) and even when Read/Glob would do, a hard block is now a net
# hazard: recent harnesses dispatch tool calls in parallel batches, and an
# exit-2 deny on ONE segment cancels every sibling call in the batch — so a
# `task test … ; tail /tmp/log` verification idiom loses the real test run.
# grep was softened first (see its case); cat/head/tail/find followed for the
# same reason.
# Additionally REDIRECTS main-session inline 'task test|test:int|test:cover|
# lint|build' to the local-check offload agent (deny or nudge per
# OFFLOAD_ENFORCE; '# offload-exempt' escape hatch; subagent calls exempt via
# agent_id) — holomush-drf7b §3.3.
# Error strategy: enforcement hook — fails open on jq/parse errors
# (command proceeds unchecked), but reliably blocks known bad patterns
# (go/lint/fmt) when parsing succeeds.
set -uo pipefail

# --- Parse phase: fail open on malformed input ---
trap 'exit 0' ERR

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null) || {
  echo "enforce-task-runner: failed to parse input — enforcement disabled for this command" >&2
  exit 0
}

[[ -z "$COMMAND" ]] && exit 0

# --- Offload enforcement config (holomush-drf7b §3.3) ---
# Main-session inline `task test|test:int|test:cover|lint|build` is redirected
# to the local-check agent. deny = PreToolUse JSON permission denial (cancels
# the call — NOTE: a deny also cancels sibling calls in a parallel tool batch;
# accepted, since the replacement is an Agent dispatch and verbose runs are
# usually solo). nudge = stderr advisory only. Env-overridable for tests and
# emergencies. Subagent calls (agent_id present) are exempt: offload agents
# and implementer subagents run task freely in their own cheap contexts.
# Escape hatch: append `# offload-exempt` to the command.
OFFLOAD_ENFORCE="${OFFLOAD_ENFORCE:-deny}"   # ← deny default: agent_id split + live deny path verified in a post-merge fresh-workspace session (holomush-afq2t). Env-overridable for tests/emergencies.
AGENT_ID=$(echo "$INPUT" | jq -r '.agent_id // empty' 2>/dev/null) || AGENT_ID=""

# --- Enforcement phase: errors here are bugs, not parse failures ---
trap - ERR

strip_leading_ws() {
  local s="$1"
  echo "${s#"${s%%[![:space:]]*}"}"
}

# Strip leading KEY=value assignments (unquoted, double-quoted, single-quoted).
strip_env_vars() {
  local s="$1"
  while [[ "$s" =~ ^[A-Za-z_][A-Za-z_0-9]*=(\"[^\"]*\"|\'[^\']*\'|[^[:space:]]*)[[:space:]] ]]; do
    s="${s#"${BASH_REMATCH[0]}"}"
    s=$(strip_leading_ws "$s")
  done
  echo "$s"
}

# Strips leading whitespace, all KEY=value env var assignments (including
# quoted values like FOO="bar baz"), shell wrapper prefixes (env, sudo,
# command, exec, nice, nohup) and their flags, then returns the first
# real command word.
#
# Known limitations:
# - KEY=value at end of string without trailing whitespace is not stripped.
first_cmd_word() {
  local segment="$1"
  segment=$(strip_leading_ws "$segment")
  segment=$(strip_env_vars "$segment")
  local word="${segment%% *}"
  # Skip shell wrapper commands, their flags, and subsequent KEY=value pairs
  while [[ "$word" =~ ^(env|command|exec|sudo|nice|nohup)$ ]]; do
    segment="${segment#"$word"}"
    segment=$(strip_leading_ws "$segment")
    while [[ "${segment%% *}" == -* ]]; do
      segment="${segment#"${segment%% *}"}"
      segment=$(strip_leading_ws "$segment")
    done
    segment=$(strip_env_vars "$segment")
    word="${segment%% *}"
  done
  echo "$word"
}

# Split command on chain operators (&& ; ||) and check each segment's
# first word before any pipe. This catches:
#   "go test ./..."           -> blocked (go)
#   "cd foo && go test"       -> blocked (go in second segment)
#   "env go test ./..."       -> blocked (env prefix stripped)
#   "env -i go test ./..."    -> blocked (env flag stripped)
#   "FOO=a BAR=b go test"    -> blocked (multiple env vars stripped)
#   "git log | grep fix"      -> allowed (grep is after pipe)
#   "task test"               -> allowed (task)
#
# Known limitations:
# - Commands inside $(...) or backticks are not inspected.
# - Quoted strings containing && ; || are incorrectly split.
# - || fallback clauses are checked as independent commands, which may
#   produce false positives (e.g., "cmd || cat /dev/null").
# - The control-flow body exemption (depth>0) applies to the offload redirect too: 'for …; do task test; done' is not matched — same accepted hole as the pre-existing go-test block; do not "fix" by changing depth semantics (it exists to kill cat/tail false positives).
# - '# offload-exempt' is a raw-command substring check: a quoted occurrence anywhere in a multi-segment command exempts all its task segments. Accepted: fail-open on an advisory layer.

# Strip single- and double-quoted string contents (across newlines) before
# segment-splitting so commands like `git commit -m 'message contains find
# or rg or cat in the body'` don't false-trigger on lines whose first
# non-quote token happens to match a blocked tool name. Crude — does not
# handle escaped quotes inside quotes — but covers real hook inputs. See
# enforce-bd-beads-dir.sh for the same pattern + caveats.
STRIPPED=$(printf '%s' "$COMMAND" | perl -0777 -pe "s/'[^']*'//g; s/\"[^\"]*\"//g" 2>/dev/null) || STRIPPED="$COMMAND"

# Split on && ; || using awk for portability (BSD sed does not support \n
# in replacement strings). Note: || is consumed by the awk split, so the
# pipe split below never misidentifies || as two separate pipe characters.
SEGMENTS=$(printf '%s' "$STRIPPED" | awk '{gsub(/ *&& */, "\n"); gsub(/ *; */, "\n"); gsub(/ *\|\| */, "\n"); print}')

# Compound control-flow scripts (for/while/until/if/case … do/then … done/fi)
# legitimately compose cat/head/tail/grep/find inside their BODIES. The splitter
# above can't distinguish a loop-body statement (`tail $f` inside `do…done`,
# split off by the loop's own `;`) from a standalone file read — the bulk of
# this hook's false positives. Track control-flow nesting depth and keep only
# TOP-LEVEL segments (depth 0): commands before the block AND after `done`/`fi`
# are still inspected (a trailing `; go test` does NOT escape), but body
# statements are exempt. Detection is segment-LEADING, so a control keyword used
# as an argument (`rg then foo.go`) doesn't trigger it. No-op when there's no
# control flow — every segment is depth 0, so SEGMENTS is unchanged.
top_level=""
depth=0
while IFS= read -r cf_seg; do
  [[ -z "$cf_seg" ]] && continue
  [[ "$depth" -eq 0 ]] && top_level+="$cf_seg"$'\n'
  case "$(first_cmd_word "${cf_seg%%|*}")" in
    for|while|until|if|case) depth=$((depth + 1)) ;;
    done|fi|'esac')          [[ "$depth" -gt 0 ]] && depth=$((depth - 1)) ;;
  esac
done <<< "$SEGMENTS"
SEGMENTS="$top_level"

while IFS= read -r segment; do
  [[ -z "$segment" ]] && continue

  # Get the part before any pipe (first command in a pipeline)
  before_pipe="${segment%%|*}"
  word=$(first_cmd_word "$before_pipe")

  # Also get the second word for "go test" / "go build" distinction
  rest="${before_pipe#*"$word"}"
  rest=$(strip_leading_ws "$rest")
  second_word="${rest%% *}"

  case "$word" in
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
    go)
      case "$second_word" in
        test)
          if echo "$rest" | command grep -qE '(^|\s)-tags[= ]'; then
            echo "Use 'task test:int' instead of 'go test -tags=...'" >&2
          else
            echo "Use 'task test' instead of 'go test'" >&2
          fi
          exit 2
          ;;
        build)
          echo "Use 'task build' instead of 'go build'" >&2
          exit 2
          ;;
      esac
      ;;
    golangci-lint)
      echo "Use 'task lint' instead of 'golangci-lint'" >&2
      exit 2
      ;;
    gofmt|goimports)
      echo "Use 'task fmt' instead of '$word'" >&2
      exit 2
      ;;
    grep|egrep|fgrep)
      # Soft nudge, NOT a block (no exit 2): the built-in Grep tool the old
      # message pointed at isn't always provisioned in the deferred-tool /
      # ToolSearch session model, so a hard block could dead-end. rg is always
      # available in Bash; probe MCP returns whole AST blocks for Go symbol
      # reads; ast-grep matches by code structure for patterns/codemods.
      echo "Nudge: prefer 'rg' over $word (faster, .gitignore-aware). For Go symbol/AST reads use mcp__probe__search_code; for structural patterns or codemods use ast-grep. ($word still runs.)" >&2
      ;;
    cat)
      # Soft nudge, NOT a block (no exit 2) — see header for the parallel-batch
      # cascade rationale. Stay silent for heredocs (cat <<EOF, cat<<EOF,
      # cat -s <<EOF) and /dev paths (cat /dev/null), which have no Read-tool
      # equivalent at all.
      if ! echo "$before_pipe" | command grep -qE "(<<|/dev/)"; then
        echo "Nudge: prefer the Read tool over cat for file reads (offset/limit for ranges). cat still runs." >&2
      fi
      ;;
    head|tail)
      # Soft nudge, NOT a block (no exit 2) — see header. head -c (byte limit),
      # tail -f, and piping (cmd | tail -N) have no Read-tool equivalent.
      echo "Nudge: prefer the Read tool with offset/limit over $word for file reads. $word still runs." >&2
      ;;
    find)
      # Allow find when used with predicates Glob can't express:
      # time-based (-mtime/-atime/-ctime/-mmin/-amin/-cmin/-newer/-newermt),
      # metadata-based (-size/-perm/-user/-group/-uid/-gid/-empty),
      # or actions (-exec/-delete/-printf). Block plain "find . -name '*.x'"
      # patterns since Glob handles those.
      if echo "$rest" | command grep -qE -- '(-(m|a|c)(time|min)|-newer|-size|-perm|-user|-group|-uid|-gid|-empty|-exec|-delete|-printf|-fprint|-iname|-iwholename|-i?regex|-maxdepth|-mindepth|-prune|-follow|-xdev)\b'; then
        :  # predicate Glob can't express — stay silent
      else
        # Soft nudge, NOT a block (no exit 2) — see header.
        echo "Nudge: prefer the Glob tool over find for plain name/path matches (find still runs; add -mtime/-newer/-size/-exec/etc. for predicates Glob can't express)." >&2
      fi
      ;;
  esac
done <<< "$SEGMENTS"

exit 0

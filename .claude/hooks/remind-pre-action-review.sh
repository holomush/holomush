#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# UserPromptSubmit hook: scans the user's prompt for intent verbs that imply a
# pre-push, pre-execute, or pre-plan reviewer gate, and injects a short
# reminder so the assistant invokes the right adversarial sub-agent before
# acting. Soft nudge — does not block.
#
# Output contract: emit additionalContext text on stdout. Stay silent (exit 0)
# when no trigger matches.

set -euo pipefail

# Read the JSON event from stdin. We only need .prompt; jq keeps us safe from
# embedded newlines and quotes in user input.
prompt="$(jq -r '.prompt // ""')"

# Lowercase once for cheap case-insensitive matching.
lower="$(printf '%s' "$prompt" | tr '[:upper:]' '[:lower:]')"

reminders=()

# code-reviewer triggers: anything that implies the work is leaving the session.
if printf '%s' "$lower" | grep -qE '(\bpush\b|jj[[:space:]]+git[[:space:]]+push|git[[:space:]]+push|gh[[:space:]]+pr[[:space:]]+create|bd[[:space:]]+close|open[[:space:]]+(a[[:space:]]+)?pr|create[[:space:]]+(a[[:space:]]+)?pr|\bmerge\b|\bship\b|\bland\b|ready[[:space:]]+to[[:space:]]+(push|merge|ship)|close[[:space:]]+(the[[:space:]]+)?bead|mark[[:space:]]+(done|complete)|wrap[[:space:]]+up|finalize)'; then
  reminders+=("**Pre-hand-off gate:** Before you run \`jj git push\`, \`gh pr create\`, or \`bd close\` for this work, the \`code-reviewer\` adversarial sub-agent MUST run on the branch diff. Invoke \`/review-code\` (or \`Agent\` with \`subagent_type: code-reviewer\`) now if it has not already run for the current branch tip. To skip, the user must explicitly say so (e.g. \"skip review\").")
fi

# plan-reviewer triggers: anything that implies a plan is about to be executed.
if printf '%s' "$lower" | grep -qE '(execute[[:space:]]+(the[[:space:]]+)?plan|run[[:space:]]+(the[[:space:]]+)?plan|start[[:space:]]+implementing|begin[[:space:]]+(the[[:space:]]+)?plan|plan[[:space:]]+is[[:space:]]+ready|approve[[:space:]]+(the[[:space:]]+)?plan|\bapproved\b)'; then
  reminders+=("**Pre-execute gate:** Before \`superpowers:executing-plans\` or \`superpowers:subagent-driven-development\` consumes a plan, the \`plan-reviewer\` adversarial sub-agent MUST run on it. Invoke \`/review-plan\` now if it has not already run for the latest revision of the plan.")
fi

# design-reviewer triggers: anything that implies a spec is about to be planned.
if printf '%s' "$lower" | grep -qE '(write[[:space:]]+(the[[:space:]]+)?plan|plan[[:space:]]+this|spec[[:space:]]+is[[:space:]]+ready|ready[[:space:]]+for[[:space:]]+planning|design[[:space:]]+is[[:space:]]+done|approve[[:space:]]+(the[[:space:]]+)?design|\bapproved\b)'; then
  reminders+=("**Pre-plan gate:** Before \`superpowers:writing-plans\` is invoked on a spec, the \`design-reviewer\` adversarial sub-agent MUST run on it. Invoke \`/review-design\` now if it has not already run for the latest revision of the spec.")
fi

if [ ${#reminders[@]} -eq 0 ]; then
  exit 0
fi

# Print all matched reminders. Each is its own paragraph.
printf '## Reviewer-gate reminder (auto-injected)\n\n'
for r in "${reminders[@]}"; do
  printf '%s\n\n' "$r"
done

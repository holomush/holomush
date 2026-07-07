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

# Best-effort: collect changed files vs trunk so domain reminders can fire when
# the prompt is generic ("push this branch") but the diff actually touches
# crypto/ABAC paths. Silent on failure (no jj/git, detached state, etc.).
changed_paths=""
if command -v jj >/dev/null 2>&1; then
  changed_paths="$(jj diff --name-only --from 'trunk()' --to '@' 2>/dev/null || true)"
fi
if [ -z "$changed_paths" ] && command -v git >/dev/null 2>&1; then
  base="$(git merge-base HEAD origin/main 2>/dev/null || true)"
  if [ -n "$base" ]; then
    changed_paths="$(git diff --name-only "$base"...HEAD 2>/dev/null || true)"
  fi
fi

reminders=()

# Detect handoff-intent verbs (push/ship/close/PR creation). Cached so multiple
# downstream gates can reuse it without re-running grep.
handoff_intent=0
if printf '%s' "$lower" | grep -qE '(\bpush\b|jj[[:space:]]+git[[:space:]]+push|git[[:space:]]+push|gh[[:space:]]+pr[[:space:]]+create|bd[[:space:]]+close|open[[:space:]]+(a[[:space:]]+)?pr|create[[:space:]]+(a[[:space:]]+)?pr|\bmerge\b|\bship\b|\bland\b|ready[[:space:]]+to[[:space:]]+(push|merge|ship)|close[[:space:]]+(the[[:space:]]+)?bead|mark[[:space:]]+(done|complete)|wrap[[:space:]]+up|finalize)'; then
  handoff_intent=1
fi

# code-reviewer triggers: anything that implies the work is leaving the session.
if [ "$handoff_intent" = "1" ]; then
  reminders+=("**Pre-hand-off gate:** Before you run \`jj git push\`, \`gh pr create\`, or \`bd close\` for this work, the \`code-reviewer\` adversarial sub-agent MUST run on the branch diff. Invoke \`/holomush-dev:review-code\` (or \`Agent\` with \`subagent_type: code-reviewer\`) now if it has not already run for the current branch tip. To skip, the user must explicitly say so (e.g. \"skip review\").")
fi

# crypto-reviewer triggers: handoff intent AND (crypto-domain mention in prompt
# OR crypto-domain paths in the diff). Path-based detection closes the gap
# where a generic handoff like "push this branch" would otherwise miss the
# crypto gate when the diff actually touches the crypto surface. Combines
# with the code-reviewer reminder so the user gets BOTH: crypto-reviewer
# first, then code-reviewer.
if [ "$handoff_intent" = "1" ] && {
     printf '%s' "$lower" | grep -qE '(internal/eventbus/crypto|internal/eventbus/codec|internal/eventbus/history/dispatcher|internal/eventbus/history/cold_postgres|internal/plugin/event_emitter|internal/eventbus/audit/projection|crypto_keys|events_audit|\baad\b|\bdek\b|authguard|rekey|encrypt|decrypt|crypto\.emits)' ||
     printf '%s' "$changed_paths" | grep -qE '(internal/eventbus/crypto/|internal/eventbus/codec/|internal/eventbus/history/dispatcher\.go|internal/eventbus/history/cold_postgres\.go|internal/plugin/event_emitter\.go|internal/eventbus/audit/projection\.go|migrations/.*crypto_keys|migrations/.*events_audit)';
   }; then
  reminders+=("**Pre-hand-off crypto gate:** Changes touch the event-payload-cryptography surface. \`crypto-reviewer\` MUST run BEFORE \`code-reviewer\`. Invoke \`/holomush-dev:review-crypto\` (or \`Agent\` with \`subagent_type: crypto-reviewer\`) first; address NOT-READY findings; then invoke \`/holomush-dev:review-code\`. To skip, the user must explicitly say so (e.g. \"skip crypto review\").")
fi

# abac-reviewer triggers: handoff intent AND (access-control mention in prompt
# OR access-control paths in the diff). Same OR pattern as crypto so the gate
# fires even when the prompt is generic but the diff touches internal/access/.
if [ "$handoff_intent" = "1" ] && {
     printf '%s' "$lower" | grep -qE '(internal/access|access[[:space:]]*control|abac|access[[:space:]]+policy|policy[[:space:]]+dsl|attribute[[:space:]]+provider|access[[:space:]]+decision)' ||
     printf '%s' "$changed_paths" | grep -qE '(internal/access/)';
   }; then
  reminders+=("**Pre-hand-off ABAC gate:** Changes touch the access-control surface. \`abac-reviewer\` MUST run alongside \`code-reviewer\`. Invoke \`/holomush-dev:review-abac\` (or \`Agent\` with \`subagent_type: abac-reviewer\`) before pushing. To skip, the user must explicitly say so (e.g. \"skip ABAC review\").")
fi

# int/e2e surface nudge: handoff intent AND the diff touches integration/E2E
# paths. Integration+E2E are CI-required (not a local mandatory gate), but a
# targeted local run before push catches breakage a CI round-trip slower.
if [ "$handoff_intent" = "1" ] && printf '%s' "$changed_paths" | grep -qE '(test/integration/|web/e2e/|_integration_test\.go|\.spec\.ts)'; then
  reminders+=("**int/e2e surface touched:** \`Integration Test\` / \`E2E Test\` are CI-required checks. A targeted local run before push (\`task test:int -- ./<domain>\` or \`task pr-prep:full\`) catches failures a CI round-trip sooner — recommended, not mandatory (CI is authoritative).")
fi

# local-check offload triggers: prompt asks to run tests/lint/build. Remind to
# dispatch the offload agent rather than running the task inline (holomush-drf7b §3.4).
if printf '%s' "$lower" | grep -qE '(run[[:space:]]+(the[[:space:]]+)?((integration|unit)[[:space:]]+)?tests?\b|does[[:space:]]+it[[:space:]]+(build|compile)\b|check[[:space:]]+(the[[:space:]]+)?lint\b|run[[:space:]]+lint\b|check[[:space:]]+(the[[:space:]]+)?(test[[:space:]]+)?coverage\b|is[[:space:]]+(it|the[[:space:]]+build)[[:space:]]+green)'; then
  reminders+=("**Offload reminder:** dispatch the \`local-check\` agent (\`subagent_type: local-check\`, prompt: \`<test|lint|build|int|cover> [args]\`) instead of running \`task test\`/\`task lint\`/\`task build\` inline — inline runs are hook-enforced in the main session (\`# offload-exempt\` to override).")
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

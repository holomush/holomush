---
name: review-code
description: Adversarially review uncommitted or branch-local code before push
---

@agent-code-reviewer Review the code changes described below against the
upstream sources of truth.

**Target:** $ARGUMENTS

**If no target was given:** review the full branch diff against the merge
base. Use `jj diff --from $(jj log -r 'trunk()' --no-graph -T commit_id --limit 1)`
(or the git equivalent via the `jj:jujutsu` skill's guidance for the current
repo state) to get the diff. Review the diff AND the full files it touches.

**If a target was given:** treat it as either a path (review that file's
changes vs merge base), a commit revset (review that revset's diff), or a
PR number (fetch with `gh pr diff <n>` and review).

**Before writing findings:** read the upstream artifact — the beads issue
this work claims to close (from the branch name, commit messages, or
`bd list --status=in_progress`) and any spec it references. You MUST know
what "done" means before judging done-ness.

Apply the adversarial stance and grounding discipline from your system
prompt. Run the external-grounding MCPs (`context7`, `deepwiki`, `exa`) for
any library/API/version claims. Produce the standard findings report with a
binary READY / NOT READY verdict.

**On receipt:** the agent's response IS the full report — read it. The agent
also persists the full report to `.claude/agent-memory/code-reviewer/reports/`.
Do NOT spawn a second agent to "retrieve full findings"; subagents are
stateless across invocations and a fresh one has nothing to retrieve. If the
response looks truncated, read the persisted file from disk directly with
`Read`.

---
description: Adversarially review an implementation plan against its spec before execution
---

@agent-plan-reviewer Review the implementation plan described below against
the spec it claims to implement.

**Target plan:** $ARGUMENTS

**If no target was given:** review the most recently modified plan file in
`docs/superpowers/plans/` or `docs/plans/`.

**Before writing findings:** the plan MUST reference a spec in its header.
Read that spec in full before touching the plan. You MUST know what the plan
is supposed to solve before you can judge whether it solves it.

Apply the adversarial stance and grounding discipline from your system
prompt. Run the traceability passes (spec → plan, plan → spec), the
definition-of-done pass, the decomposition pass, and the repo-reality pass.
Produce the standard findings report with a binary READY / NOT READY
verdict.

**On receipt:** the agent's response IS the full report — read it. The agent
also persists the full report to `.claude/agent-memory/plan-reviewer/reports/`.
Do NOT spawn a second agent to "retrieve full findings"; subagents are
stateless across invocations and a fresh one has nothing to retrieve. If the
response looks truncated, read the persisted file from disk directly with
`Read`.

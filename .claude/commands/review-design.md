---
description: Adversarially review a design spec before a plan is written for it
---

@agent-design-reviewer Review the design spec described below against its
stated goals and repo reality.

**Target spec:** $ARGUMENTS

**If no target was given:** review the most recently modified spec file in
`docs/superpowers/specs/` or `docs/specs/`.

**Before writing findings:** the spec MUST state a problem. Extract and hold
that in mind before reading the design. You MUST know what problem the spec
is supposed to solve before you can judge whether it solves it.

Apply the adversarial stance and grounding discipline from your system
prompt. Run the required-section pass, the testability pass, the consistency
pass, the repo-reality pass (including overlap-with-existing-specs), and the
scope pass. Produce the standard findings report with a binary READY /
NOT READY verdict.

**On receipt:** the agent's response IS the full report — read it. The agent
also persists the full report to `.claude/agent-memory/design-reviewer/reports/`.
Do NOT spawn a second agent to "retrieve full findings"; subagents are
stateless across invocations and a fresh one has nothing to retrieve. If the
response looks truncated, read the persisted file from disk directly with
`Read`.

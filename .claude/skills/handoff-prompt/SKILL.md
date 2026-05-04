---
name: handoff-prompt
description: |
  Generate a self-contained briefing prompt that a fresh Claude session can
  use to pick up a piece of work without inheriting the current session's
  context. Use when the user asks for "a handoff", "kickoff prompt", "session
  starter", "resume prompt", or describes wanting to spin up a separate
  session for a specific bead, epic, or task. Both Claude and the user can
  invoke; output is markdown text the user copy-pastes into a new session.
---

# Handoff Prompt

Generate a paste-ready briefing for a fresh Claude session. The pattern
recurs whenever a piece of work is large enough to warrant its own session
and needs full context bootstrapped without inheriting the current
conversation history.

## When to invoke

- User says: "give me a handoff prompt", "kickoff prompt for that", "a prompt
  to start it in a new session", "session resume for X", "spin up X in its
  own session"
- User completes one phase and wants to start the next one fresh (clean
  context budget, independent review cadence)
- A bead is large enough to need its own brainstorm → spec → plan → execute
  cycle and the work shouldn't ride alongside other in-flight work in the
  current session

The handoff is FOR a new Claude session that has zero context from the
current one. Treat it as briefing a smart colleague who just walked into
the room.

## Inputs

- **Bead ID** (primary anchor): the `bd` issue the new session will work on.
  If a bead doesn't exist yet, ask whether to file one first.
- **Optional: spec / design doc path** if the work has one already.
- **Optional: plan path** if the work has one already.
- **Optional: cross-cutting surface notes** the user wants to surface.

## Workflow

### Step 1: Gather grounding from the current session

Read the bead in full:

```bash
bd show <bead-id>
```

If the bead references a design doc, plan, or other beads, follow those
references and pull what's relevant. Don't deep-dive — extract enough that
the prompt is self-contained without becoming the work itself.

If the work spans multiple beads (epic + children, or a chain), capture the
chain structure briefly.

### Step 2: Identify the cross-cutting surface

Run targeted greps to map the file surface the new session will touch:

```bash
rg -l "<key-symbol>" --type=go --type=proto --type=ts --type=md
```

The handoff should name the specific files, not a vague "the auth module".
Examples that worked well in prior handoffs:

- Concrete file lists with line citations: `internal/eventbus/publisher.go:316,431`
- Verb-and-symbol map: "drop `LegacyId` field; update consumer paths at X, Y, Z"
- Pre-read order — which docs to load before any code action

### Step 3: Surface design questions

What decisions does the new session need to make? List them as numbered
questions, NOT as pre-decided answers. Examples that recur:

- Where does X live? (new column vs new table vs new package)
- Timing of Y? (at install vs at first use vs on demand)
- Backwards compat for Z? (drop, deprecate, dual-write)
- Wire-format transition: one PR or staged?
- Migration of existing data: required, optional, or skip?

The questions are the framing. The new session's brainstorming pass is what
answers them.

### Step 4: Restate workflow + safety

Every handoff includes a workflow block that names the project's
conventions:

- Workspace isolation (`task workspace:new -- <name>`)
- The brainstorm → spec → plan → execute cycle with adversarial review gates
  between phases (design-reviewer before plan-writing, plan-reviewer before
  execution); code-reviewer + crypto-reviewer + abac-reviewer run as
  PRE-PUSH gates, not between phases
- Beads-driven tracking (no TodoWrite)
- Bead chain creation via `bead-chain-from-plan` skill if applicable
- `task pr-prep` before push

This keeps the new session from re-discovering the project's gates.

### Step 5: List explicit out-of-scope items

The new session will encounter related work and feel pulled to fix it.
Pre-empt that by listing what's deliberately not part of this scope. Recent
examples:

- "Reworking core.Actor itself beyond what legacy_id removal needs"
- "Anything in the broader Phase 4-7 crypto epic line"
- "Integration tests covering legacy_id behavior in pre-Phase-3a paths"

If the work has known follow-up beads filed, name them so the new session
doesn't re-file duplicates.

### Step 6: Surface mitigating context / safety nets

What's already in place that makes this work safer than it might appear?
Examples:

- "Phase 3d's envelope persistence means existing audit rows ARE readable
  indefinitely with their legacy_id intact"
- "No external API consumers depend on this surface"
- "The Phase 3d code-review locked the regression class via
  TestColdPostgresUnmarshalsEnvelope"

This is morale-preserving and honest about risk.

### Step 7: Format the prompt

Output as a single markdown code block (text fence) so the user can copy
the whole thing in one paste. The prompt itself uses subheadings and bullet
lists for the new session's readability.

Standard structure:

````markdown
```text
Starting <work title> — `<bead-id>` (<priority>, <epic-or-task>). <One-line
context: why now, why single-stream-or-parallel, what triggers this.>

**Why this is needed:**

<2-3 sentence motivation. Reference the source decision (grounding doc,
issue, prior PR review). Be specific about what failure mode this prevents
or what capability it unlocks.>

**Read first (bead descriptions are sources of truth):**

  bd show <bead-id>          # this work
  bd show <related-bead>     # closely related; verify scope overlap

**Cross-cutting surface (verify scope by grepping at design time):**

  rg -n "<key-symbol>" --type=<lang>

Per the bead description, at minimum these need updates:

  - <file>:<line> — <one-line what>
  - <file> — <one-line what>
  - ...

**Key design questions to surface in brainstorming:**

  1. <question> — <hint at the trade-off space, not the answer>
  2. <question> — ...
  3. ...

**Workflow per project conventions (CLAUDE.md / AGENTS.md):**

  1. Set up isolated workspace:
       task workspace:new -- <session-name>
       cd <printed-path>

  2. Use `bd ready` to claim, work via beads, never TodoWrite.

  3. Brainstorm → spec → plan → execute, with adversarial review gates
     between each step:
       superpowers:brainstorming  (then design-reviewer agent)
       superpowers:writing-plans  (then plan-reviewer agent)
       bead-chain-from-plan       (materialize the chain in beads)
       superpowers:subagent-driven-development
     Plus pre-push code-reviewer + crypto-reviewer (when crypto-touching)
     and `task pr-prep`.

  4. The Plan → Bead Chain convention applies (CLAUDE.md / AGENTS.md):
     parent epic gets task children created from the impl plan; each task
     bead's description includes the 8 sections (Goal / Design ref / Plan
     ref / TDD acceptance / Verification / Files touched / Dependencies /
     Out of scope). Use `bead-chain-design` to write the chain section into
     the plan, then `bead-chain-from-plan` to materialize.

**Out of scope for <bead-id> (don't accidentally bundle):**

  - <out-of-scope item 1>
  - <out-of-scope item 2>
  - ...

**Mitigating context:**

  - <safety net 1>
  - <safety net 2>
  - ...

Start with `superpowers:brainstorming` to surface the design questions
listed above before touching any code.
```
````

### Step 8: Show the user

Print the prompt as the markdown code block above, with no editorial
commentary inside the block. After the block, briefly note:

- The bead ID and any related beads referenced
- That the new session should be started in a fresh workspace
  (`task workspace:new -- <suggested-name>`)
- Whether any of the design decisions have known leaning answers worth
  flagging in the prompt vs. leaving open

## Constraints

- **No editorial content INSIDE the prompt block.** The prompt is for the
  new session, not the current one. Any "this is hard because..." comments
  go after the block, in conversation with the current user.
- **Keep the prompt under ~80 lines** when reasonable. A new session reading
  a 200-line briefing is being micromanaged. Trust the brainstorming skill
  to expand.
- **Never embed credentials, tokens, or local-machine paths** like
  `~/.config/...` in the prompt.
- **Cite the source decision** for any "this is needed because..." claim —
  link the grounding doc, the bead, or the PR review.
- **Don't pre-decide design questions** that the new session should answer.
  Surface the trade-off space; let brainstorming pick.
- **Respect the project's session-isolation discipline** — every handoff
  recommends `task workspace:new -- <name>` for the new session.
- **Match the structure in Step 7.** The shape was extracted from two
  recent kickoff prompts (Phase 3d resume; `legacy_id` elimination
  kickoff) and codified here as the convention going forward. New
  handoffs should follow the same shape so readers develop muscle memory;
  if the shape doesn't fit a particular work item, surface that to the
  user rather than silently diverging.

## Origin

The shape codified in Step 7 was extracted from two ad-hoc kickoff prompts
written by hand in May 2026:

1. **Phase 3d resume** (2026-05-03) — briefed a fresh session on resuming
   Phase 3d work after Phase 3c had merged. Included current bead state,
   scope per spec, workspace setup, parallel-eligible work, project
   conventions, memory hooks worth recalling.

2. **`legacy_id` elimination kickoff** (2026-05-04) — briefed a fresh
   session on starting a top-level tech-debt epic. Included motivation,
   read-first list, cross-cutting surface (file map), design questions,
   workflow per conventions, out-of-scope list, mitigating context.

Both prompts shared a recognizable structure that this skill formalizes.
Neither was a tested template at the time; the skill is the first place
where the structure is named as the project's handoff convention.

---
name: bead-chain-design
description: |
  Generate the `## Bead chain structure` section into an implementation plan
  that lacks one. Implements the Plan → Bead Chain convention defined in
  CLAUDE.md / AGENTS.md "Plan → Bead Chain". Use after
  `superpowers:writing-plans` produces a plan, BEFORE `bead-chain-from-plan`
  is invoked. The skill reads the plan's task table and parent epic context,
  proposes a chain mapping (1:1 or merged or split), generates each task
  bead's full 8-section description, and writes the section into the plan
  (or a sidecar file when the plan is read-only).

  Both Claude and the user can invoke. ALWAYS preview the proposed chain
  shape and obtain explicit user approval before writing anything to disk.
---

# Bead Chain Design

Translate an implementation plan's task table and parent-epic context into
the `## Bead chain structure` section that the project's Plan → Bead Chain
convention requires (CLAUDE.md / AGENTS.md). This is the missing-skill step
between `superpowers:writing-plans` (produces the plan body) and
`bead-chain-from-plan` (materializes the chain into `bd` issues).

## When to invoke

- Right after `superpowers:writing-plans` saves a plan and the user wants to
  track the work in beads
- When `bead-chain-from-plan` reports the plan lacks a `## Bead chain
  structure` section and asks to generate one
- When the user says "add the bead chain", "write the chain section",
  "design the bead chain", "generate the chain structure"

The skill does not auto-fire. The user (or controller agent) explicitly
invokes it; this avoids designing chains for plans that may still be in
revision.

## Inputs

- **Plan path** (positional arg or detected from recent context): a markdown
  file under `docs/superpowers/plans/` or `docs/plans/`.
- **Parent epic ID** (optional): the existing `bd` epic the chain hangs from.
  If absent, the skill infers from the plan's `Refs:` lines or the recent
  conversation. If still ambiguous, ask the user.

If neither is supplied, ask the user which plan and which epic to design
against.

## Workflow

### Step 1: Read the plan + parent epic

Read the entire plan file. Locate:

1. The plan's task table (typically the `## Decomposition & sequencing` or
   `## Tasks` section, with rows naming task IDs like `T1`, `T2`, etc.).
2. The plan's "Files touched" / architecture table — used to populate each
   task bead's "Files touched" section.
3. The grounding doc / design spec the plan references (typically named in
   the plan header `**Spec reference:**`).
4. The parent epic via `bd show <parent-epic-id>` — used to confirm the
   chain has an owning epic and to extract description context for the
   epic-level Refs.

If any input is missing or ambiguous, surface it and ask before continuing.

### Step 2: Propose the bead split

Map plan tasks to bead beads. Three common shapes:

| Shape         | When to use                                                           | Example                                                  |
| ------------- | --------------------------------------------------------------------- | -------------------------------------------------------- |
| **1:1**       | Each plan task = one bead. Default for moderate-granularity plans.    | T1 → ojw1.4.3; T2 → ojw1.4.4; ...                        |
| **Merged**    | Multiple closely-coupled plan tasks share one bead.                   | T1 + T2 + T3 (all column-rename steps) → ojw1.4.3        |
| **Split**     | One plan task spans multiple beads with their own review surfaces.    | T6+T7+T8 (proto + goplugin + Lua) → ojw1.4.1.1, .2, .3   |

The split decision is informed by:

- Whether the tasks share a single `task pr-prep` cycle (merged) or
  separate review surfaces (split)
- Bead numbering depth — four-level (e.g. `ojw1.4.1.3`) is supported in this
  project but should be reserved for cases with genuine three-level
  hierarchy (epic → sub-epic → task)
- Plan-task dependencies — if T2 depends only on T1 and they hit the same
  files, merging is fine; if T2 depends on T1 but they hit different review
  surfaces, split

Propose the mapping as a table for user review:

```text
Proposed bead split for <plan-path>

| Plan task | Bead | Title                                            | Notes        |
|-----------|------|--------------------------------------------------|--------------|
| T0        | -    | (plan-time pre-state verification, no bead)      | housekeeping |
| T1+T2+T3  | <p>.3 | Cold-tier envelope rename + dispatcher refactor | merged       |
| T4        | <p>.4 | Crypto.Enabled default flip                      | 1:1          |
| ...       |       |                                                  |              |
```

Where `<p>` is the parent epic ID (e.g. `holomush-ojw1.4`). Ask the user
to approve, modify, or reject the split before continuing.

### Step 3: Generate per-bead descriptions

For each bead in the approved split, generate the 8-section description per
CLAUDE.md "Plan → Bead Chain" requirements:

1. **Goal** — one-sentence scope, derived from the plan task's title /
   purpose
2. **Design reference** — the grounding spec / design doc named in the plan
   header, with the most-relevant section anchor
3. **Plan reference** — the plan path with the task ID anchor
   (`docs/superpowers/plans/<plan>.md` § Task Tn)
4. **TDD acceptance criteria** — pulled from the plan's task TDD steps; for
   merged beads, the union of the merged tasks' tests
5. **Verification steps** — pulled from the plan's task verification block;
   typically `task lint`, `task test -- ./pkg/`, `task pr-prep` for the
   closing bead
6. **Files touched** — pulled from the plan's architecture table for the
   relevant tasks
7. **Dependencies** — derived from the plan task's dependency column and
   translated to `bd dep add` edges using the bead IDs from Step 2
8. **Out of scope** — explicit non-goals; pulled from the plan's "Out of
   scope" section if present, otherwise generated from the design doc's
   deferred items

Generate descriptions in heredoc form so they're directly usable by
`bd create`:

```bash
bd create \
  --title "<title>" \
  --type <task|feature|bug|epic> \
  --priority <0-4> \
  --parent <parent-epic-id> \
  --description "$(cat <<'EOF'
**Goal:** <one-sentence>

**Design reference:** <doc>:<section>
**Plan reference:** <plan>:<task-id>

**TDD acceptance criteria:**
- <test name 1>
- <test name 2>
- ...

**Verification steps:**
- task lint
- task test -- ./<pkg>/
- ...

**Files touched:**
- <path>:<line> — <one-line what>
- ...

**Dependencies:** <list of bead IDs this depends on>

**Out of scope:** <explicit non-goals>
EOF
)"
```

### Step 4: Compose the section

Assemble the generated content into a `## Bead chain structure` section
following the Phase 3d grounding doc's shape:

```markdown
## Bead chain structure

\`\`\`text
<parent-epic>                   (existing epic — <one-line scope>)
└── <parent-epic>.<n>           (existing or NEW — <one-line scope>)
    ├── <child-1>               <one-line scope>
    ├── <child-2>               <one-line scope>
    │   • Splits into <child-2>.<n.m> sub-beads (per the plan)
    └── ...
\`\`\`

<Per-bead `bd create` blocks, one per child, in the order from the tree>

### Closing-out operations

- Existing beads to close with rationale: <list>
- Existing beads to update (priority / parent / description): <list>
- Follow-up beads to file: <list>

### `bd dep add` edges

\`\`\`bash
bd dep add <child-X> <child-Y>   # X depends on Y
...
\`\`\`
```

The section is self-contained: a reader can take this section alone and
hand it to `bead-chain-from-plan` for materialization.

### Step 5: Get user approval (REQUIRED)

First **resolve the destination** using the rules in Step 6 (preferred:
`<plan-path>` itself; sidecar `<plan-path-without-ext>-bead-chain.md` only if
the plan is read-only or the user has asked for a sidecar). The approval prompt
MUST name the resolved destination so the user's `yes` covers the exact file
that will be mutated.

Display the assembled section in full, then ask explicitly with the resolved
destination interpolated:

> "Write this `## Bead chain structure` section into `<resolved-destination>`?
> (yes / no / modify)"
>
> - `yes` — write to `<resolved-destination>`
> - `no` — exit without changes
> - `modify` — describe edits (drop a bead, change a description, adjust
>   the split). After edits, re-display and prompt again with the same
>   question. Only proceed to write when the user replies `yes` against
>   the current state.

Do NOT write anything to disk without affirmative `yes` against the displayed
content AND the named destination.

### Step 6: Write the section

Write to the destination resolved in Step 5. Insertion points (in order of preference):

1. After the plan's `## Decomposition & sequencing` section (the natural
   spot — task table flows into bead chain)
2. After `## Architecture & components touched` if no Decomposition section
   exists
3. As a new section before any closing `## References` or `## Out of scope`
4. Append to end of file as last resort

If the plan file is read-only or the user prefers a sidecar:

- Write to `<plan-path-without-ext>-bead-chain.md` (sibling file, same
  directory, `-bead-chain` suffix before `.md`)
- The `bead-chain-from-plan` skill reads the sidecar if the plan itself
  lacks the section

After writing, run `task lint:markdown` to verify rumdl passes.

### Step 7: Hand off

Print:

```text
✓ Bead chain structure written to <path>
  Lines added: <N>
  Beads designed: <count>
  Edges: <count>

Next: invoke `bead-chain-from-plan` to materialize the chain into bd issues.
```

## Constraints

- **Never write without explicit user approval.** Step 5 confirmation is
  load-bearing. The `modify` flow MUST re-display the updated content and
  re-ask before write.
- **Never invent design content.** Every "Design reference" link, every
  "Files touched" path, every TDD acceptance criterion MUST come from the
  plan or the referenced grounding doc. If the plan lacks the data, ask
  the user rather than fabricating.
- **Never skip the 8 sections.** Every task bead's description MUST have
  all 8. If the plan doesn't supply enough information for one of the
  sections, surface the gap to the user — don't invent and don't omit.
- **Match parent-epic numbering.** Sub-bead IDs follow the existing project
  pattern (e.g. children of `holomush-ojw1.4` are `holomush-ojw1.4.1`,
  `.4.2`, etc.). Verify the next-available index via
  `bd show <parent-epic>` before assigning.
- **Don't pre-create beads.** This skill writes the SECTION; it doesn't run
  `bd create`. Bead materialization is `bead-chain-from-plan`'s job. Keep
  the responsibilities separate so the user can review the section before
  any `bd` mutation.
- **Idempotency**: if the plan already has a `## Bead chain structure`
  section, surface the conflict and ask whether to (a) overwrite, (b)
  merge, or (c) abort. Default: abort.

## Failure modes

- **Plan task table missing or ambiguous**: ask the user to specify the
  task list (or refine the plan's task structure first).
- **Parent epic not found**: ask the user for the correct epic ID, or for
  permission to create a new one.
- **Grounding doc not referenced in plan**: ask the user for the path; the
  Design reference cannot be fabricated.
- **Plan describes non-bead-tracked work** (e.g., a one-off doc fix with no
  multi-task structure): tell the user the Plan → Bead Chain convention
  applies to multi-task plans only; suggest skipping bead chain for this
  plan.
- **`task lint:markdown` fails after write**: surface the error; ask the
  user whether to attempt auto-fix (`task fmt:markdown`) or revert.

## Example session

User says: *"Generate the bead chain structure for
`docs/superpowers/plans/2026-05-15-legacy-id-elimination.md` under epic
`holomush-w9ml`."*

Skill:

1. Reads the plan, finds the task table at `## Decomposition & sequencing`
   with 9 tasks (T0-T8).
2. Reads `bd show holomush-w9ml` for parent epic context.
3. Proposes 1:1 split for T1-T8 (T0 is plan-housekeeping, no bead).
4. Shows the proposed split table; asks for approval.
5. User: "merge T2+T3 (both proto schema work); split T7 into three sub-beads
   for proto, registry, ABAC migration".
6. Re-displays the revised split.
7. User: "yes".
8. Generates 8-section descriptions for each bead, pulling test names from
   the plan's TDD blocks and file lists from the architecture table.
9. Composes the `## Bead chain structure` section.
10. Displays the assembled section in full; asks to write.
11. User: "yes".
12. Appends to the plan after `## Decomposition & sequencing`.
13. Runs `task lint:markdown`; reports success.
14. Hands off to `bead-chain-from-plan` for materialization.

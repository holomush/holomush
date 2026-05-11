---
name: bead-chain-from-plan
description: |
  Materialize the `## Bead chain structure` section of an implementation plan
  into actual `bd` issues, dependency edges, and parent linkages, per the
  Plan → Bead Chain convention defined in CLAUDE.md / AGENTS.md. Use after
  the plan has both a task body AND the chain section. If the plan lacks
  the chain section, this skill delegates to `bead-chain-design` to generate
  it first, then materializes it.

  Use whenever the user asks to "create the beads", "set up the bead chain",
  "materialize the chain", or names a plan path and asks to track the work.

  Both Claude and the user can invoke. ALWAYS preview the chain (dry-run) and
  obtain explicit user approval before any `bd create` / `bd dep add` /
  `bd update` execution. Never auto-apply without confirmation.
---

# Bead Chain From Plan

Translate the `## Bead chain structure` section of an implementation plan
into a concrete sequence of `bd` operations: bead creation, dependency
edges, parent linkages, priority bumps, follow-up bead filing.

The Plan → Bead Chain convention (CLAUDE.md / AGENTS.md) requires every
non-trivial plan to include the chain section. `superpowers:writing-plans`
produces the plan body; `bead-chain-design` generates the chain section
when one is missing; this skill consumes the chain section and materializes
it into `bd` state. Keep the responsibilities separate so the user can
review the chain shape (in the plan) before any `bd` mutation.

## When to invoke

- After `superpowers:writing-plans` and `bead-chain-design` have both run on
  the plan, and the plan-reviewer gate has returned READY
- Whenever the user says "create the beads", "set up the bead chain",
  "materialize the chain", or names a plan path and asks to track the work
- Before invoking `superpowers:subagent-driven-development` or
  `superpowers:executing-plans` if the plan's tasks should be tracked as beads
  during execution

The skill does NOT auto-fire by hook — the user (or controller agent)
explicitly invokes it. This avoids burning beads on plans that turn out to
need revision.

If the plan lacks the `## Bead chain structure` section, this skill delegates
to `bead-chain-design` BEFORE proceeding (see Step 1 below).

## Inputs

- **Plan path** (positional arg or detected from recent context): a markdown
  file under `docs/superpowers/plans/` or `docs/plans/` with a section
  headed `## Bead chain structure` (case-sensitive, level-2 heading).

If no path is supplied, look in the conversation for the most recent plan
file path mentioned. If still ambiguous, ask the user which plan.

## Workflow

### Step 1: Read the plan (or sidecar)

Read the entire plan file. Locate the `## Bead chain structure` section
(level-2 heading, exact wording per CLAUDE.md "Plan → Bead Chain"
convention).

**If the section is missing:** check for a sidecar file at
`<plan-path-without-ext>-bead-chain.md` (the location `bead-chain-design`
writes to when the plan itself is read-only). If neither exists:

1. Surface to the user: "The plan at `<path>` lacks a `## Bead chain
   structure` section. Generate one via `bead-chain-design` before
   materializing? (yes / no / abort)"
2. On `yes`: invoke `bead-chain-design` against the plan, wait for it to
   complete, then re-read the plan (or sidecar). Proceed to Step 2.
3. On `no`: ask the user whether the plan is meant to skip bead creation
   (some plans are too small to warrant a chain). If skip, exit gracefully.
4. On `abort`: exit without changes.

**If the section is present:** extract:

1. The bead chain tree (typically a fenced text block) showing parent epic,
   child task beads, grandchildren if four-level.
2. Each task bead's `bd create` invocation block (fenced bash block with
   the full `--description` heredoc inline).
3. Bead update operations: priority bumps (`bd update --priority`), parent
   re-linkage (`bd update --parent`), close-with-rationale (`bd close`).
4. Dependency edges (`bd dep add`).
5. Follow-up beads to file (typically under a "Closing-out operations" or
   "File new follow-up beads" subheading).

If the section exists but its sub-structure is non-standard (operations
inline rather than sub-sectioned), parse defensively — look for any
`bd create`, `bd update`, `bd dep add`, `bd close` invocations inside
fenced bash blocks within the section.

### Step 2: Validate task bead descriptions

Per CLAUDE.md / AGENTS.md "Plan → Bead Chain" → "Task bead description
requirement", every `bd create` for a task bead MUST have a description
with all 8 sections:

1. **Goal** — one-sentence scope
2. **Design reference** — link to the grounding spec / design doc
3. **Plan reference** — link to the impl plan with task ID anchor
4. **TDD acceptance criteria** — named tests or test conditions
5. **Verification steps** — concrete `task` / `bd` / `jj` commands
6. **Files touched** — explicit list per the architecture table
7. **Dependencies** — `bd dep add` edges matching the task graph
8. **Out of scope** — explicit non-goals

For each `bd create` in the plan's chain section:

- Confirm the description heredoc contains each section heading (case-
  insensitive match on the bold-title prefix `**Goal:**`, `**Design
  reference:**`, etc.)
- **If any section is missing, STOP.** The Plan → Bead Chain convention
  (CLAUDE.md / AGENTS.md) declares the 8 sections at MUST-level. Materializing
  a non-compliant task bead would create technical debt at the bead corpus
  level and break the bead-only-readers invariant. Surface the gap to the
  user and require one of:
  - Amend the plan to add the missing sections
  - Invoke `bead-chain-design` to regenerate the chain section with all 8
    headings in place

  Only proceed to materialization once every task bead's description is
  fully compliant.

### Step 3: Pre-state check

Before any execution:

```bash
bd dolt pull            # Sync from remote so local state is fresh
bd ready                # Quick sanity check that bd is functional
```

For each existing bead the plan references (parent epics, beads to update,
beads to close):

```bash
bd show <bead-id>
```

Confirm the bead exists, its current state (open/closed, priority), and its
description. If the plan says "close `holomush-X`" but the bead is already
closed, FLAG it. If the plan says "update parent linkage" but the parent has
moved, FLAG it.

#### Step 3a: Verify the plan + spec are reachable from main

Beads filed by this skill will cite the plan path and the design spec path
in their "Plan reference" and "Design reference" sections. Future sessions
(and dispatched subagents whose workspaces fork from main via
`task workspace:new`) will only see those files if they're reachable from
the base branch the new workspace inherits from.

Verify reachability:

```bash
# Plan
git ls-tree main -- '<plan-path>'              # present on main right now?
git log --all --oneline -- '<plan-path>'       # if not, where does it live?

# Spec (if the plan's header names one)
git ls-tree main -- '<spec-path>'
git log --all --oneline -- '<spec-path>'
```

If either path is NOT on main:

1. Surface to user: "The plan / spec is not on main — it lives on
   commit(s) `<sha>` which is not reachable from any branch [or: reachable
   only from branch `<name>` which has no open PR]. Future sessions will
   see beads citing files they cannot open."
2. Ask for one of:
   - **Land it first** (recommended): pause materialization, open + merge a
     PR for the plan/spec, re-invoke this skill after merge so the cited
     paths are reachable.
   - **Bookmark + acknowledge**: bookmark the chain head (`jj bookmark set
     <name> -r <head>`) and proceed; the user accepts that subagents will
     need explicit `jj new <bookmark>` to access the cited files. The
     downstream consequence (handoff prompts, subagent briefings) is the
     user's to manage.
   - **Abort**: exit without creating beads.

Do NOT silently proceed when paths are unreachable. The failure mode is
delayed and confusing: beads file successfully, then days later a fresh
session sees `Design reference: <path>` and `cat` reports no such file.
Real example: Phase 5 sub-epic E (May 2026) shipped 42 beads citing a
spec + plan that lived only on orphan commits; the next execution session
spent significant context recovering before dispatching any work.

### Step 4: Generate the operation manifest

Build an in-memory list of operations in the order they should execute:

1. **New parent epic** if the chain introduces one (rare — usually the parent already exists). MUST come before any `--parent` linkage updates that reference it.
2. **Pre-existing-bead updates** (priority bumps, parent linkage, description rewrites) — safe to land once the parent exists.
3. **`bd close` of supersession candidates** (e.g., a bead that's being closed because the new epic supersedes it).
4. **New child task beads** in the order their dependencies resolve (topological sort by `Dependencies` declared in each).
5. **`bd dep add` edges** between newly-created beads.
6. **Follow-up beads** filed under `holomush-ojw1` or as top-level epics — typically lowest priority, can come last.
7. **Final sync** — `bd dolt push` to publish.

Display the manifest as a table:

```text
Bead Chain Manifest for <plan-path>

| # | Op | Target | Description / Title (truncated) |
|---|----|--------|----------------------------------|
| 1 | update | holomush-ojw1.3.22 | bump priority P3 → P1 |
| 2 | update | holomush-ojw1.3.23 | bump priority P3 → P1 |
| 3 | close  | holomush-ojw1.4.2  | reason: architecturally not applicable |
| 4 | create | (NEW under ojw1.4) | Phase 3d.3: Cold-tier envelope rename |
| 5 | create | (NEW under ojw1.4) | Phase 3d.4: Crypto.Enabled default flip |
| 6 | dep add | ojw1.4.4 → ojw1.4.3 | flag flip blocks on cold-tier |
| ... |
```

For new bead creations, show enough of the description that the user can
spot a missing section or an obviously-wrong scope. Don't dump the entire
heredoc — show the title + first 100 chars of the Goal section.

### Step 5: Get user confirmation (REQUIRED)

Present the manifest and explicitly ask:

> "Apply N operations to the bead chain? (yes / no / modify)"
>
> - `yes` — execute all operations in order, sync at end
> - `no` — exit without changes
> - `modify` — describe edits (drop an op, change a description, etc.); after
>   the edits, re-display the updated manifest and prompt again with the same
>   `(yes / no / modify)` question. Only proceed to execute when the user
>   replies with an explicit `yes` against the current manifest state.

Do NOT proceed without an affirmative `yes` against the manifest as currently
displayed. Do NOT batch-apply. Do NOT execute partial edits inferred from the
`modify` request without re-displaying the result and getting fresh approval.

### Step 6: Execute operations

For each operation in the approved manifest:

1. Print the exact `bd` command being executed
2. Run it
3. Capture the output (especially the new bead ID for `bd create`)
4. On any failure: STOP, surface the error, ask the user how to proceed
   (retry, skip, abort)

After `bd create`, the new bead ID is needed for subsequent `bd dep add`
operations. Maintain a name-to-id mapping during execution. The plan often
uses placeholder IDs (`ojw1.4.3`, `ojw1.4.4`, etc.) — substitute the real
IDs returned by `bd create` when running `bd dep add`.

### Step 7: Post-state verification

After all operations succeed:

```bash
bd show <parent-epic>     # Confirm children list reflects new beads
bd ready                  # Confirm dep edges produce the right unblocked set
bd dolt push              # Sync to remote
```

Print a final summary:

```text
✓ Bead chain materialized for <plan-path>

Created: N new beads (ojw1.4.3, ojw1.4.4, ...)
Updated: M existing beads (priority bumps, parent linkage)
Closed:  K beads (with rationale)
Edges:   E dependency edges added
Synced:  bd dolt push complete

First ready task: <bd ready output, top entry>
```

## Constraints

- **Never use `--force` flags** without explicit user direction (per `bd memories force` → `never-bd-close-force-an-epic-with-open`: closing an epic with open children via `--force` requires user confirmation; the rule generalizes to any destructive `--force` operation).
- **Never close a bead by re-parenting it** to bypass the dependency check — re-parenting before close is OK only when the new parent legitimately owns the work.
- **Always run `bd dolt pull` before reading state** and `bd dolt push` after committing changes.
- **Respect `--parent` long-form** (project rule: don't use `-p` short form).
- **Always pass `--description`** explicitly; never let `bd create` open an editor.
- **Use heredoc with `'EOF'` quoting** for descriptions so backticks and `$()` substitutions are preserved verbatim in the bead body.
- If the plan's description heredoc references markdown-formatted content (links like `[X](path)`), preserve it exactly — bd renders some of this when shown.

## Failure modes

- **Plan section missing**: if no `## Bead chain structure` heading exists in the plan or its sidecar, follow Step 1's delegation flow — offer to invoke `bead-chain-design` to generate the section. If the user declines, ask whether the plan is meant to skip bead creation entirely (some single-task plans are too small to warrant a chain).
- **Description section missing**: per Step 2, this is a HARD STOP. Materialization MUST NOT proceed until the plan is amended (or regenerated via `bead-chain-design`) so every task bead description includes all 8 required sections. The Plan → Bead Chain convention is MUST-level; non-compliant beads are not acceptable.
- **Plan / spec not reachable from main**: per Step 3a, surface to user and require explicit decision (land first, bookmark-and-acknowledge, or abort). Do not silently materialize beads that will cite unreachable paths.
- **Bead already exists**: if `bd show` returns a bead the plan says to create as new (collision), abort and ask the user.
- **Forward-reference bead**: if a `bd create` description references an ID that doesn't exist yet but will be created later in this run, that's expected — the cross-reference will resolve naturally once the run completes. Do NOT pre-validate cross-references.
- **`bd dolt push` failure**: common. Retry once. If it persists, leave the local state with the changes applied and ask the user to debug remote sync separately.

## Example session

User says: *"create the beads from `docs/superpowers/plans/2026-05-03-event-payload-crypto-phase3d.md`"*

Skill:

1. Reads the plan, finds the `## Bead chain structure` section at line ~1620.
2. Validates each task bead description has all 8 sections — finds `ojw1.4.4`'s description is missing the "Out of scope" section.
3. STOPS and surfaces: "ojw1.4.4 is missing **Out of scope** (8-section MUST per Plan → Bead Chain). Materialization blocked. Options: (a) amend the plan to add the section, or (b) invoke `bead-chain-design` to regenerate."
4. User picks option (a), edits the plan, re-invokes the skill.
5. Validation passes; builds manifest of 14 operations (2 priority bumps, 1 close, 7 creates, 11 dep-add edges, 3 follow-up filings).
6. Shows the manifest table.
7. Asks for approval. User: "yes".
8. Executes operations in order, captures new bead IDs, substitutes them into dep-add commands.
9. Final summary with `bd ready` showing the unblocked tasks.

The user can now invoke `superpowers:subagent-driven-development` against
the plan with the bead chain already in place.

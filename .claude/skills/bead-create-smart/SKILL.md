---
name: bead-create-smart
description: Create a `bd` issue with the project-mandated 8-section description format (Goal, Design reference, Plan reference, TDD acceptance criteria, Verification steps, Files touched, Dependencies, Out of scope). Use when filing a tracked task that will be picked up by another session or sub-agent — not for trivial scratch tasks.
---

# Smart Bead Creation

Create a `bd` issue with the 8-section description format mandated by `CLAUDE.md`'s "Plan → Bead Chain convention". Sub-agents and future-you depend on these sections to pick up the work without re-reading the whole plan.

## When to use

- Creating any task bead under a multi-task epic (mandatory per CLAUDE.md)
- Filing a follow-up bead for review-finding work
- Filing remaining-work beads at session end ("Landing the Plane" step 1)

**Skip** for: trivial typo fixes, quick local debug notes, or scratch ideas (use `bd note` instead).

## Process

### Step 1 — Gather context

Before invoking `bd create`, collect:

1. **Parent epic** — `bd show <epic-id>` to confirm it exists and is open. If you're inside an epic context (e.g., a plan in `docs/plans/2026-XX-foo.md`), check the plan's `## Bead chain structure` section (per CLAUDE.md) for the parent.
2. **Spec / plan paths** — file paths of the design spec and the implementation plan that this bead executes.
3. **Acceptance criteria** — the smallest set of checks that prove this task is done. MUST use RFC2119 (`MUST`, `MUST NOT`, `SHOULD`).
4. **Verification commands** — `task test -- ./<package>`, `task lint`, etc. Be concrete.
5. **Files touched** — the rough file list. OK to be approximate; gives the implementer a starting point.
6. **Dependencies** — other bead IDs that must be done first.

### Step 2 — Build the description

Use this exact template (8 sections, headers exactly as written):

```markdown
## Goal
<one paragraph: what this task accomplishes>

## Design reference
- Spec: `docs/specs/<file>.md` (or `docs/superpowers/specs/...`)
- Section: §<n> <name>

## Plan reference
- Plan: `docs/plans/<file>.md` (or `docs/superpowers/plans/...`)
- Step: <task-id from plan's bead chain section>

## TDD acceptance criteria
- MUST <check 1>
- MUST <check 2>
- SHOULD <recommended check>

## Verification steps
1. `task test -- ./<package>`
2. `task lint`
3. <any manual repro / smoke check>

## Files touched
- `<path>` — <what change>
- `<path>` — <what change>

## Dependencies
- Blocks: <bead-id> (if any)
- Blocked by: <bead-id> (if any)

## Out of scope
- <thing that looks adjacent but belongs to a separate bead>
```

### Step 3 — Create the bead

```bash
bd create \
  --title "<short summary in imperative voice>" \
  --type task \
  --priority 2 \
  --parent <epic-id> \
  --description "$(cat /tmp/bead-desc-XXXX.md)"
```

Notes:
- `--parent` (long form), NOT `-p` (per CLAUDE.md bead operations)
- `--description` is mandatory
- `--priority` 0-4: 0 = critical, 2 = default, 4 = backlog (`P0`-`P4` also accepted, NOT "high"/"medium"/"low")

### Step 4 — Wire dependencies

After creation, add edges:

```bash
bd dep add <new-id> <blocked-by-id>
```

### Step 5 — Verify

```bash
bd show <new-id>
```

Confirm all 8 sections are present and parent + dependencies wired.

## Anti-patterns

- DO NOT run multiple `bd create` calls in parallel — `bd create` has an ID-allocation race; parallel calls all report the same ID with their respective titles but only ONE actually commits. Always run sequentially and verify with `bd show <parent>` after a batch.
- DO NOT use `bd update --notes` after the fact — it OVERWRITES the entire notes field. Use `bd note <id> "..."` (which appends) instead. Default to `bd note` for additive work; reserve `--notes` for deliberate clobber.
- DO NOT skip the 8 sections "to save time" — sub-agents can't pick up partial-context beads.
- DO NOT assume haiku is OK for sub-agent dispatch — model floor for sub-agents is sonnet.

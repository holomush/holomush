---
title: "Documentation Style Guide"
description: "Editorial quality rubric, per-mode page skeletons, and voice/terminology quick-reference for HoloMUSH docs contributors."
---

This page is the standard new docs are held to and the reference the docs audit uses to score existing pages. It's for anyone contributing to `site/src/content/docs/`.

## The 8-dimension quality rubric

Each dimension is scored 0–2. A page's total is out of 16.

| Dimension | 0 — absent or fails | 1 — partial | 2 — fully meets |
| --------- | ------------------- | ----------- | --------------- |
| **Orientation** | No opener; reader must read several paragraphs to understand what the page is | Opener present but vague or buries the audience | Opens with 1–2 sentences: what this page is and who it's for. No cold starts. |
| **Audience-fit** | Pitched at the wrong reader; unexplained jargon throughout | Right audience, but some jargon left undefined or some off-target content | Pitched at the section's reader (player / operator / plugin dev / contributor); jargon either avoided or explained on first use. |
| **Mode-fit (Diátaxis)** | Wrong mode for its bucket (e.g., lectures in a how-to, step lists in an explanation) | Mostly correct mode with noticeable lapses | Matches its bucket: how-to = goal→steps→verify, no lectures; reference = structured/scannable; tutorial = narrative arc; explanation = concept-first, no step lists. |
| **Clarity & voice** | Breathless, padded, or robotic prose; heavy filler | Mostly clear with occasional filler or passive hedges | Conversational, direct, grounded; cuts "simply / just / easily / of course"; sentences earn their place. See [Voice & terminology](#voice--terminology-quick-reference). |
| **Examples** | No examples where the reader would need them | Example present but incomplete or doesn't match the text | Concrete example or runnable command where it materially helps comprehension. |
| **Terminology** | Canonical terms violated throughout (e.g., "room" instead of "location") | One or two violations; mostly correct | Canonical terms per [`.claude/rules/terminology.md`](https://github.com/holomush/holomush/blob/main/.claude/rules/terminology.md) throughout: `location` not room, `character` vs `player`, etc. |
| **Cross-linking** | Dead end — no links to prerequisites or next steps | Some links but missing obvious connections | Links to prerequisites and logical next steps; not a dead end. |
| **Conciseness** | Significant filler, duplication of content better placed elsewhere | Some padding or redundancy | No filler, no duplication of content better placed elsewhere. |

### Score thresholds

- **< 10/16, or any 0 on Mode-fit or Terminology** → P1: full revision required before the next release.
- **10–13** → light touch: targeted edits to the lowest-scoring dimensions.
- **≥ 14** → good: leave as-is or note minor improvements for later.

Scoring is editorial judgment, not a mechanical script. Two reviewers may disagree by one point on a dimension; that's fine. Systematic disagreement on a threshold decision should prompt a discussion.

## Per-mode page skeletons

HoloMUSH docs follow [Diátaxis](https://diataxis.fr/). Before you write, identify which mode your page belongs to, then use that skeleton.

### How-to (task)

A how-to answers "how do I accomplish X?" Goal first, then steps, then verification. No lectures — save the "why" for an explanation page and link to it.

```markdown
---
title: "How to <verb> <thing>"
description: "<one sentence: what you'll accomplish>"
---

<1–2 sentence orientation: what this accomplishes and who needs it.>

## Prerequisites

- <thing the reader must have or know>

## Steps

1. <First action.>
2. <Second action.>
3. <Third action.>

## Verify

Run `<command>` and confirm `<expected output>`.
```

### Reference

A reference page is structured and scannable. Readers come to look something up, not to learn — so prose is minimal and tables/consistent headings do the work.

```markdown
---
title: "<Thing> reference"
description: "<one sentence: what's documented here>"
---

<1–2 sentence orientation.>

## <Category>

| Field / Symbol | Type | Description |
| -------------- | ---- | ----------- |
| ...            | ...  | ...         |

## <Next category>

...
```

### Tutorial

A tutorial guides a learner from start to finish through a worked example. It has a narrative arc: setup → build → see it work → what you learned. The reader follows along; every decision is made for them.

A tutorial page follows this structure:

- **Frontmatter**: `title: "<Do X> tutorial"`, `description: "<one sentence: what the reader builds>"`
- **Orientation** (1–2 sentences): what the reader will build and what concept they'll understand.
- **Before you start**: prerequisites list.
- **Step 1 / Step 2 / …**: numbered headings, each with a narrative sentence, a runnable command in a `bash` fence, and a sentence on the expected output.
- **What you built**: closing summary + link to the logical next page.

Avoid any `## Prerequisites` / `## Verify` heading patterns — those belong in how-tos. Tutorials are narrative, not checklists.

### Explanation (concept)

An explanation answers "why does this work the way it does?" Concept first; no step lists. The reader is trying to build a mental model, not follow a recipe.

```markdown
---
title: "<Concept name>"
description: "<one sentence: what this explains>"
---

<1–2 sentence orientation: what concept this covers and why it matters.>

## <Core idea>

<Prose explanation. Use analogies when they help. No numbered steps.>

## <Trade-off or design decision>

<Why it was designed this way; what alternatives were considered.>

## Further reading

- [<how-to page>](<path>) — if you want to use this in practice
- [<reference page>](<path>) — for the complete API/field catalog
```

## Voice & terminology quick-reference

### Voice (from [`site/CLAUDE.md`](https://github.com/holomush/holomush/blob/main/site/CLAUDE.md))

- **Conversational and direct.** Use "you." Write like you're explaining something to a colleague who's smart but new to this.
- **Grounded, not breathless.** State what something does and why it matters. Don't oversell.
- **Vivid when it counts.** Game-world descriptions can paint a picture; technical explanations should be plain and precise.
- **No filler.** Cut "simply," "just," "easily," "of course," "Note that." If something is simple, the explanation will show it.
- **Acknowledge the MU\* tradition.** Reference it when it helps ("Unlike traditional MU\*s…"), but don't assume everyone shares it.

### Canonical terminology (from [`.claude/rules/terminology.md`](https://github.com/holomush/holomush/blob/main/.claude/rules/terminology.md))

| Use this | Not this | Notes |
| -------- | -------- | ----- |
| `location` | room, area, zone | A place in the world model. |
| `exit` | door, path, passage | A connection between locations. |
| `character` | player, user, avatar | An in-game entity controlled by a player. |
| `player` | user, account | The human behind one or more characters. |
| `session` | connection | Server-side state for a character's ongoing presence. |
| `connection` | socket, client | A single client attachment to a session. |
| `scene` | RP scene | A structured roleplay encounter. |

These terms are enforced in both code and docs. A terminology violation is a rubric dimension 0 if it's pervasive, 1 if it's occasional.

## Next steps

- **Docs audit** — applies this rubric to every page in `site/src/content/docs/` and files `priority::high` revision issues for pages below threshold.
- **[Coding Standards](/contributing/reference/coding-standards/)** — the equivalent reference for Go code conventions.
- **Source files**: [`site/CLAUDE.md`](https://github.com/holomush/holomush/blob/main/site/CLAUDE.md) (voice and tone) · [`.claude/rules/terminology.md`](https://github.com/holomush/holomush/blob/main/.claude/rules/terminology.md) (canonical terms).

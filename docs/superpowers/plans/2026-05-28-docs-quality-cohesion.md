<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Docs Quality & Cohesion Implementation Plan (SP5)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the docs genuinely good to read and navigate — clear writing, oriented arrangement, consistent voice/terminology — with a supporting presentation layer (topic-tab nav, page actions, section overviews, community links).

**Architecture:** Two halves. **Presentation** (Phase 1) is mechanical Starlight config + 2 plugins. **Editorial** (Phases 2–4) is the substance: write a quality rubric, audit every page against it, then per-section prioritized prose passes (full-revise sub-threshold pages, light-touch the rest, defer the long tail to beads). Presentation ships first so the visible chrome lands quickly; editorial is the bulk.

**Tech Stack:** Astro Starlight 0.39 / Astro 6.3, bun, `starlight-sidebar-topics`, `starlight-llm-actions`, built-in `editLink`/`social`/`lastUpdated`, `linkinator` (link check), bats, `jj`.

**Spec:** `docs/superpowers/specs/2026-05-28-docs-quality-cohesion-design.md`
**Design bead:** `holomush-ivwij` · **Program anchor:** `holomush-rkwyb`

---

## File Structure

| Path | Responsibility | Action |
| --- | --- | --- |
| `site/package.json` | Add `starlight-sidebar-topics`, `starlight-llm-actions` | Modify |
| `site/astro.config.mjs` | Topic tabs, page-actions, `editLink`, `lastUpdated`, `social` | Modify |
| `site/src/styles/custom.css` | Light visual polish (accent/spacing/code/asides) | Modify |
| `site/src/content/docs/contributing/reference/docs-style-guide.md` | The editorial **quality rubric** (also the contributor standard) | Create |
| `docs/superpowers/sp5-docs-quality-audit.md` | Per-page audit (scores + priority + biggest issue) | Create |
| `site/src/content/docs/{guide,operating,extending,contributing,reference}/index.md` | Section **overview** pages (card grids → sub-sections) | Modify |
| `site/src/content/docs/**` | Prioritized editorial revisions + dedupe + cross-links | Modify |
| `scripts/check-docs-quality.sh` + `scripts/tests/docs-quality.bats` | Structural invariants (overviews, social, advisory terminology grep, page count) | Create |

> **Lifecycle:** runs in the `sp5-docs-polish` jj workspace (branched from `main@origin`). Commit per task with `jj commit`/`jj describe`; `task fmt:markdown` before each docs commit. Run `.mjs`/plugin installs with **bun**. The editorial tasks (Phases 3–4) are prose work guided by the rubric (Task 5) + audit (Task 6) — they have no unit tests; verification is build-green + `task docs:linkcheck` + per-section human review.
>
> **Plugin caveat:** context7 quota was exhausted at design time. Before wiring `starlight-sidebar-topics` (Task 1) and `starlight-llm-actions` (Task 2), read each plugin's current README for exact config + whether the top-level `sidebar` key is kept or removed under the topics plugin. Pin versions in `package.json`.

---

## Phase 1: Presentation & chrome

### Task 1: Topic-tab navigation (`starlight-sidebar-topics`)

**Files:** `site/package.json`, `site/astro.config.mjs`

- [ ] **Step 1: Install**

```bash
cd site && bun add starlight-sidebar-topics
```

- [ ] **Step 2: Configure 5 topics (one per audience section)**

Add the plugin and define a topic per audience, each autogenerating its own sidebar from its directory. Read the plugin README first to confirm whether to remove the top-level `sidebar` key (the plugin renders per-topic sidebars):

```javascript
import starlightSidebarTopics from 'starlight-sidebar-topics';
// ⚠️ The existing top-level `sidebar: [...]` key (astro.config.mjs) is very
// likely REMOVED here — starlight-sidebar-topics renders a per-topic sidebar,
// so keeping both produces duplicate/competing sidebars. Confirm in the 0.7.x
// README, then delete the old `sidebar` key as part of this task.
// inside starlight({ ... }) plugins: [...existing,
  starlightSidebarTopics([
    { label: 'Guide',        link: '/guide/',        icon: 'open-book',    items: [{ autogenerate: { directory: 'guide' } }] },
    { label: 'Operating',    link: '/operating/',    icon: 'setting',      items: [{ autogenerate: { directory: 'operating' } }] },
    { label: 'Extending',    link: '/extending/',    icon: 'puzzle',       items: [{ autogenerate: { directory: 'extending' } }] },
    { label: 'Contributing', link: '/contributing/', icon: 'pencil',      items: [{ autogenerate: { directory: 'contributing' } }] },
    { label: 'Reference',    link: '/reference/',    icon: 'information',  items: [{ autogenerate: { directory: 'reference' } }] },
  ]),
// ]
```

- [ ] **Step 3: Build + verify tabs**

```bash
cd site && bunx astro build
```

Then `bunx astro dev` — confirm 5 topic tabs render above the sidebar; selecting one shows only that section's sidebar; the home splash still links into `/guide/`.

- [ ] **Step 4: Commit**

`jj commit -m "docs(site): topic-tab navigation via starlight-sidebar-topics (holomush-ivwij)"`

### Task 2: Page-actions dropdown (`starlight-llm-actions`)

**Files:** `site/package.json`, `site/astro.config.mjs`

- [ ] **Step 1: Install + register** (read README for current config shape)

```bash
cd site && bun add starlight-llm-actions
```

Add `starlightLlmActions({ ... })` to the `plugins` array (copy/view as Markdown, open-in-LLM). Configure the action set per the README.

- [ ] **Step 2: Build + verify**

```bash
cd site && bunx astro build
```

`bunx astro dev` — confirm a page-actions control appears in the page title area on a content page, and "copy as Markdown" yields the page body.

- [ ] **Step 3: Commit**

`jj commit -m "docs(site): per-page LLM page-actions dropdown (holomush-ivwij)"`

### Task 3: Built-in chrome — editLink, lastUpdated, community

**Files:** `site/astro.config.mjs`

- [ ] **Step 1: Add config keys**

```javascript
      editLink: { baseUrl: 'https://github.com/holomush/holomush/edit/main/site/' },
      lastUpdated: true,
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/holomush/holomush' },
        { icon: 'comment', label: 'Discussions', href: 'https://github.com/holomush/holomush/discussions' },
        // Discord: add when the invite exists — placeholder, do not ship a dead link:
        // { icon: 'discord', label: 'Discord', href: '<DISCORD_INVITE_URL>' },
      ],
```

> Discord ships only once a real invite URL is supplied (spec ⚑). GitHub Discussions ships now. Confirm GitHub Discussions is enabled on the repo, or drop that entry.

- [ ] **Step 2: Build + verify**

```bash
cd site && bunx astro build
```

`bunx astro dev` — confirm "Edit page" link (→ GitHub edit URL), "Last updated" footer, and the social icons.

- [ ] **Step 3: Commit**

`jj commit -m "docs(site): editLink + lastUpdated + community social links (holomush-ivwij)"`

### Task 4: Light visual polish (`custom.css`)

**Files:** `site/src/styles/custom.css`

- [ ] **Step 1: Refine without changing fonts**

Tighten code-block, aside, and heading spacing; ensure the deep-orange accent has adequate contrast in both schemes. Keep Starlight's default font stack (no web-font add). Keep edits small and reversible.

- [ ] **Step 2: Build + eyeball; Commit**

```bash
cd site && bunx astro build
```

`jj commit -m "docs(site): light CSS polish, default fonts retained (holomush-ivwij)"`

---

## Phase 2: Editorial foundation

### Task 5: Write the quality rubric

**Files:** `site/src/content/docs/contributing/reference/docs-style-guide.md`

- [ ] **Step 1: Author the rubric page**

Write the 8-dimension rubric from the spec (Orientation, Audience-fit, Mode-fit, Clarity & voice, Examples, Terminology, Cross-linking, Conciseness), each scored 0–2, grounded in `site/CLAUDE.md` voice and `.claude/rules/terminology.md`. Include the per-mode page skeletons (how-to: goal→prereqs→steps→verify; reference: structured; tutorial: narrative; explanation: concept-first). Add `title` frontmatter; it will surface under Contributing → Reference and is the standard for new docs.

- [ ] **Step 2: Build + commit**

```bash
cd site && bunx astro build
```

`jj commit -m "docs(contributing): documentation quality rubric / style guide (holomush-ivwij)"`

### Task 6: Audit every page against the rubric

**Files:** `docs/superpowers/sp5-docs-quality-audit.md`

- [ ] **Step 1: Generate the page list**

```bash
fd -e md -e mdx . site/src/content/docs | rg -v 'docs/index.mdx|reference/events/' | sort
```

(≈76 content pages — excludes the root splash and auto-generated `reference/events/*`.)

- [ ] **Step 2: Score each page**

Read each page against the rubric; record a table row: `slug | Orient | Aud | Mode | Clarity | Ex | Term | Xlink | Concise | Total/16 | biggest issue`. This is editorial judgment, not a script.

- [ ] **Step 3: Assign priority**

Mark **P1 (full revision)** = total < 10 OR any 0 on Mode-fit/Terminology; **P2 (light touch)** = 10–13; **OK** = ≥14. Summarize counts per section at the top.

- [ ] **Step 4: Commit**

`jj commit -m "docs: SP5 per-page quality audit + priorities (holomush-ivwij)"`

---

## Phase 3: Arrangement

### Task 7: Section overview pages

**Files:** `site/src/content/docs/{guide,operating,extending,contributing,reference}/index.md`

- [ ] **Step 1: Enrich each section index into an oriented landing**

For each of the 5 indexes: a 1–2 sentence "what's here / who it's for", a `<CardGrid>` of `<LinkCard>`s to the mode sub-sections (and key pages), and an explicit "Start here →" pointer. Mirror the home splash's component use. `extending`/`contributing` already have link lists — upgrade to cards; `guide`/`operating`/`reference` are sparser — build out.

```mdx
---
title: Operating HoloMUSH
---
import { CardGrid, LinkCard } from '@astrojs/starlight/components';

Run and maintain a HoloMUSH server — install, deploy, secure, and operate it.

<CardGrid>
  <LinkCard title="How-to guides" href="/operating/how-to/installation/" description="Install, deploy, rotate keys, back up, monitor." />
  <LinkCard title="Reference" href="/operating/reference/configuration/" description="Configuration and operational reference." />
  <LinkCard title="Explanation" href="/operating/explanation/authentication/" description="How auth, lifecycle, and plugin security work." />
</CardGrid>
```

- [ ] **Step 2: Build + linkcheck**

```bash
cd site && bunx astro build && cd .. && task docs:linkcheck
```

Expected: build green; zero broken links.

- [ ] **Step 3: Commit**

`jj commit -m "docs: section overview landing pages with card grids (holomush-ivwij)"`

### Task 8: De-duplication pass

**Files:** overlapping pages (audit-identified), e.g. `*/authentication`, `extending/reference/events` vs `reference/events`

- [ ] **Step 1: Identify overlaps from the audit**

```bash
rg -l "authentication|password|argon2" site/src/content/docs | sort
```

For each overlap cluster (auth across operating/contributing/reference; `extending/reference/events` vs generated `reference/events`): pick the **canonical home**, reduce the others to a 1-line cross-link, and verify no content is lost (move unique material into the canonical page first).

- [ ] **Step 2: Fix inbound links (no redirects — SP1 stance)**

For any page reduced/merged, rewrite inbound links to the canonical slug. Verify:

```bash
cd site && bunx astro build && cd .. && task docs:linkcheck
```

Expected: zero broken links.

- [ ] **Step 3: Commit**

`jj commit -m "docs: de-duplicate overlapping auth/events coverage, cross-link (holomush-ivwij)"`

---

## Phase 4: Per-section editorial passes

> Tasks 9–13 apply the rubric to one section each, driven by the Task 6 audit. Per section: **full-revise** its P1 pages (clarity, orientation, mode-fit, examples, terminology, voice), **light-touch** P2 pages, leave OK pages, and **file a follow-up bead** for any page whose needed rewrite exceeds editorial scope. Verify build + linkcheck after each. These are prose tasks — no unit tests; the rubric + audit + per-section review are the bar.

### Task 9: Editorial pass — Guide

**Files:** `site/src/content/docs/guide/**`

- [ ] **Step 1:** Apply the rubric to the Guide section's audited pages (player-facing: warm, jargon-light, example-led). Full-revise P1, light-touch P2.
- [ ] **Step 2:** `cd site && bunx astro build && cd .. && task docs:linkcheck` → green.
- [ ] **Step 3:** File `theme:docs-platform` follow-up beads for any deferred page; note IDs.
- [ ] **Step 4:** `task fmt:markdown` then `jj commit -m "docs(guide): editorial pass to rubric (holomush-ivwij)"`

### Task 10: Editorial pass — Operating

**Files:** `site/src/content/docs/operating/**`

- [ ] **Step 1:** Apply the rubric (operator-facing: precise, procedure-first how-tos; clean reference). Full-revise P1, light-touch P2.
- [ ] **Step 2:** build + linkcheck → green.
- [ ] **Step 3:** file deferred-page beads.
- [ ] **Step 4:** `task fmt:markdown` then `jj commit -m "docs(operating): editorial pass to rubric (holomush-ivwij)"`

### Task 11: Editorial pass — Extending

**Files:** `site/src/content/docs/extending/**`

- [ ] **Step 1:** Apply the rubric (plugin-dev-facing: concrete code examples, clear tutorial arcs vs how-tos vs reference). Full-revise P1, light-touch P2.
- [ ] **Step 2:** build + linkcheck → green.
- [ ] **Step 3:** file deferred-page beads.
- [ ] **Step 4:** `task fmt:markdown` then `jj commit -m "docs(extending): editorial pass to rubric (holomush-ivwij)"`

### Task 12: Editorial pass — Contributing

**Files:** `site/src/content/docs/contributing/**`

- [ ] **Step 1:** Apply the rubric (contributor-facing: crisp how-tos, conceptual explanations). Full-revise P1, light-touch P2. (Style-guide page from Task 5 is already to-standard.)
- [ ] **Step 2:** build + linkcheck → green.
- [ ] **Step 3:** file deferred-page beads.
- [ ] **Step 4:** `task fmt:markdown` then `jj commit -m "docs(contributing): editorial pass to rubric (holomush-ivwij)"`

### Task 13: Editorial pass — Reference

**Files:** `site/src/content/docs/reference/**` (hand-written pages only — NOT the generated `grpc-api`/`events`, which are SP0/SP4)

- [ ] **Step 1:** Apply the rubric to hand-written reference pages (`access-control`, `audit-subjects`, `index`): structured, scannable, accurate. Do **not** edit generated `grpc-api.md` or `events/*` (owned by the generators / SP4).
- [ ] **Step 2:** build + linkcheck → green.
- [ ] **Step 3:** file deferred-page beads.
- [ ] **Step 4:** `task fmt:markdown` then `jj commit -m "docs(reference): editorial pass to hand-written reference (holomush-ivwij)"`

---

## Phase 5: Verification

### Task 14: Structural invariant checks (INV-1/2/4/7) + advisory terminology grep

**Files:** `scripts/check-docs-quality.sh`, `scripts/tests/docs-quality.bats`

- [ ] **Step 1: Write `check-docs-quality.sh`** — assert:
  - **INV-1/2:** the rubric page exists; the audit exists and its row count == the content-page count (`fd … | rg -v 'docs/index.mdx|reference/events/' | wc -l`).
  - **INV-4:** each of the 5 section `index.md` contains a `<CardGrid>`.
  - **INV-7:** `astro.config.mjs` `social` includes GitHub + Discussions (+ Discord when present).
  - **Advisory terminology grep (NOT a gate):** print candidate misuses for human review — `rg -n 'location[^.]{0,30}\broom\b' site/src/content/docs` and similar — exit 0 regardless (triage aid only, per spec INV-3).

- [ ] **Step 2: Write `docs-quality.bats`** wrapping the hard asserts (INV-1/2/4/7) with `assert_success`; the terminology grep is run for output, not asserted.

- [ ] **Step 3: Run**

```bash
cd site && bunx astro build && cd .. && bats scripts/tests/docs-quality.bats
```

Expected: PASS.

- [ ] **Step 4: Commit**

`jj commit -m "test(docs): SP5 structural invariants + terminology advisory (holomush-ivwij)"`

### Task 15: Final sweep + pr-prep

**Files:** none (verification)

- [ ] **Step 1: Full sweep**

```bash
cd site && rm -rf dist && cd .. && task docs:build && task docs:linkcheck && bats scripts/tests/docs-quality.bats
for f in llms.txt llms-full.txt llms-small.txt; do test -s "site/dist/$f" && echo "OK $f"; done
```

Expected: all green; llms files non-empty (INV-8). Spot-check topic tabs + page actions in `bunx astro preview`.

- [ ] **Step 2: Branding/IA unchanged (INV-9)**

INV-9 preserves **fonts + brand assets**, not all of `custom.css` (Task 4 deliberately polishes spacing/accent in it). So assert assets are byte-identical and `custom.css` has no *font* changes:

```bash
# Brand assets (logo/favicon) byte-identical to main@origin:
jj diff --from main@origin -- site/src/assets site/public/favicon.png   # expect: empty
# custom.css polish must NOT touch the font stack:
jj diff --from main@origin -- site/src/styles/custom.css | rg -i 'font-family|--sl-font|@font-face' \
  && echo "FONT CHANGE — investigate (INV-9 violation)" || echo "no font changes — good"
```

Expected: assets diff empty; "no font changes — good". Confirm no slug changes beyond approved dedupe merges.

- [ ] **Step 3: `task fmt:markdown` then `task pr-prep`**

Expected: pass marker `✓ Fast PR checks passed`.

- [ ] **Step 4: Commit**

`jj commit -m "docs(site): finalize SP5 quality + cohesion pass (holomush-ivwij)"`

---

## Out of scope (follow-on)

- Deferred long-tail prose rewrites (Tasks 9–13 beads).
- Discord social entry until an invite URL exists.
- SP0 (proto comments), SP4 (gRPC coverage); generated `reference/grpc-api`/`events` content.
- Hosted AI-search widget.
<!-- adr-capture: sha256=2f31e0294c76358d; session=2f5ef07e; ts=2026-05-28T21:14:29Z; adrs=holomush-q924m -->

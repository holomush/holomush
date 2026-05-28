<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Documentation Quality — Content, Arrangement, Communication (SP5)

| Field         | Value                                                          |
| ------------- | -------------------------------------------------------------- |
| Status        | Draft — pending `design-reviewer`                              |
| Tracking bead | `holomush-ivwij`                                               |
| Date          | 2026-05-28                                                     |
| Sub-project   | SP5 of the docs-platform program (anchor `holomush-rkwyb`)    |
| Depends on    | SP1 (Starlight) + SP2 (Diátaxis IA) — both landed             |

## Summary

Make the documentation genuinely good to **read** and **navigate** — clear
writing, sound arrangement, consistent voice and terminology, no overlap or
dead weight — and bring the site's presentation to parity with peer docs
(topic-tab navigation, per-page actions, section overviews, community
identity). **This is primarily a content-and-communication effort**, not a
layout one: the presentation layer is the supporting pillar, not the point.

The editorial work is made tractable with a written **quality rubric**, a
**per-page audit** that scores and prioritizes, and **prioritized passes** —
full rewrites for the worst/highest-impact pages in-scope, rubric-guided light
touches elsewhere, and follow-up beads for the long tail.

## Program context

SP5 is a new sub-project of the `theme:docs-platform` program (anchor
`holomush-rkwyb`; see `docs/roadmap.md`). SP1 (Astro Starlight migration) and
SP2 (audience-first Diátaxis IA + autogenerate sidebar) have both landed. SP5
builds on that substrate: SP2's audience folders become the input to topic-tab
navigation, and SP2's mode buckets define the per-mode page shapes this pass
makes consistent. SP0 (proto comments) and SP4 (gRPC coverage) are independent
and out of scope here.

Motivating comparison: peer docs sites (Hermes Agent, OpenClaw, Pi) read as
cohesive products — top-level navigation, oriented section overviews, page
actions, a clear voice. HoloMUSH today has the IA and a home splash, but inner
pages are plain, the sidebar is one long list, there's no per-page action or
community link, and prose quality/consistency varies page to page.

## Goals

- **MUST** establish a written editorial **quality rubric** grounded in
  `site/CLAUDE.md` voice and `.claude/rules/terminology.md`, and apply it.
- **MUST** audit every doc page (clarity / arrangement / mode-fit /
  terminology) and prioritize; **MUST** fully revise the high-priority pages
  and rubric-light-touch the rest, filing follow-up beads for deferred prose work.
- **MUST** give each of the 5 sections an orienting overview page (card grid →
  sub-sections, short "what's here / who it's for / where to start").
- **MUST** make terminology consistent and **checkable** (no `room` / `player`-
  as-character misuse in prose).
- **MUST** reduce duplication/overlap and add cross-links / next-steps.
- **MUST** add topic-tab navigation, a per-page actions dropdown, Edit-on-GitHub,
  `lastUpdated`, and community links (GitHub Discussions + Discord).
- **MUST** preserve branding and the SP2 IA; keep build / `llms.txt` / link-check green.

## Non-goals

- **MUST NOT** change the SP2 information architecture (folder structure / slugs)
  beyond merges that dedupe genuinely duplicated pages (each such merge gets a
  redirect-free link fix per SP1 stance).
- **MUST NOT** author net-new conceptual docs or fill SP0/SP4 surface (proto
  comments, gRPC coverage).
- **MUST NOT** adopt a hosted AI-search widget — the page-actions "open in LLM"
  is the lighter substitute.
- **MUST NOT** change content *meaning* — editorial passes improve clarity and
  arrangement, not the technical facts.

## Architecture / Approach

Three pillars. Pillar 1 (communication) is the substance; Pillar 3
(presentation) is the enabling layer.

### Pillar 1 — Communication: the quality rubric

A checked-in rubric (`site/docs-quality-rubric.md` or
`site/CONTRIBUTING-docs.md`) defines the bar every page is held to. Each page is
scored 0–2 on each dimension:

| Dimension | What "good" means |
| --- | --- |
| **Orientation** | Opens with 1–2 sentences: what this page is + who it's for. No cold starts. |
| **Audience-fit** | Pitched at the section's reader (player / operator / plugin dev / contributor); jargon either avoided or explained on first use. |
| **Mode-fit (Diátaxis)** | Matches its bucket: how-to = goal→steps→verify, no lectures; reference = structured/scannable; tutorial = narrative arc; explanation = concept-first, no step lists. |
| **Clarity & voice** | `site/CLAUDE.md` voice — conversational, direct, grounded; cuts "simply / just / easily / of course"; sentences earn their place. |
| **Examples** | Concrete example/command where it materially helps comprehension. |
| **Terminology** | Canonical terms per `.claude/rules/terminology.md` (`location` not room, `character` vs `player`). |
| **Cross-linking** | Links to prerequisites and logical next steps; not a dead end. |
| **Conciseness** | No filler, no duplication of content better placed elsewhere. |

The rubric is also the contributor standard going forward (lives in the docs so
new pages inherit it).

### Pillar 1 — the audit + prioritized passes

1. **Audit:** produce `site/docs-quality-audit.md` — a table of every page with
   its per-dimension scores, a total, and a one-line "biggest issue." Generated
   by reading each page against the rubric (this is editorial judgment, not a
   script).
2. **Prioritize:** pages below a threshold (e.g. total < 10/16, or any 0 on
   Mode-fit/Terminology) are **P1 — full revision in-scope**. Pages 10–13 get a
   **rubric-guided light touch**. Pages ≥14 are left, noted "good."
3. **Passes, per section:** revise P1 pages section-by-section (Guide →
   Operating → Extending → Contributing → Reference) so each section is
   independently reviewable. Deferred prose work → follow-up beads labeled
   `theme:docs-platform`.

### Pillar 2 — Arrangement

- **Section overview pages:** enrich each section `index.md` into an orienting
  landing — short intro + `<CardGrid>`/`<LinkCard>` to the mode sub-sections,
  and an explicit "start here" reading order. (Pattern already used on the home
  splash.) Note: `extending/index` and `contributing/index` already carry link
  lists and need less work than the sparser `guide/`, `operating/`, `reference/`
  indexes — the audit confirms per-section effort.
- **De-duplication:** reconcile overlapping content — e.g. authentication
  appears in `operating/`, `contributing/`, and `reference/`; `extending/events`
  vs the generated `reference/events`. For each: pick the canonical home, reduce
  the others to a cross-link, fix inbound links (no redirects, SP1 stance).
- **Per-mode page shape:** apply a consistent skeleton per Diátaxis mode so
  same-type pages feel cohesive (defined in the rubric; applied during passes).
- **Cross-links / next-steps:** every page ends pointing somewhere sensible.

### Pillar 3 — Presentation (enabling)

| Change | Mechanism |
| --- | --- |
| Top-level **topic tabs** (5 audience sections, each own sidebar) | `starlight-sidebar-topics` (Starlight ≥0.38; we're 0.39) — convert the 5 sidebar groups to topics |
| Per-page **actions dropdown** (copy/view as Markdown, open-in-LLM) | `starlight-llm-actions` (ties into SP1 llms.txt) |
| **Edit on GitHub** | built-in `editLink.baseUrl: https://github.com/holomush/holomush/edit/main/site/` |
| **Last updated** + prev/next pagination | built-in `lastUpdated: true` (pagination is default) |
| **Community** links | built-in `social`: add GitHub Discussions **and** Discord. ⚑ Discord invite URL is an implementation input — placeholder until supplied; GitHub Discussions ships regardless |
| **Light visual polish** | `customCss` (accent/spacing/code-block/admonition); keep Starlight default fonts |

## Invariants

| ID | Invariant | Verification |
| --- | --- | --- |
| INV-1 | The quality rubric exists in the docs and is referenced by the audit. | File presence + audit cross-reference. |
| INV-2 | Every page is scored in the audit; no page is unscored. | Meta-check: audit row count == content-page count, where a **content page** is any `.md`/`.mdx` under `site/src/content/docs/` **excluding** the root `index.mdx` splash and the auto-generated `reference/events/*` sub-pages. |
| INV-3 | Terminology is consistent in prose per the canonical glossary (`location` for the spatial concept; `character` vs `player` used correctly). | **Rubric/audit dimension (human-judged)** — the spatial concept is "location" (generic English "room", e.g. "common room", is allowed; `player` is itself canonical). An **advisory** `rg` helper flags *candidate* misuses (`location.*\broom\b` proximity, etc.) for reviewer attention — it is a triage aid, **not** a hard CI gate (the token-level false-positive rate is too high to gate on). |
| INV-4 | Each of the 5 sections has an orienting overview `index` with a card grid to its sub-sections. | Per-section check. |
| INV-5 | Topic tabs render for all 5 sections; each tab scopes its own sidebar. | Build + nav check. |
| INV-6 | Every doc page has the page-actions dropdown and an Edit-on-GitHub link. | Rendered-page check on a sample + config presence. |
| INV-7 | Community (GitHub Discussions + Discord) and GitHub repo links present. | `social` config check. |
| INV-8 | No broken internal links after de-dup/cross-link changes; build + `llms.txt` regenerate. | `task docs:linkcheck` + build. |
| INV-9 | Branding (logo/favicon/accent/`custom.css` fonts) preserved; SP2 IA (slugs) unchanged except approved merges. | Diff vs `main@origin` (`jj diff`). |

> The subjective core (prose quality / voice) is **not** invariant-checked — it
> is governed by the rubric and per-section human review. INV-1..9 fence the
> checkable structure around that judgment; they do not pretend to assert "well
> written."

## Risks & mitigations

| Risk | Mitigation |
| --- | --- |
| Editorial pass balloons (60 pages) | Audit-prioritized: only sub-threshold pages get full rewrites in-scope; rest are light-touch + follow-up beads. |
| Subjective quality is unreviewable | Rubric makes the standard explicit; per-section passes keep review chunks small; audit scores make "done" legible. |
| `starlight-sidebar-topics` reshapes nav / breaks deep links | Topic links target existing section roots (`/guide/` …); link-check (INV-8) backstops. |
| Discord invite not available | GitHub Discussions ships now; Discord added when the URL exists (placeholder, not a blocker). |
| De-dup merges change slugs | Treat as the only allowed IA change; fix inbound links; INV-8 guards. |

## Out of scope (follow-on)

- Deferred prose rewrites for the long-tail pages (follow-up beads).
- Net-new conceptual docs; SP0 (proto comments); SP4 (gRPC coverage).
- Hosted AI-search widget.

## References

- Current site: `site/astro.config.mjs` (Starlight 0.39 / Astro 6.3; plugins client-mermaid + llms-txt; autogenerate sidebar; social=github), `site/src/content/docs/**`, `site/src/styles/custom.css`, home `index.mdx` (splash hero + CardGrid).
- Editorial standard: `site/CLAUDE.md` (voice & tone) + `.claude/rules/terminology.md` (canonical glossary, MUST-NOT-mix).
- Plugins: `starlight-sidebar-topics` (HiDeoo), `starlight-llm-actions` (page actions), built-in `editLink`/`social`/`lastUpdated` (starlight.astro.build). context7 quota was exceeded at design time — plugin shapes grounded via the Starlight plugins registry + exa; re-verify versions/config at plan time.
- Program anchor `holomush-rkwyb`; roadmap `docs/roadmap.md` § `theme:docs-platform`.
- Reference sites: Hermes Agent, OpenClaw, Pi (user-provided exemplars).
<!-- adr-capture: sha256=206ad7cb3cd89e67; session=2f5ef07e; ts=2026-05-28T21:14:29Z; adrs=holomush-q924m -->

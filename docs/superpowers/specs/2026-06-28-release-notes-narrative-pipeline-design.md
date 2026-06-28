<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Narrative Release-Notes Pipeline

**Date:** 2026-06-28
**Design bead:** holomush-dufb8
**Status:** Draft

## Overview

HoloMUSH releases are tag-only. Per ADR [holomush-jfb9x](../../adr/holomush-jfb9x-tag-only-release-drop-changelog.md),
cutting a release MUST NOT write to `main`, there is no in-repo `CHANGELOG.md`,
and GoReleaser generates the GitHub Release notes from conventional-commit
subjects (excluding `docs:`/`test:`/`chore:`/merges via its `changelog:` block).

That surface is a **filtered commit log**, not a story. The v0.10.0 release
(v0.9.0 → v0.10.0, ~180 commits over a month) bundles real themes — web scene
management, session liveness leases, host-mediated crypto read-back, the
holographic brand refresh, colon→dot subject eradication — but none of that
*narrative* survives the commit-subject filter. A reader cannot tell **what
changed and why it matters** from the release page.

This design adds a **narrative layer on top of** the existing mechanical notes,
authored by a local maintainer-run Claude Code skill, published to two surfaces,
without reintroducing the protected-`main`-write pain that jfb9x eliminated.

The narrative is sourced primarily from substrate the project already curates —
`theme:<slug>` sections in `docs/roadmap.md` and the closed bead-epic graph —
with filtered commit subjects as a fallback and coverage cross-check. In the
v0.9.0 → v0.10.0 range, 139 of 180 commit subjects carry a `holomush-<id>` bead
reference (verified via `git log v0.9.0..v0.10.0`), so the bead graph is a dense,
reliable narrative source. The remaining ~23% fall through to the "Other changes"
catch-all (§Coverage guarantee), so the design degrades gracefully if the ratio
is lower for a given range.

## Goals

- Produce a human-digestible narrative TLDR per release: a 2–3 sentence summary
  plus theme-grouped feature/fix highlights.
- Publish to the **GitHub Release body** (above GoReleaser's mechanical list) and
  to a browsable **release-history section on the docs site**.
- Keep the existing tag-only / GoReleaser / cog flow unchanged.
- Require **no LLM API key in CI** and **no write to `main` at release-cut time**.
- Solve v0.10.0 retroactively with the same tool.

## Non-Goals

- No CI-driven automatic summarization (no API secret, no draft-review round-trip).
- No in-repo `CHANGELOG.md` — jfb9x stands.
- No change to GoReleaser changelog filters, the `cog bump` tag flow, or
  `release.yaml`.
- No RSS / blog-stream plugin in v1 (revisitable later; see §7).
- No replacement of the mechanical GoReleaser list — the narrative augments it.

## Architecture

Four components, each independently understandable and testable:

| Component | Responsibility | Interface |
| --------- | -------------- | --------- |
| **Range collector** | Resolve `prev..new` tag range; gather commits, bead refs, closed epics, touched `theme:*` roadmap sections | inputs in → structured `ReleaseInput` (markdown context block) |
| **Narrative summarizer** (the skill body) | Cluster inputs by theme; draft TLDR + grouped highlights; flag coverage gaps; ask the maintainer when a theme is ambiguous | `ReleaseInput` → draft markdown |
| **Site emitter** | Write `site/src/content/docs/releases/<vX.Y.Z>.md` + update the releases index; register the `releases` sidebar topic on first run | draft → docs page (branch → PR) |
| **GitHub publisher** | Fetch the current release body, build a combined draft (narrative + existing GoReleaser notes), then `gh release edit <tag> --notes-file <combined>` | draft → GitHub Release body |

The skill (`/release-notes`) orchestrates all four interactively in-session. A
thin `task release:notes:collect -- <tag>` wrapper documents the entry point and
shells to the skill's collector for the non-interactive data-gathering parts.

### Why a skill, not a CI script

The maintainer is already in the session; the summarizing model **is** the
in-loop model. "Generate → human edits → publish" collapses into one interactive
surface. A headless CI script would reintroduce an API secret and a draft-review
round-trip for no benefit, and could not ask the maintainer to disambiguate a
theme. This also keeps the release-cut path (`release.yaml`, App-token,
dispatch-only) completely untouched.

## Data Flow

```text
tag range prev..new
        │
        ▼
┌─────────────────────┐
│  Range collector    │  git log prev..new (subjects + holomush-<id> refs)
│                     │  bd: closed epics whose children landed in range
│                     │  docs/roadmap.md: theme:* sections touched in range
└─────────┬───────────┘
          ▼  ReleaseInput (structured markdown context)
┌─────────────────────┐
│ Narrative summarizer│  group by theme → TLDR + Features + Fixes
│   (in-session)      │  coverage check: every commit maps to a theme/epic
│                     │  OR appears under "Other changes" (never dropped)
└─────────┬───────────┘
          ▼  draft markdown  (maintainer edits inline)
     ┌────┴─────┐
     ▼          ▼
GitHub Release   site/src/content/docs/releases/<vX.Y.Z>.md
(gh release      + releases/index.mdx update
 edit            (feature branch → PR; NOT at tag-cut)
 --notes-file)
```

### Narrative source precedence

1. **`theme:<slug>` roadmap sections** touched in range → headline narrative
   (the *why*; already human-written).
2. **Closed bead epics** whose children landed in range (mapped via the
   `(holomush-<id>)` commit-subject refs) → per-theme feature bullets.
3. **Filtered commit subjects** (GoReleaser's set) → fallback for commits with no
   bead ref, grouped under "Other changes," and a coverage cross-check.

### Coverage guarantee

The summarizer MUST account for every non-excluded commit in the range: each one
either maps to a theme/epic highlight or appears under an "Other changes"
catch-all. The skill MUST emit a visible warning listing any commit it could not
place, so silent omission is impossible (mirrors the project's "no silent caps"
convention).

## Surfaces

### GitHub Release body

`gh release edit <tag> --notes-file <file>` **replaces** the entire release body
with the file's contents — it does not prepend. So the GitHub publisher MUST:

1. Capture the existing GoReleaser-generated body:
   `gh release view <tag> --json body -q .body`.
2. Build a **combined draft** = narrative section, a horizontal-rule separator,
   then the captured GoReleaser body verbatim.
3. Publish the combined draft: `gh release edit <tag> --notes-file <combined>`.

The publisher MUST refuse to run with `--notes` (inline) or with a narrative-only
file — both would drop the mechanical list and violate INV-7. If step 1 returns an
empty body, the publisher MUST **fail closed**: error out and refuse to publish
until the GoReleaser body is present. An empty body means GoReleaser hasn't run or
the wrong tag was given — a warn-and-continue would still emit a narrative-only
release and silently break the INV-7 contract, so the anomaly MUST be surfaced,
not papered over.

This is a GitHub-side edit only — no Git write to `main`, so jfb9x's INV-3
(no protected-main write at release) is preserved.

### Docs site — plain docs section

A new `site/src/content/docs/releases/` section:

- `index.mdx` — "Release history" landing page, reverse-chronological links.
  `.mdx` (not `.md`) to match every existing topic landing page
  (`guide/index.mdx`, `contributing/index.mdx`, …) and to allow Starlight's
  `<CardGrid>`/`<LinkCard>` layout components.
- `<vX.Y.Z>.md` — the per-release **narrative section only** (the same prose as
  the narrative header published to the GitHub Release, **without** the mechanical
  GoReleaser list or the `---` separator — those stay on the GitHub Release body,
  not the site page).

**Required one-time config edit.** The site's sidebar is driven by
`starlightSidebarTopics` in `site/astro.config.mjs:46-80`, which enumerates five
explicit topics (`guide`, `operating`, `extending`, `contributing`, `reference`).
A directory not registered there produces pages that are URL-reachable but
**sidebar-invisible** — and `task docs:build` + `task lint:markdown` both pass, so
the orphaning slips every local gate. The site emitter MUST therefore add a
sixth `releases` topic entry (label `Releases`, `link: '/releases/'`, an icon,
`items: [{ autogenerate: { directory: 'releases' } }]`) on its first run. This is
a single config edit — **no new npm dependency** — but it is mandatory, not
optional.

This is a **normal docs change**: it lands on a feature branch and merges via PR
like any other doc. It is authored **after** the tag is cut, never as part of the
release-cut run, so it does not reintroduce a release-time `main` write. (The
`starlight-blog` plugin — listings, tags, RSS — is a deliberate future option,
§7, not v1.)

## v0.10.0 Retroactive Backfill

Run `/release-notes v0.10.0` once against `v0.9.0..v0.10.0`:

1. Generate the narrative from the range.
2. Publish through the **same fetch-and-combine path** as the main publisher —
   `scripts/release-notes-publish.sh --tag v0.10.0 --narrative-file <draft>` — which
   captures the existing GoReleaser body and combines it under the narrative. Never
   a raw `gh release edit v0.10.0 --notes-file <draft>`: that would overwrite and
   drop the mechanical list (INV-7).
3. Create the first `site/src/content/docs/releases/v0.10.0.md` + index, via PR.

This validates the pipeline against a real, messy, month-long range before it
becomes the standing process.

## Error Handling

| Failure | Behavior |
| ------- | -------- |
| `gh` not authenticated / release missing | Skill aborts before publishing; draft is preserved locally for retry. |
| `bd` unavailable | Degrade to commit-subject-only narrative; emit a visible "bead graph unavailable — narrative quality reduced" warning. |
| Commit maps to no theme/epic | Placed under "Other changes" + listed in the coverage warning. Never dropped. |
| Tag range empty / prev tag unresolved | Hard error with the resolved range echoed; refuse to publish empty notes. |
| Site index merge conflict | Site emission is a separate PR step; a conflict there never blocks the GitHub Release body (independent sinks). |

## Testing

| Tier | What it covers |
| ---- | -------------- |
| Skill dry-run | `/release-notes <tag> --dry-run` collects the range and prints the draft without publishing; used to eyeball v0.10.0 output. |
| Collector unit (bats) | Range resolution + commit→bead mapping on a fixture range; assert coverage accounting (every commit placed). |
| Site lint | `task lint:markdown` on the generated `releases/*.mdx`/`*.md` (rumdl) — the page MUST pass the same gate as all docs. |
| Sidebar registration | Assert `site/astro.config.mjs` contains a `releases` topic after the site emitter runs — guards against the orphaned-page failure that local gates miss. |
| Publish guard | Assert the publisher fetches the existing body (`gh release view … -q .body`) and that the combined `--notes-file` body contains **both** the narrative and the GoReleaser list; assert it never uses `--notes` inline or a narrative-only file; assert it **fails closed** (exit ≠ 0, `release edit` never invoked) when the existing body is empty. |

No production Go code changes, so no Go coverage delta. The skill + collector
script live under `.claude/` and `scripts/`; bats covers the collector.

## Invariants

This design introduces no new system-level invariant in
`docs/architecture/invariants.yaml`. It **relies on** and MUST NOT violate the
existing jfb9x release invariants:

- **INV-3 (no protected-`main` write at release):** the GitHub Release body is
  edited via `gh`, and the site post is a separate post-tag PR — neither writes to
  `main` during the release-cut run.
- **INV-7 (GoReleaser notes preserved):** the narrative is placed **above** the
  retained GoReleaser body (via fetch-then-combine, not a literal prepend); the
  mechanical filtered list is never replaced or fed through `--release-notes`.

## Future Options (out of scope for v1)

- Adopt `starlight-blog` (`/hideoo/starlight-blog`) for an RSS-backed "What's New"
  feed if release cadence and readership justify the dependency.
- A `task release:notes:check` that fails CI if a published tag has no
  corresponding `releases/<tag>.md` site page (closes the loop without a
  release-time write).
<!-- adr-capture: sha256=ead0564189e75362; session=cli; ts=2026-06-28T20:07:54Z; adrs=holomush-avxk7,holomush-3rmph,holomush-qoxsv -->

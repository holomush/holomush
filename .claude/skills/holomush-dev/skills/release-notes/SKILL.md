---
name: release-notes
description: Draft and publish a narrative TLDR for a release (GitHub Release body + docs site). Invoke as /holomush-dev:release-notes <vX.Y.Z>.
disable-model-invocation: true
---

Produce human-readable narrative release notes for tag **$ARGUMENTS** (a
`vX.Y.Z` tag). The narrative augments — never replaces — GoReleaser's
mechanical commit list. Do NOT create an in-repo CHANGELOG.md (ADR
holomush-jfb9x).

**Steps:**

1. **Gather context.** Run
   `uv run "${CLAUDE_PLUGIN_ROOT}/scripts/release_notes_collect.py" $ARGUMENTS`
   (or `task release:notes:collect -- $ARGUMENTS`). Read the structured block:
   filtered commits, referenced beads (with theme labels), coverage gaps, and
   the roadmap theme pointer.

2. **Read `docs/roadmap.md`** theme sections matching the referenced beads'
   `theme:*` labels — these carry the *why* and become the narrative headlines.

3. **Draft the narrative** to a temp file. Structure: a 2–3 sentence TLDR, then
   theme-grouped Features, then Fixes, then an "Other changes" catch-all that
   MUST account for every commit in the "Coverage gaps" section — nothing is
   silently dropped. If a commit maps to no theme/epic and is non-trivial, ask
   the maintainer rather than guessing.

4. **Publish to GitHub** (only after the maintainer approves the draft):
   `uv run "${CLAUDE_PLUGIN_ROOT}/scripts/release_notes_publish.py" --tag $ARGUMENTS --narrative-file <temp>`.
   The script fetches the existing GoReleaser body and combines; never pass a
   narrative-only body.

5. **Emit the site post** as a SEPARATE post-tag docs change (feature branch →
   PR): write `site/src/content/docs/releases/$ARGUMENTS.md` (same narrative
   body) and add a reverse-chronological link to
   `site/src/content/docs/releases/index.mdx`. Confirm the `releases` topic is
   registered in `site/astro.config.mjs` or the page orphans silently. The
   frontmatter MUST set `slug: releases/$ARGUMENTS` — a `vX.Y.Z` filename would
   otherwise be slugified to a dot-stripped URL (`v0.10.0` → `/releases/v0100/`),
   breaking the index link and the docs-IA parity gate.

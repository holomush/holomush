<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-145ko; do not edit manually; use `/adr update holomush-145ko` -->

# Adopt Astro Starlight as the docs site platform

**Date:** 2026-05-27
**Status:** Accepted
**Decision:** holomush-145ko
**Deciders:** Sean Brandt

## Context

The HoloMUSH docs site was built on zensical (Python/uv, MkDocs-style). It had accumulated nav drift (~20 markdown files orphaned from the hand-maintained `zensical.toml` nav, including the core `binary-plugins`/`lua-plugins` author guides), had no path to emit `llms.txt`, and was the lone Python/uv toolchain surface in an otherwise Node-toolchain repo (the SvelteKit web client uses Node/pnpm). A decision was needed: stay on zensical, migrate to a Node-based documentation framework, or build a custom solution.

## Decision

Adopt **Astro Starlight** as the documentation platform. SP1 executes a strict **lift-and-shift** (platform swap only — identical content, structure, and navigation; no information-architecture changes) so that follow-on sub-projects can leverage Starlight's `autogenerate` sidebar (SP2 Diátaxis re-bucketing) and the `starlight-llms-txt` plugin on a proven substrate.

## Rationale

- `llms.txt`/`llms-full.txt`/`llms-small.txt` are near-zero cost on Starlight (the maintained `starlight-llms-txt` plugin) and non-trivial custom work on any other platform.
- Starlight's `autogenerate`-sidebar-from-directory structurally eliminates the nav-drift class of bug; this cannot be realized on zensical's hand-maintained nav.
- Consolidating on the Node toolchain removes the lone Python/uv surface from CI and contributor environments.
- Separating the platform swap (SP1) from the IA redesign (SP2) keeps each change diagnosable in isolation — re-platforming and re-architecting simultaneously is the migration anti-pattern that makes failures undiagnosable.

## Alternatives Considered

- **Stay on zensical** — no migration cost and no content-loss risk, but no llms.txt plugin exists, the hand-maintained nav is structurally unable to eliminate drift, and it keeps Python/uv as a toolchain island. Rejected.
- **Custom SSG or another Node framework** — maximum control over output, but no maintained llms.txt plugin, no autogenerate sidebar, and high build cost with no ecosystem leverage. Rejected.
- **Astro Starlight** — first-class autogenerate sidebar, drop-in llms.txt plugin, Node toolchain consistent with the web client, strong MDX support, Cloudflare Pages deploy unchanged. Migration cost (codemods for admonitions, content tabs, ~165 internal links) is one-time and bounded. Chosen.

## Consequences

**Positive:** nav drift becomes structurally preventable via autogenerate (SP2); `llms.txt`/`llms-full.txt`/`llms-small.txt` emitted from day one; single Node toolchain across web client and docs; Pagefind search replaces zensical's built-in with no extra config.

**Negative:** one-time migration cost (frontmatter codemods, admonition rewrites, ~165 internal-link rewrites across ~45 files); `plugin-guide.md` is forced to `.mdx` (MDX strictness is a new failure mode for that file); URL slugs may change (accepted — no external link contracts).

**Neutral:** the Cloudflare Pages project (`holomush-site`) and deploy target are unchanged; `rumdl` markdown lint continues unchanged, with `astro check` added for the single `.mdx` file.

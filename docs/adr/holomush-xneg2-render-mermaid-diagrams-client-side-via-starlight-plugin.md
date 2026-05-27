<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-xneg2; do not edit manually; use `/adr update holomush-xneg2` -->

# Render Mermaid diagrams client-side via Starlight plugin

**Date:** 2026-05-27
**Status:** Accepted
**Decision:** holomush-xneg2
**Deciders:** Sean Brandt

## Context

The HoloMUSH docs site migrates from zensical to Astro Starlight (SP1). Seven content pages use Mermaid fenced code blocks that must render on the new platform. Starlight ships `astro-expressive-code` as its built-in syntax highlighter, which intercepts the Astro/remark pipeline in a way that conflicts with build-time Mermaid renderers. CI build containers do not include a browser runtime.

## Decision

Render Mermaid diagrams **client-side** via the `@pasqal-io/starlight-client-mermaid` Starlight plugin, rather than any build-time renderer.

## Rationale

- Build-time renderers (`astro-mermaid`, `rehype-mermaid`) conflict with Starlight's `astro-expressive-code` syntax highlighter, breaking code-block highlighting site-wide.
- `rehype-mermaid` additionally requires a headless Playwright browser in the CI build container, which is not available and would add significant image weight.
- The client-side Starlight plugin is a first-class plugin-API integration that sidesteps the Astro pipeline entirely, so expressive-code highlighting is unaffected and no browser runtime is needed in CI.

## Alternatives Considered

- **`astro-mermaid` (build-time Astro integration)** — static SVG at build time, no runtime JS, but conflicts with `astro-expressive-code` and breaks all syntax-highlighted code blocks. Rejected.
- **`rehype-mermaid` (build-time rehype plugin)** — well-maintained, hooks the rehype pipeline, but needs a Playwright browser in the CI build container and triggers the same expressive-code conflict. Rejected.
- **`@pasqal-io/starlight-client-mermaid` (client-side Starlight plugin)** — avoids both constraints; single `bun add`; diagrams render in the browser at runtime. Chosen.

## Consequences

**Positive:** site-wide code-block syntax highlighting is unaffected; the CI build container needs no browser runtime; new Mermaid pages need no build-infrastructure change.

**Negative:** Mermaid diagrams require JavaScript to render — invisible to JS-disabled clients and to static-HTML scrapers; rendering is deferred to page load rather than pre-built into HTML.

**Neutral:** the seven affected pages (`operating/crypto-runbook.md` plus six `contributing/` files) keep standard fenced Mermaid blocks — no authoring changes required.

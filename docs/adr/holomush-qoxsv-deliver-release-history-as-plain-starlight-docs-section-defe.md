<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-qoxsv; do not edit manually; use `/adr update holomush-qoxsv` -->

# Deliver release history as a plain Starlight docs section, defer starlight-blog plugin

**Date:** 2026-06-28
**Status:** Accepted
**Decision:** holomush-qoxsv
**Deciders:** HoloMUSH Contributors

## Context

The project needs a browsable release history on the docs site. The
starlight-blog plugin provides RSS feeds, tag listings, and blog-style layout.
The docs site already uses Starlight with five explicit `starlightSidebarTopics`
entries; adding a sixth requires a config edit.

## Decision

Release history publishes to a plain `site/src/content/docs/releases/` Starlight
section with a single `starlightSidebarTopics` config entry. The starlight-blog
plugin is deferred until release cadence and readership justify the dependency.

## Rationale

- No new npm dependency keeps the docs build simple and auditable.
- RSS and tag listings are only valuable at a release cadence/readership that does not yet exist.
- The mandatory sidebar config edit is a one-time cost; its absence causes orphaned pages that pass all local lint gates, so it is documented explicitly as a hazard.

## Alternatives Considered

- **Plain releases/ docs section + manual sidebar registration (chosen):** no new dependency; same Markdown format as the rest of the site; one config edit; upgrade path to starlight-blog preserved.
- **starlight-blog plugin (rejected for v1):** RSS, tag/category listings, blog layout out of the box, but a new npm dependency whose value only materializes at higher cadence/readership; would lock in a third-party plugin before validating the workflow.

## Consequences

- Positive: no new dependency; same doc format as the rest of the site; upgrade path to starlight-blog preserved.
- Negative: no RSS feed; the sidebar MUST be manually registered or pages are URL-reachable but sidebar-invisible.
- Neutral: starlight-blog remains a documented future option.

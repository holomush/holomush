<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-qf2oo; do not edit manually; use `/adr update holomush-qf2oo` -->

# Use bun as the docs-site package manager

**Date:** 2026-05-27
**Status:** Accepted
**Decision:** holomush-qf2oo
**Deciders:** Sean Brandt

## Context

The Astro Starlight migration (see `holomush-145ko`) requires selecting a Node package manager for the docs site. The repo's SvelteKit web client (`web/`) already standardizes on `pnpm@11.1.3`. The project has a stated package-manager preference order — bun → pnpm → npm — but no prior docs-site Node surface existed to enforce repo-wide consistency.

## Decision

Use **bun** as the docs-site package manager, with a mandatory **pnpm → npm fallback** if bun is unavailable in a given environment. The lockfile is `bun.lock`. The divergence from `web/`'s pnpm is a deliberate, documented trade-off; reviewers MAY override to pnpm for uniformity without losing a lockfile.

## Rationale

- The docs site is an independent build with no shared dependency graph with `web/`; single-package-manager uniformity therefore provides limited practical benefit.
- The project's preference order (bun → pnpm → npm) is explicit; honoring it here is consistent with stated policy.
- The divergence is recorded and bounded: pnpm is the named fallback, so the decision is reversible to the uniformity-matching option at review time.

## Alternatives Considered

- **bun** — matches the stated bun-first preference, faster installs, `oven-sh/setup-bun` available in CI. Adds a second JS package manager to the repo. Chosen.
- **pnpm (matching `web/`)** — single package manager repo-wide; contributors already have it. But violates the stated preference, and the docs site shares no dependency graph with `web/`, so uniformity buys little. Rejected (named as the fallback).
- **npm** — universal, no setup step, but slowest with no advantage over bun/pnpm. Rejected.

## Consequences

**Positive:** faster CI install step for docs builds; consistent with the stated project package-manager preference.

**Negative:** two JS package managers coexist (bun for docs, pnpm for `web/`) — contributors must be aware of both; CI docs jobs must include an `oven-sh/setup-bun` step.

**Neutral:** a reviewer may override to pnpm; the spec explicitly flags this trade-off for that purpose.

<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-828tt; do not edit manually; use `/adr update holomush-828tt` -->

# Persistent authed chrome lives in a shared (authed)/+layout.svelte shell

**Date:** 2026-06-20
**Status:** Accepted
**Decision:** holomush-828tt
**Deciders:** Sean Brandt

## Context

Before this change there was no `web/src/routes/(authed)/+layout.svelte` — only a load-only `+layout.ts`. The Rail, footer, and surrounding chrome existed only on `/terminal` (rendered inside the terminal page) and were absent on `/scenes`, so the two sections read as separate apps. Persistent workspace chrome needed a home that wraps every authed section without coupling to any one section's internals.

## Decision

A new `(authed)/+layout.svelte` is the persistent authed shell: it owns the route-aware Rail, the persistent footer, and the section slot (`{@render children()}`). The root `+layout.svelte` retains only the globally-applicable chrome — TopBar and the global overlays (Composer, CommandPalette). Each section keeps its own inner layout (terminal panes; scenes 3-pane) unchanged.

The global keyboard handlers (⌘B rail, ⌘. sidebar, ⌘⇧E composer, ⌘L clear) STAY in the root layout for this change; relocating them into the authed shell was deferred as out of scope (it is a behavioral-surface change with no functional benefit to the shell unification). ⌘K palette likewise stays global in root.

## Rationale

- Matches SvelteKit's nested-layout design intent for auth-scoped chrome; the shell renders only for authed routes automatically, so the Rail/footer need no internal auth gate.
- Keeps the root layout from accumulating auth-gated conditionals on top of its existing terminal-gated conditionals.
- Section inner layouts are untouched, so the migration is additive (wrap), not invasive.
- Keeping key handlers in root avoids re-binding churn and an avoidable behavioral-surface change during a chrome-only refactor.

## Alternatives Considered

- **New (authed)/+layout.svelte shell (chosen):** idiomatic boundary; sections untouched; TopBar stays in root.
- **Per-section inner-layout duplication (rejected):** chrome copied into every section; the "one app" feel is never achieved; divergence is guaranteed as sections multiply.
- **Root layout with auth-conditional Rail/footer (rejected):** compounds the route-conditional logic already in the root TopBar; not idiomatic SvelteKit for auth-scoped chrome.
- **Relocate key handlers into the shell (deferred):** cleaner scoping, but a behavioral-surface change with no benefit to this refactor — handlers stay in root.

## Consequences

- Positive: adding future authed sections needs no shell change; pre-auth routes (`/login`, `/register`) never render the Rail or footer.
- Negative: the shell owns viewport height, so each section page must size to its container (the terminal sheds its own `calc(100vh - topbar)` or it overflows the footer).
- Neutral: key handlers remain in the root layout; a future change may relocate the authed-only ones into the shell if desired.

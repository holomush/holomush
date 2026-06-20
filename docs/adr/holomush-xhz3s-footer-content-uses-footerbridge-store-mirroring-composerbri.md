<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-xhz3s; do not edit manually; use `/adr update holomush-xhz3s` -->

# Footer content uses a footerBridge store mirroring composerBridge

**Date:** 2026-06-20
**Status:** Accepted
**Decision:** holomush-xhz3s
**Deciders:** Sean Brandt

## Context

The persistent shell footer must render content that varies by active section (the terminal's hotkey bar vs. a quiet baseline) without the shell layout importing section-specific components. SvelteKit nested layouts render children downward, so a section page cannot pass a snippet "up" into the shell footer via slots. The codebase already solves an analogous page→layout content-injection problem with `composerBridge.ts` (a writable store plus register/invoke helpers).

## Decision

A new `footerBridge` store (a `writable<Snippet | null>` with `setFooter`/`clearFooter`, the same shape as `composerBridge`) is the mechanism for sections to push footer content. `ShellFooter` renders the registered snippet via `{@render}`, falling back to a baseline (section name + ⌘K hint + connection dot) when nothing is registered. The terminal registers its hotkey bar as a snippet on mount and clears it on destroy.

## Rationale

- The composerBridge precedent is already proven and understood by contributors; reusing the pattern avoids inventing a second coordination mechanism.
- Register-on-mount / clear-on-destroy means the footer never renders a dead snippet, and a section with no special footer needs zero code — the baseline renders automatically.
- Storing a Svelte snippet (not plain markup) preserves the terminal's live state (line count, composer nudge) after relocation out of CommandInput.

## Alternatives Considered

- **footerBridge store, mirroring composerBridge (chosen):** decouples shell from section internals; proven pattern; always-present baseline keeps the frame visually closed.
- **Named slot / snippet passed through the layout hierarchy (rejected):** SvelteKit layouts do not hoist named slots up across layout boundaries cleanly; would require context or prop-drilling through the section's own layout.
- **Shell imports section-specific footer components (rejected):** couples the shell to every section that has footer content; adding a section means editing the shell.

## Consequences

- Positive: sections are fully decoupled from footer rendering; new sections get the baseline for free.
- Negative: two indirect coordination stores (composer + footer) make the data flow less obvious to new contributors; a malformed registered snippet could diverge from the footer's visual contract.
- Neutral: the terminal hotkey bar moves from `CommandInput.svelte` into a bridge-registered snippet — a relocation, not a rewrite. If Svelte scoped styles do not follow the relocated snippet, the `.cmd-hints` rules lift to `:global`.

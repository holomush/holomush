<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-8p5rx; do not edit manually; use `/adr update holomush-8p5rx` -->

# Mobile navigation uses a Sheet drawer hosting the same Rail

**Date:** 2026-06-20
**Status:** Accepted
**Decision:** holomush-8p5rx
**Deciders:** Sean Brandt

## Context

On viewports below 768px the persistent left Rail collapses to zero width, so a mobile user has no section navigation. The workspace must let users switch sections on mobile without permanently consuming screen space — especially on the terminal surface, where the software keyboard already limits visible lines.

## Decision

Mobile navigation (<768px) uses a shadcn `Sheet` (slide-over drawer) opened by a ☰ button in the TopBar (shown only when authenticated, on narrow viewports). The drawer hosts the SAME `SectionRail` component in an icon+label variant. Drawer open-state is a transient (non-persisted) store bridging the TopBar trigger to the shell's Sheet; the drawer closes on navigation, scrim tap, and Esc.

## Rationale

- Zero permanent screen space consumed — critical for terminal usability on mobile.
- One Rail component serves both desktop (icon-only column) and mobile (icon+label drawer); a single navigation mental model and one active-state code path.
- The `Sheet` primitive is already in the codebase (used by CreateSceneSheet) and provides scrim/Esc dismissal, so no custom dismiss logic is needed.

## Alternatives Considered

- **Sheet drawer reusing the Rail (chosen):** zero permanent space; one component; primitive already present.
- **Bottom tab bar (rejected):** conventional and always-visible, but permanently occupies vertical space and stacks above the footer — unacceptable on the terminal surface and fights the software keyboard.
- **Top tab bar / breadcrumb (rejected):** consumes vertical space, competes with the TopBar, and breaks from the desktop icon-rail aesthetic.

## Consequences

- Positive: desktop and mobile share the same section items and active-state logic; no extra permanent chrome on mobile.
- Negative: navigation takes an extra tap (☰ then section) on mobile; the drawer must be explicitly closed on navigation or it lingers after the route change.
- Neutral: the TopBar gains a ☰ button visible only to authenticated users on narrow viewports.

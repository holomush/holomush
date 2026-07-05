<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-4u3qe; do not edit manually; use `/adr update holomush-4u3qe` -->

# Manifest-declared focus redirects, not dispatcher-owned

**Date:** 2026-07-05
**Status:** Accepted
**Decision:** holomush-4u3qe
**Deciders:** Sean Brandt

## Context

Ambient conversational verbs (pose/say/ooc/emit) must reroute to a scene's IC/OOC stream when a connection is scene-focused, but the core dispatcher (`internal/command`) must stay plugin-agnostic. For holomush-g1qcw (focus-routed scene input), core-scenes chose a generic, manifest-declared `focus_redirects` table over teaching the dispatcher scene-specific routing directly. Spec: docs/superpowers/specs/2026-07-05-focus-routed-scene-input-design.md §4.1, §9.

## Decision

core-scenes declares `focus_redirects` (focus_kind, verbs, target_command) in its `plugin.yaml`; the loader compiles a verb-first table injected into the dispatcher via a generic `WithFocusRedirects` option, so redirection fires without the dispatcher knowing "scene" by name.

## Rationale

- `internal/command` MUST remain plugin-agnostic per the gateway-boundary and event-conventions ownership rules — plugin-owned vocabulary never lands in core.
- The mechanism generalizes to future focus kinds (e.g. a channel focus redirecting `say`) with only a manifest declaration, no core dispatcher change.

## Alternatives Considered

- **Manifest-declared generic focus_redirects table (chosen):** keeps `internal/command` ignorant of the scene command name and verb set; extends to future focus kinds with zero dispatcher changes; matches the existing plugin-ownership discipline. Cost: a new Manifest field, schema validation, and a loader-compiled verb-keyed table plus startup ambiguity checks.
- **Dispatcher-owned scene-specific redirect (rejected):** simpler and more direct — no manifest schema, loader table, or injected interface — but bakes plugin-specific knowledge (the "scene" command and its verb set) into `internal/command`, the exact coupling gateway-boundary and event-conventions forbid, and does not generalize to future focus kinds.

## Consequences

- Positive: a reusable mechanism for any future plugin needing focus-based redirection; the loader fails closed at startup on ambiguous (focus_kind, verb) collisions across plugins.
- Negative: a new manifest schema surface and load-time validation; understanding routing requires reading the compiled loader table rather than one dispatcher branch.
- Neutral: `focus_redirects` is optional — non-participating plugins are unaffected.
